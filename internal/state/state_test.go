package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/gitrepo"
)

func TestRepoID(t *testing.T) {
	a := RepoID(filepath.Join("x", "my repo!", ".git"))
	if !strings.HasPrefix(a, "my-repo-") || len(a) != len("my-repo-")+12 {
		t.Fatalf("id = %s", a)
	}
	if RepoID(filepath.Join("x", "my repo!", ".git")) != a {
		t.Fatal("not stable")
	}
	if RepoID(filepath.Join("y", "my repo!", ".git")) == a {
		t.Fatal("different repos share id")
	}
	if !strings.HasPrefix(RepoID(filepath.Join("srv", "proj.git")), "proj-") {
		t.Fatal("bare-style name")
	}
}

func TestResolveRejectsMissingStateThroughSymlink(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "repo alias")
	if err := os.Symlink(root, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	repo := gitrepo.Repo{Toplevel: root, CommonDir: filepath.Join(root, ".git")}
	cfg := config.Default()
	cfg.State.Dir = filepath.Join(alias, "not-created", "state")
	if _, err := Resolve(repo, cfg); err == nil {
		t.Fatal("state chưa tạo qua symlink vẫn nằm trong repo và phải bị từ chối")
	}
}

func TestResolve(t *testing.T) {
	base := t.TempDir()
	t.Setenv(EnvStateDir, base)
	repoDir := t.TempDir()
	repo := gitrepo.Repo{Toplevel: repoDir, CommonDir: filepath.Join(repoDir, ".git"), GitDir: filepath.Join(repoDir, ".git")}
	p, err := Resolve(repo, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p.Root) != base || filepath.Base(p.Root) != p.RepoID {
		t.Fatalf("root = %s", p.Root)
	}
	if p.Initialized() {
		t.Fatal("should not be initialized")
	}
	if err := p.Ensure(repo); err != nil {
		t.Fatal(err)
	}
	if !p.Initialized() {
		t.Fatal("should be initialized")
	}
	if err := p.Ensure(repo); err != nil {
		t.Fatal("ensure not idempotent:", err)
	}

	other := repo
	other.CommonDir = filepath.Join(t.TempDir(), ".git")
	if err := p.Ensure(other); !errors.Is(err, ErrForeignState) {
		t.Fatalf("want ErrForeignState, got %v", err)
	}

	cfg := config.Default()
	cfg.State.Dir = filepath.Join(repoDir, "state")
	if _, err := Resolve(repo, cfg); err == nil {
		t.Fatal("state dir inside repo must be rejected")
	}
	cfg.State.Dir = filepath.Join(base, "custom")
	if p, err := Resolve(repo, cfg); err != nil || p.Root != cfg.State.Dir {
		t.Fatalf("custom dir: %v %v", p.Root, err)
	}
}

// BaseDirFrom(os.LookupEnv) phải trùng BaseDir() với mọi tổ hợp env, và đọc từ lookup chứ không từ env tiến trình.
func TestBaseDirFromMatchesBaseDir(t *testing.T) {
	root := t.TempDir()
	cases := []map[string]string{
		{"BINT_STATE_DIR": filepath.Join(root, "s")},
		{"BINT_STATE_DIR": "tuong-doi"},
		{"XDG_STATE_HOME": filepath.Join(root, "xdg")},
		{"XDG_STATE_HOME": "tuong-doi"},
		{},
	}
	for _, env := range cases {
		for _, k := range []string{"BINT_STATE_DIR", "XDG_STATE_HOME"} {
			t.Setenv(k, "")
		}
		home := filepath.Join(root, "home")
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		for k, v := range env {
			t.Setenv(k, v)
		}
		want, werr := BaseDir()
		got, gerr := BaseDirFrom(os.LookupEnv)
		if got != want || (werr == nil) != (gerr == nil) {
			t.Errorf("env %v: BaseDirFrom = %q,%v; BaseDir = %q,%v", env, got, gerr, want, werr)
		}
	}
	m := map[string]string{"BINT_STATE_DIR": "rel"}
	got, err := BaseDirFrom(func(k string) (string, bool) { v, ok := m[k]; return v, ok })
	if err != nil || !filepath.IsAbs(got) || filepath.Base(got) != "rel" {
		t.Errorf("BINT_STATE_DIR tương đối phải thành tuyệt đối: %q %v", got, err)
	}
	if _, err := BaseDirFrom(func(string) (string, bool) { return "", false }); err == nil {
		t.Error("không có home thì phải lỗi")
	}
}
