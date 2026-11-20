package workflow

import (
	"bint/internal/testreport"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"bint/internal/affected"
	"bint/internal/eval"
	"bint/internal/task"
)

// Kiểm hẹp theo mốc của b-feat (luồng B′, giai đoạn 3). Agent gọi `bint task check` sau mỗi nhóm sửa, và Bint tự chạy nó khi cây làm việc đứng yên
// (bgverify.go). Tầng hẹp chỉ chạy test của CHÍNH các gói có tệp đổi kể từ lần kiểm hẹp trước (lần đầu: so với mốc tác vụ), không lan theo đồ thị
// phụ thuộc — phần lan và toàn repo để kiểm nền. Phạm vi phải nới ra full thì KHÔNG chạy toàn repo. Mọi lần gọi đều ghi vào State.Checks, kể cả lần
// bị bỏ qua (Skipped kèm lý do), để biết "không gọi" khác "gọi nhưng không chạy". Không tính vào cổng done.

// checkReason, checkAutoReason, checkGateReason là Verification.Reason của kiểm hẹp do agent gọi, do Bint tự chạy khi cây đứng yên, và do Bint chạy
// làm cổng sau lượt b-feat LÕI/SỬA LÕI của B′ (cổng theo pha: thay lần kiểm chứng rộng chặn luồng; kiểm rộng để lượt mở rộng và kiểm nền).
const (
	checkReason     = "kiểm hẹp theo mốc"
	checkAutoReason = "kiểm hẹp tự động (cây đứng yên)"
	checkGateReason = "kiểm hẹp cổng lõi"
)

// CheckResult là kết quả một lần kiểm hẹp.
type CheckResult struct {
	Check      task.Verification
	Skipped    string // lý do không chạy (kể cả phạm vi full)
	Background *task.Verification
	// Cached: cây không đổi kể từ lần kiểm hẹp đã chạy gần nhất nên trả lại kết quả đó, không chạy lại.
	Cached bool
	// Note: ghi chú cho agent khi kiểm nền đỏ mà kiểm hẹp xanh (bgRedNote).
	Note string
	// Summary: tóm tắt đỏ của lần kiểm hẹp (redSummary) khi nó không qua; rỗng khi qua.
	Summary string
}

// bgRedNote: kiểm nền đỏ trong khi kiểm hẹp gần nhất xanh thì lỗi có thể do môi trường chứ không do mã. Kiểm nền chạy trên checkout sạch của ảnh chụp cây
// (git worktree tạm): tệp bị .gitignore (mã sinh, vendor tải về, .env test) và submodule chưa khởi tạo không có ở đó. Không bảo agent sửa mã ngay.
func bgRedNote(t *task.Task) string {
	bg := lastBackground(t)
	last := lastRanCheck(t)
	if bg == nil || bg.Passed || last == nil || !last.Passed {
		return ""
	}
	return "kiểm nền ĐỎ nhưng kiểm hẹp gần nhất XANH: kiểm nền chạy trên checkout sạch của ảnh chụp cây (không có tệp bị .gitignore như mã sinh, vendor, .env test, " +
		"và submodule chưa khởi tạo), khác thư mục làm việc thật. Đọc log " + bg.Log + ": nếu lỗi do thiếu thứ đó thì đừng sửa mã; ghi `bint task error " +
		t.State.ID + " --step b-feat \"<lỗi môi trường checkout sạch>\"` và đi tiếp."
}

// lastRanCheck là lần kiểm hẹp gần nhất có chạy (không bị bỏ qua); nil nếu chưa có.
func lastRanCheck(t *task.Task) *task.Verification {
	for i := len(t.State.Checks) - 1; i >= 0; i-- {
		if c := t.State.Checks[i]; c.Skipped == "" {
			return &c
		}
	}
	return nil
}

// Check chạy kiểm hẹp cho tác vụ ref (agent gọi qua `bint task check`). Cùng luật xác nhận lệnh như Verify.
func (e *Env) Check(ctx context.Context, ref string) (*task.Task, CheckResult, error) {
	return e.check(ctx, ref, checkReason)
}

