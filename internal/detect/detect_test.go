package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsTestCommand(t *testing.T) {
	yes := []string{
		"go test ./...", "cd x && go test -run Foo ./pkg", "npm test", "pnpm run test:unit",
		"yarn test", "npx vitest run", "pytest -q", "python -m pytest tests/", "uv run pytest",
		"cargo test", "./gradlew test", "mvn -q test", "dotnet test", "bundle exec rspec", "make test",
	}
	no := []string{"go build ./...", "git status", "npm install", "echo testing", "ls tests/", "cat pytest.ini"}
	for _, c := range yes {
		if !IsTestCommand(c) {
			t.Errorf("should match: %q", c)
		}
	}
	for _, c := range no {
		if IsTestCommand(c) {
			t.Errorf("should not match: %q", c)
		}
	}
	if !IsTestCommand("./scripts/check.sh --fast", "./scripts/check.sh") {
		t.Error("configured command not matched")
	}
}

func TestExecutedTestCommandDoesNotInventShortCircuitedRuns(t *testing.T) {
	for _, cmd := range []string{"true || go test ./...", "true # comment; go test ./...", "false && go test ./...", "if false; then go test ./...; fi", "printf 'go test ./...'", "go test ./...; false", "go test ./... &", "go test ./...;", "go test ./... | true"} {
		if IsExecutedTestCommand(cmd) {
			t.Errorf("script không chứng minh test chạy: %q", cmd)
		}
	}
	for _, cmd := range []string{"go test ./...", "GOCACHE=/tmp/cache go test ./...", "timeout 10 go test ./...", "npm test", "mvn -q test", "go test -run '^(A|B)$' ./..."} {
		if !IsExecutedTestCommand(cmd) {
			t.Errorf("mất test trực tiếp: %q", cmd)
		}
	}
}

func TestRedact(t *testing.T) {
	in := `API_KEY=abc123 DB_URL="postgres://u:pw@h/db" go test --token=xyz -H "Authorization: Bearer qq" ./...`
	out := Redact(in, 0)
	for _, secret := range []string{"abc123", "pw@", "xyz", "qq"} {
		if strings.Contains(out, secret) {
			t.Errorf("leaked %q in %q", secret, out)
		}
	}
	if !strings.Contains(out, "go test") {
		t.Errorf("command lost: %q", out)
	}
	if got := Redact(strings.Repeat("a", 50), 10); len([]rune(got)) != 11 {
		t.Errorf("truncate: %q", got)
	}
}

func TestSuggestCommands(t *testing.T) {
	cases := []struct {
		files map[string]string
		test  string
		setup string
	}{
		{map[string]string{"go.mod": "module x"}, "go test ./...", "go mod download"},
		{map[string]string{"package.json": `{"scripts":{"test":"vitest"}}`, "pnpm-lock.yaml": ""}, "pnpm test", "pnpm install"},
		{map[string]string{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`, "package-lock.json": ""}, "", "npm ci"},
		{map[string]string{"Cargo.toml": ""}, "cargo test", "cargo fetch"},
		{map[string]string{"pyproject.toml": "", "uv.lock": ""}, "uv run pytest", "uv sync"},
		{map[string]string{"Makefile": "build:\n\ttrue\ntest:\n\ttrue\n"}, "make test", ""},
		{map[string]string{"README.md": ""}, "", ""},
	}
	for _, c := range cases {
		d := t.TempDir()
		for n, body := range c.files {
			os.WriteFile(filepath.Join(d, n), []byte(body), 0o644)
		}
		got := SuggestCommands(d)
		if got.Test != c.test || got.Setup != c.setup {
			t.Errorf("%v: got %+v", c.files, got)
		}
		// Chỉ Go có nguồn ảnh hưởng (internal/affected) nên chỉ Go có lệnh affected đề xuất.
		wantAffected := ""
		if _, ok := c.files["go.mod"]; ok {
			wantAffected = "go test -count=1 -timeout=30m {test_dirs}"
		}
		if got.Affected != wantAffected {
			t.Errorf("%v: Affected = %q, muốn %q", c.files, got.Affected, wantAffected)
		}
	}
}

// Chạy trong profile `bint iso` (HOME giả): vẫn báo cấu hình ~/.claude, ~/.codex của máy thật (iso.RealHome), không phải của profile.
func TestHarnessesUseRealHomeInsideProfile(t *testing.T) {
	real, fake := t.TempDir(), t.TempDir()
	for _, d := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(real, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(fake, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(k, fake)
		t.Setenv("BINT_ISO_REAL_"+k, real)
	}
	t.Setenv("BINT_ISO_PROFILE", "codex/work")
	want := map[string]string{"claude": filepath.Join(real, ".claude"), "codex": filepath.Join(real, ".codex")}
	for _, h := range Harnesses() {
		if w, ok := want[h.Name]; ok && h.Home != w {
			t.Errorf("Harnesses()[%s].Home = %q, muốn %q", h.Name, h.Home, w)
		}
	}
	for name, w := range want {
		if got := Find(name).Home; got != w {
			t.Errorf("Find(%s).Home = %q, muốn %q", name, got, w)
		}
	}

	// Ngoài profile: HOME như trước.
	t.Setenv("BINT_ISO_PROFILE", "")
	if got := Find("claude").Home; got != filepath.Join(fake, ".claude") {
		t.Errorf("ngoài profile: Find(claude).Home = %q, muốn %q", got, filepath.Join(fake, ".claude"))
	}
}
