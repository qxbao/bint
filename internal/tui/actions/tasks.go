package actions

import (
	"context"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/tui/jobs"
)

// EditDocOptions là tham số hành động sửa nội dung trong $EDITOR qua tệp tạm (tài liệu của tác vụ, mẫu review):
// Path là tệp tạm, Before là nội dung ban đầu để biết có thay đổi không. Không có thẻ flag/arg nên không thành form.
type EditDocOptions struct {
	Ref    string
	Name   string
	Path   string
	Before string
}

func openT(ctx context.Context, env Env) (*app.TaskWS, error) {
	return app.OpenTasks(ctx, env.Dir, env.Version)
}

// withT mở môi trường tác vụ rồi chạy f; lỗi mở trả thẳng trong ResultMsg.
func withT(id string, f func(ctx context.Context, ws *app.TaskWS, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		ws, err := openT(ctx, env)
		if err != nil {
			return res(id, opts, nil, err)
		}
		v, err := f(ctx, ws, opts)
		return res(id, opts, v, err)
	}
}

// editTemp ghi content ra tệp tạm (đuôi ext), dựng lệnh soạn thảo và trả ExecMsg; xong thì mô hình gốc gửi
// ResultMsg{ID, Opts} để màn hình đọc tệp tạm.
func editTemp(id, ext string, o EditDocOptions, content string) DoFunc {
	return func(ctx context.Context, env Env, _ any) tea.Msg {
		f, err := os.CreateTemp("", "bint-*"+ext)
		if err != nil {
			return res(id, o, nil, err)
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return res(id, o, nil, err)
		}
		o.Path, o.Before = f.Name(), content
		return ExecMsg{ID: id, Opts: o, Cmd: editorCmd(ctx, env, o.Path, 0)}
	}
}

func taskLabel(wf, ref, request string) string {
	switch {
	case ref != "":
		return wf + " · " + shortID(ref)
	case strings.TrimSpace(request) != "":
		r := []rune(strings.Join(strings.Fields(request), " "))
		if len(r) > 28 {
			r = append(r[:27], '…')
		}
		return wf + " · " + string(r)
	}
	return wf
}

func shortID(id string) string {
	if r := []rune(id); len(r) > 10 {
		return string(r[len(r)-10:])
	}
	return id
}

