package workflow

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/gitrepo"
	"bint/internal/task"
	"bint/internal/testreport"
)

// Phân loại lần kiểm chứng đỏ (docs/bprime-budget-plan.md mục 1.3 và 1.9). Lịch sử 60 tác vụ của repo này: 57 % lần kiểm chứng đỏ, trong đó ít nhất
// 38 lần do hết giờ hoặc test hay đỏ chứ không do mã của tác vụ, và mỗi lần như vậy mở một vòng sửa b-feat. Sau lần kiểm chứng đỏ mà biết được test
// nào đỏ (testreport), Bint chạy lại RIÊNG các test đó: trên cây hiện tại (xanh → flaky/hết giờ: không mở vòng sửa) rồi, nếu còn đỏ, trên mốc tác vụ
// (cũng đỏ → đỏ sẵn từ trước: không mở vòng sửa, cổng done vẫn chặn để người quyết). Còn lại là đỏ do mã.

// Giá trị của task.Verification.RedClass.
const (
	redCode        = "code"
	redFlaky       = "flaky"
	redTimeout     = "timeout"
	redPreexisting = "preexisting"
)

// triageMaxTests: đỏ nhiều test hơn thế thì gần như chắc là do mã; không chạy lại.
const triageMaxTests = 20

// triageDefaultTimeout là trần mặc định của `go test` (lệnh không đặt -timeout).
const triageDefaultTimeout = 10 * time.Minute

// redTestsOf trả các test đỏ và hết giờ đọc được từ log của v; ok=false khi không phân loại được (định dạng lạ, lỗi build, panic khác, cả gói hết giờ
// không rõ test nào, quá nhiều test đỏ).
func redTestsOf(v *task.Verification) (failed, timed []testreport.Test, ok bool) {
	if v == nil || v.Log == "" {
		return nil, nil, false
	}
	data, err := os.ReadFile(v.Log)
	if err != nil {
		return nil, nil, false
	}
	rep := testreport.Parse(string(data))
	if !strings.HasPrefix(rep.Format, "gotest") || len(rep.BuildFailed) > 0 || len(rep.Panicked) > 0 {
		return nil, nil, false
	}
	all := append(slices.Clone(rep.Failed), rep.TimedOut...)
	if len(all) == 0 || len(all) > triageMaxTests || slices.ContainsFunc(all, func(t testreport.Test) bool { return t.Package == "" || t.Name == "" }) {
		return nil, nil, false
	}
	return rep.Failed, rep.TimedOut, true
}

// goFlagsTimeout tách cờ của lệnh `go test` thuần (goTestCommand) và trần -timeout của nó (mặc định của go test khi không đặt).
func goFlagsTimeout(cmd string) ([]string, time.Duration) {
	flags, _, ok := goTestCommand(cmd)
	if !ok {
		return nil, triageDefaultTimeout
	}
	d := triageDefaultTimeout
	for _, f := range flags {
		if v, found := strings.CutPrefix(f, "-timeout="); found {
			if p, err := time.ParseDuration(v); err == nil && p > 0 {
				d = p
			}
		}
	}
	return flags, d
}

