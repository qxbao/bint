// Package testutil cung cấp tiện ích tạo repository Git tạm cho kiểm thử.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Git chạy lệnh git trong dir và dừng kiểm thử nếu lỗi.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// NewRepo tạo repository có một commit; trả về thư mục gốc.
func NewRepo(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	if files == nil {
		files = map[string]string{"README.md": "# demo\n"}
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// Isolate đặt các biến môi trường để Bint không đụng tới trạng thái thật của máy.
func Isolate(t *testing.T) (stateDir string) {
	t.Helper()
	stateDir = t.TempDir()
	t.Setenv("BINT_STATE_DIR", stateDir)
	t.Setenv("BINT_CONFIG_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Cấu hình git hệ thống (Git for Windows đặt core.autocrlf=true) không được đổi byte checkout/apply của
	// lệnh git do Bint gọi: giống testutil.Git. Kiểm hành vi autocrlf thì đặt core.autocrlf ở cấu hình repo.
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Chương trình harness giả đứng đầu PATH: kết quả không phụ thuộc máy có cài
	// claude/codex/agy hay không, và kiểm thử không bao giờ gọi harness thật.
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex", "agy"} {
		file, body := name, "#!/bin/sh\necho 'bint test: harness giả' >&2\nexit 1\n"
		if runtime.GOOS == "windows" {
			file, body = name+".bat", "@echo bint test: harness gia 1>&2\r\n@exit /b 1\r\n"
		}
		if err := os.WriteFile(filepath.Join(bin, file), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Kiểm thử có thể đang chạy trong một phiên agent/lượt workflow thật.
	for _, k := range []string{"CLAUDE_CODE_SESSION_ID", "BINT_RUN_ID", "BINT_TASK_ID", "BINT_APPROVED_PATHS"} {
		t.Setenv(k, "")
	}
	return stateDir
}