// check chạy kiểm hẹp với lý do reason (checkReason: agent gọi, by là actor; lý do khác: Bint tự chạy, by "bint").
func (e *Env) check(ctx context.Context, ref, reason string) (*task.Task, CheckResult, error) {
	var res CheckResult
	t, err := e.Tasks.Load(ref)
	if err != nil {
		return nil, res, err
	}
	res.Background = lastBackground(t)
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	vc := e.Cfg.Workflow.Verify
	by := e.actor()
	if reason != checkReason {
		by = "bint"
	}
	record := func(v task.Verification) (*task.Task, error) {
		nt, err := e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
			t.State.Checks = append(t.State.Checks, v)
			return nil
		})
		if err != nil {
			return t, err
		}
		return nt, nil
	}
	skip := func(why string, sel *affected.Selection) (*task.Task, CheckResult, error) {
		res.Skipped = why
		v := task.Verification{At: e.at(), Command: cmd, By: by, RunID: e.RunID, Reason: reason, Skipped: why, Selection: sel}
		nt, err := record(v)
		res.Check = v
		return nt, res, err
	}
	switch {
	case cmd == "":
		return skip("phiếu chưa có verify.command (bint task set --verify-command)", nil)
	case !e.VerifyConfirmed(t) && e.Cfg.Security.RequireApprovalForCommands:
		return skip("lệnh kiểm chứng chưa được người xác nhận; Bint không chạy lệnh do agent đề xuất (bint task approve "+t.State.ID+")", nil)
	case strings.TrimSpace(vc.AffectedCommand) == "":
		return skip("chưa cấu hình workflow.verify.affected_command nên không có kiểm hẹp", nil)
	}
	root, err := e.RootFor(t)
	if err != nil {
		return t, res, err
	}
	fp := task.WorktreeFingerprint(ctx, root)
	if last := lastRanCheck(t); last != nil && fp != "" && last.Worktree == fp {
		// Cây không đổi kể từ lần kiểm gần nhất (thường do Bint tự chạy khi cây đứng yên): trả lại kết quả đó, không tốn thêm một lần go test.
		res.Check, res.Cached, res.Note = *last, true, bgRedNote(t)
		if !last.Passed {
			res.Summary = redSummary(last.Log)
		}
		return t, res, nil
	}
	sel, since := e.narrowSelection(ctx, t, root, vc.BintConfigFull)
	if sel.Scope == affected.TierFull {
		why := "thay đổi buộc chạy toàn bộ bộ test (phạm vi full)"
		if len(sel.Widening) > 0 {
			w := sel.Widening[0]
			why += fmt.Sprintf(" vì %s %s %s", w.Reason, w.Path, w.Detail)
		}
		return skip(why+": kiểm hẹp không chạy, để kiểm nền lo", &sel)
	}
	ran, run := eval.ExpandTestCommand(strings.TrimSpace(vc.AffectedCommand), sel.TestFiles), sel.Scope != affected.TierNone
	log := t.LogPath(fmt.Sprintf("check-%d.log", len(t.State.Checks)+1))
	var r eval.CmdResult
	if run {
		env := eval.VerifyEnvFrom(verifyEnviron())
		tmp, cleanup := e.verifyTempDir()
		if tmp != "" {
			env = append(env, verifyTempEnv(tmp)...)
		}
		r = eval.RunShell(ctx, root, ran, time.Duration(e.Cfg.Workflow.VerifyTimeoutMinutes)*verifyTimeoutUnit, log, env)
		cleanup()
		if r.Err != nil && ctx.Err() != nil {
			return t, res, ctx.Err()
		}
	} else {
		log = ""
	}
	if since != "" {
		reason += " (gói đổi kể từ lần kiểm trước)"
	}
	// Như Verify: fingerprint và ảnh chụp cây lấy SAU khi chạy (lệnh test có thể sinh tệp), để khớp cây cuối của b-feat. Ảnh chụp không được (sandbox chặn
	// ghi .git) thì Tree rỗng: kiểm nền không khởi chạy từ lần này, chỉ từ lần Bint tự chạy ngoài sandbox.
	v := task.Verification{At: e.at(), Command: cmd, Confirmed: true, ExitCode: r.ExitCode, Passed: !run || r.OK(), TimedOut: r.TimedOut, Log: log,
		Worktree: task.WorktreeFingerprint(ctx, root), By: by, RunID: e.RunID, Reason: reason, Scope: string(sel.Scope), Selection: &sel}
	if run {
		v.Ran = ran
	}
	v.Tree, _ = WorktreeSnapshot(ctx, root)
	t, err = record(v)
	if err != nil {
		return t, res, err
	}
	res.Check = v
	if !v.Passed {
		res.Summary = redSummary(v.Log)
	}
	res.Background = lastBackground(t)
	res.Note = bgRedNote(t)
	return t, res, nil
}

