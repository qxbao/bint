package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/testutil"
)

// E2E của G2 (docs/isolation-subagent-plan.md mục 5.6): tác nhân giả bằng --agent-command, tiến trình bint thật.

const resultJSON = `echo '{"type":"result","total_cost_usd":0.01,"num_turns":1,"result":"xong"}'
`

// waitFor là đoạn sh chờ tệp $MARKS/<name> xuất hiện (tối đa ~40 giây), nếu không thì thoát mã 7.
func waitFor(name string) string {
	return `i=0; while [ ! -f "$MARKS/` + name + `" ] && [ $i -lt 400 ]; do sleep 0.1; i=$((i+1)); done
[ -f "$MARKS/` + name + `" ] || { echo "hết giờ chờ ` + name + `" >&2; exit 7; }
`
}

func initWorktreeRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tác nhân giả viết bằng sh")
	}
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	// Kiểm chứng: có tệp *.done (mỗi tác vụ tự tạo tệp của mình).
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte("eval:\n  test_command: \"ls *.done\"\n"), 0o644)
	t.Setenv("MARKS", t.TempDir())
	return root
}

type newTaskJSON struct {
	ID              string   `json:"id"`
	Isolation       string   `json:"isolation"`
	IsolationReason string   `json:"isolation_reason"`
	IsolationTasks  []string `json:"isolation_tasks"`
}

func newTask(t *testing.T, dir string, args ...string) newTaskJSON {
	t.Helper()
	out, code := run(t, dir, "", append([]string{"task", "new", "--no-plan", "--json"}, args...)...)
	var c newTaskJSON
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
		t.Fatalf("task new %v: %d\n%s", args, code, out)
	}
	return c
}

type taskState struct {
	Dir   string `json:"dir"`
	State struct {
		Status       string   `json:"status"`
		Reason       string   `json:"reason"`
		Origin       string   `json:"origin"`
		Worktree     string   `json:"worktree"`
		Branch       string   `json:"branch"`
		AwaitingLand bool     `json:"awaiting_land"`
		Touched      []string `json:"touched"`
		Landed       *struct {
			Files     []string `json:"files"`
			Conflicts []string `json:"conflicts"`
			Kept      bool     `json:"kept"`
		} `json:"landed"`
	} `json:"state"`
	Isolation       string `json:"isolation"`
	IsolationReason string `json:"isolation_reason"`
}

