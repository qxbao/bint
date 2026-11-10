package evolve

import (
	"slices"
	"sort"

	"bint/internal/eval"
)

// SkillActivation là số lượt đối chiếu (Probe) mà một skill được agent gọi, theo nhánh.
// Measured = số lượt harness đo được kích hoạt (nhánh có tập skill, harness báo tool_use);
// Measured = 0: không có dữ liệu, không được đọc thành "không bao giờ được dùng".
type SkillActivation struct {
	Skill     string `json:"skill"`
	Arm       string `json:"arm"`
	Measured  int    `json:"measured_runs"`
	Activated int    `json:"activated_runs"`
}

// ProbeActivation tổng hợp kích hoạt thực tế của các skill trong các lượt đối chiếu của một
// đợt. Đây là nguồn kích hoạt riêng của Probe (không cần hook); kiểm tra điều kiện kích hoạt
// trong CheckTriggerConditions vẫn dùng sự kiện skill.activated của lịch sử thật.
func ProbeActivation(rows []eval.AttemptRow, names []string) []SkillActivation {
	var out []SkillActivation
	for _, n := range names {
		byArm := map[string]*SkillActivation{}
		for _, r := range rows {
			if r.SkillsActivated == nil {
				continue
			}
			a := byArm[r.Arm]
			if a == nil {
				a = &SkillActivation{Skill: n, Arm: r.Arm}
				byArm[r.Arm] = a
			}
			a.Measured++
			if slices.Contains(r.SkillsActivated, n) {
				a.Activated++
			}
		}
		arms := make([]string, 0, len(byArm))
		for k := range byArm {
			arms = append(arms, k)
		}
		sort.Strings(arms)
		for _, k := range arms {
			out = append(out, *byArm[k])
		}
	}
	return out
}
