package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/testutil"
)

// baseOf trả về commit cha của commit lời giải (commit gốc của tác vụ).
func baseOf(t *testing.T, root, solution string) string {
	t.Helper()
	return testutil.Git(t, root, "rev-parse", solution+"^")
}

func baseDirOf(env Env, base string) string {
	return filepath.Join(env.WorkRoot, "bases", base+".git")
}

// F10: kho đệm bị dọn khi dùng xong; thư mục làm việc đã dựng vẫn nguyên vẹn và
// không chứa đối tượng nào của commit sau commit gốc.
func TestReleaseBaseKeepsIsolation(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, _ := fixtureRepo(t)
	env, _, _ := testEnv(t, root)
	base := baseOf(t, root, solution)
	dest := filepath.Join(t.TempDir(), "w")
	if err := prepareWorkdir(ctx, env, base, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(baseDirOf(env, base)); err != nil {
		t.Fatalf("kho đệm phải tồn tại sau khi dựng: %v", err)
	}
	releaseBase(env, base)
	if _, err := os.Stat(baseDirOf(env, base)); !os.IsNotExist(err) {
		t.Fatalf("kho đệm phải bị dọn, err=%v", err)
	}
	if _, err := git(ctx, dest, "status", "--porcelain"); err != nil {
		t.Fatalf("thư mục làm việc hỏng sau khi dọn kho đệm: %v", err)
	}
	if _, err := git(ctx, dest, "cat-file", "-e", solution+"^{commit}"); err == nil {
		t.Fatal("thư mục làm việc không được chứa commit lời giải")
	}
	// Không còn alternates trỏ về kho đệm đã xoá.
	if _, err := os.Stat(filepath.Join(dest, ".git", "objects", "info", "alternates")); err == nil {
		t.Fatal("không được dùng alternates")
	}
	// Dọn lại một kho không tồn tại không lỗi; dựng lại thì tự nạp lại.
	releaseBase(env, base)
	if err := prepareWorkdir(ctx, env, base, filepath.Join(t.TempDir(), "w2")); err != nil {
		t.Fatal(err)
	}
}

// KeepBases: giữ kho đệm như hành vi cũ.
func TestReleaseBaseKeep(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, _, _ := testEnv(t, root)
	env.Cfg.Eval.KeepBases = true
	base := baseOf(t, root, solution)
	if err := prepareWorkdir(context.Background(), env, base, filepath.Join(t.TempDir(), "w")); err != nil {
		t.Fatal(err)
	}
	releaseBase(env, base)
	if _, err := os.Stat(baseDirOf(env, base)); err != nil {
		t.Fatalf("keep_bases phải giữ kho đệm: %v", err)
	}
}

// Nạp nông: mặc định (0) giữ lịch sử đầy đủ; base_depth=1 chỉ có một commit.
func TestBaseDepth(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, _, bad := fixtureRepo(t)
	solution := bad // commit "tương lai" so với base; base có 3 commit lịch sử
	base := baseOf(t, root, bad)
	full, _, _ := testEnv(t, root)
	d1 := filepath.Join(t.TempDir(), "full")
	if err := prepareWorkdir(ctx, full, base, d1); err != nil {
		t.Fatal(err)
	}
	n, _ := gitString(ctx, d1, "rev-list", "--count", "HEAD")
	if n == "1" {
		t.Fatalf("mặc định phải giữ lịch sử đầy đủ, có %s commit", n)
	}
	shallow, _, _ := testEnv(t, root)
	shallow.Cfg.Eval.BaseDepth = 1
	d2 := filepath.Join(t.TempDir(), "shallow")
	if err := prepareWorkdir(ctx, shallow, base, d2); err != nil {
		t.Fatal(err)
	}
	if n, _ := gitString(ctx, d2, "rev-list", "--count", "HEAD"); n != "1" {
		t.Fatalf("base_depth=1 phải có đúng 1 commit, có %s", n)
	}
	if _, err := git(ctx, d2, "cat-file", "-e", solution+"^{commit}"); err == nil {
		t.Fatal("thư mục làm việc không được chứa commit lời giải")
	}
}

// Thiếu dung lượng: dừng có thông báo rõ ràng trước khi nạp.
func TestEnsureBaseFreeSpace(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, _, _ := testEnv(t, root)
	env.Cfg.Eval.MinFreeMB = 1 << 40 // lớn hơn mọi ổ đĩa
	err := prepareWorkdir(context.Background(), env, baseOf(t, root, solution), filepath.Join(t.TempDir(), "w"))
	if err == nil || !strings.Contains(err.Error(), "dung lượng") || !strings.Contains(err.Error(), "bint eval clean") {
		t.Fatalf("mong đợi lỗi dung lượng có hướng dẫn, được %v", err)
	}
	if _, serr := os.Stat(baseDirOf(env, baseOf(t, root, solution))); !os.IsNotExist(serr) {
		t.Fatal("không được tạo kho đệm khi thiếu dung lượng")
	}
}

func fakeBase(t *testing.T, workRoot, name string, size int, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(workRoot, "bases", name+".git")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack"), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-age)
	if err := os.Chtimes(dir, ts, ts); err != nil {
		t.Fatal(err)
	}
	return dir
}

// LRU: vượt trần thì xoá kho dùng lâu nhất, bỏ qua kho được bảo vệ.
func TestPruneBasesLRU(t *testing.T) {
	root := t.TempDir()
	old := fakeBase(t, root, "old", 1000, 3*time.Hour)
	mid := fakeBase(t, root, "mid", 1000, 2*time.Hour)
	cur := fakeBase(t, root, "cur", 1000, 4*time.Hour) // cũ nhất nhưng đang dùng
	newest := fakeBase(t, root, "new", 1000, time.Hour)
	n, freed, err := PruneBases(root, 2000, map[string]bool{cur: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || freed < 2000 {
		t.Fatalf("xoá %d kho, giải phóng %d", n, freed)
	}
	for _, p := range []string{old, mid} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s phải bị xoá", p)
		}
	}
	for _, p := range []string{cur, newest} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s phải còn", p)
		}
	}
	// Trần 0 = không giới hạn.
	if n, _, _ := PruneBases(root, 0, nil); n != 0 {
		t.Fatalf("trần 0 không được xoá, xoá %d", n)
	}
}

