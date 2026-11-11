package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/testutil"
)

// seedKnowledge tạo repository tạm đã init và n mục candidate của agent (bên thứ ba) gắn nhãn lô, cộng
// một mục có bằng chứng trỏ tới tệp đã bị xoá (lỗi kiểm tra) cùng lô. Trả thư mục repository.
func seedKnowledge(t *testing.T, n int, tag string) (root string, good []string, bad string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	files := map[string]string{"README.md": "# demo\n", "docs/gone.md": "một\nhai\nba\n"}
	for i := 0; i < n; i++ {
		files[fmt.Sprintf("src/f%02d.go", i)] = fmt.Sprintf("package src\n\nfunc F%02d() {}\n", i)
	}
	root = testutil.NewRepo(t, files)
	mustInit(t, InitOptions{Dir: root})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	add := func(i int, evidence string) string {
		res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeFact, Title: fmt.Sprintf("Sự thật số %02d của lô", i),
			Body: fmt.Sprintf("Nội dung riêng của mục %02d, không trùng mục nào khác.", i), Paths: []string{"src/**"},
			Evidence: []string{evidence}, Origin: knowledge.OriginAgent, ThirdParty: true, Tags: []string{tag}})
		if err != nil || !res.Saved {
			t.Fatalf("mục %d: %v %+v", i, err, res.Check.Findings)
		}
		return res.Item.ID
	}
	for i := 0; i < n; i++ {
		good = append(good, add(i, fmt.Sprintf("src/f%02d.go:3", i)))
	}
	bad = add(n, "docs/gone.md:1-2")
	if err := os.Remove(filepath.Join(root, "docs", "gone.md")); err != nil {
		t.Fatal(err)
	}
	return root, good, bad
}

func TestPlanBatchTable(t *testing.T) {
	root, good, bad := seedKnowledge(t, 3, "nguon:lo-thu")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	// Một mục đã verified sẵn: người duyệt trước.
	if _, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: good[:1]}); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanBatch(ctx, k, append(append([]string{}, good...), bad, "khong-co-id"), knowledge.StatusVerified)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Ready) != 2 || plan.Ready[0] != good[1] || plan.Ready[1] != good[2] {
		t.Fatalf("Ready = %v", plan.Ready)
	}
	reasons := map[string]string{}
	for _, b := range plan.Blocked {
		reasons[b.ID] = b.Reason
		if b.Reason == BlockedChecks && len(b.Findings) == 0 {
			t.Errorf("mục %s bị loại vì kiểm tra phải kèm finding", b.ID)
		}
	}
	want := map[string]string{good[0]: BlockedAlreadyDone, bad: BlockedChecks, "khong-co-id": BlockedMissing}
	for id, r := range want {
		if reasons[id] != r {
			t.Errorf("%s: lý do %q, muốn %q (%v)", id, reasons[id], r, reasons)
		}
	}
	// Từ chối không bị chặn bởi lỗi kiểm tra.
	plan, err = PlanBatch(ctx, k, []string{bad, good[1]}, knowledge.StatusDeprecated)
	if err != nil || len(plan.Ready) != 2 || len(plan.Blocked) != 0 {
		t.Fatalf("từ chối: %+v %v", plan, err)
	}
	if _, err := PlanBatch(ctx, k, good, "stale"); !IsUsage(err) {
		t.Fatalf("trạng thái đích lạ phải là lỗi dùng sai: %v", err)
	}
}

