package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
)

// mcpAt dựng mô hình đã ở màn MCP (phím 8) với kết quả check trong phiên làm deepwiki ở trạng thái degraded (nên danh sách đủ cả năm
// trạng thái), rồi áp keys.
func mcpAt(t *testing.T, o Options, w, h int, keys ...string) *Model {
	t.Helper()
	m := newModel(t, o, sampleData(), w, h)
	press(t, m, "8")
	m.Update(actions.ResultMsg{ID: "mcp.check", Value: actions.MCPCheckOutcome{Results: []bintapp.MCPCheckResult{
		{Service: "deepwiki", Status: bintapp.MCPStatusDegraded, Tools: []string{"read_wiki_structure"}, Missing: []string{"ask_question"},
			Detail: "upstream thiếu tool của nhóm thao tác: ask_question (danh mục lệch phiên bản upstream?)"}}}})
	press(t, m, "esc") // đóng khung kết quả và xoá thông báo của lần check giả: chỉ giữ trạng thái đã suy ra
	press(t, m, keys...)
	return m
}

func sampleMCPLog() bintapp.MCPGatewayLogResult {
	return bintapp.MCPGatewayLogResult{Service: "specmatic", LogFile: "/state/gateway/gateway.log",
		Upstreams: []bintapp.MCPGatewayLogUpstream{{Partition: "phiên 01HX0000", Running: false}, {Partition: "/work/demo", Running: true, Stderr: "warn ***\nlỗi thứ hai"}},
		Lines:     []string{"2026-11-13T04:00:01Z specmatic (phiên 01HX0000): exit status 1 [stderr: boom ***]", "2026-11-13T04:00:09Z specmatic (phiên 01HX0000): vượt giới hạn khởi động lại"},
		Note:      "gateway đang chạy là bản cũ chưa có /admin/log: chỉ có phần gateway.log"}
}

// mcpScreens liệt kê các ảnh chụp văn bản của màn MCP: danh sách đủ năm trạng thái, chi tiết, lọc dự án, form thêm, xác nhận gỡ, thẻ Quyền, thẻ
// Công cụ Bint, log, việc nền đang chạy và phiên agent.
func mcpScreens(t *testing.T, lang string, w, h int) map[string]func() *Model {
	o := Options{Mode: theme.Mono, Lang: lang}
	agent := Options{Mode: theme.Mono, Lang: lang, Agent: true}
	return map[string]func() *Model{
		"mcp_list":          func() *Model { return mcpAt(t, o, w, h) },
		"mcp_detail":        func() *Model { return mcpAt(t, o, w, h, "enter") },
		"mcp_detail_error":  func() *Model { return mcpAt(t, o, w, h, "j", "enter") },
		"mcp_detail_jira":   func() *Model { return mcpAt(t, o, w, h, "j", "j", "j", "j", "enter") },
		"mcp_filter":        func() *Model { return mcpAt(t, o, w, h, "p") },
		"mcp_add_form":      func() *Model { return mcpAt(t, o, w, h, "j", "j", "j", "j", "a") },
		"mcp_remove":        func() *Model { return mcpAt(t, o, w, h, "j", "j", "j", "j", "x") },
		"mcp_grants":        func() *Model { return mcpAt(t, o, w, h, ".") },
		"mcp_grants_detail": func() *Model { return mcpAt(t, o, w, h, ".", "j", "enter") },
		"mcp_tools":         func() *Model { return mcpAt(t, o, w, h, ".", ".") },
		"mcp_log": func() *Model {
			m := mcpAt(t, o, w, h, "j")
			m.Update(actions.ResultMsg{ID: "mcp.log", Opts: bintapp.MCPGatewayLogOptions{Service: "specmatic"}, Value: sampleMCPLog()})
			return m
		},
		"mcp_job": func() *Model {
			m := mcpAt(t, o, w, h)
			j := m.jobs.Add(jobs.Job{Action: "mcp.check", Kind: jobs.KindAction, Label: "mcp check jira"})
			j.Log.Add("Kiểm tra jira…", "jira: ổn")
			m.Update(m.spin.Tick())
			return m
		},
		"mcp_agent":        func() *Model { return mcpAt(t, agent, w, h) },
		"mcp_agent_denied": func() *Model { return mcpAt(t, agent, w, h, "a", "x") },
		"mcp_readonly":     func() *Model { return mcpAt(t, Options{Mode: theme.Mono, Lang: lang, ReadOnly: true}, w, h) },
	}
}

// Ảnh chụp văn bản của màn MCP ở 80×24 và 140×40, vi/en/locale giả; không dòng nào vượt chiều rộng.
func TestGoldenMCPScreens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			for name, build := range mcpScreens(t, lang, sz[0], sz[1]) {
				out := plain(build().Render())
				for i, l := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(l); w > sz[0] {
						t.Errorf("%s %dx%d %s: dòng %d rộng %d > %d: %q", name, sz[0], sz[1], lang, i, w, sz[0], l)
					}
				}
				if n := len(strings.Split(out, "\n")); n > sz[1] {
					t.Errorf("%s %dx%d %s: %d dòng > %d", name, sz[0], sz[1], lang, n, sz[1])
				}
				checkGolden(t, fmt.Sprintf("%s_%dx%d_%s.txt", name, sz[0], sz[1], lang), out+"\n")
			}
		}
	}
}

// Không chuỗi nào ở vi/en/locale giả còn là khoá chưa dịch (dạng "mc.x.y" hay "action.mcp.…"): mọi thẻ và trạng thái.
func TestMCPScreensHaveNoUntranslatedKeys(t *testing.T) {
	for _, lang := range []string{"vi", "en", "xx"} {
		for name, build := range mcpScreens(t, lang, 140, 40) {
			out := plain(build().Render())
			for _, bad := range []string{"mc.", "action.mcp.", "form.mcp."} {
				if strings.Contains(out, bad) {
					t.Errorf("%s (%s) còn khoá chưa dịch %q:\n%s", name, lang, bad, out)
				}
			}
		}
	}
}
