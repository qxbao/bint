package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/config"
	"bint/internal/skills"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Kiểm tra mục tiêu `TestGoalBDoLeanSlicesAndGates` của phiếu 01M7RZQ7DC3RAJ17WRKC9QMVA8: b-do nhanh, rẻ, không lãng phí.
//
// Hộp đen qua CLI: repo tạm là một module Go nhỏ (gói a có TestOld), .bint/local.yml (không commit) đặt verify.scope affected,
// eval.test_command = verify.command = `sh <rec> full` và affected_command = `sh <rec> affected {test_files}`; <rec> ghi mỗi lần chạy
// vào một tệp đếm rồi in kết quả kiểu `go test` của kiểm tra mục tiêu giả (ô c1..cN đỏ tới khi a/cK.txt ghi ok-cK). Agent giả (sh) cho
// b-req/b-goal/b-feat/b-review ghi chuỗi bước, đề bài và trạng thái tác vụ (bint task show --json) ở đầu mỗi b-feat, rồi làm hành động
// định trước theo thứ tự lần gọi b-feat. Quan sát: mã lint, số lần gọi agent b-goal, State.Slices, tệp trong cây sau lát hỏng, lý do
// chặn/lựa chọn, Verification.Scope/Selection của mọi lần kiểm chứng, số lần chạy verify.command đầy đủ, skill ba nơi và core.lock.

// ---------- dữ liệu chung ----------

var bdoCellAll = []string{"c1", "c2", "c3"}

const bdoGoalFile = "goal/goal_test.go"

// bdoAgentScript: agent giả. Biến môi trường: BINT, BDO_LOG (thư mục ghi), BDO_DOCS (requirement/plan/tasks/goal_test.go/feats),
// BDO_CELLS (các ô), BDO_FEAT_COST (chi phí báo cho mỗi b-feat).
const bdoAgentScript = `set -e
P="$(cat "$BINT_EVAL_PROMPT_FILE")"
L="$BDO_LOG"
cost=0
case "$P" in
*'step="b-req"'*)
  echo b-req >> "$L/calls"
  "$BINT" task write "$BINT_TASK_ID" requirement - < "$BDO_DOCS/requirement.md"
  "$BINT" task write "$BINT_TASK_ID" plan - < "$BDO_DOCS/plan.md"
  "$BINT" task write "$BINT_TASK_ID" tasks - < "$BDO_DOCS/tasks.md"
  ;;
*'step="b-goal"'*)
  echo b-goal >> "$L/calls"
  mkdir -p goal
  cp "$BDO_DOCS/goal_test.go" goal/goal_test.go
  ;;
*'step="b-review"'*)
  echo b-review >> "$L/calls"
  printf '{"summary":"ổn","findings":[]}' | "$BINT" task review "$BINT_TASK_ID" -
  ;;
*'step="b-feat"'*)
  k=$(( $(cat "$L/featn" 2>/dev/null || echo 0) + 1 ))
  echo $k > "$L/featn"
  echo b-feat >> "$L/calls"
  cp "$BINT_EVAL_PROMPT_FILE" "$L/feat-$k.prompt"
  "$BINT" task show "$BINT_TASK_ID" --json > "$L/feat-$k.json" 2>/dev/null || true
  for f in c1 c2 c3 x2; do if [ -f a/$f.txt ]; then cat a/$f.txt; else echo '<none>'; fi; done > "$L/feat-$k.files"
  act=$(sed -n "${k}p" "$BDO_DOCS/feats")
  [ -n "$act" ] || act=all
  cost="$BDO_FEAT_COST"
  i=0
  for c in $BDO_CELLS; do i=$((i+1)); "$BINT" task criteria "$BINT_TASK_ID" $i met --evidence "a/$c.txt ghi ok-$c" >/dev/null 2>&1 || true; done
  case "$act" in
  all) for c in $BDO_CELLS; do echo ok-$c > a/$c.txt; done ;;
  w:*) c=${act#w:}; echo ok-$c > a/$c.txt ;;
  slow:*) c=${act#slow:}; sleep 25; echo ok-$c > a/$c.txt ;;
  bad:*) c=${act#bad:}; echo sai > a/$c.txt; echo x > a/x2.txt ;;
  err:*) c=${act#err:}; echo dở > a/$c.txt; echo x > a/x2.txt; echo '{"type":"result","is_error":true,"total_cost_usd":0,"num_turns":1,"result":"lỗi giả"}'; exit 3 ;;
  hang:*) c=${act#hang:}; echo dở > a/$c.txt; echo x > a/x2.txt; (cd / && sleep 75 >/dev/null 2>&1 </dev/null); echo ok-$c > a/$c.txt ;;
  ask:*) c=${act#ask:}; echo ok-$c > a/$c.txt; "$BINT" task error "$BINT_TASK_ID" --step b-feat --question "Làm tiếp phần còn lại thế nào?" >/dev/null ;;
  cfg) for c in $BDO_CELLS; do echo ok-$c > a/$c.txt; done; printf '\n# dòng chú thích do b-feat thêm\n' >> .bint/config.yml ;;
  esac
  ;;
esac
echo "{\"type\":\"result\",\"total_cost_usd\":$cost,\"num_turns\":2,\"result\":\"xong\"}"
`

// bdoRecScript: lệnh kiểm chứng/affected giả. %[1]s = tệp đếm, %[2]d = số giây ngủ khi chạy dạng affected sau khi lát 1 đã ghi a/c1.txt
// (mô phỏng kiểm chứng và cổng mục tiêu chậm của Bint trong lát 1, không làm chậm mốc sau b-goal).
const bdoRecScript = `echo "$*" >> '%[1]s'
if [ "$1" = affected ] && [ %[2]d -gt 0 ] && [ -f a/c1.txt ] && [ ! -f a/x2.txt ]; then sleep %[2]d; fi
rc=0
if [ -f goal/goal_test.go ]; then
  fail=""
  for c in $(sed -n 's#^// CELLS: ##p' goal/goal_test.go); do
    if [ "$(cat a/$c.txt 2>/dev/null)" = "ok-$c" ]; then :; else fail="$fail $c"; fi
  done
  if [ -n "$fail" ]; then
    echo "--- FAIL: TestGoal (0.00s)"
    for c in $fail; do echo "    --- FAIL: TestGoal/$c (0.00s)"; done
    rc=1
  else
    echo "--- PASS: TestGoal (0.00s)"
  fi
fi
echo "--- PASS: TestOld (0.00s)"
if [ $rc -eq 0 ]; then echo PASS; else echo FAIL; fi
exit $rc
`

