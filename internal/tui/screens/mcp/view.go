package mcp

import (
	"fmt"
	"strings"

	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// hintAction ánh xạ (thẻ, phím) của chân phím trong khung sang hành động trong sổ ("" = phím giao diện thuần). Phím của hành động Writes
// bị bỏ ở chế độ chỉ đọc (và phím HumanOnly bị bỏ trong phiên agent) qua ui.Env.AllowedHints; màn hình không tự kiểm.
var hintAction = map[int]map[string]string{
	TabServices: {"a": "mcp.add", "x": "mcp.remove", "c": "mcp.check", "i": "mcp.index", "R": "mcp.restart", "l": "mcp.log", "s": "mcp.gateway_start",
		"S": "mcp.gateway_stop", "e": "mcp.config_edit"},
	TabGrants: {"e": "mcp.config_edit"},
}

// hintsLine dựng chân phím trong khung của thẻ hiện tại từ các cặp {phím, nhãn đã dịch}; thứ tự trong pairs là độ ưu tiên.
func (m *Model) hintsLine(pairs ...[2]string) []footer.Hint {
	out := make([]footer.Hint, len(pairs))
	for i, p := range pairs {
		out[i] = footer.Hint{Key: p[0], Label: p[1], Priority: i + 1, ActionID: hintAction[m.tab][p[0]]}
	}
	return out
}

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	if m.frm != nil {
		body = m.formView(bodyEnv)
	} else {
		body = m.tabBody(bodyEnv)
	}
	lines := strings.Split(bar+"\n"+body, "\n")
	if m.dlg != nil {
		bw, bh := min(env.W-4, 80), min(env.H-2, 22)
		box := m.dlg.c.View(th, bw, bh)
		lines = text.Overlay(lines, box, (env.W-bw)/2, max((env.H-len(box))/2, 1))
	}
	for len(lines) < env.H {
		lines = append(lines, "")
	}
	return strings.Join(lines[:env.H], "\n")
}

func (m *Model) tabBar(env ui.Env) string {
	th := env.Th
	names := make([]string, numTabs)
	ids := make([]int, numTabs)
	for i := range names {
		names[i], ids[i] = m.t("mc.tab."+TabNames[i]), i
	}
	bar := ui.SubBar(th, names, ids, m.tab, env.W-1)
	if m.toast != "" {
		if need := min(text.Width(m.toast)+4, env.W/2); env.W-text.Width(bar)-1 < need {
			bar = ui.SubBar(th, names, ids, m.tab, env.W-1-need) // chừa chỗ cho thông báo thay vì bắt thông báo bị cắt
		}
		toast := ui.ToastLabel(th, m.toast, m.toastErr, max(env.W-text.Width(bar)-1, 0))
		bar += strings.Repeat(" ", max(env.W-text.Width(bar)-text.Width(toast), 1)) + toast
	}
	return text.Fit(bar, env.W)
}

// formView vẽ form thêm service: các ô sinh từ app.MCPAddOptions (không có ô giá trị bí mật) kèm đường dẫn secrets.env và các biến credential
// còn thiếu của service đang chọn.
func (m *Model) formView(env ui.Env) string {
	th := env.Th
	iw, ih := frame.Inner(env.W, env.H)
	extra := m.secretsLines(iw, m.frmSvc)
	if m.frmBusy {
		extra = append(extra, " "+th.S(theme.Accent, env.Spin)+" "+th.S(theme.Muted, m.t("mc.add.busy")))
	}
	room := max(ih-len(extra), 4)
	body := m.frm.View(env.WithSize(iw, room), 0)
	lines := text.Lines(body)
	for len(lines) < room {
		lines = append(lines, "")
	}
	all := append(lines[:room], extra...)
	return frame.Box{Title: m.t("action.mcp.add.title"), TitleAt: frame.Left, Focused: true}.Render(th, env.W, env.H, strings.Join(all, "\n"))
}

// secretsLines là phần cuối của form thêm: nơi đặt giá trị bí mật (tệp secrets.env, sửa bằng tay) và tên biến còn thiếu của service svc.
func (m *Model) secretsLines(w int, svc string) []string {
	th := m.th
	out := []string{" " + th.S(theme.Muted, strings.Repeat("─", max(w-2, 1)))}
	out = append(out, text.WrapHang(m.t("mc.secrets.note"), max(w-2, 10), " ", " ")...)
	out = append(out, shared.KV(th, 14, w-1, m.t("mc.secrets.file"), shared.Dash(text.TildeIn(m.data.SecretsFile)))...)
	if svc == "" {
		return out
	}
	missing := m.missingEnv(svc)
	val := th.S(theme.OK, m.t("mc.secrets.none_missing"))
	if len(missing) > 0 {
		val = th.S(theme.Warn, strings.Join(missing, ", "))
	}
	return append(out, shared.KV(th, 14, w-1, m.t("mc.secrets.missing", i18n.A{"service": svc}), val)...)
}

