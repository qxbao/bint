package evolve

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/store"
	"bint/internal/testutil"
	"bint/internal/ulid"
)

var agentVars = []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "GEMINI_CLI"}

func asHuman(t *testing.T) {
	for _, k := range agentVars {
		t.Setenv(k, "")
	}
}

var now = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fixture struct {
	root string
	k    *app.KnowledgeWS
	db   *sql.DB
}

func setup(t *testing.T) *fixture {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":               "module demo\n",
		"internal/db/db.go":    "package db\n\nfunc Open(ctx any) error { return nil }\n\nfunc Close() {}\n",
		"internal/api/h.go":    "package api\n\nfunc Handle() {}\n",
		"internal/core/x.go":   "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n",
		"internal/core/y.go":   "package core\n\nfunc Y() {}\n",
		"internal/old/old.go":  "package old\n\nfunc Old() {}\n",
		"internal/core/doc.md": "core\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	f := false
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, Hooks: &f, NoSkills: true, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := app.OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := k.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &fixture{root: root, k: k, db: st.DB}
}

func (f *fixture) item(t *testing.T, typ, status, title string, paths []string, ev ...string) *knowledge.Item {
	t.Helper()
	it := &knowledge.Item{Schema: knowledge.SchemaVersion, ID: ulid.New(), Type: typ, Status: status, Title: title,
		Body: title + ".", Scope: knowledge.Scope{Paths: paths}, Origin: knowledge.Origin{Kind: knowledge.OriginHuman},
		Author: "Rev", ReviewedBy: "Rev", Version: 1, CreatedAt: knowledge.Now(now.Add(-90 * 24 * time.Hour))}
	for _, s := range ev {
		e, err := app.ParseEvidenceSpec(s)
		if err != nil {
			t.Fatal(err)
		}
		it.Evidence = append(it.Evidence, e)
	}
	if err := knowledge.FillFingerprints(it, f.k.Source()); err != nil {
		t.Fatal(err)
	}
	if err := f.k.Store.Save(it); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // ULID tăng dần
	return it
}

