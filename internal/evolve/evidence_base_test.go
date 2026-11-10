package evolve

import (
	"context"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

const pyBase = `class Repo:
    limit = 10

    def fetch(self, key):
        row = self.db.get(key)
        return row

    def save(self, key, value):
        self.db.put(key, value)
`

// Ở HEAD lớp có thêm phương thức và hàm bị đẩy xuống; nội dung fetch/save không đổi.
const pyHead = `import os

class Repo:
    limit = 10

    def count(self):
        return len(self.db)

    def fetch(self, key):
        row = self.db.get(key)
        return row

    def save(self, key, value):
        self.db.put(key, value)
`

// Hai lớp có phương thức run trùng tên; thứ tự đảo giữa base và HEAD.
const pyDupBase = `class A:
    def run(self):
        return "a"

class B:
    def run(self):
        return "b"
`
const pyDupHead = `class B:
    def run(self):
        return "b"

class A:
    def run(self):
        return "a"
`

func evidenceCase(t *testing.T, base, head string) (repo string, c *eval.Case, src knowledge.Source) {
	t.Helper()
	repo = testutil.NewRepo(t, map[string]string{"m.py": base})
	baseSHA := testutil.Git(t, repo, "rev-parse", "HEAD")
	commitFiles(t, repo, "head", map[string]string{"m.py": head})
	c = &eval.Case{Source: eval.Source{Base: baseSHA}}
	return repo, c, &knowledge.CommitSource{RepoDir: repo, Commit: "HEAD"}
}

func holdsAt(t *testing.T, repo string, c *eval.Case, src knowledge.Source, spec string) bool {
	t.Helper()
	d := &Draft{Type: "fact", Title: "t", Body: "b", Evidence: []string{spec}}
	it, err := DraftItem(d, "k1", nil, src, "symbol", "p1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return eval.EvidenceHoldsAt(context.Background(), repo, c, it)
}

// F2: bằng chứng gắn với lớp không được mất hiệu lực chỉ vì lớp có thêm phương thức.
func TestEvidenceClassSymbolSurvivesNewMethod(t *testing.T) {
	repo, c, src := evidenceCase(t, pyBase, pyHead)
	// Vùng dòng nằm trong fetch (dòng 10-11 ở HEAD), ký hiệu chỉ là cả lớp.
	if !holdsAt(t, repo, c, src, "m.py:10-11#Repo") {
		t.Error("bằng chứng trong phương thức của lớp phải còn khớp tại base")
	}
	// Vùng dòng là thuộc tính của lớp (không thuộc phương thức nào).
	if !holdsAt(t, repo, c, src, "m.py:4#Repo") {
		t.Error("bằng chứng là thân lớp phải so phần thân liên quan, không so cả lớp")
	}
}

// Không nới: phương thức thực sự khác tại base thì vẫn không khớp.
func TestEvidenceNarrowedStillStrict(t *testing.T) {
	changed := `class Repo:
    limit = 10

    def fetch(self, key):
        return None
`
	repo, c, src := evidenceCase(t, changed, pyHead)
	if holdsAt(t, repo, c, src, "m.py:10-11#Repo") {
		t.Error("fetch khác tại base thì không được tính là khớp")
	}
}

// Symbol bị dời dòng/đổi thứ tự giữa base và HEAD: tìm lại theo tên trong đúng lớp.
func TestEvidenceSymbolRelocatedByQualifiedName(t *testing.T) {
	repo, c, src := evidenceCase(t, pyDupBase, pyDupHead)
	if !holdsAt(t, repo, c, src, "m.py:6-7#A.run") {
		t.Error("A.run chỉ bị dời xuống, nội dung không đổi: phải khớp tại base")
	}
	if !holdsAt(t, repo, c, src, "m.py:2-3#B.run") {
		t.Error("B.run phải khớp tại base")
	}
}

func TestNarrowEvidence(t *testing.T) {
	e := knowledge.NarrowEvidence(knowledge.Evidence{Path: "m.py", Lines: "10-11", Symbol: "Repo"}, []byte(pyHead))
	if e.Symbol != "Repo.fetch" {
		t.Errorf("symbol = %q, muốn Repo.fetch", e.Symbol)
	}
	// Vùng dòng trải qua nhiều phương thức: giữ nguyên.
	e = knowledge.NarrowEvidence(knowledge.Evidence{Path: "m.py", Lines: "10-15", Symbol: "Repo"}, []byte(pyHead))
	if e.Symbol != "Repo" {
		t.Errorf("không được thu hẹp khi vùng dòng trải nhiều phương thức: %q", e.Symbol)
	}
}
