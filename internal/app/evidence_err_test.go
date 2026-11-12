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

	"bint/internal/knowledge"
	"bint/internal/testutil"
)

const evLine = "\t\t\tif cfg.Security.AllowExternalAgents && !cfg.Security.AllowNetworkInEvals {"

func evFiller(n int, tag string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "// %s dòng đệm số %d\n", tag, i)
	}
	return sb.String()
}

// evFixture là repository có mục tri thức mà bằng chứng là một dòng ở internal/cli/eval.go:11.
type evFixture struct {
	root string
	k    *KnowledgeWS
	id   string
}

func newEvFixture(t *testing.T, typ string) evFixture {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{
		"internal/cli/eval.go": evFiller(10, "cli") + evLine + "\n" + evFiller(5, "cli2"),
		"internal/app/doc.go":  "package app\n",
	})
	mustInit(t, InitOptions{Dir: root})
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := AddKnowledge(ctx, k, AddOptions{Type: typ, Title: "Cờ security không thay thế cô lập",
		Body: "Nội dung riêng của mục kiểm thử bằng chứng lệch.", Paths: []string{"internal/**"},
		Evidence: []string{"internal/cli/eval.go:11"}, Origin: knowledge.OriginAgent, ThirdParty: true})
	if err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	return evFixture{root, k, res.Item.ID}
}

func (f evFixture) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(f.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f evFixture) item(t *testing.T) *knowledge.Item {
	t.Helper()
	all, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	it, err := knowledge.Find(all, f.id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

// moveLine tái hiện ca thật: eval.go ngắn đi, dòng chuyển sang app/eval_run.go (cùng thụt lề).
func (f evFixture) moveLine(t *testing.T) {
	f.write(t, "internal/cli/eval.go", evFiller(4, "cli"))
	f.write(t, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+evLine+"\n}\n")
}

func approveErr(t *testing.T, f evFixture, refresh bool) (*EvidenceError, ReviewOutcome) {
	t.Helper()
	outs, err := ApproveKnowledge(context.Background(), f.k, ApproveOptions{Refs: []string{f.id}, Refresh: refresh})
	if err != nil || len(outs) != 1 {
		t.Fatalf("%v %+v", err, outs)
	}
	var ee *EvidenceError
	if !errors.As(outs[0].Err, &ee) {
		t.Fatalf("muốn *EvidenceError, được %T %v", outs[0].Err, outs[0].Err)
	}
	return ee, outs[0]
}

func TestVerifyApplyMovedRealCase(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeWarning)
	f.moveLine(t)
	ctx := context.Background()
	fp := f.item(t).Evidence[0].Fingerprint

	// Không --apply: chỉ báo, không ghi.
	rep, err := VerifyKnowledge(ctx, f.k, nil, false, time.Now())
	if err != nil || len(rep.Relocated) != 0 || f.item(t).Evidence[0].Path != "internal/cli/eval.go" {
		t.Fatalf("%v %+v", err, rep)
	}
	// Chưa áp: approve chặn bằng lỗi có cấu trúc, cách sửa là apply.
	ee, _ := approveErr(t, f, false)
	if ee.ID != f.id || ee.Fix() != knowledge.FixApply || len(ee.Issues) != 1 || ee.Issues[0].Kind != knowledge.IssueMoved ||
		ee.Issues[0].NewPath != "internal/app/eval_run.go" || ee.Issues[0].NewLines != "4" || ee.Issues[0].Path != "internal/cli/eval.go" {
		t.Fatalf("%+v", ee)
	}
	if strings.Contains(ee.Error(), "bint ") || strings.Contains(ee.Error(), "`") {
		t.Fatalf("Error() có lệnh: %q", ee.Error())
	}
	if ee.File != ".bint/knowledge/items/"+f.id+".md" {
		t.Fatalf("File = %q", ee.File)
	}

	rep, err = VerifyKnowledge(ctx, f.k, nil, true, time.Now())
	if err != nil || len(rep.Relocated) != 1 || len(rep.Moved) != 1 || rep.Moved[0] != f.id {
		t.Fatalf("%v %+v", err, rep)
	}
	it := f.item(t)
	if e := it.Evidence[0]; e.Path != "internal/app/eval_run.go" || e.Lines != "4" || e.Fingerprint != fp {
		t.Fatalf("sau apply: %+v", e)
	}
	if it.Status != knowledge.StatusCandidate {
		t.Fatalf("warning cần người duyệt, không tự verified: %s", it.Status)
	}
	// Approve không --refresh thành công.
	outs, err := ApproveKnowledge(ctx, f.k, ApproveOptions{Refs: []string{f.id}})
	if err != nil || len(outs) != 1 || outs[0].Err != nil || f.item(t).Status != knowledge.StatusVerified {
		t.Fatalf("%v %+v", err, outs)
	}
}

func TestVerifyApplyMovedKeepsVerified(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeFact)
	ctx := context.Background()
	if outs, err := ApproveKnowledge(ctx, f.k, ApproveOptions{Refs: []string{f.id}}); err != nil || outs[0].Err != nil {
		t.Fatalf("%v %+v", err, outs)
	}
	f.moveLine(t)
	rep, err := VerifyKnowledge(ctx, f.k, nil, true, time.Now())
	if err != nil || len(rep.Moved) != 1 || len(rep.Transitions) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	if it := f.item(t); it.Status != knowledge.StatusVerified || it.Evidence[0].Path != "internal/app/eval_run.go" {
		t.Fatalf("%s %+v", it.Status, it.Evidence[0])
	}
}

