package iso

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bint/internal/config"
)

// Danh mục share của mỗi OS phải có đúng tập tên mà config chấp nhận (cùng thứ tự), và mọi biến env của danh mục
// phải được iso khai là biến sở hữu (có bản REAL) cùng nằm trong config.IsoManagedEnv.
func TestCatalogMatchesConfigNames(t *testing.T) {
	r := newFakeReal(t)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		e := newEnvList(goos, append(slices.Clone(r.Env), "USERPROFILE="+r.Home))
		var names []string
		for _, s := range catalog(goos, e) {
			names = append(names, s.Name)
			if len(s.Links) == 0 {
				t.Errorf("%s/%s: không có liên kết", goos, s.Name)
			}
			for _, l := range s.Links {
				if filepath.IsAbs(l.InHome) || strings.Contains(l.InHome, "..") {
					t.Errorf("%s/%s: InHome %q phải tương đối và nằm trong HOME", goos, s.Name, l.InHome)
				}
				if !filepath.IsAbs(l.Real) {
					t.Errorf("%s/%s: Real %q phải tuyệt đối", goos, s.Name, l.Real)
				}
			}
			for _, v := range s.Env {
				if !slices.Contains(ownedVars(), v.Name) || !slices.Contains(config.IsoManagedEnv, v.Name) {
					t.Errorf("%s/%s: biến %s chưa khai là sở hữu/managed", goos, s.Name, v.Name)
				}
				if !filepath.IsAbs(v.Value) {
					t.Errorf("%s/%s: %s=%q phải tuyệt đối", goos, s.Name, v.Name, v.Value)
				}
			}
		}
		if !slices.Equal(names, config.IsoShareNames) {
			t.Errorf("%s: danh mục %v khác config.IsoShareNames %v", goos, names, config.IsoShareNames)
		}
	}
	for _, v := range shareEnvVars {
		found := false
		for _, s := range catalog("linux", newEnvList("linux", r.Env)) {
			for _, ev := range s.Env {
				found = found || ev.Name == v
			}
		}
		if !found {
			t.Errorf("shareEnvVars có %s mà danh mục Linux không đặt", v)
		}
	}
}

func TestCatalogRealPathsFollowRealEnv(t *testing.T) {
	r := newFakeReal(t)
	env := append(slices.Clone(r.Env), "XDG_DATA_HOME=/d", "GOMODCACHE=/gm", "CARGO_HOME=/cg", "BUN_INSTALL=/bn")
	got := map[string]string{}
	for _, s := range catalog("linux", newEnvList("linux", env)) {
		for _, l := range s.Links {
			got[s.Name] = l.Real
		}
	}
	for k, v := range map[string]string{
		"cache": r.Dir + "/xcache", "npm": r.Home + "/.npm", "pnpm-store": "/d/pnpm/store", "uv-data": "/d/uv",
		"go-mod": "/gm", "cargo-registry": "/cg/registry", "bun": "/bn/install/cache",
	} {
		if got[k] != v {
			t.Errorf("%s -> %q, muốn %q", k, got[k], v)
		}
	}
	// Mặc định khi không đặt biến.
	plain := newEnvList("linux", []string{"HOME=" + r.Home})
	got = map[string]string{}
	for _, s := range catalog("linux", plain) {
		got[s.Name] = s.Links[0].Real
	}
	for k, v := range map[string]string{
		"cache": r.Home + "/.cache", "go-mod": r.Home + "/go/pkg/mod", "cargo-registry": r.Home + "/.cargo/registry",
		"bun": r.Home + "/.bun/install/cache", "uv-data": r.Home + "/.local/share/uv",
	} {
		if got[k] != v {
			t.Errorf("mặc định %s -> %q, muốn %q", k, got[k], v)
		}
	}
}

func TestEnsureSharesCreatesLinks(t *testing.T) {
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	r := newFakeReal(t)
	res, err := ensureShares("linux", p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(config.IsoShareNames) {
		t.Fatalf("kết quả %d mục, muốn %d: %+v", len(res), len(config.IsoShareNames), res)
	}
	for _, x := range res {
		if x.State != LinkCreated {
			t.Errorf("%s: %v", x.Share, x.State)
		}
		if fi, err := os.Stat(x.Target); err != nil || !fi.IsDir() {
			t.Errorf("%s: đích %s phải được tạo: %v", x.Share, x.Target, err)
		}
		if isl, _ := IsLink(x.Link); !isl {
			t.Errorf("%s: %s không phải liên kết", x.Share, x.Link)
		}
	}
	// Ghi vào cache qua HOME giả phải rơi vào cache thật.
	if err := os.MkdirAll(filepath.Join(p.Home, ".cache", "uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Home, ".cache", "uv", "x"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "xcache", "uv", "x")); err != nil {
		t.Fatalf("cache không rơi vào cache thật: %v", err)
	}
	// Chạy lại: mọi mục OK.
	res, err = ensureShares("linux", p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range res {
		if x.State != LinkOK {
			t.Errorf("lần hai %s: %v", x.Share, x.State)
		}
	}
}

func TestEnsureSharesConflictAndPrivate(t *testing.T) {
	r := newFakeReal(t)
	s := testStore(t, nil)
	p := mustCreate(t, s, Claude, "w")
	// thư mục có dữ liệu ở .npm: giữ nguyên, báo xung đột
	if err := os.MkdirAll(filepath.Join(p.Home, ".npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Home, ".npm", "own"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// thư mục rỗng ở .cache: được thay
	if err := os.MkdirAll(filepath.Join(p.Home, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := ensureShares("linux", p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]LinkState{}
	for _, x := range res {
		states[x.Share] = x.State
	}
	if states["npm"] != LinkConflictData || states["cache"] != LinkReplacedEmpty {
		t.Errorf("states = %v", states)
	}
	if _, err := os.Stat(filepath.Join(p.Home, ".npm", "own")); err != nil {
		t.Error("dữ liệu riêng bị mất")
	}

	// cache private: không liên kết gì
	priv := testStore(t, func(c *config.Iso) { c.Cache = "private" })
	q := mustCreate(t, priv, Codex, "priv")
	res, err = ensureShares("linux", q, r.Env)
	if err != nil || len(res) != 0 {
		t.Fatalf("private: %v %v", res, err)
	}
	if isl, _ := IsLink(filepath.Join(q.Home, ".cache")); isl {
		t.Error("private không được liên kết cache")
	}
}
