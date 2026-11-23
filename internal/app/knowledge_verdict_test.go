package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/knowledge"
)

func writeSrc(t *testing.T, root, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Một mục verified có bằng chứng bị sửa tại chỗ (fingerprint lệch) vào danh sách rà soát; mục chỉ bị dời dòng thì
// được tự áp và không vào. Agent lưu được nhận định nhưng không áp được; người áp theo từng loại nhận định.
func TestAuditVerdictApplyFlow(t *testing.T) {
	root, good, _ := seedKnowledge(t, 4, "nguon:rasoat")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	if _, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: good}); err != nil {
		t.Fatal(err)
	}
	// good[0]: nội dung đổi (valid), good[1]: đổi (outdated), good[2]: đổi + bằng chứng dời (fix-evidence), good[3]: chỉ dời dòng.
	writeSrc(t, root, "src/f00.go", "package src\n\nfunc F00() { _ = 1 }\n")
	writeSrc(t, root, "src/f01.go", "package src\n\nfunc F01() { _ = 1 }\n")
	writeSrc(t, root, "src/f02.go", "package src\n\nfunc F02() { _ = 1 }\n")
	writeSrc(t, root, "src/f03.go", "// thêm dòng\n\npackage src\n\nfunc F03() {}\n")

	t.Setenv("CLAUDECODE", "1") // agent
	rep, err := AuditKnowledge(ctx, k, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]AuditItem{}
	for _, it := range rep.Items {
		listed[it.ID] = it
	}
	for _, id := range good[:3] {
		it, ok := listed[id]
		if !ok || len(it.Reasons) == 0 || !strings.Contains(it.Block, "mã HIỆN TẠI") {
			t.Fatalf("%s phải có trong audit kèm lý do và trích đoạn: %+v", id, it)
		}
	}
	if _, ok := listed[good[3]]; ok {
		t.Fatalf("mục chỉ dời dòng không được vào danh sách")
	}
	if len(rep.Relocated) != 1 || rep.Relocated[0] != good[3] {
		t.Fatalf("Relocated = %v", rep.Relocated)
	}

	// Agent lưu nhận định; không đổi trạng thái.
	save := func(id, verdict string, o VerdictOptions) {
		o.Ref, o.Verdict, o.Reason, o.Model = id, verdict, "xem src/f.go:3", "mo-hinh-thu"
		if _, err := RecordVerdict(ctx, k, o); err != nil {
			t.Fatal(err)
		}
	}
	save(good[0], VerdictValid, VerdictOptions{})
	save(good[1], VerdictOutdated, VerdictOptions{})
	writeSrc(t, root, "src/f02b.go", "package src\n\nfunc F02() { _ = 1 }\n")
	save(good[2], VerdictFixEvidence, VerdictOptions{Path: "src/f02b.go", Lines: "3"})
	if _, err := RecordVerdict(ctx, k, VerdictOptions{Ref: good[2], Verdict: VerdictFixEvidence, Reason: "x"}); err == nil {
		t.Fatal("fix-evidence thiếu bằng chứng đề xuất phải bị từ chối")
	}
	latest, err := LatestVerdicts(ctx, k)
	if err != nil {
		t.Fatal(err)
	}
	if sv := latest[good[0]]; sv.Verdict != VerdictValid || sv.Harness != "claude" || sv.Model != "mo-hinh-thu" || sv.Stale {
		t.Fatalf("nhận định lưu sai: %+v", sv)
	}
	if latest[good[2]].Evidence == nil || latest[good[2]].Evidence.Path != "src/f02b.go" {
		t.Fatalf("thiếu bằng chứng đề xuất: %+v", latest[good[2]])
	}
	items, _, _ := k.Items()
	for _, it := range items {
		if it.ID == good[0] && it.Status != knowledge.StatusVerified {
			t.Fatalf("verdict không được đổi trạng thái: %s", it.Status)
		}
	}
	// Agent không áp được.
	if _, err := ApplyVerdicts(ctx, k, VerdictApplyOptions{}); err == nil || !strings.Contains(err.Error(), ErrAgentForbidden.Error()) {
		t.Fatalf("agent áp nhận định phải bị chặn: %v", err)
	}

	// Người áp theo lô.
	t.Setenv("CLAUDECODE", "")
	asHuman(t)
	outs, err := ApplyVerdicts(ctx, k, VerdictApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	applied := map[string]VerdictApplied{}
	for _, o := range outs {
		applied[o.ID] = o
	}
	for _, id := range good[:3] {
		if !applied[id].Applied {
			t.Fatalf("%s chưa áp: %+v", id, applied[id])
		}
	}
	items, _, _ = k.Items()
	status := map[string]*knowledge.Item{}
	for _, it := range items {
		status[it.ID] = it
	}
	if status[good[0]].Status != knowledge.StatusVerified || status[good[1]].Status != knowledge.StatusDeprecated {
		t.Fatalf("trạng thái sau áp: valid=%s outdated=%s", status[good[0]].Status, status[good[1]].Status)
	}
	if status[good[2]].Evidence[0].Path != "src/f02b.go" || status[good[2]].Status != knowledge.StatusVerified {
		t.Fatalf("fix-evidence chưa sửa bằng chứng: %+v %s", status[good[2]].Evidence, status[good[2]].Status)
	}
	// valid đã làm mới fingerprint: audit không còn thấy mục.
	rep, _ = AuditKnowledge(ctx, k, []string{good[0], good[2]})
	if len(rep.Items) != 0 {
		t.Fatalf("sau áp không còn mục cần rà soát: %+v", rep.Items)
	}
}

