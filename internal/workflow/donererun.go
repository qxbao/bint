package workflow

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/testreport"
)

// Cổng done của luồng tự động (B′) không được dừng chờ người chỉ vì test nhạy tải. Lần phân loại đỏ (redRerun) chạy lại test trên cây
// hiện tại và trên mốc trong lúc agent, thăm dò và các lần kiểm khác còn chạy song song: test đo thời gian có thể đỏ ở cả hai, và lần đỏ bị
// gắn nhầm "đỏ sẵn ở mốc". Ba lớp chữa:
//   - baseWasGreen: cây mốc từng có lần kiểm chứng xanh phủ các gói đó thì không phải đỏ sẵn (redUnstable);
//   - doneRerun: tới cổng done (không còn bước nào chạy), chạy lại riêng các test đó, tuần tự (-p=1); xanh thì ghi lần kiểm chứng xanh;
//   - workflow.preexisting_red: accept: test vẫn đỏ, thật sự đỏ sẵn và tác vụ không đổi gói của chúng thì cho done kèm ghi chú.

// baseWasGreen: có lần kiểm chứng (của mọi tác vụ) xanh trên đúng cây mốc của t (BaseTree) mà log cho thấy mọi gói của ts đều qua.
func (r *Runner) baseWasGreen(t *task.Task, ts []testreport.Test) bool {
	base := t.State.BaseTree
	if base == "" || len(ts) == 0 {
		return false
	}
	need := map[string]bool{}
	for _, x := range ts {
		if x.Package == "" {
			return false
		}
		need[x.Package] = true
	}
	all, _ := r.Tasks.List()
	for _, o := range all {
		for _, v := range append(slices.Clone(o.State.Verifications), o.State.Checks...) {
			if !v.Passed || v.Tree != base || v.Log == "" {
				continue
			}
			data, err := os.ReadFile(v.Log)
			if err != nil {
				continue
			}
			rep := testreport.Parse(string(data))
			if !rep.Measured {
				continue
			}
			ok := 0
			for _, p := range rep.Packages {
				if p.Passed && need[p.Name] {
					ok++
				}
			}
			if ok == len(need) {
				return true
			}
		}
	}
	return false
}

// doneRerun chạy ở cổng done, ngay trước khi chuyển done: lần kiểm chứng cuối đỏ và đã phân loại preexisting/unstable thì chạy lại
// riêng các test đỏ, tuần tự, lúc không còn bước nào chạy. Xanh: ghi một lần kiểm chứng xanh (như test hay đỏ). Vẫn đỏ và đúng là
// preexisting: workflow.preexisting_red: accept cho qua khi tác vụ không đổi gói của các test đó. Trả false khi workflow phải dừng (huỷ).
func (r *Runner) doneRerun(ctx context.Context, rs *runState) bool {
	vs := rs.t.State.Verifications
	n := len(vs)
	if n == 0 {
		return true
	}
	v := vs[n-1]
	if v.Passed || v.Voided || v.AcceptedPreexisting || (v.RedClass != redPreexisting && v.RedClass != redUnstable) {
		return true
	}
	failed, timed, ok := redTestsOf(&v)
	if !ok {
		return true
	}
	root, err := r.RootFor(rs.t)
	if err != nil {
		return true
	}
	flags, limit := goFlagsTimeoutOf(v)
	flags = append(slices.DeleteFunc(slices.Clone(flags), func(f string) bool { return strings.HasPrefix(f, "-p=") }), "-p=1")
	all := append(slices.Clone(failed), timed...)
	cmd := testreport.GoRerunCommand(flags, all, 2*limit)
	if cmd == "" {
		return true
	}
	lock, locked := r.lockStep(ctx, rs, "verify", LockExclusive)
	if !locked {
		return true
	}
	logPath := rs.t.LogPath(fmt.Sprintf("verify-%d-done.log", n))
	env := eval.VerifyEnvFrom(verifyEnviron())
	tmp, cleanup := r.verifyTempDir()
	if tmp != "" {
		env = append(env, verifyTempEnv(tmp)...)
	}
	done := r.beginActivity(rs.t.State.ID, task.ActivityRerun, cmd, logPath)
	res := eval.RunShell(ctx, root, cmd, 2*limit+time.Minute, logPath, env)
	done()
	cleanup()
	lock.Release()
	if ctx.Err() != nil {
		r.interrupt(rs, "người dùng huỷ khi chạy lại test đỏ ở cổng done")
		return false
	}
	names := strings.Join(v.RedTests, ", ")
	if res.OK() {
		nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
			if len(t.State.Verifications) < n {
				return nil
			}
			t.State.Verifications[n-1].RedClass = redFlaky
			g := t.State.Verifications[n-1]
			g.Passed, g.ExitCode, g.TimedOut, g.RedClass, g.At, g.Log = true, 0, false, "", r.at(), logPath
			t.State.Verifications = append(t.State.Verifications, g)
			return nil
		})
		if err == nil {
			rs.t = nt
		}
		rs.res.Notes = append(rs.res.Notes, "cổng done: chạy lại tuần tự lúc máy rảnh, "+names+" xanh: lần đỏ trước là do tải (test hay đỏ), không phải đỏ sẵn")
		r.logf("  cổng done: chạy lại test đỏ tuần tự: xanh (%s)", names)
		return true
	}
	if v.RedClass != redPreexisting || r.Cfg.Workflow.PreexistingRed != config.PreexistingAccept {
		return true
	}
	if hit := touchedPackages(rs.t.State.Touched, all); len(hit) > 0 {
		rs.res.Notes = append(rs.res.Notes, "workflow.preexisting_red: accept không áp: tác vụ đổi tệp trong gói của test đỏ ("+strings.Join(hit, ", ")+")")
		return true
	}
	nt, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error {
		if len(t.State.Verifications) >= n {
			t.State.Verifications[n-1].AcceptedPreexisting = true
		}
		return nil
	})
	if err == nil {
		rs.t = nt
	}
	rs.res.Notes = append(rs.res.Notes, "cổng done: "+names+" đỏ sẵn ở mốc (vẫn đỏ khi chạy lại tuần tự) và tác vụ không đổi gói của chúng: "+
		"chấp nhận theo workflow.preexisting_red: accept; nên sửa riêng các test này")
	return true
}

// touchedPackages trả các gói (đường dẫn import) của ts có tệp trong touched (đường dẫn tương đối gốc repo, gạch xuôi): gói khớp khi
// đường dẫn import kết thúc bằng thư mục của tệp.
func touchedPackages(touched []string, ts []testreport.Test) []string {
	var hit []string
	for _, x := range ts {
		for _, f := range touched {
			d := path.Dir(strings.ReplaceAll(f, `\`, "/"))
			if d == "." {
				continue
			}
			if x.Package == d || strings.HasSuffix(x.Package, "/"+d) {
				if !slices.Contains(hit, x.Package) {
					hit = append(hit, x.Package)
				}
				break
			}
		}
	}
	return hit
}
