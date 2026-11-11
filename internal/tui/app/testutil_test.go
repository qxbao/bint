package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/task"
	evscreen "bint/internal/tui/screens/evolve"
	knscreen "bint/internal/tui/screens/knowledge"
	"bint/internal/tui/screens/overview"
	rnscreen "bint/internal/tui/screens/runs"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
)

// Giờ trong ảnh chụp (golden) phải không phụ thuộc múi giờ máy chạy kiểm thử.
func init() { time.Local = time.UTC }

// sampleData là dữ liệu Tổng quan cố định cho kiểm thử và ảnh chụp.
func sampleData() overview.Data {
	return overview.Data{
		Status: &bintapp.StatusReport{
			Repo: "/work/demo", RepoID: "7f3a9c", StateDir: "/state/demo-7f3a9c", Schema: 6,
			Harnesses: []string{"claude", "codex"}, Runs: map[string]int{"completed": 33, "failed": 3, "interrupted": 1, "running": 5}, TotalRuns: 42,
			PendingProposals: 2,
		},
		Doctor: &bintapp.DoctorReport{Checks: []bintapp.Check{
			{Name: "git", Level: bintapp.LevelOK, Detail: "git version 2.45"},
			{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"},
			{Name: "hook claude", Level: bintapp.LevelOK, Detail: "đã cài"},
		}},
		Pending: &bintapp.PendingWorkReport{
			KnowledgeBatches: []bintapp.PendingBatch{{Tag: "nguon:lo-thu", Candidate: 2}},
			KnowledgeOther:   1,
			Proposals:        []bintapp.PendingProposal{{ID: "01EP0000KNOW", Action: "update"}},
			Doctor:           []bintapp.Check{{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"}},
		},
		Candidates: 3,
	}
}

// sampleKnowledge là dữ liệu danh sách tri thức cố định cho kiểm thử và ảnh chụp.
func sampleKnowledge() knscreen.Data {
	mk := func(id, typ, status, title, origin string, third bool, paths []string, tags ...string) *knowledge.Item {
		return &knowledge.Item{ID: id, Type: typ, Status: status, Title: title, Body: "Nội dung của mục " + title + ".\nDòng thứ hai của nội dung.",
			Scope: knowledge.Scope{Paths: paths, Tags: tags}, Origin: knowledge.Origin{Kind: origin, ThirdParty: third},
			Author: "Tester <t@example.com>", CreatedAt: "2026-11-11T00:00:00Z", Evidence: []knowledge.Evidence{{Path: "internal/db/db.go", Lines: "40-52"}},
			Path: "/work/demo/.bint/knowledge/items/" + id + ".md"}
	}
	return knscreen.Data{Stamp: time.Unix(1, 0), Files: []string{"internal/db/db.go", "internal/api/api.go", "README.md"}, Items: []*knowledge.Item{
		mk("01KNOW000001", "rule", "candidate", "Endpoint phải có /v1", "agent", true, []string{"api/**"}, "nguon:lo-thu"),
		mk("01KNOW000002", "warning", "candidate", "Không gọi sql.Open trực tiếp", "agent", true, []string{"internal/db/**"}, "nguon:lo-thu"),
		mk("01KNOW000003", "pattern", "verified", "Handler đi qua wrap()", "human", false, nil),
		mk("01KNOW000004", "fact", "stale", "Cổng mặc định là 8080", "agent", false, []string{"cmd/**"}),
		mk("01KNOW000005", "lesson", "candidate", "Chạy go vet với GOOS=windows", "agent", false, nil),
		mk("01KNOW000006", "decision", "deprecated", "Dùng bbolt thay SQLite", "human", false, nil),
	}}
}

// sampleDetail là chi tiết giả của một mục (bằng chứng có trích đoạn, một finding).
func sampleDetail(_ context.Context, _ string, id string) (bintapp.KnowledgeDetail, error) {
	for _, it := range sampleKnowledge().Items {
		if it.ID != id {
			continue
		}
		d := bintapp.KnowledgeDetail{Item: it, Evidence: []bintapp.EvidenceView{{Evidence: it.Evidence[0], Lines: []string{"  40  func Open() {", "  41  \treturn nil", "  42  }"}}}}
		if it.Status == knowledge.StatusStale {
			d.Check.Findings = []knowledge.Finding{{Level: knowledge.LevelError, Code: knowledge.CodeFingerprint, Detail: "bằng chứng đã thay đổi"}}
		}
		return d, nil
	}
	return bintapp.KnowledgeDetail{}, errors.New("không có mục")
}

// mkTask dựng một tác vụ mẫu (không có trên đĩa) với trạng thái và thời điểm cập nhật cho trước.
func mkTask(id, status, goal, updated string) *task.Task {
	return &task.Task{Dir: "/state/tasks/" + id,
		Ticket: task.Ticket{ID: id, Goal: goal, Request: "Yêu cầu gốc: " + goal, Autonomy: task.AutonomyAskOnProtected,
			Scope:  task.Scope{Allow: []string{"internal/tui/**"}, Deny: []string{"go.mod"}},
			Verify: task.Verify{Command: "go test ./...", Criteria: []string{"Danh sách theo trạng thái", "Hành động duyệt"}}},
		State: task.State{ID: id, Status: status, Workflow: "b-do", UpdatedAt: updated, CreatedAt: updated,
			History: []task.Transition{{To: task.StatusCreated, At: updated, By: "human"}}}}
}

// sampleTasks là dữ liệu danh sách tác vụ cố định: hai tác vụ chờ người, một đang làm, một sắp làm, một xong, một huỷ.
func sampleTasks() tkscreen.Data {
	blocked := mkTask("01TASK1", task.StatusBlocked, "Thêm cột email", "2026-11-11T10:00:00Z")
	blocked.State.Reason = "thay đổi cần người duyệt: migrations/002.sql"
	blocked.State.PendingPaths = []string{"migrations/002.sql"}
	failed := mkTask("01TASK2", task.StatusFailed, "Đổi cách tính phí", "2026-11-11T09:00:00Z")
	failed.State.Errors = []task.ErrorRecord{{Step: "b-feat", Message: "agent thoát với mã 1", At: "2026-11-11T09:00:00Z", By: "agent"}}
	return tkscreen.Data{Stamp: time.Unix(1, 0), Root: "/work/demo", Files: []string{"internal/db/db.go", "README.md"}, Tasks: []*task.Task{
		mkTask("01TASK3", task.StatusDone, "Sửa lỗi khoá cơ sở dữ liệu", "2026-11-10T10:00:00Z"),
		mkTask("01TASK4", task.StatusInProgress, "Thêm màn Tác vụ", "2026-11-11T08:00:00Z"),
		mkTask("01TASK5", task.StatusPlanned, "Viết tài liệu cài đặt", "2026-11-11T07:00:00Z"),
		failed, blocked,
		mkTask("01TASK6", task.StatusCancelled, "Việc đã huỷ", "2026-11-09T10:00:00Z"),
	}}
}

// sampleTaskDetail là chi tiết giả của một tác vụ: tài liệu Markdown, kết quả lint, một vòng review có phát hiện ở nhiều mức.
func sampleTaskDetail(_ context.Context, _ string, id string, _ bool) (tkscreen.Detail, error) {
	var t *task.Task
	for _, x := range sampleTasks().Tasks {
		if x.State.ID == id {
			t = x
		}
	}
	if t == nil {
		return tkscreen.Detail{}, errors.New("không có tác vụ")
	}
	rev := &task.Review{TaskID: id, Rounds: []task.ReviewRound{{Round: 1, At: "2026-11-11T09:30:00Z", By: "agent", Summary: "Hai điểm cần sửa", Findings: []task.Finding{
		{ID: id + ".1", Kind: "suggestion", Severity: "low", Title: "Đặt tên biến rõ hơn", Path: "internal/db/db.go", Lines: "3", Evidence: "tên chưa rõ"},
		{ID: id + ".2", Kind: "bug", Severity: "critical", Title: "Không đóng kết nối", Path: "internal/db/db.go", Lines: "4-5", Evidence: "thiếu Close", Body: "Gọi `Close()` trong **defer**."},
		{ID: id + ".3", Kind: "risk", Severity: "medium", Title: "Khoá ghi quá rộng", Path: "README.md", Lines: "1", Evidence: "khoá toàn bảng"},
	}}}}
	rep := task.ArtifactReport{Warnings: []task.ArtifactIssue{{Code: "ac_gap", Artifact: "tasks.md", Detail: "AC-2 chưa có TSK"}}}
	d := tkscreen.Detail{TaskDetail: bintapp.TaskDetail{Task: t, Review: rev, Lint: &rep, Blockers: []string{"chưa có bằng chứng"},
		Assessment: task.Assessment{Risk: "high", Protected: []string{"migrations/**"}, Questions: []string{"Lệnh kiểm chứng là gì?"}}},
		Docs: map[string]string{"requirement": "# Yêu cầu\n\n## Tiêu chí chấp nhận\n- [ ] AC-1: Danh sách theo trạng thái\n- [x] AC-2: Duyệt\n\n```go\nfunc A() {}\n```\n",
			"plan": "# Kế hoạch\n\n1. Dịch vụ trước\n2. Giao diện sau\n", "tasks": "- [ ] TSK-1 [AC-1]: làm — xong khi: test xanh\n"}}
	return d, nil
}

func notInitData() overview.Data { return overview.Data{NotInit: true} }

// newModel dựng mô hình với loader giả và đã nạp xong dữ liệu.
func newModel(t *testing.T, o Options, data overview.Data, w, h int) *Model {
	t.Helper()
	if o.Dir == "" {
		o.Dir = "/work/demo"
	}
	if o.Version == "" {
		o.Version = "0.1.0"
	}
	if o.Lang == "" {
		o.Lang = "vi"
	}
	o.Loader = func(context.Context, string) (overview.Data, error) { return data, nil }
	if o.DetectHarnesses == nil {
		o.DetectHarnesses = fakeDetect // trình hướng dẫn init không phụ thuộc harness có trên máy chạy kiểm thử
	}
	o.SystemLoad = withSampleSystem(o.SystemLoad)
	if o.KnowledgeLoader == nil {
		o.KnowledgeLoader = func(context.Context, string) (knscreen.Data, error) { return sampleKnowledge(), nil }
	}
	if o.KnowledgeDetailer == nil {
		o.KnowledgeDetailer = sampleDetail
	}
	if o.TasksLoader == nil {
		o.TasksLoader = func(context.Context, string, bool) (tkscreen.Data, error) { return sampleTasks(), nil }
	}
	if o.TasksDetailer == nil {
		o.TasksDetailer = sampleTaskDetail
	}
	if o.TasksRuns == nil {
		o.TasksRuns = func(context.Context, string, string, bool) (bintapp.TaskRunsResult, error) {
			return bintapp.TaskRunsResult{Runs: []bintapp.TaskRunRow{{RunID: "01RUN000001", Workflow: "b-do", Status: "completed", Duration: "12s"}}}, nil
		}
	}
	if o.RunsLoader == nil {
		o.RunsLoader = func(context.Context, string, int, bool) (rnscreen.Data, error) { return sampleRuns(), nil }
	}
	if o.RunsDetailer == nil {
		o.RunsDetailer = sampleRunDetail
	}
	if o.RunsLessons == nil {
		o.RunsLessons = sampleLessons
	}
	if o.EvolveLoader == nil {
		o.EvolveLoader = func(context.Context, string, evscreen.Query, bool) (evscreen.Data, error) { return sampleEvolve(), nil }
	}
	if o.EvolveDetailer == nil {
		o.EvolveDetailer = sampleEvolveDetail
	}
	if o.EvalLoader == nil {
		o.EvalLoader = sampleEvalLoader
	}
	if o.EvalDetailer == nil {
		o.EvalDetailer = sampleEvalDetail
	}
	if o.EvalReporter == nil {
		o.EvalReporter = sampleEvalReport
	}
	o.SpinnerFPS = time.Millisecond
	m := New(context.Background(), o)
	m.SetSize(w, h)
	drive(t, m, m.Init())
	return m
}

// drive chạy tea.Cmd (đồng bộ) và đưa thông điệp kết quả vào Update, tới khi hết lệnh.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				drive(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
	}
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		drive(t, m, cmd)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func plain(s string) string { return ansi.Strip(s) }

// lines tách đầu ra thành các dòng (đã bỏ mã ANSI).
func lines(s string) []string { return strings.Split(plain(s), "\n") }

func allModes() []theme.Mode { return theme.Modes }
