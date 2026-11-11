package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/evolve"
	"bint/internal/runlog"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
	rnscreen "bint/internal/tui/screens/runs"
	"bint/internal/tui/theme"
)

// capt ghi các hành động của màn Lượt chạy và Tiến hoá thay vì gọi dịch vụ thật: Do trả ngay giá trị dựng sẵn, Job chờ
// tới khi được thả (release) hoặc bị huỷ.
type capt struct {
	mu      sync.Mutex
	calls   map[string][]any
	values  map[string]any
	release map[string]chan struct{}
	started map[string]chan struct{}
}

func (c *capt) record(id string, o any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[id] = append(c.calls[id], o)
}

func (c *capt) count(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls[id])
}

func (c *capt) last(t *testing.T, id string) any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	l := c.calls[id]
	if len(l) == 0 {
		t.Fatalf("hành động %s chưa được gọi", id)
	}
	return l[len(l)-1]
}

// capture dựng sổ hành động ghi lại; Options.Registry của mô hình dùng nó.
func capture(jobIDs ...string) (*actions.Registry, *capt) {
	c := &capt{calls: map[string][]any{}, values: map[string]any{}, release: map[string]chan struct{}{}, started: map[string]chan struct{}{}}
	for _, id := range jobIDs {
		c.release[id], c.started[id] = make(chan struct{}), make(chan struct{}, 4)
	}
	var list []actions.Action
	for _, a := range actions.Default().All() {
		a := a
		if a.Screen == actions.Runs || a.Screen == actions.Evolve {
			if a.Do != nil {
				a.Do = func(_ context.Context, _ actions.Env, o any) tea.Msg {
					c.record(a.ID, o)
					return actions.ResultMsg{ID: a.ID, Opts: o, Value: c.values[a.ID]}
				}
			}
			if a.Job != nil {
				a.Job = func(ctx context.Context, _ actions.Env, o any, br *jobs.Bridge) (any, error) {
					c.record(a.ID, o)
					br.Log("dòng log đầu của " + a.ID)
					c.started[a.ID] <- struct{}{}
					select {
					case <-c.release[a.ID]:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return c.values[a.ID], nil
				}
			}
		}
		list = append(list, a)
	}
	return actions.NewRegistry(list), c
}

func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		press(t, m, string(r))
	}
}

func newCapModel(t *testing.T, o Options, jobIDs ...string) (*Model, *capt) {
	t.Helper()
	reg, c := capture(jobIDs...)
	o.Registry, o.Mode = reg, theme.Mono
	return newModel(t, o, sampleData(), 140, 40), c
}

func hasAll(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("thiếu %q trong:\n%s", w, out)
		}
	}
}

// ---- Lượt chạy ----

func TestRunsListDetailAndFilter(t *testing.T) {
	m, _ := newCapModel(t, Options{})
	press(t, m, "4")
	out := plain(m.Render())
	hasAll(t, out, "Lượt chạy 3/3", "000003", "000002", "000001", "thất bại", "thành công", "chưa gán nhãn", "Tín hiệu kết quả (1)", "Đầu ra (1)", "Sự kiện (2)", "session.started")
	press(t, m, "j")
	hasAll(t, plain(m.Render()), "1RUN000002", "người dùng chấm bad")
	press(t, m, "/")
	typeText(t, m, "codex")
	press(t, m, "enter")
	if got := m.rn.Visible(); len(got) != 1 || got[0].Run.RunID != "01RUN000002" {
		t.Fatalf("lọc codex: %+v", got)
	}
	hasAll(t, plain(m.Render()), "Lượt chạy 1/3")
	press(t, m, "esc")
	if len(m.rn.Visible()) != 3 {
		t.Fatal("Esc phải bỏ lọc")
	}
}

func TestRunsNarrowSinglePane(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 80, 24)
	press(t, m, "4")
	if out := plain(m.Render()); !strings.Contains(out, "Lượt chạy 3/3") || strings.Contains(out, "Tín hiệu kết quả") {
		t.Fatalf("hẹp: chỉ danh sách:\n%s", out)
	}
	press(t, m, "tab")
	if out := plain(m.Render()); !strings.Contains(out, "Tín hiệu kết quả") || strings.Contains(out, "Lượt chạy 3/3") {
		t.Fatalf("hẹp: Tab đổi sang chi tiết:\n%s", out)
	}
}

