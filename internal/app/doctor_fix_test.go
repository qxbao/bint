package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/testutil"
)

func orphanCheck(t *testing.T, root string) *Check {
	t.Helper()
	for _, c := range Doctor(context.Background(), root).Checks {
		if c.Name == "worktree mồ côi" {
			return &c
		}
	}
	return nil
}

func addOrphanWorktree(t *testing.T, ws *TaskWS, root, id string) string {
	t.Helper()
	p := filepath.Join(ws.K.Paths.Worktrees, id)
	testutil.Git(t, root, "worktree", "add", "-q", "-b", "bint/"+id, p, "HEAD")
	return p
}

func branchExists(t *testing.T, root, name string) bool {
	t.Helper()
	return strings.Contains(testutil.Git(t, root, "branch", "--list", name), name)
}

func TestDoctorFixOnOrphanWorktree(t *testing.T) {
	ws, root, _ := taskRepo(t)
	p := addOrphanWorktree(t, ws, root, "01STRAY")
	c := orphanCheck(t, root)
	if c == nil || c.Fix == nil || c.Fix.Kind != FixWorktree || c.Fix.TaskID != "01STRAY" || filepath.Clean(c.Fix.Path) != filepath.Clean(p) {
		t.Fatalf("worktree mồ côi xoá được phải có Fix: %+v", c)
	}
	steps, err := DescribeDoctorFix(context.Background(), root, DoctorFixOptions{Kind: c.Fix.Kind, Path: c.Fix.Path, TaskID: c.Fix.TaskID})
	if err != nil || !strings.Contains(strings.Join(steps, "\n"), "bint/01STRAY") {
		t.Fatalf("mô tả: %v %v", steps, err)
	}
	res, err := FixDoctor(context.Background(), root, DoctorFixOptions{Kind: c.Fix.Kind, Path: c.Fix.Path, TaskID: c.Fix.TaskID})
	if err != nil || len(res.Actions) != 2 {
		t.Fatalf("FixDoctor: %+v %v", res, err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("thư mục worktree còn")
	}
	if branchExists(t, root, "bint/01STRAY") {
		t.Fatal("nhánh còn")
	}
	if orphanCheck(t, root) != nil {
		t.Fatal("doctor vẫn báo worktree mồ côi")
	}
}

func TestDoctorFixPrunableWorktree(t *testing.T) {
	ws, root, _ := taskRepo(t)
	p := addOrphanWorktree(t, ws, root, "01GONE")
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	c := orphanCheck(t, root)
	if c == nil || c.Fix == nil {
		t.Fatalf("%+v", c)
	}
	if _, err := FixDoctor(context.Background(), root, DoctorFixOptions{Kind: FixWorktree, Path: c.Fix.Path, TaskID: c.Fix.TaskID}); err != nil {
		t.Fatal(err)
	}
	if branchExists(t, root, "bint/01GONE") || orphanCheck(t, root) != nil {
		t.Fatal("chưa dọn sạch")
	}
}

func TestDoctorFixRefusesUnsafeTargets(t *testing.T) {
	ws, root, id := taskRepo(t)
	ctx := context.Background()
	fix := func(path, tid string) error {
		_, err := FixDoctor(ctx, root, DoctorFixOptions{Kind: FixWorktree, Path: path, TaskID: tid})
		return err
	}
	// Ngoài thư mục worktrees của Bint.
	outside := filepath.Join(t.TempDir(), "x")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fix(outside, "x"); err == nil || !strings.Contains(err.Error(), "ngoài") {
		t.Fatalf("ngoài thư mục: %v", err)
	}
	if err := fix(ws.K.Paths.Worktrees, ""); err == nil {
		t.Fatal("chính thư mục worktrees không được xoá")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal(err)
	}
	// Không còn mồ côi: thư mục không ứng với mục nào (vd tác vụ đang chạy dùng worktree).
	p := addOrphanWorktree(t, ws, root, id)
	if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error {
		tk.State.Classification.Isolation = task.IsolationWorktree
		tk.State.Origin = root
		tk.State.Status = task.StatusInProgress
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := fix(p, id); err == nil || !strings.Contains(err.Error(), "không còn là worktree mồ côi") {
		t.Fatalf("tác vụ đang chạy: %v", err)
	}
	// TaskID không khớp.
	q := addOrphanWorktree(t, ws, root, "01STRAY")
	if err := fix(q, "01OTHER"); err == nil {
		t.Fatal("TaskID sai phải bị từ chối")
	}
	for _, d := range []string{p, q} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("không được xoá %s: %v", d, err)
		}
	}
	if !branchExists(t, root, "bint/"+id) || !branchExists(t, root, "bint/01STRAY") {
		t.Fatal("nhánh bị xoá")
	}
}

