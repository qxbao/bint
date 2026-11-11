// Package ui chứa ngữ cảnh hiển thị dùng chung cho các màn hình và component: theme, danh mục chuỗi
// (i18n) và kích thước. Tách riêng để screens, components và app không import vòng nhau.
package ui

import (
	"bint/internal/i18n"
	"bint/internal/tui/theme"
)

// Env là ngữ cảnh một lần vẽ.
type Env struct {
	Th  theme.Theme
	Cat *i18n.Catalog
	// W, H là kích thước vùng được vẽ.
	W, H int
	// Spin là khung hiện tại của vòng quay "đang làm việc" (Bubbles spinner).
	Spin string
}

// T tra chuỗi giao diện.
func (e Env) T(key string, a ...i18n.A) string { return e.Cat.T(key, a...) }

// N tra chuỗi giao diện có số nhiều.
func (e Env) N(key string, count int, a ...i18n.A) string { return e.Cat.N(key, count, a...) }

// WithSize trả bản sao với kích thước khác.
func (e Env) WithSize(w, h int) Env { e.W, e.H = w, h; return e }