// triageRed phân loại lần kiểm chứng cuối của tác vụ khi nó đỏ (kiểm chứng sau bước implement), ghi RedClass/RedTests. flaky/timeout: ghi thêm một lần
// kiểm chứng xanh (lần đầy đủ chỉ đỏ ở đúng các test này, và chúng xanh khi chạy lại). Trả false khi workflow phải dừng (huỷ).
func (r *Runner) triageRed(ctx context.Context, rs *runState) bool {
	t := rs.t
	n := len(t.State.Verifications)
	if n == 0 {
		return true
	}
	v := t.State.Verifications[n-1]
	if v.Passed || v.Voided || v.RedClass != "" || v.Reason != "" || v.RunID == "" {
		return true
	}
	failed, timed, ok := redTestsOf(&v)
	if !ok {
		return true
	}
	root, err := r.RootFor(t)
	if err != nil {
		return true
	}
	flags, limit := goFlagsTimeout(cmpStr(v.Ran, v.Command))
	all := append(slices.Clone(failed), timed...)
	names := make([]string, len(all))
	for i, x := range all {
		names[i] = x.String()
	}
	run := func(dir string, ts []testreport.Test, tag string) (eval.CmdResult, testreport.Report, bool) {
		cmd := testreport.GoRerunCommand(flags, ts, 2*limit)
		if cmd == "" {
			return eval.CmdResult{}, testreport.Report{}, false
		}
		lock, locked := r.lockStep(ctx, rs, "verify", LockExclusive)
		if !locked {
			return eval.CmdResult{}, testreport.Report{}, false
		}
		defer lock.Release()
		log := t.LogPath(fmt.Sprintf("verify-%d-%s.log", n, tag))
		env := eval.VerifyEnvFrom(verifyEnviron())
		tmp, cleanup := r.verifyTempDir()
		if tmp != "" {
			env = append(env, verifyTempEnv(tmp)...)
		}
		done := r.beginActivity(t.State.ID, task.ActivityRerun, cmd, log)
		res := eval.RunShell(ctx, dir, cmd, 2*limit+time.Minute, log, env)
		done()
		cleanup()
		data, _ := os.ReadFile(log)
		return res, testreport.Parse(string(data)), true
	}
	class := redCode
	cur, curRep, ran := run(root, all, "rerun")
	switch {
	case ctx.Err() != nil:
		r.interrupt(rs, "người dùng huỷ khi chạy lại test đỏ")
		return false
	case !ran:
		return true
	case cur.OK() && len(failed) == 0:
		class = redTimeout
	case cur.OK():
		class = redFlaky
	case strings.TrimSpace(t.State.Base) != "" && curRep.Complete():
		still := curRep.Failed
		if pre := r.redAtBase(ctx, rs, root, still, run); pre {
			class = redPreexisting
		}
		if ctx.Err() != nil {
			r.interrupt(rs, "người dùng huỷ khi chạy lại test đỏ trên mốc tác vụ")
			return false
		}
	}
	nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
		if len(t.State.Verifications) < n {
			return nil
		}
		t.State.Verifications[n-1].RedClass, t.State.Verifications[n-1].RedTests = class, names
		if class == redFlaky || class == redTimeout {
			g := t.State.Verifications[n-1]
			g.Passed, g.ExitCode, g.TimedOut, g.RedClass, g.At = true, 0, false, "", r.at()
			g.Log = t.LogPath(fmt.Sprintf("verify-%d-rerun.log", n))
			t.State.Verifications = append(t.State.Verifications, g)
		}
		return nil
	})
	if err != nil {
		return true
	}
	rs.t = nt
	list := strings.Join(names, ", ")
	switch class {
	case redFlaky:
		rs.res.Notes = append(rs.res.Notes, "kiểm chứng đỏ chỉ ở "+list+" và các test đó xanh khi chạy lại riêng: coi là test hay đỏ, không mở vòng sửa")
	case redTimeout:
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("kiểm chứng đỏ chỉ vì hết giờ (%s) và các test đó xanh khi chạy lại với trần %s: không phải lỗi mã, "+
			"không mở vòng sửa; nên đặt -timeout lớn hơn trong verify.command", list, 2*limit))
	case redPreexisting:
		rs.res.Notes = append(rs.res.Notes, "kiểm chứng đỏ ở "+list+" nhưng các test đó cũng đỏ ở mốc tác vụ (đỏ sẵn từ trước): không mở vòng sửa; "+
			"cổng done vẫn cần người quyết (sửa riêng, hoặc xác nhận)")
	}
	r.logf("  phân loại kiểm chứng đỏ: %s (%s)", class, list)
	return true
}

// redAtBase chạy riêng các test ts trên mốc tác vụ (worktree tạm): true khi mọi test đó cũng đỏ (hoặc hết giờ) ở mốc. Test không có ở mốc (mới viết)
// làm lần chạy lỗi build hoặc không khớp tên: không phải đỏ sẵn.
func (r *Runner) redAtBase(ctx context.Context, rs *runState, root string, ts []testreport.Test,
	run func(string, []testreport.Test, string) (eval.CmdResult, testreport.Report, bool)) bool {
	if len(ts) == 0 {
		return false
	}
	base := verifyTempBase(r.Tasks.Dir)
	if os.MkdirAll(base, 0o700) != nil {
		return false
	}
	wt, err := os.MkdirTemp(base, "rb")
	if err != nil {
		return false
	}
	defer removeAllForce(wt)
	if _, err := gitrepo.Run(ctx, root, "worktree", "add", "-q", "--detach", wt, rs.t.State.Base); err != nil {
		return false
	}
	defer gitrepo.Run(context.WithoutCancel(ctx), root, "worktree", "remove", "--force", wt)
	res, rep, ran := run(wt, ts, "base")
	if !ran || res.OK() || !rep.Measured {
		return false
	}
	red := append(slices.Clone(rep.Failed), rep.TimedOut...)
	for _, x := range ts {
		if !slices.ContainsFunc(red, func(y testreport.Test) bool { return y.Package == x.Package && y.Top() == x.Top() }) {
			return false
		}
	}
	return true
}
