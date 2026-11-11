package hcomp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Mode chọn tập thành phần có hiệu lực (biến thể eval `bint@harness=...`).
type Mode string

const (
	// ModeOff: chỉ bản dựng sẵn — đúng nhánh bint trước khi có thành phần quy trình.
	ModeOff Mode = "off"
	// ModeOn: bản dựng sẵn + thành phần verified trong repository (mặc định, giống phiên thật).
	ModeOn Mode = "on"
	// ModeAll: thêm cả thành phần candidate (để đo thử trước khi duyệt).
	ModeAll Mode = "all"
)

// ParseMode kiểm tra giá trị chế độ; rỗng = ModeOn.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "":
		return ModeOn, nil
	case ModeOff, ModeOn, ModeAll:
		return Mode(s), nil
	}
	return "", fmt.Errorf("harness=%q: chỉ nhận off|on|all", s)
}

// Set là tập thành phần đang có hiệu lực.
type Set struct {
	Components []Component
}

// Select lọc thư viện theo chế độ. Thành phần trong repository cùng loại và id với một
// bản dựng sẵn thì thay thế nó CHỈ KHI bản đó được chọn (verified, hoặc candidate ở ModeAll);
// bản deprecated tắt bản dựng sẵn; bản candidate ở ModeOn không có hiệu lực nên không động tới
// bản dựng sẵn (nếu không, thêm một candidate trùng id sẽ đổi nhánh đo chuẩn).
func (l *Library) Select(mode Mode) Set {
	var s Set
	var chosen []Component
	override := map[string]bool{}
	if l != nil && mode != ModeOff {
		for _, c := range l.Components {
			key := string(c.Kind) + "/" + c.ID
			switch {
			case c.Status == StatusVerified,
				c.Status == StatusCandidate && mode == ModeAll:
				chosen = append(chosen, c)
				override[key] = true
			case c.Status == StatusDeprecated:
				override[key] = true
			}
		}
	}
	for _, b := range Builtins() {
		if !override[string(b.Kind)+"/"+b.ID] {
			s.Components = append(s.Components, b)
		}
	}
	s.Components = append(s.Components, chosen...)
	sort.SliceStable(s.Components, func(i, j int) bool {
		a, b := s.Components[i], s.Components[j]
		if a.Kind != b.Kind {
			return kindOrder(a.Kind) < kindOrder(b.Kind)
		}
		return a.ID < b.ID
	})
	return s
}

// Files trả về phần của tập đến từ tệp trong repository (bỏ bản dựng sẵn).
func (s Set) Files() Set {
	var o Set
	for _, c := range s.Components {
		if !c.Builtin {
			o.Components = append(o.Components, c)
		}
	}
	return o
}

func (s Set) of(k Kind) []Component {
	var out []Component
	for _, c := range s.Components {
		if c.Kind == k {
			out = append(out, c)
		}
	}
	return out
}

// Guidance, Guardrails, Scripts trả về thành phần theo loại.
func (s Set) Guidance() []Component   { return s.of(KindGuidance) }
func (s Set) Guardrails() []Component { return s.of(KindGuardrail) }
func (s Set) Scripts() []Component    { return s.of(KindScript) }

// Strategy trả về chiến lược có hiệu lực (nil nếu không có).
func (s Set) Strategy() *Component {
	for i := range s.Components {
		if s.Components[i].Kind == KindStrategy {
			return &s.Components[i]
		}
	}
	return nil
}

// Empty cho biết tập không có thành phần nào từ tệp (chỉ còn bản dựng sẵn hoặc rỗng).
func (s Set) Empty() bool { return len(s.Files().Components) == 0 }

// Hash là dấu vân tay ngắn của tập (kiểm toán nguồn ngữ cảnh của lượt eval).
func (s Set) Hash() string {
	h := sha256.New()
	for _, c := range s.Components {
		b, _ := Marshal(c)
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", c.Kind, c.ID, len(b))
		h.Write(b)
		h.Write(c.Source)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Describe liệt kê "loại/id@phiên-bản" của tập (kiểm toán).
func (s Set) Describe() []string {
	var out []string
	for _, c := range s.Components {
		out = append(out, fmt.Sprintf("%s/%s@%d", c.Kind, c.ID, c.Version))
	}
	return out
}

// MCPToolsNote là chỉ dẫn công cụ MCP của Bint ở nhánh eval `bint`. Từng byte của nó là
// một phần của hợp đồng đo (số liệu cũ còn so được); TestMCPToolsNoteBytes giữ bản gốc.
const MCPToolsNote = `
## Công cụ tri thức (Bint)

MCP server ` + "`bint`" + ` có tri thức về repository này (quy tắc, điểm dễ sai, hợp đồng hành vi).
Các công cụ có thể chưa được nạp sẵn: nạp bằng ToolSearch với truy vấn
"select:mcp__bint__context_for_task,mcp__bint__context_for_paths,mcp__bint__cite_knowledge".

1. Đầu tác vụ: gọi mcp__bint__context_for_task với mô tả tác vụ (và các tệp đã biết).
2. Trước khi sửa một tệp: gọi mcp__bint__context_for_paths với đường dẫn tệp.
3. Khi áp dụng một mục tri thức: gọi mcp__bint__cite_knowledge với ID của mục.

Trước khi đọc/sửa tệp, quy tắc khớp đường dẫn cũng có thể được chèn tự động.
`

// BuiltinMCPToolsID là ID của guidance dựng sẵn thay ghi chú MCP viết cứng.
const BuiltinMCPToolsID = "bint-mcp-tools"

// Builtins là các thành phần dựng sẵn có hiệu lực ở mọi chế độ.
func Builtins() []Component {
	return []Component{{
		Kind: KindGuidance, Builtin: true,
		Meta:  Meta{ID: BuiltinMCPToolsID, Scope: ScopeGlobal, Status: StatusVerified, Origin: OriginHuman, Version: 1},
		Title: "Công cụ tri thức (Bint)", Requires: []string{"mcp"}, Body: MCPToolsNote,
	}}
}

// Templates là mẫu để người bắt đầu (`bint harness component add --template`); chúng KHÔNG
// có hiệu lực cho tới khi được thêm vào repository và duyệt.
func Templates() []Component {
	return []Component{{
		Kind: KindGuardrail,
		Meta: Meta{ID: "test-before-stop", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginHuman, Version: 1},
		On:   OnStop, When: When{Edited: "**"}, Unless: Unless{Ran: RanTest}, Action: ActRemind,
		Message: "Chưa chạy test cho phần đã sửa; chạy test trước khi kết thúc.",
	}}
}

// Template trả về mẫu theo id.
func Template(id string) (Component, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Component{}, false
}
