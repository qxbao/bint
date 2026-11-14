package task

import (
	"slices"
	"strings"
)

// Tên trường kế thừa từ phiếu cha (State.Inherited).
const (
	InheritAllow       = "scope.allow"
	InheritDeny        = "scope.deny"
	InheritReferences  = "references"
	InheritConstraints = "constraints"
)

// Ready trả các phiếu chưa kết thúc mà mọi phiếu trong DependsOn đều có trong ts và đang done; cũ nhất trước
// (ULID tăng dần). Phụ thuộc cancelled, chưa xong hoặc không nạp được (không có trong ts) làm phiếu vắng mặt.
// Phiếu nằm trong chu trình không bao giờ sẵn sàng vì phụ thuộc của nó không thể done trước nó.
func Ready(ts []*Task) []*Task {
	status := make(map[string]string, len(ts))
	for _, t := range ts {
		status[t.State.ID] = t.State.Status
	}
	var out []*Task
	for _, t := range ts {
		if Terminal(t.State.Status) {
			continue
		}
		ok := true
		for _, d := range t.State.DependsOn {
			if status[d] != StatusDone {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b *Task) int { return strings.Compare(a.State.ID, b.State.ID) })
	return out
}

// FindCycle kiểm việc đặt phiếu id phụ thuộc thêm add (cạnh id → add) có tạo chu trình không. deps ánh xạ ID
// sang DependsOn hiện có (chưa gồm cạnh mới). Trả đường chu trình bắt đầu và kết thúc ở id (vd. A → B → A), nil
// nếu không có. Đồ thị sẵn có chu trình không liên quan do sửa tay không làm lặp vô hạn.
func FindCycle(deps map[string][]string, id string, add []string) []string {
	seen := map[string]bool{}
	var walk func(n string, path []string) []string
	walk = func(n string, path []string) []string {
		if n == id {
			return append(path, n)
		}
		if seen[n] {
			return nil
		}
		seen[n] = true
		for _, d := range deps[n] {
			if p := walk(d, append(slices.Clone(path), n)); p != nil {
				return p
			}
		}
		return nil
	}
	for _, a := range add {
		if p := walk(a, []string{id}); p != nil {
			return p
		}
	}
	return nil
}

// Inherit chép từ phiếu cha vào phiếu con từng trường còn rỗng (scope.allow, scope.deny, references, constraints)
// và trả tên các trường đã chép. clearScope (--clear-scope) nghĩa là người lập tự đặt phạm vi: không chép scope.
func Inherit(child *Ticket, parent Ticket, clearScope bool) []string {
	var got []string
	if !clearScope && len(child.Scope.Allow) == 0 && len(parent.Scope.Allow) > 0 {
		child.Scope.Allow = slices.Clone(parent.Scope.Allow)
		got = append(got, InheritAllow)
	}
	if !clearScope && len(child.Scope.Deny) == 0 && len(parent.Scope.Deny) > 0 {
		child.Scope.Deny = slices.Clone(parent.Scope.Deny)
		got = append(got, InheritDeny)
	}
	if len(child.References) == 0 && len(parent.References) > 0 {
		child.References = slices.Clone(parent.References)
		got = append(got, InheritReferences)
	}
	if len(child.Constraints) == 0 && len(parent.Constraints) > 0 {
		child.Constraints = slices.Clone(parent.Constraints)
		got = append(got, InheritConstraints)
	}
	return got
}
