package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/gitrepo"
	"bint/internal/task"
)

// Kiểm tra mục tiêu `TestSliceRatchet` của phiếu 01M7PD9T5R587EKT2Z82GJVZAJ (GĐ2: b-do chạy theo lát mỏng và chốt một chiều).
//
// Hộp đen: chỉ quan sát chuỗi bước agent, đề bài/timeout giao cho agent, lý do dừng, State.Slices (đọc qua JSON, không phụ thuộc kiểu Go),
// ref git của tác vụ, nội dung tệp trong repo tạm, Result (JSON `slices`, LastGreen) và RestoreGreen.
//
// Repo tạm: old.txt (test cũ `TestOld` đỏ khi bị sửa), verify.sh (lệnh kiểm chứng: chạy kiểm tra mục tiêu + TestOld, in dòng kiểu go test).
// b-goal giả ghi goal/check.sh: `TestGoal` có ba ô c1..c3, ô ck đỏ tới khi ck.txt ghi "ok-ck". Cổng mục tiêu chạy `sh goal/check.sh`.

const (
	slGroup1 = "SL1 lõi tối thiểu: `c1.txt` — AC-1"
	slGroup2 = "SL2 mở rộng: `c2.txt`, `old.txt`, `l2-extra.txt` — AC-2"
	slGroup3 = "SL3 hoàn thiện: `c3.txt` — AC-3"
	slOut    = "Ngoài phạm vi: SLOUT `docs/` không đổi"
	slOneAC  = "SL1 toàn bộ: `c1.txt`, `c2.txt`, `c3.txt` — AC-1, AC-2, AC-3"

	slReq = "Ba ô c1, c2, c3 của TestGoal phải xanh"

	slOldBody  = "old\n"
	slOldTamp  = "old-sửa-bởi-L2\n"
	slOutBody  = "tệp ngoài tác vụ\n"
	slGoalFile = "goal/check.sh"
)

// slGoalScript là kiểm tra mục tiêu đóng băng: in như `go test -v` (ô con thụt lề, dòng cha `--- FAIL: TestGoal` không phải lá).
const slGoalScript = `# GOAL-CHECK TestGoal
fail=""
for c in c1 c2 c3; do
  if [ "$(cat $c.txt 2>/dev/null)" = "ok-$c" ]; then :; else fail="$fail $c"; fi
done
if [ -z "$fail" ]; then
  echo "--- PASS: TestGoal (0.00s)"
  echo PASS
  exit 0
fi
echo "--- FAIL: TestGoal (0.00s)"
for c in $fail; do echo "    --- FAIL: TestGoal/$c (0.00s)"; done
echo FAIL
exit 1
`

// slVerifyScript là lệnh kiểm chứng của phiếu: kiểm tra mục tiêu (nếu đã có) + test cũ TestOld. Khi L2 để lại l2-extra.txt, nó tạo
// outside.txt: tệp đổi NGOÀI bước b-feat (không thuộc tác vụ), quay về điểm xanh phải giữ nguyên.
const slVerifyScript = `[ -f l2-extra.txt ] && printf 'tệp ngoài tác vụ\n' > outside.txt
rc=0
if [ -f goal/check.sh ]; then sh goal/check.sh || rc=1; fi
if [ "$(cat old.txt)" = "old" ]; then echo "--- PASS: TestOld (0.00s)"; else echo "--- FAIL: TestOld (0.00s)"; rc=1; fi
[ $rc -eq 0 ] && echo ok
exit $rc
`

// slOpts chọn biến thể của kịch bản.
type slOpts struct {
	small        bool     // (a) ## Cỡ kế hoạch nhỏ
	oneAC        bool     // (c) chỉ một nhóm bề mặt gắn AC
	sliceTimeout *int     // nil = để mặc định của config
	timeoutMin   int      // workflow.timeout_minutes
	l1NoProgress bool     // (f) L1 không làm giảm ô đỏ
	l2TimedOut   bool     // (d) agent L2 trả TimedOut
	budget       *float64 // (e) limits.workflows.b-do.max_cost_usd (chỉ b-feat tốn 1 USD)
}

