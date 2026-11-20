package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/flow"
	"bint/internal/gitrepo"
	"bint/internal/harness/hookcore"
	"bint/internal/spool"
	"bint/internal/task"
	"bint/internal/ulid"
)

// Giai đoạn 4 luồng B′ (reports/Luồng core Bint có lệch hướng.md): đảo thứ tự spec → test → mã. b-feat LÕI chạy ngay sau b-req; thăm dò hộp đen
// (b-goal) chạy song song trong worktree tạm dựng từ mốc tác vụ (không thấy mã đang viết, không đọc thư mục tác vụ). Lõi xanh thì Bint gộp mọi tệp
// thăm dò vào worktree, đóng băng, rồi chạy đúng 1 lượt b-feat MỞ RỘNG. Driver session không chạy song song: b-goal chạy tuần tự sau lõi (pha probe).

// Các pha của State.BPrimePhase (task.State).
const (
	bprimePhaseProbe  = "probe"
	bprimePhaseExtend = "extend"
	bprimePhaseDone   = "done"
)

// noProbeNote là ghi chú khi thăm dò không để lại tệp có dòng nhãn: không đóng băng, không có lượt mở rộng.
const noProbeNote = "luồng B′: thăm dò không để lại tệp có dòng `" + probeMarker + "` nên không có thước đo đóng băng; không chạy lượt mở rộng"

// probeFileMax là cỡ tối đa (byte) của một tệp thăm dò được gộp; lớn hơn thì bỏ qua kèm ghi chú.
const probeFileMax = 1 << 20

// bprimeProbe là thăm dò nền đang chạy (hoặc đã xong) của một lượt Run.
type bprimeProbe struct {
	cancel  context.CancelFunc
	done    chan struct{}
	child   string
	harness string
	started time.Time
	// Kết quả: ghi trước khi đóng done, đọc sau <-done.
	res   eval.AgentResult
	files map[string][]byte
	err   string
	// reused: tệp lấy từ bản đã lưu của một lần thăm dò trước (tiến trình chết trước khi gộp), không chạy agent.
	reused bool
}

// probeSaveDir là thư mục (trong thư mục tác vụ) lưu tệp thăm dò ngay khi thăm dò xong, để resume không phải chạy lại thăm dò (đợt 7: tiến trình bị
// giết trước khi gộp làm mất ~6,6M token thăm dò). Thăm dò bị rào đọc cả thư mục tác vụ nên không thấy bản lưu.
const probeSaveDir = "probe-saved"

// probeManifest mô tả bản lưu: mốc base lúc thăm dò (khác mốc hiện tại thì bỏ) và danh sách tệp.
type probeManifest struct {
	Base  string   `json:"base"`
	Child string   `json:"child"`
	Files []string `json:"files"`
}

// saveProbe ghi tệp thăm dò và manifest vào thư mục tác vụ. Lỗi ghi chỉ làm mất khả năng dùng lại.
func saveProbe(dir, base, child string, files map[string][]byte) {
	root := filepath.Join(dir, probeSaveDir)
	removeAllForce(root)
	m := probeManifest{Base: base, Child: child}
	for rel, data := range files {
		dst := filepath.Join(root, "files", filepath.FromSlash(rel))
		if os.MkdirAll(filepath.Dir(dst), 0o700) != nil || os.WriteFile(dst, data, 0o600) != nil {
			removeAllForce(root)
			return
		}
		m.Files = append(m.Files, rel)
	}
	slices.Sort(m.Files)
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600) // manifest ghi sau cùng: có manifest nghĩa là đủ tệp
}

// loadSavedProbe đọc bản lưu khớp mốc base; nil khi không có hoặc không dùng được.
func loadSavedProbe(dir, base string) (map[string][]byte, string) {
	root := filepath.Join(dir, probeSaveDir)
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, ""
	}
	var m probeManifest
	if json.Unmarshal(data, &m) != nil || m.Base != base || len(m.Files) == 0 {
		return nil, ""
	}
	files := map[string][]byte{}
	for _, rel := range m.Files {
		b, err := os.ReadFile(filepath.Join(root, "files", filepath.FromSlash(rel)))
		if err != nil {
			return nil, ""
		}
		files[rel] = b
	}
	return files, m.Child
}

