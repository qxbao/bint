//go:build !windows

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
)

// GĐ3 của bint iso (docs/iso-plan.md mục 5.4, 5.5): chia sẻ cache, bản cài Codex, du, gc, GC tự động, doctor.

func writeIsoCfg(t *testing.T, yaml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("BINT_CONFIG_HOME"), config.FileName), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isExist(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	ts := time.Now().Add(-d)
	if err := os.Chtimes(path, ts, ts); err != nil {
		t.Fatal(err)
	}
}

type duRow struct {
	ID         string `json:"id"`
	Total      int64  `json:"total"`
	AuthConfig int64  `json:"auth_config"`
	Sessions   int64  `json:"sessions"`
	Cache      int64  `json:"cache"`
	Install    int64  `json:"install"`
	Temp       int64  `json:"temp"`
	Other      int64  `json:"other"`
	UserData   int64  `json:"user_data"`
}

func duJSON(t *testing.T, dir string, args ...string) []duRow {
	t.Helper()
	out, code := run(t, dir, "", append([]string{"iso", "du", "--json"}, args...)...)
	if code != 0 {
		t.Fatalf("du: code %d\n%s", code, out)
	}
	var rows []duRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	return rows
}

// mcpScript mô phỏng MCP stdio của Codex: tiến trình con chỉ còn HOME, PATH, TMPDIR (env -i) ghi vào các thư mục
// cache của uv/npx. Chỉ liên kết mới đưa được dữ liệu ra thư mục thật.
const mcpScript = `env -i HOME="$HOME" PATH="$PATH" TMPDIR="$TMPDIR" sh -c '
mkdir -p "$HOME/.cache/uv/archive-v0" "$HOME/.npm/_npx/abc123" "$HOME/.local/share/uv/tools/ruff"
echo wheel > "$HOME/.cache/uv/archive-v0/pkg.whl"
echo npx > "$HOME/.npm/_npx/abc123/package.json"
echo tool > "$HOME/.local/share/uv/tools/ruff/uv-receipt.toml"'`

func TestIsoRunMCPWritesGoToRealCaches(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"codex": mcpScript})
	dir := t.TempDir()
	if out, code := run(t, dir, "", "iso", "create", "codex", "--name", "m", "--no-lb"); code != 0 {
		t.Fatalf("create: %d\n%s", code, out)
	}
	if out, code := run(t, dir, "", "iso", "run", "codex", "--name", "m", "--", "exec"); code != 0 {
		t.Fatalf("run: %d\n%s", code, out)
	}
	real := os.Getenv("HOME")
	for _, f := range []string{".cache/uv/archive-v0/pkg.whl", ".npm/_npx/abc123/package.json", ".local/share/uv/tools/ruff/uv-receipt.toml"} {
		if !isExist(filepath.Join(real, f)) {
			t.Errorf("%s phải nằm dưới thư mục thật", f)
		}
	}
	rows := duJSON(t, dir)
	if len(rows) != 1 || rows[0].ID != "codex/m" || rows[0].Cache != 0 {
		t.Fatalf("cache riêng phải là 0: %+v", rows)
	}
	// Bản cache riêng không được xuất hiện trong thư mục profile.
	home := filepath.Join(e.profileDir("codex", "m"), "home")
	for _, d := range []string{".cache", ".npm", ".local/share/uv"} {
		fi, err := os.Lstat(filepath.Join(home, d))
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s phải là liên kết: %v", d, err)
		}
	}
}

