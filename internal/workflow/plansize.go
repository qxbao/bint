package workflow

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"bint/internal/config"
	"bint/internal/flow"
	"bint/internal/task"
)

// Cổng cỡ kế hoạch của b-do: sau b-req (và các cổng lint/câu hỏi/lệnh kiểm chứng), Bint tự đo kế hoạch trên tài liệu. Kế hoạch NHỎ thì
// b-feat chạy ngay trong cùng lượt (không dừng chờ người giữa b-req và b-feat; kiểm chứng, review và Kiểm tra mục tiêu giữ như đường
// đầy đủ). Kế hoạch LỚN thì dừng chờ người xem kế hoạch (tác vụ blocked ở `planned`, hoặc hỏi có/không khi có Confirm tương tác);
// resume chạy b-feat, không chạy lại b-req. Bint không tin kết luận "nhỏ" của b-req: kết luận chỉ có thể làm kế hoạch lớn hơn, mọi điều
// kiện còn lại đo trên tài liệu; "nhỏ" không bao giờ dùng để cắt phạm vi (kế hoạch có giai đoạn hoãn luôn là lớn). Cổng chỉ áp cho flow b-do gốc.

// QConfirmPlanSize là khoá câu hỏi có/không của cổng cỡ kế hoạch (Text là câu hỏi đầy đủ; giao diện chưa có bản dịch thì dùng Text).
const QConfirmPlanSize = "workflow.confirm.plan_size" // Params: task, reasons

// goalNameRe: một tên trong backtick (tên test/tệp/hàm) trong mục Kiểm tra mục tiêu.
var goalNameRe = regexp.MustCompile("`[^`\n]+`")

// planSizeGated cho biết flow đang chạy có cổng cỡ kế hoạch (b-do gốc có bước plan).
func (rs *runState) planSizeGated() bool {
	return rs.flow.Name == StepDo && rs.flow.IsBuiltin() && rs.flow.HasKind(flow.KindPlan)
}

// planSizeReasons đo kế hoạch của tác vụ; rỗng khi kế hoạch là nhỏ, ngược lại là từng lý do kế hoạch lớn (mỗi lý do một cụm ngắn).
func (r *Runner) planSizeReasons(t *task.Task) []string {
	maxFiles := r.Cfg.Workflow.SmallPlanMaxFiles
	plan, err := os.ReadFile(t.ArtifactPath("plan"))
	if err != nil {
		return []string{"không đọc được plan.md"}
	}
	if task.IsBPrime(t) {
		// Luồng B′: kế hoạch gọn không có Cỡ kế hoạch/Bề mặt/Kiểm tra mục tiêu; chỉ đường dẫn được bảo vệ cần người xem.
		_, files := task.PlanSizeDecl(string(plan))
		if prot := r.planProtected(t, files); len(prot) > 0 {
			return []string{"chạm đường dẫn được bảo vệ (" + strings.Join(prot, ", ") + ")"}
		}
		return nil
	}
	var why []string
	verdict, files := task.PlanSizeDecl(string(plan))
	switch verdict {
	case task.PlanSizeSmall:
	case task.PlanSizeLarge:
		why = append(why, "b-req kết luận kế hoạch lớn")
	default:
		why = append(why, "plan.md thiếu mục `## Cỡ kế hoạch` có dòng `Kết luận: nhỏ` hoặc `Kết luận: lớn` (thiếu mục)")
	}
	if ph := task.PlanPhases(string(plan)); len(ph) > 0 {
		why = append(why, fmt.Sprintf("plan có giai đoạn hoãn/tách (%d dòng G-N dưới `## Đề xuất tách giai đoạn`): kế hoạch chia giai đoạn luôn là lớn", len(ph)))
	}
	switch {
	case maxFiles <= 0:
		why = append(why, "workflow.small_plan_max_files = 0: không kế hoạch nào là nhỏ (ngưỡng)")
	case len(files) == 0:
		why = append(why, "plan.md không liệt kê tệp nào trong `## Cỡ kế hoạch` (mỗi tệp một dòng bullet, đường dẫn trong backtick)")
	default:
		if len(files) > maxFiles {
			why = append(why, fmt.Sprintf("%d tệp sẽ sửa, vượt ngưỡng workflow.small_plan_max_files = %d", len(files), maxFiles))
		}
		for _, f := range files {
			if strings.ContainsAny(f, "*?[") {
				why = append(why, fmt.Sprintf("đường dẫn có ký tự glob (%s): không đếm được số tệp thật, kế hoạch là lớn", f))
				break
			}
		}
		switch n := task.PlanSurfaces(string(plan)); {
		case n == 0:
			why = append(why, "plan.md thiếu mục `## Bề mặt bị ảnh hưởng` (hoặc mục không có nhóm nào): không đo được số bề mặt")
		case n > maxFiles:
			why = append(why, fmt.Sprintf("%d nhóm bề mặt bị ảnh hưởng, vượt ngưỡng workflow.small_plan_max_files = %d", n, maxFiles))
		}
	}
	if req, err := os.ReadFile(t.ArtifactPath("requirement")); err != nil {
		why = append(why, "không đọc được requirement.md")
	} else if g := task.GoalCheck(string(req)); g == "" {
		why = append(why, "requirement.md thiếu mục `## Kiểm tra mục tiêu` (hoặc mục rỗng)")
	} else if !goalNameRe.MatchString(g) {
		why = append(why, "`## Kiểm tra mục tiêu` không nêu tên test cụ thể (tên trong backtick)")
	}
	if prot := r.planProtected(t, files); len(prot) > 0 {
		why = append(why, "chạm đường dẫn được bảo vệ ("+strings.Join(prot, ", ")+")")
	}
	return why
}

