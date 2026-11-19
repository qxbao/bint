// Package affected chọn tập kiểm thử bị ảnh hưởng bởi một tập tệp thay đổi để lệnh kiểm chứng chỉ chạy phần cần thiết.
//
// Phân tầng (từ hẹp đến rộng): none (không cần chạy test) → graph (test của gói đổi và gói phụ thuộc bắc cầu vào nó)
// → package (như graph nhưng có thay đổi không phải mã nằm trong gói: testdata, tệp nhúng...) → full (chạy toàn bộ).
//
// Nguyên tắc: không bao giờ thu hẹp. Thay đổi lạ, nguồn ảnh hưởng không dùng được hay không chắc thì nới ra tầng
// rộng hơn và ghi lý do (Widening). Nguồn ảnh hưởng tách theo ngôn ngữ sau interface Source; hiện có Go.
package affected

import (
	"context"
	"path"
	"slices"
	"strings"
)

// Tier là tầng phạm vi kiểm thử.
type Tier string

const (
	TierNone    Tier = "none"
	TierGraph   Tier = "graph"
	TierPackage Tier = "package"
	TierFull    Tier = "full"
)

func (t Tier) rank() int {
	switch t {
	case TierNone:
		return 0
	case TierGraph:
		return 1
	case TierPackage:
		return 2
	}
	return 3
}

// Lý do nới (mã ổn định, dùng trong Widening.Reason).
const (
	ReasonRootConfig        = "root_config"        // go.mod/go.sum/go.work, khoá phụ thuộc, cấu hình công cụ
	ReasonCIConfig          = "ci_config"          // .github/**, Makefile, script
	ReasonBintConfig        = "bint_config"        // .bint/**
	ReasonNonCode           = "non_code_change"    // tệp không phải mã trong thư mục gói
	ReasonTestdata          = "testdata"           // testdata/ của một gói
	ReasonEmbedded          = "embedded_file"      // tệp được //go:embed
	ReasonNestedModule      = "nested_module"      // tệp thuộc module lồng
	ReasonSourceUnavailable = "source_unavailable" // nguồn ảnh hưởng lỗi/không có công cụ
	ReasonUnknownPackage    = "unknown_package"    // thư mục có mã mà không ra gói nào
	ReasonDeletedPackage    = "deleted_package"    // gói bị xoá
	ReasonUnsupported       = "unsupported_language"
	ReasonUnknownFile       = "unknown_file"   // tệp lạ ngoài gói
	ReasonTestRef           = "test_reference" // tệp .md/.txt ngoài gói mà test nhắc tới tên tệp
)

