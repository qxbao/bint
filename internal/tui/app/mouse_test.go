package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
)

// cellOf trả toạ độ ô (x, y) của lần xuất hiện thứ nth (từ 0) của needle trong bản vẽ hiện tại; toạ độ lấy từ chính bản vẽ.
func cellOf(t *testing.T, m *Model, needle string, nth ...int) (int, int) {
	t.Helper()
	want := 0
	if len(nth) > 0 {
		want = nth[0]
	}
	for y, l := range lines(m.Render()) {
		for from := 0; ; {
			i := strings.Index(l[from:], needle)
			if i < 0 {
				break
			}
			if want == 0 {
				return ansi.StringWidth(l[:from+i]), y
			}
			want--
			from += i + len(needle)
		}
	}
	t.Fatalf("không thấy %q trên màn hình:\n%s", needle, plain(m.Render()))
	return 0, 0
}

// cellLeft trả ô của lần xuất hiện nằm sát bên trái nhất (khung danh sách) của needle.
func cellLeft(t *testing.T, m *Model, needle string) (int, int) {
	t.Helper()
	bx, by := -1, -1
	for y, l := range lines(m.Render()) {
		for from := 0; ; {
			i := strings.Index(l[from:], needle)
			if i < 0 {
				break
			}
			if x := ansi.StringWidth(l[:from+i]); bx < 0 || x < bx {
				bx, by = x, y
			}
			from += i + len(needle)
		}
	}
	if bx < 0 {
		t.Fatalf("không thấy %q trên màn hình:\n%s", needle, plain(m.Render()))
	}
	return bx, by
}

// cellLeftmostRight: ô của lần xuất hiện nằm sát bên phải nhất (khung chi tiết).
func cellLeftmostRight(t *testing.T, m *Model, needle string) (int, int) {
	t.Helper()
	bx, by := -1, -1
	for y, l := range lines(m.Render()) {
		for from := 0; ; {
			i := strings.Index(l[from:], needle)
			if i < 0 {
				break
			}
			if x := ansi.StringWidth(l[:from+i]); x > bx {
				bx, by = x, y
			}
			from += i + len(needle)
		}
	}
	if bx < 0 {
		t.Fatalf("không thấy %q", needle)
	}
	return bx, by
}

func clickAt(t *testing.T, m *Model, x, y int) {
	t.Helper()
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, m, cmd)
}

// clickNoDrive bấm mà không chạy lệnh kết quả (có việc nền thì jobs.Wait chặn đồng bộ: xem tri thức k:01M75FZMNEYM2X3WHNH2G7ZQA4).
func clickNoDrive(t *testing.T, m *Model, needle string) {
	t.Helper()
	x, y := cellOf(t, m, needle)
	m.Update(tea.MouseClickMsg{X: x + 1, Y: y, Button: tea.MouseLeft})
}

func clickText(t *testing.T, m *Model, needle string, nth ...int) {
	t.Helper()
	x, y := cellOf(t, m, needle, nth...)
	clickAt(t, m, x+1, y)
}

func wheelAt(t *testing.T, m *Model, x, y int, up bool, n int) {
	t.Helper()
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	for i := 0; i < n; i++ {
		_, cmd := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: b})
		drive(t, m, cmd)
	}
}

func mouseModel(t *testing.T, w, h int, keys ...string) *Model {
	t.Helper()
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), w, h)
	press(t, m, keys...)
	return m
}

