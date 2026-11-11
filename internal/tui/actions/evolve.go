package actions

import (
	"context"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/i18n"
	"bint/internal/tui/jobs"
)

// Hành động của màn Tiến hoá (giai đoạn 3): `evolve scan/propose/list/show/evaluate/apply/reject/rollback/history/
// queue/process/cycle`. propose/evaluate/process/cycle chạy nền (Action.Job) có log trực tiếp và lời hỏi giữa chừng;
// apply/reject/rollback chỉ dành cho người và có hàm chỉ đọc dựng hộp xác nhận (plan_apply, plan_rollback).

// SkillSync là kết quả đồng bộ skill sau khi áp dụng/quay lui đề xuất skill (như `syncAfterSkillChange` của CLI).
type SkillSync struct {
	// Ran: đề xuất là skill và đã thử đồng bộ.
	Ran     bool
	Install app.SkillInstall
	Err     error
}

// ApplyOutcome là kết quả áp dụng kèm đồng bộ skill.
type ApplyOutcome struct {
	Result evolve.ApplyResult
	Sync   SkillSync
}

// RollbackOutcome là kết quả quay lui kèm đồng bộ skill.
type RollbackOutcome struct {
	Result evolve.RollbackResult
	Sync   SkillSync
}

// ScanOutcome là kết quả quét cơ hội: Forced cho biết chế độ chỉ đọc đã ép bỏ quét Git và nạp spool.
type ScanOutcome struct {
	Result evolve.ScanResult
	// NoGit: lần quét này không quét lịch sử Git (người chọn hoặc chế độ chỉ đọc).
	NoGit bool
}

func openE(ctx context.Context, env Env) (*evolvesvc.Workspace, error) {
	return evolvesvc.OpenIngest(ctx, env.Dir, !env.ReadOnly)
}

func withE(id string, f func(ctx context.Context, env Env, e *evolvesvc.Workspace, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		e, err := openE(ctx, env)
		if err != nil {
			return res(id, opts, nil, err)
		}
		defer e.Close()
		v, err := f(ctx, env, e, opts)
		return res(id, opts, v, err)
	}
}