func TestIsoRunPrivateUnlinksAndKeepsCacheInProfile(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": `mkdir -p "$HOME/.cache/uv" && echo own > "$HOME/.cache/uv/own"`})
	dir := t.TempDir()
	real := os.Getenv("HOME")
	put(t, filepath.Join(real, ".cache", "uv", "thật"), "dữ liệu thật")
	if out, code := run(t, dir, "", "iso", "create", "claude", "--name", "p"); code != 0 {
		t.Fatalf("create: %d\n%s", code, out)
	}
	home := filepath.Join(e.profileDir("claude", "p"), "home")
	if fi, _ := os.Lstat(filepath.Join(home, ".cache")); fi == nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal(".cache phải là liên kết trước khi đổi sang private")
	}

	writeIsoCfg(t, "iso:\n  cache: private\n")
	if out, code := run(t, dir, "", "iso", "run", "claude", "--name", "p"); code != 0 {
		t.Fatalf("run: %d\n%s", code, out)
	}
	for _, d := range []string{".npm", ".local/share/uv"} {
		if isExist(filepath.Join(home, d)) {
			t.Errorf("%s phải bị gỡ", d)
		}
	}
	if fi, err := os.Lstat(filepath.Join(home, ".cache")); err != nil || !fi.IsDir() {
		t.Fatalf(".cache phải là thư mục thật: %v", err)
	}
	if !isExist(filepath.Join(home, ".cache", "uv", "own")) {
		t.Error("cache do harness ghi phải nằm trong home/.cache của profile")
	}
	if isExist(filepath.Join(real, ".cache", "uv", "own")) {
		t.Error("cache private không được ghi ra thư mục thật")
	}
	if b, err := os.ReadFile(filepath.Join(real, ".cache", "uv", "thật")); err != nil || string(b) != "dữ liệu thật" {
		t.Errorf("dữ liệu thật ở đích bị mất: %v", err)
	}
	if rows := duJSON(t, dir); rows[0].Cache == 0 {
		t.Errorf("private: cache riêng phải > 0: %+v", rows[0])
	}
}

func TestIsoCreateCodexPackagesLink(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	real := os.Getenv("HOME")
	// Chưa có bản cài thật: không tạo gì, status cảnh báo.
	if out, code := run(t, dir, "", "iso", "create", "codex", "--name", "a", "--no-lb"); code != 0 || !strings.Contains(out, "430 MB") {
		t.Fatalf("create không có bản thật: %d\n%s", code, out)
	}
	pk := filepath.Join(e.profileDir("codex", "a"), "home", ".codex", "packages")
	if isExist(pk) {
		t.Fatal("không có bản cài thật thì không được tạo packages")
	}
	if out, _ := run(t, dir, "", "iso", "status"); !strings.Contains(out, "! codex/a: bản cài:") {
		t.Fatalf("status phải cảnh báo:\n%s", out)
	}
	// Có bản cài thật: create liên kết ngay; run bù cho profile cũ.
	rel := filepath.Join(real, ".codex", "packages", "app-server-daemon", "releases", "1.0")
	put(t, filepath.Join(rel, "codex"), "bin")
	if err := os.Symlink(rel, filepath.Join(real, ".codex", "packages", "app-server-daemon", "current")); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "", "iso", "create", "codex", "--name", "b", "--no-lb"); code != 0 {
		t.Fatalf("create: %d\n%s", code, out)
	}
	pkB := filepath.Join(e.profileDir("codex", "b"), "home", ".codex", "packages")
	if dst, err := os.Readlink(pkB); err != nil || dst != filepath.Join(real, ".codex", "packages") {
		t.Fatalf("packages -> %q %v", dst, err)
	}
	if out, code := run(t, dir, "", "iso", "run", "codex", "--name", "a", "--", "exec"); code != 0 {
		t.Fatalf("run: %d\n%s", code, out)
	}
	if dst, err := os.Readlink(pk); err != nil || dst != filepath.Join(real, ".codex", "packages") {
		t.Fatalf("run phải liên kết packages cho profile cũ: %q %v", dst, err)
	}
	if out, _ := run(t, dir, "", "iso", "status"); strings.Contains(out, "bản cài:") {
		t.Fatalf("hết cảnh báo khi đã liên kết:\n%s", out)
	}
}

