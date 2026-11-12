package actions_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
)

// Thuộc tính của từng hành động MCP: CLIPath đúng, kiểu (Do/Job), Writes, HumanOnly (docs/mcp-gateway-plan.md mục 8).
func TestMCPActionProperties(t *testing.T) {
	type want struct {
		cli             string
		job, ui         bool
		writes, humanly bool
	}
	table := map[string]want{
		"mcp.list":           {cli: "mcp list"},
		"mcp.gateway_status": {cli: "mcp gateway status"},
		"mcp.grants":         {cli: "mcp grants"},
		"mcp.tools":          {cli: "mcp tools"},
		"mcp.log":            {cli: "mcp gateway log"},
		"mcp.check":          {cli: "mcp check", job: true},
		"mcp.index":          {cli: "mcp index", job: true, writes: true, humanly: true},
		"mcp.restart":        {cli: "mcp gateway restart", job: true, writes: true, humanly: true},
		"mcp.add":            {cli: "mcp add", writes: true, humanly: true},
		"mcp.remove":         {cli: "mcp remove", writes: true, humanly: true},
		"mcp.gateway_start":  {cli: "mcp gateway start", writes: true},
		"mcp.gateway_stop":   {cli: "mcp gateway stop", writes: true, humanly: true},
		"mcp.config_edit":    {ui: true, writes: true},
		"mcp.refresh":        {ui: true}, "mcp.filter": {ui: true}, "mcp.next_view": {ui: true}, "mcp.prev_view": {ui: true},
	}
	reg := actions.Default()
	seen := 0
	for _, a := range reg.All() {
		if a.Screen != actions.MCP {
			continue
		}
		seen++
		w, ok := table[a.ID]
		if !ok {
			t.Errorf("%s chưa có trong bảng thuộc tính", a.ID)
			continue
		}
		if a.CLIPath != w.cli || a.Writes != w.writes || a.HumanOnly != w.humanly {
			t.Errorf("%s: CLIPath=%q Writes=%v HumanOnly=%v, muốn %q %v %v", a.ID, a.CLIPath, a.Writes, a.HumanOnly, w.cli, w.writes, w.humanly)
		}
		switch {
		case w.ui:
			if a.Do != nil || a.Job != nil {
				t.Errorf("%s là hành động giao diện thuần, không có Do/Job", a.ID)
			}
		case w.job:
			if a.Job == nil || a.Do != nil || a.JobInfo == nil || a.Options == nil {
				t.Errorf("%s phải là Job (có JobInfo, Options), không có Do", a.ID)
			}
		default:
			if a.Do == nil || a.Job != nil || a.Options == nil {
				t.Errorf("%s phải là Do có Options", a.ID)
			}
		}
		if !w.ui && !actions.HasCLIPath(a.CLIPath) { // CLIPath phải có tham chiếu TUI (viết lại lời nhắc lệnh thành phím)
			t.Errorf("%s: %q chưa có trong bảng tham chiếu CLI", a.ID, a.CLIPath)
		}
		if got := a.Check(actions.Mode{ReadOnly: true}) == actions.Hidden; got != w.writes {
			t.Errorf("%s: chỉ đọc ẩn = %v, muốn %v", a.ID, got, w.writes)
		}
		if got := a.Check(actions.Mode{Agent: true}) == actions.Locked; got != w.humanly {
			t.Errorf("%s: khoá trong phiên agent = %v, muốn %v", a.ID, got, w.humanly)
		}
	}
	if seen != len(table) {
		t.Errorf("có %d hành động mcp.*, bảng có %d", seen, len(table))
	}
	if _, ok := reg.ByID("system.mcp_tools"); ok {
		t.Error("system.mcp_tools phải chuyển sang mcp.tools")
	}
}

func mcpEnv(t *testing.T) actions.Env {
	t.Helper()
	root, _ := seed(t)
	cat := i18n.MustLoad("en")
	return actions.Env{Dir: root, Version: "test", Cat: cat}
}