// Widening ghi một lần nới: tệp nào buộc phạm vi lên tầng nào, vì sao.
type Widening struct {
	Path   string `json:"path,omitempty"`
	Tier   Tier   `json:"tier"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// MaxChanged là số tệp đổi tối đa ghi vào Selection.Changed.
const MaxChanged = 200

// Selection là kết quả chọn: dùng làm bằng chứng của lần kiểm chứng.
type Selection struct {
	Scope        Tier       `json:"scope"`
	Base         string     `json:"base,omitempty"`
	Head         string     `json:"head,omitempty"`
	Source       string     `json:"source,omitempty"`
	Changed      []string   `json:"changed"`
	ChangedTotal int        `json:"changed_total"`
	Truncated    bool       `json:"truncated,omitempty"`
	Widening     []Widening `json:"widening,omitempty"`
	// TestFiles là tệp kiểm thử của các gói được chọn (tầng graph/package); TestDirs là thư mục của chúng (./dir).
	TestFiles []string `json:"test_files,omitempty"`
	TestDirs  []string `json:"test_dirs,omitempty"`
}

// Graph là đồ thị phụ thuộc của một dự án đã nạp bởi Source.
type Graph interface {
	// PackageDir: thư mục (gạch xuôi, tương đối gốc; "." là gốc) là một gói của ngôn ngữ.
	PackageDir(dir string) bool
	// HasCode: thư mục còn tệp mã nguồn của ngôn ngữ trên đĩa.
	HasCode(dir string) bool
	// Embedded: các thư mục gói nhúng tệp p (rỗng nếu không ai nhúng).
	Embedded(p string) []string
	// Nested: p thuộc một module/dự án lồng mà Source không bao phủ.
	Nested(p string) bool
	// IsCode: p là tệp mã nguồn của ngôn ngữ.
	IsCode(p string) bool
	// TestRefs: thư mục các gói có tệp kiểm thử nhắc tới tên tệp p (literal chuỗi, so theo tên cơ sở; nới, không thu hẹp).
	// Lỗi đọc nguồn trả error để người gọi nới full.
	TestRefs(p string) ([]string, error)
	// Select trả tệp kiểm thử của mọi gói có test phụ thuộc bắc cầu vào (hoặc chính là) một thư mục trong dirs.
	Select(dirs []string) ([]string, error)
}

// Source là nguồn ảnh hưởng của một ngôn ngữ.
type Source interface {
	Name() string
	// Detect: root là dự án của ngôn ngữ này.
	Detect(root string) bool
	// Load nạp đồ thị; lỗi thì người gọi nới ra full.
	Load(ctx context.Context, root string) (Graph, error)
}

// Input là đầu vào của Select.
type Input struct {
	Root    string
	Base    string
	Head    string
	Changed []string // đường dẫn tương đối gốc
	Sources []Source // nil = DefaultSources()
	// IgnoreBintConfig: tệp dưới .bint/ không nới ra full (workflow.verify.bint_config_full: false); vẫn nằm trong Changed. Mặc định false:
	// thay đổi .bint/** nới ra full (ReasonBintConfig).
	IgnoreBintConfig bool
}

func isBintPath(p string) bool { return p == ".bint" || strings.HasPrefix(p, ".bint/") }

// DefaultSources là các nguồn hỗ trợ.
func DefaultSources() []Source { return []Source{GoSource{}} }

func (s *Selection) full(w Widening) Selection {
	s.Scope = TierFull
	s.Widening = append(s.Widening, w)
	s.TestFiles, s.TestDirs = nil, nil
	return *s
}

// Select chọn phạm vi kiểm thử cho tập thay đổi in.Changed.
func Select(ctx context.Context, in Input) Selection {
	changed := make([]string, len(in.Changed))
	for i, c := range in.Changed {
		changed[i] = path.Clean(strings.ReplaceAll(c, "\\", "/"))
	}
	slices.Sort(changed)
	changed = slices.Compact(changed)
	sel := Selection{Base: in.Base, Head: in.Head, Changed: changed, ChangedTotal: len(changed), Scope: TierNone}
	if len(changed) > MaxChanged {
		sel.Changed, sel.Truncated = changed[:MaxChanged], true
	}
	if len(changed) == 0 {
		return sel
	}
	classify := changed
	if in.IgnoreBintConfig {
		classify = make([]string, 0, len(changed))
		for _, c := range changed {
			if !isBintPath(c) {
				classify = append(classify, c)
			}
		}
		if len(classify) == 0 {
			return sel // chỉ .bint/** đổi và khoá cho phép không nới: không test nào bị ảnh hưởng
		}
	}
	// Tệp cấu hình/CI quyết định full mà không cần nguồn.
	if ws := configWidenings(classify); len(ws) > 0 {
		sel.Widening, sel.Scope = ws, TierFull
		return sel
	}
	sources := in.Sources
	if sources == nil {
		sources = DefaultSources()
	}
	var src Source
	for _, s := range sources {
		if s.Detect(in.Root) {
			src = s
			break
		}
	}
	if src == nil {
		return sel.full(Widening{Tier: TierFull, Reason: ReasonUnsupported, Detail: "không có nguồn ảnh hưởng cho dự án này"})
	}
	sel.Source = src.Name()
	g, err := src.Load(ctx, in.Root)
	if err != nil {
		return sel.full(Widening{Tier: TierFull, Reason: ReasonSourceUnavailable, Detail: err.Error()})
	}
	res := Classify(classify, g)
	sel.Widening = res.Widening
	switch res.Tier {
	case TierFull:
		sel.Scope = TierFull
		return sel
	case TierNone:
		return sel
	}
	files, err := g.Select(res.Dirs)
	if err != nil {
		return sel.full(Widening{Tier: TierFull, Reason: ReasonSourceUnavailable, Detail: err.Error()})
	}
	if len(files) == 0 {
		return sel // không gói nào có test bị ảnh hưởng: không bao giờ để {test_dirs} rỗng
	}
	sel.Scope = res.Tier
	sel.TestFiles = files
	seen := map[string]bool{}
	for _, f := range files {
		d := path.Dir(f)
		if d != "." {
			d = "./" + d
		}
		if !seen[d] {
			seen[d] = true
			sel.TestDirs = append(sel.TestDirs, d)
		}
	}
	return sel
}
