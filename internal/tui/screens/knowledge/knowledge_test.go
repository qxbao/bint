package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/app"
	"bint/internal/i18n"
	kn "bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func item(id, typ, status, title, origin string, third bool, paths []string, tags ...string) *kn.Item {
	return &kn.Item{ID: id, Type: typ, Status: status, Title: title, Body: "Nội dung " + title,
		Scope: kn.Scope{Paths: paths, Tags: tags}, Origin: kn.Origin{Kind: origin, ThirdParty: third}, Author: "T <t@x>",
		CreatedAt: "2026-11-11T00:00:00Z", Evidence: []kn.Evidence{{Path: "a.go", Lines: "1-3"}}, Path: "/r/.bint/knowledge/items/" + id + ".md"}
}

func sample() []*kn.Item {
	return []*kn.Item{
		item("01A", kn.TypeRule, kn.StatusCandidate, "Endpoint phải có /v1", kn.OriginAgent, true, []string{"api/**"}, "nguon:lo-1"),
		item("01B", kn.TypeWarning, kn.StatusCandidate, "Không gọi sql.Open", kn.OriginAgent, true, []string{"internal/db/**"}, "nguon:lo-1", "db"),
		item("01C", kn.TypePattern, kn.StatusVerified, "Handler đi qua wrap()", kn.OriginHuman, false, nil),
		item("01D", kn.TypeFact, kn.StatusStale, "Cổng mặc định 8080", kn.OriginAgent, false, []string{"cmd/**"}, "nguon:lo-2"),
		item("01E", kn.TypeLesson, kn.StatusCandidate, "Chạy go vet GOOS=windows", kn.OriginImported, false, nil, "nguon:lo-2"),
		item("01F", kn.TypeDecision, kn.StatusDeprecated, "Dùng bbolt", kn.OriginHuman, false, nil),
	}
}

type harness struct {
	t    *testing.T
	m    *Model
	reqs []actions.RequestMsg
	cat  *i18n.Catalog
	th   theme.Theme
}

func newHarness(t *testing.T, items []*kn.Item, o Options) *harness {
	t.Helper()
	if o.Dir == "" {
		o.Dir = "/r"
	}
	o.Loader = func(context.Context, string) (Data, error) {
		return Data{Items: items, Files: []string{"a.go", "internal/db/db.go"}, Stamp: time.Now()}, nil
	}
	o.Detailer = func(_ context.Context, _ string, id string) (app.KnowledgeDetail, error) {
		for _, it := range items {
			if it.ID == id {
				return app.KnowledgeDetail{Item: it, Evidence: []app.EvidenceView{{Evidence: it.Evidence[0], Lines: []string{"   1  package a"}}}}, nil
			}
		}
		return app.KnowledgeDetail{}, errors.New("không có")
	}
	h := &harness{t: t, m: New(o), cat: i18n.MustLoad("vi"), th: theme.New(theme.Mono)}
	h.m.SetEnv(h.th, h.cat)
	h.run(h.m.Load())
	return h
}

// run chạy tea.Cmd đồng bộ: yêu cầu hành động được ghi lại thay vì chạy; thông điệp khác vào Update.
func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	for i := 0; cmd != nil && i < 50; i++ {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				h.run(c)
			}
			return
		case actions.RequestMsg:
			h.reqs = append(h.reqs, msg)
			return
		case actions.CancelMsg:
			h.reqs = append(h.reqs, actions.RequestMsg{ID: "cancel:" + msg.ID})
			return
		default:
			cmd = h.m.Update(msg)
		}
	}
}

func (h *harness) key(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		cmd, _ := h.m.Key(keyMsg(k))
		h.run(cmd)
	}
}

func (h *harness) typ(s string) {
	for _, c := range s {
		h.key(string(c))
	}
}

func (h *harness) lastReq() actions.RequestMsg {
	h.t.Helper()
	if len(h.reqs) == 0 {
		h.t.Fatal("chưa có yêu cầu hành động nào")
	}
	return h.reqs[len(h.reqs)-1]
}

func (h *harness) result(id string, opts, value any, err error) {
	h.run(h.m.Update(actions.ResultMsg{ID: id, Opts: opts, Value: value, Err: err}))
}

func (h *harness) env(w, ht int) ui.Env {
	return ui.Env{Th: h.th, Cat: h.cat, W: w, H: ht, Spin: "⠋"}
}

func (h *harness) view(w, ht int) string { return ansi.Strip(h.m.View(h.env(w, ht))) }

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func ids(items []*kn.Item) string {
	var s []string
	for _, it := range items {
		s = append(s, it.ID)
	}
	return strings.Join(s, ",")
}