// missingEnv là tên biến credential còn thiếu của service id (chỉ tên; giá trị không bao giờ được đọc vào màn hình).
func (m *Model) missingEnv(id string) []string {
	for _, s := range m.data.Services {
		if s.ID == id {
			return s.MissingEnv
		}
	}
	return nil
}

// stateBody trả nội dung thay thế khi thẻ chưa có dữ liệu (đang nạp hoặc lỗi); ok = đã có dữ liệu để vẽ.
func (m *Model) stateBody(env ui.Env, s slot) (string, bool) {
	th := env.Th
	switch {
	case s.Err != nil && !s.Loaded:
		return strings.Join([]string{"", " " + th.S(theme.Err, "✗ ") + m.t("mc.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, s.Err), max(env.W-6, 10), "   ", "   "), "\n"),
			"", " " + th.S(theme.Muted, m.t("mc.error.hint"))}, "\n"), false
	case !s.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading")), false
	}
	return "", true
}

func (m *Model) tabBody(env ui.Env) string {
	if msg, ok := m.stateBody(env, m.slots[m.tab]); !ok {
		return frame.Box{Title: m.t("mc.tab." + TabNames[m.tab]), TitleAt: frame.Left, Focused: true}.Render(env.Th, env.W, env.H, msg)
	}
	switch m.tab {
	case TabServices:
		// Hai khung cạnh nhau (≥ WideW): mỗi khung hẹp nên chia chân phím làm hai nửa; một khung thì mang đủ phím.
		wide := env.W >= WideW
		return m.splitAt(env, env.W*11/20, func(w int) string { return m.servicesList(env, w, env.H, wide) }, func(w int) string { return m.servicesDetail(env, w, env.H, wide) })
	case TabGrants:
		if m.grants.NotInit {
			return frame.Box{Title: m.t("mc.grants.title"), TitleAt: frame.Left, Focused: true}.Render(env.Th, env.W, env.H,
				"\n "+env.Th.S(theme.Warn, m.t("mc.grants.notinit")))
		}
		return m.split(env, func(w int) string { return m.grantsList(env, w, env.H) }, func(w int) string { return m.grantsDetail(env, w, env.H) })
	default:
		return m.split(env, func(w int) string { return m.toolsList(env, w, env.H) }, func(w int) string { return m.toolsDetail(env, w, env.H) })
	}
}

// split chia vùng thành khung trái và phải (≥ WideW cột), hoặc một khung theo pane.
func (m *Model) split(env ui.Env, left, right func(w int) string) string {
	return m.splitAt(env, 0, left, right)
}

// splitAt như split nhưng khung trái rộng leftW ô khi hiện cạnh nhau (0 = mặc định: nửa chiều rộng trừ 6, tối thiểu 46).
func (m *Model) splitAt(env ui.Env, leftW int, left, right func(w int) string) string {
	w := env.W
	if w >= WideW {
		lw := max(w/2-6, 46)
		if leftW > 0 {
			lw = min(leftW, w-40)
		}
		l, r := strings.Split(left(lw), "\n"), strings.Split(right(w-lw), "\n")
		out := make([]string, len(l))
		for i := range l {
			out[i] = l[i] + r[i]
		}
		return strings.Join(out, "\n")
	}
	if m.pane == paneDetail {
		return right(w)
	}
	return left(w)
}

func (m *Model) listBox(env ui.Env, w, h int, title, top string, hints []footer.Hint, body string) string {
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: top, BottomHints: env.AllowedHints(hints), BottomAt: frame.Left}.Render(env.Th, w, h, body)
}

func (m *Model) detailBox(env ui.Env, w, h int, title string, lines []string, hints ...footer.Hint) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	m.det.H = ih
	m.det.SetWrapped(lines, max(iw-1, 10))
	content := m.det.Render(iw, ih)
	top := th.S(theme.Muted, m.det.Position())
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top, BottomHints: env.AllowedHints(hints), BottomAt: frame.Left}.Render(th, w, h, content)
}

func (m *Model) cursorMarker(th theme.Theme, line string, cursor bool) string {
	if cursor {
		return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
	}
	return "  " + line
}

