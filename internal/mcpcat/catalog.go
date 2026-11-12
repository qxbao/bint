// Package mcpcat là danh mục MCP nhúng trong binary (docs/mcp-gateway-plan.md mục 5.1): service nào Bint
// biết, tiền tố tool tới agent, nhóm thao tác, chính sách chia sẻ, env mặc định và biến credential.
//
// Gói chỉ chứa dữ liệu và hàm thuần, không import gói nội bộ nào để `config` import được khi kiểm cấu hình.
package mcpcat

import (
	"fmt"
	"sort"
	"strings"
)

// Transport là kiểu kết nối tới upstream.
type Transport string

const (
	Stdio Transport = "stdio"
	HTTP  Transport = "http"
)

// Share là chính sách chia sẻ tiến trình (mục 5.3).
type Share string

const (
	ShareMachine  Share = "machine"
	ShareWorktree Share = "worktree"
	ShareSession  Share = "session"
	ShareRemote   Share = "remote"
)

// Cwd là thư mục chạy của tiến trình upstream.
type Cwd string

const (
	// CwdNone: không đặt (service từ xa, hoặc không phụ thuộc cwd).
	CwdNone Cwd = ""
	// CwdWorktree: gốc worktree hiện tại.
	CwdWorktree Cwd = "worktree"
	// CwdState: thư mục riêng <state>/mcp/<service>/<worktree-id>.
	CwdState Cwd = "state"
)

// WorktreePlaceholder là chỗ giữ của gốc worktree trong DefaultArgs.
const WorktreePlaceholder = "{worktree}"

// AgentPrefix là tiền tố tên tool mà harness thấy với MCP server `bint` (mcp__bint__<tool>).
const AgentPrefix = "mcp__bint__"

// MaxToolNameLen là giới hạn độ dài tên tool đầy đủ của harness.
const MaxToolNameLen = 64

// Group là một nhóm thao tác: quyền trong cấu hình trỏ tới nhóm, không trỏ tên tool thô.
type Group struct {
	Name string
	// Tools là tên tool của upstream.
	Tools []string
	// Exclusive: tại một thời điểm chỉ một phiên được dùng nhóm (khoá trong daemon, M2).
	Exclusive bool
	// MaxConcurrent là số lời gọi đồng thời tối đa của nhóm trong một phân vùng tiến trình (0 = không giới hạn).
	MaxConcurrent int
}

// Service là một mục của danh mục.
type Service struct {
	ID        string
	Prefix    string
	Transport Transport
	// Install là lệnh cài gợi ý (phiên bản ghim); Bint không tự chạy.
	Install string
	// Run là lệnh chạy gợi ý (stdio): phần tử đầu là lệnh, còn lại là đối số.
	Run []string
	// URL là địa chỉ mặc định (http).
	URL     string
	Version string
	Share   Share
	Cwd     Cwd
	Groups  []Group
	// ReadOnly: tool chỉ đọc (được retry). WritesWorktree: tool ghi vào cây làm việc (cấm ở bước chỉ đọc).
	ReadOnly       []string
	WritesWorktree []string
	// DefaultEnv là env gateway luôn đặt cho upstream.
	DefaultEnv map[string]string
	// DefaultArgs: tool → tham số → giá trị điền khi agent không truyền ({worktree} = gốc worktree).
	DefaultArgs map[string]map[string]string
	// CredentialEnv là tên biến môi trường credential cần có (giá trị nằm trong secrets.env).
	CredentialEnv []string
	// Options là tên tuỳ chọn riêng của dự án (mcp.services.<id>.options) kèm mô tả.
	Options map[string]string
}

// codegraphTools là đủ 8 tool của codegraph 1.6.2 (giá trị CODEGRAPH_MCP_TOOLS).
var codegraphTools = []string{
	"codegraph_explore", "codegraph_node", "codegraph_search", "codegraph_callers",
	"codegraph_callees", "codegraph_impact", "codegraph_files", "codegraph_status",
}

