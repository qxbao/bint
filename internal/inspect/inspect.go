// Package inspect là bước Static Discovery của bootstrap (mục 4.2 bước 1): thu
// thập dữ liệu xác định được bằng chương trình về repository — ngôn ngữ, manifest,
// dependency, công cụ, lệnh, tài liệu sẵn có, mã sinh tự động, và lịch sử Git
// (điểm nóng, commit sửa lỗi/hoàn tác) làm manh mối cho "điểm dễ sai".
// Không chạy lệnh nào của dự án, không gọi mô hình.
package inspect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"bint/internal/detect"
)

// Report là kết quả static discovery.
type Report struct {
	Root        string         `json:"root"`
	GeneratedAt string         `json:"generated_at"`
	Head        string         `json:"head,omitempty"`
	Files       int            `json:"files"`
	Languages   []Language     `json:"languages"`
	TopDirs     []DirCount     `json:"top_dirs"`
	Manifests   []Manifest     `json:"manifests"`
	Tools       []Tool         `json:"tools"`
	Commands    []Command      `json:"commands"`
	Docs        []Doc          `json:"docs"`
	Generated   []GeneratedDir `json:"generated,omitempty"`
	Git         GitInfo        `json:"git"`
	Notes       []string       `json:"notes,omitempty"`
}

// Language là thống kê theo ngôn ngữ.
type Language struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// DirCount là số tệp trong một thư mục cấp cao nhất.
type DirCount struct {
	Dir   string `json:"dir"`
	Files int    `json:"files"`
}

// Manifest là tệp khai báo dự án/dependency.
type Manifest struct {
	Path         string   `json:"path"`
	Kind         string   `json:"kind"`
	Lockfile     string   `json:"lockfile,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	DevDeps      []string `json:"dev_dependencies,omitempty"`
	Scripts      []string `json:"scripts,omitempty"`
}

// Tool là công cụ hoặc cấu hình được phát hiện.
type Tool struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"` // build | test | lint | format | ci | container | codegen | env | framework
}

// Command là lệnh tìm thấy (chỉ để agent đọc; Bint không chạy).
type Command struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source"`
}

// Doc là tài liệu sẵn có, gồm tệp ngữ cảnh agent của các harness khác.
type Doc struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"` // readme | contributing | agents | adr | docs | changelog | security
	Bytes int64  `json:"bytes"`
}

// GeneratedDir gom các tệp có dấu hiệu mã sinh tự động ("DO NOT EDIT").
type GeneratedDir struct {
	Dir     string   `json:"dir"`
	Files   int      `json:"files"`
	Samples []string `json:"samples"`
	Marker  string   `json:"marker"`
}

// GitInfo là siêu dữ liệu Git và manh mối điểm dễ sai.
type GitInfo struct {
	Commits      int        `json:"commits"`
	FirstCommit  string     `json:"first_commit,omitempty"`
	LastCommit   string     `json:"last_commit,omitempty"`
	Contributors int        `json:"contributors"`
	Hotspots     []FileStat `json:"hotspots,omitempty"`
	// FixHotspots: tệp xuất hiện nhiều trong commit sửa lỗi/hoàn tác.
	FixHotspots []FileStat  `json:"fix_hotspots,omitempty"`
	Reverts     []CommitRef `json:"reverts,omitempty"`
	FixCommits  []CommitRef `json:"fix_commits,omitempty"`
	Scanned     int         `json:"scanned_commits"`
}

// FileStat là số commit chạm vào một tệp.
type FileStat struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
}

