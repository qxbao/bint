package eval

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var tomlBareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// codexConfig là phần của config.toml của Codex mà Bint cần: tên server MCP và các dự án đã tin cậy.
type codexConfig struct {
	MCPServers map[string]any `toml:"mcp_servers"`
	Projects   map[string]struct {
		TrustLevel string `toml:"trust_level"`
	} `toml:"projects"`
}

// readCodexConfig đọc một config.toml; tệp không tồn tại cho cấu hình rỗng, TOML hỏng trả lỗi nêu đường dẫn.
func readCodexConfig(path string) (codexConfig, error) {
	var cfg codexConfig
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c codexConfig) hasTrusted() bool {
	for _, v := range c.Projects {
		if v.TrustLevel == "trusted" {
			return true
		}
	}
	return false
}

// mainRepoRoot trả gốc của repo chính khi dir là worktree liên kết (cha của `git rev-parse --git-common-dir`); rỗng khi
// dir không trong repo Git, repo bare hay không xác định được. Codex tính tin cậy theo gốc repo chính nên worktree của
// tác vụ kế thừa tin cậy của cây gốc.
func mainRepoRoot(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	common := filepath.Clean(strings.TrimSpace(string(out)))
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}

// trusts cho biết cấu hình người dùng đã tin cậy đúng thư mục dir (khoá projects.<đường dẫn>.trust_level = "trusted").
func (c codexConfig) trusts(dir string) bool {
	if dir == "" {
		return false
	}
	want := filepath.Clean(dir)
	for p, v := range c.Projects {
		if v.TrustLevel == "trusted" && filepath.Clean(p) == want {
			return true
		}
	}
	return false
}

// codexOtherMCPServers liệt kê server MCP khác bint mà Codex sẽ nạp cho yêu cầu này, không trùng, theo thứ tự tên:
// các bảng [mcp_servers.*] trong $CODEX_HOME/config.toml (CODEX_HOME trong req.Env thắng môi trường tiến trình;
// mặc định ~/.codex) và trong <req.Dir>/.codex/config.toml.
//
// Server chỉ có trong config dự án được tính khi người dùng đã tin cậy đúng thư mục đó: Codex bỏ qua config của dự
// án chưa tin cậy, và `enabled=false` cho một tên không tồn tại làm Codex báo "invalid transport" rồi không chạy.
// Tin cậy được nhận theo req.Dir hoặc gốc repo chính (khi req.Dir là worktree liên kết). Dự án tin cậy qua thư mục cha
// khác thì không nhận ra được: server riêng của dự án đó không bị tắt (không lỗi). Config dự án chưa tin cậy không được
// đọc, nên TOML hỏng ở đó không làm hỏng bước.
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
	add := func(c codexConfig) {
		for name := range c.MCPServers {
			if name != "bint" {
				seen[name] = true
			}
		}
	}
	var user codexConfig
	if home != "" {
		var err error
		if user, err = readCodexConfig(filepath.Join(home, "config.toml")); err != nil {
			return nil, err
		}
		add(user)
	}
	if req.Dir != "" {
		if user.trusts(req.Dir) || (user.hasTrusted() && user.trusts(mainRepoRoot(req.Dir))) {
			proj, err := readCodexConfig(filepath.Join(req.Dir, ".codex", "config.toml"))
			if err != nil {
				return nil, err
			}
			add(proj)
		}
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
