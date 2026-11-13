package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/skills"
)

// Dịch vụ skill dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.3): `skill list/show/overlay/diff/versions`
// và `sync` có kiểu Options, Validate() và Defaults(cfg). `skill check/regress` nằm ở evolvesvc (cần gói evolve).

// SkillRow là một dòng của danh sách skill (cũng là dạng JSON của CLI).
type SkillRow struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Anchors   []string `json:"anchors"`
	Overlay   string   `json:"overlay"`
	Published string   `json:"published"`
	// Targets: trạng thái theo từng thư mục xuất bản (current | differs | missing).
	Targets map[string]string `json:"targets,omitempty"`
	Locked  string            `json:"locked,omitempty"`
	Problem string            `json:"problem,omitempty"`
	// Kind: core | evolved | custom. Skill tiến hoá: Status (active | deprecated), Stale (lý do lỗi thời).
	// Skill tuỳ biến: Files (số tệp phụ), Problem (lỗi lint hoặc lỗi nạp, nối bằng "; ").
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	Stale  string `json:"stale,omitempty"`
	Files  int    `json:"files,omitempty"`
}

// SkillList là kết quả `skill list`.
type SkillList struct {
	Rows []SkillRow
	// EvolvedProblems: skill tiến hoá không đọc được. Orphans: lớp phủ không ứng với skill gốc nào.
	EvolvedProblems []string
	Orphans         []string
}

// SkillListOptions: `bint skill list` (không có tham số; --json chỉ là định dạng đầu ra).
type SkillListOptions struct{}

func SkillListDefaults(config.Config) SkillListOptions { return SkillListOptions{} }
func (o SkillListOptions) Validate() error             { return nil }

// ListSkills liệt kê skill gốc (điểm neo, lớp phủ, trạng thái xuất bản theo thư mục) và skill tiến hoá.
func ListSkills(k *KnowledgeWS, o SkillListOptions) (SkillList, error) {
	if err := o.Validate(); err != nil {
		return SkillList{}, err
	}
	lock, _ := skills.ReadLock(k.Root)
	targets := harness.SkillTargets(k.Config.Harnesses.Enabled)
	out := SkillList{Rows: []SkillRow{}}
	publish := func(row *SkillRow, sk skills.Skill) {
		row.Targets = map[string]string{}
		var parts []string
		for _, t := range targets {
			st, _ := skills.Status(k.Root, t, sk)
			row.Targets[t.Dir] = st
			parts = append(parts, t.Dir+":"+st)
		}
		row.Published = strings.Join(parts, " ")
	}
	for _, core := range skills.Core() {
		row := SkillRow{Name: core.Name, Version: core.Hash(), Overlay: "-", Published: "-", Kind: "core"}
		if as, err := core.Anchors(); err == nil {
			for _, a := range as {
				row.Anchors = append(row.Anchors, a.Name)
			}
		}
		if e, ok := lock.Skills[core.Name]; ok {
			row.Locked = e.SHA256
		}
		composed, ov, err := skills.Composed(k.Root, core)
		if ov != nil {
			row.Overlay = fmt.Sprintf("%d khối", len(ov.Patches))
		}
		if err != nil {
			row.Overlay, row.Problem = "xung đột", err.Error()
		} else if len(targets) > 0 {
			publish(&row, composed)
		}
		out.Rows = append(out.Rows, row)
	}
	evolved, evProblems, _ := skills.LoadEvolved(k.Root)
	out.EvolvedProblems = evProblems
	files := k.Source().Files()
	for _, ev := range evolved {
		row := SkillRow{Name: ev.Meta.Name, Version: fmt.Sprintf("v%d", ev.Meta.Version), Overlay: "-", Published: "-", Kind: "evolved",
			Status: ev.Meta.Status, Anchors: ev.Meta.Triggers.Paths}
		if stale, why := ev.Stale(files); stale {
			row.Stale = why
		}
		if ev.Active() && row.Stale == "" && len(targets) > 0 {
			publish(&row, ev.AsSkill())
		}
		out.Rows = append(out.Rows, row)
	}
	custom, _ := skills.LoadCustom(k.Root)
	for _, cs := range custom {
		row := SkillRow{Name: cs.Name, Version: cs.AsSkill().Hash(), Overlay: "-", Published: "-", Kind: "custom", Files: len(cs.Files)}
		if e, ok := lock.Custom[cs.Name]; ok {
			row.Locked = e.SHA256
		}
		if errs, _ := skills.CheckCustom(k.Root, cs); len(errs) > 0 {
			row.Problem = strings.Join(errs, "; ")
		} else if len(targets) > 0 {
			publish(&row, cs.AsSkill())
		}
		out.Rows = append(out.Rows, row)
	}
	out.Orphans = skills.OrphanOverlays(k.Root)
	return out, nil
}

