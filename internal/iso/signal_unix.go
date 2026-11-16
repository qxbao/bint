//go:build !windows

package iso

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// notifySignals bắt SIGTERM/SIGHUP để chuyển cho con và nuốt SIGINT: con cùng process group nên nhận Ctrl-C trực
// tiếp từ terminal, còn bint phải sống tới khi con thoát để nhả khoá và dọn temp. attach đăng ký tiến trình
// con (tín hiệu tới trước attach được chuyển ngay khi attach); stop thôi bắt tín hiệu, gọi nhiều lần cũng được.
// Gọi trước cmd.Start để không có khoảng hở mà SIGTERM làm bint chết khi con đã chạy.
func notifySignals() (attach func(*os.Process), stop func()) {
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	var (
		mu      sync.Mutex
		proc    *os.Process
		pending []os.Signal
		done    = make(chan struct{})
		once    sync.Once
	)
	go func() {
		for {
			select {
			case s := <-ch:
				if s == syscall.SIGINT {
					continue
				}
				mu.Lock()
				if proc != nil {
					_ = proc.Signal(s)
				} else {
					pending = append(pending, s)
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	attach = func(p *os.Process) {
		mu.Lock()
		defer mu.Unlock()
		proc = p
		for _, s := range pending {
			_ = p.Signal(s)
		}
		pending = nil
	}
	stop = func() {
		once.Do(func() {
			signal.Stop(ch)
			close(done)
		})
	}
	return attach, stop
}

// exitCode là mã thoát của con; chết vì tín hiệu n thì 128+n (quy ước của shell).
func exitCode(ps *os.ProcessState) int {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
