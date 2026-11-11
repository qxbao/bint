package actions_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/runlog"
	"bint/internal/skills"
	"bint/internal/tui/actions"
)

// skillProposal lưu một đề xuất skill (L2) hợp lệ vào cơ sở dữ liệu của repo đã init.
func skillProposal(t *testing.T, root string) string {
	t.Helper()
	e, err := evolvesvc.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	p := &evolve.Proposal{Schema: evolve.ProposalSchema, ID: "ep_skill1", Level: evolve.LevelL2, TargetType: evolve.TargetSkill, Action: evolve.ActionCreate,
		Opportunity: "op-skill", Reason: "thử", Generator: "agent:fake", Risk: evolve.RiskLow, Status: evolve.StatusProposed, CreatedAt: now, UpdatedAt: now,
		Base: []evolve.BaseVersion{}, SupportingRuns: []evolve.RunRef{}, Plan: evolve.Plan{Kind: "superiority", Metric: "pass_rate"},
		SkillChanges: []evolve.SkillChange{{Op: evolve.ActionCreate, Name: "src-order", Draft: &evolve.SkillDraft{Name: "src-order",
			Description: "Dùng khi sửa src/a.go: gọi A trước B.", Body: "1. Mở src/a.go.\n2. Gọi A trước B.", Triggers: skills.Triggers{Paths: []string{"src/**"}, Keywords: []string{"order"}},
			Evidence: []string{"src/a.go:3"}}}}}
	if err := evolve.SaveProposal(context.Background(), e.Store.DB, p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// Đề xuất skill: sau apply và rollback hành động đồng bộ skill tới harness (app.InstallSkills) và trả kết quả đi kèm,
// như `syncAfterSkillChange` của CLI; PlanApply/PlanRollback là hàm đọc, không ghi tệp.
func TestEvolveApplyRollbackSyncsSkills(t *testing.T) {
	root, _ := seed(t)
	ep := skillProposal(t, root)
	env := actions.Env{Dir: root, Version: "test"}
	skillFiles := func() []string {
		var out []string
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "SKILL.md" && strings.Contains(p, "src-order") {
				out = append(out, p)
			}
			return nil
		})
		return out
	}
	// Kế hoạch (đọc): chưa kiểm chứng và là skill.
	r := do(t, "evolve.plan_apply", env, evolvesvc.ApplyOptions{Ref: ep})
	pl, _ := r.Value.(*evolvesvc.ApplyPlan)
	if r.Err != nil || pl == nil || !pl.NeedsUnverified || !pl.IsSkill || pl.Blocked != "" {
		t.Fatalf("plan_apply: %+v %v", pl, r.Err)
	}
	if len(skillFiles()) != 0 {
		t.Fatal("plan_apply không được xuất bản skill")
	}
	// Không chọn --unverified: dịch vụ từ chối nêu rõ cờ.
	if r := do(t, "evolve.apply", env, evolvesvc.ApplyOptions{Ref: ep}); r.Err == nil || !strings.Contains(r.Err.Error(), "--unverified") {
		t.Fatalf("không cờ: %v", r.Err)
	}
	r = do(t, "evolve.apply", env, evolvesvc.ApplyOptions{Ref: ep, Unverified: true})
	out, _ := r.Value.(actions.ApplyOutcome)
	if r.Err != nil || out.Result.Proposal == nil || !out.Sync.Ran || out.Sync.Err != nil {
		t.Fatalf("apply: %+v %v", out, r.Err)
	}
	if len(skillFiles()) == 0 {
		t.Fatal("áp dụng đề xuất skill phải đồng bộ skill tới harness (không thấy SKILL.md)")
	}
	rp := do(t, "evolve.plan_rollback", env, evolvesvc.RollbackOptions{Ref: ep})
	if plan, _ := rp.Value.(*evolvesvc.RollbackPlan); rp.Err != nil || plan == nil || !plan.IsSkill || len(plan.Files) == 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("plan_rollback: %+v %v", rp.Value, rp.Err)
	}
	rb := do(t, "evolve.rollback", env, evolvesvc.RollbackOptions{Ref: ep})
	rout, _ := rb.Value.(actions.RollbackOutcome)
	if rb.Err != nil || !rout.Sync.Ran || rout.Sync.Err != nil {
		t.Fatalf("rollback: %+v %v", rout, rb.Err)
	}
	if len(skillFiles()) != 0 {
		t.Fatalf("quay lui đề xuất skill phải gỡ skill đã xuất bản: %v", skillFiles())
	}
}

