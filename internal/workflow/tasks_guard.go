package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"bint/internal/spool"
	"bint/internal/task"
)

// tasksSnapshot trả nội dung tasks.md trước bước implement khi tác vụ theo hợp đồng v1 và ba artifact đang đạt hợp đồng;
// "" khi không có gì để giữ (tác vụ v0, thiếu tệp, hoặc đã hỏng sẵn: khi đó cổng hợp đồng xử lý như cũ).
func tasksSnapshot(t *task.Task) string {
	if t.State.PlanningContractVersion < task.PlanningContractV1 {
		return ""
	}
	b, err := os.ReadFile(t.ArtifactPath("tasks"))
	if err != nil || task.LintPlanningTask(t, true).Blocking() {
		return ""
	}
	return string(b)
}

// guardTasks chạy sau bước implement: bước đó chỉ được đánh dấu tiến độ trong tasks.md, nhưng agent hay ghi lại cả tệp
// và làm mất `— xong khi: …`, khiến lần lint kế tiếp (preflightFeat của vòng sửa) chặn tác vụ chờ người. Nếu tasks.md
// không còn đạt hợp đồng v1 mà bản trước bước (before) đạt: phục hồi các dòng TSK của before, giữ đánh dấu [x]/[ ] theo
// TSK-ID của bản mới (task.RestoreTaskLines); không ghép được thì phục hồi nguyên before. Bản phục hồi phải đạt hợp đồng
// với requirement/plan hiện tại, không thì giữ nguyên (lỗi nằm ở tệp khác; cổng hợp đồng báo như cũ). Ghi chú vào Notes và log.
func (r *Runner) guardTasks(rs *runState, step, before string) {
	if before == "" {
		return
	}
	t, err := r.Tasks.Load(rs.t.State.ID)
	if err != nil {
		return
	}
	cur, err := os.ReadFile(t.ArtifactPath("tasks"))
	if err == nil && string(cur) == before {
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	rep := task.LintPlanningTask(t, true)
	if !rep.Blocking() {
		return
	}
	docs := map[string]string{}
	for _, n := range []string{"requirement", "plan"} {
		b, rerr := os.ReadFile(t.ArtifactPath(n))
		if rerr != nil {
			return
		}
		docs[n] = string(b)
	}
	passes := func(tasks string) bool {
		docs["tasks"] = tasks
		return !task.LintPlanningText(t.Ticket, docs, true).Blocking()
	}
	restored, merged := task.RestoreTaskLines(before, string(cur))
	if !passes(restored) {
		if !merged || !passes(before) {
			return
		}
		restored, merged = before, false
	}
	nt, err := r.Tasks.Update(t.State.ID, r.now(), func(t *task.Task) error {
		if _, err := t.WriteArtifact("tasks", restored); err != nil {
			return err
		}
		if t.State.Artifacts == nil {
			t.State.Artifacts = map[string]string{}
		}
		t.State.Artifacts["tasks"] = r.at()
		return nil
	})
	if err != nil {
		r.logf("  ! không phục hồi được tasks.md: %v", err)
		return
	}
	rs.t = nt
	how := "phục hồi các dòng TSK của bản trước bước, giữ đánh dấu [x]/[ ] theo TSK-ID của bản mới"
	if !merged {
		how = "phục hồi nguyên bản trước bước (không ghép được đánh dấu theo TSK-ID)"
	}
	note := fmt.Sprintf("%s ghi tasks.md không đạt hợp đồng v1 (%s): %s", step, truncate(strings.Join(issueLines(rep.Errors), "; "), 300), how)
	r.logf("  %s", note)
	rs.res.Notes = append(rs.res.Notes, note)
	r.emit(nt, spool.TaskArtifact, map[string]any{"name": "tasks", "path": nt.ArtifactPath("tasks"), "restored": true})
}
