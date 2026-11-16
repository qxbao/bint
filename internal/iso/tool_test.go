package iso

import (
	"strings"
	"testing"

	"bint/internal/config"
)

func TestValidName(t *testing.T) {
	ok := []string{"default", "personal", "a", "A-b_c.d", "x--y", ".hidden", "..x", strings.Repeat("a", 64), "1"}
	bad := []string{"", ".", "..", strings.Repeat("a", 65), "a/b", `a\b`, "a b", "é", "a:b", "a\x00b", "a\n", "../x"}
	for _, n := range ok {
		if err := ValidName(n); err != nil {
			t.Errorf("ValidName(%q) = %v, muốn hợp lệ", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidName(n); err == nil {
			t.Errorf("ValidName(%q) hợp lệ, muốn lỗi", n)
		}
	}
}

func TestParseTool(t *testing.T) {
	for _, s := range []string{"claude", "codex"} {
		if got, err := ParseTool(s); err != nil || string(got) != s {
			t.Errorf("ParseTool(%q) = %q, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "Claude", "agy", "antigravity", "gemini", "claude "} {
		if _, err := ParseTool(s); err == nil {
			t.Errorf("ParseTool(%q) phải lỗi (Agy để đợt sau)", s)
		}
	}
}

func TestParseDirName(t *testing.T) {
	cases := []struct {
		dir  string
		tool Tool
		name string
		ok   bool
	}{
		{"claude--default", Claude, "default", true},
		{"codex--a--b", Codex, "a--b", true},
		{"codex--", "", "", false},
		{"codex", "", "", false},
		{"agy--x", "", "", false},
		{"codex--..", "", "", false},
		{"README", "", "", false},
	}
	for _, c := range cases {
		tool, name, err := parseDirName(c.dir)
		if (err == nil) != c.ok || tool != c.tool || name != c.name {
			t.Errorf("parseDirName(%q) = %q,%q,%v", c.dir, tool, name, err)
		}
	}
	if dirName(Codex, "a--b") != "codex--a--b" {
		t.Error("dirName")
	}
}

// config.ValidProfileName là bản chép của ValidName (iso import config nên không gọi ngược được): hai bản phải khớp.
func TestConfigProfileNameMatchesValidName(t *testing.T) {
	for _, n := range []string{"default", "a", "A-b_c.d", "x--y", ".hidden", "..x", strings.Repeat("a", 64), "", ".", "..",
		strings.Repeat("a", 65), "a/b", `a\b`, "a b", "é", "a:b", "a\x00b", "a\n", "../x"} {
		if got, want := config.ValidProfileName(n), ValidName(n) == nil; got != want {
			t.Errorf("ValidProfileName(%q) = %v, ValidName cho %v", n, got, want)
		}
	}
}
