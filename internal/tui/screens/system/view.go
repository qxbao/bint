package system

import (
	"fmt"
	"strings"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/hcomp"
	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/components/frame"
	"bint/internal/tui/components/list"
	"bint/internal/tui/components/text"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

var supportIcon = map[harness.Support]string{harness.Yes: "✓", harness.Partial: "~", harness.No: "✗"}

// View vẽ màn hình trong vùng env.W × env.H (không gồm khung ngoài).
func (m *Model) View(env ui.Env) string {
	m.w, m.h = env.W, env.H
	th := env.Th
	bar := m.tabBar(env)
	bodyEnv := env.WithSize(env.W, max(env.H-1, 1))
	var body string
	switch {
	case m.frm != nil:
		body = m.formView(bodyEnv)
	default:
		body = m.tabBody(bodyEnv)
	}
	lines := strings.Split(bar+"\n"+body, "\n")
	if m.dlg != nil {
		bw, bh := min(env.W-4, 88), min(env.H-2, 26)
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
		names[i], ids[i] = m.t("sy.tab."+TabNames[i]), i
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

func (m *Model) formView(env ui.Env) string {
	iw, ih := frame.Inner(env.W, env.H)
	title := m.frmID
	if k := "action." + m.frmID + ".title"; m.cat.Has(k) {
		title = m.t(k)
	}
	return frame.Box{Title: title, TitleAt: frame.Left, Focused: true}.Render(env.Th, env.W, env.H, m.frm.View(env.WithSize(iw, ih), 0))
}

// split chia vùng thành khung trái và phải (≥ WideW cột), hoặc một khung theo pane.
func (m *Model) split(env ui.Env, left, right func(w int) string) string {
	w := env.W
	if w >= WideW {
		lw := max(w/2-6, 46)
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

// hintAction ánh xạ (thẻ, phím) của chân phím trong khung sang hành động trong sổ ("" = phím giao diện thuần). Phím của hành động Writes
// bị bỏ ở chế độ chỉ đọc qua ui.Env.AllowedHints; màn hình không tự kiểm ReadOnly.
var hintAction = map[int]map[string]string{
	TabConfig:     {"e": "system.config_edit", "U": "system.config_upgrade_plan"},
	TabComponents: {"n": "system.component_add", "a": "system.component_approve", "d": "system.component_deprecate", "R": "system.render"},
	TabSkills:     {"o": "system.skill_overlay", "d": "system.skill_diff", "v": "system.skill_versions", "c": "system.skill_check", "g": "system.skill_regress", "s": "system.sync_plan"},
	TabIndex:      {"b": "system.index_rebuild"},
	TabLanguage:   {"Enter": "system.language_save"},
	TabSetup:      {"e": "system.repo_plan", "U": "system.config_upgrade_plan"},
	TabUpdate:     {"u": "system.update"},
	TabHarness:    {"V": "system.harness_version"},
}

// hintsLine dựng chân phím trong khung của thẻ hiện tại từ các cặp {phím, nhãn đã dịch}.
func (m *Model) hintsLine(pairs ...[2]string) []footer.Hint {
	out := make([]footer.Hint, len(pairs))
	for i, p := range pairs {
		out[i] = footer.Hint{Key: p[0], Label: p[1], Priority: i + 1, ActionID: hintAction[m.tab][p[0]]}
	}
	return out
}

// stateBody trả nội dung thay thế khi thẻ chưa có dữ liệu (đang nạp hoặc lỗi); ok = đã có dữ liệu để vẽ.
func (m *Model) stateBody(env ui.Env, s slot, hintKey string) (string, bool) {
	th := env.Th
	switch {
	case s.Err != nil && !s.Loaded:
		return strings.Join([]string{"", " " + th.S(theme.Err, "✗ ") + m.t("sy.error"), strings.Join(text.WrapHang(ui.ErrText(m.rw, s.Err), max(env.W-6, 10), "   ", "   "), "\n"),
			"", " " + th.S(theme.Muted, m.t(hintKey))}, "\n"), false
	case !s.Loaded:
		return "\n  " + th.S(theme.Accent, env.Spin) + " " + th.S(theme.Muted, m.t("ui.loading")), false
	}
	return "", true
}

func (m *Model) tabBody(env ui.Env) string {
	slotTab := m.tab
	if slotTab == TabLanguage {
		slotTab = TabConfig
	}
	if msg, ok := m.stateBody(env, m.slots[slotTab], "sy.error.hint"); !ok && m.tab != TabIndex {
		return frame.Box{Title: m.t("sy.tab." + TabNames[m.tab]), TitleAt: frame.Left, Focused: true}.Render(env.Th, env.W, env.H, msg)
	}
	switch m.tab {
	case TabConfig:
		return m.configView(env)
	case TabHarness:
		return m.harnessView(env)
	case TabComponents:
		return m.componentsView(env)
	case TabSkills:
		return m.skillsView(env)
	case TabIndex:
		return m.indexView(env)
	case TabMCP:
		return m.mcpView(env)
	case TabLanguage:
		return m.languageView(env)
	case TabSetup:
		return m.setupView(env)
	case TabUpdate:
		return m.updateView(env)
	default:
		return m.doctorView(env)
	}
}

// listBox vẽ khung danh sách của thẻ hiện tại.
func (m *Model) listBox(env ui.Env, w, h int, title, top string, hints []footer.Hint, body string) string {
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "list", Focused: m.pane == paneList, Top: top, BottomHints: env.AllowedHints(hints), BottomAt: frame.Left}.Render(env.Th, w, h, body)
}

// detailBox vẽ khung chi tiết cuộn được từ các dòng đã dựng.
func (m *Model) detailBox(env ui.Env, w, h int, title string, lines []string) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	m.det.H = ih
	m.det.SetWrapped(lines, max(iw-1, 10))
	content := m.det.Render(iw, ih)
	top := th.S(theme.Muted, m.det.Position())
	return frame.Box{Title: title, TitleAt: frame.Left, Pane: "detail", Focused: m.pane == paneDetail, Top: top}.Render(th, w, h, content)
}

// detailOr trả khung chi tiết: kết quả hành động (out) nếu có, ngược lại các dòng chi tiết mặc định.
func (m *Model) detailOr(env ui.Env, w, h int, title string, lines func(iw int) []string) string {
	iw, _ := frame.Inner(w, h)
	if m.out != nil {
		return m.detailBox(env, w, h, m.out.Title, fitLines(m.out.Lines, max(iw-1, 10)))
	}
	return m.detailBox(env, w, h, title, fitLines(lines(max(iw-1, 10)), max(iw-1, 10)))
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

func (m *Model) cursorMarker(th theme.Theme, line string, cursor bool) string {
	if cursor {
		return th.S(theme.Key, "▸ ") + th.Selected(shared.StripColor(th, line))
	}
	return "  " + line
}

// ---- Cấu hình ----

func (m *Model) configView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.configList(env, w, env.H) },
		func(w int) string { return m.configDetail(env, w, env.H) })
}

func (m *Model) configList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	body := m.lst[TabConfig].Render(iw, ih, func(i int, cursor bool) string {
		name := m.t("sy.config.sources")
		if i > 0 {
			name = m.cfg.Groups[i-1].Key
		}
		return m.cursorMarker(th, name, cursor)
	})
	top := th.S(theme.Muted, m.t("sy.config.schema", i18n.A{"version": m.cfg.Version}))
	return m.listBox(env, w, h, m.t("sy.config.title"), top, m.hintsLine([2]string{"e", m.t("sy.hint.edit")}, [2]string{"Enter", m.t("sy.hint.open")}), body)
}

