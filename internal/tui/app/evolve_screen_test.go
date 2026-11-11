package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/hcomp"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
	evscreen "bint/internal/tui/screens/evolve"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// goProposals đưa màn Tiến hoá sang thẻ Đề xuất (con trỏ ở đề xuất đầu).
func goProposals(t *testing.T, m *Model) { t.Helper(); press(t, m, "6", ".") }

// drv chạy lệnh của mô hình trong goroutine (việc nền chặn trong jobs.Wait) và đưa thông điệp về vòng Update.
type drv struct {
	t    *testing.T
	m    *Model
	msgs chan tea.Msg
}

func newDrv(t *testing.T, m *Model) *drv { return &drv{t: t, m: m, msgs: make(chan tea.Msg, 256)} }

func (d *drv) run(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			for _, x := range msg {
				d.run(x)
			}
		default:
			d.msgs <- msg
		}
	}()
}

// settle chạy cmd và nhận các thông điệp tức thời (không chờ việc nền).
func (d *drv) settle(cmd tea.Cmd) {
	d.run(cmd)
	end := time.After(250 * time.Millisecond)
	for {
		select {
		case msg := <-d.msgs:
			_, next := d.m.Update(msg)
			d.run(next)
		case <-time.After(40 * time.Millisecond):
			return
		case <-end:
			return
		}
	}
}

func (d *drv) press(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		_, cmd := d.m.Update(keyMsg(k))
		d.settle(cmd)
	}
}

func (d *drv) typeText(s string) {
	for _, r := range s {
		d.press(string(r))
	}
}

