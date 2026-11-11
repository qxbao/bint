package evolvesvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/app"
	"bint/internal/evolve"
	"bint/internal/spool"
	"bint/internal/ulid"
)

// unusedFixture dựng một mục tri thức được truy xuất ở 3 phiên rồi sinh đề xuất deprecate (xác định, không gọi
// mô hình) cho cơ hội `unused`.
func unusedFixture(t *testing.T) (*Workspace, string, string) {
	t.Helper()
	e, _ := fixture(t)
	ctx := context.Background()
	added, err := app.AddKnowledge(ctx, e.K, app.AddOptions{Type: "warning", Title: "Handle không được chặn", Body: "Handle chạy trên luồng chính.",
		Paths: []string{"internal/core/**"}, Evidence: []string{"internal/core/x.go:3"}})
	if err != nil || !added.Saved {
		t.Fatalf("thêm mục: %v %+v", err, added.Check)
	}
	id := added.Item.ID
	base := time.Now().Add(-6 * time.Hour)
	for i, name := range []string{"s1", "s2", "s3"} {
		at := base.Add(time.Duration(i) * time.Hour)
		ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
			return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: name, Payload: p}
		}
		rating := map[int]string{0: "good", 1: "bad", 2: "bad"}[i]
		if _, err := spool.Write(e.K.Paths.Spool,
			ev(spool.SessionStarted, at, map[string]any{"model": "claude-opus-5-5"}),
			ev(spool.FileModified, at.Add(time.Minute), map[string]any{"path": "internal/core/x.go"}),
			ev(spool.ContextRetrieved, at.Add(2*time.Minute), map[string]any{"channel": "hook:path", "ids": []any{id}}),
			ev(spool.RunRated, at.Add(4*time.Minute), map[string]any{"rating": rating}),
			ev(spool.SessionEnded, at.Add(3*time.Minute), map[string]any{"reason": "exit"})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.WS.Ingest(ctx, e.Store, time.Now()); err != nil {
		t.Fatal(err)
	}
	scan, err := Scan(ctx, e, ScanOptions{NoGit: true})
	if err != nil {
		t.Fatal(err)
	}
	op := ""
	for _, o := range scan.Opportunities {
		if o.Kind == "unused" && o.Ready {
			op = o.ID
		}
	}
	if op == "" {
		t.Fatalf("không có cơ hội unused: %+v", scan.Opportunities)
	}
	po := ProposeDefaults(e.K.Config)
	po.NoAgent, po.Refs = true, []string{op}
	res, err := Propose(ctx, e, po)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range res.Outcomes {
		if o.Proposal != nil {
			return e, o.Proposal.ID, id
		}
	}
	t.Fatalf("không có đề xuất: %+v", res.Outcomes)
	return nil, "", ""
}

