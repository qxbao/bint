package tasks

import (
	"slices"
	"strings"

	"bint/internal/task"
)

// siblingOrder xếp ts (phiếu anh em) sao cho phiếu được phụ thuộc đứng trước (Kahn theo depends_on giữa chính
// các phiếu trong ts); hoà thì giữ thứ tự của ts (thứ tự nhóm/mới cập nhật của danh sách). Phiếu trong chu trình
// (không gỡ được) nối vào cuối theo thứ tự của ts.
func siblingOrder(ts []*task.Task) []*task.Task {
	in := make(map[string]*task.Task, len(ts))
	pos := make(map[string]int, len(ts))
	for i, t := range ts {
		in[t.State.ID] = t
		pos[t.State.ID] = i
	}
	pending := make(map[string]int, len(ts))
	users := map[string][]string{}
	for _, t := range ts {
		seen := map[string]bool{}
		for _, d := range t.State.DependsOn {
			if _, ok := in[d]; !ok || d == t.State.ID || seen[d] {
				continue
			}
			seen[d] = true
			pending[t.State.ID]++
			users[d] = append(users[d], t.State.ID)
		}
	}
	var ready []string
	for _, t := range ts {
		if pending[t.State.ID] == 0 {
			ready = append(ready, t.State.ID)
		}
	}
	out := make([]*task.Task, 0, len(ts))
	done := map[string]bool{}
	for len(ready) > 0 {
		slices.SortFunc(ready, func(a, b string) int { return pos[a] - pos[b] })
		id := ready[0]
		ready = ready[1:]
		done[id] = true
		out = append(out, in[id])
		for _, u := range users[id] {
			if pending[u]--; pending[u] == 0 {
				ready = append(ready, u)
			}
		}
	}
	for _, t := range ts {
		if !done[t.State.ID] {
			out = append(out, t)
		}
	}
	return out
}

// planOf trả kế hoạch chứa phiếu id: leo parent tới gốc rồi lấy mọi hậu duệ, đóng bao theo depends_on hai chiều
// (phiếu phụ thuộc hoặc được phụ thuộc bởi phiếu đã có). Phiếu lẻ (không cha, không con, không phụ thuộc) trả nil.
// Thứ tự theo ts; mọi duyệt có tập đã thăm nên chu trình không treo.
func planOf(ts []*task.Task, id string) []*task.Task {
	by := make(map[string]*task.Task, len(ts))
	near := map[string][]string{} // id → phiếu con và phiếu phụ thuộc vào nó
	for _, t := range ts {
		by[t.State.ID] = t
		if p := t.State.Parent; p != "" {
			near[p] = append(near[p], t.State.ID)
		}
		for _, d := range t.State.DependsOn {
			near[d] = append(near[d], t.State.ID)
		}
	}
	cur, ok := by[id]
	if !ok {
		return nil
	}
	seen := map[string]bool{cur.State.ID: true}
	for p := cur.State.Parent; p != ""; {
		pt, ok := by[p]
		if !ok || seen[p] {
			break
		}
		seen[p] = true
		cur, p = pt, pt.State.Parent
	}
	root := cur.State.ID
	in := map[string]bool{}
	queue := []string{root, id}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if in[n] || by[n] == nil {
			continue
		}
		in[n] = true
		for _, c := range near[n] {
			if !in[c] {
				queue = append(queue, c)
			}
		}
		queue = append(queue, by[n].State.DependsOn...)
		if p := by[n].State.Parent; p != "" {
			queue = append(queue, p)
		}
	}
	var out []*task.Task
	for _, t := range ts {
		if in[t.State.ID] {
			out = append(out, t)
		}
	}
	if len(out) <= 1 {
		return nil
	}
	return out
}

// graphLayers là kết quả xếp bậc của một kế hoạch.
type graphLayers struct {
	Levels  [][]*task.Task // Levels[i]: phiếu bậc i+1, ID tăng
	Cycle   []*task.Task   // phiếu trong chu trình hoặc phụ thuộc vào chu trình
	Missing []*task.Task   // phiếu có phụ thuộc không đọc được (không có trong ts)
}

// layers xếp plan thành bậc topo theo depends_on: bậc = 1 + bậc cao nhất của phụ thuộc; phụ thuộc ngoài plan nhưng có
// trong all tính là bậc 0 (đã nạp, không vẽ); phụ thuộc không có trong all → phiếu vào Missing (bậc vẫn tính trên
// phụ thuộc còn lại). Phần Kahn không xử lý được là chu trình.
func layers(plan, all []*task.Task) graphLayers {
	inPlan := make(map[string]bool, len(plan))
	for _, t := range plan {
		inPlan[t.State.ID] = true
	}
	known := make(map[string]bool, len(all))
	for _, t := range all {
		known[t.State.ID] = true
	}
	level := map[string]int{}
	var g graphLayers
	missing := map[string]bool{}
	for _, t := range plan {
		for _, d := range t.State.DependsOn {
			if !known[d] && !missing[t.State.ID] {
				missing[t.State.ID] = true
				g.Missing = append(g.Missing, t)
			}
		}
	}
	left := map[string]*task.Task{}
	for _, t := range plan {
		left[t.State.ID] = t
	}
	for len(left) > 0 {
		progressed := false
		for _, t := range plan {
			if _, ok := left[t.State.ID]; !ok {
				continue
			}
			lv, ok := 1, true
			for _, d := range t.State.DependsOn {
				if !inPlan[d] || d == t.State.ID {
					if d == t.State.ID {
						ok = false
					}
					continue
				}
				dl, done := level[d]
				if !done {
					ok = false
					break
				}
				lv = max(lv, dl+1)
			}
			if !ok {
				continue
			}
			level[t.State.ID] = lv
			delete(left, t.State.ID)
			progressed = true
			for len(g.Levels) < lv {
				g.Levels = append(g.Levels, nil)
			}
			g.Levels[lv-1] = append(g.Levels[lv-1], t)
		}
		if !progressed {
			break
		}
	}
	for _, t := range plan {
		if _, ok := left[t.State.ID]; ok {
			g.Cycle = append(g.Cycle, t)
		}
	}
	for _, l := range g.Levels {
		slices.SortStableFunc(l, func(a, b *task.Task) int { return strings.Compare(a.State.ID, b.State.ID) })
	}
	return g
}