// Chuột bật mặc định; không dấu vùng nào lọt ra đầu ra; bảng lệnh có app.mouse bật/tắt trong phiên.
func TestMouseOnByDefaultNoMarkersAndToggle(t *testing.T) {
	m := mouseModel(t, 100, 30)
	if mm := m.View().MouseMode; mm != tea.MouseModeCellMotion {
		t.Fatalf("chuột phải bật mặc định: %v", mm)
	}
	for _, keys := range [][]string{nil, {"2"}, {"3"}, {"4"}, {"5"}, {"6"}, {"7"}, {":"}, {"esc", "?"}} {
		press(t, m, keys...)
		if out := m.Render(); strings.Contains(out, "\x1b_") || strings.Contains(m.View().Content, "\x1b_") {
			t.Fatalf("Render còn dấu vùng chuột (màn %v)", m.Screen())
		}
	}
	press(t, m, "esc", "1")
	if _, ok := actions.Default().ByID("app.mouse"); !ok {
		t.Fatal("sổ hành động phải có app.mouse")
	}
	m.run("app.mouse")
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("app.mouse phải tắt chuột")
	}
	clickText(t, m, "Tri thức") // chuột tắt: bấm không làm gì
	if m.Screen() != actions.Overview {
		t.Fatal("chuột tắt thì bấm tab không được chuyển màn")
	}
	m.run("app.mouse")
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("app.mouse lần nữa phải bật lại chuột")
	}
	// Có trong bảng lệnh.
	press(t, m, ":")
	for _, c := range "chuột" {
		press(t, m, string(c))
	}
	hasAll(t, plain(m.Render()), "Bật/tắt chuột")
}

// Bấm thẻ ở thanh tab chuyển tab (mọi tab).
func TestMouseClickTabs(t *testing.T) {
	m := mouseModel(t, 140, 40)
	for _, s := range []struct {
		name string
		want actions.Screen
	}{{"Tri thức", actions.Knowledge}, {"Tác vụ", actions.Tasks}, {"Lượt chạy", actions.Runs}, {"Đánh giá", actions.Eval}, {"Tiến hoá", actions.Evolve},
		{"Hệ thống", actions.System}, {"Tổng quan", actions.Overview}} {
		x, y := cellOf(t, m, s.name)
		if y != 1 {
			t.Fatalf("%s phải ở thanh tab (dòng 1), thấy ở dòng %d", s.name, y)
		}
		clickAt(t, m, x, y)
		if m.Screen() != s.want {
			t.Fatalf("bấm %q: màn %s, muốn %s", s.name, m.Screen(), s.want)
		}
	}
	// Hẹp (tên rút gọn): vẫn bấm được thẻ đang hiển thị.
	n := mouseModel(t, 80, 24)
	t.Log(strings.Join(lines(n.Render())[:4], "\n"))
	x, y := cellOf(t, n, "LC")
	clickAt(t, n, x, y)
	if n.Screen() != actions.Runs {
		t.Fatalf("hẹp: bấm tab Lượt chạy → %s", n.Screen())
	}
}

// Tổng quan: bấm hàng hộp thư dời con trỏ; bấm lại hàng đang chọn làm như Enter (nhảy tới chỗ xử lý); bánh xe cuộn hộp thư.
func TestMouseOverviewInbox(t *testing.T) {
	m := mouseModel(t, 120, 40)
	rows := m.ov.Data.Inbox()
	if len(rows) < 2 {
		t.Fatalf("mẫu cần ≥ 2 hàng hộp thư: %d", len(rows))
	}
	if m.ov.Cursor != 0 {
		t.Fatal("con trỏ đầu phải ở hàng 0")
	}
	lines0 := lines(m.Render())
	var rowY []int
	for y, l := range lines0 {
		if strings.Contains(l, "→ ") && (strings.Contains(l, "▸") || strings.HasPrefix(strings.TrimLeft(l[strings.Index(l, "│")+3:], " "), "◌") || strings.Contains(l, "◌") || strings.Contains(l, "! ")) {
			rowY = append(rowY, y)
		}
	}
	if len(rowY) < 2 {
		t.Fatalf("không tìm thấy hàng hộp thư:\n%s", plain(m.Render()))
	}
	clickAt(t, m, 10, rowY[1])
	if m.ov.Cursor != 1 || m.Screen() != actions.Overview {
		t.Fatalf("bấm hàng 1: con trỏ %d, màn %s", m.ov.Cursor, m.Screen())
	}
	wheelAt(t, m, 10, rowY[0], true, 1)
	if m.ov.Cursor != 0 {
		t.Fatalf("lăn lên phải dời con trỏ: %d", m.ov.Cursor)
	}
	wheelAt(t, m, 10, rowY[0], false, 1)
	if m.ov.Cursor != 1 {
		t.Fatalf("lăn xuống phải dời con trỏ: %d", m.ov.Cursor)
	}
	want := m.ov.Data.Inbox()[1].Target
	clickAt(t, m, 10, rowY[1]) // đúng hàng đang chọn: như Enter
	if m.Screen() != want {
		t.Fatalf("bấm lại hàng đang chọn phải nhảy tới %s, đang ở %s", want, m.Screen())
	}
}

