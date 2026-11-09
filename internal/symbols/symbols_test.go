package symbols

import "testing"

func TestLocateGo(t *testing.T) {
	src := []byte(`package x

// Open mở kết nối.
func Open() error {
	return nil
}

type DB struct {
	Path string
	n    int
}

func (d *DB) Close() error {
	return nil
}

const (
	A = 1
	B = 2
)
`)
	cases := map[string][2]int{
		"Open": {4, 6}, "DB": {8, 11}, "DB.Close": {13, 15}, "(*DB).Close": {13, 15}, "DB.Path": {9, 9}, "B": {19, 19},
	}
	for sym, want := range cases {
		r, ok := Locate("a/x.go", src, sym)
		if !ok || r.From != want[0] || r.To != want[1] || !r.Exact {
			t.Errorf("%s: %+v ok=%v, muốn %v", sym, r, ok, want)
		}
	}
	if _, ok := Locate("x.go", src, "Close"); ok {
		t.Error("Close không có receiver phải không khớp phương thức")
	}
	if _, ok := Locate("x.go", src, "Missing"); ok {
		t.Error("ký hiệu không tồn tại")
	}
}

func TestLocateBraceAndIndent(t *testing.T) {
	ts := []byte(`import x from "y";

export function handle(req: Req): Res {
  if (req.a) { return "}"; }
  return ok;
}

export const limit = 10;

class Svc {
  run(a) {
    return a;
  }
}
`)
	for sym, want := range map[string][2]int{"handle": {3, 6}, "limit": {8, 8}, "Svc": {10, 14}, "Svc.run": {11, 13}} {
		r, ok := Locate("src/a.ts", ts, sym)
		if !ok || r.From != want[0] || r.To != want[1] {
			t.Errorf("ts %s: %+v ok=%v, muốn %v", sym, r, ok, want)
		}
	}
	py := []byte("import os\n\nclass A:\n    def f(self):\n        return 1\n\n    def g(self):\n        pass\n\ndef top():\n    pass\n")
	for sym, want := range map[string][2]int{"A": {3, 8}, "A.f": {4, 5}, "top": {10, 11}} {
		r, ok := Locate("m.py", py, sym)
		if !ok || r.From != want[0] || r.To != want[1] {
			t.Errorf("py %s: %+v ok=%v, muốn %v", sym, r, ok, want)
		}
	}
	yml := []byte("jobs:\n  test:\n    runs-on: x\n    steps: []\n  lint:\n    runs-on: y\n")
	if r, ok := Locate("ci.yml", yml, "test"); !ok || r.From != 2 || r.To != 4 {
		t.Errorf("yaml: %+v %v", r, ok)
	}
	if _, ok := Locate("a.txt", yml, "test"); ok {
		t.Error("đuôi không hỗ trợ")
	}
}
