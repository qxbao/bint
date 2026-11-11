package input

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func k(s string) tea.KeyPressMsg {
	switch s {
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestInputEditingWithAccents(t *testing.T) {
	m := New("")
	for _, c := range "Việt" {
		m.Update(k(string(c)))
	}
	m.Update(k("left"))
	m.Update(k("backspace"))
	if m.Text != "Vit" || m.Cur != 2 {
		t.Fatalf("%q cur=%d", m.Text, m.Cur)
	}
	m.Update(k("home"))
	m.Update(k("A"))
	m.Update(k("end"))
	m.Update(k("!"))
	if m.Text != "AVit!" {
		t.Fatalf("%q", m.Text)
	}
	m.Update(k("ctrl+u"))
	if m.Text != "" {
		t.Fatalf("ctrl+u: %q", m.Text)
	}
	if m.Update(k("enter")) {
		t.Fatal("Enter không thuộc ô một dòng")
	}
	m.Multiline = true
	m.Set("a")
	if !m.Update(k("enter")) || m.Text != "a\n" {
		t.Fatalf("%q", m.Text)
	}
}

func TestInputGhostAcceptedWithRight(t *testing.T) {
	m := New("internal/d")
	m.Ghost = func(s string) string {
		if strings.HasPrefix("internal/db/db.go", s) {
			return "internal/db/db.go"[len(s):]
		}
		return ""
	}
	th := theme.New(theme.Mono)
	if got := m.Line(th, 40, true); !strings.Contains(got, "b/db.go") || text.Width(got) != 40 {
		t.Fatalf("%q", got)
	}
	m.Update(k("right"))
	if m.Text != "internal/db/db.go" {
		t.Fatalf("%q", m.Text)
	}
}

func TestInputLineScrollsToKeepCursorVisible(t *testing.T) {
	m := New(strings.Repeat("Ắ", 50))
	th := theme.New(theme.Mono)
	got := m.Line(th, 12, true)
	if text.Width(got) != 12 || !strings.Contains(got, Cursor) {
		t.Fatalf("%q", got)
	}
	for _, l := range m.Lines(th, 10, 3, true) {
		if text.Width(l) != 10 {
			t.Fatalf("%q", l)
		}
	}
}
