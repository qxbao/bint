package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// PendingWork đếm mục tri thức lỗi thời (bằng chứng đổi) và dời dòng bằng kiểm tra xác định được, không đổi trạng thái.
func TestPendingWorkCountsStaleAndRelocated(t *testing.T) {
	testutil.Isolate(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{
		"README.md": "# demo\n",
		"src/a.go":  "package src\n\nfunc A() {\n\tprintln(\"a\")\n}\n",
		"src/b.go":  "package src\n\nfunc B() {\n\tprintln(\"b\")\n}\n",
	})
	if _, err := Init(ctx, InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, ev string) {
		res, err := AddKnowledge(ctx, k, AddOptions{Type: knowledge.TypeFact, Title: title, Body: "Nội dung riêng của " + title,
			Paths: []string{"src/**"}, Evidence: []string{ev}, Origin: knowledge.OriginAgent})
		if err != nil || !res.Saved {
			t.Fatalf("%s: %v %+v", title, err, res.Check.Findings)
		}
	}
	add("mục a", "src/a.go:3-5")
	add("mục b", "src/b.go:3-5")
	if err := os.WriteFile(filepath.Join(root, "src/a.go"), []byte("package src\n\nfunc A() {\n\tprintln(\"đổi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	} // sửa trong khối: fingerprint đổi
	if err := os.WriteFile(filepath.Join(root, "src/b.go"), []byte("package src\n\n// một\n// hai\nfunc B() {\n\tprintln(\"b\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	} // chèn phía trên: dời dòng
	rep, err := PendingWork(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if rep.KnowledgeStale != 1 || rep.KnowledgeRelocated != 1 {
		t.Errorf("lỗi thời=%d dời dòng=%d, muốn 1 và 1 (lỗi đọc: %v)", rep.KnowledgeStale, rep.KnowledgeRelocated, rep.Errors)
	}
}