// stop huỷ thăm dò và chờ goroutine dọn xong (worktree tạm đã xoá khi trả về).
func (p *bprimeProbe) stop() {
	if p == nil {
		return
	}
	p.cancel()
	<-p.done
}

// bprimeSplit: tác vụ B′ này tách lõi / thăm dò / mở rộng. Không tách khi goal_check tắt, thiếu lệnh kiểm chứng, người đã gỡ thước đo hoặc đường nhanh.
func (r *Runner) bprimeSplit(rs *runState) bool {
	t := rs.t
	if !task.IsBPrime(t) || rs.flow.Name != StepDo || !rs.flow.IsBuiltin() || !rs.flow.HasKind(flow.KindGoal) {
		return false
	}
	if !r.wantGoal(t) || strings.TrimSpace(t.Ticket.Verify.Command) == "" || t.State.GoalDropped {
		return false
	}
	if c := t.State.Classification; c != nil && c.Fast {
		return false
	}
	return t.State.GoalTest == nil || t.State.BPrimePhase != ""
}

// setBPrime ghi pha/đếm của B′ vào phiếu.
func (r *Runner) setBPrime(rs *runState, f func(s *task.State)) {
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { f(&t.State); return nil }); err == nil {
		rs.t = t
	}
}

// probeDenyRead là danh sách rào đọc của thăm dò B′: cả thư mục tác vụ, không ngoại lệ (plan/tasks/requirement liệt kê riêng cho thông báo từ chối).
func probeDenyRead(t *task.Task) []string {
	return []string{t.ArtifactPath("plan"), t.ArtifactPath("tasks"), t.Dir, t.ArtifactPath("requirement")}
}

// probeTimeout là trần thời gian của thăm dò: min(timeout_minutes, slice_timeout_minutes) như vòng sửa B′.
func (r *Runner) probeTimeout() time.Duration {
	d := time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute
	if sd := time.Duration(r.Cfg.Workflow.SliceTimeoutMinutes) * time.Minute; sd > 0 && sd < d {
		return sd
	}
	return d
}

