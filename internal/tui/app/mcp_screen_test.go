package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/form"
	"bint/internal/tui/jobs"
	mcscreen "bint/internal/tui/screens/mcp"
	"bint/internal/tui/theme"
)

// mcpCapture dựng sổ hành động ghi lại các hành động của màn MCP và Hệ thống (không gọi dịch vụ thật): Do trả ngay giá trị dựng sẵn, Job ghi một
// dòng log rồi chờ tới khi được thả (release) hoặc bị huỷ.
func mcpCapture(jobIDs ...string) (*actions.Registry, *capt) {
	c := &capt{calls: map[string][]any{}, values: map[string]any{}, release: map[string]chan struct{}{}, started: map[string]chan struct{}{}}
	for _, id := range jobIDs {
		c.release[id], c.started[id] = make(chan struct{}), make(chan struct{}, 4)
	}
	var list []actions.Action
	for _, a := range actions.Default().All() {
		a := a
		if a.Screen == actions.MCP || a.Screen == actions.System {
			if a.Do != nil {
				a.Do = func(_ context.Context, _ actions.Env, o any) tea.Msg {
					c.record(a.ID, o)
					return actions.ResultMsg{ID: a.ID, Opts: o, Value: c.values[a.ID]}
				}
			}
			if a.Job != nil {
				a.Job = func(ctx context.Context, _ actions.Env, o any, br *jobs.Bridge) (any, error) {
					c.record(a.ID, o)
					br.Log("dòng log đầu của " + a.ID)
					c.started[a.ID] <- struct{}{}
					select {
					case <-c.release[a.ID]:
					case <-ctx.Done():
						return c.values[a.ID], ctx.Err()
					}
					return c.values[a.ID], nil
				}
			}
		}
		list = append(list, a)
	}
	return actions.NewRegistry(list), c
}

// newMCPModel dựng mô hình ở màn MCP (đã nạp thẻ Dịch vụ) với sổ hành động ghi lại.
func newMCPModel(t *testing.T, o Options, jobIDs ...string) (*Model, *capt) {
	t.Helper()
	reg, c := mcpCapture(jobIDs...)
	o.Registry, o.Mode = reg, theme.Mono
	if o.Lang == "" {
		o.Lang = "vi"
	}
	m := newModel(t, o, sampleData(), 140, 40)
	press(t, m, "8")
	return m, c
}

// Phím 8, [ ] và bấm chuột vào tab chuyển tới màn MCP; màn MCP đứng ngay sau Hệ thống.
func TestMCPTabKeyBracketsAndMouse(t *testing.T) {
	if n := len(actions.Screens); n != 8 || actions.Screens[6] != actions.System || actions.Screens[7] != actions.MCP {
		t.Fatalf("Screens = %v", actions.Screens)
	}
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 140, 40)
	press(t, m, "8")
	if m.Tab() != 7 || m.Screen() != actions.MCP {
		t.Fatalf("phím 8 → tab %d (%s)", m.Tab(), m.Screen())
	}
	if out := plain(m.Render()); !strings.Contains(out, "Dịch vụ MCP (6)") {
		t.Fatalf("màn MCP:\n%s", out)
	}
	press(t, m, "7", "]")
	if m.Screen() != actions.MCP {
		t.Fatalf("] từ Hệ thống: %s", m.Screen())
	}
	press(t, m, "]")
	if m.Tab() != 0 {
		t.Fatalf("] từ MCP phải vòng về Tổng quan: %d", m.Tab())
	}
	press(t, m, "[")
	if m.Screen() != actions.MCP {
		t.Fatalf("[ từ Tổng quan phải về MCP: %s", m.Screen())
	}
	press(t, m, "1")
	x, y := cellOf(t, m, "MCP")
	if y != 1 {
		t.Fatalf("nhãn tab MCP nằm ở hàng %d, muốn hàng thanh tab (1)", y)
	}
	clickAt(t, m, x+1, y)
	if m.Screen() != actions.MCP {
		t.Fatalf("bấm vào tab MCP: %s", m.Screen())
	}
}

