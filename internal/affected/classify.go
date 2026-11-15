package affected

import (
	"path"
	"slices"
	"strings"
)

// Result là kết quả phân tầng thay đổi.
type Result struct {
	Tier     Tier
	Widening []Widening // mỗi lần nới lên package/full
	Dirs     []string   // thư mục gói cần chọn (tầng graph/package)
}

var rootConfigNames = map[string]bool{
	"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "bun.lockb": true,
	"cargo.toml": true, "cargo.lock": true, "pyproject.toml": true, "requirements.txt": true, "poetry.lock": true,
	".golangci.yml": true, ".golangci.yaml": true, "staticcheck.conf": true, "jest.config.js": true,
	"vitest.config.ts": true, "tsconfig.json": true, "pytest.ini": true, "tox.ini": true,
}

var ciNames = map[string]bool{"makefile": true, "gnumakefile": true, "dockerfile": true, "justfile": true, "taskfile.yml": true}

var scriptExts = map[string]bool{".sh": true, ".ps1": true, ".bat": true, ".cmd": true, ".mk": true, ".mjs": true, ".py": true}

// configWidening: tệp cấu hình/CI/script buộc full bất kể đồ thị; nil nếu không phải.
func configWidening(p string) *Widening {
	base := strings.ToLower(path.Base(p))
	switch {
	case rootConfigNames[base]:
		return &Widening{Path: p, Tier: TierFull, Reason: ReasonRootConfig}
	case p == ".bint" || strings.HasPrefix(p, ".bint/"):
		return &Widening{Path: p, Tier: TierFull, Reason: ReasonBintConfig}
	case strings.HasPrefix(p, ".github/"), strings.HasPrefix(p, ".gitlab/"), strings.HasPrefix(p, ".circleci/"), base == ".gitlab-ci.yml":
		return &Widening{Path: p, Tier: TierFull, Reason: ReasonCIConfig}
	case ciNames[base], scriptExts[path.Ext(base)], strings.HasPrefix(p, "scripts/"), strings.HasPrefix(p, "script/"):
		return &Widening{Path: p, Tier: TierFull, Reason: ReasonCIConfig}
	}
	return nil
}

func configWidenings(changed []string) []Widening {
	var out []Widening
	for _, p := range changed {
		if w := configWidening(p); w != nil {
			out = append(out, *w)
		}
	}
	return out
}

func isDoc(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".txt":
		return true
	}
	return false
}

// testdataOwner: thư mục chứa phần testdata/ của p (phần đường dẫn trước thành phần "testdata"); ok=false nếu p không
// nằm dưới testdata.
func testdataOwner(p string) (string, bool) {
	parts := strings.Split(p, "/")
	for i, s := range parts[:len(parts)-1] {
		if s == "testdata" {
			if i == 0 {
				return ".", true
			}
			return strings.Join(parts[:i], "/"), true
		}
	}
	return "", false
}

// owningPackage: gói gần nhất (chính d hoặc tổ tiên) chứa d.
func owningPackage(g Graph, d string) (string, bool) {
	for {
		if g.PackageDir(d) {
			return d, true
		}
		if d == "." || d == "/" || d == "" {
			return "", false
		}
		d = path.Dir(d)
	}
}

// Classify phân tầng tập thay đổi theo đồ thị g.
func Classify(changed []string, g Graph) Result {
	res := Result{Tier: TierNone}
	dirs := map[string]bool{}
	raise := func(t Tier) {
		if t.rank() > res.Tier.rank() {
			res.Tier = t
		}
	}
	widen := func(p string, t Tier, reason, detail string) {
		res.Widening = append(res.Widening, Widening{Path: p, Tier: t, Reason: reason, Detail: detail})
		raise(t)
	}
	for _, p := range changed {
		dir := path.Dir(p)
		if w := configWidening(p); w != nil {
			widen(p, w.Tier, w.Reason, "")
			continue
		}
		if g.Nested(p) {
			widen(p, TierFull, ReasonNestedModule, "")
			continue
		}
		if owner, ok := testdataOwner(p); ok {
			if pkg, ok := owningPackage(g, owner); ok {
				dirs[pkg] = true
				widen(p, TierPackage, ReasonTestdata, "gói "+pkg)
			} else {
				widen(p, TierFull, ReasonUnknownPackage, "testdata không thuộc gói nào")
			}
			continue
		}
		switch {
		case g.IsCode(p):
			switch {
			case g.PackageDir(dir):
				dirs[dir] = true
				raise(TierGraph)
			case !g.HasCode(dir):
				widen(p, TierFull, ReasonDeletedPackage, "thư mục "+dir+" không còn mã")
			default:
				widen(p, TierFull, ReasonUnknownPackage, "thư mục "+dir+" có mã nhưng không ra gói nào")
			}
		case len(g.Embedded(p)) > 0:
			for _, d := range g.Embedded(p) {
				dirs[d] = true
			}
			widen(p, TierPackage, ReasonEmbedded, "")
		case isDoc(p):
			// *.md/*.txt: chỉ xếp none khi không gói nào sở hữu và không test nào nhắc tới tên tệp. Tệp thuộc một gói
			// vẫn phải tra TestRefs: test của gói khác có thể đọc nó qua đường dẫn tương đối (chỉ nới, không thu hẹp).
			if pkg, ok := owningPackage(g, dir); ok {
				dirs[pkg] = true
				widen(p, TierPackage, ReasonNonCode, "gói "+pkg)
			}
			refs, err := g.TestRefs(p)
			switch {
			case err != nil:
				widen(p, TierFull, ReasonSourceUnavailable, err.Error())
			case len(refs) > 0:
				for _, d := range refs {
					dirs[d] = true
				}
				widen(p, TierPackage, ReasonTestRef, "test của "+strings.Join(refs, ", "))
			}
		case g.PackageDir(dir):
			dirs[dir] = true
			widen(p, TierPackage, ReasonNonCode, "gói "+dir)
		default:
			widen(p, TierFull, ReasonUnknownFile, "")
		}
	}
	for d := range dirs {
		res.Dirs = append(res.Dirs, d)
	}
	slices.Sort(res.Dirs)
	return res
}
