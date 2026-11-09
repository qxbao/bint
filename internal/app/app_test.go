package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/harness/claude"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/testutil"
)

func yes() *bool { v := true; return &v }
func no() *bool  { v := false; return &v }

func mustInit(t *testing.T, opts InitOptions) *InitReport {
	t.Helper()
	if opts.BintPath == "" {
		opts.BintPath = "bint"
	}
	rep, err := Init(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func stepOf(rep *InitReport, name string) Step {
	for _, s := range rep.Steps {
		if s.Name == name {
			return s
		}
	}
	return Step{}
}

func TestInitCreatesEverythingOutsideRepo(t *testing.T) {
	stateRoot := testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})

	rep := mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: yes()})
	if !rep.FirstInit {
		t.Fatal("first init expected")
	}
	if !strings.HasPrefix(rep.StateDir, stateRoot) {
		t.Fatalf("state dir %s not under %s", rep.StateDir, stateRoot)
	}
	for _, f := range []string{"state.sqlite", "repo.json", "spool", "backups"} {
		if _, err := os.Stat(filepath.Join(rep.StateDir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	cfg, _, err := config.Load(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Eval.TestCommand != nil || cfg.Eval.SuggestedTestCommand == nil || *cfg.Eval.SuggestedTestCommand != "go test ./..." {
		t.Fatalf("test command handling: %+v", cfg.Eval)
	}
	settings := filepath.Join(root, ".claude", "settings.local.json")
	if ok, _ := claude.HooksInstalled(settings); !ok {
		t.Fatal("hooks not installed")
	}
	gi := read(t, filepath.Join(root, ".gitignore"))
	if !strings.Contains(gi, ".bint/local.yml") || !strings.Contains(gi, ".claude/settings.local.json") {
		t.Fatalf(".gitignore = %q", gi)
	}
	// Không có tài liệu nào được tạo ở thư mục gốc khi không yêu cầu.
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", "CODEBASE.md"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			t.Errorf("%s should not be created by default", f)
		}
	}
	// Không có gì của trạng thái cục bộ trong repository.
	out := testutil.Git(t, root, "status", "--porcelain", "--untracked-files=all")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "sqlite") || strings.Contains(line, "spool") {
			t.Errorf("state leaked into repo: %s", line)
		}
	}
}

