package app

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"bint/internal/ctxengine"
	"bint/internal/harness/claude"
	"bint/internal/kindex"
	"bint/internal/knowledge"
	"bint/internal/mcpserver"
)

// RepoBackend phục vụ MCP trên repository đã khởi tạo Bint. Ghi nhận truy xuất và
// trích dẫn vào lượt chạy được thực hiện bởi hook PostToolUse của harness (hook biết
// session_id, MCP server thì không); không có hook thì chỉ không quy được về lượt chạy.
type RepoBackend struct {
	Dir string

	mu   sync.Mutex
	c    *ContextWS
	seen map[string]bool // mục đã trả về qua context_for_paths trong phiên MCP này
}

// Close giải phóng tài nguyên.
func (b *RepoBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.c != nil {
		b.c.Close()
		b.c = nil
	}
}

// engine mở (một lần) và làm tươi Engine khi tệp tri thức đổi (sau git pull,
// chuyển nhánh, hoặc người dùng vừa duyệt mục).
func (b *RepoBackend) engine(ctx context.Context) (*ContextWS, error) {
	if b.c == nil {
		c, err := OpenContext(ctx, b.Dir)
		if err != nil {
			return nil, err
		}
		b.c = c
		return c, nil
	}
	if b.c.Memory {
		items, _, err := b.c.K.Items()
		if err != nil {
			return nil, err
		}
		if err := kindex.Load(ctx, b.c.Store.DB, items, true, nil); err != nil {
			return nil, err
		}
		b.c.Engine.Items = items
		b.c.Engine.Reset()
		return b.c, nil
	}
	res, err := kindex.Sync(ctx, b.c.Store.DB, b.c.K.Store, false)
	if err != nil {
		return nil, err
	}
	if !res.Skipped {
		items, _, err := b.c.K.Items()
		if err != nil {
			return nil, err
		}
		b.c.Engine.Items = items
		b.c.Engine.Reset()
	}
	b.c.Engine.Src = b.c.K.Source() // danh sách tệp có thể đã đổi
	return b.c, nil
}

func (b *RepoBackend) ForTask(ctx context.Context, task string, paths []string) (ctxengine.Pack, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return ctxengine.Pack{}, err
	}
	return c.Engine.ForTask(ctx, task, paths)
}

func (b *RepoBackend) ForPaths(ctx context.Context, paths []string) (ctxengine.Pack, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return ctxengine.Pack{}, err
	}
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	p := c.Engine.ForPaths(relPaths(c.K.Root, paths), b.seen)
	for _, id := range p.IDs() {
		b.seen[id] = true
	}
	return p, nil
}

func (b *RepoBackend) Search(ctx context.Context, q string, limit int) ([]kindex.Hit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return nil, err
	}
	return kindex.Search(ctx, c.Store.DB, q, kindex.SearchOptions{Limit: limit})
}

func (b *RepoBackend) Get(ctx context.Context, ref string) (*knowledge.Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return nil, err
	}
	it, err := c.Engine.Get(ref)
	if err != nil {
		return nil, err
	}
	if ok, why := c.Engine.Eligible(it); !ok {
		return nil, fmt.Errorf("mục %s không được cung cấp: %s", it.ID, why)
	}
	return it, nil
}

func (b *RepoBackend) Cite(ctx context.Context, ids []string, note string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return nil, err
	}
	return resolveIDs(c.Engine, ids)
}

func (b *RepoBackend) Observe(ctx context.Context, o mcpserver.Observation) (string, error) {
	if o.Type == "" || len(o.Evidence) == 0 {
		return "", nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	c, err := b.engine(ctx)
	if err != nil {
		return "", err
	}
	res, err := AddKnowledge(ctx, c.K, AddOptions{Type: o.Type, Title: o.Title, Body: o.Body, Paths: o.Paths,
		Evidence: o.Evidence, Origin: knowledge.OriginAgent, Inputs: o.Evidence})
	if err != nil {
		return "", err
	}
	if !res.Saved {
		var msgs []string
		for _, f := range res.Check.Findings {
			if f.Level == knowledge.LevelError {
				msgs = append(msgs, f.Detail)
			}
		}
		return "", fmt.Errorf("mục không qua kiểm tra xác định được: %s", strings.Join(msgs, "; "))
	}
	return res.Item.ID, nil
}

func resolveIDs(e *ctxengine.Engine, ids []string) ([]string, error) {
	var ok []string
	var bad []string
	for _, id := range ids {
		it, err := e.Get(id)
		if err != nil {
			bad = append(bad, id)
			continue
		}
		ok = append(ok, it.ID)
	}
	if len(ok) == 0 {
		return nil, fmt.Errorf("không có mục nào khớp: %s", strings.Join(bad, ", "))
	}
	return ok, nil
}

func relPaths(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, ok := claude.RelPath(root, root, p); ok {
			out = append(out, rel)
		}
	}
	return out
}

// EvalBackend phục vụ MCP trong thư mục đánh giá cô lập (nhánh bint, mục 12.4):
// tri thức là bản sao của tập mục mà nhánh minimal dùng, chỉ mục trong bộ nhớ, ghi
// nhận vào tệp nhật ký thay vì cơ sở dữ liệu.
type EvalBackend struct {
	Engine *ctxengine.Engine
	Log    string

	mu   sync.Mutex
	seen map[string]bool
}

// NewEvalBackend dựng backend từ cấu hình ngữ cảnh đánh giá.
func NewEvalBackend(ctx context.Context, ec ctxengine.EvalConfig) (*EvalBackend, func(), error) {
	e, closeFn, err := ec.Engine(ctx)
	if err != nil {
		return nil, nil, err
	}
	return &EvalBackend{Engine: e, Log: ec.Log}, closeFn, nil
}

func (b *EvalBackend) ForTask(ctx context.Context, task string, paths []string) (ctxengine.Pack, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, err := b.Engine.ForTask(ctx, task, paths)
	if err == nil {
		ctxengine.AppendLog(b.Log, ctxengine.PackLog(ChannelMCPTask, p))
	}
	return p, err
}

func (b *EvalBackend) ForPaths(ctx context.Context, paths []string) (ctxengine.Pack, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	p := b.Engine.ForPaths(paths, b.seen)
	for _, id := range p.IDs() {
		b.seen[id] = true
	}
	ctxengine.AppendLog(b.Log, ctxengine.PackLog(ChannelMCPPaths, p))
	return p, nil
}

func (b *EvalBackend) Search(ctx context.Context, q string, limit int) ([]kindex.Hit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return kindex.Search(ctx, b.Engine.DB, q, kindex.SearchOptions{Limit: limit})
}

func (b *EvalBackend) Get(ctx context.Context, ref string) (*knowledge.Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, err := b.Engine.Get(ref)
	if err != nil {
		return nil, err
	}
	if ok, why := b.Engine.Eligible(it); !ok {
		return nil, fmt.Errorf("mục %s không được cung cấp: %s", it.ID, why)
	}
	return it, nil
}

func (b *EvalBackend) Cite(ctx context.Context, ids []string, note string) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ok, err := resolveIDs(b.Engine, ids)
	if err == nil {
		ctxengine.AppendLog(b.Log, ctxengine.LogLine{Channel: "cite", IDs: ok})
	}
	return ok, err
}

func (b *EvalBackend) Observe(ctx context.Context, o mcpserver.Observation) (string, error) {
	ctxengine.AppendLog(b.Log, ctxengine.LogLine{Channel: "observe"})
	return "", nil
}
