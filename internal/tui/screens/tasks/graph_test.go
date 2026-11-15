package tasks

import (
	"strings"
	"testing"

	"bint/internal/task"
)

func tk(id, status string, parent string, deps ...string) *task.Task {
	return &task.Task{Ticket: task.Ticket{ID: id, Goal: "g" + id},
		State: task.State{ID: id, Status: status, Parent: parent, DependsOn: deps}}
}

func idsOf(ts []*task.Task) string {
	var s []string
	for _, t := range ts {
		s = append(s, t.State.ID)
	}
	return strings.Join(s, ",")
}

func TestSiblingOrderPutsDependenciesFirst(t *testing.T) {
	// Danh sách A,B,C nhưng A phụ thuộc C: C trước A; B độc lập giữ chỗ theo thứ tự danh sách.
	got := idsOf(siblingOrder([]*task.Task{tk("A", "created", "", "C"), tk("B", "created", ""), tk("C", "created", "")}))
	if got != "B,C,A" {
		t.Errorf("thứ tự = %s, muốn B,C,A", got)
	}
	// Nhánh song song: C và B cùng phụ thuộc A; hoà giữ thứ tự danh sách (C trước B), không theo ID.
	got = idsOf(siblingOrder([]*task.Task{tk("C", "created", "", "A"), tk("B", "created", "", "A"), tk("A", "created", "")}))
	if got != "A,C,B" {
		t.Errorf("nhánh song song = %s, muốn A,C,B", got)
	}
	// Không có phụ thuộc: giữ nguyên thứ tự danh sách.
	got = idsOf(siblingOrder([]*task.Task{tk("Z", "created", ""), tk("A", "created", ""), tk("M", "created", "")}))
	if got != "Z,A,M" {
		t.Errorf("không phụ thuộc = %s, muốn Z,A,M", got)
	}
}

func TestSiblingOrderCycleAppendedAndTerminates(t *testing.T) {
	got := idsOf(siblingOrder([]*task.Task{tk("B", "created", "", "A"), tk("A", "created", "", "B"), tk("C", "created", "")}))
	if got != "C,B,A" {
		t.Errorf("chu trình nối cuối = %s, muốn C,B,A", got)
	}
}

func TestPlanOfClosure(t *testing.T) {
	ts := []*task.Task{
		tk("P", "created", ""), tk("C1", "done", "P"), tk("C2", "created", "P", "C1"),
		tk("X", "created", "", "C2"), tk("L", "created", ""),
	}
	if got := idsOf(planOf(ts, "C1")); got != "P,C1,C2,X" {
		t.Errorf("kế hoạch từ phiếu con = %s", got)
	}
	if got := planOf(ts, "L"); got != nil {
		t.Errorf("phiếu lẻ phải trả nil, được %s", idsOf(got))
	}
	// Chu trình cha–con và phụ thuộc không treo.
	cyc := []*task.Task{tk("A", "created", "B", "B"), tk("B", "created", "A", "A")}
	if got := planOf(cyc, "A"); len(got) != 2 {
		t.Errorf("chu trình = %s", idsOf(got))
	}
}

func TestLayers(t *testing.T) {
	plan := []*task.Task{
		tk("A", "done", ""), tk("B", "created", "", "A"), tk("C", "created", "", "A"), tk("D", "created", "", "B", "C"),
		tk("M", "created", "", "GONE"), tk("Y", "created", "", "Z"), tk("Z", "created", "", "Y"),
	}
	g := layers(plan, plan)
	var lv []string
	for _, l := range g.Levels {
		lv = append(lv, idsOf(l))
	}
	if got := strings.Join(lv, "|"); got != "A,M|B,C|D" {
		t.Errorf("bậc = %s", got)
	}
	if idsOf(g.Cycle) != "Y,Z" {
		t.Errorf("chu trình = %s", idsOf(g.Cycle))
	}
	if idsOf(g.Missing) != "M" {
		t.Errorf("không đọc được = %s", idsOf(g.Missing))
	}
	// Phụ thuộc ngoài kế hoạch nhưng đã nạp là bậc 0.
	all := append([]*task.Task{tk("O", "done", "")}, plan...)
	g = layers([]*task.Task{tk("B", "created", "", "O")}, all)
	if len(g.Levels) != 1 || idsOf(g.Levels[0]) != "B" {
		t.Errorf("phụ thuộc ngoài kế hoạch: %+v", g)
	}
}
