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
	"bint/internal/harness"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
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
	MCP *bool
	// MCPUser: ghi mục `bint mcp serve` vào cấu hình MCP cấp người dùng của harness chỉ có MCP cấp user (Antigravity:
	// ~/.gemini/config/mcp_config.json; có sao lưu, chạy lại không đổi).
	MCPUser  bool
	AgentsMD bool // ghi khối Bint vào AGENTS.md và nhập lại trong CLAUDE.md
	Force    bool // ghi đè khối Bint đã bị sửa tay (có sao lưu)
	BintPath string
	// Version là phiên bản Bint (ghi vào .bint/skills/core.lock).
	Version string
	// NoSkills: không xuất bản skill gốc.
	NoSkills bool
	// GitMode: track | local (xem GitTrack, GitLocal); "" = giữ lựa chọn đã lưu của
	// máy này, lần init đầu có Confirm thì hỏi, còn lại track.
	GitMode string
	// Update: nâng .bint/config.yml đã có lên mẫu hiện tại (thêm khoá còn thiếu kèm
	// chú thích, giữ nguyên mọi giá trị và chú thích sẵn có; bản cũ được sao lưu).
	Update  bool
	Confirm func(question string) bool
	Now     func() time.Time
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
	Repo      gitrepo.Repo
	StateDir  string
	FirstInit bool
	Harnesses []string
	Steps     []Step
	NextSteps []string
	// GitMode: chế độ hiện diện trong Git đang áp (track | local).
	GitMode    string
	HasProblem bool
	// ConfigBackup là đường dẫn bản sao lưu .bint/config.yml do upgradeConfig (init --update) tạo; rỗng nếu không nâng cấu hình.
	// Dùng trường này thay vì tách từ câu chữ trong Detail của bước (câu chữ có thể đổi hoặc được dịch).
	ConfigBackup string
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

	if opts.GitMode != "" && !slices.Contains(GitModes, opts.GitMode) {
		return nil, fmt.Errorf("--git: phải là %s (nhận %q)", strings.Join(GitModes, " | "), opts.GitMode)
	}
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
	// Chưa biết thư mục trạng thái (cấu hình có thể chưa có): writer chưa có nhật ký, thay bằng bản đầy đủ ngay sau
	// state.Resolve. Lần init đầu không thể có bước chỉ đọc nào đang chạy.
	w := &repoWriter{actor: "bint init"}
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
		if err := w.WriteNew(cfgPath, []byte(content)); err != nil {
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
	w = newRepoWriter(paths, cfg, "bint init")
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
	if cfgExists {
		update := opts.Update
		if update && cfgRoot != root {
			// Không sửa tệp của worktree khác từ đây.
			rep.add(".bint/config.yml", StepSkipped, "--update: cấu hình nằm ở worktree chính %s — chạy `bint init --update` ở đó", cfgRoot)
			update = false
		}
		if cfg, err = upgradeConfig(rep, w, update, cfgRoot, cfgPath, paths.Backups, cfg, now); err != nil {
			return nil, err
		}
	}
	manifest, err := fsafe.LoadManifest(paths.Manifest)
	if err != nil {
		return nil, err
	}

	// 5. Skill gốc (b-bootstrap) cho các harness có adapter.
	if !opts.NoSkills {
		if err := installSkills(rep, w, cfgRoot, cfg, paths, manifest, opts.Version, opts.Force, now); err != nil {
			return nil, err
		}
	}

	// Chế độ hiện diện trong Git: cờ → lựa chọn đã lưu → hỏi ở lần init đầu → track.
	prevGit := LoadGitMode(paths)
	gitMode := opts.GitMode
	if gitMode == "" {
		gitMode = prevGit
	}
	if gitMode == "" {
		gitMode = GitTrack
		if rep.FirstInit && opts.Confirm != nil &&
			opts.Confirm("Chỉ dùng Bint riêng trên máy này, không đưa tệp của Bint vào Git (.git/info/exclude)?") {
			gitMode = GitLocal
		}
	}
	rep.GitMode = gitMode

	// 6. Hook ghi nhận và MCP server cho từng harness được bật (theo adapter).
	gitignore := []string{".bint/local.yml"}
	var hookFiles []string // tệp hook nằm trong repository (mọi phạm vi)
	for _, h := range cfg.Harnesses.Enabled {
		ad, ok := harness.Get(h)
		if !ok {
			continue
		}
		hi, ok := ad.(harness.HookInstaller)
		if !ok {
			rep.add("hook "+h, StepSkipped, "%s không có hook", ad.Title())
			continue
		}
		path, installed, err := installHooks(ctx, rep, w, opts, ad, hi, root, paths, now, cfg.Harnesses.Codex.Binary)
		if err != nil {
			return nil, err
		}
		if installed {
			if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
				hookFiles = append(hookFiles, filepath.ToSlash(rel))
				if opts.HooksScope == claude.ScopeLocal {
					gitignore = append(gitignore, filepath.ToSlash(rel))
				}
			}
		}
	}
	if opts.MCP != nil {
		for _, h := range cfg.Harnesses.Enabled {
			ad, ok := harness.Get(h)
			if !ok {
				continue
			}
			mi, ok := ad.(harness.MCPInstaller)
			if !ok {
				rep.add("MCP "+h, StepSkipped, "%s", harness.CapabilityOf(ad, harness.CapMCP).Detail)
				continue
			}
			res, err := mi.InstallMCP(harness.MCPRequest{Root: root, BintPath: opts.BintPath, BackupDir: paths.Backups,
				Manifest: manifest, Install: *opts.MCP, Force: opts.Force, Now: now})
			if err != nil {
				return nil, err
			}
			if res.Changed {
				// Đường dẫn chỉ biết sau lời gọi nên không có băm trước (prev_sha256 nil: bước chỉ đọc không miễn).
				w.Record(absUnder(root, res.Path)) // adapter có thể trả đường dẫn tương đối với gốc repo
			}
			name := "MCP " + h
			switch {
			case res.Conflict != "":
				rep.add(name, StepConflict, "%s", res.Conflict)
			case *opts.MCP && res.Changed:
				rep.add(name, StepUpdated, "%s: mục %q (bint mcp serve)", res.Path, claude.MCPServerName)
				if opts.BintPath != "bint" {
					rep.add(name, StepWarn, "bint không có trong PATH: %s ghi đường dẫn tuyệt đối %s — không nên commit cho cả nhóm", res.Path, opts.BintPath)
				}
			case *opts.MCP:
				rep.add(name, StepOK, "%s đã có mục %q", res.Path, claude.MCPServerName)
			case res.Changed:
				rep.add(name, StepUpdated, "đã gỡ mục %q khỏi %s", claude.MCPServerName, res.Path)
			default:
				rep.add(name, StepSkipped, "%s không có mục %q", res.Path, claude.MCPServerName)
			}
			if res.Note != "" {
				rep.add(name, StepWarn, "%s", res.Note)
			}
		}
	}

	// Quy tắc quyền mcp__bint__* (công cụ của Bint và của service ngoài qua cửa vào gộp) trong settings của harness, cùng
	// phạm vi với hook: khi đăng ký MCP hoặc nâng cấp (--update).
	if (opts.MCP != nil && *opts.MCP) || opts.Update {
		for _, h := range cfg.Harnesses.Enabled {
			ad, ok := harness.Get(h)
			if !ok {
				continue
			}
			pi, ok := ad.(harness.PermissionInstaller)
			hi, hok := ad.(harness.HookInstaller)
			if !ok || !hok {
				continue
			}
			settings, err := hi.HooksPath(opts.HooksScope, root)
			if err != nil {
				return nil, err
			}
			name := "quyền MCP " + h
			repoWriteSeam("before", absUnder(root, settings))
			res, err := pi.InstallPermissions(settings, paths.Backups, now)
			repoWriteSeam("after", absUnder(root, settings))
			if err != nil {
				rep.add(name, StepWarn, "không ghi được quy tắc %s vào %s: %v", claude.PermissionRule, settings, err)
				continue
			}
			if res.Changed {
				w.recordHooks(absUnder(root, settings), res)
				rep.add(name, StepUpdated, "%s: permissions.allow thêm %q", settings, claude.PermissionRule)
			} else {
				rep.add(name, StepOK, "%s đã có quy tắc %q", settings, claude.PermissionRule)
			}
		}
	}
	// MCP cấp người dùng cho harness không có MCP cấp dự án (Antigravity), chỉ khi người dùng chọn --mcp-user.
	if opts.MCPUser {
		for _, h := range cfg.Harnesses.Enabled {
			ad, ok := harness.Get(h)
			if !ok {
				continue
			}
			ui, ok := ad.(harness.UserMCPInstaller)
			if !ok {
				continue
			}
			name := "MCP cấp người dùng " + h
			res, err := ui.InstallUserMCP(harness.MCPRequest{Root: root, BintPath: opts.BintPath, BackupDir: paths.Backups, Install: true, Now: now})
			if err != nil {
				rep.add(name, StepWarn, "không ghi được cấu hình MCP cấp người dùng: %v", err)
				continue
			}
			if res.Changed {
				rep.add(name, StepUpdated, "%s: mục %q (bint mcp serve); role lấy theo BINT_RUN_ID/BINT_TASK_ID, ngoài bint run là interactive", res.Path, claude.MCPServerName)
				if opts.BintPath != "bint" {
					rep.add(name, StepWarn, "bint không có trong PATH: %s ghi đường dẫn tuyệt đối %s", res.Path, opts.BintPath)
				}
			} else {
				rep.add(name, StepOK, "%s đã có mục %q", res.Path, claude.MCPServerName)
			}
		}
	}

	// 7. .gitignore: chỉ thêm khối, không xoá cấu hình sẵn có. Chế độ local không đụng
	// .gitignore (thường được commit); tệp riêng của máy đi vào .git/info/exclude.
	if gitMode == GitTrack {
		r, err := w.SyncBlock(fsafe.BlockSync{
			Path: filepath.Join(root, ".gitignore"), RelName: ".gitignore", Markers: fsafe.HashMarkers,
			Inner: strings.Join(gitignore, "\n"), Force: opts.Force, BackupDir: paths.Backups, Manifest: manifest, Now: now,
		})
		if err != nil {
			return nil, err
		}
		rep.addSync(".gitignore", r)
	}

	// Tài liệu cấp repository (tuỳ chọn ở P0).
	if opts.AgentsMD {
		inner, _, err := AgentsBlock(cfgRoot, cfg)
		if err != nil {
			return nil, err
		}
		if err := syncAgentDocs(rep, w, opts, root, paths, manifest, cfg, inner, now); err != nil {
			return nil, err
		}
	}

	// 8. Hiện diện trong Git (sau mọi bước ghi tệp để bao được tệp vừa tạo).
	if err := applyGitMode(ctx, rep, w, gitMode, prevGit, root, bintPaths(root, cfg.Harnesses.Enabled, hookFiles), paths, manifest, opts.Force, now); err != nil {
		return nil, err
	}

	if err := manifest.Save(); err != nil {
		return nil, err
	}

	// 9. Bước tiếp theo.
	if cfg.Eval.TestCommand == nil {
		rep.NextSteps = append(rep.NextSteps, "Xác nhận lệnh kiểm thử: điền eval.test_command trong .bint/config.yml")
	}
	if gitMode == GitTrack {
		rep.NextSteps = append(rep.NextSteps, "Commit .bint/config.yml (và .gitignore) để cả nhóm dùng chung cấu hình")
	} else {
		rep.NextSteps = append(rep.NextSteps, "Bint đang ở chế độ riêng máy (--git local): tệp của Bint không hiện trong git status; muốn chia sẻ cho nhóm thì chạy bint init --git track")
	}
	bootstrapSkill := "/b-bootstrap trong Claude Code"
	if cfg.Harnesses.Default == codex.HarnessName {
		bootstrapSkill = "$b-bootstrap trong Codex"
	}
	rep.NextSteps = append(rep.NextSteps,
		"Kiểm tra môi trường: bint doctor",
		"Xem lượt chạy đã ghi nhận: bint runs list",
		"Bootstrap tri thức: chạy "+bootstrapSkill+" (hoặc `bint bootstrap` để xem dữ liệu khảo sát), rồi `bint knowledge review` và `bint bootstrap publish`",
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

// installHooks cài (hoặc gỡ) hook ghi nhận của một harness; trả về tệp hook và việc
// hook có đang được cài.
func installHooks(ctx context.Context, rep *InitReport, w *repoWriter, opts InitOptions, ad harness.Adapter, hi harness.HookInstaller, root string, paths state.Paths, now time.Time, codexBinary string) (string, bool, error) {
	name := "hook " + ad.Name()
	settings, err := hi.HooksPath(opts.HooksScope, root)
	if err != nil {
		return "", false, err
	}
	already, err := hi.HooksInstalled(settings)
	if err != nil {
		rep.add(name, StepWarn, "không đọc được %s: %v", settings, err)
		return settings, false, nil
	}
	want := already // đã cài thì làm mới (cập nhật đường dẫn bint) mà không hỏi lại
	if opts.Hooks != nil {
		want = *opts.Hooks
	} else if !already {
		if opts.Confirm != nil {
			want = opts.Confirm(fmt.Sprintf("Cài hook ghi nhận lượt chạy cho %s vào %s?", ad.Title(), settings))
		} else {
			rep.add(name, StepSkipped, "chưa có sự đồng ý; chạy lại với --hooks để cài")
			return settings, false, nil
		}
	}
	if !want {
		if already {
			repoWriteSeam("before", absUnder(root, settings))
			res, err := hi.UninstallHooks(settings, paths.Backups, now)
			repoWriteSeam("after", absUnder(root, settings))
			if err != nil {
				return settings, false, err
			}
			if res.Changed {
				w.recordHooks(absUnder(root, settings), res)
				rep.add(name, StepUpdated, "đã gỡ khỏi %s", settings)
			}
			return settings, false, nil
		}
		rep.add(name, StepSkipped, "người dùng không đồng ý")
		return settings, false, nil
	}
	repoWriteSeam("before", absUnder(root, settings))
	res, err := hi.InstallHooks(settings, opts.BintPath, paths.Backups, now)
	repoWriteSeam("after", absUnder(root, settings))
	if err != nil {
		return settings, false, err
	}
	w.recordHooks(absUnder(root, settings), res)
	switch {
	case !res.Changed:
		rep.add(name, StepOK, "%s", settings)
	case already:
		rep.add(name, StepUpdated, "%s", settings)
	default:
		rep.add(name, StepCreated, "%s (%s)", settings, hi.HookEvents())
	}
	if ad.Name() == codex.HarnessName {
		trust, err := codex.InspectHookTrust(ctx, codexBinary, root, settings)
		if err != nil {
			rep.add(name, StepWarn, "chưa xác minh trust hook Codex: %v; mở codex trong repository và /hooks", err)
		} else if !trust.AllTrusted() {
			rep.add(name, StepWarn, "Codex: %s; mở codex trong repository và duyệt hook của Bint (/hooks)", trust.String())
		}
	}
	return settings, true, nil
}

func installSkills(rep *InitReport, w *repoWriter, root string, cfg config.Config, paths state.Paths, m *fsafe.Manifest, version string, force bool, now time.Time) error {
	targets := harness.SkillTargets(cfg.Harnesses.Enabled)
	if len(targets) == 0 {
		return nil
	}
	outs, err := w.InstallSkills(skills.InstallOptions{
		Root: root, Targets: targets, BintVersion: version, Force: force,
		BackupDir: paths.Backups, Manifest: m, Now: now, Agents: harness.SubagentFiles(cfg),
	})
	if err != nil {
		return err
	}
	for _, o := range outs {
		name := "skill " + o.Skill
		if o.Agent {
			name = "subagent " + o.Skill
		}
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

func syncClaudeImport(rep *InitReport, w *repoWriter, opts InitOptions, root string, paths state.Paths, m *fsafe.Manifest, now time.Time) error {
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
	r, err := w.SyncBlock(fsafe.BlockSync{
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

// applyGitMode áp chế độ --git và lưu lựa chọn. local: khối exclude + skip-worktree
// (chạy lại mỗi lần init để bao tệp mới); track sau local: gỡ cả hai.
func applyGitMode(ctx context.Context, rep *InitReport, w *repoWriter, mode, prev, root string, bp []string, paths state.Paths, m *fsafe.Manifest, force bool, now time.Time) error {
	const name = "git"
	switch {
	case mode == GitLocal:
		v, err := applyGitLocal(ctx, w, root, bp, paths, m, force, now)
		if err != nil {
			return err
		}
		if v.Exclude.Outcome == fsafe.Conflict {
			rep.add(name, StepConflict, "%s: %s", v.ExcludePath, v.Exclude.Detail)
		} else {
			rep.add(name, StepOK, "chế độ local: %d mục trong %s", len(v.Patterns), v.ExcludePath)
		}
		if len(v.SkipWorktree) > 0 {
			rep.add(name, StepOK, "skip-worktree cho tệp đã được theo dõi: %s (thay đổi upstream của các tệp này sẽ làm pull báo lỗi; gỡ bằng bint init --git track)",
				strings.Join(v.SkipWorktree, ", "))
		}
	case prev == GitLocal:
		v, err := revertGitLocal(ctx, w, root, bp, paths, m, force, now)
		if err != nil {
			return err
		}
		detail := "chế độ track: đã gỡ khối bint khỏi " + v.ExcludePath
		if len(v.SkipWorktree) > 0 {
			detail += "; bỏ skip-worktree: " + strings.Join(v.SkipWorktree, ", ")
		}
		rep.add(name, StepUpdated, "%s — tệp của Bint giờ hiện trong git status", detail)
	}
	if mode != prev {
		return saveGitMode(paths, mode)
	}
	return nil
}

// upgradeConfig so cấu hình đã có với mẫu hiện tại. update = false: chỉ báo khoá còn
// thiếu (không ghi gì); update = true: sao lưu rồi ghi bản đã nâng và nạp lại cấu hình.
func upgradeConfig(rep *InitReport, w *repoWriter, update bool, cfgRoot, cfgPath, backups string, cfg config.Config, now time.Time) (config.Config, error) {
	const name = ".bint/config.yml"
	old, err := os.ReadFile(cfgPath)
	if err != nil {
		return cfg, err
	}
	up, err := config.Upgrade(old)
	if err != nil {
		return cfg, fmt.Errorf("nâng %s: %w", cfgPath, err)
	}
	if !up.Changed() {
		if update {
			rep.add(name, StepOK, "đã theo mẫu hiện tại, không có khoá nào cần thêm")
		}
		return cfg, nil
	}
	if !update {
		rep.add(name, StepSkipped, "theo mẫu cũ, thiếu: %s — chạy `bint init --update` để thêm (giữ nguyên giá trị đang có)",
			orDash(config.SummarizeAdded(up.Added)))
		rep.NextSteps = append(rep.NextSteps, "Nâng cấu hình lên mẫu mới (model theo role…): bint init --update")
		return cfg, nil
	}
	bak, err := fsafe.Backup(cfgPath, backups, name, now)
	if err != nil {
		return cfg, err
	}
	if err := w.WriteFile(cfgPath, up.Content, 0o644); err != nil {
		return cfg, err
	}
	detail := "thêm: " + orDash(config.SummarizeAdded(up.Added))
	if up.Fixed > 0 {
		detail += fmt.Sprintf("; sửa %d dòng chú thích hỏng", up.Fixed)
	}
	rep.add(name, StepUpdated, "%s (bản cũ: %s)", detail, bak)
	rep.ConfigBackup = bak
	for _, n := range up.Notes {
		rep.add(name, StepSkipped, "%s", n)
	}
	cfg, _, err = config.Load(cfgRoot, true)
	if err != nil {
		return cfg, fmt.Errorf("nạp lại cấu hình sau khi nâng: %w", err)
	}
	return cfg, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
