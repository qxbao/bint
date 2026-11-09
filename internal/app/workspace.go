// Package app chứa logic các lệnh của Bint, tách khỏi tầng CLI để kiểm thử.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/runlog"
	"bint/internal/state"
	"bint/internal/store"
)

// ErrNotInitialized: repository chưa chạy `bint init` (trên máy này).
var ErrNotInitialized = errors.New("repository chưa được khởi tạo Bint trên máy này (chạy `bint init`)")

// Workspace gom các thông tin đã giải quyết cho repository hiện tại.
type Workspace struct {
	Repo    gitrepo.Repo
	Config  config.Config
	Sources config.Sources
	Paths   state.Paths
}

// OpenWorkspace xác định repository chứa dir và nạp cấu hình. Nếu requireInit,
// cần có cả .bint/config.yml và thư mục trạng thái.
func OpenWorkspace(ctx context.Context, dir string, requireInit bool) (*Workspace, error) {
	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		return nil, err
	}
	cfg, src, err := config.LoadFirst(repo.ConfigRoots(), requireInit)
	if errors.Is(err, config.ErrNoProjectConfig) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return nil, err
	}
	if requireInit && !paths.Initialized() {
		return nil, ErrNotInitialized
	}
	return &Workspace{Repo: repo, Config: cfg, Sources: src, Paths: paths}, nil
}

// OpenStore mở cơ sở dữ liệu trạng thái (áp migration nếu cần).
func (w *Workspace) OpenStore(ctx context.Context) (*store.Store, error) {
	return store.Open(ctx, w.Paths.DB)
}

// Ingest nạp hàng đợi spool vào cơ sở dữ liệu.
func (w *Workspace) Ingest(ctx context.Context, s *store.Store, now time.Time) (runlog.IngestResult, error) {
	res, err := runlog.Ingest(ctx, s, w.Paths.Spool, now, runlog.DefaultStaleAfter)
	if err != nil {
		return res, fmt.Errorf("nạp spool: %w", err)
	}
	return res, nil
}
