package task

import (
	"os"
	"strings"
	"testing"
)

func syncReq(crit ...string) string {
	var b strings.Builder
	b.WriteString("# Yêu cầu\n\n## Mục tiêu và phạm vi\n\nGiữ nguyên  khoảng  trắng — dòng này.\n\n## Tiêu chí chấp nhận\n\n")
	for i, c := range crit {
		b.WriteString("- [ ] AC-" + string(rune('1'+i)) + ": " + c + "\n")
	}
	b.WriteString("\n## Ngoài phạm vi\n\n- Không có\n\n## Câu hỏi mở\n")
	return b.String()
}

func TestSyncRequirementTextTable(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		crit  []string
		want  string
		notes int
	}{
		{"không đổi", syncReq("a", "b"), []string{"a", "b"}, syncReq("a", "b"), 0},
		{"thêm AC mới nguyên văn, số tiếp theo", syncReq("a"), []string{"a", "b mới"}, syncReq("a", "b mới"), 1},
		{"clear thu gọn", syncReq("a", "b", "c"), []string{"x"}, syncReq("x"), 3},
		{"clear mở rộng", syncReq("a"), []string{"x", "y", "z"}, syncReq("x", "y", "z"), 3},
		{"giữ [x] khi nội dung không đổi", strings.Replace(syncReq("a", "b"), "- [ ] AC-1", "- [x] AC-1", 1), []string{"a", "b", "c"},
			strings.Replace(syncReq("a", "b", "c"), "- [ ] AC-1", "- [x] AC-1", 1), 1},
		{"chuẩn hoá khoảng trắng không bị coi là đổi", syncReq("a  b"), []string{"a b"}, syncReq("a  b"), 0},
		{"thiếu mục AC: chèn trước Ngoài phạm vi",
			"# Yêu cầu\n\nMô tả\n\n## Ngoài phạm vi\n\nKhông có\n",
			[]string{"a"},
			"# Yêu cầu\n\nMô tả\n\n## Tiêu chí chấp nhận\n\n- [ ] AC-1: a\n\n## Ngoài phạm vi\n\nKhông có\n", 2},
		{"thiếu mục AC và Ngoài phạm vi: chèn cuối tệp",
			"# Yêu cầu\n\nMô tả\n", []string{"a"},
			"# Yêu cầu\n\nMô tả\n\n## Tiêu chí chấp nhận\n\n- [ ] AC-1: a\n", 2},
		{"AC trong code fence bị bỏ qua",
			"## Tiêu chí chấp nhận\n```\n- [ ] AC-1: giả\n```\n- [ ] AC-1: thật\n", []string{"thật", "hai"},
			"## Tiêu chí chấp nhận\n```\n- [ ] AC-1: giả\n```\n- [ ] AC-1: thật\n- [ ] AC-2: hai\n", 1},
	}
	for _, c := range cases {
		got, notes := syncRequirementText(c.in, c.crit)
		if got != c.want {
			t.Errorf("%s:\n--- got\n%s\n--- want\n%s", c.name, got, c.want)
		}
		if c.name != "clear thu gọn" && len(notes) != c.notes {
			t.Errorf("%s: %d ghi chú %v, muốn %d", c.name, len(notes), notes, c.notes)
		}
	}
}

func TestSyncRequirementPreservesCRLF(t *testing.T) {
	in := strings.ReplaceAll(syncReq("a"), "\n", "\r\n")
	got, _ := syncRequirementText(in, []string{"a", "b"})
	want := strings.ReplaceAll(syncReq("a", "b"), "\n", "\r\n")
	if got != want {
		t.Fatalf("CRLF:\n%q\n%q", got, want)
	}
}

func TestSyncTasksText(t *testing.T) {
	in := "# Việc\n- [ ] TSK-1 [AC-1]: sửa — xong khi: ok\n- [ ] TSK-4 [AC-2, AC-9]: khác — xong khi: ok\n"
	got, notes := syncTasksText(in, 3, "ID")
	if !strings.Contains(got, "- [ ] TSK-5 [AC-3]: ") || strings.Contains(got, "TSK-5 [AC-1]") || strings.Contains(got, "TSK-5 [AC-2]") {
		t.Fatalf("chỉ AC-3 chưa có TSK:\n%s", got)
	}
	if !strings.HasPrefix(got, in) || !strings.HasSuffix(got, "\n") {
		t.Fatalf("phần cũ phải giữ nguyên:\n%s", got)
	}
	var sawRef bool
	for _, n := range notes {
		if strings.Contains(n, "TSK-4") && strings.Contains(n, "AC-9") {
			sawRef = true
		}
	}
	if !sawRef {
		t.Fatalf("thiếu ghi chú TSK tham chiếu AC đã bỏ: %v", notes)
	}
	if again, _ := syncTasksText(got, 3, "ID"); again != got {
		t.Fatalf("đồng bộ phải bất biến khi đã đủ:\n%s", again)
	}
}

func newSyncTask(t *testing.T, crit []string, req, tasks string) *Task {
	t.Helper()
	dir := t.TempDir()
	tk := &Task{Dir: dir, Ticket: Ticket{Goal: "g", Autonomy: AutonomyAskOnProtected, Verify: Verify{Criteria: crit}}, State: State{ID: "ID"}}
	if req != "" {
		os.WriteFile(tk.ArtifactPath("requirement"), []byte(req), 0o600)
	}
	if tasks != "" {
		os.WriteFile(tk.ArtifactPath("tasks"), []byte(tasks), 0o600)
	}
	os.WriteFile(tk.ArtifactPath("plan"), []byte("# Kế hoạch\nx\n"), 0o600)
	return tk
}

func TestSyncCriteriaArtifactsLintsClean(t *testing.T) {
	tk := newSyncTask(t, []string{"a", "b", "c"}, syncReq("a"), "- [ ] TSK-1 [AC-1]: sửa — xong khi: ok\n")
	written, notes, err := SyncCriteriaArtifacts(tk)
	if err != nil || len(written) != 2 || len(notes) == 0 {
		t.Fatalf("written=%v notes=%v err=%v", written, notes, err)
	}
	if r := LintPlanningTask(tk, true); r.Blocking() {
		t.Fatalf("lint strict phải sạch sau đồng bộ: %v", r.Errors)
	}
	// Lần hai không còn gì để ghi.
	if w, _, err := SyncCriteriaArtifacts(tk); err != nil || len(w) != 0 {
		t.Fatalf("lần hai: %v %v", w, err)
	}
}

func TestSyncCriteriaArtifactsNoRequirementIsNoop(t *testing.T) {
	tk := newSyncTask(t, []string{"a"}, "", "")
	if w, n, err := SyncCriteriaArtifacts(tk); err != nil || len(w) != 0 || len(n) != 0 {
		t.Fatalf("%v %v %v", w, n, err)
	}
	if _, err := os.Stat(tk.ArtifactPath("requirement")); !os.IsNotExist(err) {
		t.Fatalf("không được tạo requirement.md: %v", err)
	}
}
