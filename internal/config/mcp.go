package config

import (
	"fmt"
	"sort"
	"strings"

	"bint/internal/mcpcat"
)

// MCP là khối `mcp:` của cấu hình dự án (docs/mcp-gateway-plan.md mục 5.2): service dự án dùng và quyền theo role.
// Việc service đã đăng ký trên máy hay chưa không thuộc Validate (cấu hình dùng chung cả nhóm): `bint doctor` báo.
type MCP struct {
	// Services: id (thuộc danh mục mcpcat) → khai báo của dự án.
	Services map[string]MCPService `yaml:"services"`
	// Grants: tên role tự do → phần tử "<service>.<nhóm>" hoặc "<service>.*". Mặc định không cấp gì.
	Grants map[string][]string `yaml:"grants"`
}

// MCPService là khai báo một service trong dự án.
type MCPService struct {
	// Required: nil = bắt buộc (true). Tuỳ chọn thì doctor chỉ báo mức thông tin khi máy chưa đăng ký.
	Required *bool `yaml:"required,omitempty"`
	// Options: tuỳ chọn riêng của dự án; tên phải có trong mcpcat.Service.Options.
	Options map[string]string `yaml:"options,omitempty"`
}

// IsRequired cho biết dự án bắt buộc service này (mặc định có).
func (s MCPService) IsRequired() bool { return s.Required == nil || *s.Required }

// MCPRoleInteractive là role của phiên tương tác ngoài `bint run` (mục 3.2, D4).
const MCPRoleInteractive = "interactive"

// validateMCP kiểm cấu trúc khối mcp; trả các vấn đề (chưa sắp xếp).
func validateMCP(m MCP) []string {
	var errs []string
	for _, id := range sortedKeys(m.Services) {
		svc, ok := mcpcat.Lookup(id)
		if !ok {
			errs = append(errs, fmt.Sprintf("mcp.services: service %q không có trong danh mục (%s)", id, strings.Join(mcpcat.IDs(), ", ")))
			continue
		}
		for _, opt := range sortedKeys(m.Services[id].Options) {
			if _, ok := svc.Options[opt]; !ok {
				errs = append(errs, fmt.Sprintf("mcp.services.%s.options: tuỳ chọn %q không có trong danh mục (có: %s)", id, opt, optionList(svc)))
			}
		}
	}
	for _, role := range sortedKeys(m.Grants) {
		if strings.TrimSpace(role) == "" || strings.ContainsAny(role, " \t\r\n") {
			errs = append(errs, fmt.Sprintf("mcp.grants: tên role %q không hợp lệ (không rỗng, không chứa khoảng trắng)", role))
		}
		for _, g := range m.Grants[role] {
			if _, _, err := mcpcat.ParseGrant(g); err != nil {
				errs = append(errs, fmt.Sprintf("mcp.grants.%s: %v", role, err))
			}
		}
	}
	return errs
}

func optionList(s mcpcat.Service) string {
	if len(s.Options) == 0 {
		return "không có tuỳ chọn nào"
	}
	return strings.Join(s.OptionNames(), ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GrantRoles trả tên các role có khai báo quyền, đã sắp xếp.
func (m MCP) GrantRoles() []string { return sortedKeys(m.Grants) }

// ServiceIDs trả id các service dự án khai báo, đã sắp xếp.
func (m MCP) ServiceIDs() []string { return sortedKeys(m.Services) }

// Uses cho biết dự án khai báo service id.
func (m MCP) Uses(id string) bool {
	_, ok := m.Services[id]
	return ok
}

// Lý do một tool được cấp trong mcp.grants nhưng không dùng được (MCPExclusion.Reasons).
const (
	// MCPExcludeNotInProject: service không có trong mcp.services của dự án.
	MCPExcludeNotInProject = "not_in_project"
	// MCPExcludeNotRegistered: máy này chưa đăng ký service.
	MCPExcludeNotRegistered = "not_registered"
	// MCPExcludeReadOnlyStep: tool ghi vào cây làm việc, cấm ở bước chỉ đọc (b-req, b-review).
	MCPExcludeReadOnlyStep = "read_only_step"
)

// MCPExclusion là tool được cấp nhưng bị loại, kèm mọi lý do.
type MCPExclusion struct {
	Tool    mcpcat.ToolRef
	Reasons []string
}

// MCPResolution là kết quả giải quyền của một role.
type MCPResolution struct {
	// Granted là tool dùng được (đã bỏ trùng theo service + tên upstream), theo thứ tự khai báo.
	Granted []mcpcat.ToolRef
	// Excluded là tool được cấp nhưng bị loại.
	Excluded []MCPExclusion
	// Implicit: role interactive không khai báo trong mcp.grants, dựng theo D4.
	Implicit bool
}

// Services trả id các service còn tool được cấp, theo thứ tự xuất hiện.
func (r MCPResolution) Services() []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range r.Granted {
		if !seen[t.Service] {
			seen[t.Service] = true
			out = append(out, t.Service)
		}
	}
	return out
}

// ToolsOf trả tool được cấp của một service.
func (r MCPResolution) ToolsOf(service string) []mcpcat.ToolRef {
	var out []mcpcat.ToolRef
	for _, t := range r.Granted {
		if t.Service == service {
			out = append(out, t)
		}
	}
	return out
}

// Resolve giải quyền của role thành tool dùng được. Daemon gateway, `bint mcp grants` và `bint run` (allowlist của
// bước) cùng gọi hàm này nên không lệch nhau (docs/mcp-gateway-plan.md mục 7).
//
//   - Quyền lấy từ mcp.grants[role]. Role interactive không khai báo (D4) được `<service>.*` của mọi service trong
//     mcp.services; role khác không khai báo thì không có gì (đóng an toàn).
//   - Chỉ tool của service có trong mcp.services VÀ đã đăng ký trên máy (registered: tập id; nil = chưa đăng ký gì).
//   - readOnly (b-req, b-review): bỏ tool WritesWorktree của danh mục dù grants có ghi.
//
// Phần tử quyền sai cấu trúc bị bỏ qua (Validate đã báo khi nạp cấu hình).
func (m MCP) Resolve(role string, readOnly bool, registered map[string]bool) MCPResolution {
	var res MCPResolution
	grants, declared := m.Grants[role]
	if role == MCPRoleInteractive && !declared {
		res.Implicit = true
		grants = nil
		for _, id := range m.ServiceIDs() {
			grants = append(grants, id+".*")
		}
	}
	seen := map[string]bool{}
	for _, g := range grants {
		refs, err := mcpcat.Expand(g)
		if err != nil {
			continue
		}
		for _, ref := range refs {
			key := ref.Service + "\x00" + ref.Upstream
			if seen[key] {
				continue
			}
			seen[key] = true
			var why []string
			if !m.Uses(ref.Service) {
				why = append(why, MCPExcludeNotInProject)
			}
			if !registered[ref.Service] {
				why = append(why, MCPExcludeNotRegistered)
			}
			if svc, ok := mcpcat.Lookup(ref.Service); ok && readOnly && svc.WritesToWorktree(ref.Upstream) {
				why = append(why, MCPExcludeReadOnlyStep)
			}
			if len(why) == 0 {
				res.Granted = append(res.Granted, ref)
			} else {
				res.Excluded = append(res.Excluded, MCPExclusion{Tool: ref, Reasons: why})
			}
		}
	}
	return res
}
