package workflow

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"bint/internal/affected"
	"bint/internal/task"
)

// goTestCommand tách lệnh `go test` thuần thành cờ và mẫu gói. ok=false (bảo thủ) khi lệnh không phải go test thuần: có
// ký tự shell đặc biệt, cờ có giá trị rời (-run X), hay mẫu gói không bắt đầu bằng "./" hoặc "." (kể cả {test_dirs}).
func goTestCommand(cmd string) (flags, pats []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "&|;<>$`'\"()\\{}*?[]\n") {
		return nil, nil, false
	}
	f := strings.Fields(cmd)
	if len(f) < 3 || f[0] != "go" || f[1] != "test" {
		return nil, nil, false
	}
	for _, a := range f[2:] {
		switch {
		case strings.HasPrefix(a, "-"):
			switch a {
			case "-race", "-v", "-short", "-failfast", "-cover":
			default:
				if !strings.Contains(a, "=") || strings.HasPrefix(a, "-run=") || strings.HasPrefix(a, "-skip=") ||
					strings.HasPrefix(a, "-bench=") || strings.HasPrefix(a, "-fuzz=") || strings.HasPrefix(a, "-tags=") ||
					strings.HasPrefix(a, "-exec=") {
					return nil, nil, false
				}
			}
			flags = append(flags, a)
		case a == "." || strings.HasPrefix(a, "./"):
			pats = append(pats, a)
		default:
			return nil, nil, false
		}
	}
	if len(pats) == 0 {
		return nil, nil, false
	}
	slices.Sort(flags)
	return flags, pats, true
}

// normDir đưa "./x/y/" hay "x/y" về dạng "./x/y" ("." giữ nguyên).
func normDir(d string) string {
	d = strings.TrimSuffix(filepath.ToSlash(d), "/")
	d = strings.TrimPrefix(d, "./")
	if d == "" || d == "." {
		return "."
	}
	return "./" + d
}

// affectedCoversCommand quyết định lần kiểm chứng affected v (đã qua, đúng cây) có bao trùm verify.command cmd hay
// không: cả hai là `go test` thuần cùng tập cờ, và mọi gói có test mà mẫu của cmd chọn đều nằm trong TestDirs mà lần
// affected đã chạy. expand mở một mẫu gói (có thể kết thúc bằng /...) thành thư mục gói có test; lỗi hay ngoài dự kiến
// thì không bao trùm (bảo thủ: chạy lại).
func affectedCoversCommand(v *task.Verification, cmd string, expand func(pattern string) ([]string, error)) bool {
	if v == nil || !v.Passed || v.Selection == nil || v.Ran == "" || v.Command != cmd {
		return false
	}
	if v.Scope != string(affected.TierGraph) && v.Scope != string(affected.TierPackage) {
		return false
	}
	cf, cp, ok := goTestCommand(cmd)
	if !ok {
		return false
	}
	rf, _, ok := goTestCommand(v.Ran)
	if !ok || !slices.Equal(cf, rf) {
		return false
	}
	ran := map[string]bool{}
	for _, d := range v.Selection.TestDirs {
		ran[normDir(d)] = true
	}
	if len(ran) == 0 {
		return false
	}
	for _, p := range cp {
		dirs, err := expand(p)
		if err != nil {
			return false
		}
		for _, d := range dirs {
			if !ran[normDir(d)] {
				return false
			}
		}
	}
	return true
}

// goListTestDirs mở mẫu gói bằng `go list` ở root thành thư mục (tương đối root) của gói có tệp kiểm thử.
func goListTestDirs(ctx context.Context, root string) func(string) ([]string, error) {
	return func(pattern string) ([]string, error) {
		c := exec.CommandContext(ctx, "go", "list", "-e", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.Dir}}{{end}}", pattern)
		c.Dir = root
		var out bytes.Buffer
		c.Stdout = &out
		if err := c.Run(); err != nil {
			return nil, err
		}
		var dirs []string
		for _, l := range strings.Split(out.String(), "\n") {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			rel, err := filepath.Rel(root, l)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, filepath.ToSlash(rel))
		}
		return dirs, nil
	}
}

// reusableAffectedPass: lần kiểm chứng cuối là affected đã qua trên đúng cây hiện tại và bao trùm verify.command (xem
// affectedCoversCommand). Khi đó finalize ghi nhận nó thay vì chạy lại verify.command đầy đủ.
func (e *Env) reusableAffectedPass(ctx context.Context, t *task.Task) bool {
	cmd := strings.TrimSpace(t.Ticket.Verify.Command)
	v := t.State.LastVerification()
	if cmd == "" || !e.VerifyConfirmed(t) || v == nil || !v.Confirmed || v.Worktree == "" {
		return false
	}
	root, err := e.RootFor(t)
	if err != nil || task.WorktreeFingerprint(ctx, root) != v.Worktree {
		return false
	}
	return affectedCoversCommand(v, cmd, goListTestDirs(ctx, root))
}

// adoptAffectedAsFull ghi một Verification đầy đủ dẫn xuất từ lần affected cuối (cùng log, cùng cây) để cổng done nhận;
// Reason nói rõ đây là dùng lại, không chạy lại.
func (e *Env) adoptAffectedAsFull(t *task.Task) (*task.Task, error) {
	return e.Tasks.Update(t.State.ID, e.now(), func(t *task.Task) error {
		last := t.State.LastVerification()
		if last == nil {
			return nil
		}
		nv := *last
		nv.At, nv.By, nv.RunID = e.at(), e.actor(), e.RunID
		nv.Scope, nv.Ran, nv.Selection = "", "", nil
		nv.Reason = "dùng lại lần kiểm chứng affected (" + last.At + "): đã qua trên đúng cây và bao trùm verify.command"
		t.State.Verifications = append(t.State.Verifications, nv)
		return nil
	})
}
