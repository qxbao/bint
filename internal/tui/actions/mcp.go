package actions

import (
	"context"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/jobs"
)

// Hành động của màn MCP (docs/mcp-gateway-plan.md mục 8, M4): danh mục MCP trên máy, daemon gateway dùng chung, quyền theo role.
// Mọi hành động gọi tầng dịch vụ app.MCP*; add, remove, index, gateway restart và gateway stop chỉ dành cho người (HumanOnly, kiểm
// ở dịch vụ bằng RequireHuman) và ghi dữ liệu (Writes). check không ghi dữ liệu của Bint (chỉ tạo thư mục cwd riêng của service) và
// agent được chạy nó (D5) nên Writes = false. Hành động nền (check, index, restart) là Job có log trong bảng việc nền.

// MCPCheckOutcome là kết quả hành động mcp.check: kết quả từng service kèm toàn bộ dòng log (bảng việc nền không giữ log việc đã xong).
type MCPCheckOutcome struct {
	Results []app.MCPCheckResult
	Log     []string
}

// MCPIndexOutcome là kết quả hành động mcp.index: kết quả lập chỉ mục kèm đầu ra của codegraph.
type MCPIndexOutcome struct {
	Result app.MCPIndexResult
	Log    []string
}

// MCPRestartOutcome là kết quả hành động mcp.restart.
type MCPRestartOutcome struct {
	Result app.MCPGatewayRestartResult
	Log    []string
}

// mcpLabel là nhãn việc nền ("mcp check jira").
func mcpLabel(verb, service string) string {
	return strings.TrimSpace("mcp " + verb + " " + service)
}

func mcpJobInfo(verb string, service func(opts any) string) func(any) jobs.Info {
	return func(opts any) jobs.Info {
		return jobs.Info{Kind: jobs.KindAction, Label: mcpLabel(verb, service(opts))}
	}
}

// runMCPCheck kiểm tra từng service (một service, hoặc mọi service đã đăng ký khi Service rỗng): mỗi service xong ghi một dòng log.
// Huỷ ctx giữa chừng thì trả phần đã có kèm lỗi huỷ.
func runMCPCheck(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
	o, _ := opts.(app.MCPCheckOptions)
	if err := o.Validate(); err != nil {
		return MCPCheckOutcome{}, err
	}
	ids := []string{o.Service}
	if o.Service == "" {
		ids = nil
		list, err := app.MCPList(ctx, env.Dir, app.MCPListOptions{})
		if err != nil {
			return MCPCheckOutcome{}, err
		}
		for _, s := range list {
			if s.Registered {
				ids = append(ids, s.ID)
			}
		}
	}
	var out MCPCheckOutcome
	say := func(line string) { out.Log = append(out.Log, line); br.Log(line) }
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		say(env.T("mc.job.check.start", argsOf("service", id)))
		rs, err := app.MCPCheck(ctx, env.Dir, app.MCPCheckOptions{Service: id})
		if err != nil {
			return out, err
		}
		for _, r := range rs {
			out.Results = append(out.Results, r)
			line := env.T("mc.job.check.one", argsOf("service", r.Service, "status", env.T("mc.chk."+r.Status)))
			if r.Detail != "" {
				line += ": " + r.Detail
			}
			say(line)
		}
	}
	if len(ids) == 0 {
		say(env.T("mc.job.check.none"))
	}
	return out, nil
}

// runMCPIndex chạy codegraph init/sync; đầu ra của codegraph đi từng dòng về khung log.
func runMCPIndex(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
	o, _ := opts.(app.MCPIndexOptions)
	var out MCPIndexOutcome
	w := &collectLines{emit: func(l string) { out.Log = append(out.Log, l); br.Log(l) }}
	r, err := app.MCPIndex(ctx, env.Dir, o, io.Writer(w))
	w.Flush()
	out.Result = r
	return out, err
}

// runMCPRestart dừng mọi upstream của service ở gateway (lời gọi sau khởi động mới).
func runMCPRestart(ctx context.Context, env Env, opts any, br *jobs.Bridge) (any, error) {
	o, _ := opts.(app.MCPGatewayRestartOptions)
	var out MCPRestartOutcome
	line := env.T("mc.job.restart.start", argsOf("service", o.Service))
	out.Log = append(out.Log, line)
	br.Log(line)
	r, err := app.MCPGatewayRestart(ctx, o)
	out.Result = r
	if err == nil {
		done := env.T("mc.job.restart.done", argsOf("service", r.Service, "stopped", r.Stopped))
		out.Log = append(out.Log, done)
		br.Log(done)
	}
	return out, err
}

func svcOf(opts any) string {
	switch o := opts.(type) {
	case app.MCPCheckOptions:
		return o.Service
	case app.MCPIndexOptions:
		return o.Service
	case app.MCPGatewayRestartOptions:
		return o.Service
	}
	return ""
}

