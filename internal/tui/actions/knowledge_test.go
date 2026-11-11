package actions_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
)

func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range app.AgentEnvVars() {
		t.Setenv(k, "")
	}
}

func seed(t *testing.T) (root string, ids []string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root = testutil.NewRepo(t, map[string]string{"README.md": "# demo\n", "src/a.go": "package src\n\nfunc A() {}\n", "src/b.go": "package src\n\nfunc B() {}\n"})
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := app.OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range []string{"src/a.go:3", "src/b.go:3"} {
		res, err := app.AddKnowledge(context.Background(), k, app.AddOptions{Type: knowledge.TypeFact, Title: []string{"Sự thật A", "Sự thật B"}[i],
			Body: []string{"Hàm A làm việc A.", "Hàm B làm việc B."}[i], Evidence: []string{f}, Origin: knowledge.OriginAgent, Tags: []string{"nguon:lo"}})
		if err != nil || !res.Saved {
			t.Fatalf("%v %+v", err, res.Check.Findings)
		}
		ids = append(ids, res.Item.ID)
	}
	return root, ids
}

func do(t *testing.T, id string, env actions.Env, opts any) actions.ResultMsg {
	t.Helper()
	a, ok := actions.Default().ByID(id)
	if !ok || a.Do == nil {
		t.Fatalf("hành động %s không có Do", id)
	}
	r, ok := a.Do(context.Background(), env, opts).(actions.ResultMsg)
	if !ok {
		t.Fatalf("%s không trả ResultMsg", id)
	}
	return r
}

func statuses(t *testing.T, root string) map[string]string {
	t.Helper()
	k, err := app.OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := k.Items()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, it := range items {
		out[it.ID] = it.Status
	}
	return out
}

// Gọi thẳng Action.Do (bỏ qua khoá của giao diện) trong phiên agent: mọi thao tác chỉ dành cho người bị tầng
// dịch vụ từ chối bằng lỗi bọc ErrAgentForbidden và không ghi gì; ca ngược với asHuman ghi đúng
// [k:01M746PHJXDZN6QMA3BEVEDNPP] [k:01M746PJW8B5A8S1N5B3BEAPC0].
func TestHumanOnlyActionsRefuseAtServiceLayer(t *testing.T) {
	root, ids := seed(t)
	env := actions.Env{Dir: root}
	humanOnly := map[string]any{
		"knowledge.approve":         app.ApproveOptions{Refs: ids},
		"knowledge.approve_refresh": app.ApproveOptions{Refs: ids},
		"knowledge.reject":          app.RejectOptions{Refs: ids, Reason: "x"},
		"knowledge.deprecate":       app.RejectOptions{Refs: ids, Reason: "x"},
		"knowledge.approve_batch":   app.BatchOptions{Refs: ids},
		"knowledge.reject_batch":    app.BatchOptions{Refs: ids, Reason: "x"},
		"knowledge.verify_apply":    app.VerifyOptions{Apply: true},
		"knowledge.revalidate":      app.RevalidateOptions{BudgetUSD: 1, AgentCommand: "true"},
	}
	for id, a := range actions.Default().All() {
		_ = id
		if a.HumanOnly {
			if _, ok := humanOnly[a.ID]; !ok {
				t.Errorf("hành động HumanOnly %s chưa có ca kiểm thử", a.ID)
			}
		}
	}
	t.Setenv("CLAUDECODE", "1")
	for id, opts := range humanOnly {
		if r := do(t, id, env, opts); !errors.Is(r.Err, app.ErrAgentForbidden) {
			t.Errorf("%s trong phiên agent: %v, muốn lỗi bọc ErrAgentForbidden", id, r.Err)
		}
	}
	for id, st := range statuses(t, root) {
		if st != knowledge.StatusCandidate {
			t.Fatalf("%s bị đổi trạng thái trong phiên agent: %s", id, st)
		}
	}
	// Hành động đọc dùng được trong phiên agent.
	if r := do(t, "knowledge.plan_batch", env, app.BatchOptions{Refs: ids, To: knowledge.StatusVerified}); r.Err != nil || len(r.Value.(app.BatchPlan).Ready) != 2 {
		t.Fatalf("plan_batch: %+v", r)
	}
	// Agent vẫn thêm được mục, luôn ở candidate.
	r := do(t, "knowledge.add", env, app.AddOptions{Type: knowledge.TypeFact, Title: "Mục của agent", Body: "Nội dung riêng của agent.", Evidence: []string{"README.md:1"}})
	if ar, _ := r.Value.(app.AddResult); r.Err != nil || !ar.Saved || ar.Item.Status != knowledge.StatusCandidate || ar.Origin != knowledge.OriginAgent {
		t.Fatalf("agent thêm mục: %+v", r)
	}

	asHuman(t)
	if r := do(t, "knowledge.approve", env, app.ApproveOptions{Refs: ids[:1]}); r.Err != nil {
		t.Fatal(r.Err)
	} else if outs := r.Value.([]app.ReviewOutcome); len(outs) != 1 || outs[0].Err != nil {
		t.Fatalf("%+v", outs)
	}
	if r := do(t, "knowledge.approve_batch", env, app.BatchOptions{Refs: ids}); r.Err != nil || len(r.Value.(app.BatchResult).Plan.Ready) != 1 {
		t.Fatalf("approve_batch: %+v", r)
	}
	st := statuses(t, root)
	if st[ids[0]] != knowledge.StatusVerified || st[ids[1]] != knowledge.StatusVerified {
		t.Fatalf("%v", st)
	}
}

