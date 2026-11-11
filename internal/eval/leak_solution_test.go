package eval

import (
	"reflect"
	"testing"
)

func TestCodeOnly(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\ts.Reset()", "s.Reset()"},
		{"// retry when the server returns 503", ""},
		{"# a server comment", ""},
		{" * continued server comment", ""},
		{"x := Flush() // flush the server cache", "x := Flush() "},
		{`log.Printf("server timeout %d", n)`, "log.Printf( , n)"},
		{"q := `SELECT server FROM t`", "q :=  "},
		{`a := "x\"y server"; b()`, "a :=  ; b()"},
		{"f(/* the server */ Reset)", "f(  Reset)"},
		{"i--", "i--"},
	}
	for _, c := range cases {
		if got := codeOnly(c.in); got != c.want {
			t.Errorf("codeOnly(%q) = %q, muốn %q", c.in, got, c.want)
		}
	}
}

// Định danh mới chỉ tính khi nằm ở vị trí mã; từ thường trong chú thích/chuỗi thì không.
func TestSolutionNewIdentifiersOnlyCodePosition(t *testing.T) {
	in := LeakInput{
		AddedLines: []string{
			"\t// retry when the server returns 503",
			"\tc.Flush() // drain the cache",
			`	log.Printf("timeout on mutex %d", n)`,
			"\tc.Reset()",
		},
		NewIdents: map[string]bool{"server": true, "returns": true, "Flush": true, "cache": true, "timeout": true,
			"mutex": true, "Reset": true, "Printf": true, "drain": true},
		TestIdents: map[string]bool{"Printf": true},
	}
	want := []string{"Flush", "Reset"}
	if got := SolutionNewIdentifiers(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("SolutionNewIdentifiers = %v, muốn %v", got, want)
	}
	// Có trong kiểm thử thì bỏ.
	in.TestIdents["Reset"] = true
	if got := SolutionNewIdentifiers(in); !reflect.DeepEqual(got, []string{"Flush"}) {
		t.Fatalf("SolutionNewIdentifiers = %v, muốn [Flush]", got)
	}
}

func TestSolutionLiteralsRawBacktick(t *testing.T) {
	in := LeakInput{AddedLines: []string{
		"\trows, err := db.Query(`SELECT id FROM quota_ledger WHERE tenant = ?`, id)",
		`	return errors.New("quota exceeded for tenant")`,
	}}
	want := []string{"SELECT id FROM quota_ledger WHERE tenant = ?", "quota exceeded for tenant"}
	if got := SolutionLiterals(in); !reflect.DeepEqual(got, want) {
		t.Fatalf("SolutionLiterals = %v, muốn %v", got, want)
	}
}
