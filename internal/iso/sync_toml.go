package iso

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"

	"bint/internal/fsafe"
)

// keyBackend là tệp đích của sync: đọc/ghi/gỡ một khoá <section>.<name>. Get/Set/Del không ghi đĩa (trừ
// backend hook, ghi ngay qua InstallHooks); save mới ghi tệp, chỉ khi có thay đổi.
type keyBackend interface {
	get(section []string, name string) (any, bool, error)
	set(section []string, name string, v any) error
	del(section []string, name string) error
	save(backupDir, rel string, now time.Time) (changed bool, err error)
}

// tomlFile là config.toml của Codex trong profile, giữ cả cây (mọi khoá lạ còn nguyên) và chỉ đổi các khoá
// của sync. Ghi lại bằng go-toml nên mất chú thích và thứ tự (chấp nhận, có sao lưu trước khi ghi).
type tomlFile struct {
	path  string
	tree  map[string]any
	dirty bool
}

func openTOML(path string) (*tomlFile, error) {
	f := &tomlFile{path: path, tree: map[string]any{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, &f.tree); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// table đi theo section; create=true thì tạo bảng còn thiếu. Gặp giá trị không phải bảng thì ok=false.
func tomlTable(root map[string]any, section []string, create bool) (map[string]any, bool) {
	cur := root
	for _, s := range section {
		next, present := cur[s]
		if !present {
			if !create {
				return nil, false
			}
			m := map[string]any{}
			cur[s] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = m
	}
	return cur, true
}

func (f *tomlFile) get(section []string, name string) (any, bool, error) {
	t, ok := tomlTable(f.tree, section, false)
	if !ok {
		return nil, false, nil
	}
	v, ok := t[name]
	return v, ok, nil
}

func (f *tomlFile) set(section []string, name string, v any) error {
	t, ok := tomlTable(f.tree, section, true)
	if !ok {
		return fmt.Errorf("%s: %q không phải bảng TOML", f.path, entryKey(section, name))
	}
	t[name] = v
	f.dirty = true
	return nil
}

func (f *tomlFile) del(section []string, name string) error {
	t, ok := tomlTable(f.tree, section, false)
	if !ok {
		return nil
	}
	if _, present := t[name]; !present {
		return nil
	}
	delete(t, name)
	f.dirty = true
	// Bảng cha rỗng do gỡ khoá cuối thì bỏ luôn (không để "[mcp_servers]" trống).
	for i := len(section); i > 0 && len(t) == 0; i-- {
		parent, _ := tomlTable(f.tree, section[:i-1], false)
		delete(parent, section[i-1])
		t = parent
	}
	return nil
}

func (f *tomlFile) save(backupDir, rel string, now time.Time) (bool, error) {
	if !f.dirty {
		return false, nil
	}
	data, err := toml.Marshal(f.tree)
	if err != nil {
		return false, fmt.Errorf("%s: %w", f.path, err)
	}
	if _, err := fsafe.Backup(f.path, backupDir, rel, now); err != nil {
		return false, err
	}
	if err := fsafe.WriteAtomic(f.path, data, 0o600); err != nil {
		return false, err
	}
	return true, nil
}