func (m *Model) configDetail(env ui.Env, w, h int) string {
	c := m.lst[TabConfig].Cursor
	title := m.t("sy.config.sources")
	if c > 0 && c-1 < len(m.cfg.Groups) {
		title = m.cfg.Groups[c-1].Key
	}
	return m.detailOr(env, w, h, title, func(iw int) []string {
		if c <= 0 || c-1 >= len(m.cfg.Groups) {
			return m.sourceLines(env, iw)
		}
		return strings.Split(strings.TrimRight(m.cfg.Groups[c-1].YAML, "\n"), "\n")
	})
}

func (m *Model) sourceLines(env ui.Env, w int) []string {
	th := env.Th
	out := []string{th.S(theme.Muted, m.t("sy.config.layers.hint")), ""}
	for _, s := range m.cfg.Sources {
		glyph, role := "○", theme.Muted
		if s.Present {
			glyph, role = "●", theme.OK
		}
		state := m.t("sy.config.layer.absent")
		if s.Present {
			state = m.t("sy.config.layer.present")
		}
		out = append(out, th.S(role, glyph)+" "+th.S(theme.Accent, text.Pad(m.t("sy.config.layer."+s.Name), 12))+state)
		path := s.Path
		if path == "" {
			path = "-"
		}
		out = append(out, "    "+text.Path(path, max(w-4, 10)))
		if s.Editable {
			out = append(out, "    "+th.S(theme.Key, m.t("sy.config.editable")))
		}
	}
	out = append(out, "", th.S(theme.Accent, m.t("sy.config.root"))+" "+text.Path(m.cfg.Root, max(w-14, 10)))
	if m.cfg.LinkedWorktree {
		out = append(out, th.S(theme.Warn, m.t("sy.linked", i18n.A{"main": text.Tilde(m.cfg.Main)})))
	}
	if m.cfg.StrategyProblem != "" {
		out = append(out, th.S(theme.Warn, m.t("sy.config.strategy_problem", i18n.A{"detail": m.cfg.StrategyProblem})))
	}
	return out
}

