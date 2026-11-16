//go:build !windows

package iso

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
)

const (
	idOld  = "aaaaaaaa-0000-4000-8000-000000000001"
	idKeep = "aaaaaaaa-0000-4000-8000-000000000002"
	idNew  = "aaaaaaaa-0000-4000-8000-000000000003"
)

func aged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	ts := time.Now().Add(-age)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func gcStore(t *testing.T, mut func(*config.Iso)) *Store {
	return testStore(t, func(c *config.Iso) {
		c.Cache = "private"
		if mut != nil {
			mut(c)
		}
	})
}

func removedPaths(rep GCReport) []string {
	var out []string
	for _, a := range rep.Actions {
		if a.Kind != GCBusy && a.Kind != GCSkip {
			out = append(out, a.Path)
		}
	}
	slices.Sort(out)
	return out
}

func rollout(id string) string { return "rollout-2026-01-02T03-04-05-" + id + ".jsonl" }

// agedTree đặt mtime cũ cho path và mọi thứ dưới nó (thư mục tạo bởi writeFile có mtime mới).
func agedTree(t *testing.T, path string, age time.Duration) {
	t.Helper()
	var all []string
	_ = filepath.WalkDir(path, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			all = append(all, p)
		}
		return nil
	})
	slices.Reverse(all) // con trước cha: đặt mtime con không đổi mtime cha vì chỉ Chtimes
	for _, p := range all {
		aged(t, p, age)
	}
}

