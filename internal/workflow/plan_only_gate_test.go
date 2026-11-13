package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/knowledge"
	"bint/internal/task"
)

// Phát hiện .13: phiếu chỉ lập kế hoạch ở worktree chỉ hưởng cổng nhẹ khi diff so với Base rỗng hoặc chỉ gồm mục
// tri thức candidate mới; thay đổi khác (mã trong scope.allow, mục không phải candidate) quay về cổng done đầy đủ.
func TestPlanOnlyWorktreeGateNarrowedToCandidateItems(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
		stray  string // rỗng: được hưởng cổng nhẹ
	}{
		{"diff rỗng", func(*testing.T, string) {}, ""},
		{"chỉ mục candidate mới", func(t *testing.T, root string) {
			if err := knowledge.NewStore(root).Save(newItem(knowledge.StatusCandidate)); err != nil {
				t.Fatal(err)
			}
		}, ""},
		{"tệp mã trong scope.allow", func(t *testing.T, root string) {
			if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "internal", "foo.go"), []byte("package internal\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "internal/foo.go"},
		{"mục tri thức không phải candidate", func(t *testing.T, root string) {
			if err := knowledge.NewStore(root).Save(newItem(knowledge.StatusVerified)); err != nil {
				t.Fatal(err)
			}
		}, knowledge.ItemsRel + "/"},
		{"candidate cùng tệp mã", func(t *testing.T, root string) {
			if err := knowledge.NewStore(root).Save(newItem(knowledge.StatusCandidate)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("sửa ngoài bước\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, "app.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t).wtHarness()
			ctx := context.Background()
			r := h.runner()
			id := planOnlyPlanned(t, r)
			tk := loadTask(t, h, id)
			if tk.State.Origin == "" {
				t.Skip("b-req không tạo worktree trong cấu hình này")
			}
			// Phiếu có lệnh kiểm chứng chưa xác nhận: cổng đầy đủ sẽ chặn vì nó.
			if _, err := h.tasks.Update(id, time.Now(), func(t *task.Task) error {
				t.Ticket.Verify.Command = "go test ./..."
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			root, err := r.Env.RootFor(tk)
			if err != nil {
				t.Fatal(err)
			}
			c.mutate(t, root)

			var nd *NotDoneError
			_, landErr := r.Land(ctx, id, LandOptions{Keep: true})
			if c.stray == "" {
				if landErr != nil {
					t.Fatalf("Land phải qua: %v", landErr)
				}
				got, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong"})
				if err != nil || got.State.Status != task.StatusDone {
					t.Fatalf("done phải qua: %v", err)
				}
				return
			}
			joined := ""
			if errors.As(landErr, &nd) {
				joined = strings.Join(nd.Blockers, ";")
			}
			if joined == "" || !strings.Contains(joined, c.stray) || !strings.Contains(joined, "kiểm chứng") {
				t.Fatalf("Land phải bị chặn, nêu tệp %q và đòi kiểm chứng: %v", c.stray, landErr)
			}
			if _, err := os.Stat(filepath.Join(h.root, "internal", "foo.go")); err == nil {
				t.Fatal("tệp không được tới cây gốc")
			}
			// done không đóng bằng ClosePlan, kể cả --force.
			for _, force := range []bool{false, true} {
				_, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong", Force: force})
				if !errors.As(err, &nd) || !strings.Contains(strings.Join(nd.Blockers, ";"), c.stray) {
					t.Fatalf("done (force=%v) phải bị chặn bằng NotDoneError: %v", force, err)
				}
			}
			if got := loadTask(t, h, id); got.State.Status != task.StatusPlanned {
				t.Fatalf("trạng thái đổi: %s", got.State.Status)
			}
		})
	}
}

// Phát hiện .10: done --force trên phiếu chỉ lập kế hoạch ghi chú thích người xác nhận vào lịch sử (như đường Apply).
func TestPlanOnlyForceNoteInHistory(t *testing.T) {
	h := newHarness(t).wtHarness()
	ctx := context.Background()
	r := h.runner()
	id := planOnlyPlanned(t, r)
	tk := loadTask(t, h, id)
	if tk.State.Origin == "" {
		t.Skip("b-req không tạo worktree trong cấu hình này")
	}
	root, _ := r.Env.RootFor(tk)
	if err := knowledge.NewStore(root).Save(newItem(knowledge.StatusCandidate)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Land(ctx, id, LandOptions{}); err != nil {
		t.Fatalf("land: %v", err)
	}
	// Sau khi đưa về, đường dẫn mục tri thức trở thành đường dẫn bảo vệ chưa duyệt: done cần --force.
	r.Env.Cfg.Limits.ProtectedPaths = append(append([]string{}, r.Env.Cfg.Limits.ProtectedPaths...), knowledge.ItemsRel+"/**")
	var nd *NotDoneError
	if _, _, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong"}); !errors.As(err, &nd) {
		t.Fatalf("không ép thì phải bị chặn: %v", err)
	}
	got, blockers, err := r.SetStatus(ctx, id, task.StatusDone, TransitionOptions{Reason: "xong", Force: true})
	if err != nil || got.State.Status != task.StatusDone || len(blockers) == 0 {
		t.Fatalf("done --force: %+v %v %v", got.State.Status, blockers, err)
	}
	last := got.State.History[len(got.State.History)-1]
	if !strings.Contains(last.Reason, "người xác nhận dù thiếu bằng chứng") || !strings.Contains(last.Reason, knowledge.ItemsRel) {
		t.Fatalf("lịch sử thiếu chú thích force: %q", last.Reason)
	}
}
