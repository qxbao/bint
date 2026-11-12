package actions

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/tui/jobs"
)

// doInitRun chạy app.Init với Options đã chọn (ToInit không gắn Confirm: người gọi luôn đặt Hooks và GitMode tường minh
// hoặc muốn giữ nguyên, nên không có câu hỏi giữa chừng). Kết quả là *app.InitReport.
func doInitRun(id string) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		o, _ := opts.(app.InitFlagOptions)
		io, err := o.ToInit(env.Dir, env.BintPath, env.Version)
		if err != nil {
			return res(id, opts, nil, err)
		}
		rep, err := app.Init(ctx, io)
		return res(id, opts, rep, err)
	}
}

// doInitPlan xem trước init (chỉ đọc). Kết quả là app.InitPlan.
func doInitPlan(id string) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		o, _ := opts.(app.InitFlagOptions)
		pl, err := app.PlanInit(ctx, env.Dir, o)
		return res(id, opts, pl, err)
	}
}

// NavID trả ID hành động chuyển tới màn hình s.
func NavID(s Screen) string { return "nav." + string(s) }

// Default là sổ hành động (giai đoạn 0 và 1). Các giai đoạn sau thêm hành động của từng màn hình vào
// đây; test phủ lệnh buộc mọi lệnh CLI hoặc có hành động, hoặc nằm trong danh sách loại trừ/chưa làm.
func Default() *Registry {
	list := []Action{
		{ID: "app.quit", Key: "q", Footer: true, Priority: 0, Pinned: true, LabelKey: "action.app.quit.label", TitleKey: "action.app.quit.title"},
		{ID: "app.help", Key: "?", Footer: true, Priority: 0, Pinned: true, LabelKey: "action.app.help.label", TitleKey: "action.app.help.title"},
		{ID: "app.palette", Key: ":", Footer: true, Priority: 2, LabelKey: "action.app.palette.label", TitleKey: "action.app.palette.title"},
		{ID: "app.language", Key: "L", Footer: true, Priority: 6, LabelKey: "action.app.language.label", TitleKey: "action.app.language.title"},
		{ID: "app.notifications", Key: "!", LabelKey: "action.app.notifications.label", TitleKey: "action.app.notifications.title"},
		{ID: "app.mouse", LabelKey: "action.app.mouse.label", TitleKey: "action.app.mouse.title"},
		{ID: "app.next_tab", Key: "]", TitleKey: "action.app.next_tab.title"},
		{ID: "app.prev_tab", Key: "[", TitleKey: "action.app.prev_tab.title"},
	}
	for i, s := range Screens {
		list = append(list, Action{ID: NavID(s), Key: string(rune('1' + i)), TitleKey: "action.nav." + string(s) + ".title"})
	}
	list = append(list,
		Action{ID: "overview.refresh", Screen: Overview, Key: "r", Footer: true, Priority: 1, LabelKey: "action.overview.refresh.label",
			TitleKey: "action.overview.refresh.title", CLIPath: "status"},
		Action{ID: "overview.doctor", Screen: Overview, TitleKey: "action.overview.doctor.title", CLIPath: "doctor"},
		// Trình hướng dẫn init (giai đoạn 5b): init.plan xem trước (chỉ đọc), init.run chạy app.Init với Options đã chọn.
		Action{ID: "init.run", Screen: Init, LabelKey: "action.init.run.label", TitleKey: "action.init.run.title", CLIPath: "init",
			Options: app.InitFlagOptions{}, Writes: true, Do: doInitRun("init.run"),
			JobInfo: func(any) jobs.Info { return jobs.Info{Kind: jobs.KindInit, Label: "init"} }},
		Action{ID: "init.plan", Screen: Init, FormID: "init.run", LabelKey: "action.init.plan.label", TitleKey: "action.init.plan.title",
			Options: app.InitFlagOptions{}, Do: doInitPlan("init.plan")},
		Action{ID: "init.continue", Screen: Init, Key: "Enter", Footer: true, Priority: 1, LabelKey: "action.init.continue.label",
			TitleKey: "action.init.continue.title"},
		Action{ID: "init.recheck", Screen: Init, Key: "r", Footer: true, Priority: 2, LabelKey: "action.init.recheck.label",
			TitleKey: "action.init.recheck.title"},
	)
	list = append(list, knowledgeActions()...)
	list = append(list, taskActions()...)
	list = append(list, runsActions()...)
	list = append(list, evalActions()...)
	list = append(list, evolveActions()...)
	list = append(list, systemActions()...)
	return NewRegistry(list)
}
