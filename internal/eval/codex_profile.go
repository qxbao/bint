package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// codexEvalProfile tách auth/runtime khỏi instructions, plugin, hook và skill cá
// nhân. Chỉ áp cho eval (req.Arm khác rỗng), không đổi môi trường bint run/Claude.
// Profile không nằm trong worktree hay thư mục log được xuất; auth mode 0600,
// xoá bằng defer ngay cả khi agent thất bại/timeout/huỷ.
func codexEvalProfile(req AgentRequest, baseEnv []string) ([]string, func(), error) {
	profile, err := os.MkdirTemp("", "bint-codex-profile-")
	if err != nil {
		return nil, nil, err
	}
	ready := false
	defer func() {
		if !ready {
			os.RemoveAll(profile)
		}
	}()
	values := map[string]string{}
	for _, kv := range baseEnv {
		k, v, _ := strings.Cut(kv, "=")
		values[k] = v
	}
	source := values["CODEX_HOME"]
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	if source == "" {
		source = filepath.Join(userHome, ".codex")
	}
	auth, err := os.ReadFile(filepath.Join(source, "auth.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("eval cô lập Codex cần auth.json dạng tệp của profile đã đăng nhập; không lấy được thông tin đăng nhập (%w)", err)
	}
	if err = os.WriteFile(filepath.Join(profile, "auth.json"), auth, 0o600); err != nil {
		return nil, nil, err
	}
	var original map[string]any
	raw, err := os.ReadFile(filepath.Join(source, "config.toml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if len(raw) > 0 {
		if err = toml.Unmarshal(raw, &original); err != nil {
			return nil, nil, fmt.Errorf("cấu hình Codex nguồn không hợp lệ: %w", err)
		}
	}
	config := map[string]any{}
	var retained []string
	for _, key := range []string{"model", "model_provider", "model_providers", "model_reasoning_effort", "model_context_window", "model_auto_compact_token_limit", "service_tier"} {
		if v, ok := original[key]; ok {
			config[key] = v
			retained = append(retained, key)
		}
	}
	config["cli_auth_credentials_store"] = "file"
	config["approval_policy"] = "never"
	config["projects"] = map[string]any{req.Dir: map[string]string{"trust_level": "trusted"}}
	// User skills ở ~/.agents/skills vẫn được Codex phát hiện dù CODEX_HOME mới.
	// Ghi deny theo đường dẫn (cả symlink và đích) thay vì giả định home mới là đủ.
	var disabled []map[string]any
	disabledPaths := map[string]bool{}
	disable := func(path string) {
		if !disabledPaths[path] {
			disabledPaths[path] = true
			disabled = append(disabled, map[string]any{"path": path, "enabled": false})
		}
	}
	seen := map[string]bool{}
	var visit func(string, ...string) error
	visit = func(root string, aliases ...string) error {
		real, err := filepath.EvalSymlinks(root)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		// Carry all spellings through a single traversal. Visiting aliases
		// separately would lose their rules when seen[real] deduplicates dirs.
		spellings := append([]string{root, real}, aliases...)
		known := map[string]bool{}
		for _, p := range spellings {
			known[p] = true
		}
		for i := 0; i < len(spellings); i++ {
			p := spellings[i]
			if target, e := os.Readlink(p); e == nil {
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(p), target)
				}
				if !known[target] {
					known[target] = true
					spellings = append(spellings, target)
				}
			}
		}
		// Một SKILL.md có thể là symlink tới file tên khác; phải chặn tên
		// được Codex phát hiện trước khi dereference, cùng đường dẫn đích.
		if filepath.Base(root) == "SKILL.md" {
			if st, e := os.Stat(real); e == nil && !st.IsDir() {
				for _, p := range spellings {
					disable(p)
				}
				return nil
			}
		}
		if seen[real] {
			return nil
		}
		seen[real] = true
		return filepath.WalkDir(real, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(real, path)
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				var childAliases []string
				for _, p := range spellings {
					childAliases = append(childAliases, filepath.Join(p, rel))
				}
				return visit(path, childAliases...)
			}
			if !d.IsDir() && d.Name() == "SKILL.md" {
				for _, p := range spellings {
					disable(filepath.Join(p, rel))
				}
			}
			return nil
		})
	}
	for _, root := range []string{filepath.Join(userHome, ".agents", "skills"), "/etc/codex/skills"} {
		if err = visit(root); err != nil {
			return nil, nil, fmt.Errorf("không cô lập được user/admin skill Codex: %w", err)
		}
	}
	config["skills"] = map[string]any{"bundled": map[string]bool{"enabled": false}, "config": disabled}
	raw, err = toml.Marshal(config)
	if err != nil {
		return nil, nil, err
	}
	if err = os.WriteFile(filepath.Join(profile, "config.toml"), raw, 0o600); err != nil {
		return nil, nil, err
	}
	var env []string
	for _, kv := range baseEnv {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CODEX_HOME", "CODEX_THREAD_ID", "CODEX_SESSION_ID":
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "CODEX_HOME="+profile)
	// Chỉ ghi metadata không chứa auth, provider values hay đường dẫn profile bí mật.
	cleanup := func() {
		removed := os.RemoveAll(profile) == nil
		if req.LogPath != "" {
			b, _ := json.MarshalIndent(map[string]any{"isolated": true, "runtime_keys_retained": retained, "user_admin_skill_rules": len(disabled), "bundled_skills": false, "copied_user_instructions": false, "copied_user_plugins": false, "copied_user_hooks": false, "profile_removed": removed}, "", "  ")
			os.WriteFile(filepath.Join(filepath.Dir(req.LogPath), "codex-profile.json"), append(b, '\n'), 0o600)
		}
	}
	ready = true
	return env, cleanup, nil
}
