package system

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/hcomp"
	"bint/internal/i18n"
	"bint/internal/skills"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/screens/shared"
	"bint/internal/tui/theme"
)

func evolvesvcRegressDefaults() evolvesvc.SkillRegressOptions {
	return evolvesvc.SkillRegressDefaults(config.Default())
}

func coloredDiff(th theme.Theme, diff string) []string {
	return viewer.ColorDiff(th, strings.TrimRight(diff, "\n"))
}

// wrapText ngắt đoạn văn bản thường (nội dung lỗi) theo chiều rộng w.
func wrapText(s string, w int) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, text.Wrap(l, w)...)
	}
	return out
}

// ---- lệnh của từng thẻ ----

// linkedHint báo cấu hình ở worktree chính khi đang ở worktree phụ (mục 7.5).
func (m *Model) linkedHint() bool {
	if m.cfg.LinkedWorktree {
		m.setToast(m.t("sy.linked", i18n.A{"main": text.Tilde(m.cfg.Main)}), true)
		return true
	}
	return false
}

func (m *Model) cmdConfigEdit() tea.Cmd {
	if !m.allowed("system.config_edit") || m.linkedHint() {
		return nil
	}
	m.openForm("system.config_edit", "", actions.ConfigEditOptions{Layer: string(config.LayerProject)})
	return nil
}

func (m *Model) cmdLanguage() tea.Cmd {
	if !m.allowed("system.language_save") || m.linkedHint() {
		return nil
	}
	o := app.UILanguageDefaults(m.cfg.Effective)
	o.Language = config.UILanguages[min(max(m.lst[TabLanguage].Cursor, 0), len(config.UILanguages)-1)]
	m.openForm("system.language_save", "", o)
	return nil
}

func (m *Model) cmdVersion() tea.Cmd {
	c := m.lst[TabHarness].Cursor
	if c < 0 || c >= len(m.har.Infos) {
		return nil
	}
	name := m.har.Infos[c].Name
	if m.verBusy[name] {
		return nil
	}
	m.verBusy[name] = true
	return m.request("system.harness_version", actions.HarnessVersionOptions{Name: name})
}

func (m *Model) cmdComponentAdd() tea.Cmd {
	if m.allowed("system.component_add") {
		m.openForm("system.component_add", "harness component add", app.AddComponentDefaults(config.Default()))
	}
	return nil
}

func (m *Model) cmdRender() tea.Cmd {
	m.openForm("system.render", "harness render", app.RenderDefaults(config.Default()))
	return nil
}

// cmdReview mở hộp duyệt (approve) hoặc loại bỏ (deprecate) thành phần dưới con trỏ. Duyệt script là hộp đỏ.
func (m *Model) cmdReview(approve bool) tea.Cmd {
	d := m.CursorComponent()
	id := "system.component_deprecate"
	if approve {
		id = "system.component_approve"
	}
	if d == nil || !m.allowed(id) {
		return nil
	}
	c := d.Component
	switch {
	case approve && c.Status != hcomp.StatusCandidate:
		m.setToast(m.t("sy.comp.only_candidate"), true)
		return nil
	case !approve && c.Status == hcomp.StatusDeprecated:
		m.setToast(m.t("sy.comp.already_deprecated"), true)
		return nil
	}
	body := []string{fmt.Sprintf("%s %s · %s · %s · %s", c.Kind, c.ID, c.Scope, c.Status, m.t("sy.comp.risk", i18n.A{"risk": d.Row.Risk}))}
	danger := false
	if c.Kind == hcomp.KindScript {
		body = append(body, "", m.th.S(theme.Err, m.t("sy.comp.script.warn")))
		danger = approve
	}
	body = append(body, "")
	body = append(body, firstLines(strings.Split(strings.TrimRight(d.Content, "\n"), "\n"), 14)...)
	title, yes := m.t("sy.comp.approve.title", i18n.A{"id": c.ID}), m.t("sy.comp.approve.yes")
	if !approve {
		title, yes = m.t("sy.comp.deprecate.title", i18n.A{"id": c.ID}), m.t("sy.comp.deprecate.yes")
	}
	o := app.ComponentReviewOptions{ID: c.ID, Kind: c.Kind}
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, Danger: danger, YesLabel: yes, NoLabel: m.t("sy.cancel")},
		onYes: func(string) tea.Cmd { return m.request(id, o) }}
	return nil
}

func firstLines(l []string, n int) []string {
	if len(l) <= n {
		return l
	}
	return append(append([]string{}, l[:n]...), "…")
}

