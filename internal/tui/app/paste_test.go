package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/clipboard"
)

func pasteMsg(s string) tea.PasteMsg { return tea.PasteMsg{Content: s} }

func send(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(msg)
	drive(t, m, cmd)
	return cmd
}

func TestPasteIntoPaletteInsertsSingleLine(t *testing.T) {
	m := mouseModel(t, 120, 30)
	press(t, m, ":")
	if m.pal == nil {
		t.Fatal("bảng lệnh không mở")
	}
	send(t, m, pasteMsg("ngôn\r\nngữ\x1b[31m"))
	if m.pal.Query != "ngôn ngữ[31m" {
		t.Fatalf("Query = %q", m.pal.Query)
	}
}

func TestPasteIntoScreenFilter(t *testing.T) {
	m := mouseModel(t, 140, 30, "3")
	press(t, m, "/")
	send(t, m, pasteMsg("Viết\ntài"))
	out := plain(m.Render())
	if !strings.Contains(out, "Viết tài") {
		t.Fatalf("ô lọc không nhận chữ dán:\n%s", out)
	}
}

func TestPasteIntoFormKeepsKeywordsAsText(t *testing.T) {
	m := mouseModel(t, 140, 30, "3")
	press(t, m, "n")
	if !m.tk.Capturing() {
		t.Fatal("form không mở")
	}
	send(t, m, pasteMsg("esc"))
	if !m.tk.Capturing() {
		t.Fatal("dán chuỗi esc không được đóng form")
	}
	if !strings.Contains(plain(m.Render()), "esc") {
		t.Fatalf("chữ dán không vào ô:\n%s", plain(m.Render()))
	}
	send(t, m, pasteMsg("enter"))
	if !m.tk.Capturing() {
		t.Fatal("dán chuỗi enter không được gửi form")
	}
}

func TestPasteWithoutInputIsInert(t *testing.T) {
	for _, s := range []string{"q", "1", "2", ":", "?", "!", "L", "esc"} {
		m := mouseModel(t, 120, 30)
		tab := m.tab
		cmd := send(t, m, pasteMsg(s))
		if cmd != nil && cmd() != nil {
			t.Errorf("dán %q: có Cmd", s)
		}
		if m.tab != tab || m.quitting || m.pal != nil || m.showHelp || m.exitDlg != nil || m.notes.dlg != nil {
			t.Errorf("dán %q làm đổi trạng thái", s)
		}
	}
	// màn có danh sách nhưng ô lọc chưa mở
	m := mouseModel(t, 140, 30, "3")
	send(t, m, pasteMsg("q"))
	if m.quitting || m.tk.Capturing() {
		t.Fatal("dán vào màn không nhận phím phải bị bỏ")
	}
}

func TestPasteIgnoredWhileHelpOpen(t *testing.T) {
	m := mouseModel(t, 120, 30)
	press(t, m, "?")
	send(t, m, pasteMsg("q"))
	if !m.showHelp || m.quitting {
		t.Fatal("dán khi trợ giúp mở phải bị bỏ")
	}
}

func TestCtrlVReadsClipboard(t *testing.T) {
	fake := &clipboard.Fake{Text: "từ clipboard"}
	m := mouseModel(t, 120, 30)
	m.clip = fake
	press(t, m, ":")
	press(t, m, "ctrl+v")
	if fake.Reads != 1 || m.pal.Query != "từ clipboard" {
		t.Fatalf("reads=%d query=%q", fake.Reads, m.pal.Query)
	}
}

func TestCtrlVReadErrorToastsAndKeepsInput(t *testing.T) {
	fake := &clipboard.Fake{ReadErr: errors.New("no xclip")}
	m := mouseModel(t, 120, 30)
	m.clip = fake
	press(t, m, ":")
	press(t, m, "a", "ctrl+v")
	if m.pal.Query != "a" {
		t.Fatalf("ô đổi: %q", m.pal.Query)
	}
	if !m.toastErr || !strings.Contains(m.toast, "no xclip") {
		t.Fatalf("toast = %q err=%v", m.toast, m.toastErr)
	}
}

func TestCtrlVWithoutInputDoesNotRead(t *testing.T) {
	fake := &clipboard.Fake{Text: "x"}
	m := mouseModel(t, 120, 30)
	m.clip = fake
	press(t, m, "ctrl+v")
	if fake.Reads != 0 {
		t.Fatal("không có ô nhập thì không đọc clipboard")
	}
}

func TestHelpHasPasteAndSelectRows(t *testing.T) {
	for _, lang := range []string{"vi", "en"} {
		m := newModel(t, Options{Lang: lang}, sampleData(), 120, 40)
		press(t, m, "?")
		out := helpAll(t, m)
		for _, want := range []string{"Ctrl+V", "Shift+"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: trợ giúp thiếu %q", lang, want)
			}
		}
	}
}