func TestPlanApplyReadOnlyAndConditions(t *testing.T) {
	e, ep, itemID := unusedFixture(t)
	ctx := context.Background()
	before, _ := os.ReadFile(e.K.Store.PathFor(itemID))

	pl, err := PlanApply(ctx, e, ApplyOptions{Ref: ep})
	if err != nil {
		t.Fatal(err)
	}
	if pl.Blocked != "" || !pl.NeedsUnverified || pl.Verified || pl.Proposal.ID != ep || len(pl.Changes) != 1 || !strings.HasPrefix(pl.Changes[0], "deprecate:") {
		t.Fatalf("kế hoạch chưa kiểm chứng: %+v", pl)
	}
	if pl.NeedsConfirmRisk != (pl.Risk == evolve.RiskHigh) {
		t.Fatalf("NeedsConfirmRisk phải khớp rủi ro %q: %+v", pl.Risk, pl)
	}
	after, _ := os.ReadFile(e.K.Store.PathFor(itemID))
	if string(before) != string(after) {
		t.Fatal("PlanApply không được ghi tệp")
	}
	if st, _ := evolve.GetProposal(ctx, e.Store.DB, ep); st.Status != evolve.StatusProposed {
		t.Fatalf("PlanApply không được đổi trạng thái: %s", st.Status)
	}

	// Không chọn cờ thì Apply từ chối nêu rõ --unverified; có cờ thì áp dụng.
	if _, err := Apply(ctx, e, ApplyOptions{Ref: ep, ConfirmRisk: true}); err == nil || !strings.Contains(err.Error(), "--unverified") {
		t.Fatalf("thiếu --unverified: %v", err)
	}

	// Gốc đã đổi: sửa tệp mục → Blocked base.
	p := filepath.Join(e.K.Store.PathFor(itemID))
	if err := os.WriteFile(p, append(before, []byte("\nthêm\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	pl, err = PlanApply(ctx, e, ApplyOptions{Ref: ep})
	if err != nil || pl.Blocked != BlockedBase || pl.State != "outdated" || len(pl.BaseProblems) == 0 {
		t.Fatalf("gốc đã đổi: %v %+v", err, pl)
	}
	if err := os.WriteFile(p, before, 0o644); err != nil {
		t.Fatal(err)
	}

	// Đã áp dụng: không ở proposed.
	if _, err := Apply(ctx, e, ApplyOptions{Ref: ep, Unverified: true, ConfirmRisk: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	pl, err = PlanApply(ctx, e, ApplyOptions{Ref: ep})
	if err != nil || pl.Blocked != BlockedStatus || pl.BlockedDetail != evolve.StatusApplied {
		t.Fatalf("đã áp dụng: %v %+v", err, pl)
	}
	if _, err := PlanApply(ctx, e, ApplyOptions{}); !app.IsUsage(err) {
		t.Fatalf("thiếu ID: %v", err)
	}
}

func TestPlanRollbackConflictsAndForceExpect(t *testing.T) {
	e, ep, itemID := unusedFixture(t)
	ctx := context.Background()
	if _, err := PlanRollback(ctx, e, RollbackOptions{Ref: ep}); err == nil || !strings.Contains(err.Error(), "chỉ quay lui được đề xuất đã áp dụng") {
		t.Fatalf("chưa áp dụng: %v", err)
	}
	if _, err := Apply(ctx, e, ApplyOptions{Ref: ep, Unverified: true, ConfirmRisk: true}); err != nil {
		t.Fatal(err)
	}
	path := e.K.Store.PathFor(itemID)
	applied, _ := os.ReadFile(path)

	pl, err := PlanRollback(ctx, e, RollbackOptions{Ref: ep})
	if err != nil || len(pl.Files) != 1 || len(pl.Conflicts) != 0 || pl.Files[0].Before == nil || pl.Files[0].Conflict {
		t.Fatalf("không xung đột: %v %+v", err, pl)
	}

	// Sửa tệp sau khi áp dụng → xung đột; không Force thì Rollback từ chối.
	edited := string(applied) + "\nsửa tay\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	pl, err = PlanRollback(ctx, e, RollbackOptions{Ref: ep})
	if err != nil || len(pl.Conflicts) != 1 || !pl.Files[0].Conflict || pl.Files[0].Current == nil || *pl.Files[0].Current != edited {
		t.Fatalf("xung đột: %v %+v", err, pl)
	}
	seen := pl.SeenConflicts()
	if len(seen) != 1 || seen[pl.Conflicts[0]] != ContentHash(&edited) {
		t.Fatalf("SeenConflicts: %v", seen)
	}
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: ep}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("xung đột không Force: %v", err)
	}

	// Force với tập xung đột đã xem khác tập hiện tại → từ chối, tệp không đổi.
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: ep, Force: true, ExpectConflicts: map[string]string{"tep/khac.md": "-"}}); err == nil || !strings.Contains(err.Error(), "xung đột đã thay đổi") {
		t.Fatalf("tập khác: %v", err)
	}
	if cur, _ := os.ReadFile(path); string(cur) != edited {
		t.Fatal("tệp không được đổi khi từ chối")
	}
	// Cùng tập tệp nhưng nội dung bị sửa tiếp sau lúc xem → từ chối, nội dung mới không bị ghi đè.
	edited2 := edited + "sửa tiếp sau lúc xem\n"
	if err := os.WriteFile(path, []byte(edited2), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: ep, Force: true, ExpectConflicts: seen}); err == nil || !strings.Contains(err.Error(), "xung đột đã thay đổi") {
		t.Fatalf("nội dung đổi sau lúc xem: %v", err)
	}
	if cur, _ := os.ReadFile(path); string(cur) != edited2 {
		t.Fatal("tệp không được đổi khi từ chối (nội dung)")
	}
	pl, err = PlanRollback(ctx, e, RollbackOptions{Ref: ep})
	if err != nil {
		t.Fatal(err)
	}
	seen = pl.SeenConflicts()
	if p, _ := evolve.GetProposal(ctx, e.Store.DB, ep); p.Status != evolve.StatusApplied {
		t.Fatalf("trạng thái phải giữ applied: %s", p.Status)
	}
	// Force với đúng tập đã xem → quay lui.
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: ep, Force: true, ExpectConflicts: seen}); err != nil {
		t.Fatalf("force đúng tập: %v", err)
	}
	if p, _ := evolve.GetProposal(ctx, e.Store.DB, ep); p.Status != evolve.StatusRolledBack {
		t.Fatalf("trạng thái: %s", p.Status)
	}
	// Force kèm tập đã xem trong phiên agent vẫn bị khoá (chỉ dành cho người).
	asAgent(t)
	if _, err := Rollback(ctx, e, RollbackOptions{Ref: ep, Force: true, ExpectConflicts: map[string]string{}}); err == nil || !strings.Contains(err.Error(), "chỉ dành cho người") {
		t.Fatalf("agent: %v", err)
	}
}

func TestOpenNoIngestLeavesSpool(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	session(t, e.K.Paths.Spool, "n1", time.Now().Add(-time.Hour), "", nil)
	root := e.WS.Repo.Toplevel
	count := func() int {
		var n int
		if err := e.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	n0 := count()
	ro, err := OpenNoIngest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	ro.Close()
	if count() != n0 {
		t.Fatal("OpenNoIngest không được nạp spool")
	}
	rw, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	rw.Close()
	if count() <= n0 {
		t.Fatal("Open phải nạp spool")
	}
}

func TestExperienceValidateAndShow(t *testing.T) {
	e, _ := fixture(t)
	ctx := context.Background()
	if err := (ExperienceDigestOptions{}).Validate(); !app.IsUsage(err) {
		t.Fatalf("thiếu ref: %v", err)
	}
	if _, err := Digest(ctx, e, ExperienceDigestOptions{}); !app.IsUsage(err) {
		t.Fatalf("digest thiếu ref: %v", err)
	}
	rows, err := Experience(ctx, e, ExperienceShowOptions{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("thư viện rỗng: %v %v", err, rows)
	}
}
