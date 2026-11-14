package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

const realLine = "\t\t\tif cfg.Security.AllowExternalAgents && !cfg.Security.AllowNetworkInEvals {"

func movedItem(commit string, evs ...Evidence) *Item {
	return &Item{Schema: 1, ID: "01JABCDEFGHJKMNPQRSTVWXYZ0", Type: TypeWarning, Status: StatusCandidate, Title: "t", Body: "b",
		Origin: Origin{Kind: OriginAgent, Commit: commit}, Version: 1, CreatedAt: "2026-01-01T00:00:00Z", Evidence: evs}
}

func fingerprinted(t *testing.T, e Evidence, content string) Evidence {
	t.Helper()
	fp, err := FingerprintWith(e, []byte(content), "hunk")
	if err != nil {
		t.Fatal(err)
	}
	e.Fingerprint = fp
	return e
}

func filler(n int, tag string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "// %s dòng đệm số %d\n", tag, i)
	}
	return sb.String()
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// realCase dựng ca thật: khối 1 dòng ở cli/eval.go:11, sau đó eval.go ngắn đi và dòng chuyển
// sang app/eval_run.go (cùng thụt lề). Trả về repo, commit gốc và bằng chứng đã có fingerprint.
func realCase(t *testing.T) (root, commit string, ev Evidence) {
	t.Helper()
	old := filler(10, "cli") + realLine + "\n" + filler(5, "cli2")
	root = testutil.NewRepo(t, map[string]string{"internal/cli/eval.go": old, "internal/app/other.go": "package app\n"})
	commit = testutil.Git(t, root, "rev-parse", "HEAD")
	ev = fingerprinted(t, Evidence{Path: "internal/cli/eval.go", Lines: "11"}, old)
	write(t, root, "internal/cli/eval.go", filler(4, "cli"))
	write(t, root, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+realLine+"\n}\n")
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", "move")
	return root, commit, ev
}