func TestPlanBatchStaleOnlyFingerprintIsBlocked(t *testing.T) {
	root, good, _ := seedKnowledge(t, 2, "nguon:lo-thu")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	// Đổi nội dung vùng bằng chứng: chỉ fingerprint lệch (đường dẫn, dòng còn nguyên).
	p := filepath.Join(root, "src", "f00.go")
	if err := os.WriteFile(p, []byte("package src\n\nfunc F00() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanBatch(ctx, k, good, knowledge.StatusVerified)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Ready) != 1 || plan.Ready[0] != good[1] {
		t.Fatalf("mục lệch fingerprint không được vào lô duyệt: %+v", plan)
	}
	if len(plan.Blocked) != 1 || plan.Blocked[0].Reason != BlockedChecks || plan.Blocked[0].Findings[0].Code != knowledge.CodeFingerprint {
		t.Fatalf("%+v", plan.Blocked)
	}
}

func TestReviewBatchHumanOnly(t *testing.T) {
	root, good, bad := seedKnowledge(t, 3, "nguon:lo-thu")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	refs := append(append([]string{}, good...), bad)

	asAgent(t)
	for _, to := range []string{knowledge.StatusVerified, knowledge.StatusDeprecated} {
		if _, err := ReviewBatch(ctx, k, BatchOptions{Refs: refs, To: to, Reason: "x"}); !errors.Is(err, ErrAgentForbidden) {
			t.Fatalf("%s trong phiên agent: %v", to, err)
		}
	}
	if _, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: good}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("approve: %v", err)
	}
	if _, err := RejectKnowledge(ctx, k, RejectOptions{Refs: good, Reason: "x"}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("reject: %v", err)
	}
	if _, err := VerifyKnowledge(ctx, k, nil, true, time.Now()); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("verify --apply: %v", err)
	}
	if _, err := RevalidateKnowledge(ctx, k, RevalidateOptions{}); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("revalidate: %v", err)
	}
	if _, err := ReviewQueue(ctx, root, ReviewQueueDefaults()); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("review: %v", err)
	}
	items, _, _ := k.Items()
	for _, it := range items {
		if it.Status != knowledge.StatusCandidate {
			t.Fatalf("%s bị đổi trạng thái trong phiên agent: %s", it.ID, it.Status)
		}
	}

	asHuman(t)
	res, err := ReviewBatch(ctx, k, BatchOptions{Refs: refs, To: knowledge.StatusVerified})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outcomes) != 3 || len(res.Plan.Blocked) != 1 || res.Plan.Blocked[0].ID != bad {
		t.Fatalf("%+v", res)
	}
	items, _, _ = k.Items()
	for _, it := range items {
		want := knowledge.StatusVerified
		if it.ID == bad {
			want = knowledge.StatusCandidate
		}
		if it.Status != want {
			t.Errorf("%s: %s, muốn %s", it.ID, it.Status, want)
		}
	}
	// Từ chối lô cần lý do; mục lỗi kiểm tra vẫn từ chối được.
	if _, err := ReviewBatch(ctx, k, BatchOptions{Refs: []string{bad}, To: knowledge.StatusDeprecated}); !IsUsage(err) {
		t.Fatalf("thiếu lý do: %v", err)
	}
	res, err = ReviewBatch(ctx, k, BatchOptions{Refs: []string{bad}, To: knowledge.StatusDeprecated, Reason: "sai"})
	if err != nil || len(res.Outcomes) != 1 || res.Outcomes[0].Err != nil {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestKnowledgeOptionsValidate(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"add thiếu nội dung", AddOptions{Type: "rule"}.Validate(), "thiếu nội dung"},
		{"add thiếu type", AddOptions{Body: "x"}.Validate(), "thiếu --type"},
		{"add type lạ", AddOptions{Body: "x", Type: "zzz"}.Validate(), "--type phải là một trong"},
		{"reject thiếu lý do", RejectOptions{Refs: []string{"a"}, Reason: "  "}.Validate(), "cần --reason"},
		{"search rỗng", SearchOptions{Query: " "}.Validate(), "truy vấn"},
		{"context rỗng", ContextOptions{}.Validate(), "cần mô tả tác vụ hoặc --paths"},
		{"revalidate budget âm", RevalidateOptions{BudgetUSD: -1}.Validate(), "--budget"},
		{"review status lạ", ReviewQueueOptions{Statuses: []string{"lạ"}}.Validate(), "--status"},
	}
	for _, tc := range cases {
		if !IsUsage(tc.err) || !strings.Contains(tc.err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, tc.err)
		}
	}
	for name, err := range map[string]error{
		"add hợp lệ": AddOptions{Body: "x", Type: "rule"}.Validate(), "reject hợp lệ": RejectOptions{Refs: []string{"a"}, Reason: "x"}.Validate(),
		"approve": ApproveOptions{Refs: []string{"a"}}.Validate(), "inspect": InspectOptions{}.Validate(),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if d := AddDefaults(config.Config{}); d.Type != knowledge.TypeWarning {
		t.Errorf("AddDefaults: %+v", d)
	}
	if d := SearchDefaults(config.Config{}); d.Limit != 20 {
		t.Errorf("SearchDefaults: %+v", d)
	}
	if d := RevalidateDefaults(config.Default()); d.MaxItems != 10 || d.BatchSize != 5 || d.Timeout <= 0 {
		t.Errorf("RevalidateDefaults: %+v", d)
	}
	if d := InspectDefaults(config.Config{}); d.MaxCommits != 1000 {
		t.Errorf("InspectDefaults: %+v", d)
	}
}

