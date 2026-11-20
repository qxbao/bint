package app

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/overview"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/theme"
)

// asHuman xoá mọi biến môi trường báo phiên agent: go test có thể đang chạy trong phiên agent
// (CLAUDECODE=1) nên kiểm thử đóng vai người phải đặt rỗng từng biến [k:01M746PHJXDZN6QMA3BEVEDNPP].
func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range bintapp.AgentEnvVars() {
		t.Setenv(k, "")
	}
}

// fullPending là hộp thư có đủ sáu nguồn và một nguồn đọc lỗi.
func fullPending() overview.Data {
	d := sampleData()
	d.Pending = &bintapp.PendingWorkReport{
		KnowledgeBatches: []bintapp.PendingBatch{{Tag: "nguon:lo-thu", Candidate: 4, Stale: 1}, {Tag: "nguon:lo-hai", Candidate: 2}},
		KnowledgeOther:   3,
		Components:       []bintapp.PendingComponent{{Kind: "guidance", ID: "g1"}, {Kind: "guardrail", ID: "r1"}},
		BlockedTasks:     []bintapp.PendingTask{{ID: "01TASK1", Goal: "Thêm cột email", Reason: "cần duyệt"}},
		Proposals:        []bintapp.PendingProposal{{ID: "01PROP1", Action: "create"}},
		EvalCandidates:   []string{"01EVAL1", "01EVAL2"},
		Doctor: []bintapp.Check{{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"},
			{Name: "skill", Level: bintapp.LevelFail, Detail: "skill lệch phiên bản"}},
		Errors: []bintapp.PendingError{{Source: bintapp.PendingSrcProposals, Err: context.DeadlineExceeded}},
	}
	d.Candidates = d.Pending.KnowledgeTotal()
	return d
}

// Mỗi nguồn của hộp thư sinh đúng dòng và đúng đích; xếp theo mức nghiêm trọng (✗, !, ◌), loại có từ 2 mục gộp một dòng.
func TestInboxLinesAndTargets(t *testing.T) {
	d := fullPending()
	in := d.Inbox()
	type want struct {
		key    string
		level  string
		target actions.Screen
	}
	wants := []want{
		{"overview.inbox.doctor_fail", "fail", actions.System}, {"overview.inbox.source_error", "fail", actions.Evolve},
		{"overview.inbox.task", "warn", actions.Tasks}, {"overview.inbox.doctor_warn", "warn", actions.System},
		{"overview.inbox.knowledge_many", "info", actions.Knowledge}, {"overview.inbox.components_many", "info", actions.System},
		{"overview.inbox.proposal", "info", actions.Evolve}, {"overview.inbox.eval_many", "info", actions.Eval},
	}
	if len(in) != len(wants) {
		t.Fatalf("%d dòng, muốn %d: %+v", len(in), len(wants), in)
	}
	for i, w := range wants {
		if in[i].Key != w.key || in[i].Target != w.target || in[i].Level != w.level {
			t.Errorf("dòng %d: %s %s → %s, muốn %s %s → %s", i, in[i].Level, in[i].Key, in[i].Target, w.level, w.key, w.target)
		}
	}
	if f := in[4].Filter; f == nil || f.Batch != "" || len(f.Statuses) != 2 || in[4].Count != 10 {
		t.Errorf("dòng tri thức chờ duyệt gộp: %+v", in[4])
	}
	if in[5].Count != 2 || in[7].Count != 2 || in[5].SystemTab != "components" {
		t.Errorf("dòng gộp thành phần/ứng viên phải có số đếm: %+v %+v", in[5], in[7])
	}
	if got := strings.Join(in[2].Commands, ";"); got != "bint task show 01TASK1" || in[2].TaskID != "01TASK1" {
		t.Errorf("lệnh tác vụ: %s", got)
	}
	if got := strings.Join(in[3].Commands, ";"); got != "bint doctor;bint init --update" || in[3].SystemTab != "doctor" {
		t.Errorf("doctor mẫu cũ phải gợi ý init --update và mở thẻ Doctor: %s / %q", got, in[3].SystemTab)
	}
	if !strings.Contains(in[1].Detail, "proposals") {
		t.Errorf("nguồn lỗi phải là một dòng lỗi riêng: %+v", in[1])
	}
	// Nguồn lỗi không làm mất nguồn khác: đủ dòng của năm nguồn còn lại.
	out := plain(newModel(t, Options{Mode: theme.Mono}, d, 140, 40).Render())
	for _, want := range []string{"10 mục tri thức chờ duyệt", "2 thành phần harness chờ duyệt", "Tác vụ bị chặn — Thêm cột email",
		"Đề xuất tiến hoá chờ duyệt — create 01PROP1", "2 ứng viên đánh giá chờ duyệt", "Doctor cảnh báo", "Doctor báo lỗi", "Không đọc được nguồn việc"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
}

func TestInboxEnterOnBatchSetsKnowledgeFilter(t *testing.T) {
	d := fullPending()
	d.Pending.KnowledgeBatches, d.Pending.KnowledgeOther = d.Pending.KnowledgeBatches[1:], 0 // một lô: dòng riêng của lô
	m := newModel(t, Options{Mode: theme.Mono}, d, 140, 40)
	pickInbox(t, m, "overview.inbox.batch", 0)
	if m.Tab() != 1 {
		t.Fatalf("tab = %d", m.Tab())
	}
	out := plain(m.Render())
	if !strings.Contains(out, "chờ duyệt · nguon:lo-hai") {
		t.Fatalf("bộ lọc phải đặt sẵn:\n%s", out)
	}
	f := m.kn.Filter()
	if f.Batch != "nguon:lo-hai" || len(f.Statuses) != 2 {
		t.Fatalf("%+v", f)
	}
}

// Enter trên dòng tác vụ bị chặn của hộp thư mở thẳng màn Tác vụ tại tác vụ đó; các dòng tab chưa làm vẫn mở trang
// giữ chỗ kèm lệnh CLI.
func TestInboxEnterOnBlockedTaskOpensTasksScreenAtThatTask(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, fullPending(), 140, 40)
	pickInbox(t, m, "overview.inbox.task", 0)
	if m.Tab() != 2 {
		t.Fatalf("tab = %d, muốn 2 (Tác vụ)", m.Tab())
	}
	if c := m.tk.Cursor(); c == nil || c.State.ID != "01TASK1" {
		t.Fatalf("con trỏ phải ở tác vụ của dòng hộp thư: %+v", c)
	}
	out := plain(m.Render())
	if !strings.Contains(out, "Thêm cột email") || !strings.Contains(out, "Chi tiết") {
		t.Fatalf("màn Tác vụ phải hiện tác vụ đó:\n%s", out)
	}
	press(t, m, "1")
	pickInbox(t, m, "overview.inbox.doctor_fail", 0) // dòng Doctor → Hệ thống, thẻ Doctor
	if m.Tab() != 6 || m.sy.Tab() != syscreen.TabDoctor {
		t.Fatalf("tab = %d, thẻ = %d, muốn 6 và Doctor", m.Tab(), m.sy.Tab())
	}
	if out := plain(m.Render()); !strings.Contains(out, "Doctor: 1 cảnh báo, 1 lỗi") || !strings.Contains(out, "hook claude") {
		t.Fatalf("thẻ Doctor:\n%s", out)
	}
}

// Phím Tab: ở Tổng quan đổi tab, ở Tri thức đổi khung; [ ] 1-7 luôn đổi tab; q trong ô lọc là chữ.
func TestKeyRoutingForKnowledgeScreen(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 140, 40)
	press(t, m, "tab")
	if m.Tab() != 1 {
		t.Fatalf("Tab ở Tổng quan phải đổi tab: %d", m.Tab())
	}
	press(t, m, "tab")
	if m.Tab() != 1 {
		t.Fatalf("Tab ở Tri thức đổi khung, không đổi tab: %d", m.Tab())
	}
	press(t, m, "]")
	if m.Tab() != 2 {
		t.Fatalf("] luôn đổi tab: %d", m.Tab())
	}
	press(t, m, "2", "[")
	if m.Tab() != 0 {
		t.Fatalf("[ luôn đổi tab: %d", m.Tab())
	}
	press(t, m, "2", "/", "q", "?")
	if m.quitting || m.showHelp || m.Tab() != 1 {
		t.Fatal("q và ? trong ô lọc là chữ")
	}
	if !strings.Contains(plain(m.Render()), "“q?”") {
		t.Fatalf("ô lọc phải nhận chữ:\n%s", plain(m.Render()))
	}
	press(t, m, "esc", "q")
	if !m.quitting {
		t.Fatal("hết ô nhập thì q thoát")
	}
}

