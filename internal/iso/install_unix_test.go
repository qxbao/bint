//go:build !windows

package iso

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeCodexPackages(t *testing.T, codexHome string) string {
	t.Helper()
	pk := filepath.Join(codexHome, "packages")
	rel := filepath.Join(pk, "app-server-daemon", "releases", "1.0")
	writeFile(t, filepath.Join(rel, "codex"), "bin")
	if err := os.Symlink(rel, filepath.Join(pk, "app-server-daemon", "current")); err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestEnsureInstallLinksRealCodexPackages(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	pk := fakeCodexPackages(t, filepath.Join(r.Home, ".codex"))

	res, err := ensureInstall("linux", p, r.Env)
	if err != nil || len(res) != 1 || res[0].Share != ShareCodexPackages || res[0].State != LinkCreated {
		t.Fatalf("%+v %v", res, err)
	}
	if got := linkTo(t, filepath.Join(p.Home, ".codex", "packages")); got != pk {
		t.Fatalf("packages -> %s, muốn %s", got, pk)
	}
	if res, _ = ensureInstall("linux", p, r.Env); res[0].State != LinkOK {
		t.Errorf("lần hai: %v", res[0].State)
	}
	if is := installIssues("linux", p, r.Env); len(is) != 0 {
		t.Errorf("không có cảnh báo khi đã liên kết: %v", is)
	}
}

func TestEnsureInstallHonoursRealCodexHome(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	ch := filepath.Join(r.Dir, "elsewhere")
	pk := fakeCodexPackages(t, ch)
	env := append(append([]string{}, r.Env...), "CODEX_HOME="+ch)
	if _, err := ensureInstall("linux", p, env); err != nil {
		t.Fatal(err)
	}
	if got := linkTo(t, filepath.Join(p.Home, ".codex", "packages")); got != pk {
		t.Fatalf("packages -> %s, muốn %s", got, pk)
	}
}

// Không có app-server-daemon/current thật thì không tạo gì và status cảnh báo.
func TestEnsureInstallWithoutRealCurrentWarns(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	writeFile(t, filepath.Join(r.Home, ".codex", "packages", "app-server-daemon", "releases", "1.0", "codex"), "x") // thiếu current

	res, err := ensureInstall("linux", p, r.Env)
	if err != nil || len(res) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Lstat(filepath.Join(p.Home, ".codex", "packages")); !os.IsNotExist(err) {
		t.Fatalf("không được tạo packages: %v", err)
	}
	is := installIssues("linux", p, r.Env)
	if len(is) != 1 || !strings.Contains(is[0], "430 MB") {
		t.Fatalf("cảnh báo = %v", is)
	}
	// Bản cài riêng có dữ liệu: cảnh báo khác, không bị ghi đè.
	writeFile(t, filepath.Join(p.Home, ".codex", "packages", "own", "f"), "x")
	if is = installIssues("linux", p, r.Env); len(is) != 1 || !strings.Contains(is[0], "bản cài riêng") {
		t.Fatalf("cảnh báo bản riêng = %v", is)
	}
}

func TestEnsureInstallConflictKeepsOwnPackagesAndClaudeIgnored(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	fakeCodexPackages(t, filepath.Join(r.Home, ".codex"))
	writeFile(t, filepath.Join(p.Home, ".codex", "packages", "own", "f"), "x")
	res, err := ensureInstall("linux", p, r.Env)
	if err != nil || len(res) != 1 || !res[0].State.Conflict() {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(p.Home, ".codex", "packages", "own", "f")); err != nil {
		t.Fatal("bản riêng bị ghi đè")
	}
	c := mustCreate(t, s, Claude, "w")
	if res, err := ensureInstall("linux", c, r.Env); err != nil || res != nil || installIssues("linux", c, r.Env) != nil {
		t.Fatalf("claude: %+v %v", res, err)
	}
}