func findingByCode(r Result, code string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Code == code {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestMovedOneLineWithCommit(t *testing.T) {
	root, commit, ev := realCase(t)
	for _, c := range []struct {
		name         string
		evCommit, oc string
	}{{"origin.commit", "", commit}, {"evidence.commit", commit, ""}} {
		t.Run(c.name, func(t *testing.T) {
			e := ev
			e.Commit = c.evCommit
			it := movedItem(c.oc, e)
			r := Verifier{Src: &WorktreeSource{Root: root}, FindMoved: true}.Verify(it)
			f := findingByCode(r, CodeMoved)
			if f == nil || f.Level != LevelError || f.Evidence == nil {
				t.Fatalf("muốn finding moved: %+v", r.Findings)
			}
			iss := f.Evidence
			if iss.Kind != IssueMoved || iss.Path != "internal/cli/eval.go" || iss.Lines != "11" ||
				iss.NewPath != "internal/app/eval_run.go" || iss.NewLines != "4" || iss.Fix() != FixApply {
				t.Fatalf("issue: %+v", iss)
			}
			if strings.Contains(f.Detail, "bint ") {
				t.Fatalf("Detail có lệnh: %q", f.Detail)
			}
			if len(r.Relocated) != 1 || r.Relocated[0].ToPath != "internal/app/eval_run.go" || r.Relocated[0].FromPath != "internal/cli/eval.go" {
				t.Fatalf("relocated: %+v", r.Relocated)
			}
			// Áp rồi kiểm lại: sạch, fingerprint giữ nguyên.
			if !r.ApplyRelocations(it) {
				t.Fatal("không áp được")
			}
			if got := it.Evidence[0]; got.Path != "internal/app/eval_run.go" || got.Lines != "4" || got.Fingerprint != ev.Fingerprint {
				t.Fatalf("sau áp: %+v", got)
			}
			if r2 := (Verifier{Src: &WorktreeSource{Root: root}, FindMoved: true}).Verify(it); !r2.OK() || len(r2.Findings) != 0 && findingByCode(r2, CodeMoved) != nil {
				t.Fatalf("sau áp vẫn lỗi: %+v", r2.Findings)
			}
		})
	}
}

func TestMovedOffByDefault(t *testing.T) {
	root, commit, ev := realCase(t)
	r := Verifier{Src: &WorktreeSource{Root: root}}.Verify(movedItem(commit, ev))
	if r.Has(CodeMoved) || len(r.Relocated) != 0 {
		t.Fatalf("FindMoved tắt mà vẫn tìm: %+v", r)
	}
	f := findingByCode(r, CodeFingerprint)
	if f == nil || f.Evidence == nil || f.Evidence.Kind != IssueOutOfRange || f.Evidence.FileLines != 4 || f.Evidence.Fix() != FixEdit {
		t.Fatalf("muốn out_of_range kèm 4 dòng: %+v", r.Findings)
	}
}

func TestMovedOneLineWithoutCommit(t *testing.T) {
	// Không có commit (nguồn trong bộ nhớ): lùi về tệp cùng đuôi; tệp .md trích cùng dòng không tính.
	old := filler(10, "cli") + realLine + "\n"
	ev := fingerprinted(t, Evidence{Path: "internal/cli/eval.go", Lines: "11"}, old)
	src := &memSource{files: map[string]string{
		"internal/cli/eval.go":     filler(4, "cli"),
		"internal/app/eval_run.go": "package app\n" + realLine + "\n",
		"docs/note.md":             "trích: " + realLine + "\n" + realLine + "\n",
	}}
	r := Verifier{Src: src, FindMoved: true}.Verify(movedItem("", ev))
	f := findingByCode(r, CodeMoved)
	if f == nil || f.Evidence.NewPath != "internal/app/eval_run.go" || f.Evidence.NewLines != "2" {
		t.Fatalf("%+v", r.Findings)
	}
	// Worktree thật nhưng commit không tồn tại: git lỗi → lùi về cùng đuôi.
	root, _, ev2 := realCase(t)
	r = Verifier{Src: &WorktreeSource{Root: root}, FindMoved: true}.Verify(movedItem("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", ev2))
	if f := findingByCode(r, CodeMoved); f == nil || f.Evidence.NewPath != "internal/app/eval_run.go" {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestMovedFileDeleted(t *testing.T) {
	body := filler(12, "x") + realLine + "\n"
	ev := fingerprinted(t, Evidence{Path: "a/old.go", Lines: "13"}, body)
	src := &memSource{files: map[string]string{"b/new.go": "package b\n" + body}}
	r := Verifier{Src: src, FindMoved: true}.Verify(movedItem("", ev))
	f := findingByCode(r, CodeMoved)
	if f == nil || f.Evidence.NewPath != "b/new.go" || f.Evidence.NewLines != "14" || f.Evidence.Path != "a/old.go" {
		t.Fatalf("%+v", r.Findings)
	}
	// Không ở đâu cả: giữ lỗi tệp mất.
	src = &memSource{files: map[string]string{"b/new.go": "package b\n"}}
	r = Verifier{Src: src, FindMoved: true}.Verify(movedItem("", ev))
	f = findingByCode(r, CodeEvidence)
	if f == nil || f.Evidence.Kind != IssueMissingFile || f.Evidence.Fix() != FixEdit || r.Has(CodeMoved) {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestMovedRename(t *testing.T) {
	body := "package x\n\nfunc Target() int {\n\treturn computeSomethingLong(1, 2, 3)\n}\n\n" + filler(8, "pad")
	root := testutil.NewRepo(t, map[string]string{"pkg/old.go": body, "pkg/keep.go": "package x\n"})
	commit := testutil.Git(t, root, "rev-parse", "HEAD")
	for _, e := range []Evidence{
		{Path: "pkg/old.go", Lines: "3-5"},
		{Path: "pkg/old.go", Symbol: "Target"},
		{Path: "pkg/old.go"},
	} {
		ev := fingerprinted(t, e, body)
		testutil.Git(t, root, "mv", "pkg/old.go", "pkg/renamed.go")
		testutil.Git(t, root, "commit", "-q", "-m", "rename")
		r := Verifier{Src: &WorktreeSource{Root: root}, FindMoved: true}.Verify(movedItem(commit, ev))
		f := findingByCode(r, CodeMoved)
		if f == nil || f.Evidence.NewPath != "pkg/renamed.go" {
			t.Fatalf("%+v: %+v", e, r.Findings)
		}
		testutil.Git(t, root, "mv", "pkg/renamed.go", "pkg/old.go")
		testutil.Git(t, root, "commit", "-q", "-m", "back")
	}
}

func TestMovedSymbol(t *testing.T) {
	body := "package x\n\nfunc Target() int {\n\treturn computeSomethingLong(1, 2, 3)\n}\n"
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "3-5", Symbol: "Target"}, body)
	src := &memSource{files: map[string]string{
		"a.go": "package x\n",
		"b.go": "package x\n\n// mới\nfunc Other() {}\n\n" + strings.TrimPrefix(body, "package x\n\n"),
	}}
	r := Verifier{Src: src, FindMoved: true}.Verify(movedItem("", ev))
	f := findingByCode(r, CodeMoved)
	if f == nil || f.Evidence.NewPath != "b.go" || f.Evidence.NewLines != "6-8" {
		t.Fatalf("%+v", r.Findings)
	}
	if r.Has(CodeSymbol) {
		t.Fatalf("ký hiệu phải được kiểm ở vị trí mới: %+v", r.Findings)
	}
}

func TestMovedAmbiguous(t *testing.T) {
	old := filler(3, "a") + realLine + "\n"
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "4"}, old)
	src := &memSource{files: map[string]string{
		"a.go": "package a\n",
		"b.go": "package b\n" + realLine + "\n",
		"c.go": realLine + "\n",
	}}
	it := movedItem("", ev)
	r := Verifier{Src: src, FindMoved: true}.Verify(it)
	f := findingByCode(r, CodeFingerprint)
	if f == nil || f.Evidence.Kind != IssueAmbiguous || len(f.Evidence.Candidates) != 2 ||
		f.Evidence.Candidates[0] != "b.go:2" || f.Evidence.Candidates[1] != "c.go:1" || f.Evidence.Fix() != FixEdit {
		t.Fatalf("%+v", r.Findings)
	}
	if r.Has(CodeMoved) || len(r.Relocated) != 0 || r.ApplyRelocations(it) {
		t.Fatalf("ambiguous không được áp: %+v", r)
	}
}

func TestMovedWeakBlockNotSearched(t *testing.T) {
	// "}" khớp đúng một nơi nhưng quá yếu để kết luận: ambiguous kèm ứng viên, không phải moved.
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "3"}, "x\ny\n}\n")
	src := &memSource{files: map[string]string{"a.go": "x\n", "b.go": "q\n}\n"}}
	r := Verifier{Src: src, FindMoved: true}.Verify(movedItem("", ev))
	f := findingByCode(r, CodeFingerprint)
	if r.Has(CodeMoved) || f == nil || f.Evidence.Kind != IssueAmbiguous || len(f.Evidence.Candidates) != 1 || len(r.Relocated) != 0 {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestMovedBudgetTruncated(t *testing.T) {
	old := filler(3, "a") + realLine + "\n"
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "4"}, old)
	files := map[string]string{"a.go": "package a\n"}
	for i := 0; i < maxMoveFiles+50; i++ {
		files[fmt.Sprintf("d/f%04d.go", i)] = "package d\n"
	}
	files["d/f9999.go"] = realLine + "\n" // ngoài giới hạn số tệp
	r := Verifier{Src: &memSource{files: files}, FindMoved: true}.Verify(movedItem("", ev))
	f := findingByCode(r, CodeFingerprint)
	if r.Has(CodeMoved) || f == nil || !f.Evidence.Truncated || f.Evidence.Kind != IssueOutOfRange {
		t.Fatalf("muốn không thấy + Truncated: %+v", r.Findings)
	}
	// Tệp lớn hơn maxMoveFile bị bỏ qua.
	big := strings.Repeat("// "+strings.Repeat("x", 60)+"\n", maxMoveFile/60) + realLine + "\n"
	r = Verifier{Src: &memSource{files: map[string]string{"a.go": "package a\n", "big.go": big}}, FindMoved: true}.Verify(movedItem("", ev))
	if r.Has(CodeMoved) {
		t.Fatalf("tệp quá lớn vẫn được quét: %+v", r.Findings)
	}
}

