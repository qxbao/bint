package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Review .24: cảnh báo sổ phiếu .bint/tasks (lỗi ghi, xung đột, chờ nhập) không bị nuốt trong TUI: mọi cảnh báo vào
// lịch sử Thông báo dưới dạng lỗi, cảnh báo cuối hiện ở thanh trên.
func TestLedgerWarningsShownAsNotices(t *testing.T) {
	m := mouseModel(t, 140, 40)
	var pending []string
	m.opts.LedgerWarnings = func() []string { out := pending; pending = nil; return out }

	pending = []string{"phiếu A: .bint/tasks và bản cục bộ đều đổi — giữ bản cục bộ", "không ghi được .bint/tasks của phiếu B: đĩa đầy"}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	txt, isErr := m.currentNotice()
	if !isErr || !strings.Contains(txt, "đĩa đầy") {
		t.Fatalf("cảnh báo cuối phải hiện như lỗi: %q err=%v", txt, isErr)
	}
	var got []string
	for _, n := range m.notes.hist {
		if n.src == ledgerNoticeSrc {
			if !n.err {
				t.Fatalf("cảnh báo sổ phiếu phải là lỗi: %+v", n)
			}
			got = append(got, n.text)
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], "phiếu A") {
		t.Fatalf("mọi cảnh báo phải vào lịch sử Thông báo đúng một lần: %q", got)
	}
	total := len(m.notes.hist)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40}) // không có cảnh báo mới: không ghi thêm
	if len(m.notes.hist) != total {
		t.Fatalf("không có cảnh báo mới thì lịch sử không đổi: %d → %d", total, len(m.notes.hist))
	}
	press(t, m, "!")
	hasAll(t, plain(m.Render()), "Thông báo", "đĩa đầy")
}
