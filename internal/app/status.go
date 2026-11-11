package app

import (
	"context"
	"database/sql"
	"time"

	"bint/internal/config"
	"bint/internal/runlog"
)

// StatusReport là trạng thái Bint của repository (`bint status`, màn Tổng quan của TUI).
type StatusReport struct {
	Repo     string
	RepoID   string
	StateDir string
	// Schema là phiên bản schema cơ sở dữ liệu trạng thái.
	Schema    int
	Harnesses []string
	// Runs là số lượt chạy theo trạng thái; TotalRuns là tổng.
	Runs      map[string]int
	TotalRuns int
	// PendingProposals là số đề xuất tiến hoá đang chờ duyệt.
	PendingProposals int
	// Ingest là kết quả nạp spool lần này (sự kiện mới, lượt chuyển interrupted).
	Ingest runlog.IngestResult
	// Config là cấu hình đã nạp (để người gọi quyết định các việc phụ như tiến hoá hoãn).
	Config config.Config
	// LinkedWorktree: đang ở worktree phụ (cấu hình nằm ở worktree chính).
	LinkedWorktree bool
	// MainWorktree là đường dẫn worktree chính (rỗng nếu không phải worktree phụ hoặc không biết).
	MainWorktree string
}

// Status mở workspace đã khởi tạo ở dir, nạp spool rồi đọc trạng thái. Lỗi ErrNotInitialized
// khi repository chưa `bint init`.
func Status(ctx context.Context, dir string) (*StatusReport, error) { return status(ctx, dir, true) }

// StatusNoIngest như Status nhưng không nạp spool vào cơ sở dữ liệu (không ghi sự kiện, không đánh
// dấu lượt chạy interrupted): cho TUI ở chế độ chỉ đọc. Ingest trong kết quả để trống.
func StatusNoIngest(ctx context.Context, dir string) (*StatusReport, error) {
	return status(ctx, dir, false)
}

func status(ctx context.Context, dir string, ingest bool) (*StatusReport, error) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	s, err := ws.OpenStore(ctx)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var res runlog.IngestResult
	if ingest {
		if res, err = ws.Ingest(ctx, s, time.Now()); err != nil {
			return nil, err
		}
	}
	counts, err := runlog.StatusCounts(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	v, _ := s.Version(ctx)
	rep := &StatusReport{
		Repo: ws.Repo.Toplevel, RepoID: ws.Paths.RepoID, StateDir: ws.Paths.Root, Schema: v,
		Harnesses: ws.Config.Harnesses.Enabled, Runs: counts, Ingest: res, Config: ws.Config,
		LinkedWorktree: ws.Repo.IsLinkedWorktree(), MainWorktree: ws.Repo.MainWorktree(),
	}
	for _, n := range counts {
		rep.TotalRuns += n
	}
	rep.PendingProposals = countPendingProposals(ctx, s.DB)
	return rep, nil
}

func countPendingProposals(ctx context.Context, db *sql.DB) int {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evolution_proposals WHERE status = 'proposed'`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// PendingProposals đếm đề xuất tiến hoá đang chờ duyệt ở repository chứa dir (đọc lại sau khi một
// việc nền như xử lý hàng đợi hoãn có thể đã tạo đề xuất mới).
func PendingProposals(ctx context.Context, dir string) int {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return 0
	}
	s, err := ws.OpenStore(ctx)
	if err != nil {
		return 0
	}
	defer s.Close()
	return countPendingProposals(ctx, s.DB)
}