// Tri thức: bấm thẻ phụ chọn thẻ đó; bấm hàng dời con trỏ, bấm lại hàng đang chọn làm như Enter (focus khung chi tiết); bánh xe dời con trỏ
// ở khung danh sách.
func TestMouseKnowledge(t *testing.T) {
	m := mouseModel(t, 140, 40, "2")
	clickText(t, m, "Tìm kiếm")
	hasAll(t, plain(m.Render()), "Nhập truy vấn")
	clickText(t, m, "Danh sách")
	hasAll(t, plain(m.Render()), "Endpoint phải có /v1")
	first := m.kn.Cursor().ID
	clickText(t, m, "Handler đi qua")
	if c := m.kn.Cursor(); c == nil || c.Title != "Handler đi qua wrap()" {
		t.Fatalf("bấm hàng phải dời con trỏ: %+v", c)
	}
	if first == m.kn.Cursor().ID {
		t.Fatal("con trỏ không đổi")
	}
	clickText(t, m, "Handler đi qua") // bấm lại hàng đang chọn: như Enter → focus khung chi tiết
	cur := m.kn.Cursor().ID
	press(t, m, "j")
	if m.kn.Cursor().ID != cur {
		t.Fatal("sau Enter (bấm lại) focus ở khung chi tiết nên j cuộn chi tiết, không dời hàng")
	}
	// Bánh xe trên khung danh sách dời con trỏ.
	press(t, m, "tab") // về khung danh sách
	x, y := cellLeft(t, m, "Handler đi qua")
	before := m.kn.Cursor().ID
	wheelAt(t, m, x+1, y, false, 1)
	if m.kn.Cursor().ID == before {
		t.Fatal("lăn bánh xe trên danh sách phải dời con trỏ")
	}
	wheelAt(t, m, x+1, y, true, 3) // lăn lên quá đầu danh sách: con trỏ dừng ở hàng đầu
	if c := m.kn.Cursor(); c == nil || c.ID != first {
		t.Fatalf("lăn lên hết cỡ phải về hàng đầu: %+v", c)
	}
}

