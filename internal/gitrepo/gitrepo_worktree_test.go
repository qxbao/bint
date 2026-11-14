package gitrepo

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := Run(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestWorktreeAddListRemoveDeleteBranch(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	head := HeadCommit(ctx, repo)
	// Git reports physical paths, while macOS TMPDIR can contain /var symlinks.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(base, "wt")
	if err := WorktreeAdd(ctx, repo, wt, "bint/T1", head); err != nil {
		t.Fatal(err)
	}
	list, err := WorktreeList(ctx, repo)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	var found *Worktree
	for i := range list {
		if Clean(list[i].Path) == Clean(wt) {
			found = &list[i]
		}
	}
	if found == nil || found.Branch != "refs/heads/bint/T1" || found.Head != head || found.Prunable {
		t.Fatalf("mục worktree: %+v", found)
	}
	// Thư mục mất ⇒ prunable.
	gone := filepath.Join(base, "gone")
	if err := WorktreeAdd(ctx, repo, gone, "bint/T2", head); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(gone)
	list, _ = WorktreeList(ctx, repo)
	prunable := false
	for _, w := range list {
		if Clean(w.Path) == Clean(gone) && w.Prunable {
			prunable = true
		}
	}
	if !prunable {
		t.Fatalf("thư mục mất phải prunable: %+v", list)
	}
	// Remove: cả khi còn thay đổi chưa commit, và khi thư mục đã mất (chỉ còn mục quản trị).
	os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("x"), 0o644)
	if err := WorktreeRemove(ctx, repo, wt); err != nil {
		t.Fatalf("remove worktree bẩn: %v", err)
	}
	if err := WorktreeRemove(ctx, repo, gone); err != nil {
		t.Fatalf("remove thư mục đã mất: %v", err)
	}
	if list, _ = WorktreeList(ctx, repo); len(list) != 1 {
		t.Fatalf("còn worktree: %+v", list)
	}
	// DeleteBranch: xoá nhánh chưa gộp; nhánh không có không phải lỗi.
	if err := DeleteBranch(ctx, repo, "bint/T1"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBranch(ctx, repo, "bint/T1"); err != nil {
		t.Fatalf("xoá nhánh không tồn tại phải êm: %v", err)
	}
	if out := gitIn(t, repo, "branch", "--list", "bint/*"); strings.Contains(out, "T1") {
		t.Fatalf("nhánh còn: %q", out)
	}
}

// A failed removal of a locked worktree must stay an error, including when
// the caller reaches it through a symlink and after its directory disappears.
func TestWorktreeRemoveLockedSymlink(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	base := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias with spaces")
	if err := os.Symlink(base, alias); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "missing"}[missing], func(t *testing.T) {
			name := t.Name()[strings.LastIndex(t.Name(), "/")+1:]
			wt := filepath.Join(alias, name+" worktree")
			if err := WorktreeAdd(ctx, repo, wt, "bint/"+name, HeadCommit(ctx, repo)); err != nil {
				t.Fatal(err)
			}
			gitIn(t, repo, "worktree", "lock", wt)
			other := filepath.Join(t.TempDir(), filepath.Base(wt))
			if err := WorktreeRemove(ctx, repo, other); err != nil {
				t.Fatalf("đường dẫn khác không được nhận nhầm là worktree khóa: %v", err)
			}
			if missing {
				if err := os.RemoveAll(wt); err != nil {
					t.Fatal(err)
				}
			}
			if err := WorktreeRemove(ctx, repo, wt); err == nil {
				t.Error("worktree bị khóa vẫn còn đăng ký: remove phải trả lỗi")
			}
			gitIn(t, repo, "worktree", "unlock", wt)
			if err := WorktreeRemove(ctx, repo, wt); err != nil {
				t.Fatal(err)
			}
		})
	}
}
