package app

import (
	"context"

	"bint/internal/config"
	"bint/internal/kindex"
	"bint/internal/knowledge"
)

// IndexRebuildOptions: `bint index rebuild` (không có tham số).
type IndexRebuildOptions struct{}

func IndexRebuildDefaults(config.Config) IndexRebuildOptions { return IndexRebuildOptions{} }
func (o IndexRebuildOptions) Validate() error                { return nil }

// IndexRebuildResult là kết quả dựng lại chỉ mục tri thức (FTS5).
type IndexRebuildResult struct {
	// Total là số mục đã đưa vào chỉ mục.
	Total int
	// Errors là các tệp tri thức hỏng bị bỏ qua.
	Errors []knowledge.LoadError
}

// RebuildIndex xoá và dựng lại chỉ mục tri thức từ .bint/knowledge/items. Chỉ mục là dữ liệu dẫn xuất nên
// không mất thông tin. Cần repository đã `bint init`.
func RebuildIndex(ctx context.Context, dir string, o IndexRebuildOptions) (IndexRebuildResult, error) {
	if err := o.Validate(); err != nil {
		return IndexRebuildResult{}, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return IndexRebuildResult{}, err
	}
	if !k.Paths.Initialized() {
		return IndexRebuildResult{}, ErrNotInitialized
	}
	s, err := k.OpenStore(ctx)
	if err != nil {
		return IndexRebuildResult{}, err
	}
	defer s.Close()
	res, err := kindex.Rebuild(ctx, s.DB, k.Store)
	if err != nil {
		return IndexRebuildResult{}, err
	}
	return IndexRebuildResult{Total: res.Total, Errors: res.Errors}, nil
}
