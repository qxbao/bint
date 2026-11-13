package actions

import (
	"context"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/tui/jobs"
)

// Hành động của tab Hệ thống (giai đoạn 5a, docs/tui-plan.md mục 5.7): cấu hình, harness và thành phần quy trình,
// skill + sync, chỉ mục, ngôn ngữ và doctor (MCP có màn riêng: mcp.go). Mọi hành động gọi tầng dịch vụ (internal/app, evolvesvc);
// hành động ghi có Writes = true, duyệt/loại bỏ thành phần là HumanOnly (kiểm ở dịch vụ bằng RequireHuman).

// ConfigEditOptions: chọn lớp cấu hình cần sửa trong $EDITOR (hành động giao diện, không có lệnh CLI).
type ConfigEditOptions struct {
	Layer string `flag:"layer" form:"required,choices=project|local"`
}

// ConfigEditState là phiên sửa cấu hình đang mở trong $EDITOR (mang qua ExecMsg → ResultMsg).
type ConfigEditState struct{ Edit app.ConfigEdit }

// ConfigApplyArgs là tham số của kiểm tra (system.config_check), ghi (system.config_save) bản đã sửa và đặt lại mốc
// (system.config_rebase; Data không dùng).
type ConfigApplyArgs struct {
	Edit app.ConfigEdit
	Data []byte
}

// HarnessVersionOptions: lấy phiên bản của một harness (gọi chương trình harness).
type HarnessVersionOptions struct {
	Name string `arg:"name" form:"required,suggest=harnesses"`
}

// OpenFileOptions: mở một tệp trong $EDITOR (vd. lớp phủ skill vừa tạo).
type OpenFileOptions struct{ Path string }

// LanguageMsg báo mô hình gốc đổi ngôn ngữ giao diện ngay trong phiên (sau khi ghi ui.language).
//
// Lang là ui.language hiệu lực (đã ghép lớp) cần dùng để chọn ngôn ngữ; Chosen là giá trị người dùng vừa chọn/ghi ("" = không có, vd.
// phím L); Layer là lớp cấu hình đang che giá trị vừa ghi ("" = không bị che). Mô hình gốc chọn ngôn ngữ theo thứ tự đã công bố
// (--lang > BINT_LANG > Lang > LC_ALL/LANG) và cảnh báo khi kết quả khác Chosen.
type LanguageMsg struct{ Lang, Chosen, Layer string }

// RestartOptions là yêu cầu khởi động lại bint sau khi cập nhật: Path là tệp vừa cài (app.UpdateResult.Target), exec lại với
// cùng đối số và môi trường. Hành động system.restart là giao diện thuần, mô hình gốc xử lý.
type RestartOptions struct{ Path string }

// UpdateOutcome là kết quả hành động system.update: kết quả của app.UpdateSelf kèm toàn bộ log go install (đủ để hiện lại sau
// khi việc nền đã xong, kể cả khi lỗi).
type UpdateOutcome struct {
	Result app.UpdateResult
	Log    []string
}

// collectLines tách đầu ra của go install thành dòng, gọi emit cho từng dòng (an toàn khi nhiều goroutine ghi).
type collectLines struct {
	mu   sync.Mutex
	buf  string
	emit func(string)
}

func (w *collectLines) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		i := strings.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(w.buf[:i], "\r")
		w.buf = w.buf[i+1:]
		w.emit(line)
	}
	return len(p), nil
}

// Flush đẩy phần còn lại (dòng cuối không có xuống dòng).
func (w *collectLines) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf != "" {
		w.emit(strings.TrimRight(w.buf, "\r"))
		w.buf = ""
	}
}

// runUpdate chạy cập nhật Bint (nền): log go install đi từng dòng về khung log; hàm cập nhật tiêm được qua Env.Update.
func runUpdate(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
	o, _ := opts.(app.UpdateOptions)
	if err := o.Validate(); err != nil {
		return UpdateOutcome{}, err
	}
	var log []string
	w := &collectLines{emit: func(l string) { log = append(log, l); br.Log(l) }}
	o.Out = w
	fn := env.Update
	if fn == nil {
		fn = app.UpdateSelf
	}
	r, err := fn(ctx, o)
	w.Flush()
	return UpdateOutcome{Result: r, Log: log}, err
}