// startProbe khởi chạy thăm dò nền (driver process). Trả nil (kèm ghi chú) khi không dựng được: khi đó b-goal chạy tuần tự sau lõi.
func (r *Runner) startProbe(ctx context.Context, rs *runState) *bprimeProbe {
	t := rs.t
	fallback := func(why string) *bprimeProbe {
		rs.res.Notes = append(rs.res.Notes, "luồng B′: thăm dò nền không khởi chạy được ("+why+"): b-goal chạy tuần tự sau lõi")
		return nil
	}
	base := strings.TrimSpace(t.State.Base)
	if base == "" {
		return fallback("phiếu không có mốc base")
	}
	if files, child := loadSavedProbe(t.Dir, base); files != nil {
		p := &bprimeProbe{cancel: func() {}, done: make(chan struct{}), child: child, started: time.Now(), files: files, reused: true}
		close(p.done)
		rs.res.Notes = append(rs.res.Notes, "luồng B′: dùng lại tệp thăm dò đã lưu của lượt "+child+" (không chạy lại thăm dò)")
		return p
	}
	spec := rs.spec(StepGoal)
	agent, err := r.Agent(spec, nil, nil)
	if err != nil {
		return fallback(err.Error())
	}
	tmpBase := verifyTempBase(r.Tasks.Dir)
	if err := os.MkdirAll(tmpBase, 0o700); err != nil {
		return fallback(err.Error())
	}
	wt, err := os.MkdirTemp(tmpBase, "bp")
	if err != nil {
		return fallback(err.Error())
	}
	if _, err := gitrepo.Run(ctx, r.Root, "worktree", "add", "-q", "--detach", wt, base); err != nil {
		removeAllForce(wt)
		return fallback("git worktree add: " + err.Error())
	}
	child := ulid.New()
	rs.probeDir = wt
	prompt := r.prompt(ctx, rs, StepGoal, child, false)
	rs.probeDir = ""
	env := []string{EnvRunID + "=" + child, EnvTaskID + "=" + t.State.ID, "BINT_AGENT=1",
		task.EnvRunnerPID + "=" + fmt.Sprint(os.Getpid()),
		EnvApprovedPaths + "=" + strings.Join(ApprovedPaths(t), "\n"),
		hookcore.EnvDenyRead + "=" + strings.Join(probeDenyRead(t), "\n")}
	sr := &stepRun{step: StepGoal, kind: flow.KindGoal, role: spec.Role, base: spec.Base, child: child, harness: agent.Name()}
	if mn, ok := agent.(eval.ModelNamer); ok {
		sr.reqModel = mn.RequestedModel()
	}
	if p := eval.EffectiveProfile(agent, eval.AgentRequest{}); p != nil {
		sr.prof = p
	}
	sessionID := ""
	if sr.harness == "claude" {
		sessionID = eval.NewSessionUUID()
	}
	mcpPath, mcpTools, cleanMCP := r.stepMCP(sr, t.State.ID)
	denySettings, cleanDeny := r.stepDenyReadSettings(sr, true)
	r.emitRun(child, spool.WorkflowStarted, map[string]any{"workflow": StepGoal, "parent_run_id": rs.runID, "harness": sr.harness, "model": sr.reqModel,
		"bint_version": r.Version, "task_id": t.State.ID})
	if nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error { t.State.LinkRun(child); return nil }); err == nil {
		rs.t = nt
	}
	log := rs.t.LogPath(StepGoal + "-" + child + ".log")
	pctx, cancel := context.WithCancel(ctx)
	p := &bprimeProbe{cancel: cancel, done: make(chan struct{}), child: child, harness: sr.harness, started: time.Now()}
	timeout := r.probeTimeout()
	root := r.Root
	r.logf("▶ b-goal [%s] chạy nền song song với b-feat lõi (lượt %s)…", sr.harness, child)
	go func() {
		defer close(p.done)
		defer cleanMCP()
		defer cleanDeny()
		defer func() {
			gitrepo.Run(context.WithoutCancel(ctx), root, "worktree", "remove", "--force", wt)
			removeAllForce(wt)
		}()
		res := agent.Run(pctx, eval.AgentRequest{Dir: wt, Prompt: prompt, SessionID: sessionID, Profile: sr.prof, Timeout: timeout, IdleTimeout: r.Cfg.Workflow.IdleTimeout(),
			LogPath: log, Env: env, WritableDirs: r.stateDirs(), MCPConfig: mcpPath, ExclusiveMCP: mcpPath != "", MCPTools: mcpTools, Settings: denySettings})
		p.res = res
		switch {
		case pctx.Err() != nil:
			p.err = "bị huỷ"
		case res.TimedOut:
			p.err = "quá trần thời gian của thăm dò"
		case res.Error != "" || res.ExitCode != 0:
			p.err = cmpStr(res.Error, fmt.Sprintf("agent thoát với mã %d", res.ExitCode))
		default:
			p.files, p.err = collectProbeFiles(pctx, wt)
			if p.err == "" && len(p.files) > 0 {
				saveProbe(t.Dir, base, child, p.files)
			}
		}
	}()
	return p
}

// collectProbeFiles đọc các tệp mới/sửa trong worktree tạm của thăm dò (git status), bỏ qua tệp quá lớn và thư mục.
func collectProbeFiles(ctx context.Context, wt string) (map[string][]byte, string) {
	out, err := gitrepo.Run(ctx, wt, "-c", "core.quotepath=false", "status", "--porcelain", "-uall")
	if err != nil {
		return nil, "git status: " + err.Error()
	}
	files := map[string][]byte{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 || strings.Contains(line[:2], "D") {
			continue
		}
		rel := strings.Trim(strings.TrimSpace(line[3:]), `"`)
		if i := strings.Index(rel, " -> "); i >= 0 {
			rel = rel[i+4:]
		}
		data, err := os.ReadFile(filepath.Join(wt, filepath.FromSlash(rel)))
		if err != nil || len(data) > probeFileMax {
			continue
		}
		files[rel] = data
	}
	return files, ""
}

