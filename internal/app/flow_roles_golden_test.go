package app

// Golden của GĐ3 (docs/custom-flow-plan.md): vai trò hoạt động theo workflow/trạng thái/phân loại
// (workflowRoles, rolesForRun, roleStep). Sinh từ mã cũ bằng BINT_UPDATE_GOLDEN=1, không sửa tay.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"bint/internal/flow"
	"bint/internal/task"
)

const rolesGoldenPath = "testdata/flow_roles_golden.json"

func sortedRoles(m map[string]bool) []string {
	out := []string{}
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// goldenFlow: flow nhúng theo tên; tên lạ coi như b-do (cách mã cũ tra tên workflow).
func goldenFlow(name string) flow.Def {
	fd, ok := flow.GetBuiltin(name)
	if !ok {
		fd, _ = flow.GetBuiltin("b-do")
	}
	return fd
}

// roleStep trả id bước của flow nhúng b-do chạy role đó (bước implement nếu role không có bước riêng): bản tham chiếu
// của mã cũ, chỉ để golden vẫn sinh đúng cùng kết quả.
func roleStep(role string) string {
	bdo, _ := flow.GetBuiltin("b-do")
	for _, s := range bdo.Steps {
		if s.EffectiveRole() == role {
			return s.ID
		}
	}
	impl, _ := bdo.FirstOfKind(flow.KindImplement)
	return impl.ID
}

func buildRolesGolden() map[string]any {
	g := map[string]any{}
	wr := map[string][]string{}
	for _, n := range []string{"b-do", "b-req", "b-feat", "b-review", "", "bogus"} {
		wr[n] = sortedRoles(workflowRoles(goldenFlow(n)))
	}
	g["workflow_roles"] = wr
	rs := map[string]string{}
	for _, r := range []string{"planner", "implementer", "reviewer", "", "bogus"} {
		rs[r] = roleStep(r)
	}
	g["role_step"] = rs

	rr := map[string][]string{}
	statuses := []string{task.StatusCreated, task.StatusPlanned, task.StatusInProgress, task.StatusInReview, task.StatusBlocked,
		task.StatusInterrupted, task.StatusFailed}
	for _, wf := range []string{"b-do", "b-req", "b-feat", "b-review"} {
		for _, noReview := range []bool{false, true} {
			rr[fmt.Sprintf("%s/nil_task/noreview=%v", wf, noReview)] = sortedRoles(rolesForRun(goldenFlow(wf), nil, noReview))
			for _, st := range statuses {
				for _, resume := range []string{"", task.StatusPlanned, task.StatusInReview} {
					for _, plan := range []string{"nil", "plan", "noplan"} {
						tk := &task.Task{State: task.State{ID: "x", Status: st, ResumeFrom: resume}}
						switch plan {
						case "plan":
							tk.State.Classification = &task.Classification{Plan: true}
						case "noplan":
							tk.State.Classification = &task.Classification{Plan: false}
						}
						rr[fmt.Sprintf("%s/%s/resume=%s/plan=%s/noreview=%v", wf, st, resume, plan, noReview)] = sortedRoles(rolesForRun(goldenFlow(wf), tk, noReview))
					}
				}
			}
		}
	}
	g["roles_for_run"] = rr
	return g
}

func TestFlowGoldenRoles(t *testing.T) {
	got, err := json.MarshalIndent(buildRolesGolden(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("BINT_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(rolesGoldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(rolesGoldenPath)
	if err != nil {
		t.Fatalf("thiếu golden (sinh bằng BINT_UPDATE_GOLDEN=1 trên mã cũ): %v", err)
	}
	if string(want) != string(got) {
		actual := filepath.Join(os.TempDir(), "bint-app-roles-golden-actual.json")
		_ = os.WriteFile(actual, got, 0o644)
		t.Fatalf("golden vai trò không khớp; bản thực tế ghi ở %s để so", actual)
	}
}