// SkillShowOptions: `bint skill show <tên>`.
type SkillShowOptions struct {
	Name string `arg:"name" form:"required,suggest=skills"`
	Core bool   `flag:"core"`
}

func SkillShowDefaults(config.Config) SkillShowOptions { return SkillShowOptions{} }

func (o SkillShowOptions) Validate() error {
	if o.Name == "" {
		return Usagef("cần tên skill")
	}
	return nil
}

// SkillContent là nội dung một skill.
type SkillContent struct {
	Name string
	// Kind: core | custom | evolved. Composed: đã ghép lớp phủ (chỉ skill gốc, khi không --core và mở được repository).
	Kind     string
	Composed bool
	Content  []byte
}

// ShowSkill trả nội dung skill: gốc (đã ghép lớp phủ nếu mở được repository ở dir, trừ khi Core) hoặc tiến hoá.
func ShowSkill(ctx context.Context, dir string, o SkillShowOptions) (SkillContent, error) {
	if err := o.Validate(); err != nil {
		return SkillContent{}, err
	}
	s, ok := skills.Get(o.Name)
	if !ok {
		if k, err := OpenKnowledge(ctx, dir); err == nil {
			if cs, err := skills.LoadCustomOne(k.Root, o.Name); err == nil && cs.Content != nil {
				return SkillContent{Name: o.Name, Kind: "custom", Content: cs.Content}, nil
			}
			if ev, err := skills.LoadEvolvedOne(k.Root, o.Name); err == nil {
				return SkillContent{Name: o.Name, Kind: "evolved", Content: ev.Content}, nil
			}
		}
		return SkillContent{}, fmt.Errorf("không có skill %q (gốc, tuỳ biến hoặc tiến hoá)", o.Name)
	}
	out := SkillContent{Name: s.Name, Kind: "core"}
	if !o.Core {
		if k, err := OpenKnowledge(ctx, dir); err == nil {
			c, _, err := skills.Composed(k.Root, s)
			if err != nil {
				return SkillContent{}, err
			}
			s, out.Composed = c, true
		}
	}
	out.Content = s.Content
	return out, nil
}

// SkillOverlayOptions: `bint skill overlay <tên>`.
type SkillOverlayOptions struct {
	Name string `arg:"name" form:"required,suggest=core_skills"`
}

func SkillOverlayDefaults(config.Config) SkillOverlayOptions { return SkillOverlayOptions{} }

func (o SkillOverlayOptions) Validate() error {
	if o.Name == "" {
		return Usagef("cần tên skill gốc")
	}
	return nil
}

// OverlayResult là kết quả tạo lớp phủ mẫu.
type OverlayResult struct {
	// Path tương đối với gốc repository; Abs là đường dẫn tuyệt đối (để mở $EDITOR).
	Path, Abs string
	// Existed: đã có tệp nên không ghi đè.
	Existed bool
}

