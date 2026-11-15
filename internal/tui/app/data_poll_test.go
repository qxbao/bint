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
	"bint/internal/knowledge"
	knscreen "bint/internal/tui/screens/knowledge"
	"bint/internal/tui/screens/overview"
	rnscreen "bint/internal/tui/screens/runs"
)

// Kiểm thử nạp lại tự động theo thay đổi dữ liệu (Options.DataPoll).

type pollCounts struct {
	ov, kn, rn atomic.Int32
	knFail     atomic.Bool
	extra      atomic.Int32 // số mục tri thức thêm vào cuối danh sách
}

func newPollFixture(t *testing.T) (*fixture, *pollCounts) {
	t.Helper()
	c := &pollCounts{}
	f := newFixture(t, Options{
		DataPoll:          10 * time.Millisecond,
		KnowledgeDetailer: sampleDetail,
		KnowledgeLoader: func(context.Context, string) (knscreen.Data, error) {
			c.kn.Add(1)
			if c.knFail.Load() {
				return knscreen.Data{}, errors.New("nạp nền lỗi")
			}
			d := sampleKnowledge()
			for i := int32(0); i < c.extra.Load(); i++ {
				d.Items = append(d.Items, &knowledge.Item{ID: "01KNOWEXTRA0" + string(rune('A'+i)), Type: "fact", Status: "verified", Title: "Mục thêm"})
			}
			return d, nil
		},
		RunsLoader: func(context.Context, string, int, bool) (rnscreen.Data, error) {
			c.rn.Add(1)
			return rnscreen.Data{}, nil
		},
	})
	f.m.opts.Loader = func(context.Context, string) (overview.Data, error) {
		c.ov.Add(1)
		return overview.Data{Status: &bintapp.StatusReport{Repo: f.root}}, nil
	}
	return f, c
}

func (f *fixture) pumpFor(d time.Duration) {
	f.t.Helper()
	end := time.Now().Add(d)
	f.pump(func() bool { return time.Now().After(end) }, nil)
}

// waitSettled chờ tới khi chữ ký nền đã ghi và mọi lần nạp đã xong.
func (f *fixture) waitSettled() {
	f.t.Helper()
	f.pump(func() bool { return f.m.dataSigSet && !f.m.ov.Loading && !f.m.kn.Loading && !f.m.rn.Loading }, nil)
	f.pumpFor(60 * time.Millisecond)
}

