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

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/fsafe"
	"bint/internal/gitrepo"
	"bint/internal/harness/claude"
	"bint/internal/skills"
	"bint/internal/state"
	"bint/internal/store"
)

// InitOptions điều khiển `bint init`.
type InitOptions struct {
	Dir       string
	Harnesses []string // rỗng: lấy từ cấu hình sẵn có hoặc tự phát hiện
	// Hooks: nil = hỏi qua Confirm (nếu có), không thì bỏ qua.
	Hooks      *bool
	HooksScope claude.Scope
	// MCP: true = đăng ký MCP server vào .mcp.json, false = gỡ, nil = giữ nguyên.
	MCP      *bool
	AgentsMD bool // ghi khối Bint vào AGENTS.md và nhập lại trong CLAUDE.md
	Force    bool // ghi đè khối Bint đã bị sửa tay (có sao lưu)
	BintPath string
	// Version là phiên bản Bint (ghi vào .bint/skills/core.lock).
	Version string
	// NoSkills: không xuất bản skill gốc.
	NoSkills bool
	Confirm  func(question string) bool
	Now      func() time.Time
}

// StepStatus là trạng thái một bước khởi tạo.
type StepStatus string

const (
	StepCreated  StepStatus = "created"
	StepUpdated  StepStatus = "updated"
	StepOK       StepStatus = "ok"
	StepSkipped  StepStatus = "skipped"
	StepWarn     StepStatus = "warn"
	StepConflict StepStatus = "conflict"
)

// Step là một dòng trong báo cáo khởi tạo.
type Step struct {
	Name   string
	Status StepStatus
	Detail string
}

// InitReport là kết quả `bint init`.
type InitReport struct {
	Repo       gitrepo.Repo
	StateDir   string
	FirstInit  bool
	Harnesses  []string
	Steps      []Step
	NextSteps  []string
	HasProblem bool
}

func (r *InitReport) add(name string, st StepStatus, format string, a ...any) {
	r.Steps = append(r.Steps, Step{Name: name, Status: st, Detail: fmt.Sprintf(format, a...)})
	if st == StepConflict || st == StepWarn {
		r.HasProblem = true
	}
}