// Ở chế độ chỉ đọc quét cơ hội ép bỏ quét Git và không nạp spool: số sự kiện trong cơ sở dữ liệu không đổi.
func TestEvolveScanReadOnlyForcesNoGitAndSkipsIngest(t *testing.T) {
	root, _ := seed(t)
	e, err := evolvesvc.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := e.Store.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	n0 := count()
	// Một phiên mới trong spool: chế độ chỉ đọc không được nạp nó.
	writeSpoolSession(t, e)
	r := do(t, "evolve.scan", actions.Env{Dir: root, ReadOnly: true}, evolvesvc.ScanOptions{})
	out, ok := r.Value.(actions.ScanOutcome)
	if r.Err != nil || !ok || !out.NoGit {
		t.Fatalf("chỉ đọc phải ép NoGit: %+v %v", r.Value, r.Err)
	}
	if count() != n0 {
		t.Fatal("chế độ chỉ đọc không được nạp spool")
	}
	r = do(t, "evolve.scan", actions.Env{Dir: root}, evolvesvc.ScanOptions{NoGit: true})
	if out, ok := r.Value.(actions.ScanOutcome); r.Err != nil || !ok || !out.NoGit {
		t.Fatalf("scan thường: %+v %v", r.Value, r.Err)
	}
	if count() <= n0 {
		t.Fatal("chế độ thường phải nạp spool")
	}
	e.Close()
}

// Hành động đọc của Tiến hoá chạy với dịch vụ thật: list, show, history, queue và plan_* trả đúng kiểu.
func TestEvolveReadActionsWithRealService(t *testing.T) {
	root, _ := seed(t)
	ep := skillProposal(t, root)
	env := actions.Env{Dir: root, Version: "test"}
	if r := do(t, "evolve.list", env, evolvesvc.ListOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	} else if rows, _ := r.Value.([]evolvesvc.ListRow); len(rows) != 1 || rows[0].Proposal.ID != ep || rows[0].State != evolve.StatusProposed {
		t.Fatalf("list: %+v", r.Value)
	}
	if r := do(t, "evolve.show", env, evolvesvc.ShowOptions{ID: ep}); r.Err != nil {
		t.Fatal(r.Err)
	} else if sr, _ := r.Value.(*evolvesvc.ShowResult); sr == nil || sr.Proposal.ID != ep {
		t.Fatalf("show: %+v", r.Value)
	}
	if r := do(t, "evolve.history", env, evolvesvc.HistoryOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	} else if h, _ := r.Value.(*evolvesvc.HistoryResult); h == nil || len(h.Entries) != 0 {
		t.Fatalf("history: %+v", r.Value)
	}
	if r := do(t, "evolve.queue", env, evolvesvc.QueueOptions{All: true}); r.Err != nil {
		t.Fatal(r.Err)
	} else if q, _ := r.Value.(*evolvesvc.QueueResult); q == nil || q.Execution == "" {
		t.Fatalf("queue: %+v", r.Value)
	}
	if r := do(t, "evolve.show", env, evolvesvc.ShowOptions{}); !app.IsUsage(r.Err) {
		t.Fatalf("thiếu ID phải là lỗi dùng sai: %v", r.Err)
	}
}

// Hành động của màn Lượt chạy với dịch vụ thật: nạp spool, danh sách, chi tiết, chấm (người), quét; ở chế độ chỉ đọc
// danh sách không nạp spool.
func TestRunsActionsWithRealService(t *testing.T) {
	root, _ := seed(t)
	e, err := evolvesvc.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	writeSpoolSession(t, e)
	e.Close()
	if r := do(t, "runs.list", actions.Env{Dir: root, ReadOnly: true}, app.RunsListOptions{Limit: 20}); r.Err != nil {
		t.Fatal(r.Err)
	} else if rows, _ := r.Value.([]app.RunRow); len(rows) != 0 {
		t.Fatalf("chỉ đọc không được nạp spool: %d lượt", len(rows))
	}
	env := actions.Env{Dir: root}
	if r := do(t, "runs.ingest", env, app.EventsIngestOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	} else if res, _ := r.Value.(runlog.IngestResult); res.Events != 2 {
		t.Fatalf("ingest: %+v", r.Value)
	}
	r := do(t, "runs.list", env, app.RunsListOptions{Limit: 20})
	rows, _ := r.Value.([]app.RunRow)
	if r.Err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %+v", r.Err, r.Value)
	}
	id := rows[0].Run.RunID
	if r := do(t, "runs.show", env, app.RunsShowOptions{Ref: id}); r.Err != nil {
		t.Fatal(r.Err)
	} else if d, _ := r.Value.(*app.RunDetail); d == nil || len(d.Events) != 2 {
		t.Fatalf("show: %+v", r.Value)
	}
	if r := do(t, "runs.rate", env, app.RunsRateOptions{Ref: id, Rating: "bad", Note: "x"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	r = do(t, "runs.show", env, app.RunsShowOptions{Ref: id})
	if d, _ := r.Value.(*app.RunDetail); r.Err != nil || d.Label != runlog.LabelFailure {
		t.Fatalf("sau khi chấm bad: %+v %v", r.Value, r.Err)
	}
	if r := do(t, "runs.scan", env, app.RunsScanOptions{Days: 7, RevertDays: 3}); r.Err != nil {
		t.Fatal(r.Err)
	} else if _, ok := r.Value.(runlog.ScanResult); !ok {
		t.Fatalf("scan: %+v", r.Value)
	}
	if r := do(t, "runs.lesson_show", env, evolvesvc.ExperienceShowOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := do(t, "runs.lesson_digest", env, evolvesvc.ExperienceDigestOptions{}); !app.IsUsage(r.Err) {
		t.Fatalf("digest thiếu tham chiếu: %v", r.Err)
	}
}
