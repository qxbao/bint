package cli

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/tui/actions"
)

// serviceBindings ghép đường lệnh với kiểu Options của dịch vụ đã tách khỏi CLI. Mọi cờ của các lệnh
// này phải ánh xạ tới một trường Options (thẻ `flag`) hoặc nằm trong danh sách loại trừ có lý do.
var serviceBindings = map[string]any{
	"knowledge review":  app.ReviewQueueOptions{},
	"bootstrap publish": app.PublishOptions{},

	"task new": app.TaskNewOptions{}, "task list": app.TaskListOptions{}, "task show": app.TaskShowOptions{}, "task set": app.TaskSetOptions{},
	"task write": app.TaskWriteOptions{}, "task status": app.TaskStatusOptions{}, "task verify": app.TaskVerifyOptions{},
	"task criteria": app.TaskCriteriaOptions{}, "task error": app.TaskErrorOptions{}, "task review": app.TaskReviewOptions{},
	"task lint": app.TaskLintOptions{}, "task approve": app.TaskApproveOptions{},
	"run": app.RunWorkflowOptions{}, "run resume": app.RunResumeOptions{}, "run status": app.TaskRunsOptions{},

	"eval mine": app.EvalMineOptions{}, "eval verify": app.EvalVerifyOptions{}, "eval list": app.EvalListOptions{},
	"eval show": app.EvalShowOptions{}, "eval spec": app.EvalSpecOptions{}, "eval approve": app.EvalApproveOptions{},
	"eval reject": app.EvalRejectOptions{}, "eval estimate": app.EvalEstimateOptions{}, "eval run": app.EvalRunOptions{},
	"eval report": app.EvalReportOptions{}, "eval bootstrap": app.EvalBootstrapOptions{}, "eval clean": app.EvalCleanOptions{},

	"runs list": app.RunsListOptions{}, "runs show": app.RunsShowOptions{}, "runs rate": app.RunsRateOptions{}, "runs scan": app.RunsScanOptions{},
	"events ingest": app.EventsIngestOptions{}, "experience digest": evolvesvc.ExperienceDigestOptions{}, "experience show": evolvesvc.ExperienceShowOptions{},

	"evolve scan": evolvesvc.ScanOptions{}, "evolve propose": evolvesvc.ProposeOptions{}, "evolve list": evolvesvc.ListOptions{},
	"evolve show": evolvesvc.ShowOptions{}, "evolve evaluate": evolvesvc.EvaluateOptions{}, "evolve apply": evolvesvc.ApplyOptions{},
	"evolve reject": evolvesvc.RejectOptions{}, "evolve rollback": evolvesvc.RollbackOptions{}, "evolve history": evolvesvc.HistoryOptions{},
	"evolve queue": evolvesvc.QueueOptions{}, "evolve process": evolvesvc.ProcessOptions{}, "evolve cycle": evolvesvc.CycleOptions{},
}

// leaf là một lệnh lá (có thể chạy được) của cây cobra.
type leaf struct {
	path  string
	flags map[string]bool
}

// leaves duyệt cây lệnh, lấy mọi lệnh chạy được (không gồm gốc). Cờ help/version do cobra tự thêm nên bỏ.
func leaves(root *cobra.Command) map[string]leaf {
	out := map[string]leaf{}
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if c.Runnable() && len(path) > 0 {
			fl := map[string]bool{}
			c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name != "help" && f.Name != "version" {
					fl[f.Name] = true
				}
			})
			p := strings.Join(path, " ")
			out[p] = leaf{path: p, flags: fl}
		}
		for _, s := range c.Commands() {
			walk(s, append(append([]string(nil), path...), s.Name()))
		}
	}
	walk(root, nil)
	return out
}