// Tác vụ: bấm thẻ con, hàng tác vụ (bỏ qua tiêu đề nhóm), bánh xe cuộn khung chi tiết dưới con trỏ.
func TestMouseTasks(t *testing.T) {
	m := mouseModel(t, 140, 22, "3")
	hasAll(t, plain(m.Render()), "Thêm màn Tác vụ")
	clickText(t, m, "Thêm màn Tác vụ")
	if c := m.tk.Cursor(); c == nil || c.Ticket.Goal != "Thêm màn Tác vụ" {
		t.Fatalf("bấm hàng tác vụ: %+v", m.tk.Cursor())
	}
	clickText(t, m, "Viết tài liệu cài đặt")
	if c := m.tk.Cursor(); c == nil || c.Ticket.Goal != "Viết tài liệu cài đặt" {
		t.Fatalf("bấm hàng tác vụ thứ hai: %+v", m.tk.Cursor())
	}
	before := m.tk.Cursor().State.ID
	clickText(t, m, "Chờ bạn") // tiêu đề nhóm: không phải hàng tác vụ
	if m.tk.Cursor().State.ID != before {
		t.Fatal("bấm tiêu đề nhóm không được dời con trỏ")
	}
	clickText(t, m, "Tài liệu")
	hasAll(t, plain(m.Render()), "Tài liệu kế")
	clickText(t, m, "Phiếu")
	out := plain(m.Render())
	if strings.Contains(out, "Tài liệu kế") {
		t.Fatalf("bấm thẻ Phiếu phải rời thẻ Tài liệu:\n%s", out)
	}
	// Bánh xe trên khung danh sách: dời con trỏ qua tác vụ kế.
	x, y := cellLeft(t, m, "Viết tài liệu cài đặt")
	id := m.tk.Cursor().State.ID
	wheelAt(t, m, x+1, y, true, 1)
	if m.tk.Cursor().State.ID == id {
		t.Fatal("lăn bánh xe trên danh sách tác vụ phải dời con trỏ")
	}
	// Bánh xe trên khung chi tiết: nội dung cuộn (dòng đầu biến mất).
	dx, dy := cellLeftmostRight(t, m, "Chi tiết")
	first := firstBodyLine(m, dx+3, dy+1)
	wheelAt(t, m, dx+3, dy+2, false, 4)
	if after := firstBodyLine(m, dx+3, dy+1); after == first {
		t.Fatalf("lăn bánh xe trên khung chi tiết phải cuộn nội dung (vẫn %q)", first)
	}
}

// firstBodyLine trả dòng chữ ở toạ độ (x, y) tới hết dòng.
func firstBodyLine(m *Model, x, y int) string {
	ls := lines(m.Render())
	if y >= len(ls) {
		return ""
	}
	return strings.TrimSpace(ansi.Cut(ls[y], x, 400))
}

// Hệ thống: bấm thẻ con và hàng (Cài đặt repo, Doctor), bánh xe cuộn.
func TestMouseSystem(t *testing.T) {
	m := mouseModel(t, 140, 40, "7")
	clickText(t, m, "Doctor")
	hasAll(t, plain(m.Render()), "Doctor")
	hasAll(t, plain(m.Render()), "Doctor: 1 cảnh báo, 1 lỗi", "✓ git  đạt")
	// Hàng: bấm hàng "hook claude" dời con trỏ (chi tiết đổi theo), vẫn ở thẻ Doctor.
	clickText(t, m, "hook claude")
	out := plain(m.Render())
	hasAll(t, out, "Doctor: 1 cảnh báo, 1 lỗi")
	if strings.Contains(out, "✓ git  đạt") {
		t.Fatalf("bấm hàng phải đổi khung chi tiết sang hàng đó:\n%s", out)
	}
	clickText(t, m, "Harness")
	hasAll(t, plain(m.Render()), "Harness")
	clickText(t, m, "Cài đặt")
	hasAll(t, plain(m.Render()), "Mẫu cấu hình")
	// Khung danh sách của Doctor: bánh xe dời con trỏ.
	clickText(t, m, "Doctor")
	x, y := cellLeft(t, m, "git")
	wheelAt(t, m, x+1, y, false, 1)
	if strings.Contains(plain(m.Render()), "✓ git  đạt") {
		t.Fatalf("bánh xe phải dời con trỏ khỏi hàng git:\n%s", plain(m.Render()))
	}
}

