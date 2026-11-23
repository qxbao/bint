package knowledge

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/i18n"
	"bint/internal/inspect"
	"bint/internal/kindex"
	kn "bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/input"
	"bint/internal/tui/components/text"
	"bint/internal/tui/components/viewer"
	"bint/internal/tui/theme"
)

// panel là một thẻ phụ: tuỳ chọn một form (tham số của hành động) và vùng kết quả cuộn được.
type panel struct {
	action   string // ID hành động chạy khi gửi
	formID   string // không gian tên nhãn của form
	cli      string
	defaults any
	form     *form.Model // nil nếu hành động không có ô nhập nào
	auto     bool        // tự chạy lần đầu khi mở thẻ
	ran      bool
	inForm   bool
	running  bool
	err      error
	lines    []string // kết quả đã dựng (có thể mang mã màu)
	view     viewer.Model
	// Xuất bản: diff đã xem ở bước chạy thử, để cảnh báo nếu lúc ghi khác đi.
	lastDiff    string
	lastChanged bool // bản xem trước (hoặc lần ghi bị từ chối gần nhất) có thay đổi để ghi
	lastOpts    any
	// Nhập: băm kế hoạch đã xem và số mục sẽ tạo; bước ghi chỉ chạy khi kế hoạch lúc ghi trùng băm này.
	lastHash   string
	lastCreate int
	// Nhận định: báo cáo gần nhất của thẻ Nhận định (để đếm mục áp được khi xác nhận).
	lastVerdicts app.VerdictsReport
}

func inputOf(s string) *input.Model { i := input.New(s); return &i }

func newPanel(action, formID, cli string, defaults any, auto bool) *panel {
	p := &panel{action: action, formID: formID, cli: cli, defaults: defaults, auto: auto}
	if len(form.Fields(defaults)) > 0 {
		f := form.New(formID, cli, defaults)
		p.form = &f
		p.inForm = true
	}
	return p
}

func (m *Model) initPanels() {
	cfg := config.Default()
	m.pnls[subSearch] = newPanel("knowledge.search", "knowledge.search", "knowledge search", app.SearchDefaults(cfg), false)
	m.pnls[subStats] = newPanel("knowledge.stats", "knowledge.stats", "knowledge stats", app.StatsDefaults(cfg), true)
	m.pnls[subDiff] = newPanel("knowledge.diff", "knowledge.diff", "knowledge diff", app.DiffDefaults(cfg), true)
	m.pnls[subDiff].inForm = false
	m.pnls[subRevalidate] = newPanel("knowledge.revalidate", "knowledge.revalidate", "knowledge revalidate", app.RevalidateDefaults(cfg), false)
	m.pnls[subVerdicts] = newPanel("knowledge.verdicts", "knowledge.verdicts", "knowledge verdicts", app.VerdictsDefaults(cfg), true)
	m.pnls[subVerdicts].inForm = false
	m.pnls[subPublish] = newPanel("bootstrap.publish_preview", "bootstrap.publish", "bootstrap publish", app.PublishDefaults(cfg, "."), false)
	m.pnls[subImport] = newPanel("knowledge.import_preview", "knowledge.import", "knowledge import", app.ImportDefaults(cfg), false)
	m.pnls[subContext] = newPanel("context", "context", "context", app.ContextDefaults(cfg), false)
	m.surv = [3]*panel{
		newPanel("inspect", "inspect", "inspect", app.InspectDefaults(cfg), false),
		newPanel("bootstrap", "bootstrap", "bootstrap", app.BootstrapDefaults(cfg), false),
		newPanel("bootstrap.skill", "bootstrap.skill", "bootstrap skill", app.BootstrapSkillDefaults(cfg), false),
	}
	m.pnls[subSurvey] = m.surv[0]
	for _, p := range m.allPanels() {
		if p.form != nil {
			p.form.SetSuggestions("files", nil)
		}
	}
}

func (m *Model) allPanels() []*panel {
	out := append([]*panel{}, m.surv[:]...)
	for _, p := range m.pnls {
		if p != nil && p != m.surv[0] {
			out = append(out, p)
		}
	}
	return out
}

// cur là thẻ phụ đang chọn (nil ở danh sách).
func (m *Model) cur() *panel {
	if m.sub == subList {
		return &panel{}
	}
	if m.sub == subSurvey {
		return m.surv[m.survM]
	}
	return m.pnls[m.sub]
}

