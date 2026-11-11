package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/fsafe"
)

func TestCoreSkills(t *testing.T) {
	s, ok := Get("b-bootstrap")
	if !ok {
		t.Fatal("thiếu b-bootstrap")
	}
	if !strings.HasPrefix(string(s.Content), "---\nname: b-bootstrap\n") || strings.Contains(s.Body(), "name: b-bootstrap") ||
		!strings.Contains(s.Body(), "bint knowledge add") {
		t.Fatalf("skill không đúng định dạng:\n%s", s.Body()[:200])
	}
}

func TestInstallNeverOverwritesEdits(t *testing.T) {
	root := t.TempDir()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	opts := InstallOptions{Root: root, Targets: []Target{ClaudeTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now()}
	outs, err := Install(opts)
	if err != nil || len(outs) == 0 || outs[0].Result != fsafe.Created {
		t.Fatalf("%v %+v", err, outs)
	}
	p := filepath.Join(root, ".claude", "skills", "b-bootstrap", "SKILL.md")
	if outs, _ := Install(opts); outs[0].Result != fsafe.Unchanged {
		t.Fatalf("lần hai: %+v", outs)
	}

	// Sửa tay → xung đột, nội dung người dùng giữ nguyên.
	os.WriteFile(p, []byte("sửa tay\n"), 0o644)
	if outs, _ := Install(opts); outs[0].Result != fsafe.Conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
	if b, _ := os.ReadFile(p); string(b) != "sửa tay\n" {
		t.Fatal("không được ghi đè")
	}
	opts.Force = true
	if outs, _ := Install(opts); outs[0].Result != fsafe.Updated || outs[0].Backup == "" {
		t.Fatalf("--force phải ghi đè có sao lưu: %+v", outs)
	}

	// Máy khác (manifest trống) với tệp khớp core.lock đã commit: coi là nguyên bản.
	opts.Force = false
	lock, _ := ReadLock(root)
	old := []byte("phiên bản skill cũ do Bint ghi\n")
	os.WriteFile(p, old, 0o644)
	lock.Skills["b-bootstrap"] = LockEntry{SHA256: Skill{Content: old}.Hash()}
	writeLock(root, lock)
	opts.Manifest, _ = fsafe.LoadManifest(filepath.Join(t.TempDir(), "m2.json"))
	if outs, _ := Install(opts); outs[0].Result != fsafe.Updated {
		t.Fatalf("bản khớp core.lock phải được nâng cấp: %+v", outs)
	}
}

// Overlay cũ gắn vào anchor; sửa khuôn b-req không được làm mất chúng.
func TestCoreSkillBReqKeepsAnchorsAndContract(t *testing.T) {
	s, ok := Get("b-req")
	if !ok {
		t.Fatal("thiếu b-req")
	}
	for _, want := range []string{
		"<!-- bint:anchor project-rules -->", "<!-- bint:anchor-end project-rules -->",
		"<!-- bint:anchor plan-template -->", "<!-- bint:anchor-end plan-template -->",
		"AC-1:", "TSK-1 [AC-1]", "xong khi:", "--clear-criteria", "bint task lint",
	} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
}
