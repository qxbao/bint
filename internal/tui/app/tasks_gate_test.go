package app

import (
	"strings"
	"testing"

	bintapp "bint/internal/app"
	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// taskActionsWhere trả các hành động của màn Tác vụ thoả pred.
func taskActionsWhere(pred func(actions.Action) bool) []actions.Action {
	var out []actions.Action
	for _, a := range actions.Default().All() {
		if a.Screen == actions.Tasks && pred(a) {
			out = append(out, a)
		}
	}
	return out
}

// Ở --read-only mọi hành động ghi của màn Tác vụ bị từ chối qua phím tắt, bảng lệnh và yêu cầu trực tiếp: không mở
// form, không có việc nền, không có mặt trong bảng lệnh.
func TestTaskWriteActionsRefusedInReadOnly(t *testing.T) {
	writes := taskActionsWhere(func(a actions.Action) bool { return a.Writes })
	if len(writes) < 10 {
		t.Fatalf("quá ít hành động ghi của màn Tác vụ: %d", len(writes))
	}
	m := newModel(t, Options{Mode: theme.Mono, ReadOnly: true}, sampleData(), 140, 40)
	press(t, m, "3")
	// Bảng lệnh không liệt kê hành động ghi.
	inPalette := map[string]bool{}
	for _, it := range m.paletteItems() {
		inPalette[it.ID] = true
	}
	for _, a := range writes {
		if inPalette[a.ID] {
			t.Errorf("%s (Writes) không được có trong bảng lệnh ở chế độ chỉ đọc", a.ID)
		}
	}
	// Phím tắt: không mở form/hộp thoại, báo "chỉ đọc".
	for _, a := range writes {
		if len(a.Key) != 1 {
			continue
		}
		m.toast = ""
		press(t, m, a.Key)
		if m.tk.Capturing() {
			t.Errorf("phím %s (%s) mở form dù chỉ đọc", a.Key, a.ID)
			press(t, m, "esc")
		}
		if out := plain(m.Render()); !strings.Contains(out, "chế độ chỉ đọc") {
			t.Errorf("phím %s (%s) phải báo chế độ chỉ đọc:\n%s", a.Key, a.ID, out)
		}
	}
	// Chọn từ bảng lệnh (run) và yêu cầu trực tiếp cũng bị chặn.
	for _, a := range writes {
		if _, cmd := m.run(a.ID); cmd != nil {
			t.Errorf("run(%s) ở chế độ chỉ đọc phải không làm gì", a.ID)
		}
		_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
		drive(t, m, cmd)
		if m.jobs.Len() != 0 {
			t.Errorf("RequestMsg %s ở chế độ chỉ đọc không được bắt đầu việc", a.ID)
		}
	}
	// Hành động đọc vẫn dùng được: bảng chuyển trạng thái và kiểm tài liệu.
	press(t, m, "T")
	if !m.tk.Capturing() || !strings.Contains(plain(m.Render()), "Bảng chuyển trạng thái tác vụ") {
		t.Fatalf("T phải mở bảng chuyển ở chế độ chỉ đọc:\n%s", plain(m.Render()))
	}
}

// Trong phiên agent (CLAUDECODE=1) hành động HumanOnly bị khoá kèm lý do: phím, bảng lệnh và yêu cầu trực tiếp đều
// bị từ chối; hành động ghi không HumanOnly (tạo tác vụ) vẫn dùng được.
func TestHumanOnlyTaskActionsLockedInAgentSession(t *testing.T) {
	asHuman(t)
	t.Setenv("CLAUDECODE", "1")
	inAgent, _ := bintapp.InAgentSession()
	if !inAgent {
		t.Fatal("CLAUDECODE=1 phải là phiên agent")
	}
	humanOnly := taskActionsWhere(func(a actions.Action) bool { return a.HumanOnly })
	if len(humanOnly) == 0 {
		t.Fatal("màn Tác vụ phải có hành động chỉ dành cho người (duyệt)")
	}
	m := newModel(t, Options{Mode: theme.Mono, Agent: inAgent}, sampleData(), 140, 40)
	press(t, m, "3")
	for _, a := range humanOnly {
		locked := ""
		for _, it := range m.paletteItems() {
			if it.ID == a.ID {
				locked = it.Locked
			}
		}
		if locked == "" {
			t.Errorf("%s phải hiện trong bảng lệnh ở trạng thái khoá", a.ID)
		}
		if len(a.Key) == 1 {
			m.toast = ""
			press(t, m, a.Key)
			if m.tk.Capturing() {
				t.Errorf("phím %s (%s) mở hộp thoại dù bị khoá", a.Key, a.ID)
				press(t, m, "esc")
			}
			if out := plain(m.Render()); !strings.Contains(out, "chỉ dành cho người") {
				t.Errorf("phím %s phải báo khoá:\n%s", a.Key, out)
			}
		}
		if _, cmd := m.run(a.ID); cmd != nil {
			t.Errorf("run(%s) trong phiên agent phải không làm gì", a.ID)
		}
		_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
		drive(t, m, cmd)
		if m.jobs.Len() != 0 {
			t.Errorf("%s: việc bị khoá không được chạy", a.ID)
		}
	}
	// Tạo tác vụ (không HumanOnly) vẫn mở được form.
	press(t, m, "n")
	if !m.tk.Capturing() {
		t.Fatal("tạo tác vụ không phải thao tác chỉ dành cho người")
	}
}
