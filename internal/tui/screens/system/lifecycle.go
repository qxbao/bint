package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/confirm"
	"bint/internal/tui/components/form"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Vòng đời của repository và của Bint (giai đoạn 5b, docs/tui-plan.md mục 5.7, 7.5, 7.6): thẻ Cài đặt repo (chạy lại init với form
// điền sẵn trạng thái hiện tại, chỉ gửi phần thay đổi, có xem trước), hộp nâng cấu hình theo mẫu hiện tại (init --update) và thẻ
// Cập nhật Bint (update chạy nền, log go install, hộp Khởi động lại).

// UpdateInfo là thông tin hiện ở thẻ Cập nhật: bản đang chạy và mã nguồn tìm được.
type UpdateInfo struct {
	Version  string
	Revision string
	Exe      string
	Source   string
	// SourceErr: không tìm thấy mã nguồn (lý do hiện cạnh gợi ý chỉ định bằng form).
	SourceErr error
}

func loadUpdateInfo(_ context.Context, _, version string) (UpdateInfo, error) {
	ui := UpdateInfo{Version: version, Revision: app.SelfRevision()}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		ui.Exe = exe
	}
	ui.Source, ui.SourceErr = app.FindSource("")
	return ui, nil
}

// updateState là phiên cập nhật gần nhất của thẻ.
type updateState struct {
	running bool
	done    bool
	out     actions.UpdateOutcome
	err     error
}

// ---- Cài đặt repo ----

// diffInit so Options đã gửi với giá trị khởi đầu của form và chỉ giữ phần đã đổi. harness = true: ô --harness bị đổi (không gửi:
// harness đổi ở thẻ Cấu hình vì app.Init không sửa cấu hình đã có).
func diffInit(init, v app.InitFlagOptions) (ch app.InitFlagOptions, harness bool) {
	hooksOn := v.Hooks && !v.NoHooks
	if hooksOn != init.Hooks {
		ch.Hooks, ch.NoHooks = hooksOn, !hooksOn
	}
	if v.HooksScope != "" && v.HooksScope != init.HooksScope {
		ch.HooksScope = v.HooksScope
		if hooksOn && !ch.Hooks { // đổi phạm vi mà hook đang bật: cài lại ở phạm vi mới (không thì init chỉ "chưa có sự đồng ý")
			ch.Hooks = true
		}
	}
	mcpOn := v.MCP && !v.NoMCP
	if mcpOn != init.MCP {
		ch.MCP, ch.NoMCP = mcpOn, !mcpOn
	}
	ch.AgentsMD = v.AgentsMD && !init.AgentsMD
	ch.Force, ch.NoSkills, ch.Update = v.Force, v.NoSkills, v.Update
	if v.GitMode != init.GitMode {
		ch.GitMode = v.GitMode
	}
	a, b := slices.Clone(init.Harnesses), slices.Clone(v.Harnesses)
	slices.Sort(a)
	slices.Sort(b)
	return ch, !slices.Equal(a, b)
}

func (m *Model) cmdSetupEdit() tea.Cmd {
	if !m.allowed("system.repo_apply") {
		return nil
	}
	if m.setup.NotInit {
		m.setToast(m.t("sy.setup.notinit"), true)
		return nil
	}
	m.setupInit = m.setup.FormDefaults()
	m.openForm("system.repo_apply", "init", m.setupInit)
	return nil
}

// submitSetup xử lý form Cài đặt repo: không đổi gì thì không chạy init; có đổi thì xin kế hoạch xem trước của phần đã đổi.
func (m *Model) submitSetup(v app.InitFlagOptions) tea.Cmd {
	ch, harness := diffInit(m.setupInit, v)
	if harness {
		m.frm.Err = m.t("sy.setup.harness_readonly")
		m.frm.FocusField("harness")
		return nil
	}
	if reflect.DeepEqual(ch, app.InitFlagOptions{}) {
		m.frm = nil
		m.setToast(m.t("sy.setup.nochange"), false)
		return nil
	}
	m.frmBusy = true
	return m.request("system.repo_plan", ch)
}

func (m *Model) onRepoPlan(msg actions.ResultMsg) tea.Cmd {
	m.finishForm("system.repo_apply")
	if msg.Err != nil {
		m.fail(msg.Err)
		return nil
	}
	pl, _ := msg.Value.(app.InitPlan)
	o, _ := msg.Opts.(app.InitFlagOptions)
	body := m.planLines(pl)
	cli := form.CLIFor("init", app.InitDefaults(m.cfg.Effective), o)
	body = append(body, "", m.th.S(theme.Muted, m.t("ui.cli_equivalent")+" ")+cli, m.th.S(theme.Muted, m.t("sy.setup.preview.estimate")))
	title, danger := m.t("sy.setup.preview.title"), false
	if o.Force {
		danger = true
		body = append([]string{m.th.S(theme.Err, m.t("sy.setup.preview.force")), ""}, body...)
	}
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, Danger: danger, YesLabel: m.t("sy.setup.preview.run"), NoLabel: m.t("sy.cancel")},
		onYes: func(string) tea.Cmd { return m.request("system.repo_apply", o) }}
	return nil
}