func TestIsoDuTableUserDataAndStatus(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	if out, code := run(t, dir, "", "iso", "du"); code != 0 || !strings.Contains(out, "Chưa có profile") {
		t.Fatalf("rỗng: %d\n%s", code, out)
	}
	run(t, dir, "", "iso", "create", "claude", "--name", "c")
	run(t, dir, "", "iso", "create", "codex", "--name", "x", "--no-lb")
	home := filepath.Join(e.profileDir("claude", "c"), "home")
	put(t, filepath.Join(home, "Projects", "x", "main.go"), strings.Repeat("a", 100_000))
	put(t, filepath.Join(home, ".claude", "projects", "s", "11111111-2222-3333-4444-555555555555.jsonl"), strings.Repeat("b", 40_000))

	out, code := run(t, dir, "", "iso", "du")
	if code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	for _, want := range []string{"PROFILE", "TỔNG", "AUTH", "PHIÊN", "CACHE RIÊNG", "BẢN CÀI", "TEMP", "KHÁC", "DỮ LIỆU NGƯỜI DÙNG",
		"claude/c", "codex/x", "! claude/c: dữ liệu người dùng trong HOME giả: Projects/", "bint không tự xoá"} {
		if !strings.Contains(out, want) {
			t.Errorf("du thiếu %q:\n%s", want, out)
		}
	}
	rows := duJSON(t, dir)
	by := map[string]duRow{}
	for _, r := range rows {
		by[r.ID] = r
	}
	c := by["claude/c"]
	if c.UserData < 100_000 || c.Sessions < 40_000 || c.Total < c.UserData+c.Sessions {
		t.Fatalf("claude/c: %+v", c)
	}
	if by["codex/x"].UserData != 0 {
		t.Errorf("codex/x không có dữ liệu người dùng: %+v", by["codex/x"])
	}
	if one := duJSON(t, dir, "--profile", "codex/x"); len(one) != 1 || one[0].ID != "codex/x" {
		t.Fatalf("--profile: %+v", one)
	}
	if _, code := run(t, dir, "", "iso", "du", "--profile", "bad"); code != 2 {
		t.Fatalf("--profile sai: code %d", code)
	}
	st, _ := run(t, dir, "", "iso", "status")
	if !strings.Contains(st, "TỔNG") || !strings.Contains(st, "! claude/c: dữ liệu người dùng trong HOME giả: Projects/") {
		t.Fatalf("status:\n%s", st)
	}
	var raw []map[string]any
	js, _ := run(t, dir, "", "iso", "status", "--json")
	if err := json.Unmarshal([]byte(js), &raw); err != nil || len(raw) != 2 {
		t.Fatalf("status --json: %v\n%s", err, js)
	}
}

func TestIsoGCDeepForbiddenForAgentAndKeepsUserData(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "g")
	home := filepath.Join(e.profileDir("claude", "g"), "home")
	user := filepath.Join(home, "Projects", "x", "old.go")
	put(t, user, "mã")
	age(t, user, 100000*time.Hour)
	vers := filepath.Join(home, ".local", "share", "claude", "versions")
	put(t, filepath.Join(vers, "1.0"), "cũ")
	put(t, filepath.Join(vers, "2.0"), "mới")
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(vers, "2.0"), filepath.Join(home, ".local", "bin", "claude")); err != nil {
		t.Fatal(err)
	}

	asAgent(t)
	out, code := run(t, dir, "", "iso", "gc", "--deep")
	if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") || !isExist(filepath.Join(vers, "1.0")) {
		t.Fatalf("agent --deep: code %d\n%s", code, out)
	}
	// Không --deep thì agent chạy được và không đụng bản cài.
	if out, code := run(t, dir, "", "iso", "gc"); code != 0 {
		t.Fatalf("agent gc: %d\n%s", code, out)
	}
	asHuman(t)
	out, code = run(t, dir, "", "iso", "gc", "--deep", "--dry-run")
	if code != 0 || !strings.Contains(out, "sẽ xoá install "+filepath.Join(vers, "1.0")) || !isExist(filepath.Join(vers, "1.0")) {
		t.Fatalf("dry-run: %d\n%s", code, out)
	}
	if strings.Contains(out, "2.0") && strings.Contains(out, "xoá install "+filepath.Join(vers, "2.0")) {
		t.Fatalf("không được xoá bản đang dùng:\n%s", out)
	}
	out, code = run(t, dir, "", "iso", "gc", "--deep")
	if code != 0 || isExist(filepath.Join(vers, "1.0")) || !isExist(filepath.Join(vers, "2.0")) || !strings.Contains(out, "Đã giải phóng") {
		t.Fatalf("deep: %d\n%s", code, out)
	}
	if !isExist(user) {
		t.Fatal("dữ liệu người dùng (Projects/x) bị xoá")
	}
}