func TestParseVerdictsFixEvidence(t *testing.T) {
	out := parseVerdicts(`xong {"verdicts":[{"id":"A","verdict":"fix-evidence","reason":"r","evidence":{"path":"a.go","lines":"1-2"}},` +
		`{"id":"B","verdict":"fix-evidence","reason":"thiếu"},{"id":"C","verdict":"lạ","reason":"r"}]}`)
	if out["A"].Verdict != VerdictFixEvidence || out["A"].Evidence == nil || out["A"].Evidence.Path != "a.go" {
		t.Fatalf("A = %+v", out["A"])
	}
	if out["B"].Verdict != VerdictUnsure || out["C"].Verdict != VerdictUnsure {
		t.Fatalf("B=%+v C=%+v", out["B"], out["C"])
	}
}

// Mục verified có tệp bằng chứng bị xoá (chỉ lỗi CodeEvidence, không có fingerprint) vẫn vào danh sách rà soát,
// giống cách verify --apply coi đó là bằng chứng thay đổi.
func TestAuditListsVerifiedWithDeletedEvidenceFile(t *testing.T) {
	root, good, _ := seedKnowledge(t, 1, "nguon:xoatep")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	if _, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: good}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "src", "f00.go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	rep, err := AuditKnowledge(ctx, k, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range rep.Items {
		if it.ID == good[0] {
			return
		}
	}
	t.Fatalf("mục verified có tệp bằng chứng bị xoá phải vào audit: %+v", rep.Items)
}

// Quét tổng quát: liệt kê cả mục bằng chứng vẫn khớp và candidate, mục có vấn đề đứng đầu kèm tín hiệu problem; nhận định
// valid trên candidate được áp thành duyệt.
func TestAuditAllListsEveryActiveItem(t *testing.T) {
	root, good, _ := seedKnowledge(t, 3, "nguon:tongquat")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	if _, err := ApproveKnowledge(ctx, k, ApproveOptions{Refs: good[:2]}); err != nil {
		t.Fatal(err)
	}
	writeSrc(t, root, "src/f01.go", "package src\n\nfunc F01() { _ = 1 }\n") // good[1]: bằng chứng đổi
	rep, err := AuditKnowledgeWith(ctx, k, AuditOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.All || len(rep.Items) < 3 {
		t.Fatalf("--all phải liệt kê mọi mục còn hiệu lực: %+v", rep.Items)
	}
	if rep.Items[0].ID != good[1] || !slices.Contains(rep.Items[0].Signals, SignalProblem) {
		t.Fatalf("mục có vấn đề phải đứng đầu kèm problem: %+v", rep.Items[0])
	}
	var cand *AuditItem
	for i := range rep.Items {
		if rep.Items[i].ID == good[2] {
			cand = &rep.Items[i]
		}
	}
	if cand == nil || cand.Status != "candidate" {
		t.Fatalf("candidate phải có trong quét tổng quát: %+v", cand)
	}
	if _, err := RecordVerdict(ctx, k, VerdictOptions{Ref: good[2], Verdict: VerdictValid, Reason: "đúng, src/f02.go:3", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	asHuman(t)
	if _, err := KnowledgeVerdicts(ctx, k, VerdictsOptions{Refs: []string{good[2]}, Apply: true}); err != nil {
		t.Fatal(err)
	}
	items, _, _ := k.Items()
	for _, it := range items {
		if it.ID == good[2] && it.Status != "verified" {
			t.Fatalf("valid trên candidate phải duyệt: %s", it.Status)
		}
	}
}

// GĐ4b (TSK-4b.4): quy tắc chưa có cơ chế máy cưỡng chế mang tín hiệu unenforced trong audit --all; enforced_by trỏ tới tệp có thật
// thì không; cú pháp sai bị từ chối lúc thêm.
func TestAuditAllFlagsUnenforcedRules(t *testing.T) {
	root, _, _ := seedKnowledge(t, 1, "nguon:cuongche")
	ctx := context.Background()
	k, _ := OpenKnowledge(ctx, root)
	add := func(title string, enforced []string) (AddResult, error) {
		return AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeRule, Title: title, Body: title + ": nội dung quy tắc riêng.",
			Paths: []string{"src/**"}, Evidence: []string{"src/f00.go:3"}, EnforcedBy: enforced})
	}
	bare, err := add("Quy tắc chưa có check", nil)
	if err != nil || !bare.Saved {
		t.Fatalf("%v %+v", err, bare.Check.Findings)
	}
	held, err := add("Quy tắc có test cưỡng chế", []string{"test:src/f00.go#TestF00"})
	if err != nil || !held.Saved || len(held.Item.EnforcedBy) != 1 || held.Item.EnforcedBy[0].Note != "TestF00" {
		t.Fatalf("%v %+v", err, held.Item)
	}
	if _, err := add("Quy tắc sai cú pháp", []string{"src/f00.go"}); err == nil {
		t.Fatal("enforced-by thiếu loại phải bị từ chối")
	}
	rep, err := AuditKnowledgeWith(ctx, k, AuditOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	sig := map[string][]string{}
	for _, it := range rep.Items {
		sig[it.ID] = it.Signals
	}
	if !slices.Contains(sig[bare.Item.ID], SignalUnenforced) {
		t.Fatalf("quy tắc không có enforced_by phải mang unenforced: %v", sig[bare.Item.ID])
	}
	if slices.Contains(sig[held.Item.ID], SignalUnenforced) {
		t.Fatalf("quy tắc có enforced_by còn tệp không được mang unenforced: %v", sig[held.Item.ID])
	}
	// Đọc lại từ đĩa: enforced_by được ghi và nạp lại.
	items, _, _ := k.Items()
	for _, it := range items {
		if it.ID == held.Item.ID && (len(it.EnforcedBy) != 1 || it.EnforcedBy[0].Path != "src/f00.go") {
			t.Fatalf("enforced_by không được lưu: %+v", it.EnforcedBy)
		}
	}
}
