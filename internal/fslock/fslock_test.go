package fslock

import (
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T, p string) *os.File {
	t.Helper()
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestTryExclusiveAndShared(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	a, b, c := open(t, p), open(t, p), open(t, p)
	if busy, err := Try(a, true); busy || err != nil {
		t.Fatalf("a: busy=%v err=%v", busy, err)
	}
	if busy, err := Try(b, true); !busy || err != nil {
		t.Fatalf("độc quyền thứ hai phải bận: busy=%v err=%v", busy, err)
	}
	if busy, err := Try(c, false); !busy || err != nil {
		t.Fatalf("chia sẻ khi có độc quyền phải bận: busy=%v err=%v", busy, err)
	}
	a.Close() // đóng ⇒ nhả
	if busy, err := Try(b, false); busy || err != nil {
		t.Fatalf("sau khi nhả: busy=%v err=%v", busy, err)
	}
	if busy, err := Try(c, false); busy || err != nil {
		t.Fatalf("hai chia sẻ cùng giữ được: busy=%v err=%v", busy, err)
	}
}