// slSnap là quan sát tại một thời điểm giữa chừng (đầu một bước agent).
type slSnap struct {
	taken      bool
	tree       string // ảnh chụp cây hiện tại
	greenTree  string // tree của refs/bint/tasks/<id>/slice-green ("" nếu không có)
	discOld    string // old.txt trong slice-2-discarded
	discExtra  string // l2-extra.txt trong slice-2-discarded
	old, extra string // nội dung tệp hiện tại (h.read: "<không có>" nếu không có)
	outside    string
	c1         string
	state      []map[string]any
	stateOK    bool
}

type slRun struct {
	h       *harness
	id      string
	first   *Result // lượt đầu (dừng ở cổng cỡ kế hoạch, hoặc chạy hết nếu kế hoạch nhỏ)
	runs    []*Result
	errs    []string
	slices  int // số lần b-feat giả chạy theo lát
	cur     int // lát của lần b-feat vừa chạy (0 = không theo lát)
	reviews int
	atL2    slSnap // đầu lát 2
	atRev   slSnap // đầu b-review đầu tiên
}

var slLatRe = regexp.MustCompile(`lát\s*\d+\s*/\s*\d+`)

// sliceTurn: lần b-feat này là một lát — Runner đã ghi State.Slices, hoặc đề bài nêu "lát k/N" kèm nguyên văn một dòng nhóm.
func (s *slRun) sliceTurn(prompt string) bool {
	if tk, err := s.h.tasks.Load(s.id); err == nil {
		if items, _ := stateSlices(tk); len(items) > 0 {
			return true
		}
	}
	if !slLatRe.MatchString(prompt) {
		return false
	}
	for _, g := range []string{slGroup1, slGroup2, slGroup3, slOneAC} {
		if strings.Contains(prompt, g) {
			return true
		}
	}
	return false
}

func slSliceRef(id, name string) string { return "refs/bint/tasks/" + id + "/" + name }

// setSliceTimeout đặt workflow.slice_timeout_minutes qua thẻ yaml (không phụ thuộc tên trường Go).
func setSliceTimeout(cfg *config.Config, v int) error {
	w := reflect.ValueOf(&cfg.Workflow).Elem()
	for i := 0; i < w.NumField(); i++ {
		name, _, _ := strings.Cut(w.Type().Field(i).Tag.Get("yaml"), ",")
		if name != "slice_timeout_minutes" {
			continue
		}
		f := w.Field(i)
		switch f.Kind() {
		case reflect.Int, reflect.Int64, reflect.Int32:
			f.SetInt(int64(v))
			return nil
		}
		return fmt.Errorf("workflow.slice_timeout_minutes có kiểu %s, muốn số nguyên", f.Type())
	}
	return fmt.Errorf("config.Workflow thiếu khoá yaml `slice_timeout_minutes`")
}

func getSliceTimeout(cfg config.Config) (int, bool) {
	w := reflect.ValueOf(cfg.Workflow)
	for i := 0; i < w.NumField(); i++ {
		if name, _, _ := strings.Cut(w.Type().Field(i).Tag.Get("yaml"), ","); name == "slice_timeout_minutes" && w.Field(i).CanInt() {
			return int(w.Field(i).Int()), true
		}
	}
	return 0, false
}

// slDocs dựng requirement/plan/tasks; plan có 3 nhóm gắn AC + một dòng Ngoài phạm vi (hoặc 1 nhóm AC khi oneAC).
func slDocs(o slOpts) map[string]string {
	req := "# Yêu cầu\n## Hiện trạng\n- app.txt: 1 tệp\n## Mục tiêu và phạm vi\nBa ô c1..c3\n## Tiêu chí chấp nhận\n" +
		"- [ ] AC-1: c1.txt ghi ok-c1\n- [ ] AC-2: c2.txt ghi ok-c2\n- [ ] AC-3: c3.txt ghi ok-c3\n" +
		"## Ngoài phạm vi\nKhông có\n## Kiểm tra mục tiêu\n`TestGoal` có ba ô c1..c3, đạt khi cả ba xanh\n## Câu hỏi mở\n"
	size := "Kết luận: lớn\n"
	if o.small {
		size = "Kết luận: nhỏ\n"
	}
	size += "- `c1.txt`\n- `c2.txt`\n- `c3.txt`\n"
	if !o.small {
		size += "- `old.txt`\n- `l2-extra.txt`\n"
	}
	surf := "- " + slGroup1 + "\n- " + slGroup2 + "\n- " + slGroup3 + "\n- " + slOut + "\n"
	if o.oneAC {
		surf = "- " + slOneAC + "\n- " + slOut + "\n"
	}
	plan := "# Kế hoạch\nBa lát cho c1..c3; lát 1 là bản nhỏ nhất đạt mục tiêu.\n## Cỡ kế hoạch\n" + size + "## Bề mặt bị ảnh hưởng\n" + surf + "## Giả định thay người dùng\nKhông có\n"
	tasks := "- [ ] TSK-1 [AC-1]: ghi c1.txt — xong khi: c1.txt ghi ok-c1\n" +
		"- [ ] TSK-2 [AC-2]: ghi c2.txt — xong khi: c2.txt ghi ok-c2\n" +
		"- [ ] TSK-3 [AC-3]: ghi c3.txt — xong khi: c3.txt ghi ok-c3\n"
	return map[string]string{"requirement": req, "plan": plan, "tasks": tasks}
}

