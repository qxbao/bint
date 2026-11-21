package app

import (
	"strings"
	"testing"

	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// Ctrl+F mở hộp tìm mọi nơi từ bất kỳ màn nào; tìm được cả tri thức không còn hiệu lực và tác vụ, Enter chuyển tới đúng mục.
func TestFindAnywhereJumpsToItem(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 140, 40)
	press(t, m, "4", "ctrl+f")
	if m.pal == nil || !m.palFind {
		t.Fatal("Ctrl+F phải mở hộp tìm")
	}
	if out := plain(m.Render()); !strings.Contains(out, "Tìm mọi nơi") {
		t.Fatalf("thiếu tiêu đề hộp tìm:\n%s", out)
	}
	it := m.kn.Data.Items[len(m.kn.Data.Items)-1] // mục cuối của dữ liệu mẫu (có thể đã loại bỏ)
	for _, r := range it.ID {
		press(t, m, string(r))
	}
	if ms := m.pal.Matches(); len(ms) == 0 || ms[0].Item.ID != findKnowledge+it.ID {
		t.Fatalf("tìm theo ID %s: %+v", it.ID, ms)
	}
	press(t, m, "enter")
	if m.Screen() != actions.Knowledge || m.kn.Cursor() == nil || m.kn.Cursor().ID != it.ID {
		t.Fatalf("Enter phải tới mục %s ở Tri thức: màn %s", it.ID, m.Screen())
	}
}
