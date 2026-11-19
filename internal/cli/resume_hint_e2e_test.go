package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// NT-WF-BASELINE.4: lời nhắc tiếp tục của CLI theo driver của tác vụ. Driver session in `bint run next <id> --resume`
// và không bao giờ `bint run resume`; driver process in `bint run resume <id>`. Kiểm ở bint run status, dòng Đưa về và
// dòng Tiếp tục từ của bint task show, bint task approve và bint task land.

// resumeWant trả lệnh tiếp tục mong đợi và lệnh của driver kia (không được xuất hiện).
func resumeWant(driver, id string) (want, other string) {
	if driver == "session" {
		return "bint run next " + id + " --resume", "bint run resume"
	}
	return "bint run resume " + id, "bint run next"
}

func checkResumeHint(t *testing.T, where, out, driver, id string) {
	t.Helper()
	want, other := resumeWant(driver, id)
	if !strings.Contains(out, want) || strings.Contains(out, other) {
		t.Fatalf("%s (driver %s): cần %q và không có %q:\n%s", where, driver, want, other, out)
	}
}

func TestResumeHintFollowsDriverCLI(t *testing.T) {
	for _, driver := range []string{"process", "session"} {
		t.Run(driver, func(t *testing.T) {
			root := initIsolationRepo(t)
			// Lệnh kiểm chứng khác eval.test_command nên chưa được xác nhận: bint task approve có việc để làm.
			out, code := run(t, root, "", "task", "new", "--no-plan", "--worktree", "--driver", driver, "--json",
				"--verify-command", "echo pending", "Thêm tính năng v2 cho app.txt")
			var c struct {
				ID string `json:"id"`
			}
			if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
				t.Fatalf("task new: %d\n%s", code, out)
			}
			id := c.ID
			if out, code := run(t, root, "", "task", "status", id, "blocked", "--reason", "thử lời nhắc"); code != 0 {
				t.Fatalf("task status: %d\n%s", code, out)
			}

			out, code = run(t, root, "", "run", "status", id)
			if code != 0 {
				t.Fatalf("run status: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint run status", out, driver, id)

			out, code = run(t, root, "", "task", "show", id)
			if code != 0 {
				t.Fatalf("task show: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint task show (Tiếp tục từ)", out, driver, id)

			out, code = run(t, root, "", "task", "approve", id)
			if code != 0 || !strings.Contains(out, "Xác nhận lệnh kiểm chứng") {
				t.Fatalf("task approve: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint task approve", out, driver, id)

			// Dòng Đưa về khi chờ đưa về (Origin có, AwaitingLand): ghi thẳng vào state.json như TestTaskDiffCLI.
			out, _ = run(t, root, "", "task", "show", id, "--json")
			var shown struct{ Dir string }
			if json.Unmarshal([]byte(out), &shown) != nil || shown.Dir == "" {
				t.Fatalf("task show --json: %s", out)
			}
			sp := filepath.Join(shown.Dir, "state.json")
			b, err := os.ReadFile(sp)
			if err != nil {
				t.Fatal(err)
			}
			var st map[string]any
			if err := json.Unmarshal(b, &st); err != nil {
				t.Fatal(err)
			}
			st["origin"], st["awaiting_land"] = root, true
			if b, err = json.Marshal(st); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sp, b, 0o600); err != nil {
				t.Fatal(err)
			}
			out, code = run(t, root, "", "task", "show", id)
			if code != 0 || !strings.Contains(out, "chờ đưa về: bint task land "+id) {
				t.Fatalf("task show khi chờ đưa về: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint task show (Đưa về)", out, driver, id)
			out, code = run(t, root, "", "run", "status", id)
			if code != 0 || !strings.Contains(out, "chờ đưa về") {
				t.Fatalf("run status khi chờ đưa về: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint run status (Đưa về)", out, driver, id)
		})
	}
}

// bint task land (tác vụ worktree đã qua kiểm chứng, chưa done) in dòng Tiếp theo đúng driver.
func TestLandResumeHintFollowsDriverCLI(t *testing.T) {
	for _, driver := range []string{"process", "session"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "process" && runtime.GOOS == "windows" {
				t.Skip("tác nhân giả viết bằng sh")
			}
			root := initIsolationRepo(t)
			var id string
			if driver == "session" {
				out, code := run(t, root, "", "task", "new", "--no-plan", "--worktree", "--driver", "session", "--json", "Thêm tính năng v2 cho app.txt")
				var c struct {
					ID string `json:"id"`
				}
				if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
					t.Fatalf("task new: %d\n%s", code, out)
				}
				id = c.ID
				a, code, out := sess(t, root, "next", id, "--json")
				if code != 0 || a.Action != "agent" || a.Worktree == "" {
					t.Fatalf("next: %d\n%s", code, out)
				}
				for name, body := range map[string]string{"app.txt": "v2\n", "done.txt": "ok\n"} {
					if err := os.WriteFile(filepath.Join(a.Worktree, name), []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if d, code, out := sess(t, root, "complete", id, "--run", a.RunID, "--agent-id", "agent-feat", "--json"); code != 0 || d.Result == nil || d.Result.Status != "completed" {
					t.Fatalf("complete: %d\n%s", code, out)
				}
			} else {
				out, code := run(t, root, "", "run", "b-feat", "--worktree", "--agent-command", writeScript(t, featScript), "Thêm tính năng v2 cho app.txt")
				if code != 0 || !strings.Contains(out, "in_review") {
					t.Fatalf("run b-feat: %d\n%s", code, out)
				}
				id = taskIDs(t, root)[0]
			}
			out, code := run(t, root, "", "task", "land", id)
			if code != 0 || !strings.Contains(out, "Đã đưa") {
				t.Fatalf("task land: %d\n%s", code, out)
			}
			checkResumeHint(t, "bint task land", out, driver, id)
		})
	}
}

// Điểm verify xanh: bint task show --json luôn có khoá last_green/open_findings (null khi chưa có); restore-green chỉ người chạy được
// và báo rõ khi tác vụ chưa có điểm xanh.
func TestRestoreGreenCLI(t *testing.T) {
	asHuman(t)
	root := initIsolationRepo(t)
	out, code := run(t, root, "", "task", "new", "--no-plan", "--json", "--verify-command", "echo pending", "Thêm tính năng v2 cho app.txt")
	var c struct {
		ID string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &c) != nil || c.ID == "" {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	id := c.ID
	if out, code := run(t, root, "", "task", "status", id, "blocked", "--reason", "thử điểm xanh"); code != 0 {
		t.Fatalf("task status: %d\n%s", code, out)
	}
	out, code = run(t, root, "", "task", "show", id, "--json")
	var shown map[string]json.RawMessage
	if code != 0 || json.Unmarshal([]byte(out), &shown) != nil {
		t.Fatalf("task show --json: %d\n%s", code, out)
	}
	for _, k := range []string{"last_green", "open_findings"} {
		if _, ok := shown[k]; !ok {
			t.Errorf("thiếu khoá %q trong task show --json", k)
		}
	}
	out, code = run(t, root, "", "task", "restore-green", id)
	if code == 0 || !strings.Contains(out, "chưa có điểm verify xanh") {
		t.Fatalf("tác vụ chưa xanh lần nào: %d\n%s", code, out)
	}
	t.Setenv("CLAUDECODE", "1")
	out, code = run(t, root, "", "task", "restore-green", id, "--apply")
	if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("phiên agent phải bị từ chối: %d\n%s", code, out)
	}
}
