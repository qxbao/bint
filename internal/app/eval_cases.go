package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/store"
	"bint/internal/task"
	"bint/internal/workflow"
)

// EvalWorkspace là không gian làm việc của khung đánh giá (và của tiến hoá): cấu hình, kho
// tác vụ đánh giá, môi trường chạy và (khi cần) cơ sở dữ liệu trạng thái.
type EvalWorkspace struct {
	WS    *Workspace
	Store *store.Store // nil khi mở với withDB = false
	Cases eval.Store
	Env   eval.Env
}

// OpenEval mở không gian đánh giá ở repository chứa dir (cần đã `bint init`). withDB mở thêm cơ
// sở dữ liệu trạng thái; người gọi phải Close.
func OpenEval(ctx context.Context, dir string, withDB bool) (*EvalWorkspace, error) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	bintDir := filepath.Dir(ws.Sources.Project)
	e := &EvalWorkspace{
		WS:    ws,
		Cases: eval.NewStore(ws.Paths.Root, bintDir),
		Env:   eval.Env{RepoDir: ws.Repo.Toplevel, WorkRoot: ws.Paths.EvalWork, BintRoot: filepath.Dir(bintDir), Cfg: ws.Config},
	}
	if withDB {
		if e.Store, err = ws.OpenStore(ctx); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// Close đóng cơ sở dữ liệu (nếu có).
func (e *EvalWorkspace) Close() {
	if e.Store != nil {
		e.Store.Close()
	}
}

// EvalProgress là một mốc tiến độ của vòng kiểm chứng: Done = false ngay trước khi chạy tác vụ,
// true ngay sau khi có kết quả (Case.Verification đã cập nhật).
type EvalProgress struct {
	Index, Total int // Index tính từ 1
	Case         *eval.Case
	Done         bool
}

// Các khoá i18n của lời hỏi xác nhận trong nhóm eval.
const (
	ConfirmEvalRunKey       = "eval.run.confirm"
	ConfirmEvalBootstrapKey = "eval.bootstrap.confirm"
)

// verifyRejectPrefix mở đầu lý do loại do kiểm chứng (phân biệt với người duyệt loại).
const verifyRejectPrefix = "kiểm chứng: "

// ManualRejectReason chuẩn hoá lý do do người duyệt nhập để không bị nhầm với lý do loại tự
// động do kiểm chứng.
func ManualRejectReason(reason string) string {
	if strings.HasPrefix(reason, verifyRejectPrefix) {
		return "người duyệt: " + reason
	}
	return reason
}

// SettleVerification đồng bộ trạng thái ứng viên với kết quả kiểm chứng mới nhất. Ứng viên bị
// loại do kiểm chứng được chấm lại: đạt thì về chờ duyệt, vẫn không đạt thì cập nhật lý do. Ứng
// viên do người duyệt loại thì giữ nguyên.
func SettleVerification(c *eval.Case) {
	autoRejected := c.Status == eval.StatusRejected && strings.HasPrefix(c.RejectReason, verifyRejectPrefix)
	switch {
	case c.Verification.Status == "failed" && (c.Status == eval.StatusCandidate || autoRejected):
		c.Status = eval.StatusRejected
		c.RejectReason = verifyRejectPrefix + c.Verification.Detail
	case c.Verified() && autoRejected:
		c.Status = eval.StatusCandidate
		c.RejectReason = ""
	}
}

// applyVerification chạy kiểm chứng và cập nhật trạng thái tác vụ: ứng viên không đạt fail→pass
// bị loại; lỗi hạ tầng giữ nguyên trạng thái để chạy lại.
func (e *EvalWorkspace) applyVerification(ctx context.Context, c *eval.Case, runs int) {
	c.Verification = eval.VerifyCase(ctx, e.Env, c, runs, time.Now())
	SettleVerification(c)
}

// checkSplit: tên tập con hợp lệ (rỗng = không chọn).
func checkSplit(flag, split string) error {
	if split != "" && !slices.Contains(eval.Splits, split) {
		return Usagef("%s phải là build | validation | test", flag)
	}
	return nil
}

// ---- eval mine ----

// EvalMineOptions: `bint eval mine`.
type EvalMineOptions struct {
	Rev        string `flag:"rev"`
	Limit      int    `flag:"limit" form:"advanced"`
	Verify     bool   `flag:"verify"`
	VerifyRuns int    `flag:"verify-runs" form:"advanced"`
	// FromTasks: dựng ứng viên từ phiếu b-do đã done thay vì lịch sử Git.
	FromTasks bool `flag:"from-tasks"`
	// Progress báo tiến độ kiểm chứng từng ứng viên mới (chỉ khi Verify).
	Progress func(EvalProgress)
}

// EvalMineDefaults: giá trị mặc định (không phụ thuộc cấu hình).
func EvalMineDefaults(config.Config) EvalMineOptions {
	return EvalMineOptions{Rev: "HEAD", VerifyRuns: 2}
}

// Validate: chưa có ràng buộc mức giá trị (kiểm tra eval.test_command làm khi chạy).
func (o EvalMineOptions) Validate() error {
	if o.FromTasks && o.Rev != "" && o.Rev != "HEAD" {
		return Usagef("--from-tasks không đi cùng --rev")
	}
	return nil
}

// EvalMineResult là kết quả khai thác.
type EvalMineResult struct {
	eval.MineResult
	CandidatesDir string
}

// EvalMine khai thác ứng viên từ lịch sử Git; với Verify thì kiểm chứng fail→pass từng ứng viên
// mới. Khi bị huỷ giữa chừng trả ctx.Err() cùng kết quả đã có.
func EvalMine(ctx context.Context, e *EvalWorkspace, opts EvalMineOptions) (*EvalMineResult, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.Verify && !opts.FromTasks {
		if _, err := e.Env.TestCommand(&eval.Case{}); err != nil {
			return nil, err
		}
	}
	var res eval.MineResult
	var err error
	if opts.FromTasks {
		var srcs []eval.TaskSource
		if srcs, err = taskSources(e); err == nil {
			res, err = eval.MineTasks(ctx, e.WS.Repo.Toplevel, e.WS.Config.Eval.Mine, e.Cases, srcs, eval.MineOptions{Limit: opts.Limit})
		}
	} else {
		res, err = eval.Mine(ctx, e.WS.Repo.Toplevel, e.WS.Config.Eval.Mine, e.Cases, eval.MineOptions{Rev: opts.Rev, Limit: opts.Limit})
	}
	if err != nil {
		return nil, err
	}
	out := &EvalMineResult{MineResult: res, CandidatesDir: e.Cases.CandidatesDir}
	if opts.Verify {
		for i, c := range res.New {
			if opts.Progress != nil {
				opts.Progress(EvalProgress{Index: i + 1, Total: len(res.New), Case: c})
			}
			e.applyVerification(ctx, c, opts.VerifyRuns)
			if err := e.Cases.SaveCandidate(c); err != nil {
				return out, err
			}
			if opts.Progress != nil {
				opts.Progress(EvalProgress{Index: i + 1, Total: len(res.New), Case: c, Done: true})
			}
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
		}
	}
	return out, nil
}

// ---- eval verify ----

// EvalVerifyOptions: `bint eval verify [id...]`.
type EvalVerifyOptions struct {
	IDs      []string `arg:"id" form:"suggest=candidates"`
	All      bool     `flag:"all"`
	Runs     int      `flag:"runs" form:"advanced"`
	Progress func(EvalProgress)
}

func EvalVerifyDefaults(config.Config) EvalVerifyOptions { return EvalVerifyOptions{Runs: 2} }

// Validate: --all loại trừ danh sách ID; cần một trong hai.
func (o EvalVerifyOptions) Validate() error {
	if o.All && len(o.IDs) > 0 {
		return Usagef("--all không đi cùng danh sách ID")
	}
	if !o.All && len(o.IDs) == 0 {
		return Usagef("cần ID tác vụ hoặc --all")
	}
	return nil
}

// EvalVerifyResult: Total = số tác vụ được kiểm chứng (0 = không có gì cần làm).
type EvalVerifyResult struct {
	Total, Failed int
}

type evalTarget struct {
	c        *eval.Case
	approved bool
}

func (e *EvalWorkspace) save(t evalTarget) error {
	if t.approved {
		return e.Cases.SaveCase(t.c)
	}
	return e.Cases.SaveCandidate(t.c)
}

// EvalVerify kiểm chứng fail→pass cho ứng viên hoặc tác vụ đã duyệt (chạy eval.test_command).
func EvalVerify(ctx context.Context, e *EvalWorkspace, opts EvalVerifyOptions) (*EvalVerifyResult, error) {
	if _, err := e.Env.TestCommand(&eval.Case{}); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	var targets []evalTarget
	if opts.All {
		cands, err := e.Cases.Candidates()
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			if c.Status == eval.StatusCandidate && !c.Verified() {
				targets = append(targets, evalTarget{c, false})
			}
		}
		cases, err := e.Cases.Cases()
		if err != nil {
			return nil, err
		}
		for _, c := range cases {
			if !c.Verified() {
				targets = append(targets, evalTarget{c, true})
			}
		}
	} else {
		for _, id := range opts.IDs {
			c, approved, err := e.Cases.Get(id)
			if err != nil {
				return nil, err
			}
			targets = append(targets, evalTarget{c, approved})
		}
	}
	res := &EvalVerifyResult{Total: len(targets)}
	for i, t := range targets {
		if opts.Progress != nil {
			opts.Progress(EvalProgress{Index: i + 1, Total: len(targets), Case: t.c})
		}
		e.applyVerification(ctx, t.c, opts.Runs)
		if err := e.save(t); err != nil {
			return res, err
		}
		if opts.Progress != nil {
			opts.Progress(EvalProgress{Index: i + 1, Total: len(targets), Case: t.c, Done: true})
		}
		if !t.c.Verified() {
			res.Failed++
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
	}
	return res, nil
}

// ---- eval list ----

// EvalListOptions: `bint eval list`.
type EvalListOptions struct {
	All   bool   `flag:"all"`
	Split string `flag:"split" form:"choices=build|validation|test"`
}

func EvalListDefaults(config.Config) EvalListOptions { return EvalListOptions{} }

func (o EvalListOptions) Validate() error { return checkSplit("--split", o.Split) }

// EvalListResult: tác vụ đã duyệt và ứng viên chờ duyệt.
type EvalListResult struct {
	Cases, Pending          []*eval.Case
	CasesDir, CandidatesDir string
}

// EvalList liệt kê tác vụ đã duyệt (lọc theo tập) và ứng viên chờ duyệt (All: gồm cả đã loại).
func EvalList(e *EvalWorkspace, opts EvalListOptions) (*EvalListResult, error) {
	cases, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	cands, err := e.Cases.Candidates()
	if err != nil {
		return nil, err
	}
	res := &EvalListResult{CasesDir: e.Cases.CasesDir, CandidatesDir: e.Cases.CandidatesDir}
	for _, c := range cands {
		if c.Status == eval.StatusCandidate || (opts.All && c.Status == eval.StatusRejected) {
			res.Pending = append(res.Pending, c)
		}
	}
	if opts.Split != "" {
		cases = FilterSplit(cases, opts.Split)
	}
	res.Cases = cases
	return res, nil
}

// FilterSplit giữ các tác vụ thuộc tập split.
func FilterSplit(cs []*eval.Case, split string) []*eval.Case {
	var out []*eval.Case
	for _, c := range cs {
		if c.Split == split {
			out = append(out, c)
		}
	}
	return out
}

// ---- eval show ----

// EvalShowOptions: `bint eval show <id>`.
type EvalShowOptions struct {
	ID string `arg:"id" form:"required,suggest=eval_ids"`
}

func EvalShowDefaults(config.Config) EvalShowOptions { return EvalShowOptions{} }

func (o EvalShowOptions) Validate() error {
	if o.ID == "" {
		return Usagef("cần ID tác vụ")
	}
	return nil
}

// EvalShowResult: chi tiết một tác vụ.
type EvalShowResult struct {
	Case     *eval.Case
	Approved bool
	Path     string
	// TestCommand là lệnh chấm của tác vụ; TestCommandErr khác nil nếu chưa có.
	TestCommand    string
	TestCommandErr error
}

// EvalShow đọc tác vụ và kiểm tra lại rò rỉ trên đề bài hiện tại (có thể vừa được sửa tay).
func EvalShow(ctx context.Context, e *EvalWorkspace, opts EvalShowOptions) (*EvalShowResult, error) {
	c, approved, err := e.Cases.Get(opts.ID)
	if err != nil {
		return nil, err
	}
	if in, err := eval.BuildLeakInput(ctx, e.WS.Repo.Toplevel, c); err == nil {
		c.Leak = eval.CheckLeak(c.Prompt, in)
	}
	res := &EvalShowResult{Case: c, Approved: approved, Path: e.Cases.CandidatePath(c.ID)}
	if approved {
		res.Path = e.Cases.CasePath(c.ID)
	}
	res.TestCommand, res.TestCommandErr = e.Env.TestCommand(c)
	return res, nil
}

// ---- eval spec ----

// EvalSpecOptions: `bint eval spec [id...]`.
type EvalSpecOptions struct {
	IDs      []string `arg:"id" form:"suggest=candidates"`
	All      bool     `flag:"all"`
	Approved bool     `flag:"approved"`
}

func EvalSpecDefaults(config.Config) EvalSpecOptions { return EvalSpecOptions{} }

func (o EvalSpecOptions) Validate() error {
	if o.All && len(o.IDs) > 0 {
		return Usagef("--all không đi cùng danh sách ID")
	}
	if !o.All && len(o.IDs) == 0 {
		return Usagef("cần ID tác vụ hoặc --all")
	}
	return nil
}

// EvalSpecResult: số tác vụ đã phân tích và thống kê theo trạng thái/loại cảnh báo.
type EvalSpecResult struct {
	Analyzed int
	// Counts: khoá "pass", "warn", "interface" và các loại cảnh báo (interface_gap, exact_text, short_prompt).
	Counts map[string]int
}

// EvalSpec phân tích đặc tả đề bài và nối chữ ký giao diện kiểm thử.
func EvalSpec(ctx context.Context, e *EvalWorkspace, opts EvalSpecOptions) (*EvalSpecResult, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	var targets []evalTarget
	if opts.All {
		cands, err := e.Cases.Candidates()
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			if c.Status != eval.StatusApproved {
				targets = append(targets, evalTarget{c, false})
			}
		}
		if opts.Approved {
			cases, err := e.Cases.Cases()
			if err != nil {
				return nil, err
			}
			for _, c := range cases {
				targets = append(targets, evalTarget{c, true})
			}
		}
	} else {
		for _, id := range opts.IDs {
			c, approved, err := e.Cases.Get(id)
			if err != nil {
				return nil, err
			}
			if approved && !opts.Approved {
				return nil, Usagef("%s đã được duyệt: đổi đề bài làm đổi fingerprint; thêm --approved nếu chắc chắn", id)
			}
			targets = append(targets, evalTarget{c, approved})
		}
	}
	res := &EvalSpecResult{Counts: map[string]int{}}
	for _, t := range targets {
		in, err := eval.BuildLeakInput(ctx, e.WS.Repo.Toplevel, t.c)
		if err != nil {
			return res, err
		}
		eval.ApplySpec(ctx, e.WS.Repo.Toplevel, t.c, in)
		t.c.Leak = eval.CheckLeak(t.c.Prompt, in)
		if err := e.save(t); err != nil {
			return res, err
		}
		res.Counts[t.c.Spec.Status]++
		if len(t.c.Spec.Interface) > 0 {
			res.Counts["interface"]++
		}
		for _, f := range t.c.Spec.Findings {
			res.Counts[f.Kind]++
		}
	}
	res.Analyzed = len(targets)
	return res, nil
}