func TestEditReturnsExecWithFakeEditor(t *testing.T) {
	root, ids := seed(t)
	var gotPath string
	env := actions.Env{Dir: root, Editor: func(p string) *execCmd { gotPath = p; return newCmd("true") }}
	k, _ := app.OpenKnowledge(context.Background(), root)
	items, _, _ := k.Items()
	path := items[0].Path
	a, _ := actions.Default().ByID("knowledge.edit")
	msg := a.Do(context.Background(), env, actions.EditOptions{ID: ids[0], Path: path})
	if em, ok := msg.(actions.ExecMsg); !ok || em.Cmd == nil || gotPath != path || filepath.Base(gotPath) != ids[0]+".md" {
		t.Fatalf("%T %v %q", msg, msg, gotPath)
	}
	if _, err := os.Stat(gotPath); err != nil {
		t.Fatal(err)
	}
}

func TestStatsInReadOnlyDoesNotIngestSpool(t *testing.T) {
	root, _ := seed(t)
	ws, err := app.OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	spoolFile := filepath.Join(ws.Paths.Spool, "evt-1.json")
	if err := os.MkdirAll(ws.Paths.Spool, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spoolFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := func() int { es, _ := os.ReadDir(ws.Paths.Spool); return len(es) }
	before := snapshot()
	if r := do(t, "knowledge.stats", actions.Env{Dir: root, ReadOnly: true}, app.StatsOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if snapshot() != before {
		t.Fatalf("chỉ đọc không được nạp (xoá) spool: %d → %d", before, snapshot())
	}
}

func TestFilterOptionsToFilter(t *testing.T) {
	f := actions.FilterOptions{Status: "pending", Type: "rule", Origin: "agent", ThirdParty: true, Batch: "nguon:x", Path: "a/b.go", Text: "q"}.Filter()
	if len(f.Statuses) != 2 || f.Types[0] != "rule" || f.Origins[0] != "agent" || !f.ThirdParty || f.Batch != "nguon:x" || f.Path != "a/b.go" || f.Text != "q" {
		t.Fatalf("%+v", f)
	}
	if g := (actions.FilterOptions{Status: "verified"}).Filter(); len(g.Statuses) != 1 || g.Statuses[0] != "verified" {
		t.Fatalf("%+v", g)
	}
	if g := (actions.FilterOptions{Status: "pending", NoBatch: true}).Filter(); !g.NoBatch || g.Batch != "" {
		t.Fatalf("%+v", g)
	}
	if g := (actions.FilterOptions{NoBatch: true, Batch: "x"}).Filter(); g.NoBatch {
		t.Fatalf("nhập lô thì bỏ điều kiện không thuộc lô: %+v", g)
	}
	if !(actions.FilterOptions{}).Filter().Empty() {
		t.Fatal("rỗng")
	}
}

// Hành động context khai Writes=false nên không được ghi gì: dù shell mở TUI có BINT_SESSION_ID, xem Ngữ cảnh
// không ghi spool (lượt truy xuất) hay bộ nhớ phiên. Đối chứng: ContextPack gọi như CLI thì có ghi.
func TestContextActionDoesNotRecordRetrieval(t *testing.T) {
	root, _ := seed(t)
	t.Setenv("BINT_SESSION_ID", "sess-tui")
	ws, err := app.OpenWorkspace(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		n := 0
		_ = filepath.WalkDir(ws.Paths.Spool, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n
	}
	if a, _ := actions.Default().ByID("context"); a.Writes {
		t.Fatal("context phải là hành động chỉ-đọc")
	}
	if r := do(t, "context", actions.Env{Dir: root}, app.ContextOptions{Task: "hàm A", Session: "sess-field"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if n := count(); n != 0 {
		t.Fatalf("TUI không được ghi lượt truy xuất, có %d tệp spool", n)
	}
	if _, err := app.ContextPack(context.Background(), root, app.ContextOptions{Task: "hàm A"}); err != nil {
		t.Fatal(err)
	}
	if count() == 0 {
		t.Fatal("đối chứng: CLI có BINT_SESSION_ID phải ghi spool (nếu không, test này vô nghĩa)")
	}
}

// Bước ghi xuất bản không tự đặt Yes: chỉ ghi khi Confirm đồng ý với diff lúc ghi. Thiếu Confirm thì từ chối,
// Confirm trả false (diff khác bản đã xem) thì không ghi và trả diff mới.
func TestPublishWriteNeedsConfirmMatchingSeenDiff(t *testing.T) {
	root, ids := seed(t)
	k, err := app.OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := app.AddKnowledge(context.Background(), k, app.AddOptions{Type: knowledge.TypeRule, Title: "Luật X", Body: "Luôn gọi A trước B.",
		Evidence: []string{"src/a.go:3"}, Origin: knowledge.OriginHuman})
	if err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	if _, err := app.ApproveKnowledge(context.Background(), k, app.ApproveOptions{Refs: []string{res.Item.ID}}); err != nil {
		t.Fatal(err)
	}
	_ = ids
	env := actions.Env{Dir: root}
	agents := filepath.Join(root, "AGENTS.md")
	read := func() string { b, _ := os.ReadFile(agents); return string(b) }
	before := read()

	prev := do(t, "bootstrap.publish_preview", env, app.PublishOptions{})
	if prev.Err != nil {
		t.Fatal(prev.Err)
	}
	pr := prev.Value.(app.PublishReport)
	if !pr.Changed || pr.Diff == "" || pr.Written || read() != before {
		t.Fatalf("xem trước phải có thay đổi và không ghi: %+v", pr)
	}

	// Không Confirm: không ai trả lời được nên không ghi (không lén đặt Yes).
	if r := do(t, "bootstrap.publish", env, app.PublishOptions{}); r.Err == nil || read() != before {
		t.Fatalf("thiếu Confirm phải lỗi và không ghi: err=%v", r.Err)
	}
	// Confirm so Detail với một diff cũ khác: không ghi, báo cáo mang diff hiện tại để xem lại.
	var asked string
	r := do(t, "bootstrap.publish", env, app.PublishOptions{Confirm: func(req app.ConfirmRequest) bool {
		asked = req.Detail
		return req.Detail == "diff cũ đã xem"
	}})
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	rep := r.Value.(app.PublishReport)
	if rep.Written || asked != pr.Diff || rep.Diff != pr.Diff || read() != before {
		t.Fatalf("diff khác bản đã xem thì không được ghi: %+v asked=%q", rep, asked)
	}
	// Confirm đồng ý với đúng diff đã xem: ghi.
	r = do(t, "bootstrap.publish", env, app.PublishOptions{Confirm: func(req app.ConfirmRequest) bool { return req.Detail == pr.Diff }})
	if r.Err != nil || !r.Value.(app.PublishReport).Written || read() == before {
		t.Fatalf("đúng diff đã xem phải ghi: %v %+v", r.Err, r.Value)
	}
}