// VersionResult là phiên bản harness lấy được.
type VersionResult struct{ Name, Version string }

func openEvolveNoIngest(ctx context.Context, env Env) (*evolvesvc.Workspace, error) {
	return evolvesvc.OpenIngest(ctx, env.Dir, false)
}

func withForm(ns string, a Action) Action { a.FormID = ns; return a }

func systemActions() []Action {
	mk := func(id, name, key string, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		return Action{ID: id, Screen: System, Key: key, Priority: prio, LabelKey: "action.system." + name + ".label",
			TitleKey: "action.system." + name + ".title", CLIPath: cli, Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	withComp := func(id string, f func(ctx context.Context, w *app.ComponentWS, opts any) (any, error)) DoFunc {
		return func(ctx context.Context, env Env, opts any) tea.Msg {
			w, err := app.OpenComponents(ctx, env.Dir)
			if err != nil {
				return res(id, opts, nil, err)
			}
			v, err := f(ctx, w, opts)
			return res(id, opts, v, err)
		}
	}
	withEvolve := func(id string, f func(ctx context.Context, e *evolvesvc.Workspace, opts any) (any, error)) DoFunc {
		return func(ctx context.Context, env Env, opts any) tea.Msg {
			e, err := openEvolveNoIngest(ctx, env)
			if err != nil {
				return res(id, opts, nil, err)
			}
			defer e.Close()
			v, err := f(ctx, e, opts)
			return res(id, opts, v, err)
		}
	}
	withKnow := func(id string, f func(ctx context.Context, k *app.KnowledgeWS, opts any) (any, error)) DoFunc {
		return withK(id, f)
	}
	review := func(id, to string) DoFunc {
		return withComp(id, func(ctx context.Context, w *app.ComponentWS, o any) (any, error) {
			return app.ReviewComponent(ctx, w, o.(app.ComponentReviewOptions), to)
		})
	}
	return []Action{
		// Giao diện thuần.
		mk("system.next_view", "next_view", ".", 9, "", nil, false, false, nil),
		mk("system.prev_view", "prev_view", ",", 9, "", nil, false, false, nil),
		mk("system.refresh", "refresh", "ctrl+r", 9, "", nil, false, false, nil),
		mk("system.filter", "filter", "/", 7, "", nil, false, false, nil),

		// Cấu hình.
		mk("system.config_edit", "config_edit", "e", 1, "", ConfigEditOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				o := opts.(ConfigEditOptions)
				e, err := app.PrepareConfigEdit(ctx, env.Dir, config.Layer(o.Layer))
				if err != nil {
					return res("system.config_edit", opts, nil, err)
				}
				return ExecMsg{ID: "system.config_edit", Opts: ConfigEditState{Edit: e}, Cmd: editorCmd(ctx, env, e.TempPath, 0)}
			}),
		mk("system.config_check", "config_check", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				a := opts.(ConfigApplyArgs)
				c, err := app.CheckConfigEdit(ctx, env.Dir, a.Edit, a.Data)
				return res("system.config_check", a, c, err)
			}),
		// Đặt lại mốc sau ErrConfigChanged: gộp ba chiều thay đổi của người dùng lên tệp gốc mới (chỉ ghi bản tạm, không đụng tệp gốc).
		mk("system.config_rebase", "config_rebase", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				a := opts.(ConfigApplyArgs)
				e, rb, err := app.RebaseConfigEdit(ctx, env.Dir, a.Edit)
				a.Edit = e
				return res("system.config_rebase", a, rb, err)
			}),
		mk("system.config_save", "config_save", "", 0, "", nil, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				a := opts.(ConfigApplyArgs)
				r, err := app.SaveConfigEdit(ctx, env.Dir, a.Edit, a.Data)
				if err == nil { // bị từ chối thì giữ bản tạm: màn hình cho sửa lại hoặc bỏ (người dùng không mất bản sửa)
					_ = os.Remove(a.Edit.TempPath)
				}
				return res("system.config_save", opts, r, err)
			}),
		mk("system.config_view", "config_view", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.ConfigView(ctx, env.Dir)
				return res("system.config_view", opts, v, err)
			}),

		// Ngôn ngữ.
		mk("system.language_save", "language_save", "enter", 1, "", app.UILanguageOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				r, err := app.SetUILanguage(ctx, env.Dir, opts.(app.UILanguageOptions))
				return res("system.language_save", opts, r, err)
			}),

		// Harness.
		mk("system.harness_list", "harness_list", "", 0, "harness list", app.HarnessListOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.ListHarnesses(ctx, env.Dir, opts.(app.HarnessListOptions))
				return res("system.harness_list", opts, v, err)
			}),
		mk("system.harness_show", "harness_show", "enter", 2, "harness show", app.HarnessShowOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.ShowHarness(ctx, env.Dir, opts.(app.HarnessShowOptions))
				return res("system.harness_show", opts, v, err)
			}),
		mk("system.harness_runtime", "harness_runtime", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.HarnessRuntime(ctx, env.Dir)
				return res("system.harness_runtime", opts, v, err)
			}),
		mk("system.harness_version", "harness_version", "V", 3, "", HarnessVersionOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				o := opts.(HarnessVersionOptions)
				return res("system.harness_version", opts, VersionResult{Name: o.Name, Version: app.HarnessVersion(ctx, env.Dir, o.Name)}, nil)
			}),

		// Thành phần quy trình.
		mk("system.component_list", "component_list", "", 0, "harness component list", app.ComponentListOptions{}, false, false,
			withComp("system.component_list", func(_ context.Context, w *app.ComponentWS, o any) (any, error) {
				return app.ListComponents(w, o.(app.ComponentListOptions))
			})),
		mk("system.component_show", "component_show", "enter", 2, "harness component show", app.ComponentShowOptions{}, false, false,
			withComp("system.component_show", func(_ context.Context, w *app.ComponentWS, o any) (any, error) {
				return app.ShowComponent(w, o.(app.ComponentShowOptions))
			})),
		mk("system.component_add", "component_add", "n", 1, "harness component add", app.AddComponentOptions{}, true, false,
			withComp("system.component_add", func(ctx context.Context, w *app.ComponentWS, o any) (any, error) {
				return app.AddComponent(ctx, w, o.(app.AddComponentOptions))
			})),
		withForm("system.component_review", mk("system.component_approve", "component_approve", "a", 2, "harness component approve", app.ComponentReviewOptions{}, true, true,
			review("system.component_approve", "verified"))),
		withForm("system.component_review", mk("system.component_deprecate", "component_deprecate", "d", 3, "harness component deprecate", app.ComponentReviewOptions{}, true, true,
			review("system.component_deprecate", "deprecated"))),
		mk("system.render", "render", "R", 4, "harness render", app.RenderOptions{}, false, false,
			withComp("system.render", func(_ context.Context, w *app.ComponentWS, o any) (any, error) {
				return app.RenderHarness(w, o.(app.RenderOptions))
			})),

		// Skill.
		mk("system.skill_list", "skill_list", "", 0, "skill list", app.SkillListOptions{}, false, false,
			withKnow("system.skill_list", func(_ context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.ListSkills(k, o.(app.SkillListOptions))
			})),
		mk("system.skill_show", "skill_show", "enter", 2, "skill show", app.SkillShowOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.ShowSkill(ctx, env.Dir, opts.(app.SkillShowOptions))
				return res("system.skill_show", opts, v, err)
			}),
		mk("system.skill_overlay", "skill_overlay", "o", 3, "skill overlay", app.SkillOverlayOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.CreateOverlay(ctx, env.Dir, opts.(app.SkillOverlayOptions))
				return res("system.skill_overlay", opts, v, err)
			}),
		mk("system.skill_new", "skill_new", "n", 8, "skill new", app.SkillNewOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.NewSkill(ctx, env.Dir, opts.(app.SkillNewOptions))
				return res("system.skill_new", opts, v, err)
			}),
		mk("system.skill_lint", "skill_lint", "l", 9, "skill lint", app.SkillLintOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.LintSkills(ctx, env.Dir, opts.(app.SkillLintOptions))
				return res("system.skill_lint", opts, v, err)
			}),
		mk("system.skill_diff", "skill_diff", "d", 4, "skill diff", app.SkillDiffOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.DiffSkill(ctx, env.Dir, opts.(app.SkillDiffOptions))
				return res("system.skill_diff", opts, v, err)
			}),
		mk("system.skill_versions", "skill_versions", "v", 5, "skill versions", app.SkillVersionsOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.SkillVersions(ctx, env.Dir, opts.(app.SkillVersionsOptions))
				return res("system.skill_versions", opts, v, err)
			}),
		mk("system.skill_check", "skill_check", "c", 6, "skill check", evolvesvc.SkillCheckOptions{}, false, false,
			withEvolve("system.skill_check", func(ctx context.Context, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.SkillCheck(ctx, e, o.(evolvesvc.SkillCheckOptions))
			})),
		mk("system.skill_regress", "skill_regress", "g", 7, "skill regress", evolvesvc.SkillRegressOptions{}, false, false,
			withEvolve("system.skill_regress", func(ctx context.Context, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.SkillRegress(ctx, e, o.(evolvesvc.SkillRegressOptions))
			})),
		mk("system.open_file", "open_file", "", 0, "", OpenFileOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				o := opts.(OpenFileOptions)
				return ExecMsg{ID: "system.open_file", Opts: o, Cmd: editorCmd(ctx, env, o.Path, 0)}
			}),
		// Xem trước sync (chỉ đọc) rồi ghi.
		withForm("system.sync", mk("system.sync_plan", "sync_plan", "s", 1, "", app.SyncOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.PlanSync(ctx, env.Dir, env.Version, opts.(app.SyncOptions))
				return res("system.sync_plan", opts, v, err)
			})),
		mk("system.sync", "sync", "", 0, "sync", app.SyncOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.Sync(ctx, env.Dir, env.Version, opts.(app.SyncOptions))
				return res("system.sync", opts, v, err)
			}),

		// Cài đặt repo (giai đoạn 5b): trạng thái init hiện tại, xem trước (chỉ đọc) rồi chạy lại init với phần đã đổi.
		mk("system.repo_state", "repo_state", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.ReadInitState(ctx, env.Dir)
				return res("system.repo_state", opts, v, err)
			}),
		withForm("system.repo_apply", mk("system.repo_plan", "repo_plan", "e", 1, "", app.InitFlagOptions{}, false, false,
			doInitPlan("system.repo_plan"))),
		mk("system.repo_apply", "repo_apply", "", 0, "init", app.InitFlagOptions{}, true, false, doInitRun("system.repo_apply")),

		// Nâng cấu hình theo mẫu hiện tại (bint init --update): xem diff (chỉ đọc) rồi ghi.
		mk("system.config_upgrade_plan", "config_upgrade_plan", "U", 2, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.PlanConfigUpgrade(ctx, env.Dir)
				return res("system.config_upgrade_plan", opts, v, err)
			}),
		mk("system.config_upgrade", "config_upgrade", "", 0, "init", nil, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.UpgradeConfig(ctx, env.Dir, app.ConfigUpgradeOptions{BintPath: env.BintPath, Version: env.Version})
				return res("system.config_upgrade", opts, v, err)
			}),

		// Cập nhật Bint (chạy nền, có log) và khởi động lại.
		func() Action {
			a := mk("system.update", "update", "u", 1, "update", app.UpdateOptions{}, true, false, nil)
			a.Job = runUpdate
			a.JobInfo = func(any) jobs.Info { return jobs.Info{Kind: jobs.KindAction, Label: "update"} }
			return a
		}(),
		mk("system.restart", "restart", "", 0, "", nil, false, false, nil),

		// Chỉ mục, Doctor.
		mk("system.index_rebuild", "index_rebuild", "b", 1, "index rebuild", app.IndexRebuildOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.RebuildIndex(ctx, env.Dir, opts.(app.IndexRebuildOptions))
				return res("system.index_rebuild", opts, v, err)
			}),
		mk("system.doctor", "doctor", "", 0, "", nil, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				return res("system.doctor", opts, app.Doctor(ctx, env.Dir), nil)
			}),
		mk("system.doctor_filter", "doctor_filter", "w", 5, "", nil, false, false, nil),
	}
}