// ---- thẻ Dịch vụ ----

// glyph và vai trò màu của từng trạng thái; nhãn chữ luôn đi kèm để NO_COLOR không phân biệt chỉ bằng màu.
func kindStyle(k Kind) (string, theme.Role) {
	switch k {
	case Running:
		return "▶", theme.OK
	case Registered:
		return "●", theme.Accent
	case Degraded:
		return "!", theme.Warn
	case Error:
		return "✗", theme.Err
	}
	return "○", theme.Muted
}

func (m *Model) kindLabel(k Kind) string { return m.t("mc.state." + string(k)) }

// stateWidth là độ rộng cột trạng thái: nhãn dài nhất của locale hiện tại (kèm ký hiệu) cộng một khoảng trắng ngăn cột tóm tắt.
func (m *Model) stateWidth() int {
	w := 0
	for _, k := range Kinds {
		glyph, _ := kindStyle(k)
		w = max(w, text.Width(glyph+" "+m.kindLabel(k)))
	}
	return w + 1
}

// mb đổi KB sang chuỗi "12.3 MB" ("-" nếu 0).
func mb(kb uint64) string {
	if kb == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MB", float64(kb)/1024)
}

// mbShort như mb nhưng làm tròn MB (cột danh sách hẹp).
func mbShort(kb uint64) string {
	if kb == 0 {
		return "-"
	}
	return fmt.Sprintf("%d MB", (kb+512)/1024)
}

// summary là phần tóm tắt cuối dòng danh sách.
func (m *Model) summary(r Row) string {
	switch r.Kind {
	case Running:
		s := m.t("mc.run.short", i18n.A{"pid": r.Up.PID, "rss": mbShort(r.Up.RSSKB), "sessions": r.Up.Sessions})
		if r.Extra > 0 {
			s += " +" + fmt.Sprint(r.Extra)
		}
		return s
	case Error, Degraded:
		return shared.Dash(r.Detail)
	}
	return r.Svc.Transport + " · " + r.Svc.Share
}

// serviceHints là chân phím của thẻ Dịch vụ; half: 0 = đủ, 1 = nửa đầu (phím chính), 2 = nửa sau.
func (m *Model) serviceHints(half int) []footer.Hint {
	all := m.hintsLine([2]string{"Enter", m.t("mc.hint.open")}, [2]string{"a", m.t("mc.hint.add")}, [2]string{"c", m.t("mc.hint.check")},
		[2]string{"x", m.t("mc.hint.remove")}, [2]string{"i", m.t("mc.hint.index")}, [2]string{"R", m.t("mc.hint.restart")},
		[2]string{"l", m.t("mc.hint.log")}, [2]string{"p", m.t("mc.hint.filter")}, [2]string{"s", m.t("mc.hint.start")}, [2]string{"S", m.t("mc.hint.stop")})
	switch half {
	case 1:
		return all[:4]
	case 2:
		return all[4:]
	}
	return all
}

func (m *Model) servicesList(env ui.Env, w, h int, wide bool) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	sw := m.stateWidth()
	body := m.lst[TabServices].Render(iw, ih, func(i int, cursor bool) string {
		r := m.rows[m.idx[i]]
		glyph, role := kindStyle(r.Kind)
		name := text.Pad(r.Svc.ID, 16)
		state := th.S(role, text.Pad(glyph+" "+m.kindLabel(r.Kind), sw))
		return m.cursorMarker(th, name+state+th.S(theme.Muted, m.summary(r)), cursor)
	})
	title := m.t("mc.services.title", i18n.A{"count": len(m.idx)})
	if m.projOnly {
		title = m.t("mc.services.title_filtered", i18n.A{"count": len(m.idx), "total": len(m.rows)})
	}
	half := 0
	if wide {
		half = 1
	}
	return m.listBox(env, w, h, title, m.gatewayLabel(th), m.serviceHints(half), body)
}

// gatewayLabel là nhãn trạng thái daemon ở viền trên khung danh sách.
func (m *Model) gatewayLabel(th theme.Theme) string {
	switch {
	case m.data.Gateway != nil:
		return th.S(theme.OK, m.t("mc.gw.up", i18n.A{"pid": m.data.Gateway.PID, "sessions": m.data.Gateway.Sessions}))
	case m.data.GatewayErr != "":
		return th.S(theme.Warn, m.t("mc.gw.err"))
	}
	return th.S(theme.Muted, m.t("mc.gw.down"))
}

