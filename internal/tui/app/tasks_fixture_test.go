package app

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/task"
	"bint/internal/testutil"
	"bint/internal/tui/jobs"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
	"bint/internal/workflow"
)

var stepRe = regexp.MustCompile(`step="([a-z-]+)"`)

// fixture là repository thật đã `init`, mô hình TUI dùng dịch vụ thật (không loader giả cho màn Tác vụ) và agent giả
// tiêm qua Options.AgentFactory. behave mô phỏng việc của từng bước (có thể chặn để kiểm tra trạng thái giữa chừng).
type fixture struct {
	t     *testing.T
	root  string
	m     *Model
	ws    *bintapp.TaskWS
	calls []string
	mu    sync.Mutex
	// behave chạy trong goroutine của agent; logPath là tệp log của bước (ghi dần để mô phỏng tiến trình thật).
	behave func(ctx context.Context, step string, e *workflow.Env, taskID, logPath string) error
	msgs   chan tea.Msg
}

func newFixture(t *testing.T, o Options) *fixture {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{"app.txt": "v1\n", "migrations/001.sql": "--\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if _, err := bintapp.Init(context.Background(), bintapp.InitOptions{Dir: root, Hooks: boolPtr(false), NoSkills: true}); err != nil {
		t.Fatal(err)
	}
	ws, err := bintapp.OpenTasks(context.Background(), root, "test")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, root: root, ws: ws, msgs: make(chan tea.Msg, 256)}
	f.behave = f.happy
	o.Dir, o.Version, o.Lang, o.Mode = root, "test", "vi", theme.Mono
	o.Loader = func(context.Context, string) (overview.Data, error) {
		return overview.Data{Status: &bintapp.StatusReport{Repo: root}}, nil
	}
	o.AgentFactory = f.factory
	o.SpinnerFPS, o.TailEvery = time.Millisecond, 5*time.Millisecond
	if o.ExitGrace == 0 {
		o.ExitGrace = 3 * time.Second
	}
	f.m = New(context.Background(), o)
	f.m.SetSize(140, 40)
	f.pump(func() bool { return !f.m.ov.Loading }, f.m.Init())
	return f
}

func boolPtr(b bool) *bool { return &b }

func (f *fixture) factory(step string, _ *float64, _ *int) (eval.Agent, error) {
	return &fakeAgent{f: f}, nil
}

type fakeAgent struct{ f *fixture }

func (a *fakeAgent) Name() string                   { return "fake" }
func (a *fakeAgent) Version(context.Context) string { return "" }
func (a *fakeAgent) ReportsCost() bool              { return false }

func (a *fakeAgent) Run(ctx context.Context, req eval.AgentRequest) eval.AgentResult {
	f := a.f
	step := stepRe.FindStringSubmatch(req.Prompt)[1]
	envs := map[string]string{}
	for _, kv := range req.Env {
		k, v, _ := strings.Cut(kv, "=")
		envs[k] = v
	}
	f.mu.Lock()
	f.calls = append(f.calls, step)
	f.mu.Unlock()
	e := *f.ws.Env
	e.Human, e.Actor, e.RunID = false, "agent", envs[workflow.EnvRunID]
	res := eval.AgentResult{Duration: time.Millisecond, Model: "fake-model"}
	if err := f.behave(ctx, step, &e, envs[workflow.EnvTaskID], req.LogPath); err != nil {
		res.ExitCode, res.Error = 1, err.Error()
	}
	if ctx.Err() != nil {
		res.ExitCode, res.Error = -1, ctx.Err().Error()
	}
	return res
}