func TestInitIdempotentAndPreservesUserEdits(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, nil)
	gitignore := filepath.Join(root, ".gitignore")
	os.WriteFile(gitignore, []byte("node_modules/\n*.log\n"), 0o644)
	settings := filepath.Join(root, ".claude", "settings.local.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	userSettings := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {"PostToolUse": [{"matcher": "Write", "hooks": [{"type": "command", "command": "prettier --write"}]}]}
}`
	os.WriteFile(settings, []byte(userSettings), 0o644)

	opts := InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: yes(), AgentsMD: true}
	mustInit(t, opts)

	// Người dùng sửa cấu hình và sửa tay khối bint trong AGENTS.md.
	cfgPath := config.ProjectPath(root)
	cfgEdited := strings.Replace(read(t, cfgPath), "max_tokens: 6000", "max_tokens: 4000", 1)
	os.WriteFile(cfgPath, []byte(cfgEdited), 0o644)
	agents := filepath.Join(root, "AGENTS.md")
	agentsEdited := strings.Replace(read(t, agents), "Khối do Bint", "Khối do TÔI", 1)
	os.WriteFile(agents, []byte("# Quy tắc của nhóm\n\n"+agentsEdited), 0o644)
	agentsBefore := read(t, agents)
	gitignoreBefore := read(t, gitignore)
	settingsBefore := read(t, settings)

	// Chạy lại nhiều lần.
	for i := 0; i < 3; i++ {
		rep := mustInit(t, InitOptions{Dir: root, AgentsMD: true})
		if rep.FirstInit {
			t.Fatal("re-init reported as first init")
		}
		if s := stepOf(rep, "AGENTS.md"); s.Status != StepConflict {
			t.Fatalf("AGENTS.md step = %+v", s)
		}
		if s := stepOf(rep, "hook claude"); s.Status != StepOK {
			t.Fatalf("hook step = %+v", s)
		}
	}
	if read(t, cfgPath) != cfgEdited {
		t.Fatal("config overwritten")
	}
	if read(t, agents) != agentsBefore {
		t.Fatal("user-edited AGENTS.md block overwritten")
	}
	if read(t, gitignore) != gitignoreBefore || !strings.HasPrefix(gitignoreBefore, "node_modules/\n*.log\n") {
		t.Fatalf(".gitignore changed or lost user lines: %q", read(t, gitignore))
	}
	if read(t, settings) != settingsBefore {
		t.Fatal("settings rewritten although nothing changed")
	}
	var s map[string]any
	json.Unmarshal([]byte(settingsBefore), &s)
	if _, ok := s["permissions"]; !ok {
		t.Fatal("user settings lost")
	}
	if !strings.Contains(settingsBefore, "prettier --write") {
		t.Fatal("user hook lost")
	}

	// --force ghi đè khối và sao lưu bản người dùng.
	rep := mustInit(t, InitOptions{Dir: root, AgentsMD: true, Force: true})
	if st := stepOf(rep, "AGENTS.md"); st.Status != StepUpdated {
		t.Fatalf("force: %+v", st)
	}
	if !strings.HasPrefix(read(t, agents), "# Quy tắc của nhóm\n") {
		t.Fatal("content outside block lost on force")
	}
	backups, _ := filepath.Glob(filepath.Join(rep.StateDir, "backups", "*", "AGENTS.md"))
	found := false
	for _, b := range backups {
		if strings.Contains(read(t, b), "Khối do TÔI") {
			found = true
		}
	}
	if !found {
		t.Fatal("user version not backed up")
	}

	// --no-hooks gỡ hook của Bint, giữ hook của người dùng.
	mustInit(t, InitOptions{Dir: root, Hooks: no()})
	if ok, _ := claude.HooksInstalled(settings); ok {
		t.Fatal("hooks not removed")
	}
	if !strings.Contains(read(t, settings), "prettier --write") {
		t.Fatal("user hook removed")
	}
}

func TestInitRejectsInvalidExistingConfig(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, nil)
	os.MkdirAll(filepath.Join(root, ".bint"), 0o755)
	bad := "version: 1\nharnesses:\n  enabled: [vim]\n"
	os.WriteFile(config.ProjectPath(root), []byte(bad), 0o644)
	if _, err := Init(context.Background(), InitOptions{Dir: root, Hooks: yes(), BintPath: "bint"}); err == nil {
		t.Fatal("want error")
	}
	if read(t, config.ProjectPath(root)) != bad {
		t.Fatal("invalid config modified")
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err == nil {
		t.Fatal("must not touch other files when config invalid")
	}
}

func TestInitNotARepo(t *testing.T) {
	testutil.Isolate(t)
	if _, err := Init(context.Background(), InitOptions{Dir: t.TempDir()}); err == nil {
		t.Fatal("want error")
	}
}

func hookInput(session, event, cwd string, extra map[string]any) string {
	m := map[string]any{"session_id": session, "hook_event_name": event, "cwd": cwd}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func runHook(t *testing.T, input string) claude.HookResult {
	t.Helper()
	res, err := claude.HandleHook(context.Background(), strings.NewReader(input), claude.HookEnv{BintVersion: "test"})
	if err != nil {
		t.Error(err)
	}
	return res
}

// Nghiệm thu P0: nhiều worktree chạy song song ghi về cùng một cơ sở dữ liệu,
// không trùng và không mất sự kiện.
func TestParallelWorktreesShareOneDatabase(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})

	// .bint chưa commit: worktree phụ vẫn phải ghi về cùng thư mục trạng thái.
	wts := []string{root}
	for i := 0; i < 3; i++ {
		wt := filepath.Join(filepath.Dir(root), fmt.Sprintf("wt%d", i))
		testutil.Git(t, root, "worktree", "add", "-q", "-b", fmt.Sprintf("feat%d", i), wt)
		wts = append(wts, wt)
	}

	const editsPerSession = 15
	var wg sync.WaitGroup
	for i, wt := range wts {
		wg.Add(1)
		go func(i int, wt string) {
			defer wg.Done()
			sid := fmt.Sprintf("session-%d", i)
			if r := runHook(t, hookInput(sid, "SessionStart", wt, map[string]any{"source": "startup", "model": "claude-test"})); !r.Written {
				t.Errorf("start not written in %s: %s", wt, r.Skipped)
			}
			for j := 0; j < editsPerSession; j++ {
				// Xen kẽ đường dẫn tuyệt đối và tương đối (theo cwd).
				fp := fmt.Sprintf("f%d.go", j%4)
				if j%2 == 0 {
					fp = filepath.Join(wt, fp)
				}
				in := hookInput(sid, "PostToolUse", wt, map[string]any{
					"tool_name": "Edit", "tool_use_id": fmt.Sprintf("tu-%d-%d", i, j),
					"tool_input": map[string]any{"file_path": fp},
				})
				runHook(t, in)
				if j%5 == 0 {
					runHook(t, in) // hook trùng (cài ở hai phạm vi)
				}
			}
			runHook(t, hookInput(sid, "PostToolUse", wt, map[string]any{
				"tool_name": "Bash", "tool_use_id": fmt.Sprintf("bash-ok-%d", i),
				"tool_input": map[string]any{"command": "SECRET=abc go test ./..."},
			}))
			runHook(t, hookInput(sid, "PostToolUseFailure", wt, map[string]any{
				"tool_name": "Bash", "tool_use_id": fmt.Sprintf("bash-fail-%d", i),
				"tool_input": map[string]any{"command": "go test ./..."},
			}))
			// Lệnh không phải kiểm thử và công cụ ngoài phạm vi: bỏ qua.
			if r := runHook(t, hookInput(sid, "PostToolUse", wt, map[string]any{
				"tool_name": "Bash", "tool_use_id": "ls", "tool_input": map[string]any{"command": "ls -la"}})); r.Written {
				t.Error("non-test bash recorded")
			}
			if r := runHook(t, hookInput(sid, "PostToolUse", wt, map[string]any{
				"tool_name": "Edit", "tool_use_id": "outside",
				"tool_input": map[string]any{"file_path": filepath.Join(os.TempDir(), "x.go")}})); r.Written {
				t.Error("file outside repo recorded")
			}
			runHook(t, hookInput(sid, "SessionEnd", wt, map[string]any{"reason": "prompt_input_exit"}))
		}(i, wt)
	}
	wg.Wait()

	// init trong worktree phụ không tạo cấu hình riêng.
	rep := mustInit(t, InitOptions{Dir: wts[1]})
	if _, err := os.Stat(config.ProjectPath(wts[1])); err == nil {
		t.Fatal("init in linked worktree created its own config")
	}
	if rep.StateDir != mustWS(t, root).Paths.Root {
		t.Fatalf("linked worktree init used state dir %s", rep.StateDir)
	}

	// Mọi worktree phân giải ra cùng một workspace.
	var dbs []string
	for _, wt := range wts {
		ws, err := OpenWorkspace(context.Background(), wt, true)
		if err != nil {
			t.Fatalf("%s: %v", wt, err)
		}
		dbs = append(dbs, ws.Paths.DB)
	}
	for _, d := range dbs[1:] {
		if d != dbs[0] {
			t.Fatalf("worktrees use different databases: %v", dbs)
		}
	}

	// Nạp song song từ hai worktree.
	var iwg sync.WaitGroup
	for _, wt := range []string{wts[1], wts[2]} {
		iwg.Add(1)
		go func(wt string) {
			defer iwg.Done()
			ws, _ := OpenWorkspace(context.Background(), wt, true)
			s, err := ws.OpenStore(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			defer s.Close()
			if _, err := ws.Ingest(context.Background(), s, time.Now()); err != nil {
				t.Error(err)
			}
		}(wt)
	}
	iwg.Wait()

	ws, _ := OpenWorkspace(context.Background(), root, true)
	s, err := ws.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := ws.Ingest(context.Background(), s, time.Now()); err != nil {
		t.Fatal(err)
	}
	if left, _ := spool.Pending(ws.Paths.Spool); len(left) != 0 {
		t.Fatalf("spool not drained: %d", len(left))
	}
	runs, err := runlog.ListRuns(context.Background(), s.DB, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != len(wts) {
		t.Fatalf("runs = %d, want %d", len(runs), len(wts))
	}
	seenWT := map[string]bool{}
	for _, r := range runs {
		// start + 15 edit + 2 test + end
		if r.Events != editsPerSession+4 {
			t.Errorf("run %s events = %d, want %d", r.SessionID.String, r.Events, editsPerSession+4)
		}
		if r.Status != "completed" || r.FilesModified != 4 || r.TestsOK != 1 || r.TestsFailed != 1 {
			t.Errorf("run = %+v", r)
		}
		if r.Model.String != "claude-test" || r.BintVersion.String != "test" || !r.GitBaseCommit.Valid {
			t.Errorf("metadata = %+v", r)
		}
		seenWT[r.Worktree.String] = true
		arts, _ := runlog.RunArtifacts(context.Background(), s.DB, r.RunID)
		for _, a := range arts {
			if strings.Contains(a.Path, "abc") || filepath.IsAbs(a.Path) || strings.Contains(a.Path, `\`) {
				t.Errorf("bad artifact path/command: %q", a.Path)
			}
		}
	}
	if len(seenWT) != len(wts) {
		t.Fatalf("worktrees recorded = %v", seenWT)
	}
}

func TestHookSkipsUninitializedRepo(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, nil)
	r := runHook(t, hookInput("s", "SessionStart", root, nil))
	if r.Written || r.Skipped == "" {
		t.Fatalf("hook should be a no-op: %+v", r)
	}
	r = runHook(t, hookInput("s", "SessionStart", t.TempDir(), nil))
	if r.Written {
		t.Fatal("hook outside git wrote an event")
	}
}

func TestDoctor(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, nil)
	rep := Doctor(context.Background(), root)
	if !rep.Failed() {
		t.Fatal("doctor should fail before init")
	}
	mustInit(t, InitOptions{Dir: root, Hooks: yes()})
	rep = Doctor(context.Background(), root)
	if rep.Failed() {
		for _, c := range rep.Checks {
			t.Logf("%s %s %s", c.Level, c.Name, c.Detail)
		}
		t.Fatal("doctor failed after init")
	}
}

func mustWS(t *testing.T, dir string) *Workspace {
	t.Helper()
	ws, err := OpenWorkspace(context.Background(), dir, true)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}
