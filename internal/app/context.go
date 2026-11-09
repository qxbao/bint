package app

import (
	"context"
	"fmt"

	"bint/internal/ctxengine"
	"bint/internal/kindex"
	"bint/internal/store"
)

// ContextWS là Context Engine đã mở cho một repository.
type ContextWS struct {
	K      *KnowledgeWS
	Store  *store.Store
	Engine *ctxengine.Engine
	Sync   kindex.SyncResult
	// Memory = true khi chỉ mục nằm trong bộ nhớ (repository chưa có thư mục trạng
	// thái trên máy này).
	Memory bool
}

// Close đóng cơ sở dữ liệu.
func (c *ContextWS) Close() {
	if c.Store != nil {
		c.Store.Close()
	}
}

// OpenContext mở tri thức, đồng bộ chỉ mục FTS5 (dẫn xuất) và dựng Engine. Nếu
// repository chưa có thư mục trạng thái trên máy này thì chỉ mục nằm trong bộ nhớ.
func OpenContext(ctx context.Context, dir string) (*ContextWS, error) {
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	items, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	c := &ContextWS{K: k}
	if k.Paths.Initialized() {
		s, err := k.OpenStore(ctx)
		if err != nil {
			return nil, err
		}
		c.Store = s
		if c.Sync, err = kindex.Sync(ctx, s.DB, k.Store, false); err != nil {
			s.Close()
			return nil, fmt.Errorf("đồng bộ chỉ mục: %w", err)
		}
	} else {
		s, err := kindex.Memory(ctx, items)
		if err != nil {
			return nil, err
		}
		c.Store, c.Memory = s, true
	}
	cfg := k.Config.Context
	c.Engine = &ctxengine.Engine{Items: items, Src: k.Source(), DB: c.Store.DB, MaxTokens: cfg.MaxTokens, PathMaxItems: cfg.PathMaxItems}
	return c, nil
}

// Kênh truy xuất (bí danh của ctxengine).
const (
	ChannelMCPTask  = ctxengine.ChannelMCPTask
	ChannelMCPPaths = ctxengine.ChannelMCPPaths
	ChannelHookPath = ctxengine.ChannelHookPath
	ChannelCLI      = ctxengine.ChannelCLI
)

// ContextEvent dựng sự kiện context.retrieved cho spool.
var ContextEvent = ctxengine.ContextEvent

// SessionMemory là bộ nhớ mục đã chèn trong phiên.
type SessionMemory = ctxengine.SessionMemory

// NewSessionMemory trả về bộ nhớ phiên trong thư mục runtime.
var NewSessionMemory = ctxengine.NewSessionMemory

// KnowledgeStats trả về thống kê truy xuất/trích dẫn theo mục (mục 8.8).
func KnowledgeStats(ctx context.Context, c *ContextWS) ([]kindex.Stats, error) {
	if c.Memory {
		return nil, ErrNotInitialized
	}
	return kindex.ItemStats(ctx, c.Store.DB)
}
