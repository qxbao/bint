package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// E2E GĐ4 của docs/custom-flow-plan.md: chạy flow tuỳ biến tdd (plan optional, red verify:false role test-writer, green,
// review fix:green) bằng tiến trình bint thật với tác nhân giả. Tác nhân ghi bước nó chạy vào $STEPLOG để kiểm thứ tự.

// tddAgent là tác nhân giả của flow tdd. Hành vi sai (plan ghi tệp, red ghi ngoài scope) bật bằng tệp cờ trong $FLAGS.
const tddAgent = `set -e
P="$(cat "$BINT_EVAL_PROMPT_FILE")"
STEP="$(printf '%s' "$P" | sed -n 's/.*<bint-task id="[^"]*" step="\([^"]*\)".*/\1/p' | head -1)"
echo "$STEP" >> "$STEPLOG"
printf '%s' "$P" > "$PROMPTS/$STEP.$(wc -l < "$STEPLOG" | tr -d '[:space:]').txt"
flag() { test -e "$FLAGS/$1"; }
case "$STEP" in
plan)
  if flag plan-writes; then echo oops > plan-wrote.txt; fi
  printf '# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n' | "$BINT" task write "$BINT_TASK_ID" requirement -
  printf '# Kế hoạch\nSửa app.txt\n' | "$BINT" task write "$BINT_TASK_ID" plan -
  printf -- '- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: chứa v2\n' | "$BINT" task write "$BINT_TASK_ID" tasks -
  ;;
red)
  echo "kiểm thử đỏ" > red_test.txt
  if flag red-outside; then mkdir -p secret && echo x > secret/leak.txt; fi
  ;;
green)
  echo v2 > app.txt
  echo ok > done.txt
  "$BINT" task criteria "$BINT_TASK_ID" 1 met --evidence "app.txt chứa v2"
  ;;
review)
  N="$(grep -c '^review$' "$STEPLOG")"
  if [ "$N" = 1 ]; then
    "$BINT" task review "$BINT_TASK_ID" - <<'EOF'
{"summary":"cần sửa","findings":[{"kind":"bug","severity":"high","title":"Thiếu xử lý","path":"app.txt","lines":"1","evidence":"app.txt chưa đủ","body":"Sửa lại."}]}
EOF
  else
    printf '{"summary":"ổn","findings":[]}' | "$BINT" task review "$BINT_TASK_ID" -
  fi
  ;;
esac
echo '{"type":"result","total_cost_usd":0.01,"num_turns":2,"result":"xong"}'
`

