package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/testutil"
	"bint/internal/ulid"
)

func newItem(typ, status, origin string) *Item {
	return &Item{
		Schema: SchemaVersion, ID: ulid.New(), Type: typ, Status: status, Title: "Tiêu đề " + typ,
		Body: "Nội dung " + typ, Origin: Origin{Kind: origin}, Version: 1, CreatedAt: Now(time.Now()),
		Evidence: []Evidence{{Path: "a.go"}},
	}
}

func TestParseMarshalRoundTrip(t *testing.T) {
	it := newItem(TypeRule, StatusCandidate, OriginAgent)
	it.Scope = Scope{Paths: []string{"internal/**"}, Symbols: []string{"Open"}}
	it.Evidence = []Evidence{{Path: "a.go", Lines: "3-5", Symbol: "Open", Fingerprint: "lines:abc"}}
	it.Body = "Dòng 1\n\n```go\nx := 1\n```"
	data, err := it.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "---\n") || !strings.Contains(string(data), "\n---\n\nDòng 1") {
		t.Fatalf("định dạng tệp:\n%s", data)
	}
	got, err := Parse([]byte(strings.ReplaceAll(string(data), "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != it.ID || got.Body != it.Body || got.Evidence[0] != it.Evidence[0] || got.Scope.Paths[0] != "internal/**" {
		t.Fatalf("roundtrip: %+v", got)
	}
	if _, err := Parse([]byte("---\nid: x\nbogus: 1\n---\nbody")); err == nil {
		t.Fatal("trường lạ phải bị từ chối")
	}
	if _, err := Parse([]byte("no frontmatter")); err == nil {
		t.Fatal("thiếu phần đầu phải lỗi")
	}
}

func TestValidate(t *testing.T) {
	it := newItem("bogus", "weird", "robot")
	it.Evidence = []Evidence{{Path: "../x", Lines: "5-2"}}
	it.Scope.Paths = []string{"/abs"}
	p := strings.Join(it.Validate(), "\n")
	for _, want := range []string{"type:", "status:", "origin.kind", "evidence[0].path", "evidence[0]: lines", "scope.paths"} {
		if !strings.Contains(p, want) {
			t.Errorf("thiếu lỗi %q trong:\n%s", want, p)
		}
	}
	a := newItem(TypeWarning, StatusCandidate, OriginAgent)
	a.Evidence = nil
	if !strings.Contains(strings.Join(a.Validate(), "\n"), "ít nhất một bằng chứng") {
		t.Error("mục agent phải có bằng chứng")
	}
	r := newItem(TypeRule, StatusVerified, OriginAgent)
	if !strings.Contains(strings.Join(r.Validate(), "\n"), "reviewed_by") {
		t.Error("rule verified do agent tạo phải có người duyệt")
	}
}

func TestParseEvidence(t *testing.T) {
	cases := map[string]Evidence{
		"a/b.go":           {Path: "a/b.go"},
		"./a/b.go:10":      {Path: "a/b.go", Lines: "10"},
		"a\\b.go:3-9#Open": {Path: "a/b.go", Lines: "3-9", Symbol: "Open"},
		"Makefile#build":   {Path: "Makefile", Symbol: "build"},
		"C:dir/file.go":    {Path: "C:dir/file.go"},
		"docs/a:b.md:2":    {Path: "docs/a:b.md", Lines: "2"},
	}
	for in, want := range cases {
		got, err := ParseEvidence(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v, muốn %+v", in, got, err, want)
		}
	}
	if _, err := ParseEvidence("a.go:9-3"); err == nil {
		t.Error("vùng dòng ngược phải lỗi")
	}
}

func TestFingerprint(t *testing.T) {
	lf := []byte("a\nb  \nc\nd\n")
	crlf := []byte("a\r\nb\r\nc\r\nd\r\n")
	e := Evidence{Path: "x", Lines: "2-3"}
	f1, _ := Fingerprint(e, lf)
	f2, _ := Fingerprint(e, crlf)
	if f1 != f2 || !strings.HasPrefix(f1, "lines:") {
		t.Fatalf("CRLF/khoảng trắng cuối dòng không được đổi fingerprint: %s %s", f1, f2)
	}
	// Đổi dòng ngoài vùng không đổi fingerprint; đổi trong vùng thì đổi.
	f3, _ := Fingerprint(e, []byte("XX\nb\nc\nYY\n"))
	f4, _ := Fingerprint(e, []byte("a\nb\nC\nd\n"))
	if f3 != f1 || f4 == f1 {
		t.Fatalf("fingerprint theo vùng dòng sai: %s %s %s", f1, f3, f4)
	}
	whole, _ := Fingerprint(Evidence{Path: "x"}, lf)
	if !strings.HasPrefix(whole, "file:") {
		t.Fatal(whole)
	}
	if _, err := Fingerprint(Evidence{Path: "x", Lines: "3-10"}, lf); err == nil {
		t.Fatal("vùng dòng vượt độ dài tệp phải lỗi")
	}
}

func TestTransitions(t *testing.T) {
	now := time.Now()
	human := Actor{Kind: ActorHuman, Name: "Rev <r@x>"}
	sys := Actor{Kind: ActorSystem}
	agent := Actor{Kind: ActorAgent}
	req := []string{TypeRule}

	// Agent không bao giờ xác thực được.
	it := newItem(TypePattern, StatusCandidate, OriginAgent)
	if err := Transition(it, TransitionInput{To: StatusVerified, Actor: agent, ChecksPassed: true, RequireHuman: req, Now: now}); !errors.Is(err, ErrTransition) {
		t.Fatal("agent không được xác thực")
	}
	// Hệ thống không xác thực được rule, mục gắn cờ bên thứ ba, hay loại cấu hình cần người.
	rule := newItem(TypeRule, StatusCandidate, OriginAgent)
	if err := Transition(rule, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: true, RequireHuman: nil, Now: now}); err == nil {
		t.Fatal("rule luôn cần người duyệt, kể cả khi cấu hình bỏ rule")
	}
	tp := newItem(TypePattern, StatusCandidate, OriginAgent)
	tp.Origin.ThirdParty = true
	if err := Transition(tp, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: true, RequireHuman: req, Now: now}); err == nil {
		t.Fatal("mục bên thứ ba cần người duyệt")
	}
	fact := newItem(TypeFact, StatusCandidate, OriginAgent)
	if err := Transition(fact, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: true, RequireHuman: []string{TypeRule, TypeFact}, Now: now}); err == nil {
		t.Fatal("loại trong require_human_approval_for cần người duyệt")
	}
	// Hệ thống xác thực pattern khi kiểm tra qua; không qua thì không.
	p := newItem(TypePattern, StatusCandidate, OriginAgent)
	if err := Transition(p, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: false, RequireHuman: req, Now: now}); err == nil {
		t.Fatal("kiểm tra chưa qua")
	}
	if err := Transition(p, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: true, RequireHuman: req, Now: now}); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusVerified || p.ReviewedBy != "" || p.VerifiedAt == "" {
		t.Fatalf("%+v", p)
	}
	// Người duyệt rule: ghi người duyệt.
	if err := Transition(rule, TransitionInput{To: StatusVerified, Actor: human, ChecksPassed: true, RequireHuman: req, Now: now}); err != nil {
		t.Fatal(err)
	}
	if rule.ReviewedBy != "Rev <r@x>" || rule.ReviewedAt == "" {
		t.Fatalf("%+v", rule)
	}
	// Mâu thuẫn chặn xác thực.
	c := newItem(TypeWarning, StatusCandidate, OriginAgent)
	if err := Transition(c, TransitionInput{To: StatusVerified, Actor: human, ChecksPassed: true, Conflicts: []string{"X"}, Now: now}); err == nil {
		t.Fatal("mâu thuẫn phải chặn")
	}
	// verified → stale chỉ bởi hệ thống/người; stale → verified chỉ người.
	if err := Transition(rule, TransitionInput{To: StatusStale, Actor: agent, Now: now}); err == nil {
		t.Fatal("agent không đánh dấu stale")
	}
	if err := Transition(rule, TransitionInput{To: StatusStale, Actor: sys, Reason: "fp", Now: now}); err != nil || rule.Status != StatusStale {
		t.Fatal(err)
	}
	if err := Transition(rule, TransitionInput{To: StatusVerified, Actor: sys, ChecksPassed: true, RequireHuman: req, Now: now}); err == nil {
		t.Fatal("stale → verified cần người")
	}
	if err := Transition(rule, TransitionInput{To: StatusCandidate, Actor: human, Now: now}); err == nil {
		t.Fatal("không quay về candidate")
	}
	// deprecated: cần lý do, agent không được.
	if err := Transition(rule, TransitionInput{To: StatusDeprecated, Actor: human, Now: now}); err == nil {
		t.Fatal("cần lý do")
	}
	if err := Transition(rule, TransitionInput{To: StatusDeprecated, Actor: agent, Reason: "x", Now: now}); err == nil {
		t.Fatal("agent không loại bỏ")
	}
	if err := Transition(rule, TransitionInput{To: StatusDeprecated, Actor: human, Reason: "cũ", Now: now}); err != nil || rule.Status != StatusDeprecated {
		t.Fatal(err)
	}
}

func repoWith(t *testing.T, files map[string]string) (string, Source) {
	t.Helper()
	root := testutil.NewRepo(t, files)
	return root, &WorktreeSource{Root: root}
}

func codes(r Result) string {
	var cs []string
	for _, f := range r.Findings {
		cs = append(cs, f.Level+":"+f.Code)
	}
	return strings.Join(cs, ",")
}

func TestVerify(t *testing.T) {
	root, src := repoWith(t, map[string]string{
		"internal/db/db.go": "package db\n\n// Open mở cơ sở dữ liệu.\nfunc Open(path string) error {\n\treturn nil\n}\n",
		"Makefile":          "gen:\n\tgo generate ./...\n",
		"vendor/x/x.go":     "package x\n",
	})
	good := newItem(TypeRule, StatusCandidate, OriginAgent)
	good.Scope = Scope{Paths: []string{"internal/db/**"}, Symbols: []string{"Open"}}
	good.Evidence = []Evidence{{Path: "internal/db/db.go", Lines: "4-6", Symbol: "Open"}, {Path: "Makefile"}}
	if err := FillFingerprints(good, src); err != nil {
		t.Fatal(err)
	}
	good.Body = "Mở bằng `internal/db/db.go`.\n\n```go path=internal/db/db.go\nfunc Open(path string) error {\n```\n\n```sh\nmake gen\n```"
	v := Verifier{Src: src, All: []*Item{good}}
	if r := v.Verify(good); !r.OK() || len(r.Findings) != 0 {
		t.Fatalf("mục tốt: %s %+v", codes(r), r.Findings)
	}

	bad := newItem(TypeWarning, StatusCandidate, OriginAgent)
	bad.Title = good.Title
	bad.Scope = Scope{Paths: []string{"nope/**"}, Symbols: []string{"Missing"}}
	bad.Evidence = []Evidence{{Path: "gone.go"}, {Path: "internal/db/db.go", Lines: "1-2", Symbol: "Open", Fingerprint: "lines:stale"},
		{Path: "vendor/x/x.go"}}
	bad.Origin.Inputs = []string{"https://example.com/doc"}
	bad.Body = "Xem `missing/dir/file.go`.\n\n```go\nfunc Totally(different) code() {}\n```"
	r := Verifier{Src: src, All: []*Item{good, bad}}.Verify(bad)
	got := codes(r)
	for _, want := range []string{"error:evidence", "error:fingerprint", "error:symbol", "error:scope", "error:example",
		"warn:path-ref", "error:duplicate", "error:third-party"} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %s trong %s", want, got)
		}
	}

	// Bằng chứng đổi sau khi ghi → lỗi fingerprint; mục không đổi phạm vi vẫn ổn.
	os.WriteFile(filepath.Join(root, "internal", "db", "db.go"), []byte("package db\n\n// Open mở.\nfunc Open(path string, ro bool) error {\n\treturn nil\n}\n"), 0o644)
	r = Verifier{Src: &WorktreeSource{Root: root}, All: []*Item{good}}.Verify(good)
	if !r.OnlyErrors(CodeFingerprint, CodeExample) || !r.Has(CodeFingerprint) {
		t.Fatalf("sau khi sửa bằng chứng: %s", codes(r))
	}

	// Tổng quan kiến trúc bị cảnh báo.
	ov := newItem(TypeFact, StatusCandidate, OriginAgent)
	ov.Title = "Tổng quan kiến trúc của dự án"
	ov.Evidence = []Evidence{{Path: "Makefile"}}
	FillFingerprints(ov, src)
	if r := (Verifier{Src: src}).Verify(ov); !strings.Contains(codes(r), "warn:overview") {
		t.Fatalf("overview: %s", codes(r))
	}
}

func TestCommitSource(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"a.go": "v1\n", "d/b.go": "x\n"})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(root, "a.go"), []byte("v2\n"), 0o644)
	testutil.Git(t, root, "commit", "-qam", "v2")
	src := &CommitSource{RepoDir: root, Commit: base}
	b, ok := src.Read("a.go")
	if !ok || string(b) != "v1\n" {
		t.Fatalf("%q %v", b, ok)
	}
	if _, ok := src.Read("nope.go"); ok {
		t.Fatal("tệp không tồn tại")
	}
	if !HasFileOrDir(src, "d") || HasFileOrDir(src, "e") || len(src.Files()) != 2 {
		t.Fatalf("files: %v", src.Files())
	}
}

func TestStoreAndFind(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	a := newItem(TypeRule, StatusCandidate, OriginAgent)
	b := newItem(TypeRule, StatusCandidate, OriginAgent)
	if err := s.Save(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(b); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "BROKEN.md"), []byte("garbage"), 0o644)
	items, bad, err := s.List()
	if err != nil || len(items) != 2 || len(bad) != 1 {
		t.Fatalf("%d %v %v", len(items), bad, err)
	}
	if got, err := Find(items, strings.ToLower(a.ID[len(a.ID)-6:])); err != nil || got.ID != a.ID {
		t.Fatalf("hậu tố: %v %v", got, err)
	}
	if _, err := Find(items, a.ID[:4]); err == nil && a.ID[:4] == b.ID[:4] {
		t.Fatal("tiền tố chung phải mơ hồ")
	}
	if _, err := Find(items, "ab"); err == nil {
		t.Fatal("quá ngắn")
	}
	bad2 := newItem(TypeRule, StatusCandidate, OriginAgent)
	bad2.Title = ""
	if err := s.Save(bad2); err == nil {
		t.Fatal("mục không hợp lệ không được lưu")
	}
}

func TestRender(t *testing.T) {
	mk := func(typ, status string, paths ...string) *Item {
		it := newItem(typ, status, OriginAgent)
		it.Scope.Paths = paths
		return it
	}
	gRule := mk(TypeRule, StatusVerified)
	pRule := mk(TypeRule, StatusVerified, "internal/db/**")
	warn := mk(TypeWarning, StatusVerified, "internal/db/**")
	cand := mk(TypeRule, StatusCandidate)
	stale := mk(TypeRule, StatusStale)
	dep := mk(TypeRule, StatusDeprecated)
	fact := mk(TypeFact, StatusVerified)
	tp := mk(TypeRule, StatusCandidate)
	tp.Origin.ThirdParty = true
	long := mk(TypeWarning, StatusVerified)
	long.Body = "a\n```go\n1\n2\n3\n4\n5\n6\n7\n8\n9\n```"
	items := []*Item{pRule, warn, cand, stale, dep, fact, gRule, tp, long}

	out, sel := RenderAgents(items, RenderOptions{MaxItems: 10})
	if len(sel.Rules) != 2 || sel.Rules[0] != gRule || len(sel.Warnings) != 2 || len(sel.Reference) != 0 {
		t.Fatalf("chọn sai: %+v", sel)
	}
	for _, s := range []string{cand.ID, stale.ID, dep.ID, fact.ID, tp.ID} {
		if strings.Contains(out, s) {
			t.Errorf("%s không được xuất hiện", s)
		}
	}
	if !strings.Contains(out, "Khi làm việc với `internal/db/**`") || !strings.Contains(out, "[k:"+gRule.ID+"]") {
		t.Fatalf("định dạng:\n%s", out)
	}
	if strings.Count(out, "```")%2 != 0 {
		t.Fatalf("khối mã bị cắt phải được đóng:\n%s", out)
	}
	if !strings.Contains(out, "bint knowledge add") {
		t.Fatal("khối AGENTS.md cần chỉ dẫn dùng Bint")
	}

	evalOut, sel := RenderAgents(items, RenderOptions{MaxItems: 10, IncludeCandidates: true, ForEval: true})
	if len(sel.Reference) != 1 || sel.Reference[0] != cand || strings.Contains(evalOut, "bint knowledge") {
		t.Fatalf("eval: %+v\n%s", sel, evalOut)
	}
	var omittedTP bool
	for _, o := range sel.Omitted {
		omittedTP = omittedTP || o.ID == tp.ID
	}
	if !omittedTP {
		t.Fatal("mục bên thứ ba chưa duyệt phải bị bỏ")
	}

	_, sel = RenderAgents(items, RenderOptions{MaxItems: 1})
	if sel.Count() != 1 || len(sel.Omitted) < 3 {
		t.Fatalf("giới hạn: %+v", sel)
	}
	empty, _ := RenderAgents(nil, RenderOptions{})
	if !strings.Contains(empty, "Chưa có quy tắc") {
		t.Fatal(empty)
	}
}

func TestThirdPartyInput(t *testing.T) {
	for in, want := range map[string]bool{
		"vendor/github.com/x/y.go": true, "web/node_modules/a/index.js": true, "https://x.io": true,
		"issue:42": true, "internal/vendorize.go": false, "README.md": false,
	} {
		if IsThirdPartyInput(in) != want {
			t.Errorf("%s: muốn %v", in, want)
		}
	}
}