func (m *Model) servicesDetail(env ui.Env, w, h int, wide bool) string {
	th := env.Th
	iw, _ := frame.Inner(w, h)
	half := 0
	if wide {
		half = 2
	}
	hints := m.serviceHints(half)
	if j := m.runningJob(); j != nil {
		title := m.t("mc.job.title", i18n.A{"label": j.Label})
		out := []string{th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("mc.job.running")), ""}
		out = append(out, j.Log.Lines...)
		return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)), hints...)
	}
	if m.out != nil {
		return m.detailBox(env, w, h, m.out.Title, fitLines(m.out.Lines, max(iw-1, 10)), hints...)
	}
	r := m.Cursor()
	title := m.t("mc.detail")
	var out []string
	if r != nil {
		title += " · " + r.Svc.ID
		out = m.serviceLines(th, *r, max(iw-1, 10))
	}
	return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)), hints...)
}

// serviceLines dựng các dòng chi tiết của một service: trạng thái và lý do, đăng ký, dự án, tiến trình đang chạy, biến credential còn thiếu,
// kết quả check trong phiên.
func (m *Model) serviceLines(th theme.Theme, r Row, w int) []string {
	const lw = 16
	kv := func(key, val string) []string { return shared.KV(th, lw, w, m.t("mc.f."+key), val) }
	glyph, role := kindStyle(r.Kind)
	out := []string{th.S(theme.Accent, r.Svc.ID) + th.S(theme.Muted, " ("+r.Svc.Prefix+")"), ""}
	out = append(out, kv("state", th.S(role, glyph+" "+m.kindLabel(r.Kind)))...)
	if r.Detail != "" {
		out = append(out, kv("reason", r.Detail)...)
	}
	if !r.Checked {
		out = append(out, kv("check", th.S(theme.Muted, m.t("mc.check.never")))...)
	}
	out = append(out, kv("transport", r.Svc.Transport+" · "+r.Svc.Share)...)
	out = append(out, kv("groups", strings.Join(r.Svc.Groups, ", "))...)
	switch reg := r.Svc.Registration; {
	case reg == nil:
		out = append(out, kv("registration", th.S(theme.Muted, m.t("mc.reg.none")))...)
		if r.Svc.Install != "" {
			out = append(out, kv("install", r.Svc.Install)...)
		}
	case reg.URL != "":
		out = append(out, kv("registration", reg.URL)...)
	default:
		out = append(out, kv("registration", strings.TrimSpace(reg.Command+" "+strings.Join(reg.Args, " ")))...)
	}
	proj := th.S(theme.Muted, m.t("mc.proj.no"))
	if r.Svc.ProjectUses {
		proj = m.t("mc.proj.optional")
		if r.Svc.Required {
			proj = m.t("mc.proj.required")
		}
	}
	out = append(out, kv("project", proj)...)
	if r.Kind == Running {
		u := r.Up
		out = append(out, "")
		out = append(out, kv("pid", fmt.Sprint(u.PID))...)
		out = append(out, kv("rss", mb(u.RSSKB))...)
		out = append(out, kv("sessions", fmt.Sprintf("%d (%s %d)", u.Sessions, m.t("mc.f.inflight"), u.Inflight))...)
		out = append(out, kv("partition", u.Partition)...)
		if u.LastCall != nil {
			out = append(out, kv("last_call", fmt.Sprintf("%s %s (%dms)", u.LastCall.Tool, u.LastCall.Outcome, u.LastCall.DurationMS))...)
		}
		if r.Extra > 0 {
			out = append(out, kv("extra", m.t("mc.run.extra", i18n.A{"count": r.Extra}))...)
		}
	}
	if len(r.Parts) > 0 && r.Kind != Running {
		out = append(out, "")
		for _, u := range r.Parts {
			out = append(out, kv("partition", u.Partition+" · "+fmt.Sprintf("%s %d", m.t("mc.f.restarts"), u.Restarts))...)
		}
	}
	if r.Svc.Transport == "stdio" || len(r.Svc.MissingEnv) > 0 {
		out = append(out, "")
		out = append(out, kv("secrets", shared.Dash(text.TildeIn(m.data.SecretsFile)))...)
		missing := th.S(theme.OK, m.t("mc.secrets.none_missing"))
		if len(r.Svc.MissingEnv) > 0 {
			missing = th.S(theme.Warn, strings.Join(r.Svc.MissingEnv, ", "))
		}
		out = append(out, kv("missing_env", missing)...)
	}
	if chk, ok := m.checks[r.Svc.ID]; ok {
		out = append(out, "")
		out = append(out, kv("check", th.S(m.chkRole(chk.Status), m.t("mc.chk."+chk.Status)))...)
		if len(chk.Tools) > 0 {
			out = append(out, kv("tools", fmt.Sprint(len(chk.Tools)))...)
		}
		if len(chk.Missing) > 0 {
			out = append(out, kv("missing_tools", strings.Join(chk.Missing, ", "))...)
		}
		if len(chk.Extra) > 0 {
			out = append(out, kv("extra_tools", strings.Join(chk.Extra, ", "))...)
		}
		for _, wmsg := range chk.Warnings {
			out = append(out, kv("warning", wmsg)...)
		}
	}
	return out
}