// narrowSelection chọn tầng hẹp: tệp đổi kể từ ảnh chụp của lần kiểm hẹp XANH gần nhất (nếu có, since là cây đó), không thì kể từ mốc tác vụ;
// chỉ test của chính các gói có tệp đổi. Lần kiểm trước đỏ thì so với mốc của lần xanh trước nữa, để gói đỏ được kiểm lại.
func (e *Env) narrowSelection(ctx context.Context, t *task.Task, root string, bintConfigFull bool) (affected.Selection, string) {
	since := ""
	for i := len(t.State.Checks) - 1; i >= 0; i-- {
		c := t.State.Checks[i]
		if c.Skipped != "" {
			continue
		}
		if !c.Passed {
			break // gói đỏ của lần đó phải chạy lại: không thu hẹp qua nó
		}
		if c.Tree != "" {
			since = c.Tree
		}
		break
	}
	base := t.State.Base
	if since != "" {
		base = since
	}
	if strings.TrimSpace(base) == "" {
		return SelectAffectedCfg(ctx, root, base, bintConfigFull), ""
	}
	head, changed, err := changedSince(ctx, root, base)
	if err != nil {
		return affected.Selection{Scope: affected.TierFull, Base: base, Changed: []string{},
			Widening: []affected.Widening{{Tier: affected.TierFull, Reason: affected.ReasonSourceUnavailable, Detail: err.Error()}}}, since
	}
	return affected.Select(ctx, affected.Input{Root: root, Base: base, Head: head, Changed: changed, IgnoreBintConfig: !bintConfigFull, Direct: true}), since
}

func lastBackground(t *task.Task) *task.Verification {
	if n := len(t.State.BackgroundVerifications); n > 0 {
		v := t.State.BackgroundVerifications[n-1]
		return &v
	}
	return nil
}

// Cổng theo pha của B′ (docs/bprime-budget-plan.md mục 1.2). Sau lượt b-feat LÕI và SỬA LÕI, cổng chặn luồng là kiểm hẹp (test của chính các gói đổi)
// thay cho kiểm chứng rộng: lịch sử repo này có trung vị 10–19 phút cho mỗi lần kiểm rộng, và lượt lõi luôn có lượt mở rộng theo sau. Kiểm rộng chạy
// một lần ở lượt mở rộng; không có lượt mở rộng (thăm dò không để lại thước đo) thì bprimeAfterCore chạy nó trước review (wideAfterGate).

// useCoreGate: lượt implement vừa xong là lượt LÕI/SỬA LÕI của một tác vụ B′ có tách lõi/mở rộng, và là bước implement cuối của flow.
func (r *Runner) useCoreGate(rs *runState, nextImpl bool) bool {
	t := rs.t
	return !nextImpl && task.IsBPrime(t) && t.State.BPrimePhase == "" && rs.activeSlice() == nil && r.bprimeSplit(rs)
}

// coreGate chạy kiểm hẹp làm cổng sau lượt lõi/sửa lõi. gated=false khi không áp dụng hoặc kiểm hẹp không chạy được (bỏ qua: phạm vi full, thiếu
// affected_command, lệnh chưa xác nhận): người gọi lùi về kiểm chứng rộng như cũ. ok=false khi workflow phải dừng (đã đặt trạng thái).
func (r *Runner) coreGate(ctx context.Context, rs *runState, nextImpl bool) (gated, ok bool) {
	if !r.useCoreGate(rs, nextImpl) {
		return false, true
	}
	lock, locked := r.lockStep(ctx, rs, "verify", LockExclusive)
	if !locked {
		return false, false
	}
	t, cr, err := r.check(ctx, rs.t.State.ID, checkGateReason)
	lock.Release()
	switch {
	case err != nil && ctx.Err() != nil:
		r.interrupt(rs, "người dùng huỷ khi kiểm hẹp")
		return false, false
	case err != nil:
		r.fail(rs, "verify", err.Error())
		return false, false
	}
	rs.t = t
	if cr.Skipped != "" {
		rs.res.Notes = append(rs.res.Notes, "luồng B′: kiểm hẹp cổng lõi không chạy ("+cr.Skipped+"): kiểm chứng rộng như cũ")
		return false, true
	}
	v := cr.Check
	if cr.Cached {
		// Cây không đổi kể từ lần kiểm hẹp gần nhất (thường do Bint tự chạy khi cây đứng yên): ghi lại đúng kết quả đó với lý do cổng, để redVerification
		// và wideAfterGate nhận ra lần cổng này.
		v.Reason, v.At, v.By, v.RunID = checkGateReason+" (dùng lại kết quả lúc "+cr.Check.At+")", r.at(), "bint", r.RunID
		nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.Checks = append(t.State.Checks, v)
			return nil
		})
		if err != nil {
			r.fail(rs, "verify", err.Error())
			return false, false
		}
		rs.t = nt
	}
	r.logf("  kiểm hẹp cổng lõi (scope %s): %s → %s", v.Scope, cmpStr(v.Ran, "không test nào bị ảnh hưởng"), passWord(v.Passed))
	return true, true
}