// Hành động đọc gọi dịch vụ thật: danh mục, trạng thái gateway (chưa chạy: không lỗi), công cụ, log (không daemon), quyền.
func TestMCPReadActionsWithRealServices(t *testing.T) {
	env := mcpEnv(t)
	if r := do(t, "mcp.list", env, app.MCPListOptions{}); r.Err != nil || len(r.Value.([]app.MCPServiceStatus)) < 4 {
		t.Fatalf("mcp.list: %+v", r)
	}
	if r := do(t, "mcp.gateway_status", env, app.MCPGatewayStatusOptions{}); r.Err != nil || r.Value.(app.MCPGatewayStatusResult).Running {
		t.Fatalf("mcp.gateway_status: %+v", r)
	}
	if r := do(t, "mcp.tools", env, app.MCPToolsOptions{}); r.Err != nil || len(r.Value.([]app.MCPTool)) < 4 {
		t.Fatalf("mcp.tools: %+v", r)
	}
	r := do(t, "mcp.log", env, app.MCPGatewayLogOptions{Service: "codegraph"})
	lg, _ := r.Value.(app.MCPGatewayLogResult)
	if r.Err != nil || lg.Service != "codegraph" || !strings.Contains(lg.Note, "gateway") {
		t.Fatalf("mcp.log: %+v", r)
	}
	if r := do(t, "mcp.log", env, app.MCPGatewayLogOptions{Service: "khong-co"}); !app.IsUsage(r.Err) {
		t.Fatalf("mcp.log service lạ: %+v", r)
	}
	if r := do(t, "mcp.grants", env, app.MCPGrantsOptions{}); r.Err != nil {
		t.Fatalf("mcp.grants: %v", r.Err)
	}
}

// add/remove qua Do thật (người dùng): đăng ký rồi gỡ; đăng ký service lạ là lỗi cách dùng; trong phiên agent bị từ chối ở dịch vụ.
func TestMCPAddRemoveActions(t *testing.T) {
	env := mcpEnv(t)
	r := do(t, "mcp.add", env, app.MCPAddOptions{Service: "deepwiki"})
	if r.Err != nil || r.Value.(app.MCPAddResult).Service != "deepwiki" {
		t.Fatalf("mcp.add: %+v", r)
	}
	if r := do(t, "mcp.add", env, app.MCPAddOptions{Service: "khong-co"}); !app.IsUsage(r.Err) {
		t.Fatalf("mcp.add lạ: %+v", r)
	}
	if r := do(t, "mcp.remove", env, app.MCPRemoveOptions{Service: "deepwiki"}); r.Err != nil || r.Value.(app.MCPRemoveResult).Service != "deepwiki" {
		t.Fatalf("mcp.remove: %+v", r)
	}
	if r := do(t, "mcp.remove", env, app.MCPRemoveOptions{Service: "deepwiki"}); !app.IsUsage(r.Err) {
		t.Fatalf("gỡ lần hai phải lỗi: %+v", r)
	}
	t.Setenv("CLAUDECODE", "1")
	if r := do(t, "mcp.add", env, app.MCPAddOptions{Service: "deepwiki"}); !errors.Is(r.Err, app.ErrAgentForbidden) {
		t.Fatalf("mcp.add trong phiên agent: %v", r.Err)
	}
	if r := do(t, "mcp.gateway_stop", env, app.MCPGatewayStopOptions{}); !errors.Is(r.Err, app.ErrAgentForbidden) {
		t.Fatalf("mcp.gateway_stop trong phiên agent: %v", r.Err)
	}
}

// bridge dựng cầu việc nền giả và gom log.
func bridge(ctx context.Context) (*jobs.Bridge, func() []string, func() any) {
	ch := make(chan tea.Msg, 64)
	br := jobs.NewBridge(ctx, 1, ch)
	drain := func() []string {
		var lines []string
		for {
			select {
			case m := <-ch:
				if l, ok := m.(jobs.LogMsg); ok {
					lines = append(lines, l.Line)
				}
			default:
				return lines
			}
		}
	}
	return br, drain, func() any { return nil }
}

func job(t *testing.T, id string) actions.JobFunc {
	t.Helper()
	a, ok := actions.Default().ByID(id)
	if !ok || a.Job == nil {
		t.Fatalf("%s không có Job", id)
	}
	return a.Job
}

// mcp.check: mỗi service một dòng log và kết quả được trả; service chưa đăng ký là lỗi; không có service đăng ký thì ghi chú.
func TestMCPCheckJob(t *testing.T) {
	env := mcpEnv(t)
	br, drain, _ := bridge(t.Context())
	v, err := job(t, "mcp.check")(t.Context(), env, app.MCPCheckOptions{}, br)
	out := v.(actions.MCPCheckOutcome)
	if err != nil || len(out.Results) != 0 || len(drain()) == 0 || len(out.Log) == 0 {
		t.Fatalf("không service đăng ký: %+v %v", out, err)
	}
	if _, err := job(t, "mcp.check")(t.Context(), env, app.MCPCheckOptions{Service: "jira"}, br); !app.IsUsage(err) {
		t.Fatalf("service chưa đăng ký: %v", err)
	}
	// Service đăng ký nhưng thiếu lệnh trên PATH: kết quả fail kèm dòng log, không phải lỗi của Job.
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{"codegraph": {Command: "khong-co-lenh-nay-xyz"}}}); err != nil {
		t.Fatal(err)
	}
	br, drain, _ = bridge(t.Context())
	v, err = job(t, "mcp.check")(t.Context(), env, app.MCPCheckOptions{}, br)
	out = v.(actions.MCPCheckOutcome)
	lines := drain()
	if err != nil || len(out.Results) != 1 || out.Results[0].Status != app.MCPStatusFail || len(lines) < 2 || !strings.Contains(strings.Join(lines, "\n"), "codegraph") {
		t.Fatalf("check codegraph: %+v %v log=%q", out, err, lines)
	}
	// Huỷ trước khi bắt đầu: trả lỗi huỷ.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	br, _, _ = bridge(ctx)
	if _, err := job(t, "mcp.check")(ctx, env, app.MCPCheckOptions{}, br); !errors.Is(err, context.Canceled) {
		t.Fatalf("huỷ: %v", err)
	}
}

