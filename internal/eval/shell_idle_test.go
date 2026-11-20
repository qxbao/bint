package eval

import (
	"context"
	"os"
	"testing"
	"time"
)

// Tiến trình con giả lập harness: in một dòng mỗi BINT_IDLE_EVERY rồi (BINT_IDLE_HANG=1) im lặng lâu.
func idleFixture() {
	every, _ := time.ParseDuration(os.Getenv("BINT_IDLE_EVERY"))
	for range 6 {
		os.Stdout.WriteString("tick\n")
		time.Sleep(every)
	}
	if os.Getenv("BINT_IDLE_HANG") == "1" {
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func runIdleFixture(t *testing.T, every string, hang bool, idle time.Duration) CmdResult {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "BINT_IDLE_FIXTURE=1", "BINT_IDLE_EVERY="+every)
	if hang {
		env = append(env, "BINT_IDLE_HANG=1")
	}
	return runProcessIdle(context.Background(), t.TempDir(), bin, []string{"-test.run=^TestRunProcessIdle$"}, time.Minute, idle, "", env, nil, nil)
}

func TestRunProcessIdle(t *testing.T) {
	if os.Getenv("BINT_IDLE_FIXTURE") == "1" {
		idleFixture()
	}
	// Đầu ra đều đặn dưới trần im lặng: chạy tới hết, không bị coi là treo.
	if res := runIdleFixture(t, "100ms", false, 2*time.Second); !res.OK() || res.Idle {
		t.Errorf("tiến trình có đầu ra đều bị dừng: %+v", res)
	}
	// Im lặng quá trần: dừng sớm (không chờ trần thời gian 1 phút), TimedOut và Idle.
	start := time.Now()
	res := runIdleFixture(t, "10ms", true, time.Second)
	if !res.Idle || !res.TimedOut || res.OK() {
		t.Errorf("tiến trình treo không bị dừng vì im lặng: %+v", res)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("dừng quá muộn: %s", d)
	}
	// idle = 0: không theo dõi im lặng.
	if res := runProcessIdle(context.Background(), t.TempDir(), "go", []string{"version"}, time.Minute, 0, "", nil, nil, nil); res.Idle || !res.OK() {
		t.Errorf("idle = 0 mà vẫn theo dõi: %+v", res)
	}
}
