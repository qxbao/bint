package ulid

import (
	"testing"
	"time"
)

func TestFormatAndOrder(t *testing.T) {
	a := At(time.UnixMilli(1000))
	b := At(time.UnixMilli(2000))
	if !Valid(a) || !Valid(b) {
		t.Fatalf("invalid: %s %s", a, b)
	}
	if a[:10] >= b[:10] {
		t.Fatalf("time prefix not ordered: %s %s", a, b)
	}
	if At(time.UnixMilli(0))[:10] != "0000000000" {
		t.Fatal("zero time prefix")
	}
}

func TestUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10000; i++ {
		id := New()
		if seen[id] {
			t.Fatal("duplicate")
		}
		seen[id] = true
	}
}