// Lọc theo từng chiều và tổ hợp: trạng thái, loại, nguồn, cờ bên thứ ba, nhãn lô, đường dẫn, chữ.
func TestFilterEachDimensionAndCombinations(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	if got := ids(h.m.Visible()); got != "01A,01B,01C,01D,01E" {
		t.Fatalf("mặc định ẩn mục deprecated: %s", got)
	}
	cases := []struct {
		name string
		f    kn.Filter
		want string
	}{
		{"chờ duyệt", kn.Filter{Statuses: []string{kn.StatusCandidate, kn.StatusStale}}, "01A,01B,01D,01E"},
		{"verified", kn.Filter{Statuses: []string{kn.StatusVerified}}, "01C"},
		{"deprecated hiện khi chọn rõ", kn.Filter{Statuses: []string{kn.StatusDeprecated}}, "01F"},
		{"loại", kn.Filter{Types: []string{kn.TypeRule}}, "01A"},
		{"nguồn agent", kn.Filter{Origins: []string{kn.OriginAgent}}, "01A,01B,01D"},
		{"nguồn imported", kn.Filter{Origins: []string{kn.OriginImported}}, "01E"},
		{"bên thứ ba ⚑", kn.Filter{ThirdParty: true}, "01A,01B"},
		{"nhãn lô", kn.Filter{Batch: "nguon:lo-2"}, "01D,01E"},
		{"nhãn lô dạng rút gọn", kn.Filter{Batch: "lo-1"}, "01A,01B"},
		{"đường dẫn", kn.Filter{Path: "internal/db/db.go"}, "01B,01C,01E"},
		{"đường dẫn khác", kn.Filter{Path: "api/users.go"}, "01A,01C,01E"},
		{"chữ", kn.Filter{Text: "wrap"}, "01C"},
		{"tổ hợp: chờ duyệt + lô + nguồn agent", kn.Filter{Statuses: []string{kn.StatusCandidate, kn.StatusStale}, Batch: "nguon:lo-2", Origins: []string{kn.OriginAgent}}, "01D"},
		{"tổ hợp loại trừ nhau", kn.Filter{Types: []string{kn.TypeRule}, Origins: []string{kn.OriginHuman}}, ""},
	}
	for _, c := range cases {
		h.run(h.m.Focus(c.f))
		if got := ids(h.m.Visible()); got != c.want {
			t.Errorf("%s: %q, muốn %q", c.name, got, c.want)
		}
	}
}

// Ô lọc nhanh `/`: chữ q là chữ (không thoát), Enter giữ bộ lọc, Esc trả về giá trị cũ.
func TestTextFilterCapturesKeys(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("/")
	if !h.m.Capturing() {
		t.Fatal("ô lọc phải nhận mọi phím")
	}
	h.typ("sql")
	if got := ids(h.m.Visible()); got != "01B" {
		t.Fatalf("lọc theo chữ: %s", got)
	}
	h.key("enter")
	if h.m.Capturing() || ids(h.m.Visible()) != "01B" {
		t.Fatal("Enter thoát ô nhập và giữ bộ lọc")
	}
	h.key("/")
	h.typ("q")
	if got := ids(h.m.Visible()); got != "" || !h.m.Capturing() {
		t.Fatalf("q là chữ trong ô nhập: %q capturing=%v", got, h.m.Capturing())
	}
	h.key("esc")
	if got := ids(h.m.Visible()); got != "01B" || h.m.Capturing() {
		t.Fatalf("Esc trả lại bộ lọc cũ: %q", got)
	}
	h.key("c")
	if got := ids(h.m.Visible()); got != "01A,01B,01C,01D,01E" {
		t.Fatalf("c bỏ bộ lọc: %s", got)
	}
}

// Form lọc (F) sinh từ FilterOptions: chọn trạng thái "chờ duyệt" và nhập nhãn lô.
func TestFilterFormAppliesFilter(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("F")
	if h.m.frm == nil {
		t.Fatal("F phải mở form lọc")
	}
	h.m.frm.FocusField("status")
	h.key("right") // (trống) → pending
	h.m.frm.FocusField("batch")
	h.typ("nguon:lo-1")
	h.key("ctrl+s")
	if h.m.frm != nil || ids(h.m.Visible()) != "01A,01B" {
		t.Fatalf("frm=%v visible=%s", h.m.frm != nil, ids(h.m.Visible()))
	}
	if chips := h.m.filterChips(); chips != "chờ duyệt · nguon:lo-1" {
		t.Fatalf("chips = %q", chips)
	}
	if !strings.Contains(h.view(140, 30), "chờ duyệt · nguon:lo-1") {
		t.Fatal("thanh tiêu đề khung phải hiện bộ lọc đang bật")
	}
}