var catalog = []Service{
	{
		ID: "codegraph", Prefix: "cg", Transport: Stdio, Version: "1.6.2",
		Install: "npm i -g @colbymchenry/codegraph@1.6.2",
		Run:     []string{"codegraph", "serve", "--mcp"},
		Share:   ShareWorktree, Cwd: CwdWorktree,
		Groups: []Group{
			{Name: "explore", Tools: []string{"codegraph_explore"}},
			{Name: "query", Tools: []string{"codegraph_node", "codegraph_search", "codegraph_callers", "codegraph_callees",
				"codegraph_impact", "codegraph_files", "codegraph_status"}},
		},
		ReadOnly:   codegraphTools,
		DefaultEnv: map[string]string{"CODEGRAPH_TELEMETRY": "0", "CODEGRAPH_MCP_TOOLS": strings.Join(codegraphTools, ",")},
	},
	{
		ID: "specmatic", Prefix: "spec", Transport: Stdio, Version: "1.2.0",
		Install: "uv tool install specmatic-mcp==1.2.0",
		Run:     []string{"specmatic-mcp"},
		Share:   ShareWorktree, Cwd: CwdState,
		Groups: []Group{
			{Name: "compat", Tools: []string{"backward_compatibility_check"}},
			{Name: "contract", Tools: []string{"run_contract_test", "run_resiliency_test"}, MaxConcurrent: 1},
			{Name: "mock", Tools: []string{"manage_mock_server"}, Exclusive: true},
		},
		ReadOnly:       []string{"backward_compatibility_check"},
		WritesWorktree: []string{"run_contract_test", "run_resiliency_test", "manage_mock_server"},
		DefaultArgs:    map[string]map[string]string{"backward_compatibility_check": {"repoDir": WorktreePlaceholder}},
		Options:        map[string]string{"specs": "glob tệp OpenAPI/AsyncAPI của dự án"},
	},
	{
		ID: "deepwiki", Prefix: "wiki", Transport: HTTP, URL: "https://mcp.deepwiki.com/mcp",
		Share: ShareRemote,
		Groups: []Group{
			{Name: "read", Tools: []string{"read_wiki_structure", "read_wiki_contents", "ask_question"}},
		},
		ReadOnly: []string{"read_wiki_structure", "read_wiki_contents", "ask_question"},
	},
	{
		ID: "context7", Prefix: "ctx7", Transport: HTTP, URL: "https://mcp.context7.com/mcp",
		Share: ShareRemote,
		Groups: []Group{
			{Name: "read", Tools: []string{"resolve-library-id", "get-library-docs"}},
		},
		ReadOnly: []string{"resolve-library-id", "get-library-docs"},
	},
	{
		ID: "jira", Prefix: "jira", Transport: Stdio, Version: "3.4.0",
		Install: "uv tool install atlassian-jira-mcp==3.4.0",
		Run:     []string{"atlassian-jira-mcp"},
		Share:   ShareMachine,
		Groups: []Group{
			{Name: "read", Tools: []string{"jira_issue_get", "jira_issue_search", "jira_project_list", "jira_comment_list"}},
			{Name: "write", Tools: []string{"jira_issue_create", "jira_issue_update", "jira_issue_transition", "jira_comment_add"}},
		},
		ReadOnly:      []string{"jira_issue_get", "jira_issue_search", "jira_project_list", "jira_comment_list"},
		CredentialEnv: []string{"JIRA_MCP_URL", "JIRA_MCP_USERNAME", "JIRA_MCP_PASSWORD"},
	},
	{
		ID: "chrome-devtools", Prefix: "chrome", Transport: Stdio, Version: "1.13.0",
		Install: "npm i -g chrome-devtools-mcp@1.13.0",
		Run:     []string{"chrome-devtools-mcp"},
		Share:   ShareSession,
		Groups: []Group{
			{Name: "read", Tools: []string{"list_pages", "take_snapshot", "take_screenshot", "list_console_messages",
				"get_console_message", "list_network_requests", "get_network_request"}},
			{Name: "act", Tools: []string{"new_page", "select_page", "close_page", "navigate_page", "click", "fill",
				"hover", "press_key", "evaluate_script", "wait_for"}},
		},
		ReadOnly: []string{"list_pages", "take_snapshot", "take_screenshot", "list_console_messages",
			"get_console_message", "list_network_requests", "get_network_request"},
	},
}

