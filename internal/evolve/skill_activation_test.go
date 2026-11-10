package evolve

import (
	"reflect"
	"testing"

	"bint/internal/eval"
)

func TestProbeActivation(t *testing.T) {
	rows := []eval.AttemptRow{
		{Arm: "base", SkillsActivated: nil},                     // không đo được: bỏ qua
		{Arm: "candidate", SkillsActivated: []string{"a", "b"}}, // dùng a
		{Arm: "candidate", SkillsActivated: []string{}},         // đo rồi, không dùng
		{Arm: "candidate", SkillsActivated: []string{"b"}},      // dùng b, không a
		{Arm: "base", SkillsActivated: []string{"a"}},           // base cũng gọi a
	}
	got := ProbeActivation(rows, []string{"a"})
	want := []SkillActivation{
		{Skill: "a", Arm: "base", Measured: 1, Activated: 1},
		{Skill: "a", Arm: "candidate", Measured: 3, Activated: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, muốn %+v", got, want)
	}
	if got := ProbeActivation([]eval.AttemptRow{{Arm: "base"}}, []string{"a"}); len(got) != 0 {
		t.Errorf("không đo được thì không có dòng nào: %+v", got)
	}
}
