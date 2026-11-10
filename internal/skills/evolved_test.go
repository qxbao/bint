package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/fsafe"
)

func newEvolved(name, desc, body string, paths ...string) *Evolved {
	ev := &Evolved{Content: BuildContent(name, desc, body)}
	ev.Meta = EvolvedMeta{Schema: EvolvedSchema, ID: name, Name: name, Status: EvActive, Version: 1, Triggers: Triggers{Paths: paths}, CreatedAt: "2026-01-01T00:00:00.000Z"}
	ev.Meta.Versions = []VersionEntry{{N: 1, SHA256: ev.AsSkill().Hash(), At: "2026-01-01T00:00:00.000Z"}}
	return ev
}

const goodDesc = "Dùng khi sửa mã trong internal/core: gọi Reset trước khi thử lại."

func TestEvolvedStoreRoundTripAndIntegrity(t *testing.T) {
	root := t.TempDir()
	ev := newEvolved("core-retry", goodDesc, "1. Gọi Reset.", "internal/core/**")
	if err := ev.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := LoadEvolvedOne(root, "core-retry")
	if err != nil || got.Doc().Description != goodDesc || got.Doc().Body != "1. Gọi Reset." || got.Last() == nil {
		t.Fatalf("%v %+v", err, got)
	}
	if probs := CheckEvolved(root); len(probs) != 0 {
		t.Fatalf("toàn vẹn: %+v", probs)
	}
	// Hai thay đổi cùng sửa một skill rồi merge: nội dung không khớp phiên bản → xung đột thủ công.
	os.WriteFile(filepath.Join(EvolvedDir(root, "core-retry"), "SKILL.md"), BuildContent("core-retry", goodDesc, "1. Gọi Reset.\n2. Thêm bước khác."), 0o644)
	if probs := CheckEvolved(root); len(probs) == 0 || !probs[0].Conflict {
		t.Fatalf("phải báo xung đột: %+v", probs)
	}
	// Dấu xung đột Git.
	os.WriteFile(filepath.Join(EvolvedDir(root, "core-retry"), "skill.yaml"), []byte("<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> b\n"), 0o644)
	found := false
	for _, p := range CheckEvolved(root) {
		found = found || (p.Conflict && strings.Contains(p.Detail, "xung đột merge"))
	}
	if !found {
		t.Fatalf("phải thấy dấu xung đột merge: %+v", CheckEvolved(root))
	}
	if _, err := LoadEvolvedOne(root, "../etc"); err == nil {
		t.Fatal("tên có '..' phải bị từ chối")
	}
}

func TestDiffAndStale(t *testing.T) {
	d := Diff("a", "b", "x\ny\nz\n", "x\ny2\nz\nw\n")
	if !strings.Contains(d, "-y\n") || !strings.Contains(d, "+y2\n") || !strings.Contains(d, "+w\n") || strings.Contains(d, "-x") {
		t.Fatalf("diff:\n%s", d)
	}
	if Diff("a", "b", "x\n", "x\n") != "" {
		t.Fatal("giống nhau phải rỗng")
	}
	ev := newEvolved("core-retry", goodDesc, "b", "internal/core/**")
	if stale, _ := ev.Stale([]string{"internal/core/x.go"}); stale {
		t.Fatal("còn khớp tệp: không lỗi thời")
	}
	if stale, why := ev.Stale([]string{"cmd/main.go"}); !stale || !strings.Contains(why, "internal/core/**") {
		t.Fatalf("không còn khớp: %v %s", stale, why)
	}
	if stale, _ := ev.Stale(nil); stale {
		t.Fatal("không có danh sách tệp → không kết luận")
	}
}

// Xuất bản skill tiến hoá cùng skill gốc; skill bị loại bỏ hoặc lỗi thời được gỡ; bản đã bị sửa
// tay không bao giờ bị xoá; skill gốc không bị động tới.
func TestInstallEvolvedPublishAndUnpublish(t *testing.T) {
	root := t.TempDir()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	files := []string{"internal/core/x.go", "internal/db/db.go"}
	opts := InstallOptions{Root: root, Targets: []Target{ClaudeTarget, AgentsTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now(), Files: files}
	ev := newEvolved("core-retry", goodDesc, "1. Gọi Reset.", "internal/core/**")
	other := newEvolved("db-open", "Dùng khi sửa mã trong internal/db: mở kết nối bằng Open.", "1. Gọi Open.", "internal/db/**")
	ev.Save(root)
	other.Save(root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".claude/skills/core-retry/SKILL.md", ".agents/skills/core-retry/SKILL.md", ".claude/skills/db-open/SKILL.md", ".claude/skills/b-req/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Fatalf("thiếu %s: %v", p, err)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(root, ".agents/skills/core-retry/SKILL.md"))
	if !strings.Contains(string(agents), "name: core-retry") || strings.Contains(string(agents), "triggers") {
		t.Fatalf("bản .agents:\n%s", agents)
	}
	lock, _ := ReadLock(root)
	if len(lock.Evolved["core-retry"].Published) != 2 {
		t.Fatalf("core.lock: %+v", lock.Evolved)
	}

	// Loại bỏ skill → gỡ bản đã xuất bản; skill còn lại giữ nguyên.
	ev.Meta.Status = EvDeprecated
	ev.Save(root)
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, o := range outs {
		if o.Result == fsafe.Removed && o.Skill == "core-retry" {
			removed++
		}
	}
	if removed != 2 {
		t.Fatalf("phải gỡ 2 bản: %+v", outs)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/skills/core-retry")); err == nil {
		t.Fatal("thư mục skill đã gỡ phải biến mất")
	}
	if lock, _ = ReadLock(root); len(lock.Evolved) != 1 || lock.Evolved["db-open"].SHA256 == "" {
		t.Fatalf("core.lock sau gỡ: %+v", lock.Evolved)
	}

	// Skill lỗi thời (không còn tệp khớp) không được xuất bản, bản cũ được gỡ.
	opts.Files = []string{"internal/core/x.go"}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/skills/db-open")); err == nil {
		t.Fatal("skill lỗi thời không được ưu tiên: phải gỡ khỏi harness")
	}

	// Bản đã bị sửa tay thì không bị xoá.
	opts.Files = files
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, ".claude/skills/db-open/SKILL.md")
	os.WriteFile(p, []byte("sửa tay\n"), 0o644)
	other.Meta.Status = EvDeprecated
	other.Save(root)
	outs, _ = Install(opts)
	conflict := false
	for _, o := range outs {
		conflict = conflict || (o.Skill == "db-open" && o.Result == fsafe.Conflict)
	}
	if b, _ := os.ReadFile(p); string(b) != "sửa tay\n" || !conflict {
		t.Fatalf("không được xoá bản sửa tay: %q %+v", b, outs)
	}
}
