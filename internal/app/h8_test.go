package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/workflow"
)

// Ca H8 (docs/isolation-subagent-plan.md 4.5): trong lúc bước chỉ đọc chạy, người đổi cấu hình / duyệt tri thức
// qua Bint thì không bị tính là agent sửa cây làm việc. Agent giả chạy ngay trong tiến trình test và gọi thẳng
// dịch vụ như TUI/CLI sẽ làm; vai người/agent do biến môi trường của tiến trình (InAgentSession).

// readonlyAgent là agent giả của bước chỉ đọc: chạy act đúng một lần ở bước b-review.
type readonlyAgent struct{ act func() }

func (a readonlyAgent) Name() string                   { return "fake" }
func (a readonlyAgent) Version(context.Context) string { return "" }
func (a readonlyAgent) ReportsCost() bool              { return false }
func (a readonlyAgent) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	if !req.ReadOnly {
		return eval.AgentResult{Duration: time.Millisecond}
	}
	a.act()
	return eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
}

// h8Fixture: repo đã init, một tác vụ và một mục tri thức candidate để duyệt/xoá.
type h8Fixture struct {
	ws     *TaskWS
	root   string
	taskID string
	itemID string
	itemRel,
	cfgRel string
}

func newH8(t *testing.T) *h8Fixture {
	t.Helper()
	ws, root, id := taskRepo(t) // đã asHuman
	res, err := AddKnowledge(context.Background(), ws.K, AddOptions{Type: knowledge.TypeFact, Title: "Hàm A rỗng",
		Body: "Hàm A trong a.go chưa làm gì.", Paths: []string{"a.go"}, Evidence: []string{"a.go:3"},
		Origin: knowledge.OriginAgent, ThirdParty: true})
	if err != nil || !res.Saved {
		t.Fatalf("tạo candidate: %v %+v", err, res.Check.Findings)
	}
	return &h8Fixture{ws: ws, root: root, taskID: id, itemID: res.Item.ID,
		itemRel: knowledge.ItemsRel + "/" + res.Item.ID + ".md", cfgRel: ".bint/config.yml"}
}

// runReview chạy bước b-review với agent giả chạy act, trả kết quả bước review và các dòng log.
func (f *h8Fixture) runReview(t *testing.T, act func()) (workflow.StepResult, []string) {
	t.Helper()
	var logs []string
	o := RunWorkflowDefaults(f.ws.K.Config)
	o.Workflow, o.Task = "b-review", f.taskID
	o.RunCallbacks = RunCallbacks{
		Log:   func(s string) { logs = append(logs, s) },
		Agent: func(string, *float64, *int) (eval.Agent, error) { return readonlyAgent{act}, nil },
	}
	res, err := RunWorkflow(context.Background(), f.ws, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Steps {
		if s.Step == workflow.StepReview {
			return s, logs
		}
	}
	t.Fatalf("không có bước b-review: %+v", res)
	return workflow.StepResult{}, nil
}

func (f *h8Fixture) setLang(t *testing.T) {
	t.Helper()
	if _, err := SetUILanguage(context.Background(), f.root, UILanguageOptions{Language: "vi", Layer: "project"}); err != nil {
		t.Errorf("SetUILanguage: %v", err)
	}
}

func (f *h8Fixture) approve(t *testing.T) error {
	t.Helper()
	_, err := ApproveKnowledge(context.Background(), f.ws.K, ApproveOptions{Refs: []string{f.itemID}})
	return err
}

// asAgentInline đặt biến agent trong khi agent giả chạy rồi trả lại như cũ (asHuman đã đặt rỗng).
func asAgentInline() func() {
	os.Setenv("BINT_AGENT", "1")
	return func() { os.Setenv("BINT_AGENT", "") }
}

func TestH8HumanWritesDuringReadonlyStepAreExempt(t *testing.T) {
	f := newH8(t)
	step, logs := f.runReview(t, func() {
		f.setLang(t)
		if err := f.approve(t); err != nil {
			t.Errorf("duyệt: %v", err)
		}
	})
	if step.Status != "completed" || step.Error != "" {
		t.Fatalf("bước phải completed: %+v", step)
	}
	want := []string{f.cfgRel, f.itemRel}
	got := slices.Clone(step.HumanWrites)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("HumanWrites = %v, muốn %v", got, want)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "bỏ qua thay đổi do người ghi qua Bint") {
		t.Fatalf("thiếu dòng báo trong log: %q", logs)
	}
	// Dịch vụ ghi thật sự có hiệu lực (không chỉ được miễn).
	if !strings.Contains(read(t, filepath.Join(f.root, f.cfgRel)), "language: vi") {
		t.Fatal("ui.language chưa được ghi")
	}
}

