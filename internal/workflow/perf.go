package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/task"
)

// Cổng hồi quy hiệu năng (workflow.perf.command): đo mốc trước b-feat đầu tiên và đo lại sau mỗi b-feat. Mỗi lần đo chạy lệnh 3 lần, lấy thời gian nhỏ nhất;
// chậm hơn mốc quá max_slowdown lần và quá sàn nhiễu perfNoiseFloor, hoặc lệnh thoát khác 0, thì đỏ (log perf-N.log), coi như kiểm chứng không qua.

const (
	perfRuns       = 3
	perfNoiseFloor = 100 * time.Millisecond
)

// perfEnabled: lệnh đo đã khai báo và chính lệnh đó đã được người xác nhận (bint task approve ghi băm vào PerfApproval;
// lệnh đổi sau đó thì không chạy), hoặc dự án không đòi duyệt lệnh.
func (r *Runner) perfEnabled(t *task.Task) bool {
	if strings.TrimSpace(r.Cfg.Workflow.Perf.Command) == "" {
		return false
	}
	return r.PerfConfirmed(t) || !r.Cfg.Security.RequireApprovalForCommands
}

// perfMeasure chạy lệnh đo perfRuns lần, trả thời gian nhỏ nhất. Dừng ở lần đầu thoát khác 0 (ok=false); log của lần đó (hoặc lần đầu) ở logPath.
func (r *Runner) perfMeasure(ctx context.Context, root, logPath string) (best time.Duration, res eval.CmdResult, ok bool) {
	env := eval.VerifyEnvFrom(verifyEnviron())
	tmp, cleanup := r.verifyTempDir()
	defer cleanup()
	if tmp != "" {
		env = append(env, verifyTempEnv(tmp)...)
	}
	timeout := time.Duration(r.Cfg.Workflow.VerifyTimeoutMinutes) * time.Minute
	for i := 0; i < perfRuns; i++ {
		lp := logPath
		if i > 0 {
			lp = logPath + fmt.Sprintf(".%d.tmp", i)
		}
		res = eval.RunShell(ctx, root, r.Cfg.Workflow.Perf.Command, timeout, lp, env)
		if i > 0 {
			_ = os.Remove(lp)
		}
		if !res.OK() {
			if i > 0 { // log của lần lỗi: ghi lại vào log chính
				_ = os.WriteFile(logPath, []byte(res.Tail), 0o644)
			}
			return 0, res, false
		}
		if i == 0 || res.Duration < best {
			best = res.Duration
		}
	}
	return best, res, true
}

// perfBaseline đo mốc trước b-feat đầu tiên (gọi khi đã giữ khoá bước). Lỗi hoặc lệnh đo hỏng thì không có mốc (cổng bỏ qua, ghi Notes).
func (r *Runner) perfBaseline(ctx context.Context, rs *runState) {
	if strings.TrimSpace(r.Cfg.Workflow.Perf.Command) == "" || rs.t.State.PerfBaseline != nil || rs.t.State.PerfBaselineTried || len(rs.t.State.PerfChecks) > 0 {
		return
	}
	if !r.perfEnabled(rs.t) {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("cổng hiệu năng: workflow.perf.command chưa được người xác nhận (hoặc đã đổi sau khi duyệt), không chạy: %s — bint task approve %s", r.Cfg.Workflow.Perf.Command, rs.t.State.ID))
		return
	}
	// Đánh dấu đã thử trước khi đo: ở chế độ lát PerfChecks không bao giờ được ghi nên không có dấu này thì mốc có thể đo muộn trên cây đã sửa.
	if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.PerfBaselineTried = true
		return nil
	}); err == nil {
		rs.t = nt
	}
	root, err := r.RootFor(rs.t)
	if err != nil {
		return
	}
	best, res, ok := r.perfMeasure(ctx, root, rs.t.LogPath("perf-0.log"))
	if !ok {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("cổng hiệu năng: lệnh đo hỏng ngay ở mốc (mã thoát %d), không đo hồi quy: %s", res.ExitCode, r.Cfg.Workflow.Perf.Command))
		return
	}
	secs := best.Seconds()
	if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		t.State.PerfBaseline = &secs
		return nil
	}); err == nil {
		rs.t = nt
	}
	r.logf("  hiệu năng: mốc %.3f giây (%s)", secs, r.Cfg.Workflow.Perf.Command)
}

// perfCheck đo lại sau b-feat. over=true khi đỏ; msg là số đo (rỗng khi cổng tắt/không có mốc). record: ghi vào State.PerfChecks
// (không chia lát; đỏ vào vòng sửa qua redVerification). Lát thì không ghi: lát đỏ bị bỏ, cây về điểm xanh.
func (r *Runner) perfCheck(ctx context.Context, rs *runState, record bool) (over bool, msg string) {
	if !r.perfEnabled(rs.t) || rs.t.State.PerfBaseline == nil {
		return false, ""
	}
	base := *rs.t.State.PerfBaseline
	root, err := r.RootFor(rs.t)
	if err != nil {
		return false, ""
	}
	lock, ok := r.lockStep(ctx, rs, "perf", LockExclusive)
	if !ok {
		return false, ""
	}
	// Đánh số theo tệp log có sẵn: ở chế độ lát PerfChecks không được ghi nên len(PerfChecks)+1 luôn là 1 và đè perf-1.log.
	n := len(rs.t.State.PerfChecks) + 1
	for {
		if _, err := os.Stat(rs.t.LogPath(fmt.Sprintf("perf-%d.log", n))); err != nil {
			break
		}
		n++
	}
	log := rs.t.LogPath(fmt.Sprintf("perf-%d.log", n))
	best, res, passedRun := r.perfMeasure(ctx, root, log)
	lock.Release()
	if res.Err != nil && ctx.Err() != nil {
		return false, ""
	}
	maxSlow := r.Cfg.Workflow.Perf.MaxSlowdown
	secs := best.Seconds()
	switch {
	case !passedRun:
		over = true
		msg = fmt.Sprintf("lệnh đo hiệu năng thoát mã %d (mốc %.3f giây), log %s", res.ExitCode, base, log)
		if res.TimedOut {
			msg = fmt.Sprintf("lệnh đo hiệu năng hết giờ (mốc %.3f giây), log %s", base, log)
		}
	default:
		over = secs > base*maxSlow && secs-base > perfNoiseFloor.Seconds()
		msg = fmt.Sprintf("%.3f giây (nhỏ nhất của %d lần) so với mốc %.3f giây = ×%.2f, trần workflow.perf.max_slowdown = %.2f và sàn nhiễu %.1f giây, log %s",
			secs, perfRuns, base, secs/max(base, 1e-9), maxSlow, perfNoiseFloor.Seconds(), log)
	}
	if record {
		exit := res.ExitCode
		if over && exit == 0 {
			exit = 1
		}
		v := task.Verification{At: r.at(), Command: "hiệu năng (perf): " + msg, Confirmed: true, ExitCode: exit, Passed: !over, TimedOut: res.TimedOut, Log: log,
			Seconds: secs, BaselineSeconds: base, By: r.actor(), RunID: r.RunID}
		if nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			t.State.PerfChecks = append(t.State.PerfChecks, v)
			return nil
		}); err == nil {
			rs.t = nt
		}
	}
	r.logf("  hiệu năng: %s → %s", msg, passWord(!over))
	return over, msg
}

// isPerfCheck cho biết v là lần đo hiệu năng gần nhất của t.
func isPerfCheck(t *task.Task, v *task.Verification) bool {
	return v != nil && v == t.State.LastPerfCheck()
}