// +/- mở hộp ghi chú rồi gửi runs.rate với đúng Options; hộp ghi rõ lượt và điểm.
func TestRunsRateSendsOptions(t *testing.T) {
	m, c := newCapModel(t, Options{})
	press(t, m, "4", "j", "-")
	hasAll(t, plain(m.Render()), "Chấm lượt 01RUN000002: xấu (bad)", "Ghi chú")
	typeText(t, m, "lỗi")
	press(t, m, "enter")
	if o := c.last(t, "runs.rate").(bintapp.RunsRateOptions); o.Ref != "01RUN000002" || o.Rating != "bad" || o.Note != "lỗi" {
		t.Fatalf("%+v", o)
	}
	// Từ chối (Esc) thì không gửi gì.
	n := c.count("runs.rate")
	press(t, m, "+", "esc")
	if c.count("runs.rate") != n {
		t.Fatal("Esc không được chấm")
	}
	// Kết quả → thông báo và nạp lại.
	m.Update(actions.ResultMsg{ID: "runs.rate", Value: bintapp.RateResult{RunID: "01RUN000002", Rating: "bad"}})
	if !strings.Contains(plain(m.Render()), "Đã chấm 01RUN000002: bad") {
		t.Fatalf("thiếu thông báo:\n%s", plain(m.Render()))
	}
}

// Form quét Git: ngày mặc định 30/14 từ Defaults của dịch vụ; đổi giá trị và gửi; dòng CLI tương đương đúng.
func TestRunsScanFormSendsOptionsAndCLI(t *testing.T) {
	m, c := newCapModel(t, Options{})
	press(t, m, "4", "S")
	out := plain(m.Render())
	hasAll(t, out, "Số ngày", "Ngày hoàn tác", "30", "14", "bint runs scan")
	for i := 0; i < 2; i++ {
		press(t, m, "backspace")
	}
	typeText(t, m, "7")
	if out := plain(m.Render()); !strings.Contains(out, "bint runs scan --days 7") {
		t.Fatalf("dòng CLI:\n%s", out)
	}
	press(t, m, "ctrl+s")
	if o := c.last(t, "runs.scan").(bintapp.RunsScanOptions); o.Days != 7 || o.RevertDays != 14 {
		t.Fatalf("%+v", o)
	}
	m.Update(actions.ResultMsg{ID: "runs.scan", Value: runlog.ScanResult{Runs: 3, Committed: 2, Reverted: 1}})
	hasAll(t, plain(m.Render()), "Đã quét 3 lượt: 2 commit được gắn, 1 hoàn tác")
}

func TestRunsIngestKey(t *testing.T) {
	m, c := newCapModel(t, Options{})
	press(t, m, "4", "I")
	if _, ok := c.last(t, "runs.ingest").(bintapp.EventsIngestOptions); !ok {
		t.Fatal("I phải gọi runs.ingest")
	}
}