// checkCoverage trả các vấn đề của phủ lệnh và phủ cờ (rỗng = đạt). phase là giai đoạn đã làm xong.
func checkCoverage(root *cobra.Command, cov actions.Coverage, reg *actions.Registry, bindings map[string]any, phase int) []string {
	var problems []string
	bad := func(f string, a ...any) { problems = append(problems, fmt.Sprintf(f, a...)) }
	ls := leaves(root)
	hasAction := map[string]actions.Action{}
	for _, a := range reg.All() {
		if a.CLIPath != "" {
			hasAction[a.CLIPath] = a
		}
	}
	paths := slices.Sorted(maps.Keys(ls))

	// Phủ lệnh: mỗi lệnh lá thuộc đúng một nhóm.
	for _, p := range paths {
		_, ex := cov.Excluded[p]
		_, pe := cov.Pending[p]
		_, ac := hasAction[p]
		n := 0
		for _, b := range []bool{ex, pe, ac} {
			if b {
				n++
			}
		}
		switch {
		case n == 0:
			bad("lệnh `bint %s` chưa khai báo: thêm hành động vào sổ, hoặc vào Excluded (kèm lý do), hoặc vào Pending (kèm giai đoạn)", p)
		case n > 1:
			bad("lệnh `bint %s` nằm trong nhiều nhóm (hành động=%v, Excluded=%v, Pending=%v)", p, ac, ex, pe)
		}
		if ex && cov.Excluded[p] == "" {
			bad("Excluded[%q] thiếu lý do", p)
		}
	}
	// Chiều ngược: mục khai báo phải trỏ tới lệnh có thật.
	for p := range cov.Excluded {
		if _, ok := ls[p]; !ok {
			bad("Excluded có `%s` nhưng CLI không còn lệnh này", p)
		}
	}
	for p, ph := range cov.Pending {
		if _, ok := ls[p]; !ok {
			bad("Pending có `%s` nhưng CLI không còn lệnh này", p)
		}
		if ph < 1 || ph > 6 {
			bad("Pending[%q] = %d: giai đoạn phải từ 1 đến 6", p, ph)
		}
	}
	for p := range hasAction {
		if _, ok := ls[p]; !ok {
			bad("hành động trong sổ trỏ tới lệnh `%s` không tồn tại", p)
		}
	}
	for p := range bindings {
		if _, ok := ls[p]; !ok {
			bad("serviceBindings có `%s` nhưng CLI không còn lệnh này", p)
		}
	}

	// Phủ cờ: lệnh đã có hành động hoặc dịch vụ.
	split := func(key string) (string, string, bool) {
		i := strings.LastIndex(key, " --")
		if i < 0 {
			return "", "", false
		}
		return key[:i], key[i+3:], true
	}
	covered := func(p string) bool {
		_, ac := hasAction[p]
		_, bd := bindings[p]
		return ac || bd
	}
	for _, p := range paths {
		if !covered(p) {
			continue
		}
		opt := map[string]bool{}
		if a, ok := hasAction[p]; ok {
			for _, f := range a.Flags() {
				opt[f] = true
			}
		}
		if b, ok := bindings[p]; ok {
			for _, f := range actions.FlagsOf(b) {
				opt[f] = true
			}
		}
		for _, f := range slices.Sorted(maps.Keys(ls[p].flags)) {
			_, pf := cov.PendingFlags[p+" --"+f]
			_, ef := cov.ExcludedCmdFlags[p+" --"+f]
			_, eg := cov.ExcludedFlags["--"+f]
			if !opt[f] && !pf && !ef && !eg {
				bad("cờ --%s của `bint %s` chưa có trường Options (thẻ `flag:\"%s\"`), mục loại trừ có lý do, hay PendingFlags", f, p, f)
			}
			if opt[f] && (pf || ef) {
				bad("cờ --%s của `bint %s` vừa có trường Options vừa nằm trong danh sách loại trừ/chưa làm", f, p)
			}
		}
		for f := range opt {
			if !ls[p].flags[f] {
				bad("Options của `bint %s` có thẻ flag %q nhưng lệnh không có cờ này", p, f)
			}
		}
	}
	for key := range cov.PendingFlags {
		p, f, ok := split(key)
		switch {
		case !ok:
			bad("PendingFlags[%q] sai khuôn dạng (cần \"đường lệnh --cờ\")", key)
		case ls[p].path == "":
			bad("PendingFlags có `%s` nhưng CLI không còn lệnh này", key)
		case !ls[p].flags[f]:
			bad("PendingFlags có %q nhưng lệnh không còn cờ này", key)
		case !covered(p):
			bad("PendingFlags[%q]: lệnh chưa có hành động hoặc dịch vụ nên cờ nằm trong Pending của lệnh", key)
		}
	}
	for key, reason := range cov.ExcludedCmdFlags {
		p, f, ok := split(key)
		switch {
		case !ok || ls[p].path == "" || !ls[p].flags[f]:
			bad("ExcludedCmdFlags có %q nhưng lệnh hoặc cờ không còn", key)
		case reason == "":
			bad("ExcludedCmdFlags[%q] thiếu lý do", key)
		}
	}
	for f, reason := range cov.ExcludedFlags {
		if reason == "" {
			bad("ExcludedFlags[%q] thiếu lý do", f)
		}
	}

	// Giai đoạn đã xong thì không còn mục chưa làm của giai đoạn đó (hay trước đó).
	for p, ph := range cov.Pending {
		if ph <= phase {
			bad("Pending[%q] = %d nhưng giai đoạn %d đã xong: lệnh phải có hành động hoặc nằm trong Excluded", p, ph, phase)
		}
	}
	for key, ph := range cov.PendingFlags {
		if ph <= phase {
			bad("PendingFlags[%q] = %d nhưng giai đoạn %d đã xong: cờ phải có trường Options hoặc nằm trong ExcludedCmdFlags", key, ph, phase)
		}
	}

	// Danh sách chưa làm chỉ được thu nhỏ; giai đoạn 6 đòi nó rỗng.
	total := len(cov.Pending) + len(cov.PendingFlags)
	if total > cov.PendingCeiling {
		bad("danh sách chưa làm có %d mục, vượt trần %d (chỉ được thu nhỏ)", total, cov.PendingCeiling)
	}
	if phase >= 6 && total > 0 {
		bad("giai đoạn 6 đòi danh sách chưa làm rỗng, còn %d mục", total)
	}
	sort.Strings(problems)
	return problems
}