// Init khởi tạo Bint cho repository. Có tính lặp an toàn: chạy lại không mất dữ
// liệu và không ghi đè nội dung người dùng đã sửa.
func Init(ctx context.Context, opts InitOptions) (*InitReport, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HooksScope == "" {
		opts.HooksScope = claude.ScopeLocal
	}
	now := opts.Now()
	rep := &InitReport{}

	// 1. Thư mục gốc Git.
	repo, err := gitrepo.Discover(ctx, opts.Dir)
	if err != nil {
		return nil, err
	}
	rep.Repo = repo
	root := repo.Toplevel

	for _, h := range opts.Harnesses {
		if !slices.Contains(config.KnownHarnesses, h) {
			return nil, fmt.Errorf("harness không hỗ trợ %q (hỗ trợ: %s)", h, strings.Join(config.KnownHarnesses, ", "))
		}
	}

	// 2–4. Cấu hình dự án: tạo nếu chưa có, không bao giờ ghi đè.
	cfgRoot := root
	for _, r := range repo.ConfigRoots() {
		if _, err := os.Stat(config.ProjectPath(r)); err == nil {
			cfgRoot = r
			break
		}
	}
	cfgPath := config.ProjectPath(cfgRoot)
	_, statErr := os.Stat(cfgPath)
	cfgExists := statErr == nil
	rep.FirstInit = !cfgExists
	detected := detect.Harnesses()

	if !cfgExists {
		harnesses := opts.Harnesses
		if len(harnesses) == 0 {
			for _, h := range detected {
				if h.Present {
					harnesses = append(harnesses, h.Name)
				}
			}
		}
		if len(harnesses) == 0 {
			harnesses = []string{"claude"}
		}
		sug := detect.SuggestCommands(root)
		content := config.Render(config.InitParams{
			ProjectName:           filepath.Base(root),
			Harnesses:             harnesses,
			SuggestedTestCommand:  sug.Test,
			SuggestedSetupCommand: sug.Setup,
		})
		if err := writeNew(cfgPath, []byte(content)); err != nil {
			return nil, fmt.Errorf("tạo %s: %w", cfgPath, err)
		}
		rep.add(".bint/config.yml", StepCreated, "harness: %s", strings.Join(harnesses, ", "))
		if sug.Test != "" || sug.Setup != "" {
			rep.add("lệnh kiểm thử", StepSkipped, "đề xuất từ %s: test=%q setup=%q — chưa chạy; xác nhận bằng cách điền eval.test_command / eval.setup_command",
				sug.Reason, sug.Test, sug.Setup)
		} else {
			rep.add("lệnh kiểm thử", StepSkipped, "không phát hiện được; điền eval.test_command thủ công nếu có")
		}
	} else if cfgRoot != root {
		rep.add(".bint/config.yml", StepOK, "dùng cấu hình của worktree chính %s (chưa có trong worktree này)", cfgRoot)
	} else {
		rep.add(".bint/config.yml", StepOK, "đã tồn tại, giữ nguyên")
	}

	cfg, _, err := config.Load(cfgRoot, true)
	if err != nil {
		return nil, fmt.Errorf("cấu hình hiện có không hợp lệ, không thay đổi gì thêm:\n%w", err)
	}
	rep.Harnesses = cfg.Harnesses.Enabled
	if cfgExists && len(opts.Harnesses) > 0 && !slices.Equal(opts.Harnesses, cfg.Harnesses.Enabled) {
		rep.add("harness", StepWarn, "--harness khác harnesses.enabled trong cấu hình (%s); Bint không sửa cấu hình đã có — hãy chỉnh tay",
			strings.Join(cfg.Harnesses.Enabled, ", "))
	}
	for _, h := range cfg.Harnesses.Enabled {
		for _, d := range detected {
			if d.Name == h && !d.Present {
				rep.add("harness "+h, StepWarn, "không thấy chương trình hoặc thư mục cấu hình trên máy này")
			}
		}
	}

	// Thư mục trạng thái cục bộ (ngoài repository) và SQLite.
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return nil, err
	}
	rep.StateDir = paths.Root
	wasInit := paths.Initialized()
	if err := paths.Ensure(repo); err != nil {
		return nil, err
	}
	s, err := store.Open(ctx, paths.DB)
	if err != nil {
		return nil, err
	}
	v, _ := s.Version(ctx)
	s.Close()
	if wasInit {
		rep.add("thư mục trạng thái", StepOK, "%s (schema v%d)", paths.Root, v)
	} else {
		rep.add("thư mục trạng thái", StepCreated, "%s (schema v%d)", paths.Root, v)
	}
	manifest, err := fsafe.LoadManifest(paths.Manifest)
	if err != nil {
		return nil, err
	}

	// 5. Skill gốc (b-bootstrap) cho các harness có adapter.
	if !opts.NoSkills {
		if err := installSkills(rep, cfgRoot, cfg, paths, manifest, opts.Version, opts.Force, now); err != nil {
			return nil, err
		}
	}

	// 6. Hook ghi nhận.
	gitignore := []string{".bint/local.yml"}
	hookInstalled := false
	for _, h := range cfg.Harnesses.Enabled {
		if h != claude.HarnessName {
			rep.add("hook "+h, StepSkipped, "adapter %s chưa có ở P0", h)
			continue
		}
		installed, err := installClaudeHooks(rep, opts, root, paths, now)
		if err != nil {
			return nil, err
		}
		hookInstalled = installed
	}
	if opts.MCP != nil && slices.Contains(cfg.Harnesses.Enabled, claude.HarnessName) {
		p := claude.MCPConfigPath(root)
		res, err := claude.InstallMCP(p, opts.BintPath, paths.Backups, *opts.MCP, now)
		if err != nil {
			return nil, err
		}
		switch {
		case *opts.MCP && res.Changed:
			rep.add("MCP server", StepUpdated, ".mcp.json: mục %q (bint mcp serve)", claude.MCPServerName)
			if opts.BintPath != "bint" {
				rep.add("MCP server", StepWarn, "bint không có trong PATH: .mcp.json ghi đường dẫn tuyệt đối %s — không nên commit cho cả nhóm", opts.BintPath)
			}
		case *opts.MCP:
			rep.add("MCP server", StepOK, ".mcp.json đã có mục %q", claude.MCPServerName)
		case res.Changed:
			rep.add("MCP server", StepUpdated, "đã gỡ mục %q khỏi .mcp.json", claude.MCPServerName)
		default:
			rep.add("MCP server", StepSkipped, ".mcp.json không có mục %q", claude.MCPServerName)
		}
	}
	if hookInstalled && opts.HooksScope == claude.ScopeLocal {
		gitignore = append(gitignore, ".claude/settings.local.json")
	}

	// 7. .gitignore: chỉ thêm khối, không xoá cấu hình sẵn có.
	r, err := fsafe.SyncBlock(fsafe.BlockSync{
		Path: filepath.Join(root, ".gitignore"), RelName: ".gitignore", Markers: fsafe.HashMarkers,
		Inner: strings.Join(gitignore, "\n"), Force: opts.Force, BackupDir: paths.Backups, Manifest: manifest, Now: now,
	})
	if err != nil {
		return nil, err
	}
	rep.addSync(".gitignore", r)

	// Tài liệu cấp repository (tuỳ chọn ở P0).
	if opts.AgentsMD {
		inner, _, err := AgentsBlock(cfgRoot, cfg)
		if err != nil {
			return nil, err
		}
		if err := syncAgentDocs(rep, opts, root, paths, manifest, cfg, inner, now); err != nil {
			return nil, err
		}
	}

	if err := manifest.Save(); err != nil {
		return nil, err
	}

	// 9. Bước tiếp theo.
	if cfg.Eval.TestCommand == nil {
		rep.NextSteps = append(rep.NextSteps, "Xác nhận lệnh kiểm thử: điền eval.test_command trong .bint/config.yml")
	}
	rep.NextSteps = append(rep.NextSteps,
		"Commit .bint/config.yml (và .gitignore) để cả nhóm dùng chung cấu hình",
		"Kiểm tra môi trường: bint doctor",
		"Xem lượt chạy đã ghi nhận: bint runs list",
		"Bootstrap tri thức: chạy /b-bootstrap trong Claude Code (hoặc `bint bootstrap` để xem dữ liệu khảo sát), rồi `bint knowledge review` và `bint bootstrap publish`",
		"Khung đánh giá (bint eval --help) chỉ cần khi muốn đo trên repository này",
	)
	return rep, nil
}

