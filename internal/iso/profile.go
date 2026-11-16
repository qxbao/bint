package iso

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bint/internal/fsafe"
)

// MetaVersion là phiên bản của profile.json.
const MetaVersion = 1

// Chế độ Codex LB của profile ("" = không dùng LB). Việc dò và chốt URL thuộc GĐ2.
const (
	LBLinked = "linked"
	LBDirect = "direct"
)

// LB là cấu hình Codex LB đã chốt cho profile.
type LB struct {
	Mode string `json:"mode,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Meta là nội dung profile.json (0600).
type Meta struct {
	Version   int       `json:"version"`
	Tool      Tool      `json:"tool"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LB        LB        `json:"lb"`
}

// Profile là một profile đã nạp từ store.
type Profile struct {
	Tool Tool
	Name string
	// StoreRoot là gốc store; Dir = <StoreRoot>/profiles/<tool>--<name>; Home = <Dir>/home (HOME giả).
	StoreRoot string
	Dir       string
	Home      string
	Meta      Meta
	Settings  Settings
}

// ID là "<tool>/<name>", giá trị của BINT_ISO_PROFILE.
func (p Profile) ID() string { return string(p.Tool) + "/" + p.Name }

// ConfigDir là thư mục cấu hình của harness trong HOME giả: <Home>/.codex (CODEX_HOME) hoặc <Home>/.claude
// (CLAUDE_CONFIG_DIR). Nơi harness ghi rollout/transcript của phiên chạy trong profile.
func (p Profile) ConfigDir() string { return filepath.Join(p.Home, p.Tool.spec().ConfigDir) }

// MetaPath là đường dẫn profile.json.
func (p Profile) MetaPath() string { return filepath.Join(p.Dir, "profile.json") }

// LockPath là đường dẫn session.lock.
func (p Profile) LockPath() string { return filepath.Join(p.Dir, "session.lock") }

// SaveMeta ghi lại profile.json (nguyên tử, 0600).
func (p Profile) SaveMeta() error { return writeMeta(p.MetaPath(), p.Meta) }

func writeMeta(path string, m Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(path, append(data, '\n'), 0o600)
}

// readMeta đọc và kiểm profile.json của thư mục profile dir (đã biết tool/name từ tên thư mục).
func readMeta(path string, t Tool, name string) (Meta, error) {
	var m Meta
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	switch {
	case m.Version != MetaVersion:
		return m, fmt.Errorf("%s: phiên bản %d chưa hỗ trợ (cần %d)", path, m.Version, MetaVersion)
	case m.Tool != t || m.Name != name:
		return m, fmt.Errorf("%s: ghi %s/%s nhưng thư mục là %s/%s", path, m.Tool, m.Name, t, name)
	case m.LB.Mode != "" && m.LB.Mode != LBLinked && m.LB.Mode != LBDirect:
		return m, fmt.Errorf("%s: lb.mode %q không hợp lệ", path, m.LB.Mode)
	}
	return m, nil
}
