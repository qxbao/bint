package app

import (
	"regexp"
	"strings"
	"testing"

	"bint/internal/tui/components/footer"
)

// fkv là một phím kèm nhãn như hiện ở chân phím trong khung ("a duyệt").
type fkv struct{ Key, Label string }

func (p fkv) String() string { return p.Key + " " + p.Label }

var (
	innerFooterRE = regexp.MustCompile(`└─ (.+?) ─*┘`)
	gapRE         = regexp.MustCompile(`\s{2,}`)
)

// innerFooterPairs trả các cặp (phím, nhãn) ở chân phím trong khung (viền dưới dạng "└─ a duyệt  x loại ──┘") của bản vẽ.
func innerFooterPairs(out string) []fkv {
	var res []fkv
	for _, l := range strings.Split(plain(out), "\n") {
		for _, m := range innerFooterRE.FindAllStringSubmatch(l, -1) {
			for _, part := range gapRE.Split(strings.TrimSpace(m[1]), -1) {
				if f := strings.Fields(part); len(f) > 0 {
					res = append(res, fkv{f[0], strings.Join(f[1:], " ")})
				}
			}
		}
	}
	return res
}

// collectHints trả chân phím trong khung (kèm ActionID) mà màn hình hiện tại dựng khi vẽ, đã lọc theo chế độ của mô hình (ui.Env.Allowed).
func collectHints(m *Model) []footer.Hint {
	var hs []footer.Hint
	env := m.newEnv(m.w, m.h)
	env.Collect = func(h []footer.Hint) { hs = append(hs, h...) }
	_ = m.screenBody(env)
	return hs
}

// writeHintPairs: các cặp (phím, nhãn) của hint có ActionID trỏ tới hành động Writes trong sổ của mô hình.
func writeHintPairs(m *Model, hs []footer.Hint) []fkv {
	var res []fkv
	for _, h := range hs {
		if a, ok := m.reg.ByID(h.ActionID); ok && a.Writes {
			res = append(res, fkv{h.Key, h.Label})
		}
	}
	return res
}

// missingPairs trả các cặp want không có trong have (so cả phím lẫn nhãn).
func missingPairs(have, want []fkv) []fkv {
	set := map[fkv]bool{}
	for _, p := range have {
		set[p] = true
	}
	var miss []fkv
	for _, p := range want {
		if !set[p] {
			miss = append(miss, p)
		}
	}
	return miss
}

// leakedPairs trả các cặp trong writes mà have vẫn còn (phím ghi lọt vào nơi không được có).
func leakedPairs(have, writes []fkv) []fkv {
	set := map[fkv]bool{}
	for _, p := range have {
		set[p] = true
	}
	var leaked []fkv
	for _, p := range writes {
		if set[p] {
			leaked = append(leaked, p)
		}
	}
	return leaked
}

// helpBoxLines trả các dòng nội dung của hộp trợ giúp đang hiển thị (hộp vẽ giữa khung màn hình nên dòng nội dung có bốn dấu "│": hai của khung
// ngoài, hai của hộp); khoảng trắng được gộp. Dòng trống giữ lại để phân nhóm.
func helpBoxLines(out string) []string {
	var res []string
	for _, l := range strings.Split(plain(out), "\n") {
		parts := strings.Split(l, "│")
		if len(parts) != 5 {
			continue
		}
		res = append(res, strings.Join(strings.Fields(parts[2]), " "))
	}
	return res
}

// helpGroupLines đọc nhóm bắt đầu bằng dòng tiêu đề title trong các dòng của hộp trợ giúp, tới dòng trống kế tiếp (gồm đoạn giới thiệu và các hàng
// "phím nhãn"). ok=false nếu không thấy tiêu đề hoặc nhóm chưa kết thúc trong phần đang hiển thị.
func helpGroupLines(lines []string, title string) (group []string, ok bool) {
	for i, l := range lines {
		if l != title {
			continue
		}
		for _, g := range lines[i+1:] {
			if g == "" {
				return group, true
			}
			group = append(group, g)
		}
		return group, false
	}
	return nil, false
}

// pairsInGroup trả những cặp trong cands có dòng khớp đúng "phím nhãn" trong group.
func pairsInGroup(group []string, cands []fkv) []fkv {
	set := map[string]bool{}
	for _, g := range group {
		set[g] = true
	}
	var res []fkv
	for _, p := range cands {
		if set[strings.Join(strings.Fields(p.String()), " ")] {
			res = append(res, p)
		}
	}
	return res
}

// Ca ngược của các hàm khẳng định: chúng phải đỏ khi thiếu phím hoặc khi phím ghi lọt vào.
func TestFooterAssertHelpersCatchGaps(t *testing.T) {
	drawn := []fkv{{"a", "duyệt"}, {"x", "loại"}, {"A", "gồm đã loại"}}
	write := []fkv{{"a", "duyệt"}, {"x", "loại"}}
	group := []string{"a duyệt", "x loại", "A gồm đã loại", "Enter mở"}
	if m := missingPairs(pairsInGroup(group, drawn), drawn); len(m) != 0 {
		t.Fatalf("nhóm đủ phím mà báo thiếu: %v", m)
	}
	// Thiếu một phím → phải bị bắt.
	if m := missingPairs(pairsInGroup([]string{"a duyệt", "A gồm đã loại"}, drawn), drawn); len(m) != 1 || m[0].Key != "x" {
		t.Fatalf("bỏ phím x mà không bị bắt: %v", m)
	}
	// Đúng phím nhưng sai nhãn → coi là thiếu (không chỉ so chữ cái của phím).
	if m := missingPairs(pairsInGroup([]string{"a xoá", "x loại", "A gồm đã loại"}, drawn), drawn); len(m) != 1 || m[0].Key != "a" {
		t.Fatalf("sai nhãn mà không bị bắt: %v", m)
	}
	// Phím ghi lọt vào nhóm → bị bắt; không lọt thì không.
	if l := leakedPairs(pairsInGroup(group, write), write); len(l) != 2 {
		t.Fatalf("phím ghi lọt vào mà không bị bắt: %v", l)
	}
	if l := leakedPairs(pairsInGroup([]string{"A gồm đã loại", "Enter mở"}, write), write); len(l) != 0 {
		t.Fatalf("không có phím ghi mà báo lọt: %v", l)
	}
	// Đọc nhóm theo tiêu đề: dừng ở dòng trống, báo chưa kết thúc nếu hết trang.
	lines := []string{"Đánh giá › Ứng viên", "mô tả", "a duyệt", "", "Đánh giá", "m khai thác"}
	if g, ok := helpGroupLines(lines, "Đánh giá › Ứng viên"); !ok || len(g) != 2 {
		t.Fatalf("đọc nhóm: %v %v", g, ok)
	}
	if _, ok := helpGroupLines(lines, "Đánh giá"); ok {
		t.Fatal("nhóm chưa kết thúc trong trang phải báo ok=false")
	}
	// Khung vẽ: tách cặp phím + nhãn.
	got := innerFooterPairs("│ └─ a duyệt  x loại  A gồm đã loại ──┘└─ e sửa  Enter đổi khung ──┘ │")
	if len(got) != 5 || got[2] != (fkv{"A", "gồm đã loại"}) || got[4] != (fkv{"Enter", "đổi khung"}) {
		t.Fatalf("tách chân phím trong khung: %v", got)
	}
}