func (m *Model) panelFor(id string) *panel {
	for _, p := range m.allPanels() {
		if p.action == id || (id == "bootstrap.publish" && p.action == "bootstrap.publish_preview") ||
			(id == "knowledge.import" && p.action == "knowledge.import_preview") ||
			(id == "knowledge.verdicts_apply" && p.action == "knowledge.verdicts") {
			return p
		}
	}
	return nil
}

// WantsInterrupt: có việc của thẻ phụ đang chạy; Esc/Ctrl+C huỷ việc đó thay vì thoát.
func (m *Model) WantsInterrupt() bool { return m.sub != subList && m.cur().running }

// autoRun chạy thẻ chỉ-đọc không cần ô nhập (Thống kê, Thay đổi) khi mở lần đầu.
func (m *Model) autoRun() tea.Cmd {
	if m.sub == subList {
		return m.wantDetail()
	}
	if p := m.cur(); p.auto && !p.ran && !p.running {
		return m.runPanel(p, p.defaults)
	}
	return nil
}

func (m *Model) runPanel(p *panel, opts any) tea.Cmd {
	if !m.allowed(p.action) {
		return nil
	}
	p.running, p.ran, p.err, p.inForm = true, true, nil, false
	if p.action == "knowledge.import_preview" {
		p.lastHash, p.lastCreate, p.lastOpts = "", 0, nil
	}
	if p.action == "bootstrap.publish_preview" || p.action == "knowledge.import_preview" {
		p.lastOpts = opts
	}
	return request(p.action, opts)
}

func (m *Model) panelKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.cur()
	s := msg.String()
	if p.running {
		if s == "esc" || s == "ctrl+c" {
			return func() tea.Msg { return actions.CancelMsg{ID: p.action} }, true
		}
		return nil, s != "q" && s != "?" && s != ":" && s != "L"
	}
	if p.inForm {
		switch p.form.Update(msg) {
		case form.Cancel:
			p.inForm = false
		case form.Copy:
			return tea.SetClipboard(p.form.CLI()), true
		case form.Submit:
			return m.submitPanel(p), true
		}
		return nil, true
	}
	switch s {
	case "enter", "i":
		if p.form != nil {
			if p.action == "knowledge.import_preview" {
				p.lastHash, p.lastCreate, p.lastOpts = "", 0, nil
				p.lines = nil
				p.view.SetLines(nil)
			}
			p.inForm = true
			return nil, true
		}
		return m.runPanel(p, p.defaults), true
	case "r", "ctrl+r", "f5":
		if p.form != nil && p.ran {
			return m.runPanel(p, p.form.Value()), true
		}
		return m.runPanel(p, p.defaults), true
	case "esc":
		m.sub = subList
		return m.wantDetail(), true
	case "j", "down":
		p.view.Scroll(1)
	case "k", "up":
		p.view.Scroll(-1)
	case "pgdown", "ctrl+d", " ", "space":
		p.view.Page(1)
	case "pgup", "ctrl+u":
		p.view.Page(-1)
	case "g", "home":
		p.view.Home()
	case "G", "end":
		p.view.End()
	case "m":
		if m.sub == subSurvey {
			m.survM = (m.survM + 1) % len(m.surv)
			m.pnls[subSurvey] = m.surv[m.survM]
			return nil, true
		}
		return nil, false
	case "w", "V":
		switch m.sub {
		case subPublish:
			return m.confirmPublish(p), true
		case subVerdicts:
			return m.confirmVerdicts(p), true
		case subImport:
			return m.confirmImport(p), true
		}
		return nil, false
	default:
		return nil, false
	}
	return nil, true
}

// submitPanel gửi form của thẻ; Thẩm định lại hỏi xác nhận (tốn tiền) trước.
func (m *Model) submitPanel(p *panel) tea.Cmd {
	v := p.form.Value()
	if ro, ok := v.(app.RevalidateOptions); ok {
		if !m.allowed(p.action) {
			return nil
		}
		body := []string{m.t("kn.revalidate.confirm", i18n.A{"max": ro.MaxItems, "budget": fmt.Sprintf("%.2f", ro.BudgetUSD)})}
		if ro.BudgetUSD <= 0 && ro.AgentCommand == "" {
			body = append(body, "", m.th.S(theme.Warn, m.t("err.no_budget")))
		}
		p.inForm = false
		m.dlg = &dialog{c: confirm.Model{Title: m.t("kn.revalidate.title"), Body: body, Danger: true,
			YesLabel: m.t("kn.dialog.confirm"), NoLabel: m.t("kn.dialog.cancel")},
			onYes: func(string) tea.Cmd { return m.runPanel(p, v) }}
		return nil
	}
	return m.runPanel(p, v)
}

