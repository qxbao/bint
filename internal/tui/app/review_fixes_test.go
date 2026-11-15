package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/tui/screens/overview"
)

// Lần nạp Tổng quan do thăm dò dữ liệu mà lỗi thì giữ dữ liệu đang hiện; nạp tay (r) lỗi vẫn hiện lỗi.
func TestDataPollOverviewErrorKeepsDataButManualReloadShowsError(t *testing.T) {
	f, c := newPollFixture(t)
	var fail atomic.Bool
	f.m.opts.Loader = func(context.Context, string) (overview.Data, error) {
		c.ov.Add(1)
		if fail.Load() {
			return overview.Data{}, errors.New("tổng quan nạp lỗi")
		}
		return overview.Data{Status: &bintapp.StatusReport{Repo: f.root}}, nil
	}
	f.waitSettled()
	if !f.m.ov.Loaded {
		t.Fatal("Tổng quan phải có dữ liệu")
	}
	ov := c.ov.Load()
	fail.Store(true)
	f.touchKnowledge("ovfail.md")
	f.pump(func() bool { return c.ov.Load() > ov && !f.m.ov.Loading }, nil)
	f.waitSettled()
	if f.m.ov.Err != nil || !f.m.ov.Loaded {
		t.Fatalf("nạp nền lỗi phải giữ dữ liệu: err=%v loaded=%v", f.m.ov.Err, f.m.ov.Loaded)
	}
	if r := plain(f.m.Render()); strings.Contains(r, "tổng quan nạp lỗi") {
		t.Fatalf("dữ liệu đang hiện bị thay bằng trang lỗi:\n%s", r)
	}
	for _, key := range []string{"r"} { // Tổng quan: r là nạp tay (ctrl+r/F5 thuộc các màn chuyên biệt)
		fail.Store(false)
		f.press("r")
		f.waitSettled()
		fail.Store(true)
		before := c.ov.Load()
		f.press(key)
		f.pump(func() bool { return c.ov.Load() > before && !f.m.ov.Loading }, nil)
		if f.m.ov.Err == nil {
			t.Fatalf("nạp tay bằng %s lỗi vẫn phải hiện lỗi", key)
		}
	}
}

// Chữ ký dữ liệu ổn định sau một lượt tự nạp với Loader thật (overview.Load, nạp chậm); ghi nối logs của tác vụ không
// kích hoạt nạp lại.
func TestDataPollStableWithRealOverviewLoaderAndLogWrites(t *testing.T) {
	f, _ := newPollFixture(t)
	var calls atomic.Int32
	f.m.opts.Loader = func(ctx context.Context, dir string) (overview.Data, error) {
		calls.Add(1)
		return overview.Load(ctx, dir)
	}
	f.waitSettled()
	f.touchKnowledge("real.md")
	f.pump(func() bool { return calls.Load() >= 1 }, nil)
	// Chờ yên: không đang nạp và Loader không được gọi thêm trong nhiều lượt thăm dò (10ms) liên tiếp.
	quiet := func() int32 {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			f.pump(func() bool { return !f.m.ov.Loading && !f.m.dataLoading }, nil)
			c0 := calls.Load()
			f.pumpFor(300 * time.Millisecond)
			if calls.Load() == c0 && !f.m.ov.Loading {
				return c0
			}
			if time.Now().After(deadline) {
				t.Fatalf("Loader thật không ổn định: vẫn được gọi lại (%d lần)", calls.Load())
			}
		}
	}
	logs := filepath.Join(f.ws.K.Paths.Runtime, "tasks", "T1", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "verify-1.log"), []byte("dau"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.pumpFor(100 * time.Millisecond)
	base := quiet() // sau một lượt tự nạp, chữ ký phải ổn định
	for i := 0; i < 5; i++ {
		fh, err := os.OpenFile(filepath.Join(logs, "verify-1.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fh.WriteString("dòng log\n")
		fh.Close()
		f.pumpFor(50 * time.Millisecond)
	}
	f.pump(func() bool { return !f.m.ov.Loading && !f.m.dataLoading }, nil)
	f.pumpFor(100 * time.Millisecond)
	if calls.Load() != base {
		t.Fatalf("ghi nối logs làm nạp lại %d lần", calls.Load()-base)
	}
}