// GC cache riêng theo khối: khối con trực tiếp của .cache chỉ bị xoá khi mọi mục trong nó đều cũ.
func TestGCCacheOldFilesAndEmptiedDirs(t *testing.T) {
	s := gcStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	cache := filepath.Join(p.Home, ".cache")
	oldUV := filepath.Join(cache, "uv")
	writeFile(t, filepath.Join(oldUV, "old", "a"), "x")
	agedTree(t, oldUV, 800*time.Hour) // > cache_ttl 720h
	pip := filepath.Join(cache, "pip")
	writeFile(t, filepath.Join(pip, "old", "b"), "x")
	writeFile(t, filepath.Join(pip, "new"), "y")
	agedTree(t, filepath.Join(pip, "old"), 800*time.Hour)
	tool := filepath.Join(p.Home, ".local", "share", "uv", "tools", "ruff", "receipt") // uv-data: tool đã cài, không phải cache
	writeFile(t, tool, "x")
	agedTree(t, filepath.Join(p.Home, ".local", "share", "uv"), 800*time.Hour)

	dry, err := GC(s, GCOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(oldUV) {
		t.Fatal("dry-run đã xoá")
	}
	want := []string{oldUV}
	if got := removedPaths(dry); !slices.Equal(got, want) {
		t.Fatalf("dry-run = %v, muốn %v", got, want)
	}
	rep, err := GC(s, GCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := removedPaths(rep); !slices.Equal(got, want) {
		t.Fatalf("thật = %v, muốn %v", got, want)
	}
	if exists(oldUV) {
		t.Error("khối toàn cũ phải bị xoá cả khối")
	}
	if !exists(tool) {
		t.Error("uv-data (tool đã cài) không được dọn như cache")
	}
	if !exists(filepath.Join(pip, "old", "b")) || !exists(filepath.Join(pip, "new")) || !exists(cache) {
		t.Error("khối còn mục mới phải được giữ nguyên toàn bộ (kể cả tệp cũ), và gốc .cache còn")
	}
	for i, a := range rep.Actions {
		if !a.Removed || a.Bytes != dry.Actions[i].Bytes || a.Bytes <= 0 {
			t.Errorf("hành động %d: %+v vs dry %+v", i, a, dry.Actions[i])
		}
	}
	if rep.Freed() <= 0 {
		t.Error("Freed phải > 0")
	}
}

// Gói giải nén dở dang: tệp nội dung cũ còn tệp đánh dấu mới thì giữ cả khối; mọi thứ cũ thì xoá cả khối.
func TestGCCacheUnitsKeepPartialPackages(t *testing.T) {
	s := gcStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	h := p.Home
	npx := filepath.Join(h, ".npm", "_npx")
	writeFile(t, filepath.Join(npx, "H", "node_modules", "p", "index.js"), "x")
	writeFile(t, filepath.Join(npx, "H", "node_modules", ".package-lock.json"), "{}")
	cargo := filepath.Join(h, ".cargo", "registry")
	writeFile(t, filepath.Join(cargo, "src", "i", "c-1.0", "lib.rs"), "x")
	writeFile(t, filepath.Join(cargo, "src", "i", "c-1.0", ".cargo-ok"), "{}")
	gomod := filepath.Join(h, "go", "pkg", "mod")
	writeFile(t, filepath.Join(gomod, "example.com", "m@v1.0.0", "m.go"), "x")
	writeFile(t, filepath.Join(gomod, "cache", "download", "example.com", "m", "@v", "v1.0.0.zip"), "x")
	cacheKeep := filepath.Join(h, ".cache", "keep")
	writeFile(t, filepath.Join(cacheKeep, "a"), "x")

	oldFiles := []string{
		filepath.Join(npx, "H", "node_modules", "p", "index.js"),
		filepath.Join(cargo, "src", "i", "c-1.0", "lib.rs"),
		filepath.Join(gomod, "example.com", "m@v1.0.0", "m.go"),
	}
	for _, f := range oldFiles {
		aged(t, f, 800*time.Hour)
	}
	if _, err := GC(s, GCOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, f := range append(oldFiles, filepath.Join(npx, "H", "node_modules", ".package-lock.json"),
		filepath.Join(cargo, "src", "i", "c-1.0", ".cargo-ok"), filepath.Join(gomod, "cache", "download", "example.com", "m", "@v", "v1.0.0.zip")) {
		if !exists(f) {
			t.Errorf("khối còn mục mới phải được giữ nguyên, mất %s", f)
		}
	}

	// Mọi thứ đều cũ: xoá cả khối, giữ gốc.
	agedTree(t, npx, 800*time.Hour)
	agedTree(t, cargo, 800*time.Hour)
	agedTree(t, gomod, 800*time.Hour)
	rep, err := GC(s, GCOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{npx, cargo, gomod} {
		if !exists(filepath.Dir(root)) {
			t.Errorf("gốc %s phải còn", filepath.Dir(root))
		}
	}
	if exists(npx) {
		t.Error("_npx toàn cũ phải bị xoá cả khối")
	}
	for _, root := range []string{cargo, gomod} {
		if !exists(root) {
			t.Errorf("thư mục gốc %s phải được giữ", root)
		}
		if ents, _ := os.ReadDir(root); len(ents) != 0 {
			t.Errorf("nội dung %s phải bị xoá hết: %v", root, ents)
		}
	}
	if !exists(cacheKeep) {
		t.Error("khối mới trong .cache bị xoá")
	}
	if rep.Freed() <= 0 {
		t.Error("Freed phải > 0")
	}
}

// Cây module Go chỉ đọc (thư mục 0555, tệp 0444): xoá được mà không lỗi EACCES, không để cây mồ côi.
func TestGCCacheReadOnlyGoModCache(t *testing.T) {
	s := gcStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	gomod := filepath.Join(p.Home, "go", "pkg", "mod")
	build := func(root string) {
		mod := filepath.Join(root, "example.com", "m@v1.0.0")
		writeFile(t, filepath.Join(mod, "sub", "m.go"), "x")
		writeFile(t, filepath.Join(root, "cache", "download", "example.com", "m", "@v", "v1.0.0.zip"), "x")
		_ = filepath.WalkDir(root, func(path string, de os.DirEntry, err error) error {
			if err == nil && strings.Contains(path, "@v1.0.0") {
				if de.IsDir() {
					return nil
				}
				return os.Chmod(path, 0o444)
			}
			return nil
		})
		// thư mục chỉ đọc: chmod từ con lên cha
		for _, d := range []string{filepath.Join(mod, "sub"), mod} {
			if err := os.Chmod(d, 0o555); err != nil {
				t.Fatal(err)
			}
		}
		agedTree(t, root, 800*time.Hour)
	}
	build(gomod)
	t.Cleanup(func() {
		_ = filepath.WalkDir(gomod, func(p string, d os.DirEntry, err error) error { _ = os.Chmod(p, 0o700); return nil })
	})
	rep, err := GC(s, GCOptions{})
	if err != nil {
		t.Fatalf("GC lỗi trên cây chỉ đọc: %v", err)
	}
	if ents, _ := os.ReadDir(gomod); len(ents) != 0 {
		t.Fatalf("cây module phải bị xoá hết, còn %v", ents)
	}
	if !exists(gomod) {
		t.Error("gốc go/pkg/mod phải còn")
	}
	if rep.Freed() <= 0 {
		t.Error("Freed phải > 0")
	}

	// RemoveProfile cũng xoá được cây chỉ đọc.
	build(gomod)
	if err := RemoveProfile(p); err != nil {
		t.Fatalf("RemoveProfile trên cây chỉ đọc: %v", err)
	}
	if exists(p.Dir) {
		t.Error("profile phải bị xoá")
	}
}

// removeTree chỉ cấp quyền cho thư mục thật nó đi vào; đích của liên kết giữ nguyên quyền và dữ liệu.
func TestRemoveTreeChmodSkipsLinkTargets(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	writeFile(t, filepath.Join(target, "keep"), "x")
	if err := os.Chmod(target, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o700) })
	tree := filepath.Join(base, "tree")
	writeFile(t, filepath.Join(tree, "ro", "f"), "x")
	if err := os.Symlink(target, filepath.Join(tree, "ro", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(tree, "ro"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := removeTree(tree); err != nil {
		t.Fatal(err)
	}
	if exists(tree) {
		t.Error("cây phải bị xoá")
	}
	fi, err := os.Stat(filepath.Join(target, "keep"))
	if err != nil || fi == nil {
		t.Fatalf("đích của liên kết bị đụng: %v", err)
	}
	if st, _ := os.Stat(target); st.Mode().Perm() != 0o555 {
		t.Errorf("đích của liên kết bị đổi quyền: %v", st.Mode().Perm())
	}
}

// Cache chia sẻ (liên kết) không bị duyệt: tệp cũ ở đích thật còn nguyên.
func TestGCDoesNotFollowShareLinks(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	cache := filepath.Join(r.Dir, "xcache", "uv", "old")
	npm := filepath.Join(r.Home, ".npm", "_npx", "old")
	pk := filepath.Join(r.Home, ".codex", "packages", "app-server-daemon", "releases", "0.1", "bin")
	for _, f := range []string{cache, npm, pk} {
		writeFile(t, f, "thật")
		aged(t, f, 5000*time.Hour)
	}
	if _, err := ensureShares("linux", p, r.Env); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.Home, ".codex", "packages"), filepath.Join(p.Home, ".codex", "packages")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(r.Dir, "xcache", "uv"), filepath.Join(p.Home, ".codex", "sessions")); err != nil {
		t.Fatal(err)
	}
	rep, err := GC(s, GCOptions{Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := removedPaths(rep); len(got) != 0 {
		t.Fatalf("đã xoá qua liên kết: %v", got)
	}
	for _, f := range []string{cache, npm, pk} {
		if !exists(f) {
			t.Errorf("%s bị xoá", f)
		}
	}
}

func TestGCSessionsCodexAndClaude(t *testing.T) {
	s := gcStore(t, nil)
	cx := mustCreate(t, s, Codex, "w")
	day := filepath.Join(cx.Home, ".codex", "sessions", "2026", "01", "02")
	oldF, keepF, newF := filepath.Join(day, rollout(idOld)), filepath.Join(day, rollout(idKeep)), filepath.Join(day, rollout(idNew))
	other := filepath.Join(day, "notes.txt")
	arch := filepath.Join(cx.Home, ".codex", "archived_sessions", rollout("bbbbbbbb-0000-4000-8000-00000000000a"))
	for _, f := range []string{oldF, keepF, newF, other, arch} {
		writeFile(t, f, "{}")
	}
	for _, f := range []string{oldF, keepF, other, arch} {
		aged(t, f, 2200*time.Hour) // > sessions_ttl 2160h
	}

	cl := mustCreate(t, s, Claude, "w")
	slug := filepath.Join(cl.Home, ".claude", "projects", "-tmp-x")
	cOld, cKeep, cNew := filepath.Join(slug, idOld+".jsonl"), filepath.Join(slug, idKeep+".jsonl"), filepath.Join(slug, idNew+".jsonl")
	companion := filepath.Join(slug, idOld, "subagents", "a.jsonl")
	for _, f := range []string{cOld, cKeep, cNew, companion} {
		writeFile(t, f, "{}")
	}
	for _, f := range []string{cOld, cKeep} {
		aged(t, f, 2200*time.Hour)
	}

	keep := func(id string) bool { return id == idKeep }
	dry, err := GC(s, GCOptions{DryRun: true, KeepSession: keep})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(oldF) || !exists(cOld) {
		t.Fatal("dry-run đã xoá")
	}
	rep, err := GC(s, GCOptions{KeepSession: keep})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removedPaths(dry), removedPaths(rep)) {
		t.Fatalf("dry-run %v khác thật %v", removedPaths(dry), removedPaths(rep))
	}
	for _, f := range []string{oldF, arch, cOld, filepath.Join(slug, idOld)} {
		if exists(f) {
			t.Errorf("%s phải bị xoá", f)
		}
	}
	for _, f := range []string{keepF, newF, other, cKeep, cNew} {
		if !exists(f) {
			t.Errorf("%s phải còn", f)
		}
	}
	if !exists(day) {
		t.Error("thư mục ngày còn tệp phải còn")
	}
}

func TestGCSessionsTTLZeroDeletesNoSession(t *testing.T) {
	s := gcStore(t, func(c *config.Iso) { c.SessionsTTL = "0s" })
	p := mustCreate(t, s, Codex, "w")
	f := filepath.Join(p.Home, ".codex", "sessions", rollout(idOld))
	writeFile(t, f, "{}")
	aged(t, f, 100000*time.Hour)
	rep, err := GC(s, GCOptions{Deep: true})
	if err != nil || len(removedPaths(rep)) != 0 || !exists(f) {
		t.Fatalf("sessions_ttl 0 vẫn xoá: %v %v", removedPaths(rep), err)
	}
}

func TestGCDeepInstallsAndGoneProjects(t *testing.T) {
	s := gcStore(t, nil)
	cl := mustCreate(t, s, Claude, "w")
	vers := filepath.Join(cl.Home, ".local", "share", "claude", "versions")
	writeFile(t, filepath.Join(vers, "2.0.0"), "old")
	writeFile(t, filepath.Join(vers, "2.1.0"), "cur")
	if err := os.MkdirAll(filepath.Join(cl.Home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vers, "2.1.0"), filepath.Join(cl.Home, ".local", "bin", "claude")); err != nil {
		t.Fatal(err)
	}
	liveCwd := t.TempDir()
	goneCwd := filepath.Join(t.TempDir(), "deleted-repo") // cha còn, thư mục đã xoá
	lostCwd := "/mnt/definitely-missing-volume/repo"      // cha cũng không còn: giữ
	proj := func(slug, id, cwd string) string {
		f := filepath.Join(cl.Home, ".claude", "projects", slug, id+".jsonl")
		line, _ := json.Marshal(map[string]any{"type": "x"})
		l2, _ := json.Marshal(map[string]any{"cwd": cwd})
		writeFile(t, f, string(line)+"\n"+string(l2)+"\n")
		return filepath.Dir(f)
	}
	live := proj("-live", idNew, liveCwd)
	gone := proj("-gone", idOld, goneCwd)
	lost := proj("-lost", "cccccccc-0000-4000-8000-000000000001", lostCwd)
	kept := proj("-keptgone", idKeep, goneCwd)
	nocwd := filepath.Join(cl.Home, ".claude", "projects", "-nocwd")
	writeFile(t, filepath.Join(nocwd, "memory.md"), "m")

	cx := mustCreate(t, s, Codex, "w")
	pk := filepath.Join(cx.Home, ".codex", "packages", "app-server-daemon")
	writeFile(t, filepath.Join(pk, "releases", "1.0", "bin"), "o")
	writeFile(t, filepath.Join(pk, "releases", "1.1", "bin"), "c")
	writeFile(t, filepath.Join(pk, "releases", ".staging.x", "bin"), "s")
	if err := os.Symlink(filepath.Join("releases", "1.1"), filepath.Join(pk, "current")); err != nil {
		t.Fatal(err)
	}
	nocur := filepath.Join(cx.Home, ".codex", "packages", "other", "releases", "9")
	writeFile(t, filepath.Join(nocur, "bin"), "n") // không có current: giữ hết

	user := filepath.Join(cl.Home, "Projects", "x", "old.go")
	writeFile(t, user, "mã người dùng")
	aged(t, user, 100000*time.Hour)

	keep := func(id string) bool { return id == idKeep }
	// Không --deep: bản cài và dự án mồ côi không bị đụng.
	if rep, err := GC(s, GCOptions{KeepSession: keep}); err != nil || len(removedPaths(rep)) != 0 {
		t.Fatalf("không --deep: %v %v", removedPaths(rep), err)
	}
	dry, err := GC(s, GCOptions{Deep: true, DryRun: true, KeepSession: keep})
	if err != nil {
		t.Fatal(err)
	}
	if !exists(gone) {
		t.Fatal("dry-run đã xoá")
	}
	rep, err := GC(s, GCOptions{Deep: true, KeepSession: keep})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removedPaths(dry), removedPaths(rep)) {
		t.Fatalf("dry-run %v khác thật %v", removedPaths(dry), removedPaths(rep))
	}
	for _, f := range []string{filepath.Join(vers, "2.0.0"), gone, filepath.Join(pk, "releases", "1.0"), filepath.Join(pk, "releases", ".staging.x")} {
		if exists(f) {
			t.Errorf("%s phải bị xoá", f)
		}
	}
	for _, f := range []string{filepath.Join(vers, "2.1.0"), live, lost, kept, nocwd, filepath.Join(pk, "releases", "1.1"), nocur, user} {
		if !exists(f) {
			t.Errorf("%s phải còn", f)
		}
	}
}

func TestGCBusyAndDaemonAlive(t *testing.T) {
	s := gcStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	sess := filepath.Join(p.Home, ".codex", "sessions", rollout(idOld))
	cache := filepath.Join(p.Home, ".cache", "old")
	rel := filepath.Join(p.Home, ".codex", "packages", "app-server-daemon", "releases", "1.0", "bin")
	for _, f := range []string{sess, cache, rel} {
		writeFile(t, f, "x")
		aged(t, f, 100000*time.Hour)
	}
	if err := os.Symlink(filepath.Join("releases", "1.0"), filepath.Join(p.Home, ".codex", "packages", "app-server-daemon", "current")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(p.Home, ".codex", "packages", "app-server-daemon", "releases", "2.0", "bin"), "x")

	// Đang có phiên (khoá S): bỏ qua cả profile.
	release, busy, err := p.Lock(false)
	if err != nil || busy {
		t.Fatal(err, busy)
	}
	rep, err := GC(s, GCOptions{Deep: true})
	release()
	if err != nil || len(removedPaths(rep)) != 0 || !exists(sess) || !exists(cache) {
		t.Fatalf("profile đang có phiên: %v %v", removedPaths(rep), err)
	}
	if len(rep.Actions) != 1 || rep.Actions[0].Kind != GCBusy {
		t.Fatalf("thiếu hành động busy: %+v", rep.Actions)
	}

	// Daemon sống: cache vẫn dọn, phiên và bản cài giữ.
	pid, _ := json.Marshal(map[string]any{"pid": os.Getpid()})
	writeFile(t, filepath.Join(p.Home, ".codex", "app-server-daemon", "daemon.pid"), string(pid))
	rep, err = GC(s, GCOptions{Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	if exists(cache) {
		t.Error("cache riêng vẫn phải được dọn khi daemon sống")
	}
	if !exists(sess) || !exists(filepath.Join(p.Home, ".codex", "packages", "app-server-daemon", "releases", "2.0")) {
		t.Error("phiên và bản cài phải được giữ khi daemon sống")
	}
	skipped := false
	for _, a := range rep.Actions {
		skipped = skipped || a.Kind == GCSkip
	}
	if !skipped {
		t.Errorf("thiếu hành động skip: %+v", rep.Actions)
	}
}

func TestGCOnlyProfileAndTemp(t *testing.T) {
	s := gcStore(t, nil)
	a := mustCreate(t, s, Codex, "a")
	b := mustCreate(t, s, Codex, "b")
	fa, fb := filepath.Join(a.Home, ".cache", "o"), filepath.Join(b.Home, ".cache", "o")
	for _, f := range []string{fa, fb} {
		writeFile(t, f, "x")
		aged(t, f, 100000*time.Hour)
	}
	env := newFakeReal(t).Env
	tmpA := a.tempDirFrom(env)
	writeFile(t, filepath.Join(tmpA, "t"), "x")
	rep, err := GC(s, GCOptions{Only: "codex/a", Env: env})
	if err != nil {
		t.Fatal(err)
	}
	if exists(fa) || !exists(fb) {
		t.Errorf("Only=codex/a: a còn %v, b còn %v", exists(fa), exists(fb))
	}
	if exists(tmpA) || len(rep.Temp) != 1 || rep.Temp[0].Kind != TempRemoveAll {
		t.Errorf("temp của a phải bị xoá: %+v", rep.Temp)
	}
}

func TestOpenTaskSessions(t *testing.T) {
	testStore(t, nil) // Isolate: BINT_STATE_DIR
	base := os.Getenv("BINT_STATE_DIR")
	mk := func(repo, id, status, session string) {
		dir := filepath.Join(base, repo, "runtime", "tasks", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ticket := fmt.Sprintf("id: %s\ngoal: g\nautonomy: ask_on_protected\n", id)
		st := map[string]any{"schema": 1, "id": id, "status": status}
		if session != "" {
			st["last_step"] = map[string]any{"step": "b-feat", "run_id": "r", "stop_class": "external", "session_id": session}
		}
		data, _ := json.Marshal(st)
		writeFile(t, filepath.Join(dir, "task.yml"), ticket)
		writeFile(t, filepath.Join(dir, "state.json"), string(data))
	}
	mk("repo1", "01M7H8YGR23QJQFMNNWKZGGR18", "in_progress", idKeep)
	mk("repo2", "01M7H8YGR23QJQFMNNWKZGGR19", "done", idOld)
	mk("repo2", "01M7H8YGR23QJQFMNNWKZGGR1A", "blocked", "")
	keep, err := OpenTaskSessions(os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if !keep(idKeep) || keep(idOld) || keep("khong-co") {
		t.Fatalf("keep(%s)=%v keep(%s)=%v", idKeep, keep(idKeep), idOld, keep(idOld))
	}
	_ = strings.TrimSpace
}