func mcpActions() []Action {
	mk := func(id, name, key string, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		return Action{ID: id, Screen: MCP, Key: key, Priority: prio, LabelKey: "action.mcp." + name + ".label",
			TitleKey: "action.mcp." + name + ".title", CLIPath: cli, Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	job := func(a Action, verb string, run JobFunc) Action {
		a.Do, a.Job, a.JobInfo = nil, run, mcpJobInfo(verb, svcOf)
		return a
	}
	return []Action{
		// Giao diện thuần.
		mk("mcp.next_view", "next_view", ".", 9, "", nil, false, false, nil),
		mk("mcp.prev_view", "prev_view", ",", 9, "", nil, false, false, nil),
		mk("mcp.refresh", "refresh", "ctrl+r", 9, "", nil, false, false, nil),
		mk("mcp.filter", "filter", "p", 6, "", nil, false, false, nil),
		// Sửa cấu hình: màn MCP không chép luồng sửa; mô hình gốc chuyển sang Hệ thống và chạy system.config_edit (Validate khi lưu).
		mk("mcp.config_edit", "config_edit", "e", 1, "", nil, true, false, nil),

		// Đọc.
		mk("mcp.list", "list", "", 0, "mcp list", app.MCPListOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPList(ctx, env.Dir, opts.(app.MCPListOptions))
				return res("mcp.list", opts, v, err)
			}),
		mk("mcp.gateway_status", "gateway_status", "", 0, "mcp gateway status", app.MCPGatewayStatusOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPGatewayStatus(ctx, opts.(app.MCPGatewayStatusOptions))
				return res("mcp.gateway_status", opts, v, err)
			}),
		mk("mcp.grants", "grants", "", 0, "mcp grants", app.MCPGrantsOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPGrants(ctx, env.Dir, opts.(app.MCPGrantsOptions))
				return res("mcp.grants", opts, v, err)
			}),
		mk("mcp.tools", "tools", "", 0, "mcp tools", app.MCPToolsOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPTools(env.Version, opts.(app.MCPToolsOptions))
				return res("mcp.tools", opts, v, err)
			}),
		mk("mcp.log", "log", "l", 4, "mcp gateway log", app.MCPGatewayLogOptions{}, false, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPGatewayLog(ctx, opts.(app.MCPGatewayLogOptions))
				return res("mcp.log", opts, v, err)
			}),

		// Việc nền: kiểm tra (đọc), lập chỉ mục và khởi động lại (ghi, chỉ người).
		job(mk("mcp.check", "check", "c", 2, "mcp check", app.MCPCheckOptions{}, false, false, nil), "check", runMCPCheck),
		job(mk("mcp.index", "index", "i", 5, "mcp index", app.MCPIndexOptions{}, true, true, nil), "index", runMCPIndex),
		job(mk("mcp.restart", "restart", "R", 5, "mcp gateway restart", app.MCPGatewayRestartOptions{}, true, true, nil), "restart", runMCPRestart),

		// Đăng ký trên máy (ghi mcp.yml; chỉ người).
		mk("mcp.add", "add", "a", 1, "mcp add", app.MCPAddOptions{}, true, true,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPAdd(ctx, env.Dir, opts.(app.MCPAddOptions))
				return res("mcp.add", opts, v, err)
			}),
		mk("mcp.remove", "remove", "x", 3, "mcp remove", app.MCPRemoveOptions{}, true, true,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPRemove(ctx, env.Dir, opts.(app.MCPRemoveOptions))
				return res("mcp.remove", opts, v, err)
			}),

		// Daemon gateway: agent được khởi động (D5: shim tự khởi động), dừng thì chỉ người.
		mk("mcp.gateway_start", "gateway_start", "s", 7, "mcp gateway start", app.MCPGatewayStartOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				o := opts.(app.MCPGatewayStartOptions)
				o.Foreground = false // TUI luôn khởi động tách rời: chạy tại chỗ sẽ chặn giao diện
				v, err := app.MCPGatewayStart(ctx, env.Version, o, io.Discard, nil)
				return res("mcp.gateway_start", opts, v, err)
			}),
		mk("mcp.gateway_stop", "gateway_stop", "S", 8, "mcp gateway stop", app.MCPGatewayStopOptions{}, true, true,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				v, err := app.MCPGatewayStop(ctx, opts.(app.MCPGatewayStopOptions))
				return res("mcp.gateway_stop", opts, v, err)
			}),
	}
}

func argsOf(kv ...any) i18n.A {
	a := i18n.A{}
	for i := 0; i+1 < len(kv); i += 2 {
		a[kv[i].(string)] = kv[i+1]
	}
	return a
}
