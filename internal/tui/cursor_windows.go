//go:build windows

package tui

import (
	"fmt"
	"io"
	"os"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// cursorQueryTimeout là thời gian chờ terminal trả lời vị trí con trỏ.
const cursorQueryTimeout = 500 * time.Millisecond

// saveCursor hỏi terminal vị trí con trỏ trước khi mở TUI (CSI 6n, kèm DA1 làm mốc kết thúc) và trả hàm đặt con trỏ về đúng chỗ đó sau
// khi thoát. Trên Windows (ConPTY), khôi phục con trỏ của chế độ alt screen 1049 có thể lệch với terminal: sau khi thoát, dòng lệnh mới
// ghi đè lên chữ cũ ở đầu màn hình cho tới khi gõ reset. Đặt lại tuyệt đối (CUP) làm ConPTY và terminal khớp nhau. Không hỏi được thì
// trả hàm rỗng.
func saveCursor(in, out *os.File) func() {
	if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return func() {}
	}
	row, col, ok := queryCursor(in, out)
	if !ok {
		return func() {}
	}
	return func() { _, _ = fmt.Fprint(out, ansi.CursorPosition(col, row)) }
}

func queryCursor(in, out *os.File) (row, col int, ok bool) {
	st, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 0, 0, false
	}
	defer term.Restore(int(in.Fd()), st) //nolint:errcheck
	rd, err := uv.NewCancelReader(in)
	if err != nil {
		return 0, 0, false
	}
	defer rd.Close() //nolint:errcheck
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-time.After(cursorQueryTimeout):
			rd.Cancel()
		}
	}()
	if _, err := io.WriteString(out, ansi.RequestCursorPositionReport+ansi.RequestPrimaryDeviceAttributes); err != nil {
		return 0, 0, false
	}
	pa := ansi.GetParser()
	defer ansi.PutParser(pa)
	var buf [256]byte
	var state byte
	for {
		n, err := rd.Read(buf[:])
		if err != nil {
			return row, col, ok
		}
		p := buf[:n]
		for len(p) > 0 {
			seq, _, read, next := ansi.DecodeSequence(p, state, pa)
			if next == ansi.NormalState && ansi.HasCsiPrefix(seq) {
				switch pa.Command() {
				case 'R': // CPR: hàng;cột (từ 1)
					r, _ := pa.Param(0, 1)
					c, _ := pa.Param(1, 1)
					row, col, ok = r, c, true
				case ansi.Command('?', 0, 'c'): // DA1: terminal đã trả lời hết
					return row, col, ok
				}
			}
			state = next
			p = p[read:]
		}
	}
}