// run thêm một lượt đã kết thúc; label: "good" | "bad" | "" (không có tín hiệu).
func (f *fixture) run(t *testing.T, id string, ago time.Duration, label string, modified, read, retrieved []string) {
	t.Helper()
	ctx := context.Background()
	ts := store.FormatTime(now.Add(-ago))
	if _, err := f.db.ExecContext(ctx, `INSERT INTO runs (run_id, status, workflow, started_at, finished_at, cost_usd)
		VALUES (?, 'completed', 'session', ?, ?, 0.5)`, id, ts, ts); err != nil {
		t.Fatal(err)
	}
	for kind, paths := range map[string][]string{"file.modified": modified, "file.read": read} {
		for _, p := range paths {
			if _, err := f.db.ExecContext(ctx, `INSERT INTO run_artifacts (artifact_id, run_id, kind, path, first_seen_at, last_seen_at, count)
				VALUES (?, ?, ?, ?, ?, ?, 2)`, ulid.New(), id, kind, p, ts, ts); err != nil {
				t.Fatal(err)
			}
		}
	}
	if label != "" {
		if err := runlog.UpsertSignal(ctx, f.db, runlog.Signal{RunID: id, Kind: runlog.SignalRating, Positive: label == "good",
			Strength: "strong", Source: "cli", DedupKey: "rating:" + id, ObservedAt: ts}); err != nil {
			t.Fatal(err)
		}
	}
	for i, it := range retrieved {
		if _, err := f.db.ExecContext(ctx, `INSERT INTO context_usages (usage_id, run_id, event_id, channel, item_id, rank, created_at)
			VALUES (?, ?, ?, 'hook:path', ?, ?, ?)`, ulid.New(), id, "ev-"+id, it, i+1, ts); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fixture) scan(t *testing.T) (ScanResult, *History) {
	t.Helper()
	items, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	h, err := LoadHistory(context.Background(), f.db, now, DefaultWindow)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Scan(context.Background(), ScanInput{DB: f.db, Cfg: f.k.Config, Items: items, Now: now, History: h})
	if err != nil {
		t.Fatal(err)
	}
	return res, h
}

func byKey(res ScanResult) map[string]Opportunity {
	m := map[string]Opportunity{}
	for _, o := range res.Opportunities {
		m[o.Key] = o
	}
	return m
}

func TestScanProposeApplyRollback(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	api := f.item(t, "warning", "verified", "Handle không được chặn", []string{"internal/api/**"}, "internal/api/h.go:3")
	dbA := f.item(t, "fact", "verified", "Database open requires context", []string{"internal/db/**"}, "internal/db/db.go:3")
	dbB := f.item(t, "fact", "verified", "Database open requires a context", []string{"internal/db/**"}, "internal/db/db.go:3", "internal/db/db.go:5")
	old := f.item(t, "fact", "verified", "Old package is legacy", []string{"internal/old/**"}, "internal/old/old.go:3")
	old.Status, old.StatusReason, old.UpdatedAt = knowledge.StatusStale, "fingerprint đổi", knowledge.Now(now.Add(-40*24*time.Hour))
	if err := f.k.Store.Save(old); err != nil {
		t.Fatal(err)
	}

	core := []string{"internal/core/x.go"}
	f.run(t, "R1", 5*time.Hour, "good", core, []string{"internal/core/y.go"}, []string{api.ID})
	f.run(t, "R2", 4*time.Hour, "bad", core, nil, []string{api.ID})
	f.run(t, "R3", 3*time.Hour, "bad", core, nil, []string{api.ID})
	f.run(t, "R4", 2*time.Hour, "", core, nil, nil) // unlabeled: không là bằng chứng
	// Lượt con của R1 (bước workflow) được gộp vào R1.
	if _, err := f.db.ExecContext(ctx, `INSERT INTO runs (run_id, parent_run_id, status, workflow, started_at) VALUES ('R1c', 'R1', 'completed', 'b-feat', ?)`,
		store.FormatTime(now.Add(-5*time.Hour))); err != nil {
		t.Fatal(err)
	}
	f.db.ExecContext(ctx, `INSERT INTO run_artifacts (artifact_id, run_id, kind, path, first_seen_at, last_seen_at) VALUES ('a1', 'R1c', 'file.read', 'internal/core/doc.md', 'x', 'x')`)

	res, h := f.scan(t)
	if res.Runs != 4 || res.Labeled != 3 {
		t.Fatalf("runs=%d labeled=%d", res.Runs, res.Labeled)
	}
	if r := h.ByID["R1"]; r == nil || !strings.Contains(strings.Join(r.Read, ","), "internal/core/doc.md") || r.Module != "internal/core" {
		t.Fatalf("R1 gộp lượt con: %+v", r)
	}
	ops := byKey(res)
	exp, ok := ops["exploration:internal/core"]
	if !ok || !exp.Ready || len(exp.Files) != 1 || exp.Files[0].Path != "internal/core/x.go" || exp.Files[0].Runs != 3 || len(exp.SupportingRuns) != 3 {
		t.Fatalf("exploration: %+v\n%+v", exp, res.Opportunities)
	}
	if hot := ops["failure:internal/core"]; !hot.Ready || hot.Files[0].Failures != 2 || len(hot.SupportingRuns) != 2 {
		t.Fatalf("failure hotspot: %+v", hot)
	}
	if u := ops["unused:"+api.ID]; !u.Ready || u.Action != ActionDeprecate || len(u.SupportingRuns) != 3 {
		t.Fatalf("unused: %+v", u)
	}
	if d := ops["duplicate:"+dbA.ID+"+"+dbB.ID]; !d.Ready || d.Action != ActionMerge {
		t.Fatalf("duplicate: %+v", ops)
	}
	if e := ops["expired:"+old.ID]; !e.Ready {
		t.Fatalf("expired: %+v", ops)
	}
	if res.Prerequisites.Quantitative {
		t.Fatal("không có tác vụ đánh giá thì không thể kiểm chứng định lượng")
	}

	// Đề xuất xác định (không agent).
	rep, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Now: now,
		Refs: []string{ops["unused:"+api.ID].ID, ops["duplicate:"+dbA.ID+"+"+dbB.ID].ID, "exploration:internal/core"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Outcomes) != 3 || rep.Outcomes[0].Proposal == nil || rep.Outcomes[1].Proposal == nil || rep.Outcomes[2].Proposal != nil {
		t.Fatalf("propose: %+v", rep.Outcomes)
	}
	dep, merge := rep.Outcomes[0].Proposal, rep.Outcomes[1].Proposal
	if dep.Risk != RiskMedium || dep.Plan.Kind != "non_inferiority" || dep.Quantitative || len(dep.SupportingRuns) != 3 || len(dep.Base) != 1 {
		t.Fatalf("deprecate proposal: %+v", dep)
	}
	if merge.Action != ActionMerge || len(merge.Changes) != 2 || merge.Changes[0].Op != ActionUpdate || merge.Changes[0].ItemID != dbA.ID ||
		merge.Changes[1].Op != ActionDeprecate || merge.Changes[1].ItemID != dbB.ID {
		t.Fatalf("merge: %+v", merge.Changes)
	}
	if !strings.Contains(rep.Outcomes[2].Skipped, "agent") {
		t.Fatalf("cơ hội cần agent mà không có agent: %+v", rep.Outcomes[2])
	}

	// Cơ hội đã có đề xuất chờ thì không đề xuất lại.
	res2, _ := f.scan(t)
	if u := byKey(res2)["unused:"+api.ID]; u.Ready || !strings.Contains(u.Blocked, dep.ID) {
		t.Fatalf("pending: %+v", u)
	}

	// Áp dụng: chưa kiểm chứng → cần --unverified.
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: dep.ID, Now: now}); err == nil || !strings.Contains(err.Error(), "--unverified") {
		t.Fatalf("apply chưa kiểm chứng: %v", err)
	}
	// Agent không được áp dụng.
	t.Setenv("BINT_AGENT", "1")
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: dep.ID, Unverified: true, Now: now}); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("agent apply: %v", err)
	}
	asHuman(t)
	before, _ := os.ReadFile(f.k.Store.PathFor(api.ID))
	ar, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: dep.ID[len(dep.ID)-6:], Unverified: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if ar.Verified || len(ar.Files) != 1 || ar.Proposal.Status != StatusApplied {
		t.Fatalf("apply: %+v", ar)
	}
	got, _ := knowledge.ReadFile(f.k.Store.PathFor(api.ID))
	if got.Status != knowledge.StatusDeprecated || !strings.Contains(got.StatusReason, dep.ID) {
		t.Fatalf("sau apply: %+v", got)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: dep.ID, Unverified: true, Now: now}); err == nil {
		t.Fatal("áp dụng lần hai phải lỗi")
	}

	// Đề xuất gộp: phiên bản gốc đổi (sửa tay mục) → không hợp lệ.
	dbB2, _ := knowledge.ReadFile(f.k.Store.PathFor(dbB.ID))
	dbB2.Body += "\nSửa tay."
	f.k.Store.Save(dbB2)
	if probs, _ := merge.CheckBase(f.k.Store); len(probs) != 1 || probs[0].ID != dbB.ID {
		t.Fatalf("base: %+v", probs)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: merge.ID, Unverified: true, Now: now}); err == nil || !strings.Contains(err.Error(), "phiên bản gốc") {
		t.Fatalf("apply với gốc đã đổi: %v", err)
	}
	if _, err := Reject(ctx, f.db, merge.ID, "gốc đã đổi", now); err != nil {
		t.Fatal(err)
	}

	// Quay lui: tệp đã bị sửa sau khi áp dụng → cần --force.
	cur, _ := os.ReadFile(f.k.Store.PathFor(api.ID))
	os.WriteFile(f.k.Store.PathFor(api.ID), append(cur, []byte("\nthêm\n")...), 0o644)
	if _, err := Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: dep.ID, Now: now}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("rollback xung đột: %v", err)
	}
	os.WriteFile(f.k.Store.PathFor(api.ID), cur, 0o644)
	rb, err := Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: dep.ID, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(f.k.Store.PathFor(api.ID)); string(after) != string(before) || rb.Proposal.Status != StatusRolledBack {
		t.Fatalf("rollback không khôi phục:\n%s", after)
	}
	hs, err := HistoryOf(ctx, f.db, "", true, nil)
	if err != nil || len(hs) != 2 || hs[0].Action != "rollback" || hs[1].Action != "apply" || hs[1].Verified {
		t.Fatalf("history: %+v %v", hs, err)
	}

	// Tín hiệu đến muộn: R2 được chấm lại good → nhãn căn cứ của đề xuất đã đổi.
	runlog.UpsertSignal(ctx, f.db, runlog.Signal{RunID: "R2", Kind: runlog.SignalRating, Positive: true, Strength: "strong",
		Source: "cli", DedupKey: "rating:R2", ObservedAt: store.FormatTime(now)})
	_, h3 := f.scan(t)
	if d := LabelDrift(dep, h3); len(d) != 1 || !strings.HasPrefix(d[0], "R2: failure → success") {
		t.Fatalf("drift: %v", d)
	}
}

