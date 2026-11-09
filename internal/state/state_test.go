package state

import (
	"errors"
	"path/filepath"
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
