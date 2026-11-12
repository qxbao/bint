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
