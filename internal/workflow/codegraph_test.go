package workflow

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/task"
)

// Test chuẩn bị chỉ mục codegraph cho worktree tác vụ (docs/mcp-gateway-plan.md §5.5).

func (h *harness) useCodegraph() {
	h.cfg.MCP.Services = map[string]config.MCPService{"codegraph": {}}
}

// excludeCodegraph thêm .codegraph/ vào info/exclude của repo (như codegraph init/bint mcp index làm).
func (h *harness) excludeCodegraph() {
	h.t.Helper()
	if _, _, err := mcpgw.EnsureGitExclude(context.Background(), h.root, ".codegraph/"); err != nil {
		h.t.Fatal(err)
	}
}

func setCodegraphSeams(t *testing.T, init func(context.Context, string, io.Writer) (mcpgw.CodegraphInitResult, error),
	cp func(context.Context, string, string) error) {
	t.Helper()
	oi, oc := codegraphInit, codegraphCopy
	codegraphInit, codegraphCopy = init, cp
	t.Cleanup(func() { codegraphInit, codegraphCopy = oi, oc })
}

func TestWorktreeCodegraphCopiesConsistentIndex(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.useCodegraph()
	h.excludeCodegraph()
	dir := filepath.Join(h.root, ".codegraph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE nodes(n TEXT)", "INSERT INTO nodes VALUES('trong-wal')"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{".gitignore": "*\n", "codegraph.pid": "1", "daemon.sock": "x", "daemon.lock": "x"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	var rows string
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			ents, _ := os.ReadDir(filepath.Join(e.Root, ".codegraph"))
			for _, en := range ents {
				seen = append(seen, en.Name())
			}
			c, err := sql.Open("sqlite", filepath.Join(e.Root, ".codegraph", "codegraph.db"))
			if err != nil {
				return err
			}
			defer c.Close()
			if err := c.QueryRow("SELECT n FROM nodes").Scan(&rows); err != nil {
				return err
			}
		}
		return featWrites(h, nil)(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	if rows != "trong-wal" {
		t.Fatalf("dữ liệu còn trong WAL của gốc không có trong bản chép: %q", rows)
	}
	// Chỉ codegraph.db và .gitignore; không -wal/-shm/*.pid/daemon.* (SQLite có thể tạo -wal/-shm khi mở bản chép để đọc,
	// nên chỉ kiểm các tệp mà Bint chép từ gốc).
	for _, n := range seen {
		if n == "codegraph.pid" || strings.HasPrefix(n, "daemon.") {
			t.Errorf("tệp không được chép sang worktree: %s (thấy %v)", n, seen)
		}
	}
	if !slicesContainsPrefix(seen, "codegraph.db") || !slicesContainsPrefix(seen, ".gitignore") {
		t.Fatalf("worktree thiếu codegraph.db hoặc .gitignore: %v", seen)
	}
}

func TestPrepareCodegraphInitsOnceUnderConcurrency(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.useCodegraph()
	var inits atomic.Int32
	setCodegraphSeams(t, func(ctx context.Context, root string, out io.Writer) (mcpgw.CodegraphInitResult, error) {
		inits.Add(1)
		time.Sleep(150 * time.Millisecond) // giữ khoá đủ lâu để tác vụ kia phải chờ
		if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
			return mcpgw.CodegraphInitResult{}, err
		}
		if err := os.WriteFile(filepath.Join(root, mcpgw.CodegraphDB), []byte("db"), 0o644); err != nil {
			return mcpgw.CodegraphInitResult{}, err
		}
		_, _, err := mcpgw.EnsureGitExclude(ctx, root, ".codegraph/")
		return mcpgw.CodegraphInitResult{Action: "init"}, err
	}, func(context.Context, string, string) error { return nil })
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.runner().prepareCodegraph(context.Background(), h.root, filepath.Join(t.TempDir(), "wt"), "")
		}()
	}
	wg.Wait()
	if n := inits.Load(); n != 1 {
		t.Fatalf("init chạy %d lần, mong 1", n)
	}
	b, _ := os.ReadFile(filepath.Join(h.root, ".git", "info", "exclude"))
	if !strings.Contains(string(b), ".codegraph/") {
		t.Fatalf("info/exclude thiếu .codegraph/: %q", b)
	}
}

