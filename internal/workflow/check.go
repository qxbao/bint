package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bint/internal/affected"
	"bint/internal/eval"
	"bint/internal/task"
)

// Kiểm hẹp theo mốc của b-feat (luồng B′, giai đoạn 3): agent gọi `bint task check` sau mỗi nhóm sửa. Chỉ chạy test của gói bị chạm so với mốc
// tác vụ; phạm vi phải nới ra full thì KHÔNG chạy toàn repo (kiểm nền lo). Kết quả vào State.Checks, không tính vào cổng done.

// CheckResult là kết quả một lần kiểm hẹp.
type CheckResult struct {
	Check      task.Verification
	Skipped    string // lý do không chạy (kể cả phạm vi full)
	Background *task.Verification
	// Note: ghi chú cho agent khi kiểm nền đỏ mà kiểm hẹp xanh (bgRedNote).
	Note string
}

// bgRedNote: kiểm nền đỏ trong khi kiểm hẹp gần nhất xanh thì lỗi có thể do môi trường chứ không do mã. Kiểm nền chạy trên checkout sạch của ảnh chụp cây
// (git worktree tạm): tệp bị .gitignore (mã sinh, vendor tải về, .env test) và submodule chưa khởi tạo không có ở đó. Không bảo agent sửa mã ngay.
func bgRedNote(t *task.Task) string {
	bg := lastBackground(t)
	if bg == nil || bg.Passed || len(t.State.Checks) == 0 || !t.State.Checks[len(t.State.Checks)-1].Passed {
		return ""
	}
	return "kiểm nền ĐỎ nhưng kiểm hẹp gần nhất XANH: kiểm nền chạy trên checkout sạch của ảnh chụp cây (không có tệp bị .gitignore như mã sinh, vendor, .env test, " +
		"và submodule chưa khởi tạo), khác thư mục làm việc thật. Đọc log " + bg.Log + ": nếu lỗi do thiếu thứ đó thì đừng sửa mã; ghi `bint task error " +
		t.State.ID + " --step b-feat \"<lỗi môi trường checkout sạch>\"` và đi tiếp."
}

// Check chạy kiểm hẹp cho tác vụ ref. Cùng luật xác nhận lệnh như Verify.
func (e *Env) Check(ctx context.Context, ref string) (*task.Task, CheckResult, error) {
	var res CheckResult
	t, err := e.Tasks.Load(ref)
	if err != nil {
		return nil, res, err
	}
	res.Background = lastBackground(t)
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	vc := e.Cfg.Workflow.Verify
	switch {
	case cmd == "":
		res.Skipped = "phiếu chưa có verify.command (bint task set --verify-command)"
		return t, res, nil
	case !e.VerifyConfirmed(t) && e.Cfg.Security.RequireApprovalForCommands:
		res.Skipped = "lệnh kiểm chứng chưa được người xác nhận; Bint không chạy lệnh do agent đề xuất (bint task approve " + t.State.ID + ")"
		return t, res, nil
	case strings.TrimSpace(vc.AffectedCommand) == "":
		res.Skipped = "chưa cấu hình workflow.verify.affected_command nên không có kiểm hẹp"
		return t, res, nil
	}
	root, err := e.RootFor(t)
	if err != nil {
		return t, res, err
	}
	sel := SelectAffectedCfg(ctx, root, t.State.Base, vc.BintConfigFull)
	if sel.Scope == affected.TierFull {
		res.Skipped = "thay đổi buộc chạy toàn bộ bộ test (phạm vi full)"
		if len(sel.Widening) > 0 {
			w := sel.Widening[0]
			res.Skipped += fmt.Sprintf(" vì %s %s %s", w.Reason, w.Path, w.Detail)
		}
		res.Skipped += ": kiểm hẹp không chạy, để kiểm nền lo"
		return t, res, nil
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
	v := task.Verification{At: e.at(), Command: cmd, Confirmed: true, ExitCode: r.ExitCode, Passed: !run || r.OK(), TimedOut: r.TimedOut, Log: log,
		Worktree: task.WorktreeFingerprint(ctx, root), By: e.actor(), RunID: e.RunID, Reason: "kiểm hẹp theo mốc", Scope: string(sel.Scope), Selection: &sel}
	if run {
		v.Ran = ran
	}
	v.Tree, _ = WorktreeSnapshot(ctx, root)
	t, err = e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
		t.State.Checks = append(t.State.Checks, v)
		return nil
	})
	if err != nil {
		return t, res, err
	}
	res.Check = v
	res.Background = lastBackground(t)
	res.Note = bgRedNote(t)
	return t, res, nil
}

func lastBackground(t *task.Task) *task.Verification {
	if n := len(t.State.BackgroundVerifications); n > 0 {
		v := t.State.BackgroundVerifications[n-1]
		return &v
	}
	return nil
}
