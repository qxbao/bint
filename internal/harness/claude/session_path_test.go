package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSlug(t *testing.T) {
	if got := ProjectSlug(`D:\Projects\bint`); got != "D--Projects-bint" {
		t.Fatal(got)
	}
	if got := ProjectSlug("/home/a b/proj.x"); got != "-home-a-b-proj-x" {
		t.Fatal(got)
	}
}

func TestSessionTranscriptPath(t *testing.T) {
	cfg, cwd := t.TempDir(), filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(cwd, 0o755)
	dir := filepath.Join(cfg, "projects", ProjectSlug(cwd))
	os.MkdirAll(dir, 0o755)
	want := filepath.Join(dir, "sess-1.jsonl")
	if SessionTranscriptPath(cfg, cwd, "sess-1") != "" {
		t.Fatal("chưa có tệp phải rỗng")
	}
	os.WriteFile(want, []byte("{}\n"), 0o600)
	if got := SessionTranscriptPath(cfg, cwd, "sess-1"); got != want {
		t.Fatalf("got %q", got)
	}
	// Phiên chỉ nhìn thấy ở thư mục dự án của đúng cwd.
	if SessionTranscriptPath(cfg, filepath.Join(cwd, "sub"), "sess-1") != "" || SessionTranscriptPath(cfg, cwd, "") != "" {
		t.Fatal("khác cwd / id rỗng phải rỗng")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if SessionTranscriptPath("", cwd, "sess-1") != want {
		t.Fatal("configDir rỗng dùng CLAUDE_CONFIG_DIR")
	}
}
