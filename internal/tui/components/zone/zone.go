// Package zone đánh dấu vùng bấm chuột trong chuỗi đã vẽ. Component bọc phần tử bấm được bằng Mark; mô hình gốc vẽ
// xong thì Scan gỡ mọi dấu (đầu ra của Render không còn dấu nào) và trả danh sách vùng theo toạ độ ô, để đổi sự kiện
// chuột thành hành động. Dấu là chuỗi APC (ESC _ … ESC \): độ rộng bằng 0 nên mọi phép đo/cắt/đệm theo ô giữ nguyên,
// và text.Truncate/Cut/Overlay mang theo hoặc bỏ dấu cùng phần chữ bị cắt.
//
// Mark chỉ có tác dụng khi mô hình gốc đang vẽ (Begin … End): component vẽ riêng (gallery, kiểm thử component, golden)
// không có dấu nào nên đầu ra không đổi.
package zone

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	startPrefix = "\x1b_Zs;"
	endMark     = "\x1b_Ze\x1b\\"
	st          = "\x1b\\"
)

var active bool

// Begin bật việc chèn dấu (gọi ở đầu lần vẽ của mô hình gốc); End tắt. Mô hình Bubble Tea vẽ trong một goroutine nên
// cờ toàn cục này không bị đua.
func Begin() { active = true }

// End tắt việc chèn dấu.
func End() { active = false }

// Active cho biết đang trong một lần vẽ có thu vùng.
func Active() bool { return active }

// Mark bọc s bằng dấu vùng id (rỗng nếu không đang thu vùng, id rỗng hoặc s rỗng thì trả nguyên s). id không được chứa ESC.
func Mark(id, s string) string {
	if !active || id == "" || s == "" {
		return s
	}
	return startPrefix + id + st + s + endMark
}

// Zone là một vùng chữ nhật một dòng: dòng Y, cột [X0, X1).
type Zone struct {
	ID     string
	X0, X1 int
	Y      int
}

// Contains cho biết ô (x, y) nằm trong vùng.
func (z Zone) Contains(x, y int) bool { return y == z.Y && x >= z.X0 && x < z.X1 }

// Scan gỡ mọi dấu khỏi rendered, trả chuỗi sạch và các vùng theo thứ tự xuất hiện. Dấu mở không có dấu đóng thì vùng chạy
// tới hết dòng; dấu đóng không có dấu mở bị bỏ; vùng rỗng bị bỏ.
func Scan(rendered string) (string, []Zone) {
	if !strings.Contains(rendered, "\x1b_Z") {
		return rendered, nil
	}
	lines := strings.Split(rendered, "\n")
	var zones []Zone
	type open struct {
		id string
		x  int
	}
	for y, line := range lines {
		if !strings.Contains(line, "\x1b_Z") {
			continue
		}
		var sb strings.Builder
		var stack []open
		x := 0
		for len(line) > 0 {
			i := strings.Index(line, "\x1b_Z")
			if i < 0 {
				sb.WriteString(line)
				x += ansi.StringWidth(line)
				break
			}
			seg := line[:i]
			sb.WriteString(seg)
			x += ansi.StringWidth(seg)
			rest := line[i:]
			j := strings.Index(rest, st)
			if j < 0 { // dấu hỏng: bỏ phần còn lại của dấu
				break
			}
			body := rest[3:j] // sau "\x1b_Z"
			line = rest[j+len(st):]
			switch {
			case strings.HasPrefix(body, "s;"):
				stack = append(stack, open{id: body[2:], x: x})
			case body == "e":
				if n := len(stack); n > 0 {
					o := stack[n-1]
					stack = stack[:n-1]
					if x > o.x {
						zones = append(zones, Zone{ID: o.id, X0: o.x, X1: x, Y: y})
					}
				}
			}
		}
		for _, o := range stack {
			if x > o.x {
				zones = append(zones, Zone{ID: o.id, X0: o.x, X1: x, Y: y})
			}
		}
		lines[y] = sb.String()
	}
	return strings.Join(lines, "\n"), zones
}

// At trả vùng hẹp nhất chứa ô (x, y) có id thoả match (nil = mọi id); ok = false nếu không có.
func At(zones []Zone, x, y int, match func(id string) bool) (Zone, bool) {
	best, found := Zone{}, false
	for _, z := range zones {
		if !z.Contains(x, y) || (match != nil && !match(z.ID)) {
			continue
		}
		if !found || z.X1-z.X0 < best.X1-best.X0 {
			best, found = z, true
		}
	}
	return best, found
}