// Danh sách 5 000 mục chỉ vẽ các hàng nằm trong vùng nhìn.
func TestListDrawsOnlyVisibleRows(t *testing.T) {
	var items []*kn.Item
	for i := 0; i < 5000; i++ {
		items = append(items, item(fmt.Sprintf("01X%05d", i), kn.TypeFact, kn.StatusCandidate, fmt.Sprintf("Mục số %d", i), kn.OriginAgent, false, nil))
	}
	h := newHarness(t, items, Options{})
	const w, ht = 140, 30
	for _, step := range [][]string{nil, {"j", "j"}, {"G"}, {"g"}, {"pgdown", "pgdown"}} {
		h.key(step...)
		h.m.RowsDrawn = 0
		out := h.view(w, ht)
		if h.m.RowsDrawn == 0 || h.m.RowsDrawn > ht {
			t.Fatalf("sau %v: vẽ %d hàng, chiều cao %d", step, h.m.RowsDrawn, ht)
		}
		if !strings.Contains(out, "5000/5000") {
			t.Fatalf("tiêu đề phải nêu 5000/5000:\n%s", out)
		}
	}
	h.key("G")
	if h.m.Cursor().ID != "01X04999" {
		t.Fatalf("G phải tới hàng cuối: %s", h.m.Cursor().ID)
	}
}

// Rộng ≥ 100 cột: hai khung; hẹp hơn: một khung, Enter/Tab đổi sang chi tiết, Esc về danh sách.
func TestLayoutWideAndNarrow(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	wide := h.view(140, 30)
	if !strings.Contains(wide, "Danh sách (5/6)") || !strings.Contains(wide, "Chi tiết") || !strings.Contains(wide, "Endpoint phải có /v1") {
		t.Fatalf("khung rộng:\n%s", wide)
	}
	narrow := h.view(80, 24)
	if strings.Contains(narrow, "Chi tiết") {
		t.Fatalf("khung hẹp chỉ hiện danh sách:\n%s", narrow)
	}
	h.key("enter")
	narrow = h.view(80, 24)
	if !strings.Contains(narrow, "Chi tiết") || strings.Contains(narrow, "Danh sách (") || !strings.Contains(narrow, "Nội dung") {
		t.Fatalf("Enter mở chi tiết:\n%s", narrow)
	}
	h.key("esc")
	if !strings.Contains(h.view(80, 24), "Danh sách (") {
		t.Fatal("Esc về danh sách")
	}
	h.key("tab")
	if !strings.Contains(h.view(80, 24), "Chi tiết") {
		t.Fatal("Tab đổi khung")
	}
}