// confirmPublish mở hộp xác nhận ghi AGENTS.md sau khi người dùng đã xem diff (bước hai).
func (m *Model) confirmPublish(p *panel) tea.Cmd {
	if !m.allowed("bootstrap.publish") {
		return nil
	}
	opts, ok := p.lastOpts.(app.PublishOptions)
	if !ok || !p.ran {
		m.setToast(m.t("kn.publish.preview_first"), true)
		return nil
	}
	if !p.lastChanged {
		m.setToast(m.t("kn.publish.nothing_to_write"), true)
		return nil
	}
	// Chỉ ghi khi bản thay đổi lúc ghi đúng bằng bản đã xem; khác thì PublishAgents không ghi và trả diff mới.
	seen := p.lastDiff
	opts.Confirm = func(req app.ConfirmRequest) bool { return req.Detail == seen }
	body := []string{m.t("kn.publish.confirm")}
	if opts.Force {
		body = append(body, "", m.th.S(theme.Err, m.t("kn.publish.force_warning")))
	}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("kn.publish.title"), Body: body, Danger: opts.Force,
		YesLabel: m.t("kn.publish.write"), NoLabel: m.t("kn.dialog.cancel")},
		onYes: func(string) tea.Cmd {
			p.running, p.err = true, nil
			return request("bootstrap.publish", opts)
		}}
	return nil
}

// confirmImport mở hộp xác nhận ghi các mục nhập sau khi người dùng đã xem trước (bước hai). Ghi chỉ chạy khi kế hoạch dựng lại
// lúc ghi có băm trùng bản đã xem (Expect); lệch thì dịch vụ không ghi và màn hình hiện kế hoạch mới.
func (m *Model) confirmImport(p *panel) tea.Cmd {
	if !m.allowed("knowledge.import") {
		return nil
	}
	opts, ok := p.lastOpts.(app.ImportOptions)
	if !ok || !p.ran || p.lastHash == "" || p.err != nil {
		m.setToast(m.t("kn.import.preview_first"), true)
		return nil
	}
	if p.lastCreate == 0 {
		m.setToast(m.t("kn.import.nothing_to_write"), true)
		return nil
	}
	opts.Expect, opts.DryRun = p.lastHash, false
	body := []string{m.t("kn.import.confirm", i18n.A{"count": p.lastCreate}), "", m.th.S(theme.Muted, m.t("kn.import.confirm_note"))}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("kn.import.title"), Body: body,
		YesLabel: m.t("kn.import.write"), NoLabel: m.t("kn.dialog.cancel")},
		onYes: func(string) tea.Cmd {
			p.running, p.err = true, nil
			return request("knowledge.import", opts)
		}}
	return nil
}

