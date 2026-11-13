package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// GĐ5: bint sync xuất bản skill điều phối của flow tuỳ biến hợp lệ và gỡ nó khi flow bị xoá; flow lỗi không xuất bản.
func TestSyncPublishesAndRemovesFlowSkillEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	// tdd dùng skill tuỳ biến write-failing-test nên cần skill đó trước.
	if out, code := run(t, root, "", "skill", "new", "write-failing-test"); code != 0 {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	writeFlowE2E(t, root, "tdd.yml", e2eTDD)
	claude := filepath.Join(root, ".claude", "skills", "tdd", "SKILL.md")
	agents := filepath.Join(root, ".agents", "skills", "tdd", "SKILL.md")

	if out, code := run(t, root, "", "sync"); code != 0 || !strings.Contains(out, ".claude/skills/tdd/SKILL.md") {
		t.Fatalf("sync: %d\n%s", code, out)
	}
	b, err := os.ReadFile(claude)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"disable-model-invocation: true", "bint task new", "--workflow tdd", "bint run tdd --task", "| red | implement | write-failing-test | test-writer |"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("skill điều phối thiếu %q", want)
		}
	}
	if a, err := os.ReadFile(agents); err != nil || strings.Contains(string(a), "disable-model-invocation") {
		t.Fatalf("bản .agents: %v\n%s", err, a)
	}
	if lock, _ := os.ReadFile(filepath.Join(root, ".bint", "skills", "core.lock")); !strings.Contains(string(lock), "flows:") || !strings.Contains(string(lock), ".claude/skills/tdd/SKILL.md") {
		t.Fatalf("core.lock thiếu mục flows:\n%s", lock)
	}

	// Flow lỗi lint (bỏ skill tuỳ biến của bước red): sync báo xung đột (thoát 1), giữ bản đã xuất bản.
	if err := os.RemoveAll(filepath.Join(root, ".bint", "skills", "write-failing-test")); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, root, "", "sync")
	if code == 0 || !strings.Contains(out, "write-failing-test") {
		t.Fatalf("sync với flow lỗi phải báo xung đột: %d\n%s", code, out)
	}
	if after, _ := os.ReadFile(claude); string(after) != string(b) {
		t.Fatal("bản đã xuất bản không được đổi khi flow lỗi")
	}

	// Xoá flow: skill điều phối được gỡ, mục lock biến mất.
	if err := os.Remove(filepath.Join(root, ".bint", "flows", "tdd.yml")); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, root, "", "sync"); code != 0 {
		t.Fatalf("sync sau khi xoá flow: %d\n%s", code, out)
	}
	for _, p := range []string{claude, agents} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s phải được gỡ", p)
		}
	}
	if lock, _ := os.ReadFile(filepath.Join(root, ".bint", "skills", "core.lock")); strings.Contains(string(lock), "flows:") {
		t.Fatalf("mục flows phải biến mất:\n%s", lock)
	}
}