// until nhận thông điệp tới khi pred đúng (tối đa 10 giây).
func (d *drv) until(pred func() bool) bool {
	d.t.Helper()
	deadline := time.After(10 * time.Second)
	for !pred() {
		select {
		case msg := <-d.msgs:
			_, next := d.m.Update(msg)
			d.run(next)
		case <-deadline:
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
	return true
}

func TestEvolveOpportunitiesTabAndProposeForm(t *testing.T) {
	m, c := newCapModel(t, Options{}, "evolve.propose")
	d := newDrv(t, m)
	d.press("6")
	hasAll(t, plain(m.Render()), "Cơ hội (2)", "failure_hotspot", "Điều kiện tiên quyết", "✗ tác vụ đánh giá", "chưa đủ điều kiện", "internal/db", "Cơ hội op-3f9a2c1b", "sẵn sàng")
	d.press("j")
	hasAll(t, plain(m.Render()), "Cơ hội op-7e1d4a90", "deprecate", "chưa: chưa đủ")
	d.press("k", "p")
	hasAll(t, plain(m.Render()), "Cơ hội", "Ngân sách USD", "bint evolve propose op-3f9a2c1b")
	d.press("ctrl+s")
	if !d.until(func() bool {
		return m.jobs.Len() == 1 && strings.Contains(plain(m.Render()), "dòng log đầu của evolve.propose")
	}) {
		t.Fatalf("thẻ Log phải hiện log trực tiếp:\n%s", plain(m.Render()))
	}
	if o := c.last(t, "evolve.propose").(evolvesvc.ProposeOptions); len(o.Refs) != 1 || o.Refs[0] != "op-3f9a2c1b" {
		t.Fatalf("%+v", o)
	}
	hasAll(t, plain(m.Render()), "Đang chạy: evolve propose")
	close(c.release["evolve.propose"])
	if !d.until(func() bool { return m.jobs.Len() == 0 }) {
		t.Fatal("việc phải kết thúc")
	}
	hasAll(t, plain(m.Render()), "Đã xong: evolve propose")
}

func TestEvolveProposalListAndAllToggle(t *testing.T) {
	var qs []evscreen.Query
	m, _ := newCapModel(t, Options{EvolveLoader: func(_ context.Context, _ string, q evscreen.Query, _ bool) (evscreen.Data, error) {
		qs = append(qs, q)
		return sampleEvolve(), nil
	}})
	goProposals(t, m)
	hasAll(t, plain(m.Render()), "Đề xuất (6)", "đang chờ", "00KNOW", "update", "high", "supported", "Thông tin · Thay đổi · Đối chiếu", "Lợi ích", "bớt thất bại lặp lại", "Căn cứ (1 lượt có nhãn)")
	press(t, m, "A")
	if !qs[len(qs)-1].All || !m.ev.ShowAll() {
		t.Fatalf("A phải nạp với mọi trạng thái (evolve list --all): %+v", qs)
	}
	hasAll(t, plain(m.Render()), "mọi trạng thái")
	press(t, m, "A")
	if qs[len(qs)-1].All {
		t.Fatal("A lần nữa phải về đang chờ")
	}
}

// Thẻ con Thay đổi: diff với hiện trạng cho tri thức và skill; khung riêng cho bốn loại thành phần quy trình.
func TestEvolveChangesTabForEveryKind(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goProposals(t, m)
	press(t, m, "v")
	hasAll(t, plain(m.Render()), "[update] 01KNOW000003 — làm rõ", "- Tiêu đề", "+ Tiêu đề", "Handler đi qua wrap() và log", "--- hiện trạng", "+++ đề xuất", "-Dòng thứ hai.", "+Dòng thứ hai đã sửa.")
	press(t, m, "j") // skill
	hasAll(t, plain(m.Render()), "[create] skill db-retry", "Thử lại truy vấn DB", "SKILL.md", "+# db-retry", "+Gọi Reset trước Retry.")
	press(t, m, "j") // guidance
	hasAll(t, plain(m.Render()), "[create] guidance chay-test", "Chạy test trước khi kết thúc", "go test ./... trước khi dừng", "Thành phần mới")
	press(t, m, "j") // guardrail
	hasAll(t, plain(m.Render()), "[create] guardrail khong-sua-migrations", "on=pre_edit", "when=edited:migrations/**", "action=ask", "Sửa migration cần người duyệt.")
	press(t, m, "j") // script
	hasAll(t, plain(m.Render()), "[create] script kiem-tra-nhanh", "Bint không bao giờ tự chạy script này", "go vet ./...", "bash kiem-tra-nhanh.sh", "rủi ro high")
	press(t, m, "j") // strategy
	hasAll(t, plain(m.Render()), "[update] strategy strategy", "strategy.yml")
	press(t, m, "v")
	hasAll(t, plain(m.Render()), "Chưa có lần đối chiếu nào")
}

// Đề xuất loại harness có diff với thành phần hiện có cùng ID.
func TestEvolveHarnessDiffAgainstExistingComponent(t *testing.T) {
	m, _ := newCapModel(t, Options{EvolveDetailer: func(ctx context.Context, d, id string, ro bool) (*evolvesvc.ShowResult, error) {
		r, err := sampleEvolveDetail(ctx, d, id, ro)
		if err == nil {
			r.Harness = []hcomp.Component{{Kind: hcomp.KindGuidance, Meta: hcomp.Meta{ID: "chay-test", Scope: "global", Status: hcomp.StatusVerified, Origin: hcomp.OriginHuman, Version: 1},
				Title: "Chạy test", Body: "Chạy test trước khi dừng.\n"}}
		}
		return r, err
	}})
	goProposals(t, m)
	press(t, m, "v", "j", "j")
	out := plain(m.Render())
	hasAll(t, out, "[create] guidance chay-test", "--- hiện trạng", "+++ đề xuất", "-Chạy test trước khi dừng.")
	if strings.Contains(out, "Thành phần mới") {
		t.Fatalf("đã có thành phần cùng ID thì phải có diff:\n%s", out)
	}
}

func TestEvolveEvaluationsTab(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goProposals(t, m)
	press(t, m, "v", "v")
	hasAll(t, plain(m.Render()), "ee_1", "validation", "2 tác vụ", "supported", "độc lập", "đạt 2/2, không kém base")
}

// ---- hộp áp dụng, bác bỏ, quay lui ----

func applyPlan(m *Model, mod func(*evolvesvc.ApplyPlan)) *evolvesvc.ApplyPlan {
	pl := samplePlanApply(m.ev.Cursor().Proposal)
	if mod != nil {
		mod(pl)
	}
	return pl
}

// Hộp áp dụng dựng từ PlanApply: nêu điều kiện; cờ chỉ được đặt khi người chọn ô trong hộp cảnh báo (đỏ).
func TestEvolveApplyDialogFlags(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goProposals(t, m)
	c.values["evolve.plan_apply"] = applyPlan(m, nil)
	press(t, m, "a")
	open, danger, checks := m.ev.Dialog()
	if !open || !danger || len(checks) != 2 || checks[0] || checks[1] {
		t.Fatalf("hộp đỏ có hai ô chưa chọn: open=%v danger=%v %v", open, danger, checks)
	}
	hasAll(t, plain(m.Render()), "Áp dụng đề xuất 01EP0000KNOW", "CẢNH BÁO", "CHƯA có kết quả supported", "Rủi ro CAO", "--unverified", "--confirm-risk", "Phiên bản gốc còn khớp")
	// Không chọn ô nào: yêu cầu gửi không có cờ (dịch vụ sẽ từ chối nêu rõ cờ cần thiết).
	press(t, m, "enter")
	if o := c.last(t, "evolve.apply").(evolvesvc.ApplyOptions); o.Ref != "01EP0000KNOW" || o.Unverified || o.ConfirmRisk {
		t.Fatalf("không chọn thì không có cờ: %+v", o)
	}
	// Chọn đủ hai ô + ghi chú.
	press(t, m, "a")
	press(t, m, "x", "tab", "x", "tab")
	typeText(t, m, "ok")
	if _, _, checks := m.ev.Dialog(); !checks[0] || !checks[1] {
		t.Fatalf("hai ô phải được chọn: %v", checks)
	}
	press(t, m, "enter")
	if o := c.last(t, "evolve.apply").(evolvesvc.ApplyOptions); !o.Unverified || !o.ConfirmRisk || o.Reason != "ok" {
		t.Fatalf("chọn đủ: %+v", o)
	}
	// Chỉ chọn ô thứ nhất.
	press(t, m, "a", "x", "enter")
	if o := c.last(t, "evolve.apply").(evolvesvc.ApplyOptions); !o.Unverified || o.ConfirmRisk {
		t.Fatalf("chỉ chọn ô đầu: %+v", o)
	}
	// Esc: không gửi gì.
	n := c.count("evolve.apply")
	press(t, m, "a", "esc")
	if c.count("evolve.apply") != n {
		t.Fatal("Esc không được áp dụng")
	}
}

// Đề xuất đã kiểm chứng và rủi ro thấp: hộp thường (không đỏ), không có ô chọn; không thể áp dụng: hộp chỉ đọc.
func TestEvolveApplyDialogPlainAndBlocked(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goProposals(t, m)
	c.values["evolve.plan_apply"] = applyPlan(m, func(p *evolvesvc.ApplyPlan) {
		p.Verified, p.NeedsUnverified, p.NeedsConfirmRisk, p.Risk, p.RiskReasons = true, false, false, evolve.RiskLow, nil
	})
	press(t, m, "a")
	open, danger, checks := m.ev.Dialog()
	if !open || danger || len(checks) != 0 {
		t.Fatalf("hộp thường: %v %v %v", open, danger, checks)
	}
	hasAll(t, plain(m.Render()), "Đã có kết quả supported, độc lập")
	press(t, m, "enter")
	if o := c.last(t, "evolve.apply").(evolvesvc.ApplyOptions); o.Unverified || o.ConfirmRisk {
		t.Fatalf("%+v", o)
	}
	n := c.count("evolve.apply")
	c.values["evolve.plan_apply"] = applyPlan(m, func(p *evolvesvc.ApplyPlan) {
		p.Blocked, p.BlockedDetail = evolvesvc.BlockedBase, "01KNOW000003: mục đã thay đổi"
	})
	press(t, m, "a")
	hasAll(t, plain(m.Render()), "phiên bản gốc đã đổi", "01KNOW000003: mục đã thay đổi")
	press(t, m, "enter")
	if c.count("evolve.apply") != n {
		t.Fatal("hộp bị chặn không được gửi áp dụng")
	}
}

func TestEvolveRejectDialogSendsReason(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goProposals(t, m)
	press(t, m, "x")
	hasAll(t, plain(m.Render()), "Bác bỏ đề xuất 01EP0000KNOW", "Lý do")
	typeText(t, m, "sai")
	press(t, m, "enter")
	if o := c.last(t, "evolve.reject").(evolvesvc.RejectOptions); o.Ref != "01EP0000KNOW" || o.Reason != "sai" {
		t.Fatalf("%+v", o)
	}
}

// a/x/u/E thao tác lên đề xuất dưới con trỏ thẻ Đề xuất: ở thẻ khác (con trỏ đó bị ẩn) không mở hộp/form nào, chỉ
// chuyển sang thẻ Đề xuất và nhắc chọn đề xuất.
func TestEvolveProposalKeysNeedProposalsTab(t *testing.T) {
	m, c := newCapModel(t, Options{})
	for _, k := range []string{"a", "x", "u", "E"} {
		press(t, m, "6")
		for m.ev.Tab() != 0 {
			press(t, m, ",")
		}
		n := c.count("evolve.plan_apply") + c.count("evolve.plan_rollback")
		press(t, m, k)
		if open, _, _ := m.ev.Dialog(); open || m.ev.Capturing() {
			t.Fatalf("%s ở thẻ Cơ hội không được mở hộp/form", k)
		}
		if c.count("evolve.plan_apply")+c.count("evolve.plan_rollback") != n {
			t.Fatalf("%s ở thẻ Cơ hội không được gửi yêu cầu", k)
		}
		if m.ev.Tab() != 1 {
			t.Fatalf("%s phải chuyển sang thẻ Đề xuất, đang ở %d", k, m.ev.Tab())
		}
		hasAll(t, plain(m.Render()), "Chọn một đề xuất ở danh sách trước.")
	}
	// Ở thẻ Đề xuất thì x mở hộp bác bỏ như thường.
	press(t, m, "x")
	if open, _, _ := m.ev.Dialog(); !open {
		t.Fatal("x ở thẻ Đề xuất phải mở hộp bác bỏ")
	}
}

// Quay lui: có tệp xung đột thì hộp đỏ; --force chỉ khi chọn ô, kèm tập xung đột đã xem (ExpectConflicts).
func TestEvolveRollbackDialogForceAndExpectConflicts(t *testing.T) {
	m, c := newCapModel(t, Options{})
	goProposals(t, m)
	pl := samplePlanRollback(m.ev.Cursor().Proposal)
	c.values["evolve.plan_rollback"] = pl
	press(t, m, "u")
	open, danger, checks := m.ev.Dialog()
	if !open || !danger || len(checks) != 1 || checks[0] {
		t.Fatalf("hộp đỏ có ô --force chưa chọn: %v %v %v", open, danger, checks)
	}
	hasAll(t, plain(m.Render()), "Quay lui đề xuất", "đã bị sửa sau khi áp dụng", "--force", ".bint/knowledge/items/01KNOW000003.md",
		"hiện tại (sẽ mất)", "-sửa tay") // diff nêu đúng phần sẽ bị ghi đè
	press(t, m, "enter")
	if o := c.last(t, "evolve.rollback").(evolvesvc.RollbackOptions); o.Force || o.ExpectConflicts != nil {
		t.Fatalf("không chọn thì không Force: %+v", o)
	}
	press(t, m, "u", "x", "enter")
	o := c.last(t, "evolve.rollback").(evolvesvc.RollbackOptions)
	if !o.Force || len(o.ExpectConflicts) != 1 || o.ExpectConflicts[pl.Conflicts[0]] != evolvesvc.ContentHash(pl.Files[0].Current) {
		t.Fatalf("chọn --force phải gửi tệp xung đột và dấu vân tay nội dung đã xem: %+v", o)
	}
	// Không xung đột: hộp thường, không ô chọn.
	pl2 := samplePlanRollback(m.ev.Cursor().Proposal)
	pl2.Conflicts, pl2.Files = []string{}, pl2.Files[1:]
	c.values["evolve.plan_rollback"] = pl2
	press(t, m, "u")
	if open, danger, checks := m.ev.Dialog(); !open || danger || len(checks) != 0 {
		t.Fatalf("không xung đột: %v %v %v", open, danger, checks)
	}
}

// Sau áp dụng đề xuất skill, kết quả đồng bộ skill hiện cho người dùng (như CLI in kết quả sync).
func TestEvolveApplyResultShowsSkillSync(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	goProposals(t, m)
	p := m.ev.Cursor().Proposal
	for _, tc := range []struct {
		sync actions.SkillSync
		want string
	}{{actions.SkillSync{Ran: true}, "Đã đồng bộ skill tới các harness."},
		{actions.SkillSync{Ran: true, Install: bintapp.SkillInstall{Conflicts: 2}}, "2 xung đột khi đồng bộ skill"},
		{actions.SkillSync{Ran: true, Err: errors.New("hỏng")}, "Không đồng bộ được skill: hỏng"}} {
		m.Update(actions.ResultMsg{ID: "evolve.apply", Value: actions.ApplyOutcome{Result: evolve.ApplyResult{Proposal: p, AdoptionID: "ead_9"}, Sync: tc.sync}})
		hasAll(t, plain(m.Render()), "Đã áp dụng 01EP0000KNOW", tc.want)
		press(t, m, "esc")
	}
}

// ---- việc nền ----

// Mỗi lúc một việc Tiến hoá nặng: việc thứ hai báo lỗi riêng và không bắt đầu; Esc ở thẻ Log huỷ việc.
func TestEvolveOneHeavyJobAndEscCancels(t *testing.T) {
	m, c := newCapModel(t, Options{}, "evolve.cycle", "evolve.evaluate")
	d := newDrv(t, m)
	d.press("6", "C")
	d.typeText("5")
	d.press("ctrl+s")
	if !d.until(func() bool { return m.jobs.Len() == 1 }) {
		t.Fatal("cycle phải chạy nền")
	}
	if o := c.last(t, "evolve.cycle").(evolvesvc.CycleOptions); o.BudgetUSD != 5 {
		t.Fatalf("%+v", o)
	}
	if !m.ev.WantsInterrupt() {
		t.Fatal("ở thẻ Log của việc đang chạy, Esc phải huỷ việc")
	}
	_, cmd := m.Update(actions.RequestMsg{ID: "evolve.evaluate", Opts: evolvesvc.EvaluateOptions{ID: "x"}})
	d.settle(cmd)
	if m.jobs.Len() != 1 || c.count("evolve.evaluate") != 0 {
		t.Fatalf("việc thứ hai không được bắt đầu: jobs=%d", m.jobs.Len())
	}
	if !d.until(func() bool {
		return strings.Contains(plain(m.Render()), "Đang có một việc Tiến hoá chạy nền")
	}) {
		t.Fatalf("thiếu lỗi việc thứ hai:\n%s", plain(m.Render()))
	}
	start := time.Now()
	d.press("esc")
	if !d.until(func() bool { return m.jobs.Len() == 0 }) || time.Since(start) > 5*time.Second {
		t.Fatal("huỷ phải kết thúc việc trong hạn giờ")
	}
	hasAll(t, plain(m.Render()), "Đã xong: evolve cycle")
}

// Confirm của evaluate thành hộp thoại kèm ước tính; trả "không" thì việc kết thúc với ErrCancelled; huỷ khi đang hỏi
// không treo goroutine (chạy với -race).
func TestEvolveEvaluateConfirmDialog(t *testing.T) {
	for _, mode := range []string{"no", "cancel"} {
		reg, _ := capture()
		var list []actions.Action
		for _, a := range reg.All() {
			if a.ID == "evolve.evaluate" {
				a.Job = func(ctx context.Context, _ actions.Env, _ any, br *jobs.Bridge) (any, error) {
					ok := br.ConfirmRequest(bintapp.ConfirmRequest{Key: evolvesvc.ConfirmEvaluateKey, Text: "Bắt đầu đối chiếu?", Detail: "Tập validation: 2 tác vụ\nƯớc tính: 12 lượt, chi phí ~$3.40"})
					if !ok {
						return nil, bintapp.ErrCancelled
					}
					return nil, nil
				}
			}
			list = append(list, a)
		}
		m := newModel(t, Options{Mode: theme.Mono, Registry: actions.NewRegistry(list)}, sampleData(), 140, 40)
		d := newDrv(t, m)
		d.press("6", ".", "E", "ctrl+s")
		if !d.until(func() bool { return m.prompt != nil }) {
			t.Fatalf("%s: phải có hộp hỏi xác nhận:\n%s", mode, plain(m.Render()))
		}
		hasAll(t, plain(m.Render()), "Bắt đầu đối chiếu?", "gọi mô hình và tốn chi phí", "Ước tính: 12 lượt, chi phí ~$3.40")
		if mode == "no" {
			d.press("esc")
		} else {
			_, cmd := m.Update(actions.CancelMsg{ID: "evolve.evaluate"})
			d.settle(cmd)
		}
		if !d.until(func() bool { return m.jobs.Len() == 0 }) {
			t.Fatalf("%s: việc phải kết thúc", mode)
		}
		if m.prompt != nil {
			t.Fatalf("%s: không còn câu hỏi", mode)
		}
		hasAll(t, plain(m.Render()), "Đã xong: evolve evaluate")
	}
}

// ---- chế độ chỉ đọc, phiên agent, hộp thư ----

func evolveActionsWhere(pred func(actions.Action) bool) []actions.Action {
	var out []actions.Action
	for _, a := range actions.Default().All() {
		if a.Screen == actions.Evolve && pred(a) {
			out = append(out, a)
		}
	}
	return out
}

// Mọi hành động Writes của màn Tiến hoá (kể cả hành động nền) bị từ chối ở --read-only qua phím, bảng lệnh và yêu
// cầu trực tiếp; quét cơ hội vẫn dùng được (đọc).
func TestEvolveWriteActionsRefusedInReadOnly(t *testing.T) {
	writes := evolveActionsWhere(func(a actions.Action) bool { return a.Writes })
	if len(writes) < 6 {
		t.Fatalf("quá ít hành động ghi: %d", len(writes))
	}
	var sawRO atomicBool
	m, c := newCapModel(t, Options{ReadOnly: true, EvolveLoader: func(_ context.Context, _ string, _ evscreen.Query, ro bool) (evscreen.Data, error) {
		sawRO.set(ro)
		return sampleEvolve(), nil
	}}, "evolve.propose", "evolve.evaluate", "evolve.process", "evolve.cycle")
	goProposals(t, m)
	if !sawRO.get() {
		t.Fatal("loader phải nhận readOnly = true")
	}
	inPalette := map[string]bool{}
	for _, it := range m.paletteItems() {
		inPalette[it.ID] = true
	}
	for _, a := range writes {
		if inPalette[a.ID] {
			t.Errorf("%s (Writes) không được có trong bảng lệnh", a.ID)
		}
		if len(a.Key) == 1 {
			m.toast = ""
			press(t, m, a.Key)
			if m.ev.Capturing() {
				t.Errorf("phím %s (%s) mở hộp/form dù chỉ đọc", a.Key, a.ID)
				press(t, m, "esc")
			}
			if !strings.Contains(plain(m.Render()), "chế độ chỉ đọc") {
				t.Errorf("phím %s (%s) phải báo chỉ đọc", a.Key, a.ID)
			}
		}
		if _, cmd := m.run(a.ID); cmd != nil {
			t.Errorf("run(%s) phải không làm gì", a.ID)
		}
		_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
		drive(t, m, cmd)
		if m.jobs.Len() != 0 || c.count(a.ID) != 0 {
			t.Errorf("%s tới dịch vụ ở chế độ chỉ đọc", a.ID)
		}
	}
	press(t, m, "s")
	if !m.ev.Capturing() {
		t.Fatal("quét cơ hội là hành động đọc: phải mở được form ở chế độ chỉ đọc")
	}
}

// Trong phiên agent mọi hành động HumanOnly của Tiến hoá bị khoá (phím, bảng lệnh, yêu cầu trực tiếp).
func TestEvolveHumanOnlyActionsLockedInAgentSession(t *testing.T) {
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
	humanOnly := evolveActionsWhere(func(a actions.Action) bool { return a.HumanOnly })
	if len(humanOnly) != 5 {
		t.Fatalf("muốn 5 hành động HumanOnly (propose, apply, reject, rollback, process): %d", len(humanOnly))
	}
	m, c := newCapModel(t, Options{Agent: true}, "evolve.propose", "evolve.process")
	goProposals(t, m)
	for _, a := range humanOnly {
		locked := false
		for _, it := range m.paletteItems() {
			locked = locked || (it.ID == a.ID && it.Locked != "")
		}
		if !locked {
			t.Errorf("%s phải hiện khoá trong bảng lệnh", a.ID)
		}
		m.toast = ""
		press(t, m, a.Key)
		if m.ev.Capturing() || !strings.Contains(plain(m.Render()), "chỉ dành cho người") {
			t.Errorf("phím %s (%s) phải bị khoá:\n%s", a.Key, a.ID, plain(m.Render()))
			press(t, m, "esc")
		}
		_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
		drive(t, m, cmd)
		if c.count(a.ID) != 0 || m.jobs.Len() != 0 {
			t.Errorf("%s: việc bị khoá không được chạy", a.ID)
		}
	}
}

// Hộp thư: Enter trên dòng đề xuất mở màn Tiến hoá tại đúng đề xuất, kể cả khi nó không phải dòng đầu.
func TestInboxEnterOnProposalFocusesIt(t *testing.T) {
	d := sampleData()
	d.Pending.Proposals = []bintapp.PendingProposal{{ID: "01EP0000KNOW", Action: "update"}, {ID: "01EP0000GUID", Action: "create"}}
	d.Pending.KnowledgeBatches, d.Pending.KnowledgeOther = nil, 0
	m, _ := newCapModel(t, Options{})
	m = newModelData(t, m, d)
	press(t, m, "j", "enter") // dòng đề xuất thứ hai
	if m.Tab() != 5 {
		t.Fatalf("tab = %d, muốn 5 (Tiến hoá)", m.Tab())
	}
	if r := m.ev.Cursor(); r == nil || r.Proposal.ID != "01EP0000GUID" || m.ev.Tab() != 1 {
		t.Fatalf("con trỏ phải ở đúng đề xuất: tab=%d %+v", m.ev.Tab(), r)
	}
	hasAll(t, plain(m.Render()), "Chi tiết · EP0000GUID", "01EP0000GUID (proposed)")
}

// Đề xuất không còn chờ (không có trong danh sách "đang chờ") vẫn được mở: màn hình nạp lại với mọi trạng thái.
func TestEvolveFocusLoadsAllStatesWhenMissing(t *testing.T) {
	var qs []evscreen.Query
	m, _ := newCapModel(t, Options{EvolveLoader: func(_ context.Context, _ string, q evscreen.Query, _ bool) (evscreen.Data, error) {
		qs = append(qs, q)
		d := sampleEvolve()
		if !q.All {
			d.Rows = d.Rows[:1]
		}
		return d, nil
	}})
	press(t, m, "6")
	cmd := m.ev.Focus("01EP0000SCRI")
	drive(t, m, cmd)
	if len(qs) < 2 || !qs[len(qs)-1].All {
		t.Fatalf("phải nạp lại với mọi trạng thái: %+v", qs)
	}
	if r := m.ev.Cursor(); r == nil || r.Proposal.ID != "01EP0000SCRI" {
		t.Fatalf("%+v", r)
	}
}

// newModelData dựng lại mô hình với dữ liệu Tổng quan khác nhưng cùng cấu hình (sổ ghi lại, loader giả).
func newModelData(t *testing.T, m *Model, d overview.Data) *Model {
	t.Helper()
	o := m.opts
	return newModel(t, o, d, 140, 40)
}