func TestVerifyApplyAmbiguousNotApplied(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeWarning)
	f.write(t, "internal/cli/eval.go", evFiller(4, "cli"))
	f.write(t, "internal/app/a.go", "package app\n"+evLine+"\n")
	f.write(t, "internal/app/b.go", "package app\n"+evLine+"\n")
	rep, err := VerifyKnowledge(context.Background(), f.k, nil, true, time.Now())
	if err != nil || len(rep.Relocated) != 0 || len(rep.Moved) != 0 {
		t.Fatalf("%v %+v", err, rep)
	}
	if e := f.item(t).Evidence[0]; e.Path != "internal/cli/eval.go" || e.Lines != "11" {
		t.Fatalf("ambiguous bị áp: %+v", e)
	}
	ee, _ := approveErr(t, f, false)
	if ee.Fix() != knowledge.FixEdit || ee.Issues[0].Kind != knowledge.IssueAmbiguous || len(ee.Issues[0].Candidates) != 2 {
		t.Fatalf("%+v", ee)
	}
}

func TestApproveEvidenceErrorFixes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, f evFixture)
		kind   string
		fix    string
		fileLn int
	}{
		{"moved", func(t *testing.T, f evFixture) { f.moveLine(t) }, knowledge.IssueMoved, knowledge.FixApply, 0},
		{"tệp ngắn đi", func(t *testing.T, f evFixture) { f.write(t, "internal/cli/eval.go", evFiller(4, "cli")) }, knowledge.IssueOutOfRange, knowledge.FixEdit, 4},
		{"tệp bị xoá", func(t *testing.T, f evFixture) { os.Remove(filepath.Join(f.root, "internal/cli/eval.go")) }, knowledge.IssueMissingFile, knowledge.FixEdit, 0},
		{"nội dung đổi trong vùng", func(t *testing.T, f evFixture) {
			f.write(t, "internal/cli/eval.go", evFiller(10, "cli")+"\t\t\tif cfg.Khac() {\n"+evFiller(5, "cli2"))
		}, knowledge.IssueChanged, knowledge.FixRefresh, 0},
	}
	for _, c := range cases {
		for _, refresh := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refresh=%v", c.name, refresh), func(t *testing.T) {
				f := newEvFixture(t, knowledge.TypeWarning)
				c.mutate(t, f)
				if refresh && c.fix == knowledge.FixRefresh {
					// --refresh sửa được ca này: không lỗi.
					outs, err := ApproveKnowledge(context.Background(), f.k, ApproveOptions{Refs: []string{f.id}, Refresh: true})
					if err != nil || outs[0].Err != nil {
						t.Fatalf("%v %+v", err, outs)
					}
					return
				}
				if refresh && c.fix == knowledge.FixApply {
					// --refresh áp vị trí mới rồi mới làm mới fingerprint.
					outs, err := ApproveKnowledge(context.Background(), f.k, ApproveOptions{Refs: []string{f.id}, Refresh: true})
					if err != nil || outs[0].Err != nil {
						t.Fatalf("%v %+v", err, outs)
					}
					if e := f.item(t).Evidence[0]; e.Path != "internal/app/eval_run.go" || e.Lines != "4" {
						t.Fatalf("%+v", e)
					}
					return
				}
				ee, o := approveErr(t, f, refresh)
				if ee.Fix() != c.fix || len(ee.Issues) != 1 || ee.Issues[0].Kind != c.kind || ee.Issues[0].FileLines != c.fileLn || ee.ID != f.id {
					t.Fatalf("%+v", ee.Issues)
				}
				if strings.Contains(ee.Error(), "có 4 dòng, bằng chứng tham chiếu") || strings.Contains(ee.Error(), "bint ") {
					t.Fatalf("lỗi thô hoặc có lệnh: %q", ee.Error())
				}
				if o.Check == nil {
					t.Fatal("thiếu Check")
				}
				if got := f.item(t); got.Status != knowledge.StatusCandidate {
					t.Fatalf("mục không được đổi: %s", got.Status)
				}
			})
		}
	}
}

