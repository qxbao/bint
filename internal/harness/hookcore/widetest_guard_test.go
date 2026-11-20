package hookcore

import (
	"testing"
	"time"

	"bint/internal/task"
)

func TestSameWideRun(t *testing.T) {
	verify := "go test -timeout 30m ./..."
	for cmd, want := range map[string]bool{
		"go test -timeout 30m ./...":      true,
		"  go  test -timeout 30m  ./... ": true,
		"go test ./...":                   true,
		"go test -count=1 ./...":          true,
		"go test ./internal/workflow":     false,
		"go test -run TestX ./internal/a": false,
		"go vet ./...":                    false,
		"cd x && go test ./...":           false,
		"go build ./...":                  false,
	} {
		if got := sameWideRun(cmd, verify); got != want {
			t.Errorf("sameWideRun(%q) = %v, muốn %v", cmd, got, want)
		}
	}
}

// Chỉ chặn trong tác vụ B′ đang in_progress (BINT_TASK_ID của tiến trình agent); classic và tác vụ đã xong không bị chặn.
func TestWideTestDenial(t *testing.T) {
	rt := t.TempDir()
	st := task.NewStore(rt)
	mk := func(variant, status string) string {
		tk, err := st.Create(task.Ticket{Goal: "x", Verify: task.Verify{Command: "go test ./..."}}, task.State{}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Update(tk.State.ID, time.Now(), func(t *task.Task) error {
			t.State.Classification = &task.Classification{Variant: variant}
			t.State.Status = status
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return tk.State.ID
	}
	bp := mk(task.VariantBPrime, task.StatusInProgress)
	t.Setenv("BINT_TASK_ID", bp)
	if why := wideTestDenial(rt, "go test ./..."); why == "" {
		t.Error("B′ in_progress: lệnh kiểm chứng đầy đủ phải bị chặn")
	}
	if why := wideTestDenial(rt, "go test ./internal/a"); why != "" {
		t.Errorf("lệnh hẹp bị chặn: %s", why)
	}
	t.Setenv("BINT_TASK_ID", mk("", task.StatusInProgress))
	if why := wideTestDenial(rt, "go test ./..."); why != "" {
		t.Errorf("classic bị chặn: %s", why)
	}
	t.Setenv("BINT_TASK_ID", mk(task.VariantBPrime, task.StatusInReview))
	if why := wideTestDenial(rt, "go test ./..."); why != "" {
		t.Errorf("B′ ngoài b-feat bị chặn: %s", why)
	}
	t.Setenv("BINT_TASK_ID", "")
	if why := wideTestDenial(rt, "go test ./..."); why != "" {
		t.Errorf("không có tác vụ mà vẫn chặn: %s", why)
	}
}
