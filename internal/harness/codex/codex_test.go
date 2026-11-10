package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionEndTimeoutMatchesRealCodex(t *testing.T) {
	// Codex 0.167 thực tế cảnh báo và ép timeout SessionEnd xuống 3s.
	raw, err := os.ReadFile("testdata/codex-0.167/session-end-timeout.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "clamping SessionEnd hook timeout to 3s") {
		t.Fatal("fixture không có cảnh báo thật")
	}
	path := filepath.Join(t.TempDir(), "hooks.json")
	if _, err := InstallHooks(path, "bint", t.TempDir(), time.Now()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout int `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	for _, g := range f.Hooks["SessionEnd"] {
		for _, h := range g.Hooks {
			if h.Timeout < 1 || h.Timeout > 3 {
				t.Fatalf("SessionEnd timeout=%d: Codex chỉ cho phép tối đa 3s", h.Timeout)
			}
		}
	}
}
