package knowledge

import (
	"strings"
	"testing"
)

func TestSymbolFingerprintIgnoresUnrelatedEdits(t *testing.T) {
	v1 := []byte("package x\n\nfunc A() int {\n\treturn 1\n}\n\nfunc B() int {\n\treturn 2\n}\n")
	e := Evidence{Path: "x.go", Lines: "7-9", Symbol: "B"}
	fp, err := Fingerprint(e, v1)
	if err != nil || !strings.HasPrefix(fp, FPSymbol+":") {
		t.Fatalf("fp=%q err=%v", fp, err)
	}
	e.Fingerprint = fp
	// Sửa A và chèn dòng phía trên: B không đổi.
	v2 := []byte("package x\n\nimport \"fmt\"\n\nfunc A() int {\n\tfmt.Println()\n\treturn 10\n}\n\nfunc B() int {\n\treturn 2\n}\n")
	if c := CheckEvidence(e, v2); c.Status != EvMatch {
		t.Fatalf("B không đổi nhưng %+v", c)
	}
	v3 := strings.Replace(string(v2), "return 2", "return 3", 1)
	if c := CheckEvidence(e, []byte(v3)); c.Status != EvChanged {
		t.Fatalf("B đổi nhưng %+v", c)
	}
	v4 := strings.Replace(string(v2), "func B()", "func C()", 1)
	if c := CheckEvidence(e, []byte(v4)); c.Status != EvChanged || !strings.Contains(c.Detail, "ký hiệu") {
		t.Fatalf("B mất nhưng %+v", c)
	}
}

func TestLinesFingerprintRelocates(t *testing.T) {
	v1 := []byte("a\nb\nTARGET1\nTARGET2\nc\n")
	e := Evidence{Path: "f.txt", Lines: "3-4"}
	fp, _ := FingerprintWith(e, v1, "hunk")
	if !strings.HasPrefix(fp, FPLines+":") {
		t.Fatal(fp)
	}
	e.Fingerprint = fp
	v2 := []byte("new\nnew2\na\nb\nTARGET1\nTARGET2\nc changed\n")
	c := CheckEvidence(e, v2)
	if c.Status != EvRelocated || c.Lines != "5-6" {
		t.Fatalf("%+v", c)
	}
	v3 := []byte("a\nb\nTARGET1 sửa\nTARGET2\nc\n")
	if c := CheckEvidence(e, v3); c.Status != EvChanged {
		t.Fatalf("%+v", c)
	}
	// Mục cũ (fingerprint "lines:") vẫn được kiểm tra theo mức đã lưu dù có symbol.
	e2 := Evidence{Path: "x.go", Lines: "1", Symbol: "x"}
	e2.Fingerprint, _ = FingerprintWith(e2, []byte("package x\n"), "hunk")
	if c := CheckEvidence(e2, []byte("package x\n")); c.Status != EvMatch {
		t.Fatalf("%+v", c)
	}
}

func TestVerifierReportsRelocation(t *testing.T) {
	src := &memSource{files: map[string]string{"f.txt": "x\ny\nKEY LINE\n"}}
	it := &Item{Schema: 1, ID: "01JABCDEFGHJKMNPQRSTVWXYZ0", Type: TypeFact, Status: StatusVerified, Title: "t", Body: "b",
		Origin: Origin{Kind: OriginHuman}, Version: 1, CreatedAt: "2026-01-01T00:00:00Z",
		Evidence: []Evidence{{Path: "f.txt", Lines: "2"}}}
	it.Evidence[0].Fingerprint, _ = FingerprintWith(it.Evidence[0], []byte("x\ny\n"), "hunk")
	r := Verifier{Src: src}.Verify(it)
	if !r.OK() || len(r.Relocated) != 0 {
		t.Fatalf("%+v", r)
	}
	src.files["f.txt"] = "new\nx\ny\nKEY LINE\n"
	r = Verifier{Src: src}.Verify(it)
	if !r.OK() || len(r.Relocated) != 1 || r.Relocated[0].To != "3" {
		t.Fatalf("%+v", r)
	}
	if !r.ApplyRelocations(it) || it.Evidence[0].Lines != "3" {
		t.Fatal(it.Evidence[0])
	}
}

type memSource struct{ files map[string]string }

func (m *memSource) Name() string { return "mem" }
func (m *memSource) Read(rel string) ([]byte, bool) {
	s, ok := m.files[rel]
	return []byte(s), ok
}
func (m *memSource) Files() []string {
	var out []string
	for k := range m.files {
		out = append(out, k)
	}
	return out
}