// cursorSkill trả skill dưới con trỏ ở thẻ Skill (nil nếu trống).
func (m *Model) cursorSkill() *app.SkillRow {
	c := m.lst[TabSkills].Cursor
	if c < 0 || c >= len(m.skills.Rows) {
		return nil
	}
	return &m.skills.Rows[c]
}

func (m *Model) cmdSkillShow() tea.Cmd {
	r := m.cursorSkill()
	if r == nil {
		return nil
	}
	return m.request("system.skill_show", app.SkillShowOptions{Name: r.Name, Core: m.coreView && r.Kind == "core"})
}

func (m *Model) cmdSkillOverlay() tea.Cmd {
	r := m.cursorSkill()
	if r == nil || !m.allowed("system.skill_overlay") {
		return nil
	}
	if r.Kind != "core" {
		m.setToast(m.t("sy.skill.overlay.core_only"), true)
		return nil
	}
	return m.request("system.skill_overlay", app.SkillOverlayOptions{Name: r.Name})
}

// cmdSkillNew mở form tạo skill tuỳ biến mẫu (ghi tệp nên không mở ở chế độ chỉ đọc).
func (m *Model) cmdSkillNew() tea.Cmd {
	if m.allowed("system.skill_new") {
		m.openForm("system.skill_new", "skill new", app.SkillNewDefaults(config.Default()))
	}
	return nil
}

// cmdSkillLint kiểm skill tuỳ biến dưới con trỏ, hoặc mọi skill tuỳ biến khi con trỏ không ở skill tuỳ biến.
func (m *Model) cmdSkillLint() tea.Cmd {
	o := app.SkillLintOptions{}
	if r := m.cursorSkill(); r != nil && r.Kind == "custom" {
		o.Name = r.Name
	}
	return m.request("system.skill_lint", o)
}

// lintLines dựng các dòng kết quả kiểm skill tuỳ biến.
func (m *Model) lintLines(v any) []string {
	res, _ := v.([]app.SkillLintResult)
	if len(res) == 0 {
		return []string{m.t("sy.skill.lint.none")}
	}
	var out []string
	for _, r := range res {
		glyph, role := "✓", theme.OK
		if !r.OK() {
			glyph, role = "✗", theme.Err
		}
		out = append(out, m.th.S(role, glyph)+" "+r.Path)
		for _, e := range r.Errors {
			out = append(out, "    "+m.th.S(theme.Err, m.t("sy.skill.lint.err"))+" "+m.cli(e))
		}
		for _, w := range r.Warnings {
			out = append(out, "    "+m.th.S(theme.Warn, m.t("sy.skill.lint.warn"))+" "+m.cli(w))
		}
	}
	return out
}

func (m *Model) cmdSkillForm(id, cli string) tea.Cmd {
	name := ""
	if r := m.cursorSkill(); r != nil && r.Kind == "evolved" {
		name = r.Name
	}
	if id == "system.skill_diff" {
		m.openForm(id, cli, app.SkillDiffOptions{Name: name})
	} else {
		m.openForm(id, cli, evolvesvc.SkillCheckOptions{Target: name})
	}
	return nil
}

func (m *Model) cmdSkillVersions() tea.Cmd {
	r := m.cursorSkill()
	if r == nil {
		return nil
	}
	if r.Kind != "evolved" {
		m.setToast(m.t("sy.skill.evolved_only"), true)
		return nil
	}
	return m.request("system.skill_versions", app.SkillVersionsOptions{Name: r.Name})
}

// cmdSyncPlan xin xem trước sync (chỉ đọc). Sync ghi tệp nên ở chế độ chỉ đọc không mở bản xem trước (không có gì để xác nhận).
func (m *Model) cmdSyncPlan(o app.SyncOptions) tea.Cmd {
	if !m.allowed("system.sync") {
		return nil
	}
	return m.request("system.sync_plan", o)
}

func (m *Model) cmdIndexRebuild() tea.Cmd {
	if !m.allowed("system.index_rebuild") {
		return nil
	}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.index.confirm.title"), Body: wrapText(m.t("sy.index.confirm.body"), 64),
		YesLabel: m.t("sy.index.confirm.yes"), NoLabel: m.t("sy.cancel")},
		onYes: func(string) tea.Cmd { return m.request("system.index_rebuild", app.IndexRebuildOptions{}) }}
	return nil
}

// ---- dòng kết quả ----