// All trả về mọi service của danh mục (bản sao nông, theo thứ tự khai báo).
func All() []Service {
	out := make([]Service, len(catalog))
	copy(out, catalog)
	return out
}

// IDs trả về id của mọi service, theo thứ tự khai báo.
func IDs() []string {
	out := make([]string, len(catalog))
	for i, s := range catalog {
		out[i] = s.ID
	}
	return out
}

// Lookup tìm service theo id.
func Lookup(id string) (Service, bool) {
	for _, s := range catalog {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

// Group tìm nhóm theo tên.
func (s Service) Group(name string) (Group, bool) {
	for _, g := range s.Groups {
		if g.Name == name {
			return g, true
		}
	}
	return Group{}, false
}

// GroupNames trả về tên các nhóm theo thứ tự khai báo.
func (s Service) GroupNames() []string {
	out := make([]string, len(s.Groups))
	for i, g := range s.Groups {
		out[i] = g.Name
	}
	return out
}

// Tools trả về mọi tool upstream của service (theo nhóm, theo thứ tự khai báo).
func (s Service) Tools() []string {
	var out []string
	for _, g := range s.Groups {
		out = append(out, g.Tools...)
	}
	return out
}

// IsReadOnly cho biết tool upstream có chỉ đọc (được retry) không.
func (s Service) IsReadOnly(tool string) bool { return contains(s.ReadOnly, tool) }

// OptionNames trả về tên tuỳ chọn riêng của dự án, đã sắp xếp.
func (s Service) OptionNames() []string {
	out := make([]string, 0, len(s.Options))
	for k := range s.Options {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AgentToolName là tên tool tới agent: <tiền tố>_<tên upstream bỏ tiền tố trùng>. Tiền tố trùng là id của
// service hoặc tiền tố ngắn (codegraph_explore → cg_explore; jira_issue_get → jira_issue_get).
func AgentToolName(s Service, upstream string) string {
	rest := upstream
	for _, p := range []string{s.ID + "_", s.Prefix + "_"} {
		if strings.HasPrefix(rest, p) && len(rest) > len(p) {
			rest = strings.TrimPrefix(rest, p)
			break
		}
	}
	return s.Prefix + "_" + rest
}

// ToolRef là một tool đã giải từ quyền.
type ToolRef struct {
	Service  string
	Group    string
	Upstream string
	Agent    string
}

// ParseGrant tách "<service>.<nhóm>" hoặc "<service>.*" (nhóm "*" = mọi nhóm).
// Kiểm cả service lẫn nhóm có trong danh mục.
func ParseGrant(grant string) (Service, string, error) {
	svcID, grp, ok := strings.Cut(strings.TrimSpace(grant), ".")
	if !ok || svcID == "" || grp == "" || strings.Contains(grp, ".") {
		return Service{}, "", fmt.Errorf("%q: cần dạng <service>.<nhóm> hoặc <service>.*", grant)
	}
	svc, found := Lookup(svcID)
	if !found {
		return Service{}, "", fmt.Errorf("%q: service %q không có trong danh mục (%s)", grant, svcID, strings.Join(IDs(), ", "))
	}
	if grp != "*" {
		if _, found := svc.Group(grp); !found {
			return Service{}, "", fmt.Errorf("%q: nhóm %q không có trong %s (có: %s)", grant, grp, svcID, strings.Join(svc.GroupNames(), ", "))
		}
	}
	return svc, grp, nil
}

// Expand giải một phần tử quyền thành tool thật (theo thứ tự nhóm rồi tool khai báo).
func Expand(grant string) ([]ToolRef, error) {
	svc, grp, err := ParseGrant(grant)
	if err != nil {
		return nil, err
	}
	var out []ToolRef
	for _, g := range svc.Groups {
		if grp != "*" && g.Name != grp {
			continue
		}
		for _, t := range g.Tools {
			out = append(out, ToolRef{Service: svc.ID, Group: g.Name, Upstream: t, Agent: AgentToolName(svc, t)})
		}
	}
	return out, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