func TestIssueKinds(t *testing.T) {
	body := "package x\n\nfunc A() int {\n\treturn 1\n}\n\nfunc B() int {\n\treturn 2\n}\n"
	lineEv := fingerprinted(t, Evidence{Path: "x.go", Lines: "8"}, body)
	symEv := Evidence{Path: "x.go", Lines: "7-9", Symbol: "B"}
	symEv.Fingerprint, _ = FingerprintWith(symEv, []byte(body), FPSymbol)
	cases := []struct {
		name    string
		ev      Evidence
		content *string
		kind    string
		code    string
		fix     string
		fileLn  int
	}{
		{name: "missing_file", ev: lineEv, kind: IssueMissingFile, code: CodeEvidence, fix: FixEdit},
		{name: "out_of_range", ev: fingerprinted(t, Evidence{Path: "x.go", Lines: "9"}, body+"\n\n"), content: ptr("package x\n"), kind: IssueOutOfRange, code: CodeFingerprint, fix: FixEdit, fileLn: 1},
		{name: "changed", ev: lineEv, content: ptr(strings.Replace(body, "return 2", "return 3", 1)), kind: IssueChanged, code: CodeFingerprint, fix: FixRefresh},
		{name: "symbol_gone", ev: symEv, content: ptr(strings.Replace(body, "func B()", "func C()", 1)), kind: IssueSymbolGone, code: CodeFingerprint, fix: FixEdit},
		{name: "relocated", ev: lineEv, content: ptr("// mới\n" + body), kind: IssueRelocated, code: CodeRelocated, fix: FixApply},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := &memSource{files: map[string]string{}}
			if c.content != nil {
				src.files["x.go"] = *c.content
			}
			r := Verifier{Src: src}.Verify(movedItem("", c.ev))
			f := findingByCode(r, c.code)
			if f == nil || f.Evidence == nil || f.Evidence.Kind != c.kind || f.Evidence.Fix() != c.fix || f.Evidence.FileLines != c.fileLn {
				t.Fatalf("%+v", r.Findings)
			}
			if strings.Contains(f.Detail, "bint ") || strings.Contains(f.Detail, "`") {
				t.Fatalf("Detail có lệnh: %q", f.Detail)
			}
			if f.Evidence.Describe() == "" || strings.Contains(f.Evidence.Describe(), "bint ") {
				t.Fatalf("Describe: %q", f.Evidence.Describe())
			}
		})
	}
	// Không có fingerprint: cảnh báo no_fingerprint, không cần sửa gì.
	noFP := Evidence{Path: "x.go", Lines: "8"}
	r := Verifier{Src: &memSource{files: map[string]string{"x.go": body}}}.Verify(movedItem("", noFP))
	if f := findingByCode(r, CodeFingerprint); f == nil || f.Evidence.Kind != IssueNoFingerprint || f.Evidence.Fix() != FixNone {
		t.Fatalf("%+v", r.Findings)
	}
}

