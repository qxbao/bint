package skills

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/fsafe"
)

func TestCompareBuild(t *testing.T) {
	const (
		t1 = "2026-11-01T00:00:00Z"
		t2 = "2026-11-02T00:00:00Z"
	)
	for _, tc := range []struct {
		name string
		lock Lock
		bin  BuildStamp
		want Direction
	}{
		{"semver binary cũ", Lock{BintVersion: "v1.2.0"}, BuildStamp{Version: "v1.1.9"}, DirBinOlder},
		{"semver binary mới", Lock{BintVersion: "v1.2.0"}, BuildStamp{Version: "v1.10.0"}, DirBinNewer},
		{"semver thắng revision", Lock{BintVersion: "v2.0.0", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "v1.0.0", Revision: "b", Time: t2}, DirBinOlder},
		{"cùng semver, vcs.time mới hơn", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "0.1.0-dev", Revision: "b", Time: t2}, DirBinNewer},
		{"cùng semver, vcs.time cũ hơn", Lock{BintVersion: "0.1.0-dev", BintRevision: "b", BintRevisionTime: t2}, BuildStamp{Version: "0.1.0-dev", Revision: "a", Time: t1}, DirBinOlder},
		{"cùng thời điểm khác revision", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "0.1.0-dev", Revision: "b", Time: t1}, DirUnknown},
		{"cùng revision sạch", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "0.1.0-dev", Revision: "a", Time: t1}, DirSame},
		{"cùng revision, lock có sửa, binary sạch", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1, BintModified: true}, BuildStamp{Version: "0.1.0-dev", Revision: "a", Time: t1}, DirBinOlder},
		{"binary có sửa", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "0.1.0-dev", Revision: "b", Time: t2, Modified: true}, DirUnknown},
		{"binary không có VCS", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: t1}, BuildStamp{Version: "0.1.0-dev"}, DirUnknown},
		{"lock không có revision", Lock{BintVersion: "0.1.0-dev"}, BuildStamp{Version: "0.1.0-dev", Revision: "b", Time: t2}, DirUnknown},
		{"thời điểm hỏng", Lock{BintVersion: "0.1.0-dev", BintRevision: "a", BintRevisionTime: "hỏng"}, BuildStamp{Version: "0.1.0-dev", Revision: "b", Time: t2}, DirUnknown},
		{"lock rỗng", Lock{}, BuildStamp{}, DirUnknown},
	} {
		if got := CompareBuild(tc.lock, tc.bin); got != tc.want {
			t.Errorf("%s: %v, muốn %v", tc.name, got, tc.want)
		}
	}
}

func TestBuildStampString(t *testing.T) {
	if got := (BuildStamp{Version: "0.1.0-dev"}).String(); got != "0.1.0-dev" {
		t.Errorf("không VCS: %q", got)
	}
	got := BuildStamp{Version: "v1", Revision: "0123456789abcdef", Time: "2026-11-01T00:00:00Z", Modified: true}.String()
	if got != "v1 (0123456, 2026-11-01T00:00:00Z, sửa)" {
		t.Errorf("%q", got)
	}
}

func installAt(t *testing.T, root string, bin BuildStamp, allow bool) error {
	t.Helper()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	_, err := Install(InstallOptions{Root: root, Targets: []Target{ClaudeTarget}, BintVersion: bin.Version, Build: bin,
		AllowDowngrade: allow, BackupDir: t.TempDir(), Manifest: m, Now: time.Now()})
	return err
}

func readStamp(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallRefusesDowngrade(t *testing.T) {
	root := t.TempDir()
	newer := BuildStamp{Version: "0.1.0-dev", Revision: "bbbbbbbbbbbb", Time: "2026-11-02T00:00:00Z"}
	older := BuildStamp{Version: "0.1.0-dev", Revision: "aaaaaaaaaaaa", Time: "2026-11-01T00:00:00Z"}
	if err := installAt(t, root, newer, false); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, filepath.FromSlash(LockRel))
	if l, _ := ReadLock(root); l.BintRevision != newer.Revision || l.BintRevisionTime != newer.Time {
		t.Fatalf("core.lock thiếu dấu build: %+v", l)
	}

	// Làm băm skill gốc trong lock khác bản nhúng (mô phỏng lock do bản mới hơn ghi), xoá tệp xuất.
	l, _ := ReadLock(root)
	e := l.Skills["b-bootstrap"]
	e.SHA256 = "0123456789abcdef"
	l.Skills["b-bootstrap"] = e
	if _, err := writeLock(root, l); err != nil {
		t.Fatal(err)
	}
	lock := readStamp(t, lockPath)
	skillFile := filepath.Join(root, ".claude", "skills", "b-bootstrap", "SKILL.md")
	if err := os.Remove(skillFile); err != nil {
		t.Fatal(err)
	}

	err := installAt(t, root, older, false)
	var de *DowngradeError
	if !errors.Is(err, ErrDowngrade) || !errors.As(err, &de) {
		t.Fatalf("phải từ chối hạ cấp: %v", err)
	}
	if _, serr := os.Stat(skillFile); !os.IsNotExist(serr) {
		t.Fatal("từ chối rồi mà vẫn ghi tệp skill")
	}
	if readStamp(t, lockPath) != lock {
		t.Fatal("từ chối rồi mà core.lock đổi")
	}

	// --allow-downgrade vẫn ghi được.
	if err := installAt(t, root, older, true); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(skillFile); serr != nil {
		t.Fatal("AllowDowngrade phải ghi tệp")
	}
	if l, _ := ReadLock(root); l.BintRevision != older.Revision {
		t.Fatalf("dấu build phải theo binary vừa ghi: %+v", l)
	}
}

func TestInstallStampStableWhenHashMatches(t *testing.T) {
	root := t.TempDir()
	a := BuildStamp{Version: "0.1.0-dev", Revision: "aaaaaaaaaaaa", Time: "2026-11-01T00:00:00Z"}
	b := BuildStamp{Version: "0.1.0-dev", Revision: "bbbbbbbbbbbb", Time: "2026-11-02T00:00:00Z", Modified: true}
	if err := installAt(t, root, a, false); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, filepath.FromSlash(LockRel))
	before := readStamp(t, lockPath)
	// Băm khớp: binary khác (kể cả cũ hơn) không bị từ chối và không đổi core.lock.
	for _, bin := range []BuildStamp{b, {Version: "0.1.0-dev", Revision: "0000000", Time: "2020-01-01T00:00:00Z"}} {
		if err := installAt(t, root, bin, false); err != nil {
			t.Fatalf("băm khớp không được từ chối: %v", err)
		}
		if readStamp(t, lockPath) != before {
			t.Fatal("sync lặp lại bằng binary khác làm đổi core.lock")
		}
	}
}
