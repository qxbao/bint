package app

import (
	"context"
	"strings"
	"testing"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/spool"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

// waitText chờ tới khi màn hình có chuỗi sub (đọc kết quả của việc nền và dịch vụ thật).
func (f *fixture) waitText(sub string) {
	f.t.Helper()
	f.pump(func() bool { return strings.Contains(plain(f.m.Render()), sub) }, nil)
}

// spoolSession ghi các sự kiện của một phiên Claude Code vào spool, như hook làm (mục tri thức được truy xuất nhưng
// chưa từng được trích dẫn).
func (f *fixture) spoolSession(id string, at time.Time, rating string, modified []string, retrieved []string) {
	f.t.Helper()
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: id, Payload: p}
	}
	evs := []spool.Event{ev(spool.SessionStarted, at, map[string]any{"model": "claude-opus-5-5"})}
	for _, m := range modified {
		evs = append(evs, ev(spool.FileModified, at.Add(time.Minute), map[string]any{"path": m}))
	}
	ids := []any{}
	for _, r := range retrieved {
		ids = append(ids, r)
	}
	evs = append(evs, ev(spool.ContextRetrieved, at.Add(2*time.Minute), map[string]any{"channel": "hook:path", "ids": ids}),
		ev(spool.RunRated, at.Add(4*time.Minute), map[string]any{"rating": rating}),
		ev(spool.SessionEnded, at.Add(3*time.Minute), map[string]any{"reason": "exit"}))
	if _, err := spool.Write(f.ws.K.Paths.Spool, evs...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) itemStatus(id string) string {
	f.t.Helper()
	k, err := bintapp.OpenKnowledge(context.Background(), f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	items, _, err := k.Items()
	if err != nil {
		f.t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == id {
			return it.Status
		}
	}
	f.t.Fatalf("không thấy mục %s", id)
	return ""
}

// Mốc xong của giai đoạn 3 (docs/tui-plan.md mục 9): áp dụng và quay lui một đề xuất hoàn toàn trong TUI, chỉ bằng
// phím, với dịch vụ thật (không gọi mô hình). Theo kịch bản của TestEvolveEndToEnd (CLI): spool 3 phiên có nhãn và một
// mục được truy xuất nhưng chưa trích dẫn → cơ hội `unused` → đề xuất loại bỏ (deprecate) xác định → áp dụng (chưa kiểm
// chứng) → mục thành deprecated → quay lui → mục hết deprecated.
func TestMilestoneApplyRollbackProposal(t *testing.T) {
	f := newFixture(t, Options{})
	f.write("internal/api/h.go", "package api\n\nfunc Handle() {}\n")
	f.write("internal/core/x.go", "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n")
	testutil.Git(t, f.root, "add", "-A")
	testutil.Git(t, f.root, "commit", "-q", "-m", "thêm mã mẫu")
	added, err := bintapp.AddKnowledge(context.Background(), f.ws.K, bintapp.AddOptions{Type: knowledge.TypeWarning, Title: "Handle không được chặn",
		Body: "Handle chạy trên luồng chính.", Paths: []string{"internal/api/**"}, Evidence: []string{"internal/api/h.go:3"}})
	if err != nil || !added.Saved {
		t.Fatalf("thêm mục tri thức: %v %+v", err, added.Check)
	}
	item := added.Item.ID

	// 1. Lượt chạy: chưa có lượt nào; spool có 3 phiên → nạp spool (I) rồi quét Git (S) — chỉ bằng phím.
	f.press("4")
	f.pump(func() bool { return f.m.rn.Loaded }, nil)
	base := time.Now().Add(-6 * time.Hour)
	core := []string{"internal/core/x.go"}
	f.spoolSession("s1", base, "good", core, []string{item})
	f.spoolSession("s2", base.Add(time.Hour), "bad", core, []string{item})
	f.spoolSession("s3", base.Add(2*time.Hour), "bad", core, []string{item})
	f.press("I")
	f.waitText("Đã nạp 3 tệp")
	f.waitText("Lượt chạy 3/3")
	f.press("S", "ctrl+s")
	f.waitText("Đã quét 3 lượt")

	// 2. Tiến hoá › Cơ hội: quét (s) rồi chọn cơ hội `unused`.
	f.press("6")
	f.pump(func() bool { return f.m.ev.Loaded }, nil)
	f.press("s", "ctrl+s")
	f.waitText("Đã quét")
	for i := 0; i < 8 && (f.m.ev.CursorOpportunity() == nil || f.m.ev.CursorOpportunity().Kind != "unused"); i++ {
		f.press("j")
	}
	if op := f.m.ev.CursorOpportunity(); op == nil || op.Kind != "unused" || !op.Ready {
		t.Fatalf("phải có cơ hội unused sẵn sàng: %+v", op)
	}

	// 3. Đề xuất (p): form điền sẵn cơ hội; bật "Không dùng agent" nên không gọi mô hình; chạy nền có log.
	f.press("p")
	f.press("tab", "tab", " ") // ngân sách → không dùng agent: bật
	if out := plain(f.m.Render()); !strings.Contains(out, "bint evolve propose") || !strings.Contains(out, "--no-agent") {
		t.Fatalf("dòng lệnh tương đương phải khớp cờ:\n%s", out)
	}
	f.press("ctrl+s")
	f.waitJobsDone()
	f.waitText("Kết quả tạo đề xuất")
	out := plain(f.m.Render())
	if !strings.Contains(out, "+ ep_") || !strings.Contains(out, "deprecate:"+item) {
		t.Fatalf("kết quả đề xuất:\n%s", out)
	}
	f.press("esc") // đóng hộp kết quả

	// 4. Danh sách đề xuất: con trỏ ở đề xuất loại bỏ; thẻ Thay đổi nêu đúng mục.
	f.pump(func() bool {
		c := f.m.ev.Cursor()
		return f.m.ev.Tab() == 1 && c != nil && c.Proposal.Action == "deprecate"
	}, nil)
	ep := f.m.ev.Cursor().Proposal.ID
	f.press("v")
	f.waitText("[deprecate] " + item)

	// 5. Áp dụng (a): hộp đỏ vì chưa kiểm chứng; chọn các ô rồi xác nhận; mục thành deprecated.
	f.press("a")
	f.pump(func() bool { open, _, _ := f.m.ev.Dialog(); return open }, nil)
	_, danger, checks := f.m.ev.Dialog()
	if !danger || len(checks) == 0 {
		t.Fatalf("áp dụng chưa kiểm chứng phải là hộp đỏ có ô chọn: danger=%v %v", danger, checks)
	}
	if st := f.itemStatus(item); st == knowledge.StatusDeprecated {
		t.Fatal("xem hộp áp dụng không được ghi gì")
	}
	for i := range checks {
		f.press("x", "tab")
		_ = i
	}
	f.press("enter")
	f.waitText("Đã áp dụng " + ep)
	if st := f.itemStatus(item); st != knowledge.StatusDeprecated {
		t.Fatalf("mục phải thành deprecated sau khi áp dụng: %s", st)
	}
	f.press("esc")
	f.pump(func() bool { c := f.m.ev.Cursor(); return c != nil && c.State == "applied" }, nil)

	// 6. Quay lui (u): hộp liệt kê tệp, không xung đột; xác nhận; mục được khôi phục.
	f.press("u")
	f.pump(func() bool { open, _, _ := f.m.ev.Dialog(); return open }, nil)
	if open, danger, checks := f.m.ev.Dialog(); !open || danger || len(checks) != 0 {
		t.Fatalf("quay lui không xung đột là hộp thường: %v %v %v", open, danger, checks)
	}
	hasAll(t, plain(f.m.Render()), "Quay lui đề xuất "+ep, ".bint/knowledge/items/"+item+".md")
	f.press("enter")
	f.waitText("Đã quay lui " + ep)
	if st := f.itemStatus(item); st == knowledge.StatusDeprecated {
		t.Fatalf("mục phải hết deprecated sau khi quay lui: %s", st)
	}
	f.press("esc")
	// Lịch sử ghi cả hai lần.
	f.press(".")
	f.waitText("rollback")
	hasAll(t, plain(f.m.Render()), "apply", "rollback")
}
