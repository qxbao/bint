package evolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/workflow"
)

// snapshotTimeout là hạn mỗi lần chụp cây khi ctx của người gọi không có hạn.
const snapshotTimeout = 2 * time.Minute

// maxSubmoduleDepth chặn đệ quy submodule lồng nhau quá sâu.
const maxSubmoduleDepth = 8

// repoToplevelFn là biến hàm để test đếm số lần guardReadOnly tính gốc repository.
var repoToplevelFn = repoToplevel

// guardReadOnly chạy run (agent evolver chỉ được đọc) và báo lỗi nếu nội dung cây làm việc đổi
// hoặc nếu không quan sát được cây trước/sau (P6-02). Dùng chung cho evolver tri thức và skill.
//
// Giao thức chụp là workflow.WorktreeSnapshot: chỉ mục tạm (GIT_INDEX_FILE, không đụng chỉ mục thật
// của người dùng), `git add -A -- .`, rồi `git write-tree`. Mã cây đổi theo NỘI DUNG, nên tệp vốn đã
// bẩn (M) mà bị sửa thêm vẫn bị phát hiện; tệp bị .gitignore thì không tính. Lỗi Git được trả ra,
// không chấp nhận kết quả khi không quan sát được cây.
//
// dir có thể là thư mục con của repository (.bint nằm trong thư mục con): chụp ở GỐC repo
// (`git rev-parse --show-toplevel` từ dir) để phủ cả tệp ngoài dir. Gốc repo được tính MỘT lần và
// dùng cho cả hai lần chụp.
//
// Submodule: write-tree chỉ ghi gitlink (commit) của submodule, nên mỗi ảnh chụp còn đệ quy vào mọi
// submodule đã checkout (gitlink trong chính cây vừa ghi) và chụp cây làm việc của nó bằng cùng giao
// thức; sửa tệp trong submodule (kể cả tệp vốn đã bẩn) đổi mã cây của submodule nên bị phát hiện.
// Submodule chưa checkout (không có .git) không có gì để chụp.
func guardReadOnly(ctx context.Context, dir string, run func()) error {
	withTimeout := func() (context.Context, context.CancelFunc) {
		if _, ok := ctx.Deadline(); !ok {
			return context.WithTimeout(ctx, snapshotTimeout)
		}
		return ctx, func() {}
	}
	rctx, cancel := withTimeout()
	root, err := repoToplevelFn(rctx, dir)
	cancel()
	if err != nil {
		return fmt.Errorf("không chụp được cây Git trước khi chạy agent evolver: %w", err)
	}
	snap := func() (string, error) {
		sctx, cancel := withTimeout()
		defer cancel()
		return snapshotTree(sctx, root, 0)
	}
	before, err := snap()
	if err != nil {
		return fmt.Errorf("không chụp được cây Git trước khi chạy agent evolver: %w", err)
	}
	run()
	after, err := snap()
	if err != nil {
		return fmt.Errorf("không chụp được cây Git sau khi chạy agent evolver (không kiểm được nó có sửa cây hay không): %w", err)
	}
	if before != after {
		return fmt.Errorf("agent evolver đã sửa cây làm việc (chỉ được đọc); bỏ kết quả — kiểm tra `git status`")
	}
	return nil
}

// snapshotTree trả mã cây của root, nối thêm mã cây của từng submodule đã checkout (đệ quy).
func snapshotTree(ctx context.Context, root string, depth int) (string, error) {
	tree, err := workflow.WorktreeSnapshot(ctx, root)
	if err != nil {
		return "", err
	}
	if depth >= maxSubmoduleDepth {
		return tree, nil
	}
	cmd := gitrepo.CommandContext(ctx, "ls-tree", "-r", "-z", tree)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git ls-tree: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	var b strings.Builder
	b.WriteString(tree)
	for _, ent := range strings.Split(string(out), "\x00") {
		meta, path, ok := strings.Cut(ent, "\t")
		if !ok || !strings.HasPrefix(meta, "160000 ") {
			continue
		}
		sub := filepath.Join(root, filepath.FromSlash(path))
		if _, err := os.Stat(filepath.Join(sub, ".git")); err != nil {
			continue // chưa checkout
		}
		st, err := snapshotTree(ctx, sub, depth+1)
		if err != nil {
			return "", fmt.Errorf("submodule %s: %w", path, err)
		}
		b.WriteString("\n" + path + "=" + st)
	}
	return b.String(), nil
}

// repoToplevel trả gốc repository Git chứa dir.
func repoToplevel(ctx context.Context, dir string) (string, error) {
	cmd := gitrepo.CommandContext(ctx, "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("không tìm được gốc repository Git: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("không tìm được gốc repository Git (kết quả rỗng)")
	}
	return root, nil
}
