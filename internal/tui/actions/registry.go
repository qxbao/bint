package actions

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
)

// InitDoneMsg là kết quả hành động khởi tạo với mặc định.
type InitDoneMsg struct {
	Report *app.InitReport
	Err    error
}

// runInitDefaults gọi app.Init với tuỳ chọn mặc định, không hỏi (Hooks=nil, Confirm=nil: đúng như
// `bint init` không tương tác).
func runInitDefaults(ctx context.Context, env Env) tea.Msg {
	rep, err := app.Init(ctx, app.InitOptions{Dir: env.Dir, BintPath: env.BintPath, Version: env.Version})
	return InitDoneMsg{Report: rep, Err: err}
}

// NavID trả ID hành động chuyển tới màn hình s.
func NavID(s Screen) string { return "nav." + string(s) }

// Default là sổ hành động (giai đoạn 0 và 1). Các giai đoạn sau thêm hành động của từng màn hình vào
// đây; test phủ lệnh buộc mọi lệnh CLI hoặc có hành động, hoặc nằm trong danh sách loại trừ/chưa làm.
func Default() *Registry {
	list := []Action{
		{ID: "app.quit", Key: "q", Footer: true, Priority: 0, Pinned: true, LabelKey: "action.app.quit.label", TitleKey: "action.app.quit.title"},
		{ID: "app.help", Key: "?", Footer: true, Priority: 0, Pinned: true, LabelKey: "action.app.help.label", TitleKey: "action.app.help.title"},
		{ID: "app.palette", Key: ":", Footer: true, Priority: 3, LabelKey: "action.app.palette.label", TitleKey: "action.app.palette.title"},
		{ID: "app.language", Key: "L", Footer: true, Priority: 6, LabelKey: "action.app.language.label", TitleKey: "action.app.language.title"},
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
		Action{ID: "init.defaults", Screen: Init, Key: "Enter", Footer: true, Priority: 1, LabelKey: "action.init.defaults.label",
			TitleKey: "action.init.defaults.title", CLIPath: "init", Writes: true, Run: runInitDefaults},
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
	return NewRegistry(list)
}