func (m *Model) versionLines(v any) []string {
	meta, _ := v.(skills.EvolvedMeta)
	head := fmt.Sprintf("%s — %s, %s", meta.Name, meta.Status, m.t("sy.skill.version_now", i18n.A{"version": meta.Version}))
	if meta.StatusReason != "" {
		head += " (" + meta.StatusReason + ")"
	}
	out := []string{m.th.S(theme.Accent, head), ""}
	for _, e := range meta.Versions {
		ok := "✗"
		if e.Verified {
			ok = "✓"
		}
		out = append(out, fmt.Sprintf("v%d %s %s %s/%s", e.N, ok, shared.Time(e.At), shared.Dash(e.Harness), shared.Dash(e.Model)))
		line := "    " + m.t("sy.skill.ver.detail", i18n.A{"sha": shared.Dash(e.SHA256), "proposal": shared.Dash(e.Proposal), "by": shared.Dash(e.By)})
		out = append(out, line)
		if e.Note != "" {
			out = append(out, "    "+e.Note)
		}
	}
	return out
}

func (m *Model) checkLines(v any) []string {
	res, _ := v.(evolvesvc.SkillCheckResult)
	var out []string
	for _, r := range res.Reports {
		glyph, role := "✓", theme.OK
		if !r.OK {
			glyph, role = "✗", theme.Err
		}
		out = append(out, m.th.S(role, glyph)+" "+m.t("sy.skill.check.skill", i18n.A{"skill": r.Skill}))
		keys := make([]string, 0, len(r.Paths))
		for p := range r.Paths {
			keys = append(keys, p)
		}
		sort.Strings(keys)
		for _, p := range keys {
			out = append(out, "    "+m.t("sy.skill.check.path", i18n.A{"path": p, "count": r.Paths[p]}))
		}
		if r.Positives > 0 {
			out = append(out, "    "+m.t("sy.skill.check.pos", i18n.A{"hit": r.PositiveHits, "total": r.Positives}))
		}
		if r.Negatives > 0 {
			out = append(out, "    "+m.t("sy.skill.check.neg", i18n.A{"hit": r.FalseHits, "total": r.Negatives}))
		}
		if r.Eligible > 0 {
			out = append(out, "    "+m.t("sy.skill.check.act", i18n.A{"hit": r.Activated, "total": r.Eligible}))
		}
		for _, o := range r.Overlaps {
			out = append(out, "    "+m.t("sy.skill.check.overlap", i18n.A{"skill": o}))
		}
		for _, p := range r.Problems {
			out = append(out, "    "+m.th.S(theme.Err, "✗ ")+m.cli(p))
		}
		for _, n := range r.Notes {
			out = append(out, "    · "+m.cli(n))
		}
		out = append(out, "")
	}
	if res.OK {
		out = append(out, m.th.S(theme.OK, m.t("sy.skill.check.ok")))
	} else {
		out = append(out, m.th.S(theme.Err, m.t("sy.skill.check.fail")))
	}
	return out
}

func (m *Model) regressLines(v any) []string {
	res, _ := v.(evolvesvc.SkillRegressResult)
	out := []string{m.t("sy.skill.regress.model", i18n.A{"model": shared.Dash(res.Model)}), ""}
	if len(res.Statuses) == 0 {
		return append(out, m.th.S(theme.Muted, m.t("sy.skill.regress.none")))
	}
	for _, s := range res.Statuses {
		out = append(out, fmt.Sprintf("%s v%d  %s  %s", s.Skill, s.Version, s.Status, shared.Dash(s.Verified)))
		out = append(out, text.WrapHang(m.cli(s.Detail), 72, "    ", "    ")...)
	}
	return out
}

// outcomeGlyph là ký hiệu và vai trò màu của kết quả một tệp skill khi sync.
func outcomeGlyph(r fsafe.Outcome) (string, theme.Role) {
	switch r {
	case fsafe.Conflict:
		return "✗", theme.Err
	case fsafe.Created:
		return "+", theme.OK
	case fsafe.Updated:
		return "~", theme.Key
	case fsafe.Removed:
		return "-", theme.Warn
	}
	return "✓", theme.Muted
}

