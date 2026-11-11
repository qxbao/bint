package logview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

func secs(n int) []Section {
	var a, b []string
	for i := 0; i < n; i++ {
		a = append(a, "điều phối "+string(rune('A'+i%26)))
		b = append(b, "agent "+string(rune('a'+i%26)))
	}
	return []Section{{Title: "Điều phối", Lines: a}, {Title: "Bước b-feat", Lines: b}}
}

func TestFollowShowsNewestAndHeight(t *testing.T) {
	th := theme.New(theme.Mono)
	m := New()
	out := m.Render(th, secs(30), 40, 8)
	ls := strings.Split(ansi.Strip(out), "\n")
	if len(ls) != 8 {
		t.Fatalf("phải đúng 8 dòng: %d", len(ls))
	}
	if !strings.HasPrefix(ls[7], "agent ") {
		t.Fatalf("dòng cuối phải là dòng mới nhất của đoạn cuối: %q", ls[7])
	}
	for _, l := range strings.Split(out, "\n") {
		if text.Width(l) != 40 {
			t.Fatalf("dòng không rộng đúng 40: %d", text.Width(l))
		}
	}
}

func TestScrollUpStopsFollowAndBottomResumes(t *testing.T) {
	th := theme.New(theme.Mono)
	m := New()
	s := secs(30)
	total := Total(s)
	m.Render(th, s, 40, 8)
	m.Scroll(-3, total, 8)
	if m.Follow {
		t.Fatal("cuộn lên phải thôi bám theo")
	}
	before := ansi.Strip(m.Render(th, s, 40, 8))
	// Dòng mới không làm khung đang xem nhảy.
	s2 := secs(31)
	after := ansi.Strip(m.Render(th, s2, 40, 8))
	if before == "" || before != after && !strings.Contains(after, "agent") {
		t.Fatalf("khung không được nhảy khi không bám theo:\n%s\n---\n%s", before, after)
	}
	m.Scroll(100, Total(s2), 8)
	if !m.Follow {
		t.Fatal("cuộn tới đáy phải bám theo lại")
	}
}

func TestTruncatesWideLines(t *testing.T) {
	th := theme.New(theme.Mono)
	m := New()
	out := m.Render(th, []Section{{Lines: []string{strings.Repeat("Nguyễn ", 20)}}}, 20, 3)
	for _, l := range strings.Split(out, "\n") {
		if text.Width(l) > 20 {
			t.Fatalf("vượt độ rộng: %q", l)
		}
	}
}
