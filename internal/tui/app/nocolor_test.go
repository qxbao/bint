package app

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	bintapp "bint/internal/app"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

var sgrRE = regexp.MustCompile("\x1b\\[([0-9;:]*)m")

// colorCodes trả các mã SGR đặt màu (30–37, 38, 40–47, 48, 90–97, 100–107 và dạng 38;5;n / 38;2;r;g;b) trong s. Đậm, mờ, gạch chân, đảo màu và
// các mã đặt lại (0, 22, 24, 27, 39, 49) không phải màu nên được phép ở chế độ đơn sắc.
func colorCodes(s string) []string {
	var out []string
	for _, m := range sgrRE.FindAllStringSubmatch(s, -1) {
		for _, tok := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' }) {
			n, err := strconv.Atoi(tok)
			if err != nil {
				continue
			}
			if n >= 30 && n <= 38 || n >= 40 && n <= 48 || n >= 90 && n <= 97 || n >= 100 && n <= 107 {
				out = append(out, fmt.Sprintf("ESC[%sm", m[1]))
				break
			}
		}
	}
	return out
}

// NO_COLOR (chế độ Mono): Render() của mọi kịch bản golden (màn chính và Hệ thống) không chứa mã màu — chỉ còn đậm/đảo màu/gạch chân.
func TestMonoRenderHasNoColorCodes(t *testing.T) {
	if len(colorCodes("\x1b[38;2;1;2;3mx\x1b[0m \x1b[31mx\x1b[39m")) != 2 || len(colorCodes("\x1b[1mx\x1b[7m\x1b[22m\x1b[0m\x1b[39m")) != 0 {
		t.Fatal("bộ dò mã màu hỏng")
	}
	n := 0
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {140, 40}} {
		w, h := sz[0], sz[1]
		for _, lang := range []string{"vi", "en", "xx"} {
			for name, build := range allScenarios(t, lang, w, h) {
				n++
				if bad := colorCodes(build().Render()); len(bad) > 0 {
					t.Errorf("%s %dx%d %s: Render() ở Mono có mã màu %q", name, w, h, lang, bad[:min(len(bad), 3)])
				}
			}
		}
	}
	if n < 100 {
		t.Fatalf("quét quá ít kịch bản: %d", n)
	}
}

// sgrLeak trả kiểu (đậm, mờ, gạch chân, đảo màu) còn bật ở cuối s theo các mã SGR; rỗng nghĩa là mọi kiểu đã được tắt/đặt lại.
func sgrLeak(s string) string {
	on := map[int]bool{}
	for _, m := range sgrRE.FindAllStringSubmatch(s, -1) {
		if m[1] == "" {
			on = map[int]bool{}
			continue
		}
		for _, tok := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' }) {
			switch n, _ := strconv.Atoi(tok); n {
			case 0:
				on = map[int]bool{}
			case 1, 2, 4, 7:
				on[n] = true
			case 22:
				delete(on, 1)
				delete(on, 2)
			case 24:
				delete(on, 4)
			case 27:
				delete(on, 7)
			}
		}
	}
	var out []string
	for _, n := range []int{1, 2, 4, 7} {
		if on[n] {
			out = append(out, strconv.Itoa(n))
		}
	}
	return strings.Join(out, ",")
}

// Phát hiện review .1: NoColor từng xoá ESC[m (đặt lại không tham số) nên đậm/đảo màu tràn sang phần sau của màn hình ở Mono.
func TestMonoRenderDoesNotLeakAttributes(t *testing.T) {
	if sgrLeak("\x1b[7m\x1b[1mx\x1b[m") != "" || sgrLeak("\x1b[1mx") != "1" || sgrLeak("\x1b[7mx\x1b[27m") != "" {
		t.Fatal("bộ dò kiểu tràn hỏng")
	}
	n := 0
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {140, 40}} {
		w, h := sz[0], sz[1]
		for _, lang := range []string{"vi", "en"} {
			for name, build := range allScenarios(t, lang, w, h) {
				n++
				if leak := sgrLeak(build().Render()); leak != "" {
					t.Errorf("%s %dx%d %s: Render() ở Mono để tràn kiểu SGR %s", name, w, h, lang, leak)
				}
			}
		}
	}
	if n < 60 {
		t.Fatalf("quét quá ít kịch bản: %d", n)
	}
}

