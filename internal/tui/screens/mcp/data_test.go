package mcp

import (
	"slices"
	"testing"

	"bint/internal/app"
	"bint/internal/mcpgw"
)

func up(svc, part string, running bool, mod func(*mcpgw.UpstreamStatus)) mcpgw.UpstreamStatus {
	u := mcpgw.UpstreamStatus{Service: svc, Partition: part, Running: running}
	if running {
		u.PID, u.RSSKB, u.Sessions = 100, 2048, 1
	}
	if mod != nil {
		mod(&u)
	}
	return u
}

// State: đúng một trạng thái, theo thứ tự ưu tiên của docs/mcp-gateway-plan.md mục 8.
func TestStateTable(t *testing.T) {
	reg := app.MCPServiceStatus{ID: "codegraph", Registered: true}
	chk := func(status, detail string) *app.MCPCheckResult {
		return &app.MCPCheckResult{Service: "codegraph", Status: status, Detail: detail}
	}
	gw := func(us ...mcpgw.UpstreamStatus) *mcpgw.Status { return &mcpgw.Status{Upstreams: us} }
	cases := []struct {
		name    string
		st      app.MCPServiceStatus
		gw      *mcpgw.Status
		chk     *app.MCPCheckResult
		root    string
		want    Kind
		detail  string
		pid     int // PID của phân vùng được chọn (0 = không chạy)
		extra   int
		checked bool
	}{
		{name: "chưa đăng ký", st: app.MCPServiceStatus{ID: "codegraph"}, want: NotRegistered},
		{name: "chưa đăng ký dù gateway còn phân vùng cũ và check fail", st: app.MCPServiceStatus{ID: "codegraph"},
			gw: gw(up("codegraph", "/w", true, nil)), chk: chk("fail", "x"), want: NotRegistered, checked: true},
		{name: "đã đăng ký, không gateway", st: reg, want: Registered},
		{name: "đã đăng ký, gateway không có phân vùng của service", st: reg, gw: gw(up("jira", "machine", true, nil)), want: Registered},
		{name: "đã đăng ký, check ok, không chạy", st: reg, chk: chk("ok", ""), want: Registered, checked: true},
		{name: "đang chạy", st: reg, gw: gw(up("codegraph", "/w", true, func(u *mcpgw.UpstreamStatus) { u.PID = 7 })), want: Running, pid: 7},
		{name: "đang chạy: ưu tiên phân vùng của worktree hiện tại, +N", st: reg, root: "/w2",
			gw: gw(up("codegraph", "/w1", true, func(u *mcpgw.UpstreamStatus) { u.PID = 1 }), up("codegraph", "/w2", true, func(u *mcpgw.UpstreamStatus) { u.PID = 2 }),
				up("codegraph", "/w3", false, nil)), want: Running, pid: 2, extra: 1},
		{name: "đang chạy: không có phân vùng của worktree thì lấy phân vùng đầu", st: reg, root: "/khac",
			gw: gw(up("codegraph", "/w1", true, func(u *mcpgw.UpstreamStatus) { u.PID = 1 }), up("codegraph", "/w2", true, func(u *mcpgw.UpstreamStatus) { u.PID = 2 })), want: Running, pid: 1, extra: 1},
		{name: "đang chạy: worktree rỗng (ngoài repo) thì lấy phân vùng đầu", st: reg,
			gw: gw(up("codegraph", "/w1", true, func(u *mcpgw.UpstreamStatus) { u.PID = 1 })), want: Running, pid: 1},
		{name: "degraded từ check", st: reg, chk: chk("degraded", "thiếu tool"), want: Degraded, detail: "thiếu tool", checked: true},
		{name: "degraded thắng đang chạy", st: reg, chk: chk("degraded", "thiếu tool"), gw: gw(up("codegraph", "/w", true, nil)), want: Degraded, detail: "thiếu tool", checked: true},
		{name: "lỗi từ check fail", st: reg, chk: chk("fail", "không có lệnh"), want: Error, detail: "không có lệnh", checked: true},
		{name: "check fail thắng check degraded/đang chạy", st: reg, chk: chk("fail", "hỏng"), gw: gw(up("codegraph", "/w", true, nil)), want: Error, detail: "hỏng", checked: true},
		{name: "lỗi: phân vùng bị giới hạn restart", st: reg,
			gw: gw(up("codegraph", "/w", false, func(u *mcpgw.UpstreamStatus) { u.Limited, u.LastError = true, "vượt giới hạn" })), want: Error, detail: "vượt giới hạn"},
		{name: "lỗi: giới hạn restart thắng cả khi phân vùng khác đang chạy", st: reg,
			gw: gw(up("codegraph", "/a", true, nil), up("codegraph", "/b", false, func(u *mcpgw.UpstreamStatus) { u.Limited = true })), want: Error},
		{name: "lỗi: phân vùng không chạy mà còn LastError", st: reg,
			gw: gw(up("codegraph", "/w", false, func(u *mcpgw.UpstreamStatus) { u.LastError = "chết" })), want: Error, detail: "chết"},
		{name: "lỗi gateway thắng degraded", st: reg, chk: chk("degraded", "d"),
			gw: gw(up("codegraph", "/w", false, func(u *mcpgw.UpstreamStatus) { u.LastError = "chết" })), want: Error, detail: "chết", checked: true},
		{name: "phân vùng đang chạy còn LastError cũ không phải lỗi", st: reg,
			gw: gw(up("codegraph", "/w", true, func(u *mcpgw.UpstreamStatus) { u.LastError = "đã hồi phục" })), want: Running, pid: 100},
		{name: "phân vùng chết ở worktree khác không che phân vùng đang chạy", st: reg, root: "/a",
			gw: gw(up("codegraph", "/a", true, func(u *mcpgw.UpstreamStatus) { u.PID = 5 }), up("codegraph", "/b", false, func(u *mcpgw.UpstreamStatus) { u.LastError = "chết" })), want: Running, pid: 5},
		{name: "phân vùng đã dừng không lỗi vẫn là đã đăng ký", st: reg, gw: gw(up("codegraph", "/w", false, nil)), want: Registered},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := State(c.st, c.gw, c.chk, c.root)
			if r.Kind != c.want || r.Detail != c.detail || r.Checked != c.checked || r.Extra != c.extra {
				t.Fatalf("State = {%s %q checked=%v extra=%d}, muốn {%s %q checked=%v extra=%d}", r.Kind, r.Detail, r.Checked, r.Extra, c.want, c.detail, c.checked, c.extra)
			}
			pid := 0
			if r.Up != nil {
				pid = r.Up.PID
			}
			if pid != c.pid {
				t.Fatalf("PID phân vùng = %d, muốn %d", pid, c.pid)
			}
			if (r.Kind == Running) != (r.Up != nil) {
				t.Fatalf("Up và trạng thái đang chạy phải đi cùng nhau: %v %v", r.Kind, r.Up)
			}
		})
	}
}