func slWrite(root, rel, body string) error {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(body), 0o644)
}

// slFeatPrompt trả đề bài của lần b-feat đang chạy (fakeAgent ghi h.prompts trước khi gọi behave).
func (h *harness) slFeatPrompt() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.prompts[StepFeat]
}

func (s *slRun) snap(ctx context.Context) slSnap {
	h := s.h
	out := slSnap{taken: true, old: h.read("old.txt"), extra: h.read("l2-extra.txt"), outside: h.read("outside.txt"), c1: h.read("c1.txt")}
	if tr, err := WorktreeSnapshot(ctx, h.root); err == nil {
		out.tree = tr
	}
	if g, err := gitrepo.Run(ctx, h.root, "rev-parse", "--verify", "-q", slSliceRef(s.id, "slice-green")+"^{tree}"); err == nil {
		out.greenTree = strings.TrimSpace(g)
	}
	d := slSliceRef(s.id, "slice-2-discarded")
	out.discOld, out.discExtra = "<không có>", "<không có>"
	if b, err := gitrepo.Run(ctx, h.root, "show", d+":old.txt"); err == nil {
		out.discOld = b
	}
	if b, err := gitrepo.Run(ctx, h.root, "show", d+":l2-extra.txt"); err == nil {
		out.discExtra = b
	}
	if tk, err := h.tasks.Load(s.id); err == nil {
		out.state, out.stateOK = stateSlices(tk)
	}
	return out
}

// slBox dựng harness và chạy b-do: lượt đầu → (kế hoạch lớn thì) resume như người → tới khi dừng/xong.
func slBox(t *testing.T, o slOpts) *slRun {
	t.Helper()
	ctx := context.Background()
	h := newHarness(t)
	s := &slRun{h: h}
	h.write("old.txt", slOldBody)
	h.write("verify.sh", slVerifyScript)
	h.commitAll(h.root, "test cũ và lệnh kiểm chứng")
	vc := "sh verify.sh"
	h.cfg.Eval.TestCommand = &vc
	h.cfg.Workflow.Verify.AffectedCommand = "sh {test_files}"
	h.cfg.Workflow.TimeoutMinutes = o.timeoutMin
	if o.sliceTimeout != nil {
		if err := setSliceTimeout(&h.cfg, *o.sliceTimeout); err != nil {
			s.errs = append(s.errs, err.Error())
			return s
		}
	}
	if o.budget != nil {
		h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxCostUSD: o.budget}}
		h.costFor = func(step string) *float64 {
			if step == StepFeat {
				return ptr(1.0)
			}
			return ptr(0.0)
		}
	}
	h.goal = func(_ context.Context, e *Env, _ string) error { return slWrite(e.Root, slGoalFile, slGoalScript) }
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepReq:
			s.id = id
			if _, _, err := e.SetTicket(id, TicketEdit{ClearCriteria: true, Criteria: []string{"c1.txt ghi ok-c1", "c2.txt ghi ok-c2", "c3.txt ghi ok-c3"}}); err != nil {
				return err
			}
			for n, body := range slDocs(o) {
				if _, _, err := e.WriteArtifact(id, n, body); err != nil {
					return err
				}
			}
			return nil
		case StepReview:
			if s.reviews == 0 {
				s.atRev = s.snap(ctx)
			}
			s.reviews++
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		case StepFeat:
			s.cur = 0
			if s.reviews > 0 || !s.sliceTurn(h.slFeatPrompt()) {
				// Một b-feat làm cả việc (kế hoạch không chia lát), hoặc vòng sửa sau review.
				for _, c := range []string{"c1", "c2", "c3"} {
					if err := slWrite(e.Root, c+".txt", "ok-"+c+"\n"); err != nil {
						return err
					}
				}
				return nil
			}
			s.slices++
			s.cur = s.slices
			switch s.slices {
			case 1:
				if o.l1NoProgress {
					return slWrite(e.Root, "c1.txt", "sai\n")
				}
				return slWrite(e.Root, "c1.txt", "ok-c1\n")
			case 2:
				s.atL2 = s.snap(ctx)
				for rel, body := range map[string]string{"c2.txt": "ok-c2\n", "old.txt": slOldTamp, "l2-extra.txt": "phụ của L2\n"} {
					if err := slWrite(e.Root, rel, body); err != nil {
						return err
					}
				}
				return nil
			case 3:
				return slWrite(e.Root, "c3.txt", "ok-c3\n")
			}
			return fmt.Errorf("b-feat theo lát lần thứ %d: chỉ có 3 lát", s.slices)
		}
		return nil
	}
	if o.l2TimedOut {
		h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
			if step == StepFeat && s.cur == 2 {
				res.TimedOut, res.ExitCode, res.Error = true, -1, "quá giờ"
			}
		}
	}
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: slReq, Plan: ptr(true)})
	if err != nil {
		s.errs = append(s.errs, "lượt đầu: "+err.Error())
		return s
	}
	s.first, s.id = res, res.TaskID
	s.runs = append(s.runs, res)
	if res.Status == task.StatusBlocked && strings.HasPrefix(res.Reason, "kế hoạch lớn") {
		res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: s.id, Resume: true})
		if err != nil {
			s.errs = append(s.errs, "resume sau cổng cỡ kế hoạch: "+err.Error())
			return s
		}
		s.runs = append(s.runs, res)
	}
	return s
}

