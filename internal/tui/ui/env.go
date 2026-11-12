// Package ui chứa ngữ cảnh hiển thị dùng chung cho các màn hình và component: theme, danh mục chuỗi
// (i18n) và kích thước. Tách riêng để screens, components và app không import vòng nhau.
package ui

import (
	"bint/internal/i18n"
	"bint/internal/tui/components/footer"
	"bint/internal/tui/theme"
)

// Env là ngữ cảnh một lần vẽ.
type Env struct {
	Th  theme.Theme
	Cat *i18n.Catalog
	// RW viết lại thông điệp của tầng dịch vụ (lệnh CLI → phím TUI) theo sổ và chế độ của mô hình đang vẽ.
	RW Rewriter
	// W, H là kích thước vùng được vẽ.
	W, H int
	// Spin là khung hiện tại của vòng quay "đang làm việc" (Bubbles spinner).
	Spin string
	// Allowed tra sổ hành động: id có dùng được ở chế độ hiện tại (chỉ đọc, phiên agent) không; nil = mọi hành động. Chân phím trong khung lọc
	// qua đây thay vì để màn hình tự kiểm ReadOnly.
	Allowed func(id string) bool
	// Collect (nếu khác nil) nhận chân phím trong khung (đã lọc theo Allowed) mỗi lần một màn hình dựng chúng; trợ giúp theo ngữ cảnh dùng nó để
	// liệt kê đúng các phím của thẻ phụ đang xem mà màn hình không phải khai báo lần hai.
	Collect func([]footer.Hint)
}

// Rewriter trả bộ viết lại thông điệp của env (RW; thiếu danh mục thì lấy Cat).
func (e Env) Rewriter() Rewriter {
	rw := e.RW
	if rw.Cat == nil {
		rw.Cat = e.Cat
	}
	return rw
}

// AllowedHints bỏ các phím gắn với hành động mà sổ không cho dùng ở chế độ hiện tại; phím giao diện thuần (ActionID rỗng) luôn giữ.
func (e Env) AllowedHints(hs []footer.Hint) []footer.Hint {
	out := hs
	if e.Allowed != nil {
		out = hs[:0:0]
		for _, h := range hs {
			if h.ActionID == "" || e.Allowed(h.ActionID) {
				out = append(out, h)
			}
		}
	}
	if e.Collect != nil {
		e.Collect(out)
	}
	return out
}

// T tra chuỗi giao diện.
func (e Env) T(key string, a ...i18n.A) string { return e.Cat.T(key, a...) }

// N tra chuỗi giao diện có số nhiều.
func (e Env) N(key string, count int, a ...i18n.A) string { return e.Cat.N(key, count, a...) }

// WithSize trả bản sao với kích thước khác.
func (e Env) WithSize(w, h int) Env { e.W, e.H = w, h; return e }