// Bấm một phím ở chân phím (khung ngoài và chân phím trong khung của thẻ phụ) chạy đúng việc như nhấn phím đó.
func TestMouseFooterHintsEqualKeys(t *testing.T) {
	cases := []struct {
		name  string
		keys  []string
		hint  string // chữ của phím ở chân phím
		press string
	}{
		{"khung ngoài ?", []string{"2"}, "[?→", "?"},
		{"khung ngoài :", []string{"2"}, "[:→", ":"},
		{"khung ngoài Enter ở Tri thức", []string{"2"}, "[Enter→", "enter"},
		{"khung ngoài r ở Tổng quan", nil, "[r→", "r"},
		{"chân phím trong khung: d ở thẻ Tài liệu", []string{"3", "."}, "d Tài liệu kế", "d"},
		{"chân phím trong khung: Esc ở thẻ phụ Tri thức", []string{"2", ".", "."}, "Esc Thoát", "esc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := mouseModel(t, 140, 40, tc.keys...)
			b := mouseModel(t, 140, 40, tc.keys...)
			x, y := cellOf(t, b, tc.hint)
			clickAt(t, b, x+1, y)
			press(t, a, tc.press)
			if pa, pb := plain(a.Render()), plain(b.Render()); pa != pb {
				t.Fatalf("bấm %q khác nhấn %q:\n--- bấm\n%s\n--- nhấn\n%s", tc.hint, tc.press, pb, pa)
			}
		})
	}
}

// Nút [Enter]/[Esc] của hộp thoại bấm được; bấm ngoài hộp không tác động màn bên dưới (hộp thoại việc nền, hộp của màn hình, bảng
// lệnh, trợ giúp).
func TestMouseDialogButtonsAndOutsideClicks(t *testing.T) {
	addJob := func(m *Model) *bool {
		cancelled := false
		m.jobs.Add(jobs.Job{Action: "x.y", Kind: jobs.KindAction, Label: "việc thử", Cancel: func() { cancelled = true }})
		return &cancelled
	}
	t.Run("hộp thoát: bấm ngoài không đổi tab, [Esc] đóng", func(t *testing.T) {
		m := mouseModel(t, 120, 40, "2")
		addJob(m)
		m.Update(keyMsg("q"))
		if m.exitDlg == nil {
			t.Fatal("phải mở hộp thoát khi còn việc nền")
		}
		clickNoDrive(t, m, "Tác vụ") // thanh tab nằm ngoài hộp
		if m.Screen() != actions.Knowledge || m.exitDlg == nil {
			t.Fatalf("bấm ngoài hộp không được tác động màn bên dưới: %s", m.Screen())
		}
		clickNoDrive(t, m, "[Esc]")
		if m.exitDlg != nil || m.exiting {
			t.Fatal("[Esc] phải đóng hộp, ở lại")
		}
	})
	t.Run("hộp thoát: [Enter] xác nhận huỷ việc rồi thoát", func(t *testing.T) {
		m := mouseModel(t, 120, 40)
		cancelled := addJob(m)
		m.Update(keyMsg("q"))
		clickNoDrive(t, m, "[Enter]")
		if !*cancelled || !m.exiting {
			t.Fatalf("[Enter] phải huỷ việc và thoát: cancelled=%v exiting=%v", *cancelled, m.exiting)
		}
	})
	t.Run("hộp của màn hình (duyệt tác vụ)", func(t *testing.T) {
		m := mouseModel(t, 140, 40, "3")
		m.Update(actions.ResultMsg{ID: "tasks.plan_approve", Value: bintapp.ApprovePlan{Task: m.tk.Cursor(), VerifyCommand: "go test ./...", Resumable: true}})
		hasAll(t, plain(m.Render()), "Duyệt tác vụ")
		cur := m.tk.Cursor().State.ID
		clickText(t, m, "Viết tài liệu cài đặt") // hàng danh sách bên dưới hộp
		clickText(t, m, "Lượt chạy")             // thanh tab
		if m.tk.Cursor().State.ID != cur || m.Screen() != actions.Tasks {
			t.Fatal("bấm ngoài hộp không được tác động màn bên dưới")
		}
		clickText(t, m, "[Esc]")
		if strings.Contains(plain(m.Render()), "Duyệt tác vụ") {
			t.Fatal("[Esc] phải đóng hộp duyệt")
		}
	})
	t.Run("bảng lệnh và trợ giúp", func(t *testing.T) {
		m := mouseModel(t, 120, 40)
		press(t, m, ":")
		clickText(t, m, "Tri thức")
		if m.pal == nil || m.Screen() != actions.Overview {
			t.Fatal("bấm ngoài bảng lệnh không được tác động")
		}
		for _, c := range "chuột" {
			press(t, m, string(c))
		}
		clickText(t, m, "Bật/tắt chuột") // bấm dòng bảng lệnh chạy hành động
		if m.pal != nil || !m.mouseOff {
			t.Fatalf("bấm dòng bảng lệnh phải chạy hành động: pal=%v mouseOff=%v", m.pal != nil, m.mouseOff)
		}
		m.run("app.mouse")
		press(t, m, "?")
		clickText(t, m, "Tri thức")
		if !m.showHelp || m.Screen() != actions.Overview {
			t.Fatal("bấm ngoài trợ giúp không được tác động")
		}
	})
}