func (s *slRun) last() *Result { return s.runs[len(s.runs)-1] }

func (s *slRun) need(t *testing.T) {
	t.Helper()
	if len(s.errs) > 0 || s.first == nil {
		t.Fatalf("không dựng/chạy được kịch bản: %v", s.errs)
	}
}

// featReqs trả các yêu cầu b-feat (theo thứ tự) và chỉ số b-review đầu tiên trong h.calls (-1 nếu không có).
func (s *slRun) featReqs() (feats []eval.AgentRequest, featIdx []int, firstReview int) {
	firstReview = -1
	for i, c := range s.h.calls {
		switch c {
		case StepFeat:
			feats = append(feats, s.h.reqs[i])
			featIdx = append(featIdx, i)
		case StepReview:
			if firstReview < 0 {
				firstReview = i
			}
		}
	}
	return
}

// stateSlices đọc State.Slices qua JSON (nil, false khi State chưa có trường Slices).
func stateSlices(tk *task.Task) ([]map[string]any, bool) {
	f := reflect.ValueOf(tk.State).FieldByName("Slices")
	if !f.IsValid() {
		return nil, false
	}
	return jsonSlices(f.Interface()), true
}

// resultSlices đọc khoá JSON `slices` của Result.
func resultSlices(res *Result) []map[string]any {
	b, err := json.Marshal(res)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m["slices"] == nil {
		return nil
	}
	var out []map[string]any
	if json.Unmarshal(m["slices"], &out) != nil {
		return nil
	}
	return out
}

func jsonSlices(v any) []map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []map[string]any
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

// strVals gom mọi giá trị chuỗi (đệ quy) của một phần tử.
func strVals(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, e := range x {
			strVals(e, out)
		}
	case map[string]any:
		for _, e := range x {
			strVals(e, out)
		}
	}
}

func slText(m map[string]any) string {
	var vs []string
	strVals(m, &vs)
	return strings.Join(vs, " | ")
}

// slStatus: "accepted"/"discarded" nếu phần tử có giá trị chuỗi đúng bằng nó, ngược lại "" (chưa làm/đang làm).
func slStatus(m map[string]any) string {
	var vs []string
	strVals(m, &vs)
	for _, st := range []string{"accepted", "discarded"} {
		if slices.Contains(vs, st) {
			return st
		}
	}
	return ""
}