func TestAgentProposalAndRisk(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	core := []string{"internal/core/x.go"}
	f.run(t, "R1", 5*time.Hour, "good", core, nil, nil)
	f.run(t, "R2", 4*time.Hour, "bad", core, nil, nil)
	f.run(t, "R3", 3*time.Hour, "bad", core, nil, nil)
	res, h := f.scan(t)
	op := byKey(res)["failure:internal/core"]
	if !op.Ready {
		t.Fatalf("op: %+v", op)
	}

	// Agent giả: ứng viên đầu có bằng chứng không tồn tại (bị loại), ứng viên sau hợp lệ.
	answer := `Phân tích xong.
{"proposals":[
 {"type":"warning","title":"Sai","body":"x","paths":["internal/core/**"],"evidence":["internal/core/nope.go:1"]},
 {"type":"warning","title":"Gọi Reset trước Retry trong internal/core","body":"` + "`Retry`" + ` trong internal/core/x.go giả định trạng thái đã được ` + "`Reset`" + `; gọi Reset trước.","paths":["internal/core/**"],"evidence":["internal/core/x.go:3-4#Retry"],"inputs":["internal/core/x.go"],"expected_benefit":"ít lượt thất bại hơn khi sửa core","reason":"x.go:3"}
]}`
	out, _ := json.Marshal(map[string]any{"type": "result", "result": answer, "total_cost_usd": 0.02})
	resp := filepath.Join(t.TempDir(), "resp.json")
	os.WriteFile(resp, out, 0o644)
	agent := &eval.CommandAgent{Command: "cat " + filepath.ToSlash(resp)}

	rep, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Agent: agent, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	o := rep.Outcomes[0]
	if o.Proposal == nil || len(o.Rejected) != 1 || !strings.Contains(o.Rejected[0], "evidence") {
		t.Fatalf("agent proposal: %+v", o)
	}
	p := o.Proposal
	ch := p.Changes[0]
	if p.Action != ActionCreate || ch.Op != ActionCreate || ch.Draft.Type != "warning" || p.Risk != RiskMedium || !strings.HasPrefix(p.Generator, "agent:") {
		t.Fatalf("proposal: %+v", p)
	}
	if _, err := os.Stat(f.k.Store.PathFor(ch.ItemID)); !os.IsNotExist(err) {
		t.Fatal("đề xuất không được ghi tệp tri thức trước khi áp dụng")
	}
	ar, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	it, err := knowledge.ReadFile(f.k.Store.PathFor(ch.ItemID))
	if err != nil || it.Status != knowledge.StatusCandidate || it.Origin.Kind != knowledge.OriginAgent || it.Origin.RunID != "evolve:"+p.ID ||
		it.Evidence[0].Fingerprint == "" || len(ar.Files) != 1 || ar.Files[0].Before != nil {
		t.Fatalf("mục tạo ra: %+v %v", it, err)
	}

	// Đề xuất sửa quy tắc đã duyệt: rủi ro cao → cần --confirm-risk.
	rule := f.item(t, "rule", "verified", "Không gọi Retry khi chưa Reset", []string{"internal/core/**"}, "internal/core/x.go:3")
	pr := &Proposal{Schema: ProposalSchema, ID: "ep_" + ulid.New(), Level: "L1", TargetType: "knowledge", Action: ActionDeprecate,
		TargetIDs: []string{rule.ID}, Opportunity: "manual:" + rule.ID, Changes: []Change{{Op: ActionDeprecate, ItemID: rule.ID, Reason: "thử"}},
		Status: StatusProposed, CreatedAt: store.FormatTime(now), UpdatedAt: store.FormatTime(now), Generator: GenDeterministic}
	items, _, _ := f.k.Items()
	if err := validateChanges(pr, indexItems(items)); err != nil {
		t.Fatal(err)
	}
	pr.Risk, pr.RiskReasons = assessRisk(pr, indexItems(items))
	pr.Base, _ = BaseOf(f.k.Store, []*knowledge.Item{rule})
	if pr.Risk != RiskHigh {
		t.Fatalf("risk: %s", pr.Risk)
	}
	if err := SaveProposal(ctx, f.db, pr); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: pr.ID, Unverified: true, Now: now}); err == nil || !strings.Contains(err.Error(), "--confirm-risk") {
		t.Fatalf("rủi ro cao: %v", err)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: pr.ID, Unverified: true, ConfirmRisk: true, Now: now}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateChangesBoundaries(t *testing.T) {
	verified := &knowledge.Item{ID: "A", Type: "fact", Status: knowledge.StatusVerified}
	by := map[string]*knowledge.Item{"A": verified}
	base := func(chs ...Change) *Proposal {
		return &Proposal{Level: "L1", TargetType: "knowledge", Action: ActionUpdate, Changes: chs}
	}
	for name, p := range map[string]*Proposal{
		"đổi loại":       base(Change{Op: ActionUpdate, ItemID: "A", Draft: &Draft{Type: "rule", Title: "t", Body: "b"}}),
		"xoá mục duyệt":  base(Change{Op: ActionDelete, ItemID: "A"}),
		"thao tác lạ":    base(Change{Op: "chmod", ItemID: "A"}),
		"không thay đổi": base(),
		"cấp khác":       {Level: "L2", TargetType: "skill", Action: ActionUpdate, Changes: []Change{{Op: ActionDeprecate, ItemID: "A"}}},
	} {
		if err := validateChanges(p, by); err == nil {
			t.Errorf("%s: phải bị từ chối", name)
		}
	}
}