func (r *InitReport) addSync(name string, res fsafe.Result) {
	switch res.Outcome {
	case fsafe.Created:
		r.add(name, StepCreated, "")
	case fsafe.Appended:
		r.add(name, StepUpdated, "thêm khối bint")
	case fsafe.Updated:
		r.add(name, StepUpdated, "cập nhật khối bint, bản cũ: %s", res.BackupPath)
	case fsafe.Unchanged:
		r.add(name, StepOK, "không thay đổi")
	case fsafe.Conflict:
		r.add(name, StepConflict, "%s", res.Detail)
	}
}

func installClaudeHooks(rep *InitReport, opts InitOptions, root string, paths state.Paths, now time.Time) (bool, error) {
	settings, err := claude.SettingsPath(opts.HooksScope, root)
	if err != nil {
		return false, err
	}
	already, err := claude.HooksInstalled(settings)
	if err != nil {
		rep.add("hook claude", StepWarn, "không đọc được %s: %v", settings, err)
		return false, nil
	}
	want := already // đã cài thì làm mới (cập nhật đường dẫn bint) mà không hỏi lại
	if opts.Hooks != nil {
		want = *opts.Hooks
	} else if !already {
		if opts.Confirm != nil {
			want = opts.Confirm(fmt.Sprintf("Cài hook ghi nhận lượt chạy cho Claude Code vào %s?", settings))
		} else {
			rep.add("hook claude", StepSkipped, "chưa có sự đồng ý; chạy lại với --hooks để cài")
			return false, nil
		}
	}
	if !want {
		if already {
			res, err := claude.UninstallHooks(settings, paths.Backups, now)
			if err != nil {
				return false, err
			}
			if res.Changed {
				rep.add("hook claude", StepUpdated, "đã gỡ khỏi %s", settings)
			}
			return false, nil
		}
		rep.add("hook claude", StepSkipped, "người dùng không đồng ý")
		return false, nil
	}
	res, err := claude.InstallHooks(settings, opts.BintPath, paths.Backups, now)
	if err != nil {
		return false, err
	}
	switch {
	case !res.Changed:
		rep.add("hook claude", StepOK, "%s", settings)
	case already:
		rep.add("hook claude", StepUpdated, "%s", settings)
	default:
		rep.add("hook claude", StepCreated, "%s (SessionStart, SessionEnd, PostToolUse, PostToolUseFailure)", settings)
	}
	return true, nil
}