// Mọi hành động Writes gửi qua RequestMsg ở --read-only không bao giờ tới Do; hành động chỉ-người trong
// phiên agent cũng không. Hành động đọc thì tới.
func TestReadOnlyAndAgentNeverReachDo(t *testing.T) {
	var mu sync.Mutex
	called := map[string]int{}
	var list []actions.Action
	for _, a := range actions.Default().All() {
		if a.Do != nil {
			id := a.ID
			a.Do = func(_ context.Context, _ actions.Env, o any) tea.Msg {
				mu.Lock()
				called[id]++
				mu.Unlock()
				return actions.ResultMsg{ID: id, Opts: o}
			}
		}
		list = append(list, a)
	}
	reg := actions.NewRegistry(list)
	writes, humans, reads := 0, 0, 0
	for _, a := range reg.All() {
		if a.Do == nil {
			continue
		}
		for _, mode := range []Options{{ReadOnly: true}, {Agent: true}, {}} {
			mode.Registry, mode.Mode = reg, theme.Mono
			m := newModel(t, mode, sampleData(), 100, 30)
			_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
			drive(t, m, cmd)
			want := 1
			switch {
			case mode.ReadOnly && a.Writes, mode.Agent && a.HumanOnly:
				want = 0
			}
			if got := called[a.ID]; got != want {
				t.Errorf("%s (ReadOnly=%v Agent=%v): Do được gọi %d lần, muốn %d", a.ID, mode.ReadOnly, mode.Agent, got, want)
			}
			called[a.ID] = 0
		}
		switch {
		case a.Writes:
			writes++
		case a.HumanOnly:
			humans++
		default:
			reads++
		}
	}
	if writes < 10 || reads < 5 {
		t.Fatalf("quét thiếu hành động: ghi=%d chỉ-người=%d đọc=%d", writes, humans, reads)
	}
}