// Ở 80×24 với locale giả (chuỗi dài hơn ~40%) thanh tab hiện đủ nhãn của cả tám tab, dù tab nào đang chọn (tab chọn: nhãn đầy đủ, còn lại: nhãn rút
// gọn), và vi/en cũng vậy.
func TestMCPTabBarShowsAllEightLabelsAtEveryTab(t *testing.T) {
	for _, lang := range []string{"xx", "vi", "en"} {
		m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), 80, 24)
		for i, s := range actions.Screens {
			press(t, m, string(rune('1'+i)))
			bar := lines(m.Render())[1]
			for j, other := range actions.Screens {
				key := "tab." + string(other) + ".short"
				if i == j {
					key = "tab." + string(other)
				}
				if want := m.cat.T(key); !strings.Contains(bar, want) {
					t.Errorf("%s, tab %s đang chọn: thanh tab thiếu nhãn %q của tab %s:\n%s", lang, s, want, other, bar)
				}
			}
		}
	}
}

// Danh sách liệt kê mọi mục danh mục với đúng một trong năm trạng thái; đang chạy kèm PID, RSS và số phiên.
func TestMCPListShowsEveryServiceWithExactlyOneState(t *testing.T) {
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi"}, 140, 40)
	rows := m.mc.Rows()
	if len(rows) != 6 {
		t.Fatalf("%d dòng, muốn 6 (mọi mục danh mục)", len(rows))
	}
	seen := map[mcscreen.Kind]bool{}
	for _, r := range rows {
		seen[r.Kind] = true
	}
	for _, k := range mcscreen.Kinds {
		if !seen[k] {
			t.Errorf("danh sách mẫu thiếu trạng thái %s", k)
		}
	}
	labels := map[mcscreen.Kind]string{mcscreen.NotRegistered: "chưa đăng ký", mcscreen.Registered: "đã đăng ký", mcscreen.Running: "đang chạy",
		mcscreen.Degraded: "degraded", mcscreen.Error: "lỗi"}
	out := lines(m.Render())
	for _, r := range rows {
		var left string
		for _, l := range out {
			if i := strings.Index(l, r.Svc.ID+" "); i >= 0 && strings.Contains(l, "││") && strings.Index(l, "││") > i {
				left, _, _ = strings.Cut(l[i:], "││")
			}
		}
		if left == "" {
			t.Fatalf("không thấy dòng của %s:\n%s", r.Svc.ID, strings.Join(out, "\n"))
		}
		n := 0
		for k, lb := range labels {
			if strings.Contains(left, lb) {
				n++
				if k != r.Kind {
					t.Errorf("%s: dòng hiện %q nhưng trạng thái là %s: %q", r.Svc.ID, lb, r.Kind, left)
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: dòng phải hiện đúng một trạng thái, có %d: %q", r.Svc.ID, n, left)
		}
	}
	hasAll(t, plain(m.Render()), "PID 4301 · 180 MB · 2 phiên +1", "gateway chạy (PID 4242, 3 phiên)")
}

// Lọc dự án (phím p): chỉ giữ service có trong mcp.services; bật tắt lại được; tiêu đề ghi rõ đang lọc.
func TestMCPProjectFilter(t *testing.T) {
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi"}, 140, 40)
	press(t, m, "p")
	ids := func() []string {
		var out []string
		for _, r := range m.mc.Rows() {
			out = append(out, r.Svc.ID)
		}
		return out
	}
	if got := strings.Join(ids(), ","); got != "codegraph,specmatic,deepwiki,jira" {
		t.Fatalf("lọc dự án: %s", got)
	}
	hasAll(t, plain(m.Render()), "Dịch vụ của dự án (4/6)")
	if out := plain(m.Render()); strings.Contains(out, "context7 ") || strings.Contains(out, "chrome-devtools ") {
		t.Fatalf("service ngoài dự án không được hiện khi lọc:\n%s", out)
	}
	press(t, m, "p")
	if len(ids()) != 6 {
		t.Fatalf("tắt lọc: %v", ids())
	}
}

// Form thêm sinh từ MCPAddOptions và không có ô nhập giá trị bí mật; màn hiện đường dẫn secrets.env và biến còn thiếu của service đang chọn; gửi form
// chạy mcp.add với đúng tham số.
func TestMCPAddFormHasNoSecretFieldAndShowsSecretsFile(t *testing.T) {
	m, c := newMCPModel(t, Options{})
	press(t, m, "j", "j", "j", "j", "a") // jira: đã đăng ký, thiếu JIRA_MCP_PASSWORD
	if !m.mc.Capturing() {
		t.Fatal("phím a phải mở form")
	}
	out := plain(m.Render())
	hasAll(t, out, "Dịch vụ", "Lệnh chạy", "Đối số", "Tên biến credential", "URL", "jira", "bint mcp add jira",
		"/home/u/.config/bint/secrets.env", "JIRA_MCP_PASSWORD", "Giá trị bí mật không nhập ở đây")
	var names []string
	for _, f := range form.Fields(bintapp.MCPAddOptions{}) {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "service,command,arg,env-name,url" {
		t.Fatalf("các ô của form thêm: %v, muốn đúng service, command, arg, env-name, url", names)
	}
	for _, f := range form.AllFields(bintapp.MCPAddOptions{}) {
		for _, bad := range []string{"secret", "password", "token", "value", "key"} {
			if strings.Contains(strings.ToLower(f.Name), bad) {
				t.Errorf("ô %q trông như ô nhập giá trị bí mật", f.Name)
			}
		}
	}
	c.values["mcp.add"] = bintapp.MCPAddResult{Service: "jira", Check: bintapp.MCPCheckResult{Service: "jira", Status: bintapp.MCPStatusFail, Detail: "thiếu biến"}}
	d := newDrv(t, m)
	d.press("ctrl+s")
	d.until(func() bool { return c.count("mcp.add") == 1 && !m.mc.Capturing() })
	if o, _ := c.last(t, "mcp.add").(bintapp.MCPAddOptions); o.Service != "jira" || o.Command != "" || o.URL != "" {
		t.Fatalf("tham số mcp.add: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Đã đăng ký jira (kiểm tra: lỗi)")
	if r := m.mc.Cursor(); r == nil || r.Svc.ID != "jira" || r.Kind != mcscreen.Error {
		t.Fatalf("kết quả check của add phải vào trạng thái: %+v", r)
	}
}

// Gỡ đăng ký phải qua hộp xác nhận: chọn Không thì mcp.remove không được gọi; Có thì gọi đúng một lần.
func TestMCPRemoveNeedsConfirmation(t *testing.T) {
	m, c := newMCPModel(t, Options{})
	press(t, m, "j", "j", "j", "j", "x") // jira
	if !m.mc.Capturing() {
		t.Fatal("phím x phải mở hộp xác nhận")
	}
	hasAll(t, plain(m.Render()), "Gỡ đăng ký jira", "secrets.env không bị đụng tới", "Dự án vẫn khai báo")
	press(t, m, "esc")
	if m.mc.Capturing() || c.count("mcp.remove") != 0 {
		t.Fatalf("Không: mcp.remove được gọi %d lần, hộp còn mở=%v", c.count("mcp.remove"), m.mc.Capturing())
	}
	press(t, m, "x", "n")
	if c.count("mcp.remove") != 0 {
		t.Fatal("n là Không")
	}
	d := newDrv(t, m)
	c.values["mcp.remove"] = bintapp.MCPRemoveResult{Service: "jira", ProjectUses: true}
	d.press("x", "enter")
	d.until(func() bool { return c.count("mcp.remove") == 1 })
	if o, _ := c.last(t, "mcp.remove").(bintapp.MCPRemoveOptions); o.Service != "jira" {
		t.Fatalf("tham số: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Đã gỡ đăng ký jira")
	// Service chưa đăng ký: không mở hộp, báo lý do.
	press(t, m, "k") // context7... con trỏ đang ở jira (4); k → context7 (3), chưa đăng ký
	press(t, m, "x")
	if m.mc.Capturing() || c.count("mcp.remove") != 1 {
		t.Fatal("service chưa đăng ký: không có hộp xác nhận, không gọi")
	}
	hasAll(t, plain(m.Render()), "context7 chưa đăng ký")
}

// Kiểm tra, lập chỉ mục và khởi động lại là việc nền (Action.Job) có log trong bảng việc nền; kết quả cập nhật trạng thái. Dùng drv (không dùng drive()).
func TestMCPJobsCheckIndexRestart(t *testing.T) {
	m, c := newMCPModel(t, Options{}, "mcp.check", "mcp.index", "mcp.restart")
	d := newDrv(t, m)

	// check jira: việc nền, nhãn và log hiện trong bảng việc nền và khung chi tiết.
	d.press("j", "j", "j", "j", "c")
	d.until(func() bool { return m.jobs.Running("mcp.check") != nil && len(c.started["mcp.check"]) > 0 })
	j := m.jobs.Running("mcp.check")
	if j == nil || j.Label != "mcp check jira" || j.Kind != jobs.KindAction {
		t.Fatalf("việc nền: %+v", j)
	}
	if o, _ := c.last(t, "mcp.check").(bintapp.MCPCheckOptions); o.Service != "jira" {
		t.Fatalf("tham số check: %+v", o)
	}
	d.until(func() bool { return len(j.Log.Lines) > 0 })
	hasAll(t, plain(m.Render()), "Đang chạy: mcp check jira", "dòng log đầu của mcp.check", "1 việc nền")
	if !m.mc.WantsInterrupt() {
		t.Fatal("có việc nền thì Esc phải huỷ việc")
	}
	// Chạy lần hai khi đang chạy: không sinh việc thứ hai.
	d.press("c")
	hasAll(t, plain(m.Render()), "Việc này đang chạy")
	c.values["mcp.check"] = actions.MCPCheckOutcome{Log: []string{"jira: lỗi"}, Results: []bintapp.MCPCheckResult{{Service: "jira", Status: bintapp.MCPStatusFail, Detail: "thiếu biến JIRA_MCP_PASSWORD"}}}
	close(c.release["mcp.check"])
	d.until(func() bool { return m.jobs.Running("mcp.check") == nil })
	if r := m.mc.Cursor(); r == nil || r.Kind != mcscreen.Error || !strings.Contains(r.Detail, "JIRA_MCP_PASSWORD") {
		t.Fatalf("kết quả check phải vào trạng thái: %+v", r)
	}
	hasAll(t, plain(m.Render()), "Kết quả kiểm tra", "jira — lỗi")

	// index: chỉ codegraph; service khác báo lý do, không chạy.
	d.press("esc")
	d.press("c") // không: đang ở jira → check lại không liên quan; dùng k để về codegraph
	d.press("esc")
	d.press("g", "i") // g = về đầu (codegraph)
	d.until(func() bool { return len(c.started["mcp.index"]) > 0 })
	if o, _ := c.last(t, "mcp.index").(bintapp.MCPIndexOptions); o.Service != "codegraph" {
		t.Fatalf("tham số index: %+v", o)
	}
	c.values["mcp.index"] = actions.MCPIndexOutcome{Log: []string{"codegraph sync"}, Result: bintapp.MCPIndexResult{Service: "codegraph", Action: "sync", Cwd: "/work/demo"}}
	close(c.release["mcp.index"])
	d.until(func() bool { return m.jobs.Running("mcp.index") == nil })
	hasAll(t, plain(m.Render()), "Lập chỉ mục codegraph", "codegraph sync tại /work/demo")

	// restart: hộp xác nhận trước; Không thì không chạy; Có thì Job.
	d.press("R", "esc")
	if c.count("mcp.restart") != 0 {
		t.Fatal("Không: restart không được chạy")
	}
	c.values["mcp.restart"] = actions.MCPRestartOutcome{Result: bintapp.MCPGatewayRestartResult{Service: "codegraph", Stopped: 2}}
	d.press("R", "enter")
	d.until(func() bool { return len(c.started["mcp.restart"]) > 0 })
	close(c.release["mcp.restart"])
	d.until(func() bool { return m.jobs.Running("mcp.restart") == nil })
	hasAll(t, plain(m.Render()), "Đã dừng 2 upstream của codegraph")
}

// Esc huỷ việc nền đang chạy của màn MCP; index trên service không phải codegraph báo lý do và không chạy.
func TestMCPCancelJobAndIndexOnlyCodegraph(t *testing.T) {
	m, c := newMCPModel(t, Options{}, "mcp.check")
	d := newDrv(t, m)
	d.press("j", "i") // specmatic
	if c.count("mcp.index") != 0 {
		t.Fatal("index chỉ cho codegraph")
	}
	hasAll(t, plain(m.Render()), "Chỉ codegraph có bước lập chỉ mục")
	d.press("c")
	d.until(func() bool { return m.jobs.Running("mcp.check") != nil && len(c.started["mcp.check"]) > 0 })
	d.press("esc")
	d.until(func() bool { return m.jobs.Running("mcp.check") == nil })
	if m.mc.WantsInterrupt() {
		t.Fatal("hết việc thì không còn chờ huỷ")
	}
}

// Log: mcp.log chạy với tên service và số dòng; kết quả hiện stderr đã che, gateway.log và ghi chú; Esc đóng.
func TestMCPLogShowsUpstreamStderrAndGatewayLog(t *testing.T) {
	m, c := newMCPModel(t, Options{})
	c.values["mcp.log"] = sampleMCPLog()
	press(t, m, "j", "l")
	if o, _ := c.last(t, "mcp.log").(bintapp.MCPGatewayLogOptions); o.Service != "specmatic" || o.Lines != 50 {
		t.Fatalf("tham số log: %+v", o)
	}
	out := plain(m.Render())
	hasAll(t, out, "Log của specmatic", "stderr của upstream /work/demo (đang chạy)", "warn ***", "gateway.log (2 dòng", "exit status 1", "vượt", "bản cũ")
	press(t, m, "esc")
	if strings.Contains(plain(m.Render()), "Log của specmatic") {
		t.Fatal("Esc phải đóng khung log")
	}
}

// Thẻ Quyền: bảng role × service dựng từ MCPGrants (gồm role ngầm và cảnh báo), không có thao tác sửa; phím sửa cấu hình đi qua luồng
// system.config_edit của màn Hệ thống (form chọn lớp rồi $EDITOR).
func TestMCPGrantsTabIsReadOnlyAndEditGoesThroughSystemConfigEdit(t *testing.T) {
	m, c := newMCPModel(t, Options{})
	press(t, m, ".")
	out := plain(m.Render())
	hasAll(t, out, "Quyền role × service", "implementer", "reviewer", "interactive ~", "chỉ xem", "Sửa cấu hình")
	press(t, m, "j", "enter")
	hasAll(t, plain(m.Render()), "SERVICE", "NHÓM THAO TÁC", "codegraph  explore", "specmatic  compat", "chưa đăng ký", "chạy ở bước chỉ đọc")
	press(t, m, "esc", "j", "enter")
	hasAll(t, plain(m.Render()), "Role ngầm (D4)", "jira       read, write")
	// Không có thao tác ghi nào ở thẻ này.
	press(t, m, "a", "x", "c", "i", "R", "S", "s")
	if m.mc.Capturing() || m.jobs.Len() != 0 {
		t.Fatal("thẻ Quyền chỉ để xem")
	}
	for _, a := range []string{"mcp.add", "mcp.remove", "mcp.check", "mcp.index", "mcp.restart", "mcp.gateway_stop", "mcp.gateway_start"} {
		if c.count(a) != 0 {
			t.Errorf("%s đã chạy ở thẻ Quyền", a)
		}
	}
	// e: sang màn Hệ thống, thẻ Cấu hình, mở form chọn lớp của system.config_edit; gửi form thì chạy system.config_edit.
	press(t, m, "e")
	if m.Screen() != actions.System || m.sy.Tab() != 0 || !m.sy.Capturing() {
		t.Fatalf("e phải mở luồng sửa cấu hình của Hệ thống: màn %s, thẻ %d, form=%v", m.Screen(), m.sy.Tab(), m.sy.Capturing())
	}
	d := newDrv(t, m)
	d.press("ctrl+s")
	d.until(func() bool { return c.count("system.config_edit") == 1 })
	if o, _ := c.last(t, "system.config_edit").(actions.ConfigEditOptions); o.Layer != "project" {
		t.Fatalf("lớp mặc định phải là project: %+v", o)
	}
}

// Chế độ chỉ đọc: phím e không chuyển màn và báo lý do; thẻ Quyền không hiện phím sửa cấu hình.
func TestMCPConfigEditHiddenInReadOnly(t *testing.T) {
	m, _ := newMCPModel(t, Options{ReadOnly: true})
	press(t, m, ".", "e")
	if m.Screen() != actions.MCP || m.sy.Capturing() {
		t.Fatalf("chỉ đọc: e không được mở luồng sửa: màn %s", m.Screen())
	}
	hasAll(t, plain(m.Render()), "Chế độ chỉ đọc")
	if out := plain(m.Render()); strings.Contains(out, "Sửa cấu hình") {
		t.Fatalf("chỉ đọc: không hiện phím sửa cấu hình ở chân phím:\n%s", out)
	}
}

// Thẻ Công cụ Bint: công cụ của bint mcp serve (chuyển từ màn Hệ thống).
func TestMCPToolsTab(t *testing.T) {
	m, _ := newMCPModel(t, Options{})
	press(t, m, ".", ".")
	hasAll(t, plain(m.Render()), "Công cụ của bint mcp serve (3)", "context_for_task", "knowledge_search", "cite_knowledge", "Chi tiết · context_for_task", "Gói ngữ cảnh cho một tác vụ")
	press(t, m, "7")
	if strings.Contains(plain(m.Render()), "context_for_task") {
		t.Fatal("màn Hệ thống không còn thẻ MCP")
	}
}

// Màn Hệ thống không còn thẻ MCP và không còn hành động system.mcp_tools; cũng không văn bản nào nhắc "Hệ thống › MCP".
func TestSystemScreenHasNoMCPTab(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 200, 40)
	press(t, m, "7")
	bar := lines(m.Render())[4]
	if strings.Contains(bar, "MCP") {
		t.Fatalf("thanh thẻ con của Hệ thống còn MCP: %s", bar)
	}
	hasAll(t, bar, "Cấu hình", "Harness", "Thành phần", "Skill", "Chỉ mục", "Ngôn ngữ", "Doctor", "Cài đặt", "Cập nhật")
}

// Phiên agent: thao tác chỉ-người bị khoá (phím báo lý do, không mở hộp/form, không gọi), phím của chúng không hiện ở chân phím; thao tác đọc dùng được.
func TestMCPAgentSessionLocksHumanOnlyActions(t *testing.T) {
	reg, called, reset := countingRegistry()
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi", Agent: true, Registry: reg}, 140, 40)
	humans := []string{"mcp.add", "mcp.remove", "mcp.index", "mcp.restart", "mcp.gateway_stop"}
	for _, id := range humans {
		a, _ := reg.ByID(id)
		if !a.Writes || !a.HumanOnly {
			t.Fatalf("%s phải Writes và HumanOnly", id)
		}
	}
	press(t, m, "a", "x", "i", "R", "S")
	if m.mc.Capturing() {
		t.Fatal("phiên agent: không mở form/hộp của thao tác chỉ-người")
	}
	hasAll(t, plain(m.Render()), "chỉ dành cho người")
	out := plain(m.Render())
	for _, bad := range []string{" a Thêm", " x Gỡ", " i Chỉ mục", " R Chạy lại", " S Tắt gateway"} {
		if strings.Contains(out, bad) {
			t.Errorf("phiên agent: chân phím còn %q:\n%s", bad, out)
		}
	}
	hasAll(t, out, "c Kiểm tra", "l Log") // đọc vẫn hiện
	for _, id := range humans {
		if n := called(id); n != 0 {
			t.Errorf("%s đã chạy %d lần trong phiên agent", id, n)
		}
	}
	for _, id := range humans {
		_, cmd := m.Update(actions.RequestMsg{ID: id, Opts: nilOpts(reg, id)})
		drive(t, m, cmd)
		_, cmd = m.run(id)
		drive(t, m, cmd)
	}
	for _, id := range humans {
		if n := called(id); n != 0 {
			t.Errorf("%s đã chạy %d lần (RequestMsg/bảng lệnh) trong phiên agent", id, n)
		}
	}
	if m.jobs.Len() != 0 {
		t.Fatalf("phiên agent: có việc nền: %d", m.jobs.Len())
	}
	// Bảng lệnh liệt kê chúng ở dạng khoá kèm lý do.
	press(t, m, ":")
	for _, r := range "Đăng ký service MCP" {
		press(t, m, string(r))
	}
	hasAll(t, plain(m.Render()), "Đăng ký service MCP trên máy này", "chỉ dành cho người")
	press(t, m, "enter")
	if called("mcp.add") != 0 {
		t.Fatal("chọn hành động khoá từ bảng lệnh không được chạy")
	}
	reset()
	press(t, m, "esc")
	// Bấm chuột vào mọi phím ở chân phím và mọi hàng: không hành động chỉ-người nào chạy.
	m.Render()
	for _, z := range m.zones {
		if strings.HasPrefix(z.ID, "key:") && z.ID != "key:q" {
			_, cmd := m.Update(tea.MouseClickMsg{X: z.X0, Y: z.Y, Button: tea.MouseLeft})
			drive(t, m, cmd)
		}
	}
	for _, id := range humans {
		if n := called(id); n != 0 {
			t.Errorf("%s đã chạy %d lần qua chuột trong phiên agent", id, n)
		}
	}
}

func nilOpts(reg *actions.Registry, id string) any { a, _ := reg.ByID(id); return a.Options }

// Người dùng: mọi hành động MCP ghi chạy được qua phím (Do và cả Job); chạy đúng một lần mỗi hành động.
func TestMCPHumanSessionRunsWritesOnce(t *testing.T) {
	reg, called, _ := countingRegistry()
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi", Registry: reg}, 140, 40)
	d := newDrv(t, m)
	// add: mở form, gửi.
	d.press("a", "ctrl+s")
	d.until(func() bool { return called("mcp.add") == 1 })
	// remove: x rồi Enter (Có).
	d.press("x", "enter")
	d.until(func() bool { return called("mcp.remove") == 1 })
	// check/index/restart (Job): codegraph ở hàng đầu.
	d.press("i")
	d.until(func() bool { return called("mcp.index") == 1 })
	d.until(func() bool { return m.jobs.Len() == 0 })
	d.press("R", "enter")
	d.until(func() bool { return called("mcp.restart") == 1 })
	d.until(func() bool { return m.jobs.Len() == 0 })
	d.press("c")
	d.until(func() bool { return called("mcp.check") == 1 })
	d.until(func() bool { return m.jobs.Len() == 0 })
	// gateway: đang chạy nên s báo đã chạy; S + Enter tắt.
	d.press("s")
	if called("mcp.gateway_start") != 0 {
		t.Fatal("gateway đã chạy: s không gọi lại")
	}
	d.press("S", "enter")
	d.until(func() bool { return called("mcp.gateway_stop") == 1 })
}

// Chuột trên màn MCP: bấm thẻ phụ, hàng danh sách (bấm lại hàng đang chọn = Enter), khung chi tiết, nút ở chân phím.
func TestMCPMouse(t *testing.T) {
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi"}, 140, 40)
	// Hàng danh sách: bấm hàng specmatic chọn nó.
	x, y := cellOf(t, m, "specmatic")
	clickAt(t, m, x+1, y)
	if r := m.mc.Cursor(); r == nil || r.Svc.ID != "specmatic" {
		t.Fatalf("bấm hàng: %+v", r)
	}
	hasAll(t, plain(m.Render()), "Chi tiết · specmatic")
	// Thẻ phụ.
	x, y = cellOf(t, m, "Quyền")
	clickAt(t, m, x+1, y)
	if m.mc.Tab() != mcscreen.TabGrants {
		t.Fatalf("bấm thẻ Quyền: %d", m.mc.Tab())
	}
	x, y = cellOf(t, m, "Công cụ Bint")
	clickAt(t, m, x+1, y)
	if m.mc.Tab() != mcscreen.TabTools {
		t.Fatalf("bấm thẻ Công cụ Bint: %d", m.mc.Tab())
	}
	x, y = cellOf(t, m, "Dịch vụ")
	clickAt(t, m, x+1, y)
	// Phím ở chân phím: bấm "p Lọc dự án" bật lọc.
	x, y = cellOf(t, m, "Lọc dự án")
	clickAt(t, m, x+1, y)
	if len(m.mc.Rows()) != 4 {
		t.Fatalf("bấm phím p ở chân phím: %d dòng", len(m.mc.Rows()))
	}
	// Bánh xe cuộn khung chi tiết.
	m.Update(tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown})
}

// Chế độ chỉ đọc: mọi hành động MCP ghi (Do và cả Job) không chạy qua RequestMsg, bảng lệnh, phím tắt ở cả ba thẻ phụ và chuột; không có việc nền; phím
// của chúng không hiện ở chân phím; thao tác đọc vẫn dùng được (kiểm tra là Job đọc).
func TestMCPReadOnlyNeverRunsWritingActions(t *testing.T) {
	reg, called, reset := countingRegistry()
	var writes, reads []actions.Action
	jobsN := 0
	for _, a := range reg.All() {
		if a.Screen != actions.MCP {
			continue
		}
		if a.Writes {
			writes = append(writes, a)
			if a.Job != nil {
				jobsN++
			}
		} else if a.Do != nil || a.Job != nil {
			reads = append(reads, a)
		}
	}
	if len(writes) != 7 || jobsN != 2 || len(reads) != 6 {
		t.Fatalf("hành động MCP: %d ghi (%d Job), %d đọc", len(writes), jobsN, len(reads))
	}
	newRO := func() *Model {
		return mcpAt(t, Options{Mode: theme.Mono, Lang: "vi", ReadOnly: true, Registry: reg}, 140, 40)
	}
	assertNone := func(path string) {
		t.Helper()
		for _, a := range writes {
			if n := called(a.ID); n != 0 {
				t.Errorf("%s: %s đã chạy %d lần ở chế độ chỉ đọc", path, a.ID, n)
				reset()
			}
		}
	}
	m := newRO()
	for _, a := range writes {
		guard(t, "RequestMsg "+a.ID, func() {
			_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
			drive(t, m, cmd)
			_, cmd = m.run(a.ID)
			drive(t, m, cmd)
		})
		if m.jobs.Len() != 0 {
			t.Errorf("%s bắt đầu việc nền ở chế độ chỉ đọc", a.ID)
		}
	}
	assertNone("RequestMsg/bảng lệnh")
	inPalette := map[string]bool{}
	for _, it := range m.paletteItems() {
		inPalette[it.ID] = true
	}
	for _, a := range writes {
		if inPalette[a.ID] {
			t.Errorf("%s có trong bảng lệnh ở chế độ chỉ đọc", a.ID)
		}
	}
	for _, a := range reads {
		if a.Screen == actions.MCP && !inPalette[a.ID] && a.TitleKey != "" {
			t.Errorf("%s (đọc) phải có trong bảng lệnh ở chế độ chỉ đọc", a.ID)
		}
	}
	// Phím tắt ở cả ba thẻ phụ, trên các hàng khác nhau (hộp và form không mở được).
	for _, tab := range []int{0, 1, 2} {
		for _, row := range []int{0, 1, 4} {
			m := newRO()
			keys := []string{}
			for i := 0; i < tab; i++ {
				keys = append(keys, ".")
			}
			for i := 0; i < row; i++ {
				keys = append(keys, "j")
			}
			press(t, m, keys...)
			for _, a := range writes {
				if a.Key == "" || len(a.Key) != 1 {
					continue
				}
				press(t, m, a.Key, "enter", "esc")
				if m.mc.Capturing() || m.sy.Capturing() || m.Screen() != actions.MCP {
					t.Fatalf("chỉ đọc: phím %s (%s) mở hộp, form hoặc đổi màn", a.Key, a.ID)
				}
			}
		}
	}
	assertNone("phím tắt")
	// Chuột: bấm mọi vùng phím ở chân phím và mọi hàng.
	for _, tab := range []int{0, 1, 2} {
		m := newRO()
		for i := 0; i < tab; i++ {
			press(t, m, ".")
		}
		m.Render()
		var zs []string
		for _, z := range m.zones {
			if strings.HasPrefix(z.ID, "key:") && z.ID != "key:q" {
				zs = append(zs, z.ID)
			}
		}
		for _, id := range zs {
			for _, z := range m.zones {
				if z.ID == id {
					_, cmd := m.Update(tea.MouseClickMsg{X: z.X0, Y: z.Y, Button: tea.MouseLeft})
					drive(t, m, cmd)
					break
				}
			}
			press(t, m, "esc")
		}
	}
	assertNone("chuột")
	// Không phím ghi nào ở chân phím; phím đọc (kiểm tra, log, lọc) còn.
	out := plain(newRO().Render())
	for _, bad := range []string{" a Thêm", " x Gỡ", " i Chỉ mục", " R Chạy lại", " G Bật gateway", " S Tắt gateway"} {
		if strings.Contains(out, bad) {
			t.Errorf("chỉ đọc: chân phím còn %q", bad)
		}
	}
	hasAll(t, out, "c Kiểm tra", "l Log", "p Lọc dự án")
}

// Phát hiện M4 .5: ở thẻ Dịch vụ, G về cuối danh sách (như mọi thẻ), không khởi động gateway.
func TestMCPServicesGGoesToEnd(t *testing.T) {
	m := mcpAt(t, Options{Mode: theme.Mono, Lang: "vi"}, 140, 40)
	press(t, m, "G")
	if !strings.Contains(plain(m.Render()), "▸ chrome-devtools") {
		t.Fatalf("G phải chọn hàng cuối:\n%s", plain(m.Render()))
	}
	if m.jobs.Len() != 0 {
		t.Fatal("G không được chạy hành động nào")
	}
}
