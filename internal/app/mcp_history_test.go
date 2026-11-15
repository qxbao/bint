package app

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"bint/internal/mcpserver"
	"bint/internal/runlog"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/testutil"
)

func histEv(id, typ, session string, ts time.Time, p map[string]any) spool.Event {
	return spool.Event{SchemaVersion: 1, EventID: id, Type: typ, Source: "test", Timestamp: ts, Harness: "claude", SessionID: session, Payload: p}
}

func TestHistoryTools(t *testing.T) {
	testutil.Isolate(t)
	for _, k := range agentEnvVars {
		t.Setenv(k, "")
	}
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"internal/db/db.go": "package db\n", "README.md": "x\n"})
	mustInit(t, InitOptions{Dir: root})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	// Phiếu có hai phát hiện: một ở internal/db/db.go (khớp), một ở README.md (không khớp).
	store := task.NewStore(k.Paths.Runtime)
	tk, err := store.Create(task.Ticket{Goal: "sửa db"}, task.State{Workflow: "b-feat"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sub := task.Submission{Summary: "s", Findings: []task.Finding{
		{Kind: "bug", Severity: "high", Title: "Open bỏ qua lỗi", Path: "internal/db/db.go", Lines: "1", Evidence: "e"},
		{Kind: "risk", Severity: "low", Title: "README cũ", Path: "README.md", Evidence: "e"},
	}}
	if _, err := store.Update(tk.State.ID, time.Now(), func(x *task.Task) error {
		_, err := x.AddRound(sub, "agent", "", "t")
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Hai lượt chạy qua spool: S1 sửa db.go (kiểm thử hỏng), S2 chỉ đọc db.go.
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if _, err := spool.Write(k.Paths.Spool,
		histEv("h1", spool.SessionStarted, "S1", t0, map[string]any{"model": "m1", "git_head": "abc123"}),
		histEv("h2", spool.FileModified, "S1", t0.Add(time.Second), map[string]any{"path": "internal/db/db.go"}),
		histEv("h3", spool.TestCompleted, "S1", t0.Add(2*time.Second), map[string]any{"command": "go test ./internal/db", "ok": false}),
		histEv("h4", spool.SessionStarted, "S2", t0.Add(time.Minute), map[string]any{"model": "m2"}),
		histEv("h5", spool.FileRead, "S2", t0.Add(time.Minute+time.Second), map[string]any{"paths": []string{"internal/db/db.go"}}),
	); err != nil {
		t.Fatal(err)
	}

	b := &RepoBackend{Dir: root}
	defer b.Close()
	// Spool được nạp trong lời gọi; gán phiếu cho S1 sau khi nạp.
	res, err := b.HistoryForPaths(ctx, mcpHistoryQuery("internal/db"))
	if err != nil || res.NoData || len(res.Runs) != 1 {
		t.Fatalf("chỉ lượt sửa: %+v %v", res, err)
	}
	c, err := OpenContext(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Store.DB.Exec(`UPDATE runs SET task_id = ? WHERE model = 'm1'`, tk.State.ID); err != nil {
		t.Fatal(err)
	}

	res, err = b.HistoryForPaths(ctx, mcpHistoryQuery("internal/db"))
	if err != nil || len(res.Runs) != 1 {
		t.Fatal(res, err)
	}
	r := res.Runs[0]
	if r.Model != "m1" || r.TaskID != tk.State.ID || r.Harness != "claude" || r.StartedAt == "" ||
		len(r.Files) != 1 || r.Files[0] != "internal/db/db.go" || len(r.FailedTests) != 1 || r.FailedTests[0] != "go test ./internal/db" {
		t.Fatalf("run = %+v", r)
	}
	if r.Label == "" {
		t.Fatalf("thiếu nhãn: %+v", r)
	}
	// Phát hiện lọc theo path: chỉ cái ở internal/db/db.go.
	if len(r.Findings) != 1 || r.Findings[0].ID != tk.State.ID+".1" || r.Findings[0].Severity != "high" || r.Findings[0].Outcome != "open" ||
		r.Findings[0].Title != "Open bỏ qua lỗi" || r.FindingsTotal != 1 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	// Đường dẫn tuyệt đối trong repository cũng được chấp nhận; đường dẫn khác thì không có lượt.
	if res, _ := b.HistoryForPaths(ctx, mcpHistoryQuery(root+"/internal/db/db.go")); len(res.Runs) != 1 {
		t.Fatalf("đường dẫn tuyệt đối: %+v", res)
	}
	if res, _ := b.HistoryForPaths(ctx, mcpHistoryQuery("README.md")); len(res.Runs) != 0 {
		t.Fatalf("README không có lượt: %+v", res)
	}
	// include_reads thêm lượt S2, mới nhất trước; limit.
	q := mcpHistoryQuery("internal/db/db.go")
	q.IncludeReads = true
	if res, _ = b.HistoryForPaths(ctx, q); len(res.Runs) != 2 || res.Runs[0].Model != "m2" {
		t.Fatalf("include_reads: %+v", res)
	}
	q.Limit = 1
	if res, _ = b.HistoryForPaths(ctx, q); len(res.Runs) != 1 {
		t.Fatalf("limit: %+v", res)
	}
	// Đường dẫn ngoài repository là lỗi.
	if _, err := b.HistoryForPaths(ctx, mcpHistoryQuery("../ngoai")); err == nil {
		t.Fatal("đường dẫn ngoài repository phải lỗi")
	}

	// run_get theo tiền tố.
	info, err := b.RunGet(ctx, r.RunID[:12])
	if err != nil {
		t.Fatal(err)
	}
	if info.RunID != r.RunID || info.TaskID != tk.State.ID || info.BaseCommit != "abc123" || info.Model != "m1" ||
		len(info.Modified) != 1 || len(info.Tests) != 1 || info.Tests[0].OK || info.Tests[0].Command != "go test ./internal/db" ||
		info.Label == "" || len(info.Findings) != 2 || info.FindingsTotal != 2 {
		t.Fatalf("run_get = %+v", info)
	}
	if _, err := b.RunGet(ctx, "ZZZZ"); !errors.Is(err, runlog.ErrRunNotFound) {
		t.Fatalf("không tồn tại: %v", err)
	}
	// Tiền tố mơ hồ: ULID của hai lượt cùng chung chữ đầu.
	if _, err := b.RunGet(ctx, ""); !errors.Is(err, runlog.ErrAmbiguous) {
		t.Fatalf("tiền tố rỗng khớp cả hai lượt: %v", err)
	}
}

func TestHistoryToolsNoStateDir(t *testing.T) {
	testutil.Isolate(t)
	for _, k := range agentEnvVars {
		t.Setenv(k, "")
	}
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	mustInit(t, InitOptions{Dir: root})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Mô phỏng máy chưa có thư mục trạng thái: ContextWS ở chế độ bộ nhớ.
	if err := os.RemoveAll(k.Paths.Root); err != nil {
		t.Skipf("không gỡ được thư mục trạng thái: %v", err)
	}
	b := &RepoBackend{Dir: root}
	defer b.Close()
	res, err := b.HistoryForPaths(ctx, mcpHistoryQuery("a.go"))
	if err != nil || !res.NoData {
		t.Fatalf("muốn NoData, được %+v %v", res, err)
	}
	if info, err := b.RunGet(ctx, "01"); err != nil || !info.NoData {
		t.Fatalf("run_get muốn NoData, được %+v %v", info, err)
	}
}

func mcpHistoryQuery(paths ...string) mcpserver.HistoryQuery {
	return mcpserver.HistoryQuery{Paths: paths}
}
