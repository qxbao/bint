package task

import (
	"reflect"
	"testing"
)

func mk(id, status string, deps ...string) *Task {
	return &Task{State: State{ID: id, Status: status, DependsOn: deps}}
}

func ids(ts []*Task) []string {
	out := []string{}
	for _, t := range ts {
		out = append(out, t.State.ID)
	}
	return out
}

func TestReady(t *testing.T) {
	ts := []*Task{
		mk("05", StatusCreated, "01"),       // dep done → sẵn sàng
		mk("04", StatusCreated, "02"),       // dep in_progress
		mk("03", StatusPlanned, "06"),       // dep cancelled
		mk("02", StatusInProgress),          // không dep
		mk("01", StatusDone),                // kết thúc → loại
		mk("06", StatusCancelled),           // kết thúc → loại
		mk("07", StatusCreated, "99"),       // dep không đọc được
		mk("08", StatusBlocked, "01", "02"), // một dep chưa xong
		mk("09", StatusCreated, "10"),       // chu trình 09 ↔ 10
		mk("10", StatusCreated, "09"),
	}
	got := ids(Ready(ts))
	want := []string{"02", "05"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ready = %v, muốn %v", got, want)
	}
}

func TestFindCycle(t *testing.T) {
	deps := map[string][]string{"B": {"C"}, "C": {"D"}, "X": {"Y"}, "Y": {"X"}}
	cases := []struct {
		name string
		id   string
		add  []string
		want []string
	}{
		{"tự trỏ", "A", []string{"A"}, []string{"A", "A"}},
		{"hai nút", "C", []string{"B"}, []string{"C", "B", "C"}},
		{"ba nút", "D", []string{"B"}, []string{"D", "B", "C", "D"}},
		{"không chu trình", "A", []string{"B", "D"}, nil},
		{"chu trình có sẵn không liên quan", "A", []string{"X"}, nil},
	}
	for _, c := range cases {
		if got := FindCycle(deps, c.id, c.add); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}

func TestInherit(t *testing.T) {
	parent := Ticket{Scope: Scope{Allow: []string{"a/**"}, Deny: []string{"d/**"}},
		References: []string{"doc.md"}, Constraints: []string{"c1"}}
	var child Ticket
	got := Inherit(&child, parent, false)
	if len(got) != 4 || child.Scope.Allow[0] != "a/**" || child.Constraints[0] != "c1" {
		t.Fatalf("kế thừa đủ: %v %+v", got, child)
	}
	child.Scope.Allow[0] = "x" // bản chép, không dùng chung mảng
	if parent.Scope.Allow[0] != "a/**" {
		t.Fatal("dùng chung mảng với phiếu cha")
	}
	own := Ticket{Scope: Scope{Allow: []string{"own"}}, Constraints: []string{"k"}}
	got = Inherit(&own, parent, false)
	if !reflect.DeepEqual(got, []string{InheritDeny, InheritReferences}) || own.Scope.Allow[0] != "own" || own.Constraints[0] != "k" {
		t.Fatalf("không ghi đè: %v %+v", got, own)
	}
	var cleared Ticket
	got = Inherit(&cleared, parent, true)
	if !reflect.DeepEqual(got, []string{InheritReferences, InheritConstraints}) || len(cleared.Scope.Allow) != 0 || len(cleared.Scope.Deny) != 0 {
		t.Fatalf("clearScope: %v %+v", got, cleared)
	}
}