// ---- eval approve ----

// EvalApproveOptions: `bint eval approve <id>...`.
type EvalApproveOptions struct {
	IDs        []string `arg:"id" form:"required,suggest=candidates"`
	Force      bool     `flag:"force" form:"hidden"`      // chỉ đặt qua ô chọn của hộp đỏ
	Unverified bool     `flag:"unverified" form:"hidden"` // như Force
	Split      string   `flag:"split" form:"choices=build|validation|test"`
}

func EvalApproveDefaults(config.Config) EvalApproveOptions { return EvalApproveOptions{} }

func (o EvalApproveOptions) Validate() error {
	if len(o.IDs) == 0 {
		return Usagef("cần ít nhất một ID tác vụ")
	}
	return checkSplit("--split", o.Split)
}

// EvalApproveOutcome là kết quả của một ID. Đúng một trong: Already, Approved, Problem.
type EvalApproveOutcome struct {
	ID       string
	Already  bool   // đã được duyệt trước đó
	Approved bool   // vừa duyệt
	Path     string // tệp tác vụ đã duyệt
	Split    string
	Note     string // ghi chú kèm theo, đã có khoảng trắng đầu (rò rỉ, thiếu đặc tả, chưa kiểm chứng)
	Problem  string // lý do chưa duyệt được
}

