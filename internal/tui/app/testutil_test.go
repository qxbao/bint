package app

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// sampleData là dữ liệu Tổng quan cố định cho kiểm thử và ảnh chụp.
func sampleData() overview.Data {
	return overview.Data{
		Status: &bintapp.StatusReport{
			Repo: "/work/demo", RepoID: "7f3a9c", StateDir: "/state/demo-7f3a9c", Schema: 6,
			Harnesses: []string{"claude", "codex"}, Runs: map[string]int{"done": 38, "failed": 3, "interrupted": 1}, TotalRuns: 42,
			PendingProposals: 2,
		},
		Doctor: &bintapp.DoctorReport{Checks: []bintapp.Check{
			{Name: "git", Level: bintapp.LevelOK, Detail: "git version 2.45"},
			{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"},
			{Name: "hook claude", Level: bintapp.LevelOK, Detail: "đã cài"},
		}},
		Candidates: 3,
	}
}

func notInitData() overview.Data { return overview.Data{NotInit: true} }

// newModel dựng mô hình với loader giả và đã nạp xong dữ liệu.
func newModel(t *testing.T, o Options, data overview.Data, w, h int) *Model {
	t.Helper()
	if o.Dir == "" {
		o.Dir = "/work/demo"
	}
	if o.Version == "" {
		o.Version = "0.1.0"
	}
	if o.Lang == "" {
		o.Lang = "vi"
	}
	o.Loader = func(context.Context, string) (overview.Data, error) { return data, nil }
	o.SpinnerFPS = time.Millisecond
	m := New(context.Background(), o)
	m.SetSize(w, h)
	drive(t, m, m.Init())
	return m
}

// drive chạy tea.Cmd (đồng bộ) và đưa thông điệp kết quả vào Update, tới khi hết lệnh.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				drive(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
	}
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		drive(t, m, cmd)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func plain(s string) string { return ansi.Strip(s) }

// lines tách đầu ra thành các dòng (đã bỏ mã ANSI).
func lines(s string) []string { return strings.Split(plain(s), "\n") }

func allModes() []theme.Mode { return theme.Modes }