// mcp.restart khi chưa có daemon: lỗi rõ ràng và vẫn có dòng log đầu; trong phiên agent bị từ chối. mcp.index khi chưa đăng ký: lỗi cách dùng.
func TestMCPRestartAndIndexJobs(t *testing.T) {
	env := mcpEnv(t)
	br, drain, _ := bridge(t.Context())
	v, err := job(t, "mcp.restart")(t.Context(), env, app.MCPGatewayRestartOptions{Service: "codegraph"}, br)
	if err == nil || !strings.Contains(err.Error(), "không chạy") || len(drain()) != 1 || len(v.(actions.MCPRestartOutcome).Log) != 1 {
		t.Fatalf("restart không daemon: %+v %v", v, err)
	}
	if _, err := job(t, "mcp.index")(t.Context(), env, app.MCPIndexOptions{Service: "codegraph"}, br); !app.IsUsage(err) {
		t.Fatalf("index chưa đăng ký: %v", err)
	}
	t.Setenv("CLAUDECODE", "1")
	if _, err := job(t, "mcp.restart")(t.Context(), env, app.MCPGatewayRestartOptions{Service: "codegraph"}, br); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("restart trong phiên agent: %v", err)
	}
	if _, err := job(t, "mcp.index")(t.Context(), env, app.MCPIndexOptions{Service: "codegraph"}, br); !errors.Is(err, app.ErrAgentForbidden) {
		t.Fatalf("index trong phiên agent: %v", err)
	}
	// Nhãn việc nền.
	a, _ := actions.Default().ByID("mcp.check")
	if l := a.JobInfo(app.MCPCheckOptions{Service: "jira"}); l.Label != "mcp check jira" || l.Kind != jobs.KindAction {
		t.Errorf("JobInfo: %+v", l)
	}
	if l := a.JobInfo(app.MCPCheckOptions{}); l.Label != "mcp check" {
		t.Errorf("JobInfo không service: %+v", l)
	}
}

// Thẻ MCP của Hệ thống đã chuyển sang màn MCP: bảng tham chiếu CLI, lý do loại trừ của test phủ lệnh, danh mục chuỗi và tài liệu không còn nhắc
// "Hệ thống › MCP" (hay "System › MCP"), và `mcp tools` trỏ tới màn MCP.
func TestNoReferenceToSystemMCPTab(t *testing.T) {
	docs := []string{"../../../docs/tui-plan.md", "../../../docs/workflows.md", "../../../docs/mcp-gateway-plan.md", "../../../README.md"}
	code := []string{"cliref.go", "coverage.go", "system.go", "../../i18n/locales/vi.yaml", "../../i18n/locales/en.yaml"}
	check := func(files []string, bads ...string) {
		for _, f := range files {
			for _, l := range strings.Split(string(mustRead(t, f)), "\n") {
				for _, bad := range bads {
					if strings.Contains(l, bad) {
						t.Errorf("%s còn nhắc %q: %s", f, bad, strings.TrimSpace(l))
					}
				}
			}
		}
	}
	check(append(docs, code...), "Hệ thống › MCP", "Hệ thống > MCP", "System › MCP")
	check(code, "sy.tab.mcp", "sy.mcp.", "system.mcp_tools")
	if !strings.Contains(string(mustRead(t, "cliref.go")), `{path: "mcp tools", id: "mcp.tools", tab: "mc.tab.tools"`) {
		t.Error("cliref.go: mcp tools phải trỏ tới màn MCP (mcp.tools, thẻ Công cụ Bint)")
	}
	if ex := actions.Data.Excluded; ex["mcp serve"] == "" {
		t.Error("mcp serve phải còn trong Excluded")
	} else {
		for path := range ex {
			if strings.HasPrefix(path, "mcp ") && path != "mcp serve" {
				t.Errorf("Excluded còn lệnh mcp %q (chỉ mcp serve được loại trừ)", path)
			}
		}
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
