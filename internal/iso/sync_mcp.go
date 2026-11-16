package iso

import (
	"path/filepath"
	"strings"

	"bint/internal/state"
)

// BintMCPEnv là bảng env mà mục MCP stdio chạy `bint` của Codex phải mang (docs/iso-plan.md mục 5.6, S1: Codex
// chỉ chuyển HOME/PATH/TMPDIR cho MCP stdio, nên không có bảng này `bint mcp` rơi về state giả): BINT_STATE_DIR
// và BINT_CONFIG_HOME tuyệt đối của máy thật, cùng nguồn với Env.
func BintMCPEnv(env []string) (map[string]string, error) {
	e := realEnvList(env)
	stateDir, err := state.BaseDirFrom(e.get)
	if err != nil {
		return nil, err
	}
	cfgDir, err := realConfigHome(e)
	if err != nil {
		return nil, err
	}
	return map[string]string{envStateDir: stateDir, envConfigHome: cfgDir}, nil
}

// isBintCommand: tên tệp của lệnh là bint hoặc bint.exe.
func isBintCommand(cmd string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(cmd, `\`, "/")))
	return base == "bint" || base == "bint.exe"
}

// AdaptCodexMCP trả bản sao của mục [mcp_servers.X] đã thêm env: mục stdio có lệnh bint nhận
// BINT_STATE_DIR/BINT_CONFIG_HOME (giữ giá trị sẵn có trong bản thật). Mục khác (URL, lệnh khác) giữ nguyên.
func AdaptCodexMCP(v any, bintEnv map[string]string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if _, isURL := m["url"]; isURL {
		return v
	}
	cmd, _ := m["command"].(string)
	if !isBintCommand(cmd) {
		return v
	}
	out := make(map[string]any, len(m)+1)
	for k, x := range m {
		out[k] = x
	}
	envTbl := map[string]any{}
	if old, ok := m["env"].(map[string]any); ok {
		for k, x := range old {
			envTbl[k] = x
		}
	}
	for k, x := range bintEnv {
		if _, has := envTbl[k]; !has {
			envTbl[k] = x
		}
	}
	out["env"] = envTbl
	return out
}