func TestDoctorFixUnlandedAndAwaitingMergeHaveNoFix(t *testing.T) {
	ws, root, id := taskRepo(t)
	p := addOrphanWorktree(t, ws, root, id)
	set := func(f func(s *task.State)) {
		t.Helper()
		if _, err := ws.Tasks.Update(id, time.Now(), func(tk *task.Task) error { f(&tk.State); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string) {
		t.Helper()
		c := orphanCheck(t, root)
		if c == nil || c.Fix != nil || !strings.Contains(c.Detail, p) {
			t.Fatalf("%s: không được có Fix: %+v", name, c)
		}
		if _, err := FixDoctor(context.Background(), root, DoctorFixOptions{Kind: FixWorktree, Path: p, TaskID: id}); err == nil {
			t.Fatalf("%s: FixDoctor phải từ chối", name)
		}
		if _, err := os.Stat(p); err != nil || !branchExists(t, root, "bint/"+id) {
			t.Fatalf("%s: worktree/nhánh bị đụng", name)
		}
	}
	set(func(s *task.State) { s.Status, s.Origin, s.Landed = task.StatusDone, root, nil })
	check("unlanded")
	set(func(s *task.State) {
		s.Status, s.Origin = task.StatusInReview, root
		s.Landed = &task.Landing{Kept: true, Unmerged: []string{"a.go"}}
	})
	check("awaiting merge")
}

func TestDoctorFixKindsFromChecks(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	ctx := context.Background()
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no(), NoSkills: true})
	oldTemplate(t, root)
	kinds := map[string][]DoctorFixKind{}
	for _, c := range Doctor(ctx, root).Checks {
		if c.Fix != nil {
			kinds[c.Name] = append(kinds[c.Name], c.Fix.Kind)
		}
	}
	if k := kinds[CheckConfigTemplate]; len(k) != 1 || k[0] != FixConfigTemplate {
		t.Fatalf("mẫu cấu hình: %v", kinds)
	}
	syncs := 0
	for name, k := range kinds {
		if strings.HasPrefix(name, "skill ") || strings.HasPrefix(name, "subagent ") {
			syncs++
			if k[0] != FixSync {
				t.Fatalf("%s: %v", name, k)
			}
		}
	}
	if syncs == 0 {
		t.Fatalf("skill/subagent chưa xuất bản phải có Fix sync: %v", kinds)
	}
}

func TestDoctorFixSpoolOver200(t *testing.T) {
	ws, root, _ := taskRepo(t)
	for i := 0; i < 201; i++ {
		if err := os.WriteFile(filepath.Join(ws.K.Paths.Spool, fmt.Sprintf("e%04d.jsonl", i)), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var c *Check
	for _, x := range Doctor(context.Background(), root).Checks {
		if x.Name == "spool" {
			c = &x
		}
	}
	if c == nil || c.Level != LevelWarn || c.Fix == nil || c.Fix.Kind != FixSpool {
		t.Fatalf("%+v", c)
	}
	if _, err := FixDoctor(context.Background(), root, DoctorFixOptions{Kind: FixSpool}); err != nil {
		t.Fatal(err)
	}
	if err := (DoctorFixOptions{Kind: FixSync}).Validate(); err == nil {
		t.Fatal("loại sync không chạy qua dịch vụ")
	}
}
