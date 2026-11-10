package workflow

import (
	"context"
	"testing"
	"time"

	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/ulid"
)

func verifiedRule(t *testing.T, h *harness, st knowledge.Store) *knowledge.Item {
	t.Helper()
	now := knowledge.Now(time.Now())
	it := &knowledge.Item{Schema: 1, ID: ulid.New(), Type: knowledge.TypeRule, Status: knowledge.StatusVerified,
		Title: "Quy tắc duyệt", Body: "Thay đổi được bảo vệ cần chủ sở hữu duyệt.",
		Origin: knowledge.Origin{Kind: knowledge.OriginHuman}, Author: "owner", ReviewedBy: "owner", ReviewedAt: now, VerifiedAt: now,
		Version: 1, CreatedAt: now, Scope: knowledge.Scope{Paths: []string{"app.txt"}},
		Evidence: []knowledge.Evidence{{Path: "app.txt", Lines: "1"}}}
	if err := knowledge.FillFingerprints(it, &knowledge.WorktreeSource{Root: h.root}); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(it); err != nil {
		t.Fatal(err)
	}
	return it
}

func newItem(status string) *knowledge.Item {
	now := knowledge.Now(time.Now())
	return &knowledge.Item{Schema: 1, ID: ulid.New(), Type: knowledge.TypeRule, Status: status,
		Title: "Mục mới", Body: "Nội dung mới.", Origin: knowledge.Origin{Kind: knowledge.OriginHuman},
		Author: "agent", Version: 1, CreatedAt: now, Scope: knowledge.Scope{Paths: []string{"app.txt"}},
		Evidence: []knowledge.Evidence{{Path: "app.txt", Lines: "1"}}}
}

// R2: bước chỉ đọc chỉ được tạo tệp tri thức MỚI ở trạng thái candidate.
func TestReadOnlyStepKnowledgeExceptionNarrow(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, st knowledge.Store, old *knowledge.Item)
		ok     bool
	}{
		{"tạo candidate mới", func(t *testing.T, st knowledge.Store, _ *knowledge.Item) {
			if err := st.Save(newItem(knowledge.StatusCandidate)); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"sửa mục verified", func(t *testing.T, st knowledge.Store, old *knowledge.Item) {
			c := *old
			c.Body = "Thay đổi được bảo vệ không cần duyệt."
			if err := st.Save(&c); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"xoá mục có sẵn", func(t *testing.T, st knowledge.Store, old *knowledge.Item) {
			if err := st.Remove(old.ID); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"tệp mới tự gán verified", func(t *testing.T, st knowledge.Store, _ *knowledge.Item) {
			if err := st.Save(newItem(knowledge.StatusVerified)); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			st := knowledge.NewStore(h.root)
			old := verifiedRule(t, h, st)
			h.behave = func(ctx context.Context, step string, e *Env, id string) error {
				if step == StepReq {
					c.mutate(t, st, old)
				}
				return h.happy(ctx, step, e, id)
			}
			res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepReq, Request: "Plan update to app.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if c.ok && res.Status == task.StatusFailed {
				t.Fatalf("candidate mới phải được phép: %+v", res.Steps)
			}
			if !c.ok && res.Status != task.StatusFailed {
				t.Fatalf("thay đổi không được phép mà bước vẫn qua: task=%s steps=%+v", res.Status, res.Steps)
			}
		})
	}
}