// EvalApprove duyệt ứng viên vào tập đánh giá. Kết quả liệt kê theo thứ tự ID đầu vào; lỗi hệ
// thống giữa chừng trả kèm phần kết quả đã có.
func EvalApprove(ctx context.Context, e *EvalWorkspace, opts EvalApproveOptions) ([]EvalApproveOutcome, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	who := GitIdentity(ctx, e.WS.Repo.Toplevel)
	var outs []EvalApproveOutcome
	for _, id := range opts.IDs {
		c, approved, err := e.Cases.Get(id)
		if err != nil {
			outs = append(outs, EvalApproveOutcome{ID: id, Problem: err.Error()})
			continue
		}
		if approved {
			outs = append(outs, EvalApproveOutcome{ID: c.ID, Already: true})
			continue
		}
		problem := func(f string, a ...any) {
			outs = append(outs, EvalApproveOutcome{ID: c.ID, Problem: fmt.Sprintf(f, a...)})
		}
		if c.Status == eval.StatusRejected && !opts.Force {
			problem("%s đã bị loại (%s); dùng --force nếu vẫn muốn duyệt", c.ID, c.RejectReason)
			continue
		}
		in, err := eval.BuildLeakInput(ctx, e.WS.Repo.Toplevel, c)
		if err != nil {
			return outs, err
		}
		c.Leak = eval.CheckLeak(c.Prompt, in)
		if c.Leak.Status == "fail" && !opts.Force {
			problem("%s: đề bài lộ lời giải (%s) — sửa `prompt` trong %s hoặc dùng --force",
				c.ID, c.Leak.Findings[0].Detail, e.Cases.CandidatePath(c.ID))
			continue
		}
		if !c.Verified() && !opts.Unverified {
			problem("%s: chưa qua kiểm chứng fail→pass (`bint eval verify %s`) — hoặc dùng --unverified", c.ID, c.ID)
			continue
		}
		out := *c
		out.Status = eval.StatusApproved
		out.OriginalMessage = ""
		out.ReviewedBy, out.ReviewedAt = who, time.Now().UTC().Format(time.RFC3339)
		out.RejectReason = ""
		out.Split = opts.Split
		if out.Split == "" {
			out.Split = eval.AssignSplit(c.ID)
		}
		if err := e.Cases.SaveCase(&out); err != nil {
			return outs, err
		}
		c.Status = eval.StatusApproved
		if err := e.Cases.SaveCandidate(c); err != nil {
			return outs, err
		}
		note := ""
		if c.Leak.Status != "pass" {
			note = " (rò rỉ: " + c.Leak.Status + ")"
		}
		if c.Spec != nil && c.Spec.Status != "pass" {
			var kinds []string
			for _, f := range c.Spec.Findings {
				kinds = append(kinds, f.Kind)
			}
			note += " (thiếu đặc tả: " + strings.Join(kinds, ", ") + ")"
		}
		if !c.Verified() {
			note += " (chưa kiểm chứng)"
		}
		outs = append(outs, EvalApproveOutcome{ID: c.ID, Approved: true, Path: e.Cases.CasePath(c.ID), Split: out.Split, Note: note})
	}
	return outs, nil
}