func TestIsoGCSessionsTTLZeroAndOpenTaskSession(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "codex", "--name", "s", "--no-lb")
	home := filepath.Join(e.profileDir("codex", "s"), "home")
	oldID, keepID := "aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002"
	oldF := filepath.Join(home, ".codex", "sessions", "2026", "01", "01", "rollout-2026-01-01T00-00-00-"+oldID+".jsonl")
	keepF := filepath.Join(home, ".codex", "sessions", "2026", "01", "01", "rollout-2026-01-01T00-00-00-"+keepID+".jsonl")
	for _, f := range []string{oldF, keepF} {
		put(t, f, "{}")
		age(t, f, 3000*time.Hour)
	}
	// Tác vụ chưa kết thúc tham chiếu keepID qua last_step.session_id.
	tdir := filepath.Join(os.Getenv("BINT_STATE_DIR"), "repo", "runtime", "tasks", "01M7H8YGR23QJQFMNNWKZGGR18")
	put(t, filepath.Join(tdir, "task.yml"), "id: 01M7H8YGR23QJQFMNNWKZGGR18\ngoal: g\nautonomy: ask_on_protected\n")
	st, _ := json.Marshal(map[string]any{"schema": 1, "id": "01M7H8YGR23QJQFMNNWKZGGR18", "status": "blocked",
		"last_step": map[string]any{"step": "b-feat", "run_id": "r", "stop_class": "external", "session_id": keepID}})
	put(t, filepath.Join(tdir, "state.json"), string(st))

	writeIsoCfg(t, "iso:\n  sessions_ttl: 0s\n")
	if out, code := run(t, dir, "", "iso", "gc", "--deep"); code != 0 || !isExist(oldF) {
		t.Fatalf("sessions_ttl 0 không được xoá phiên nào: %d\n%s", code, out)
	}
	writeIsoCfg(t, "iso:\n  sessions_ttl: 24h\n")
	out, code := run(t, dir, "", "iso", "gc")
	if code != 0 || isExist(oldF) || !isExist(keepF) {
		t.Fatalf("gc phiên: %d\n%s", code, out)
	}
}

func orphanTemp(t *testing.T, e isoEnv, name string) string {
	t.Helper()
	parent := filepath.Join(e.Tmp, "bint-iso-"+itoa(os.Getuid()))
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(parent, name)
	put(t, filepath.Join(p, "x"), "x")
	age(t, p, 72*time.Hour) // > tmp_ttl 24h
	return p
}