// CommitRef là một commit được trích dẫn.
type CommitRef struct {
	SHA     string   `json:"sha"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	Files   []string `json:"files,omitempty"`
}

// Options điều khiển Inspect.
type Options struct {
	// MaxCommits giới hạn số commit quét cho điểm nóng (mặc định 1000).
	MaxCommits int
	Now        func() time.Time
}

var extLang = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript", ".mts": "TypeScript", ".cts": "TypeScript",
	".js": "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".py": "Python", ".rs": "Rust", ".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin", ".scala": "Scala",
	".rb": "Ruby", ".php": "PHP", ".cs": "C#", ".fs": "F#", ".swift": "Swift", ".m": "Objective-C",
	".c": "C", ".h": "C/C++ header", ".cc": "C++", ".cpp": "C++", ".hpp": "C++", ".cxx": "C++",
	".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".clj": "Clojure", ".hs": "Haskell", ".ml": "OCaml",
	".dart": "Dart", ".lua": "Lua", ".r": "R", ".jl": "Julia", ".zig": "Zig", ".nim": "Nim",
	".sh": "Shell", ".bash": "Shell", ".ps1": "PowerShell", ".sql": "SQL", ".proto": "Protobuf",
	".vue": "Vue", ".svelte": "Svelte", ".css": "CSS", ".scss": "SCSS", ".html": "HTML",
	".tf": "Terraform", ".graphql": "GraphQL", ".gql": "GraphQL",
}

// Inspect thu thập báo cáo static discovery cho repository có thư mục gốc root.
func Inspect(ctx context.Context, root string, opts Options) (*Report, error) {
	if opts.MaxCommits <= 0 {
		opts.MaxCommits = 1000
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	rep := &Report{Root: root, GeneratedAt: opts.Now().UTC().Format(time.RFC3339)}
	out, err := git(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	rep.Files = len(files)
	if h, err := git(ctx, root, "rev-parse", "HEAD"); err == nil {
		rep.Head = strings.TrimSpace(string(h))
	}

	langs := map[string]*Language{}
	dirs := map[string]int{}
	for _, f := range files {
		if i := strings.IndexByte(f, '/'); i > 0 {
			dirs[f[:i]]++
		} else {
			dirs["."]++
		}
		ext := strings.ToLower(path.Ext(f))
		name, ok := extLang[ext]
		if !ok || isVendored(f) {
			continue
		}
		l := langs[name]
		if l == nil {
			l = &Language{Name: name}
			langs[name] = l
		}
		l.Files++
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			l.Bytes += fi.Size()
		}
	}
	for _, l := range langs {
		rep.Languages = append(rep.Languages, *l)
	}
	sort.Slice(rep.Languages, func(i, j int) bool {
		if rep.Languages[i].Bytes != rep.Languages[j].Bytes {
			return rep.Languages[i].Bytes > rep.Languages[j].Bytes
		}
		return rep.Languages[i].Name < rep.Languages[j].Name
	})
	for d, n := range dirs {
		rep.TopDirs = append(rep.TopDirs, DirCount{d, n})
	}
	sort.Slice(rep.TopDirs, func(i, j int) bool {
		if rep.TopDirs[i].Files != rep.TopDirs[j].Files {
			return rep.TopDirs[i].Files > rep.TopDirs[j].Files
		}
		return rep.TopDirs[i].Dir < rep.TopDirs[j].Dir
	})
	if len(rep.TopDirs) > 20 {
		rep.TopDirs = rep.TopDirs[:20]
	}

	set := map[string]bool{}
	for _, f := range files {
		set[f] = true
	}
	rep.Manifests = manifests(root, files, set)
	rep.Tools = tools(files)
	rep.Commands = commands(root, set, rep.Manifests)
	rep.Docs = docs(root, files)
	rep.Generated = generated(root, files)
	rep.Git = gitInfo(ctx, root, opts.MaxCommits)
	if rep.Files > 0 && len(rep.Languages) == 0 {
		rep.Notes = append(rep.Notes, "không nhận diện được ngôn ngữ lập trình nào từ phần mở rộng tệp")
	}
	return rep, nil
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func isVendored(f string) bool {
	for _, p := range []string{"vendor/", "node_modules/", "third_party/", "third-party/", ".venv/"} {
		if strings.HasPrefix(f, p) || strings.Contains(f, "/"+p) {
			return true
		}
	}
	return false
}

var lockfiles = map[string][]string{
	"go.mod":           {"go.sum"},
	"package.json":     {"package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock"},
	"Cargo.toml":       {"Cargo.lock"},
	"pyproject.toml":   {"uv.lock", "poetry.lock", "pdm.lock"},
	"Gemfile":          {"Gemfile.lock"},
	"composer.json":    {"composer.lock"},
	"pom.xml":          nil,
	"build.gradle":     nil,
	"build.gradle.kts": nil,
	"requirements.txt": nil,
	"setup.py":         nil,
	"mix.exs":          {"mix.lock"},
	"pubspec.yaml":     {"pubspec.lock"},
	"Package.swift":    {"Package.resolved"},
	"deno.json":        {"deno.lock"},
}

func manifests(root string, files []string, set map[string]bool) []Manifest {
	var out []Manifest
	for _, f := range files {
		if isVendored(f) || strings.Count(f, "/") > 3 {
			continue
		}
		base := path.Base(f)
		locks, ok := lockfiles[base]
		if !ok && !strings.HasSuffix(base, ".csproj") {
			continue
		}
		m := Manifest{Path: f, Kind: base}
		dir := path.Dir(f)
		for _, l := range locks {
			lp := l
			if dir != "." {
				lp = dir + "/" + l
			}
			if set[lp] {
				m.Lockfile = lp
				break
			}
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err == nil {
			switch base {
			case "go.mod":
				m.Dependencies = goModDeps(data)
			case "package.json":
				m.Dependencies, m.DevDeps, m.Scripts = packageJSON(data)
			case "Cargo.toml":
				m.Dependencies = tomlTableKeys(data, "dependencies")
				m.DevDeps = tomlTableKeys(data, "dev-dependencies")
			case "pyproject.toml":
				m.Dependencies = pyprojectDeps(data)
			case "requirements.txt":
				m.Dependencies = requirementsDeps(data)
			}
		}
		out = append(out, m)
		if len(out) >= 30 {
			break
		}
	}
	return out
}

func goModDeps(data []byte) []string {
	var deps []string
	inBlock := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(l, "require ("):
			inBlock = true
		case inBlock && l == ")":
			inBlock = false
		case inBlock && l != "" && !strings.HasPrefix(l, "//"):
			if !strings.Contains(l, "// indirect") {
				deps = append(deps, strings.Fields(l)[0])
			}
		case strings.HasPrefix(l, "require "):
			if f := strings.Fields(l); len(f) >= 2 && !strings.Contains(l, "// indirect") {
				deps = append(deps, f[1])
			}
		}
	}
	return deps
}

func packageJSON(data []byte) (deps, dev, scripts []string) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Scripts         map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil, nil, nil
	}
	return sortedKeys(pkg.Dependencies), sortedKeys(pkg.DevDependencies), sortedKeys(pkg.Scripts)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func tomlTableKeys(data []byte, table string) []string {
	var out []string
	in := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			in = l == "["+table+"]"
			continue
		}
		if in && l != "" && !strings.HasPrefix(l, "#") {
			if k, _, ok := strings.Cut(l, "="); ok {
				out = append(out, strings.Trim(strings.TrimSpace(k), `"`))
			}
		}
	}
	return out
}

