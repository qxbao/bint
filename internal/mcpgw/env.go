package mcpgw

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/iso"
	"bint/internal/mcpcat"
	"bint/internal/state"
)

// BaseEnv là env tối thiểu truyền cho tiến trình upstream: không kế thừa env của shell (credential lấy từ
// secrets.env, D2 của docs/mcp-gateway-plan.md).
var BaseEnv = []string{"PATH", "HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "SystemRoot", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL"}

// Env dựng env cho tiến trình upstream: env tối thiểu, env mặc định của danh mục và biến credential từ secrets.
// Trả thêm tên biến credential còn thiếu và các giá trị bí mật (để che khỏi đầu ra).
//
// BaseEnv lấy từ env THẬT của máy: tiến trình chạy trong profile `bint iso` (docs/iso-plan.md mục 5.7) có HOME/TMPDIR/XDG giả,
// còn daemon dùng chung và upstream của nó phải thấy home và temp thật, không giữ env của profile đầu tiên khởi động daemon.
func Env(svc mcpcat.Service, reg config.Registration, secrets map[string]string) (env, missing, secretVals []string) {
	return envFrom(os.Environ(), svc, reg, secrets)
}

// envFrom như Env với environ cho trước (kiểm thử).
func envFrom(environ []string, svc mcpcat.Service, reg config.Registration, secrets map[string]string) (env, missing, secretVals []string) {
	real := map[string]string{}
	for _, kv := range iso.RealEnv(environ) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if runtime.GOOS == "windows" {
				k = strings.ToUpper(k)
			}
			real[k] = v
		}
	}
	for _, k := range BaseEnv {
		key := k
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(k)
		}
		if v := real[key]; v != "" {
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
