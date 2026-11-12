// Package mcp là màn MCP (docs/mcp-gateway-plan.md mục 8, M4; docs/tui-plan.md mục 5.8): danh mục MCP của máy kèm trạng thái đăng ký và
// tiến trình đang chạy ở daemon gateway, bảng quyền role × service (chỉ xem) và công cụ MCP của Bint. Mọi thao tác đọc gọi tầng dịch vụ
// (internal/app); thao tác ghi đi qua actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá theo sổ hành động (màn hình không tự kiểm).
package mcp

import (
	"context"
	"errors"
	"maps"
	"slices"

	"bint/internal/app"
	"bint/internal/gitrepo"
	"bint/internal/mcpcat"
	"bint/internal/mcpgw"
	"bint/internal/tui/actions"
)

// Các thẻ phụ của màn hình.
const (
	TabServices = iota
	TabGrants
	TabTools
	numTabs
)

// TabNames là tên (khoá i18n mc.tab.<tên>) của các thẻ phụ theo thứ tự.
var TabNames = [numTabs]string{"services", "grants", "tools"}

// Kind là trạng thái hiển thị của một service (đúng một trạng thái cho mỗi mục danh mục).
type Kind string

const (
	NotRegistered Kind = "not_registered"
	Registered    Kind = "registered"
	Running       Kind = "running"
	Degraded      Kind = "degraded"
	Error         Kind = "error"
)

// Kinds là các trạng thái theo thứ tự hiển thị.
var Kinds = []Kind{NotRegistered, Registered, Running, Degraded, Error}

// Row là một dòng của danh sách dịch vụ: mục danh mục kèm trạng thái đã suy ra.
type Row struct {
	Svc  app.MCPServiceStatus
	Kind Kind
	// Detail là lý do (lỗi, degraded: từ kết quả check hoặc từ gateway); rỗng nếu không có.
	Detail string
	// Checked: có kết quả check trong phiên này.
	Checked bool
	// Up là phân vùng đang chạy được chọn (nil nếu không chạy); Extra là số phân vùng đang chạy khác.
	Up    *mcpgw.UpstreamStatus
	Extra int
	// Parts là mọi phân vùng của service ở gateway (đang chạy hoặc đã chết).
	Parts []mcpgw.UpstreamStatus
}

// State suy trạng thái hiển thị của một service từ trạng thái đăng ký (st), ảnh chụp gateway (gw; nil = gateway không chạy), kết quả check
// trong phiên (chk; nil = chưa check) và gốc worktree hiện tại. Thứ tự ưu tiên (docs/mcp-gateway-plan.md mục 8):
//
//  1. chưa đăng ký;
//  2. check fail, hoặc phân vùng bị giới hạn khởi động lại (Limited), hoặc phân vùng không chạy mà còn LastError (chết chưa
//     hồi phục) khi không phân vùng nào của service đang chạy → lỗi;
//  3. check degraded → degraded;
//  4. có phân vùng đang chạy → đang chạy (ưu tiên phân vùng của worktree hiện tại; không có thì phân vùng đầu);
//  5. còn lại → đã đăng ký.
func State(st app.MCPServiceStatus, gw *mcpgw.Status, chk *app.MCPCheckResult, worktree string) Row {
	r := Row{Svc: st, Checked: chk != nil}
	if gw != nil {
		for _, u := range gw.Upstreams {
			if u.Service == st.ID {
				r.Parts = append(r.Parts, u)
			}
		}
	}
	if !st.Registered {
		r.Kind = NotRegistered
		return r
	}
	switch {
	case chk != nil && chk.Status == app.MCPStatusFail:
		r.Kind, r.Detail = Error, chk.Detail
		return r
	}
	anyRunning := false
	for _, u := range r.Parts {
		anyRunning = anyRunning || u.Running
	}
	for _, u := range r.Parts {
		switch {
		case u.Limited:
			r.Kind, r.Detail = Error, u.LastError
			return r
		case !u.Running && u.LastError != "" && !anyRunning:
			// Gateway xoá LastError khi khởi động lại thành công, nên lỗi còn lại ở phân vùng không chạy là lần chết chưa hồi
			// phục; phân vùng khác (worktree khác) đang chạy thì service vẫn dùng được, lỗi chỉ hiện ở chi tiết phân vùng.
			r.Kind, r.Detail = Error, u.LastError
			return r
		}
	}
	if chk != nil && chk.Status == app.MCPStatusDegraded {
		r.Kind, r.Detail = Degraded, chk.Detail
		return r
	}
	var running []int
	for i, u := range r.Parts {
		if u.Running {
			running = append(running, i)
		}
	}
	if len(running) == 0 {
		r.Kind = Registered
		return r
	}
	pick := running[0]
	for _, i := range running {
		if worktree != "" && r.Parts[i].Partition == worktree {
			pick = i
			break
		}
	}
	u := r.Parts[pick]
	r.Kind, r.Up, r.Extra = Running, &u, len(running)-1
	return r
}

