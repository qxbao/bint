package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func fakeSource(t *testing.T, module string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "cmd", "bint"), 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.25\n"), 0o644)
	return dir
}

func TestFindSource(t *testing.T) {
	t.Setenv(SourceEnv, "")
	good := fakeSource(t, "bint")
	if got, err := FindSource(good); err != nil || got != good {
		t.Fatalf("--source: %q %v", got, err)
	}
	if _, err := FindSource(fakeSource(t, "khac")); !errors.Is(err, ErrNoSource) {
		t.Fatalf("module khác phải báo ErrNoSource: %v", err)
	}
	t.Setenv(SourceEnv, good)
	if got, err := FindSource(""); err != nil || got != good {
		t.Fatalf("%s: %q %v", SourceEnv, got, err)
	}
	// Không có ghi đè: suy ra từ đường dẫn lúc biên dịch (test chạy từ mã nguồn thật).
	t.Setenv(SourceEnv, "")
	got, err := FindSource("")
	if err != nil || !IsBintSource(got) {
		t.Fatalf("đường dẫn lúc build: %q %v", got, err)
	}
}

func TestParseBuildRevision(t *testing.T) {
	out := "/x/bint: go1.25.1\n\tpath\tbint/cmd/bint\n\tbuild\tvcs=git\n\tbuild\tvcs.revision=de4c70f1234567\n\tbuild\tvcs.modified=true\n"
	if got := ParseBuildRevision(out); got != "de4c70f+sửa" {
		t.Fatalf("%q", got)
	}
	if got := ParseBuildRevision("\tbuild\tvcs.revision=abc\n\tbuild\tvcs.modified=false\n"); got != "abc" {
		t.Fatalf("%q", got)
	}
	if got := ParseBuildRevision(""); got != "" {
		t.Fatalf("%q", got)
	}
}

// Chạy go install thật vào GOBIN tạm.
func TestUpdateSelfInstalls(t *testing.T) {
	if testing.Short() {
		t.Skip("build toàn bộ Bint")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("không có go")
	}
	t.Setenv(SourceEnv, "")
	gobin := t.TempDir()
	t.Setenv("GOBIN", gobin)
	res, err := UpdateSelf(context.Background(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	exe := "bint"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if res.Target != filepath.Join(gobin, exe) || !res.Mismatch || res.OldBackup != "" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(res.Target); err != nil {
		t.Fatal(err)
	}
}

// Bản .old của lần update trước còn bị tiến trình khác giữ (bint mcp serve sống lâu): không được thất bại, phải dùng
// tên khác; bản cũ không còn bị giữ thì được dọn.
func TestOldBackupPathSkipsHeldBackup(t *testing.T) {
	dir := t.TempDir()
	cur := filepath.Join(dir, "bint.exe")
	if got := oldBackupPath(cur); got != cur+".old" {
		t.Fatalf("chưa có bản cũ: %q", got)
	}
	for _, p := range []string{cur + ".old", cur + ".old.1", cur + ".old.2"} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := oldBackupPath(cur); got != cur+".old" {
		t.Fatalf("bản cũ không bị giữ phải được dọn: %q", got)
	}
	for _, p := range []string{cur + ".old.1", cur + ".old.2"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s chưa được dọn: %v", p, err)
		}
	}
	if runtime.GOOS != "windows" {
		return // chỉ Windows chặn xoá tệp đang mở
	}
	if err := os.WriteFile(cur+".old", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(cur + ".old") // giữ như tiến trình đang chạy từ bản cũ
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := oldBackupPath(cur)
	if got != cur+".old.1" {
		t.Fatalf("bản .old bị giữ phải chuyển sang .old.1: %q", got)
	}
	if err := os.WriteFile(cur, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cur, got); err != nil {
		t.Fatalf("đổi tên sang %s: %v", got, err)
	}
}