// planLines dựng các dòng của kế hoạch init: mục đổi trước, mục giữ nguyên sau.
func (m *Model) planLines(pl app.InitPlan) []string {
	icon := map[app.PlanChange]struct {
		s    string
		role theme.Role
	}{app.PlanCreate: {"+", theme.OK}, app.PlanUpdate: {"~", theme.OK}, app.PlanRemove: {"-", theme.Warn}, app.PlanKeep: {"·", theme.Muted}}
	var out, keeps []string
	for _, it := range pl.Items {
		ci := icon[it.Change]
		line := ci.s + " " + m.t("wiz.change."+string(it.Change)) + "  " + text.TildeIn(it.Path)
		if it.Detail != "" {
			line += "  " + m.th.S(theme.Muted, text.TildeIn(it.Detail))
		}
		line = m.th.S(ci.role, ci.s) + line[len(ci.s):]
		if it.Change == app.PlanKeep {
			keeps = append(keeps, line)
		} else {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		out = append(out, m.th.S(theme.Muted, m.t("sy.setup.preview.none")))
	}
	if pl.ConfigDiff != "" {
		out = append(out, "", m.th.S(theme.Accent, m.t("sy.setup.preview.diff")))
		out = append(out, coloredDiff(m.th, pl.ConfigDiff)...)
	}
	return append(append(out, ""), keeps...)
}

func (m *Model) onRepoApply(msg actions.ResultMsg) tea.Cmd {
	if msg.Err != nil {
		m.fail(msg.Err)
		return nil
	}
	rep, _ := msg.Value.(*app.InitReport)
	m.showOut(m.t("sy.setup.applied.title"), m.reportLines(rep))
	m.setToast(m.t("sy.setup.applied", i18n.A{"problem": boolInt(rep != nil && rep.HasProblem)}), rep != nil && rep.HasProblem)
	return tea.Batch(m.reload(TabSetup), m.reload(TabConfig))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// reportLines dựng báo cáo các bước của app.InitReport (dùng cho khung kết quả).
func (m *Model) reportLines(rep *app.InitReport) []string {
	if rep == nil {
		return nil
	}
	icon := map[app.StepStatus]struct {
		s    string
		role theme.Role
	}{app.StepCreated: {"+", theme.OK}, app.StepUpdated: {"~", theme.OK}, app.StepOK: {"✓", theme.OK}, app.StepSkipped: {"-", theme.Muted},
		app.StepWarn: {"!", theme.Warn}, app.StepConflict: {"✗", theme.Err}}
	var out []string
	for _, s := range rep.Steps {
		ci := icon[s.Status]
		l := m.th.S(ci.role, ci.s) + " " + text.TildeIn(s.Name)
		if s.Detail != "" {
			l += "  " + m.th.S(theme.Muted, text.TildeIn(s.Detail))
		}
		out = append(out, l)
	}
	if len(rep.NextSteps) > 0 {
		out = append(out, "", m.th.Bold(m.t("wiz.next")))
		for i, s := range rep.NextSteps {
			out = append(out, string(rune('1'+i))+". "+s)
		}
	}
	return out
}

// ---- nâng cấu hình ----

// OpenConfigUpgrade mở thẻ Cấu hình và hộp nâng mẫu (từ hộp thư của Tổng quan).
func (m *Model) OpenConfigUpgrade() tea.Cmd {
	m.tab, m.pane, m.out = TabConfig, paneList, nil
	return tea.Batch(m.ensureTab(TabConfig), m.cmdConfigUpgrade())
}

func (m *Model) cmdConfigUpgrade() tea.Cmd {
	if !m.allowed("system.config_upgrade") {
		return nil
	}
	return m.request("system.config_upgrade_plan", nil)
}

func (m *Model) infoBox(title string, body []string) {
	m.dlg = &dialog{c: confirm.Model{Title: title, Body: body, YesLabel: "OK", NoLabel: m.t("sy.close")}}
}

func (m *Model) onUpgradePlan(msg actions.ResultMsg) tea.Cmd {
	if msg.Err != nil {
		var le app.LinkedWorktreeError
		if errors.As(msg.Err, &le) { // worktree phụ: không đổi tệp nào, nói rõ chỗ phải làm (mục 7.5)
			m.infoBox(m.t("sy.upgrade.worktree.title"), append(wrapText(ui.ErrText(m.cat, msg.Err), 72), "", m.th.S(theme.Accent, "cd "+le.Main+" && bint")))
			return nil
		}
		m.fail(msg.Err)
		return nil
	}
	pl, _ := msg.Value.(app.ConfigUpgradePlan)
	if !pl.Changed {
		m.setToast(m.t("sy.upgrade.uptodate"), false)
		return nil
	}
	body := []string{m.th.S(theme.Muted, pl.Path), m.t("sy.upgrade.added", i18n.A{"keys": orDash(strings.Join(pl.Added, ", "))})}
	if pl.Fixed > 0 {
		body = append(body, m.t("sy.upgrade.fixed", i18n.A{"count": pl.Fixed}))
	}
	for _, n := range pl.Notes {
		body = append(body, m.th.S(theme.Warn, "! "+n))
	}
	// Lưu ý sao lưu đứng trước diff: diff dài sẽ cuộn, còn dòng này phải đọc được ngay.
	body = append(body, m.th.S(theme.Muted, m.t("sy.upgrade.backup_note")), "")
	body = append(body, coloredDiff(m.th, pl.Diff)...)
	m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.upgrade.title"), Body: body, YesLabel: m.t("sy.upgrade.run"), NoLabel: m.t("sy.upgrade.keep")},
		onYes: func(string) tea.Cmd { return m.request("system.config_upgrade", nil) }}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (m *Model) onUpgradeDone(msg actions.ResultMsg) tea.Cmd {
	if msg.Err != nil {
		m.fail(msg.Err)
		return nil
	}
	rep, _ := msg.Value.(*app.InitReport)
	body := []string{m.t("sy.upgrade.done.body")}
	if bak := app.ConfigBackupPath(rep); bak != "" {
		body = append(body, "", m.t("sy.upgrade.done.backup"), "  "+m.th.S(theme.Accent, text.TildeIn(bak)))
	}
	if rep != nil && rep.HasProblem {
		body = append(body, "", m.th.S(theme.Warn, m.t("sy.upgrade.done.problem")))
	}
	m.infoBox(m.t("sy.upgrade.done.title"), body)
	return tea.Batch(m.reload(TabConfig), m.reload(TabSetup), m.reload(TabDoctor))
}

// ---- cập nhật Bint ----

func (m *Model) cmdUpdate() tea.Cmd {
	if !m.allowed("system.update") || m.upd.running {
		return nil
	}
	m.openForm("system.update", "update", app.UpdateDefaults())
	if m.updInfo.Source != "" {
		m.frm.SetSuggestions("bint_source", []string{m.updInfo.Source})
	}
	return nil
}

func (m *Model) submitUpdate(v app.UpdateOptions) tea.Cmd {
	m.frm, m.frmBusy = nil, false
	m.upd = updateState{running: true}
	return m.request("system.update", v)
}

func (m *Model) onUpdateDone(msg actions.ResultMsg) tea.Cmd {
	out, _ := msg.Value.(actions.UpdateOutcome)
	m.upd = updateState{done: true, out: out, err: msg.Err}
	if msg.Err != nil {
		if errors.Is(msg.Err, context.Canceled) {
			m.setToast(m.t("sy.update.cancelled"), true)
		} else {
			m.setToast(m.t("sy.update.failed"), true)
		}
		return nil
	}
	m.setToast(m.t("sy.update.done"), false)
	r := out.Result
	body := []string{m.t("sy.update.versions", i18n.A{"old": orUnknown(m, r.OldRev), "new": orUnknown(m, r.NewRev)}), m.th.S(theme.Muted, text.TildeIn(r.Target))}
	if r.Mismatch {
		body = append(body, "", m.th.S(theme.Warn, "! "+m.t("sy.update.mismatch", i18n.A{"current": text.TildeIn(r.Current)})))
	}
	if r.OldBackup != "" {
		body = append(body, "", m.t("sy.update.backup", i18n.A{"path": text.TildeIn(r.OldBackup)}))
	}
	if !m.o.CanRestart {
		m.infoBox(m.t("sy.update.reopen.title"), append(body, "", m.t("sy.update.reopen.body")))
		return nil
	}
	body = append(body, "", m.t("sy.update.restart.body"))
	target := r.Target
	m.dlg = &dialog{c: confirm.Model{Title: m.t("sy.update.restart.title"), Body: body, YesLabel: m.t("sy.update.restart.yes"), NoLabel: m.t("sy.update.restart.later")},
		onYes: func(string) tea.Cmd { return restartCmd(target) }}
	return nil
}

func orUnknown(m *Model, s string) string {
	if s == "" {
		return m.t("sy.update.unknown")
	}
	return s
}

// restartCmd gửi yêu cầu khởi động lại cho mô hình gốc (không qua m.request: không có ResultMsg để đếm).
func restartCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return actions.RequestMsg{ID: "system.restart", Opts: actions.RestartOptions{Path: path}}
	}
}

// cmdRestart mở lại hộp Khởi động lại khi đã cập nhật xong mà chưa khởi động lại (phím R ở thẻ Cập nhật).
func (m *Model) cmdRestart() tea.Cmd {
	if !m.upd.done || m.upd.err != nil || m.upd.out.Result.Target == "" {
		return nil
	}
	if !m.o.CanRestart {
		m.infoBox(m.t("sy.update.reopen.title"), []string{m.t("sy.update.reopen.body")})
		return nil
	}
	return restartCmd(m.upd.out.Result.Target)
}
