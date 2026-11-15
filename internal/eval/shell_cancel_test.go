package eval

import (
	"context"
	"errors"
	"testing"
)

// Khi ctx đã huỷ mà lỗi không phải ExitError (Windows: "TerminateProcess: Access is denied"), Err phải là ctx.Err().
func TestRunProcessCancelledNonExitErrorReportsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := runProcess(ctx, t.TempDir(), "bint-no-such-program-xyz", nil, 0, "", nil, nil, nil)
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, muốn context.Canceled", res.Err)
	}
}