var depNameRe = regexp.MustCompile(`^\s*"?([A-Za-z0-9_.\-\[\]]+)`)

func pyprojectDeps(data []byte) []string {
	var out []string
	in := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "dependencies") && strings.Contains(l, "[") {
			in = !strings.Contains(l, "]")
			continue
		}
		if in {
			if strings.HasPrefix(l, "]") {
				in = false
				continue
			}
			if m := depNameRe.FindStringSubmatch(l); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return append(out, tomlTableKeys(data, "tool.poetry.dependencies")...)
}

func requirementsDeps(data []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "-") {
			continue
		}
		if m := depNameRe.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

var toolSpecs = []struct {
	pattern, name, kind string
}{
	{"Makefile", "make", "build"}, {"justfile", "just", "build"}, {"Taskfile.yml", "task", "build"},
	{"Dockerfile", "docker", "container"}, {"docker-compose.yml", "docker compose", "container"},
	{"docker-compose.yaml", "docker compose", "container"}, {"compose.yaml", "docker compose", "container"},
	{".golangci.yml", "golangci-lint", "lint"}, {".golangci.yaml", "golangci-lint", "lint"},
	{".eslintrc", "eslint", "lint"}, {".eslintrc.js", "eslint", "lint"}, {".eslintrc.json", "eslint", "lint"},
	{".eslintrc.cjs", "eslint", "lint"}, {"eslint.config.js", "eslint", "lint"}, {"eslint.config.mjs", "eslint", "lint"},
	{"biome.json", "biome", "lint"}, {".prettierrc", "prettier", "format"}, {".prettierrc.json", "prettier", "format"},
	{"prettier.config.js", "prettier", "format"}, {".editorconfig", "editorconfig", "format"},
	{"ruff.toml", "ruff", "lint"}, {".flake8", "flake8", "lint"}, {"mypy.ini", "mypy", "lint"},
	{"rustfmt.toml", "rustfmt", "format"}, {"clippy.toml", "clippy", "lint"},
	{"tsconfig.json", "typescript", "build"}, {"vite.config.ts", "vite", "build"}, {"vite.config.js", "vite", "build"},
	{"next.config.js", "next.js", "framework"}, {"next.config.mjs", "next.js", "framework"}, {"next.config.ts", "next.js", "framework"},
	{"jest.config.js", "jest", "test"}, {"jest.config.ts", "jest", "test"}, {"vitest.config.ts", "vitest", "test"},
	{"playwright.config.ts", "playwright", "test"}, {"pytest.ini", "pytest", "test"}, {"tox.ini", "tox", "test"},
	{"conftest.py", "pytest", "test"}, {"buf.yaml", "buf", "codegen"}, {"buf.gen.yaml", "buf", "codegen"},
	{"sqlc.yaml", "sqlc", "codegen"}, {"sqlc.yml", "sqlc", "codegen"}, {".goreleaser.yml", "goreleaser", "build"},
	{".goreleaser.yaml", "goreleaser", "build"}, {".pre-commit-config.yaml", "pre-commit", "lint"},
	{".env.example", "dotenv", "env"}, {".tool-versions", "asdf", "env"}, {".nvmrc", "nvm", "env"},
	{"flake.nix", "nix", "env"}, {".devcontainer/devcontainer.json", "devcontainer", "env"},
	{".github/workflows/*", "github actions", "ci"}, {".gitlab-ci.yml", "gitlab ci", "ci"},
	{".circleci/config.yml", "circleci", "ci"}, {"Jenkinsfile", "jenkins", "ci"}, {"azure-pipelines.yml", "azure pipelines", "ci"},
}

