package evolve

import (
	"slices"
	"testing"
)

type fakeSrc []string

func (f fakeSrc) Read(string) ([]byte, bool) { return nil, false }
func (f fakeSrc) Files() []string            { return f }
func (f fakeSrc) Name() string               { return "fake" }

func TestPruneScopePaths(t *testing.T) {
	src := fakeSrc{"a/b.py", "a/c.py"}
	if got := pruneScopePaths([]string{"a/b.py", "a/tests/test_x.py", "a/**/*.py"}, src); !slices.Equal(got, []string{"a/b.py", "a/**/*.py"}) {
		t.Fatalf("%v", got)
	}
	if got := pruneScopePaths([]string{"z.py"}, src); !slices.Equal(got, []string{"z.py"}) {
		t.Fatalf("không còn gì thì giữ nguyên: %v", got)
	}
}
