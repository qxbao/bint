package hcomp

import (
	"fmt"
	"strings"

	"bint/internal/pathglob"
)

// RenderOpts điều khiển việc dựng khối chỉ dẫn.
type RenderOpts struct {
	// MCP: kênh MCP của Bint có bật (guidance có requires: [mcp] chỉ hiện khi đúng).
	MCP bool
}

func (o RenderOpts) met(c Component) bool {
	for _, r := range c.Requires {
		if r == "mcp" && !o.MCP {
			return false
		}
	}
	return true
}

// block chuẩn hoá một guidance thành khối Markdown bắt đầu và kết thúc bằng '\n'.
// Thân đã có tiêu đề (#) thì giữ nguyên; chưa có thì thêm "## <title>".
func block(c Component) string {
	body := strings.Trim(c.Body, "\n")
	if !strings.HasPrefix(strings.TrimSpace(body), "#") && c.Title != "" {
		body = "## " + c.Title + "\n\n" + body
	}
	return "\n" + body + "\n"
}

// RenderGuidance dựng phần chỉ dẫn CHUNG (guidance global + danh sách script) để nối vào
// AGENTS.md; rỗng nếu không có gì. Với bản dựng sẵn bint-mcp-tools kết quả bằng đúng từng
// byte MCPToolsNote.
func (s Set) RenderGuidance(o RenderOpts) string {
	var b strings.Builder
	for _, c := range s.Guidance() {
		if c.Global() && o.met(c) {
			b.WriteString(block(c))
		}
	}
	b.WriteString(s.RenderScripts())
	return b.String()
}

// RenderScripts liệt kê script có hiệu lực để agent biết mà gọi. Bint không bao giờ tự chạy.
func (s Set) RenderScripts() string {
	var items []string
	for _, c := range s.Scripts() {
		if len(c.Source) == 0 {
			continue
		}
		line := fmt.Sprintf("- `.bint/%s/scripts/%s.sh` — %s", DirName, c.ID, oneLine(c.Description))
		if c.Usage != "" {
			line += " Cách dùng: `" + oneLine(c.Usage) + "`."
		}
		items = append(items, line)
	}
	if len(items) == 0 {
		return ""
	}
	return "\n## Script hỗ trợ (Bint)\n\nCác script này nằm trong repository và đã được người duyệt; chạy khi cần (Bint không tự chạy):\n\n" +
		strings.Join(items, "\n") + "\n"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// DomainGuidance trả về guidance RIÊNG có phạm vi khớp một trong rels và chưa được chèn
// (seen = ID đã chèn trong phiên).
func (s Set) DomainGuidance(rels []string, o RenderOpts, seen map[string]bool) []Component {
	var out []Component
	for _, c := range s.Guidance() {
		g := c.Domain()
		if g == "" || seen[c.ID] || !o.met(c) {
			continue
		}
		p := pathglob.Compile(g)
		for _, r := range rels {
			if p.Match(r) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// RenderDomain dựng văn bản chèn cho guidance riêng (qua hook theo đường dẫn).
func RenderDomain(cs []Component) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "## Chỉ dẫn quy trình (Bint) — %s\n\n%s\n\n", c.ID, strings.Trim(c.Body, "\n"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// Matches cho biết đường dẫn rel khớp phạm vi của thành phần (global khớp mọi đường dẫn).
func (m Meta) Matches(rel string) bool {
	if m.Global() {
		return true
	}
	g := m.Domain()
	return g != "" && pathglob.Compile(g).Match(rel)
}
