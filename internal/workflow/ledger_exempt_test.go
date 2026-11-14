package workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/testutil"
)

// Sổ phiếu (.bint/tasks) do Bint ghi, kể cả trong bước chỉ-đọc: mọi kiểm tra cây làm việc phải bỏ qua nó.
func TestLedgerPathExemptFromTreeChecks(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n", ".bint/tasks/01M7D2DDP0QFAQKXP484QS7VJX/state.json": "{}\n"})
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	fp0 := task.WorktreeFingerprint(ctx, root)
	write(".bint/tasks/01M7D2DDP0QFAQKXP484QS7VJX/state.json", `{"x":1}`+"\n") // sửa tệp đã theo dõi
	write(".bint/tasks/01M7D2DDP0QFAQKXP484QS7VJX/plan.md", "mới\n")           // tệp mới
	write(".bint/tasks/01M7D2DDP0QFAQKXP484QS7VJY/task.yml", "x\n")            // phiếu mới
	after, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("ảnh chụp cây phải không đổi khi chỉ .bint/tasks đổi")
	}
	if ps, _ := DiffPaths(ctx, root, "HEAD", after); len(ps) != 0 {
		t.Fatalf("DiffPaths phải bỏ qua .bint/tasks: %v", ps)
	}
	if v := ReadonlyViolations(ctx, root, "HEAD", after, time.Time{}, ""); len(v.Violations) != 0 {
		t.Fatalf("ReadonlyViolations phải bỏ qua .bint/tasks: %v", v.Violations)
	}
	if ps, err := nonCandidateChanges(ctx, root, "HEAD", after); err != nil || len(ps) != 0 {
		t.Fatalf("nonCandidateChanges: %v %v", ps, err)
	}
	if fs, _ := task.ChangedFiles(ctx, root, "HEAD"); len(fs) != 0 {
		t.Fatalf("ChangedFiles phải bỏ qua .bint/tasks: %v", fs)
	}
	if fp := task.WorktreeFingerprint(ctx, root); fp != fp0 {
		t.Fatal("dấu vân tay phải không đổi khi chỉ .bint/tasks đổi (kiểm chứng trước không được lỗi thời)")
	}
	// thay đổi mã thật vẫn được thấy.
	write("a.go", "package a\n// đổi\n")
	after2, _ := WorktreeSnapshot(ctx, root)
	if ps, _ := DiffPaths(ctx, root, before, after2); !slices.Equal(ps, []string{"a.go"}) {
		t.Fatalf("thay đổi mã thật phải còn: %v", ps)
	}
	if fp := task.WorktreeFingerprint(ctx, root); fp == fp0 {
		t.Fatal("dấu vân tay phải đổi khi mã đổi")
	}
}
