package app

import (
	"strings"
	"testing"
)

// Chia lô pathspec: mỗi lô không vượt giới hạn, giữ nguyên thứ tự, không mất đường dẫn (phát hiện review .3).
func TestPathBatches(t *testing.T) {
	var paths []string
	for i := 0; i < 1000; i++ {
		paths = append(paths, "internal/some/deep/package/"+strings.Repeat("x", 40)+"_"+string(rune('a'+i%26))+".go")
	}
	batches := pathBatches(paths, 16000)
	if len(batches) < 2 {
		t.Fatalf("1000 đường dẫn dài phải chia nhiều lô, được %d", len(batches))
	}
	var flat []string
	for _, b := range batches {
		n := 0
		for _, p := range b {
			n += len(p) + 1
		}
		if n > 16000 {
			t.Fatalf("lô %d byte vượt giới hạn", n)
		}
		flat = append(flat, b...)
	}
	if len(flat) != len(paths) || flat[0] != paths[0] || flat[len(flat)-1] != paths[len(paths)-1] {
		t.Fatalf("mất hoặc đổi thứ tự đường dẫn: %d/%d", len(flat), len(paths))
	}
	if got := pathBatches(nil, 100); len(got) != 0 {
		t.Fatalf("rỗng phải không có lô: %v", got)
	}
	// Một đường dẫn dài hơn giới hạn vẫn đi một mình.
	if got := pathBatches([]string{strings.Repeat("y", 50)}, 10); len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("%v", got)
	}
}