// Onboarding: NavMsg có KnowledgeSub mở đúng thẻ phụ của màn Tri thức (bước tiếp theo của báo cáo init → thẻ Nhập).
func TestNavMsgOpensKnowledgeImportTab(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 120, 36)
	_, cmd := m.Update(overview.NavMsg{Screen: actions.Knowledge, KnowledgeSub: "import"})
	drive(t, m, cmd)
	if m.Tab() != 1 || m.kn.SubTab() != "Nhập" {
		t.Fatalf("phải mở thẻ Nhập của màn Tri thức: tab=%d thẻ=%q", m.Tab(), m.kn.SubTab())
	}
}

// Mọi hành động ghi mới khai báo Writes (chỉ đọc ẩn được) — các hành động của tầng dịch vụ chỉ-dành-cho-người
// còn khai báo HumanOnly để TUI khoá kèm lý do.
func TestWriteActionsAreDeclared(t *testing.T) {
	humanOnly := []string{"knowledge.approve", "knowledge.approve_refresh", "knowledge.reject", "knowledge.deprecate", "knowledge.approve_batch",
		"knowledge.reject_batch", "knowledge.verify_apply", "knowledge.revalidate", "knowledge.import"}
	writes := append([]string{"knowledge.edit", "knowledge.add", "knowledge.rm", "bootstrap.publish", "bootstrap"}, humanOnly...)
	reg := actions.Default()
	for _, id := range writes {
		a, ok := reg.ByID(id)
		if !ok || !a.Writes {
			t.Errorf("%s phải có Writes = true (ok=%v)", id, ok)
		}
	}
	for _, id := range humanOnly {
		if a, _ := reg.ByID(id); !a.HumanOnly {
			t.Errorf("%s phải HumanOnly", id)
		}
	}
	for _, id := range []string{"knowledge.search", "knowledge.stats", "knowledge.diff", "knowledge.verify", "bootstrap.publish_preview", "knowledge.import_preview", "inspect", "context", "knowledge.plan_batch"} {
		if a, _ := reg.ByID(id); a.Writes {
			t.Errorf("%s là hành động đọc, không được Writes", id)
		}
	}
	// Trong phiên agent, hành động chỉ-người hiện khoá kèm lý do ở bảng lệnh; ở chế độ chỉ đọc thì ẩn.
	m := newModel(t, Options{Mode: theme.Mono, Agent: true}, sampleData(), 120, 36)
	press(t, m, ":")
	for _, c := range "duyệt mục" {
		press(t, m, string(c))
	}
	if out := plain(m.Render()); !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("hành động khoá phải kèm lý do:\n%s", out)
	}
	m = newModel(t, Options{Mode: theme.Mono, ReadOnly: true}, sampleData(), 120, 36)
	press(t, m, ":")
	for _, c := range "xoá tệp" {
		press(t, m, string(c))
	}
	if out := plain(m.Render()); strings.Contains(out, "Xoá tệp của mục candidate") {
		t.Fatalf("chỉ đọc phải ẩn hành động ghi:\n%s", out)
	}
}