// ---- Harness ----

func (m *Model) harnessView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.harnessList(env, w, env.H) },
		func(w int) string { return m.harnessDetail(env, w, env.H) })
}

func (m *Model) version(in harness.Info) string {
	if v, ok := m.versions[in.Name]; ok && v != "" {
		return v
	}
	return shared.Dash(in.Version)
}

func (m *Model) harnessList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	var lines []string
	widths := list.Widths(iw-2, 1, []int{11, 4, 5, 9, 10, 0})
	head := list.Row(widths, 1, m.t("sy.har.col.name"), m.t("sy.har.col.on"), m.t("sy.har.col.here"), m.t("sy.har.col.version"), m.t("sy.har.col.verified"), m.t("sy.har.col.level"))
	lines = append(lines, "  "+th.S(theme.Accent, head))
	yes, no := m.t("sy.yes"), "-"
	for i, in := range m.har.Infos {
		on, here := no, no
		if in.Enabled {
			on = yes
		}
		if in.Installed {
			here = yes
		}
		ver := m.version(in)
		if m.verBusy[in.Name] {
			ver = env.Spin
		}
		row := list.Row(widths, 1, in.Name, on, here, ver, in.VerifiedWith, fmt.Sprintf("%d", in.Level))
		lines = append(lines, m.cursorMarker(th, row, i == m.lst[TabHarness].Cursor))
	}
	lines = append(lines, "", " "+th.S(theme.Accent, m.t("sy.har.caps")))
	cw := list.Widths(iw-2, 1, matrixWidths(len(m.har.Infos)))
	capHead := []string{""}
	for _, in := range m.har.Infos {
		capHead = append(capHead, text.Truncate(in.Name, 4))
	}
	lines = append(lines, "  "+th.S(theme.Muted, list.Row(cw, 1, capHead...)))
	for _, ck := range harness.AllCapabilities {
		cells := []string{m.capTitle(ck.Key, ck.Title)}
		for _, in := range m.har.Infos {
			cells = append(cells, supportIcon[capOf(in, ck.Key).Support])
		}
		lines = append(lines, "  "+list.Row(cw, 1, cells...))
	}
	lines = append(lines, "  "+th.S(theme.Muted, m.t("sy.har.legend")))
	for len(lines) < ih {
		lines = append(lines, "")
	}
	hints := m.hintsLine([2]string{"V", m.t("sy.hint.version")}, [2]string{"Enter", m.t("sy.hint.open")})
	return m.listBox(env, w, h, m.t("sy.har.title"), "", hints, strings.Join(lines[:min(len(lines), ih)], "\n"))
}

// matrixWidths: cột đầu (tên khả năng) lấy phần còn lại, mỗi harness một cột 4 (tiêu đề là 4 chữ đầu của tên, bảng
// trên đã ghi tên đầy đủ) để tên khả năng dài vẫn hiện trọn ở 80 cột.
func matrixWidths(n int) []int {
	out := []int{0}
	for i := 0; i < n; i++ {
		out = append(out, 4)
	}
	return out
}

func (m *Model) capTitle(key, fallback string) string {
	if k := "sy.cap." + key; m.cat.Has(k) {
		return m.t(k)
	}
	return fallback
}

func capOf(in harness.Info, key string) harness.Capability {
	for _, c := range in.Capabilities {
		if c.Key == key {
			return c
		}
	}
	return harness.Capability{Key: key, Support: harness.No}
}

