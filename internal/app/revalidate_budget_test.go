package app

import "testing"

// F13: lô đầu không chạy khi ngân sách nhỏ hơn chi phí tiên nghiệm; có số đo thì dùng số đo.
func TestBatchUnaffordable(t *testing.T) {
	if batchUnaffordable(0.01, 0, 0, 0.5) == "" {
		t.Fatal("ngân sách $0,01 < tiên nghiệm $0,50 phải dừng")
	}
	if batchUnaffordable(0.6, 0, 0, 0.5) != "" {
		t.Fatal("ngân sách $0,60 đủ cho lô đầu")
	}
	if batchUnaffordable(0.6, 1.0, 1, 0.5) == "" {
		t.Fatal("đã đo $1,00/lô, còn $0,60 phải dừng")
	}
	if batchUnaffordable(0.01, 0, 0, 0) != "" {
		t.Fatal("tiên nghiệm 0 giữ hành vi cũ")
	}
	if batchUnaffordable(0, 0, 0, 0) == "" {
		t.Fatal("hết ngân sách phải dừng")
	}
}