// Rows suy trạng thái của mọi service trong d, theo thứ tự danh mục; checks là kết quả check trong phiên theo ID service.
func Rows(d Data, checks map[string]app.MCPCheckResult) []Row {
	out := make([]Row, 0, len(d.Services))
	for _, s := range d.Services {
		var chk *app.MCPCheckResult
		if c, ok := checks[s.ID]; ok {
			chk = &c
		}
		out = append(out, State(s, d.Gateway, chk, d.Root))
	}
	return out
}

// Visible trả chỉ số các dòng hiện ra: projectOnly = chỉ service mà dự án khai báo trong mcp.services.
func Visible(rows []Row, projectOnly bool) []int {
	var idx []int
	for i, r := range rows {
		if !projectOnly || r.Svc.ProjectUses {
			idx = append(idx, i)
		}
	}
	return idx
}

// Data là dữ liệu của thẻ Dịch vụ.
type Data struct {
	Services []app.MCPServiceStatus
	// Gateway là ảnh chụp daemon (nil = không chạy hoặc không đọc được, xem GatewayErr).
	Gateway    *mcpgw.Status
	GatewayErr string
	// SecretsFile là đường dẫn secrets.env của máy (giá trị không bao giờ được đọc vào màn hình).
	SecretsFile string
	// Root là gốc worktree hiện tại ("" nếu không phải repository git).
	Root string
}

// GrantCell là ô của bảng quyền: các nhóm thao tác của một service mà role được cấp.
type GrantCell struct {
	Service string
	Groups  []string
}

// GrantCells gom các tool đã giải của một role thành ô theo service (thứ tự danh mục, nhóm theo thứ tự xuất hiện, không lặp).
func GrantCells(g app.MCPRoleGrants) []GrantCell {
	by := map[string][]string{}
	for _, t := range g.Tools {
		if !slices.Contains(by[t.Service], t.Group) {
			by[t.Service] = append(by[t.Service], t.Group)
		}
	}
	var out []GrantCell
	for _, id := range mcpcat.IDs() {
		if gs, ok := by[id]; ok {
			out = append(out, GrantCell{Service: id, Groups: gs})
			delete(by, id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(by)) { // service ngoài danh mục (không xảy ra với cấu hình hợp lệ): vẫn hiện
		out = append(out, GrantCell{Service: id, Groups: by[id]})
	}
	return out
}

// GrantsData là dữ liệu của thẻ Quyền.
type GrantsData struct {
	Roles []app.MCPRoleGrants
	// NotInit: repository chưa init nên không có cấu hình dự án để giải quyền.
	NotInit bool
}

// Loaders gom các hàm nạp dữ liệu của từng thẻ (thay được trong kiểm thử; nil = nạp thật từ dịch vụ).
type Loaders struct {
	Services func(ctx context.Context, dir string) (Data, error)
	Grants   func(ctx context.Context, dir string) (GrantsData, error)
	Tools    func(ctx context.Context, dir, version string) ([]app.MCPTool, error)
}

func (l Loaders) withDefaults() Loaders {
	if l.Services == nil {
		l.Services = loadServices
	}
	if l.Grants == nil {
		l.Grants = loadGrants
	}
	if l.Tools == nil {
		l.Tools = func(_ context.Context, _, version string) ([]app.MCPTool, error) {
			return app.MCPTools(version, app.MCPToolsOptions{})
		}
	}
	return l
}

// loadServices đọc danh mục và trạng thái đăng ký, hỏi gateway nếu đang chạy (không bao giờ khởi động daemon).
func loadServices(ctx context.Context, dir string) (Data, error) {
	list, err := app.MCPList(ctx, dir, app.MCPListOptions{})
	if err != nil {
		return Data{}, err
	}
	d := Data{Services: list}
	if d.SecretsFile, err = app.MCPSecretsFile(); err != nil {
		return Data{}, err
	}
	if repo, err := gitrepo.Discover(ctx, dir); err == nil {
		d.Root = repo.Toplevel
	}
	gw, err := app.MCPGatewayStatus(ctx, app.MCPGatewayStatusOptions{})
	switch {
	case err != nil:
		d.GatewayErr = err.Error()
	case gw.Running:
		d.Gateway = gw.Status
	}
	return d, nil
}

// loadGrants giải quyền role × service của dự án; repository chưa init không phải lỗi (hiện thông báo chưa init).
func loadGrants(ctx context.Context, dir string) (GrantsData, error) {
	roles, err := app.MCPGrants(ctx, dir, app.MCPGrantsOptions{})
	if errors.Is(err, app.ErrNotInitialized) {
		return GrantsData{NotInit: true}, nil
	}
	return GrantsData{Roles: roles}, err
}

// LoadedMsg là kết quả nạp dữ liệu của một thẻ (Value có kiểu theo thẻ: Data, GrantsData, []app.MCPTool).
type LoadedMsg struct {
	Tab   int
	Value any
	Err   error
}

// Target cho mô hình gốc biết thông điệp này thuộc màn MCP.
func (LoadedMsg) Target() actions.Screen { return actions.MCP }
