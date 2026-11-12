package app

import (
	"strings"
	"testing"

	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// Review 6b .4/.10: hai mô hình khác ReadOnly trong cùng tiến trình viết lại thông điệp đúng theo từng mô hình, bất kể thứ tự dựng
// (chế độ chỉ đọc không còn là biến toàn cục của gói ui).
func TestTwoModelsKeepTheirOwnReadOnlyNote(t *testing.T) {
	const msg = "duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`"
	for _, order := range [][2]bool{{false, true}, {true, false}} {
		var ms [2]*Model
		for i, ro := range order {
			ms[i] = newModel(t, Options{Mode: theme.Mono, ReadOnly: ro}, sampleData(), 140, 40)
		}
		// Kiểm sau khi CẢ HAI đã dựng: mô hình dựng sau không được đổi kết quả của mô hình dựng trước.
		for i, m := range ms {
			m.setToast(msg, false)
			label := plain(m.toastLabel())
			if !strings.Contains(label, "phím a ở Tri thức") {
				t.Fatalf("mô hình %d (thứ tự %v): thiếu tham chiếu TUI: %q", i, order, label)
			}
			if got := strings.Contains(label, "chỉ đọc"); got != order[i] {
				t.Errorf("mô hình %d (ReadOnly=%v, thứ tự dựng %v): ghi chú chỉ đọc=%v: %q", i, order[i], order, got, label)
			}
		}
	}
}

// Mô hình có sổ hành động tuỳ chỉnh (phím khác) viết lại theo phím của sổ đó.
func TestModelWithCustomRegistryRewritesByItsKeys(t *testing.T) {
	var list []actions.Action
	for _, a := range actions.Default().All() {
		if a.ID == "knowledge.approve" {
			a.Key = "Y"
		}
		list = append(list, a)
	}
	m := newModel(t, Options{Mode: theme.Mono, Registry: actions.NewRegistry(list)}, sampleData(), 140, 40)
	m.setToast("duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`", false)
	if label := plain(m.toastLabel()); !strings.Contains(label, "phím Y ở Tri thức") || strings.Contains(label, "phím a ở") {
		t.Fatalf("phải theo phím của sổ tuỳ chỉnh: %q", label)
	}
	// Mô hình mặc định dựng cùng tiến trình vẫn dùng phím a.
	d := newModel(t, Options{Mode: theme.Mono}, sampleData(), 140, 40)
	d.setToast("duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`", false)
	if label := plain(d.toastLabel()); !strings.Contains(label, "phím a ở Tri thức") {
		t.Fatalf("mô hình mặc định: %q", label)
	}
}

// Đổi ngôn ngữ dựng lại Rewriter cho mọi màn (danh mục mới) mà vẫn giữ chế độ chỉ đọc và sổ của mô hình.
func TestRewriterSurvivesLanguageChange(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, ReadOnly: true}, sampleData(), 140, 40)
	m.cycleLanguage() // vi → en
	m.setToast("duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`", false)
	label := plain(m.toastLabel())
	if !strings.Contains(label, "key a in Knowledge") || !strings.Contains(label, "read-only") {
		t.Fatalf("sau đổi ngôn ngữ phải viết lại bằng en và giữ chỉ đọc: %q", label)
	}
}
