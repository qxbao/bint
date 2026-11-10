package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/testutil"
)

// TestDeferredEvolution: execution = deferred — bint status (lần gọi CLI phù hợp tiếp theo) đánh dấu
// yêu cầu khi đủ bằng chứng mới và xử lý phần không cần mô hình; phần cần agent chờ ngân sách; không
// đánh dấu hay đề xuất lặp.
func TestDeferredEvolution(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":             "module demo\n",
		"internal/api/h.go":  "package api\n\nfunc Handle() {}\n",
		"internal/core/x.go": "package core\n\n// Retry phải gọi Reset trước.\nfunc Retry() {}\n\nfunc Reset() {}\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %s", out)
	}
	out, code := run(t, root, "", "knowledge", "add", "--type", "warning", "--title", "Handle không được chặn",
		"--paths", "internal/api/**", "--evidence", "internal/api/h.go:3", "--json", "Handle chạy trên luồng chính.")
	var added struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil {
		t.Fatalf("knowledge add: %d\n%s", code, out)
	}
	sp := filepath.Join(stateRoot(t, root), "spool")
	base := time.Now().Add(-6 * time.Hour)
	core := []string{"internal/core/x.go"}
	session(t, sp, "s1", base, "good", core, []string{added.ID})
	session(t, sp, "s2", base.Add(time.Hour), "bad", core, []string{added.ID})
	session(t, sp, "s3", base.Add(2*time.Hour), "bad", core, []string{added.ID})

	// Manual (mặc định): status không tự đánh dấu gì.
	if out, _ := run(t, root, "", "status"); strings.Contains(out, "Tiến hoá hoãn") {
		t.Fatalf("manual không được tự xử lý:\n%s", out)
	}
	if out, _ := run(t, root, "", "evolve", "queue"); !strings.Contains(out, "Không có yêu cầu") || !strings.Contains(out, "manual") {
		t.Fatalf("queue manual:\n%s", out)
	}

	cfgPath := filepath.Join(root, ".bint", "config.yml")
	cfg, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfg), "execution: manual") {
		t.Fatalf("mẫu cấu hình thiếu evolution.execution:\n%s", cfg)
	}
	os.WriteFile(cfgPath, []byte(strings.Replace(string(cfg), "execution: manual", "execution: deferred", 1)), 0o644)
	session(t, sp, "s4", base.Add(3*time.Hour), "bad", core, []string{added.ID}) // sự kiện mới → tick

	out, code = run(t, root, "", "status")
	if code != 0 || !strings.Contains(out, "Tiến hoá hoãn:") || !strings.Contains(out, "đề xuất ep_") ||
		!strings.Contains(out, "cần agent evolver") || !strings.Contains(out, "đề xuất chờ duyệt") {
		t.Fatalf("status deferred: %d\n%s", code, out)
	}
	out, _ = run(t, root, "", "evolve", "queue", "--all", "--json")
	var raw []map[string]any
	if json.Unmarshal([]byte(out), &raw) != nil || len(raw) < 2 {
		t.Fatalf("queue:\n%s", out)
	}
	done, waiting := 0, 0
	for _, r := range raw {
		switch r["status"] {
		case "done":
			done++
			if r["proposal_id"] == "" {
				t.Fatalf("yêu cầu done thiếu đề xuất: %v", r)
			}
		case "pending":
			waiting++
		}
	}
	if done == 0 || waiting == 0 {
		t.Fatalf("done=%d waiting=%d:\n%s", done, waiting, out)
	}
	nProps := strings.Count(mustRun(t, root, "evolve", "list"), "ep_")

	// Lần gọi sau: không đánh dấu lặp, không tạo đề xuất trùng.
	run(t, root, "", "status")
	run(t, root, "", "status")
	if got := strings.Count(mustRun(t, root, "evolve", "list"), "ep_"); got != nProps {
		t.Fatalf("đề xuất lặp: %d → %d", nProps, got)
	}
	out, _ = run(t, root, "", "evolve", "queue", "--all", "--json")
	var raw2 []map[string]any
	json.Unmarshal([]byte(out), &raw2)
	if len(raw2) != len(raw) {
		t.Fatalf("yêu cầu lặp: %d → %d", len(raw), len(raw2))
	}

	// Agent không được xử lý; xử lý tay cần người.
	asAgent(t)
	if out, code := run(t, root, "", "evolve", "process"); code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("agent process: %d\n%s", code, out)
	}
	if out, _ := run(t, root, "", "status"); strings.Contains(out, "Tiến hoá hoãn") {
		t.Fatalf("status trong phiên agent không được xử lý tiến hoá:\n%s", out)
	}
}

func mustRun(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, code := run(t, root, "", args...)
	if code != 0 {
		t.Fatalf("%v: %d\n%s", args, code, out)
	}
	return out
}
