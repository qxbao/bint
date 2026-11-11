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