func ptr(s string) *string { return &s }

func TestFillFingerprintsStructuredError(t *testing.T) {
	src := &memSource{files: map[string]string{"x.go": "a\nb\n"}}
	it := movedItem("", Evidence{Path: "x.go", Lines: "1"}, Evidence{Path: "x.go", Lines: "9"})
	err := FillFingerprints(it, src)
	var ie *EvidenceIssueError
	if !errors.As(err, &ie) || ie.Issue.Kind != IssueOutOfRange || ie.Issue.Index != 1 || ie.Issue.FileLines != 2 {
		t.Fatalf("%v (%+v)", err, ie)
	}
	if !strings.Contains(err.Error(), "x.go có 2 dòng, bằng chứng tham chiếu dòng 9") {
		t.Fatalf("chữ lỗi đổi: %v", err)
	}
	err = FillFingerprints(movedItem("", Evidence{Path: "gone.go"}), src)
	if !errors.As(err, &ie) || ie.Issue.Kind != IssueMissingFile || !strings.Contains(err.Error(), "không có tệp trong mem") {
		t.Fatalf("%v", err)
	}
}

func TestApplyRelocationsPathGuard(t *testing.T) {
	mk := func() *Item {
		return movedItem("", Evidence{Path: "a.go", Lines: "5", Fingerprint: "lines:abc", Symbol: "S", Commit: "c1"})
	}
	r := Result{Relocated: []Relocation{{Index: 0, From: "5", To: "9", FromPath: "a.go", ToPath: "b.go"}}}
	it := mk()
	if !r.ApplyRelocations(it) {
		t.Fatal("phải áp")
	}
	if e := it.Evidence[0]; e.Path != "b.go" || e.Lines != "9" || e.Fingerprint != "lines:abc" || e.Symbol != "S" || e.Commit != "c1" {
		t.Fatalf("%+v", e)
	}
	// Path hoặc lines đã đổi kể từ lúc kiểm: không áp.
	for _, mut := range []func(*Item){
		func(i *Item) { i.Evidence[0].Path = "z.go" },
		func(i *Item) { i.Evidence[0].Lines = "6" },
	} {
		it := mk()
		mut(it)
		before := it.Evidence[0]
		if r.ApplyRelocations(it) || it.Evidence[0] != before {
			t.Fatalf("không được áp: %+v", it.Evidence[0])
		}
	}
	// Chỉ đổi vùng dòng (relocated cùng tệp) vẫn áp, giữ path.
	r2 := Result{Relocated: []Relocation{{Index: 0, From: "5", To: "7", FromPath: "a.go"}}}
	it = mk()
	if !r2.ApplyRelocations(it) || it.Evidence[0].Path != "a.go" || it.Evidence[0].Lines != "7" {
		t.Fatalf("%+v", it.Evidence[0])
	}
}