// Thẻ Bài học: thư viện nhóm theo vùng với số đếm; d mở form digest gợi ý latest; kết quả hiện nhóm và trạng thái sẵn sàng.
func TestRunsLessonsTabAndDigest(t *testing.T) {
	m, c := newCapModel(t, Options{})
	press(t, m, "4", ".")
	out := plain(m.Render())
	hasAll(t, out, "Thư viện bài học (2)", "Chung (mọi vùng mã)", "Riêng — internal/db/**", "Gọi Reset trước Retry", "chèn 7 · eval đạt 5/trượt 2", "[candidate]")
	if g := m.rn.LessonGroups(); len(g) != 2 || g[0] != "" || g[1] != "internal/db/**" {
		t.Fatalf("nhóm: %q", g)
	}
	press(t, m, "j")
	hasAll(t, plain(m.Render()), "Đóng kết nối sau truy vấn", "Mục candidate chỉ được chèn như tham khảo")
	press(t, m, "d")
	hasAll(t, plain(m.Render()), "Đợt hoặc lượt", "latest", "bint experience digest")
	press(t, m, "ctrl+s")
	if o := c.last(t, "runs.lesson_digest").(evolvesvc.ExperienceDigestOptions); o.Ref != "latest" {
		t.Fatalf("%+v", o)
	}
	m.Update(actions.ResultMsg{ID: "runs.lesson_digest", Opts: evolvesvc.ExperienceDigestOptions{Ref: "latest"}, Value: sampleDigest()})
	hasAll(t, plain(m.Render()), "Đợt eb_01BATCH: 2 lượt thất bại", "internal/db — 2 tác vụ [sẵn sàng", "[chưa: cần ≥ 2 tác vụ phân biệt]", "(~42 token)")
	// Ở thẻ Danh sách, d điền sẵn lượt đang chọn (digest theo lượt).
	press(t, m, "esc", ",", "j", "d")
	hasAll(t, plain(m.Render()), "01RUN000002")
}

// --read-only: không ghi (chấm, quét, nạp) qua phím, bảng lệnh hay yêu cầu trực tiếp; loader nhận readOnly để không
// nạp spool vào SQLite.
func TestRunsReadOnlyRefusesWritesAndSkipsIngest(t *testing.T) {
	var sawRO atomicBool
	m, c := newCapModel(t, Options{ReadOnly: true,
		RunsLoader: func(_ context.Context, _ string, _ int, ro bool) (rnscreen.Data, error) {
			sawRO.set(ro)
			return sampleRuns(), nil
		}})
	press(t, m, "4")
	if !sawRO.get() {
		t.Fatal("loader phải nhận readOnly = true (không nạp spool)")
	}
	for _, k := range []string{"+", "-", "S", "I"} {
		m.toast = ""
		press(t, m, k)
		if m.rn.Capturing() {
			t.Errorf("phím %s mở hộp/form dù chỉ đọc", k)
			press(t, m, "esc")
		}
		if !strings.Contains(plain(m.Render()), "chế độ chỉ đọc") {
			t.Errorf("phím %s phải báo chỉ đọc", k)
		}
	}
	for _, id := range []string{"runs.rate", "runs.scan", "runs.ingest"} {
		_, cmd := m.Update(actions.RequestMsg{ID: id, Opts: actionOptions(id)})
		drive(t, m, cmd)
		if c.count(id) != 0 {
			t.Errorf("%s đã tới Do ở chế độ chỉ đọc", id)
		}
	}
	// Hành động đọc vẫn dùng được: thẻ Bài học và digest.
	press(t, m, ".", "d")
	if !m.rn.Capturing() {
		t.Fatal("digest là hành động đọc, phải mở được ở chế độ chỉ đọc")
	}
}

// Trong phiên agent, chấm lượt (HumanOnly) bị khoá; quét Git vẫn dùng được.
func TestRunsRateLockedInAgentSession(t *testing.T) {
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
	m, c := newCapModel(t, Options{Agent: true})
	press(t, m, "4", "+")
	if m.rn.Capturing() || !strings.Contains(plain(m.Render()), "chỉ dành cho người") {
		t.Fatalf("+ phải bị khoá:\n%s", plain(m.Render()))
	}
	_, cmd := m.Update(actions.RequestMsg{ID: "runs.rate", Opts: actionOptions("runs.rate")})
	drive(t, m, cmd)
	if c.count("runs.rate") != 0 {
		t.Fatal("runs.rate không được tới Do trong phiên agent")
	}
	press(t, m, "S")
	if !m.rn.Capturing() {
		t.Fatal("quét Git không phải thao tác chỉ dành cho người")
	}
}

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBool) set(v bool) { a.mu.Lock(); a.v = v; a.mu.Unlock() }
func (a *atomicBool) get() bool  { a.mu.Lock(); defer a.mu.Unlock(); return a.v }

func actionOptions(id string) any {
	a, _ := actions.Default().ByID(id)
	return a.Options
}

var _ = time.Second
var _ = evolve.StatusProposed
