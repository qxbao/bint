// Package hcomp quản lý thành phần quy trình của agent (kế hoạch H1): chỉ dẫn dùng công
// cụ, guardrail, script hỗ trợ và chiến lược ngữ cảnh. Mỗi thành phần là một tệp có
// phiên bản trong .bint/harness/, quản lý bằng Git như tri thức:
//
//	.bint/harness/guidance/<id>.md    chỉ dẫn (frontmatter YAML + thân Markdown)
//	.bint/harness/guardrails/<id>.yml luật kiểm tra hành vi trong vòng gọi công cụ
//	.bint/harness/scripts/<id>.sh     script hỗ trợ, kèm <id>.yml mô tả
//	.bint/harness/strategy.yml        tham số chọn ngữ cảnh (ghi đè một phần context.*)
//
// Gói này không I/O ngoài việc đọc/ghi các tệp trên, không gọi mô hình, không mở SQLite,
// không import evolve/eval/config (để hook và eval cùng dùng). Chỉ thành phần `verified`
// có hiệu lực ở phiên thật; Bint không bao giờ tự chạy script.
package hcomp

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// DirName là thư mục thành phần quy trình trong .bint/.
const DirName = "harness"

// Kind là loại thành phần.
type Kind string

const (
	KindGuidance  Kind = "guidance"
	KindGuardrail Kind = "guardrail"
	KindScript    Kind = "script"
	KindStrategy  Kind = "strategy"
)

// Kinds liệt kê mọi loại theo thứ tự hiển thị.
var Kinds = []Kind{KindGuidance, KindGuardrail, KindScript, KindStrategy}

// Trạng thái và nguồn gốc.
const (
	StatusCandidate  = "candidate"
	StatusVerified   = "verified"
	StatusDeprecated = "deprecated"

	OriginHuman = "human"
	OriginAgent = "agent"
)

// Sự kiện guardrail (on) và hành động (action).
const (
	OnPreEdit = "pre_edit"
	OnPreBash = "pre_bash"
	OnStop    = "stop"

	ActRemind = "remind"
	ActAsk    = "ask"
	ActDeny   = "deny"
)

// Từ khoá đặc biệt của unless.ran: lệnh kiểm thử (detect.IsTestCommand + eval.test_command).
const RanTest = "test"

// StrategyID là ID cố định của tệp chiến lược.
const StrategyID = "strategy"

// ScopeGlobal là phạm vi chung; phạm vi riêng viết "domain:<glob>".
const ScopeGlobal = "global"

// ScopePrefix là tiền tố phạm vi riêng.
const ScopePrefix = "domain:"

// Meta là các trường chung của mọi thành phần.
type Meta struct {
	ID      string `yaml:"id"`
	Scope   string `yaml:"scope"`
	Status  string `yaml:"status"`
	Origin  string `yaml:"origin"`
	Version int    `yaml:"version"`
	// ApprovedBy: người duyệt gần nhất (ghi khi approve hoặc khi người áp dụng update).
	ApprovedBy string `yaml:"approved_by,omitempty"`
}

// When là điều kiện kích hoạt guardrail; mọi điều kiện đã khai báo phải cùng đúng.
type When struct {
	// Edited: glob tệp đã sửa trong phiên.
	Edited string `yaml:"edited,omitempty" json:"edited,omitempty"`
	// Paths: glob tệp của lần gọi hiện tại (pre_edit).
	Paths string `yaml:"paths,omitempty" json:"paths,omitempty"`
	// Command: biểu thức chính quy của lệnh hiện tại (pre_bash).
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
}

// Unless là điều kiện miễn: guardrail không kích hoạt khi điều kiện này đúng.
type Unless struct {
	// Ran: "test" hoặc biểu thức chính quy; đúng khi phiên đã chạy lệnh khớp.
	Ran string `yaml:"ran,omitempty" json:"ran,omitempty"`
}

// Component là một thành phần quy trình. Trường riêng của từng loại chỉ có nghĩa với loại đó.
type Component struct {
	Kind Kind
	Meta
	// File là đường dẫn tương đối (dấu '/') trong .bint/harness/; rỗng với bản dựng sẵn.
	File string
	// Builtin: thành phần dựng sẵn trong mã (không phải tệp trong repository).
	Builtin bool

	// Guidance.
	Title    string
	Requires []string // "mcp": chỉ áp dụng khi kênh MCP bật
	Body     string

	// Guardrail.
	On      string
	When    When
	Unless  Unless
	Action  string
	Message string

	// Script.
	Description string
	Usage       string
	Source      []byte // nội dung .sh (rỗng khi chỉ có mô tả)

	// Strategy.
	Context *StrategyContext
}

// StrategyContext là tập con của context.* mà strategy.yml được ghi đè. Con trỏ nil =
// không ghi đè khoá đó.
type StrategyContext struct {
	MaxTokens    *int                `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	PathMaxItems *int                `yaml:"path_max_items,omitempty" json:"path_max_items,omitempty"`
	Lessons      *StrategyLessons    `yaml:"lessons,omitempty" json:"lessons,omitempty"`
	Interfaces   *StrategyInterfaces `yaml:"interfaces,omitempty" json:"interfaces,omitempty"`
}

// StrategyLessons ghi đè context.lessons.
type StrategyLessons struct {
	GlobalK   *int `yaml:"global_k,omitempty" json:"global_k,omitempty"`
	MaxTokens *int `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
}

// StrategyInterfaces ghi đè context.interfaces (chỉ mcp và path_hooks).
type StrategyInterfaces struct {
	MCP       *bool `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	PathHooks *bool `yaml:"path_hooks,omitempty" json:"path_hooks,omitempty"`
}

// Domain trả về glob của phạm vi riêng ("" nếu phạm vi chung).
func (m Meta) Domain() string {
	g, _ := strings.CutPrefix(m.Scope, ScopePrefix)
	if g == m.Scope {
		return ""
	}
	return g
}

// Global cho biết thành phần có phạm vi chung.
func (m Meta) Global() bool { return m.Scope == ScopeGlobal }

// Verified cho biết thành phần đang có hiệu lực.
func (m Meta) Verified() bool { return m.Status == StatusVerified }

// RelFile trả về đường dẫn tệp chuẩn của thành phần trong .bint/harness/.
func RelFile(kind Kind, id string) string {
	switch kind {
	case KindGuidance:
		return "guidance/" + id + ".md"
	case KindGuardrail:
		return "guardrails/" + id + ".yml"
	case KindScript:
		return "scripts/" + id + ".yml"
	case KindStrategy:
		return "strategy.yml"
	}
	return ""
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidID cho biết id hợp lệ (chữ thường, số, '-' và '_'; dùng làm tên tệp).
func ValidID(id string) bool { return idRe.MatchString(id) }

// ValidScope kiểm tra phạm vi: "global" hoặc "domain:<glob>".
func ValidScope(s string) error {
	if s == ScopeGlobal {
		return nil
	}
	if g, ok := strings.CutPrefix(s, ScopePrefix); ok {
		g = strings.TrimSpace(g)
		if g == "" || strings.HasPrefix(g, "/") || strings.Contains(g, `\`) || strings.Contains(g, "..") {
			return fmt.Errorf("scope %q: glob không hợp lệ (đường dẫn tương đối, dấu '/')", s)
		}
		if _, err := path.Match(strings.ReplaceAll(g, "**", "*"), "x"); err != nil {
			return fmt.Errorf("scope %q: glob không hợp lệ: %v", s, err)
		}
		return nil
	}
	return fmt.Errorf("scope %q: cần global hoặc domain:<glob>", s)
}
