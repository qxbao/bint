package hookcore

import "testing"

func TestIsConfirmedTest(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name string
		tc   *string
		cmd  string
		want bool
	}{
		{"chưa cấu hình", nil, "go test ./...", false},
		{"cấu hình rỗng", s(" "), "go test ./...", false},
		{"không placeholder", s("go test ./..."), "cd x && go test ./...", true},
		{"placeholder thật", s("uv run pytest -q {test_files}"), "uv run pytest -q tests/test_a.py", true},
		{"placeholder bọc timeout", s("uv run pytest -q {test_files}"), "cd a && timeout 600 uv run pytest -q t/x.py t/y.py", true},
		{"echo", s("uv run pytest -q {test_files}"), `echo "uv run pytest -q t.py"`, false},
		{"thiếu đối số", s("uv run pytest -q {test_files}"), "uv run pytest -q", false},
	}
	for _, c := range cases {
		if got := isConfirmedTest(c.tc, c.cmd); got != c.want {
			t.Errorf("%s: got %v, muốn %v", c.name, got, c.want)
		}
	}
}
