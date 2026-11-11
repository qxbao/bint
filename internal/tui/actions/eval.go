package actions

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/jobs"
)

// Hành động của màn Đánh giá (giai đoạn 4): `eval mine/verify/list/show/spec/approve/reject/estimate/run/report/
// bootstrap/clean`. run, bootstrap, verify và mine có kiểm chứng chạy nền (Action.Job, loại jobs.KindEval, mỗi lúc
// một việc) có log trực tiếp và hộp xác nhận kèm ước tính; còn lại là hàm đọc/ghi nhanh (Action.Do).
//
// `eval approve/reject` không phải HumanOnly: CLI hiện không chặn InAgentSession ở hai lệnh này (mục 2 của kế hoạch
// không liệt kê eval trong thao tác chỉ dành cho người), và giai đoạn này giữ nguyên hành vi CLI.

// EvalAttempt là kết quả một lượt của đợt đang chạy (gửi về màn hình bằng jobs.DataMsg): N lượt đã xong trên Total.
type EvalAttempt struct {
	Row      eval.AttemptRow
	N, Total int
}

// EvalVerifyStep là mốc tiến độ kiểm chứng của việc `eval verify`/`eval mine --verify` (gửi bằng jobs.DataMsg).
type EvalVerifyStep struct {
	Index, Total int
	CaseID       string
	Done         bool
	// Status là trạng thái kiểm chứng sau khi chạy (chỉ khi Done).
	Status string
}

// EvalBootstrapProgress là mốc của vòng dựng tri thức (gửi bằng jobs.DataMsg).
type EvalBootstrapProgress struct{ Step app.EvalBootstrapStep }

func openEval(ctx context.Context, env Env) (*app.EvalWorkspace, error) {
	return app.OpenEval(ctx, env.Dir, true)
}

// withEval mở không gian đánh giá (có cơ sở dữ liệu) rồi chạy f; lỗi mở trả thẳng trong ResultMsg.
func withEval(id string, f func(ctx context.Context, env Env, e *app.EvalWorkspace, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		e, err := openEval(ctx, env)
		if err != nil {
			return res(id, opts, nil, err)
		}
		defer e.Close()
		v, err := f(ctx, env, e, opts)
		return res(id, opts, v, err)
	}
}

// evalJob dựng JobFunc: mở không gian đánh giá trong goroutine của việc, gọi f, đóng lại.
func evalJob(f func(ctx context.Context, env Env, e *app.EvalWorkspace, opts any, br *jobs.Bridge) (any, error)) JobFunc {
	return func(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
		e, err := openEval(ctx, env)
		if err != nil {
			return nil, err
		}
		defer e.Close()
		return f(ctx, env, e, opts, br)
	}
}

func evalInfo(name string, id func(opts any) string) func(opts any) jobs.Info {
	return func(opts any) jobs.Info {
		label := "eval " + name
		if id != nil {
			if v := id(opts); v != "" {
				label += " · " + shortID(v)
			}
		}
		return jobs.Info{Kind: jobs.KindEval, Label: label}
	}
}