// Trong phiên agent phím duyệt báo khoá ngay và không gửi gì; --read-only ẩn thao tác ghi.
func TestKeysGatedInAgentAndReadOnlyModes(t *testing.T) {
	for name, o := range map[string]Options{"agent": {Agent: true}, "chỉ đọc": {ReadOnly: true}} {
		o.Mode = theme.Mono
		m := newModel(t, o, sampleData(), 140, 40)
		press(t, m, "2", "a", "A", "r")
		if o.ReadOnly {
			press(t, m, "n") // thêm mục là ghi; trong phiên agent thì được (mục luôn là candidate)
		}
		if m.kn.Capturing() {
			t.Fatalf("%s: không được mở hộp/form", name)
		}
		out := plain(m.Render())
		want := "chỉ dành cho người"
		if o.ReadOnly {
			want = "chỉ đọc"
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s: thiếu thông báo %q:\n%s", name, want, out)
		}
	}
}

// Sửa trong $EDITOR: lệnh soạn thảo giả được gọi đúng với đường dẫn tệp của mục.
func TestEditUsesEditorWithItemPath(t *testing.T) {
	var got []string
	m := newModel(t, Options{Mode: theme.Mono, Editor: func(path string) *exec.Cmd {
		got = append(got, path)
		return exec.Command("true")
	}}, sampleData(), 140, 40)
	press(t, m, "2", "e")
	if len(got) != 1 || got[0] != "/work/demo/.bint/knowledge/items/01KNOW000001.md" {
		t.Fatalf("trình soạn thảo được gọi với %v", got)
	}
}

// Huỷ Thẩm định lại huỷ context của việc nền.
func TestCancelRevalidateCancelsBackgroundContext(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	var list []actions.Action
	for _, a := range actions.Default().All() {
		if a.ID == "knowledge.revalidate" {
			a.Do = func(ctx context.Context, _ actions.Env, o any) tea.Msg {
				close(started)
				<-ctx.Done()
				close(cancelled)
				return actions.ResultMsg{ID: "knowledge.revalidate", Opts: o, Err: ctx.Err()}
			}
		}
		list = append(list, a)
	}
	m := newModel(t, Options{Mode: theme.Mono, Registry: actions.NewRegistry(list)}, sampleData(), 140, 40)
	_, cmd := m.Update(actions.RequestMsg{ID: "knowledge.revalidate", Opts: bintapp.RevalidateOptions{}})
	done := make(chan tea.Msg, 1)
	go func() {
		// cmd là Batch(Do, tick): chạy hàm Do trong goroutine riêng.
		batch, _ := cmd().(tea.BatchMsg)
		for _, c := range batch {
			if c == nil {
				continue
			}
			if msg := c(); msg != nil {
				if _, isRes := msg.(actions.ResultMsg); isRes {
					done <- msg
				}
			}
		}
		done <- nil
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("Do chưa được gọi")
	}
	if m.jobs.Len() != 1 {
		t.Fatalf("việc nền phải được ghi nhận: %d", m.jobs.Len())
	}
	m.Update(actions.CancelMsg{ID: "knowledge.revalidate"})
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("context của việc nền chưa bị huỷ")
	}
	for r := range done {
		if r != nil {
			m.Update(r)
			break
		}
	}
	if m.jobs.Len() != 0 {
		t.Fatal("kết quả về thì xoá việc nền")
	}
}

