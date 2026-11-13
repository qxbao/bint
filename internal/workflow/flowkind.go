package workflow

// Runner chọn bước, trạng thái tác vụ, chế độ chỉ đọc và cổng sau bước theo kind của flow đang chạy (GĐ3 và GĐ4 của
// docs/custom-flow-plan.md), không theo tên bước. Các hằng StepReq/StepFeat/StepReview/StepDo chỉ còn là tên workflow
// gốc (khoá limits.workflows.<tên>, sự kiện spool, tên log, lệnh `bint run <tên>`), không dùng để rẽ nhánh hành vi.
//
// Chế độ chỉ đọc, role và subagent của một bước có MỘT nguồn: StepSpec dựng từ flow của tác vụ đang chạy (rs.flow). Flow
// tuỳ biến có thể đặt id bước trùng tên bước gốc (ví dụ bước implement tên b-review), nên không tra theo id trên flow nhúng.

import (
	"errors"
	"fmt"
	"strings"

	"bint/internal/flow"
	adapters "bint/internal/harness"
	"bint/internal/skills"
	"bint/internal/task"
)

// StepSpec là thuộc tính của một bước theo flow đang chạy: bên tạo agent, khoá worktree, MCP và driver session đều dùng
// nó thay vì tự suy từ id bước.
type StepSpec struct {
	ID    string
	Kind  flow.Kind
	Skill string
	// Role là role của bước (role tuỳ biến của flow, hoặc role gốc theo kind); Base là role gốc theo kind
	// (flow.DefaultRole): role chưa cấu hình rơi về Base.
	Role, Base string
	ReadOnly   bool
}

// specOf dựng StepSpec từ một bước của flow.
func specOf(s flow.Step) StepSpec {
	return StepSpec{ID: s.ID, Kind: s.Kind, Skill: s.Skill, Role: s.EffectiveRole(), Base: flow.DefaultRole(s.Kind),
		ReadOnly: readOnlyKind(s.Kind)}
}

// spec trả StepSpec của bước theo flow đang chạy; Kind rỗng (ReadOnly false) với bước giả hoặc id không thuộc flow.
func (rs *runState) spec(step string) StepSpec {
	s, ok := rs.flow.StepByID(step)
	if !ok {
		return StepSpec{ID: step}
	}
	return specOf(s)
}

// builtinFlow trả flow nhúng theo tên workflow; Def rỗng (không bước nào) nếu tên không phải workflow gốc.
func builtinFlow(name string) flow.Def {
	d, _ := flow.GetBuiltin(name)
	return d
}

// isCustomFlow cho biết tên workflow không phải workflow gốc (tức flow tuỳ biến trong .bint/flows).
func isCustomFlow(name string) bool { return name != "" && !flow.IsBuiltin(name) }

// kindOf trả kind của bước theo flow đang chạy; "" với bước giả (post-feat, verify, scope) hoặc id không thuộc flow.
func (rs *runState) kindOf(step string) flow.Kind {
	s, _ := rs.flow.StepByID(step)
	return s.Kind
}

// currentImplement là bước implement đang chạy hoặc kế tiếp: State.FlowStep nếu nó là bước implement của flow, không thì
// bước implement đầu (tác vụ cũ, tác vụ vừa vào in_progress, flow nhúng một bước implement).
func (rs *runState) currentImplement() flow.Step {
	if s, ok := rs.flow.StepByID(rs.t.State.FlowStep); ok && s.Kind == flow.KindImplement {
		return s
	}
	s, _ := rs.flow.FirstOfKind(flow.KindImplement)
	return s
}

// currentReview là bước review đang chạy hoặc kế tiếp: State.FlowStep nếu nó là bước review, không thì bước review đầu.
func (rs *runState) currentReview() flow.Step {
	if s, ok := rs.flow.StepByID(rs.t.State.FlowStep); ok && s.Kind == flow.KindReview {
		return s
	}
	s, _ := rs.flow.FirstOfKind(flow.KindReview)
	return s
}

// stepAfter trả bước đứng ngay sau bước id trong flow (ok=false nếu id là bước cuối hoặc không thuộc flow).
func (rs *runState) stepAfter(id string) (flow.Step, bool) {
	for i, s := range rs.flow.Steps {
		if s.ID == id {
			if i+1 < len(rs.flow.Steps) {
				return rs.flow.Steps[i+1], true
			}
			break
		}
	}
	return flow.Step{}, false
}

// lastImplement trả bước implement cuối của flow.
func (rs *runState) lastImplement() flow.Step {
	var out flow.Step
	for _, s := range rs.flow.Steps {
		if s.Kind == flow.KindImplement {
			out = s
		}
	}
	return out
}

// countKind đếm bước của flow theo kind.
func (rs *runState) countKind(k flow.Kind) int {
	n := 0
	for _, s := range rs.flow.Steps {
		if s.Kind == k {
			n++
		}
	}
	return n
}

// readOnlyKind: bước plan và review không được sửa tệp; chỉ bước implement có quyền ghi.
func readOnlyKind(k flow.Kind) bool { return k == flow.KindPlan || k == flow.KindReview }

