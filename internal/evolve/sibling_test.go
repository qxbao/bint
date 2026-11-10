package evolve

import (
	"context"
	"fmt"
	"testing"

	"bint/internal/runlog"
)

func (f *fixture) scanRuns(t *testing.T, runs []*Run) ScanResult {
	t.Helper()
	h := &History{Runs: runs, ByID: map[string]*Run{}}
	for _, r := range runs {
		h.ByID[r.ID] = r
	}
	res, err := Scan(context.Background(), ScanInput{DB: f.db, Cfg: f.k.Config, Now: now, History: h})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func okRun(id string, files ...string) *Run {
	return &Run{ID: id, Workflow: "x", Label: runlog.LabelSuccess, Modified: files}
}

func TestSiblingFilesEnableExploration(t *testing.T) {
	f := setup(t)
	res := f.scanRuns(t, []*Run{
		okRun("A", "handlers/user_handler.py"),
		okRun("B", "handlers/order_handler.py"),
		okRun("C", "handlers/cart_handler.py"),
	})
	ops := byKey(res)
	exp, ok := ops["exploration:handlers"]
	if !ok || len(exp.Files) != 1 || exp.Files[0].Path != "handlers/*_handler.py" || len(exp.Files[0].Members) != 3 || len(exp.SupportingRuns) != 3 || !exp.Ready {
		t.Fatalf("exploration: %+v", exp)
	}
	if sk, ok := ops["skill:handlers"]; !ok || sk.Files[0].Path != "handlers/*_handler.py" {
		t.Fatalf("skill_candidate: %+v", sk)
	}
}

func TestScatteredFilesDoNotEnable(t *testing.T) {
	f := setup(t)
	res := f.scanRuns(t, []*Run{
		okRun("A", "a/one.go"), okRun("B", "b/two.go"), okRun("C", "c/three.go"),
		okRun("D", "handlers/x_handler.py"), okRun("E", "handlers/y_handler.py"), // chỉ 2 lượt
	})
	for k := range byKey(res) {
		if k == "exploration:a" || k == "exploration:handlers" || k == "skill:handlers" {
			t.Fatalf("không được bật: %s", k)
		}
	}
}

func TestSiblingDedupWithFileLevel(t *testing.T) {
	f := setup(t)
	var runs []*Run
	for i := 0; i < 3; i++ {
		runs = append(runs, okRun(fmt.Sprintf("R%d", i), "pkg/core.go"))
	}
	res := f.scanRuns(t, runs)
	exp := byKey(res)["exploration:pkg"]
	if len(exp.Files) != 1 || exp.Files[0].Path != "pkg/core.go" || len(exp.Files[0].Members) != 0 {
		t.Fatalf("%+v", exp.Files)
	}
}

func TestMonorepoSplitsDominantGroup(t *testing.T) {
	var runs []*Run
	for i, p := range []string{"apps/web/ui/a.ts", "apps/web/ui/b.ts", "apps/web/api/c.ts", "apps/web/api/d.ts", "apps/web/ui/e.ts", "apps/web/api/f.ts"} {
		runs = append(runs, okRun(fmt.Sprintf("R%d", i), p))
	}
	mods := assignModules(runs, 0, 3)
	if mods["R0"] != "apps/web/ui" || mods["R2"] != "apps/web/api" {
		t.Fatalf("%v", mods)
	}
	// Không có nhóm trội: giữ hai cấp.
	mixed := []*Run{okRun("A", "x/y/z/a.go"), okRun("B", "p/q/r/b.go"), okRun("C", "m/n/o/c.go")}
	if got := assignModules(mixed, 0, 1); got["A"] != "x/y" {
		t.Fatalf("%v", got)
	}
	// group_depth cấu hình.
	if got := assignModules(mixed, 3, 1); got["A"] != "x/y/z" {
		t.Fatalf("%v", got)
	}
}