// CreateOverlay tạo tệp lớp phủ mẫu (liệt kê điểm neo) cho một skill gốc; không bao giờ ghi đè tệp có sẵn.
func CreateOverlay(ctx context.Context, dir string, o SkillOverlayOptions) (OverlayResult, error) {
	if err := o.Validate(); err != nil {
		return OverlayResult{}, err
	}
	s, ok := skills.Get(o.Name)
	if !ok {
		return OverlayResult{}, fmt.Errorf("không có skill gốc %q", o.Name)
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return OverlayResult{}, err
	}
	rel := skills.OverlayPath(s.Name)
	p := filepath.Join(k.Root, filepath.FromSlash(rel))
	res := OverlayResult{Path: rel, Abs: p}
	if _, err := os.Stat(p); err == nil {
		res.Existed = true
		return res, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	body, err := skills.OverlayTemplate(s)
	if err != nil {
		return res, err
	}
	return res, k.writer("bint skill overlay").WriteFile(p, []byte(body), 0o644)
}

// SkillNewOptions: `bint skill new <tên>`.
type SkillNewOptions struct {
	Name string `arg:"name" form:"required"`
}

func SkillNewDefaults(config.Config) SkillNewOptions { return SkillNewOptions{} }

func (o SkillNewOptions) Validate() error {
	if o.Name == "" {
		return Usagef("cần tên skill")
	}
	return nil
}

// SkillNewResult là kết quả tạo skill tuỳ biến mẫu.
type SkillNewResult struct {
	// Path tương đối với gốc repository; Abs là đường dẫn tuyệt đối (để mở $EDITOR).
	Path, Abs string
	// Existed: đã có SKILL.md nên không ghi đè.
	Existed bool
}

// NewSkill tạo .bint/skills/<tên>/SKILL.md từ mẫu (qua lint không lỗi); không bao giờ ghi đè SKILL.md có sẵn.
func NewSkill(ctx context.Context, dir string, o SkillNewOptions) (SkillNewResult, error) {
	if err := o.Validate(); err != nil {
		return SkillNewResult{}, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return SkillNewResult{}, err
	}
	if err := skills.CheckCustomName(k.Root, o.Name); err != nil {
		return SkillNewResult{}, Usagef("%v", err)
	}
	rel := skills.CustomPath(o.Name, "SKILL.md")
	p := filepath.Join(k.Root, filepath.FromSlash(rel))
	res := SkillNewResult{Path: rel, Abs: p}
	if _, err := os.Lstat(p); err == nil {
		res.Existed = true
		return res, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	return res, k.writer("bint skill new").WriteFile(p, skills.CustomTemplate(o.Name), 0o644)
}

// SkillLintOptions: `bint skill lint [tên]` (không tên = mọi skill tuỳ biến).
type SkillLintOptions struct {
	Name string `arg:"name" form:"suggest=custom_skills"`
}

func SkillLintDefaults(config.Config) SkillLintOptions { return SkillLintOptions{} }
func (o SkillLintOptions) Validate() error             { return nil }

// SkillLintResult là kết quả kiểm một skill tuỳ biến.
type SkillLintResult struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// OK cho biết skill không có lỗi (cảnh báo không tính).
func (r SkillLintResult) OK() bool { return len(r.Errors) == 0 }

// LintSkills kiểm hình thức skill tuỳ biến (skills.CheckCustom): một skill theo tên, hoặc mọi skill khi Name rỗng.
// Tên là skill gốc/tiến hoá → lỗi cách dùng (lệnh này chỉ kiểm skill tuỳ biến).
func LintSkills(ctx context.Context, dir string, o SkillLintOptions) ([]SkillLintResult, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	var list []*skills.CustomSkill
	if o.Name != "" {
		cs, err := skills.LoadCustomOne(k.Root, o.Name)
		if err != nil {
			if _, core := skills.Get(o.Name); core {
				return nil, Usagef("%q là skill gốc: lệnh này chỉ kiểm skill tuỳ biến", o.Name)
			}
			if _, err := skills.LoadEvolvedOne(k.Root, o.Name); err == nil {
				return nil, Usagef("%q là skill tiến hoá: lệnh này chỉ kiểm skill tuỳ biến (kiểm bằng `bint skill check`)", o.Name)
			}
			return nil, Usagef("không có skill tuỳ biến %q (tạo bằng `bint skill new`)", o.Name)
		}
		list = []*skills.CustomSkill{cs}
	} else if list, err = skills.LoadCustom(k.Root); err != nil {
		return nil, err
	}
	out := []SkillLintResult{}
	for _, cs := range list {
		errs, warns := skills.CheckCustom(k.Root, cs)
		out = append(out, SkillLintResult{Name: cs.Name, Path: skills.CustomPath(cs.Name), Errors: nonNil(errs), Warnings: nonNil(warns)})
	}
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SkillDiffOptions: `bint skill diff <tên> [--from N] [--to N]`.
type SkillDiffOptions struct {
	Name string `arg:"name" form:"required,suggest=evolved_skills"`
	From int    `flag:"from"`
	To   int    `flag:"to"`
}

func SkillDiffDefaults(config.Config) SkillDiffOptions { return SkillDiffOptions{} }

func (o SkillDiffOptions) Validate() error {
	switch {
	case o.Name == "":
		return Usagef("cần tên skill tiến hoá")
	case o.From < 0 || o.To < 0:
		return Usagef("--from/--to phải ≥ 0")
	}
	return nil
}

// SkillDiffResult là bản thay đổi giữa hai phiên bản của skill tiến hoá.
type SkillDiffResult struct {
	Name     string
	From, To int
	// Current là phiên bản hiện tại của skill. OnlyVersion: skill chỉ có một phiên bản (From < 1) nên không có bản thay đổi.
	Current     int
	OnlyVersion bool
	// Same: hai phiên bản giống nhau. Diff: văn bản diff (rỗng nếu Same hoặc OnlyVersion).
	Same bool
	Diff string
}

// DiffSkill trả bản thay đổi giữa hai phiên bản của skill tiến hoá (mặc định: phiên bản trước → hiện tại).
func DiffSkill(ctx context.Context, dir string, o SkillDiffOptions) (SkillDiffResult, error) {
	if err := o.Validate(); err != nil {
		return SkillDiffResult{}, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return SkillDiffResult{}, err
	}
	ev, err := skills.LoadEvolvedOne(k.Root, o.Name)
	if err != nil {
		return SkillDiffResult{}, err
	}
	from, to := o.From, o.To
	if to == 0 {
		to = ev.Meta.Version
	}
	if from == 0 {
		from = to - 1
	}
	res := SkillDiffResult{Name: ev.Meta.Name, From: from, To: to, Current: ev.Meta.Version}
	if from < 1 {
		res.OnlyVersion = true
		return res, nil
	}
	a, err := skills.ReadVersion(k.Root, ev.Meta.Name, from)
	if err != nil {
		return res, err
	}
	b, err := skills.ReadVersion(k.Root, ev.Meta.Name, to)
	if err != nil {
		return res, err
	}
	res.Diff = skills.Diff(fmt.Sprintf("%s@v%d", ev.Meta.Name, from), fmt.Sprintf("%s@v%d", ev.Meta.Name, to), string(a), string(b))
	res.Same = res.Diff == ""
	return res, nil
}

// SkillVersionsOptions: `bint skill versions <tên>`.
type SkillVersionsOptions struct {
	Name string `arg:"name" form:"required,suggest=evolved_skills"`
}

func SkillVersionsDefaults(config.Config) SkillVersionsOptions { return SkillVersionsOptions{} }

func (o SkillVersionsOptions) Validate() error {
	if o.Name == "" {
		return Usagef("cần tên skill tiến hoá")
	}
	return nil
}

// SkillVersions trả siêu dữ liệu (gồm lịch sử phiên bản) của skill tiến hoá.
func SkillVersions(ctx context.Context, dir string, o SkillVersionsOptions) (skills.EvolvedMeta, error) {
	if err := o.Validate(); err != nil {
		return skills.EvolvedMeta{}, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return skills.EvolvedMeta{}, err
	}
	ev, err := skills.LoadEvolvedOne(k.Root, o.Name)
	if err != nil {
		return skills.EvolvedMeta{}, err
	}
	return ev.Meta, nil
}

// ---- sync ----

// SyncOptions: `bint sync`.
type SyncOptions struct {
	Force bool `flag:"force"`
}

func SyncDefaults(config.Config) SyncOptions { return SyncOptions{} }
func (o SyncOptions) Validate() error        { return nil }

func openSync(ctx context.Context, dir string) (*KnowledgeWS, error) {
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !k.Paths.Initialized() {
		return nil, errors.New("chưa `bint init` trên máy này")
	}
	return k, nil
}

// Sync ghép skill gốc với lớp phủ, xuất bản cho harness và cập nhật core.lock. Tự kiểm lại tại lúc ghi
// (tệp bị sửa tay thì xung đột, không ghi đè trừ Force). version là phiên bản Bint ghi vào core.lock.
func Sync(ctx context.Context, dir, version string, o SyncOptions) (SkillInstall, error) {
	if err := o.Validate(); err != nil {
		return SkillInstall{}, err
	}
	k, err := openSync(ctx, dir)
	if err != nil {
		return SkillInstall{}, err
	}
	return InstallSkills(k, version, o.Force)
}

// SyncFile là một tệp skill sẽ được `sync` xử lý.
type SyncFile struct {
	Skill  string
	Path   string
	Result fsafe.Outcome
	Detail string
	// Diff: thay đổi sẽ ghi (hiện tại → sau sync); rỗng nếu không đổi hoặc xung đột.
	Diff string
}

// SyncPlan là bản xem trước của `sync` (không ghi gì).
type SyncPlan struct {
	NoTargets bool
	Files     []SyncFile
	Conflicts int
	// Changes: số tệp sẽ được tạo/cập nhật/gỡ, kể cả core.lock (khi LockChanged): mọi chỗ dùng Changes
	// (kể cả "đã cập nhật" của màn hình) nhờ vậy không bỏ sót việc chỉ có core.lock cần ghi.
	Changes int
	// LockChanged: core.lock sau sync khác bản thật (bint_version khác hoặc thiếu tệp); LockDiff là thay đổi đó.
	LockChanged bool
	LockDiff    string
}

// PlanSync xem trước `sync` mà không ghi gì vào repository: chạy chính skills.Install trên một bản sao tạm
// của .bint/skills, các thư mục skill của harness và manifest, rồi so từng tệp với bản thật. Nhờ vậy
// kết quả (kể cả xung đột) đúng như Sync sẽ cho nếu repository không đổi trong lúc chờ xác nhận.
func PlanSync(ctx context.Context, dir, version string, o SyncOptions) (SyncPlan, error) {
	if err := o.Validate(); err != nil {
		return SyncPlan{}, err
	}
	k, err := openSync(ctx, dir)
	if err != nil {
		return SyncPlan{}, err
	}
	targets := harness.SkillTargets(k.Config.Harnesses.Enabled)
	if len(targets) == 0 {
		return SyncPlan{NoTargets: true}, nil
	}
	tmp, err := os.MkdirTemp("", "bint-sync-plan-*")
	if err != nil {
		return SyncPlan{}, err
	}
	defer os.RemoveAll(tmp)
	root := filepath.Join(tmp, "repo")
	if err := copyTree(filepath.Join(k.Root, ".bint", "skills"), filepath.Join(root, ".bint", "skills"), true); err != nil {
		return SyncPlan{}, err
	}
	// .bint/flows: flow tuỳ biến được xuất bản thành skill điều phối; symlink vẫn là lỗi của tệp như ở sync thật.
	if err := copyTree(filepath.Join(k.Root, ".bint", "flows"), filepath.Join(root, ".bint", "flows"), true); err != nil {
		return SyncPlan{}, err
	}
	for _, t := range targets {
		if err := copyTree(filepath.Join(k.Root, filepath.FromSlash(t.Dir)), filepath.Join(root, filepath.FromSlash(t.Dir)), false); err != nil {
			return SyncPlan{}, err
		}
	}
	agents := harness.SubagentFiles(k.Config)
	if len(agents) > 0 {
		if err := copyTree(filepath.Join(k.Root, filepath.FromSlash(harness.SubagentDir)), filepath.Join(root, filepath.FromSlash(harness.SubagentDir)), false); err != nil {
			return SyncPlan{}, err
		}
	}
	manifest := filepath.Join(tmp, "manifest.json")
	if data, err := os.ReadFile(k.Paths.Manifest); err == nil {
		if err := os.WriteFile(manifest, data, 0o644); err != nil {
			return SyncPlan{}, err
		}
	}
	m, err := fsafe.LoadManifest(manifest)
	if err != nil {
		return SyncPlan{}, err
	}
	outs, err := skills.Install(skills.InstallOptions{Root: root, Targets: targets, BintVersion: version, Force: o.Force,
		BackupDir: filepath.Join(tmp, "backups"), Manifest: m, Now: time.Now(), Files: k.Source().Files(), Agents: agents})
	if err != nil {
		return SyncPlan{}, err
	}
	plan := SyncPlan{}
	for _, out := range outs {
		f := SyncFile{Skill: out.Skill, Path: out.Path, Result: out.Result, Detail: out.Detail}
		switch out.Result {
		case fsafe.Conflict:
			plan.Conflicts++
		case fsafe.Created, fsafe.Updated, fsafe.Removed:
			plan.Changes++
			before, _ := os.ReadFile(filepath.Join(k.Root, filepath.FromSlash(out.Path)))
			after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(out.Path)))
			f.Diff = skills.Diff(out.Path+" (hiện tại)", out.Path+" (sau sync)", string(before), string(after))
		}
		plan.Files = append(plan.Files, f)
	}
	lockRel := filepath.FromSlash(skills.LockRel)
	before, _ := os.ReadFile(filepath.Join(k.Root, lockRel))
	after, _ := os.ReadFile(filepath.Join(root, lockRel))
	if string(before) != string(after) {
		plan.LockChanged, plan.Changes = true, plan.Changes+1
		plan.LockDiff = skills.Diff(skills.LockRel+" (hiện tại)", skills.LockRel+" (sau sync)", string(before), string(after))
	}
	return plan, nil
}

// copyTree sao chép cây thư mục src sang dst (src không tồn tại thì bỏ qua); chỉ tệp thường (và symlink nếu keepLinks).
func copyTree(src, dst string, keepLinks bool) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			// Bản xem trước phải thấy đúng cái Sync thật thấy: LoadCustom báo symlink trong skill tuỳ biến là vấn đề
			// (skill không được xuất bản), nên tái tạo nó (chỉ Lstat, không bao giờ đi theo). Chỉ làm với nguồn skill
			// (keepLinks): symlink trong thư mục xuất bản của harness mà tái tạo thì Install ghi xuyên qua nó vào
			// repository thật. Nơi không tạo được symlink (Windows không đặc quyền) thì bỏ qua như trước.
			if keepLinks {
				if link, err := os.Readlink(p); err == nil {
					_ = os.Symlink(link, target)
				}
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// Giữ bit thực thi của tệp nguồn: Install đồng bộ bit này cho script phụ (chmod +x không đổi nội dung), nên bản
		// xem trước phải thấy đúng cái Sync thật làm.
		perm := os.FileMode(0o644)
		if info, err := d.Info(); err == nil {
			perm |= info.Mode().Perm() & 0o111
		}
		return os.WriteFile(target, data, perm)
	})
}
