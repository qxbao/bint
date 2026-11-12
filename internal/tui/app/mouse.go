package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/palette"
	"bint/internal/tui/components/tabs"
	"bint/internal/tui/components/zone"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Chuột (docs/tui-plan.md mục 4.3.7): bật mặc định (View.MouseMode), mọi thứ vẫn dùng được chỉ bằng bàn phím.
//
// Cách làm: component và màn hình bọc phần tử bấm được bằng zone.Mark khi vẽ; Render gỡ dấu khỏi đầu ra và giữ danh sách vùng
// (theo toạ độ ô). Sự kiện chuột tra vùng dưới con trỏ rồi đổi thành việc tương đương:
//
//	tab:<i>   chuyển tab            key:<phím>  như nhấn phím đó (chân phím, nút [Enter]/[Esc] của hộp, thông báo "[!]")
//	r:<i>     hàng danh sách        t:<i>       thẻ phụ           p:<khung>  khung (focus khi bấm, cuộn khi lăn bánh xe)
//	pal:<i>   dòng bảng lệnh        dlg         vùng của hộp thoại (bấm ngoài hộp không tác động màn bên dưới)

// wheelStep là số dòng mỗi nấc bánh xe.
const wheelStep = 3

// filterModal: khi có hộp thoại/bảng phủ (vùng "dlg") thì chỉ giữ vùng nằm trong khung chữ nhật của hộp, nên bấm ngoài hộp
// không chạm màn bên dưới.
func (m *Model) filterModal(zs []zone.Zone) []zone.Zone {
	x0, x1, y0, y1, found := 0, 0, 0, 0, false
	for _, z := range zs {
		if z.ID != confirm.ModalZone {
			continue
		}
		if !found {
			x0, x1, y0, y1, found = z.X0, z.X1, z.Y, z.Y, true
			continue
		}
		x0, x1, y0, y1 = min(x0, z.X0), max(x1, z.X1), min(y0, z.Y), max(y1, z.Y)
	}
	if !found {
		return zs
	}
	var out []zone.Zone
	for _, z := range zs {
		if z.Y >= y0 && z.Y <= y1 && z.X0 >= x0 && z.X1 <= x1 {
			out = append(out, z)
		}
	}
	return out
}

// markModal bọc từng dòng của hộp phủ (bảng lệnh, trợ giúp) bằng vùng "dlg".
func markModal(box []string) []string {
	out := make([]string, len(box))
	for i, l := range box {
		out[i] = zone.Mark(confirm.ModalZone, l)
	}
	return out
}

func (m *Model) modalOpen() bool {
	for _, z := range m.zones {
		if z.ID == confirm.ModalZone {
			return true
		}
	}
	return false
}

// zoneAt trả id vùng hẹp nhất dưới ô (x, y), bỏ qua "dlg" và khung "p:"; không có thì thử khung "p:".
func (m *Model) zoneAt(x, y int) (string, bool) {
	z, ok := zone.At(m.zones, x, y, func(id string) bool { return id != confirm.ModalZone && !strings.HasPrefix(id, "p:") })
	if !ok {
		z, ok = zone.At(m.zones, x, y, func(id string) bool { return strings.HasPrefix(id, "p:") })
	}
	return z.ID, ok
}