func TestIsoAutoGC(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": "exit 7"})
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "a")
	stamp := filepath.Join(e.Store, "gc.stamp")
	if isExist(stamp) {
		t.Fatal("create không được ghi gc.stamp")
	}

	// Chưa có stamp ⇒ phiên kết thúc chạy GC: dọn temp mồ côi, ghi stamp; mã thoát vẫn là của harness.
	ghost := orphanTemp(t, e, "codex--ghost")
	if out, code := run(t, dir, "", "iso", "run", "claude", "--name", "a"); code != 7 {
		t.Fatalf("code %d, muốn 7\n%s", code, out)
	}
	if isExist(ghost) {
		t.Error("GC tự động phải dọn temp mồ côi")
	}
	fi, err := os.Stat(stamp)
	if err != nil {
		t.Fatalf("thiếu gc.stamp: %v", err)
	}

	// Stamp còn mới (< gc_interval 6h) ⇒ không chạy.
	ghost = orphanTemp(t, e, "codex--ghost2")
	run(t, dir, "", "iso", "run", "claude", "--name", "a")
	if !isExist(ghost) {
		t.Error("stamp còn mới: GC không được chạy")
	}
	if fi2, _ := os.Stat(stamp); !fi2.ModTime().Equal(fi.ModTime()) {
		t.Error("stamp không được ghi lại khi GC không chạy")
	}

	// Stamp cũ hơn gc_interval ⇒ chạy và ghi lại stamp.
	age(t, stamp, 7*time.Hour)
	run(t, dir, "", "iso", "run", "claude", "--name", "a")
	if isExist(ghost) {
		t.Error("stamp cũ: GC phải chạy")
	}
	if fi3, _ := os.Stat(stamp); time.Since(fi3.ModTime()) > time.Minute {
		t.Error("stamp phải được ghi lại")
	}

	// gc_interval 0 = tắt.
	writeIsoCfg(t, "iso:\n  gc_interval: 0s\n")
	os.Remove(stamp)
	ghost = orphanTemp(t, e, "codex--ghost3")
	run(t, dir, "", "iso", "run", "claude", "--name", "a")
	if !isExist(ghost) || isExist(stamp) {
		t.Error("gc_interval 0: không được chạy GC hay ghi stamp")
	}
}

// Lỗi GC (ở đây: ghi gc.stamp không được vì tên bị thư mục chiếm) chỉ in cảnh báo; mã thoát là của harness.
func TestIsoAutoGCFailureOnlyWarns(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": "exit 9"})
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "a")
	stamp := filepath.Join(e.Store, "gc.stamp")
	if err := os.MkdirAll(stamp, 0o755); err != nil {
		t.Fatal(err)
	}
	age(t, stamp, 24*time.Hour)
	out, code := run(t, dir, "", "iso", "run", "claude", "--name", "a")
	if code != 9 || !strings.Contains(out, "GC tự động") {
		t.Fatalf("code %d, muốn 9 kèm cảnh báo\n%s", code, out)
	}
}

func TestIsoGCListsEveryActionAndDryRunMatches(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	writeIsoCfg(t, "iso:\n  cache: private\n")
	run(t, dir, "", "iso", "create", "claude", "--name", "c")
	home := filepath.Join(e.profileDir("claude", "c"), "home")
	var files []string
	for _, n := range []string{"a", "b", "c"} {
		f := filepath.Join(home, ".cache", "uv", n)
		put(t, f, strings.Repeat("x", 5000))
		age(t, f, 1000*time.Hour)
		files = append(files, f)
	}
	dry, code := run(t, dir, "", "iso", "gc", "--dry-run")
	if code != 0 {
		t.Fatalf("%d\n%s", code, dry)
	}
	for _, f := range files {
		if !strings.Contains(dry, "claude/c: sẽ xoá cache "+f) || !isExist(f) {
			t.Fatalf("dry-run thiếu %s hoặc đã xoá:\n%s", f, dry)
		}
	}
	real, code := run(t, dir, "", "iso", "gc")
	if code != 0 {
		t.Fatalf("%d\n%s", code, real)
	}
	lines := func(s string) []string {
		var out []string
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "xoá cache") {
				out = append(out, strings.TrimSpace(strings.Replace(l, "sẽ xoá", "xoá", 1)))
			}
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(lines(dry), lines(real)) {
		t.Fatalf("danh sách dry-run khác thật:\n%v\n%v", lines(dry), lines(real))
	}
	for _, f := range files {
		if isExist(f) {
			t.Errorf("%s phải bị xoá", f)
		}
	}
}