func (m *Model) levelTitle(in harness.Info) string {
	if k := fmt.Sprintf("sy.level.%d", in.Level); m.cat.Has(k) {
		return m.t(k)
	}
	return in.LevelTitle
}

func (m *Model) harnessDetail(env ui.Env, w, h int) string {
	c := m.lst[TabHarness].Cursor
	if c < 0 || c >= len(m.har.Infos) {
		return m.detailBox(env, w, h, m.t("sy.detail"), []string{"", " " + env.Th.S(theme.Muted, m.t("sy.har.none"))})
	}
	in := m.har.Infos[c]
	return m.detailBox(env, w, h, m.t("sy.detail")+" · "+in.Name, m.harnessLines(env, in, max(w-5, 10)))
}

func (m *Model) harnessLines(env ui.Env, in harness.Info, w int) []string {
	th := env.Th
	kv := func(key, val string) []string { return shared.KV(th, 20, w, m.t("sy.har.f."+key), val) }
	var out []string
	out = append(out, th.S(theme.Accent, fmt.Sprintf("%s (%s)", in.Title, in.Name)))
	out = append(out, kv("level", fmt.Sprintf("%d — %s", in.Level, m.levelTitle(in)))...)
	loc := in.Binary
	if loc == "" {
		loc = in.Home
	}
	if loc == "" {
		loc = m.t("sy.har.notfound")
	}
	out = append(out, kv("binary", loc)...)
	out = append(out, kv("version", fmt.Sprintf("%s (%s %s)", m.version(in), m.t("sy.har.f.verified"), in.VerifiedWith))...)
	enabled := m.t("sy.har.enabled.no")
	if in.Enabled {
		enabled = m.t("sy.yes")
	}
	out = append(out, kv("enabled", enabled)...)
	out = append(out, kv("skilldir", in.SkillDir)...)
	// Trạng thái hook và MCP: cùng nguồn với doctor (app.HarnessRuntime).
	out = append(out, "", th.S(theme.Accent, m.t("sy.har.runtime")))
	switch {
	case m.rtSl.Loading && !m.rtSl.Loaded:
		out = append(out, "  "+th.S(theme.Accent, env.Spin)+" "+th.S(theme.Muted, m.t("ui.loading")))
	case m.rtSl.Err != nil:
		out = append(out, "  "+th.S(theme.Err, "✗ ")+ui.ErrText(m.rw, m.rtSl.Err))
	default:
		if st := m.runtimeOf(in.Name); st == nil {
			out = append(out, "  "+th.S(theme.Muted, m.t("sy.har.runtime.off")))
		} else {
			out = append(out, m.runtimeLines(th, st, w)...)
		}
	}
	out = append(out, "", th.S(theme.Accent, m.t("sy.har.caps")))
	for _, ck := range harness.AllCapabilities {
		cp := capOf(in, ck.Key)
		role := map[harness.Support]theme.Role{harness.Yes: theme.OK, harness.Partial: theme.Warn, harness.No: theme.Muted}[cp.Support]
		out = append(out, text.WrapHang(th.S(role, supportIcon[cp.Support])+" "+m.capTitle(ck.Key, ck.Title)+"  "+th.S(theme.Muted, m.cli(cp.Detail)), w, " ", "    ")...)
	}
	return out
}

func (m *Model) runtimeOf(name string) *app.HarnessStatus {
	for i := range m.rt {
		if m.rt[i].Name == name {
			return &m.rt[i]
		}
	}
	return nil
}

func levelRole(l app.Level) (string, theme.Role) {
	switch l {
	case app.LevelOK:
		return "✓", theme.OK
	case app.LevelWarn:
		return "!", theme.Warn
	case app.LevelFail:
		return "✗", theme.Err
	}
	return "·", theme.Muted
}

