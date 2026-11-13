package actions

import (
	"context"
	"strings"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/i18n"
	"bint/internal/tui/jobs"
)

// SkillTestOutcome là kết quả hành động system.skill_test (kết quả một phần khi bị huỷ giữa chừng).
type SkillTestOutcome struct {
	Result *app.SkillTestResult
}

// runSkillTest chạy `bint skill test` nền: ước tính và từng lượt vào log; lời hỏi "chạy?" thành hộp thoại kèm chính ước tính
// để người quyết định theo chi phí (agent không được gọi trước khi đồng ý). Không bao giờ đặt Yes.
func runSkillTest(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
	o, _ := opts.(app.SkillTestOptions)
	o.Yes = false
	var lines []string
	log := func(s string) {
		lines = append(lines, s)
		br.Log(s)
	}
	o.OnEstimate = func(e app.SkillTestEstimate) {
		if e.Total != nil {
			log(env.T("sy.skilltest.log.estimate", i18n.A{"runs": e.Runs, "arms": strings.Join(e.Arms, "+"), "cost": *e.Total}))
		} else {
			log(env.T("sy.skilltest.log.estimate_unknown", i18n.A{"runs": e.Runs, "arms": strings.Join(e.Arms, "+")}))
		}
		if !e.ReportsCost {
			log(env.T("sy.skilltest.log.no_cost"))
		}
	}
	o.Progress = func(r eval.SkillTestRun, n, total int) {
		status := "pass"
		switch {
		case r.Error != "":
			status = "error"
		case !r.Complied:
			status = "fail"
		}
		br.Log(env.T("sy.skilltest.log.run", i18n.A{"n": n, "total": total, "scenario": r.Scenario, "arm": r.Arm, "rep": r.Rep,
			"status": env.T("sy.skilltest.status." + status)}))
	}
	o.Confirm = func(r app.ConfirmRequest) bool {
		r.Detail = strings.Join(lines, "\n")
		return br.ConfirmRequest(r)
	}
	res, err := app.SkillTest(ctx, env.Dir, o)
	if res == nil {
		return nil, err
	}
	return SkillTestOutcome{Result: res}, err
}