// panelResult dựng kết quả của một thẻ phụ từ ResultMsg.
func (m *Model) panelResult(p *panel, r actions.ResultMsg) tea.Cmd {
	p.running = false
	if r.Err != nil {
		p.err = r.Err
		p.lines = nil
		return nil
	}
	th := m.th
	var l []string
	reload := false
	add := func(s ...string) { l = append(l, s...) }
	switch v := r.Value.(type) {
	case []kindex.Hit:
		if len(v) == 0 {
			add(th.S(theme.Muted, m.t("kn.search.none")))
		}
		for _, h := range v {
			add(fmt.Sprintf("%s  %-18s %5.2f  %s", short(h.ID), h.Type+"/"+h.Status, h.Score, h.Title))
		}
	case []kindex.Stats:
		add(th.S(theme.Accent, fmt.Sprintf("%-10s %-18s %6s %6s %5s  %s", "ID", m.t("kn.stats.col.type"), m.t("kn.stats.col.retrieved"), m.t("kn.stats.col.cited"), m.t("kn.stats.col.runs"), m.t("kn.stats.col.title"))))
		for _, s := range v {
			add(fmt.Sprintf("%-10s %-18s %6d %6d %5d  %s", short(s.ID), s.Type+"/"+s.Status, s.Retrieved, s.Cited, s.Runs, s.Title))
		}
		add("", th.S(theme.Muted, m.t("kn.stats.note")))
	case app.DiffReport:
		if len(v.Entries) == 0 {
			add(th.S(theme.Muted, m.t("kn.diff.none", i18n.A{"range": v.Range})))
		} else {
			add(th.Bold(m.t("kn.diff.head", i18n.A{"range": v.Range})), "")
		}
		for _, e := range v.Entries {
			icon := map[string]string{"added": "+", "modified": "~", "deleted": "-"}[e.Change]
			meta, title := "", ""
			if e.Item != nil {
				title = e.Item.Title
				meta = " [" + e.Item.Type + "/" + e.Item.Status + ", " + e.Item.Origin.Kind + "]"
				if e.Item.Origin.ThirdParty {
					meta += " ⚑"
				}
			}
			add(icon+" "+short(e.ID)+meta+" "+title, "    "+th.S(theme.Muted, m.t("kn.diff.by", i18n.A{"authors": strings.Join(e.Authors, ", ")})))
		}
	case app.VerdictsReport:
		p.lastVerdicts = v
		if len(v.Rows) == 0 && len(v.Applied) == 0 {
			add(th.S(theme.Muted, m.t("kn.verdicts.none")))
		}
		vicon := map[string]string{"valid": th.S(theme.OK, "✓"), "outdated": th.S(theme.Err, "✗"), "fix-evidence": th.S(theme.Warn, "~"), "unsure": th.S(theme.Warn, "?")}
		for _, a := range v.Applied {
			mark := th.S(theme.Muted, "-")
			if a.Applied {
				mark = th.S(theme.OK, "✓")
			}
			add(mark + " " + short(a.ID) + "  " + a.Verdict + ": " + m.cli(a.Detail))
		}
		if len(v.Applied) > 0 {
			add("")
			reload = true
		}
		for _, r := range v.Rows {
			note := ""
			switch {
			case r.Stale:
				note = " [" + m.t("kn.verdicts.old") + "]"
			case !r.Actionable:
				note = " [" + m.t("kn.verdicts.noapply") + "]"
			}
			who := r.Harness
			if r.Model != "" {
				who += "/" + r.Model
			}
			add(vicon[r.Verdict] + " " + short(r.ID) + " " + r.Verdict + note + " — " + r.Reason + th.S(theme.Muted, " ("+who+")"))
			if r.Evidence != nil {
				add("    " + th.S(theme.Muted, m.t("kn.verdicts.proposed")+" "+r.Evidence.Path+":"+r.Evidence.Lines+" "+r.Evidence.Symbol))
			}
		}
		add("", th.S(theme.Muted, m.t("kn.verdicts.hint")))
	case app.RevalidateReport:
		if len(v.Results) == 0 && len(v.Skipped) == 0 {
			add(th.S(theme.Muted, m.t("kn.revalidate.none")))
		}
		icon := map[string]string{"valid": th.S(theme.OK, "✓"), "outdated": th.S(theme.Err, "✗"), "fix-evidence": th.S(theme.Warn, "~"), "unsure": th.S(theme.Warn, "?")}
		for _, rv := range v.Results {
			if rv.Blocked != "" {
				add(th.S(theme.Warn, "! ") + short(rv.ID) + " " + m.t("kn.revalidate.blocked") + ": " + m.cli(rv.Blocked))
				continue
			}
			add(icon[rv.Verdict] + " " + short(rv.ID) + "  " + rv.Verdict + " — " + rv.Reason)
			if rv.Evidence != nil {
				add("    " + th.S(theme.Muted, m.t("kn.verdicts.proposed")+" "+rv.Evidence.Path+":"+rv.Evidence.Lines+" "+rv.Evidence.Symbol))
			}
		}
		for _, id := range v.Skipped {
			add(th.S(theme.Muted, "- "+short(id)+"  "+m.t("kn.revalidate.skipped")))
		}
		if v.StopReason != "" {
			add("", th.S(theme.Warn, m.t("kn.revalidate.stopped", i18n.A{"reason": v.StopReason})))
		}
		add("", m.t("kn.revalidate.spent", i18n.A{"usd": fmt.Sprintf("%.4f", v.SpentUSD)}), th.S(theme.Muted, m.t("kn.revalidate.hint")))
	case app.PublishReport:
		l = m.publishLines(p, r, v)
	case app.ImportResult:
		var focus tea.Cmd
		l, focus = m.importLines(p, r, v)
		p.lines = l
		p.view.SetLines(l)
		p.view.Reset()
		return focus
	case *inspect.Report:
		l = m.inspectLines(v)
	case *app.BootstrapReport:
		l = m.bootstrapLines(v)
		reload = len(v.Items) > 0 // nạp lại danh sách, nhưng SAU khi đã gán kết quả cho khung
	case []byte:
		l = strings.Split(strings.TrimRight(string(v), "\n"), "\n")
	case app.ContextResult:
		l = m.contextLines(v)
	}
	p.lines = l
	p.view.SetLines(l)
	p.view.Reset()
	if reload {
		return m.Load()
	}
	return nil
}