func TestRefreshHashesRelocatedRegion(t *testing.T) {
	// Vùng chỉ dời trong cùng tệp (đẩy xuống 2 dòng): --refresh phải băm nội dung ở vị trí mới,
	// không phải nội dung ở số dòng cũ.
	f := newEvFixture(t, knowledge.TypeWarning)
	f.write(t, "internal/cli/eval.go", "// thêm 1\n// thêm 2\n"+evFiller(10, "cli")+evLine+"\n"+evFiller(5, "cli2"))
	fp := f.item(t).Evidence[0].Fingerprint
	outs, err := ApproveKnowledge(context.Background(), f.k, ApproveOptions{Refs: []string{f.id}, Refresh: true})
	if err != nil || outs[0].Err != nil {
		t.Fatalf("%v %+v", err, outs)
	}
	if e := f.item(t).Evidence[0]; e.Lines != "13" || e.Fingerprint != fp {
		t.Fatalf("%+v (fp cũ %s)", e, fp)
	}
}

// Sau --apply, Results là kết quả kiểm lại sau khi áp: khối đã chuyển không còn là lỗi
// (phát hiện .1).
func TestVerifyApplyMovedResultsAfterApply(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeWarning)
	f.moveLine(t)
	rep, err := VerifyKnowledge(context.Background(), f.k, nil, true, time.Now())
	if err != nil || len(rep.Moved) != 1 || len(rep.Results) != 1 {
		t.Fatalf("%v %+v", err, rep)
	}
	r := rep.Results[0]
	if !r.OK() || r.Has(knowledge.CodeMoved) || r.Has(knowledge.CodeRelocated) || r.Has(knowledge.CodeFingerprint) || len(r.Relocated) != 0 {
		t.Fatalf("Results phải là kết quả sau khi áp: %+v", r)
	}
	// Không --apply thì vẫn báo lỗi moved.
	f2 := newEvFixture(t, knowledge.TypeWarning)
	f2.moveLine(t)
	rep, err = VerifyKnowledge(context.Background(), f2.k, nil, false, time.Now())
	if err != nil || len(rep.Results) != 1 || !rep.Results[0].Has(knowledge.CodeMoved) || rep.Results[0].OK() {
		t.Fatalf("%v %+v", err, rep)
	}
}

// Nội dung đổi tại chỗ mà khối cũ còn ở hai tệp khác: vẫn là changed, --refresh thành công
// (phát hiện .2).
func TestRefreshInPlaceChangeWithCopies(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeWarning)
	f.write(t, "internal/cli/eval.go", evFiller(10, "cli")+"\t\t\tif cfg.Khac() {\n"+evFiller(5, "cli2"))
	f.write(t, "internal/app/a.go", "package app\n"+evLine+"\n")
	f.write(t, "internal/app/b.go", "package app\n"+evLine+"\n")
	// Chưa --refresh: lỗi changed/refresh, không phải ambiguous.
	ee, _ := approveErr(t, f, false)
	if ee.Fix() != knowledge.FixRefresh || ee.Issues[0].Kind != knowledge.IssueChanged {
		t.Fatalf("%+v", ee.Issues)
	}
	outs, err := ApproveKnowledge(context.Background(), f.k, ApproveOptions{Refs: []string{f.id}, Refresh: true})
	if err != nil || len(outs) != 1 || outs[0].Err != nil {
		t.Fatalf("%v %+v", err, outs)
	}
	it := f.item(t)
	if it.Status != knowledge.StatusVerified || it.Evidence[0].Path != "internal/cli/eval.go" {
		t.Fatalf("%s %+v", it.Status, it.Evidence[0])
	}
}

// Mục verified có bằng chứng đổi tại chỗ, khối cũ còn ở đúng một tệp khác cùng đuôi: --apply
// chuyển mục sang stale, path giữ nguyên, không có finding moved (phát hiện .4).
func TestVerifyApplyInPlaceChangeGoesStale(t *testing.T) {
	f := newEvFixture(t, knowledge.TypeFact)
	ctx := context.Background()
	if outs, err := ApproveKnowledge(ctx, f.k, ApproveOptions{Refs: []string{f.id}}); err != nil || outs[0].Err != nil {
		t.Fatalf("%v %+v", err, outs)
	}
	f.write(t, "internal/cli/eval.go", evFiller(10, "cli")+"\t\t\tif cfg.Khac() {\n"+evFiller(5, "cli2"))
	f.write(t, "internal/app/copy.go", "package app\n"+evLine+"\n")
	rep, err := VerifyKnowledge(ctx, f.k, nil, true, time.Now())
	if err != nil || len(rep.Results) != 1 || len(rep.Moved) != 0 || len(rep.Relocated) != 0 || len(rep.Transitions) != 1 {
		t.Fatalf("%v %+v", err, rep)
	}
	if rep.Results[0].Has(knowledge.CodeMoved) {
		t.Fatalf("không được có moved: %+v", rep.Results[0].Findings)
	}
	it := f.item(t)
	if it.Status != knowledge.StatusStale || it.Evidence[0].Path != "internal/cli/eval.go" || it.Evidence[0].Lines != "11" {
		t.Fatalf("%s %+v", it.Status, it.Evidence[0])
	}
}
