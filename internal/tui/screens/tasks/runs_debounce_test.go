package tasks_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/tui/screens/tasks"
)

type runCall struct {
	id     string
	noScan bool
}

type runRecorder struct {
	mu    sync.Mutex
	calls []runCall
}

func (r *runRecorder) load(_ context.Context, _ string, id string, readOnly bool) (app.TaskRunsResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, runCall{id, readOnly})
	return app.TaskRunsResult{Runs: []app.TaskRunRow{{RunID: "01RUN000001", Workflow: "b-do", Status: "completed", Duration: "12s"}}}, nil
}

func (r *runRecorder) snapshot() []runCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runCall(nil), r.calls...)
}

func (r *runRecorder) reset() {
	r.mu.Lock()
	r.calls = nil
	r.mu.Unlock()
}

// Thẻ Phiếu: dời con trỏ liên tiếp qua 5 tác vụ chỉ nạp lượt chạy một lần (tác vụ cuối), không nạp spool/Reconcile.
func TestInfoTabRunsDebouncedAndLite(t *testing.T) {
	rec := &runRecorder{}
	r := newRig(t, tasks.Options{Runs: rec.load, RunsDebounce: 30 * time.Millisecond})
	r.press("a") // hiện cả tác vụ đã xong để có ≥ 6 hàng
	time.Sleep(60 * time.Millisecond)
	r.feed(r.m.Update(tasks.RefreshMsg{}))
	rec.reset()
	var cmds []tea.Cmd
	for i := 0; i < 5; i++ {
		cmd, _ := r.m.Key(key("j"))
		cmds = append(cmds, cmd)
	}
	last := r.m.Cursor().State.ID
	for _, c := range cmds {
		r.feed(c)
	}
	calls := rec.snapshot()
	if len(calls) != 1 || calls[0].id != last || !calls[0].noScan {
		t.Fatalf("muốn đúng 1 lần nạp nhẹ cho %s, có %+v", last, calls)
	}
}

// Thẻ Lượt chạy vẫn nạp đầy đủ (có spool) ngoài chế độ chỉ đọc, kể cả khi khối nổi bật đã nạp nhẹ trước đó.
func TestRunsTabStillLoadsFull(t *testing.T) {
	rec := &runRecorder{}
	r := newRig(t, tasks.Options{Runs: rec.load})
	time.Sleep(20 * time.Millisecond)
	id := r.m.Cursor().State.ID
	r.feed(r.m.Update(tasks.RefreshMsg{}))
	r.press(",") // Phiếu → Lượt chạy
	var full bool
	for _, c := range rec.snapshot() {
		if c.id == id && !c.noScan {
			full = true
		}
	}
	if !full {
		t.Fatalf("thẻ Lượt chạy phải nạp đầy đủ (readOnly=false): %+v", rec.snapshot())
	}
}

// Click của màn Tác vụ chỉ có một khối kiểm hộp xác nhận và một khối kiểm biểu mẫu (không lặp mã chết).
func TestPointerClickHasSingleDialogAndFormBlock(t *testing.T) {
	src, err := os.ReadFile("pointer.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	click := s[strings.Index(s, "func (m *Model) Click("):strings.Index(s, "func (m *Model) Wheel(")]
	if n := strings.Count(click, "if m.dlg != nil {"); n != 1 {
		t.Fatalf("khối m.dlg lặp %d lần", n)
	}
	if n := strings.Count(click, "if m.frm != nil {"); n != 1 {
		t.Fatalf("khối m.frm lặp %d lần", n)
	}
}