// fitLines ngắt dòng dài theo độ rộng w thay vì cắt bằng …: dòng tiếp thụt theo dòng gốc cộng 2 ô.
func fitLines(ls []string, w int) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		if text.Width(l) <= w {
			out = append(out, l)
			continue
		}
		out = append(out, text.WrapHang(l, w, "", strings.Repeat(" ", len(l)-len(strings.TrimLeft(l, " "))+2))...)
	}
	return out
}

// ---- thẻ Quyền ----

func (m *Model) grantsList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	body := m.lst[TabGrants].Render(iw, ih, func(i int, cursor bool) string {
		g := m.grants.Roles[i]
		name := g.Role
		if g.Implicit {
			name += " ~"
		}
		cells := GrantCells(g)
		line := text.Pad(name, 16) + th.S(theme.Muted, m.t("mc.grants.summary", i18n.A{"services": len(cells), "tools": len(g.Tools)}))
		if len(g.Warnings) > 0 {
			line += " " + th.S(theme.Warn, "!")
		}
		return m.cursorMarker(th, line, cursor)
	})
	hints := m.hintsLine([2]string{"e", m.t("mc.hint.edit_config")}, [2]string{"Enter", m.t("mc.hint.open")})
	return m.listBox(env, w, h, m.t("mc.grants.title"), th.S(theme.Muted, m.t("mc.grants.readonly")), hints, body)
}

func (m *Model) grantsDetail(env ui.Env, w, h int) string {
	th := env.Th
	iw, _ := frame.Inner(w, h)
	c := m.lst[TabGrants].Cursor
	title := m.t("mc.detail")
	var out []string
	if c >= 0 && c < len(m.grants.Roles) {
		g := m.grants.Roles[c]
		title += " · " + g.Role
		out = append(out, th.S(theme.Accent, g.Role))
		if g.Implicit {
			out = append(out, th.S(theme.Muted, m.t("mc.grants.implicit")))
		}
		out = append(out, "")
		cells := GrantCells(g)
		sw := 0
		for _, ce := range cells {
			sw = max(sw, text.Width(ce.Service))
		}
		out = append(out, th.S(theme.Muted, text.Pad(m.t("mc.grants.col_service"), sw+2)+m.t("mc.grants.col_groups")))
		for _, ce := range cells {
			out = append(out, th.S(theme.Accent, text.Pad(ce.Service, sw+2))+strings.Join(ce.Groups, ", "))
		}
		if len(cells) == 0 {
			out = append(out, th.S(theme.Muted, m.t("mc.grants.none")))
		}
		for _, wmsg := range g.Warnings {
			out = append(out, "", th.S(theme.Warn, "! ")+wmsg)
		}
	}
	out = append(out, "", th.S(theme.Muted, m.t("mc.grants.note")))
	return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)))
}

// ---- thẻ Công cụ Bint ----

func (m *Model) toolsList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	body := m.lst[TabTools].Render(iw, ih, func(i int, cursor bool) string {
		t := m.tools[i]
		return m.cursorMarker(th, text.Pad(t.Name, 22)+" "+th.S(theme.Muted, t.Description), cursor)
	})
	return m.listBox(env, w, h, m.t("mc.tools.title", i18n.A{"count": len(m.tools)}), "", m.hintsLine([2]string{"Enter", m.t("mc.hint.open")}), body)
}

func (m *Model) toolsDetail(env ui.Env, w, h int) string {
	th := env.Th
	iw, _ := frame.Inner(w, h)
	c := m.lst[TabTools].Cursor
	var out []string
	title := m.t("mc.detail")
	if c >= 0 && c < len(m.tools) {
		t := m.tools[c]
		title += " · " + t.Name
		out = append(out, th.S(theme.Accent, t.Name), "")
		out = append(out, text.Wrap(t.Description, max(iw-1, 10))...)
	}
	out = append(out, "", th.S(theme.Muted, m.t("mc.tools.note")))
	return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)))
}
