//go:build !windows

package iso

import (
	"bint/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func bigFile(t *testing.T, path string, n int) {
	t.Helper()
	writeFile(t, path, strings.Repeat("x", n))
}

func TestWalkUsageNoFollowHardlinkOnce(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "real")
	bigFile(t, filepath.Join(real, "big"), 200_000)
	root := filepath.Join(d, "root")
	bigFile(t, filepath.Join(root, "a"), 10_000)
	if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "a2")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	var paths []string
	var sum int64
	if err := walkUsage(root, map[fileID]bool{}, func(p string, _ os.FileInfo, size int64) {
		paths = append(paths, filepath.Base(p))
		sum += size
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if p == "big" {
			t.Fatal("đi qua liên kết")
		}
	}
	if sum >= 100_000 {
		t.Fatalf("tổng %d: đếm cả đích liên kết hoặc hardlink hai lần", sum)
	}
	one, _ := treeSize(filepath.Join(root, "a"))
	if sum < one || sum > one+4*4096+4096 {
		t.Fatalf("tổng %d với một tệp %d: hardlink phải đếm một lần", sum, one)
	}
}

// profileFixture dựng một profile có đủ nhóm dung lượng.
func profileFixture(t *testing.T) (Profile, []string) {
	t.Helper()
	s := testStore(t, func(c *config.Iso) { c.Cache = "private" })
	p := mustCreate(t, s, Claude, "w")
	h := p.Home
	bigFile(t, filepath.Join(h, ".claude", ".credentials.json"), 5_000)
	bigFile(t, filepath.Join(h, ".claude", "projects", "slug", "11111111-2222-3333-4444-555555555555.jsonl"), 30_000)
	bigFile(t, filepath.Join(h, ".cache", "uv", "wheel"), 50_000)
	bigFile(t, filepath.Join(h, ".local", "share", "claude", "versions", "2.1.0"), 70_000)
	bigFile(t, filepath.Join(h, ".local", "share", "other", "f"), 9_000)
	bigFile(t, filepath.Join(h, "Projects", "x", "main.go"), 90_000)
	bigFile(t, filepath.Join(h, "notes.txt"), 6_000)
	bigFile(t, filepath.Join(h, ".hidden", "f"), 4_000) // bắt đầu bằng "." ⇒ không phải dữ liệu người dùng
	r := newFakeReal(t)
	return p, r.Env
}

func TestUsageGroupsAndUserData(t *testing.T) {
	p, env := profileFixture(t)
	tmp := p.tempDirFrom(env)
	bigFile(t, filepath.Join(tmp, "t"), 20_000)
	u, err := Usage(p, env)
	if err != nil {
		t.Fatal(err)
	}
	if u.Sessions < 30_000 || u.Sessions > 60_000 || u.Cache < 50_000 || u.Install < 70_000 || u.Temp < 20_000 || u.AuthConfig < 5_000 {
		t.Fatalf("nhóm: %+v", u)
	}
	if u.UserData < 96_000 || u.UserData > 120_000 {
		t.Fatalf("user_data = %d", u.UserData)
	}
	if u.Other < 13_000 { // .local/share/other + .hidden
		t.Fatalf("other = %d", u.Other)
	}
	if got := u.AuthConfig + u.Sessions + u.Cache + u.Install + u.Other + u.UserData; got != u.Total {
		t.Fatalf("tổng các nhóm %d != Total %d", got, u.Total)
	}
	var names []string
	for _, e := range u.UserEntries {
		names = append(names, e.Path)
	}
	if strings.Join(names, ",") != "Projects/,notes.txt" {
		t.Fatalf("user_entries = %v", names)
	}
}

// Liên kết cache chia sẻ: dữ liệu ở đích không bị tính, bản thân liên kết không phải "cache riêng".
func TestUsageSharedCacheLinkCountsZero(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	bigFile(t, filepath.Join(r.Dir, "xcache", "uv", "big"), 300_000)
	if _, err := ensureShares("linux", p, r.Env); err != nil {
		t.Fatal(err)
	}
	u, err := Usage(p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if u.Cache != 0 || u.Total > 100_000 {
		t.Fatalf("cache riêng = %d, tổng = %d", u.Cache, u.Total)
	}
	// Một harness ghi qua liên kết: dữ liệu vào cache thật, cache riêng vẫn 0.
	bigFile(t, filepath.Join(p.Home, ".cache", "uv", "new"), 40_000)
	bigFile(t, filepath.Join(p.Home, ".npm", "_npx", "p"), 40_000)
	bigFile(t, filepath.Join(p.Home, ".local", "share", "uv", "tools", "t"), 40_000)
	if u, _ = Usage(p, r.Env); u.Cache != 0 || u.Total > 100_000 {
		t.Fatalf("sau khi ghi qua liên kết: cache riêng = %d, tổng = %d", u.Cache, u.Total)
	}
}

func duSK(t *testing.T, dir string) int64 {
	t.Helper()
	out, err := exec.Command("du", "-sk", dir).Output()
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.ParseInt(strings.Fields(string(out))[0], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Tổng thư mục profile và temp (KiB) bằng đúng `du -sk`, kể cả hardlink, liên kết, thư mục rỗng, tệp thưa.
func TestUsageMatchesDuSK(t *testing.T) {
	if _, err := exec.LookPath("du"); err != nil {
		t.Skip("không có du")
	}
	p, env := profileFixture(t)
	h := p.Home
	if err := os.Link(filepath.Join(h, "notes.txt"), filepath.Join(h, "notes2.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h, "emptydir", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := t.TempDir()
	bigFile(t, filepath.Join(real, "huge"), 1<<20)
	if err := os.Symlink(real, filepath.Join(h, ".npm")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(strings.Repeat("d/", 40)+"x", filepath.Join(h, "longlink")); err != nil { // đích dài: tốn khối riêng
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(h, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(50 << 20); err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteAt([]byte("x"), 12345)
	f.Close()
	tmp := p.tempDirFrom(env)
	bigFile(t, filepath.Join(tmp, "a", "t"), 33_333)
	if err := os.MkdirAll(filepath.Join(tmp, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	u, err := Usage(p, env)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := KiB(u.Total), duSK(t, p.Dir); got != want {
		t.Errorf("profile: %d KiB, du -sk %d", got, want)
	}
	if got, want := KiB(u.Temp), duSK(t, tmp); got != want {
		t.Errorf("temp: %d KiB, du -sk %d", got, want)
	}
}
