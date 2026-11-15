package workflow

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"bint/internal/fslock"
	"bint/internal/mcpgw"
)

// Chuẩn bị chỉ mục codegraph cho worktree tác vụ (docs/mcp-gateway-plan.md §5.5): chép bản nhất quán chỉ mục của repo gốc
// sang worktree để codegraph (qua gateway, projectPath = worktree) dùng được ngay và tự cập nhật theo tệp của worktree.
// Mọi lỗi chỉ là ghi chú, không làm tác vụ failed.

// Các biến gói cho phép kiểm thử thay thế.
var (
	// codegraphInit lập chỉ mục ở repo gốc (codegraph init -y); out nhận đầu ra của codegraph.
	codegraphInit = mcpgw.CodegraphInit
	// codegraphCopy chép chỉ mục từ origin sang worktree wt.
	codegraphCopy = CopyCodegraphIndex
	// releaseGatewayWorktree nhả phân vùng gateway của worktree wt; lỗi được người gọi bỏ qua.
	releaseGatewayWorktree = releaseViaGateway
)

const (
	codegraphInitTimeout = 10 * time.Minute
	releaseTimeout       = 3 * time.Second
)

// codegraphLockPath là tệp khoá tuần tự hoá việc init chỉ mục ở repo gốc (một lần dù nhiều tác vụ tạo worktree đồng thời).
func (e *Env) codegraphLockPath() string {
	return filepath.Join(filepath.Dir(e.Tasks.Dir), "codegraph-index.lock")
}

// prepareCodegraph trả các ghi chú (lỗi hoặc việc đã làm) của bước chuẩn bị chỉ mục cho worktree wt.
func (r *Runner) prepareCodegraph(ctx context.Context, origin, wt, logPath string) []string {
	if !r.Cfg.MCP.Uses("codegraph") {
		return nil
	}
	var notes []string
	if _, err := os.Stat(filepath.Join(origin, mcpgw.CodegraphDB)); err != nil {
		n, err := r.initOriginIndex(ctx, origin, logPath)
		if err != nil {
			return []string{"codegraph: không lập được chỉ mục ở repo gốc: " + err.Error()}
		}
		notes = append(notes, n...)
	}
	// Chỉ mục gốc có thể do codegraph tự lập (không qua bint mcp index) nên info/exclude chưa chắc có .codegraph/; thiếu thì
	// .codegraph/ chép sang thành tệp chưa theo dõi của worktree và bị tính khi kiểm phạm vi b-feat. info/exclude nằm ở
	// thư mục git chung nên áp cho cả worktree.
	if _, _, err := mcpgw.EnsureGitExclude(ctx, origin, ".codegraph/"); err != nil {
		return append(notes, "codegraph: không thêm .codegraph/ vào info/exclude, bỏ qua chép chỉ mục: "+err.Error())
	}
	if err := codegraphCopy(ctx, origin, wt); err != nil {
		return append(notes, "codegraph: không chép chỉ mục sang worktree: "+err.Error())
	}
	r.logf("worktree: đã chép chỉ mục codegraph sang %s", wt)
	return notes
}

// initOriginIndex chạy codegraph init -y ở repo gốc dưới khoá liên tiến trình; kiểm lại chỉ mục sau khi có khoá (tác vụ
// khác có thể đã lập xong) nên init chạy đúng một lần. Khoá dùng fslock.Try: hệ điều hành nhả khi tiến trình chết.
func (r *Runner) initOriginIndex(ctx context.Context, origin, logPath string) ([]string, error) {
	lp := r.codegraphLockPath()
	if err := os.MkdirAll(filepath.Dir(lp), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for {
		busy, err := fslock.Try(f, true)
		if err != nil {
			return nil, err
		}
		if !busy {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Khoá được nhả khi f đóng (defer).
	if _, err := os.Stat(filepath.Join(origin, mcpgw.CodegraphDB)); err == nil {
		return nil, nil
	}
	var out io.Writer = io.Discard
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
			if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				defer lf.Close()
				out = lf
			}
		}
	}
	r.logf("worktree: repo gốc chưa có chỉ mục codegraph, chạy codegraph init -y (có thể mất vài giây)")
	ictx, cancel := context.WithTimeout(ctx, codegraphInitTimeout)
	defer cancel()
	res, err := codegraphInit(ictx, origin, out)
	if err != nil {
		return nil, err
	}
	n := []string{"codegraph: đã lập chỉ mục ở repo gốc (" + res.Action + ")"}
	if res.Warning != "" {
		n = append(n, "codegraph: "+res.Warning)
	}
	return n, nil
}

// releaseViaGateway nhả phân vùng gateway của worktree wt; không có gateway.json thì trả ngay (không có gì để nhả).
func releaseViaGateway(ctx context.Context, wt string) error {
	dir, err := mcpgw.Dir()
	if err != nil {
		return err
	}
	c, err := mcpgw.Connect(dir)
	if err != nil {
		if errors.Is(err, mcpgw.ErrNotRunning) {
			return nil
		}
		return err
	}
	_, err = c.ReleaseWorktree(ctx, wt)
	return err
}

// releaseWorktreeParts gọi releaseGatewayWorktree có giới hạn thời gian: gateway không chạy, daemon bản cũ (404) hoặc lỗi
// thì bỏ qua (không ghi nhận), không chặn việc dọn.
func (e *Env) releaseWorktreeParts(ctx context.Context, wt string) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()
	done := make(chan error, 1)
	// Đọc seam trước khi mở goroutine: hết hạn thì hàm trả về không chờ goroutine, nên đọc bên trong goroutine sẽ đua với
	// việc test gán lại seam (race detector).
	release := releaseGatewayWorktree
	go func() { done <- release(rctx, wt) }()
	select {
	case <-done: // lỗi (gateway không chạy, daemon cũ trả 404...) bị bỏ qua
	case <-rctx.Done():
	}
}