func TestListShowSearchStatsDiff(t *testing.T) {
	root, good, bad := seedKnowledge(t, 3, "nguon:lo-thu")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	l, err := ListKnowledge(ctx, k, KnowledgeListOptions{Status: []string{knowledge.StatusCandidate}})
	if err != nil || len(l.Items) != 4 {
		t.Fatalf("%d %v", len(l.Items), err)
	}
	if l, _ = ListKnowledge(ctx, k, KnowledgeListOptions{Type: []string{knowledge.TypeRule}}); len(l.Items) != 0 {
		t.Fatalf("lọc loại: %d", len(l.Items))
	}
	d, err := ShowKnowledge(ctx, k, KnowledgeShowOptions{Ref: good[0]})
	if err != nil || len(d.Evidence) != 1 || d.Evidence[0].Missing || len(d.Evidence[0].Lines) == 0 || !d.Check.OK() {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = ShowKnowledge(ctx, k, KnowledgeShowOptions{Ref: bad})
	if err != nil || !d.Evidence[0].Missing || d.Check.OK() {
		t.Fatalf("mục lỗi: %+v %v", d, err)
	}
	hits, err := SearchKnowledge(ctx, root, SearchOptions{Query: "riêng", Limit: 5})
	if err != nil || len(hits) == 0 || len(hits) > 5 {
		t.Fatalf("%d %v", len(hits), err)
	}
	if _, err := StatsKnowledge(ctx, root, StatsOptions{NoIngest: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := StatsKnowledge(ctx, root, StatsOptions{}); err != nil {
		t.Fatal(err)
	}
	if files := TrackedFiles(k, 3); len(files) != 3 {
		t.Fatalf("%v", files)
	}
}

func TestBootstrapInspectContextServices(t *testing.T) {
	root, _, _ := seedKnowledge(t, 2, "nguon:lo-thu")
	ctx := context.Background()
	rep, err := Inspect(ctx, root, InspectDefaults(config.Config{}))
	if err != nil || rep.Files == 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	br, err := Bootstrap(ctx, root, BootstrapOptions{NoSkill: true, Version: "t"})
	if err != nil || br.Inspect == nil || br.InspectPath == "" || br.Counts[knowledge.StatusCandidate] != 3 || br.Skills != nil {
		t.Fatalf("%v %+v", err, br)
	}
	br, err = Bootstrap(ctx, root, BootstrapOptions{ReadOnly: true})
	if err != nil || br.InspectPath != "" || len(br.Items) != 3 {
		t.Fatalf("%v %+v", err, br)
	}
	content, err := BootstrapSkill(ctx, root, BootstrapSkillOptions{})
	if err != nil || len(content) == 0 {
		t.Fatalf("%v", err)
	}
	if _, err := BootstrapSkill(ctx, root, BootstrapSkillOptions{Name: "khong-co"}); err == nil {
		t.Fatal("skill lạ phải lỗi")
	}
	cr, err := ContextPack(ctx, root, ContextOptions{Paths: []string{"src/f00.go"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = cr
	if _, err := ContextPack(ctx, root, ContextOptions{}); !IsUsage(err) {
		t.Fatalf("%v", err)
	}
}

func TestPendingWorkSixSourcesAndSourceFailure(t *testing.T) {
	root, _, _ := seedKnowledge(t, 3, "nguon:lo-thu")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	// Thêm một mục không thuộc lô.
	if res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeFact, Title: "Mục lẻ", Body: "Không có nhãn lô nào cả.",
		Evidence: []string{"README.md:1"}, Origin: knowledge.OriginAgent}); err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	rep, err := PendingWork(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.KnowledgeBatches) != 1 || rep.KnowledgeBatches[0].Tag != "nguon:lo-thu" || rep.KnowledgeBatches[0].Candidate != 4 || rep.KnowledgeOther != 1 {
		t.Fatalf("tri thức: %+v", rep)
	}
	if rep.KnowledgeTotal() != 5 {
		t.Fatalf("tổng: %d", rep.KnowledgeTotal())
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("lỗi nguồn: %+v", rep.Errors)
	}
	// Thành phần harness candidate, tác vụ blocked, đề xuất, ứng viên eval, doctor: nguồn nào cũng không
	// làm hỏng nguồn khác; các nguồn trống cho danh sách rỗng.
	if len(rep.Components)+len(rep.BlockedTasks)+len(rep.Proposals)+len(rep.EvalCandidates) != 0 {
		t.Fatalf("repo mới không có việc nào khác: %+v", rep)
	}

	// Thành phần candidate.
	cdir := filepath.Join(root, ".bint", "harness", "guidance")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	g := "---\nid: g1\nscope: global\nstatus: candidate\norigin: agent\nversion: 1\n---\n# Tiêu đề\nNội dung\n"
	if err := os.WriteFile(filepath.Join(cdir, "g1.md"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	// Một nguồn hỏng (tác vụ: thư mục không đọc được) không làm mất các nguồn khác.
	ws, _ := OpenWorkspace(ctx, root, true)
	broken := filepath.Join(ws.Paths.Runtime, "tasks", "hong")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err = PendingWork(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ErrorOf(PendingSrcTasks) == nil || rep.KnowledgeTotal() != 5 || len(rep.Components) != 1 {
		t.Fatalf("nguồn tác vụ hỏng phải báo lỗi riêng, nguồn khác còn: %+v", rep)
	}
	if err := os.RemoveAll(broken); err != nil {
		t.Fatal(err)
	}

	// Tác vụ blocked.
	tk, err := task.NewStore(ws.Paths.Runtime).Create(task.Ticket{Goal: "chờ người", Scope: task.Scope{Allow: []string{"src/**"}}},
		task.State{Status: task.StatusBlocked, Workflow: "b-feat", Reason: "cần duyệt"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rep, err = PendingWork(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.BlockedTasks) != 1 || rep.BlockedTasks[0].ID != tk.State.ID || rep.BlockedTasks[0].Reason != "cần duyệt" {
		t.Fatalf("tác vụ: %+v", rep.BlockedTasks)
	}
	if len(rep.Components) != 1 || rep.Components[0].ID != "g1" || rep.ErrorOf(PendingSrcTasks) != nil {
		t.Fatalf("thành phần: %+v (%v)", rep.Components, rep.Errors)
	}

	// Còn tác vụ đọc được mà một tệp tác vụ khác hỏng: vẫn giữ tác vụ blocked đọc được và báo lỗi nguồn kèm số tệp hỏng.
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err = PendingWork(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.BlockedTasks) != 1 || rep.ErrorOf(PendingSrcTasks) == nil {
		t.Fatalf("tác vụ hỏng khi còn tác vụ đọc được phải có dòng lỗi riêng: %+v", rep)
	}
}

func TestEvidenceExcerptReportsRemainingLines(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "dòng %d\n", i)
	}
	e := knowledge.Evidence{Path: "x.go", Lines: "1-30"}
	lines, more := EvidenceExcerpt(e, []byte(b.String()), 12)
	if len(lines) != 12 || more != 18 {
		t.Fatalf("%d dòng, còn %d", len(lines), more)
	}
	for _, l := range lines {
		if strings.Contains(l, "nữa") {
			t.Fatalf("trích đoạn không mang chuỗi giao diện: %q", l)
		}
	}
	if lines, more = EvidenceExcerpt(knowledge.Evidence{Path: "x.go", Lines: "2-3"}, []byte(b.String()), 12); len(lines) != 2 || more != 0 {
		t.Fatalf("%d %d", len(lines), more)
	}
}

func TestAgentEnvVarsIsACopy(t *testing.T) {
	v := AgentEnvVars()
	if len(v) == 0 || len(v) != len(agentEnvVars) {
		t.Fatalf("%v", v)
	}
	v[0] = "ĐÃ_SỬA"
	if agentEnvVars[0] == "ĐÃ_SỬA" {
		t.Fatal("AgentEnvVars phải trả bản sao")
	}
}

// Kho trống: Items nil nhưng đã đọc được (ItemsRead), để CLI không nhầm với lỗi đọc tri thức.
func TestBootstrapEmptyKnowledgeIsItemsRead(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"README.md": "# demo\n"})
	mustInit(t, InitOptions{Dir: root})
	br, err := Bootstrap(context.Background(), root, BootstrapOptions{NoSkill: true})
	if err != nil || br.Items != nil || !br.ItemsRead {
		t.Fatalf("%v %+v", err, br)
	}
}
