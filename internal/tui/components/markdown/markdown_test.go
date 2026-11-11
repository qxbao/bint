package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

const sample = "# Yêu cầu\n\n## Tiêu chí chấp nhận\n- [ ] AC-1: Màn Tác vụ hiển thị danh sách theo trạng thái và chi tiết theo thẻ con\n- [x] AC-2: Hành động duyệt\n- Mục danh sách thường\n\n1. Bước đầu tiên\n2. Bước thứ hai có `go test ./...` và **chữ đậm**\n\n```go\nfunc main() { fmt.Println(\"xin chào\") }\n```\n\n> Trích dẫn dài hơn một dòng để kiểm tra việc ngắt dòng theo độ rộng của khung\n\n---\nĐoạn văn thường có chữ tiếng Việt có dấu: Nguyễn Đặng Quảng, thử nghiệm ngắt dòng.\n"

func plainLines(w int) []string {
	var out []string
	for _, l := range Render(theme.New(theme.Mono), sample, w) {
		out = append(out, ansi.Strip(l))
	}
	return out
}

func TestRenderElements(t *testing.T) {
	got := strings.Join(plainLines(60), "\n")
	for _, want := range []string{"Yêu cầu", "Tiêu chí chấp nhận", "[ ] AC-1", "[x] AC-2", "• ", "1. Bước đầu tiên", "2. Bước thứ hai",
		"│ func main()", "┌ go", "▎ Trích dẫn", "go test ./...", "chữ đậm", "Nguyễn Đặng Quảng"} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q trong:\n%s", want, got)
		}
	}
	for _, bad := range []string{"**", "```", "# Yêu cầu", "## Tiêu"} {
		if strings.Contains(got, bad) {
			t.Errorf("dấu Markdown %q còn sót:\n%s", bad, got)
		}
	}
	// Tiêu đề cấp 1-2 có đường kẻ dưới.
	if !strings.Contains(got, "───") {
		t.Errorf("thiếu đường kẻ dưới tiêu đề:\n%s", got)
	}
}

// Mọi độ rộng: không dòng nào vượt w ô (chữ có dấu, khối mã dài, danh sách thụt treo).
func TestRenderNeverExceedsWidth(t *testing.T) {
	for _, w := range []int{8, 12, 20, 33, 40, 60, 100} {
		for i, l := range Render(theme.New(theme.Dark), sample, w) {
			if got := text.Width(l); got > w {
				t.Errorf("w=%d dòng %d rộng %d: %q", w, i, got, ansi.Strip(l))
			}
		}
	}
}

func TestChecklistWrapsHanging(t *testing.T) {
	ls := plainLines(30)
	var cont string
	for i, l := range ls {
		if strings.Contains(l, "[ ] AC-1") && i+1 < len(ls) {
			cont = ls[i+1]
		}
	}
	if !strings.HasPrefix(cont, "    ") {
		t.Fatalf("dòng gói của ô kiểm phải thụt treo: %q", cont)
	}
}

func TestEmptyAndCRLF(t *testing.T) {
	if got := Render(theme.New(theme.Mono), "", 20); len(got) != 0 {
		t.Fatalf("rỗng: %q", got)
	}
	got := Render(theme.New(theme.Mono), "# A\r\n\r\nđoạn\r\n", 20)
	if len(got) < 3 || strings.Contains(strings.Join(got, ""), "\r") {
		t.Fatalf("CRLF: %q", got)
	}
}