// taskActions là các hành động của màn Tác vụ (giai đoạn 2).
func taskActions() []Action {
	T := Tasks
	t := func(name string) (string, string) {
		return "action.tasks." + name + ".label", "action.tasks." + name + ".title"
	}
	mk := func(id, name, key string, footer bool, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		l, ti := t(name)
		return Action{ID: id, Screen: T, Key: key, Footer: footer, Priority: prio, LabelKey: l, TitleKey: ti, CLIPath: cli,
			Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	list := []Action{
		mk("tasks.run", "run", "r", true, 1, "run", app.RunWorkflowOptions{}, true, false, nil),
		mk("tasks.resume", "resume", "c", true, 2, "run resume", app.RunResumeOptions{}, true, false, nil),
		mk("tasks.approve", "approve", "a", true, 1, "task approve", app.TaskApproveOptions{}, true, true,
			withT("tasks.approve", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				_, a, err := app.ApproveTask(ctx, ws, o.(app.TaskApproveOptions))
				return a, err
			})),
		// Kế hoạch duyệt (chỉ đọc): hộp xác nhận liệt kê lệnh kiểm chứng và đường dẫn sẽ được duyệt, tính trước khi ghi.
		mk("tasks.plan_approve", "plan_approve", "", false, 0, "", app.TaskApproveOptions{}, false, false,
			withT("tasks.plan_approve", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.PlanTaskApprove(ctx, ws, o.(app.TaskApproveOptions))
			})),
		mk("tasks.status", "status", "s", true, 3, "task status", app.TaskStatusOptions{}, true, false,
			withT("tasks.status", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.SetTaskStatus(ctx, ws, o.(app.TaskStatusOptions))
			})),
		mk("tasks.new", "new", "n", true, 4, "task new", app.TaskNewOptions{}, true, false,
			withT("tasks.new", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.NewTask(ctx, ws, o.(app.TaskNewOptions))
			})),
		mk("tasks.verify", "verify", "v", true, 5, "task verify", app.TaskVerifyOptions{}, true, false, nil),
		mk("tasks.set", "set", "e", true, 6, "task set", app.TaskSetOptions{}, true, false,
			withT("tasks.set", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.SetTask(ctx, ws, o.(app.TaskSetOptions))
			})),
		mk("tasks.write", "write", "", false, 0, "task write", app.TaskWriteOptions{}, true, false,
			withT("tasks.write", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.WriteTaskArtifact(ctx, ws, o.(app.TaskWriteOptions))
			})),
		mk("tasks.edit_doc", "edit_doc", "", false, 0, "", nil, true, false, nil),
		mk("tasks.criteria", "criteria", "m", false, 7, "task criteria", app.TaskCriteriaOptions{}, true, false,
			withT("tasks.criteria", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.CheckTaskCriterion(ctx, ws, o.(app.TaskCriteriaOptions))
			})),
		mk("tasks.error", "error", "i", false, 8, "task error", app.TaskErrorOptions{}, true, false,
			withT("tasks.error", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.RecordTaskError(ctx, ws, o.(app.TaskErrorOptions))
			})),
		mk("tasks.review", "review", "", false, 0, "task review", app.TaskReviewOptions{}, true, false,
			withT("tasks.review", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.SubmitTaskReview(ctx, ws, o.(app.TaskReviewOptions))
			})),
		mk("tasks.review_edit", "review_edit", "w", false, 9, "", nil, true, false, nil),
		mk("tasks.lint", "lint", "l", false, 8, "task lint", app.TaskLintOptions{}, false, false,
			withT("tasks.lint", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.LintTask(ctx, ws, o.(app.TaskLintOptions))
			})),
		// Thay đổi của riêng tác vụ (chỉ đọc): kết quả hiện trong hộp thoại có tô màu diff.
		mk("tasks.diff", "diff", "D", false, 9, "task diff", app.TaskDiffOptions{}, false, false,
			withT("tasks.diff", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.DiffTask(ctx, ws, o.(app.TaskDiffOptions))
			})),
		// Đưa thay đổi của tác vụ worktree về cây gốc; --3way chỉ người (dịch vụ chặn bằng RequireHuman).
		mk("tasks.land", "land", "o", false, 8, "task land", app.TaskLandOptions{}, true, false,
			withT("tasks.land", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.LandTask(ctx, ws, o.(app.TaskLandOptions))
			})),
		mk("tasks.cancel", "cancel", "x", false, 8, "task cancel", app.TaskCancelOptions{}, true, false,
			withT("tasks.cancel", func(ctx context.Context, ws *app.TaskWS, o any) (any, error) {
				return app.CancelTask(ctx, ws, o.(app.TaskCancelOptions))
			})),
		mk("tasks.transitions", "transitions", "T", false, 9, "task transitions", nil, false, false, nil),
		mk("tasks.list", "list", "A", true, 7, "task list", app.TaskListOptions{}, false, false, nil),
		mk("tasks.show", "show", "Enter", true, 2, "task show", app.TaskShowOptions{}, false, false, nil),
		mk("tasks.runs", "runs", "", false, 0, "run status", app.TaskRunsOptions{}, false, false, nil),
		mk("tasks.filter", "filter", "/", true, 6, "", nil, false, false, nil),
		mk("tasks.refresh", "refresh", "ctrl+r", false, 9, "", nil, false, false, nil),
		mk("tasks.next_view", "next_view", ".", false, 9, "", nil, false, false, nil),
		mk("tasks.prev_view", "prev_view", ",", false, 9, "", nil, false, false, nil),
		// Phát hiện review → tri thức: điền sẵn rồi mở form Thêm tri thức (dùng chung nhãn form với knowledge.add).
		mk("tasks.finding_prefill", "finding_prefill", "", false, 0, "", nil, false, false,
			withK("tasks.finding_prefill", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				ao := o.(app.AddOptions)
				err := ao.FillFromReview(k)
				if ao.Origin == "" {
					// Nội dung do b-review viết chứ chưa phải người kiểm: mục chờ duyệt (candidate); người đổi được ở ô Nguồn.
					ao.Origin = knowledge.OriginAgent
				}
				return ao, err
			})),
		mk("tasks.add_knowledge", "add_knowledge", "K", false, 5, "knowledge add", app.AddOptions{}, true, false,
			withK("tasks.add_knowledge", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.AddKnowledge(ctx, k, o.(app.AddOptions))
			})),
		mk("tasks.open_file", "open_file", "", false, 0, "", EditOptions{}, true, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				eo := o.(EditOptions)
				return ExecMsg{ID: "tasks.open_file", Opts: eo, Cmd: editorCmd(ctx, env, eo.Path, eo.Line)}
			}),
	}
	for i := range list {
		switch list[i].ID {
		case "tasks.run":
			list[i].Job = func(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
				ws, err := openT(ctx, env)
				if err != nil {
					return nil, err
				}
				o := opts.(app.RunWorkflowOptions)
				o.RunCallbacks = br.Callbacks()
				o.Agent = env.Agent
				return app.RunWorkflow(ctx, ws, o)
			}
			list[i].JobInfo = func(opts any) jobs.Info {
				o := opts.(app.RunWorkflowOptions)
				return jobs.Info{Kind: jobs.KindWorkflow, Label: taskLabel(o.Workflow, o.Task, o.Request), TaskID: o.Task,
					Resume: resumeHint(o.Task)}
			}
		case "tasks.resume":
			list[i].Job = func(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
				ws, err := openT(ctx, env)
				if err != nil {
					return nil, err
				}
				o := opts.(app.RunResumeOptions)
				o.RunCallbacks = br.Callbacks()
				o.Agent = env.Agent
				return app.ResumeWorkflow(ctx, ws, o)
			}
			list[i].JobInfo = func(opts any) jobs.Info {
				o := opts.(app.RunResumeOptions)
				return jobs.Info{Kind: jobs.KindWorkflow, Label: taskLabel("resume", o.Ref, ""), TaskID: o.Ref, Resume: resumeHint(o.Ref)}
			}
		case "tasks.verify":
			list[i].Job = func(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
				ws, err := openT(ctx, env)
				if err != nil {
					return nil, err
				}
				return app.VerifyTask(ctx, ws, opts.(app.TaskVerifyOptions))
			}
			list[i].JobInfo = func(opts any) jobs.Info {
				o := opts.(app.TaskVerifyOptions)
				return jobs.Info{Kind: jobs.KindVerify, Label: "verify · " + shortID(o.Ref), TaskID: o.Ref}
			}
		case "tasks.edit_doc":
			list[i].Do = func(ctx context.Context, env Env, opts any) tea.Msg {
				o := opts.(EditDocOptions)
				ws, err := openT(ctx, env)
				if err != nil {
					return res("tasks.edit_doc", opts, nil, err)
				}
				t, err := ws.Tasks.Load(o.Ref)
				if err != nil {
					return res("tasks.edit_doc", opts, nil, err)
				}
				content, _ := os.ReadFile(t.ArtifactPath(o.Name))
				return editTemp("tasks.edit_doc", ".md", o, string(content))(ctx, env, opts)
			}
		case "tasks.review_edit":
			list[i].Do = func(ctx context.Context, env Env, opts any) tea.Msg {
				return editTemp("tasks.review_edit", ".json", opts.(EditDocOptions), app.TaskReviewTemplate)(ctx, env, opts)
			}
		}
	}
	// Hành động dùng chung Options với hành động khác dùng chung nhãn form.
	for i := range list {
		switch list[i].ID {
		case "tasks.add_knowledge":
			list[i].FormID = "knowledge.add"
		case "tasks.plan_approve":
			list[i].FormID = "tasks.approve"
		}
	}
	return list
}

func resumeHint(id string) string {
	if id == "" {
		return ""
	}
	return "bint run resume " + id
}
