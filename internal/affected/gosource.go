package affected

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// GoSource là nguồn ảnh hưởng cho module Go: `go list -e -test -json ./...` cho đồ thị phụ thuộc mức gói, chính xác
// hơn chỉ mục ký hiệu (tệp bị build tag loại như *_windows.go vẫn ra gói của nó). Chạy được trên mọi GOOS.
type GoSource struct{}

func (GoSource) Name() string { return "go" }

func (GoSource) Detect(root string) bool {
	st, err := os.Stat(filepath.Join(root, "go.mod"))
	return err == nil && !st.IsDir()
}

type goPkg struct {
	ImportPath                                           string
	Dir                                                  string
	Name                                                 string
	ForTest                                              string
	Deps                                                 []string
	GoFiles                                              []string
	TestGoFiles, XTestGoFiles                            []string
	EmbedPatterns, TestEmbedPatterns, XTestEmbedPatterns []string
	DepsErrors                                           []json.RawMessage
	Error                                                *struct{ Err string }
	Module                                               *struct{ Dir string }
}

// Load chạy go list và dựng đồ thị. Lỗi (không có go, go list hỏng, gói lỗi biên dịch/nhập) khiến người gọi nới full.
func (GoSource) Load(ctx context.Context, root string) (Graph, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("không tìm thấy go trên PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, goBin, "list", "-e", "-test", "-json", "./...")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var pkgs []goPkg
	dec := json.NewDecoder(&stdout)
	for {
		var p goPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list: đầu ra không đọc được: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return nil, errors.New("go list không trả gói nào")
	}
	return buildGoGraph(root, pkgs)
}

func normalizeImport(s string) string {
	before, _, _ := strings.Cut(s, " [")
	return before
}

type goGraph struct {
	root     string
	dirs     map[string]string   // thư mục (tương đối, gạch xuôi) → import path
	byImport map[string]*goPkg   // import path → gói
	dirOf    map[string]string   // import path → thư mục
	tests    map[string][]string // import path → deps của test main (đã chuẩn hoá)
	embed    []embedRule
}

type embedRule struct {
	dir      string
	patterns []string
}

func buildGoGraph(root string, pkgs []goPkg) (*goGraph, error) {
	g := &goGraph{root: root, dirs: map[string]string{}, byImport: map[string]*goPkg{}, dirOf: map[string]string{}, tests: map[string][]string{}}
	// Thư mục gốc module: ngắn nhất trong các Module.Dir.
	modDir := ""
	for i := range pkgs {
		if m := pkgs[i].Module; m != nil && m.Dir != "" && (modDir == "" || len(m.Dir) < len(modDir)) {
			modDir = m.Dir
		}
	}
	if modDir == "" {
		return nil, errors.New("go list không cho biết thư mục module")
	}
	for i := range pkgs {
		p := &pkgs[i]
		isTestMain := p.Name == "main" && strings.HasSuffix(p.ImportPath, ".test") && !strings.Contains(p.ImportPath, " [")
		variant := strings.Contains(p.ImportPath, " [")
		if p.Error != nil && !strings.Contains(p.Error.Err, "build constraints exclude all Go files") {
			return nil, fmt.Errorf("gói %s lỗi: %s", p.ImportPath, strings.TrimSpace(p.Error.Err))
		}
		if len(p.DepsErrors) > 0 {
			return nil, fmt.Errorf("gói %s có phụ thuộc lỗi", p.ImportPath)
		}
		switch {
		case isTestMain:
			deps := make([]string, 0, len(p.Deps))
			for _, d := range p.Deps {
				deps = append(deps, normalizeImport(d))
			}
			g.tests[strings.TrimSuffix(p.ImportPath, ".test")] = deps
		case variant || p.ForTest != "":
			// biến thể biên dịch cho test: Deps đã tính ở test main
		default:
			rel, err := filepath.Rel(modDir, p.Dir)
			if err != nil || strings.HasPrefix(rel, "..") {
				return nil, fmt.Errorf("gói %s nằm ngoài module gốc", p.ImportPath)
			}
			dir := filepath.ToSlash(rel)
			g.dirs[dir] = p.ImportPath
			g.byImport[p.ImportPath] = p
			g.dirOf[p.ImportPath] = dir
			var pats []string
			pats = append(pats, p.EmbedPatterns...)
			pats = append(pats, p.TestEmbedPatterns...)
			pats = append(pats, p.XTestEmbedPatterns...)
			if len(pats) > 0 {
				g.embed = append(g.embed, embedRule{dir, pats})
			}
		}
	}
	if len(g.dirs) == 0 {
		return nil, errors.New("go list không trả gói chuẩn nào")
	}
	return g, nil
}

func (g *goGraph) PackageDir(dir string) bool { _, ok := g.dirs[dir]; return ok }

func (g *goGraph) IsCode(p string) bool { return strings.HasSuffix(p, ".go") }

func (g *goGraph) HasCode(dir string) bool {
	ents, err := os.ReadDir(filepath.Join(g.root, filepath.FromSlash(dir)))
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// Nested: có go.mod ở thư mục tổ tiên của p (không kể gốc).
func (g *goGraph) Nested(p string) bool {
	for d := path.Dir(p); d != "." && d != "/" && d != ""; d = path.Dir(d) {
		if st, err := os.Stat(filepath.Join(g.root, filepath.FromSlash(d), "go.mod")); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// Embedded khớp p với mẫu //go:embed của từng gói (theo mẫu, không theo danh sách tệp: tệp đã xoá vẫn khớp).
func (g *goGraph) Embedded(p string) []string {
	var out []string
	for _, r := range g.embed {
		rel := p
		if r.dir != "." {
			if !strings.HasPrefix(p, r.dir+"/") {
				continue
			}
			rel = strings.TrimPrefix(p, r.dir+"/")
		}
		if embedMatches(r.patterns, rel) {
			out = append(out, r.dir)
		}
	}
	return out
}

func embedMatches(patterns []string, rel string) bool {
	parts := strings.Split(rel, "/")
	for _, pat := range patterns {
		pat = strings.TrimPrefix(pat, "all:")
		for i := 1; i <= len(parts); i++ { // mẫu có thể là thư mục chứa tệp
			if ok, _ := path.Match(pat, strings.Join(parts[:i], "/")); ok {
				return true
			}
		}
	}
	return false
}

// TestRefs quét mọi *_test.go trong thư mục các gói (cả tệp bị build tag loại) tìm tên cơ sở của p.
func (g *goGraph) TestRefs(p string) ([]string, error) {
	base := path.Base(p)
	var out []string
	for dir := range g.dirs {
		ents, err := os.ReadDir(filepath.Join(g.root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, fmt.Errorf("đọc thư mục %s: %w", dir, err)
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(dir), e.Name()))
			if err != nil {
				return nil, fmt.Errorf("đọc %s/%s: %w", dir, e.Name(), err)
			}
			if bytes.Contains(b, []byte(base)) {
				out = append(out, dir)
				break
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

func (g *goGraph) Select(dirs []string) ([]string, error) {
	changed := map[string]bool{}
	for _, d := range dirs {
		ip, ok := g.dirs[d]
		if !ok {
			return nil, fmt.Errorf("thư mục %s không phải gói Go", d)
		}
		changed[ip] = true
	}
	var files []string
	for ip, deps := range g.tests {
		hit := changed[ip]
		if !hit {
			hit = slices.ContainsFunc(deps, func(d string) bool { return changed[d] })
		}
		if !hit {
			continue
		}
		p := g.byImport[ip]
		if p == nil {
			return nil, fmt.Errorf("không có gói %s cho test", ip)
		}
		dir := g.dirOf[ip]
		for _, f := range append(slices.Clone(p.TestGoFiles), p.XTestGoFiles...) {
			files = append(files, path.Join(dir, f))
		}
	}
	slices.Sort(files)
	return files, nil
}
