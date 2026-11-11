// Package tasks là màn Tác vụ (docs/tui-plan.md mục 5.3): danh sách theo trạng thái (tác vụ chờ người lên đầu),
// chi tiết theo thẻ con (Phiếu, Tài liệu, Tiêu chí, Kiểm chứng, Review, Lỗi, Lượt chạy, Log khi có việc nền) và
// các hành động tạo/chạy/tiếp tục/duyệt/đổi trạng thái/chuyển phát hiện thành tri thức. Mọi thao tác ghi đi qua
// actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá theo sổ hành động.
package tasks

import (
	"context"
	"os"
	"sort"
	"time"

	"bint/internal/app"
	"bint/internal/task"
	"bint/internal/tui/actions"
)

// Data là dữ liệu của danh sách tác vụ (mọi tác vụ, kể cả done/cancelled; màn hình tự ẩn/hiện).
type Data struct {
	Tasks []*task.Task
	// Bad là số tệp tác vụ không đọc được.
	Bad int
	// Files là các tệp được Git theo dõi (gợi ý cho ô phạm vi).
	Files []string
	// Root là thư mục gốc repository (đọc trích đoạn tệp của phát hiện review).
	Root  string
	Stamp time.Time
}

// IDs trả ID của mọi tác vụ.
func (d Data) IDs() []string {
	out := make([]string, len(d.Tasks))
	for i, t := range d.Tasks {
		out[i] = t.State.ID
	}
	return out
}

// Detail là chi tiết một tác vụ kèm nội dung ba tài liệu.
type Detail struct {
	app.TaskDetail
	// Docs là nội dung tài liệu theo tên (requirement, plan, tasks); thiếu thì không có khoá.
	Docs map[string]string
}

// LoadedMsg là kết quả nạp danh sách.
type LoadedMsg struct {
	Data Data
	Err  error
}

// DetailMsg là kết quả nạp chi tiết một tác vụ.
type DetailMsg struct {
	ID     string
	Detail Detail
	Err    error
}

// RunsMsg là kết quả nạp các lượt chạy của một tác vụ.
type RunsMsg struct {
	ID   string
	Runs app.TaskRunsResult
	Err  error
}

// RefreshMsg yêu cầu nạp lại danh sách và chi tiết (việc nền vừa đổi trạng thái tác vụ).
type RefreshMsg struct{}

// Target cho mô hình gốc biết các thông điệp này thuộc màn Tác vụ.
func (LoadedMsg) Target() actions.Screen  { return actions.Tasks }
func (DetailMsg) Target() actions.Screen  { return actions.Tasks }
func (RunsMsg) Target() actions.Screen    { return actions.Tasks }
func (RefreshMsg) Target() actions.Screen { return actions.Tasks }

// Loader nạp danh sách; Detailer nạp chi tiết một tác vụ; RunsLoader nạp các lượt chạy của nó. Thay được trong
// kiểm thử; nil = nạp thật. readOnly: không ghi lại trạng thái của tiến trình điều phối đã chết, không nạp spool.
type (
	Loader     func(ctx context.Context, dir string, readOnly bool) (Data, error)
	Detailer   func(ctx context.Context, dir, id string, readOnly bool) (Detail, error)
	RunsLoader func(ctx context.Context, dir, id string, readOnly bool) (app.TaskRunsResult, error)
)

// maxSuggestFiles giới hạn số tệp gợi ý để bộ nhớ và thời gian nạp không phình theo repository lớn.
const maxSuggestFiles = 20000

// Load đọc mọi tác vụ của repository chứa dir.
func Load(ctx context.Context, dir string, readOnly bool) (Data, error) {
	ws, err := app.OpenTasks(ctx, dir, "")
	if err != nil {
		return Data{}, err
	}
	l, err := app.ListTasks(ctx, ws, app.TaskListOptions{All: true, NoReconcile: readOnly})
	if err != nil {
		return Data{}, err
	}
	return Data{Tasks: l.Tasks, Bad: len(l.Bad), Files: app.TrackedFiles(ws.K, maxSuggestFiles), Root: ws.Root, Stamp: time.Now()}, nil
}

// LoadDetail đọc chi tiết tác vụ id và nội dung các tài liệu.
func LoadDetail(ctx context.Context, dir, id string, readOnly bool) (Detail, error) {
	ws, err := app.OpenTasks(ctx, dir, "")
	if err != nil {
		return Detail{}, err
	}
	d, err := app.ShowTask(ctx, ws, app.TaskShowOptions{Ref: id, NoReconcile: readOnly})
	if err != nil {
		return Detail{}, err
	}
	out := Detail{TaskDetail: d, Docs: map[string]string{}}
	for _, a := range d.Artifacts {
		if a.Exists {
			if b, err := os.ReadFile(a.Path); err == nil {
				out.Docs[a.Name] = string(b)
			}
		}
	}
	return out, nil
}

// LoadRuns đọc các lượt chạy gắn với tác vụ id (không nạp spool ở chế độ chỉ đọc).
func LoadRuns(ctx context.Context, dir, id string, readOnly bool) (app.TaskRunsResult, error) {
	ws, err := app.OpenTasks(ctx, dir, "")
	if err != nil {
		return app.TaskRunsResult{}, err
	}
	return app.TaskRuns(ctx, ws, app.TaskRunsOptions{Ref: id, NoIngest: readOnly})
}

// Nhóm trạng thái trong danh sách, theo thứ tự hiển thị.
const (
	groupWaiting  = iota // blocked, interrupted, failed: chờ người
	groupActive          // in_progress, in_review
	groupPlanned         // created, planned
	groupFinished        // done, cancelled
	numGroups
)

// GroupOf trả nhóm của một trạng thái tác vụ.
func GroupOf(status string) int {
	switch status {
	case task.StatusBlocked, task.StatusInterrupted, task.StatusFailed:
		return groupWaiting
	case task.StatusInProgress, task.StatusInReview:
		return groupActive
	case task.StatusDone, task.StatusCancelled:
		return groupFinished
	}
	return groupPlanned
}

// Waiting cho biết tác vụ đang chờ người (blocked, interrupted, failed).
func Waiting(t *task.Task) bool { return GroupOf(t.State.Status) == groupWaiting }

// sortTasks xếp theo nhóm trạng thái rồi theo thời điểm cập nhật mới nhất trước, rồi ID.
func sortTasks(ts []*task.Task) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if ga, gb := GroupOf(a.State.Status), GroupOf(b.State.Status); ga != gb {
			return ga < gb
		}
		if a.State.UpdatedAt != b.State.UpdatedAt {
			return a.State.UpdatedAt > b.State.UpdatedAt
		}
		return a.State.ID > b.State.ID
	})
}
