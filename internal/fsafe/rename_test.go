package fsafe

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Người đọc giữ tệp đích trong chốc lát (TUI quét trạng thái) không được làm WriteAtomic lỗi: trên Windows đổi tên
// đè lên tệp đang mở trả Access is denied, rename phải thử lại tới khi người đọc đóng tệp.
func TestWriteAtomicWhileReaderHoldsTarget(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.Close()
	}()
	if err := WriteAtomic(p, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteAtomic khi có người đọc: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "new" {
		t.Fatalf("nội dung: %q", b)
	}
}