func (m *Model) publishLines(p *panel, r actions.ResultMsg, rep app.PublishReport) []string {
	th := m.th
	var l []string
	real := r.ID == "bootstrap.publish"
	if real && !rep.Written && rep.Diff != p.lastDiff {
		l = append(l, th.S(theme.Warn, "! "+m.t("kn.publish.diff_changed")), "")
	}
	if rep.Diff != "" {
		l = append(l, viewer.ColorDiff(th, rep.Diff)...)
		l = append(l, "")
	}
	// Diff vừa hiện là bản để xem lại: lần ghi kế tiếp so với nó. Ghi xong thì không còn gì để ghi.
	p.lastDiff, p.lastChanged = rep.Diff, rep.Changed && !rep.Written
	sel := rep.Selection
	l = append(l, th.Bold(m.t("kn.publish.block", i18n.A{"rules": len(sel.Rules), "warnings": len(sel.Warnings)})))
	for _, o := range sel.Omitted {
		l = append(l, th.S(theme.Muted, "  - "+m.t("kn.publish.omitted", i18n.A{"id": short(o.ID), "reason": m.cli(o.Reason)})))
	}
	for _, s := range rep.Steps {
		l = append(l, "  "+stepIcon(s.Status)+" "+s.Name+" "+m.cli(s.Detail))
	}
	switch {
	case real && rep.Written:
		l = append(l, "", th.S(theme.OK, "✓ "+m.t("kn.publish.written")))
	case rep.Changed:
		l = append(l, "", th.S(theme.Key, m.t("kn.publish.press_w")))
	default:
		l = append(l, "", th.S(theme.Muted, m.t("kn.publish.unchanged")))
	}
	return l
}

// importLines dựng kết quả của thẻ Nhập. Ghi xong thì chuyển sang danh sách lọc theo lô vừa nhập để duyệt.
func (m *Model) importLines(p *panel, r actions.ResultMsg, res app.ImportResult) ([]string, tea.Cmd) {
	th := m.th
	plan := res.Plan
	real := r.ID == "knowledge.import"
	var l []string
	if res.Mismatch {
		l = append(l, th.S(theme.Warn, "! "+m.t("kn.import.plan_changed")), "")
	}
	l = append(l, th.Bold(m.t("kn.import.head", i18n.A{"source": plan.Source, "files": plan.Files, "ideas": len(plan.Ideas)})), "")
	for _, i := range plan.Ideas {
		if i.Outcome == app.ImportCreate {
			id := ""
			if i.ID != "" {
				id = " → " + short(i.ID)
			}
			l = append(l, th.S(theme.OK, "+ ")+i.Title+th.S(theme.Muted, "  ("+i.Ref+")")+id)
			l = append(l, "    "+th.S(theme.Muted, m.t("kn.import.evidence")+": "+strings.Join(i.Evidence, ", ")))
			continue
		}
		l = append(l, th.S(theme.Muted, "- "+i.Title+"  ("+i.Ref+")"))
		l = append(l, "    "+th.S(theme.Warn, m.reasonText(i.Reason))+th.S(theme.Muted, " — "+m.cli(i.Detail)))
	}
	create := plan.Count(app.ImportCreate)
	// Kế hoạch vừa hiện là bản để xem lại: lần ghi kế tiếp so với băm của nó. Ghi xong thì không còn gì để ghi.
	p.lastHash, p.lastCreate = plan.Hash, create
	if res.Written {
		p.lastCreate = 0
	}
	var focus tea.Cmd
	switch {
	case real && res.Written:
		l = append(l, "", th.S(theme.OK, "✓ "+m.t("kn.import.written", i18n.A{"count": len(res.IDs), "batch": plan.Batch})))
		m.setToast(m.t("kn.import.written", i18n.A{"count": len(res.IDs), "batch": plan.Batch}), false)
		focus = m.Focus(kn.Filter{Batch: plan.Batch, Statuses: []string{kn.StatusCandidate, kn.StatusStale}})
	case create > 0:
		l = append(l, "", th.S(theme.Key, m.t("kn.import.press_w", i18n.A{"count": create})))
	default:
		l = append(l, "", th.S(theme.Muted, m.t("kn.import.none")))
	}
	return l, focus
}

