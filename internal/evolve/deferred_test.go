package evolve

import (
	"context"
	"strings"
	"testing"
)

func TestDeferredQueueLockAndStale(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	rt := f.k.Paths.Runtime
	scan := ScanResult{Opportunities: []Opportunity{
		{ID: "op-1", Key: "exploration:internal/core", Kind: KindExploration, Action: ActionCreate, Generator: GenAgent, Ready: true, SupportingRuns: []string{"R1"}},
		{ID: "op-2", Key: "blocked:x", Kind: KindUnused, Ready: false, Blocked: "chưa đủ"},
	}}
	added, err := Enqueue(rt, scan, now)
	if err != nil || len(added) != 1 || added[0].Level != LevelL1 || added[0].Status != ReqPending {
		t.Fatalf("enqueue: %v %+v", err, added)
	}
	// Không đánh dấu lặp khi đã có yêu cầu đang chờ cho cùng khoá.
	if again, _ := Enqueue(rt, scan, now); len(again) != 0 {
		t.Fatalf("đánh dấu lặp: %+v", again)
	}

	in := ProcessInput{K: f.k, DB: f.db, Scan: scan, History: &History{ByID: map[string]*Run{}}, Now: now}
	// Khoá tiến hoá đang bị giữ: không xử lý gì, yêu cầu vẫn chờ.
	lock, err := AcquireLock(rt, "test")
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ProcessRequests(ctx, in)
	if err != nil || rep.Locked == "" || len(rep.Processed) != 0 || len(rep.Waiting) != 1 {
		t.Fatalf("khoá: %v %+v", err, rep)
	}
	lock.Release()

	// Cần agent mà không có agent/ngân sách: chờ, không tốn gì.
	rep, err = ProcessRequests(ctx, in)
	if err != nil || len(rep.Processed) != 0 || len(rep.Waiting) != 1 || !strings.Contains(rep.Waiting[0].Note, "agent evolver") {
		t.Fatalf("thiếu agent: %v %+v", err, rep)
	}

	// Cơ hội không còn sẵn sàng → yêu cầu bị bỏ qua có lý do, không chạy.
	in.Scan = ScanResult{}
	rep, err = ProcessRequests(ctx, in)
	if err != nil || len(rep.Processed) != 1 || rep.Processed[0].Status != ReqSkipped || !strings.Contains(rep.Processed[0].Note, "không còn sẵn sàng") {
		t.Fatalf("lỗi thời: %v %+v", err, rep)
	}
	if p, _ := ListRequests(rt, ReqPending); len(p) != 0 {
		t.Fatalf("còn yêu cầu chờ: %+v", p)
	}
	all, _ := ListRequests(rt)
	if len(all) != 1 || all[0].Status != ReqSkipped || all[0].ProcessedAt == "" {
		t.Fatalf("lịch sử yêu cầu: %+v", all)
	}
}
