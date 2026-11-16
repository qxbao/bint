package iso

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/state"
	"bint/internal/testutil"
)

const childFlag = "BINT_ISO_TEST_CHILD"

type childReport struct {
	BaseDir, DB, Config, StateEnv, ConfigEnv, Home string
}

// TestChildHelper là tiến trình con: in nơi nó thấy state/config; không làm gì khi chạy như test thường.
func TestChildHelper(t *testing.T) {
	if os.Getenv(childFlag) != "1" {
		t.Skip("chỉ chạy như tiến trình con")
	}
	var rep childReport
	var err error
	if rep.BaseDir, err = state.BaseDir(); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	repo, err := gitrepo.Discover(context.Background(), cwd)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := state.Resolve(repo, config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rep.DB, rep.Config = paths.DB, config.GlobalPath()
	rep.StateEnv, rep.ConfigEnv, rep.Home = os.Getenv(envStateDir), os.Getenv(envConfigHome), os.Getenv("HOME")
	out, _ := json.Marshal(rep)
	if err := os.WriteFile(os.Getenv(childFlag+"_OUT"), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// AC-3: con chạy với env do iso.Env dựng (HOME và XDG giả, cha không có BINT_STATE_DIR/BINT_CONFIG_HOME) phải mở
// đúng state.sqlite và đọc đúng config toàn cục của cha.
func TestChildProcessSeesRealStateAndConfig(t *testing.T) {
	cases := []struct {
		name     string
		stateEnv string // giá trị BINT_STATE_DIR của cha ("" = không đặt)
		cfgEnv   string
		xdg      bool
	}{
		{"cha dùng mặc định theo HOME", "", "", false},
		{"cha dùng XDG", "", "", true},
		{"cha ghim đường dẫn tương đối", "rel-state", "rel-cfg", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testStore(t, nil) // Isolate: HOME/BINT_* tạm; ghi đè bên dưới cho đúng kịch bản
			realHome := filepath.Join(t.TempDir(), "realhome")
			t.Chdir(t.TempDir())
			t.Setenv("HOME", realHome)
			t.Setenv("USERPROFILE", realHome)
			t.Setenv(envStateDir, c.stateEnv)
			t.Setenv(envConfigHome, c.cfgEnv)
			t.Setenv("XDG_STATE_HOME", "")
			t.Setenv("XDG_CONFIG_HOME", "")
			if c.xdg {
				t.Setenv("XDG_STATE_HOME", filepath.Join(realHome, "xs"))
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(realHome, "xc"))
			}
			repoDir := testutil.NewRepo(t, nil)

			// giá trị thật theo cha
			parentBase, err := state.BaseDir()
			if err != nil {
				t.Fatal(err)
			}
			repo, err := gitrepo.Discover(context.Background(), repoDir)
			if err != nil {
				t.Fatal(err)
			}
			parentPaths, err := state.Resolve(repo, config.Default())
			if err != nil {
				t.Fatal(err)
			}
			// GlobalPath không tuyệt đối hoá giá trị tương đối của BINT_CONFIG_HOME: so theo cwd của cha.
			parentCfg, err := filepath.Abs(config.GlobalPath())
			if err != nil {
				t.Fatal(err)
			}

			p := mustCreate(t, s, Codex, "w")
			env, err := Env(p, os.Environ())
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out.json")
			cmd := exec.Command(os.Args[0], "-test.run=^TestChildHelper$", "-test.count=1")
			cmd.Dir = repoDir // cwd khác cha: đường dẫn tương đối của cha phải đã được ghim tuyệt đối
			cmd.Env = append(env, childFlag+"=1", childFlag+"_OUT="+out)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("con lỗi: %v\n%s", err, b)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("con không ghi kết quả (bị Skip?): %v", err)
			}
			var rep childReport
			if err := json.Unmarshal(data, &rep); err != nil {
				t.Fatal(err)
			}
			if rep.Home != p.Home {
				t.Errorf("con phải chạy với HOME giả %q, thấy %q", p.Home, rep.Home)
			}
			if rep.BaseDir != parentBase {
				t.Errorf("state.BaseDir: con %q, cha %q", rep.BaseDir, parentBase)
			}
			if rep.DB != parentPaths.DB {
				t.Errorf("state.sqlite: con %q, cha %q", rep.DB, parentPaths.DB)
			}
			if rep.Config != parentCfg {
				t.Errorf("config.GlobalPath: con %q, cha %q", rep.Config, parentCfg)
			}
			if !filepath.IsAbs(rep.StateEnv) || rep.StateEnv != parentBase {
				t.Errorf("BINT_STATE_DIR của con = %q, muốn tuyệt đối %q", rep.StateEnv, parentBase)
			}
			if !filepath.IsAbs(rep.ConfigEnv) || filepath.Join(rep.ConfigEnv, config.FileName) != parentCfg {
				t.Errorf("BINT_CONFIG_HOME của con = %q, muốn thư mục của %q", rep.ConfigEnv, parentCfg)
			}
		})
	}
}