// kindOfPending trả kind của bước đang dở: Kind đã ghi lúc mở bước; phiếu của bản cũ không có thì tra bước của flow nhúng.
func kindOfPending(ps *task.PendingStep) flow.Kind {
	if ps.Kind != "" {
		return flow.Kind(ps.Kind)
	}
	s, _ := flow.BuiltinStep(ps.Step)
	return s.Kind
}

// subagentOf trả tên subagent theo kind của bước ("" nếu kind lạ): subagent là theo kind (planner/implementer/reviewer),
// không theo role tuỳ biến, vì Bint chỉ cài ba subagent.
func subagentOf(k flow.Kind) string { return adapters.SubagentName(flow.DefaultRole(k)) }

// ---- nạp và kiểm flow

// configRoot là thư mục chứa .bint/ (nơi đọc flow và lớp phủ skill).
func (e *Env) configRoot() string {
	if e.ConfigRoot != "" {
		return e.ConfigRoot
	}
	return e.Root
}

// FlowSkillExists cho biết skill có dùng được cho flow không: skill gốc, hoặc skill tuỳ biến qua kiểm hình thức không
// lỗi (skill lỗi lint không được xuất bản nên flow không được dựa vào). Skill tiến hoá không tính. Dùng chung cho
// `bint flow lint` và cho Runner, để lint và chạy cùng một luật.
func FlowSkillExists(root string) func(string) bool {
	return func(name string) bool {
		if _, ok := skills.Get(name); ok {
			return true
		}
		cs, err := skills.LoadCustomOne(root, name)
		if err != nil {
			return false
		}
		errs, _ := skills.CheckCustom(root, cs)
		return len(errs) == 0
	}
}

// ErrUnknownWorkflow: tên không phải workflow gốc và không có flow tuỳ biến trùng tên.
var ErrUnknownWorkflow = errors.New("workflow không hợp lệ")

// resolveFlow nạp flow theo tên: flow nhúng, hoặc flow tuỳ biến ở .bint/flows/<tên>.yml của ConfigRoot. Flow tuỳ biến
// phải qua flow.Validate (kể cả skill của bước tồn tại) trước khi chạy; kết quả đã chuẩn hoá role.
func (e *Env) resolveFlow(name string) (flow.Def, error) {
	if d, ok := flow.GetBuiltin(name); ok {
		return d, nil
	}
	root := e.configRoot()
	d, err := flow.Get(root, name)
	if err != nil {
		if errors.Is(err, flow.ErrNotFound) {
			return flow.Def{}, fmt.Errorf("%w: %q không phải workflow gốc (%s) và không có flow %s/%s.yml",
				ErrUnknownWorkflow, name, strings.Join(flow.BuiltinNames(), ", "), flow.Rel, name)
		}
		return flow.Def{}, err
	}
	if errs := flow.Validate(d, name, FlowSkillExists(root)); len(errs) > 0 {
		return flow.Def{}, fmt.Errorf("flow %q không hợp lệ (xem `bint flow lint %s`): %s", name, name, strings.Join(errs, "; "))
	}
	return flow.Normalize(d), nil
}

// flowGate kiểm flow của một tác vụ flow tuỳ biến trước khi chạy: trả flow hiện tại và lý do chặn ("" nếu chạy được).
// Chặn khi flow không nạp/kiểm được (đã bị xoá hoặc sửa hỏng) hoặc băm định nghĩa khác FlowSHA ghi lúc lập phiếu (flow
// đã đổi giữa chừng): không trộn hai định nghĩa trong một tác vụ. Phiếu không có FlowSHA không bị kiểm băm.
func (e *Env) flowGate(t *task.Task) (flow.Def, string) {
	name := t.State.Workflow
	d, err := e.resolveFlow(name)
	if err != nil {
		return flow.Def{Name: name}, flowBlockReason(t, name, fmt.Sprintf("flow %s không còn dùng được: %v", name, err))
	}
	if sha := t.State.FlowSHA; sha != "" && sha != flow.Hash(d) {
		return d, flowBlockReason(t, name, fmt.Sprintf("flow %s đã đổi từ lúc lập phiếu", name))
	}
	return d, ""
}

func flowBlockReason(t *task.Task, name, why string) string {
	return fmt.Sprintf("%s: khôi phục %s/%s.yml về bản lúc lập phiếu rồi `bint run resume %s`, hoặc huỷ tác vụ bằng `bint task cancel %s`",
		why, flow.Rel, name, t.State.ID, t.State.ID)
}

// chain cho biết Runner điều phối cả chuỗi bước của flow: flow nhiều bước, hoặc MỌI flow tuỳ biến (kể cả flow chỉ có
// một bước implement). Flow gốc một bước (b-req/b-feat/b-review) chỉ chạy đúng bước đó do b-do hoặc người điều phối;
// flow tuỳ biến không có điều phối viên nào khác nên phải tới done (sau implement cuối: review nếu có, không thì
// finalize) và chịu ngân sách chung limits.workflows.<flow> (tên flow khác id bước).
func (rs *runState) chain() bool { return rs.flow.Multi() || isCustomFlow(rs.flow.Name) }
