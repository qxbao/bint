package knowledge

import (
	"context"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/inspect"
	kn "bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
)

// newRealRepo dựng repository thật có một mục tri thức kèm bằng chứng.
func newRealRepo(t *testing.T) string {
	t.Helper()
	testutil.Isolate(t)
	for _, k := range app.AgentEnvVars() {
		t.Setenv(k, "")
	}
	root := testutil.NewRepo(t, map[string]string{"README.md": "# demo\n", "src/a.go": "package src\n\nfunc A() {}\n"})
	if _, err := app.Init(context.Background(), app.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := app.OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := app.AddKnowledge(context.Background(), k, app.AddOptions{Type: kn.TypeFact, Title: "Sự thật A", Body: "Hàm A làm việc A.",
		Evidence: []string{"src/a.go:3"}, Origin: kn.OriginAgent})
	if err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	return root
}

// Repository đã có mục tri thức: kết quả bootstrap vẫn phải hiện ở khung (trước đây nạp lại danh sách rồi
// thoát trước khi gán dòng kết quả), và danh sách được nạp lại.
func TestBootstrapResultShownWhenItemsExist(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.m.EnterSub("survey")
	h.key("esc") // rời form
	h.key("m")   // chế độ bootstrap
	rep := &app.BootstrapReport{Inspect: &inspect.Report{Root: "/r", Files: 3}, Items: sample(), ItemsRead: true,
		InspectPath: "/r/.bint/generated/inspect.json", Initialized: true, Counts: map[string]int{"verified": 1, "candidate": 3, "stale": 1}}
	h.m.Loading = false
	h.result("bootstrap", nil, rep, nil)
	p := h.m.surv[1]
	if len(p.lines) == 0 || !strings.Contains(strings.Join(p.lines, "\n"), "inspect.json") {
		t.Fatalf("khung kết quả bootstrap trống: %q", p.lines)
	}
	if out := h.view(120, 30); !strings.Contains(out, "inspect.json") {
		t.Fatalf("kết quả không hiện:\n%s", out)
	}
	if !h.m.Loaded {
		t.Fatal("danh sách phải được nạp lại")
	}
}

// Yêu cầu của form bị từ chối (--read-only hoặc phiên agent): form phải thoát trạng thái đang chạy và báo lý do.
func TestFormUnlockedWhenRequestDenied(t *testing.T) {
	h := newHarness(t, sample(), Options{})
	h.key("v")
	h.m.frm.FocusField("apply")
	h.key("space")
	h.key("ctrl+s")
	if r := h.lastReq(); r.ID != "knowledge.verify_apply" || !h.m.frmBusy || !h.m.Busy() {
		t.Fatalf("form phải đang chạy: %+v busy=%v", r, h.m.frmBusy)
	}
	h.run(h.m.Update(actions.DeniedMsg{ID: "knowledge.verify_apply", Locked: true}))
	if h.m.frmBusy || h.m.Busy() || h.m.frm == nil || h.m.frm.Err == "" {
		t.Fatalf("form kẹt sau DeniedMsg: busy=%v err=%q", h.m.frmBusy, h.m.frm.Err)
	}
	// Sửa được rồi gửi lại: form nhận phím trở lại.
	h.key("space")
	h.key("ctrl+s")
	if r := h.lastReq(); r.ID != "knowledge.verify" {
		t.Fatalf("sau khi tắt apply gửi lại phải là knowledge.verify: %+v", r)
	}
}

// Lý do từ chối mặc định là dữ liệu ghi vào tệp mục: không đổi theo ngôn ngữ giao diện, giống CLI.
func TestDefaultRejectReasonIgnoresLocale(t *testing.T) {
	for _, loc := range []string{"vi", "en"} {
		h := newHarness(t, sample(), Options{})
		h.cat = i18n.MustLoad(loc)
		h.m.SetEnv(h.th, h.cat)
		h.key("r")
		h.key("enter")
		r := h.lastReq()
		if r.ID != "knowledge.reject" || r.Opts.(app.RejectOptions).Reason != app.DefaultRejectReason {
			t.Errorf("%s: %+v", loc, r)
		}
	}
}

// Mặc định (không Detailer) chi tiết dựng từ danh sách đã nạp và Data.WS, không mở lại kho: kiểm bằng một
// không gian tri thức thật.
func TestDefaultDetailBuiltFromLoadedData(t *testing.T) {
	root := newRealRepo(t)
	d, err := Load(context.Background(), root)
	if err != nil || d.WS == nil || len(d.Items) == 0 {
		t.Fatalf("%v %+v", err, d)
	}
	got, ok := d.detail(d.Items[0])
	if !ok || got.Item != d.Items[0] || len(got.Evidence) != 1 || len(got.Evidence[0].Lines) == 0 {
		t.Fatalf("%+v ok=%v", got, ok)
	}
	if _, ok := (Data{Items: d.Items}).detail(d.Items[0]); ok {
		t.Fatal("không có WS thì phải dùng đường dự phòng")
	}

	h := newHarness(t, d.Items, Options{Dir: root})
	h.m.o.Detailer = nil // nạp thật
	h.m.Data = d
	h.m.asked = map[string]bool{}
	h.run(h.m.wantDetail())
	if _, ok := h.m.cache[h.m.Cursor().ID]; !ok {
		t.Fatalf("chi tiết chưa vào bộ nhớ đệm: err=%v", h.m.cacheErr)
	}
}

// Trích đoạn bằng chứng dài bị cắt: giao diện tự định dạng "còn N dòng" theo ngôn ngữ (không mang chuỗi
// tiếng Việt cứng từ dịch vụ).
func TestEvidenceMoreFormattedByLocale(t *testing.T) {
	items := sample()
	h := newHarness(t, items, Options{})
	h.cat = i18n.MustLoad("en")
	h.m.SetEnv(h.th, h.cat)
	h.m.cache[items[0].ID] = app.KnowledgeDetail{Item: items[0], Check: kn.Result{ID: items[0].ID},
		Evidence: []app.EvidenceView{{Evidence: items[0].Evidence[0], Lines: []string{"   1  package a"}, More: 7}}}
	out := h.view(120, 40)
	if !strings.Contains(out, "(7 more lines)") || strings.Contains(out, "dòng nữa") {
		t.Fatalf("thiếu dòng cắt bớt theo ngôn ngữ:\n%s", out)
	}
}
