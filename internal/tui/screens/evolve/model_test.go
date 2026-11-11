package evolve

import (
	"context"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
)

// Focus khi đang nạp không được chạy lần nạp thứ hai song song: kết quả cũ (chỉ đề xuất đang chờ) về sau không
// được ghi đè kết quả có mọi trạng thái, và con trỏ vẫn tới đúng đề xuất cần mở.
func TestFocusWhileLoadingDoesNotRaceStaleLoad(t *testing.T) {
	var mu sync.Mutex
	var queries []Query
	pending := evolvesvc.ListRow{Proposal: &evolve.Proposal{ID: "ep_pending"}}
	applied := evolvesvc.ListRow{Proposal: &evolve.Proposal{ID: "ep_applied"}}
	m := New(Options{Loader: func(_ context.Context, _ string, q Query, _ bool) (Data, error) {
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		if q.All {
			return Data{Rows: []evolvesvc.ListRow{pending, applied}}, nil
		}
		return Data{Rows: []evolvesvc.ListRow{pending}}, nil
	}, Detailer: func(context.Context, string, string, bool) (*evolvesvc.ShowResult, error) { return nil, nil }})

	// Lần nạp đầu (chỉ đang chờ) xong.
	m.Update(m.Ensure()())
	// Lần nạp thứ hai (Ctrl+R) đang chạy, với Query{All: false}.
	stale := m.Load()
	if stale == nil || !m.Loading {
		t.Fatal("lần nạp thứ hai phải bắt đầu")
	}
	// Mở đề xuất đã áp dụng từ hộp thư trong lúc đang nạp: chỉ hẹn nạp lại, không chạy thêm lần nạp song song.
	if cmd := m.Focus("ep_applied"); cmd != nil {
		t.Fatal("Focus khi đang nạp không được khởi chạy lần nạp song song")
	}
	// Kết quả cũ về: màn hình tự nạp lại với mọi trạng thái.
	next := m.Update(stale())
	for range 5 {
		if m.Cursor() != nil && m.Cursor().Proposal.ID == "ep_applied" {
			break
		}
		if next == nil {
			break
		}
		next = runLoads(m, next)
	}
	if c := m.Cursor(); c == nil || c.Proposal.ID != "ep_applied" || len(m.Data.Rows) != 2 {
		t.Fatalf("phải nạp lại với mọi trạng thái và chọn ep_applied: %+v (rows=%d)", c, len(m.Data.Rows))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 3 || queries[1].All || !queries[2].All {
		t.Fatalf("thứ tự nạp: %+v", queries)
	}
}

// runLoads chạy lệnh (kể cả lô) và đưa mọi LoadedMsg về Update; trả lệnh kế tiếp từ các LoadedMsg đó.
func runLoads(m *Model, cmd tea.Cmd) tea.Cmd {
	var next []tea.Cmd
	var walk func(tea.Msg)
	walk = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					walk(c())
				}
			}
		case LoadedMsg:
			next = append(next, m.Update(v))
		}
	}
	walk(cmd())
	return tea.Batch(next...)
}