func evalActions() []Action {
	mk := func(id, name, key string, footer bool, prio int, cli string, o any, writes bool, do DoFunc) Action {
		return Action{ID: id, Screen: Eval, Key: key, Footer: footer, Priority: prio, LabelKey: "action.eval." + name + ".label",
			TitleKey: "action.eval." + name + ".title", CLIPath: cli, Options: o, Writes: writes, Do: do}
	}
	list := []Action{
		mk("eval.mine", "mine", "m", true, 2, "eval mine", app.EvalMineOptions{}, true, nil),
		mk("eval.verify", "verify", "v", true, 3, "eval verify", app.EvalVerifyOptions{}, true, nil),
		mk("eval.list", "list", "A", true, 7, "eval list", app.EvalListOptions{}, false,
			withEval("eval.list", func(_ context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalList(e, o.(app.EvalListOptions))
			})),
		mk("eval.show", "show", "Enter", true, 4, "eval show", app.EvalShowOptions{}, false,
			withEval("eval.show", func(ctx context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalShow(ctx, e, o.(app.EvalShowOptions))
			})),
		mk("eval.spec", "spec", "p", true, 5, "eval spec", app.EvalSpecOptions{}, true,
			withEval("eval.spec", func(ctx context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalSpec(ctx, e, o.(app.EvalSpecOptions))
			})),
		mk("eval.approve", "approve", "a", true, 1, "eval approve", app.EvalApproveOptions{}, true,
			withEval("eval.approve", func(ctx context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalApprove(ctx, e, o.(app.EvalApproveOptions))
			})),
		mk("eval.reject", "reject", "x", true, 2, "eval reject", app.EvalRejectOptions{}, true,
			withEval("eval.reject", func(_ context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalReject(e, o.(app.EvalRejectOptions))
			})),
		mk("eval.estimate", "estimate", "E", true, 5, "eval estimate", app.EvalEstimateOptions{}, false,
			withEval("eval.estimate", func(ctx context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				eo := o.(app.EvalEstimateOptions)
				def := app.EvalEstimateDefaults(e.WS.Config)
				if len(eo.Arms) == 0 { // ô trống = nhánh theo cấu hình
					eo.Arms = def.Arms
				}
				eo.ArmsExplicit = !slices.Equal(eo.Arms, def.Arms)
				return app.EvalEstimate(ctx, e, eo)
			})),
		mk("eval.run", "run", "r", true, 1, "eval run", app.EvalRunOptions{}, true, nil),
		// Chạy tiếp một đợt chưa xong (--resume): dùng chung form và nhãn với eval.run.
		mk("eval.resume", "resume", "R", true, 3, "", app.EvalRunOptions{}, true, nil),
		mk("eval.report", "report", "", false, 0, "eval report", app.EvalReportOptions{}, false,
			withEval("eval.report", func(ctx context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalReport(ctx, e, o.(app.EvalReportOptions))
			})),
		mk("eval.bootstrap", "bootstrap", "B", false, 6, "eval bootstrap", app.EvalBootstrapOptions{}, true, nil),
		mk("eval.clean", "clean", "C", false, 7, "eval clean", app.EvalCleanOptions{}, true,
			withEval("eval.clean", func(_ context.Context, _ Env, e *app.EvalWorkspace, o any) (any, error) {
				return app.EvalClean(e, o.(app.EvalCleanOptions))
			})),
		// Mở tệp ứng viên trong $EDITOR để sửa đề bài (ghi tệp nên ẩn ở chế độ chỉ đọc).
		mk("eval.open_file", "open_file", "e", false, 6, "", EditOptions{}, true,
			func(ctx context.Context, env Env, o any) tea.Msg {
				eo := o.(EditOptions)
				return ExecMsg{ID: "eval.open_file", Opts: eo, Cmd: editorCmd(ctx, env, eo.Path, eo.Line)}
			}),
		// Hành động giao diện thuần.
		mk("eval.baseline", "baseline", "b", false, 8, "", nil, false, nil),
		mk("eval.split", "split", "t", false, 8, "", nil, false, nil),
		mk("eval.filter", "filter", "/", true, 6, "", nil, false, nil),
		mk("eval.refresh", "refresh", "ctrl+r", false, 9, "", nil, false, nil),
		mk("eval.next_view", "next_view", ".", false, 9, "", nil, false, nil),
		mk("eval.prev_view", "prev_view", ",", false, 9, "", nil, false, nil),
	}
	for i := range list {
		switch list[i].ID {
		case "eval.mine":
			list[i].Job = evalJob(runMine)
			list[i].JobInfo = func(o any) jobs.Info {
				// Khai thác không kiểm chứng chỉ đọc Git và ghi tệp ứng viên: việc thường, không cần khoá việc nặng.
				if !o.(app.EvalMineOptions).Verify {
					return jobs.Info{Kind: jobs.KindAction, Label: "eval mine"}
				}
				return jobs.Info{Kind: jobs.KindEval, Label: "eval mine"}
			}
		case "eval.verify":
			list[i].Job = evalJob(runVerify)
			list[i].JobInfo = evalInfo("verify", nil)
		case "eval.run":
			list[i].Job = evalJob(runEvalRun)
			list[i].JobInfo = evalInfo("run", nil)
		case "eval.resume":
			list[i].FormID = "eval.run"
			list[i].Job = evalJob(runEvalRun)
			list[i].JobInfo = func(o any) jobs.Info {
				id := o.(app.EvalRunOptions).Resume
				return jobs.Info{Kind: jobs.KindEval, Label: "eval run · " + shortID(id), Resume: "bint eval run --resume " + id}
			}
		case "eval.bootstrap":
			list[i].Job = evalJob(runBootstrap)
			list[i].JobInfo = evalInfo("bootstrap", nil)
		}
	}
	return list
}

// runMine khai thác ứng viên; với Verify kiểm chứng fail→pass từng ứng viên mới, tiến độ vào log.
func runMine(ctx context.Context, env Env, e *app.EvalWorkspace, opts any, br *jobs.Bridge) (any, error) {
	o := opts.(app.EvalMineOptions)
	o.Progress = verifyProgress(env, br)
	r, err := app.EvalMine(ctx, e, o)
	if r != nil {
		br.Log(env.T("eval.log.mined", i18n.A{"new": len(r.New), "dir": r.CandidatesDir}))
	}
	return r, err
}

func runVerify(ctx context.Context, env Env, e *app.EvalWorkspace, opts any, br *jobs.Bridge) (any, error) {
	o := opts.(app.EvalVerifyOptions)
	o.Progress = verifyProgress(env, br)
	return app.EvalVerify(ctx, e, o)
}

