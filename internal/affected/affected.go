// Package affected chọn test bị ảnh hưởng bởi một thay đổi, cho mọi lần chạy test của Bint. Phần chọn do bgraph
// (github.com/qxbao/bgraph) tính: đồ thị phụ thuộc mức tệp của cây mốc và cây mới, đi ngược từ tệp đổi, cho Node, Python, Rust, JVM, .NET
// và Go. Gói này chỉ chuyển kết quả sang dạng Bint lưu trong state.json và dựng lệnh chạy.
//
// Phân tầng (từ hẹp đến rộng): none (không test nào bị ảnh hưởng) → graph (chứng minh được từ đồ thị) → package (nới tới gói/thư mục/target)
// → module (nới tới dự án: package.json, crate, module Maven, project .NET, module Go) → full (bgraph không chắc ở mức nào hẹp hơn).
//
// Nguyên tắc: không bao giờ thu hẹp im lặng. Mọi lần nới có lý do (Widening). Scope full chỉ xảy ra khi bgraph nói vậy, kèm lý do; khi đó
// người gọi chạy lệnh kiểm chứng của phiếu (verify.command).
package affected

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/qxbao/bgraph"
)

// Tier là tầng phạm vi kiểm thử.
type Tier string

const (
	TierNone    Tier = Tier(bgraph.TierNone)
	TierGraph   Tier = Tier(bgraph.TierGraph)
	TierPackage Tier = Tier(bgraph.TierPackage)
	TierModule  Tier = Tier(bgraph.TierModule)
	TierFull    Tier = Tier(bgraph.TierFull)
)

// Lý do nới riêng của Bint (lý do của bgraph giữ nguyên mã của nó, xem bgraph.Reason*).
const (
	// ReasonSourceUnavailable: không tính được phần chọn (git lỗi, không mở được repo): nới ra full.
	ReasonSourceUnavailable = "source_unavailable"
	// ReasonNoBase: không có commit mốc để so.
	ReasonNoBase = bgraph.ReasonNoBase
)