func (f *fixture) write(name, body string) {
	p := filepath.Join(f.root, filepath.FromSlash(name))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// happy: b-req ghi ba tài liệu hợp lệ, b-feat sửa app.txt, b-review ghi vòng không có phát hiện chặn.
func (f *fixture) happy(_ context.Context, step string, e *workflow.Env, id, _ string) error {
	switch step {
	case workflow.StepReq:
		const crit = "app.txt ghi v2"
		if _, _, err := e.SetTicket(id, workflow.TicketEdit{ClearCriteria: true, Criteria: []string{crit}}); err != nil {
			return err
		}
		docs := map[string]string{
			"requirement": "# Yêu cầu\n## Mục tiêu và phạm vi\nĐổi app.txt\n## Tiêu chí chấp nhận\n- [ ] AC-1: " + crit + "\n## Ngoài phạm vi\nKhông có\n## Câu hỏi mở\n",
			"plan":        "# Kế hoạch\nSửa app.txt\n",
			"tasks":       "- [ ] TSK-1 [AC-1]: sửa app.txt — xong khi: " + crit + "\n",
		}
		for n, body := range docs {
			if _, _, err := e.WriteArtifact(id, n, body); err != nil {
				return err
			}
		}
	case workflow.StepFeat:
		f.write("app.txt", "v2\n")
		if _, err := e.CheckCriterion(id, 1, true, "app.txt chứa v2"); err != nil && !strings.Contains(err.Error(), "không tồn tại") {
			return err
		}
	case workflow.StepReview:
		_, _, err := e.SubmitReview(id, []byte(`{"summary":"Thay đổi nhỏ, đúng yêu cầu","findings":[{"kind":"suggestion","severity":"low","title":"Đặt tên rõ hơn","path":"app.txt","lines":"1","evidence":"tên chưa rõ"}]}`))
		return err
	}
	return nil
}

// newTask lập tác vụ qua dịch vụ (như người đã gõ `task new`).
func (f *fixture) newTask(goal string, edit func(*bintapp.TaskNewOptions)) string {
	f.t.Helper()
	o := bintapp.TaskNewDefaults(config.Default())
	o.Request = goal
	if edit != nil {
		edit(&o)
	}
	c, err := bintapp.NewTask(context.Background(), f.ws, o)
	if err != nil {
		f.t.Fatal(err)
	}
	return c.Task.State.ID
}

func (f *fixture) task(id string) *task.Task {
	f.t.Helper()
	tk, err := f.ws.Tasks.Load(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return tk
}

// pump chạy cmd và mọi lệnh phát sinh trong goroutine riêng, đưa thông điệp về Update (trong goroutine của kiểm
// thử) tới khi until() đúng; quá hạn thì báo lỗi kèm màn hình hiện tại.
func (f *fixture) pump(until func() bool, cmd tea.Cmd) {
	f.t.Helper()
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case nil:
			case tea.BatchMsg:
				for _, x := range msg {
					run(x)
				}
			default:
				f.msgs <- msg
			}
		}()
	}
	run(cmd)
	deadline := time.After(20 * time.Second)
	for !until() {
		select {
		case msg := <-f.msgs:
			_, next := f.m.Update(msg)
			run(next)
		case <-deadline:
			f.t.Fatalf("hết hạn chờ; màn hình:\n%s", plain(f.m.Render()))
		}
	}
}

// press gửi phím rồi chạy lệnh phát sinh (không chờ điều kiện nào).
func (f *fixture) press(keys ...string) {
	f.t.Helper()
	for _, k := range keys {
		_, cmd := f.m.Update(keyMsg(k))
		f.settle(cmd)
	}
}

// settle chạy cmd và các thông điệp tức thời của nó (không chờ việc nền).
func (f *fixture) settle(cmd tea.Cmd) {
	f.t.Helper()
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case nil:
			case tea.BatchMsg:
				for _, x := range msg {
					run(x)
				}
			default:
				f.msgs <- msg
			}
		}()
	}
	run(cmd)
	// Nhận thông điệp tới khi yên lặng hoặc hết hạn ngắn (vòng quay chạy liên tục khi có việc nền nên không bao giờ yên).
	end := time.After(250 * time.Millisecond)
	for {
		select {
		case msg := <-f.msgs:
			_, next := f.m.Update(msg)
			run(next)
		case <-time.After(40 * time.Millisecond):
			return
		case <-end:
			return
		}
	}
}

// waitJobsDone chờ bảng việc nền rỗng.
func (f *fixture) waitJobsDone() {
	f.t.Helper()
	f.pump(func() bool { return f.m.jobs.Len() == 0 }, nil)
}

func (f *fixture) job() *jobs.Job {
	for _, j := range f.m.jobs.All() {
		return j
	}
	return nil
}

// selectTask chuyển sang màn Tác vụ và chọn tác vụ id.
func (f *fixture) selectTask(id string) {
	f.t.Helper()
	f.press("3")
	f.pump(func() bool { return f.m.tk.Loaded }, nil)
	f.settle(f.m.tk.Focus(id))
	f.pump(func() bool { c := f.m.tk.Cursor(); return c != nil && c.State.ID == id }, nil)
}

// logWriter ghi từng dòng vào tệp log của bước, mở rộng dần như tiến trình agent thật.
type logWriter struct {
	t *testing.T
	f *os.File
}

func newLogWriter(t *testing.T, path string) *logWriter {
	os.MkdirAll(filepath.Dir(path), 0o700)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Error(err)
	}
	return &logWriter{t, f}
}

func (w *logWriter) line(s string) {
	if _, err := w.f.WriteString(s + "\n"); err != nil {
		w.t.Error(err)
	}
}

type bintappTaskNew = bintapp.TaskNewOptions

func bintappResume(id string) bintapp.RunResumeOptions {
	o := bintapp.RunResumeDefaults(config.Default())
	o.Ref = id
	return o
}
