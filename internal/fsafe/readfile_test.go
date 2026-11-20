package fsafe

import (
	"path/filepath"
	"sync"
	"testing"
)

// ReadFile đọc không lỗi trong lúc WriteAtomic thay tệp liên tục (Windows: sharing violation phía đọc).
func TestReadFileDuringAtomicWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := WriteAtomic(p, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			if err := WriteAtomic(p, []byte("0123456789"), 0o600); err != nil {
				t.Error(err)
				break
			}
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			wg.Wait()
			return
		default:
		}
		if b, err := ReadFile(p); err != nil || len(b) != 10 {
			t.Fatalf("ReadFile: %q %v", b, err)
		}
	}
}