// Chi tiết gồm siêu dữ liệu, nội dung, bằng chứng kèm trích đoạn và kết quả kiểm tra (mã dịch, chi tiết nguyên văn).
func TestDetailShowsEvidenceAndChecks(t *testing.T) {
	items := sample()
	h := newHarness(t, items, Options{})
	out := h.view(140, 40)
	for _, want := range []string{"01A", "rule · candidate · agent ⚑", "Phạm vi", "api/**", "nguon:lo-1", "Nội dung", "Bằng chứng", "a.go:1-3", "package a", "Kiểm tra", "0 lỗi"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	// Mục có finding lỗi: nhãn dịch + chi tiết nguyên văn.
	h.m.cache["01B"] = app.KnowledgeDetail{Item: items[1], Evidence: []app.EvidenceView{{Evidence: items[1].Evidence[0], Missing: true}},
		Check: kn.Result{Findings: []kn.Finding{{Level: kn.LevelError, Code: kn.CodeEvidence, Detail: "a.go không tồn tại"}}}}
	h.m.asked["01B"] = true
	h.key("j")
	out = h.view(140, 40)
	for _, want := range []string{"Bằng chứng không còn", "a.go không tồn tại", "(không còn tệp)", "1 lỗi", "chi tiết kỹ thuật"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
}

func TestSelectionAndBatchScope(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.run(h.m.Focus(kn.Filter{Statuses: []string{kn.StatusCandidate, kn.StatusStale}}))
	h.key("x", "x") // chọn 01A, 01B; con trỏ xuống 01D
	if got := strings.Join(h.m.Selected(), ","); got != "01A,01B" {
		t.Fatalf("đã chọn %s", got)
	}
	h.key("A")
	if r := h.lastReq(); r.ID != "knowledge.plan_batch" || strings.Join(r.Opts.(app.BatchOptions).Refs, ",") != "01A,01B" || r.Opts.(app.BatchOptions).To != kn.StatusVerified {
		t.Fatalf("chọn nhiều: %+v", r)
	}
	h.key("esc") // bỏ chọn
	if h.m.sel.Count() != 0 {
		t.Fatal("Esc bỏ chọn")
	}
	h.key("A") // không chọn: cả danh sách đang lọc
	if got := strings.Join(h.lastReq().Opts.(app.BatchOptions).Refs, ","); got != "01A,01B,01D,01E" {
		t.Fatalf("cả lô: %s", got)
	}
	h.key("X")
	if h.m.sel.Count() != 4 {
		t.Fatalf("X chọn cả danh sách đang lọc: %d", h.m.sel.Count())
	}
	h.key("X")
	if h.m.sel.Count() != 0 {
		t.Fatal("X lần nữa bỏ chọn tất cả")
	}
	h.key("R")
	if r := h.lastReq(); r.Opts.(app.BatchOptions).To != kn.StatusDeprecated {
		t.Fatalf("%+v", r)
	}
}

// Hộp xác nhận lô nêu số mục, liệt kê mục bị loại do lỗi kiểm tra; chỉ mục Ready được gửi đi duyệt.
func TestBatchDialogListsBlockedAndSendsOnlyReady(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	plan := app.BatchPlan{To: kn.StatusVerified, Ready: []string{"01A"}, Blocked: []app.BatchBlocked{
		{ID: "01B", Reason: app.BlockedChecks, Findings: []kn.Finding{{Level: kn.LevelError, Code: kn.CodeFingerprint, Detail: "đổi"}}},
		{ID: "01D", Reason: app.BlockedAlreadyDone}}}
	h.result("knowledge.plan_batch", nil, plan, nil)
	if h.m.dlg == nil {
		t.Fatal("phải mở hộp xác nhận")
	}
	out := h.view(100, 30)
	for _, want := range []string{"Duyệt 1 mục?", "Sẽ ghi 1 mục.", "2 mục bị loại khỏi lô", "01B", "không qua kiểm tra: Bằng chứng đã thay đổi", "01D", "đã ở trạng thái đích"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	h.key("esc")
	if h.m.dlg != nil || len(h.reqs) != 0 {
		t.Fatal("Esc huỷ, không gửi gì")
	}
	h.result("knowledge.plan_batch", nil, plan, nil)
	h.key("enter")
	r := h.lastReq()
	bo, _ := r.Opts.(app.BatchOptions)
	if r.ID != "knowledge.approve_batch" || strings.Join(bo.Refs, ",") != "01A" {
		t.Fatalf("%+v", r)
	}
	// Không còn mục nào duyệt được: hộp chỉ để đọc.
	h.reqs = nil
	h.result("knowledge.plan_batch", nil, app.BatchPlan{To: kn.StatusVerified, Blocked: plan.Blocked}, nil)
	if !strings.Contains(h.view(100, 30), "Không có mục nào duyệt được.") {
		t.Fatal("thiếu thông báo")
	}
	h.key("enter")
	if len(h.reqs) != 0 || h.m.dlg != nil {
		t.Fatal("hộp chỉ-đọc đóng mà không gửi gì")
	}
}

// Từ chối lô cần lý do; mục có lỗi kiểm tra không bị chặn.
func TestRejectBatchAsksReason(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.result("knowledge.plan_batch", nil, app.BatchPlan{To: kn.StatusDeprecated, Ready: []string{"01A", "01B"}}, nil)
	h.key("enter")
	if h.m.dlg == nil {
		t.Fatal("thiếu lý do thì hộp vẫn mở")
	}
	h.typ("sai nguồn")
	h.key("enter")
	r := h.lastReq()
	bo, _ := r.Opts.(app.BatchOptions)
	if r.ID != "knowledge.reject_batch" || bo.Reason != "sai nguồn" || len(bo.Refs) != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestSingleActionsAndReasonDialogs(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("a")
	if r := h.lastReq(); r.ID != "knowledge.approve" || r.Opts.(app.ApproveOptions).Refs[0] != "01A" {
		t.Fatalf("%+v", r)
	}
	h.key("f")
	if r := h.lastReq(); r.ID != "knowledge.approve_refresh" {
		t.Fatalf("%+v", r)
	}
	h.key("r")
	h.key("enter") // reject: lý do mặc định
	if r := h.lastReq(); r.ID != "knowledge.reject" || r.Opts.(app.RejectOptions).Reason != "người duyệt từ chối" {
		t.Fatalf("%+v", r)
	}
	h.key("d")
	h.key("enter") // deprecate: lý do bắt buộc
	if h.m.dlg == nil {
		t.Fatal("deprecate thiếu lý do không được đóng")
	}
	h.typ("lỗi thời")
	h.key("enter")
	if r := h.lastReq(); r.ID != "knowledge.deprecate" || r.Opts.(app.RejectOptions).Reason != "lỗi thời" {
		t.Fatalf("%+v", r)
	}
	h.key("e")
	if r := h.lastReq(); r.ID != "knowledge.edit" || r.Opts.(actions.EditOptions).Path != "/r/.bint/knowledge/items/01A.md" {
		t.Fatalf("%+v", r)
	}
}

// Hành động bị khoá (phiên agent) hoặc ẩn (chỉ đọc) báo ngay, không mở hộp và không gửi yêu cầu.
func TestGatedActionsDoNotSendRequests(t *testing.T) {
	for name, av := range map[string]actions.Availability{"agent": actions.Locked, "chỉ đọc": actions.Hidden} {
		h := newHarness(t, sample(), Options{Gate: func(id string) actions.Availability {
			if strings.HasPrefix(id, "knowledge.") {
				return av
			}
			return actions.Available
		}})
		for _, k := range []string{"a", "f", "r", "d", "A", "R", "e", "n", "delete"} {
			h.key(k)
			if len(h.reqs) != 0 || h.m.dlg != nil || h.m.frm != nil {
				t.Fatalf("%s: phím %s đã gửi yêu cầu hoặc mở hộp (reqs=%v)", name, k, h.reqs)
			}
			if h.m.toast == "" {
				t.Fatalf("%s: phím %s không báo lý do", name, k)
			}
		}
		want := "chỉ dành cho người"
		if av == actions.Hidden {
			want = "chỉ đọc"
		}
		if !strings.Contains(h.m.toast, want) {
			t.Errorf("%s: thông báo %q thiếu %q", name, h.m.toast, want)
		}
	}
}

func TestAddFormSubmitsAndKeepsOpenOnFindings(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("n")
	if h.m.frm == nil || h.m.frmID != "knowledge.add" {
		t.Fatal("n mở form thêm")
	}
	h.m.frm.FocusField("body")
	h.typ("Mở SQLite bằng store.Open")
	h.m.frm.FocusField("evidence")
	h.typ("a.go:1")
	h.key("ctrl+s")
	r := h.lastReq()
	ao, _ := r.Opts.(app.AddOptions)
	if r.ID != "knowledge.add" || ao.Body != "Mở SQLite bằng store.Open" || ao.Type != "warning" || len(ao.Evidence) != 1 {
		t.Fatalf("%+v", r)
	}
	if !h.m.frmBusy {
		t.Fatal("form giữ trạng thái bận trong lúc chờ kết quả")
	}
	// Không qua kiểm tra: form mở lại kèm lý do.
	h.result("knowledge.add", ao, app.AddResult{Check: kn.Result{Findings: []kn.Finding{{Level: kn.LevelError, Code: kn.CodeEvidence, Detail: "a.go:1 không có"}}}}, nil)
	if h.m.frm == nil || h.m.frmBusy || !strings.Contains(h.m.frm.Err, "Bằng chứng không còn") {
		t.Fatalf("frm=%v err=%q", h.m.frm != nil, h.m.frm.Err)
	}
	// Ghi được: đóng form, thông báo, nạp lại.
	h.result("knowledge.add", ao, app.AddResult{Saved: true, Item: item("01Z", "warning", "verified", "x", "human", false, nil)}, nil)
	if h.m.frm != nil || !strings.Contains(h.m.toast, "01Z") {
		t.Fatalf("%v %q", h.m.frm != nil, h.m.toast)
	}
}

func TestVerifyFormChoosesApplyAction(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("v")
	h.key("ctrl+s")
	if r := h.lastReq(); r.ID != "knowledge.verify" || r.Opts.(app.VerifyOptions).Refs[0] != "01A" {
		t.Fatalf("%+v", r)
	}
	h.result("knowledge.verify", nil, app.VerifyReport{Results: []kn.Result{{ID: "01A", Findings: []kn.Finding{{Level: kn.LevelError, Code: kn.CodeSymbol, Detail: "Foo"}}}},
		Items: map[string]*kn.Item{"01A": sample()[0]}}, nil)
	out := h.view(100, 30)
	if !strings.Contains(out, "Kết quả kiểm tra") || !strings.Contains(out, "Ký hiệu không còn trong mã") || !strings.Contains(out, "Endpoint phải có /v1") {
		t.Fatalf("hiện kết quả từng mục:\n%s", out)
	}
	h.key("esc")
	h.key("v")
	h.m.frm.FocusField("apply")
	h.key("space")
	h.key("ctrl+s")
	if r := h.lastReq(); r.ID != "knowledge.verify_apply" || !r.Opts.(app.VerifyOptions).Apply {
		t.Fatalf("%+v", r)
	}
}

func TestRemoveFormAndResult(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("delete")
	h.key("ctrl+s")
	if r := h.lastReq(); r.ID != "knowledge.rm" || r.Opts.(app.RemoveOptions).Refs[0] != "01A" {
		t.Fatalf("%+v", r)
	}
	h.result("knowledge.rm", nil, []string{"01A"}, nil)
	if h.m.frm != nil || !strings.Contains(h.m.toast, "Đã xoá 1 mục") {
		t.Fatalf("%q", h.m.toast)
	}
}

func TestErrorsAreTranslated(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.result("knowledge.approve", nil, nil, fmt.Errorf("%w (`bint knowledge approve`)", app.ErrAgentForbidden))
	if !strings.Contains(h.m.toast, "chỉ dành cho người dùng") {
		t.Fatalf("%q", h.m.toast)
	}
	h.result("knowledge.approve", nil, nil, errors.New("đĩa đầy"))
	if !strings.Contains(h.m.toast, "chi tiết kỹ thuật") || !strings.Contains(h.m.toast, "đĩa đầy") {
		t.Fatalf("%q", h.m.toast)
	}
	h.result("knowledge.approve", nil, []app.ReviewOutcome{}, nil)
	h.result("knowledge.approve", nil, []app.ReviewOutcome{{ID: "01A", Err: errors.New("x"), Check: &kn.Result{Findings: []kn.Finding{{Code: kn.CodeFingerprint, Level: kn.LevelError}}}}}, nil)
	if !strings.Contains(h.m.toast, "Bằng chứng đã thay đổi") {
		t.Fatalf("%q", h.m.toast)
	}
}

func TestTransitionsTable(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("T")
	out := h.view(140, 40)
	if !strings.Contains(out, "Bảng chuyển trạng thái") || !strings.Contains(out, "candidate → verified") || !strings.Contains(out, "stale → verified") || !strings.Contains(out, "* → deprecated") {
		t.Fatalf("%s", out)
	}
	h.key("esc")
	if h.m.dlg != nil {
		t.Fatal("Esc đóng")
	}
}

// Mọi thẻ phụ vẽ vừa kích thước ở mọi ngôn ngữ và chế độ màu.
func TestEveryViewFitsEverywhere(t *testing.T) {
	for _, lang := range []string{"vi", "en", i18n.Pseudo} {
		for _, mode := range theme.Modes {
			for _, sz := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {140, 40}} {
				h := newHarness(t, sample(), Options{})
				h.cat, h.th = i18n.MustLoad(lang), theme.New(mode)
				h.m.SetEnv(h.th, h.cat)
				for sub := 0; sub < numSubs; sub++ {
					h.m.sub = sub
					checkFits(t, fmt.Sprintf("%s/%s/%v/sub%d", lang, mode, sz, sub), h.m.View(h.env(sz[0], sz[1]-5)), sz[0], sz[1]-5)
				}
				h.m.sub = subList
				h.key("enter")
				checkFits(t, fmt.Sprintf("%s/%s/%v/detail", lang, mode, sz), h.m.View(h.env(sz[0], sz[1]-5)), sz[0], sz[1]-5)
				h.key("esc")
				for _, k := range []string{"n", "F", "T"} {
					h.key(k)
					checkFits(t, fmt.Sprintf("%s/%s/%v/%s", lang, mode, sz, k), h.m.View(h.env(sz[0], sz[1]-5)), sz[0], sz[1]-5)
					h.key("esc")
					h.key("esc")
				}
			}
		}
	}
}

func checkFits(t *testing.T, name, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%s: %d dòng, muốn %d", name, len(lines), h)
	}
	for i, l := range lines {
		if text.Width(l) != w {
			t.Fatalf("%s: dòng %d rộng %d, muốn %d: %q", name, i, text.Width(l), w, ansi.Strip(l))
		}
	}
}

func TestSubTabsAndKeysInPanels(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key(".")
	if h.m.sub != subSearch || !h.m.Capturing() {
		t.Fatalf("thẻ Tìm kiếm có form: sub=%d capturing=%v", h.m.sub, h.m.Capturing())
	}
	// Trong form: phím chữ vào ô, kể cả q và dấu chấm.
	h.typ("q.x")
	h.key("enter")
	r := h.lastReq()
	so, _ := r.Opts.(app.SearchOptions)
	if r.ID != "knowledge.search" || so.Query != "q.x" || so.Limit != 20 {
		t.Fatalf("%+v", r)
	}
	if !h.m.WantsInterrupt() {
		t.Fatal("đang chạy: Esc/Ctrl+C huỷ")
	}
	h.m.cur().running = false
	h.key(",")
	if h.m.sub != subList || h.m.Capturing() {
		t.Fatal(", quay về danh sách")
	}
	h.key(".", ".") // search → stats tự chạy
	if r := h.lastReq(); r.ID != "knowledge.stats" {
		t.Fatalf("Thống kê tự chạy khi mở: %+v", r)
	}
	h.m.cur().running = false
	h.key(".")
	if r := h.lastReq(); r.ID != "knowledge.diff" {
		t.Fatalf("Thay đổi tự chạy khi mở: %+v", r)
	}
}

func TestPanelResultsRender(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	cases := []struct {
		sub   string
		id    string
		value any
		want  string
	}{
		{"search", "knowledge.search", []kindexHit{}, "Không tìm thấy."},
		{"diff", "knowledge.diff", app.DiffReport{Range: "ORIG_HEAD..HEAD"}, "Không có tri thức mới hoặc bị sửa từ người khác trong ORIG_HEAD..HEAD."},
		{"revalidate", "knowledge.revalidate", app.RevalidateReport{Results: []app.Revalidation{{ID: "01D", Verdict: "valid", Reason: "vẫn đúng"}, {ID: "01E", Blocked: "đường dẫn mất"}}, SpentUSD: 0.5, StopReason: "hết ngân sách"},
			"vẫn đúng"},
		{"context", "context", app.ContextResult{}, "Không có tri thức liên quan."},
	}
	for _, c := range cases {
		h.m.EnterSub(c.sub)
		p := h.m.cur()
		p.running = true
		h.result(c.id, nil, c.value, nil)
		if p.running {
			t.Fatalf("%s: vẫn chạy", c.sub)
		}
		if out := h.view(120, 30); !strings.Contains(out, c.want) {
			t.Errorf("%s: thiếu %q:\n%s", c.sub, c.want, out)
		}
	}
	h.m.EnterSub("search")
	h.m.cur().running = true
	h.result("knowledge.search", nil, nil, context.Canceled)
	if out := h.view(120, 30); !strings.Contains(out, "Đã huỷ.") {
		t.Fatalf("lỗi huỷ phải được dịch:\n%s", out)
	}
}

// Thẩm định lại: hộp xác nhận nêu ngân sách; Esc khi đang chạy gửi yêu cầu huỷ.
func TestRevalidateConfirmAndCancel(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.m.EnterSub("revalidate")
	h.m.cur().form.FocusField("budget")
	h.typ("1.5")
	h.key("enter")
	if h.m.dlg == nil || !strings.Contains(h.view(120, 30), "ngân sách $1.50") {
		t.Fatalf("thiếu hộp xác nhận nêu ngân sách:\n%s", h.view(120, 30))
	}
	if len(h.reqs) != 0 {
		t.Fatal("chưa gửi trước khi xác nhận")
	}
	h.key("enter")
	r := h.lastReq()
	if r.ID != "knowledge.revalidate" || r.Opts.(app.RevalidateOptions).BudgetUSD != 1.5 {
		t.Fatalf("%+v", r)
	}
	h.key("esc")
	if r := h.lastReq(); r.ID != "cancel:knowledge.revalidate" {
		t.Fatalf("Esc khi đang chạy phải huỷ việc nền: %+v", r)
	}
	if !strings.Contains(h.view(120, 30), "Đang chạy") {
		t.Fatal("hiện trạng thái đang chạy")
	}
}

// Xuất bản hai bước: chạy thử → xem diff → w → xác nhận → ghi; diff lúc ghi khác diff đã xem thì cảnh báo.
func TestPublishTwoStepsWithDiffChangedWarning(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.m.EnterSub("publish")
	h.key("esc") // rời form để dùng phím của thẻ
	h.key("w")
	if h.m.dlg != nil || !strings.Contains(h.m.toast, "Chạy thử") {
		t.Fatalf("chưa chạy thử thì không ghi được: %q", h.m.toast)
	}
	h.key("enter") // vào lại form
	h.key("enter") // gửi: chạy thử
	r := h.lastReq()
	if r.ID != "bootstrap.publish_preview" {
		t.Fatalf("%+v", r)
	}
	diff := "--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -1 +1,2 @@\n giữ\n+quy tắc mới\n"
	h.result("bootstrap.publish_preview", r.Opts, app.PublishReport{Diff: diff, Changed: true}, nil)
	out := h.view(120, 30)
	for _, want := range []string{"+quy tắc mới", "Nhấn w để ghi."} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	h.key("w")
	if h.m.dlg == nil || h.m.dlg.c.Danger {
		t.Fatal("không --force thì hộp không đỏ")
	}
	h.key("enter")
	r = h.lastReq()
	if r.ID != "bootstrap.publish" {
		t.Fatalf("%+v", r)
	}
	// Confirm do màn hình dựng chỉ đồng ý với đúng diff đã xem (và bước ghi không tự đặt Yes).
	po := r.Opts.(app.PublishOptions)
	if po.Confirm == nil || po.Yes || !po.Confirm(app.ConfirmRequest{Detail: diff}) || po.Confirm(app.ConfirmRequest{Detail: diff + "+khác\n"}) {
		t.Fatalf("Confirm phải so Detail với diff đã xem: %+v", po)
	}
	// Dịch vụ từ chối ghi vì diff lúc ghi khác: không ghi, hiện diff mới kèm cảnh báo để xem lại.
	h.result("bootstrap.publish", nil, app.PublishReport{Diff: diff + "+khác\n", Changed: true}, nil)
	out = h.view(120, 30)
	if !strings.Contains(out, "AGENTS.md vừa đổi") || !strings.Contains(out, "+khác") || strings.Contains(out, "Đã ghi AGENTS.md.") || !strings.Contains(out, "Nhấn w để ghi.") {
		t.Fatalf("thiếu cảnh báo diff khác / lời nhắc xem lại:\n%s", out)
	}
	// Ghi lần hai: so với diff mới đã xem.
	h.key("w")
	h.key("enter")
	r = h.lastReq()
	po = r.Opts.(app.PublishOptions)
	if r.ID != "bootstrap.publish" || !po.Confirm(app.ConfirmRequest{Detail: diff + "+khác\n"}) || po.Confirm(app.ConfirmRequest{Detail: diff}) {
		t.Fatalf("lần ghi sau phải so với diff mới: %+v", r)
	}
	h.result("bootstrap.publish", nil, app.PublishReport{Diff: diff + "+khác\n", Changed: true, Written: true}, nil)
	out = h.view(120, 30)
	if strings.Contains(out, "AGENTS.md vừa đổi") || !strings.Contains(out, "Đã ghi AGENTS.md.") {
		t.Fatalf("ghi đúng diff đã xem thì không cảnh báo:\n%s", out)
	}
	// Ghi xong thì không còn gì để ghi: w không mở hộp.
	h.key("w")
	if h.m.dlg != nil {
		t.Fatal("đã ghi xong, w không được mở hộp xác nhận")
	}
}

// Bản xem trước báo không đổi thì chặn phím w (không ghi cái chưa ai xem).
func TestPublishBlockedWhenPreviewUnchanged(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.m.EnterSub("publish")
	h.key("enter")
	r := h.lastReq()
	h.result("bootstrap.publish_preview", r.Opts, app.PublishReport{Changed: false}, nil)
	h.key("w")
	if h.m.dlg != nil || !strings.Contains(h.m.toast, "không có thay đổi") {
		t.Fatalf("w phải bị chặn khi không có thay đổi: dlg=%v toast=%q", h.m.dlg, h.m.toast)
	}
}

func TestPublishForceShowsWarning(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.m.EnterSub("publish")
	h.m.cur().form.FocusField("force")
	h.key("space")
	h.key("enter")
	r := h.lastReq()
	if !r.Opts.(app.PublishOptions).Force {
		t.Fatalf("%+v", r.Opts)
	}
	h.result("bootstrap.publish_preview", r.Opts, app.PublishReport{Diff: "+x\n", Changed: true}, nil)
	h.key("w")
	if h.m.dlg == nil || !h.m.dlg.c.Danger || !strings.Contains(h.view(120, 30), "--force: ghi đè khối bint") {
		t.Fatalf("--force phải có cảnh báo:\n%s", h.view(120, 30))
	}
}

func TestCommandsFromPalette(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.run(h.m.Command("knowledge.search"))
	if h.m.sub != subSearch {
		t.Fatalf("sub=%d", h.m.sub)
	}
	h.run(h.m.Command("bootstrap.skill"))
	if h.m.sub != subSurvey || h.m.survM != 2 {
		t.Fatalf("sub=%d mode=%d", h.m.sub, h.m.survM)
	}
	h.run(h.m.Command("knowledge.review"))
	if h.m.sub != subList || ids(h.m.Visible()) != "01A,01B,01D,01E" {
		t.Fatalf("review: %s", ids(h.m.Visible()))
	}
	h.run(h.m.Command("knowledge.approve_batch"))
	if r := h.lastReq(); r.ID != "knowledge.plan_batch" {
		t.Fatalf("%+v", r)
	}
}

// Mỗi tên thẻ phụ có chuỗi nhãn và hướng dẫn ở mọi locale thật.
func TestSubViewStringsExist(t *testing.T) {
	for _, lang := range i18n.Langs {
		cat := i18n.MustLoad(lang)
		for _, n := range subNames {
			if !cat.Has("kn.sub." + n) {
				t.Errorf("%s: thiếu kn.sub.%s", lang, n)
			}
			if n != "list" && !cat.Has("kn.panel.hint."+n) {
				t.Errorf("%s: thiếu kn.panel.hint.%s", lang, n)
			}
		}
	}
}

// Mọi form của màn hình dùng không gian tên nhãn có thật trong sổ hành động.
func TestPanelFormNamespacesMatchRegistry(t *testing.T) {
	ns := map[string]bool{}
	for _, a := range actions.Default().All() {
		ns[a.FormNS()] = true
	}
	h := newHarness(t, nil, Options{})
	for _, p := range h.m.allPanels() {
		if !ns[p.formID] {
			t.Errorf("thẻ %s: không gian tên form %q không có trong sổ", p.action, p.formID)
		}
		if _, ok := actions.Default().ByID(p.action); !ok {
			t.Errorf("hành động %q không có trong sổ", p.action)
		}
	}
}

// Thao tác ghi xong trong lúc đang nạp: nạp thêm một lần để thấy thay đổi (không bỏ lỡ).
func TestReloadRequestedWhileLoadingIsNotLost(t *testing.T) {
	loads := 0
	h := newHarness(t, sample(), Options{})
	inner := h.m.o.Loader
	h.m.o.Loader = func(ctx context.Context, dir string) (Data, error) { loads++; return inner(ctx, dir) }
	first := h.m.Load() // đang nạp
	if first == nil || h.m.Load() != nil {
		t.Fatal("lần gọi thứ hai khi đang nạp không khởi thêm goroutine")
	}
	h.run(first)
	if loads != 2 || h.m.Loading {
		t.Fatalf("phải nạp đúng 2 lần (1 + 1 yêu cầu dồn): %d, loading=%v", loads, h.m.Loading)
	}
}
