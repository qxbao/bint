package actions

import (
	"context"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/knowledge"
)

// FilterOptions là bộ lọc danh sách tri thức của TUI (không phải lệnh CLI: hành động giao diện thuần
// `knowledge.filter`); form lọc sinh từ các thẻ ở đây. Status "pending" = candidate + stale; rỗng = mọi
// mục còn hiệu lực (trừ deprecated).
type FilterOptions struct {
	Status     string `flag:"status" form:"choices=pending|candidate|verified|stale|deprecated"`
	Type       string `flag:"type" form:"choices=rule|warning|pattern|fact|decision|lesson"`
	Origin     string `flag:"origin" form:"choices=human|agent|imported"`
	ThirdParty bool   `flag:"third-party"`
	Batch      string `flag:"batch" form:"suggest=batches"`
	Path       string `flag:"path" form:"suggest=files"`
	Text       string `flag:"text"`
	// NoBatch: chỉ mục không thuộc lô nào. Chỉ đặt từ hộp thư (dòng "tri thức còn lại"), không phải ô form;
	// form giữ nguyên giá trị này trừ khi người dùng nhập một lô.
	NoBatch bool
}

// Filter đổi các giá trị form thành bộ lọc dùng chung với CLI.
func (o FilterOptions) Filter() knowledge.Filter {
	f := knowledge.Filter{ThirdParty: o.ThirdParty, Batch: o.Batch, NoBatch: o.NoBatch && o.Batch == "", Path: o.Path, Text: o.Text}
	switch o.Status {
	case "":
	case "pending":
		f.Statuses = []string{knowledge.StatusCandidate, knowledge.StatusStale}
	default:
		f.Statuses = []string{o.Status}
	}
	if o.Type != "" {
		f.Types = []string{o.Type}
	}
	if o.Origin != "" {
		f.Origins = []string{o.Origin}
	}
	return f
}

// EditOptions là tham số hành động sửa một mục trong $EDITOR.
type EditOptions struct {
	ID   string
	Path string
	// Line > 0: mở tại dòng đó nếu trình soạn thảo hỗ trợ (+N).
	Line int
}

// MarkOptions chọn thẻ phụ/chế độ giao diện thuần (không có thẻ flag/arg: không thành ô form).
type MarkOptions struct{}

func openK(ctx context.Context, env Env) (*app.KnowledgeWS, error) {
	return app.OpenKnowledge(ctx, env.Dir)
}

// res gói kết quả thành ResultMsg.
func res(id string, opts, value any, err error) tea.Msg {
	return ResultMsg{ID: id, Opts: opts, Value: value, Err: err}
}

// withK mở không gian tri thức rồi chạy f; lỗi mở trả thẳng trong ResultMsg.
func withK(id string, f func(ctx context.Context, k *app.KnowledgeWS, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		k, err := openK(ctx, env)
		if err != nil {
			return res(id, opts, nil, err)
		}
		v, err := f(ctx, k, opts)
		return res(id, opts, v, err)
	}
}

// editorCmd dựng lệnh mở path trong trình soạn thảo (tại dòng line nếu hỗ trợ); env.Editor thay được trong kiểm thử.
func editorCmd(ctx context.Context, env Env, path string, line int) *exec.Cmd {
	if env.Editor != nil {
		return env.Editor(path)
	}
	argv := app.EditorArgvAt(path, line)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

func doEdit(ctx context.Context, env Env, opts any) tea.Msg {
	o := opts.(EditOptions)
	return ExecMsg{ID: "knowledge.edit", Opts: o, Cmd: editorCmd(ctx, env, o.Path, o.Line)}
}

func doPublish(id string, dry bool) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		o := opts.(app.PublishOptions)
		o.Dir, o.DryRun = env.Dir, dry
		// Bước ghi không bao giờ đặt Yes: nó chỉ ghi khi o.Confirm (do màn hình dựng, so Detail với diff đã
		// xem) đồng ý; thiếu Confirm thì PublishAgents từ chối ghi.
		o.Yes = false
		rep, err := app.PublishAgents(ctx, o)
		if rep == nil { // màn hình khớp theo giá trị app.PublishReport, không phải con trỏ
			return res(id, opts, nil, err)
		}
		return res(id, opts, *rep, err)
	}
}