// tddRepo dựng repo có flow tdd, skill tuỳ biến write-failing-test và lệnh kiểm chứng đếm số lần chạy vào $VERIFYLOG.
func tddRepo(t *testing.T) (root string, env map[string]string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root = testutil.NewRepo(t, map[string]string{"app.txt": "v1\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	tmp := t.TempDir()
	env = map[string]string{"STEPLOG": filepath.Join(tmp, "steps"), "VERIFYLOG": filepath.Join(tmp, "verify"),
		"FLAGS": filepath.Join(tmp, "flags"), "PROMPTS": filepath.Join(tmp, "prompts"), "BINT": bintBin}
	for _, d := range []string{env["FLAGS"], env["PROMPTS"]} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"),
		[]byte("eval:\n  test_command: \"echo run >> \\\"$VERIFYLOG\\\"; test -f done.txt\"\n"), 0o644)
	if out, code := run(t, root, "", "skill", "new", "write-failing-test"); code != 0 {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	skill := filepath.Join(root, ".bint", "skills", "write-failing-test", "SKILL.md")
	b, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(skill, append(b, []byte("\nVIẾT-KIỂM-THỬ-ĐỎ-TRƯỚC: tạo kiểm thử thất bại rồi dừng.\n")...), 0o644)
	writeFlowE2E(t, root, "tdd.yml", e2eTDD)
	if out, code := run(t, root, "", "flow", "lint", "tdd"); code != 0 {
		t.Fatalf("flow lint: %d\n%s", code, out)
	}
	script := filepath.Join(tmp, "agent.sh")
	os.WriteFile(script, []byte(tddAgent), 0o755)
	env["SCRIPT"] = script
	return root, env
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return strings.Fields(string(b))
}

type flowTask struct {
	State struct {
		Status       string `json:"status"`
		Reason       string `json:"reason"`
		Workflow     string `json:"workflow"`
		FlowStep     string `json:"flow_step"`
		FlowSHA      string `json:"flow_sha"`
		Verification []struct {
			Passed bool `json:"passed"`
		} `json:"verifications"`
	} `json:"state"`
}

func showFlowTask(t *testing.T, root, id string) flowTask {
	t.Helper()
	out, code := run(t, root, "", "task", "show", id, "--json")
	var s flowTask
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil {
		t.Fatalf("task show %s: %d\n%s", id, code, out)
	}
	return s
}

func newFlowTask(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, code := run(t, root, "", append([]string{"task", "new", "--json", "--workflow", "tdd"}, args...)...)
	var c struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
		t.Fatalf("task new --workflow tdd: %d\n%s", code, out)
	}
	return c.ID
}

// Thứ tự plan, red, green, review, green, review rồi done; red không kiểm chứng, mỗi green kiểm chứng một lần; prompt của
// red chứa thân skill tuỳ biến; flow đổi giữa chừng làm resume blocked kèm lý do.
func TestFlowRunTDDEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	cmd := agentCmd(env)

	id := newFlowTask(t, root, "--plan", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	s := showFlowTask(t, root, id)
	if s.State.Workflow != "tdd" || !strings.HasPrefix(s.State.FlowSHA, "sha256:") {
		t.Fatalf("phiếu phải mang flow và băm: %+v", s.State)
	}

	out, code := run(t, root, "", "run", "tdd", "--task", id, "--agent-command", cmd)
	if code != 0 || !strings.Contains(out, ": done") {
		t.Fatalf("run tdd: %d\n%s", code, out)
	}
	if got, want := strings.Join(readLines(t, env["STEPLOG"]), ","), "plan,red,green,review,green,review"; got != want {
		t.Fatalf("thứ tự bước = %s, muốn %s\n%s", got, want, out)
	}
	// Kiểm chứng: không chạy sau red; chạy sau mỗi green (hai lần), nên đúng 2 lần.
	if n := len(readLines(t, env["VERIFYLOG"])); n != 2 {
		t.Fatalf("lệnh kiểm chứng chạy %d lần, muốn 2 (sau mỗi green, không sau red)", n)
	}
	if s = showFlowTask(t, root, id); s.State.Status != "done" || len(s.State.Verification) != 2 {
		t.Fatalf("trạng thái cuối: %+v", s.State)
	}
	// Lượt chạy: lượt cha mang tên flow, lượt con mang id bước của flow (nạp spool vào CSDL không vướng tên lạ).
	if out, code = run(t, root, "", "run", "status", id); code != 0 {
		t.Fatalf("run status: %d\n%s", code, out)
	}
	for _, want := range []string{"tdd", "plan", "red", "green", "review"} {
		if !strings.Contains(out, want) {
			t.Errorf("run status thiếu %q:\n%s", want, out)
		}
	}
	if out, code = run(t, root, "", "task", "show", id); code != 0 || !strings.Contains(out, "Workflow:   tdd") || !strings.Contains(out, "Bước flow:  review") {
		t.Fatalf("task show phải hiện flow và bước flow: %d\n%s", code, out)
	}
	// Prompt của red chứa thân skill tuỳ biến; prompt của green là skill gốc b-feat.
	red, _ := os.ReadFile(filepath.Join(env["PROMPTS"], "red.2.txt"))
	if !strings.Contains(string(red), "VIẾT-KIỂM-THỬ-ĐỎ-TRƯỚC") {
		t.Fatalf("prompt của red thiếu thân skill write-failing-test:\n%s", red)
	}
	if grn, _ := os.ReadFile(filepath.Join(env["PROMPTS"], "green.3.txt")); strings.Contains(string(grn), "VIẾT-KIỂM-THỬ-ĐỎ-TRƯỚC") {
		t.Fatal("prompt của green không được chứa skill của red")
	}
}

// Bước plan (chỉ đọc) ghi tệp ⇒ blocked (giữ kết quả bước); red sửa tệp ngoài scope (scope.deny) ⇒ blocked chờ người duyệt, không chạy green.
func TestFlowRunReadOnlyAndScopeEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	cmd := agentCmd(env)

	os.WriteFile(filepath.Join(env["FLAGS"], "plan-writes"), nil, 0o644)
	id := newFlowTask(t, root, "--plan", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	out, code := run(t, root, "", "run", "tdd", "--task", id, "--agent-command", cmd)
	if code == 0 || !strings.Contains(out, "chỉ đọc") || !strings.Contains(out, "plan-wrote.txt") {
		t.Fatalf("plan ghi tệp phải blocked vì chỉ đọc: %d\n%s", code, out)
	}
	if s := showFlowTask(t, root, id); s.State.Status != "blocked" || !strings.Contains(s.State.Reason, "bint run resume") {
		t.Fatalf("sau plan ghi tệp: %+v", s.State)
	}

	os.Remove(filepath.Join(root, "plan-wrote.txt"))
	os.Remove(env["STEPLOG"])
	os.Remove(filepath.Join(env["FLAGS"], "plan-writes"))
	os.WriteFile(filepath.Join(env["FLAGS"], "red-outside"), nil, 0o644)
	id = newFlowTask(t, root, "--no-plan", "--deny", "secret/**", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	out, code = run(t, root, "", "run", "tdd", "--task", id, "--agent-command", cmd)
	if code == 0 || !strings.Contains(out, "secret/leak.txt") {
		t.Fatalf("red sửa ngoài scope phải blocked: %d\n%s", code, out)
	}
	if s := showFlowTask(t, root, id); s.State.Status != "blocked" {
		t.Fatalf("sau red ngoài scope: %+v", s.State)
	}
	if got := strings.Join(readLines(t, env["STEPLOG"]), ","); got != "red" {
		t.Fatalf("chỉ red được chạy, nhưng chạy %s", got)
	}
	if n := len(readLines(t, env["VERIFYLOG"])); n != 0 {
		t.Fatalf("không kiểm chứng sau red bị chặn: %d lần", n)
	}
}

// Flow bị sửa giữa chừng (khác FlowSHA): run --task và resume đều blocked kèm lý do nêu flow, không chạy bước nào; khôi
// phục tệp thì resume chạy tiếp.
func TestFlowRunChangedFlowBlocksEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	cmd := agentCmd(env)
	id := newFlowTask(t, root, "--no-plan", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")

	writeFlowE2E(t, root, "tdd.yml", strings.Replace(e2eTDD, "verify: false", "verify: true", 1))
	out, code := run(t, root, "", "run", "tdd", "--task", id, "--agent-command", cmd)
	if code == 0 || !strings.Contains(out, "flow tdd đã đổi từ lúc lập phiếu") {
		t.Fatalf("flow đổi phải blocked: %d\n%s", code, out)
	}
	s := showFlowTask(t, root, id)
	if s.State.Status != "blocked" || !strings.Contains(s.State.Reason, ".bint/flows/tdd.yml") {
		t.Fatalf("trạng thái: %+v", s.State)
	}
	if len(readLines(t, env["STEPLOG"])) != 0 {
		t.Fatal("không bước nào được chạy khi flow đổi")
	}
	if out, code = run(t, root, "", "run", "resume", id, "--agent-command", cmd); code == 0 || !strings.Contains(out, "flow tdd đã đổi") {
		t.Fatalf("resume phải blocked: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "run", "tdd", "--task", id, "--agent-command", cmd); code == 0 || !strings.Contains(out, "flow tdd đã đổi") {
		t.Fatalf("run --task phải blocked: %d\n%s", code, out)
	}

	writeFlowE2E(t, root, "tdd.yml", e2eTDD)
	if out, code = run(t, root, "", "run", "resume", id, "--agent-command", cmd); code != 0 || !strings.Contains(out, ": done") {
		t.Fatalf("resume sau khi khôi phục flow: %d\n%s", code, out)
	}
	if got := strings.Join(readLines(t, env["STEPLOG"]), ","); got != "red,green,review,green,review" {
		t.Fatalf("thứ tự sau resume = %s", got)
	}
}

// bint run <flow> "<mô tả>" tự lập phiếu rồi chạy hết flow; --step và tên flow lạ bị từ chối.
func TestFlowRunNewTaskAndStepEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	cmd := agentCmd(env)

	out, code := run(t, root, "", "run", "tdd", "--no-plan", "--agent-command", cmd, "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")
	if code != 0 || !strings.Contains(out, ": done") {
		t.Fatalf("run tdd tự lập phiếu: %d\n%s", code, out)
	}
	if got := strings.Join(readLines(t, env["STEPLOG"]), ","); got != "red,green,review,green,review" {
		t.Fatalf("thứ tự = %s", got)
	}

	if out, code = run(t, root, "", "run", "khong-co-flow", "--agent-command", cmd, "Việc gì đó"); code == 0 || !strings.Contains(out, "khong-co-flow") {
		t.Fatalf("flow lạ phải lỗi: %d\n%s", code, out)
	}
	id := newFlowTask(t, root, "--no-plan", "--criteria", "app.txt chứa v2", "Việc khác cho app.txt")
	if out, code = run(t, root, "", "run", "tdd", "--step", "red", "--agent-command", cmd, "Việc"); code != 2 {
		t.Fatalf("--step không có --task phải là lỗi dùng sai (mã 2): %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "run", "b-feat", "--task", id, "--step", "red"); code != 2 {
		t.Fatalf("--step với workflow gốc phải mã 2: %d\n%s", code, out)
	}
	if out, code = run(t, root, "", "run", "tdd", "--task", id, "--step", "green", "--agent-command", cmd); code == 0 || !strings.Contains(out, "bước kế tiếp") {
		t.Fatalf("--step không phải bước kế tiếp phải bị từ chối: %d\n%s", code, out)
	}
	os.Remove(env["STEPLOG"])
	if out, code = run(t, root, "", "run", "tdd", "--task", id, "--step", "red", "--agent-command", cmd); code != 0 {
		t.Fatalf("--step red: %d\n%s", code, out)
	}
	if got := strings.Join(readLines(t, env["STEPLOG"]), ","); got != "red" {
		t.Fatalf("--step red chỉ chạy đúng một bước, chạy %s", got)
	}
	if s := showFlowTask(t, root, id); s.State.Status != "in_progress" || s.State.FlowStep != "green" {
		t.Fatalf("sau red: %+v", s.State)
	}
}

// sessAct mở rộng sessAction với các trường GĐ4 (model) cho test driver session.
type sessActModel struct {
	sessAction
	Model string `json:"model"`
}

func sessM(t *testing.T, dir string, args ...string) (sessActModel, int, string) {
	t.Helper()
	out, code := run(t, dir, "", append([]string{"run"}, args...)...)
	var a sessActModel
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&a); err != nil && code != 2 {
		t.Fatalf("run %v: không phải JSON: %v\n%s", args, err, out)
	}
	return a, code, out
}

// Driver session trên flow tdd: step/subagent/readonly/model từng bước, vòng sửa về green, kiểm chứng đúng số lần, done.
func TestFlowSessionTDDEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	asAgent(t) // phiên orchestrator: không BINT_RUN_ID
	cfg := "eval:\n  test_command: \"echo run >> \\\"$VERIFYLOG\\\"; test -f done.txt\"\nharnesses:\n  claude:\n    models:\n      test-writer: haiku\n"
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(cfg), 0o644)
	id := newFlowTask(t, root, "--plan", "--shared", "--driver", "session", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")

	// 1. plan: chỉ đọc, subagent theo kind.
	a, code, out := sessM(t, root, "next", id, "--json")
	if code != 0 || a.Action != "agent" || a.Step != "plan" || a.Subagent != "bint-planner" || !a.ReadOnly {
		t.Fatalf("next plan: %d\n%s", code, out)
	}
	for _, d := range []struct{ name, body string }{{"requirement", "# Yêu cầu\n## Tiêu chí chấp nhận\n- [ ] AC-1: app.txt chứa v2\n## Câu hỏi mở\n"},
		{"plan", "# Kế hoạch\nSửa app.txt\n"}, {"tasks", "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: chứa v2\n"}} {
		if o, c := run(t, root, d.body, "task", "write", id, d.name, "-"); c != 0 {
			t.Fatalf("task write %s: %d\n%s", d.name, c, o)
		}
	}
	if _, code, out = sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-plan", "--json"); code != 0 {
		t.Fatalf("complete plan: %d\n%s", code, out)
	}

	// 2. red: role test-writer ⇒ model riêng, subagent theo kind implement, được ghi; không kiểm chứng.
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Action != "agent" || a.Step != "red" || a.Subagent != "bint-implementer" || a.ReadOnly || a.Model != "haiku" ||
		!strings.Contains(a.Prompt, "VIẾT-KIỂM-THỬ-ĐỎ-TRƯỚC") {
		t.Fatalf("next red: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(a.Worktree, "red_test.txt"), []byte("đỏ\n"), 0o644)
	if _, code, out = sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-red", "--json"); code != 0 {
		t.Fatalf("complete red: %d\n%s", code, out)
	}
	if n := len(readLines(t, env["VERIFYLOG"])); n != 0 {
		t.Fatalf("không kiểm chứng sau red: %d lần", n)
	}
	if s := showFlowTask(t, root, id); s.State.Status != "in_progress" || s.State.FlowStep != "green" {
		t.Fatalf("sau red: %+v", s.State)
	}

	// 3. green: model của implementer (role gốc), kiểm chứng sau bước.
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Step != "green" || a.Subagent != "bint-implementer" || a.ReadOnly || a.Model != "sonnet" {
		t.Fatalf("next green: %d\n%s", code, out)
	}
	os.WriteFile(filepath.Join(a.Worktree, "app.txt"), []byte("v2\n"), 0o644)
	os.WriteFile(filepath.Join(a.Worktree, "done.txt"), []byte("ok\n"), 0o644)
	if o, c := run(t, root, "", "task", "criteria", id, "1", "met", "--evidence", "app.txt chứa v2"); c != 0 {
		t.Fatalf("criteria: %d\n%s", c, o)
	}
	if _, code, out = sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-green", "--json"); code != 0 {
		t.Fatalf("complete green: %d\n%s", code, out)
	}
	if n := len(readLines(t, env["VERIFYLOG"])); n != 1 {
		t.Fatalf("kiểm chứng sau green: %d lần, muốn 1", n)
	}

	// 4. review vòng 1 có phát hiện nghiêm trọng ⇒ quay về green (Fix), không về red.
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Step != "review" || a.Subagent != "bint-reviewer" || !a.ReadOnly {
		t.Fatalf("next review: %d\n%s", code, out)
	}
	if o, c := run(t, root, `{"summary":"cần sửa","findings":[{"kind":"bug","severity":"high","title":"Thiếu xử lý","path":"app.txt","lines":"1","evidence":"app.txt chưa đủ","body":"Sửa lại."}]}`,
		"task", "review", id, "-"); c != 0 {
		t.Fatalf("task review: %d\n%s", c, o)
	}
	if _, code, out = sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-rev1", "--json"); code != 0 {
		t.Fatalf("complete review 1: %d\n%s", code, out)
	}
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Step != "green" || !strings.Contains(a.Prompt, "Thiếu xử lý") {
		t.Fatalf("vòng sửa phải về green kèm phát hiện: %d\n%s", code, out)
	}
	if _, code, out = sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-green2", "--json"); code != 0 {
		t.Fatalf("complete green 2: %d\n%s", code, out)
	}
	if n := len(readLines(t, env["VERIFYLOG"])); n != 2 {
		t.Fatalf("kiểm chứng sau green sửa: %d lần, muốn 2", n)
	}

	// 5. review vòng 2 sạch ⇒ done.
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Step != "review" {
		t.Fatalf("next review 2: %d\n%s", code, out)
	}
	if o, c := run(t, root, reviewOK, "task", "review", id, "-"); c != 0 {
		t.Fatalf("task review 2: %d\n%s", c, o)
	}
	d, code, out := sessM(t, root, "complete", id, "--run", a.RunID, "--agent-id", "ag-rev2", "--json")
	if code != 0 || d.Action != "done" || d.Status != "done" {
		t.Fatalf("complete review 2: %d\n%s", code, out)
	}
}