// planProtected là các mẫu limits.protected_paths mà phạm vi phiếu hoặc một tệp liệt kê trong plan chạm tới (dùng chung task.Assess:
// cùng luật so khớp/chồng lấn với lúc lập phiếu).
func (r *Runner) planProtected(t *task.Task, files []string) []string {
	prot := r.Cfg.Limits.ProtectedPaths
	out := slices.Clone(r.Assess(t).Protected)
	probe := task.Ticket{Scope: task.Scope{Allow: files}}
	for _, p := range task.Assess(probe, prot, true).Protected {
		out = appendUnique(out, p)
	}
	return out
}

// Giá trị của State.PlanSize.
const (
	planSizePassed  = "passed"
	planSizeBlocked = "blocked"
)

func (r *Runner) setPlanSize(rs *runState, v string) {
	if rs.t.State.PlanSize == v {
		return
	}
	if t, err := r.Tasks.Update(rs.t.State.ID, r.now(), func(t *task.Task) error { t.State.PlanSize = v; return nil }); err == nil {
		rs.t = t
	}
}

// preflightPlanSize chạy trước mỗi lần gọi b-feat trên tác vụ planned của b-do gốc (beginStep → preflightFeat): đường dừng trước cổng
// (lint b-req, lệnh kiểm chứng, câu hỏi mở) rồi resume vào thẳng b-feat không được bỏ qua cổng. Đã "passed" thì đi tiếp; "blocked" nghĩa là
// cổng đã dừng và người vừa resume (quyết định của người) nên đi tiếp; chưa áp ("") thì áp cổng ngay bây giờ.
func (r *Runner) preflightPlanSize(rs *runState) bool {
	c := rs.t.State.Classification
	if !rs.planSizeGated() || rs.t.State.Status != task.StatusPlanned || c == nil || !c.Plan {
		return true
	}
	switch rs.t.State.PlanSize {
	case planSizePassed:
		return true
	case planSizeBlocked:
		r.setPlanSize(rs, planSizePassed)
		return true
	}
	return r.planSizeGate(rs)
}

