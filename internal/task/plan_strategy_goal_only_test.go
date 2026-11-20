package task

import "testing"

// b-req gọn của B′ chỉ viết dòng b-goal (không có dòng Chia lát): phải đọc được "không" để Bint bỏ thăm dò (docs/bprime-budget-plan.md mục 1.8e).
func TestPlanStrategyGoalOnly(t *testing.T) {
	_, goal := PlanStrategy("# Kế hoạch\nSửa a.go.\n\n## Chiến lược thực thi\nKiểm tra mục tiêu độc lập (b-goal): không — chỉ đổi tên hàm\n")
	if goal != StrategyNo {
		t.Errorf("goal = %q, muốn %q", goal, StrategyNo)
	}
}