func (m *Model) syncPlanLines(p app.SyncPlan) []string {
	out := []string{m.t("sy.sync.summary", i18n.A{"changes": p.Changes, "conflicts": p.Conflicts}), ""}
	for _, f := range p.Files {
		g, role := outcomeGlyph(f.Result)
		line := m.th.S(role, g) + " " + f.Path
		if f.Detail != "" {
			line += " — " + m.cli(f.Detail)
		}
		out = append(out, line)
	}
	if p.LockChanged {
		g, role := outcomeGlyph(fsafe.Updated)
		out = append(out, m.th.S(role, g)+" "+skills.LockRel+" — "+m.t("sy.sync.lock"))
	}
	for _, f := range p.Files {
		if f.Diff != "" {
			out = append(out, "")
			out = append(out, coloredDiff(m.th, f.Diff)...)
		}
	}
	if p.LockDiff != "" {
		out = append(out, "")
		out = append(out, coloredDiff(m.th, p.LockDiff)...)
	}
	return out
}

func (m *Model) syncResultLines(si app.SkillInstall) []string {
	if si.NoTargets {
		return []string{m.t("sy.sync.notargets")}
	}
	var out []string
	for _, o := range si.Outcomes {
		g, role := outcomeGlyph(o.Result)
		line := m.th.S(role, g) + " " + o.Skill + ": " + o.Path
		if o.Detail != "" {
			line += " — " + m.cli(o.Detail)
		}
		out = append(out, line)
	}
	return out
}

// issuesOf là số kiểm tra mức cảnh báo và lỗi của báo cáo doctor.
func issuesOf(r *app.DoctorReport) (warn, fail int) {
	if r == nil {
		return 0, 0
	}
	for _, c := range r.Checks {
		switch c.Level {
		case app.LevelWarn:
			warn++
		case app.LevelFail:
			fail++
		}
	}
	return
}

// flowListLines dựng các dòng của `bint flow list`: flow nhúng rồi flow tuỳ biến, kèm trạng thái lint.
func (m *Model) flowListLines(v any) []string {
	rows, _ := v.([]app.FlowRow)
	width := 0
	for _, r := range rows {
		width = max(width, len([]rune(r.Name)))
	}
	var out []string
	for _, r := range rows {
		glyph, role := "✓", theme.OK
		if !r.OK {
			glyph, role = "✗", theme.Err
		}
		src := m.t("sy.flow.source.builtin")
		if r.Source == app.FlowCustom {
			src = r.Path
		}
		out = append(out, fmt.Sprintf("%s %-*s  %s", m.th.S(role, glyph), width, r.Name, m.t("sy.flow.list.row", i18n.A{"source": src, "steps": r.Steps, "desc": r.Description})))
		for _, p := range r.Problems {
			out = append(out, "    "+m.th.S(theme.Err, m.t("sy.skill.lint.err"))+" "+m.cli(p))
		}
	}
	return out
}

// flowShowLines dựng các dòng của `bint flow show`: nguồn, băm, YAML chuẩn hoá, rồi lỗi/cảnh báo lint nếu có.
func (m *Model) flowShowLines(v app.FlowShowResult) []string {
	src := m.t("sy.flow.source.builtin")
	if v.Source == app.FlowCustom {
		src = v.Path
	}
	out := []string{m.t("sy.flow.show.source", i18n.A{"source": src}), m.t("sy.flow.show.hash", i18n.A{"hash": v.Hash}), ""}
	out = append(out, strings.Split(strings.TrimRight(v.YAML, "\n"), "\n")...)
	for _, e := range v.Errors {
		out = append(out, m.th.S(theme.Err, m.t("sy.skill.lint.err"))+" "+m.cli(e))
	}
	for _, w := range v.Warnings {
		out = append(out, m.th.S(theme.Warn, m.t("sy.skill.lint.warn"))+" "+m.cli(w))
	}
	return out
}

// flowLintLines dựng các dòng kết quả kiểm flow.
func (m *Model) flowLintLines(v any) []string {
	res, _ := v.([]app.FlowLintResult)
	if len(res) == 0 {
		return []string{m.t("sy.flow.lint.none")}
	}
	var out []string
	for _, r := range res {
		glyph, role := "✓", theme.OK
		if !r.OK() {
			glyph, role = "✗", theme.Err
		}
		where := r.Path
		if where == "" {
			where = m.t("sy.flow.lint.builtin", i18n.A{"name": r.Name})
		}
		out = append(out, m.th.S(role, glyph)+" "+where)
		for _, e := range r.Errors {
			out = append(out, "    "+m.th.S(theme.Err, m.t("sy.skill.lint.err"))+" "+m.cli(e))
		}
		for _, w := range r.Warnings {
			out = append(out, "    "+m.th.S(theme.Warn, m.t("sy.skill.lint.warn"))+" "+m.cli(w))
		}
	}
	return out
}