// planSizeGate áp cổng cỡ kế hoạch sau b-req của b-do. Trả true khi b-feat được chạy tiếp (kế hoạch nhỏ, hoặc người đồng ý khi hỏi),
// false khi tác vụ dừng chờ người (blocked, đã ghi lý do). Không đặt LastStep awaiting_human và giữ State.LastPlan: resume vào thẳng
// b-feat (nối tiếp phiên b-req) thay vì chạy lại b-req.
func (r *Runner) planSizeGate(rs *runState) bool {
	if !rs.planSizeGated() {
		return true
	}
	why := r.planSizeReasons(rs.t)
	if len(why) == 0 {
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("cỡ kế hoạch: nhỏ (kết luận của b-req khớp số đo của Bint; tối đa %d tệp/nhóm bề mặt): chạy b-feat ngay, "+
			"kiểm chứng, review và Kiểm tra mục tiêu giữ như đường đầy đủ", r.Cfg.Workflow.SmallPlanMaxFiles))
		r.setPlanSize(rs, planSizePassed)
		return true
	}
	id := rs.t.State.ID
	list := strings.Join(why, "; ")
	if pv := r.slicePreview(rs); pv != "" {
		list += "; " + pv // kế hoạch lớn không chỉ dừng báo: nêu các lát Bint sẽ chạy khi người cho chạy b-feat (slice.go)
	}
	if r.Confirm != nil {
		q := Question{Key: QConfirmPlanSize, Params: map[string]any{"task": id, "reasons": list},
			Text: fmt.Sprintf("Kế hoạch của tác vụ %s là lớn (%s). Xem plan.md (bint task show %s) rồi chọn: chạy b-feat luôn?", id, list, id)}
		if r.Confirm(q) {
			rs.res.Notes = append(rs.res.Notes, "cỡ kế hoạch: lớn ("+list+"); người đã đồng ý chạy b-feat")
			r.setPlanSize(rs, planSizePassed)
			return true
		}
	}
	r.setPlanSize(rs, planSizeBlocked)
	r.block(rs, fmt.Sprintf("kế hoạch lớn: %s — b-feat chưa chạy. Xem kế hoạch (`bint task show %s`; requirement.md, plan.md, tasks.md), sửa nếu cần, "+
		"rồi `%s` để chạy b-feat (không chạy lại b-req)", list, id, r.resumeCmd(rs)))
	return false
}

// planConfigBlock là khối đề bài của bước lập kế hoạch nêu các khoá workflow ĐÃ HỢP NHẤT (.bint/config.yml, .bint/local.yml, cấu hình
// người dùng) mà b-req dựa vào khi chọn chiến lược. Planner đọc một tệp .bint/*.yml riêng lẻ có thể thấy giá trị đã bị lớp sau ghi đè
// (benchmark đợt 6: đọc slice_timeout_minutes: 0 ở config.yml trong khi local.yml đặt 30, nên chọn không chia lát).
func planConfigBlock(w config.Workflow) string {
	slicing := "bật (theo dòng `Chia lát` của plan.md)"
	switch {
	case w.SliceTimeoutMinutes <= 0 || w.Slicing == config.GateOff:
		slicing = "tắt (ghi `Chia lát: không`)"
	case w.Slicing == config.GateAlways:
		slicing = "luôn chia kế hoạch lớn"
	}
	verify := w.Verify.Scope
	if w.Verify.Scope == config.VerifyScopeAffected && strings.TrimSpace(w.Verify.AffectedCommand) != "" {
		verify += " (`" + strings.TrimSpace(w.Verify.AffectedCommand) + "`)"
	}
	var b strings.Builder
	b.WriteString("Cấu hình hiệu lực của Bint (đã hợp nhất .bint/config.yml, .bint/local.yml và cấu hình người dùng). Dùng đúng các giá trị " +
		"này; đừng suy cấu hình từ một tệp .bint/*.yml riêng lẻ vì lớp sau ghi đè lớp trước:\n")
	fmt.Fprintf(&b, "- workflow.slicing: %s, workflow.slice_timeout_minutes: %d → chia lát: %s\n", w.Slicing, w.SliceTimeoutMinutes, slicing)
	fmt.Fprintf(&b, "- workflow.goal_check: %s\n", w.GoalCheck)
	fmt.Fprintf(&b, "- workflow.small_plan_max_files: %d\n", w.SmallPlanMaxFiles)
	fmt.Fprintf(&b, "- workflow.verify.scope: %s\n", verify)
	fmt.Fprintf(&b, "- workflow.max_review_rounds: %d\n\n", w.MaxReviewRounds)
	return b.String()
}
