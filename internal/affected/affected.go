// Package affected là kiểu dữ liệu của phần chọn test bị ảnh hưởng mà Bint lưu trong state.json (Selection, Widening) và cách dựng lệnh chạy
// (Script). Phần chọn do bgraph tính, ở gói internal/testsel; gói này không import bgraph để các gói nhẹ (task, mcpgw) không kéo theo chỉ mục
// SQLite của nó.
//
// Phân tầng (từ hẹp đến rộng): none (không test nào bị ảnh hưởng) → graph (chứng minh được từ đồ thị) → package (nới tới gói/thư mục/target)
// → module (nới tới dự án: package.json, crate, module Maven, project .NET, module Go) → full (bgraph không chắc ở mức nào hẹp hơn).
//
// Nguyên tắc: không bao giờ thu hẹp im lặng. Mọi lần nới có lý do (Widening). Scope full chỉ xảy ra khi bgraph nói vậy, kèm lý do; khi đó
// người gọi chạy lệnh kiểm chứng của phiếu (verify.command).
package affected

import "strings"

// Tier là tầng phạm vi kiểm thử (cùng giá trị với bgraph.Tier).
type Tier string

const (
	TierNone    Tier = "none"
	TierGraph   Tier = "graph"
	TierPackage Tier = "package"
	TierModule  Tier = "module"
	TierFull    Tier = "full"
)

// Lý do nới riêng của Bint (lý do của bgraph giữ nguyên mã của nó).
const (
	// ReasonSourceUnavailable: không tính được phần chọn (git lỗi, không mở được repo): nới ra full.
	ReasonSourceUnavailable = "source_unavailable"
	// ReasonNoBase: không có commit mốc để so (cùng mã với bgraph).
	ReasonNoBase = "no_base"
	// ReasonOnlyUnselected: chỉ chạy một tập tệp test (cổng mục tiêu) mà không đơn vị test nào bgraph biết chứa chúng.
	ReasonOnlyUnselected = "only_files_unselected"
	// ReasonNoSelectiveCommand: bgraph chọn được nhưng không dựng được lệnh chạy riêng cho các đơn vị đó.
	ReasonNoSelectiveCommand = "no_selective_command"
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
		b.WriteString("; (")
		b.WriteString(commandLine(c))
		b.WriteString(") || rc=1")
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

// shellQuote đặt s trong nháy đơn khi cần; nháy đơn bên trong được đóng, thoát rồi mở lại theo cú pháp sh.
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