// Thông báo dài bị rút gọn có dấu báo; phím ! (hoặc bấm vào dấu) mở hộp Thông báo hiện đủ mọi từ, của mô hình gốc và của một màn.
func TestNoticesFullTextOfRootAndScreenToasts(t *testing.T) {
	var words []string
	for i := 0; i < 50; i++ {
		words = append(words, fmt.Sprintf("tu%03d", i))
	}
	long := strings.Join(words, " ") // 300 ký tự
	check := func(t *testing.T, m *Model, wantOnScreen bool) {
		t.Helper()
		out := plain(m.Render())
		for i, l := range strings.Split(out, "\n") {
			if ansi.StringWidth(l) != 80 {
				t.Fatalf("dòng %d rộng %d ≠ 80", i, ansi.StringWidth(l))
			}
		}
		if !strings.Contains(out, "…") || !strings.Contains(out, "[!]") {
			t.Fatalf("thông báo bị rút gọn phải có dấu … và [!]:\n%s", out)
		}
		if strings.Contains(out, words[49]) {
			t.Fatalf("thông báo 300 ký tự không thể hiện đủ trong 80 cột:\n%s", out)
		}
		press(t, m, "!")
		box := plain(m.Render())
		for _, w := range words {
			if !strings.Contains(box, w) {
				t.Fatalf("hộp Thông báo thiếu %q:\n%s", w, box)
			}
		}
		hasAll(t, box, "Thông báo", "Hiện tại")
		press(t, m, "esc")
		if m.notes.dlg != nil {
			t.Fatal("Esc phải đóng hộp Thông báo")
		}
	}
	t.Run("mô hình gốc", func(t *testing.T) {
		m := mouseModel(t, 80, 24)
		m.toast = long
		check(t, m, true)
	})
	t.Run("bấm vào [!]", func(t *testing.T) {
		m := mouseModel(t, 80, 24)
		m.toast = long
		clickText(t, m, "[!]")
		hasAll(t, plain(m.Render()), "Thông báo", words[49])
	})
	t.Run("một màn hình (Hệ thống)", func(t *testing.T) {
		m := mouseModel(t, 80, 24, "7")
		m.Update(actions.ResultMsg{ID: "system.index_rebuild", Err: errors.New(long)})
		out := plain(m.Render())
		if !strings.Contains(out, "[!]") || strings.Contains(out, words[49]) {
			t.Fatalf("toast của màn hình phải bị rút gọn có dấu:\n%s", out)
		}
		check(t, m, true)
	})
	t.Run("lịch sử tối đa 50 thông báo của mọi màn", func(t *testing.T) {
		m := mouseModel(t, 100, 30)
		for i := 0; i < 60; i++ {
			m.toast = fmt.Sprintf("thongbao%03d", i)
			m.harvestNotices()
		}
		m.toast = ""
		m.harvestNotices()
		if len(m.notes.hist) != 50 {
			t.Fatalf("giữ tối đa 50 thông báo: %d", len(m.notes.hist))
		}
		press(t, m, "!")
		hasAll(t, plain(m.Render()), "Gần đây (50)", "thongbao059")
		if strings.Contains(plain(m.Render()), "thongbao000") {
			t.Fatal("thông báo cũ hơn 50 phải bị bỏ")
		}
	})
}

