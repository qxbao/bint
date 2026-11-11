package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// --git local: mọi tệp Bint tạo hoặc sửa đều không hiện trong git status; tệp đã được
// theo dõi (AGENTS.md) được đặt skip-worktree; .gitignore không bị đụng; lựa chọn được
// nhớ; --git track gỡ sạch.
func TestInitGitLocalKeepsRepoClean(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "AGENTS.md": "# Quy ước của nhóm\n"})

	rep := mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude", "codex"}, Hooks: yes(), MCP: yes(),
		AgentsMD: true, GitMode: GitLocal})
	if rep.GitMode != GitLocal {
		t.Fatalf("GitMode = %q", rep.GitMode)
	}
	if st := gitOut(t, root, "status", "--porcelain", "--untracked-files=all"); st != "" {
		t.Fatalf("git status phải sạch ở chế độ local:\n%s", st)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err == nil {
		t.Fatal("chế độ local không được tạo .gitignore")
	}
	if !strings.Contains(read(t, filepath.Join(root, "AGENTS.md")), "bint:begin") {
		t.Fatal("AGENTS.md phải có khối bint")
	}
	if !strings.HasPrefix(gitOut(t, root, "ls-files", "-v", "AGENTS.md"), "S ") {
		t.Fatal("AGENTS.md (đã theo dõi) phải có skip-worktree")
	}
	exclude := read(t, filepath.Join(root, ".git", "info", "exclude"))
	for _, want := range []string{"/.bint/", "/.claude/skills/b-do/", "/.agents/skills/b-do/", "/.mcp.json", "/CLAUDE.md"} {
		if !strings.Contains(exclude, want) {
			t.Errorf("exclude thiếu %s:\n%s", want, exclude)
		}
	}

	// Chạy lại không cờ: giữ local.
	if rep := mustInit(t, InitOptions{Dir: root, Hooks: yes()}); rep.GitMode != GitLocal {
		t.Fatalf("lựa chọn phải được nhớ: %q", rep.GitMode)
	}
	if st := gitOut(t, root, "status", "--porcelain"); st != "" {
		t.Fatalf("chạy lại vẫn phải sạch:\n%s", st)
	}

	// Chuyển sang track: gỡ khối exclude và skip-worktree, ghi .gitignore.
	rep = mustInit(t, InitOptions{Dir: root, Hooks: yes(), GitMode: GitTrack})
	if s := lastStep(rep, "git"); s.Status != StepUpdated || !strings.Contains(s.Detail, "AGENTS.md") {
		t.Fatalf("bước git: %+v", s)
	}
	if strings.Contains(read(t, filepath.Join(root, ".git", "info", "exclude")), "bint") {
		t.Fatal("khối bint phải bị gỡ khỏi exclude")
	}
	if strings.HasPrefix(gitOut(t, root, "ls-files", "-v", "AGENTS.md"), "S ") {
		t.Fatal("skip-worktree phải được gỡ")
	}
	st := gitOut(t, root, "status", "--porcelain")
	for _, want := range []string{" M AGENTS.md", "?? .bint/", "?? .gitignore"} {
		if !strings.Contains(st, want) {
			t.Errorf("git status thiếu %q:\n%s", want, st)
		}
	}
}

func TestInitGitModeInvalid(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, nil)
	if _, err := Init(t.Context(), InitOptions{Dir: root, GitMode: "hidden", BintPath: "bint"}); err == nil || !strings.Contains(err.Error(), "--git") {
		t.Fatalf("err = %v", err)
	}
}