func showTask(t *testing.T, dir, id string) taskState {
	t.Helper()
	out, code := run(t, dir, "", "task", "show", id, "--json")
	var s taskState
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil {
		t.Fatalf("task show %s: %d\n%s", id, code, out)
	}
	return s
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC-1: hai tác vụ lập liên tiếp trên một cây ⇒ tác vụ hai tự vào worktree (concurrent_task); cả hai b-feat chạy
// ĐỒNG THỜI (mỗi bên chờ bên kia bắt đầu) tới done; cây gốc nhận đủ thay đổi của cả hai dưới dạng chưa commit,
// chỉ mục không đổi.
func TestWorktreeParallelEndToEnd(t *testing.T) {
	root := initWorktreeRepo(t)
	marks := os.Getenv("MARKS")
	a := newTask(t, root, "Thêm tính năng A cho app.txt")
	b := newTask(t, root, "Thêm tính năng B cho app.txt")
	if a.Isolation != "shared" || a.IsolationReason != "default" || len(a.IsolationTasks) != 0 {
		t.Fatalf("tác vụ A: %+v", a)
	}
	if b.Isolation != "worktree" || b.IsolationReason != "concurrent_task" || len(b.IsolationTasks) != 1 || b.IsolationTasks[0] != a.ID {
		t.Fatalf("tác vụ B phải tự vào worktree vì A: %+v", b)
	}
	if s := showTask(t, root, b.ID); s.Isolation != "worktree" || s.IsolationReason != "concurrent_task" {
		t.Fatalf("task show --json: %+v", s)
	}
	headBefore := testutil.Git(t, root, "rev-parse", "HEAD")

	scriptA := writeScript(t, `echo A > a.done
echo "A" > a-only.txt
touch "$MARKS/a-started"
`+waitFor("b-started")+resultJSON)
	// B chờ cả A bắt đầu (cùng chạy) lẫn A đã done (để B đưa về sau A: tránh làm mất hiệu lực kiểm chứng của A).
	scriptB := writeScript(t, `echo B > b.done
echo "B" > b-only.txt
pwd > "$MARKS/b-cwd"
touch "$MARKS/b-started"
`+waitFor("a-started")+waitFor("a-done")+resultJSON)

	type result struct {
		out  string
		code int
	}
	var wg sync.WaitGroup
	results := map[string]*result{}
	var mu sync.Mutex
	start := func(id, script string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, code := run(t, root, "", "run", "b-do", "--task", id, "--no-review", "--agent-command", script)
			mu.Lock()
			results[id] = &result{out, code}
			mu.Unlock()
		}()
	}
	// B tạo worktree (chụp cây gốc dưới khoá chia sẻ) TRƯỚC khi A (shared) giữ khoá độc quyền của cây gốc trong
	// b-feat: nếu A vào trước, B chờ khoá tới khi A xong mà A lại chờ B bắt đầu ⇒ treo tới hết giờ chờ.
	start(b.ID, scriptB)
	for wait := time.Now().Add(60 * time.Second); showTask(t, root, b.ID).State.Origin == ""; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(wait) {
			t.Fatal("B không tạo được worktree")
		}
	}
	start(a.ID, scriptA)

	// Chờ A done rồi báo cho B (và kiểm run status của B khi nó còn đang chạy trong worktree).
	deadline := time.Now().Add(60 * time.Second)
	for showTask(t, root, a.ID).State.Status != "done" {
		if time.Now().After(deadline) {
			st := showTask(t, root, a.ID).State; t.Fatalf("A không tới done: %s %s", st.Status, st.Reason)
		}
		time.Sleep(200 * time.Millisecond)
	}
	bs := showTask(t, root, b.ID)
	if bs.State.Origin != root || bs.State.Branch != "bint/"+b.ID || !strings.HasSuffix(filepath.ToSlash(bs.State.Worktree), "/worktrees/"+b.ID) ||
		!exists(bs.State.Worktree) {
		t.Fatalf("B phải đang ở worktree riêng: %+v", bs.State)
	}
	if bs.State.Status != "in_progress" {
		t.Fatalf("B phải đang b-feat khi A đã done: %s", bs.State.Status)
	}
	out, code := run(t, root, "", "run", "status", b.ID)
	if code != 0 || !strings.Contains(out, bs.State.Worktree) || !strings.Contains(out, "bint/"+b.ID) || !strings.Contains(out, "chưa") ||
		!strings.Contains(out, "worktree") {
		t.Fatalf("run status của B thiếu worktree/nhánh/trạng thái đưa về: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "run", "status", b.ID, "--json")
	var rs struct {
		Isolation string `json:"isolation"`
		Worktree  string `json:"worktree"`
		Branch    string `json:"branch"`
		Origin    string `json:"origin"`
		Landed    bool   `json:"landed"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || rs.Isolation != "worktree" || rs.Worktree != bs.State.Worktree ||
		rs.Branch != "bint/"+b.ID || rs.Origin != root || rs.Landed {
		t.Fatalf("run status --json: %d\n%s", code, out)
	}
	// Agent của B chạy trong worktree, không phải cây gốc.
	if got, _ := os.ReadFile(filepath.Join(marks, "b-cwd")); !strings.Contains(string(got), "worktrees") {
		t.Fatalf("B chạy ở %q", got)
	}
	// Khi B còn đang chạy, cây gốc chưa có tệp của B.
	if exists(filepath.Join(root, "b.done")) {
		t.Fatal("tệp của B lọt vào cây gốc trước khi đưa về")
	}
	touch(t, filepath.Join(marks, "a-done"))
	wg.Wait()
	for id, r := range results {
		if r.code != 0 || !strings.Contains(r.out, "done") {
			t.Fatalf("run %s: %d\n%s", id, r.code, r.out)
		}
	}
	if results[b.ID] == nil || !strings.Contains(results[b.ID].out, "đã đưa") {
		t.Fatalf("kết quả B thiếu ghi chú đưa về: %+v", results[b.ID])
	}

	for _, f := range []string{"a.done", "a-only.txt", "b.done", "b-only.txt"} {
		if !exists(filepath.Join(root, f)) {
			t.Errorf("cây gốc thiếu %s", f)
		}
	}
	if got := testutil.Git(t, root, "status", "--porcelain"); !strings.Contains(got, "?? a.done") || !strings.Contains(got, "?? b.done") {
		t.Fatalf("thay đổi phải là chưa commit:\n%s", got)
	}
	if got := testutil.Git(t, root, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
		t.Fatalf("chỉ mục bị đổi: %q", got)
	}
	if testutil.Git(t, root, "rev-parse", "HEAD") != headBefore {
		t.Fatal("HEAD bị đổi")
	}
	bd := showTask(t, root, b.ID)
	if bd.State.Status != "done" || bd.State.Landed == nil || bd.State.Landed.Kept || len(bd.State.Landed.Files) == 0 {
		t.Fatalf("B sau khi xong: %+v", bd.State)
	}
	if exists(bs.State.Worktree) {
		t.Fatal("worktree của B phải được gỡ")
	}
	if got := testutil.Git(t, root, "branch", "--list", "bint/*"); strings.TrimSpace(got) != "" {
		t.Fatalf("nhánh bint còn: %q", got)
	}
	if got := testutil.Git(t, root, "for-each-ref", "refs/bint"); strings.TrimSpace(got) != "" {
		t.Fatalf("ref bint còn: %q", got)
	}
	// task diff của tác vụ đã đưa về dùng bản lưu (worktree đã gỡ).
	if out, code := run(t, root, "", "task", "diff", b.ID, "--name-only"); code != 0 || !strings.Contains(out, "b.done") || strings.Contains(out, "a.done") {
		t.Fatalf("task diff B sau land: %d\n%s", code, out)
	}
}

// AC-2/3/4 theo từng bước (như skill b-do khi chạy từng bước): task new --worktree → run b-feat → land --keep →
// done; cancel cần --discard (chỉ người); land --3way từ agent bị từ chối; doctor báo worktree còn lại.
func TestWorktreeStepwiseLandKeepCancelDoctor(t *testing.T) {
	root := initWorktreeRepo(t)
	c := newTask(t, root, "--worktree", "Thêm tính năng v2 cho app.txt")
	if c.Isolation != "worktree" || c.IsolationReason != "user" {
		t.Fatalf("--worktree: %+v", c)
	}
	script := writeScript(t, "echo v2 > app.txt\necho ok > x.done\n"+resultJSON)
	if out, code := run(t, root, "", "run", "b-feat", "--task", c.ID, "--agent-command", script); code != 0 || !strings.Contains(out, "in_review") {
		t.Fatalf("run b-feat: %d\n%s", code, out)
	}
	s := showTask(t, root, c.ID)
	wt := s.State.Worktree
	if !exists(wt) || s.State.Origin != root {
		t.Fatalf("thiếu worktree: %+v", s.State)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "app.txt")); string(got) != "v1\n" {
		t.Fatal("cây gốc bị chạm trước khi đưa về")
	}

	// task show nêu nơi chạy và trạng thái đưa về.
	if out, _ := run(t, root, "", "task", "show", c.ID); !strings.Contains(out, "Worktree:") || !strings.Contains(out, "Đưa về:      chưa") ||
		!strings.Contains(out, "Cô lập:") {
		t.Fatalf("task show thiếu thông tin cô lập:\n%s", out)
	}

	// Huỷ khi còn thay đổi chưa đưa về ⇒ cần --discard.
	if out, code := run(t, root, "", "task", "cancel", c.ID); code == 0 || !strings.Contains(out, "--discard") || !exists(wt) {
		t.Fatalf("cancel không --discard phải bị từ chối: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "status", c.ID, "cancelled"); code == 0 || !strings.Contains(out, "--discard") || !exists(wt) {
		t.Fatalf("status cancelled cùng đường: %d\n%s", code, out)
	}
	// Từ phiên agent: --discard và land --3way bị từ chối (ErrAgentForbidden), không đổi gì.
	t.Setenv("BINT_AGENT", "1")
	for _, args := range [][]string{{"task", "cancel", c.ID, "--discard"}, {"task", "land", c.ID, "--3way"}} {
		if out, code := run(t, root, "", args...); code == 0 || !strings.Contains(out, "chỉ dành cho người") || !exists(wt) {
			t.Fatalf("%v từ agent phải bị từ chối: %d\n%s", args, code, out)
		}
	}
	t.Setenv("BINT_AGENT", "")
	asHuman(t)

	// Chưa kiểm chứng/duyệt thì land bị chặn bởi điều kiện done? Ở đây b-feat đã tự kiểm chứng ⇒ land được, --keep.
	out, code := run(t, root, "", "task", "land", c.ID, "--keep")
	if code != 0 || !strings.Contains(out, "Đã đưa 2 tệp") || !strings.Contains(out, "giữ worktree") {
		t.Fatalf("land --keep: %d\n%s", code, out)
	}
	if !exists(wt) {
		t.Fatal("--keep phải giữ worktree")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "app.txt")); string(got) != "v2\n" || !exists(filepath.Join(root, "x.done")) {
		t.Fatal("cây gốc không nhận thay đổi")
	}
	if got := testutil.Git(t, root, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
		t.Fatalf("chỉ mục bị đổi: %q", got)
	}
	if out, code := run(t, root, "", "task", "land", c.ID); code == 0 || !strings.Contains(out, "đã đưa về") {
		t.Fatalf("land lần hai: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "task", "status", c.ID, "done"); code != 0 {
		t.Fatalf("done sau land: %d\n%s", code, out)
	}
	if got := showTask(t, root, c.ID); got.State.Status != "done" || got.State.Landed == nil || !got.State.Landed.Kept {
		t.Fatalf("trạng thái cuối: %+v", got.State)
	}

	// doctor báo worktree còn lại (giữ bằng --keep) cùng thư mục lạ, và in lệnh dọn.
	stateDir := filepath.Dir(filepath.Dir(filepath.Dir(showTask(t, root, c.ID).Dir)))
	stray := filepath.Join(stateDir, "worktrees", "01STRAYTASK")
	if err := os.MkdirAll(stray, 0o755); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, root, "", "doctor")
	if !strings.Contains(out, "worktree mồ côi") || !strings.Contains(out, stray) || !strings.Contains(out, wt) ||
		!strings.Contains(out, "worktree remove --force") {
		t.Fatalf("doctor không báo worktree mồ côi: %d\n%s", code, out)
	}
}

// Xung đột: người sửa cùng dòng ở cây gốc trong lúc tác vụ chạy ⇒ blocked "xung đột khi đưa về", cây gốc không bị
// đụng; land --3way (người) để lại dấu xung đột; resume ⇒ done.
func TestWorktreeConflictBlocksThenThreeWay(t *testing.T) {
	root := initWorktreeRepo(t)
	c := newTask(t, root, "--worktree", "Thêm tính năng v2 cho app.txt")
	script := writeScript(t, `echo v2 > app.txt
echo ok > x.done
printf 'nguoi dung sua\n' > "$ORIGIN/app.txt"
`+resultJSON)
	t.Setenv("ORIGIN", root)
	out, code := run(t, root, "", "run", "b-do", "--task", c.ID, "--no-review", "--agent-command", script)
	if code != 1 || !strings.Contains(out, "blocked") || !strings.Contains(out, "xung đột khi đưa về: app.txt") {
		t.Fatalf("phải blocked vì xung đột: %d\n%s", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "app.txt")); string(got) != "nguoi dung sua\n" {
		t.Fatalf("cây gốc bị đụng: %q", got)
	}
	if exists(filepath.Join(root, "x.done")) {
		t.Fatal("tệp không xung đột cũng không được áp")
	}
	if s := showTask(t, root, c.ID); s.State.Status != "blocked" || !s.State.AwaitingLand || !exists(s.State.Worktree) {
		t.Fatalf("trạng thái: %+v", s.State)
	}
	// land thường: lỗi nêu tệp xung đột, cây gốc không đổi.
	if out, code := run(t, root, "", "task", "land", c.ID); code == 0 || !strings.Contains(out, "xung đột khi đưa về: app.txt") {
		t.Fatalf("land thường: %d\n%s", code, out)
	}
	// Người --3way.
	out, code = run(t, root, "", "task", "land", c.ID, "--3way")
	if code != 0 || !strings.Contains(out, "dấu xung đột") || !strings.Contains(out, "app.txt") {
		t.Fatalf("land --3way: %d\n%s", code, out)
	}
	got, _ := os.ReadFile(filepath.Join(root, "app.txt"))
	if !strings.Contains(string(got), "<<<<<<<") || !strings.Contains(string(got), "nguoi dung sua") || !strings.Contains(string(got), "v2") {
		t.Fatalf("app.txt thiếu dấu xung đột:\n%s", got)
	}
	if !exists(filepath.Join(root, "x.done")) {
		t.Fatal("tệp sạch phải được áp")
	}
	if got := testutil.Git(t, root, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
		t.Fatalf("chỉ mục bị đổi: %q", got)
	}
	if s := showTask(t, root, c.ID); s.State.Landed == nil || len(s.State.Landed.Conflicts) != 1 || exists(s.State.Worktree) {
		t.Fatalf("Landed: %+v", s.State)
	}
	// resume vào thẳng finalize (không chạy lại agent) nhưng còn dấu xung đột ⇒ chưa done.
	out, code = run(t, root, "", "run", "resume", c.ID, "--agent-command", "false")
	if code != 1 || !strings.Contains(out, "xung đột") || !strings.Contains(out, "app.txt") {
		t.Fatalf("resume khi còn dấu xung đột phải blocked: %d\n%s", code, out)
	}
	// Người gỡ dấu xung đột rồi resume ⇒ done (script sẽ ghi đè app.txt nếu agent chạy lại).
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("da gop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = run(t, root, "", "run", "resume", c.ID, "--agent-command", "false")
	if code != 0 || !strings.Contains(out, "done") {
		t.Fatalf("resume: %d\n%s", code, out)
	}
}

// Lỗi dùng sai: --worktree/--shared loại trừ nhau, không dùng cùng --task.
func TestWorktreeFlagUsageErrors(t *testing.T) {
	root := initWorktreeRepo(t)
	if out, code := run(t, root, "", "task", "new", "--worktree", "--shared", "Việc gì đó"); code != 2 || !strings.Contains(out, "loại trừ nhau") {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	c := newTask(t, root, "Thêm tính năng v2 cho app.txt")
	if out, code := run(t, root, "", "run", "b-do", "--worktree", "--shared", "Việc gì đó"); code != 2 || !strings.Contains(out, "loại trừ nhau") {
		t.Fatalf("run hai cờ: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "run", "b-do", "--task", c.ID, "--worktree"); code != 2 || !strings.Contains(out, "--task") {
		t.Fatalf("run --task --worktree: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "run", "b-do", "--task", c.ID, "--shared"); code != 2 {
		t.Fatalf("run --task --shared: %d\n%s", code, out)
	}
}