// knowledgeActions là các hành động của màn Tri thức (giai đoạn 1).
func knowledgeActions() []Action {
	K := Knowledge
	t := func(name string) (string, string) {
		return "action.knowledge." + name + ".label", "action.knowledge." + name + ".title"
	}
	mk := func(id, name, key string, footer bool, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		l, ti := t(name)
		return Action{ID: id, Screen: K, Key: key, Footer: footer, Priority: prio, LabelKey: l, TitleKey: ti, CLIPath: cli,
			Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	list := []Action{
		// Duyệt (chỉ người; kiểm InAgentSession nằm ở tầng dịch vụ, khoá ở sổ chỉ để báo sớm).
		mk("knowledge.approve", "approve", "a", true, 1, "knowledge approve", app.ApproveOptions{}, true, true,
			withK("knowledge.approve", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.ApproveKnowledge(ctx, k, o.(app.ApproveOptions))
			})),
		mk("knowledge.approve_refresh", "approve_refresh", "f", false, 2, "knowledge approve", app.ApproveOptions{}, true, true,
			withK("knowledge.approve_refresh", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				ao := o.(app.ApproveOptions)
				ao.Refresh = true
				return app.ApproveKnowledge(ctx, k, ao)
			})),
		mk("knowledge.reject", "reject", "r", true, 2, "knowledge reject", app.RejectOptions{}, true, true,
			withK("knowledge.reject", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.RejectKnowledge(ctx, k, o.(app.RejectOptions))
			})),
		mk("knowledge.deprecate", "deprecate", "d", false, 5, "knowledge deprecate", app.RejectOptions{}, true, true,
			withK("knowledge.deprecate", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.RejectKnowledge(ctx, k, o.(app.RejectOptions))
			})),
		mk("knowledge.approve_batch", "approve_batch", "A", true, 3, "", app.BatchOptions{}, true, true,
			withK("knowledge.approve_batch", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				bo := o.(app.BatchOptions)
				bo.To = knowledge.StatusVerified
				return app.ReviewBatch(ctx, k, bo)
			})),
		mk("knowledge.reject_batch", "reject_batch", "R", false, 6, "", app.BatchOptions{}, true, true,
			withK("knowledge.reject_batch", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				bo := o.(app.BatchOptions)
				bo.To = knowledge.StatusDeprecated
				return app.ReviewBatch(ctx, k, bo)
			})),
		// Kế hoạch lô (chỉ đọc): hộp xác nhận nêu số mục và liệt kê mục bị loại vì lỗi kiểm tra.
		mk("knowledge.plan_batch", "plan_batch", "", false, 0, "", app.BatchOptions{}, false, false,
			withK("knowledge.plan_batch", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				bo := o.(app.BatchOptions)
				return app.PlanBatch(ctx, k, bo.Refs, bo.To)
			})),
		mk("knowledge.edit", "edit", "e", true, 4, "", EditOptions{}, true, false, doEdit),
		// Thêm / xoá / kiểm tra.
		mk("knowledge.add", "add", "n", true, 5, "knowledge add", app.AddOptions{}, true, false,
			withK("knowledge.add", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.AddKnowledge(ctx, k, o.(app.AddOptions))
			})),
		mk("knowledge.rm", "rm", "delete", false, 7, "knowledge rm", app.RemoveOptions{}, true, false,
			withK("knowledge.rm", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				ro := o.(app.RemoveOptions)
				removed, err := app.RemoveKnowledge(k, ro.Refs)
				return removed, err
			})),
		mk("knowledge.verify", "verify", "v", false, 6, "knowledge verify", app.VerifyOptions{}, false, false,
			withK("knowledge.verify", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				vo := o.(app.VerifyOptions)
				return app.VerifyKnowledge(ctx, k, vo.Refs, false, time.Now())
			})),
		mk("knowledge.verify_apply", "verify_apply", "", false, 8, "knowledge verify", app.VerifyOptions{}, true, true,
			withK("knowledge.verify_apply", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				vo := o.(app.VerifyOptions)
				return app.VerifyKnowledge(ctx, k, vo.Refs, true, time.Now())
			})),
		// Duyệt/lọc/xem: hành động giao diện thuần (mô hình màn hình xử lý), có trong bảng lệnh.
		mk("knowledge.review", "review", "", false, 0, "knowledge review", app.ReviewQueueOptions{}, false, false, nil),
		mk("knowledge.list", "list", "F", true, 6, "knowledge list", app.KnowledgeListOptions{}, false, false, nil),
		mk("knowledge.show", "show", "Enter", true, 1, "knowledge show", app.KnowledgeShowOptions{}, false, false, nil),
		mk("knowledge.filter", "filter", "/", true, 4, "", FilterOptions{}, false, false, nil),
		mk("knowledge.select", "select", "x", true, 5, "", MarkOptions{}, false, false, nil),
		mk("knowledge.transitions", "transitions", "T", false, 9, "knowledge transitions", nil, false, false, nil),
		mk("knowledge.refresh", "refresh", "ctrl+r", false, 9, "", nil, false, false, nil),
		mk("knowledge.next_view", "next_view", ".", false, 9, "", nil, false, false, nil),
		mk("knowledge.prev_view", "prev_view", ",", false, 9, "", nil, false, false, nil),
		// Thẻ phụ.
		mk("knowledge.search", "search", "", false, 0, "knowledge search", app.SearchOptions{}, false, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				v, err := app.SearchKnowledge(ctx, env.Dir, o.(app.SearchOptions))
				return res("knowledge.search", o, v, err)
			}),
		mk("knowledge.stats", "stats", "", false, 0, "knowledge stats", app.StatsOptions{}, false, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				so := o.(app.StatsOptions)
				so.NoIngest = so.NoIngest || env.ReadOnly // chế độ chỉ đọc không nạp spool vào cơ sở dữ liệu
				v, err := app.StatsKnowledge(ctx, env.Dir, so)
				return res("knowledge.stats", o, v, err)
			}),
		mk("knowledge.diff", "diff", "", false, 0, "knowledge diff", app.DiffOptions{}, false, false,
			withK("knowledge.diff", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				return app.DiffKnowledge(ctx, k, o.(app.DiffOptions))
			})),
		mk("knowledge.revalidate", "revalidate", "", false, 0, "knowledge revalidate", app.RevalidateOptions{}, true, true,
			withK("knowledge.revalidate", func(ctx context.Context, k *app.KnowledgeWS, o any) (any, error) {
				ro := o.(app.RevalidateOptions)
				d := app.RevalidateDefaults(k.Config)
				ro.PriorRunUSD, ro.Timeout = d.PriorRunUSD, d.Timeout
				return app.RevalidateKnowledge(ctx, k, ro)
			})),
		mk("bootstrap.publish_preview", "publish_preview", "", false, 0, "bootstrap publish", app.PublishOptions{}, false, false,
			doPublish("bootstrap.publish_preview", true)),
		mk("bootstrap.publish", "publish", "", false, 0, "bootstrap publish", app.PublishOptions{}, true, false,
			doPublish("bootstrap.publish", false)),
		mk("inspect", "inspect", "", false, 0, "inspect", app.InspectOptions{}, false, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				v, err := app.Inspect(ctx, env.Dir, o.(app.InspectOptions))
				return res("inspect", o, v, err)
			}),
		mk("bootstrap", "bootstrap", "", false, 0, "bootstrap", app.BootstrapOptions{}, true, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				bo := o.(app.BootstrapOptions)
				bo.Version = env.Version
				v, err := app.Bootstrap(ctx, env.Dir, bo)
				return res("bootstrap", o, v, err)
			}),
		mk("bootstrap.skill", "skill", "", false, 0, "bootstrap skill", app.BootstrapSkillOptions{}, false, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				v, err := app.BootstrapSkill(ctx, env.Dir, o.(app.BootstrapSkillOptions))
				return res("bootstrap.skill", o, v, err)
			}),
		mk("context", "context", "", false, 0, "context", app.ContextOptions{}, false, false,
			func(ctx context.Context, env Env, o any) tea.Msg {
				co := o.(app.ContextOptions)
				co.NoTrack = true // hành động chỉ-đọc: không ghi spool/bộ nhớ phiên dù có BINT_SESSION_ID
				v, err := app.ContextPack(ctx, env.Dir, co)
				return res("context", o, v, err)
			}),
	}
	// Hành động dùng chung Options với hành động khác dùng chung nhãn form.
	ns := map[string]string{"knowledge.approve_refresh": "knowledge.approve", "knowledge.deprecate": "knowledge.reject",
		"knowledge.approve_batch": "knowledge.batch", "knowledge.reject_batch": "knowledge.batch", "knowledge.plan_batch": "knowledge.batch",
		"knowledge.verify_apply": "knowledge.verify", "bootstrap.publish_preview": "bootstrap.publish"}
	for i := range list {
		list[i].FormID = ns[list[i].ID]
	}
	return list
}
