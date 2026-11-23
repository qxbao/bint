package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"bint/internal/fsafe"
)

// withCore thay danh sách skill gốc trong một test (bản nhúng chưa chắc có skill nhiều tệp phụ lồng thư mục).
func withCore(t *testing.T, list func() []Skill) {
	t.Helper()
	old := loadCore
	loadCore = list
	t.Cleanup(func() { loadCore = old })
}

func coreOutcome(t *testing.T, outs []Outcome, rel string) Outcome {
	t.Helper()
	for _, o := range outs {
		if o.Path == rel {
			return o
		}
	}
	t.Fatalf("thiếu kết quả cho %s: %+v", rel, outs)
	return Outcome{}
}

// TSK-1.0: skill gốc có tệp phụ lồng thư mục được xuất bản cho mọi target, băm từng tệp trong core.lock, sửa tay →
// xung đột, tệp bị bỏ ở bản mới được gỡ (trừ khi đã sửa tay), thư mục tests/ không được xuất bản.
func TestInstallCoreAuxFiles(t *testing.T) {
	skill := []byte("---\nname: demo\ndescription: d\ndisable-model-invocation: true\n---\n\nXem [tests.md](tests.md).\n")
	files := []CustomFile{
		{Rel: "agents/openai.yaml", Content: []byte("policy:\n  allow_implicit_invocation: false\n"), Mode: 0o644},
		{Rel: "scripts/loop.sh", Content: []byte("#!/bin/sh\necho hi\n"), Mode: 0o755},
		{Rel: "tests.md", Content: []byte("# tests\n"), Mode: 0o644},
	}
	cur := []Skill{{Name: "demo", Content: skill, Files: files}}
	withCore(t, func() []Skill { return cur })

	root := t.TempDir()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	opts := InstallOptions{Root: root, Targets: []Target{ClaudeTarget, AgentsTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now()}
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude/skills", ".agents/skills"} {
		// Tệp phụ tạo mới được gộp vào dòng SKILL.md (báo cáo init/sync gọn).
		if o := coreOutcome(t, outs, dir+"/demo/SKILL.md"); o.Result != fsafe.Created || o.Detail != "+3 tệp phụ" {
			t.Fatalf("%s: %+v", dir, o)
		}
		for _, f := range files {
			rel := dir + "/demo/" + f.Rel
			for _, o := range outs {
				if o.Path == rel {
					t.Fatalf("tệp phụ tạo mới không được có dòng riêng: %+v", o)
				}
			}
			b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil || string(b) != string(f.Content) {
				t.Fatalf("%s: %q %v", rel, b, err)
			}
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(root, ".claude", "skills", "demo", "scripts", "loop.sh")); fi.Mode().Perm()&0o111 == 0 {
			t.Fatalf("script phải có bit thực thi: %v", fi.Mode())
		}
	}
	lock, _ := ReadLock(root)
	e := lock.Skills["demo"]
	if e.SHA256 != cur[0].Version() || e.SHA256 == (Skill{Content: skill}).Hash() || len(e.Files) != 6 {
		t.Fatalf("lock phải ghi phiên bản gồm tệp phụ và băm từng tệp: %+v", e)
	}
	if st, _ := Status(root, ClaudeTarget, cur[0]); st != StateCurrent {
		t.Fatalf("trạng thái: %s", st)
	}

	// Sửa tay một tệp phụ → xung đột, giữ nội dung người dùng; Status báo khác.
	edited := filepath.Join(root, ".claude", "skills", "demo", "tests.md")
	os.WriteFile(edited, []byte("sửa tay\n"), 0o644)
	if st, _ := Status(root, ClaudeTarget, cur[0]); st != StateDiffers {
		t.Fatalf("trạng thái sau sửa tay: %s", st)
	}
	files[2].Content = []byte("# tests v2\n")
	cur = []Skill{{Name: "demo", Content: skill, Files: files}}
	outs, err = Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if o := coreOutcome(t, outs, ".claude/skills/demo/tests.md"); o.Result != fsafe.Conflict {
		t.Fatalf("phải xung đột: %+v", o)
	}
	if o := coreOutcome(t, outs, ".agents/skills/demo/tests.md"); o.Result != fsafe.Updated {
		t.Fatalf("bản chưa sửa tay phải được nâng cấp: %+v", o)
	}
	if b, _ := os.ReadFile(edited); string(b) != "sửa tay\n" {
		t.Fatal("không được ghi đè tệp sửa tay")
	}

	// Bản mới bỏ scripts/loop.sh: gỡ ở cả hai target, dọn thư mục rỗng; tệp sửa tay vẫn còn xung đột.
	cur = []Skill{{Name: "demo", Content: skill, Files: []CustomFile{files[0], files[2]}}}
	outs, err = Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".claude", ".agents"} {
		rel := dir + "/skills/demo/scripts/loop.sh"
		if o := coreOutcome(t, outs, rel); o.Result != fsafe.Removed {
			t.Fatalf("%s phải bị gỡ: %+v", rel, o)
		}
		if _, err := os.Stat(filepath.Join(root, dir, "skills", "demo", "scripts")); !os.IsNotExist(err) {
			t.Fatalf("thư mục scripts rỗng phải được dọn: %v", err)
		}
	}
	lock, _ = ReadLock(root)
	if _, ok := lock.Skills["demo"].Files[".claude/skills/demo/scripts/loop.sh"]; ok {
		t.Fatalf("lock còn tệp đã gỡ: %+v", lock.Skills["demo"].Files)
	}
	if lock.Skills["demo"].Files[".claude/skills/demo/tests.md"] == "" {
		t.Fatal("tệp xung đột phải giữ băm cũ trong lock")
	}

	// --force ghi đè tệp sửa tay (có sao lưu).
	opts.Force = true
	outs, _ = Install(opts)
	if o := coreOutcome(t, outs, ".claude/skills/demo/tests.md"); o.Result != fsafe.Updated || o.Backup == "" {
		t.Fatalf("--force: %+v", o)
	}
}

// Skill gốc nhúng: tệp phụ nạp từ core/<tên>/ (trừ SKILL.md và tests/), script .sh có bit thực thi; skill không có
// tệp phụ giữ Version() == Hash() để core.lock cũ không đổi.
func TestCoreFilesFromEmbed(t *testing.T) {
	for _, s := range Core() {
		if len(s.Files) == 0 && s.Version() != s.Hash() {
			t.Fatalf("%s: Version phải bằng Hash khi không có tệp phụ", s.Name)
		}
		for _, f := range s.Files {
			if f.Rel == "SKILL.md" || len(f.Rel) > 6 && f.Rel[:6] == "tests/" {
				t.Fatalf("%s: không được xuất bản %s", s.Name, f.Rel)
			}
			if filepath.Ext(f.Rel) == ".sh" && f.Mode&0o111 == 0 {
				t.Fatalf("%s/%s phải thực thi", s.Name, f.Rel)
			}
		}
	}
}