func (m *Model) runtimeLines(th theme.Theme, st *app.HarnessStatus, w int) []string {
	var out []string
	if st.Hook != nil {
		g, role := levelRole(st.Hook.Level)
		scopes := m.t("sy.har.hook.none")
		if len(st.Hook.Scopes) > 0 {
			scopes = strings.Join(st.Hook.Scopes, ", ")
		}
		out = append(out, text.WrapHang(th.S(role, g)+" "+m.t("sy.har.hook", i18n.A{"scopes": scopes}), w, "  ", "      ")...)
		out = append(out, text.WrapHang(th.S(theme.Muted, m.cli(st.Hook.Detail)), w, "      ", "      ")...)
	} else {
		out = append(out, "  "+th.S(theme.Muted, m.t("sy.har.hook.na")))
	}
	if st.MCP != nil {
		g, role, key := "✓", theme.OK, "sy.har.mcp.yes"
		if !st.MCP.Registered {
			g, role, key = "·", theme.Warn, "sy.har.mcp.no"
		}
		out = append(out, "  "+th.S(role, g)+" "+m.t(key))
		if st.MCP.CitationUnverified {
			out = append(out, text.WrapHang(th.S(theme.Warn, "!")+" "+m.t("sy.har.mcp.citation"), w, "  ", "    ")...)
		}
	} else {
		out = append(out, "  "+th.S(theme.Muted, m.t("sy.har.mcp.na")))
	}
	if st.CodexUntrusted {
		out = append(out, text.WrapHang(th.S(theme.Warn, "!")+" "+m.t("sy.har.codex.trust"), w, "  ", "    ")...)
	}
	out = append(out, "  "+th.S(theme.Muted, m.t("sy.har.cli")))
	return out
}

// ---- Thành phần ----

func (m *Model) componentsView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.componentsList(env, w, env.H) },
		func(w int) string { return m.componentsDetail(env, w, env.H) })
}

func statusGlyph(status string) (string, theme.Role) {
	switch status {
	case hcomp.StatusVerified:
		return "✓", theme.OK
	case hcomp.StatusDeprecated:
		return "✗", theme.Muted
	}
	return "◌", theme.Warn
}

func (m *Model) componentsList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	title := m.t("sy.comp.title", i18n.A{"shown": len(m.compIdx), "total": len(m.comps.Details)})
	var chips []string
	if m.kindF != "" {
		chips = append(chips, m.kindF)
	}
	if m.statusF != "" {
		chips = append(chips, m.statusF)
	}
	if m.filter != "" {
		chips = append(chips, "“"+m.filter+"”")
	}
	if m.textIn {
		chips = append(chips, "/"+m.filter+"▏")
	}
	if n := len(m.comps.Problems); n > 0 {
		chips = append(chips, m.t("sy.comp.problems", i18n.A{"count": n}))
	}
	top := th.S(theme.Key, text.Truncate(strings.Join(chips, " · "), max(w/2, 12)))
	var body string
	switch {
	case len(m.comps.Details) == 0:
		body = "\n " + th.S(theme.Muted, m.t("sy.comp.empty"))
	case len(m.compIdx) == 0:
		body = "\n " + th.S(theme.Muted, m.t("sy.comp.empty_filtered"))
	default:
		widths := list.Widths(iw-2, 1, []int{1, 9, 0, 10, 6})
		body = m.lst[TabComponents].Render(iw, ih, func(i int, cursor bool) string {
			d := m.comps.Details[m.compIdx[i]]
			c := d.Component
			g, role := statusGlyph(c.Status)
			line := list.Row(widths, 1, th.S(role, g), string(c.Kind), c.ID, c.Status, d.Row.Risk)
			return m.cursorMarker(th, line, cursor)
		})
	}
	hints := m.hintsLine([2]string{"n", m.t("sy.hint.add")}, [2]string{"a", m.t("sy.hint.approve")}, [2]string{"d", m.t("sy.hint.deprecate")},
		[2]string{"R", m.t("sy.hint.render")}, [2]string{"f", m.t("sy.hint.filter")})
	return m.listBox(env, w, h, title, top, hints, body)
}

