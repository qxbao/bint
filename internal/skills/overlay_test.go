package skills

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bint/internal/fsafe"
)

const coreDoc = "---\nname: x\n---\n# X\n\n<!-- bint:anchor rules -->\nmặc định\n<!-- bint:anchor-end rules -->\n\ngiữa\n\n<!-- bint:anchor checklist -->\n<!-- bint:anchor-end checklist -->\ncuối\n"

func TestComposeModes(t *testing.T) {
	s := Skill{Name: "x", Content: []byte(coreDoc)}
	as, err := s.Anchors()
	if err != nil || len(as) != 2 || as[0].Name != "rules" || strings.TrimSpace(as[0].Default) != "mặc định" {
		t.Fatalf("anchors: %+v %v", as, err)
	}
	ov, err := ParseOverlay("x", "o.md", []byte("ghi chú bị bỏ qua\n<!-- bint:overlay rules append -->\nthêm A\n<!-- bint:overlay-end -->\n"+
		"    <!-- bint:overlay checklist -->\n<!-- bint:overlay checklist replace -->\n- mục 1\n<!-- bint:overlay-end -->\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compose(s, ov)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"mặc định\n\nthêm A\n<!-- bint:anchor-end rules -->", "<!-- bint:anchor checklist -->\n- mục 1\n<!-- bint:anchor-end checklist -->\ncuối"} {
		if !strings.Contains(got, want) {
			t.Fatalf("thiếu %q trong:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ghi chú bị bỏ qua") {
		t.Fatal("văn bản ngoài khối overlay phải bị bỏ qua")
	}
	// Không có lớp phủ: giữ nguyên byte.
	if out, _ := Compose(s, nil); string(out) != coreDoc {
		t.Fatal("không lớp phủ phải giữ nguyên")
	}
	pre, _ := ParseOverlay("x", "o.md", []byte("<!-- bint:overlay rules prepend -->\nB\n<!-- bint:overlay-end -->\n"))
	if out, _ := Compose(s, pre); !strings.Contains(string(out), "B\n\nmặc định") {
		t.Fatalf("prepend:\n%s", out)
	}
}

func TestOverlayErrors(t *testing.T) {
	for name, src := range map[string]string{
		"không đóng":   "<!-- bint:overlay rules -->\nx\n",
		"chế độ lạ":    "<!-- bint:overlay rules merge -->\nx\n<!-- bint:overlay-end -->\n",
		"trùng neo":    "<!-- bint:overlay rules -->\n<!-- bint:overlay-end -->\n<!-- bint:overlay rules -->\n<!-- bint:overlay-end -->\n",
		"khai báo neo": "<!-- bint:anchor rules -->\n<!-- bint:anchor-end rules -->\n",
		"end lẻ":       "<!-- bint:overlay-end -->\n",
	} {
		if _, err := ParseOverlay("x", "o.md", []byte(src)); err == nil {
			t.Errorf("%s: phải lỗi", name)
		}
	}
	s := Skill{Name: "x", Content: []byte(coreDoc)}
	ov, _ := ParseOverlay("x", "o.md", []byte("<!-- bint:overlay gone -->\ny\n<!-- bint:overlay-end -->\n"))
	_, err := Compose(s, ov)
	var ce *ConflictError
	if !errors.As(err, &ce) || ce.Missing[0] != "gone" {
		t.Fatalf("neo không còn phải là xung đột: %v", err)
	}
}

func TestCoreSkillsHaveAnchors(t *testing.T) {
	want := map[string]string{"b-req": "project-rules", "b-feat": "test-strategy", "b-review": "review-checklist", "b-do": "routing"}
	for name, anchor := range want {
		s, ok := Get(name)
		if !ok {
			t.Fatalf("thiếu skill %s", name)
		}
		as, err := s.Anchors()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		found := false
		for _, a := range as {
			found = found || a.Name == anchor
		}
		if !found || !strings.HasPrefix(string(s.Content), "---\nname: "+name+"\n") {
			t.Fatalf("%s: thiếu điểm neo %s hoặc phần đầu sai", name, anchor)
		}
		tpl, err := OverlayTemplate(s)
		if err != nil {
			t.Fatal(err)
		}
		// Mẫu lớp phủ chưa có hiệu lực (khối thụt lề).
		if ov, err := ParseOverlay(name, "t.md", []byte(tpl)); err != nil || len(ov.Patches) != 0 {
			t.Fatalf("%s: mẫu lớp phủ phải hợp lệ và chưa có khối nào: %v", name, err)
		}
	}
}

// Nâng cấp: lớp phủ dùng điểm neo không còn → xung đột, giữ bản đã xuất bản và mục
// core.lock cũ; sửa lớp phủ thì áp lại được.
func TestInstallAppliesOverlayAndKeepsOldOnConflict(t *testing.T) {
	root := t.TempDir()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	opts := InstallOptions{Root: root, Targets: []Target{ClaudeTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now()}
	ovPath := filepath.Join(root, filepath.FromSlash(OverlayPath("b-review")))
	os.MkdirAll(filepath.Dir(ovPath), 0o755)
	os.WriteFile(ovPath, []byte("<!-- bint:overlay review-checklist -->\n- Kiểm tra quyền truy cập theo tenant.\n<!-- bint:overlay-end -->\n"), 0o644)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	pub := filepath.Join(root, ".claude", "skills", "b-review", "SKILL.md")
	b, _ := os.ReadFile(pub)
	if !strings.Contains(string(b), "Kiểm tra quyền truy cập theo tenant.") {
		t.Fatal("lớp phủ chưa được áp")
	}
	lock, _ := ReadLock(root)
	e := lock.Skills["b-review"]
	core, _ := Get("b-review")
	if e.SHA256 != core.Hash() || e.Overlay == "" || e.Composed != (Skill{Content: b}).Hash() {
		t.Fatalf("core.lock: %+v", e)
	}
	// Chạy lại: không đổi.
	outs, _ := Install(opts)
	for _, o := range outs {
		if o.Skill == "b-review" && o.Result != fsafe.Unchanged {
			t.Fatalf("lần hai: %+v", o)
		}
	}

	// Mô phỏng nâng cấp làm mất điểm neo: lớp phủ trỏ tới neo không tồn tại.
	os.WriteFile(ovPath, []byte("<!-- bint:overlay old-checklist -->\nx\n<!-- bint:overlay-end -->\n"), 0o644)
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	conflict := false
	for _, o := range outs {
		if o.Skill == "b-review" {
			conflict = o.Result == fsafe.Conflict && strings.Contains(o.Detail, "old-checklist")
		}
	}
	if !conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
	if b2, _ := os.ReadFile(pub); string(b2) != string(b) {
		t.Fatal("bản đã xuất bản phải được giữ")
	}
	if l2, _ := ReadLock(root); !reflect.DeepEqual(l2.Skills["b-review"], e) {
		t.Fatalf("mục core.lock cũ phải được giữ: %+v", l2.Skills["b-review"])
	}

	// Gỡ lớp phủ: xuất bản lại bản gốc (tệp cũ khớp core.lock → coi là nguyên bản).
	os.Remove(ovPath)
	outs, _ = Install(opts)
	for _, o := range outs {
		if o.Skill == "b-review" && o.Result != fsafe.Updated {
			t.Fatalf("phải cập nhật về bản gốc: %+v", o)
		}
	}
	if b3, _ := os.ReadFile(pub); string(b3) != string(core.Content) {
		t.Fatal("bản gốc chưa được xuất bản lại")
	}

	// Lớp phủ cho skill không tồn tại.
	os.WriteFile(filepath.Join(filepath.Dir(ovPath), "b-nope.md"), []byte(""), 0o644)
	outs, _ = Install(opts)
	if last := outs[len(outs)-1]; last.Result != fsafe.Conflict || last.Skill != "b-nope" {
		t.Fatalf("lớp phủ mồ côi: %+v", last)
	}
}
