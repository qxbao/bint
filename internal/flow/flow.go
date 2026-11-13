// Package flow là mô hình flow (GĐ2 của docs/custom-flow-plan.md): một flow là dãy bước, mỗi bước có id, kind
// (plan | implement | review), skill và role. Bốn workflow gốc là flow nhúng (Builtin); người dùng khai báo thêm
// flow tuỳ biến trong .bint/flows/<tên>.yml.
//
// Gói này là gói lá: không import internal/workflow, internal/config hay internal/skills, để workflow (GĐ3) và
// config (GĐ4) import flow mà không tạo vòng. Việc skill có tồn tại hay không do bên gọi truyền vào Validate.
// Runner (GĐ3) đọc flow nhúng để chọn bước, trạng thái, chế độ chỉ đọc và cổng sau bước theo kind; flow tuỳ biến
// hiện chỉ xem và kiểm được (bint flow list|show|lint), chạy được từ GĐ4.
package flow

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// Schema là phiên bản định dạng tệp flow duy nhất được hỗ trợ.
const Schema = 1

// Rel là thư mục chứa flow tuỳ biến, tương đối với gốc repository.
const Rel = ".bint/flows"

// Kind là loại bước. Kind do Bint giữ: nó quyết định trạng thái tác vụ, chế độ chỉ đọc và cổng sau bước.
type Kind string

const (
	KindPlan      Kind = "plan"
	KindImplement Kind = "implement"
	KindReview    Kind = "review"
)

// Kinds là các kind hợp lệ theo thứ tự bắt buộc của flow (plan → implement → review).
var Kinds = []Kind{KindPlan, KindImplement, KindReview}

// rank là vị trí của kind trong thứ tự bắt buộc; -1 nếu kind không hợp lệ.
func (k Kind) rank() int {
	for i, x := range Kinds {
		if x == k {
			return i
		}
	}
	return -1
}

// DefaultRole là role mặc định của bước theo kind (role nhận harness/model/quyền MCP từ cấu hình).
func DefaultRole(k Kind) string {
	switch k {
	case KindPlan:
		return "planner"
	case KindImplement:
		return "implementer"
	case KindReview:
		return "reviewer"
	}
	return ""
}

// Step là một bước của flow.
type Step struct {
	ID    string `yaml:"id" json:"id"`
	Kind  Kind   `yaml:"kind" json:"kind"`
	Skill string `yaml:"skill" json:"skill"`
	// Role rỗng nhận DefaultRole(Kind).
	Role string `yaml:"role,omitempty" json:"role,omitempty"`
	// Optional: chỉ ở bước plan; bước có thể bỏ qua theo phân loại tác vụ.
	Optional bool `yaml:"optional,omitempty" json:"optional,omitempty"`
	// Verify: có chạy lệnh kiểm chứng sau bước implement hay không; nil = có.
	Verify *bool `yaml:"verify,omitempty" json:"verify,omitempty"`
	// Fix: chỉ ở bước review; id bước implement mà vòng sửa quay về.
	Fix string `yaml:"fix,omitempty" json:"fix,omitempty"`
}

// VerifyOn cho biết có chạy kiểm chứng sau bước này không (mặc định có).
func (s Step) VerifyOn() bool { return s.Verify == nil || *s.Verify }

// EffectiveRole là Role, hoặc role mặc định theo kind khi Role rỗng.
func (s Step) EffectiveRole() string {
	if s.Role != "" {
		return s.Role
	}
	return DefaultRole(s.Kind)
}

// Def là định nghĩa một flow.
type Def struct {
	Schema      int    `yaml:"schema" json:"schema"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Steps       []Step `yaml:"steps" json:"steps"`

	// builtin đánh dấu flow nhúng; không xuất ra YAML/JSON nên không đặt được từ tệp của người dùng.
	builtin bool
}

// IsBuiltin cho biết đây là flow nhúng (một trong bốn workflow gốc).
func (d Def) IsBuiltin() bool { return d.builtin }

// Clone là bản sao sâu (bước và con trỏ Verify không chia sẻ với bản gốc).
func (d Def) Clone() Def {
	out := d
	out.Steps = make([]Step, len(d.Steps))
	for i, s := range d.Steps {
		if s.Verify != nil {
			v := *s.Verify
			s.Verify = &v
		}
		out.Steps[i] = s
	}
	return out
}

// Normalize trả về bản sao sâu đã điền role mặc định theo kind.
func Normalize(d Def) Def {
	out := d.Clone()
	for i := range out.Steps {
		out.Steps[i].Role = out.Steps[i].EffectiveRole()
	}
	return out
}

// StepByID tìm bước theo id.
func (d Def) StepByID(id string) (Step, bool) {
	for _, s := range d.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// HasKind cho biết flow có ít nhất một bước kind k.
func (d Def) HasKind(k Kind) bool {
	_, ok := d.FirstOfKind(k)
	return ok
}

// FirstOfKind trả bước đầu tiên có kind k.
func (d Def) FirstOfKind(k Kind) (Step, bool) {
	for _, s := range d.Steps {
		if s.Kind == k {
			return s, true
		}
	}
	return Step{}, false
}

// OnlyKind cho biết flow không rỗng và mọi bước đều có kind k (ví dụ b-req chỉ có bước plan).
func (d Def) OnlyKind(k Kind) bool {
	if len(d.Steps) == 0 {
		return false
	}
	for _, s := range d.Steps {
		if s.Kind != k {
			return false
		}
	}
	return true
}

// Multi cho biết flow có nhiều bước (Runner điều phối cả chuỗi, có ngân sách và cổng chung); flow một bước chỉ chạy
// đúng bước đó.
func (d Def) Multi() bool { return len(d.Steps) > 1 }

// PlanOptional cho biết flow có bước plan có thể bỏ qua theo phân loại tác vụ (b-do).
func (d Def) PlanOptional() bool {
	s, ok := d.FirstOfKind(KindPlan)
	return ok && s.Optional
}

// Marshal xuất định nghĩa đã chuẩn hoá (role mặc định được điền) thành YAML, thụt lề 2 khoảng trắng.
func Marshal(d Def) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(Normalize(d)); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
