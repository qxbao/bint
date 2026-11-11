// Package gallery là trang trưng bày (bint tui --gallery, ẩn): mọi component dùng chung ở nền tối,
// sáng, 16 màu và NO_COLOR, với chuỗi vi, en và locale giả kéo dài. Dùng để duyệt component mới mà
// không cần dựng dữ liệu, và để chụp ảnh bằng VHS.
package gallery

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/logo"
	"bint/internal/tui/components/palette"
	"bint/internal/tui/components/tabs"
	"bint/internal/tui/theme"
)

// Options chọn trạng thái ban đầu.
type Options struct {
	Lang string
	Mode theme.Mode
}

// langs là các ngôn ngữ để duyệt, gồm locale giả kéo dài.
var langs = []string{i18n.Vietnamese, i18n.English, i18n.Pseudo}

// Model là mô hình của trang trưng bày.
type Model struct {
	w, h   int
	mode   theme.Mode
	lang   int
	offset int // dòng đầu của phần đang xem (cuộn)
}

// New dựng trang trưng bày.
func New(o Options) *Model {
	m := &Model{mode: o.Mode}
	for i, l := range langs {
		if l == o.Lang {
			m.lang = i
		}
	}
	return m
}

// SetSize đặt kích thước (kiểm thử).
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetState đặt chế độ màu và ngôn ngữ (kiểm thử và kịch bản VHS).
func (m *Model) SetState(mode theme.Mode, lang string) {
	m.mode = mode
	for i, l := range langs {
		if l == lang {
			m.lang = i
		}
	}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "m", "t":
			m.mode = theme.Modes[(int(m.mode)+1)%len(theme.Modes)]
		case "l", "L":
			m.lang = (m.lang + 1) % len(langs)
		case "j", "down":
			m.offset++
		case "k", "up":
			m.offset = max(m.offset-1, 0)
		case "pgdown", "space":
			m.offset += max(m.h-4, 1)
		case "pgup":
			m.offset = max(m.offset-max(m.h-4, 1), 0)
		case "g", "home":
			m.offset = 0
		}
	}
	return m, nil
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.Render())
	v.AltScreen = true
	return v
}

// Render vẽ trang trưng bày vừa m.w × m.h (mỗi dòng không vượt m.w ô).
func (m *Model) Render() string {
	w, h := m.w, m.h
	if w < 40 || h < 10 {
		return ""
	}
	th := theme.New(m.mode)
	cat := i18n.MustLoad(langs[m.lang])
	var body []string
	add := func(l ...string) { body = append(body, l...) }
	section := func(title string) { add("", th.S(theme.Accent, th.Bold("▌ "+title))) }

	iw, _ := frame.Inner(w, h)

	section(cat.T("gallery.section.palette"))
	add("  " + th.S(theme.Fg, "fg") + "  " + th.S(theme.Muted, "muted") + "  " + th.S(theme.Accent, "accent") + "  " + th.S(theme.Key, "key") +
		"  " + th.S(theme.OK, "✓ ok") + "  " + th.S(theme.Warn, "! warn") + "  " + th.S(theme.Err, "✗ err"))
	add("  " + th.S(theme.OK, "✓ verified") + "  " + th.S(theme.Accent, "◌ candidate") + "  " + th.S(theme.Warn, "⟳ stale") + "  " + th.S(theme.Muted, "✗ deprecated"))

	section(cat.T("gallery.section.tabs"))
	items := []tabs.Item{}
	for _, k := range []string{"overview", "knowledge", "tasks", "runs", "eval", "evolve", "system"} {
		items = append(items, tabs.Item{Label: cat.T("tab." + k), Short: cat.T("tab." + k + ".short")})
	}
	items[1].Badge = 3
	add("  " + tabs.Render(th, iw-2, items, 1, false))
	add("  " + tabs.Render(th, iw-2, items, 1, true))

	section(cat.T("gallery.section.footer"))
	hints := []footer.Hint{
		{Key: "Enter", Label: cat.T("action.open"), Priority: 1}, {Key: "a", Label: cat.T("action.approve"), Priority: 2},
		{Key: "/", Label: cat.T("action.search"), Priority: 3}, {Key: ":", Label: cat.T("action.app.palette.label"), Priority: 4},
		{Key: "x", Label: cat.T("action.select"), Priority: 5}, {Key: "?", Label: cat.T("action.app.help.label"), Pinned: true},
		{Key: "q", Label: cat.T("action.app.quit.label"), Pinned: true},
	}
	for _, width := range []int{iw - 2, 60, 34} {
		add("  " + footer.Line(th, hints, width))
	}

	section(cat.T("gallery.section.frame"))
	box := frame.Box{Title: cat.T("gallery.frame.title"), TitleAt: frame.Left, Focused: true,
		Bottom: footer.Line(th, hints, 44), BottomAt: frame.Left, Top: th.S(theme.Muted, cat.T("gallery.frame.right"))}
	add(strings.Split(box.Render(th, min(iw-2, 60), 5, cat.T("gallery.frame.body")), "\n")...)

	section(cat.T("gallery.section.logo"))
	for _, l := range logo.Render(th, true) {
		add("  " + l)
	}
	add("  " + logo.Render(th, false)[0])

	section(cat.T("gallery.section.palette_cmd"))
	p := palette.New([]palette.Item{
		{ID: "a", Title: cat.T("action.open"), Group: cat.T("tab.knowledge"), Key: "Enter"},
		{ID: "b", Title: cat.T("action.approve"), Group: cat.T("tab.knowledge"), Key: "a", Locked: cat.T("ui.locked.agent")},
	})
	add(strings.Split(p.View(th, min(iw-2, 60), 6, cat.T("palette.title"), cat.T("palette.empty")), "\n")...)

	// Cuộn: cắt phần đang xem, giữ offset trong khoảng hợp lệ.
	_, ih := frame.Inner(w, h)
	maxOff := max(len(body)-ih, 0)
	m.offset = max(min(m.offset, maxOff), 0)
	body = body[m.offset:min(m.offset+ih, len(body))]

	// Thanh trạng thái của trang trưng bày.
	status := th.S(theme.Muted, fmt.Sprintf(" %s: %s · %s: %s · ", cat.T("gallery.mode"), m.mode, cat.T("gallery.lang"), langs[m.lang])) +
		th.S(theme.Key, "[m]") + th.S(theme.Muted, " "+cat.T("gallery.hint.mode")+"  ") + th.S(theme.Key, "[l]") + th.S(theme.Muted, " "+cat.T("gallery.hint.lang")+"  ") +
		th.S(theme.Key, "[q]") + th.S(theme.Muted, " "+cat.T("action.app.quit.label"))
	return frame.Box{Title: cat.T("gallery.title"), TitleAt: frame.Center, Focused: true, Bottom: status, BottomAt: frame.Left}.Render(th, w, h, strings.Join(body, "\n"))
}