// click xử lý bấm trái. Vẽ lại trước để vùng khớp trạng thái hiện tại (một lần mỗi lần bấm, rẻ).
func (m *Model) click(mo tea.Mouse) (tea.Model, tea.Cmd) {
	if m.mouseOff || mo.Button != tea.MouseLeft || m.w < MinW || m.h < MinH {
		return m, nil
	}
	m.Render()
	id, ok := m.zoneAt(mo.X, mo.Y)
	if !ok {
		return m, nil // ngoài mọi vùng (kể cả bấm ngoài hộp thoại): không làm gì
	}
	if id != "key:!" { // "[!]" mở hộp Thông báo: giữ thông báo hiện tại để hộp hiện đủ
		m.setToast("", false)
	}
	switch {
	case id == "key:!": // mở hộp Thông báo ngay cả khi một ô nhập đang nhận phím (nhấn ! lúc đó chỉ gõ chữ "!")
		if m.notes.dlg == nil && m.exitDlg == nil && m.prompt == nil && !m.exiting {
			m.run("app.notifications")
		}
		return m, nil
	case strings.HasPrefix(id, "key:"):
		km, ok := footer.KeyMsg(strings.TrimPrefix(id, "key:"))
		if !ok {
			return m, nil
		}
		return m.key(km)
	case m.pal != nil:
		if i, ok := palette.ParseRowZone(id); ok {
			m.pal.SetCursor(i)
			return m.paletteKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
	case m.modalOpen():
		return m, nil
	}
	if i, ok := tabs.ParseZone(id); ok && i < len(actions.Screens) {
		return m.run(actions.NavID(actions.Screens[i]))
	}
	return m.screenPointer(func(p ui.Pointer) tea.Cmd { return p.Click(id) }, func() tea.Cmd { return m.overviewClick(id) })
}

// wheel xử lý bánh xe: hộp thoại/bảng phủ cuộn nội dung của nó; còn lại cuộn khung dưới con trỏ.
func (m *Model) wheel(mo tea.Mouse) (tea.Model, tea.Cmd) {
	if m.mouseOff || m.w < MinW || m.h < MinH || (mo.Button != tea.MouseWheelUp && mo.Button != tea.MouseWheelDown) {
		return m, nil
	}
	dy := wheelStep
	if mo.Button == tea.MouseWheelUp {
		dy = -wheelStep
	}
	m.Render()
	if m.modalOpen() {
		if _, ok := zone.At(m.zones, mo.X, mo.Y, func(id string) bool { return id == confirm.ModalZone }); !ok {
			return m, nil
		}
		code := tea.KeyDown
		if dy < 0 {
			code = tea.KeyUp
		}
		var cmds []tea.Cmd
		for i := 0; i < wheelStep; i++ {
			_, cmd := m.key(tea.KeyPressMsg{Code: code})
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	z, ok := zone.At(m.zones, mo.X, mo.Y, func(id string) bool { return strings.HasPrefix(id, "p:") })
	if !ok {
		return m, nil
	}
	pane := strings.TrimPrefix(z.ID, "p:")
	return m.screenPointer(func(p ui.Pointer) tea.Cmd { return p.Wheel(pane, dy) }, func() tea.Cmd { return m.overviewWheel(pane, dy) })
}

// screenPointer chuyển việc cho màn hình hiện tại (ui.Pointer) hoặc, với Tổng quan, cho hàm ov.
func (m *Model) screenPointer(f func(ui.Pointer) tea.Cmd, ov func() tea.Cmd) (tea.Model, tea.Cmd) {
	if m.Screen() == actions.Overview {
		return m, ov()
	}
	if p, ok := m.screens[m.Screen()].(ui.Pointer); ok {
		return m, tea.Batch(f(p), m.tickIfBusy())
	}
	return m, nil
}

// overviewClick: bấm hàng hộp thư dời con trỏ tới hàng đó; bấm lại hàng đang chọn làm như Enter.
func (m *Model) overviewClick(id string) tea.Cmd {
	i, ok := list.ParseRowZone(id)
	if !ok || i >= len(m.ov.Data.Inbox()) {
		return nil
	}
	if i != m.ov.Cursor {
		m.ov.Cursor = i
		return nil
	}
	var cmd tea.Cmd
	m.ov, cmd = m.ov.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return cmd
}

func (m *Model) overviewWheel(pane string, dy int) tea.Cmd {
	if pane == "inbox" {
		if n := len(m.ov.Data.Inbox()); n > 0 {
			m.ov.Cursor = max(0, min(m.ov.Cursor+dy/wheelStep, n-1))
		}
	}
	return nil
}

// ---- thông báo ----

// maxNotices là số thông báo giữ lại trong phiên.
const maxNotices = 50

type note struct {
	at   time.Time
	src  string
	text string
	err  bool
}

// notices giữ lịch sử thông báo (toast của mô hình gốc và của mọi màn hình) và hộp Thông báo đang mở.
type notices struct {
	hist []note
	seen map[string]string
	dlg  *confirm.Model
}

// harvestNotices ghi nhận thông báo mới (khác lần ghi trước của cùng nguồn) sau mỗi lần Update.
func (m *Model) harvestNotices() {
	if m.notes.seen == nil {
		m.notes.seen = map[string]string{}
	}
	record := func(src, text string, isErr bool) {
		if text == "" {
			delete(m.notes.seen, src)
			return
		}
		if m.notes.seen[src] == text {
			return
		}
		m.notes.seen[src] = text
		m.notes.hist = append(m.notes.hist, note{at: time.Now(), src: src, text: text, err: isErr})
		if len(m.notes.hist) > maxNotices {
			m.notes.hist = m.notes.hist[len(m.notes.hist)-maxNotices:]
		}
	}
	record("", m.rw().Rewrite(m.toast), m.toastErr)
	for _, s := range actions.Screens {
		if t, ok := m.screens[s].(ui.Toaster); ok {
			text, isErr := t.Toast()
			record(string(s), m.rw().Rewrite(text), isErr)
		}
	}
}

// currentNotice là thông báo đang hiện ở màn hình hiện tại (của mô hình gốc trước, rồi của màn hình).
func (m *Model) currentNotice() (string, bool) {
	if m.toast != "" {
		return m.rw().Rewrite(m.toast), m.toastErr
	}
	if t, ok := m.screens[m.Screen()].(ui.Toaster); ok {
		text, isErr := t.Toast()
		return m.rw().Rewrite(text), isErr
	}
	return "", false
}

// openNotices mở hộp Thông báo: nguyên văn thông báo hiện tại (ngắt dòng, cuộn được) và tối đa 50 thông báo gần nhất của mọi màn.
func (m *Model) openNotices() {
	th := m.th
	var body []string
	cur, curErr := m.currentNotice()
	if cur != "" {
		role := theme.OK
		if curErr {
			role = theme.Warn
		}
		body = append(body, th.Bold(m.cat.T("notices.current")), th.S(role, cur), "")
	}
	hist := m.notes.hist
	if cur != "" && len(hist) > 0 && hist[len(hist)-1].text == cur { // thông báo hiện tại đã nằm đầu danh sách: không lặp lại
		hist = hist[:len(hist)-1]
	}
	switch {
	case len(hist) > 0:
		body = append(body, th.Bold(m.cat.N("notices.recent", len(hist))))
		for i := len(hist) - 1; i >= 0; i-- {
			n := hist[i]
			src := m.cat.T("notices.src.app")
			if n.src != "" {
				src = m.groupName(actions.Screen(n.src))
			}
			body = append(body, th.S(theme.Muted, n.at.Format("15:04:05")+" · "+src), n.text, "")
		}
	case cur == "":
		body = append(body, th.S(theme.Muted, m.cat.T("notices.empty")))
	}
	m.notes.dlg = &confirm.Model{Title: m.cat.T("notices.title"), Body: body, Single: true, YesLabel: m.cat.T("notices.close"), NoLabel: m.cat.T("notices.close")}
}

func (m *Model) notesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch s := msg.String(); s {
	case "!", "q", "?", ":":
		m.notes.dlg = nil
		return m, nil
	}
	if m.notes.dlg.Update(msg) != confirm.Pending {
		m.notes.dlg = nil
	}
	return m, nil
}
