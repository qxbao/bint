package overview

import (
	"reflect"
	"testing"

	"bint/internal/i18n"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func TestRunParts(t *testing.T) {
	env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi")}
	for _, c := range []struct {
		name string
		runs map[string]int
		want []string
	}{
		{"rỗng", nil, nil},
		{"số 0 và âm bị bỏ", map[string]int{"completed": 0, "failed": -1, "running": 2}, []string{"2 đang chạy"}},
		{"thứ tự cố định", map[string]int{"running": 1, "interrupted": 2, "cancelled": 3, "failed": 4, "completed": 5},
			[]string{"5 xong", "4 lỗi", "3 đã huỷ", "2 bị ngắt", "1 đang chạy"}},
		{"trạng thái lạ hiện nguyên dạng, xếp sau theo tên", map[string]int{"zeta": 1, "queued": 7, "completed": 2, "alpha": 0},
			[]string{"2 xong", "7 queued", "1 zeta"}},
	} {
		if got := runParts(env, c.runs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: runParts = %q, muốn %q", c.name, got, c.want)
		}
	}
}

func TestCenterVertically(t *testing.T) {
	lines := []string{"a", "b", "c"}
	for _, c := range []struct {
		name    string
		h       int
		wantPad int
	}{
		{"spare 0", 3, 0},
		{"spare 3 (dưới ngưỡng) giữ sát trên", 6, 0},
		{"spare 4 (đúng ngưỡng) đệm 2", 7, 2},
		{"spare 5 đệm 2", 8, 2},
		{"spare lớn", 23, 10},
	} {
		got := centerVertically(lines, c.h)
		if len(got) != len(lines)+c.wantPad {
			t.Errorf("%s: %d dòng, muốn %d", c.name, len(got), len(lines)+c.wantPad)
			continue
		}
		for i := 0; i < c.wantPad; i++ {
			if got[i] != "" {
				t.Errorf("%s: dòng đệm %d không rỗng: %q", c.name, i, got[i])
			}
		}
		if !reflect.DeepEqual(got[c.wantPad:], lines) {
			t.Errorf("%s: nội dung đổi: %q", c.name, got)
		}
	}
	// nội dung dài hơn h: không đệm, không cắt
	if got := centerVertically(lines, 2); len(got) != 3 {
		t.Errorf("h nhỏ hơn nội dung: %d dòng", len(got))
	}
}
