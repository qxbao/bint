package evolve

import (
	"strings"
	"testing"
)

func TestSiblingPatternNarrow(t *testing.T) {
	for in, want := range map[string]string{
		"handlers/user_handler.py": "handlers/*_handler.py",
		"pkg/a_test.go":            "pkg/*_test.go",
		"tests/test_x.py":          "tests/test_*.py",
		"pkg/plain.go":             "",
	} {
		if got := siblingPattern(in); got != want {
			t.Errorf("%s: %q != %q", in, got, want)
		}
	}
}

func TestUnrelatedSameDirFilesDoNotEnable(t *testing.T) {
	f := setup(t)
	res := f.scanRuns(t, []*Run{okRun("A", "svc/alpha.py"), okRun("B", "svc/beta.py"), okRun("C", "svc/gamma.py")})
	for k := range byKey(res) {
		if k == "exploration:svc" || k == "skill:svc" {
			t.Fatalf("không được bật: %s", k)
		}
	}
}

func TestPromptListsSiblingMembersAndObservations(t *testing.T) {
	f := setup(t)
	runs := []*Run{okRun("A", "handlers/user_handler.py"), okRun("B", "handlers/order_handler.py"), okRun("C", "handlers/cart_handler.py")}
	res := f.scanRuns(t, runs)
	h := &History{Runs: runs, ByID: map[string]*Run{}, Observations: []Observation{
		{RunID: "OTHER", Kind: "note", Title: "bẫy-xyz", Paths: []string{"handlers/cart_handler.py"}}}}
	for _, r := range runs {
		h.ByID[r.ID] = r
	}
	in := ProposeInput{K: f.k, Scan: res, History: h}
	ops := byKey(res)
	p := evolverPrompt(in, ops["exploration:handlers"], nil, nil, f.k.Source())
	for _, want := range []string{"handlers/*_handler.py", "handlers/user_handler.py", "handlers/order_handler.py", "bẫy-xyz"} {
		if !strings.Contains(p, want) {
			t.Errorf("evolverPrompt thiếu %q", want)
		}
	}
	sp := skillEvolverPrompt(in, ops["skill:handlers"], nil, nil)
	for _, want := range []string{"handlers/cart_handler.py", "bẫy-xyz"} {
		if !strings.Contains(sp, want) {
			t.Errorf("skillEvolverPrompt thiếu %q", want)
		}
	}
}
