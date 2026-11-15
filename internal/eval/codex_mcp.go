package eval

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"bint/internal/codexconfig"
)

var tomlBareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// codexOtherMCPServers liệt kê server MCP khác bint mà Codex sẽ nạp cho yêu cầu này, không trùng, theo thứ tự tên:
// các bảng [mcp_servers.*] trong $CODEX_HOME/config.toml (CODEX_HOME trong req.Env thắng môi trường tiến trình;
// mặc định ~/.codex) và trong <req.Dir>/.codex/config.toml.
//
// Chỉ đọc các lớp cấu hình dự án được Codex tin cậy, từ cwd tới gốc worktree.
// Một thư mục có trust riêng thì dùng giá trị đó; còn lại kế thừa gốc repo chính.
// Không đọc TOML dự án chưa tin cậy: Codex bỏ qua nó và tắt tên server không tồn
// tại có thể làm CLI báo invalid transport.
func codexOtherMCPServers(req AgentRequest) ([]string, error) {
	home := ""
	for _, kv := range req.Env {
		if v, ok := strings.CutPrefix(kv, "CODEX_HOME="); ok {
			home = v
		}
	}
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		if uh, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(uh, ".codex")
		}
	}
	seen := map[string]bool{}
	add := func(c codexconfig.Config) {
		for name := range c.MCPServers {
			if name != "bint" {
				seen[name] = true
			}
		}
	}
	var user codexconfig.Config
	if home != "" {
		var err error
		if user, err = codexconfig.Read(filepath.Join(home, "config.toml")); err != nil {
			return nil, err
		}
		add(user)
	}
	for _, dir := range user.TrustedDirs(req.Dir) {
		proj, err := codexconfig.Read(filepath.Join(dir, ".codex", "config.toml"))
		if err != nil {
			return nil, err
		}
		add(proj)
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// codexDisableMCPArgs dựng các cặp `-c` tắt mọi server MCP khác bint (req.ExclusiveMCP).
//
// Tên là khoá trần TOML (chữ, số, `_`, `-`) → `-c mcp_servers.<tên>.enabled=false`. Codex tách đường dẫn khoá của -c
// theo dấu chấm mà không hiểu dấu trích dẫn (`mcp_servers."a.b".enabled` báo lỗi), nên tên cần trích dẫn (có dấu chấm,
// khoảng trắng...) gộp vào một bảng nội tuyến `-c mcp_servers={"a.b"={enabled=false}}`. Bảng nội tuyến thay thế mọi
// khoá mcp_servers.* đặt bằng -c trước nó, nên các cặp này phải đứng TRƯỚC mọi `-c mcp_servers.*` khác (kể cả của bint).
func codexDisableMCPArgs(req AgentRequest) ([]string, error) {
	names, err := codexOtherMCPServers(req)
	if err != nil {
		return nil, err
	}
	var bare, quoted []string
	for _, n := range names {
		if tomlBareKey.MatchString(n) {
			bare = append(bare, n)
		} else {
			quoted = append(quoted, tomlString(n)+"={enabled=false}")
		}
	}
	var args []string
	if len(quoted) > 0 {
		args = append(args, "-c", "mcp_servers={"+strings.Join(quoted, ",")+"}")
	}
	for _, n := range bare {
		args = append(args, "-c", "mcp_servers."+n+".enabled=false")
	}
	return args, nil
}