// Bấm "[!]" khi một form đang nhận phím vẫn mở hộp Thông báo (không gõ chữ "!" vào ô nhập).
func TestMouseNoticeMarkWhileFormCaptures(t *testing.T) {
	m := mouseModel(t, 80, 24, "3", "n")
	if !m.tk.Capturing() {
		t.Fatal("điều kiện thử: form tạo tác vụ phải đang mở")
	}
	_, long := longText("nf")
	m.toast = strings.Join(long, " ")
	clickText(t, m, "[!]")
	hasAll(t, plain(m.Render()), "Thông báo", long[len(long)-1])
	if m.notes.dlg == nil {
		t.Fatal("hộp Thông báo phải mở")
	}
}

// Lượt chạy › Bài học: bấm tiêu đề nhóm không đặt con trỏ lên tiêu đề (bàn phím luôn bỏ qua), bánh xe dời như phím
// (bỏ qua tiêu đề, đặt lại khung chi tiết). Review 01M764SBMNG91M0998BX0AWKN0.1.
func TestMouseRunsLessonsSkipGroupHeaders(t *testing.T) {
	m := mouseModel(t, 140, 40, "4", ".")
	if m.rn.LessonCursor() == nil {
		t.Fatal("con trỏ ban đầu phải ở bài học")
	}
	first := m.rn.LessonCursor().Title
	// Bấm tiêu đề nhóm "Riêng": con trỏ vẫn ở một bài học (không dời lên tiêu đề).
	clickText(t, m, "Riêng — internal/db/**")
	if m.rn.LessonCursor() == nil {
		t.Fatal("bấm tiêu đề nhóm đặt con trỏ lên tiêu đề")
	}
	if m.rn.LessonCursor().Title != first {
		t.Fatalf("bấm tiêu đề nhóm không được dời con trỏ: %q → %q", first, m.rn.LessonCursor().Title)
	}
	// Bánh xe xuống một nấc: sang bài học kế tiếp, bỏ qua tiêu đề nhóm, và khung chi tiết được đặt lại.
	x, y := cellLeft(t, m, "Gọi Reset trước Retry")
	wheelAt(t, m, x+1, y, false, 1)
	c := m.rn.LessonCursor()
	if c == nil || c.Title == first {
		t.Fatalf("bánh xe phải dời tới bài học kế tiếp, bỏ qua tiêu đề: %+v", c)
	}
	hasAll(t, plain(m.Render()), "Đóng kết nối sau truy vấn")
}

// Review 6a .3: toast thường của mô hình gốc (app.mouse bật/tắt) không bị ghi là lỗi trong hộp Thông báo; lỗi vẫn là lỗi.
func TestRootToastErrFlag(t *testing.T) {
	m := mouseModel(t, 100, 30)
	m.run("app.mouse")
	m.harvestNotices()
	if txt, isErr := m.currentNotice(); txt == "" || isErr {
		t.Fatalf("thông báo bật/tắt chuột không phải lỗi: %q err=%v", txt, isErr)
	}
	if n := m.notes.hist[len(m.notes.hist)-1]; n.err {
		t.Fatalf("lịch sử ghi thông báo thường là lỗi: %+v", n)
	}
	press(t, m, "!")
	if m.notes.dlg == nil {
		t.Fatal("! phải mở hộp Thông báo")
	}
	press(t, m, "esc")
	// Mô hình gốc báo lỗi (hành động bị khoá) thì vẫn là lỗi.
	m.setToast("bị khoá", true)
	if _, isErr := m.currentNotice(); !isErr {
		t.Fatal("toast lỗi phải mang cờ lỗi")
	}
}