func TestH8AgentEnvStillViolates(t *testing.T) {
	f := newH8(t)
	var approveErr error
	step, _ := f.runReview(t, func() {
		defer asAgentInline()()
		f.setLang(t) // agent: true trong nhật ký
		approveErr = f.approve(t)
	})
	if approveErr == nil || !strings.Contains(approveErr.Error(), ErrAgentForbidden.Error()) {
		t.Fatalf("duyệt trong phiên agent phải bị từ chối: %v", approveErr)
	}
	if step.Status != "failed" || !strings.Contains(step.Error, f.cfgRel) || len(step.HumanWrites) != 0 {
		t.Fatalf("muốn vi phạm vì %s: %+v", f.cfgRel, step)
	}
}

func TestH8AgentEditAfterHumanViolates(t *testing.T) {
	f := newH8(t)
	step, _ := f.runReview(t, func() {
		f.setLang(t)
		p := filepath.Join(f.root, f.cfgRel)
		b, _ := os.ReadFile(p)
		if err := os.WriteFile(p, append(b, []byte("# agent chèn thêm\n")...), 0o644); err != nil {
			t.Error(err)
		}
	})
	if step.Status != "failed" || !strings.Contains(step.Error, f.cfgRel) {
		t.Fatalf("băm lệch phải là vi phạm: %+v", step)
	}
}

func TestH8DirectWriteViolates(t *testing.T) {
	f := newH8(t)
	step, _ := f.runReview(t, func() {
		p := filepath.Join(f.root, f.cfgRel)
		b, _ := os.ReadFile(p)
		if err := os.WriteFile(p, append(b, []byte("# sửa thẳng\n")...), 0o644); err != nil {
			t.Error(err)
		}
	})
	if step.Status != "failed" || !strings.Contains(step.Error, f.cfgRel) || len(step.HumanWrites) != 0 {
		t.Fatalf("sửa không qua dịch vụ phải là vi phạm: %+v", step)
	}
}

func TestH8RemoveThroughServiceRecordsEmptyHash(t *testing.T) {
	f := newH8(t)
	step, _ := f.runReview(t, func() {
		if _, err := RemoveKnowledge(f.ws.K, []string{f.itemID}); err != nil {
			t.Errorf("xoá: %v", err)
		}
	})
	if step.Status != "completed" || !slices.Equal(step.HumanWrites, []string{f.itemRel}) {
		t.Fatalf("xoá qua dịch vụ phải được miễn: %+v", step)
	}
	if _, err := os.Stat(filepath.Join(f.root, f.itemRel)); !os.IsNotExist(err) {
		t.Fatalf("tệp phải đã bị xoá: %v", err)
	}
	recs, err := workflow.ReadWrites(workflow.WritesLogPath(f.ws.K.Paths.Runtime), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	last := recs[len(recs)-1]
	if last.SHA256 != "" || last.Agent || !strings.HasSuffix(last.Path, f.itemRel) {
		t.Fatalf("dòng nhật ký của lần xoá: %+v", last)
	}
}

// Hook không ghi vào nhật ký ghi: hook không ghi tệp trong repo, và chỉ dịch vụ mới được đánh dấu "người ghi qua Bint".
func TestHooksNeverWriteWritesLog(t *testing.T) {
	root := guardRepo(t, []string{"claude"}, map[string]string{
		"guardrails/test-before-stop.yml": gmeta("test-before-stop", "verified") + stopGuard,
	})
	ws, err := OpenWorkspace(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	log := workflow.WritesLogPath(ws.Paths.Runtime)
	os.Remove(log) // init có thể đã ghi (config/skill): chỉ đo phần hook thêm vào
	before := fileSize(log)
	for _, ev := range []struct {
		name string
		in   map[string]any
	}{
		{"PreToolUse", map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "tool_use_id": "t1"}},
		{"PostToolUse", map[string]any{"tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(root, "src", "a.go")}, "tool_use_id": "t1"}},
		{"Stop", map[string]any{"stop_hook_active": false}},
		{"SessionStart", map[string]any{"source": "compact"}},
	} {
		runHook(t, hookInput("s1", ev.name, root, ev.in))
	}
	if after := fileSize(log); after != before {
		t.Fatalf("hook đã ghi vào %s (%d → %d byte)", log, before, after)
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatalf("hook không được tạo %s", log)
	}
}

func fileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}