// reasonText là nhãn của lý do bỏ qua một ý nhập; khoá liệt kê tường minh để kiểm tra khoá i18n thấy được chúng.
func (m *Model) reasonText(reason string) string {
	keys := map[string]string{
		"no_evidence": "kn.import.reason.no_evidence", "duplicate": "kn.import.reason.duplicate",
		"already_imported": "kn.import.reason.already_imported", "work_state": "kn.import.reason.work_state",
		"private": "kn.import.reason.private", "bint_derived": "kn.import.reason.bint_derived",
		"index_file": "kn.import.reason.index_file", "check_failed": "kn.import.reason.check_failed",
	}
	if k, ok := keys[reason]; ok {
		return m.t(k)
	}
	return reason
}

func stepIcon(s app.StepStatus) string {
	switch s {
	case app.StepConflict:
		return "✗"
	case app.StepCreated:
		return "+"
	case app.StepUpdated:
		return "~"
	}
	return "·"
}

func (m *Model) inspectLines(r *inspect.Report) []string {
	th := m.th
	row := func(k, v string) string { return th.S(theme.Accent, text.Pad(m.t("kn.insp."+k), 14)) + v }
	head := fmt.Sprintf("%s (%d)", r.Root, r.Files)
	if r.Head != "" {
		head += ", HEAD " + short(r.Head)
	}
	l := []string{row("repo", head)}
	var langs []string
	for i, x := range r.Languages {
		if i == 8 {
			break
		}
		langs = append(langs, fmt.Sprintf("%s %d", x.Name, x.Files))
	}
	if len(langs) > 0 {
		l = append(l, row("lang", strings.Join(langs, ", ")))
	}
	for _, mf := range r.Manifests {
		l = append(l, row("manifest", fmt.Sprintf("%s (%d)", mf.Path, len(mf.Dependencies)+len(mf.DevDeps))))
	}
	seen := map[string]bool{}
	var tools []string
	for _, t := range r.Tools {
		if !seen[t.Name] {
			seen[t.Name] = true
			tools = append(tools, t.Name)
		}
	}
	if len(tools) > 0 {
		l = append(l, row("tools", strings.Join(tools, ", ")))
	}
	for i, c := range r.Commands {
		if i == 15 {
			l = append(l, "  "+th.S(theme.Muted, m.t("kn.insp.more", i18n.A{"count": len(r.Commands) - 15})))
			break
		}
		l = append(l, row("command", c.Name+"  "+text.Truncate(c.Command, 60)+"  "+th.S(theme.Muted, c.Source)))
	}
	if len(r.Docs) > 0 {
		var d []string
		for _, x := range r.Docs {
			d = append(d, x.Path)
		}
		l = append(l, row("docs", strings.Join(d, ", ")))
	}
	for _, g := range r.Generated {
		l = append(l, row("generated", fmt.Sprintf("%s (%d)", g.Dir, g.Files)))
	}
	g := r.Git
	l = append(l, row("git", fmt.Sprintf("%d commit, %d %s", g.Commits, g.Contributors, m.t("kn.insp.contributors"))))
	if len(g.FixHotspots) > 0 {
		var h []string
		for i, f := range g.FixHotspots {
			if i == 6 {
				break
			}
			h = append(h, fmt.Sprintf("%s (%d)", f.Path, f.Commits))
		}
		l = append(l, row("hotspots", strings.Join(h, ", ")))
	}
	for _, n := range r.Notes {
		l = append(l, th.S(theme.Warn, "! "+m.cli(n)))
	}
	return l
}