func TestCommandAndFlagCoverage(t *testing.T) {
	if p := checkCoverage(NewRoot(IO{}), actions.Data, actions.Default(), serviceBindings, actions.CurrentPhase); len(p) > 0 {
		t.Fatalf("phủ lệnh/cờ chưa đạt:\n  %s", strings.Join(p, "\n  "))
	}
}

func clone(c actions.Coverage) actions.Coverage {
	c.Excluded, c.ExcludedFlags = maps.Clone(c.Excluded), maps.Clone(c.ExcludedFlags)
	c.ExcludedCmdFlags, c.Pending, c.PendingFlags = maps.Clone(c.ExcludedCmdFlags), maps.Clone(c.Pending), maps.Clone(c.PendingFlags)
	return c
}

func mentions(problems []string, sub string) bool {
	for _, p := range problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

// Ca ngược: thêm lệnh hoặc cờ CLI mà không khai báo thì kiểm tra phải đỏ; mục khai báo thừa cũng đỏ.
func TestCoverageCheckerCatchesGaps(t *testing.T) {
	reg := actions.Default()
	check := func(root *cobra.Command, cov actions.Coverage, b map[string]any, phase int) []string {
		return checkCoverage(root, cov, reg, b, phase)
	}

	t.Run("lệnh mới không khai báo", func(t *testing.T) {
		root := NewRoot(IO{})
		root.AddCommand(&cobra.Command{Use: "zebra", RunE: func(*cobra.Command, []string) error { return nil }})
		if p := check(root, actions.Data, serviceBindings, 0); !mentions(p, "`bint zebra` chưa khai báo") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("lệnh con mới của nhóm có sẵn", func(t *testing.T) {
		root := NewRoot(IO{})
		for _, c := range root.Commands() {
			if c.Name() == "knowledge" {
				c.AddCommand(&cobra.Command{Use: "export", RunE: func(*cobra.Command, []string) error { return nil }})
			}
		}
		if p := check(root, actions.Data, serviceBindings, 0); !mentions(p, "`bint knowledge export` chưa khai báo") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("cờ mới của lệnh đã có dịch vụ", func(t *testing.T) {
		root := NewRoot(IO{})
		for _, c := range root.Commands() {
			if c.Name() == "eval" {
				for _, s := range c.Commands() {
					if s.Name() == "run" {
						s.Flags().Bool("turbo", false, "cờ mới quên form")
					}
				}
			}
		}
		if p := check(root, actions.Data, serviceBindings, 0); !mentions(p, "cờ --turbo của `bint eval run`") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("cờ mới của lệnh đã có hành động (init)", func(t *testing.T) {
		root := NewRoot(IO{})
		for _, c := range root.Commands() {
			if c.Name() == "init" {
				c.Flags().Bool("turbo", false, "x")
			}
		}
		if p := check(root, actions.Data, serviceBindings, 0); !mentions(p, "cờ --turbo của `bint init`") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("lệnh biến mất khỏi CLI mà còn khai báo", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.Pending["ghost cmd"] = 3
		cov.Excluded["ghost two"] = "lý do"
		b := maps.Clone(serviceBindings)
		b["ghost three"] = app.EvalCleanOptions{}
		p := check(NewRoot(IO{}), cov, b, 0)
		for _, want := range []string{"Pending có `ghost cmd`", "Excluded có `ghost two`", "serviceBindings có `ghost three`"} {
			if !mentions(p, want) {
				t.Errorf("thiếu %q trong %v", want, p)
			}
		}
	})
	t.Run("lệnh nằm trong hai nhóm", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.Excluded["knowledge add"] = "lý do"
		if p := check(NewRoot(IO{}), cov, serviceBindings, 1); !mentions(p, "`bint knowledge add` nằm trong nhiều nhóm") {
			t.Fatalf("%v", p)
		}
		cov = clone(actions.Data)
		cov.Pending["status"] = 1 // status đã có hành động
		if p := check(NewRoot(IO{}), cov, serviceBindings, 0); !mentions(p, "`bint status` nằm trong nhiều nhóm") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("thiếu lý do và giai đoạn sai", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.Excluded["tui"] = ""
		cov.Pending["sync"] = 9
		cov.ExcludedFlags["--json"] = ""
		p := check(NewRoot(IO{}), cov, serviceBindings, 0)
		for _, want := range []string{`Excluded["tui"] thiếu lý do`, "giai đoạn phải từ 1 đến 6", `ExcludedFlags["--json"] thiếu lý do`} {
			if !mentions(p, want) {
				t.Errorf("thiếu %q trong %v", want, p)
			}
		}
	})
	t.Run("thẻ flag trỏ cờ không tồn tại", func(t *testing.T) {
		type fake struct {
			X string `flag:"does-not-exist"`
		}
		b := maps.Clone(serviceBindings)
		b["eval clean"] = fake{}
		if p := check(NewRoot(IO{}), actions.Data, b, 0); !mentions(p, `thẻ flag "does-not-exist"`) {
			t.Fatalf("%v", p)
		}
	})
	t.Run("PendingFlags sai", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.PendingFlags["init --nope"] = 5
		cov.PendingFlags["sync --force"] = 2      // lệnh chưa có hành động/dịch vụ (giai đoạn 5)
		cov.PendingFlags["eval run --budget"] = 4 // cờ đã có trường Options
		p := check(NewRoot(IO{}), cov, serviceBindings, 0)
		for _, want := range []string{`"init --nope" nhưng lệnh không còn cờ này`, "cờ nằm trong Pending của lệnh", "vừa có trường Options vừa nằm trong danh sách"} {
			if !mentions(p, want) {
				t.Errorf("thiếu %q trong %v", want, p)
			}
		}
	})
	t.Run("trần chưa làm và giai đoạn 6", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.PendingCeiling = len(cov.Pending) + len(cov.PendingFlags) - 1
		if p := check(NewRoot(IO{}), cov, serviceBindings, 0); !mentions(p, "vượt trần") {
			t.Fatalf("%v", p)
		}
		if p := check(NewRoot(IO{}), actions.Data, serviceBindings, 6); !mentions(p, "giai đoạn 6 đòi danh sách chưa làm rỗng") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("Pending còn mục của giai đoạn đã xong", func(t *testing.T) {
		cov := clone(actions.Data)
		cov.Pending["sync"] = actions.CurrentPhase // lệnh của giai đoạn 5 bị đánh dấu xong sớm
		if p := check(NewRoot(IO{}), cov, serviceBindings, actions.CurrentPhase); !mentions(p, `Pending["sync"]`) {
			t.Fatalf("%v", p)
		}
		cov = clone(actions.Data)
		cov.PendingFlags["init --force"] = actions.CurrentPhase
		if p := check(NewRoot(IO{}), cov, serviceBindings, actions.CurrentPhase); !mentions(p, `PendingFlags["init --force"]`) {
			t.Fatalf("%v", p)
		}
	})
	t.Run("bỏ thẻ flag của một trường AddOptions", func(t *testing.T) {
		type addNoTags struct {
			Type string `flag:"type"`
		}
		var list []actions.Action
		for _, a := range reg.All() {
			if a.ID == "knowledge.add" || a.ID == "tasks.add_knowledge" { // hai hành động cùng lệnh `knowledge add`
				a.Options = addNoTags{}
			}
			list = append(list, a)
		}
		p := checkCoverage(NewRoot(IO{}), actions.Data, actions.NewRegistry(list), serviceBindings, actions.CurrentPhase)
		if !mentions(p, "cờ --paths của `bint knowledge add`") || !mentions(p, "cờ --evidence của `bint knowledge add`") {
			t.Fatalf("%v", p)
		}
	})
	t.Run("bỏ thẻ flag của một trường Options của run", func(t *testing.T) {
		// Ca ngược giai đoạn 2: bỏ thẻ `flag` của trường Harness thì cờ --harness của `bint run` không còn ánh xạ.
		type runNoHarness struct {
			Model string `flag:"model"`
		}
		var list []actions.Action
		for _, a := range reg.All() {
			if a.ID == "tasks.run" {
				a.Options = runNoHarness{}
			}
			list = append(list, a)
		}
		b := maps.Clone(serviceBindings)
		delete(b, "run")
		p := checkCoverage(NewRoot(IO{}), actions.Data, actions.NewRegistry(list), b, actions.CurrentPhase)
		if !mentions(p, "cờ --harness của `bint run`") || !mentions(p, "cờ --no-review của `bint run`") {
			t.Fatalf("%v", p)
		}
		// Có binding đủ thì xanh trở lại.
		if p := checkCoverage(NewRoot(IO{}), actions.Data, reg, serviceBindings, actions.CurrentPhase); len(p) != 0 {
			t.Fatalf("%v", p)
		}
	})
	t.Run("xoá mục khỏi Pending làm đỏ", func(t *testing.T) {
		cov := clone(actions.Data)
		delete(cov.Pending, "sync")
		if p := check(NewRoot(IO{}), cov, serviceBindings, 0); !mentions(p, "`bint sync` chưa khai báo") {
			t.Fatalf("%v", p)
		}
	})
}

// Mọi lệnh đã tách dịch vụ có đủ ba thành phần: Options + Validate + Defaults (kiểm bằng phản xạ ở
// các test dịch vụ); ở đây chỉ bảo đảm bảng ghép phủ đủ eval và evolve.
func TestBindingsCoverEvalAndEvolve(t *testing.T) {
	ls := leaves(NewRoot(IO{}))
	for p := range ls {
		if strings.HasPrefix(p, "eval ") || strings.HasPrefix(p, "evolve ") {
			if _, ok := serviceBindings[p]; !ok {
				t.Errorf("`bint %s` chưa có Options trong serviceBindings", p)
			}
		}
	}
	if _, ok := serviceBindings["knowledge review"]; !ok {
		t.Error("knowledge review")
	}
}