// hasStringSet: phần tử có một mảng chuỗi (ở bất kỳ độ sâu) đúng bằng tập want.
func hasStringSet(v any, want []string) bool {
	switch x := v.(type) {
	case []any:
		var got []string
		all := true
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				all = false
				break
			}
			got = append(got, s)
		}
		if all {
			w := slices.Clone(want)
			slices.Sort(w)
			slices.Sort(got)
			if slices.Equal(slices.Compact(got), w) {
				return true
			}
		}
		for _, e := range x {
			if hasStringSet(e, want) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasStringSet(e, want) {
				return true
			}
		}
	}
	return false
}

func slDump(items []map[string]any) string {
	b, _ := json.Marshal(items)
	return string(b)
}

// checkSliceList kiểm danh sách lát: 3 phần tử theo thứ tự nhóm (SL1..SL3), trạng thái mong đợi ("" = chưa làm).
func checkSliceList(t *testing.T, what string, items []map[string]any, want []string) {
	t.Helper()
	if len(items) != len(want) {
		t.Errorf("%s: %d lát, muốn %d (3 nhóm gắn AC, dòng Ngoài phạm vi không thành lát): %s", what, len(items), len(want), slDump(items))
		return
	}
	for i, it := range items {
		tok := fmt.Sprintf("SL%d", i+1)
		if txt := slText(it); !strings.Contains(txt, tok) || strings.Contains(txt, "SLOUT") {
			t.Errorf("%s: lát %d phải là nhóm %s (theo thứ tự dòng trong plan.md): %s", what, i+1, tok, txt)
		}
		if got := slStatus(it); got != want[i] {
			t.Errorf("%s: lát %d trạng thái %q, muốn %q: %s", what, i+1, got, want[i], slText(it))
		}
	}
}

var slLatHeadRe = regexp.MustCompile(`lát\s*1\b[\s\S]*lát\s*2\b[\s\S]*lát\s*3\b`)