// lineWriter tách dòng từ io.Writer của dịch vụ (Log, Warn) rồi gửi về log của việc nền.
type lineWriter struct {
	mu  sync.Mutex
	br  *jobs.Bridge
	buf string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.br.Log(strings.TrimRight(w.buf[:i], "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// flush gửi nốt phần chưa kết thúc dòng.
func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf != "" {
		w.br.Log(w.buf)
		w.buf = ""
	}
}

// syncSkills đồng bộ skill tới các harness sau khi đề xuất skill đổi (không làm hỏng kết quả chính khi lỗi).
func syncSkills(env Env, e *evolvesvc.Workspace, p *evolve.Proposal) SkillSync {
	if p == nil || !p.IsSkill() || !e.K.Paths.Initialized() {
		return SkillSync{}
	}
	si, err := app.InstallSkills(e.K, env.Version, false)
	return SkillSync{Ran: true, Install: si, Err: err}
}

// evolveJob dựng JobFunc: mở không gian tiến hoá trong goroutine của việc, gọi f, đóng lại.
func evolveJob(f func(ctx context.Context, env Env, e *evolvesvc.Workspace, opts any, br *jobs.Bridge) (any, error)) JobFunc {
	return func(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
		e, err := evolvesvc.Open(ctx, env.Dir)
		if err != nil {
			return nil, err
		}
		defer e.Close()
		return f(ctx, env, e, opts, br)
	}
}

func evolveInfo(name string, id func(opts any) string) func(opts any) jobs.Info {
	return func(opts any) jobs.Info {
		label := "evolve " + name
		if id != nil {
			if v := id(opts); v != "" {
				label += " · " + shortID(v)
			}
		}
		return jobs.Info{Kind: jobs.KindEvolve, Label: label}
	}
}

func evolveActions() []Action {
	mk := func(id, name, key string, footer bool, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		return Action{ID: id, Screen: Evolve, Key: key, Footer: footer, Priority: prio, LabelKey: "action.evolve." + name + ".label",
			TitleKey: "action.evolve." + name + ".title", CLIPath: cli, Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	list := []Action{
		mk("evolve.scan", "scan", "s", true, 1, "evolve scan", evolvesvc.ScanOptions{}, false, false,
			withE("evolve.scan", func(ctx context.Context, env Env, e *evolvesvc.Workspace, o any) (any, error) {
				so := o.(evolvesvc.ScanOptions)
				if env.ReadOnly {
					so.NoGit = true // quét Git ghi tín hiệu vào cơ sở dữ liệu: không làm ở chế độ chỉ đọc (mục 7.4)
				}
				r, err := evolvesvc.Scan(ctx, e, so)
				return ScanOutcome{Result: r, NoGit: so.NoGit}, err
			})),
		mk("evolve.propose", "propose", "p", true, 2, "evolve propose", evolvesvc.ProposeOptions{}, true, true, nil),
		mk("evolve.list", "list", "A", true, 7, "evolve list", evolvesvc.ListOptions{}, false, false,
			withE("evolve.list", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.List(ctx, e, o.(evolvesvc.ListOptions))
			})),
		mk("evolve.show", "show", "Enter", true, 4, "evolve show", evolvesvc.ShowOptions{}, false, false,
			withE("evolve.show", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.Show(ctx, e, o.(evolvesvc.ShowOptions))
			})),
		mk("evolve.evaluate", "evaluate", "E", true, 3, "evolve evaluate", evolvesvc.EvaluateOptions{}, true, false, nil),
		mk("evolve.apply", "apply", "a", true, 1, "evolve apply", evolvesvc.ApplyOptions{}, true, true,
			withE("evolve.apply", func(ctx context.Context, env Env, e *evolvesvc.Workspace, o any) (any, error) {
				r, err := evolvesvc.Apply(ctx, e, o.(evolvesvc.ApplyOptions))
				out := ApplyOutcome{Result: r}
				if err == nil {
					out.Sync = syncSkills(env, e, r.Proposal)
				}
				return out, err
			})),
		// Kế hoạch áp dụng (chỉ đọc): hộp xác nhận liệt kê điều kiện, tính trước khi ghi.
		mk("evolve.plan_apply", "plan_apply", "", false, 0, "", evolvesvc.ApplyOptions{}, false, false,
			withE("evolve.plan_apply", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.PlanApply(ctx, e, o.(evolvesvc.ApplyOptions))
			})),
		mk("evolve.reject", "reject", "x", true, 2, "evolve reject", evolvesvc.RejectOptions{}, true, true,
			withE("evolve.reject", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.Reject(ctx, e, o.(evolvesvc.RejectOptions))
			})),
		mk("evolve.rollback", "rollback", "u", true, 3, "evolve rollback", evolvesvc.RollbackOptions{}, true, true,
			withE("evolve.rollback", func(ctx context.Context, env Env, e *evolvesvc.Workspace, o any) (any, error) {
				r, err := evolvesvc.Rollback(ctx, e, o.(evolvesvc.RollbackOptions))
				out := RollbackOutcome{Result: r}
				if err == nil {
					out.Sync = syncSkills(env, e, r.Proposal)
				}
				return out, err
			})),
		mk("evolve.plan_rollback", "plan_rollback", "", false, 0, "", evolvesvc.RollbackOptions{}, false, false,
			withE("evolve.plan_rollback", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.PlanRollback(ctx, e, o.(evolvesvc.RollbackOptions))
			})),
		mk("evolve.history", "history", "", false, 0, "evolve history", evolvesvc.HistoryOptions{}, false, false,
			withE("evolve.history", func(ctx context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.History(ctx, e, o.(evolvesvc.HistoryOptions))
			})),
		mk("evolve.queue", "queue", "", false, 0, "evolve queue", evolvesvc.QueueOptions{}, false, false,
			withE("evolve.queue", func(_ context.Context, _ Env, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.Queue(e, o.(evolvesvc.QueueOptions))
			})),
		mk("evolve.process", "process", "P", true, 5, "evolve process", evolvesvc.ProcessOptions{}, true, true, nil),
		mk("evolve.cycle", "cycle", "C", true, 4, "evolve cycle", evolvesvc.CycleOptions{}, true, false, nil),
		mk("evolve.next_sub", "next_sub", "v", false, 8, "", nil, false, false, nil),
		mk("evolve.refresh", "refresh", "ctrl+r", false, 9, "", nil, false, false, nil),
		mk("evolve.next_view", "next_view", ".", false, 9, "", nil, false, false, nil),
		mk("evolve.prev_view", "prev_view", ",", false, 9, "", nil, false, false, nil),
	}
	for i := range list {
		switch list[i].ID {
		case "evolve.propose":
			list[i].Job = evolveJob(func(ctx context.Context, _ Env, e *evolvesvc.Workspace, opts any, br *jobs.Bridge) (any, error) {
				o := opts.(evolvesvc.ProposeOptions)
				w := &lineWriter{br: br}
				defer w.flush()
				o.Warn = w
				return evolvesvc.Propose(ctx, e, o)
			})
			list[i].JobInfo = evolveInfo("propose", nil)
		case "evolve.evaluate":
			list[i].Job = evolveJob(runEvaluate)
			list[i].JobInfo = evolveInfo("evaluate", func(o any) string { return o.(evolvesvc.EvaluateOptions).ID })
		case "evolve.process":
			list[i].Job = evolveJob(func(ctx context.Context, _ Env, e *evolvesvc.Workspace, opts any, br *jobs.Bridge) (any, error) {
				o := opts.(evolvesvc.ProcessOptions)
				w := &lineWriter{br: br}
				defer w.flush()
				o.Warn = w
				return evolvesvc.Process(ctx, e, o)
			})
			list[i].JobInfo = evolveInfo("process", nil)
		case "evolve.cycle":
			list[i].Job = evolveJob(func(ctx context.Context, env Env, e *evolvesvc.Workspace, opts any, br *jobs.Bridge) (any, error) {
				o := opts.(evolvesvc.CycleOptions)
				w := &lineWriter{br: br}
				defer w.flush()
				o.Log, o.BintVersion, o.Runner = w, env.Version, env.CycleRunner
				return evolvesvc.Cycle(ctx, e, o)
			})
			list[i].JobInfo = evolveInfo("cycle", nil)
		case "evolve.plan_apply":
			list[i].FormID = "evolve.apply"
		case "evolve.plan_rollback":
			list[i].FormID = "evolve.rollback"
		}
	}
	return list
}

