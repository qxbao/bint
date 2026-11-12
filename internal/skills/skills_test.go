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

// Một skill b-req cho cả hai mức; b-do chuyển giao độ sâu và giữ anchor routing.
func TestCoreSkillsDepthGuidance(t *testing.T) {
	req, _ := Get("b-req")
	for _, want := range []string{"## Độ sâu phân tích", "standard", "deep", "Giả định và mâu thuẫn", "Lựa chọn và lý do",
		"Vùng ảnh hưởng", "Rủi ro và rollback", "Chiến lược kiểm thử", "Checklist nhất quán", "classification.depth"} {
		if !strings.Contains(req.Body(), want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
	do, _ := Get("b-do")
	for _, want := range []string{"<!-- bint:anchor routing -->", "<!-- bint:anchor-end routing -->", "classification.depth", "--depth deep"} {
		if !strings.Contains(do.Body(), want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
	for _, n := range []string{"b-req", "b-do"} {
		if _, ok := Get(n + "-deep"); ok {
			t.Errorf("không được thêm skill %s-deep", n)
		}
	}
}

// b-knowledge giao việc nhập liệu cho agent nhưng không bao giờ thay người duyệt.
func TestCoreSkillBKnowledgeContract(t *testing.T) {
	s, ok := Get("b-knowledge")
	if !ok {
		t.Fatal("thiếu b-knowledge")
	}
	if !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Error("b-knowledge chỉ do người gọi")
	}
	for _, want := range []string{
		"bint knowledge add", "--tags nguon:", "--evidence", "--third-party", "bint knowledge verify",
		"không bao giờ** chạy `bint knowledge approve`",
		"<!-- bint:anchor ingest-focus -->", "<!-- bint:anchor-end ingest-focus -->",
	} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-knowledge thiếu %q", want)
		}
	}
}

// b-review dùng `bint task diff <id>` làm phạm vi mặc định (không lẫn thay đổi của tác vụ khác).
func TestCoreSkillBReviewUsesTaskDiff(t *testing.T) {
	sk, ok := Get("b-review")
	if !ok {
		t.Fatal("thiếu skill b-review")
	}
	body := sk.Body()
	for _, want := range []string{"bint task diff <id>", "--stat", "--name-only"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-review thiếu %q", want)
		}
	}
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "git diff <base>") {
			t.Errorf("b-review không được chỉ dẫn chạy git diff <base> (lẫn thay đổi của tác vụ khác): %q", l)
		}
	}
}

// G2 (docs/isolation-subagent-plan.md 5.5): b-do chọn chế độ cô lập, báo nơi chạy, xử lý xung đột khi đưa về và
// `task land` trước `done` khi chạy từng bước; b-review và b-feat nhắc cwd là worktree của tác vụ.
func TestCoreSkillsMentionWorktreeIsolation(t *testing.T) {
	want := map[string][]string{
		"b-do": {"isolation_reason", "concurrent_task", "--worktree", "--shared", "bint task land <id>", "xung đột khi đưa về",
			"--3way", "chỉ người", "bint task cancel <id>", "landed", "cây cô lập"},
		"b-review": {"worktree riêng", "bint task diff <id>", "không `bint task land`"},
		"b-feat":   {"worktree riêng", "bint/<id>", "bint run b-do|b-feat --task <id>"},
	}
	for name, subs := range want {
		sk, ok := Get(name)
		if !ok {
			t.Fatalf("thiếu skill %s", name)
		}
		for _, s := range subs {
			if !strings.Contains(sk.Body(), s) {
				t.Errorf("%s thiếu %q", name, s)
			}
		}
	}
	// Orchestrator không được tự chạy --3way / --discard (chỉ người).
	b, _ := Get("b-do")
	for _, l := range strings.Split(b.Body(), "\n") {
		if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "bint task land <id> --3way") || strings.HasPrefix(tl, "bint task cancel <id> --discard") {
			t.Fatalf("b-do chỉ dẫn lệnh chỉ-người như việc của orchestrator: %q", l)
		}
	}
}

// G3b (docs/isolation-subagent-plan.md 6.7): b-do có nhánh driver session — vòng next → Agent → complete --agent-id, action
// process chạy nền, blocked kèm lệnh complete, không có công cụ Agent thì dùng driver process; ranh giới tin cậy giữ nguyên.
func TestBDoDriverSessionBranch(t *testing.T) {
	b, ok := Get("b-do")
	if !ok {
		t.Fatal("thiếu b-do")
	}
	body := b.Body()
	for _, want := range []string{"--driver session", "bint run next <id> --json --no-wait", "bint run complete <id> --run <run_id> --agent-id <agentId> --json",
		"--failed", "action: \"process\"", "action: \"blocked\"", "driver process", "bint run abort <id>", "subagent_type", "công cụ Agent",
		"**không tự làm**", "không tự sửa mã, không `bint task approve`"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
	if !strings.Contains(string(b.Content), "dùng subagent") {
		t.Error("description phải nhắc kích hoạt bằng 'dùng subagent'")
	}
	// Mọi bản xuất bản (Claude và .agents) mang cùng nhánh.
	for _, tg := range []Target{ClaudeTarget, AgentsTarget} {
		if !strings.Contains(ForTarget(b, tg).Body(), "Bước 2b") {
			t.Errorf("%s thiếu Bước 2b", tg.Dir)
		}
	}
}