func TestSliceRatchet(t *testing.T) {
	groups := []string{slGroup1, slGroup2, slGroup3}
	base := slBox(t, slOpts{timeoutMin: 45, sliceTimeout: ptr(20)})

	t.Run("split", func(t *testing.T) {
		s := base
		s.need(t)
		if s.first.Status != task.StatusBlocked || !strings.HasPrefix(s.first.Reason, "kế hoạch lớn") {
			t.Fatalf("lượt đầu phải dừng ở cổng cỡ kế hoạch: %s %q", s.first.Status, s.first.Reason)
		}
		r := s.first.Reason
		if !(strings.Contains(r, "SL1") && strings.Contains(r, "SL2") && strings.Contains(r, "SL3")) && !slLatHeadRe.MatchString(r) {
			t.Errorf("lý do chặn của cổng cỡ kế hoạch phải liệt kê các lát L1, L2, L3: %q", r)
		}
		feats, idx, firstRev := s.featReqs()
		var sliceFeats []eval.AgentRequest
		for i, f := range feats {
			if firstRev < 0 || idx[i] < firstRev {
				sliceFeats = append(sliceFeats, f)
			}
		}
		if len(sliceFeats) != 3 {
			t.Fatalf("muốn đúng 3 bước b-feat (một mỗi lát) trước b-review, được %d; chuỗi bước %v", len(sliceFeats), s.h.calls)
		}
		for k, f := range sliceFeats {
			p := f.Prompt
			if !regexp.MustCompile(fmt.Sprintf(`lát\s*%d\s*/\s*3\b`, k+1)).MatchString(p) {
				t.Errorf("đề bài b-feat thứ %d phải nêu \"lát %d/3\":\n%s", k+1, k+1, p)
			}
			if !strings.Contains(p, groups[k]) {
				t.Errorf("đề bài lát %d phải chứa nguyên văn dòng nhóm %q:\n%s", k+1, groups[k], p)
			}
			if regexp.MustCompile(`lát\s*\d+\s*/\s*[^3\s]`).MatchString(p) || regexp.MustCompile(`lát\s*4\b`).MatchString(p) {
				t.Errorf("đề bài lát %d coi số lát khác 3 (dòng Ngoài phạm vi không phải lát):\n%s", k+1, p)
			}
			if want := 20 * time.Minute; f.Timeout != want {
				t.Errorf("lát %d: AgentRequest.Timeout = %v, muốn min(timeout_minutes=45, slice_timeout_minutes=20) = %v", k+1, f.Timeout, want)
			}
		}
		tk := loadTask(t, s.h, s.id)
		items, ok := stateSlices(tk)
		if !ok {
			t.Fatalf("task.State thiếu trường Slices")
		}
		checkSliceList(t, "State.Slices", items, []string{"accepted", "discarded", "accepted"})
	})

	t.Run("ratchet", func(t *testing.T) {
		s := base
		s.need(t)
		tk := loadTask(t, s.h, s.id)
		items, ok := stateSlices(tk)
		if !ok || len(items) != 3 {
			t.Fatalf("State.Slices (có trường=%v): %s", ok, slDump(items))
		}
		if !hasStringSet(items[0], []string{"TestGoal/c2", "TestGoal/c3"}) {
			t.Errorf("L1 chốt phải ghi tập ô đỏ {TestGoal/c2, TestGoal/c3}: %s", slText(items[0]))
		}
		if slStatus(items[0]) != "accepted" {
			t.Errorf("L1 phải accepted (ô đỏ 3→2, test cũ xanh): %s", slText(items[0]))
		}
		if !hasStringSet(items[2], []string{"TestGoal/c2"}) {
			t.Errorf("L3 chốt phải ghi tập ô đỏ {TestGoal/c2}: %s", slText(items[2]))
		}
		if slStatus(items[2]) != "accepted" {
			t.Errorf("L3 phải accepted (ô đỏ {c2,c3}→{c2}): %s", slText(items[2]))
		}
		// slice-green: sau L1 (đo ở đầu L2) và sau L3 (đo ở đầu b-review).
		if !s.atL2.taken {
			t.Errorf("lát 2 không chạy: không đo được slice-green sau L1")
		} else if s.atL2.greenTree == "" || s.atL2.greenTree != s.atL2.tree {
			t.Errorf("đầu lát 2: refs/bint/tasks/%s/slice-green trỏ tree %q, muốn cây sau L1 %q", s.id, s.atL2.greenTree, s.atL2.tree)
		}
		if !s.atRev.taken {
			t.Fatalf("không có b-review sau các lát: chuỗi bước %v", s.h.calls)
		}
		if s.atRev.greenTree == "" || s.atRev.greenTree != s.atRev.tree {
			t.Errorf("sau L3: slice-green trỏ tree %q, muốn cây sau L3 %q", s.atRev.greenTree, s.atRev.tree)
		}
		if s.atL2.taken && s.atRev.greenTree == s.atL2.greenTree {
			t.Errorf("slice-green phải tiến sau L3 chốt (vẫn là cây sau L1 %q)", s.atL2.greenTree)
		}
		// Kết luận: mọi lượt có lát mỏng ghi Result.Slices.
		var rs []map[string]any
		for _, r := range s.runs[1:] {
			if got := resultSlices(r); len(got) > 0 {
				rs = got
			}
		}
		checkSliceList(t, "Result.Slices (JSON `slices`) của lượt chạy lát", rs, []string{"accepted", "discarded", "accepted"})
	})

	t.Run("discard", func(t *testing.T) {
		t.Run("L2 làm đỏ test cũ", func(t *testing.T) { slCheckL2Discard(t, base) })
		slDiscardVariants(t)
	})

	t.Run("deliver", func(t *testing.T) { slCheckDeliver(t) })

	t.Run("controls", func(t *testing.T) { slCheckControls(t) })
}

func slCheckL2Discard(t *testing.T, s *slRun) {
	s.need(t)
	if !s.atRev.taken {
		t.Fatalf("không có b-review sau các lát (L3 phải chạy sau khi bỏ L2, hết lát thì sang b-review): chuỗi bước %v", s.h.calls)
	}
	st := s.atRev.state
	if !s.atRev.stateOK || len(st) != 3 {
		t.Fatalf("State.Slices ở đầu b-review (có trường=%v): %s", s.atRev.stateOK, slDump(st))
	}
	if slStatus(st[1]) != "discarded" || !strings.Contains(slText(st[1]), "TestOld") {
		t.Errorf("L2 phải discarded với lý do nêu test cũ đỏ TestOld: %s", slText(st[1]))
	}
	if s.atRev.old != slOldBody {
		t.Errorf("old.txt phải về nội dung gốc %q sau khi bỏ L2, được %q", slOldBody, s.atRev.old)
	}
	if s.atRev.extra != "<không có>" {
		t.Errorf("l2-extra.txt (tạo sau điểm xanh) phải bị xoá khi bỏ L2, còn %q", s.atRev.extra)
	}
	if s.atRev.outside != slOutBody {
		t.Errorf("outside.txt (không thuộc tác vụ) phải giữ nguyên, được %q", s.atRev.outside)
	}
	if !strings.Contains(s.atRev.discOld, strings.TrimSpace(slOldTamp)) {
		t.Errorf("refs/bint/tasks/%s/slice-2-discarded phải chứa old.txt đã sửa, được %q", s.id, s.atRev.discOld)
	}
	if s.atRev.discExtra == "<không có>" {
		t.Errorf("refs/bint/tasks/%s/slice-2-discarded phải chứa l2-extra.txt", s.id)
	}
	if s.slices != 3 {
		t.Errorf("L3 phải vẫn chạy sau khi bỏ L2: %d lát đã chạy", s.slices)
	}
}

