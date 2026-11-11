package cli_test

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"bint/internal/app"
	"bint/internal/cli"
	"bint/internal/config"
	"bint/internal/testutil"
)

// `runs rate` kiểm tham số trước (mã 2) rồi mới tới kiểm chỉ-dành-cho-người (nay nằm ở dịch vụ): điểm sai trong phiên agent
// vẫn là lỗi dùng sai, điểm đúng trong phiên agent là lỗi "chỉ dành cho người dùng" (mã 1).
func TestRunsRateOrderOfErrors(t *testing.T) {
	testutil.Isolate(t)
	asAgent(t)
	root := testutil.NewRepo(t, map[string]string{"a.go": "package a\n"})
	if out, code := run(t, root, "", "runs", "rate", "x", "maybe"); code != 2 || !strings.Contains(out, "good hoặc bad") {
		t.Fatalf("điểm sai: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "runs", "rate", "x", "good"); code != 1 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("điểm đúng trong phiên agent: %d\n%s", code, out)
	}
}

// Giá trị mặc định của cờ CLI lấy từ Defaults(cfg) của dịch vụ: không chép hằng số sang CLI.
func TestRunsFlagDefaultsComeFromServiceDefaults(t *testing.T) {
	cfg := config.Default()
	ld, sd := app.RunsListDefaults(cfg), app.RunsScanDefaults(cfg)
	root := cli.NewRoot(cli.IO{})
	find := func(path ...string) *pflag.FlagSet {
		c, _, err := root.Find(path)
		if err != nil || c == nil {
			t.Fatalf("không thấy lệnh %v: %v", path, err)
		}
		return c.Flags()
	}
	check := func(fs *pflag.FlagSet, name, want string) {
		t.Helper()
		if f := fs.Lookup(name); f == nil || f.DefValue != want {
			t.Errorf("cờ --%s: mặc định %v, muốn %s", name, f, want)
		}
	}
	check(find("runs", "list"), "limit", "20")
	check(find("runs", "scan"), "days", "30")
	check(find("runs", "scan"), "revert-days", "14")
	if ld.Limit != 20 || sd.Days != 30 || sd.RevertDays != 14 {
		t.Fatalf("Defaults: %+v %+v", ld, sd)
	}
}