type bdoOpts struct {
	cells     []string // ô của kiểm tra mục tiêu; mỗi ô là một nhóm bề mặt gắn AC
	planSlice string   // "có" | "không" | "" (không ghi mục Chiến lược thực thi)
	planGoal  string
	slicing   string // workflow.slicing ("" = không ghi, theo mặc định)
	goalCheck string // workflow.goal_check
	sliceMin  int    // workflow.slice_timeout_minutes (0 = để 30)
	cfgFull   *bool  // workflow.verify.bint_config_full (nil = không ghi)
	budget    float64
	featCost  float64
	recSleep  int
	feats     []string // hành động của lần gọi b-feat thứ 1, 2, ...; hết danh sách thì "all"
	fast      bool
}

type bdoBox struct {
	t       *testing.T
	o       bdoOpts
	root    string
	logDir  string
	docs    string
	rec     string
	recLog  string
	agent   string
	vc, ac  string
	id      string
	outs    []string
	results []bdoResult
}

type bdoResult struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"task_status"`
	Reason  string `json:"reason"`
	Choices []struct {
		Label   string `json:"label"`
		Command string `json:"command"`
	} `json:"choices"`
	raw string
}

type bdoSlice struct {
	N      int    `json:"n"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type bdoVerification struct {
	Command   string `json:"command"`
	Scope     string `json:"scope"`
	Ran       string `json:"ran"`
	Passed    bool   `json:"passed"`
	Reason    string `json:"reason"`
	Selection *struct {
		Scope    string   `json:"scope"`
		Changed  []string `json:"changed"`
		Widening []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"widening"`
	} `json:"selection"`
}

type bdoState struct {
	Status        string            `json:"status"`
	Slices        []bdoSlice        `json:"slices"`
	Verifications []bdoVerification `json:"verifications"`
}

type bdoShow struct {
	State bdoState `json:"state"`
}

func (s bdoState) active() *bdoSlice {
	for i := range s.Slices {
		if s.Slices[i].Status == task.SliceActive {
			return &s.Slices[i]
		}
	}
	return nil
}

func bdoGroup(c string) string {
	return fmt.Sprintf("NHÓM-%s ghi ô %s: `a/%s.txt` — AC-%s", strings.ToUpper(c), c, c, strings.TrimPrefix(c, "c"))
}

func bdoCriterion(c string) string { return fmt.Sprintf("a/%s.txt ghi ok-%s", c, c) }

func bdoStrategy(slice, goal string) string {
	if slice == "" && goal == "" {
		return ""
	}
	return "## Chiến lược thực thi\n" +
		"Chia lát: " + slice + " — mỗi ô đo riêng được bằng TestGoal nên tách thành lát độc lập\n" +
		"Kiểm tra mục tiêu độc lập (b-goal): " + goal + " — mục tiêu đo được từ ngoài bằng test hộp đen\n"
}

func (o bdoOpts) docs() map[string]string {
	var ac, size, surf, tasks, crit strings.Builder
	for i, c := range o.cells {
		fmt.Fprintf(&ac, "- [ ] AC-%d: %s\n", i+1, bdoCriterion(c))
		fmt.Fprintf(&size, "- `a/%s.txt`\n", c)
		fmt.Fprintf(&surf, "- %s\n", bdoGroup(c))
		fmt.Fprintf(&tasks, "- [ ] TSK-%d [AC-%d]: ghi a/%s.txt — xong khi: TestGoal/%s xanh\n", i+1, i+1, c, c)
		crit.WriteString(c + " ")
	}
	req := "# Yêu cầu\n## Mục tiêu và phạm vi\nGhi các ô của TestGoal trong thư mục a.\n" +
		"## Hiện trạng\n- `a/a.go`: 1 tệp, 10 byte; 0 ô xanh.\n" +
		"## Tiêu chí chấp nhận\n" + ac.String() +
		"## Ngoài phạm vi\nKhông có\n" +
		"## Kiểm tra mục tiêu\n`TestGoal` trong `" + bdoGoalFile + "` có " + strconv.Itoa(len(o.cells)) + " ô, đạt khi mọi ô xanh.\n" +
		"## Câu hỏi mở\n"
	plan := "# Kế hoạch\nGhi từng ô của TestGoal.\n" +
		"## Cỡ kế hoạch\nKết luận: lớn\n" + size.String() + "- `a/x2.txt`\n- `.bint/config.yml`\n" +
		"## Bề mặt bị ảnh hưởng\n" + surf.String() + "- Tài liệu hướng dẫn: ngoài phạm vi\n" +
		bdoStrategy(o.planSlice, o.planGoal) +
		"## Giả định thay người dùng\n- Giữ nguyên a/a.go.\n"
	// Kiểm tra mục tiêu thật (go test do bgraph dựng chạy nó): ô cK xanh khi a/cK.txt ghi ok-cK. Dòng CELLS để rec.sh (lệnh của phiếu) đo cùng
	// luật khi bgraph đòi chạy cả bộ.
	goal := "package goal\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"strings\"\n\t\"testing\"\n)\n\n// CELLS: " + strings.TrimSpace(crit.String()) +
		"\nfunc TestGoal(t *testing.T) {\n\tfor _, c := range strings.Fields(\"" + strings.TrimSpace(crit.String()) + "\") {\n" +
		"\t\tt.Run(c, func(t *testing.T) {\n\t\t\tb, _ := os.ReadFile(filepath.Join(\"..\", \"a\", c+\".txt\"))\n" +
		"\t\t\tif strings.TrimSpace(string(b)) != \"ok-\"+c {\n\t\t\t\tt.Fatal(\"chưa xanh\")\n\t\t\t}\n\t\t})\n\t}\n}\n"
	return map[string]string{"requirement.md": req, "plan.md": plan, "tasks.md": tasks.String(), "goal_test.go": goal,
		"feats": strings.Join(o.feats, "\n") + "\n"}
}

// newBDo dựng repo tạm, cấu hình, agent và lệnh đếm; chưa chạy gì.
func newBDo(t *testing.T, o bdoOpts) *bdoBox {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	if len(o.cells) == 0 {
		o.cells = bdoCellAll
	}
	b := &bdoBox{t: t, o: o}
	b.root = testutil.NewRepo(t, map[string]string{
		"go.mod":      "module demo\n\ngo 1.21\n",
		"a/a.go":      "package a\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n",
	})
	testutil.Git(t, b.root, "config", "user.name", "Rev")
	testutil.Git(t, b.root, "config", "user.email", "rev@example.com")
	if out, code := run(t, b.root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	gitCommitAll(t, b.root)

	aux := t.TempDir()
	b.logDir = filepath.Join(aux, "log")
	b.docs = filepath.Join(aux, "docs")
	for _, d := range []string{b.logDir, b.docs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range o.docs() {
		if err := os.WriteFile(filepath.Join(b.docs, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b.recLog = filepath.ToSlash(filepath.Join(aux, "rec.log"))
	b.rec = filepath.ToSlash(filepath.Join(aux, "rec.sh"))
	if err := os.WriteFile(b.rec, []byte(fmt.Sprintf(bdoRecScript, b.recLog, o.recSleep)), 0o755); err != nil {
		t.Fatal(err)
	}
	b.agent = writeScript(t, bdoAgentScript)
	b.vc = "sh " + b.rec + " full"
	b.ac = "sh " + b.rec + " affected {test_files}"
	b.writeLocal(o)

	t.Setenv("BINT", filepath.ToSlash(bintBin))
	t.Setenv("BDO_LOG", filepath.ToSlash(b.logDir))
	t.Setenv("BDO_DOCS", filepath.ToSlash(b.docs))
	t.Setenv("BDO_CELLS", strings.Join(o.cells, " "))
	t.Setenv("BDO_FEAT_COST", strconv.FormatFloat(o.featCost, 'f', -1, 64))
	t.Setenv("GOFLAGS", "-count=1") // kiểm tra mục tiêu đọc a/cK.txt: không dùng kết quả test đã cache
	return b
}

// writeLocal ghi .bint/local.yml (bị .gitignore loại, không vào diff của tác vụ).
func (b *bdoBox) writeLocal(o bdoOpts) {
	b.t.Helper()
	sliceMin := o.sliceMin
	if sliceMin == 0 {
		sliceMin = 30
	}
	if sliceMin < 0 {
		sliceMin = 0
	}
	var y strings.Builder
	fmt.Fprintf(&y, "eval:\n  test_command: %q\n", b.vc)
	// Các ca này kiểm luồng classic (lát mỏng, cổng); B′ là mặc định nên ghim variant.
	fmt.Fprintf(&y, "workflow:\n  isolation: shared\n  variant: classic\n  slice_timeout_minutes: %d\n", sliceMin)
	if o.slicing != "" {
		fmt.Fprintf(&y, "  slicing: %s\n", o.slicing)
	}
	if o.goalCheck != "" {
		fmt.Fprintf(&y, "  goal_check: %s\n", o.goalCheck)
	}
	fmt.Fprintf(&y, "  verify:\n    affected_command: %q\n", b.ac)
	if o.cfgFull != nil {
		fmt.Fprintf(&y, "    bint_config_full: %v\n", *o.cfgFull)
	}
	if o.budget > 0 {
		fmt.Fprintf(&y, "limits:\n  workflows:\n    b-do:\n      max_cost_usd: %v\n", o.budget)
	}
	if err := os.WriteFile(filepath.Join(b.root, ".bint", "local.yml"), []byte(y.String()), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *bdoBox) parse(out string) bdoResult {
	var r bdoResult
	_ = json.Unmarshal(jsonPart(out), &r)
	r.raw = out
	return r
}

// start chạy `bint run b-do` rồi resume như người qua cổng cỡ kế hoạch; trả kết quả cuối.
func (b *bdoBox) start() bdoResult {
	b.t.Helper()
	args := []string{"run", "b-do", "--json", "--agent-command", b.agent, "--verify-command", b.vc,
		"--allow", "a/**", "--allow", "goal/**", "--allow", ".bint/config.yml"}
	if b.o.fast {
		args = append(args, "--fast")
	} else {
		args = append(args, "--plan")
	}
	for _, c := range b.o.cells {
		args = append(args, "--criteria", bdoCriterion(c))
	}
	args = append(args, "Ghi các ô c1..cN của TestGoal trong thư mục a")
	out, _ := run(b.t, b.root, "", args...)
	r := b.parse(out)
	b.outs = append(b.outs, out)
	b.results = append(b.results, r)
	if r.TaskID == "" {
		b.t.Fatalf("bint run b-do không ra JSON có task_id:\n%s", out)
	}
	b.id = r.TaskID
	for i := 0; i < 2 && r.Status == task.StatusBlocked && strings.HasPrefix(r.Reason, "kế hoạch lớn"); i++ {
		r = b.resume()
	}
	return r
}

func (b *bdoBox) resume(extra ...string) bdoResult {
	b.t.Helper()
	args := append([]string{"run", "resume", b.id, "--json", "--agent-command", b.agent}, extra...)
	out, _ := run(b.t, b.root, "", args...)
	r := b.parse(out)
	b.outs = append(b.outs, out)
	b.results = append(b.results, r)
	return r
}

func (b *bdoBox) show() bdoState {
	b.t.Helper()
	out, code := run(b.t, b.root, "", "task", "show", b.id, "--json")
	var s bdoShow
	if code != 0 || json.Unmarshal(jsonPart(out), &s) != nil {
		b.t.Fatalf("task show: %d\n%s", code, out)
	}
	return s.State
}

func (b *bdoBox) calls() []string {
	data, _ := os.ReadFile(filepath.Join(b.logDir, "calls"))
	return strings.Fields(string(data))
}

func (b *bdoBox) count(step string) int {
	n := 0
	for _, c := range b.calls() {
		if c == step {
			n++
		}
	}
	return n
}

func (b *bdoBox) featN() int { return b.count("b-feat") }

// featState là trạng thái tác vụ agent thấy ở đầu lần b-feat thứ k (bint task show --json).
func (b *bdoBox) featState(k int) (bdoState, bool) {
	data, err := os.ReadFile(filepath.Join(b.logDir, fmt.Sprintf("feat-%d.json", k)))
	if err != nil {
		return bdoState{}, false
	}
	var s bdoShow
	if json.Unmarshal(jsonPart(string(data)), &s) != nil {
		return bdoState{}, false
	}
	return s.State, true
}

// featFiles là nội dung a/c1, a/c2, a/c3, a/x2 ở đầu lần b-feat thứ k ("<none>" = không có).
func (b *bdoBox) featFiles(k int) map[string]string {
	data, _ := os.ReadFile(filepath.Join(b.logDir, fmt.Sprintf("feat-%d.files", k)))
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	out := map[string]string{}
	for i, n := range []string{"c1", "c2", "c3", "x2"} {
		if i < len(lines) {
			out[n] = lines[i]
		}
	}
	return out
}

func (b *bdoBox) featPrompt(k int) string {
	data, _ := os.ReadFile(filepath.Join(b.logDir, fmt.Sprintf("feat-%d.prompt", k)))
	return string(data)
}

// sliceFeats đếm lần b-feat chạy như một lát (đầu lần đó tác vụ có lát active).
func (b *bdoBox) sliceFeats() int {
	n := 0
	for k := 1; k <= b.featN(); k++ {
		if s, ok := b.featState(k); ok && s.active() != nil {
			n++
		}
	}
	return n
}

func (b *bdoBox) read(rel string) string {
	data, err := os.ReadFile(filepath.Join(b.root, filepath.FromSlash(rel)))
	if err != nil {
		return "<none>"
	}
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
}

func (b *bdoBox) recLines() []string {
	data, _ := os.ReadFile(b.recLog)
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

func (b *bdoBox) fullRuns() int {
	n := 0
	for _, l := range b.recLines() {
		if l == "full" {
			n++
		}
	}
	return n
}

func (b *bdoBox) dump() string {
	return fmt.Sprintf("chuỗi bước %v\nlệnh đếm %q\nđầu ra cuối:\n%.3000s", b.calls(), b.recLines(), b.outs[len(b.outs)-1])
}

func (b *bdoBox) openSlices() []bdoSlice {
	var open []bdoSlice
	for _, s := range b.show().Slices {
		if s.Status == task.SlicePending || s.Status == task.SliceActive {
			open = append(open, s)
		}
	}
	return open
}

// checkNoSilentDone: tác vụ không bao giờ done khi còn lát pending/active.
func (b *bdoBox) checkNoSilentDone(r bdoResult) {
	b.t.Helper()
	if open := b.openSlices(); len(open) > 0 && (r.Status == task.StatusDone || b.show().Status == task.StatusDone) {
		b.t.Errorf("tác vụ done khi còn %d lát pending/active: %+v\n%s", len(open), open, b.dump())
	}
}

// checkOpenListed: tác vụ chưa done mà còn lát pending/active thì lý do chặn phải liệt kê từng lát.
func (b *bdoBox) checkOpenListed(r bdoResult) {
	b.t.Helper()
	if r.Status == task.StatusDone {
		return
	}
	for _, s := range b.openSlices() {
		if !strings.Contains(r.Reason, s.Text) && !regexp.MustCompile(fmt.Sprintf(`lát\s*%d\b`, s.N)).MatchString(r.Reason) {
			b.t.Errorf("tác vụ %s còn lát %d (%s) chưa chốt nhưng lý do chặn không liệt kê nó: %q", r.Status, s.N, s.Status, r.Reason)
		}
	}
}

func bdoHasSliceOptions(r bdoResult) []string {
	var text strings.Builder
	text.WriteString(r.Reason)
	for _, c := range r.Choices {
		text.WriteString("\n" + c.Label + " " + c.Command)
	}
	var miss []string
	for _, opt := range []string{"keep", "discard", "more"} {
		if !regexp.MustCompile(`--slice[ =]` + opt + `\b`).MatchString(text.String()) {
			miss = append(miss, "--slice "+opt)
		}
	}
	return miss
}

// ---------- cấu hình (qua YAML, không phụ thuộc tên trường Go) ----------

func bdoYAMLGet(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func bdoCfgMap(t *testing.T, c config.Config) map[string]any {
	t.Helper()
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// bdoDecodeValidate giải mã một đoạn YAML đè lên Default rồi Validate.
func bdoDecodeValidate(snippet string) (decErr, valErr error) {
	c := config.Default()
	if err := config.Decode(strings.NewReader(snippet), &c); err != nil {
		return err, nil
	}
	return nil, c.Validate()
}

func checkConfigKeys(t *testing.T) {
	def := bdoCfgMap(t, config.Default())
	for _, k := range [][]string{{"workflow", "slicing"}, {"workflow", "goal_check"}} {
		name := strings.Join(k, ".")
		if v, ok := bdoYAMLGet(def, k...); !ok || v != "auto" {
			t.Errorf("Default(): %s = %v (có khoá=%v), muốn auto", name, v, ok)
		}
		for _, val := range []string{"auto", "off", "always"} {
			snip := fmt.Sprintf("workflow:\n  %s: %s\n", k[1], val)
			if d, v := bdoDecodeValidate(snip); d != nil || v != nil {
				t.Errorf("%s: %s phải hợp lệ: decode=%v validate=%v", name, val, d, v)
			}
		}
		snip := fmt.Sprintf("workflow:\n  %s: sometimes\n", k[1])
		if d, v := bdoDecodeValidate(snip); d == nil && (v == nil || !strings.Contains(v.Error(), name)) {
			t.Errorf("%s: sometimes phải bị Validate từ chối và nêu tên khoá, được %v", name, v)
		}
	}
	if v, ok := bdoYAMLGet(def, "workflow", "verify", "bint_config_full"); !ok || v != true {
		t.Errorf("Default(): workflow.verify.bint_config_full = %v (có khoá=%v), muốn true", v, ok)
	}
	if d, v := bdoDecodeValidate("workflow:\n  verify:\n    bint_config_full: false\n"); d != nil || v != nil {
		t.Errorf("workflow.verify.bint_config_full: false phải hợp lệ: decode=%v validate=%v", d, v)
	}
	// Mẫu Render có đủ ba khoá và khớp mặc định.
	tpl := config.Render(config.InitParams{ProjectName: "p"})
	for _, key := range []string{"slicing:", "goal_check:", "bint_config_full:"} {
		if !strings.Contains(tpl, key) {
			t.Errorf("mẫu Render thiếu khoá %s", key)
		}
	}
	var rc config.Config
	if err := config.Decode(strings.NewReader(tpl), &rc); err != nil {
		t.Fatalf("giải mã mẫu Render: %v", err)
	}
	rm := bdoCfgMap(t, rc)
	for _, k := range [][]string{{"workflow", "slicing"}, {"workflow", "goal_check"}, {"workflow", "verify", "bint_config_full"}} {
		got, _ := bdoYAMLGet(rm, k...)
		want, _ := bdoYAMLGet(def, k...)
		if got != want {
			t.Errorf("mẫu Render: %s = %v, mặc định %v", strings.Join(k, "."), got, want)
		}
	}
	// .bint/config.yml của repo đặt bint_config_full: false.
	data, err := os.ReadFile(filepath.Join("..", "..", ".bint", "config.yml"))
	if err != nil {
		t.Fatalf("đọc .bint/config.yml của repo: %v", err)
	}
	var repo map[string]any
	if err := yaml.Unmarshal(data, &repo); err != nil {
		t.Fatalf("giải mã .bint/config.yml của repo: %v", err)
	}
	if v, ok := bdoYAMLGet(repo, "workflow", "verify", "bint_config_full"); !ok || v != false {
		t.Errorf(".bint/config.yml của repo: workflow.verify.bint_config_full = %v (có khoá=%v), muốn false", v, ok)
	}
}

// ---------- lint hợp đồng v2 ----------

const bdoStrategyValid = "## Chiến lược thực thi\n" +
	"Chia lát: không — một tệp, một bước là đủ\n" +
	"Kiểm tra mục tiêu độc lập (b-goal): có — mục tiêu đo được từ ngoài bằng TestGoalList\n"

func checkLintStrategy(t *testing.T) {
	root := newLintRepo(t)
	for rel, body := range map[string]string{
		"src/app.go":      "package src\n\nfunc List() []string { return nil }\n",
		"src/app_test.go": "package src\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, root)
	withStrategy := func(s string) goalDocs {
		d := goalValidDocs()
		d.plan += s
		return d
	}
	mt := goalNewTask(t, root, goalCritMeasured, goalCritGoal)

	t.Run("valid", func(t *testing.T) {
		goalWriteDocs(t, root, mt.ID, withStrategy(bdoStrategyValid))
		errs, _, ver, raw := goalLint(t, root, mt.ID)
		if ver < 2 {
			t.Errorf("tác vụ mới phải theo hợp đồng v2 trở lên, có %d", ver)
		}
		if slices.Contains(errs, "strategy_missing") || len(errs) != 0 {
			t.Errorf("plan có đủ mục Chiến lược thực thi không được có lỗi, có %v\n%s", errs, raw)
		}
	})
	t.Run("valid_bullets", func(t *testing.T) {
		goalWriteDocs(t, root, mt.ID, withStrategy("## Chiến lược thực thi\n- Chia lát: có — hai bề mặt độc lập\n- Kiểm tra mục tiêu độc lập (b-goal): không — sửa một dòng, test hiện có đủ\n"))
		errs, _, _, raw := goalLint(t, root, mt.ID)
		if slices.Contains(errs, "strategy_missing") {
			t.Errorf("dòng có gạch đầu dòng vẫn phải hợp lệ, có %v\n%s", errs, raw)
		}
	})
	faults := map[string]string{
		"missing_section":      "",
		"missing_slice_line":   "## Chiến lược thực thi\nKiểm tra mục tiêu độc lập (b-goal): có — đo từ ngoài\n",
		"missing_goal_line":    "## Chiến lược thực thi\nChia lát: không — một tệp\n",
		"slice_without_reason": "## Chiến lược thực thi\nChia lát: không\nKiểm tra mục tiêu độc lập (b-goal): có — đo từ ngoài\n",
		"goal_without_reason":  "## Chiến lược thực thi\nChia lát: không — một tệp\nKiểm tra mục tiêu độc lập (b-goal): có\n",
		"slice_bad_value":      "## Chiến lược thực thi\nChia lát: tuỳ — chưa rõ\nKiểm tra mục tiêu độc lập (b-goal): có — đo từ ngoài\n",
	}
	for name, s := range faults {
		t.Run(name, func(t *testing.T) {
			goalWriteDocs(t, root, mt.ID, withStrategy(s))
			errs, _, _, raw := goalLint(t, root, mt.ID)
			if !slices.Contains(errs, "strategy_missing") {
				t.Errorf("thiếu lỗi strategy_missing (errors %v)\n%s", errs, raw)
			}
		})
	}
	t.Run("v1_warning_only", func(t *testing.T) {
		tk := goalNewTask(t, root, goalCritMeasured, goalCritGoal)
		store := task.Store{Dir: filepath.Dir(tk.Dir)}
		if _, err := store.Update(tk.ID, time.Now(), func(x *task.Task) error {
			x.State.PlanningContractVersion = 1
			return nil
		}); err != nil {
			t.Fatalf("hạ hợp đồng về v1: %v", err)
		}
		goalWriteDocs(t, root, tk.ID, goalValidDocs())
		errs, warns, ver, raw := goalLint(t, root, tk.ID)
		if ver != 1 {
			t.Fatalf("contract_version=%d, muốn 1\n%s", ver, raw)
		}
		if slices.Contains(errs, "strategy_missing") {
			t.Errorf("v1: strategy_missing không được là lỗi\n%s", raw)
		}
		if !slices.Contains(warns, "strategy_missing") {
			t.Errorf("v1: thiếu cảnh báo strategy_missing (warnings %v)\n%s", warns, raw)
		}
	})
}

// ---------- skill ----------

// Không gắn với Go: "gói", "package" hay "phần" đã sửa đều được (Bint chạy cho dự án mọi ngôn ngữ).
var bdoPkgTestRule = regexp.MustCompile(`(?i)(test|kiểm thử)[^\n]{0,80}(gói|package|phần)[^\n]{0,40}(đã sửa|vừa sửa|bị sửa|đã đổi|mình sửa|có thay đổi|bị ảnh hưởng)`)

func checkSkills(t *testing.T) {
	repo := filepath.Join("..", "..")
	var lock skills.Lock
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(skills.LockRel)))
	if err != nil {
		t.Fatalf("đọc core.lock: %v", err)
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		t.Fatalf("giải mã core.lock: %v", err)
	}
	for _, name := range []string{"b-feat", "b-req", "b-do"} {
		s, ok := skills.Get(name)
		if !ok {
			t.Errorf("skills.Get(%q) không có", name)
			continue
		}
		for _, tg := range []skills.Target{skills.ClaudeTarget, skills.AgentsTarget} {
			p := filepath.Join(repo, filepath.FromSlash(tg.Dir), name, "SKILL.md")
			got, err := os.ReadFile(p)
			if err != nil {
				t.Errorf("%s: %v", p, err)
				continue
			}
			if want := skills.ForTarget(s, tg).Content; !bytes.Equal(got, want) {
				t.Errorf("%s khác từng byte bản xuất bản từ internal/skills/core (%d byte so với %d); chạy bint sync", p, len(got), len(want))
			}
		}
		e, ok := lock.Skills[name]
		if !ok {
			t.Errorf("core.lock thiếu skill %s", name)
			continue
		}
		if e.Overlay == "" {
			if e.SHA256 != s.Hash() {
				t.Errorf("core.lock: %s sha256 %s, băm skill core %s", name, e.SHA256, s.Hash())
			}
			if h := skills.ForTarget(s, skills.AgentsTarget).Hash(); e.Variants["agents"] != h {
				t.Errorf("core.lock: %s variants.agents %s, băm bản .agents %s", name, e.Variants["agents"], h)
			}
		}
	}
	feat, _ := skills.Get("b-feat")
	if !bdoPkgTestRule.Match(feat.Content) {
		t.Errorf("skill b-feat chưa dặn chỉ chạy test của gói đã sửa (không khớp %s)", bdoPkgTestRule)
	}
	req, _ := skills.Get("b-req")
	if !bdoPkgTestRule.Match(req.Content) {
		t.Errorf("skill b-req chưa dặn tasks.md chỉ đòi test của gói đã sửa (không khớp %s)", bdoPkgTestRule)
	}
	for _, want := range []string{"## Chiến lược thực thi", "Chia lát:", "Kiểm tra mục tiêu độc lập (b-goal):"} {
		if !strings.Contains(string(req.Content), want) {
			t.Errorf("skill b-req chưa hướng dẫn ghi %q trong plan.md", want)
		}
	}
	if !strings.Contains(string(req.Content), "không đòi chạy toàn bộ bộ test") {
		t.Errorf("skill b-req phải nói rõ tasks.md không đòi chạy toàn bộ bộ test")
	}
}

// ---------- kiểm tra mục tiêu ----------

func TestGoalBDoLeanSlicesAndGates(t *testing.T) {
	t.Run("config_keys", checkConfigKeys)
	t.Run("lint_strategy", checkLintStrategy)
	t.Run("skills_package_tests_synced", checkSkills)

	// AC-2: khoá workflow.slicing / workflow.goal_check và plan.md quyết định b-goal và lát.
	t.Run("gating", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			o         bdoOpts
			wantGoal  bool
			wantSlice bool
		}{
			{"plan_no_auto", bdoOpts{planSlice: "không", planGoal: "không"}, false, false},
			{"plan_yes_off", bdoOpts{planSlice: "có", planGoal: "có", slicing: "off", goalCheck: "off"}, false, false},
			{"plan_no_always", bdoOpts{planSlice: "không", planGoal: "không", slicing: "always", goalCheck: "always",
				feats: []string{"w:c1", "w:c2", "w:c3"}}, true, true},
			{"fast", bdoOpts{planSlice: "có", planGoal: "có", fast: true}, false, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				b := newBDo(t, tc.o)
				r := b.start()
				b.checkNoSilentDone(r)
				if b.featN() == 0 {
					t.Fatalf("không chạy tới b-feat (không đo được b-goal/lát): %s %q\n%s", r.Status, r.Reason, b.dump())
				}
				goals, sl := b.count("b-goal"), len(b.show().Slices)
				if tc.wantGoal && goals != 1 {
					t.Errorf("muốn đúng 1 lần gọi agent b-goal, được %d\n%s", goals, b.dump())
				}
				if !tc.wantGoal && goals != 0 {
					t.Errorf("muốn 0 lần gọi agent b-goal, được %d\n%s", goals, b.dump())
				}
				if tc.wantSlice && (sl < 2 || b.sliceFeats() < 2) {
					t.Errorf("muốn chạy theo lát như cũ (≥ 2 lát): State.Slices %d, b-feat theo lát %d\n%s", sl, b.sliceFeats(), b.dump())
				}
				if !tc.wantSlice && (sl != 0 || b.sliceFeats() != 0) {
					t.Errorf("muốn 0 lát: State.Slices %d, b-feat theo lát %d\n%s", sl, b.sliceFeats(), b.dump())
				}
			})
		}
	})

	// AC-6 (+ AC-2 auto/có): tác vụ 2 lát: không lần nào chạy cả bộ (verify.command); mọi lần kiểm chứng là phần chọn của bgraph, cổng done nhận nó.
	t.Run("two_slices_no_full_run", func(t *testing.T) {
		b := newBDo(t, bdoOpts{cells: []string{"c1", "c2"}, planSlice: "có", planGoal: "có", feats: []string{"w:c1", "w:c2"}})
		r := b.start()
		b.checkNoSilentDone(r)
		if got := b.count("b-goal"); got != 1 {
			t.Errorf("plan ghi có, goal_check auto: muốn 1 lần b-goal, được %d", got)
		}
		st := b.show()
		if len(st.Slices) != 2 || b.sliceFeats() != 2 {
			t.Fatalf("plan ghi có, slicing auto: muốn 2 lát, State.Slices %d, b-feat theo lát %d\n%s", len(st.Slices), b.sliceFeats(), b.dump())
		}
		if r.Status != task.StatusDone {
			t.Errorf("hai lát đạt: tác vụ phải done, được %s %q\n%s", r.Status, r.Reason, b.dump())
		}
		if n := b.fullRuns(); n != 0 {
			t.Errorf("verify.command (cả bộ) chạy %d lần, muốn 0\n%s", n, b.dump())
		}
		for i, v := range st.Verifications {
			if v.Selection == nil || v.Scope == "" || v.Scope == "full" {
				t.Errorf("lần kiểm chứng %d/%d không phải phần chọn của bgraph: scope %q selection %+v", i+1, len(st.Verifications), v.Scope, v.Selection)
			}
		}
		for k := 1; k <= 2; k++ {
			p := b.featPrompt(k)
			if !strings.Contains(p, bdoGroup(fmt.Sprintf("c%d", k))) {
				t.Errorf("đề bài lát %d thiếu nguyên văn nhóm của lát:\n%.2000s", k, p)
			}
			if !bdoPkgTestRule.MatchString(p) {
				t.Errorf("đề bài lát %d không dặn chỉ chạy test của gói đã sửa", k)
			}
		}
	})

	// AC-3: lát không đạt → giữ cây, blocked với 3 lựa chọn; mỗi lựa chọn chạy đúng hành vi.
	t.Run("failed_slice", func(t *testing.T) {
		for _, opt := range []string{"keep", "discard", "more"} {
			t.Run(opt, func(t *testing.T) {
				b := newBDo(t, bdoOpts{planSlice: "có", planGoal: "có", feats: []string{"w:c1", "bad:c2"}})
				r := b.start()
				b.checkNoSilentDone(r)
				if b.featN() < 2 {
					t.Fatalf("lát 2 không chạy: %s %q\n%s", r.Status, r.Reason, b.dump())
				}
				if b.featN() != 2 {
					t.Errorf("lát 2 không đạt: Bint phải dừng hỏi người, không tự chạy b-feat tiếp (đã chạy %d b-feat)\n%s", b.featN(), b.dump())
				}
				if r.Status != task.StatusBlocked {
					t.Errorf("lát 2 không đạt: tác vụ phải blocked, được %s %q", r.Status, r.Reason)
				}
				if miss := bdoHasSliceOptions(r); len(miss) > 0 {
					t.Errorf("lý do chặn/lựa chọn thiếu %v: %q %+v", miss, r.Reason, r.Choices)
				}
				reverted := 0
				for rel, want := range map[string]string{"a/c2.txt": "sai", "a/x2.txt": "x", "a/c1.txt": "ok-c1"} {
					if got := b.read(rel); got != want {
						reverted++
						t.Errorf("%s bị đổi sau lát hỏng: %q, muốn giữ %q", rel, got, want)
					}
				}
				if reverted > 0 {
					t.Errorf("%d tệp bị Bint đưa về điểm xanh, muốn 0", reverted)
				}
				r = b.resume("--slice", opt)
				b.checkNoSilentDone(r)
				if b.featN() < 3 {
					t.Fatalf("resume --slice %s không chạy b-feat nào: %s %q\n%s", opt, r.Status, r.Reason, b.dump())
				}
				fs, _ := b.featState(3)
				files := b.featFiles(3)
				act := fs.active()
				switch opt {
				case "keep":
					if files["c2"] != "sai" || files["x2"] != "x" {
						t.Errorf("keep: lát kế phải bắt đầu từ cây của lát 2 (c2=sai, x2=x), được %v", files)
					}
					if act == nil || act.N != 3 {
						t.Errorf("keep: b-feat kế phải là lát 3, lát active %+v", act)
					}
					if len(fs.Slices) >= 2 && fs.Slices[1].Status != task.SliceAccepted {
						t.Errorf("keep: lát 2 phải được chốt (accepted), được %q", fs.Slices[1].Status)
					}
				case "discard":
					if files["c2"] != "<none>" || files["x2"] != "<none>" || files["c1"] != "ok-c1" {
						t.Errorf("discard: cây phải về điểm xanh sau lát 1 (c1=ok-c1, không c2, không x2), được %v", files)
					}
					if act == nil || act.N != 3 {
						t.Errorf("discard: b-feat kế phải là lát 3, lát active %+v", act)
					}
					if len(fs.Slices) >= 2 && fs.Slices[1].Status != task.SliceDiscarded {
						t.Errorf("discard: lát 2 phải discarded, được %q", fs.Slices[1].Status)
					}
				case "more":
					if files["c2"] != "sai" || files["x2"] != "x" {
						t.Errorf("more: lát 2 chạy lại phải bắt đầu từ cây hiện tại (c2=sai, x2=x), được %v", files)
					}
					if act == nil || act.N != 2 {
						t.Errorf("more: b-feat kế phải chạy lại lát 2, lát active %+v", act)
					}
				}
			})
		}
	})

	// AC-3: agent lỗi trong lát → giữ cây, blocked với 3 lựa chọn.
	t.Run("agent_error_slice", func(t *testing.T) {
		b := newBDo(t, bdoOpts{planSlice: "có", planGoal: "có", feats: []string{"w:c1", "err:c2"}})
		r := b.start()
		b.checkNoSilentDone(r)
		if b.featN() != 2 {
			t.Errorf("agent lỗi ở lát 2: muốn dừng sau 2 b-feat, đã chạy %d\n%s", b.featN(), b.dump())
		}
		if r.Status != task.StatusBlocked {
			t.Errorf("agent lỗi ở lát 2: tác vụ phải blocked, được %s %q", r.Status, r.Reason)
		}
		if miss := bdoHasSliceOptions(r); len(miss) > 0 {
			t.Errorf("lý do chặn/lựa chọn thiếu %v: %q %+v", miss, r.Reason, r.Choices)
		}
		for rel, want := range map[string]string{"a/c2.txt": "dở", "a/x2.txt": "x"} {
			if got := b.read(rel); got != want {
				t.Errorf("%s bị đưa về điểm xanh sau agent lỗi: %q, muốn giữ %q", rel, got, want)
			}
		}
	})

	// AC-3/AC-4: trần lát chỉ tính thời gian agent; lát quá trần giữ cây và hỏi người.
	t.Run("slice_cap_agent_time_only", func(t *testing.T) {
		b := newBDo(t, bdoOpts{cells: []string{"c1", "c2"}, planSlice: "có", planGoal: "có", sliceMin: 1, recSleep: 20,
			feats: []string{"slow:c1", "hang:c2"}})
		r := b.start()
		b.checkNoSilentDone(r)
		st := b.show()
		if len(st.Slices) != 2 {
			t.Fatalf("muốn 2 lát, State.Slices %+v\n%s", st.Slices, b.dump())
		}
		if st.Slices[0].Status != task.SliceAccepted {
			t.Errorf("lát 1: agent 25 giây < trần 1 phút (kiểm chứng + cổng mục tiêu ~40 giây không tính) phải accepted, được %q %q",
				st.Slices[0].Status, st.Slices[0].Reason)
		}
		if b.featN() != 2 {
			t.Errorf("lát 2 quá trần: muốn dừng sau 2 b-feat, đã chạy %d", b.featN())
		}
		if r.Status != task.StatusBlocked {
			t.Errorf("lát 2 quá trần: tác vụ phải blocked, được %s %q", r.Status, r.Reason)
		}
		if miss := bdoHasSliceOptions(r); len(miss) > 0 {
			t.Errorf("lý do chặn/lựa chọn thiếu %v: %q %+v", miss, r.Reason, r.Choices)
		}
		if got := b.read("a/x2.txt"); got != "x" {
			t.Errorf("a/x2.txt của lát quá trần bị đưa về điểm xanh: %q, muốn giữ", got)
		}
		if got := b.read("a/c1.txt"); got != "ok-c1" {
			t.Errorf("a/c1.txt (lát 1 đã chốt) phải còn: %q", got)
		}
	})

	// AC-4: tắt lát khi tác vụ đang chia lát → resume chạy 1 b-feat thường cho phần còn lại.
	t.Run("disable_slicing_midway", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			off  func(*bdoOpts)
		}{
			{"slice_timeout_0", func(o *bdoOpts) { o.sliceMin = -1 }},
			{"slicing_off", func(o *bdoOpts) { o.slicing = "off" }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				// Lát 1 ghi ô c1 rồi hỏi người (bint task error --question): tác vụ dừng giữa chừng khi đang chia lát.
				o := bdoOpts{planSlice: "có", planGoal: "có", feats: []string{"ask:c1"}}
				b := newBDo(t, o)
				r := b.start()
				b.checkNoSilentDone(r)
				st := b.show()
				if b.featN() != 1 || len(st.Slices) != 3 || r.Status != task.StatusBlocked {
					t.Fatalf("điều kiện đầu: muốn dừng chờ người trong lát 1/3 (b-feat %d, lát %d, %s %q)\n%s",
						b.featN(), len(st.Slices), r.Status, r.Reason, b.dump())
				}
				tc.off(&o)
				b.writeLocal(o)
				r = b.resume("--answer", "Làm nốt mọi ô còn lại trong một lượt.")
				b.checkNoSilentDone(r)
				b.checkOpenListed(r)
				if got := b.featN() - 1; got != 1 {
					t.Errorf("sau khi tắt lát, resume phải chạy đúng 1 b-feat, đã chạy %d\n%s", got, b.dump())
				}
				if fs, ok := b.featState(2); ok && fs.active() != nil {
					t.Errorf("b-feat sau khi tắt lát vẫn chạy như lát %d", fs.active().N)
				}
				if b.featN() >= 2 {
					if p := b.featPrompt(2); regexp.MustCompile(`lát\s*\d+\s*/\s*3`).MatchString(p) && strings.Contains(p, "CHỈ LÀM") {
						t.Errorf("đề bài b-feat sau khi tắt lát vẫn là đề bài lát:\n%.1500s", p)
					}
				}
			})
		}
	})

	// AC-5: bint_config_full: false → .bint/** bị bỏ khỏi tập thay đổi (không vào Changed, không gây nới).
	t.Run("bint_config_full_false", func(t *testing.T) {
		f := false
		b := newBDo(t, bdoOpts{cells: []string{"c1", "c2"}, planSlice: "không", planGoal: "không", cfgFull: &f, feats: []string{"cfg"}})
		r := b.start()
		b.checkNoSilentDone(r)
		if b.featN() == 0 {
			t.Fatalf("không chạy tới b-feat: %s %q\n%s", r.Status, r.Reason, b.dump())
		}
		st := b.show()
		scoped, sawCfg := 0, false
		for i, v := range st.Verifications {
			if v.Selection == nil {
				continue
			}
			scoped++
			if slices.Contains(v.Selection.Changed, ".bint/config.yml") {
				sawCfg = true
			}
			for _, w := range v.Selection.Widening {
				if w.Reason == "bint_config" {
					t.Errorf("lần kiểm chứng %d nới %s vì bint_config (%s) dù bint_config_full: false", i+1, v.Scope, w.Path)
				}
			}
		}
		if scoped == 0 {
			t.Errorf("không có lần kiểm chứng nào có Selection: %+v\n%s", st.Verifications, b.dump())
		} else if sawCfg {
			t.Errorf("bint_config_full: false thì .bint/config.yml phải bị bỏ khỏi tập thay đổi: %+v", st.Verifications)
		}
	})
}
