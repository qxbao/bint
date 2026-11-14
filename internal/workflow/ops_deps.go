package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"bint/internal/task"
)

// ErrDependencyInput bọc lỗi đầu vào của quan hệ giữa các phiếu (ID không có, tiền tố mơ hồ, trỏ chính mình,
// chu trình). Lớp app đổi nó thành lỗi cách dùng.
var ErrDependencyInput = errors.New("đầu vào quan hệ phiếu không hợp lệ")

func dependencyInputf(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrDependencyInput, fmt.Sprintf(format, a...))
}

// resolveTaskRefs đổi ID/tiền tố thành ID đầy đủ (bỏ trùng, giữ thứ tự). flag là tên cờ để báo lỗi.
func (e *Env) resolveTaskRefs(flag string, refs []string) ([]string, error) {
	var out []string
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		id, err := e.Tasks.Resolve(r)
		if err != nil {
			return nil, dependencyInputf("%s %q: %v", flag, r, err)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// checkNoCycle từ chối cạnh id → add nếu nó tạo chu trình phụ thuộc; đồ thị dựng từ mọi phiếu đọc được,
// riêng phiếu id lấy theo cur (phụ thuộc hiện có sau khi xoá/trước khi thêm).
func (e *Env) checkNoCycle(id string, cur, add []string) error {
	ts, _ := e.Tasks.List()
	deps := make(map[string][]string, len(ts))
	for _, t := range ts {
		deps[t.State.ID] = t.State.DependsOn
	}
	deps[id] = cur
	if p := task.FindCycle(deps, id, add); p != nil {
		return dependencyInputf("phụ thuộc tạo chu trình: %s", strings.Join(p, " → "))
	}
	return nil
}

// SetDependencies thêm add vào depends_on của phiếu ref (sau khi xoá hết nếu clear). Chỉ ghi state.json; không
// đổi task.yml. Khoá thư mục chỉ theo từng phiếu nên hai lệnh song song A→B và B→A có thể cùng qua kiểm.
func (e *Env) SetDependencies(ref string, add []string, clear bool) (*task.Task, error) {
	id, err := e.Tasks.Resolve(ref)
	if err != nil {
		return nil, err
	}
	deps, err := e.resolveTaskRefs("--depends-on", add)
	if err != nil {
		return nil, err
	}
	return e.Tasks.Update(id, e.now(), func(t *task.Task) error {
		if task.Terminal(t.State.Status) {
			return fmt.Errorf("tác vụ đã %s", t.State.Status)
		}
		return e.applyDependencies(t, deps, clear)
	})
}

// applyDependencies (gọi trong callback Update): kiểm chu trình rồi đặt State.DependsOn; lỗi thì không đổi gì.
func (e *Env) applyDependencies(t *task.Task, deps []string, clear bool) error {
	cur := slices.Clone(t.State.DependsOn)
	if clear {
		cur = nil
	}
	next := slices.Clone(cur)
	for _, d := range deps {
		if !slices.Contains(next, d) {
			next = append(next, d)
		}
	}
	if err := e.checkNoCycle(t.State.ID, cur, deps); err != nil {
		return err
	}
	t.State.DependsOn = next
	return nil
}

// SetTicketAndDependencies sửa phiếu và phụ thuộc trong MỘT lần cập nhật: kiểm hết rồi mới ghi, nên lỗi ở bên
// nào cũng không để state.json cập nhật dở dang.
func (e *Env) SetTicketAndDependencies(ref string, ed TicketEdit, add []string, clear bool) (*task.Task, task.Assessment, []string, error) {
	if !e.Human && ed.Autonomy != nil && *ed.Autonomy == task.AutonomyAuto {
		return nil, task.Assessment{}, nil, fmt.Errorf("%w: chỉ người mới đặt được autonomy: auto", ErrAgent)
	}
	id, err := e.Tasks.Resolve(ref)
	if err != nil {
		return nil, task.Assessment{}, nil, err
	}
	deps, err := e.resolveTaskRefs("--depends-on", add)
	if err != nil {
		return nil, task.Assessment{}, nil, err
	}
	var sync criteriaSync
	t, err := e.Tasks.Update(id, e.now(), func(t *task.Task) error {
		if task.Terminal(t.State.Status) {
			return fmt.Errorf("tác vụ đã %s", t.State.Status)
		}
		before := slices.Clone(t.Ticket.Verify.Criteria)
		if err := ed.apply(&t.Ticket); err != nil {
			return err
		}
		if err := e.applyDependencies(t, deps, clear); err != nil {
			return err
		}
		return e.syncCriteria(t, before, &sync)
	})
	if err != nil {
		return nil, task.Assessment{}, nil, err
	}
	e.emitSynced(t, sync)
	return t, e.Assess(t), sync.notes, nil
}

// Children trả các phiếu con (State.Parent == id), cũ nhất trước.
func (e *Env) Children(id string) []*task.Task {
	ts, _ := e.Tasks.List()
	var out []*task.Task
	for _, t := range ts {
		if t.State.Parent == id {
			out = append(out, t)
		}
	}
	slices.Reverse(out) // List: mới nhất trước
	return out
}