// Rows: mỗi mục danh mục đúng một dòng với đúng một trạng thái; kết quả check chỉ áp cho service của nó; Visible lọc theo dự án.
func TestRowsAndVisible(t *testing.T) {
	d := Data{Root: "/w", Services: []app.MCPServiceStatus{
		{ID: "a", Registered: true, ProjectUses: true}, {ID: "b"}, {ID: "c", Registered: true}, {ID: "d", ProjectUses: true},
	}, Gateway: &mcpgw.Status{Upstreams: []mcpgw.UpstreamStatus{up("a", "/w", true, nil)}}}
	rows := Rows(d, map[string]app.MCPCheckResult{"c": {Service: "c", Status: app.MCPStatusFail, Detail: "x"}})
	var got []Kind
	for _, r := range rows {
		got = append(got, r.Kind)
	}
	if want := []Kind{Running, NotRegistered, Error, NotRegistered}; !slices.Equal(got, want) {
		t.Fatalf("trạng thái = %v, muốn %v", got, want)
	}
	if v := Visible(rows, false); !slices.Equal(v, []int{0, 1, 2, 3}) {
		t.Errorf("không lọc: %v", v)
	}
	if v := Visible(rows, true); !slices.Equal(v, []int{0, 3}) {
		t.Errorf("lọc dự án: %v, muốn [0 3] (chỉ service có trong mcp.services)", v)
	}
	if v := Visible(nil, true); len(v) != 0 {
		t.Errorf("rỗng: %v", v)
	}
	// Mọi Kind đều có trong Kinds (nhãn dịch theo mc.state.<kind>).
	for _, k := range []Kind{NotRegistered, Registered, Running, Degraded, Error} {
		if !slices.Contains(Kinds, k) {
			t.Errorf("Kinds thiếu %s", k)
		}
	}
}

// GrantCells: gom theo service (thứ tự danh mục), nhóm không lặp, giữ thứ tự xuất hiện.
func TestGrantCells(t *testing.T) {
	g := app.MCPRoleGrants{Tools: []app.MCPGrantTool{
		{Service: "jira", Group: "read"}, {Service: "codegraph", Group: "explore"}, {Service: "codegraph", Group: "query"}, {Service: "codegraph", Group: "query"},
		{Service: "lạ", Group: "x"},
	}}
	got := GrantCells(g)
	if len(got) != 3 || got[0].Service != "codegraph" || !slices.Equal(got[0].Groups, []string{"explore", "query"}) ||
		got[1].Service != "jira" || got[2].Service != "lạ" {
		t.Fatalf("ô quyền: %+v", got)
	}
	if len(GrantCells(app.MCPRoleGrants{})) != 0 {
		t.Error("role không có tool thì không có ô")
	}
}
