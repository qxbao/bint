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
