package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/iso"
)

// isoStoreWithProfile tạo store (BINT_ISO_HOME của testutil.Isolate) có một profile codex; userData != "" thì thêm
// thư mục dữ liệu người dùng đó vào HOME giả.
// realDoctor gán loader Doctor thật (loader mẫu của sysReal chỉ có hai dòng giả).
func realDoctor(o *Options) {
	o.SystemLoad.Doctor = func(ctx context.Context, dir string) (*bintapp.DoctorReport, error) {
		return bintapp.Doctor(ctx, dir), nil
	}
}

func isoStoreWithProfile(t *testing.T, userData string) {
	t.Helper()
	st, err := iso.OpenStore(config.Default().Iso, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(iso.Codex, "work")
	if err != nil {
		t.Fatal(err)
	}
	if userData != "" {
		dir := filepath.Join(p.Home, userData)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Thẻ Doctor của tab Hệ thống hiện dòng iso (doctor thật) khi store có profile: mức warn và gợi ý dọn khi HOME giả
// có dữ liệu người dùng.
func TestSystemDoctorShowsIsoRow(t *testing.T) {
	m, _, _ := sysRealWith(t, func(string) { isoStoreWithProfile(t, "Projects") }, realDoctor)
	press(t, m, ".", ".", ".", ".", ".", ".") // Cấu hình → … → Doctor
	press(t, m, "w")                          // chỉ hiện mục warn/fail
	var out string
	for i := 0; i < 12; i++ {
		out = plain(m.Render())
		if strings.Contains(out, "1 profile") {
			break
		}
		press(t, m, "j")
	}
	hasAll(t, out, "iso", "1 profile", "bint iso gc", "Projects/", "tự xoá")
}

func TestSystemDoctorHasNoIsoRowWithoutProfiles(t *testing.T) {
	m, _, _ := sysRealWith(t, nil, realDoctor)
	press(t, m, ".", ".", ".", ".", ".", ".")
	for i := 0; i < 20; i++ {
		if out := plain(m.Render()); strings.Contains(out, "profile, tổng") {
			t.Fatalf("store chưa có profile mà doctor có dòng iso:\n%s", out)
		}
		press(t, m, "j")
	}
}
