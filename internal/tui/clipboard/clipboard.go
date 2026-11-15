// Package clipboard là giao diện đọc/ghi clipboard hệ thống của TUI: bản thật bọc github.com/atotto/clipboard
// (Windows dùng Win32 không cgo, macOS pbcopy/pbpaste, Linux xclip, xsel hoặc wl-clipboard; thiếu công cụ thì trả lỗi),
// bản giả dùng trong kiểm thử.
package clipboard

import (
	"errors"

	sys "github.com/atotto/clipboard"
)

// Clipboard là clipboard hệ thống.
type Clipboard interface {
	Read() (string, error)
	Write(text string) error
}

type system struct{}

// System trả clipboard thật của hệ điều hành.
func System() Clipboard { return system{} }

func (system) Read() (string, error) { return sys.ReadAll() }
func (system) Write(s string) error  { return sys.WriteAll(s) }

// Fake là clipboard giả trong bộ nhớ cho kiểm thử: Text là nội dung hiện tại, ReadErr/WriteErr buộc lỗi.
type Fake struct {
	Text              string
	ReadErr, WriteErr error
	Reads, Writes     int
}

// Read trả Text (hoặc ReadErr).
func (f *Fake) Read() (string, error) {
	f.Reads++
	if f.ReadErr != nil {
		return "", f.ReadErr
	}
	return f.Text, nil
}

// Write đặt Text (hoặc trả WriteErr).
func (f *Fake) Write(s string) error {
	f.Writes++
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.Text = s
	return nil
}

// ErrUnavailable là lỗi mẫu cho kiểm thử.
var ErrUnavailable = errors.New("clipboard unavailable")