// ---- eval reject ----

// EvalRejectOptions: `bint eval reject <id>...`.
type EvalRejectOptions struct {
	IDs    []string `arg:"id" form:"required,suggest=candidates"`
	Reason string   `flag:"reason"`
}

func EvalRejectDefaults(config.Config) EvalRejectOptions {
	return EvalRejectOptions{Reason: "người duyệt loại"}
}

func (o EvalRejectOptions) Validate() error {
	if len(o.IDs) == 0 {
		return Usagef("cần ít nhất một ID tác vụ")
	}
	return nil
}

// EvalReject loại ứng viên (khai thác lại sẽ không tạo lại) và trả ID đã loại; lỗi giữa chừng
// trả kèm phần đã loại.
func EvalReject(e *EvalWorkspace, opts EvalRejectOptions) ([]string, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	var done []string
	for _, id := range opts.IDs {
		c, approved, err := e.Cases.Get(id)
		if err != nil {
			return done, err
		}
		if approved {
			return done, fmt.Errorf("%s đã được duyệt; xoá %s nếu muốn bỏ khỏi tập đánh giá", c.ID, e.Cases.CasePath(c.ID))
		}
		c.Status = eval.StatusRejected
		c.RejectReason = ManualRejectReason(opts.Reason)
		if err := e.Cases.SaveCandidate(c); err != nil {
			return done, err
		}
		done = append(done, c.ID)
	}
	return done, nil
}

