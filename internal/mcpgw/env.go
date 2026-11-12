package mcpgw

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/mcpcat"
	"bint/internal/state"
)

// BaseEnv là env tối thiểu truyền cho tiến trình upstream: không kế thừa env của shell (credential lấy từ
// secrets.env, D2 của docs/mcp-gateway-plan.md).
var BaseEnv = []string{"PATH", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SystemRoot", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL"}

// Env dựng env cho tiến trình upstream: env tối thiểu, env mặc định của danh mục và biến credential từ secrets.
// Trả thêm tên biến credential còn thiếu và các giá trị bí mật (để che khỏi đầu ra).
func Env(svc mcpcat.Service, reg config.Registration, secrets map[string]string) (env, missing, secretVals []string) {
	for _, k := range BaseEnv {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(svc.DefaultEnv)) {
		env = append(env, k+"="+svc.DefaultEnv[k])
	}
	names := slices.Clone(svc.CredentialEnv)
	for _, n := range reg.EnvNames {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	for _, n := range names {
		if v := secrets[n]; v != "" {
			env = append(env, n+"="+v)
			secretVals = append(secretVals, v)
		} else {
			missing = append(missing, n)
		}
	}
	return env, missing, secretVals
}

// ScrubSecrets che mọi giá trị bí mật còn sót trong s, cắt gọn thành một dòng ≤ max ký tự.
func ScrubSecrets(s string, secretVals []string, max int) string {
	for _, v := range secretVals {
		if len(v) >= 3 {
			s = strings.ReplaceAll(s, v, "***")
		}
	}
	return detect.Redact(strings.Join(strings.Fields(s), " "), max)
}

// StateCwd là thư mục cwd riêng của service trong thư mục trạng thái: <stateRoot>/mcp/<service>/<worktree-id>
// (tạo 0700). worktree-id là state.RepoID của gốc worktree.
func StateCwd(stateRoot, serviceID, worktreeRoot string) (string, error) {
	p := filepath.Join(stateRoot, "mcp", serviceID, state.RepoID(worktreeRoot))
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("tạo thư mục cwd của %s: %w", serviceID, err)
	}
	return p, nil
}