// mergeProbe chờ thăm dò xong, ghi nhận lượt chạy (chi phí, bước), rồi chép các tệp hợp lệ vào cây làm việc và Touched. Trả đường dẫn đã gộp (đã sắp xếp).
// Tệp chạm đường dẫn bảo vệ/scope.deny, tệp trùng đường dẫn với tệp b-feat đã sửa (giữ bản b-feat) bị bỏ, kèm ghi chú.
func (r *Runner) mergeProbe(ctx context.Context, rs *runState, p *bprimeProbe) []string {
	select {
	case <-p.done:
	case <-ctx.Done():
		p.stop()
		return nil
	}
	// Bản lưu dùng lại: lượt thăm dò gốc đã chạy (và tính chi phí) ở lần chạy trước; không ghi thêm bước hay chi phí.
	if !p.reused {
		if p.res.CostUSD != nil {
			rs.spent += *p.res.CostUSD
			rs.costKnown = true
		} else {
			rs.costMissing = true
		}
		status := "completed"
		if p.err != "" {
			status = "failed"
		}
		rs.res.Steps = append(rs.res.Steps, StepResult{Step: StepGoal, RunID: p.child, Harness: p.harness, Model: p.res.Model, CostUSD: p.res.CostUSD,
			Duration: time.Since(p.started).Round(time.Second).String(), Error: p.err, Status: status})
		payload := map[string]any{"status": status, "end_reason": p.err, "model": p.res.Model, "task_id": rs.t.State.ID,
			"result_summary": truncate(strings.TrimSpace(p.res.Text), 500)}
		if p.res.CostUSD != nil {
			payload["cost_usd"] = *p.res.CostUSD
		}
		r.emitRun(p.child, spool.WorkflowFinished, payload)
	}
	defer removeAllForce(filepath.Join(rs.t.Dir, probeSaveDir)) // đã gộp (hoặc bỏ): bản lưu hết tác dụng
	if p.err != "" {
		rs.res.Notes = append(rs.res.Notes, "luồng B′: thăm dò nền kết thúc không có thước đo ("+p.err+"); b-feat chạy tiếp không có thước đo đóng băng")
		return nil
	}
	rels := make([]string, 0, len(p.files))
	for rel := range p.files {
		rels = append(rels, rel)
	}
	slices.Sort(rels)
	if !slices.ContainsFunc(rels, func(rel string) bool { return probeMarkerIn(p.files[rel]) }) {
		// Không tệp nào mang dòng nhãn: không gộp gì vào cây (tệp chưa đóng băng sẽ chỉ làm cây đổi sau kiểm chứng).
		rs.res.Notes = append(rs.res.Notes, noProbeNote)
		return nil
	}
	t := rs.t
	need := map[string]bool{}
	for _, f := range task.CheckScope(rels, t.Ticket, r.Cfg.Limits.ProtectedPaths).NeedsApproval() {
		need[f] = true
	}
	var merged, skipped []string
	for _, rel := range rels {
		switch {
		case need[rel]:
			skipped = append(skipped, rel+" (đường dẫn bảo vệ/scope.deny)")
		case slices.Contains(t.State.Touched, rel):
			skipped = append(skipped, rel+" (b-feat đã sửa tệp này, giữ bản b-feat)")
		default:
			dst := filepath.Join(r.Root, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil || os.WriteFile(dst, p.files[rel], 0o644) != nil {
				skipped = append(skipped, rel+" (không ghi được)")
				continue
			}
			merged = append(merged, rel)
		}
	}
	if len(skipped) > 0 {
		rs.res.Notes = append(rs.res.Notes, "luồng B′: không gộp tệp thăm dò: "+strings.Join(skipped, "; "))
	}
	if len(merged) > 0 {
		r.setBPrime(rs, func(s *task.State) { s.Touched = unionSorted(s.Touched, merged) })
	}
	return merged
}

// bprimeAfterCore chạy trong afterStep (bước implement B′), sau kiểm chứng và trước implementDone. handled=true: nó đã quyết định (cont là kết quả afterStep).
func (r *Runner) bprimeAfterCore(ctx context.Context, rs *runState) (cont, handled bool) {
	if !task.IsBPrime(rs.t) {
		return false, false
	}
	switch rs.t.State.BPrimePhase {
	case bprimePhaseExtend:
		r.setBPrime(rs, func(s *task.State) { s.BPrimePhase = bprimePhaseDone })
		return false, false
	case "":
	default:
		return false, false
	}
	done := func() (bool, bool) {
		rs.probe.stop()
		rs.probe = nil
		r.setBPrime(rs, func(s *task.State) { s.BPrimePhase = bprimePhaseDone })
		if !r.wideAfterGate(ctx, rs) { // không có lượt mở rộng: kiểm rộng một lần trước review (cổng theo pha, check.go)
			return false, true
		}
		return false, false
	}
	if !r.bprimeSplit(rs) {
		return done()
	}
	if red := redVerification(rs.t); red != nil && rs.t.State.BPrimeCoreFix < 1 {
		r.setBPrime(rs, func(s *task.State) { s.BPrimeCoreFix++ })
		rs.res.Notes = append(rs.res.Notes, "luồng B′: lõi chưa xanh (`"+cmpStr(red.Ran, red.Command)+"`): 1 lượt sửa lõi trước khi gộp thước đo")
		return true, true
	}
	p := rs.probe
	if p == nil {
		// Driver session, hoặc thăm dò nền không dựng được: b-goal chạy tuần tự ngay sau lõi (pickStep trả b-goal ở pha probe).
		core := slices.Clone(rs.t.State.Touched)
		r.setBPrime(rs, func(s *task.State) { s.BPrimePhase, s.BPrimeCoreTouched = bprimePhaseProbe, core })
		rs.res.Notes = append(rs.res.Notes, "luồng B′: lõi xong; b-goal (thăm dò hộp đen) chạy tuần tự rồi đến lượt mở rộng")
		return true, true
	}
	rs.probe = nil
	core := slices.Clone(rs.t.State.Touched)
	merged := r.mergeProbe(ctx, rs, p)
	if len(merged) == 0 {
		r.setBPrime(rs, func(s *task.State) { s.BPrimePhase = bprimePhaseDone })
		if len(rs.res.Notes) == 0 || rs.res.Notes[len(rs.res.Notes)-1] != noProbeNote {
			rs.res.Notes = append(rs.res.Notes, noProbeNote)
		}
		if !r.wideAfterGate(ctx, rs) {
			return false, true
		}
		return false, false
	}
	// Pha probe trong lúc đóng băng: kiểm phạm vi dừng chờ người thì resume vào stepGoalCheck (không chạy lại b-feat lõi).
	r.setBPrime(rs, func(s *task.State) { s.BPrimePhase, s.BPrimeCoreTouched = bprimePhaseProbe, core })
	rs.probeFiles = merged
	ok := r.afterGoal(ctx, rs)
	rs.probeFiles = nil
	if !ok {
		return false, true
	}
	if rs.t.State.GoalTest == nil {
		r.setBPrime(rs, func(s *task.State) { s.BPrimePhase = bprimePhaseDone })
		if !r.wideAfterGate(ctx, rs) {
			return false, true
		}
		return false, false
	}
	r.setBPrime(rs, func(s *task.State) { s.BPrimePhase = bprimePhaseExtend })
	lead := "lõi xanh"
	if redVerification(rs.t) != nil {
		lead = "lõi CHƯA xanh sau lượt sửa lõi (lượt mở rộng sửa trước)"
	}
	rs.res.Notes = append(rs.res.Notes, "luồng B′: "+lead+", đã gộp và đóng băng thước đo thăm dò; chạy 1 lượt b-feat mở rộng")
	if !r.probeConfirmGate(rs) {
		return false, true // workflow.probe_confirm: chờ người bổ sung loại đầu vào; resume --answer chạy lượt mở rộng
	}
	return true, true
}
