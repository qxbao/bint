package knowledge

import "testing"

// Ký hiệu định danh đầy đủ (Lớp.phương_thức) không xuất hiện nguyên văn trong vùng mã.
func TestContainsSymbolQualified(t *testing.T) {
	region := []byte("    def __post_init__(self) -> None:\n        s = self.session\n")
	for sym, want := range map[string]bool{
		"RunPaths.__post_init__": true, "__post_init__": true, "pkg::__post_init__": true,
		"RunPaths._sfx": false, "post_init": false,
	} {
		if got := containsSymbol(region, sym); got != want {
			t.Errorf("containsSymbol(%q) = %v, mong đợi %v", sym, got, want)
		}
	}
}

// Agent ghi lệch vùng dòng: bằng chứng được đặt lại theo định nghĩa của ký hiệu.
func TestSnapEvidence(t *testing.T) {
	src := []byte("x = 1\n\n\ndef helper():\n    return 2\n\n\ndef target(a):\n    return a\n")
	e := SnapEvidence(Evidence{Path: "m.py", Lines: "1-2", Symbol: "target"}, src)
	if e.Lines != "8-9" {
		t.Fatalf("mong đợi 8-9, được %q", e.Lines)
	}
	ok := Evidence{Path: "m.py", Lines: "4-5", Symbol: "helper"}
	if SnapEvidence(ok, src) != ok {
		t.Fatal("bằng chứng đúng không được đổi")
	}
	missing := Evidence{Path: "m.py", Lines: "1-2", Symbol: "nope"}
	if SnapEvidence(missing, src) != missing {
		t.Fatal("ký hiệu không có thì giữ nguyên để kiểm tra báo lỗi")
	}
}
