package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bint/internal/skills"
)

const (
	autoT1 = "2026-11-01T00:00:00Z"
	autoT2 = "2026-11-02T00:00:00Z"
)

var (
	autoOldBin = skills.BuildStamp{Version: "0.1.0-dev", Revision: "aaaaaaaaaaaa", Time: autoT1}
	autoNewBin = skills.BuildStamp{Version: "0.1.0-dev", Revision: "bbbbbbbbbbbb", Time: autoT2}
)

// staleRepo dựng repository mà tệp skill và core.lock do một bản Bint cũ (autoOldBin) ghi: nội dung tệp cũ, băm gốc trong
// lock là băm của nội dung cũ đó (nên tệp "chưa bị sửa tay").
func staleRepo(t *testing.T) (root, file string) {
	t.Helper()
	root = sysRepo(t, InitOptions{})
	const name = "b-bootstrap"
	file = filepath.Join(root, ".claude", "skills", name, "SKILL.md")
	old := []byte("nội dung cũ của bản Bint cũ\n")
	if err := os.WriteFile(file, old, 0o644); err != nil {
		t.Fatal(err)
	}
	// Manifest cũng phải coi tệp cũ là do Bint ghi: dùng băm trong lock (cách máy khác trong nhóm nhận ra).
	lock, err := skills.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Skills[name]
	e.SHA256, e.Composed, e.Overlay = skills.Skill{Content: old}.Hash(), "", ""
	lock.Skills[name] = e
	lock.BintVersion = "0.1.0-dev"
	lock.BintRevision, lock.BintRevisionTime, lock.BintModified = autoOldBin.Revision, autoOldBin.Time, false
	writeTestLock(t, root, lock)
	return root, file
}

func writeTestLock(t *testing.T, root string, lock skills.Lock) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(skills.LockRel))
	data, err := yaml.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoResyncRefreshesStaleSkills(t *testing.T) {
	root, file := staleRepo(t)
	t.Cleanup(skills.SetBuildOverride(&autoNewBin))
	msg, err := AutoResync(context.Background(), root, "0.1.0-dev")
	if err != nil || !strings.HasPrefix(msg, "bint: đã tự xuất lại skill") || strings.Contains(msg, "\n") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	want, _ := skills.Get("b-bootstrap")
	if b, _ := os.ReadFile(file); string(b) != string(want.Content) {
		t.Fatal("tệp skill chưa được xuất lại")
	}
	lock, _ := skills.ReadLock(root)
	if lock.BintRevision != autoNewBin.Revision || lock.BintRevisionTime != autoNewBin.Time {
		t.Fatalf("core.lock phải ghi dấu binary mới: %+v", lock)
	}
	// Lần hai: băm đã khớp nên không làm gì.
	if msg, err := AutoResync(context.Background(), root, "0.1.0-dev"); msg != "" || err != nil {
		t.Fatalf("lần hai: %q %v", msg, err)
	}
}

func TestAutoResyncSkips(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bin   skills.BuildStamp
		setup func(t *testing.T, root, file string)
	}{
		{"binary cũ hơn", autoOldBin, func(t *testing.T, root, _ string) {
			l, _ := skills.ReadLock(root)
			l.BintRevision, l.BintRevisionTime = "cccccccccccc", "2026-12-01T00:00:00Z"
			writeTestLock(t, root, l)
		}},
		{"không xác định", skills.BuildStamp{Version: "0.1.0-dev"}, nil},
		{"binary có sửa", skills.BuildStamp{Version: "0.1.0-dev", Revision: "bbbbbbbbbbbb", Time: autoT2, Modified: true}, nil},
		{"tệp bị sửa tay", autoNewBin, func(t *testing.T, _, file string) {
			if err := os.WriteFile(file, []byte("người dùng sửa\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"phiên agent", autoNewBin, func(t *testing.T, _, _ string) { t.Setenv("CLAUDECODE", "1") }},
		{"BINT_RUN_ID", autoNewBin, func(t *testing.T, _, _ string) { t.Setenv("BINT_RUN_ID", "01ABC") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file := staleRepo(t)
			if tc.setup != nil {
				tc.setup(t, root, file)
			}
			beforeFile, _ := os.ReadFile(file)
			beforeLock, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(skills.LockRel)))
			t.Cleanup(skills.SetBuildOverride(&tc.bin))
			msg, err := AutoResync(context.Background(), root, "0.1.0-dev")
			if msg != "" || err != nil {
				t.Fatalf("phải bỏ qua: %q %v", msg, err)
			}
			afterFile, _ := os.ReadFile(file)
			afterLock, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(skills.LockRel)))
			if string(afterFile) != string(beforeFile) || string(afterLock) != string(beforeLock) {
				t.Fatal("bỏ qua mà vẫn ghi")
			}
		})
	}
}

