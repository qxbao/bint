package workflow

import (
	"context"
	"strings"
	"testing"

	"bint/internal/task"
)

// Lát đầu phải làm cho test đóng băng biên dịch được (stub API của lát sau); chỉ nhắc khi có test đóng băng và chỉ ở lát đầu.
func TestSliceBlockFirstSliceStubNote(t *testing.T) {
	mk := func(frozen bool, activeN int) *task.Task {
		tk := &task.Task{}
		tk.State.Slices = []task.Slice{
			{N: 1, Text: "A", Status: task.SlicePending},
			{N: 2, Text: "B", Status: task.SlicePending},
		}
		tk.State.Slices[activeN-1].Status = task.SliceActive
		if frozen {
			tk.State.GoalTest = &task.GoalTest{}
		}
		return tk
	}
	if b := sliceBlock(mk(true, 1)); !strings.Contains(b, "BIÊN DỊCH ĐƯỢC") || !strings.Contains(b, "[build failed]") {
		t.Errorf("lát 1 có test đóng băng phải dặn stub: %s", b)
	}
	if b := sliceBlock(mk(true, 2)); strings.Contains(b, "BIÊN DỊCH ĐƯỢC") {
		t.Errorf("lát 2 không cần lời dặn stub: %s", b)
	}
	if b := sliceBlock(mk(false, 1)); strings.Contains(b, "BIÊN DỊCH ĐƯỢC") {
		t.Errorf("không có test đóng băng thì không dặn: %s", b)
	}
	if !strings.Contains(sliceGuideBlock, "biên dịch được") {
		t.Errorf("sliceGuideBlock phải dặn nhóm đầu làm test đóng băng biên dịch được")
	}
}

// deleteTaskRefs phải xoá cả ref slice-<N>-discarded (for-each-ref khớp theo thành phần đường dẫn nên phải liệt kê theo thư mục tác vụ).
func TestDeleteTaskRefsRemovesDiscardedSliceRefs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	head := strings.TrimSpace(h.git("rev-parse", "HEAD"))
	const id, other = "TASKA", "TASKB"
	for _, r := range []string{
		baseRef(id), greenRef(id), "refs/bint/tasks/" + id + "/slice-1-discarded", "refs/bint/tasks/" + id + "/slice-12-discarded",
		sliceGreenRef(id), "refs/bint/tasks/" + other + "/slice-1-discarded", "refs/bint/tasks/" + id + "X/slice-1-discarded",
	} {
		h.git("update-ref", r, head)
	}
	if err := deleteTaskRefs(ctx, h.root, id); err != nil {
		t.Fatal(err)
	}
	left := h.git("for-each-ref", "--format=%(refname)", "refs/bint/tasks/")
	if strings.Contains(left, "refs/bint/tasks/"+id+"/") {
		t.Errorf("còn ref của tác vụ %s: %s", id, left)
	}
	for _, keep := range []string{"refs/bint/tasks/" + other + "/slice-1-discarded", "refs/bint/tasks/" + id + "X/slice-1-discarded"} {
		if !strings.Contains(left, keep) {
			t.Errorf("không được xoá ref của tác vụ khác: %s", keep)
		}
	}
}