func TestComputeVerdict(t *testing.T) {
	stat := func(m, lo, hi float64) eval.Stat { return eval.Stat{Mean: m, CI: eval.CI{Lo: lo, Hi: hi}, N: 10} }
	rep := func(pass eval.Stat, cost *eval.Stat, diags ...eval.Diagnostic) eval.Report {
		return eval.Report{Paired: []eval.Paired{{Arm: eval.ArmCandidate, Baseline: eval.ArmBase, PassDiff: pass, CostDiff: cost}}, Diagnostics: diags}
	}
	sup := Plan{Kind: "superiority", Metric: "pass_rate", Margin: 0.05}
	non := Plan{Kind: "non_inferiority", Metric: "pass_rate", Margin: 0.05}
	cost := Plan{Kind: "superiority", Metric: "cost_usd", Margin: 0.05}
	c := stat(-0.2, -0.3, -0.1)
	for _, tc := range []struct {
		name string
		r    eval.Report
		p    Plan
		want string
	}{
		{"tăng rõ", rep(stat(0.2, 0.05, 0.35), nil), sup, VerdictSupported},
		{"không rõ", rep(stat(0.1, -0.05, 0.25), nil), sup, VerdictNotSupported},
		{"không kém", rep(stat(0, -0.04, 0.04), nil), non, VerdictSupported},
		{"có thể kém", rep(stat(0, -0.1, 0.1), nil), non, VerdictNotSupported},
		{"chi phí giảm", rep(stat(0, -0.02, 0.02), &c), cost, VerdictSupported},
		{"chi phí giảm nhưng kém", rep(stat(-0.05, -0.2, 0.1), &c), cost, VerdictNotSupported},
		{"mô hình đổi", rep(stat(0.2, 0.05, 0.35), nil, eval.Diagnostic{Level: "warn", Kind: "model_changed", Detail: "x"}), sup, VerdictInconclusive},
		{"economy không chặn", rep(stat(0.2, 0.05, 0.35), nil, eval.Diagnostic{Level: "warn", Kind: "leakage_economy", Detail: "x"}), sup, VerdictSupported},
		{"không CI", eval.Report{}, sup, VerdictInconclusive},
	} {
		if got := ComputeVerdict(tc.r, tc.p); got.Verdict != tc.want {
			t.Errorf("%s: %s (%s)", tc.name, got.Verdict, got.Detail)
		}
	}
}

func TestParseCandidates(t *testing.T) {
	txt := "trước {\"x\":1}\n{\"proposals\":[{\"type\":\"fact\",\"title\":\"a\",\"body\":\"{b}\"}]} sau\n{\n  \"proposals\": []\n}"
	c, ok := parseCandidates(txt)
	if !ok || len(c) != 0 {
		t.Fatalf("khối cuối cùng thắng: %v %+v", ok, c)
	}
	c, ok = parseCandidates("{\"proposals\":[{\"type\":\"fact\",\"title\":\"a\",\"body\":\"{b}\"}]}")
	if !ok || len(c) != 1 || c[0].Body != "{b}" {
		t.Fatalf("%v %+v", ok, c)
	}
	if _, ok := parseCandidates("không có JSON"); ok {
		t.Fatal("không có khối")
	}
}

func TestModuleOf(t *testing.T) {
	for in, want := range map[string]string{"internal/eval/report.go": "internal/eval", "cmd/main.go": "cmd", "README.md": ".", "a/b/c/d.go": "a/b"} {
		if got := ModuleOf(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
