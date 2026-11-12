package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/inspect"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/spool"
)

// ---- inspect ----

// InspectOptions: `bint inspect`.
type InspectOptions struct {
	MaxCommits int `flag:"max-commits"`
}

// InspectDefaults: quét tối đa 1000 commit để tìm điểm nóng.
func InspectDefaults(config.Config) InspectOptions { return InspectOptions{MaxCommits: 1000} }

func (o InspectOptions) Validate() error { return nil }

// Inspect thu thập dữ liệu xác định được về repository chứa dir; không ghi gì.
func Inspect(ctx context.Context, dir string, o InspectOptions) (*inspect.Report, error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	return inspect.Inspect(ctx, ws.Repo.Toplevel, inspect.Options{MaxCommits: o.MaxCommits})
}

// ---- bootstrap ----

// BootstrapOptions: `bint bootstrap`.
type BootstrapOptions struct {
	NoSkill bool `flag:"no-skill"`
	Force   bool `flag:"force"`
	// ReadOnly: chỉ khảo sát và đọc tri thức hiện có, không lưu inspect.json, không cài skill
	// (CLI `--json`). Không phải cờ của form.
	ReadOnly bool `form:"hidden"`
	// Version là phiên bản Bint ghi vào core.lock của skill.
	Version string `form:"hidden"`
}

func BootstrapDefaults(config.Config) BootstrapOptions { return BootstrapOptions{} }
func (o BootstrapOptions) Validate() error             { return nil }

// BootstrapReport là kết quả bootstrap. Khi Bootstrap trả lỗi, các trường đã điền trước lỗi vẫn hợp lệ.
type BootstrapReport struct {
	Inspect *inspect.Report
	// Items là các mục tri thức hiện có (mọi trạng thái); Bad là số tệp hỏng.
	Items []*knowledge.Item
	Bad   int
	// ItemsRead: đã đọc được kho tri thức (Items nil khi ItemsRead = true nghĩa là kho trống).
	ItemsRead bool
	// InspectPath: nơi đã lưu dữ liệu khảo sát (rỗng nếu không lưu).
	InspectPath string
	// Initialized: máy này đã có thư mục trạng thái (mới cài được skill, lưu được dữ liệu).
	Initialized bool
	Skills      *SkillInstall
	// Counts là số mục theo trạng thái.
	Counts map[string]int
}

// SkillInstall là kết quả cài skill gốc.
type SkillInstall struct {
	// NoTargets: không harness nào bật có adapter skill.
	NoTargets bool
	Outcomes  []skills.Outcome
	Conflicts int
}

