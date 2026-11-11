package text

import (
	"strings"
	"testing"
	"time"
)

func TestWrapHangIndentsContinuationAndKeepsCodeWhole(t *testing.T) {
	s := "Không cài hook khi khởi tạo không hỏi; cài sau bằng `bint init --hooks` nhé."
	for w := 20; w <= 60; w++ {
		got := WrapHang(s, w, "  • ", "    ")
		for i, l := range got {
			if Width(l) > w {
				t.Fatalf("w=%d dòng %d rộng %d: %q", w, i, Width(l), l)
			}
			want := "    "
			if i == 0 {
				want = "  • "
			}
			if !strings.HasPrefix(l, want) {
				t.Fatalf("w=%d dòng %d thiếu tiền tố %q: %q", w, i, want, l)
			}
		}
		joined := strings.Join(got, "\n")
		if w >= len("    `bint init --hooks`") && !strings.Contains(joined, "`bint init --hooks`") {
			t.Fatalf("w=%d: đoạn mã bị ngắt:\n%s", w, joined)
		}
	}
}

func TestWrapHangHardCutsLongWord(t *testing.T) {
	got := WrapHang(strings.Repeat("x", 25), 10, "- ", "  ")
	for _, l := range got {
		if Width(l) > 10 {
			t.Fatalf("dòng quá rộng: %q", l)
		}
	}
	if strings.ReplaceAll(strings.Join(got, ""), " ", "") != "-"+strings.Repeat("x", 25) {
		t.Fatalf("mất chữ: %q", got)
	}
}

func TestWrapHangUnbalancedBacktick(t *testing.T) {
	got := WrapHang("một `hai ba bốn năm sáu", 12, "", "")
	if len(got) < 2 {
		t.Fatalf("dấu huyền không đóng phải ngắt như chữ thường: %q", got)
	}
}

func TestTildeRespectsComponentBoundary(t *testing.T) {
	t.Setenv("HOME", "/home/ab")
	t.Setenv("USERPROFILE", "/home/ab")
	for in, want := range map[string]string{
		"/home/ab":         "~",
		"/home/ab/x/y":     "~/x/y",
		"/home/abc/x":      "/home/abc/x",
		"/mnt/other":       "/mnt/other",
		"relative/home/ab": "relative/home/ab",
	} {
		if got := Tilde(in); got != want {
			t.Errorf("Tilde(%q) = %q, muốn %q", in, got, want)
		}
	}
}

func TestPathShortensHeadWithEllipsis(t *testing.T) {
	t.Setenv("HOME", "/home/ab")
	t.Setenv("USERPROFILE", "/home/ab")
	if got := Path("/home/ab/proj", 40); got != "~/proj" {
		t.Fatalf("got %q", got)
	}
	long := "/mnt/01DCE9A4D6522BD0/Project/Go/bint/internal/tui"
	for w := 5; w < len(long); w++ {
		got := Path(long, w)
		if Width(got) > w {
			t.Fatalf("w=%d: %q rộng %d", w, got, Width(got))
		}
		if !strings.HasPrefix(got, "…") {
			t.Fatalf("w=%d: thiếu … ở đầu: %q", w, got)
		}
		if !strings.HasSuffix(long, strings.TrimPrefix(got, "…")) {
			t.Fatalf("w=%d: %q không phải đuôi của đường dẫn", w, got)
		}
	}
	if got := Path(long, 20); got != "…/bint/internal/tui" {
		t.Fatalf("nên bắt đầu ở ranh giới thư mục: %q", got)
	}
}

// Ký tự rộng 2 ô với chỗ trống 1 ô: trước đây lặp vô hạn (Truncate trả rỗng). Phải kết thúc, không
// mất chữ, và mỗi dòng chứa ít nhất một ký tự.
func TestWrapHangWideRuneNarrowRoom(t *testing.T) {
	for _, s := range []string{"漢字漢字", "ab 漢字 cd", "😀😀😀"} {
		for w := 1; w <= 6; w++ {
			done := make(chan []string, 1)
			go func() { done <- WrapHang(s, w, "  ", "  ") }()
			select {
			case got := <-done:
				if len(got) > 4*len([]rune(s)) {
					t.Fatalf("s=%q w=%d: số dòng bất thường %d", s, w, len(got))
				}
				joined := strings.ReplaceAll(strings.Join(got, ""), " ", "")
				if want := strings.ReplaceAll(s, " ", ""); joined != want {
					t.Errorf("s=%q w=%d: mất chữ: %q", s, w, joined)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("s=%q w=%d: WrapHang không kết thúc", s, w)
			}
		}
	}
}

// Từ có mã ANSI (ký tự rộng lẫn ASCII): trước đây ansi.Truncate chép mã thoát sau điểm cắt nên
// TrimPrefix không bỏ được gì và vòng lặp không bao giờ kết thúc. Phải kết thúc, không mất chữ lẫn mã.
func TestWrapHangAnsiTerminates(t *testing.T) {
	const on, off = "\x1b[1m", "\x1b[0m"
	for _, s := range []string{
		on + "漢字漢字" + off,
		on + "xxxxxxxxxxxx" + off,
		"ab " + on + "漢字xyz" + off + " cd",
		"\x1b[38;5;196m" + "😀😀😀" + off,
	} {
		for w := 1; w <= 8; w++ {
			done := make(chan []string, 1)
			go func() { done <- WrapHang(s, w, "  ", "  ") }()
			select {
			case got := <-done:
				if len(got) > 4*len(s) {
					t.Fatalf("s=%q w=%d: số dòng bất thường %d", s, w, len(got))
				}
				joined := strings.ReplaceAll(strings.Join(got, ""), " ", "")
				if want := strings.ReplaceAll(s, " ", ""); joined != want {
					t.Errorf("s=%q w=%d: mất chữ hoặc mã: %q", s, w, joined)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("s=%q w=%d: WrapHang không kết thúc", s, w)
			}
		}
	}
}

func TestTildeIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, c := range []struct{ in, want string }{
		{home + "/.local/state/bint/x (schema v6)", "~/.local/state/bint/x (schema v6)"},
		{"đã ghi " + home + "/a/b và \"" + home + "/c\"", "đã ghi ~/a/b và \"~/c\""},
		{"home=" + home, "home=~"},
		{home + "x/y", home + "x/y"},                 // /home/a không khớp /home/ab
		{"/mnt" + home + "/y", "/mnt" + home + "/y"}, // home giữa đường dẫn khác: không đổi
		{"không có đường dẫn", "không có đường dẫn"},
	} {
		if got := TildeIn(c.in); got != c.want {
			t.Errorf("TildeIn(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}
