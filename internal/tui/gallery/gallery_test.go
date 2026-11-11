package gallery

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

var update = flag.Bool("update", false, "ghi lại tệp golden")

func check(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("thiếu golden %s (chạy với -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("golden %s khác kết quả hiện tại (-update nếu chủ ý):\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// Không dòng nào vượt chiều rộng, ở mọi kích thước, chế độ màu và ngôn ngữ — kể cả locale giả kéo dài 40%.
func TestNoLineExceedsWidth(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {140, 40}} {
		for _, mode := range theme.Modes {
			for _, lang := range langs {
				m := New(Options{Lang: lang, Mode: mode})
				m.SetSize(sz[0], sz[1])
				// Duyệt hết các trang cuộn.
				for page := 0; page < 14; page++ {
					out := m.Render()
					ls := strings.Split(out, "\n")
					if len(ls) != sz[1] {
						t.Fatalf("%v %s %s: %d dòng", sz, mode, lang, len(ls))
					}
					for i, l := range ls {
						if w := text.Width(l); w > sz[0] {
							t.Fatalf("%v %s %s trang %d dòng %d rộng %d > %d: %q", sz, mode, lang, page, i, w, sz[0], ansi.Strip(l))
						}
					}
					m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
				}
			}
		}
	}
}

func TestGoldenGallery(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 60}} {
		for _, lang := range langs {
			m := New(Options{Lang: lang, Mode: theme.Mono})
			m.SetSize(sz[0], sz[1])
			check(t, fmt.Sprintf("gallery_%dx%d_%s.txt", sz[0], sz[1], lang), ansi.Strip(m.Render())+"\n")
		}
	}
}

func TestKeysCycleModeLanguageAndQuit(t *testing.T) {
	m := New(Options{Lang: "vi", Mode: theme.Dark})
	m.SetSize(100, 20)
	press := func(k string) tea.Cmd {
		r := []rune(k)
		_, cmd := m.Update(tea.KeyPressMsg{Code: r[0], Text: k})
		return cmd
	}
	press("m")
	if m.mode != theme.Light {
		t.Fatalf("mode = %v", m.mode)
	}
	for i := 0; i < 3; i++ {
		press("m")
	}
	if m.mode != theme.Dark {
		t.Fatal("m phải đi vòng qua bốn chế độ")
	}
	press("l")
	if langs[m.lang] != "en" {
		t.Fatalf("lang = %s", langs[m.lang])
	}
	press("l")
	if langs[m.lang] != "xx" {
		t.Fatal("phải qua locale giả")
	}
	press("l")
	if langs[m.lang] != "vi" {
		t.Fatal("vòng lại vi")
	}
	if cmd := press("q"); cmd == nil {
		t.Fatal("q phải thoát")
	}
	// Cuộn không vượt biên.
	press("k")
	if m.offset < 0 {
		t.Fatal("offset âm")
	}
	for i := 0; i < 200; i++ {
		press("j")
	}
	m.Render()
	if m.offset > 200 || m.offset < 1 {
		t.Fatalf("offset phải bị chặn: %d", m.offset)
	}
}

func TestShowsEveryComponent(t *testing.T) {
	m := New(Options{Lang: "en", Mode: theme.Mono})
	m.SetSize(120, 130)
	out := ansi.Strip(m.Render())
	for _, want := range []string{"Role palette", "Tab bar", "Footer by priority", "Frame with a title", "Logo", "Command palette", "██", "│Knowledge│",
		"Virtual list", "colored diff", "+- **Endpoints need /v1**", "Confirm dialog", "Reject 12 items?", "Form generated from Options", "Equivalent CLI command", "▸ Advanced"} {
		if !strings.Contains(out, want) {
			t.Errorf("trang trưng bày thiếu %q", want)
		}
	}
}
