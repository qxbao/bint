package detect

import "testing"

func TestMatchesTemplate(t *testing.T) {
	cases := []struct {
		name, cmd, tmpl string
		want            bool
	}{
		{"không placeholder, khớp đúng", "go test ./...", "go test ./...", true},
		{"không placeholder, đối số thừa", "go test ./... -run X", "go test ./...", true},
		{"placeholder một tệp", "uv run pytest -q tests/test_a.py", "uv run pytest -q {test_files}", true},
		{"placeholder nhiều tệp", "uv run pytest -q a_test.py b_test.py", "uv run pytest -q {test_files}", true},
		{"placeholder rỗng", "uv run pytest -q", "uv run pytest -q {test_files}", false},
		{"placeholder gặp cờ", "uv run pytest -q --lf", "uv run pytest -q {test_files}", false},
		{"đường dẫn có nháy", `uv run pytest -q 'tests/a b.py'`, "uv run pytest -q {test_files}", true},
		{"placeholder giữa lệnh", "pytest a.py b.py -x", "pytest {test_files} -x", true},
		{"bọc cd &&", "cd svc && uv run pytest -q t/test_x.py", "uv run pytest -q {test_files}", true},
		{"bọc timeout", "timeout 600 uv run pytest -q t.py", "uv run pytest -q {test_files}", true},
		{"bọc env", "FOO=1 uv run pytest -q t.py", "uv run pytest -q {test_files}", true},
		{"chuỗi con giữa từ", "xuv run pytest -q t.py", "uv run pytest -q {test_files}", false},
		{"khác token", "uv run pytests -q t.py", "uv run pytest -q {test_files}", false},
		{"echo bọc lệnh", `echo "uv run pytest -q t.py"`, "uv run pytest -q {test_files}", false},
		{"echo không placeholder", `echo "go test ./..."`, "go test ./...", false},
		{"sh -c không bóc", `sh -c "go test ./..."`, "go test ./...", false},
		{"sau dấu ;", "ls; go test ./...", "go test ./...", true},
		{"mẫu rỗng", "go test", "  ", false},
		{"placeholder test_dirs", "go test ./pkg/a ./pkg/b", "go test {test_dirs}", true},
	}
	for _, c := range cases {
		if got := MatchesTemplate(c.cmd, c.tmpl); got != c.want {
			t.Errorf("%s: MatchesTemplate(%q, %q) = %v, muốn %v", c.name, c.cmd, c.tmpl, got, c.want)
		}
	}
}

func TestIsBintInvocation(t *testing.T) {
	for cmd, want := range map[string]bool{
		"bint task write T plan -":                                    true,
		"/opt/x/bin/bint task review T - <<'EOF'\ngo test ./...\nEOF": true,
		"FOO=1 bint task error T \"go test ./... lỗi\"":               true,
		"C:/tools/bint.exe task show T":                               true,
		"go test ./...":                                               false,
		"cd x && bint task show T":                                    false,
		"echo bint":                                                   false,
		"./mybint test":                                               false,
		"":                                                            false,
	} {
		if got := IsBintInvocation(cmd); got != want {
			t.Errorf("%q: got %v, muốn %v", cmd, got, want)
		}
	}
}