// Mốc giai đoạn 1: duyệt 30 mục của một lần /b-knowledge (nhãn nguon:lo-thu) từ hộp thư bằng không quá 5 phím,
// không gõ chữ; mục lỗi bằng chứng cùng lô vẫn candidate. Chạy với dịch vụ thật trên repository tạm.
func TestMilestoneApproveThirtyItemsFromInbox(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	files := map[string]string{"README.md": "# demo\n", "docs/gone.md": "một\nhai\nba\n"}
	for i := 0; i < 30; i++ {
		files[fileName(i)] = "package src\n\nfunc F() {}\n"
	}
	root := testutil.NewRepo(t, files)
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	add := func(i int, evidence string) string {
		res, err := bintapp.AddKnowledge(ctx, k, bintapp.AddOptions{Type: knowledge.TypeFact, Title: titleOf(i), Body: "Nội dung riêng của mục " + titleOf(i),
			Paths: []string{"src/**"}, Evidence: []string{evidence}, Origin: knowledge.OriginAgent, ThirdParty: true, Tags: []string{"nguon:lo-thu"}})
		if err != nil || !res.Saved {
			t.Fatalf("mục %d: %v %+v", i, err, res.Check.Findings)
		}
		return res.Item.ID
	}
	var good []string
	for i := 0; i < 30; i++ {
		good = append(good, add(i, fileName(i)+":3"))
	}
	bad := add(30, "docs/gone.md:1-2")
	if err := removeFile(root, "docs/gone.md"); err != nil {
		t.Fatal(err)
	}

	m := New(ctx, Options{Dir: root, Version: "t", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond})
	m.SetSize(140, 40)
	drive(t, m, m.Init())
	keys := 0
	press1 := func(k string) {
		keys++
		press(t, m, k)
	}
	keys++
	pickInbox(t, m, "overview.inbox.batch", 0) // dòng lô nguon:lo-thu của hộp thư
	if m.Tab() != 1 || m.kn.Filter().Batch != "nguon:lo-thu" {
		t.Fatalf("Enter phải tới màn Tri thức với bộ lọc lô: tab=%d filter=%+v\n%s", m.Tab(), m.kn.Filter(), plain(m.Render()))
	}
	press1("A") // duyệt cả lô: lập kế hoạch rồi hỏi xác nhận
	out := plain(m.Render())
	if !strings.Contains(out, "Duyệt 30 mục?") || !strings.Contains(out, "1 mục bị loại khỏi lô") || !strings.Contains(out, "không qua kiểm tra") {
		t.Fatalf("hộp xác nhận phải nêu 30 mục và liệt kê mục lỗi:\n%s", out)
	}
	press1("enter")
	if keys > 5 {
		t.Fatalf("dùng %d phím, muốn ≤ 5", keys)
	}
	items, _, err := k.Items()
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, it := range items {
		status[it.ID] = it.Status
	}
	for _, id := range good {
		if status[id] != knowledge.StatusVerified {
			t.Fatalf("%s: %s, muốn verified", id, status[id])
		}
	}
	if status[bad] != knowledge.StatusCandidate {
		t.Fatalf("mục lỗi phải còn candidate: %s", status[bad])
	}
	if !strings.Contains(plain(m.Render()), "Đã duyệt 30 mục") {
		t.Fatalf("thiếu thông báo kết quả:\n%s", plain(m.Render()))
	}
}

