package iso

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTempDirLayout(t *testing.T) {
	r := newFakeReal(t)
	t.Setenv("TMPDIR", r.Tmp)
	t.Setenv("TMP", r.Tmp)
	t.Setenv("TEMP", r.Tmp)
	t.Setenv(EnvProfile, "")
	p := mustCreate(t, testStore(t, nil), Codex, "w")
	t.Setenv("TMPDIR", r.Tmp)
	t.Setenv("TMP", r.Tmp)
	t.Setenv("TEMP", r.Tmp)
	want := filepath.Join(r.Tmp, tempParentName(), StoreKey(p.StoreRoot), "codex--w")
	if got := p.TempDir(); got != want {
		t.Fatalf("TempDir = %q, muốn %q", got, want)
	}
	// Đang trong profile: TMPDIR hiện tại là temp của profile, temp mới phải theo bản thật.
	t.Setenv(EnvProfile, "claude/other")
	t.Setenv("BINT_ISO_REAL_TMPDIR", r.Tmp)
	t.Setenv("BINT_ISO_REAL_TMP", r.Tmp)
	t.Setenv("BINT_ISO_REAL_TEMP", r.Tmp)
	t.Setenv("TMPDIR", filepath.Join(r.Dir, "profile-tmp"))
	t.Setenv("TMP", filepath.Join(r.Dir, "profile-tmp"))
	t.Setenv("TEMP", filepath.Join(r.Dir, "profile-tmp"))
	if got := p.TempDir(); got != want || strings.Contains(got, "profile-tmp") {
		t.Fatalf("TempDir trong profile = %q, muốn %q", got, want)
	}
}

func TestTempBaseDefaults(t *testing.T) {
	if got := tempBase(newEnvList("linux", nil)); got != "/tmp" {
		t.Errorf("linux mặc định: %q", got)
	}
	if got := tempBase(newEnvList("linux", []string{"TMPDIR=/x"})); got != "/x" {
		t.Errorf("linux TMPDIR: %q", got)
	}
	if got := tempBase(newEnvList("windows", []string{"Temp=C:\\t", "USERPROFILE=C:\\u"})); got != "C:\\t" {
		t.Errorf("windows TEMP: %q", got)
	}
	if got := tempBase(newEnvList("windows", []string{"USERPROFILE=C:\\u"})); got != "C:\\u" {
		t.Errorf("windows USERPROFILE: %q", got)
	}
}

// Hai store khác gốc nhưng cùng codex/default phải nhận TMPDIR khác nhau (khoá S nằm trong từng store).
func TestTempDirDiffersPerStore(t *testing.T) {
	r := newFakeReal(t)
	t.Setenv("TMPDIR", r.Tmp)
	t.Setenv(EnvProfile, "")
	a := profileAt(filepath.Join(r.Dir, "storeA"), Codex, "default", defaultSettings(t, nil), LB{})
	b := profileAt(filepath.Join(r.Dir, "storeB"), Codex, "default", defaultSettings(t, nil), LB{})
	if a.tempDirIn(r.Tmp) == b.tempDirIn(r.Tmp) {
		t.Fatalf("hai store dùng chung TMPDIR %q", a.tempDirIn(r.Tmp))
	}
	if again := profileAt(filepath.Join(r.Dir, "storeA"), Codex, "default", defaultSettings(t, nil), LB{}); again.tempDirIn(r.Tmp) != a.tempDirIn(r.Tmp) {
		t.Fatal("cùng store phải ra cùng TMPDIR")
	}
	ea, err := buildEnv("linux", a, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	eb, err := buildEnv("linux", b, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if envMap(ea)["TMPDIR"] == envMap(eb)["TMPDIR"] {
		t.Fatalf("Env của hai store có cùng TMPDIR %q", envMap(ea)["TMPDIR"])
	}
}