func (f *fixture) touchKnowledge(name string) {
	f.t.Helper()
	dir := filepath.Join(f.root, ".bint", "knowledge", "items")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"+time.Now().String()), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// AC-1, AC-2, AC-5: đổi dữ liệu thì Tổng quan và màn đang hiện tự nạp lại; không đổi thì không gọi Loader nào;
// màn khác chỉ bị đánh dấu cũ và nạp khi chuyển tới.
func TestDataPollReloadsVisibleScreenOnly(t *testing.T) {
	f, c := newPollFixture(t)
	f.press("2")
	f.pump(func() bool { return f.m.kn.Loaded }, nil)
	f.waitSettled()

	ov, kn := c.ov.Load(), c.kn.Load()
	f.pumpFor(200 * time.Millisecond) // nhiều lượt tick, dữ liệu không đổi
	if c.ov.Load() != ov || c.kn.Load() != kn || c.rn.Load() != 0 {
		t.Fatalf("không đổi dữ liệu mà Loader vẫn được gọi: ov %d→%d kn %d→%d rn %d", ov, c.ov.Load(), kn, c.kn.Load(), c.rn.Load())
	}

	c.extra.Store(1)
	f.touchKnowledge("new.md")
	f.pump(func() bool { return c.kn.Load() > kn && c.ov.Load() > ov && !f.m.kn.Loading }, nil)
	if len(f.m.kn.Data.Items) != 7 {
		t.Fatalf("danh sách chưa cập nhật: %d mục", len(f.m.kn.Data.Items))
	}
	f.waitSettled()
	if c.rn.Load() != 0 {
		t.Fatalf("màn Lượt chạy không hiện không được nạp nền (rn=%d)", c.rn.Load())
	}

	f.press("4") // chuyển tới Lượt chạy: nạp khi tới
	f.pump(func() bool { return c.rn.Load() == 1 && f.m.rn.Loaded }, nil)
}

// Thay đổi tệp tác vụ cũng được thấy.
func TestDataPollSeesTaskLedger(t *testing.T) {
	f, c := newPollFixture(t)
	f.waitSettled()
	ov := c.ov.Load()
	dir := filepath.Join(f.root, ".bint", "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.yml"), []byte("id: t1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.pump(func() bool { return c.ov.Load() > ov }, nil)
}

// AC-3: nạp tự động giữ con trỏ, không hiện "đang tải", lỗi nạp nền giữ dữ liệu cũ.
func TestDataPollKeepsCursorAndSurvivesError(t *testing.T) {
	f, c := newPollFixture(t)
	f.press("2")
	f.pump(func() bool { return f.m.kn.Loaded }, nil)
	f.press("j", "j")
	want := f.m.kn.Cursor().ID
	f.waitSettled()

	kn := c.kn.Load()
	c.extra.Store(1)
	f.touchKnowledge("a.md")
	f.pump(func() bool { return c.kn.Load() > kn && !f.m.kn.Loading }, nil)
	if got := f.m.kn.Cursor().ID; got != want {
		t.Fatalf("con trỏ nhảy: %s → %s", want, got)
	}
	if r := plain(f.m.Render()); strings.Contains(r, "Đang tải") {
		t.Fatalf("nạp nền không được hiện ui.loading:\n%s", r)
	}

	f.waitSettled()
	kn = c.kn.Load()
	c.knFail.Store(true)
	f.touchKnowledge("b.md")
	f.pump(func() bool { return c.kn.Load() > kn && !f.m.kn.Loading }, nil)
	if f.m.kn.Err != nil || len(f.m.kn.Data.Items) != 7 {
		t.Fatalf("lỗi nạp nền phải giữ dữ liệu cũ: err=%v items=%d", f.m.kn.Err, len(f.m.kn.Data.Items))
	}
	if r := plain(f.m.Render()); strings.Contains(r, "nạp nền lỗi") || !strings.Contains(r, "Mục thêm") {
		t.Fatalf("dữ liệu đang hiện bị thay bằng trang lỗi:\n%s", r)
	}
}

// AC-4: không nạp khi đang Capturing; thay đổi được nạp sau khi đóng.
func TestDataPollDefersWhileCapturing(t *testing.T) {
	f, c := newPollFixture(t)
	f.press("2")
	f.pump(func() bool { return f.m.kn.Loaded }, nil)
	f.waitSettled()

	f.press("/")
	if !f.m.kn.Capturing() {
		t.Fatal("ô lọc phải đang Capturing")
	}
	ov, kn := c.ov.Load(), c.kn.Load()
	f.touchKnowledge("c.md")
	f.pumpFor(200 * time.Millisecond)
	if c.ov.Load() != ov || c.kn.Load() != kn {
		t.Fatalf("đang Capturing mà vẫn nạp: ov %d→%d kn %d→%d", ov, c.ov.Load(), kn, c.kn.Load())
	}
	f.press("esc")
	if f.m.kn.Capturing() {
		t.Fatal("esc phải đóng ô lọc")
	}
	f.pump(func() bool { return c.kn.Load() > kn && c.ov.Load() > ov }, nil)
}

// AC-6: ctrl+r vẫn nạp lại tay khi bật DataPoll.
func TestDataPollManualRefreshStillWorks(t *testing.T) {
	f, c := newPollFixture(t)
	f.press("2")
	f.pump(func() bool { return f.m.kn.Loaded }, nil)
	f.waitSettled()
	kn := c.kn.Load()
	f.press("ctrl+r")
	f.pump(func() bool { return c.kn.Load() > kn }, nil)
}

// AC-7: DataPoll bằng 0 không hẹn tick nào.
func TestDataPollZeroDisablesPolling(t *testing.T) {
	f := newFixture(t, Options{})
	if f.m.opts.DataPoll != 0 || f.m.dataTicking || f.m.dataLoading || f.m.startDataPoll() != nil {
		t.Fatal("DataPoll=0 không được hẹn thăm dò")
	}
}
