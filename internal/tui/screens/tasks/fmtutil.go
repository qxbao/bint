package tasks

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ansiStrip bỏ mã ANSI để đo độ rộng.
func ansiStrip(s string) string { return ansi.Strip(s) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// trimZero in một chữ số thập phân và bỏ ".0" thừa ("1.5", "2").
func trimZero(f float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(f, 'f', 1, 64), ".0")
}
