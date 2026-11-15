package tasks_test

import (
	"context"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// planRig: 01TASK5 phụ thuộc 01TASK3 (done) → sẵn sàng; 01TASK1 phụ thuộc 01TASK4 (đang làm) → chờ; 01TASK2 là con
// của 01TASK1; 01TASK4 có 2 phát hiện open.
func planRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t}
	o := tasks.Options{}
	o.Loader = func(context.Context, string, bool) (tasks.Data, error) {
		d := data(t.TempDir())
		for _, tk := range d.Tasks {
			switch tk.State.ID {
			case "01TASK5":
				tk.State.DependsOn = []string{"01TASK3"}
			case "01TASK1":
				tk.State.DependsOn = []string{"01TASK4"}
			case "01TASK2":
				tk.State.Parent = "01TASK1"
			}
		}
		d.Ready = map[string]bool{"01TASK5": true}
		d.OpenFindings = map[string]int{"01TASK4": 2}
		return d, nil
	}
	o.Detailer = func(ctx context.Context, root, id string, f bool) (tasks.Detail, error) {
		d, err := detailer(ctx, root, id, f)
		if id == "01TASK1" {
			d.Deps = []app.RelatedTask{{ID: "01TASK4", Status: "in_progress", Goal: "Thêm màn Tác vụ"}}
			d.Children = []app.RelatedTask{{ID: "01TASK2", Status: "failed", Goal: "Đổi cách tính phí"}}
		}
		return d, err
	}
	o.Runs = runs
	o.Gate = func(string) actions.Availability { return actions.Available }
	r.m = tasks.New(o)
	r.m.SetEnv(theme.New(theme.Mono), ui.Rewriter{Cat: i18n.MustLoad("vi")})
	r.feed(r.m.Ensure())
	return r
}

func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestListMarksDependenciesAndOpenFindings(t *testing.T) {
	out := planRig(t).view(140, 40)
	if l := lineWith(out, "Viết tài liệu cài đặt"); !strings.Contains(l, "◇") {
		t.Errorf("phiếu sẵn sàng thiếu ◇: %q", l)
	}
	if l := lineWith(out, "Thêm cột email"); !strings.Contains(l, "⧗") {
		t.Errorf("phiếu chờ phụ thuộc thiếu ⧗: %q", l)
	}
	if l := lineWith(out, "Thêm màn Tác vụ"); !strings.Contains(l, "⚑2") || strings.ContainsAny(l, "◇⧗") {
		t.Errorf("phiếu có 2 phát hiện open, không depends_on: %q", l)
	}
	if l := lineWith(out, "Đổi cách tính phí"); strings.ContainsAny(l, "◇⧗⚑") {
		t.Errorf("phiếu trơn không được có dấu: %q", l)
	}
}

func TestTreeModeIndentsChildrenAndToggles(t *testing.T) {
	r := planRig(t)
	before := ids(r.m.Visible())
	r.press("P")
	got := ids(r.m.Visible())
	pos := func(s []string, id string) int {
		for i, x := range s {
			if x == id {
				return i
			}
		}
		return -1
	}
	if pos(got, "01TASK2") != pos(got, "01TASK1")+1 {
		t.Errorf("phiếu con phải ngay dưới phiếu cha: %v", got)
	}
	out := r.view(140, 40)
	if l := lineWith(out, "Đổi cách tính phí"); !strings.Contains(l, "└ Đổi") {
		t.Errorf("phiếu con phải có ký tự nối cây: %q", l)
	}
	r.press("P")
	if after := ids(r.m.Visible()); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Errorf("tắt cây phải trả thứ tự cũ: %v vs %v", after, before)
	}
}

func TestRelationsSectionOnlyWithRelations(t *testing.T) {
	r := planRig(t)
	if r.m.Cursor().State.ID != "01TASK1" {
		t.Fatalf("con trỏ = %s", r.m.Cursor().State.ID)
	}
	out := r.view(140, 50)
	for _, want := range []string{"Quan hệ", "Phụ thuộc", "Phiếu con", "01TASK4", "Đang làm"} {
		if !strings.Contains(out, want) {
			t.Errorf("thẻ Phiếu thiếu %q:\n%s", want, out)
		}
	}
	r.press("j") // 01TASK2 không có Parent trong Detail mẫu: không có mục Quan hệ
	if out := r.view(140, 50); strings.Contains(out, "Quan hệ") {
		t.Errorf("phiếu không có quan hệ không được có mục Quan hệ:\n%s", out)
	}
}