// ---- eval clean ----

// EvalCleanOptions: `bint eval clean`.
type EvalCleanOptions struct{}

func EvalCleanDefaults(config.Config) EvalCleanOptions { return EvalCleanOptions{} }
func (o EvalCleanOptions) Validate() error             { return nil }

// EvalCleanResult: số kho đệm commit gốc đã xoá và dung lượng giải phóng.
type EvalCleanResult struct {
	Removed int
	Freed   int64
}

// EvalClean xoá kho đệm commit gốc (<eval-work>/bases).
func EvalClean(e *EvalWorkspace, _ EvalCleanOptions) (EvalCleanResult, error) {
	n, freed, err := eval.CleanBases(e.Env.WorkRoot)
	return EvalCleanResult{Removed: n, Freed: freed}, err
}

// resolveArms trả về các nhánh sẽ chạy. Nhánh lấy từ cấu hình mà chưa có trong bản này thì bị
// bỏ kèm thông báo (ghi vào log); nhánh do người dùng chỉ định (explicit) thì báo lỗi dùng sai.
func (e *EvalWorkspace) resolveArms(arms []string, explicit bool, log func(string, ...any)) ([]string, error) {
	cfg := e.WS.Config
	var out []string
	for _, a := range arms {
		a = strings.TrimSpace(a)
		if slices.Contains(out, a) {
			continue
		}
		if err := eval.ArmNotImplemented(a); err != nil {
			if explicit {
				return nil, UsageError{err}
			}
			log("Bỏ nhánh %s: %v\n", a, err)
			continue
		}
		if err := eval.ArmRequirement(a, cfg); err != nil {
			if explicit {
				return nil, UsageError{err}
			}
			log("Bỏ nhánh %s: %v\n", a, err)
			continue
		}
		if a == eval.ArmMinimal && cfg.Eval.MinimalSource == "file" {
			p := filepath.Join(e.WS.Repo.Toplevel, filepath.FromSlash(cfg.Eval.MinimalContext))
			if _, err := os.Stat(p); err != nil {
				msg := fmt.Sprintf("chưa có %s (eval.minimal_source: file); đặt minimal_source: knowledge để dựng từ tri thức", cfg.Eval.MinimalContext)
				if explicit {
					return nil, fmt.Errorf("nhánh minimal: %s", msg)
				}
				log("Bỏ nhánh minimal: %s\n", msg)
				continue
			}
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("không còn nhánh nào để chạy")
	}
	return out, nil
}

// ---- eval mine --from-tasks ----

// taskSources đọc kho phiếu và dựng nguồn cho đề: chỉ phiếu workflow b-do ở trạng thái done. Phiếu không
// đọc được bị bỏ qua (kho phiếu hỏng không được chặn việc khai thác).
func taskSources(e *EvalWorkspace) ([]eval.TaskSource, error) {
	tasks, _ := task.NewStore(e.WS.Paths.Runtime).List()
	wenv := &workflow.Env{Root: e.WS.Repo.Toplevel, Cfg: e.WS.Config, Tasks: task.NewStore(e.WS.Paths.Runtime)}
	var out []eval.TaskSource
	for _, t := range tasks {
		st := &t.State
		if st.Workflow != "b-do" || st.Status != task.StatusDone {
			continue
		}
		ts := eval.TaskSource{
			ID: st.ID, Goal: t.Ticket.Goal, Request: t.Ticket.Request, Criteria: t.Ticket.Verify.Criteria,
			Base: st.Base, BaseTree: st.BaseTree,
		}
		if data, err := os.ReadFile(t.ArtifactPath("requirement")); err == nil {
			ts.Requirement = string(data)
		}
		if wenv.VerifyConfirmed(t) {
			ts.VerifyCommand = strings.TrimSpace(t.Ticket.Verify.Command)
		}
		switch {
		case st.Landed != nil && st.Landed.Tree != "":
			ts.AfterTree, ts.Files = st.Landed.Tree, slices.Clone(st.Landed.Files)
		default:
			for i := len(st.Verifications) - 1; i >= 0; i-- {
				if v := st.Verifications[i]; v.Passed && v.Tree != "" {
					ts.AfterTree = v.Tree
					break
				}
			}
			ts.Files = slices.Clone(st.Touched)
		}
		runs := slices.Clone(st.Runs)
		runs = append(runs, st.SessionRun)
		for _, a := range st.AgentRuns {
			runs = append(runs, a.RunID)
		}
		for _, v := range st.Verifications {
			runs = append(runs, v.RunID)
		}
		if rv, err := t.LoadReview(); err == nil {
			for _, r := range rv.Rounds {
				runs = append(runs, r.RunID)
			}
		}
		for _, r := range runs {
			if r != "" && !slices.Contains(ts.Runs, r) {
				ts.Runs = append(ts.Runs, r)
			}
		}
		for _, h := range st.History {
			if h.To == task.StatusDone {
				ts.DoneAt, ts.Author = h.At, h.By
			}
		}
		out = append(out, ts)
	}
	slices.SortFunc(out, func(a, b eval.TaskSource) int { return strings.Compare(a.DoneAt+a.ID, b.DoneAt+b.ID) })
	return out, nil
}