func TestWorktreeCodegraphErrorsAreNotesOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		init func(context.Context, string, io.Writer) (mcpgw.CodegraphInitResult, error)
		cp   func(context.Context, string, string) error
		have bool // gốc đã có chỉ mục
	}{
		"init lỗi": {init: func(context.Context, string, io.Writer) (mcpgw.CodegraphInitResult, error) {
			return mcpgw.CodegraphInitResult{}, errors.New("codegraph init: exit status 1")
		}, cp: func(context.Context, string, string) error { return nil }},
		"chép lỗi": {init: nil, cp: func(context.Context, string, string) error { return errors.New("đĩa đầy") }, have: true},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t).wtHarness()
			h.useCodegraph()
			h.behave = featWrites(h, nil)
			if tc.have {
				h.write(".codegraph/codegraph.db", "x")
				h.excludeCodegraph()
			}
			setCodegraphSeams(t, tc.init, tc.cp)
			r := h.runner()
			tk := h.newDo(r, NewTaskOptions{})
			res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
			if err != nil || res.Status != task.StatusDone {
				t.Fatalf("lỗi chỉ mục không được làm tác vụ failed: %+v %v", res, err)
			}
			if !slicesContainsPrefix(res.Notes, "codegraph:") {
				t.Fatalf("thiếu ghi chú codegraph: %v", res.Notes)
			}
		})
	}
}

func slicesContainsPrefix(ss []string, p string) bool {
	for _, s := range ss {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func TestWorktreeCodegraphNotDeclaredDoesNothing(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.write(".codegraph/codegraph.db", "x")
	h.excludeCodegraph()
	var calls atomic.Int32
	setCodegraphSeams(t, func(context.Context, string, io.Writer) (mcpgw.CodegraphInitResult, error) {
		calls.Add(1)
		return mcpgw.CodegraphInitResult{}, nil
	}, func(context.Context, string, string) error { calls.Add(1); return nil })
	var has bool
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			_, err := os.Stat(filepath.Join(e.Root, ".codegraph"))
			has = err == nil
		}
		return featWrites(h, nil)(ctx, step, e, id)
	}
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID}); err != nil || res.Status != task.StatusDone {
		t.Fatalf("run: %+v %v", res, err)
	}
	if calls.Load() != 0 || has {
		t.Fatalf("không khai báo codegraph mà vẫn chạy (lời gọi %d, có .codegraph %v)", calls.Load(), has)
	}
}

func TestCleanupWorktreeReleasesGatewayBounded(t *testing.T) {
	o := releaseGatewayWorktree
	t.Cleanup(func() { releaseGatewayWorktree = o })
	h := newHarness(t).wtHarness()
	r := h.runner()
	wt := filepath.Join(t.TempDir(), "wt")

	var got string
	releaseGatewayWorktree = func(ctx context.Context, p string) error { got = p; return nil }
	r.cleanupWorktree(context.Background(), "ID1", h.root, wt)
	if got != wt {
		t.Fatalf("không gọi nhả với worktree: %q", got)
	}

	releaseGatewayWorktree = func(context.Context, string) error { return &mcpgw.StatusError{Code: 404, Body: "not found"} }
	r.cleanupWorktree(context.Background(), "ID1", h.root, wt) // lỗi 404 (daemon cũ) bị bỏ qua

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	releaseGatewayWorktree = func(context.Context, string) error { <-stop; return nil } // treo
	// Đo riêng phần nhả: cleanupWorktree còn 5–6 lệnh git mà trên Windows tải nặng (agent, thăm dò, go test song song, Defender) mất vài
	// giây, nên đo cả hàm làm test đỏ theo tải máy (6–7 s > trần 5 s) dù phần nhả vẫn dừng đúng hạn.
	start := time.Now()
	r.releaseWorktreeParts(context.Background(), wt)
	if d := time.Since(start); d > releaseTimeout+2*time.Second {
		t.Fatalf("nhả treo không dừng sau releaseTimeout: %s", d)
	}
	// Dọn với nhả treo vẫn phải xong (không chờ gateway mãi); trần rộng vì gồm các lệnh git.
	done := make(chan struct{})
	go func() { r.cleanupWorktree(context.Background(), "ID1", h.root, wt); close(done) }()
	select {
	case <-done:
	case <-time.After(releaseTimeout + time.Minute):
		t.Fatal("nhả treo làm chặn dọn worktree")
	}
}

// Gốc đã có chỉ mục (codegraph tự lập, không qua bint mcp index) nhưng info/exclude thiếu .codegraph/: bước chuẩn bị phải thêm
// dòng đó trước khi chép, nếu không .codegraph/ trong worktree thành tệp chưa theo dõi.
func TestWorktreeCodegraphAddsExcludeWhenOriginIndexed(t *testing.T) {
	h := newHarness(t).wtHarness()
	h.useCodegraph()
	h.behave = featWrites(h, nil)
	h.write(".codegraph/codegraph.db", "x")
	setCodegraphSeams(t, func(context.Context, string, io.Writer) (mcpgw.CodegraphInitResult, error) {
		t.Fatal("gốc đã có chỉ mục thì không được init")
		return mcpgw.CodegraphInitResult{}, nil
	}, func(context.Context, string, string) error { return nil })
	r := h.runner()
	tk := h.newDo(r, NewTaskOptions{})
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, TaskRef: tk.State.ID})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ := os.ReadFile(filepath.Join(h.root, ".git", "info", "exclude"))
	if !strings.Contains(string(b), ".codegraph/") {
		t.Fatalf("info/exclude thiếu .codegraph/: %q", b)
	}
}
