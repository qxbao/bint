//go:build !windows

package iso

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"bint/internal/config"
)

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func linkTo(t *testing.T, path string) string {
	t.Helper()
	dst, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("%s không phải liên kết: %v", path, err)
	}
	return dst
}

// sharedProfile tạo profile với cache shared và liên kết mọi mục; dữ liệu thật được đặt sẵn ở đích.
func sharedProfile(t *testing.T) (Profile, fakeReal) {
	t.Helper()
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	writeFile(t, filepath.Join(r.Dir, "xcache", "uv", "keep"), "cache thật")
	writeFile(t, filepath.Join(r.Home, ".npm", "_npx", "keep"), "npx thật")
	if _, err := ensureShares("linux", p, r.Env); err != nil {
		t.Fatal(err)
	}
	return p, r
}

// Chuyển sang cache private: liên kết trỏ đúng đích danh mục bị gỡ, dữ liệu ở đích còn nguyên, home/.cache là
// thư mục thật để XDG_CACHE_HOME có chỗ ghi.
func TestEnsureSharesPrivateUnlinksCatalogTargets(t *testing.T) {
	p, r := sharedProfile(t)
	for _, in := range []string{".cache", ".npm", ".local/share/uv"} {
		if isl, _ := IsLink(filepath.Join(p.Home, in)); !isl {
			t.Fatalf("%s phải là liên kết trước khi đổi sang private", in)
		}
	}
	p.Settings.Cache = "private"

	if got, err := checkShares("linux", p, r.Env); err != nil || len(got) != len(config.IsoShareNames) {
		t.Fatalf("check: %+v %v", got, err)
	} else {
		for _, x := range got {
			if x.State != LinkRemoved {
				t.Errorf("check %s: %v, muốn removed", x.Share, x.State)
			}
		}
	}
	if isl, _ := IsLink(filepath.Join(p.Home, ".cache")); !isl {
		t.Fatal("checkShares không được ghi gì")
	}

	res, err := ensureShares("linux", p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range res {
		if x.State != LinkRemoved {
			t.Errorf("%s: %v, muốn removed", x.Share, x.State)
		}
	}
	for _, in := range []string{".npm", ".local/share/uv", "go/pkg/mod", ".cargo/registry"} {
		if _, err := os.Lstat(filepath.Join(p.Home, in)); !os.IsNotExist(err) {
			t.Errorf("%s phải bị gỡ: %v", in, err)
		}
	}
	fi, err := os.Lstat(filepath.Join(p.Home, ".cache"))
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("home/.cache phải là thư mục thật 0700: %v %v", fi, err)
	}
	for _, f := range []string{filepath.Join(r.Dir, "xcache", "uv", "keep"), filepath.Join(r.Home, ".npm", "_npx", "keep")} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("dữ liệu thật bị mất: %v", err)
		}
	}
	// Chạy lại: không còn gì để gỡ, cache riêng không bị đụng.
	writeFile(t, filepath.Join(p.Home, ".cache", "own"), "riêng")
	if res, err = ensureShares("linux", p, r.Env); err != nil || len(res) != 0 {
		t.Fatalf("lần hai: %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(p.Home, ".cache", "own")); err != nil {
		t.Error("cache riêng bị mất")
	}
}

// Bỏ một mục khỏi iso.share chỉ gỡ mục đó; liên kết lạ và thư mục có dữ liệu ở chỗ mục đã bỏ giữ nguyên.
func TestEnsureSharesDroppedEntryKeepsForeignAndData(t *testing.T) {
	p, r := sharedProfile(t)
	p.Settings.Share = slices.DeleteFunc(slices.Clone(p.Settings.Share), func(n string) bool {
		return n == "npm" || n == "bun" || n == "go-mod"
	})
	// bun: liên kết trỏ chỗ khác (người dùng tự đặt) ; go-mod: thay bằng thư mục có dữ liệu.
	other := t.TempDir()
	bun := filepath.Join(p.Home, ".bun", "install", "cache")
	if err := os.Remove(bun); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, bun); err != nil {
		t.Fatal(err)
	}
	gm := filepath.Join(p.Home, "go", "pkg", "mod")
	if err := os.Remove(gm); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(gm, "own"), "dữ liệu")

	res, err := ensureShares("linux", p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	removed := map[string]bool{}
	for _, x := range res {
		if x.State == LinkRemoved {
			removed[x.Share] = true
		}
	}
	if !removed["npm"] || removed["bun"] || removed["go-mod"] {
		t.Errorf("gỡ = %v, muốn đúng npm", removed)
	}
	if _, err := os.Lstat(filepath.Join(p.Home, ".npm")); !os.IsNotExist(err) {
		t.Error(".npm phải bị gỡ")
	}
	if linkTo(t, bun) != other {
		t.Error("liên kết lạ bị đụng")
	}
	if _, err := os.Stat(filepath.Join(gm, "own")); err != nil {
		t.Error("thư mục có dữ liệu bị đụng")
	}
	if _, err := os.Stat(filepath.Join(r.Home, ".npm", "_npx", "keep")); err != nil {
		t.Error("dữ liệu thật ở đích bị mất")
	}
	// Mục còn hiệu lực vẫn OK; cache vẫn liên kết.
	if isl, _ := IsLink(filepath.Join(p.Home, ".cache")); !isl {
		t.Error("cache còn hiệu lực phải vẫn là liên kết")
	}
}

func TestEnsurePrivateKeepsDataDirAtLinkPath(t *testing.T) {
	s := testStore(t, func(c *config.Iso) { c.Cache = "private" })
	p := mustCreate(t, s, Claude, "w")
	r := newFakeReal(t)
	writeFile(t, filepath.Join(p.Home, ".cache", "own", "x"), "dữ liệu")
	if res, err := ensureShares("linux", p, r.Env); err != nil || len(res) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(p.Home, ".cache", "own", "x")); err != nil {
		t.Fatal("dữ liệu bị mất")
	}
}