func verifyProgress(env Env, br *jobs.Bridge) func(app.EvalProgress) {
	return func(p app.EvalProgress) {
		step := EvalVerifyStep{Index: p.Index, Total: p.Total, CaseID: p.Case.ID, Done: p.Done}
		if p.Done {
			step.Status = p.Case.Verification.Status
			br.Log(env.T("eval.log.verified", i18n.A{"n": p.Index, "total": p.Total, "case": p.Case.ID, "status": step.Status}))
		} else {
			br.Log(env.T("eval.log.verifying", i18n.A{"n": p.Index, "total": p.Total, "case": p.Case.ID}))
		}
		br.Send(step)
	}
}

// runEvalRun chạy đợt (mới hoặc --resume) nền: ước tính, lưu ý và từng lượt vào log; kế hoạch ô và kết quả lượt về
// màn hình bằng thông điệp có kiểu; lời hỏi "Bắt đầu chạy?" thành hộp thoại kèm chính ước tính và lưu ý để người
// quyết định theo chi phí. Không bao giờ đặt Yes.
func runEvalRun(ctx context.Context, env Env, e *app.EvalWorkspace, opts any, br *jobs.Bridge) (any, error) {
	o := opts.(app.EvalRunOptions)
	def := app.EvalRunDefaults(e.WS.Config)
	// Chỉ đặt cờ "tường minh" khi người đổi khỏi mặc định (như CLI: cờ Changed).
	if len(o.Arms) == 0 { // ô trống = nhánh theo cấu hình
		o.Arms = def.Arms
	}
	o.ArmsExplicit = !slices.Equal(o.Arms, def.Arms)
	o.BudgetExplicit = !sameBudget(o.BudgetUSD, def.BudgetUSD)
	o.Yes, o.BintVersion = false, env.Version
	var lines []string
	log := func(s string) {
		lines = append(lines, s)
		br.Log(s)
	}
	w := &lineWriter{br: br}
	defer w.flush()
	o.Log = w
	o.OnResume = func(b eval.Batch) {
		log(env.T("eval.log.resume", i18n.A{"batch": b.BatchID, "status": b.Status}))
	}
	o.OnEstimate = func(est eval.Estimate, arms []string, repeats int, budget *float64) {
		if est.TotalCost != nil {
			log(env.T("eval.log.estimate", i18n.A{"runs": est.Runs, "cost": *est.TotalCost}))
		} else {
			log(env.T("eval.log.estimate_unknown", i18n.A{"runs": est.Runs}))
		}
		if budget != nil {
			log(env.T("eval.log.budget", i18n.A{"budget": *budget}))
		}
	}
	o.OnNotes = func(notes []string, _ bool) {
		for _, n := range notes {
			log(env.T("eval.log.note", i18n.A{"note": n}))
		}
	}
	o.OnPlan = func(p app.EvalRunPlan) {
		br.Send(p)
		log(env.T("eval.log.plan", i18n.A{"cases": len(p.CaseIDs), "arms": strings.Join(p.Arms, ","), "repeats": p.Repeats, "done": len(p.Done)}))
	}
	o.Progress = func(r eval.AttemptRow, n, total int) {
		br.Log(env.T("eval.log.attempt", i18n.A{"n": n, "total": total, "case": r.CaseID, "arm": r.Arm, "repeat": r.Repeat, "status": r.Status}))
		br.Send(EvalAttempt{Row: r, N: n, Total: total})
	}
	o.Confirm = func(r app.ConfirmRequest) bool {
		r.Detail = strings.Join(lines, "\n")
		return br.ConfirmRequest(r)
	}
	r, err := app.EvalRun(ctx, e, o)
	return r, err
}

func sameBudget(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// runBootstrap dựng tri thức tại commit gốc nền; hộp xác nhận kèm số tác vụ/commit gốc.
func runBootstrap(ctx context.Context, env Env, e *app.EvalWorkspace, opts any, br *jobs.Bridge) (any, error) {
	o := opts.(app.EvalBootstrapOptions)
	o.Yes, o.BintVersion = false, env.Version
	var lines []string
	log := func(s string) {
		lines = append(lines, s)
		br.Log(s)
	}
	o.OnPlan = func(cases, bases, todo int) {
		log(env.T("eval.log.bootstrap_plan", i18n.A{"cases": cases, "bases": bases, "todo": todo}))
	}
	o.OnStep = func(s app.EvalBootstrapStep) {
		switch {
		case s.Stopped:
			br.Log(env.T("eval.log.bootstrap_stopped", i18n.A{"spent": s.Spent, "budget": s.Budget, "left": s.RemainingBases}))
		case s.Err != nil:
			br.Log(env.T("eval.log.bootstrap_failed", i18n.A{"n": s.Index, "total": s.Total, "base": s.Base, "error": s.Err.Error()}))
		default:
			br.Log(env.T("eval.log.bootstrap_step", i18n.A{"n": s.Index, "total": s.Total, "base": s.Base}))
		}
		br.Send(EvalBootstrapProgress{Step: s})
	}
	o.Confirm = func(r app.ConfirmRequest) bool {
		r.Detail = strings.Join(lines, "\n")
		return br.ConfirmRequest(r)
	}
	return app.EvalBootstrap(ctx, e, o)
}