// lastGateCheck là lần kiểm hẹp cổng lõi gần nhất (nil nếu chưa có).
func lastGateCheck(t *task.Task) *task.Verification {
	for i := len(t.State.Checks) - 1; i >= 0; i-- {
		if c := t.State.Checks[i]; c.Skipped == "" && strings.HasPrefix(c.Reason, checkGateReason) {
			return &c
		}
	}
	return nil
}

// gateAfterVerify: lần cổng lõi g mới hơn lần kiểm chứng rộng gần nhất (chưa có kiểm rộng nào kể từ cổng).
func gateAfterVerify(t *task.Task) *task.Verification {
	g := lastGateCheck(t)
	if g == nil || g.Voided {
		return nil
	}
	if v := t.State.LastVerification(); v != nil && v.At > g.At {
		return nil
	}
	return g
}

// wideAfterGate: lượt lõi chỉ qua cổng hẹp mà sẽ không có lượt mở rộng (thăm dò không để lại thước đo, không tách) thì chạy kiểm chứng rộng một lần
// trước review, như luồng không có cổng theo pha. Trả false khi workflow phải dừng.
func (r *Runner) wideAfterGate(ctx context.Context, rs *runState) bool {
	if gateAfterVerify(rs.t) == nil || strings.TrimSpace(rs.t.Ticket.Verify.Command) == "" {
		return true
	}
	if r.reuseBackground(ctx, rs) {
		return true
	}
	return r.verifyAfterImplement(ctx, rs)
}

// redSummaryMax là trần cỡ bản tóm tắt đỏ in cho agent (đầu ra công cụ đọc lại ở mọi lượt sau, nên giữ nhỏ).
const redSummaryMax = 2048

// redSummary tóm tắt log của một lần kiểm đỏ cho agent: test đỏ, test hết giờ, gói lỗi build (testreport), kèm vài dòng lỗi đầu của mỗi test đỏ.
// Không đọc được định dạng thì lấy phần cuối log. Trần redSummaryMax byte; rỗng khi không có log.
func redSummary(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil || len(data) == 0 {
		return ""
	}
	text := string(data)
	rep := testreport.Parse(text)
	var b strings.Builder
	if !rep.Measured {
		lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
		b.WriteString("cuối log:\n")
		b.WriteString(strings.Join(lines[max(0, len(lines)-25):], "\n"))
		return clip(b.String(), redSummaryMax)
	}
	if len(rep.BuildFailed) > 0 {
		fmt.Fprintf(&b, "lỗi build: %s\n", strings.Join(rep.BuildFailed, ", "))
	}
	if len(rep.TimedOut) > 0 {
		var ns []string
		for _, t := range rep.TimedOut {
			ns = append(ns, cmpStr(t.String(), t.Package+" (cả gói)"))
		}
		fmt.Fprintf(&b, "hết giờ (đang chạy khi bị dừng, chưa chắc là lỗi mã): %s\n", strings.Join(ns, ", "))
	}
	if len(rep.Panicked) > 0 {
		fmt.Fprintf(&b, "panic: %s\n", strings.Join(rep.Panicked, ", "))
	}
	for _, t := range rep.Failed {
		fmt.Fprintf(&b, "ĐỎ %s\n", t)
		if detail := failDetail(text, t.Name, 4); detail != "" {
			b.WriteString(detail)
		}
	}
	return clip(b.String(), redSummaryMax)
}

// failDetail trả tối đa n dòng ngay sau dòng `--- FAIL: <name>` (thông báo lỗi của test), thụt lề.
func failDetail(text, name string, n int) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.TrimSpace(strings.TrimRight(l, "\r")) == "" || !strings.Contains(l, "--- FAIL: "+name+" ") {
			continue
		}
		var out []string
		for _, d := range lines[i+1:] {
			d = strings.TrimRight(d, "\r")
			if len(out) == n || strings.Contains(d, "--- FAIL: ") || strings.HasPrefix(d, "FAIL") || strings.HasPrefix(d, "=== ") {
				break
			}
			if strings.TrimSpace(d) != "" {
				out = append(out, "    "+strings.TrimSpace(d))
			}
		}
		if len(out) == 0 {
			return ""
		}
		return strings.Join(out, "\n") + "\n"
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… (cắt bớt; xem log)"
}
