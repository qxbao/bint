package mcpgw

import (
	"sync"
	"testing"
	"time"
)

// Nhóm exclusive (specmatic.mock): phiên thứ hai chờ có giới hạn rồi nhận busy; dùng được sau DELETE hoặc khi khoá hết TTL.
func TestExclusiveGroup(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.ExclusiveWait = 200 * time.Millisecond
		o.ExclusiveTTL = 500 * time.Millisecond
	})
	h.register("specmatic")
	repo := evalDir(t, newRepo(t, true))
	wt2 := addWorktree(t, repo, "wt2")
	mockCalls := func() (n int) {
		for _, e := range h.recvs("specmatic", "tools/call") {
			if e["tool"] == "manage_mock_server" {
				n++
			}
		}
		return
	}

	a := h.open("specmatic", worktreeHdr(repo))
	b := h.open("specmatic", worktreeHdr(repo))
	if m, _ := a.call(1, "manage_mock_server", map[string]any{"action": "start"}); len(m.Error) > 0 {
		t.Fatal(string(m.Error))
	}
	start := time.Now()
	m, _ := b.call(1, "manage_mock_server", map[string]any{"action": "start"})
	if rpcCode(m) != CodeBusy {
		t.Fatalf("phiên thứ hai: %s, muốn CodeBusy", m.Error)
	}
	if el := time.Since(start); el < 150*time.Millisecond {
		t.Errorf("busy sau %s: phải chờ có giới hạn (~200ms) trước khi báo", el)
	}
	if mockCalls() != 1 {
		t.Errorf("lời gọi bị busy vẫn tới upstream (%d)", mockCalls())
	}
	// Nhóm khác không bị chặn; phiên giữ khoá gọi lại được.
	if m, _ := b.call(2, "backward_compatibility_check", nil); len(m.Error) > 0 {
		t.Errorf("nhóm compat không độc quyền: %s", m.Error)
	}
	if m, _ := a.call(2, "manage_mock_server", map[string]any{"action": "stop"}); len(m.Error) > 0 {
		t.Errorf("chủ khoá gọi lại: %s", m.Error)
	}
	// Worktree khác = phân vùng khác: không tranh khoá.
	other := h.open("specmatic", worktreeHdr(wt2))
	if m, _ := other.call(1, "manage_mock_server", nil); len(m.Error) > 0 {
		t.Errorf("phân vùng khác không được chia khoá: %s", m.Error)
	}
	// DELETE nhả khoá.
	a.close()
	if m, _ := b.call(3, "manage_mock_server", nil); len(m.Error) > 0 {
		t.Errorf("sau khi chủ khoá đóng phiên: %s", m.Error)
	}
	// Hết TTL (không ai gia hạn) thì phiên khác lấy được.
	c := h.open("specmatic", worktreeHdr(repo))
	if m, _ := c.call(1, "manage_mock_server", nil); rpcCode(m) != CodeBusy {
		t.Fatalf("b đang giữ khoá: %s", m.Error)
	}
	time.Sleep(600 * time.Millisecond)
	if m, _ := c.call(2, "manage_mock_server", nil); len(m.Error) > 0 {
		t.Errorf("sau khi khoá hết TTL: %s", m.Error)
	}
}

// Nhóm contract chạy tối đa một lời gọi đồng thời mỗi phân vùng (Group.MaxConcurrent); nhóm không giới hạn thì chồng nhau được.
func TestGroupMaxConcurrent(t *testing.T) {
	h := newHarness(t, nil)
	h.register("specmatic")
	repo := evalDir(t, newRepo(t, true))
	c1 := h.open("specmatic", worktreeHdr(repo))
	c2 := h.open("specmatic", worktreeHdr(repo))
	run := func(tool string) (outs [2]toolOut) {
		var wg sync.WaitGroup
		for i, c := range []*client{c1, c2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, outs[i] = c.call(i, tool, map[string]any{"sleep_ms": 250})
			}()
		}
		wg.Wait()
		return
	}
	start := time.Now()
	outs := run("run_contract_test")
	for i, o := range outs {
		if o.MaxConcurrent != 1 {
			t.Errorf("contract: upstream thấy %d lời gọi đồng thời (client %d), muốn 1", o.MaxConcurrent, i)
		}
	}
	if el := time.Since(start); el < 450*time.Millisecond {
		t.Errorf("hai lời gọi contract xong sau %s: không tuần tự", el)
	}
	// Đối chứng: nhóm compat không giới hạn nên chồng nhau (chứng minh kiểm thử có khả năng bắt lỗi).
	outs = run("backward_compatibility_check")
	if max(outs[0].MaxConcurrent, outs[1].MaxConcurrent) != 2 {
		t.Errorf("compat: tối đa %d đồng thời, muốn 2", max(outs[0].MaxConcurrent, outs[1].MaxConcurrent))
	}
}
