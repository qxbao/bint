package app

import (
	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/help"
	"bint/internal/tui/ui"
)

// helpSize là kích thước hộp trợ giúp.
func (m *Model) helpSize() (w, h int) { return min(m.w-4, 84), min(m.h-7, 26) }

// helpContext dựng nhóm đầu của hộp trợ giúp: tên màn và thẻ phụ đang xem, đoạn giới thiệu ngắn của thẻ (khoá help.ctx.<màn>[.<thẻ>]) và các
// phím của thẻ — chính chân phím trong khung mà màn hình dựng khi vẽ (đã lọc theo chỉ đọc qua ui.Env.Allowed), nên không bao giờ lệch.
func (m *Model) helpContext() (help.Group, bool) {
	scr := m.Screen()
	sc := m.screens[scr]
	g := help.Group{Title: m.groupName(scr)}
	if st, ok := sc.(ui.SubTabber); ok {
		if sub := st.SubTab(); sub != "" {
			g.Title += " › " + sub
		}
	}
	key := "help.ctx." + string(scr)
	if h, ok := sc.(ui.Helper); ok {
		if tab := h.HelpTab(); tab != "" {
			key += "." + tab
		}
	}
	if m.cat.Has(key) {
		g.Intro = m.cat.T(key)
	}
	if sc != nil {
		var hints []footer.Hint
		env := m.newEnv(m.w, m.h)
		env.Collect = func(hs []footer.Hint) { hints = append(hints, hs...) }
		mainH := m.h - 3
		iw, ih := frame.Inner(m.w, mainH)
		_ = m.screenBody(env.WithSize(iw, ih))
		seen := map[string]bool{}
		for _, h := range hints {
			if seen[h.Key] {
				continue
			}
			seen[h.Key] = true
			g.Rows = append(g.Rows, help.Row{Key: h.Key, Title: h.Label})
		}
	}
	return g, g.Intro != "" || len(g.Rows) > 0
}

// helpPos là nhãn vị trí cuộn ở viền dưới hộp trợ giúp.
func (m *Model) helpPos(from, to, total int) string {
	return m.cat.T("help.pos", i18n.A{"from": from, "to": to, "total": total})
}

// helpKey xử lý phím khi hộp trợ giúp mở: đóng bằng Esc/?/q/Enter; cuộn bằng j/k, mũi tên, PgUp/PgDn, Home/End (bánh xe đến qua đây dưới dạng
// phím mũi tên).
func (m *Model) helpKey(s string) (tea.Model, tea.Cmd) {
	bw, bh := m.helpSize()
	iw, _ := frame.Inner(bw, bh)
	total := len(help.Layout(m.th, iw, m.helpGroups()))
	visible, maxTop := help.Window(bh, total)
	switch s {
	case "esc", "ctrl+c", "?", "q", "enter":
		m.showHelp = false
	case "j", "down":
		m.helpTop++
	case "k", "up":
		m.helpTop--
	case "pgdown", "space", "ctrl+d":
		m.helpTop += max(visible-1, 1)
	case "pgup", "ctrl+u":
		m.helpTop -= max(visible-1, 1)
	case "home", "g":
		m.helpTop = 0
	case "end", "G":
		m.helpTop = maxTop
	}
	m.helpTop = max(0, min(m.helpTop, maxTop))
	return m, nil
}
