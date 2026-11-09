package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/knowledge"
	"bint/internal/ulid"
)

// agentEnvVars là các biến môi trường do harness đặt cho tiến trình con của agent.
var agentEnvVars = []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "GEMINI_CLI"}

// InAgentSession cho biết Bint đang được gọi từ bên trong phiên agent (theo biến
// môi trường harness đặt). Đây là lớp phòng vệ bổ sung để agent không tự duyệt tri
// thức (mục 15.5), không phải ranh giới bảo mật: agent có quyền shell vẫn có thể
// xoá biến môi trường. Kiểm soát chính là review thay đổi .bint/knowledge qua Git.
func InAgentSession() (bool, string) {
	for _, k := range agentEnvVars {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" && v != "0" {
			return true, k
		}
	}
	return false, ""
}

// ErrAgentForbidden: thao tác chỉ dành cho người.
var ErrAgentForbidden = errors.New("thao tác này chỉ dành cho người dùng, không chạy được từ trong phiên agent")

func requireHuman(op string) error {
	if in, k := InAgentSession(); in {
		return fmt.Errorf("%w (`%s`; phát hiện biến %s)", ErrAgentForbidden, op, k)
	}
	return nil
}

// GitIdentity trả về danh tính Git "Tên <email>" của người dùng.
func GitIdentity(ctx context.Context, dir string) string {
	name, _ := gitrepo.Run(ctx, dir, "config", "user.name")
	email, _ := gitrepo.Run(ctx, dir, "config", "user.email")
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case name != "":
		return name
	case email != "":
		return email
	}
	for _, k := range []string{"USER", "USERNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}

// KnowledgeWS là không gian làm việc cho các lệnh tri thức. Chỉ cần
// .bint/config.yml (không cần thư mục trạng thái) để chạy được cả trong thư mục
// đánh giá cô lập.
type KnowledgeWS struct {
	*Workspace
	// Root là thư mục chứa .bint/ đang dùng; đường dẫn trong tri thức tương đối với nó.
	Root  string
	Store knowledge.Store
}

// OpenKnowledge mở không gian làm việc tri thức.
func OpenKnowledge(ctx context.Context, dir string) (*KnowledgeWS, error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	if ws.Sources.Project == "" {
		return nil, ErrNotInitialized
	}
	root := filepath.Dir(filepath.Dir(ws.Sources.Project))
	return &KnowledgeWS{Workspace: ws, Root: root, Store: knowledge.NewStore(root)}, nil
}

// Source trả về nguồn cây làm việc cho kiểm tra.
func (k *KnowledgeWS) Source() knowledge.Source { return &knowledge.WorktreeSource{Root: k.Root} }

// Items đọc mọi mục; tệp hỏng trả về như lỗi đọc.
func (k *KnowledgeWS) Items() ([]*knowledge.Item, []knowledge.LoadError, error) {
	return k.Store.List()
}

// AddOptions điều khiển `bint knowledge add`.
type AddOptions struct {
	Type       string
	Title      string
	Body       string
	Paths      []string
	Symbols    []string
	Tags       []string
	Domains    []string
	Evidence   []string // path[:a-b][#symbol][@commit]
	Inputs     []string
	Origin     string // "" = tự xác định (agent nếu trong phiên agent, ngược lại human)
	ThirdParty bool
	Confidence string
	RunID      string
	EvalCase   string
	Now        time.Time
}

// AddResult là kết quả thêm mục.
type AddResult struct {
	Item   *knowledge.Item
	Check  knowledge.Result
	Saved  bool
	Origin string
}

// ParseEvidenceSpec đọc path[:a-b][#symbol][@commit].
func ParseEvidenceSpec(spec string) (knowledge.Evidence, error) {
	commit := ""
	if i := strings.LastIndex(spec, "@"); i > 0 {
		c := strings.TrimSpace(spec[i+1:])
		if c != "" && strings.Trim(strings.ToLower(c), "0123456789abcdef") == "" {
			commit, spec = c, spec[:i]
		}
	}
	e, err := knowledge.ParseEvidence(spec)
	e.Commit = commit
	return e, err
}

// AddKnowledge tạo một mục tri thức. Mục của người (origin human) ở trạng thái
// verified ngay, người tạo là người duyệt (mục 8.1); mục do agent tạo ở candidate.
// Mục không qua kiểm tra xác định được thì không được ghi.
func AddKnowledge(ctx context.Context, k *KnowledgeWS, opts AddOptions) (AddResult, error) {
	var res AddResult
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	inAgent, envVar := InAgentSession()
	origin := opts.Origin
	switch {
	case origin == "" && inAgent:
		origin = knowledge.OriginAgent
	case origin == "":
		origin = knowledge.OriginHuman
	case origin == knowledge.OriginHuman && inAgent:
		return res, fmt.Errorf("%w: agent không ghi được mục origin: human (phát hiện biến %s); mục do agent ghi sẽ chờ người duyệt", ErrAgentForbidden, envVar)
	case origin != knowledge.OriginHuman && origin != knowledge.OriginAgent:
		return res, fmt.Errorf("--origin phải là human hoặc agent (imported dành cho `bint knowledge import`, sau v1.0)")
	}
	res.Origin = origin
	author := GitIdentity(ctx, k.Root)
	it := &knowledge.Item{
		Schema: knowledge.SchemaVersion, Type: opts.Type, Status: knowledge.StatusCandidate,
		Title: strings.TrimSpace(opts.Title), Body: strings.TrimSpace(opts.Body),
		Scope:      knowledge.Scope{Paths: cleanList(opts.Paths), Symbols: cleanList(opts.Symbols), Tags: cleanList(opts.Tags), Domains: cleanList(opts.Domains)},
		Origin:     knowledge.Origin{Kind: origin, RunID: opts.RunID, Inputs: cleanList(opts.Inputs), ThirdParty: opts.ThirdParty, EvalCase: opts.EvalCase},
		Author:     author,
		Confidence: opts.Confidence,
		Version:    1,
		CreatedAt:  knowledge.Now(opts.Now),
	}
	if it.Title == "" {
		it.Title = firstLine(it.Body)
	}
	for i, p := range it.Scope.Paths {
		it.Scope.Paths[i] = strings.TrimPrefix(filepath.ToSlash(p), "./")
	}
	if s := os.Getenv("CLAUDE_CODE_SESSION_ID"); s != "" && origin == knowledge.OriginAgent {
		it.Origin.Session = s
	}
	it.Origin.Commit = strings.TrimSpace(gitrepo.HeadCommit(ctx, k.Root))
	for _, spec := range opts.Evidence {
		e, err := ParseEvidenceSpec(spec)
		if err != nil {
			return res, err
		}
		it.Evidence = append(it.Evidence, e)
	}
	src := k.Source()
	for i := range it.Evidence {
		e := &it.Evidence[i]
		if b, ok := src.Read(e.Path); ok {
			if fp, err := knowledge.FingerprintWith(*e, b, k.Config.Context.Freshness.Fingerprint); err == nil {
				e.Fingerprint = fp
			}
		}
	}
	if origin == knowledge.OriginHuman {
		it.Status = knowledge.StatusVerified
		it.ReviewedBy, it.ReviewedAt, it.VerifiedAt = author, knowledge.Now(opts.Now), knowledge.Now(opts.Now)
	}
	all, _, err := k.Items()
	if err != nil {
		return res, err
	}
	// Cấp ID trước khi kiểm tra để phát hiện trùng lặp không nhầm với chính nó.
	it.ID = newID(opts.Now)
	res.Item = it
	res.Check = knowledge.Verifier{Src: src, All: all}.Verify(it)
	if !res.Check.OK() {
		return res, nil
	}
	if err := k.Store.Save(it); err != nil {
		return res, err
	}
	res.Saved = true
	return res, nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	l = strings.TrimSpace(strings.TrimLeft(l, "#-* "))
	if r := []rune(l); len(r) > knowledge.MaxTitleRunes {
		l = string(r[:knowledge.MaxTitleRunes-1]) + "…"
	}
	return l
}

func cleanList(in []string) []string {
	var out []string
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// VerifyReport là kết quả kiểm tra toàn bộ tri thức.
type VerifyReport struct {
	Results     []knowledge.Result
	Items       map[string]*knowledge.Item
	LoadErrors  []knowledge.LoadError
	Transitions []AppliedTransition
	// Relocated: mục được cập nhật vùng dòng (nội dung bằng chứng không đổi).
	Relocated []string
	// DeprecateProposals: mục stale quá knowledge.stale_deprecate_days ngày (mục 8.4).
	// Chỉ là đề xuất; người dùng quyết định bằng `bint knowledge deprecate`.
	DeprecateProposals []string
	// NeedsReview: mục stale rủi ro cao (rule, warning) cần người hoặc agent nhận định.
	NeedsReview []string
}

// AppliedTransition là một lần chuyển trạng thái đã áp dụng.
type AppliedTransition struct {
	ID, From, To, Reason string
}

// VerifyKnowledge chạy kiểm tra xác định được cho các mục (rỗng = mọi mục còn hiệu
// lực). Với apply: candidate đủ điều kiện → verified (bởi hệ thống, chỉ loại không
// cần người duyệt), verified có bằng chứng thay đổi/mất → stale.
func VerifyKnowledge(ctx context.Context, k *KnowledgeWS, refs []string, apply bool, now time.Time) (VerifyReport, error) {
	rep := VerifyReport{Items: map[string]*knowledge.Item{}}
	if apply {
		if err := requireHuman("bint knowledge verify --apply"); err != nil {
			return rep, err
		}
	}
	all, bad, err := k.Items()
	if err != nil {
		return rep, err
	}
	rep.LoadErrors = bad
	targets, err := selectItems(all, refs, func(it *knowledge.Item) bool { return it.Active() })
	if err != nil {
		return rep, err
	}
	v := knowledge.Verifier{Src: k.Source(), All: all}
	staleLimit := time.Duration(k.Config.Knowledge.StaleDeprecateDays) * 24 * time.Hour
	for _, it := range targets {
		r := v.Verify(it)
		rep.Results = append(rep.Results, r)
		rep.Items[it.ID] = it
		if it.Status == knowledge.StatusStale {
			if t, err := time.Parse(time.RFC3339, it.UpdatedAt); err == nil && now.Sub(t) > staleLimit {
				rep.DeprecateProposals = append(rep.DeprecateProposals, it.ID)
			} else if knowledge.HighRisk(it.Type) {
				rep.NeedsReview = append(rep.NeedsReview, it.ID)
			}
		}
		if !apply {
			continue
		}
		from := it.Status
		changed := false
		if r.ApplyRelocations(it) {
			// Chỉ cập nhật vị trí; nội dung bằng chứng không đổi nên trạng thái giữ nguyên.
			changed = true
			rep.Relocated = append(rep.Relocated, it.ID)
			r = v.Verify(it)
		}
		switch {
		case it.Status == knowledge.StatusVerified && (r.Has(knowledge.CodeFingerprint) || r.Has(knowledge.CodeEvidence) || r.Has(knowledge.CodeSymbol)) && !r.OK():
			reason := "bằng chứng thay đổi: " + firstError(r)
			if err := knowledge.Transition(it, knowledge.TransitionInput{To: knowledge.StatusStale, Actor: knowledge.Actor{Kind: knowledge.ActorSystem}, Reason: reason, Now: now}); err == nil {
				changed = true
			}
		case it.Status == knowledge.StatusCandidate && r.OK():
			err := knowledge.Transition(it, knowledge.TransitionInput{
				To: knowledge.StatusVerified, Actor: knowledge.Actor{Kind: knowledge.ActorSystem}, ChecksPassed: true,
				RequireHuman: k.Config.Knowledge.RequireHumanApprovalFor, Now: now,
				Reason: "qua kiểm tra xác định được",
			})
			if err == nil {
				changed = true
			}
		case it.Status == knowledge.StatusStale && !knowledge.HighRisk(it.Type) && (r.OK() || r.OnlyErrors(knowledge.CodeFingerprint)):
			// Xác thực lại loại rủi ro thấp (mục 8.4 bước 1): đường dẫn, ký hiệu, ví dụ mã
			// vẫn khớp → làm mới fingerprint. Hệ thống không làm được thì để nguyên.
			cp := *it
			cp.Evidence = append([]knowledge.Evidence(nil), it.Evidence...)
			if knowledge.FillFingerprints(&cp, k.Source(), k.Config.Context.Freshness.Fingerprint) != nil {
				break
			}
			if r2 := v.Verify(&cp); !r2.OK() {
				break
			}
			err := knowledge.Transition(&cp, knowledge.TransitionInput{
				To: knowledge.StatusVerified, Actor: knowledge.Actor{Kind: knowledge.ActorSystem}, ChecksPassed: true,
				RequireHuman: k.Config.Knowledge.RequireHumanApprovalFor, Now: now,
				Reason: "xác thực lại: kiểm tra xác định được qua, fingerprint được làm mới",
			})
			if err == nil {
				*it = cp
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := k.Store.Save(it); err != nil {
			return rep, err
		}
		if it.Status != from {
			rep.Transitions = append(rep.Transitions, AppliedTransition{it.ID, from, it.Status, it.StatusReason})
		}
	}
	return rep, nil
}

func firstError(r knowledge.Result) string {
	for _, f := range r.Findings {
		if f.Level == knowledge.LevelError {
			return f.Detail
		}
	}
	return ""
}

func selectItems(all []*knowledge.Item, refs []string, def func(*knowledge.Item) bool) ([]*knowledge.Item, error) {
	if len(refs) == 0 {
		var out []*knowledge.Item
		for _, it := range all {
			if def(it) {
				out = append(out, it)
			}
		}
		return out, nil
	}
	var out []*knowledge.Item
	for _, r := range refs {
		it, err := knowledge.Find(all, r)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	return out, nil
}

// ReviewAction là một thao tác duyệt của người.
type ReviewAction struct {
	Refs []string
	// To: verified (approve) hoặc deprecated (reject/deprecate).
	To     string
	Reason string
	// Refresh: làm mới fingerprint của bằng chứng đã thay đổi (người xác nhận bằng
	// chứng hiện tại vẫn đúng).
	Refresh bool
	Now     time.Time
}

// ReviewOutcome là kết quả cho một mục.
type ReviewOutcome struct {
	ID, From, To string
	Err          error
	Check        *knowledge.Result
}

// ReviewKnowledge áp thao tác duyệt của người lên các mục. Chạy từ trong phiên
// agent bị từ chối.
func ReviewKnowledge(ctx context.Context, k *KnowledgeWS, act ReviewAction) ([]ReviewOutcome, error) {
	op := map[string]string{knowledge.StatusVerified: "bint knowledge approve", knowledge.StatusDeprecated: "bint knowledge reject/deprecate"}[act.To]
	if err := requireHuman(op); err != nil {
		return nil, err
	}
	if act.Now.IsZero() {
		act.Now = time.Now()
	}
	all, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	targets, err := selectItems(all, act.Refs, func(*knowledge.Item) bool { return false })
	if err != nil {
		return nil, err
	}
	who := GitIdentity(ctx, k.Root)
	v := knowledge.Verifier{Src: k.Source(), All: all}
	var outs []ReviewOutcome
	for _, it := range targets {
		o := ReviewOutcome{ID: it.ID, From: it.Status, To: act.To}
		in := knowledge.TransitionInput{To: act.To, Actor: knowledge.Actor{Kind: knowledge.ActorHuman, Name: who},
			Reason: act.Reason, RequireHuman: k.Config.Knowledge.RequireHumanApprovalFor, Now: act.Now}
		if act.To == knowledge.StatusVerified {
			if act.Refresh {
				if err := knowledge.FillFingerprints(it, k.Source(), k.Config.Context.Freshness.Fingerprint); err != nil {
					o.Err = err
					outs = append(outs, o)
					continue
				}
			}
			r := v.Verify(it)
			o.Check = &r
			in.ChecksPassed = r.OK()
			if !r.OK() && r.OnlyErrors(knowledge.CodeFingerprint) {
				o.Err = fmt.Errorf("bằng chứng đã thay đổi kể từ khi ghi; xem lại (`bint knowledge show %s`) rồi `bint knowledge approve --refresh %s`", it.ID, it.ID)
				outs = append(outs, o)
				continue
			}
			if in.Reason == "" {
				in.Reason = "người duyệt chấp nhận"
			}
		}
		if err := knowledge.Transition(it, in); err != nil {
			o.Err = err
			outs = append(outs, o)
			continue
		}
		if act.To == knowledge.StatusDeprecated && o.From == knowledge.StatusCandidate {
			it.ReviewedBy, it.ReviewedAt = who, knowledge.Now(act.Now)
		}
		if err := k.Store.Save(it); err != nil {
			o.Err = err
		}
		outs = append(outs, o)
	}
	return outs, nil
}

// RemoveKnowledge xoá tệp của mục candidate (ví dụ agent sửa lại mục vừa ghi).
// Mục đã duyệt phải được loại bỏ bằng deprecate để giữ dấu vết kiểm toán.
func RemoveKnowledge(k *KnowledgeWS, refs []string) ([]string, error) {
	all, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	targets, err := selectItems(all, refs, func(*knowledge.Item) bool { return false })
	if err != nil {
		return nil, err
	}
	inAgent, _ := InAgentSession()
	for _, it := range targets {
		if it.Status != knowledge.StatusCandidate {
			return nil, fmt.Errorf("%s ở trạng thái %s: chỉ xoá được mục candidate; dùng `bint knowledge deprecate`", it.ID, it.Status)
		}
		if inAgent && it.Origin.Kind != knowledge.OriginAgent {
			return nil, fmt.Errorf("%w: agent chỉ xoá được mục do agent tạo (%s)", ErrAgentForbidden, it.ID)
		}
	}
	var removed []string
	for _, it := range targets {
		if err := k.Store.Remove(it.ID); err != nil {
			return removed, err
		}
		removed = append(removed, it.ID)
	}
	return removed, nil
}

func newID(t time.Time) string { return ulid.At(t) }
