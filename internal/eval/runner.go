package eval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"bint/internal/ulid"
)

// Attempt là một ô trong ma trận tác vụ × nhánh × lần lặp.
type Attempt struct {
	Case   *Case
	Arm    string
	Repeat int
}

func (a Attempt) key() string { return fmt.Sprintf("%s-%s-%d", a.Case.ID, a.Arm, a.Repeat) }

// RunOptions điều khiển một đợt chạy.
type RunOptions struct {
	Cases          []*Case
	Arms           []string
	Repeats        int
	LeakageMode    string
	Agent          Agent
	RequestedModel string
	BudgetUSD      *float64
	EstimatedUSD   *float64
	// PriorRunUSD là chi phí tiên nghiệm mỗi lượt, chỉ dùng khi đợt chưa có lượt nào
	// đo được chi phí (eval.prior_run_usd). 0 = không áp tiên nghiệm (hành vi cũ).
	PriorRunUSD  float64
	Parallel     int
	Seed         uint64
	KeepWorkdirs bool
	BintVersion  string
	Notes        []string
	// ResumeBatch chạy tiếp các lượt chưa xong của một đợt cũ (cùng tác vụ, nhánh, lần lặp).
	ResumeBatch string
	Progress    func(AttemptRow, int, int)
	// RepeatMajor sắp kế hoạch theo lần lặp: mọi tác vụ và nhánh của lần lặp 1 (xáo trộn
	// trong lần lặp) chạy trước lần lặp 2, v.v. Nhờ đó tại mỗi ranh giới lần lặp mọi cặp
	// đều cân bằng. Look != nil ngầm bật.
	RepeatMajor bool
	// Look được gọi (tuần tự, không bao giờ đồng thời) khi mọi lượt của một lần lặp đã xong,
	// theo thứ tự lần lặp 1, 2, ...; khi chạy song song chỉ gọi sau khi lượt cuối của lần
	// lặp đó kết thúc hẳn. stop = true thì không bắt đầu lượt mới (lượt đang chạy vẫn xong)
	// và đợt có trạng thái "stopped_early". Khi tiếp tục (ResumeBatch), các lần lặp đã xong
	// từ trước cũng được "nhìn" lại theo thứ tự trước khi chạy lượt đầu tiên.
	Look func(repeatDone int) (stop bool, reason string)
	// OnBatch được gọi ngay sau khi đợt mới được tạo (trước lượt đầu tiên), để bên gọi
	// liên kết đợt với dữ liệu của nó (ví dụ đề xuất tiến hoá); lỗi thì đợt bị huỷ.
	OnBatch func(batchID string) error
	Now     func() time.Time
}

// RunResult tóm tắt một đợt.
type RunResult struct {
	BatchID  string
	Status   string
	Total    int
	Done     int
	Skipped  int
	SpentUSD float64
	// StopReason giải thích vì sao đợt dừng do ngân sách (rỗng nếu không).
	StopReason string
}