func (m *Model) componentsDetail(env ui.Env, w, h int) string {
	th := env.Th
	d := m.CursorComponent()
	title := m.t("sy.detail")
	if d != nil {
		title += " · " + d.Component.ID
	}
	return m.detailOr(env, w, h, title, func(iw int) []string {
		var out []string
		if d == nil {
			out = []string{"", " " + th.S(theme.Muted, m.t("sy.comp.none"))}
		} else {
			c := d.Component
			kv := func(key, val string) {
				out = append(out, shared.KV(th, 12, iw, m.t("sy.comp.f."+key), shared.Dash(val))...)
			}
			kv("kind", string(c.Kind))
			kv("id", c.ID)
			kv("scope", c.Scope)
			g, role := statusGlyph(c.Status)
			out = append(out, th.S(theme.Accent, text.Pad(m.t("sy.comp.f.status"), 12))+th.S(role, g)+" "+c.Status)
			kv("origin", c.Origin)
			kv("version", fmt.Sprintf("%d", c.Version))
			kv("risk", d.Row.Risk)
			kv("approved", c.ApprovedBy)
			kv("file", d.Row.File)
			out = append(out, "", th.S(theme.Accent, m.t("sy.comp.content")))
			out = append(out, strings.Split(strings.TrimRight(d.Content, "\n"), "\n")...)
			if d.Script != "" {
				out = append(out, "", th.S(theme.Accent, m.t("sy.comp.script", i18n.A{"id": c.ID})), th.S(theme.Warn, m.t("sy.comp.script.warn")))
				out = append(out, strings.Split(strings.TrimRight(d.Script, "\n"), "\n")...)
			}
		}
		if len(m.comps.Problems) > 0 {
			out = append(out, "", th.S(theme.Accent, m.t("sy.comp.problems", i18n.A{"count": len(m.comps.Problems)})))
			for _, p := range m.comps.Problems {
				out = append(out, text.WrapHang(th.S(theme.Warn, "! ")+m.cli(p), iw, "", "  ")...)
			}
		}
		return out
	})
}

// ---- Skill ----

func (m *Model) skillsView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.skillsList(env, w, env.H) },
		func(w int) string { return m.skillsDetail(env, w, env.H) })
}

func (m *Model) skillsList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	title := m.t("sy.skill.title", i18n.A{"count": len(m.skills.Rows)})
	var body string
	if len(m.skills.Rows) == 0 {
		body = "\n " + th.S(theme.Muted, m.t("sy.skill.empty"))
	} else {
		widths := list.Widths(iw-2, 1, []int{5, 0, 9, 13})
		body = m.lst[TabSkills].Render(iw, ih, func(i int, cursor bool) string {
			r := m.skills.Rows[i]
			state := publishedSummary(r)
			line := list.Row(widths, 1, m.t("sy.skill.kind."+r.Kind), r.Name, r.Version, m.t("sy.skill.pub."+state))
			return m.cursorMarker(th, line, cursor)
		})
	}
	top := ""
	if m.coreView {
		top = th.S(theme.Key, m.t("sy.skill.coremode"))
	}
	hints := m.hintsLine([2]string{"Enter", m.t("sy.hint.content")}, [2]string{"s", m.t("sy.hint.sync")}, [2]string{"o", m.t("sy.hint.overlay")},
		[2]string{"d", m.t("sy.hint.diff")}, [2]string{"c", m.t("sy.hint.check")})
	return m.listBox(env, w, h, title, top, hints, body)
}

// publishedSummary tóm tắt trạng thái xuất bản: ok (mọi thư mục current), differs, missing, none (không có đích hoặc không xuất bản).
func publishedSummary(r app.SkillRow) string {
	if r.Problem != "" {
		return "conflict"
	}
	if len(r.Targets) == 0 {
		return "none"
	}
	state := "ok"
	for _, st := range r.Targets {
		switch st {
		case "differs":
			return "differs"
		case "missing":
			state = "missing"
		}
	}
	return state
}

