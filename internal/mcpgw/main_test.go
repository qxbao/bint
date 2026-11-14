package mcpgw

import (
	"context"
	"os"
	"os/signal"
	"testing"
	"time"
)

// TestMain: binary test được chạy lại làm upstream giả (__fake_upstream), tiến trình con của upstream (__child) hoặc daemon
// foreground (__gateway), nên chạy được trên mọi nền tảng mà không cần chương trình ngoài.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__fake_upstream":
			runFake(os.Args[2:])
			os.Exit(0)
		case "__child":
			time.Sleep(10 * time.Minute)
			os.Exit(0)
		case "__gateway":
			if len(os.Args) > 2 {
				cwd, err := os.Getwd()
				if err != nil {
					os.Exit(1)
				}
				if err := os.WriteFile(os.Args[2], []byte(cwd), 0o600); err != nil {
					os.Exit(1)
				}
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()
			if err := Run(ctx, Options{Version: "test"}); err != nil {
				os.Stderr.WriteString("gateway: " + err.Error() + "\n")
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
