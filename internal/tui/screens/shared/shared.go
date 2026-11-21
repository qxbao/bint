// Package shared chứa hàm nhỏ dùng chung của các màn hình chuyên biệt (rút gọn ID, bỏ mã màu, định dạng thời gian).
package shared

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"bint/internal/i18n"
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

// Now là đồng hồ của Ago (kiểm thử golden đặt cố định).
var Now = time.Now

// Person bỏ phần " <email>" của "Tên <email>" (khung chi tiết chỉ cần tên; email nằm trong tệp/Git).
func Person(s string) string {
	if i := strings.Index(s, " <"); i > 0 && strings.HasSuffix(strings.TrimSpace(s), ">") {
		return s[:i]
	}
	return s
}

// ParseTime đọc thời điểm ở các dạng Bint lưu (cơ sở dữ liệu, RFC 3339, ngày).
func ParseTime(s string) (time.Time, bool) {
	for _, f := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Ago là thời gian tương đối tới Now ("3 giờ trước") trong 30 ngày; xa hơn thì ngày địa phương. Không đọc được thì trả nguyên s; rỗng → "-".
// cat là danh mục chuỗi (khoá ago.*).
func Ago(cat Catalog, s string) string {
	if s == "" {
		return "-"
	}
	t, ok := ParseTime(s)
	if !ok {
		return s
	}
	d := Now().Sub(t)
	switch {
	case d < time.Minute:
		return cat.T("ago.now")
	case d < time.Hour:
		return cat.N("ago.minutes", int(d/time.Minute))
	case d < 48*time.Hour:
		return cat.N("ago.hours", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return cat.N("ago.days", int(d/(24*time.Hour)))
	}
	return t.Local().Format("2006-01-02")
}

// Catalog là phần danh mục chuỗi mà Ago cần (i18n.Catalog thoả).
type Catalog interface {
	T(key string, args ...i18n.A) string
	N(key string, count int, args ...i18n.A) string
}