// Plan dựng danh sách lượt chạy, xáo trộn theo hạt giống để các nhánh xen kẽ nhau
// theo thời gian (giảm sai lệch do tải máy hoặc dịch vụ thay đổi giữa chừng).
func Plan(cases []*Case, arms []string, repeats int, seed uint64) []Attempt {
	var out []Attempt
	for _, c := range cases {
		for _, arm := range arms {
			for r := 1; r <= repeats; r++ {
				out = append(out, Attempt{Case: c, Arm: arm, Repeat: r})
			}
		}
	}
	rng := rand.New(rand.NewPCG(seed, 0x6576616c))
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// PlanByRepeat dựng danh sách lượt theo lần lặp: lần lặp 1 (mọi tác vụ × nhánh, xáo
// trộn theo hạt giống trong lần lặp) trước, rồi lần lặp 2, v.v. Cùng hạt giống cho cùng
// thứ tự nên tiếp tục (resume) một đợt vẫn chạy phần còn lại theo đúng thứ tự cũ.
func PlanByRepeat(cases []*Case, arms []string, repeats int, seed uint64) []Attempt {
	rng := rand.New(rand.NewPCG(seed, 0x6576616c))
	var out []Attempt
	for r := 1; r <= repeats; r++ {
		start := len(out)
		for _, c := range cases {
			for _, arm := range arms {
				out = append(out, Attempt{Case: c, Arm: arm, Repeat: r})
			}
		}
		part := out[start:]
		rng.Shuffle(len(part), func(i, j int) { part[i], part[j] = part[j], part[i] })
	}
	return out
}

// StatusStoppedEarly: đợt dừng theo kế hoạch tuần tự (Look báo đủ bằng chứng), khác với
// chạy dở do lỗi, huỷ hoặc hết ngân sách.
const StatusStoppedEarly = "stopped_early"

// StoppedEarlyNote là tiền tố ghi chú của đợt dừng sớm theo kế hoạch tuần tự.
const StoppedEarlyNote = "dừng sớm theo kế hoạch tuần tự: "

// InfraErrorPrefix mở đầu lỗi của lượt hỏng vì hạ tầng; tiếp tục đợt (--resume) chạy lại các lượt này.
const InfraErrorPrefix = "lỗi hạ tầng: "

// QuotaStopNote là tiền tố ghi chú của đợt dừng vì agent hết hạn mức sử dụng.
const QuotaStopNote = "dừng vì hết hạn mức: "

// ErrQuota: agent hết hạn mức sử dụng giữa đợt.
var ErrQuota = errors.New("agent hết hạn mức sử dụng")

// ErrBudget: ngân sách không đủ.
var ErrBudget = errors.New("vượt ngân sách")

// Run chạy một đợt đánh giá và ghi mọi lượt vào cơ sở dữ liệu.
func Run(ctx context.Context, env Env, db *sql.DB, opts RunOptions) (RunResult, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Parallel < 1 {
		opts.Parallel = 1
	}
	var res RunResult
	for _, arm := range opts.Arms {
		if err := ArmNotImplemented(arm); err != nil {
			return res, err
		}
	}
	for _, c := range opts.Cases {
		if _, err := env.TestCommand(c); err != nil {
			return res, err
		}
	}
	harnessVersion := opts.Agent.Version(ctx)
	done := map[string]bool{}
	var batch Batch
	if opts.ResumeBatch != "" {
		b, err := GetBatch(ctx, db, opts.ResumeBatch)
		if err != nil {
			return res, err
		}
		if b.Harness != opts.Agent.Name() {
			return res, fmt.Errorf("đợt %s chạy bằng harness %q, không tiếp tục bằng %q", b.BatchID, b.Harness, opts.Agent.Name())
		}
		if opts.BudgetUSD == nil {
			opts.BudgetUSD = b.BudgetUSD
		} else if b.BudgetUSD == nil || *opts.BudgetUSD != *b.BudgetUSD {
			// --budget khi tiếp tục là ngân sách tổng mới của cả đợt.
			b.Notes = append(b.Notes, fmt.Sprintf("ngân sách đổi thành $%.2f khi tiếp tục (%s)", *opts.BudgetUSD, nowString(opts.Now())))
			if _, err := db.ExecContext(ctx, `UPDATE eval_batches SET budget_usd = ? WHERE batch_id = ?`, *opts.BudgetUSD, b.BatchID); err != nil {
				return res, err
			}
		}
		// Tiếp tục một đợt đã dừng sớm: ghi chú dừng cũ hết hiệu lực, kết thúc mới sẽ ghi lại.
		b.Notes = slices.DeleteFunc(slices.Clone(b.Notes), func(n string) bool { return strings.HasPrefix(n, StoppedEarlyNote) })
		batch = b
		byID := map[string]*Case{}
		for _, c := range opts.Cases {
			byID[c.ID] = c
		}
		opts.Cases = nil
		for _, id := range b.CaseIDs {
			c, ok := byID[id]
			if !ok {
				return res, fmt.Errorf("đợt %s cần tác vụ %s nhưng không còn trong .bint/evals/cases", b.BatchID, id)
			}
			opts.Cases = append(opts.Cases, c)
		}
		opts.Arms, opts.Repeats, opts.LeakageMode, opts.Seed = b.Arms, b.Repeats, b.LeakageMode, b.Seed
		rows, err := Attempts(ctx, db, "batch_id = ?", b.BatchID)
		if err != nil {
			return res, err
		}
		for _, r := range rows {
			if r.Status != "running" && !(r.Status == "error" && strings.HasPrefix(r.Error, InfraErrorPrefix)) {
				done[fmt.Sprintf("%s-%s-%d", r.CaseID, r.Arm, r.Repeat)] = true
			}
		}
		if err := updateBatch(ctx, db, b.BatchID, "running", "", b.Notes); err != nil {
			return res, err
		}
	} else {
		ids := make([]string, len(opts.Cases))
		for i, c := range opts.Cases {
			ids[i] = c.ID
		}
		head, _ := gitString(ctx, env.RepoDir, "rev-parse", "HEAD")
		batch = Batch{
			BatchID: "eb_" + ulid.New(), Status: "running", Harness: opts.Agent.Name(), HarnessVersion: harnessVersion,
			RequestedModel: opts.RequestedModel, BintVersion: opts.BintVersion, LeakageMode: opts.LeakageMode,
			Arms: opts.Arms, Repeats: opts.Repeats, CaseIDs: ids, PromptVersion: promptVersion, Seed: opts.Seed,
			BudgetUSD: opts.BudgetUSD, EstimatedUSD: opts.EstimatedUSD, GitHead: head,
			StartedAt: nowString(opts.Now()), Notes: opts.Notes,
		}
		if err := createBatch(ctx, db, batch); err != nil {
			return res, err
		}
		if opts.OnBatch != nil {
			if err := opts.OnBatch(batch.BatchID); err != nil {
				return res, finishWith(ctx, db, &batch, "failed", opts.Now, err)
			}
		}
	}
	res.BatchID = batch.BatchID

	var plan []Attempt
	if opts.RepeatMajor || opts.Look != nil {
		plan = PlanByRepeat(opts.Cases, opts.Arms, opts.Repeats, opts.Seed)
	} else {
		plan = Plan(opts.Cases, opts.Arms, opts.Repeats, opts.Seed)
	}
	res.Total = len(plan)
	var todo []Attempt
	// remaining[r] = số lượt của lần lặp r chưa xong; về 0 thì lần lặp r đã xong hẳn.
	remaining := map[int]int{}
	for _, a := range plan {
		if !done[a.key()] {
			todo = append(todo, a)
			remaining[a.Repeat]++
		}
	}
	res.Done = len(plan) - len(todo)

	// Kiểm tra commit gốc và dựng dữ liệu đối chiếu rò rỉ cho mỗi tác vụ. Kho đệm commit
	// gốc được tạo lười ở lượt đầu dùng base và dọn ngay khi lượt cuối dùng base xong,
	// nên số kho tồn tại cùng lúc bị chặn bởi số lượt chạy song song thay vì số tác vụ.
	leakIns := map[string]*LeakInput{}
	for _, c := range opts.Cases {
		if err := checkBaseCommit(ctx, env.RepoDir, c.Source.Base); err != nil {
			return res, finishWith(ctx, db, &batch, "failed", opts.Now, err)
		}
		in, err := BuildLeakInput(ctx, env.RepoDir, c)
		if err != nil {
			return res, finishWith(ctx, db, &batch, "failed", opts.Now, err)
		}
		leakIns[c.ID] = &in
	}

	// pending đếm số lượt còn phải chạy trên mỗi base; về 0 thì dọn kho đệm của base.
	pending := map[string]int{}
	for _, a := range todo {
		pending[a.Case.Source.Base]++
	}
	defer func() { // lượt không được chạy (huỷ, hết ngân sách): dọn nốt kho còn lại
		for base := range pending {
			releaseBase(env, base)
		}
	}()
	var (
		mu         sync.Mutex
		budgetHit  bool
		quotaHit   bool
		stopReason string
		inflight   int
		wg         sync.WaitGroup
		jobs       = make(chan Attempt)
		finishedCt = res.Done
	)
	spent, costN, err := batchSpent(ctx, db, batch.BatchID)
	if err != nil {
		return res, err
	}
	// tryStart áp ngân sách ngay trước khi bắt đầu một lượt: không bắt đầu nếu đã
	// tiêu + chi phí dự kiến của các lượt đang chạy và của lượt này (theo trung bình
	// các lượt đã xong) vượt ngân sách. Khi chưa lượt nào có chi phí thì dùng chi phí
	// tiên nghiệm PriorRunUSD thay cho số đo; từ khi có lượt thật thì dùng chi phí đo
	// được. Nhờ vậy lượt đầu không chạy khi ngân sách còn lại nhỏ hơn chi phí ước tính.
	var (
		lookMu      sync.Mutex // Look không bao giờ chạy đồng thời
		nextLook    = 1
		lookStopped bool
		lookReason  string
	)
	// runLooks gọi Look theo thứ tự cho mọi lần lặp đã xong hẳn mà chưa được nhìn.
	runLooks := func() {
		if opts.Look == nil {
			return
		}
		lookMu.Lock()
		defer lookMu.Unlock()
		for {
			mu.Lock()
			k := nextLook
			ready := !lookStopped && k <= opts.Repeats && remaining[k] == 0
			if ready {
				nextLook++
			}
			mu.Unlock()
			if !ready {
				return
			}
			if stop, why := opts.Look(k); stop {
				mu.Lock()
				lookStopped, lookReason = true, why
				mu.Unlock()
				return
			}
		}
	}
	runLooks() // tiếp tục: các lần lặp đã xong từ trước
	tryStart := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if budgetHit || lookStopped || quotaHit {
			return false
		}
		if b := opts.BudgetUSD; b != nil {
			perRun, measured := opts.PriorRunUSD, false
			if costN > 0 {
				perRun, measured = spent/float64(costN), true
			}
			expected := perRun * float64(inflight+1)
			if spent >= *b || spent+expected > *b {
				budgetHit = true
				if measured {
					stopReason = fmt.Sprintf("còn $%.2f, chi phí ước tính %d lượt (đang chạy + lượt kế) ≈ $%.2f theo trung bình đo được $%.2f/lượt",
						max(*b-spent, 0), inflight+1, expected, perRun)
				} else {
					stopReason = fmt.Sprintf("còn $%.2f, chưa đo được chi phí lượt nào nên dùng giá trị tiên nghiệm $%.2f/lượt (eval.prior_run_usd); tăng --budget hoặc hạ eval.prior_run_usd",
						max(*b-spent, 0), perRun)
				}
				return false
			}
		}
		inflight++
		return true
	}
	worker := func() {
		defer wg.Done()
		for a := range jobs {
			if ctx.Err() != nil || !tryStart() {
				continue // xả hàng đợi
			}
			row := runAttempt(ctx, env, db, batch, opts, a, harnessVersion, leakIns[a.Case.ID])
			mu.Lock()
			inflight--
			base := a.Case.Source.Base
			pending[base]--
			if pending[base] == 0 {
				delete(pending, base)
				releaseBase(env, base)
			}
			if row.CostUSD != nil {
				spent += *row.CostUSD
				costN++
			}
			finishedCt++
			remaining[a.Repeat]--
			if row.Status == "skipped" {
				res.Skipped++
			}
			if row.quota && !quotaHit {
				quotaHit = true
				stopReason = strings.TrimPrefix(row.Error, InfraErrorPrefix)
			}
			n := finishedCt
			mu.Unlock()
			if opts.Progress != nil {
				opts.Progress(row, n, res.Total)
			}
			runLooks()
		}
	}
	for i := 0; i < opts.Parallel; i++ {
		wg.Add(1)
		go worker()
	}
	for _, a := range todo {
		mu.Lock()
		stop := budgetHit || lookStopped || quotaHit
		mu.Unlock()
		if ctx.Err() != nil || stop {
			break
		}
		select {
		case jobs <- a:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()

	res.Done = finishedCt
	res.SpentUSD = spent
	res.StopReason = stopReason
	status := "completed"
	switch {
	case ctx.Err() != nil:
		status = "cancelled"
	case lookStopped && finishedCt < res.Total:
		// Dừng theo kế hoạch tuần tự (không phải lỗi); ưu tiên hơn hết ngân sách vì đã đủ bằng chứng.
		status, res.StopReason = StatusStoppedEarly, lookReason
	case quotaHit:
		status = "interrupted"
		batch.Notes = append(batch.Notes, QuotaStopNote+stopReason)
	case budgetHit:
		status = "budget_exhausted"
	}
	res.Status = status
	dbStatus := status
	if status == StatusStoppedEarly {
		// Bảng chỉ cho phép các trạng thái cũ; dừng sớm theo kế hoạch là đợt "completed"
		// kèm ghi chú đánh dấu (StoppedEarlyNote) để báo cáo không coi là chạy dở.
		dbStatus = "completed"
		batch.Notes = append(batch.Notes, StoppedEarlyNote+res.StopReason)
	}
	if err := updateBatch(context.WithoutCancel(ctx), db, batch.BatchID, dbStatus, nowString(opts.Now()), batch.Notes); err != nil {
		return res, err
	}
	if status == "budget_exhausted" {
		return res, fmt.Errorf("%w: đã tiêu $%.2f / $%.2f (%s); chạy tiếp bằng `bint eval run --resume %s --budget <mới>`",
			ErrBudget, spent, *opts.BudgetUSD, stopReason, batch.BatchID)
	}
	if status == "interrupted" {
		return res, fmt.Errorf("%w (%s); đợi hạn mức đặt lại rồi chạy tiếp bằng `bint eval run --resume %s`",
			ErrQuota, stopReason, batch.BatchID)
	}
	return res, ctx.Err()
}

func finishWith(ctx context.Context, db *sql.DB, b *Batch, status string, now func() time.Time, err error) error {
	b.Notes = append(b.Notes, "lỗi: "+err.Error())
	updateBatch(context.WithoutCancel(ctx), db, b.BatchID, status, nowString(now()), b.Notes)
	return err
}

// runAttempt chạy một lượt: dựng thư mục cô lập tại commit gốc → đặt ngữ cảnh
// của nhánh → dựng môi trường → agent làm → áp tệp kiểm thử ẩn → chấm.
func runAttempt(ctx context.Context, env Env, db *sql.DB, b Batch, opts RunOptions, a Attempt, harnessVersion string, leakIn *LeakInput) AttemptRow {
	start := opts.Now()
	key := a.key()
	work := filepath.Join(env.WorkRoot, "runs", b.BatchID, key)
	logDir := filepath.Join(env.WorkRoot, "logs", b.BatchID, key)
	row := AttemptRow{
		AttemptID: "ea_" + ulid.New(), BatchID: b.BatchID, CaseID: a.Case.ID, CaseFingerprint: a.Case.Fingerprint(),
		CaseSplit: a.Case.Split, Arm: a.Arm, Repeat: a.Repeat, Harness: opts.Agent.Name(), HarnessVersion: harnessVersion,
		StartedAt: nowString(start), LogDir: logDir,
	}
	bg := context.WithoutCancel(ctx)
	if err := startAttempt(bg, db, row); err != nil {
		row.Status, row.Error = "error", err.Error()
		return row
	}
	finish := func(status, errMsg string) AttemptRow {
		row.Status = status
		if errMsg != "" {
			row.Error = errMsg
		}
		if ctx.Err() != nil && status == "error" {
			row.Error = "bị huỷ: " + row.Error
		}
		end := opts.Now()
		row.FinishedAt = nowString(end)
		d := end.Sub(start).Milliseconds()
		row.DurationMS = &d
		if err := finishAttempt(bg, db, row); err != nil && row.Error == "" {
			row.Error = err.Error()
		}
		if !opts.KeepWorkdirs {
			os.RemoveAll(work)
		}
		return row
	}
	os.MkdirAll(logDir, 0o700)

	// Harness không hỗ trợ nhánh này một cách trung thực (ví dụ thiếu MCP và hook
	// theo đường dẫn) → bỏ qua kèm lý do thay vì đo một nhánh khác dưới tên này.
	if ac, ok := opts.Agent.(ArmChecker); ok {
		if why := ac.ArmUnavailable(env.DeliveryArm(a.Arm)); why != "" {
			return finish("skipped", why)
		}
	}

	if err := prepareWorkdir(ctx, env, a.Case.Source.Base, work); err != nil {
		return finish("error", "dựng thư mục làm việc: "+err.Error())
	}
	setup, err := prepareArm(ctx, env, a.Arm, opts.LeakageMode, a.Case, leakIn, work, logDir)
	if errors.Is(err, ErrArmUnavailable) {
		return finish("skipped", err.Error())
	}
	if err != nil {
		return finish("error", "chuẩn bị nhánh: "+err.Error())
	}
	row.ContextSource = setup.ContextSource
	prep, err := gitCommitAll(ctx, work, "bint-eval: "+a.Arm)
	if err != nil {
		return finish("error", "commit chuẩn bị: "+err.Error())
	}

	if s := env.setupCommand(); s != "" {
		r := RunShell(ctx, work, s, env.timeout(), filepath.Join(logDir, "setup.log"), cleanEnv())
		ok := r.OK()
		row.SetupOK = &ok
		if !ok {
			return finish("error", fmt.Sprintf("lệnh dựng môi trường thất bại (mã %d, timeout=%v)", r.ExitCode, r.TimedOut))
		}
		// Tệp do bước dựng sinh ra (ví dụ node_modules) không tính là thay đổi của agent.
		if prep, err = gitCommitAll(ctx, work, "bint-eval: setup"); err != nil {
			return finish("error", "commit sau dựng môi trường: "+err.Error())
		}
	}

	// Nhánh có tập skill (.claude/skills do prepareArm đặt vào): cấp công cụ Skill và đo kích hoạt.
	_, skErr := os.Stat(filepath.Join(work, ".claude", "skills"))
	ar := opts.Agent.Run(ctx, AgentRequest{
		Dir: work, HasSkills: skErr == nil, Prompt: AgentPrompt(a.Case.Prompt), Timeout: env.timeout(),
		LogPath: filepath.Join(logDir, "agent.log"), CaseID: a.Case.ID, Arm: a.Arm,
		MCPConfig: setup.MCPConfig, Settings: setup.Settings, Env: evalContextEnv(setup),
	})
	if err := recordContextUsage(bg, db, row.AttemptID, setup); err != nil {
		return finish("error", "ghi nhận ngữ cảnh: "+err.Error())
	}
	adur := ar.Duration.Milliseconds()
	exit := int64(ar.ExitCode)
	row.AgentDurationMS, row.AgentExitCode = &adur, &exit
	row.NumTurns, row.InputTokens, row.OutputTokens, row.CostUSD, row.Model = ar.NumTurns, ar.InputTokens, ar.OutputTokens, ar.CostUSD, ar.Model
	row.SkillsActivated = ar.SkillsActivated
	if ctx.Err() != nil {
		return finish("error", "bị huỷ trong khi agent chạy")
	}
	if ar.Infra != "" {
		// Không chấm: lượt hỏng vì hạ tầng không nói gì về nhánh, chấm sẽ ghi nhầm thành trượt.
		row.quota = ar.QuotaHit
		return finish("error", InfraErrorPrefix+ar.Infra)
	}

	cls := NewClassifier(env.Cfg.Eval.Mine)
	files, lines, touched, err := changeStats(ctx, work, prep, cls)
	if err != nil {
		return finish("error", "đo thay đổi: "+err.Error())
	}
	f64, l64 := int64(files), int64(lines)
	row.FilesChanged, row.LinesChanged, row.TouchedTests = &f64, &l64, &touched

	// Tệp kiểm thử của commit được áp SAU khi agent làm xong (agent không thấy chúng).
	if err := copyFilesFromCommit(ctx, env.RepoDir, a.Case.Source.Commit, a.Case.TestFiles, work); err != nil {
		return finish("error", "áp tệp kiểm thử: "+err.Error())
	}
	cmd, _ := env.TestCommand(a.Case)
	tr := RunShell(ctx, work, cmd, env.timeout(), filepath.Join(logDir, "test.log"), cleanEnv())
	if ctx.Err() != nil {
		return finish("error", "bị huỷ trong khi chấm")
	}
	if tr.Err != nil {
		return finish("error", "không chạy được lệnh kiểm thử: "+tr.Err.Error())
	}
	te := int64(tr.ExitCode)
	passed := tr.OK()
	row.TestExitCode, row.TestPassed = &te, &passed

	status := "failed"
	if passed {
		status = "passed"
	}
	msg := ar.Error
	if ar.TimedOut {
		status = "timeout"
		msg = fmt.Sprintf("agent quá %v; vẫn chấm phần đã làm (kiểm thử %s)", env.timeout(), map[bool]string{true: "qua", false: "không qua"}[passed])
	}
	return finish(status, msg)
}
