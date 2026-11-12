package mcpgw

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"bint/internal/ulid"
)

func TestRunBindingRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	b := RunBinding{RunID: ulid.New(), TaskID: ulid.New(), Step: "b-review", Role: "reviewer", ReadOnly: true, Worktree: "/w"}
	path, err := WriteRunBinding(dir, b)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := RunBindingPath(dir, b.RunID); path != want {
		t.Errorf("đường dẫn %s, muốn %s", path, want)
	}
	if st, err := os.Stat(path); err != nil || (!isWindows && st.Mode().Perm() != 0o600) {
		t.Errorf("quyền tệp: %v %v", st, err)
	}
	got, err := ReadRunBinding(dir, b.RunID)
	if err != nil || got != b {
		t.Fatalf("đọc lại: %+v %v", got, err)
	}
	id, err := ResolveIdentity(dir, b.RunID, b.TaskID)
	if err != nil || id.Role != "reviewer" || !id.ReadOnly {
		t.Errorf("ResolveIdentity: %+v %v", id, err)
	}
	RemoveRunBinding(dir, b.RunID)
	if _, err := ReadRunBinding(dir, b.RunID); !errors.Is(err, ErrNoBinding) {
		t.Errorf("sau khi xoá: %v", err)
	}
	if _, err := ResolveIdentity(dir, b.RunID, ""); !errors.Is(err, ErrNoBinding) {
		t.Errorf("không bản ghi phải lỗi: %v", err)
	}
	if id, err := ResolveIdentity("", "", ""); err != nil || id.Role != "interactive" || id.ReadOnly {
		t.Errorf("không run_id: %+v %v", id, err)
	}
	if _, err := ResolveIdentity("", ulid.New(), ""); err == nil {
		t.Error("run_id mà không có thư mục runtime phải lỗi")
	}
}

func TestRunBindingRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"../x", "a/b", `a\b`, "..", "", "01RUN", ulid.New() + "/../../x", ulid.New() + ".json"} {
		if _, err := RunBindingPath(dir, id); err == nil {
			t.Errorf("run_id %q phải bị từ chối", id)
		}
		if _, err := WriteRunBinding(dir, RunBinding{RunID: id}); err == nil {
			t.Errorf("WriteRunBinding(%q) phải lỗi", id)
		}
		if _, err := ReadRunBinding(dir, id); err == nil {
			t.Errorf("ReadRunBinding(%q) phải lỗi", id)
		}
	}
	// Không tạo gì ngoài dir.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("run_id xấu để lại tệp: %v", entries)
	}
	// Tệp có run_id khác tên tệp bị từ chối (không tin nội dung lệch).
	a, b := ulid.New(), ulid.New()
	if _, err := WriteRunBinding(dir, RunBinding{RunID: a}); err != nil {
		t.Fatal(err)
	}
	pa, _ := RunBindingPath(dir, a)
	pb, _ := RunBindingPath(dir, b)
	data, _ := os.ReadFile(pa)
	_ = os.WriteFile(pb, data, 0o600)
	if _, err := ReadRunBinding(dir, b); err == nil {
		t.Error("tệp có run_id lệch tên phải bị từ chối")
	}
}