func tools(files []string) []Tool {
	var out []Tool
	seen := map[string]bool{}
	for _, f := range files {
		if isVendored(f) {
			continue
		}
		for _, s := range toolSpecs {
			match := false
			if strings.Contains(s.pattern, "*") {
				ok, _ := path.Match(s.pattern, f)
				match = ok
			} else if strings.Contains(s.pattern, "/") {
				match = f == s.pattern
			} else {
				match = path.Base(f) == s.pattern && strings.Count(f, "/") <= 2
			}
			if match && !seen[s.name+"|"+f] {
				seen[s.name+"|"+f] = true
				out = append(out, Tool{Name: s.name, Path: f, Kind: s.kind})
			}
		}
		if len(out) >= 60 {
			break
		}
	}
	return out
}

var makeTargetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9][A-Za-z0-9_.\-/]*)\s*:([^=]|$)`)

func commands(root string, set map[string]bool, ms []Manifest) []Command {
	var out []Command
	if sug := detect.SuggestCommands(root); sug.Test != "" || sug.Setup != "" {
		if sug.Test != "" {
			out = append(out, Command{"test (đề xuất)", sug.Test, sug.Reason})
		}
		if sug.Setup != "" {
			out = append(out, Command{"setup (đề xuất)", sug.Setup, sug.Reason})
		}
	}
	for _, m := range ms {
		if m.Kind != "package.json" || len(m.Scripts) == 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.Path)))
		if err != nil {
			continue
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(data, &pkg) != nil {
			continue
		}
		for _, k := range m.Scripts {
			out = append(out, Command{"npm:" + k, pkg.Scripts[k], m.Path})
		}
	}
	for _, mk := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if !set[mk] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, mk))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, m := range makeTargetRe.FindAllStringSubmatch(string(data), -1) {
			t := m[1]
			if seen[t] || strings.HasPrefix(t, ".") || strings.Contains(t, "%") {
				continue
			}
			seen[t] = true
			out = append(out, Command{"make:" + t, "make " + t, mk})
		}
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func docs(root string, files []string) []Doc {
	var out []Doc
	for _, f := range files {
		if isVendored(f) {
			continue
		}
		base := strings.ToLower(path.Base(f))
		kind := ""
		switch {
		case strings.HasPrefix(base, "readme") && strings.Count(f, "/") <= 2:
			kind = "readme"
		case strings.HasPrefix(base, "contributing"):
			kind = "contributing"
		case base == "agents.md" || base == "claude.md" || base == "gemini.md" || base == ".cursorrules" ||
			base == ".windsurfrules" || base == "copilot-instructions.md" || strings.HasPrefix(f, ".cursor/rules/") ||
			strings.HasPrefix(f, ".claude/") && strings.HasSuffix(base, ".md"):
			kind = "agents"
		case strings.HasPrefix(base, "changelog"):
			kind = "changelog"
		case strings.HasPrefix(base, "security"):
			kind = "security"
		case strings.Contains(f, "/adr/") || strings.HasPrefix(f, "adr/") || strings.Contains(f, "/decisions/"):
			kind = "adr"
		case (strings.HasPrefix(f, "docs/") || strings.HasPrefix(f, "doc/")) && (strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".rst")):
			kind = "docs"
		}
		if kind == "" {
			continue
		}
		var size int64
		if fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
			size = fi.Size()
		}
		out = append(out, Doc{Path: f, Kind: kind, Bytes: size})
		if len(out) >= 60 {
			break
		}
	}
	return out
}

var generatedRe = regexp.MustCompile(`(?i)(code generated .* do not edit|@generated|auto-?generated|this file was generated|do not edit (this file|manually))`)

// generated đọc vài dòng đầu của tệp nguồn để tìm dấu hiệu mã sinh tự động — một
// quy tắc bất thường kinh điển ("không sửa tay, chạy lệnh sinh lại").
func generated(root string, files []string) []GeneratedDir {
	groups := map[string]*GeneratedDir{}
	scanned := 0
	for _, f := range files {
		if isVendored(f) {
			continue
		}
		if _, ok := extLang[strings.ToLower(path.Ext(f))]; !ok {
			continue
		}
		if scanned++; scanned > 5000 {
			break
		}
		fh, err := os.Open(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		buf := make([]byte, 1024)
		n, _ := fh.Read(buf)
		fh.Close()
		m := generatedRe.Find(buf[:n])
		if m == nil {
			continue
		}
		dir := path.Dir(f)
		g := groups[dir]
		if g == nil {
			g = &GeneratedDir{Dir: dir, Marker: string(m)}
			groups[dir] = g
		}
		g.Files++
		if len(g.Samples) < 3 {
			g.Samples = append(g.Samples, f)
		}
	}
	var out []GeneratedDir
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Dir < out[j].Dir
	})
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

var (
	fixRe    = regexp.MustCompile(`(?i)\b(fix(es|ed)?|bug|hotfix|regression|sửa|lỗi)\b`)
	revertRe = regexp.MustCompile(`(?i)^revert\b|\brevert(s|ed)?\b`)
)

func gitInfo(ctx context.Context, root string, max int) GitInfo {
	var gi GitInfo
	if out, err := git(ctx, root, "rev-list", "--count", "HEAD"); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &gi.Commits)
	}
	if gi.Commits == 0 {
		return gi
	}
	if out, err := git(ctx, root, "log", "--reverse", "--format=%cs", "--max-parents=0", "HEAD"); err == nil {
		if l := strings.Fields(string(out)); len(l) > 0 {
			gi.FirstCommit = l[0]
		}
	}
	if out, err := git(ctx, root, "log", "-1", "--format=%cs"); err == nil {
		gi.LastCommit = strings.TrimSpace(string(out))
	}
	if out, err := git(ctx, root, "shortlog", "-sne", "HEAD"); err == nil {
		gi.Contributors = len(strings.Split(strings.TrimSpace(string(out)), "\n"))
	}
	out, err := git(ctx, root, "log", "--no-merges", fmt.Sprintf("-n%d", max), "--name-only", "--format=%x1e%H%x1f%cs%x1f%s")
	if err != nil {
		return gi
	}
	touch, fixTouch := map[string]int{}, map[string]int{}
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		head, rest, _ := strings.Cut(rec, "\n")
		parts := strings.SplitN(head, "\x1f", 3)
		if len(parts) < 3 {
			continue
		}
		gi.Scanned++
		var fs []string
		for _, f := range strings.Split(rest, "\n") {
			if f = strings.TrimSpace(f); f != "" {
				fs = append(fs, f)
			}
		}
		isRevert, isFix := revertRe.MatchString(parts[2]), fixRe.MatchString(parts[2])
		// Commit chạm quá nhiều tệp (định dạng lại, đổi tên hàng loạt) không mang tín hiệu.
		if len(fs) > 30 {
			continue
		}
		for _, f := range fs {
			touch[f]++
			if isFix || isRevert {
				fixTouch[f]++
			}
		}
		ref := CommitRef{SHA: parts[0][:12], Date: parts[1], Subject: parts[2]}
		if len(fs) <= 5 {
			ref.Files = fs
		}
		if isRevert && len(gi.Reverts) < 15 {
			gi.Reverts = append(gi.Reverts, ref)
		} else if isFix && len(gi.FixCommits) < 25 {
			gi.FixCommits = append(gi.FixCommits, ref)
		}
	}
	gi.Hotspots = top(touch, 15, 3)
	gi.FixHotspots = top(fixTouch, 15, 2)
	return gi
}

func top(m map[string]int, n, min int) []FileStat {
	var out []FileStat
	for p, c := range m {
		if c >= min && !isVendored(p) && !isLock(p) {
			out = append(out, FileStat{p, c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Commits != out[j].Commits {
			return out[i].Commits > out[j].Commits
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func isLock(p string) bool {
	b := path.Base(p)
	for _, ls := range lockfiles {
		for _, l := range ls {
			if b == l {
				return true
			}
		}
	}
	return false
}