// Bootstrap làm phần xác định được của bootstrap: khảo sát, lưu inspect.json, cài skill b-bootstrap.
// Thứ tự và điều kiện giống CLI cũ; lỗi giữa chừng vẫn trả báo cáo phần đã làm.
func Bootstrap(ctx context.Context, dir string, o BootstrapOptions) (*BootstrapReport, error) {
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	rep := &BootstrapReport{Initialized: k.Paths.Initialized(), Counts: map[string]int{}}
	if rep.Inspect, err = inspect.Inspect(ctx, k.Repo.Toplevel, inspect.Options{}); err != nil {
		return rep, err
	}
	items, bad, err := k.Items()
	if err != nil {
		return rep, err
	}
	rep.Items, rep.Bad, rep.ItemsRead = items, len(bad), true
	for _, it := range items {
		rep.Counts[it.Status]++
	}
	if o.ReadOnly {
		return rep, nil
	}
	if rep.Initialized {
		p := filepath.Join(k.Paths.Generated, "inspect.json")
		if data, err := json.MarshalIndent(rep.Inspect, "", "  "); err == nil {
			if err := fsafe.WriteAtomic(p, append(data, '\n'), 0o600); err == nil {
				rep.InspectPath = p
			}
		}
	}
	if !o.NoSkill && rep.Initialized {
		si, err := InstallSkills(k, o.Version, o.Force)
		rep.Skills = &si
		if err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// InstallSkills xuất bản skill gốc (đã ghép lớp phủ) tới các harness bật.
func InstallSkills(k *KnowledgeWS, version string, force bool) (SkillInstall, error) {
	var si SkillInstall
	targets := harness.SkillTargets(k.Config.Harnesses.Enabled)
	if len(targets) == 0 {
		si.NoTargets = true
		return si, nil
	}
	m, err := fsafe.LoadManifest(k.Paths.Manifest)
	if err != nil {
		return si, err
	}
	outs, err := k.writer("bint sync").InstallSkills(skills.InstallOptions{Root: k.Root, Targets: targets, BintVersion: version, Force: force,
		BackupDir: k.Paths.Backups, Manifest: m, Now: time.Now(), Files: k.Source().Files()})
	if err != nil {
		return si, err
	}
	if err := m.Save(); err != nil {
		return si, err
	}
	si.Outcomes = outs
	for _, o := range outs {
		if o.Result == fsafe.Conflict {
			si.Conflicts++
		}
	}
	return si, nil
}

// ---- bootstrap skill ----

// BootstrapSkillOptions: `bint bootstrap skill [tên]`.
type BootstrapSkillOptions struct {
	Name string `arg:"name"`
}

// BootstrapSkillDefaults: skill b-bootstrap.
func BootstrapSkillDefaults(config.Config) BootstrapSkillOptions {
	return BootstrapSkillOptions{Name: "b-bootstrap"}
}

func (o BootstrapSkillOptions) Validate() error { return nil }

// BootstrapSkill trả nội dung skill gốc (đã ghép lớp phủ nếu mở được repository ở dir).
func BootstrapSkill(ctx context.Context, dir string, o BootstrapSkillOptions) ([]byte, error) {
	name := o.Name
	if name == "" {
		name = "b-bootstrap"
	}
	s, ok := skills.Get(name)
	if !ok {
		return nil, fmt.Errorf("không có skill gốc %q", name)
	}
	if k, err := OpenKnowledge(ctx, dir); err == nil {
		if c, _, err := skills.Composed(k.Root, s); err == nil {
			s = c
		}
	}
	return s.Content, nil
}

// ---- context ----

// ContextOptions: `bint context ["<mô tả tác vụ>"]`.
type ContextOptions struct {
	Task    string   `arg:"task"`
	Paths   []string `flag:"paths" form:"suggest=files"`
	Budget  int      `flag:"budget" form:"advanced"`
	Session string   `flag:"session" form:"hidden"` // TUI không ghi nhận lượt truy xuất nên không có ô này
	// Verbose: CLI in lý do chọn/bỏ ra stderr; TUI luôn hiện (không phải trường form).
	Verbose bool `flag:"verbose" form:"hidden"`
	// NoTrack: không ghi nhận lượt truy xuất (spool, bộ nhớ phiên) và bỏ qua Session/BINT_SESSION_ID. TUI đặt
	// cờ này để hành động chỉ-đọc đúng là không ghi gì. Không phải cờ CLI.
	NoTrack bool
}

func ContextDefaults(config.Config) ContextOptions { return ContextOptions{} }

func (o ContextOptions) Validate() error {
	if strings.TrimSpace(o.Task) == "" && len(o.Paths) == 0 {
		return Usagef("cần mô tả tác vụ hoặc --paths")
	}
	return nil
}

// ContextResult là gói ngữ cảnh kèm cảnh báo không chặn (ví dụ không ghi nhận được lần truy xuất).
type ContextResult struct {
	Pack     ctxengine.Pack
	Warnings []string
}

// ContextPack truy xuất gói ngữ cảnh theo mô tả và/hoặc đường dẫn. Có phiên (Session hoặc biến
// BINT_SESSION_ID) thì ghi nhận lần truy xuất vào spool.
func ContextPack(ctx context.Context, dir string, o ContextOptions) (ContextResult, error) {
	var res ContextResult
	if err := o.Validate(); err != nil {
		return res, err
	}
	task := strings.TrimSpace(o.Task)
	c, err := OpenContext(ctx, dir)
	if err != nil {
		return res, err
	}
	defer c.Close()
	if o.Budget > 0 {
		c.Engine.MaxTokens = o.Budget
	}
	session := o.Session
	if session == "" {
		session = os.Getenv("BINT_SESSION_ID")
	}
	if o.NoTrack {
		session = ""
	}
	var mem SessionMemory
	exclude := map[string]bool{}
	track := session != "" && c.K.Paths.Initialized()
	if track {
		mem = NewSessionMemory(c.K.Paths.Runtime, "cli", session)
		if task == "" {
			exclude = mem.Seen()
		}
	}
	var p ctxengine.Pack
	if task == "" {
		p = c.Engine.ForPaths(o.Paths, exclude)
	} else if p, err = c.Engine.ForTask(ctx, task, o.Paths); err != nil {
		return res, err
	}
	if track {
		now := time.Now()
		ev := ContextEvent("cli", session, c.K.Repo.Toplevel, ChannelCLI, now.Format(time.RFC3339Nano), p, now)
		if _, err := spool.Write(c.K.Paths.Spool, ev); err != nil {
			res.Warnings = append(res.Warnings, "không ghi nhận được lần truy xuất: "+err.Error())
		}
		if task == "" {
			mem.Add(p.IDs())
		}
	}
	res.Pack = p
	return res, nil
}