// Nội dung ngoài mang mã màu riêng (log của agent, trích đoạn tệp) bị bỏ màu ở Mono nhưng giữ chữ; chế độ có màu giữ nguyên.
func TestMonoStripsExternalContentColor(t *testing.T) {
	det := func(ctx context.Context, dir, id string) (bintapp.KnowledgeDetail, error) {
		d, err := sampleDetail(ctx, dir, id)
		if err == nil && len(d.Evidence) > 0 {
			d.Evidence[0].Lines = []string{"\x1b[31m  40  func Open() {\x1b[0m", "\x1b[1;32m  41  \treturn nil\x1b[0m"}
		}
		return d, err
	}
	open := func(mode theme.Mode) *Model {
		m := newModel(t, Options{Mode: mode, Lang: "vi", KnowledgeDetailer: det}, sampleData(), 140, 40)
		press(t, m, "2", "j", "enter")
		return m
	}
	mono := open(theme.Mono)
	if bad := colorCodes(mono.Render()); len(bad) > 0 {
		t.Fatalf("nội dung ngoài còn mã màu ở Mono: %q", bad)
	}
	if out := plain(mono.Render()); !strings.Contains(out, "func Open() {") || !strings.Contains(out, "return nil") {
		t.Fatalf("bỏ màu không được làm mất chữ:\n%s", out)
	}
	if raw := open(theme.Dark).Render(); !strings.Contains(raw, "\x1b[31m") {
		t.Fatal("chế độ có màu phải giữ màu của nội dung (đối chứng của test)")
	}
}

// Ở Mono không thông tin nào chỉ dựa vào màu: khung đang focus khác khung thường, toast lỗi khác toast thường, hành động bị khoá có dấu bằng chữ.
func TestMonoNothingReliesOnColorAlone(t *testing.T) {
	// 1. Khung focus: đổi focus giữa danh sách và chi tiết thì bản vẽ thô (có kiểu chữ) khác nhau dù không có màu nào.
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 140, 40)
	press(t, m, "2")
	listFocus := m.Render()
	press(t, m, "tab")
	detailFocus := m.Render()
	if listFocus == detailFocus {
		t.Fatal("khung đang focus phải khác khung thường ở Mono (kiểu chữ), không chỉ nhờ màu viền")
	}
	if len(colorCodes(listFocus))+len(colorCodes(detailFocus)) != 0 {
		t.Fatal("sự khác biệt focus không được dùng mã màu")
	}
	// 2. Toast lỗi khác toast thường bằng chữ.
	th := theme.New(theme.Mono)
	errT, okT := plain(ui.ToastLabel(th, "hỏng rồi", true, 60)), plain(ui.ToastLabel(th, "hỏng rồi", false, 60))
	if errT == okT || !strings.Contains(errT, "✗") || strings.Contains(okT, "✗") {
		t.Fatalf("toast lỗi %q phải khác toast thường %q bằng chữ", errT, okT)
	}
	// 3. Hành động bị khoá (phiên agent) có dấu bằng chữ trong bảng lệnh và trợ giúp.
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
	locked := newModel(t, Options{Mode: theme.Mono, Lang: "vi", Agent: true}, sampleData(), 140, 40)
	press(t, locked, "3", ":")
	for _, c := range "duyệt" {
		press(t, locked, string(c))
	}
	if out := plain(locked.Render()); !strings.Contains(out, "chỉ dành cho người") {
		t.Fatalf("bảng lệnh phải đánh dấu hành động khoá bằng chữ:\n%s", out)
	}
	press(t, locked, "esc", "?")
	if out := helpAll(t, locked); !strings.Contains(out, "(chỉ dành cho người)") {
		t.Fatalf("trợ giúp phải đánh dấu hành động khoá bằng chữ:\n%s", out)
	}
}