func TestCleanBases(t *testing.T) {
	root := t.TempDir()
	fakeBase(t, root, "a", 500, time.Hour)
	fakeBase(t, root, "b", 700, time.Hour)
	if err := os.MkdirAll(filepath.Join(root, "bases", "c.git.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	n, freed, err := CleanBases(root)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || freed < 1200 {
		t.Fatalf("xoá %d, giải phóng %d", n, freed)
	}
	if ents, _ := os.ReadDir(filepath.Join(root, "bases")); len(ents) != 0 {
		t.Fatalf("còn %d mục", len(ents))
	}
	// Thư mục chưa có: không lỗi.
	if _, _, err := CleanBases(filepath.Join(root, "none")); err != nil {
		t.Fatal(err)
	}
}

// Song song: nhiều lượt dựng cùng một base trong khi base bị dọn xen kẽ vẫn không lỗi.
func TestPrepareWorkdirConcurrentWithRelease(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	root, solution, _ := fixtureRepo(t)
	env, _, _ := testEnv(t, root)
	base := baseOf(t, root, solution)
	tmp := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dest := filepath.Join(tmp, "w"+string(rune('a'+i)))
			if err := prepareWorkdir(context.Background(), env, base, dest); err != nil {
				errs <- err
			}
			releaseBase(env, base)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// Run dọn kho đệm của base khi mọi lượt dùng base đó đã xong.
func TestRunReleasesBases(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	if _, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{}); err != nil {
		t.Fatal(err)
	}
	cands, _ := st.Candidates()
	var good *Case
	for _, c := range cands {
		if c.Source.Commit == solution {
			good = c
		}
	}
	agentDir := t.TempDir()
	writeFiles(t, agentDir, map[string]string{"agent.sh": "echo '{\"type\":\"result\",\"total_cost_usd\":0.1}'\n"})
	agent := &CommandAgent{Command: "sh " + filepath.ToSlash(filepath.Join(agentDir, "agent.sh"))}
	_, err := Run(ctx, env, db, RunOptions{
		Cases: []*Case{good}, Arms: []string{ArmNone}, Repeats: 2, LeakageMode: LeakStrict,
		Agent: agent, Parallel: 2, Seed: 1, Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(filepath.Join(env.WorkRoot, "bases")); len(ents) != 0 {
		t.Fatalf("kho đệm phải được dọn sau đợt, còn %d", len(ents))
	}
}