// Doctor: mục "phiên bản skill" báo đúng hướng; chỉ khi binary mới hơn mới có quick fix sync.
func TestDoctorVersionCheckStates(t *testing.T) {
	root, _ := staleRepo(t)
	find := func() Check {
		t.Helper()
		for _, c := range Doctor(context.Background(), root).Checks {
			if c.Name == "phiên bản skill" {
				return c
			}
		}
		t.Fatal("thiếu mục phiên bản skill")
		return Check{}
	}
	for _, tc := range []struct {
		name  string
		bin   skills.BuildStamp
		level Level
		want  string
		fix   bool
	}{
		{"mới hơn", autoNewBin, LevelInfo, "binary mới hơn core.lock", true},
		{"không xác định", skills.BuildStamp{Version: "0.1.0-dev"}, LevelInfo, "chưa xác định", false},
		{"cũ hơn", skills.BuildStamp{Version: "0.0.9"}, LevelWarn, "bint update --pull", false},
	} {
		restore := skills.SetBuildOverride(&tc.bin)
		c := find()
		restore()
		if c.Level != tc.level || !strings.Contains(c.Detail, tc.want) || (c.Fix != nil) != tc.fix {
			t.Errorf("%s: %+v", tc.name, c)
		}
		if tc.name == "cũ hơn" && c.Fix != nil {
			t.Errorf("binary cũ không được gắn FixSync")
		}
	}
	// Khớp: sau sync băm khớp.
	t.Cleanup(skills.SetBuildOverride(&autoNewBin))
	if _, err := Sync(context.Background(), root, "0.1.0-dev", SyncOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if c := find(); c.Level != LevelOK || !strings.Contains(c.Detail, "khớp") {
		t.Errorf("khớp: %+v", c)
	}
}

// Sync từ chối hạ cấp khi binary cũ hơn core.lock và băm gốc khác; --allow-downgrade ghi được.
func TestSyncRefusesDowngrade(t *testing.T) {
	root, file := staleRepo(t)
	l, _ := skills.ReadLock(root)
	l.BintRevision, l.BintRevisionTime = "cccccccccccc", "2026-12-01T00:00:00Z"
	writeTestLock(t, root, l)
	t.Cleanup(skills.SetBuildOverride(&autoNewBin))
	lockPath := filepath.Join(root, filepath.FromSlash(skills.LockRel))
	beforeLock, _ := os.ReadFile(lockPath)
	beforeFile, _ := os.ReadFile(file)
	_, err := Sync(context.Background(), root, "0.1.0-dev", SyncOptions{})
	if err == nil || !strings.Contains(err.Error(), "cũ hơn") {
		t.Fatalf("phải từ chối: %v", err)
	}
	afterLock, _ := os.ReadFile(lockPath)
	afterFile, _ := os.ReadFile(file)
	if string(afterLock) != string(beforeLock) || string(afterFile) != string(beforeFile) {
		t.Fatal("từ chối mà vẫn ghi")
	}
	if _, err := Sync(context.Background(), root, "0.1.0-dev", SyncOptions{AllowDowngrade: true}); err != nil {
		t.Fatal(err)
	}
}
