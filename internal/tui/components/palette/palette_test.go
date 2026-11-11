package palette

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func items() []Item {
	return []Item{
		{ID: "knowledge.approve", Title: "Duyệt mục tri thức", Group: "Tri thức", Keywords: "bint knowledge approve --refresh --reason", Key: "a"},
		{ID: "eval.run", Title: "Chạy đánh giá so sánh", Group: "Đánh giá", Keywords: "bint eval run --budget --repeats --arms", Key: ""},
		{ID: "evolve.apply", Title: "Áp dụng đề xuất tiến hoá", Group: "Tiến hoá", Keywords: "bint evolve apply --unverified --confirm-risk", Locked: "chỉ người dùng"},
		{ID: "app.quit", Title: "Thoát", Key: "q"},
		{ID: "app.language", Title: "Đổi ngôn ngữ", Key: "L"},
	}
}

func ids(m Model) []string {
	var out []string
	for _, x := range m.Matches() {
		out = append(out, x.Item.ID)
	}
	return out
}

func TestEmptyQueryListsAllInOrder(t *testing.T) {
	m := New(items())
	if got := strings.Join(ids(m), ","); got != "knowledge.approve,eval.run,evolve.apply,app.quit,app.language" {
		t.Fatal(got)
	}
}

// Tìm không phân biệt hoa/thường và dấu tiếng Việt; khớp theo dãy con.
func TestFuzzyIgnoresCaseAndDiacritics(t *testing.T) {
	for _, q := range []string{"duyet", "DUYỆT", "dm tt", "tri thuc"} {
		m := New(items())
		m.Type(q)
		if len(m.Matches()) == 0 || m.Matches()[0].Item.ID != "knowledge.approve" {
			t.Errorf("%q → %v", q, ids(m))
		}
	}
	m := New(items())
	m.Type("doi ngon")
	if got := ids(m); len(got) == 0 || got[0] != "app.language" {
		t.Fatalf("%v", got)
	}
}

// Tìm theo tên cờ cũng ra hành động (bảo đảm hành động hiếm dùng vẫn tìm được).
func TestSearchByFlagAndCLIPath(t *testing.T) {
	m := New(items())
	m.Type("--budget")
	if got := ids(m); len(got) != 1 || got[0] != "eval.run" {
		t.Fatalf("--budget → %v", got)
	}
	m = New(items())
	m.Type("confirm-risk")
	if got := ids(m); len(got) != 1 || got[0] != "evolve.apply" {
		t.Fatalf("confirm-risk → %v", got)
	}
	m = New(items())
	m.Type("bint knowledge")
	if got := ids(m); len(got) == 0 || got[0] != "knowledge.approve" {
		t.Fatalf("bint knowledge → %v", got)
	}
}

func TestNoMatch(t *testing.T) {
	m := New(items())
	m.Type("zzzz")
	if len(m.Matches()) != 0 {
		t.Fatal(ids(m))
	}
	if _, ok := m.Selected(); ok {
		t.Fatal("không có dòng nào để chọn")
	}
	m.Clear()
	if len(m.Matches()) != 5 {
		t.Fatal("Clear phải trả mọi dòng")
	}
}

func TestScoreRanking(t *testing.T) {
	// Khớp liên tiếp, đầu từ thắng khớp rải rác.
	a, _ := Score("thoat", "Thoát")
	b, _ := Score("thoat", "Tìm hộp ốc áo trên")
	if a <= b {
		t.Fatalf("liên tiếp %d phải hơn rải rác %d", a, b)
	}
	if _, ok := Score("xyz", "Thoát"); ok {
		t.Fatal("không khớp")
	}
	if s, ok := Score("", "bất kỳ"); !ok || s != 0 {
		t.Fatal("query rỗng khớp mọi thứ")
	}
}

func TestCursorWrapsAndBackspace(t *testing.T) {
	m := New(items())
	m.Move(-1)
	if it, _ := m.Selected(); it.ID != "app.language" {
		t.Fatalf("vòng ngược: %s", it.ID)
	}
	m.Move(1)
	if it, _ := m.Selected(); it.ID != "knowledge.approve" {
		t.Fatalf("vòng xuôi: %s", it.ID)
	}
	m.Type("thoat")
	m.Backspace()
	m.Backspace()
	if m.Query != "tho" {
		t.Fatal(m.Query)
	}
}

func TestViewFitsAndMarksLocked(t *testing.T) {
	th := theme.New(theme.Mono)
	m := New(items())
	for _, w := range []int{30, 60, 100} {
		out := m.View(th, w, 12, "Bảng lệnh", "Không có kết quả")
		for i, l := range strings.Split(out, "\n") {
			if text.Width(l) != w {
				t.Fatalf("w=%d dòng %d rộng %d", w, i, text.Width(l))
			}
		}
	}
	out := ansi.Strip(m.View(th, 80, 12, "Bảng lệnh", "x"))
	if !strings.Contains(out, "chỉ người dùng") || !strings.Contains(out, "▸ ") {
		t.Fatalf("thiếu nhãn khoá hoặc con trỏ:\n%s", out)
	}
	m.Type("zzzz")
	if out := ansi.Strip(m.View(th, 80, 8, "Bảng lệnh", "Không có kết quả")); !strings.Contains(out, "Không có kết quả") {
		t.Fatalf("thiếu thông báo rỗng:\n%s", out)
	}
}
