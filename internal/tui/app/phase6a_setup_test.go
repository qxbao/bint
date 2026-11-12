package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/harness/claude"
	"bint/internal/tui/actions"
	syscreen "bint/internal/tui/screens/system"
)

// failSecondApply bọc system.repo_apply: bước đầu chạy thật rồi bị đánh dấu HasProblem; bước hai trả lỗi giả.
func failSecondApply(o *Options) {
	var n atomic.Int32
	var list []actions.Action
	for _, a := range o.Registry.All() {
		if a.ID == "system.repo_apply" {
			orig := a.Do
			a.Do = func(ctx context.Context, env actions.Env, opts any) tea.Msg {
				switch n.Add(1) {
				case 1:
					msg := orig(ctx, env, opts)
					if rm, ok := msg.(actions.ResultMsg); ok {
						if r, ok := rm.Value.(*bintapp.InitReport); ok {
							r.HasProblem = true
						}
						return rm
					}
					return msg
				case 2:
					return actions.ResultMsg{ID: "system.repo_apply", Opts: opts, Err: errors.New("lỗi giả ở bước hai")}
				}
				return orig(ctx, env, opts)
			}
		}
		list = append(list, a)
	}
	o.Registry = actions.NewRegistry(list)
}

// 6a.2 (01M75YH2JGR9AD685VH886WE33.1+.2): nhiều bước lỗi ở bước sau thì khung kết quả giữ báo cáo bước đã xong kèm dòng lỗi
// "Bước i/n"; setupProblem được xoá nên lần chạy sạch kế tiếp không báo vấn đề.
func TestSetupMultiStepFailureKeepsReportAndClearsProblem(t *testing.T) {
	f, root, c := lifeFixture(t, failSecondApply)
	installClaudeHook(t, root, claude.ScopeLocal)
	f.sysTab(syscreen.TabSetup)
	f.waitFor("Mẫu cấu hình")
	f.press("e", "tab", "tab", "tab", " ", "ctrl+s") // phạm vi: local → project (hai bước)
	f.waitFor("Xem trước init")
	f.press("enter")
	f.waitFor("lỗi giả ở bước hai")
	out := f.screen()
	hasAll(t, out, "Cài đặt repo: dừng ở bước lỗi", "Bước 1/2", "Bước 2/2 lỗi", "lỗi giả ở bước hai")
	if strings.Index(out, "Bước 1/2") > strings.LastIndex(out, "Bước 2/2 lỗi") {
		t.Fatalf("báo cáo bước đã xong phải đứng trước dòng lỗi:\n%s", out)
	}
	// Lần chạy sạch kế tiếp (bước đơn: đổi MCP) không được báo "có vấn đề" do cờ còn sót từ lần lỗi.
	f.press("esc")
	f.pump(func() bool { return strings.Contains(f.screen(), "Mẫu cấu hình") && !f.m.sy.Capturing() }, nil)
	before := c.count("system.repo_apply")
	f.press("e", "tab", "tab", "tab", "tab", "tab", " ", "ctrl+s")
	f.waitFor("Xem trước init")
	f.press("enter")
	f.pump(func() bool { return c.count("system.repo_apply") > before }, nil)
	f.waitFor("Đã chạy init")
	if out := f.screen(); strings.Contains(out, "có bước cảnh báo") {
		t.Fatalf("setupProblem phải được xoá sau lần lỗi:\n%s", out)
	}
}

// 6a.4 (01M75YH2JGR9AD685VH886WE33.4): thẻ Cài đặt repo nạp xong khi đang có form khác thì yêu cầu mở form đang chờ bị bỏ
// kèm toast; lần nạp lại sau đó (Ctrl+R) không tự mở form.
func TestSetupPendingDroppedWhenFormOpen(t *testing.T) {
	g := newGatedInit()
	f, _, _ := lifeFixture(t, func(o *Options) { o.SystemLoad.Init = g.load })
	_, cmd := f.m.run("system.repo_plan") // bảng lệnh: nhớ việc mở form khi nạp xong
	f.pump(func() bool { return g.started.Load() == 1 }, cmd)
	for i := syscreen.TabSetup; i > syscreen.TabConfig; i-- { // trong lúc chờ nạp, người dùng sang thẻ Cấu hình và mở form sửa
		f.press(",")
	}
	f.press("e")
	if !f.m.sy.Capturing() {
		t.Fatalf("điều kiện thử: form sửa cấu hình phải đang mở:\n%s", f.screen())
	}
	close(g.release)
	f.pump(func() bool { return strings.Contains(f.screen(), "Đã bỏ yêu cầu mở form") }, nil)
	f.press("esc") // đóng form sửa cấu hình
	for i := syscreen.TabConfig; i < syscreen.TabSetup; i++ {
		f.press(".")
	}
	f.press("ctrl+r")
	f.pump(func() bool { return g.started.Load() >= 2 && strings.Contains(f.screen(), "Mẫu cấu hình") }, nil)
	if f.m.sy.Capturing() {
		t.Fatalf("lần nạp lại không được tự mở form Cài đặt repo:\n%s", f.screen())
	}
}