// runEvaluate chạy đối chiếu nền: dòng tiến độ, kế hoạch Probe và ước tính chi phí vào log; lời hỏi "Bắt đầu đối chiếu?"
// thành hộp thoại kèm chính các dòng đó để người quyết định theo ước tính.
func runEvaluate(ctx context.Context, env Env, e *evolvesvc.Workspace, opts any, br *jobs.Bridge) (any, error) {
	o := opts.(evolvesvc.EvaluateOptions)
	def := evolvesvc.EvaluateDefaults(e.K.Config)
	// Chỉ đặt cờ "tường minh" khi người đổi khỏi mặc định (như CLI: cờ Changed).
	o.RepeatsExplicit = o.Repeats != def.Repeats
	o.DeliveryExplicit = o.Delivery != "" && o.Delivery != def.Delivery
	o.BintVersion = env.Version
	var lines []string
	log := func(s string) {
		lines = append(lines, s)
		br.Log(s)
	}
	o.OnSelect = func(s evolvesvc.EvaluateSelection) {
		log(env.T("evolve.log.select", i18n.A{"split": s.Split, "pool": s.Pool, "unverified": s.Unverified, "chosen": s.Chosen}))
	}
	o.OnPlan = func(pp eval.ProbePlan, _ eval.VarModel, in eval.PlanInput, _ bool) {
		if !pp.Feasible {
			log(env.T("evolve.log.plan_infeasible", i18n.A{"available": in.Available, "repeats": pp.Repeats}))
			return
		}
		log(env.T("evolve.log.plan", i18n.A{"cases": pp.Cases, "repeats": pp.Repeats, "runs": pp.MaxRuns}))
	}
	o.OnEstimate = func(est eval.Estimate, arms []string, repeats int, _ *float64) {
		if est.TotalCost != nil {
			log(env.T("evolve.log.estimate", i18n.A{"runs": est.Runs, "cost": *est.TotalCost}))
			return
		}
		log(env.T("evolve.log.estimate_unknown", i18n.A{"runs": est.Runs}))
	}
	o.Progress = func(r eval.AttemptRow, n, total int) {
		br.Log(env.T("evolve.log.attempt", i18n.A{"n": n, "total": total, "case": r.CaseID, "arm": r.Arm, "repeat": r.Repeat, "status": r.Status}))
	}
	o.Confirm = func(r app.ConfirmRequest) bool {
		r.Detail = strings.Join(lines, "\n")
		return br.ConfirmRequest(r)
	}
	r, err := evolvesvc.Evaluate(ctx, e, o)
	if err == nil && r != nil && r.RunErr != nil {
		// Đợt có kết quả nhưng dừng giữa chừng (huỷ, hết ngân sách...): trả cả hai để màn hình hiện kết luận một phần.
		return r, r.RunErr
	}
	return r, err
}
