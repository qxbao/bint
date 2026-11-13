package evolve

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"bint/internal/workflow"
)

// snapshotTimeout là hạn mỗi lần chụp cây khi ctx của người gọi không có hạn.
const snapshotTimeout = 2 * time.Minute

// guardReadOnly chạy run (agent evolver chỉ được đọc) và báo lỗi nếu nội dung cây làm việc đổi
// hoặc nếu không quan sát được cây trước/sau (P6-02). Dùng chung cho evolver tri thức và skill.
//
// Giao thức chụp là workflow.WorktreeSnapshot: chỉ mục tạm (GIT_INDEX_FILE, không đụng chỉ mục thật
// của người dùng), `git add -A -- .`, rồi `git write-tree`. Mã cây đổi theo NỘI DUNG, nên tệp vốn đã
// bẩn (M) mà bị sửa thêm vẫn bị phát hiện; tệp bị .gitignore thì không tính. Lỗi Git được trả ra,
// không chấp nhận kết quả khi không quan sát được cây.
//
// dir có thể là thư mục con của repository (.bint nằm trong thư mục con): chụp ở GỐC repo
// (`git rev-parse --show-toplevel` từ dir) để phủ cả tệp ngoài dir.
//
// Giới hạn đã biết: write-tree chỉ ghi gitlink (commit) của submodule, nên sửa trong cây làm việc
// của một submodule không bị phát hiện (so chuỗi `git status` cũ thấy được khi submodule vốn sạch).
func guardReadOnly(ctx context.Context, dir string, run func()) error {
	snap := func() (string, error) {
		sctx := ctx
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			sctx, cancel = context.WithTimeout(ctx, snapshotTimeout)
			defer cancel()
		}
		root, err := repoToplevel(sctx, dir)
		if err != nil {
			return "", err
		}
		return workflow.WorktreeSnapshot(sctx, root)
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

// repoToplevel trả gốc repository Git chứa dir.
func repoToplevel(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
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