func (m *Model) skillsDetail(env ui.Env, w, h int) string {
	th := env.Th
	r := m.cursorSkill()
	title := m.t("sy.detail")
	if r != nil {
		title += " · " + r.Name
	}
	return m.detailOr(env, w, h, title, func(iw int) []string {
		var out []string
		if r == nil {
			out = []string{"", " " + th.S(theme.Muted, m.t("sy.skill.none"))}
		} else {
			kv := func(key, val string) {
				out = append(out, shared.KV(th, 14, iw, m.t("sy.skill.f."+key), shared.Dash(val))...)
			}
			kv("kind", m.t("sy.skill.kind."+r.Kind))
			kv("version", r.Version)
			if r.Kind == "core" {
				locked := "-"
				if r.Locked != "" {
					locked = r.Locked
					if r.Locked != r.Version {
						locked += " " + m.t("sy.skill.lock.old")
					}
				}
				kv("lock", locked)
				kv("overlay", r.Overlay)
			} else {
				st := r.Status
				if r.Stale != "" {
					st += ", " + m.t("sy.skill.stale")
				}
				kv("status", st)
			}
			if r.Kind == "evolved" {
				kv("triggers", strings.Join(r.Anchors, ", "))
			} else {
				kv("anchors", strings.Join(r.Anchors, ", "))
			}
			out = append(out, "", th.S(theme.Accent, m.t("sy.skill.published")))
			if len(r.Targets) == 0 {
				out = append(out, "  "+th.S(theme.Muted, m.t("sy.skill.pub.none")))
			}
			for _, dir := range sortedKeys(r.Targets) {
				g, role := "✓", theme.OK
				switch r.Targets[dir] {
				case "differs":
					g, role = "~", theme.Warn
				case "missing":
					g, role = "✗", theme.Muted
				}
				out = append(out, "  "+th.S(role, g)+" "+dir+"  "+th.S(theme.Muted, m.t("sy.skill.tgt."+r.Targets[dir])))
			}
			if r.Stale != "" {
				out = append(out, "", th.S(theme.Warn, "! ")+m.t("sy.skill.stale_why", i18n.A{"why": r.Stale}))
			}
			if r.Problem != "" {
				out = append(out, "", th.S(theme.Err, "✗ ")+m.cli(r.Problem))
			}
		}
		for _, p := range m.skills.EvolvedProblems {
			out = append(out, "", th.S(theme.Err, "✗ ")+m.t("sy.skill.evolved_bad", i18n.A{"detail": p}))
		}
		for _, o := range m.skills.Orphans {
			out = append(out, "", th.S(theme.Warn, "! ")+m.t("sy.skill.orphan", i18n.A{"path": o}))
		}
		return out
	})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ---- Chỉ mục ----

func (m *Model) indexView(env ui.Env) string {
	th := env.Th
	iw, ih := frame.Inner(env.W, env.H)
	out := []string{""}
	for _, l := range text.Wrap(m.t("sy.index.about"), max(iw-2, 10)) {
		out = append(out, " "+l)
	}
	out = append(out, "", " "+th.S(theme.Muted, m.t("sy.index.cli")), "")
	if m.idxRes != nil {
		out = append(out, " "+th.S(theme.Accent, m.t("sy.index.last")))
		out = append(out, "  "+th.S(theme.OK, "✓ ")+m.t("sy.index.done", i18n.A{"total": m.idxRes.Total, "bad": len(m.idxRes.Errors)}))
		for _, e := range m.idxRes.Errors {
			out = append(out, text.WrapHang("  "+th.S(theme.Err, "✗ ")+e.Error(), iw-1, "", "    ")...)
		}
	} else {
		out = append(out, " "+th.S(theme.Muted, m.t("sy.index.never")))
	}
	hints := m.hintsLine([2]string{"b", m.t("sy.hint.rebuild")})
	return frame.Box{Title: m.t("sy.index.title"), TitleAt: frame.Left, Focused: true, BottomHints: env.AllowedHints(hints), BottomAt: frame.Left}.
		Render(th, env.W, env.H, strings.Join(fitLines(out, iw)[:min(len(out), ih)], "\n"))
}

// ---- MCP ----

func (m *Model) mcpView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.mcpList(env, w, env.H) },
		func(w int) string { return m.mcpDetail(env, w, env.H) })
}

func (m *Model) mcpList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	body := m.lst[TabMCP].Render(iw, ih, func(i int, cursor bool) string {
		t := m.mcp[i]
		return m.cursorMarker(th, text.Pad(t.Name, 22)+" "+th.S(theme.Muted, t.Description), cursor)
	})
	return m.listBox(env, w, h, m.t("sy.mcp.title", i18n.A{"count": len(m.mcp)}), "", m.hintsLine([2]string{"Enter", m.t("sy.hint.open")}), body)
}

func (m *Model) mcpDetail(env ui.Env, w, h int) string {
	th := env.Th
	iw, _ := frame.Inner(w, h)
	c := m.lst[TabMCP].Cursor
	var out []string
	title := m.t("sy.detail")
	if c >= 0 && c < len(m.mcp) {
		t := m.mcp[c]
		title += " · " + t.Name
		out = append(out, th.S(theme.Accent, t.Name), "")
		out = append(out, text.Wrap(t.Description, max(iw-1, 10))...)
	}
	out = append(out, "", th.S(theme.Muted, m.t("sy.mcp.note")))
	return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)))
}