// Role test-writer gán harness khác Claude ⇒ action process chạy đúng một bước bằng --step; flow đổi giữa chừng thì next
// blocked kèm lý do, khôi phục flow rồi next --resume chạy tiếp.
func TestFlowSessionProcessStepAndChangedFlowEndToEnd(t *testing.T) {
	root, env := tddRepo(t)
	asAgent(t)
	cfg := "eval:\n  test_command: \"echo run >> \\\"$VERIFYLOG\\\"; test -f done.txt\"\nharnesses:\n  enabled: [claude, codex]\nroles:\n  test-writer: codex\n"
	os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(cfg), 0o644)
	id := newFlowTask(t, root, "--no-plan", "--shared", "--driver", "session", "--criteria", "app.txt chứa v2", "Thêm tính năng v2 cho app.txt")

	// Flow đổi trước khi mở bước nào ⇒ blocked, không bước nào được mở.
	writeFlowE2E(t, root, "tdd.yml", strings.Replace(e2eTDD, "verify: false", "verify: true", 1))
	a, code, out := sessM(t, root, "next", id, "--json")
	if code != 1 || a.Action != "blocked" || !strings.Contains(a.Reason, "flow tdd đã đổi từ lúc lập phiếu") || a.RunID != "" {
		t.Fatalf("flow đổi phải blocked: %d\n%s", code, out)
	}
	if a, code, out = sessM(t, root, "next", id, "--resume", "--json"); code != 1 || a.Action != "blocked" {
		t.Fatalf("next --resume khi flow còn lệch phải blocked: %d\n%s", code, out)
	}
	writeFlowE2E(t, root, "tdd.yml", e2eTDD)

	a, code, out = sessM(t, root, "next", id, "--resume", "--json")
	want := "bint run tdd --task " + id + " --step red --json"
	if code != 0 || a.Action != "process" || a.Step != "red" || a.Command != want {
		t.Fatalf("role test-writer=codex phải trả process --step red: %d\n%s", code, out)
	}
	// Chạy lệnh process (thêm tác nhân giả), rồi next trả bước kế tiếp (green, role implementer ⇒ claude ⇒ agent).
	if out, code = run(t, root, "", "run", "tdd", "--task", id, "--step", "red", "--agent-command", agentCmd(env)); code != 0 {
		t.Fatalf("chạy lệnh process: %d\n%s", code, out)
	}
	if got := strings.Join(readLines(t, env["STEPLOG"]), ","); got != "red" {
		t.Fatalf("lệnh process chỉ chạy một bước: %s", got)
	}
	a, code, out = sessM(t, root, "next", id, "--json")
	if code != 0 || a.Action != "agent" || a.Step != "green" || a.Subagent != "bint-implementer" {
		t.Fatalf("sau red: %d\n%s", code, out)
	}
}

// agentCmd là lệnh shell chạy tác nhân giả: đường dẫn đổi sang `/` và có nháy để shell (Git Bash trên Windows)
// không bỏ dấu `\` hay tách đường dẫn có dấu cách.
func agentCmd(env map[string]string) string {
	return "sh '" + filepath.ToSlash(env["SCRIPT"]) + "'"
}