// Nội dung đổi tại chỗ (tệp còn đủ dài, vùng cũ nay chứa nội dung khác) là changed/refresh, dù
// khối cũ còn ở tệp khác: không tìm chuyển tệp, không có Relocation (phát hiện .2 và .4).
func TestMovedNotForInPlaceChange(t *testing.T) {
	old := filler(3, "a") + realLine + "\n" + filler(2, "a2")
	changed := filler(3, "a") + "\t\t\tif cfg.Khac() {\n" + filler(2, "a2")
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "4"}, old)
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"một bản sao", map[string]string{"a.go": changed, "b.go": "package b\n" + realLine + "\n"}},
		{"hai bản sao", map[string]string{"a.go": changed, "b.go": "package b\n" + realLine + "\n", "c.go": realLine + "\n"}},
		{"khối yếu", map[string]string{"a.go": changed, "b.go": "q\n}\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := ev
			if c.name == "khối yếu" {
				e = fingerprinted(t, Evidence{Path: "a.go", Lines: "4"}, filler(3, "a")+"}\n"+filler(2, "a2"))
			}
			it := movedItem("", e)
			r := Verifier{Src: &memSource{files: c.files}, FindMoved: true}.Verify(it)
			f := findingByCode(r, CodeFingerprint)
			if f == nil || f.Evidence == nil || f.Evidence.Kind != IssueChanged || f.Evidence.Fix() != FixRefresh {
				t.Fatalf("muốn changed/refresh: %+v", r.Findings)
			}
			if r.Has(CodeMoved) || len(r.Relocated) != 0 || r.ApplyRelocations(it) {
				t.Fatalf("đổi tại chỗ không được coi là chuyển tệp: %+v", r)
			}
		})
	}
}

