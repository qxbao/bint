package iso

import (
	"os"
	"testing"
)

func TestSessionLock(t *testing.T) {
	p := mustCreate(t, testStore(t, nil), Codex, "w")
	s1, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatalf("S1: busy=%v err=%v", busy, err)
	}
	s2, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatalf("hai khoá chia sẻ cùng giữ được: busy=%v err=%v", busy, err)
	}
	if x, busy, err := p.Lock(true); err != nil || !busy || x != nil {
		t.Fatalf("X khi còn S phải bận: busy=%v err=%v", busy, err)
	}
	if err := s1(); err != nil {
		t.Fatal(err)
	}
	if _, busy, _ := p.Lock(true); !busy {
		t.Fatal("còn một S thì X vẫn bận")
	}
	if err := s2(); err != nil {
		t.Fatal(err)
	}
	if err := s2(); err != nil {
		t.Fatalf("release gọi lại phải an toàn: %v", err)
	}
	x, busy, err := p.Lock(true)
	if err != nil || busy {
		t.Fatalf("nhả hết thì X được: busy=%v err=%v", busy, err)
	}
	if s, busy, _ := p.Lock(false); !busy || s != nil {
		t.Fatal("S khi còn X phải bận")
	}
	if _, busy, _ := p.Lock(true); !busy {
		t.Fatal("hai X phải bận")
	}
	if err := x(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LockPath()); err != nil {
		t.Fatalf("tệp khoá phải được giữ lại sau khi nhả: %v", err)
	}
	s3, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatalf("sau X lại lấy S được: %v %v", busy, err)
	}
	s3()
}

func TestSessionLockMissingDir(t *testing.T) {
	p := mustCreate(t, testStore(t, nil), Codex, "w")
	p.Dir += "-khong-co"
	if _, _, err := p.Lock(false); err == nil {
		t.Fatal("thiếu thư mục profile phải lỗi")
	}
}
