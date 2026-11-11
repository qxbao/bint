// Package shared chứa hàm nhỏ dùng chung của các màn hình chuyên biệt (rút gọn ID, bỏ mã màu, định dạng thời gian).
package shared

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
)

// Short rút ID dài thành 10 ký tự cuối (phần phân biệt của ULID).
func Short(id string) string {
	if r := []rune(id); len(r) > 10 {
		return string(r[len(r)-10:])
	}
	return id
}

// StripColor bỏ mã màu để dòng được chọn chỉ mang kiểu "được chọn".
func StripColor(th theme.Theme, s string) string {
	if !th.Colored() {
		return s
	}
	return ansi.Strip(s)
}

// Time định dạng thời điểm lưu ở cơ sở dữ liệu ("2006-01-02T15:04:05.000Z") theo giờ địa phương; rỗng → "-".
func Time(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return s
	}
	return t.Local().Format("2006-01-02 15:04")
}

// Dash trả "-" cho chuỗi rỗng.
func Dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// TimeShort như Time nhưng gọn cho cột danh sách ("01-02 15:04").
func TimeShort(s string) string {
	if s == "" {
		return "-"
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", s)
	if err != nil {
		return s
	}
	return t.Local().Format("01-02 15:04")
}

// KV dựng dòng "nhãn  giá trị" với cột nhãn rộng lw ô (nhãn dài hơn bị cắt) và giá trị ngắt dòng theo độ rộng w:
// các dòng tiếp theo thụt vào đúng cột giá trị. Khoảng trắng liền nhau trong giá trị không được giữ.
func KV(th theme.Theme, lw, w int, label, val string) []string {
	label = text.Pad(text.Truncate(label, max(lw-1, 1)), lw)
	vals := text.Wrap(val, max(w-lw, 8))
	if len(vals) == 0 {
		vals = []string{""}
	}
	out := make([]string, 0, len(vals))
	for i, v := range vals {
		if i == 0 {
			out = append(out, th.S(theme.Accent, label)+v)
		} else {
			out = append(out, strings.Repeat(" ", lw)+v)
		}
	}
	return out
}