// Dòng hộp thư "tri thức còn lại" đếm mục không thuộc lô nào: Enter phải mở danh sách chỉ gồm các mục đó
// (không lẫn mục của lô), và A không chọn gì lập lô đúng bằng số ở dòng hộp thư.
func TestInboxOtherKnowledgeFilterExcludesBatchItems(t *testing.T) {
	d := fullPending()
	d.Pending.KnowledgeBatches = nil // chỉ còn dòng "không thuộc lô nào" (nhiều dòng tri thức sẽ gộp)
	m := newModel(t, Options{Mode: theme.Mono}, d, 140, 40)
	pickInbox(t, m, "overview.inbox.knowledge", 0)
	f := m.kn.Filter()
	if !f.NoBatch || f.Batch != "" || len(f.Statuses) != 2 {
		t.Fatalf("%+v", f)
	}
	for _, it := range m.kn.Visible() {
		if it.Scope.Batch() != "" {
			t.Errorf("mục %s thuộc lô %s lọt vào dòng 'không thuộc lô nào'", it.ID, it.Scope.Batch())
		}
	}
	if out := plain(m.Render()); !strings.Contains(out, "không thuộc lô") {
		t.Fatalf("thanh lọc phải nêu điều kiện:\n%s", out)
	}
}

// collectMsgs chạy cmd (và các cmd con của tea.Batch) rồi trả mọi thông điệp, không đưa lại vào Update.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// Form nhúng trong thẻ phụ Tri thức: chưa có tiêu điểm thì chân form không bấm được; có tiêu điểm thì Esc ở chân chỉ thoát form
// (vẫn ở thẻ phụ) và Enter ở chân gửi form đúng một lần.
func TestKnowledgeSubFormFooterMouse(t *testing.T) {
	requests := func(cmd tea.Cmd) int {
		n := 0
		for _, msg := range collectMsgs(cmd) {
			if r, ok := msg.(actions.RequestMsg); ok && r.ID == "knowledge.search" {
				n++
			}
		}
		return n
	}
	subOpen := func(m *Model) bool { return strings.Contains(plain(m.Render()), "Nhập truy vấn") }

	t.Run("chưa có tiêu điểm: chân form không có vùng bấm", func(t *testing.T) {
		m := mouseModel(t, 140, 40, "2", ".", "esc") // thẻ phụ Tìm kiếm mở form có tiêu điểm; Esc rời form, vẫn ở thẻ phụ
		if !subOpen(m) || strings.Contains(plain(m.Render()), "Enter gửi  Esc Thoát") {
			t.Fatalf("phải ở thẻ phụ và form chưa có tiêu điểm:\n%s", plain(m.Render()))
		}
		for _, label := range []string{"Esc thoát", "Enter gửi"} {
			x, y := cellOf(t, m, label)
			if cmd := clickMsgs(m, x+1, y); len(collectMsgs(cmd)) != 0 || requests(cmd) != 0 {
				t.Fatalf("bấm %q ở chân form chưa có tiêu điểm không được làm gì", label)
			}
			if !subOpen(m) || m.Screen() != actions.Knowledge {
				t.Fatalf("bấm %q làm rời thẻ phụ:\n%s", label, plain(m.Render()))
			}
		}
	})

	t.Run("có tiêu điểm: Esc ở chân chỉ thoát form", func(t *testing.T) {
		m := mouseModel(t, 140, 40, "2", ".")
		x, y := cellOf(t, m, "Esc thoát")
		clickMsgs(m, x+1, y)
		if m.Screen() != actions.Knowledge || !subOpen(m) {
			t.Fatalf("Esc ở chân form phải chỉ thoát form, vẫn ở thẻ phụ:\n%s", plain(m.Render()))
		}
		if strings.Contains(plain(m.Render()), "Enter gửi  Esc Thoát") {
			t.Fatal("form phải mất tiêu điểm")
		}
	})

	t.Run("có tiêu điểm: Enter ở chân gửi form một lần", func(t *testing.T) {
		for _, label := range []string{"Enter gửi", "Enter gửi  Esc Thoát"} {
			m := mouseModel(t, 140, 40, "2", ".", "a", "b")
			x, y := cellOf(t, m, label)
			cmd := clickMsgs(m, x+1, y)
			if n := requests(cmd); n != 1 {
				t.Fatalf("bấm %q: gửi %d lần, muốn 1", label, n)
			}
		}
	})
}
