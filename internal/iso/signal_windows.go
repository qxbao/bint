//go:build windows

package iso

import (
	"os"
	"os/signal"
	"sync"
)

// Trên Windows Ctrl-C tới cả console group (cha lẫn con) nên không có gì để chuyển; bint chỉ nuốt os.Interrupt
// để không thoát trước con (nhả khoá và dọn temp sau khi con thoát).
func notifySignals() (attach func(*os.Process), stop func()) {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func(*os.Process) {}, func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
}

func exitCode(ps *os.ProcessState) int { return ps.ExitCode() }