func slDiscardVariants(t *testing.T) {
	t.Run("(d) L2 quá trần thời gian", func(t *testing.T) {
		d := slBox(t, slOpts{timeoutMin: 15, l2TimedOut: true})
		d.need(t)
		feats, _, _ := d.featReqs()
		if len(feats) > 0 && feats[0].Timeout != 15*time.Minute {
			t.Errorf("Timeout lát = %v, muốn min(timeout_minutes=15, slice_timeout_minutes mặc định 30) = 15m", feats[0].Timeout)
		}
		if d.slices != 3 {
			t.Fatalf("L3 phải vẫn chạy sau khi L2 quá trần: %d lát đã chạy, chuỗi bước %v, kết quả %s %q", d.slices, d.h.calls, d.last().Status, d.last().Reason)
		}
		items, ok := stateSlices(loadTask(t, d.h, d.id))
		if !ok || len(items) != 3 {
			t.Fatalf("State.Slices: %s", slDump(items))
		}
		txt := slText(items[1])
		if slStatus(items[1]) != "discarded" || !(strings.Contains(txt, "trần") || strings.Contains(txt, "quá giờ") || strings.Contains(txt, "hết giờ") || strings.Contains(txt, "timeout")) {
			t.Errorf("L2 phải discarded với lý do quá trần thời gian: %s", txt)
		}
		if !d.atRev.taken || d.atRev.old != slOldBody || d.atRev.extra != "<không có>" {
			t.Errorf("L2 quá trần phải được đưa về điểm xanh trước L3/b-review: old.txt=%q l2-extra.txt=%q (đã review=%v)", d.atRev.old, d.atRev.extra, d.atRev.taken)
		}
	})

	t.Run("(f) L1 không làm giảm ô đỏ", func(t *testing.T) {
		f := slBox(t, slOpts{timeoutMin: 45, l1NoProgress: true})
		f.need(t)
		res := f.last()
		if res.Status != task.StatusBlocked {
			t.Errorf("lát 1 bị bỏ thì tác vụ phải blocked: %s %q", res.Status, res.Reason)
		}
		feats, _, _ := f.featReqs()
		if len(feats) != 1 {
			t.Errorf("lát 1 bị bỏ: không được có b-feat thứ hai, chuỗi bước %v", f.h.calls)
		}
		items, ok := stateSlices(loadTask(t, f.h, f.id))
		if !ok || len(items) == 0 || slStatus(items[0]) != "discarded" {
			t.Errorf("L1 phải discarded (ô đỏ 3→3 không giảm): %s", slDump(items))
		}
		if got := f.h.read("c1.txt"); got != "<không có>" {
			t.Errorf("c1.txt do L1 tạo phải bị xoá khi quay về điểm xanh, còn %q", got)
		}
	})
}

