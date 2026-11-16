package config

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestValidateIso(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "iso")
	cases := []struct {
		name string
		mut  func(*Iso)
		want string // rỗng = hợp lệ
	}{
		{"mặc định", func(*Iso) {}, ""},
		{"home tuyệt đối", func(i *Iso) { i.Home = abs }, ""},
		{"home rỗng", func(i *Iso) { i.Home = "" }, "iso.home"},
		{"home tương đối", func(i *Iso) { i.Home = "rel/iso" }, "iso.home"},
		{"cache lạ", func(i *Iso) { i.Cache = "shard" }, "iso.cache"},
		{"cache private", func(i *Iso) { i.Cache = "private" }, ""},
		{"share lạ", func(i *Iso) { i.Share = []string{"cache", "docker"} }, `"docker"`},
		{"share trùng", func(i *Iso) { i.Share = []string{"npm", "npm"} }, "trùng"},
		{"share rỗng", func(i *Iso) { i.Share = nil }, ""},
		{"cache_ttl hỏng", func(i *Iso) { i.CacheTTL = "30 ngày" }, "iso.cache_ttl"},
		{"cache_ttl âm", func(i *Iso) { i.CacheTTL = "-1h" }, "iso.cache_ttl"},
		{"sessions_ttl 0", func(i *Iso) { i.SessionsTTL = "0" }, ""},
		{"sessions_ttl rỗng", func(i *Iso) { i.SessionsTTL = "" }, "iso.sessions_ttl"},
		{"gc_interval hỏng", func(i *Iso) { i.GCInterval = "6" }, "iso.gc_interval"},
		{"tmp_ttl âm", func(i *Iso) { i.TmpTTL = "-5m" }, "iso.tmp_ttl"},
		{"pass_env credential", func(i *Iso) { i.PassEnv = []string{"OPENAI_API_KEY"} }, ""},
		{"pass_env do iso quản lý", func(i *Iso) { i.PassEnv = []string{"HOME"} }, "tự đặt"},
		{"pass_env khác hoa thường", func(i *Iso) { i.PassEnv = []string{"xdg_cache_home"} }, "tự đặt"},
		{"pass_env BINT_STATE_DIR", func(i *Iso) { i.PassEnv = []string{"BINT_STATE_DIR"} }, "tự đặt"},
		{"pass_env BINT_ISO_REAL_X", func(i *Iso) { i.PassEnv = []string{"bint_iso_real_x"} }, "tự đặt"},
		{"pass_env có dấu =", func(i *Iso) { i.PassEnv = []string{"A=B"} }, "iso.pass_env"},
		{"drop_env rỗng", func(i *Iso) { i.DropEnv = []string{""} }, "iso.drop_env"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Default()
			c.mut(&cfg.Iso)
			err := cfg.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("muốn hợp lệ, lỗi: %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("muốn lỗi chứa %q, nhận %v", c.want, err)
			}
		})
	}
}

func TestIsoDefaultsUseCatalog(t *testing.T) {
	d := Default().Iso
	if !slices.Equal(d.Share, IsoShareNames) {
		t.Fatalf("share mặc định %v khác IsoShareNames %v", d.Share, IsoShareNames)
	}
	d.Share[0] = "x"
	if IsoShareNames[0] == "x" {
		t.Fatal("Default().Iso.Share dùng chung mảng với IsoShareNames")
	}
}

// GlobalPathFrom(os.LookupEnv) phải trùng hành vi cũ (os.UserConfigDir) trên OS đang chạy.
func TestGlobalPathFromMatchesUserConfigDir(t *testing.T) {
	t.Setenv("BINT_CONFIG_HOME", "")
	for _, env := range []map[string]string{
		{},
		{"XDG_CONFIG_HOME": filepath.Join(t.TempDir(), "xdg")},
		{"XDG_CONFIG_HOME": "tuong-doi"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		want := ""
		if d, err := os.UserConfigDir(); err == nil {
			want = filepath.Join(d, "bint", FileName)
		}
		if got := GlobalPath(); got != want {
			t.Errorf("env %v: GlobalPath = %q, muốn %q", env, got, want)
		}
		if got := GlobalPathFrom(os.LookupEnv); got != want {
			t.Errorf("env %v: GlobalPathFrom = %q, muốn %q", env, got, want)
		}
	}
}

func TestGlobalPathFromLookup(t *testing.T) {
	look := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	if got := GlobalPathFrom(look(map[string]string{"BINT_CONFIG_HOME": "/c"})); got != filepath.Join("/c", FileName) {
		t.Errorf("BINT_CONFIG_HOME: %q", got)
	}
	if got := GlobalPathFrom(look(nil)); got != "" {
		t.Errorf("env rỗng: %q", got)
	}
	for goos, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"linux":   {map[string]string{"HOME": "/h"}, "/h/.config"},
		"darwin":  {map[string]string{"HOME": "/Users/u", "XDG_CONFIG_HOME": "/x"}, "/Users/u/Library/Application Support"},
		"windows": {map[string]string{"AppData": `C:\Users\u\AppData\Roaming`, "HOME": "/h"}, `C:\Users\u\AppData\Roaming`},
	} {
		if got := userConfigDir(goos, look(c.env)); got != c.want {
			t.Errorf("%s: %q, muốn %q", goos, got, c.want)
		}
	}
}

// Cấu hình cũ chưa có khối iso: Upgrade chèn khối (giữ mọi dòng cũ), nạp lại được và hợp lệ.
func TestUpgradeAddsIso(t *testing.T) {
	old := Render(InitParams{ProjectName: "demo"})
	i := strings.Index(old, "\niso:")
	if i < 0 {
		t.Fatalf("mẫu thiếu khối iso:\n%s", old)
	}
	old = old[:i+1]
	res, err := Upgrade([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Added, "iso") && !slices.Contains(res.Added, "iso.home") {
		t.Fatalf("Added = %v", res.Added)
	}
	got := string(res.Content)
	if !strings.HasPrefix(got, strings.TrimRight(old, "\n")) || !strings.Contains(got, "iso:") {
		t.Fatalf("khối iso chèn sai:\n%s", got)
	}
	cfg := Default()
	if err := Decode(strings.NewReader(got), &cfg); err != nil || cfg.Validate() != nil {
		t.Fatalf("nạp lại: %v", err)
	}
	if !reflect.DeepEqual(cfg.Iso, Default().Iso) {
		t.Fatalf("iso sau nâng cấp %+v khác mặc định %+v", cfg.Iso, Default().Iso)
	}
	// Người dùng đã đặt iso.cache: giữ nguyên.
	mine, err := Upgrade([]byte(old + "\niso:\n  cache: private\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mine.Content), "cache: private") || slices.Contains(mine.Added, "iso.cache") {
		t.Fatalf("không được chèn lại iso.cache: %v\n%s", mine.Added, mine.Content)
	}
}