// Tìm bị cắt theo giới hạn: một nơi khớp trong phần đã quét chưa đủ để kết luận "đã chuyển"
// (phát hiện .3). Một bản trong giới hạn, một bản ngoài giới hạn.
func TestMovedTruncatedSingleMatchAmbiguous(t *testing.T) {
	old := filler(3, "a") + realLine + "\n"
	ev := fingerprinted(t, Evidence{Path: "a.go", Lines: "4"}, old)
	files := map[string]string{"a.go": "package a\n"}
	files["in.go"] = "package in\n" + realLine + "\n"
	for i := 0; i < maxMoveFiles+50; i++ {
		files[fmt.Sprintf("z/f%04d.go", i)] = "package d\n"
	}
	files["z/zzzz.go"] = realLine + "\n" // ngoài giới hạn số tệp
	it := movedItem("", ev)
	r := Verifier{Src: &memSource{files: files}, FindMoved: true}.Verify(it)
	f := findingByCode(r, CodeFingerprint)
	if f == nil || f.Evidence == nil {
		t.Fatalf("%+v", r.Findings)
	}
	iss := f.Evidence
	if iss.Kind != IssueAmbiguous || !iss.Truncated || len(iss.Candidates) != 1 || iss.Candidates[0] != "in.go:2" || iss.Fix() != FixEdit {
		t.Fatalf("muốn ambiguous + Truncated + 1 ứng viên: %+v", iss)
	}
	if r.Has(CodeMoved) || len(r.Relocated) != 0 || r.ApplyRelocations(it) {
		t.Fatalf("bị cắt mà vẫn chuyển: %+v", r)
	}
}

// Trường commit bắt đầu bằng "-" không được tới git dưới dạng tuỳ chọn: `--output=<tệp>` sẽ ghi đè tệp trong repo.
func TestChangedSinceRejectsOptionLikeCommit(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n", "victim.txt": "giữ nguyên\n"})
	head := testutil.Git(t, root, "rev-parse", "HEAD")
	const evil = "--output=victim.txt"
	if _, ok := (&WorktreeSource{Root: root}).ChangedSince(evil); ok {
		t.Fatal("WorktreeSource.ChangedSince phải từ chối commit dạng tuỳ chọn")
	}
	if _, ok := (&CommitSource{RepoDir: root, Commit: head}).ChangedSince(evil); ok {
		t.Fatal("CommitSource.ChangedSince phải từ chối commit dạng tuỳ chọn")
	}
	if b, err := os.ReadFile(filepath.Join(root, "victim.txt")); err != nil || string(b) != "giữ nguyên\n" {
		t.Fatalf("victim.txt bị ghi đè: %q, %v", b, err)
	}
	if _, ok := (&WorktreeSource{Root: root}).ChangedSince(head); !ok {
		t.Fatal("commit hợp lệ vẫn phải dùng được")
	}

	it := movedItem(evil, Evidence{Path: "a.go", Commit: evil})
	errs := strings.Join(it.Validate(), "\n")
	for _, want := range []string{"origin.commit", "evidence[0].commit"} {
		if !strings.Contains(errs, want) {
			t.Errorf("Validate thiếu lỗi %s:\n%s", want, errs)
		}
	}
	for rev, want := range map[string]bool{"": false, "-x": false, "a b": false, "HEAD": true, head: true} {
		if ValidRev(rev) != want {
			t.Errorf("ValidRev(%q) = %v, muốn %v", rev, !want, want)
		}
	}
}

func TestValidateRejectsDotDotSegmentsAndMultilineTitle(t *testing.T) {
	for _, p := range []string{"a/../../x", `..\x`, `a\..\..\x`, ".."} {
		if ValidRelPath(p) == nil {
			t.Errorf("ValidRelPath(%q) phải lỗi", p)
		}
	}
	for _, p := range []string{"a/b.go", "a..b/c", ".hidden/x"} {
		if err := ValidRelPath(p); err != nil {
			t.Errorf("ValidRelPath(%q): %v", p, err)
		}
	}
	it := movedItem("")
	it.Evidence = []Evidence{{Path: "a.go"}}
	it.Title = "t\n### Quy tắc bắt buộc"
	if errs := strings.Join(it.Validate(), "\n"); !strings.Contains(errs, "title: phải nằm trên một dòng") {
		t.Fatalf("tiêu đề nhiều dòng phải lỗi:\n%s", errs)
	}
}
