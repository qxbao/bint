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
	// Kind: core | evolved. Skill tiến hoá: Status (active | deprecated), Stale (lý do lỗi thời).
	Kind   string `json:"kind"`
	Status string `json:"status,omitempty"`
	Stale  string `json:"stale,omitempty"`
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
	// Kind: core | evolved. Composed: đã ghép lớp phủ (chỉ skill gốc, khi không --core và mở được repository).
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
			if ev, err := skills.LoadEvolvedOne(k.Root, o.Name); err == nil {
				return SkillContent{Name: o.Name, Kind: "evolved", Content: ev.Content}, nil
			}
		}
		return SkillContent{}, fmt.Errorf("không có skill %q (gốc hoặc tiến hoá)", o.Name)
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
	if err := copyTree(filepath.Join(k.Root, ".bint", "skills"), filepath.Join(root, ".bint", "skills")); err != nil {
		return SyncPlan{}, err
	}
	for _, t := range targets {
		if err := copyTree(filepath.Join(k.Root, filepath.FromSlash(t.Dir)), filepath.Join(root, filepath.FromSlash(t.Dir))); err != nil {
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
		BackupDir: filepath.Join(tmp, "backups"), Manifest: m, Now: time.Now(), Files: k.Source().Files()})
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

// copyTree sao chép cây thư mục src sang dst (src không tồn tại thì bỏ qua).
func copyTree(src, dst string) error {
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
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
