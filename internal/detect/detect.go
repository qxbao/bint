// Package detect chứa các phát hiện xác định được bằng chương trình: harness đã
// cài, lệnh kiểm thử / dựng môi trường đề xuất, và nhận diện lệnh kiểm thử.
// Không chạy bất kỳ lệnh nào của dự án.
package detect

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Harness mô tả một harness được phát hiện trên máy.
type Harness struct {
	Name    string
	Binary  string // đường dẫn chương trình, rỗng nếu không thấy trong PATH
	Home    string // thư mục cấu hình người dùng, rỗng nếu không có
	Present bool
}

var harnessSpecs = []struct {
	name  string
	bins  []string
	homes []string
}{
	{"claude", []string{"claude"}, []string{".claude"}},
	{"codex", []string{"codex"}, []string{".codex"}},
	// Antigravity CLI cài chương trình `agy`; cấu hình nằm trong ~/.gemini.
	{"antigravity", []string{"agy", "antigravity"}, []string{".gemini/antigravity-cli", ".gemini/antigravity", ".antigravity"}},
}

// Harnesses phát hiện các harness Bint biết.
func Harnesses() []Harness {
	home, _ := os.UserHomeDir()
	var out []Harness
	for _, s := range harnessSpecs {
		h := Harness{Name: s.name}
		for _, b := range s.bins {
			if p, err := exec.LookPath(b); err == nil {
				h.Binary = p
				break
			}
		}
		if home != "" {
			for _, d := range s.homes {
				if fi, err := os.Stat(filepath.Join(home, filepath.FromSlash(d))); err == nil && fi.IsDir() {
					h.Home = filepath.Join(home, filepath.FromSlash(d))
					break
				}
			}
		}
		h.Present = h.Binary != "" || h.Home != ""
		out = append(out, h)
	}
	return out
}

// Find trả về kết quả phát hiện của một harness.
func Find(name string) Harness {
	for _, h := range Harnesses() {
		if h.Name == name {
			return h
		}
	}
	return Harness{Name: name}
}

// Commands là lệnh kiểm thử và dựng môi trường đề xuất.
type Commands struct {
	Test   string
	Setup  string
	Reason string // tệp làm căn cứ
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

// SuggestCommands đoán lệnh kiểm thử và dựng môi trường từ manifest ở thư mục gốc.
// Kết quả chỉ là đề xuất; người dùng phải xác nhận trước khi Bint dùng.
func SuggestCommands(root string) Commands {
	switch {
	case exists(root, "go.mod"):
		return Commands{Test: "go test ./...", Setup: "go mod download", Reason: "go.mod"}
	case exists(root, "package.json"):
		pm := "npm"
		switch {
		case exists(root, "pnpm-lock.yaml"):
			pm = "pnpm"
		case exists(root, "yarn.lock"):
			pm = "yarn"
		case exists(root, "bun.lockb"), exists(root, "bun.lock"):
			pm = "bun"
		}
		setup := pm + " install"
		if pm == "npm" && exists(root, "package-lock.json") {
			setup = "npm ci"
		}
		c := Commands{Setup: setup, Reason: "package.json"}
		if hasNpmScript(filepath.Join(root, "package.json"), "test") {
			if pm == "bun" {
				c.Test = "bun run test"
			} else {
				c.Test = pm + " test"
			}
		}
		return c
	case exists(root, "Cargo.toml"):
		return Commands{Test: "cargo test", Setup: "cargo fetch", Reason: "Cargo.toml"}
	case exists(root, "pyproject.toml"), exists(root, "pytest.ini"), exists(root, "setup.py"):
		c := Commands{Test: "pytest", Reason: "pyproject.toml/pytest.ini/setup.py"}
		if exists(root, "uv.lock") {
			c.Test, c.Setup = "uv run pytest", "uv sync"
		} else if exists(root, "poetry.lock") {
			c.Test, c.Setup = "poetry run pytest", "poetry install"
		}
		return c
	case exists(root, "pom.xml"):
		return Commands{Test: "mvn -q test", Reason: "pom.xml"}
	case exists(root, "gradlew"):
		return Commands{Test: "./gradlew test", Reason: "gradlew"}
	case exists(root, "Makefile") && makefileHasTarget(filepath.Join(root, "Makefile"), "test"):
		return Commands{Test: "make test", Reason: "Makefile"}
	}
	return Commands{}
}

func hasNpmScript(path, name string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return false
	}
	s, ok := pkg.Scripts[name]
	// Mẫu mặc định của npm init không phải kiểm thử thật.
	return ok && !strings.Contains(s, "no test specified")
}

func makefileHasTarget(path, target string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `\s*:`)
	return re.Match(data)
}

var testRunner = regexp.MustCompile(`(?i)(^|[\s;&|(])(` + strings.Join([]string{
	`go\s+test`,
	`(npm|pnpm|yarn|bun)\s+(run\s+)?test(:\S+)?`,
	`bun\s+test`,
	`npx\s+(jest|vitest|mocha|playwright\s+test)`,
	`(jest|vitest|mocha)(\s|$)`,
	`(python3?\s+-m\s+)?pytest`,
	`(uv|poetry)\s+run\s+pytest`,
	`cargo\s+(test|nextest)`,
	`mvnw?\s+(\S+\s+)*test`,
	`(\./)?gradlew?\s+(\S+\s+)*test`,
	`dotnet\s+test`,
	`(bundle\s+exec\s+)?rspec`,
	`phpunit`,
	`mix\s+test`,
	`make\s+test`,
	`ctest`,
}, "|") + `)(\s|$|[;&|)])`)

// IsTestCommand cho biết lệnh shell có phải lệnh chạy kiểm thử không. Lệnh đã
// được người dùng cấu hình (configured) luôn được nhận diện theo tiền tố.
func IsTestCommand(cmd string, configured ...string) bool {
	c := strings.TrimSpace(cmd)
	if c == "" {
		return false
	}
	for _, k := range configured {
		k = strings.TrimSpace(k)
		if k != "" && strings.Contains(c, k) {
			return true
		}
	}
	return testRunner.MatchString(c)
}

var (
	envAssign   = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)=("[^"]*"|'[^']*'|\S+)`)
	secretFlags = regexp.MustCompile(`(?i)(--?(token|password|passwd|secret|api[-_]?key|auth)[= ])("[^"]*"|'[^']*'|\S+)`)
	bearer      = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	urlCreds    = regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`)
)

// Redact che các giá trị có thể là bí mật trong một lệnh trước khi ghi lại, và
// cắt ngắn tới maxLen ký tự.
func Redact(cmd string, maxLen int) string {
	s := envAssign.ReplaceAllString(cmd, "$1=***")
	s = secretFlags.ReplaceAllString(s, "$1***")
	s = bearer.ReplaceAllString(s, "$1***")
	s = urlCreds.ReplaceAllString(s, "$1***@")
	if maxLen > 0 && len([]rune(s)) > maxLen {
		s = string([]rune(s)[:maxLen]) + "…"
	}
	return s
}
