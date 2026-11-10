package eval

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestCodexEvalIsolatesHomeAndRemovesCredentials(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub POSIX, không chạy model")
	}
	home := t.TempDir()
	source := filepath.Join(home, ".codex")
	os.MkdirAll(source, 0o700)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", source)
	os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"fixture-not-a-real-key"}`), 0o600)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("model = 'gpt-probe'\nnotify = ['USER_ONLY']\n[plugins.USER_ONLY]\nenabled = true\n"), 0o600)
	os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("USER_ONLY"), 0o600)
	bin := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"%s\"}}\\n' \"$CODEX_HOME\"\n"), 0o755)
	r := (&CodexAgent{Binary: bin}).Run(context.Background(), AgentRequest{Dir: t.TempDir(), Arm: ArmNone, Prompt: "probe", Timeout: 5 * time.Second})
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	if r.Text == source || r.Text == "" {
		t.Fatalf("eval vẫn dùng Codex home của người dùng: %q", r.Text)
	}
	if _, err := os.Stat(r.Text); !os.IsNotExist(err) {
		t.Fatalf("chưa xoá profile chứa auth: %q %v", r.Text, err)
	}
	if b, _ := os.ReadFile(filepath.Join(source, "auth.json")); !strings.Contains(string(b), "fixture-not-a-real-key") {
		t.Fatal("đã sửa auth gốc")
	}
}

func TestCodexProfileKeepsRuntimeWithoutUserContext(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, ".codex")
	os.MkdirAll(source, 0o700)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", source)
	auth := []byte(`{"tokens":{"access_token":"fixture-only"}}`)
	os.WriteFile(filepath.Join(source, "auth.json"), auth, 0o600)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("model='gpt-probe'\nmodel_provider='probe'\nnotify=['USER_ONLY']\n[model_providers.probe]\nbase_url='http://localhost:1/v1'\n[plugins.USER_ONLY]\nenabled=true\n[hooks]\nstate={}\n"), 0o600)
	skill := filepath.Join(home, ".agents", "skills", "user-canary", "SKILL.md")
	os.MkdirAll(filepath.Dir(skill), 0o700)
	os.WriteFile(skill, []byte("user canary"), 0o600)
	linked := filepath.Join(home, ".agents", "skills", "linked", "SKILL.md")
	os.MkdirAll(filepath.Dir(linked), 0o700)
	target := filepath.Join(home, "instructions.md")
	os.WriteFile(target, []byte("linked user canary"), 0o600)
	linkCreated := os.Symlink(target, linked) == nil
	os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("USER_ONLY"), 0o600)
	env, cleanup, err := codexEvalProfile(AgentRequest{Dir: t.TempDir()}, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	profile := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "CODEX_HOME=") {
			profile = strings.TrimPrefix(kv, "CODEX_HOME=")
		}
	}
	var cfg map[string]any
	b, _ := os.ReadFile(filepath.Join(profile, "config.toml"))
	if err := toml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["model"] != "gpt-probe" || cfg["model_provider"] != "probe" || cfg["model_providers"] == nil {
		t.Fatal("mất runtime/model provider")
	}
	for _, key := range []string{"notify", "plugins", "hooks"} {
		if cfg[key] != nil {
			t.Errorf("đã mang context user %s sang eval", key)
		}
	}
	if _, err := os.Stat(filepath.Join(profile, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("đã chép AGENTS người dùng")
	}
	if info, err := os.Stat(filepath.Join(profile, "auth.json")); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("auth permissions: %v %v", info, err)
	}
	sk := cfg["skills"].(map[string]any)
	rules := sk["config"].([]any)
	found := false
	for _, r := range rules {
		m := r.(map[string]any)
		if m["path"] == skill && m["enabled"] == false {
			found = true
		}
	}
	if !found {
		t.Fatal("HOME/.agents user skill chưa bị tắt")
	}
	if linkCreated {
		for _, want := range []string{linked, target} {
			found = false
			for _, r := range rules {
				m := r.(map[string]any)
				if m["path"] == want && m["enabled"] == false {
					found = true
				}
			}
			if !found {
				t.Errorf("bỏ sót symlink skill %s", want)
			}
		}
	}
}

func TestCodexEvalProfileRemovedAfterTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub POSIX")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := filepath.Join(home, ".codex")
	os.MkdirAll(source, 0o700)
	t.Setenv("CODEX_HOME", source)
	os.WriteFile(filepath.Join(source, "auth.json"), []byte(`{"tokens":{"access_token":"fixture-only"}}`), 0o600)
	bin := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"%s\"}}\\n' \"$CODEX_HOME\"\nsleep 60\n"), 0o755)
	r := (&CodexAgent{Binary: bin}).Run(context.Background(), AgentRequest{Dir: t.TempDir(), Arm: ArmNone, Prompt: "probe", Timeout: 5 * time.Second})
	if !r.TimedOut || r.Text == "" || r.Text == source {
		t.Fatalf("không tái hiện timeout trong profile riêng: %+v", r)
	}
	if _, err := os.Stat(r.Text); !os.IsNotExist(err) {
		t.Fatalf("auth profile chưa dọn sau timeout: %v", err)
	}
}