// ---- Ngôn ngữ ----

func (m *Model) languageView(env ui.Env) string {
	th := env.Th
	iw, ih := frame.Inner(env.W, env.H)
	cfgLang := m.cfg.Effective.UI.Language
	out := []string{""}
	kv := func(key, val string) { out = append(out, shared.KV(th, 28, iw-1, m.t("sy.lang.f."+key), val)...) }
	kv("session", m.cat.Lang())
	kv("config", cfgLang)
	out = append(out, "")
	for _, l := range text.Wrap(m.t("sy.lang.order"), max(iw-2, 10)) {
		out = append(out, " "+th.S(theme.Muted, l))
	}
	out = append(out, "")
	for i, l := range config.UILanguages {
		mark := " "
		if l == cfgLang {
			mark = th.S(theme.OK, "✓")
		}
		line := mark + " " + text.Pad(l, 6) + m.t("sy.lang.opt."+l)
		out = append(out, m.cursorMarker(th, line, i == m.lst[TabLanguage].Cursor))
	}
	out = append(out, "")
	for _, l := range text.Wrap(m.t("sy.lang.where"), max(iw-2, 10)) {
		out = append(out, " "+l)
	}
	switch {
	case m.o.ReadOnly:
		out = append(out, "", " "+th.S(theme.Warn, m.t("sy.lang.readonly")))
	case m.cfg.LinkedWorktree:
		out = append(out, "", " "+th.S(theme.Warn, m.t("sy.linked", i18n.A{"main": text.Tilde(m.cfg.Main)})))
	}
	hints := m.hintsLine([2]string{"Enter", m.t("sy.hint.choose")}, [2]string{"L", m.t("sy.hint.session")})
	return frame.Box{Title: m.t("sy.lang.title"), TitleAt: frame.Left, Focused: true, BottomHints: env.AllowedHints(hints), BottomAt: frame.Left}.
		Render(th, env.W, env.H, strings.Join(fitLines(out, iw)[:min(len(out), ih)], "\n"))
}

// ---- Doctor ----

func (m *Model) doctorView(env ui.Env) string {
	return m.split(env,
		func(w int) string { return m.doctorList(env, w, env.H) },
		func(w int) string { return m.doctorDetail(env, w, env.H) })
}

func (m *Model) doctorList(env ui.Env, w, h int) string {
	th := env.Th
	iw, ih := frame.Inner(w, h)
	warn, fail := issuesOf(m.doctor)
	title := m.t("sy.doc.title", i18n.A{"warn": warn, "fail": fail})
	top := ""
	if m.onlyIssues {
		top = th.S(theme.Key, m.t("sy.doc.only"))
	}
	var body string
	if len(m.docIdx) == 0 {
		body = "\n " + th.S(theme.OK, "✓ ") + th.S(theme.Muted, m.t("sy.doc.clean"))
	} else {
		body = m.lst[TabDoctor].Render(iw, ih, func(i int, cursor bool) string {
			c := m.doctor.Checks[m.docIdx[i]]
			g, role := levelRole(c.Level)
			return m.cursorMarker(th, th.S(role, g)+" "+c.Name, cursor)
		})
	}
	hints := m.hintsLine([2]string{"w", m.t("sy.hint.issues")}, [2]string{"Ctrl+R", m.t("sy.hint.refresh")}, [2]string{"Enter", m.t("sy.hint.open")})
	return m.listBox(env, w, h, title, top, hints, body)
}

func (m *Model) doctorDetail(env ui.Env, w, h int) string {
	th := env.Th
	iw, _ := frame.Inner(w, h)
	c := m.lst[TabDoctor].Cursor
	title := m.t("sy.detail")
	var out []string
	if c >= 0 && c < len(m.docIdx) {
		ck := m.doctor.Checks[m.docIdx[c]]
		g, role := levelRole(ck.Level)
		title += " · " + ck.Name
		out = append(out, th.S(role, g)+" "+th.S(theme.Accent, ck.Name)+"  "+th.S(theme.Muted, m.t("sy.doc.level."+string(ck.Level))), "")
		out = append(out, text.Wrap(m.cli(ck.Detail), max(iw-1, 10))...)
	} else {
		out = []string{"", " " + th.S(theme.Muted, m.t("sy.doc.none"))}
	}
	return m.detailBox(env, w, h, title, fitLines(out, max(iw-1, 10)))
}
