package harness

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/fsafe"
	"bint/internal/harness/antigravity"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
)

// InstallResult.Prev/SHA256 là băm nội dung chính hàm cài/gỡ đã đọc và đã ghi (repoWriter dùng chúng làm
// prev_sha256/sha256 trong runtime/writes.jsonl thay vì băm lại đĩa).
type hookOps struct {
	name      string
	install   func(path string) (claude.InstallResult, error)
	uninstall func(path string) (claude.InstallResult, error)
	existing  string // nội dung tệp có sẵn trước khi cài (giữ nguyên khi gỡ)
}

func hookOpsTable(backups string) []hookOps {
	now := time.Now()
	return []hookOps{
		{"claude",
			func(p string) (claude.InstallResult, error) { return claude.InstallHooks(p, "bint", backups, now) },
			func(p string) (claude.InstallResult, error) { return claude.UninstallHooks(p, backups, now) },
			`{"zeta": 1}`},
		{"codex",
			func(p string) (claude.InstallResult, error) { return codex.InstallHooks(p, "bint", backups, now) },
			func(p string) (claude.InstallResult, error) { return codex.UninstallHooks(p, backups, now) },
			`{"zeta": 1}`},
		{"antigravity",
			func(p string) (claude.InstallResult, error) { return antigravity.InstallHooks(p, "bint", backups, now) },
			func(p string) (claude.InstallResult, error) { return antigravity.UninstallHooks(p, backups, now) },
			`{"zeta": {"enabled": true}}`},
	}
}

func readOrEmpty(t *testing.T, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

func TestHookResultsCarryReadAndWrittenHashes(t *testing.T) {
	for _, op := range hookOpsTable(t.TempDir()) {
		t.Run(op.name+"/cài lên tệp chưa có rồi gỡ làm xoá tệp", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "hooks.json")
			res, err := op.install(p)
			if err != nil || !res.Changed {
				t.Fatalf("cài: %+v %v", res, err)
			}
			after, _ := readOrEmpty(t, p)
			if res.Prev != "" || res.SHA256 != fsafe.Hash(after) {
				t.Fatalf("cài lên tệp chưa có: Prev=%q (muốn rỗng), SHA256 khớp ghi=%v", res.Prev, res.SHA256 == fsafe.Hash(after))
			}
			// Gỡ: tệp chỉ còn hook của Bint ⇒ Claude/Codex ghi lại tệp rỗng-hooks, Antigravity xoá tệp.
			res2, err := op.uninstall(p)
			if err != nil || !res2.Changed {
				t.Fatalf("gỡ: %+v %v", res2, err)
			}
			final, exists := readOrEmpty(t, p)
			if res2.Prev != fsafe.Hash(after) {
				t.Fatalf("gỡ: Prev phải là băm nội dung trước khi gỡ")
			}
			want := ""
			if exists {
				want = fsafe.Hash(final)
			}
			if res2.SHA256 != want {
				t.Fatalf("gỡ: SHA256=%q muốn %q (tệp tồn tại=%v)", res2.SHA256, want, exists)
			}
			if op.name == "antigravity" && (exists || res2.SHA256 != "") {
				t.Fatalf("antigravity gỡ làm xoá tệp: tồn tại=%v SHA256=%q", exists, res2.SHA256)
			}
		})
		t.Run(op.name+"/cài lên tệp có sẵn rồi gỡ", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "hooks.json")
			if err := os.WriteFile(p, []byte(op.existing), 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := op.install(p)
			if err != nil || !res.Changed {
				t.Fatalf("cài: %+v %v", res, err)
			}
			after, _ := readOrEmpty(t, p)
			if res.Prev != fsafe.Hash(op.existing) || res.SHA256 != fsafe.Hash(after) || res.Prev == res.SHA256 {
				t.Fatalf("cài lên tệp có sẵn: %+v", res)
			}
			res2, err := op.uninstall(p)
			if err != nil || !res2.Changed {
				t.Fatalf("gỡ: %+v %v", res2, err)
			}
			final, exists := readOrEmpty(t, p)
			if !exists || res2.Prev != fsafe.Hash(after) || res2.SHA256 != fsafe.Hash(final) {
				t.Fatalf("gỡ khỏi tệp còn khoá khác: %+v (tồn tại=%v)", res2, exists)
			}
		})
		t.Run(op.name+"/tệp rỗng khác tệp chưa có", func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "hooks.json")
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := op.install(p)
			if err != nil || !res.Changed || res.Prev != fsafe.Hash("") {
				t.Fatalf("cài lên tệp rỗng: Prev=%q muốn băm của chuỗi rỗng; %+v %v", res.Prev, res, err)
			}
		})
	}
}