// Widening ghi một lần nới: tệp nào buộc phạm vi lên tầng nào, vì sao, và gợi ý thu hẹp (luật bgraph.toml) nếu có.
type Widening struct {
	Path    string `json:"path,omitempty"`
	Tier    Tier   `json:"tier"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Suggest string `json:"suggest,omitempty"`
}

// Test là một đơn vị test được chọn (tệp test, Cargo target, module Maven, project .NET, gói Go).
type Test struct {
	Unit    string   `json:"unit"`
	Runner  string   `json:"runner,omitempty"`
	Project string   `json:"project,omitempty"`
	Files   []string `json:"files,omitempty"`
	Widened bool     `json:"widened,omitempty"`
}

// Command là một lệnh chạy test do bgraph dựng cho runner của dự án.
type Command struct {
	Runner  string   `json:"runner"`
	Project string   `json:"project"`
	Argv    []string `json:"argv"`
	Env     []string `json:"env,omitempty"`
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
	Warnings     []Widening `json:"warnings,omitempty"`
	Tests        []Test     `json:"tests,omitempty"`
	// TestFiles là các tệp test của đơn vị được chọn; TestDirs là thư mục của chúng (./dir). Dùng để hiển thị và cho lệnh có {test_files}.
	TestFiles []string `json:"test_files,omitempty"`
	TestDirs  []string `json:"test_dirs,omitempty"`
	// Commands: lệnh chạy phần chọn (rỗng khi scope none hoặc full).
	Commands []Command `json:"commands,omitempty"`
}

// Input là đầu vào của Select.
type Input struct {
	Root string
	// Base: commit/ref mốc. Rỗng → full (no_base).
	Base string
	// Head: commit đích; rỗng thì so với cây làm việc ở Root (gồm phần chưa commit và tệp chưa theo dõi không bị ignore).
	Head string
	// Exclude: mẫu đường dẫn (gạch xuôi, tương đối gốc repo, `dir/**`) bỏ khỏi tập thay đổi: sổ phiếu của Bint, và .bint/** khi
	// workflow.verify.bint_config_full là false.
	Exclude []string
	// StateDir: thư mục chỉ mục bền của bgraph (rỗng: thư mục cache của người dùng).
	StateDir string
	// PreviouslyFailed: test đỏ ở lần trước, luôn chạy lại.
	PreviouslyFailed []string
	// Only: nếu khác rỗng, chỉ giữ các đơn vị test chứa một trong các tệp này (cổng mục tiêu chạy riêng test đóng băng). Không đơn vị nào
	// khớp thì scope full (người gọi chạy lệnh của phiếu).
	Only []string
}

// Select tính phần chọn. Không bao giờ trả lỗi: lỗi hạ tầng (git không chạy, repo hỏng) thành scope full kèm lý do.
func Select(ctx context.Context, in Input) Selection {
	full := func(reason, detail string) Selection {
		return Selection{Scope: TierFull, Base: in.Base, Head: in.Head, Source: "bgraph", Changed: []string{},
			Widening: []Widening{{Tier: TierFull, Reason: reason, Detail: detail}}}
	}
	if strings.TrimSpace(in.Base) == "" {
		return full(ReasonNoBase, "không có commit mốc để so thay đổi")
	}
	ix, err := bgraph.Open(ctx, in.Root, bgraph.Options{StateDir: in.StateDir})
	if err != nil {
		return full(ReasonSourceUnavailable, err.Error())
	}
	defer ix.Close()
	ch := bgraph.Change{Base: in.Base, Head: in.Head, Worktree: in.Head == "", PreviouslyFailed: in.PreviouslyFailed, Exclude: in.Exclude}
	bs, err := ix.Select(ctx, ch)
	if err != nil {
		return full(ReasonSourceUnavailable, err.Error())
	}
	if len(in.Only) > 0 {
		bs = restrict(bs, in.Only)
	}
	sel := FromBgraph(bs)
	if in.Head == "" {
		sel.Head = ""
	}
	return sel
}

// ReasonOnlyUnselected: Input.Only không khớp đơn vị test nào bgraph biết.
const ReasonOnlyUnselected = "only_files_unselected"

// restrict giữ các đơn vị test của bs chứa một tệp trong only (so theo Files, hoặc Unit khi đơn vị là một tệp/thư mục chứa tệp).
func restrict(bs bgraph.Selection, only []string) bgraph.Selection {
	if bs.Scope == bgraph.TierFull || bs.Scope == bgraph.TierNone && len(bs.Tests) == 0 {
		// full: không biết đơn vị; none: bgraph không thấy tệp nào đổi ảnh hưởng tới chúng, vẫn phải chạy chúng nên lùi về lệnh của phiếu.
		bs.Scope = bgraph.TierFull
		bs.Tests = nil
		bs.Widening = append(bs.Widening, bgraph.Widening{Tier: bgraph.TierFull, Reason: ReasonOnlyUnselected,
			Detail: "không dựng được đơn vị test cho: " + strings.Join(only, ", ")})
		return bs
	}
	match := func(t bgraph.SelectedTest) bool {
		dir, isDir := unitDir(t)
		for _, o := range only {
			if t.Unit == o || slices.Contains(t.Files, o) || isDir && path.Dir(o) == dir {
				return true
			}
		}
		return false
	}
	var keep []bgraph.SelectedTest
	for _, t := range bs.Tests {
		if match(t) {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		bs.Tests = nil
		return restrict(bgraph.Selection{Scope: bgraph.TierFull, Base: bs.Base, Head: bs.Head, Changed: bs.Changed, Widening: bs.Widening}, only)
	}
	bs.Tests = keep
	return bs
}

// unitDir: thư mục của đơn vị test là cả một gói (Go: SelectedTest.Dir, Files chỉ là nút ảo). ok=false với đơn vị theo tệp.
func unitDir(t bgraph.SelectedTest) (string, bool) {
	return t.Dir, t.Dir != ""
}

// FromBgraph chuyển Selection của bgraph sang dạng Bint, kèm lệnh chạy.
func FromBgraph(bs bgraph.Selection) Selection {
	sel := Selection{Scope: Tier(bs.Scope), Base: bs.Base, Head: bs.Head, Source: "bgraph", Changed: slices.Clone(bs.Changed)}
	if sel.Changed == nil {
		sel.Changed = []string{}
	}
	sel.ChangedTotal = len(sel.Changed)
	if len(sel.Changed) > MaxChanged {
		sel.Changed, sel.Truncated = sel.Changed[:MaxChanged], true
	}
	conv := func(ws []bgraph.Widening) []Widening {
		var out []Widening
		for _, w := range ws {
			out = append(out, Widening{Path: w.Path, Tier: Tier(w.Tier), Reason: w.Reason, Detail: w.Detail, Scope: w.Scope, Suggest: w.Suggest})
		}
		return out
	}
	sel.Widening, sel.Warnings = conv(bs.Widening), conv(bs.Warnings)
	if sel.Scope == TierFull {
		return sel
	}
	seenF, seenD := map[string]bool{}, map[string]bool{}
	for _, t := range bs.Tests {
		sel.Tests = append(sel.Tests, Test{Unit: t.Unit, Runner: t.Runner, Project: t.Project, Files: slices.Clone(t.Files), Widened: t.Widened})
		addDir := func(d string) {
			if d != "." {
				d = "./" + strings.TrimPrefix(d, "./")
			}
			if !seenD[d] {
				seenD[d] = true
				sel.TestDirs = append(sel.TestDirs, d)
			}
		}
		if d, ok := unitDir(t); ok {
			addDir(d) // đơn vị là cả gói (Go): Files chỉ là nút ảo
			continue
		}
		for _, f := range t.Files {
			if !seenF[f] {
				seenF[f] = true
				sel.TestFiles = append(sel.TestFiles, f)
			}
			addDir(path.Dir(f))
		}
	}
	slices.Sort(sel.TestFiles)
	slices.Sort(sel.TestDirs)
	if sel.Scope == TierNone {
		return sel
	}
	plan, err := bgraph.Commands(bs, bgraph.CommandOptions{})
	if err != nil {
		sel.Scope = TierFull
		sel.Widening = append(sel.Widening, Widening{Tier: TierFull, Reason: ReasonSourceUnavailable, Detail: "không dựng được lệnh chạy: " + err.Error()})
		return sel
	}
	if Tier(plan.Scope) == TierFull {
		// bgraph không dựng được lệnh chọn lọc (đơn vị không chạy riêng được): chạy cả bộ, ghi lý do.
		sel.Scope = TierFull
		sel.Widening = append(sel.Widening, Widening{Tier: TierFull, Reason: "no_selective_command", Detail: strings.Join(plan.Notes, "; ")})
		return sel
	}
	for _, c := range plan.Commands {
		sel.Commands = append(sel.Commands, Command{Runner: c.Runner, Project: c.Project, Argv: slices.Clone(c.Argv), Env: slices.Clone(c.Env)})
	}
	return sel
}

// Script dựng một lệnh sh chạy mọi lệnh của phần chọn, chạy hết cả khi một lệnh đỏ (để có đủ danh sách test đỏ) rồi thoát 1 nếu có lệnh đỏ.
// Rỗng khi không có lệnh (scope none hoặc full).
func (s Selection) Script() string {
	if len(s.Commands) == 0 {
		return ""
	}
	if len(s.Commands) == 1 {
		return commandLine(s.Commands[0])
	}
	var b strings.Builder
	b.WriteString("rc=0")
	for _, c := range s.Commands {
		b.WriteString("; (" + commandLine(c) + ") || rc=1")
	}
	b.WriteString("; exit $rc")
	return b.String()
}

func commandLine(c Command) string {
	var parts []string
	for _, e := range c.Env {
		k, v, _ := strings.Cut(e, "=")
		parts = append(parts, k+"="+shellQuote(v))
	}
	for _, a := range c.Argv {
		parts = append(parts, shellQuote(a))
	}
	line := strings.Join(parts, " ")
	if p := strings.TrimSpace(c.Project); p != "" && p != "." {
		line = "cd " + shellQuote(p) + " && " + line
	}
	return line
}

// shellQuote đặt s trong nháy đơn khi cần (sh: nháy đơn bên trong viết '\”).
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@,+%^", r))
	}) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Full: phần chọn đòi chạy cả bộ.
func (s Selection) Full() bool { return s.Scope == TierFull }