func slCheckDeliver(t *testing.T) {
	ctx := context.Background()
	e := slBox(t, slOpts{timeoutMin: 45, budget: ptr(1.0)})
	e.need(t)
	res := e.last()
	if res.Status != task.StatusBlocked || !strings.Contains(res.Reason, "ngân sách") {
		t.Fatalf("ngân sách chỉ đủ L1: tác vụ phải blocked vì ngân sách: %s %q (chuỗi bước %v)", res.Status, res.Reason, e.h.calls)
	}
	if e.slices != 1 {
		t.Errorf("hết ngân sách sau L1: chỉ được chạy 1 lát, đã chạy %d", e.slices)
	}
	for k, tok := range map[int]string{2: "SL2", 3: "SL3"} {
		if !strings.Contains(res.Reason, tok) && !regexp.MustCompile(fmt.Sprintf(`lát\s*%d\b`, k)).MatchString(res.Reason) {
			t.Errorf("lý do phải liệt kê lát chưa làm L%d: %q", k, res.Reason)
		}
	}
	checkSliceList(t, "Result.Slices khi hết ngân sách", resultSlices(res), []string{"accepted", "", ""})
	green, err := gitrepo.Run(ctx, e.h.root, "rev-parse", "--verify", "-q", slSliceRef(e.id, "slice-green")+"^{tree}")
	green = strings.TrimSpace(green)
	if err != nil || green == "" {
		t.Errorf("thiếu ref slice-green sau L1 chốt: %v", err)
	}
	if res.LastGreen == nil || res.LastGreen.Tree != green {
		t.Errorf("Result.LastGreen phải là điểm xanh lát (tree %q), được %+v", green, res.LastGreen)
	}
	pre, err := e.h.runner().Env.RestoreGreen(ctx, e.id, false)
	if err != nil || pre == nil || pre.Tree != green || len(pre.Restore) != 0 {
		t.Errorf("RestoreGreen xem trước phải dùng điểm xanh lát và không có tệp cần khôi phục: %+v %v", pre, err)
	}
	// Nâng ngân sách rồi resume: chạy tiếp L2, không chạy lại L1.
	e.h.cfg.Limits.Workflows = map[string]config.PerRunLimits{StepDo: {MaxCostUSD: ptr(100.0)}}
	before := len(e.h.calls)
	res2, err := e.h.runner().Run(ctx, RunOptions{Workflow: StepDo, TaskRef: e.id, Resume: true})
	if err != nil {
		t.Fatalf("resume sau khi nâng ngân sách: %v", err)
	}
	var next *eval.AgentRequest
	for i := before; i < len(e.h.calls); i++ {
		if e.h.calls[i] == StepFeat {
			next = &e.h.reqs[i]
			break
		}
	}
	if next == nil {
		t.Fatalf("resume không chạy b-feat nào: chuỗi bước %v, kết quả %s %q", e.h.calls[before:], res2.Status, res2.Reason)
	}
	if !regexp.MustCompile(`lát\s*2\s*/\s*3\b`).MatchString(next.Prompt) || !strings.Contains(next.Prompt, slGroup2) {
		t.Errorf("b-feat đầu tiên sau resume phải là lát 2/3 (nhóm %q):\n%s", slGroup2, next.Prompt)
	}
	if e.slices != 3 {
		t.Errorf("sau resume phải chạy L2 và L3 (không chạy lại L1): tổng %d lát đã chạy", e.slices)
	}
	items, _ := stateSlices(loadTask(t, e.h, e.id))
	checkSliceList(t, "State.Slices sau resume", items, []string{"accepted", "discarded", "accepted"})
}

func slCheckControls(t *testing.T) {
	if v, ok := getSliceTimeout(config.Default()); !ok || v != 30 {
		t.Errorf("workflow.slice_timeout_minutes mặc định phải là 30 (có khoá=%v, giá trị %d)", ok, v)
	}
	for _, tc := range []struct {
		name string
		o    slOpts
	}{
		{"(a) kế hoạch nhỏ", slOpts{timeoutMin: 45, small: true}},
		{"(b) slice_timeout_minutes = 0", slOpts{timeoutMin: 45, sliceTimeout: ptr(0)}},
		{"(c) một nhóm gắn AC", slOpts{timeoutMin: 45, oneAC: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := slBox(t, tc.o)
			s.need(t)
			feats, _, _ := s.featReqs()
			if len(feats) != 1 || s.slices != 0 {
				t.Errorf("phải chạy đúng một b-feat như cũ: %d b-feat (%d theo lát), chuỗi bước %v", len(feats), s.slices, s.h.calls)
			}
			if items, _ := stateSlices(loadTask(t, s.h, s.id)); len(items) != 0 {
				t.Errorf("State.Slices phải rỗng: %s", slDump(items))
			}
			if res := s.last(); res.Status != task.StatusDone {
				t.Errorf("một b-feat làm đủ việc thì phải done: %s %q", res.Status, res.Reason)
			}
		})
	}
}