func (m *Model) bootstrapLines(r *app.BootstrapReport) []string {
	th := m.th
	var l []string
	if r.Inspect != nil {
		l = m.inspectLines(r.Inspect)
	}
	l = append(l, "")
	if r.InspectPath != "" {
		l = append(l, th.S(theme.OK, "✓ ")+m.t("kn.boot.saved", i18n.A{"path": r.InspectPath}))
	}
	switch {
	case !r.Initialized:
		l = append(l, th.S(theme.Warn, "! "+m.t("kn.boot.not_init")))
	case r.Skills != nil && r.Skills.NoTargets:
		l = append(l, th.S(theme.Warn, "! "+m.t("kn.boot.no_targets")))
	case r.Skills != nil:
		for _, o := range r.Skills.Outcomes {
			icon := "✓"
			switch o.Result {
			case fsafe.Conflict:
				icon = th.S(theme.Err, "✗")
			case fsafe.Created:
				icon = "+"
			case fsafe.Updated:
				icon = "~"
			}
			kind := "skill"
			if o.Agent {
				kind = "subagent"
			}
			l = append(l, fmt.Sprintf("%s %s %s: %s", icon, kind, o.Skill, o.Path))
		}
	}
	l = append(l, "", m.t("kn.boot.counts", i18n.A{"verified": r.Counts["verified"], "candidate": r.Counts["candidate"], "stale": r.Counts["stale"]}),
		"", th.S(theme.Accent, m.t("kn.boot.next")), "  1. "+m.t("kn.boot.next1"), "  2. "+m.t("kn.boot.next2"), "  3. "+m.t("kn.boot.next3"))
	return l
}

func (m *Model) contextLines(res app.ContextResult) []string {
	th := m.th
	p := res.Pack
	l := []string{th.Bold(m.t("kn.ctx.head", i18n.A{"tokens": p.Tokens, "budget": p.Budget, "count": len(p.Entries)}))}
	if p.WorkKind != "" {
		l[0] += th.S(theme.Muted, " · "+m.t("kn.ctx.kind", i18n.A{"kind": p.WorkKind}))
	}
	for _, e := range p.Entries {
		l = append(l, th.S(theme.OK, "+ ")+short(e.ID)+" ["+e.Label+"] "+fmt.Sprintf("%.2f", e.Score)+" "+th.S(theme.Muted, strings.Join(e.Reasons, " ")))
	}
	for _, o := range p.Omitted {
		l = append(l, th.S(theme.Warn, "- ")+short(o.ID)+": "+th.S(theme.Muted, m.cli(o.Reason)))
	}
	for _, w := range res.Warnings {
		l = append(l, th.S(theme.Warn, "! "+m.cli(w)))
	}
	l = append(l, "", th.S(theme.Accent, "── "+m.t("kn.ctx.pack")+" ──"))
	if len(p.Entries) == 0 && len(p.Lessons) == 0 {
		l = append(l, th.S(theme.Muted, m.t("kn.ctx.none")))
	} else {
		l = append(l, strings.Split(strings.TrimRight(p.Render(), "\n"), "\n")...)
	}
	return l
}

// confirmVerdicts mở hộp xác nhận áp nhận định của agent theo lô (chỉ người).
func (m *Model) confirmVerdicts(p *panel) tea.Cmd {
	if !m.allowed("knowledge.verdicts_apply") {
		return nil
	}
	rep, ok := p.lastVerdicts, p.ran
	n := 0
	for _, r := range rep.Rows {
		if r.Actionable {
			n++
		}
	}
	if !ok || n == 0 {
		m.setToast(m.t("kn.verdicts.nothing"), true)
		return nil
	}
	body := []string{m.t("kn.verdicts.confirm", i18n.A{"count": n}), "", m.th.S(theme.Muted, m.t("kn.verdicts.confirm_note"))}
	m.dlg = &dialog{c: confirm.Model{Title: m.t("kn.verdicts.title"), Body: body, Danger: true,
		YesLabel: m.t("kn.verdicts.apply"), NoLabel: m.t("kn.dialog.cancel")},
		onYes: func(string) tea.Cmd {
			p.running, p.err = true, nil
			return request("knowledge.verdicts_apply", app.VerdictsOptions{})
		}}
	return nil
}