func TestFindingsKeyRequestsOpenList(t *testing.T) {
	r := planRig(t)
	r.press("F")
	req := r.lastReq()
	if req.ID != "tasks.findings" {
		t.Fatalf("yêu cầu = %+v", req)
	}
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.findings", Value: app.FindingsList{}}))
	if out := r.view(140, 40); !strings.Contains(out, "Không có phát hiện nào còn open") {
		t.Errorf("hộp rỗng:\n%s", out)
	}
	// Value rỗng không làm gì và không gửi yêu cầu kế tiếp.
	n := len(r.reqs)
	r.feed(r.m.Update(actions.ResultMsg{ID: "tasks.findings"}))
	if len(r.reqs) != n {
		t.Errorf("Value rỗng không được sinh yêu cầu")
	}
}

func TestTreeConnectorsAndDependencyOrder(t *testing.T) {
	r := planRig(t)
	r.press("P")
	out := r.view(140, 40)
	if l := lineWith(out, "Đổi cách tính phí"); !strings.Contains(l, "└ ") {
		t.Errorf("con cuối phải có └: %q", l)
	}
	// 01TASK1 phụ thuộc 01TASK4 nên 01TASK4 phải đứng trước.
	if a, b := lineIndex(out, "Thêm màn Tác vụ"), lineIndex(out, "Thêm cột email"); a < 0 || b < 0 || a > b {
		t.Errorf("phiếu được phụ thuộc phải đứng trước (%d, %d):\n%s", a, b, out)
	}
}

// Bật cây không được xáo phiếu gốc không có quan hệ phụ thuộc: giữ thứ tự của danh sách thường.
func TestTreeKeepsListOrderForUnrelatedRoots(t *testing.T) {
	r := planRig(t)
	before := ids(r.m.Visible())
	r.press("P")
	got := ids(r.m.Visible())
	pos := func(s []string, id string) int {
		for i, x := range s {
			if x == id {
				return i
			}
		}
		return -1
	}
	// 01TASK3 và 01TASK5: 5 phụ thuộc 3; hai phiếu này không dính 01TASK4 nên thứ tự tương đối với 4 giữ như cũ.
	for _, p := range [][2]string{{"01TASK4", "01TASK3"}, {"01TASK3", "01TASK4"}} {
		if pos(before, p[0]) < pos(before, p[1]) && pos(got, p[0]) > pos(got, p[1]) {
			t.Errorf("cây đổi thứ tự gốc %s/%s: %v → %v", p[0], p[1], before, got)
		}
	}
}

// Phiếu sẵn sàng nhưng đang chạy không được gợi ý làm Tiếp theo.
func TestGraphBoxNextSkipsStartedTasks(t *testing.T) {
	r := planRig(t)
	for _, tk := range r.m.Data.Tasks {
		if tk.State.ID == "01TASK5" {
			tk.State.Status = "in_progress"
		}
	}
	for r.m.Cursor().State.ID != "01TASK5" {
		r.press("j")
	}
	r.press("R")
	if out := r.view(140, 50); strings.Contains(out, "Tiếp theo:") {
		t.Errorf("phiếu đang chạy không được là Tiếp theo:\n%s", out)
	}
}

func TestGraphBoxShowsLevelsReadyAndWaits(t *testing.T) {
	r := planRig(t)
	if r.m.Cursor().State.ID != "01TASK1" {
		t.Fatalf("con trỏ = %s", r.m.Cursor().State.ID)
	}
	r.press("R")
	out := r.view(140, 50)
	for _, want := range []string{"Đồ thị phụ thuộc", "Bậc 1", "Bậc 2", "01TASK4", "▸", "chờ: 01TASK4", "Không có phiếu nào sẵn sàng"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp đồ thị thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tk.graph.") {
		t.Errorf("khoá i18n thô:\n%s", out)
	}
}

func TestGraphBoxNextReadyAndSolo(t *testing.T) {
	r := planRig(t)
	for r.m.Cursor().State.ID != "01TASK5" {
		r.press("j")
	}
	r.press("R")
	out := r.view(140, 50)
	for _, want := range []string{"Tiếp theo:", "01TASK5", "sẵn sàng", "01TASK3"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "tk.graph.") {
		t.Errorf("khoá i18n thô:\n%s", out)
	}
}

func TestGraphBoxCycleDoesNotHang(t *testing.T) {
	r := planRig(t)
	for _, tk := range r.m.Data.Tasks {
		switch tk.State.ID {
		case "01TASK4":
			tk.State.DependsOn = []string{"01TASK1"} // 1→4→1
		}
	}
	r.press("R")
	out := r.view(140, 50)
	if !strings.Contains(out, "Chu trình phụ thuộc") || strings.Contains(out, "tk.graph.") {
		t.Errorf("ca chu trình:\n%s", out)
	}
}

// lineIndex là số thứ tự dòng đầu tiên chứa sub, -1 nếu không có.
func lineIndex(out, sub string) int {
	for i, l := range strings.Split(out, "\n") {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}
