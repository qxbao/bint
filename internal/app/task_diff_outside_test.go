package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/task"
	"bint/internal/testutil"
)

// NT-WF-BASELINE: tệp người/tác vụ khác đổi vào cùng cây sau BaseTree không được lẫn vào diff của tác vụ.
func TestDiffTaskExcludesFilesOutsideTouched(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v", res)
	}
	// Người khác sửa hai tệp (một commit, một chưa commit) sau khi tác vụ chạy.
	for _, f := range []string{"other1.txt", "other2.txt"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("của người khác\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testutil.Git(t, root, "add", "other1.txt")
	testutil.Git(t, root, "commit", "-q", "-m", "commit của người khác")

	for _, tc := range []struct {
		name string
		o    TaskDiffOptions
	}{
		{"name-only", TaskDiffOptions{Ref: id, NameOnly: true}},
		{"stat", TaskDiffOptions{Ref: id, Stat: true}},
		{"diff", TaskDiffOptions{Ref: id}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := DiffTask(ctx, ws, tc.o)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Paths) != 1 || d.Paths[0] != "a.go" || strings.Contains(d.Text, "other") || !strings.Contains(d.Text, "a.go") {
				t.Fatalf("diff mặc định chỉ có a.go: paths=%v text=%q", d.Paths, d.Text)
			}
			if len(d.Excluded) != 2 || d.Excluded[0] != "other1.txt" || d.Excluded[1] != "other2.txt" {
				t.Fatalf("Excluded = %v", d.Excluded)
			}
			for _, want := range []string{"đã loại 2 tệp không thuộc tác vụ", "other1.txt", "other2.txt", "--all"} {
				if !strings.Contains(d.Note, want) {
					t.Errorf("Note thiếu %q: %s", want, d.Note)
				}
			}
		})
	}

	// --all: như hành vi cũ, hiện lại các tệp ngoài tác vụ, không có ghi chú loại tệp.
	d, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true, All: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(d.Text); strings.Join(got, ",") != "a.go,other1.txt,other2.txt" || len(d.Excluded) != 0 || strings.Contains(d.Note, "đã loại") {
		t.Fatalf("--all: %q excluded=%v note=%q", d.Text, d.Excluded, d.Note)
	}
}

// Tác vụ worktree: worktree riêng không có người viết khác và Land đưa mọi tệp đổi trong đó về cây gốc, nên diff mặc
// định phải hiện cả tệp ngoài Touched (phát hiện review 01M7AJMTHSJPN53DRYQN1PR4M8.1).
func TestDiffTaskWorktreeShowsFilesOutsideTouched(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v", res)
	}
	// Đánh dấu tác vụ chạy trong worktree riêng (cây hiện tại) lập từ một cây gốc khác.
	if _, err := ws.Tasks.Update(id, ws.Now(), func(tk *task.Task) error {
		tk.State.Origin = t.TempDir()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Tệp đổi trong worktree ngoài b-feat (ví dụ sửa tay giữa các bước): Land sẽ đưa nó về.
	if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("sửa ngoài b-feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(d.Text); strings.Join(got, ",") != "a.go,extra.txt" || len(d.Excluded) != 0 || strings.Contains(d.Note, "đã loại") {
		t.Fatalf("worktree: %q excluded=%v note=%q", d.Text, d.Excluded, d.Note)
	}
}

// Không tệp nào ngoài tác vụ: không có ghi chú, kết quả như cũ.
func TestDiffTaskNoNoteWhenNothingExcluded(t *testing.T) {
	ws, _, id := taskRepo(t)
	ctx := context.Background()
	if _, _, err := ApproveTask(ctx, ws, TaskApproveOptions{Ref: id}); err != nil {
		t.Fatal(err)
	}
	if res := runFeat(t, ws, id); res.Status != task.StatusInReview {
		t.Fatalf("b-feat: %+v", res)
	}
	d, err := DiffTask(ctx, ws, TaskDiffOptions{Ref: id, NameOnly: true})
	if err != nil || strings.TrimSpace(d.Text) != "a.go" || len(d.Excluded) != 0 || strings.Contains(d.Note, "đã loại") {
		t.Fatalf("%+v %v", d, err)
	}
}