func installSkills(rep *InitReport, root string, cfg config.Config, paths state.Paths, m *fsafe.Manifest, version string, force bool, now time.Time) error {
	var targets []skills.Target
	for _, h := range cfg.Harnesses.Enabled {
		if h == claude.HarnessName {
			targets = append(targets, skills.ClaudeTarget)
		} else {
			rep.add("skill "+h, StepSkipped, "adapter %s chưa có (P5)", h)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	outs, err := skills.Install(skills.InstallOptions{
		Root: root, Targets: targets, BintVersion: version, Force: force,
		BackupDir: paths.Backups, Manifest: m, Now: now,
	})
	if err != nil {
		return err
	}
	for _, o := range outs {
		name := "skill " + o.Skill
		switch o.Result {
		case fsafe.Created:
			rep.add(name, StepCreated, "%s", o.Path)
		case fsafe.Updated:
			rep.add(name, StepUpdated, "%s (bản cũ: %s)", o.Path, o.Backup)
		case fsafe.Unchanged:
			rep.add(name, StepOK, "%s", o.Path)
		case fsafe.Conflict:
			rep.add(name, StepConflict, "%s: %s", o.Path, o.Detail)
		}
	}
	return nil
}

func syncClaudeImport(rep *InitReport, opts InitOptions, root string, paths state.Paths, m *fsafe.Manifest, now time.Time) error {
	claudeMD := filepath.Join(root, "CLAUDE.md")
	if data, err := os.ReadFile(claudeMD); err == nil {
		block, _ := fsafe.ReadBlock(claudeMD, fsafe.HTMLMarkers)
		if block == nil && hasImportLine(string(data), "@AGENTS.md") {
			rep.add("CLAUDE.md", StepOK, "đã nhập AGENTS.md")
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r, err := fsafe.SyncBlock(fsafe.BlockSync{
		Path: claudeMD, RelName: "CLAUDE.md", Markers: fsafe.HTMLMarkers, Inner: "@AGENTS.md",
		Force: opts.Force, BackupDir: paths.Backups, Manifest: m, Now: now, Placement: fsafe.AtStart,
	})
	if err != nil {
		return err
	}
	rep.addSync("CLAUDE.md", r)
	return nil
}

func hasImportLine(content, imp string) bool {
	for _, ln := range strings.Split(content, "\n") {
		if strings.TrimSpace(ln) == imp {
			return true
		}
	}
	return false
}

// writeNew tạo tệp mới, thất bại nếu tệp đã tồn tại (không ghi đè).
func writeNew(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
