package gitrepo

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Lệnh git bị giết vì ctx bị huỷ phải cho ra lỗi nhận được bằng errors.Is(context.Canceled) trên mọi hệ điều hành
// (Windows chỉ báo "exit status 1"); lỗi thường thì không.
func TestCommandErrorWrapsContextCancel(t *testing.T) {
	plain := errors.New("exit status 1")
	if err := CommandError(context.Background(), []string{"diff"}, plain, " boom \n"); errors.Is(err, context.Canceled) || !errors.Is(err, plain) {
		t.Fatalf("lỗi thường không được coi là huỷ: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, t.TempDir(), "--version")
	if err == nil {
		t.Fatal("ctx đã huỷ thì git phải lỗi")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lỗi phải bọc context.Canceled: %v", err)
	}

	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	<-dctx.Done()
	if err := CommandError(dctx, []string{"diff"}, plain, ""); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, plain) {
		t.Fatalf("lỗi phải bọc cả DeadlineExceeded lẫn lỗi gốc: %v", err)
	}
}
