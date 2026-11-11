// Package form dựng biểu mẫu từ struct Options của tầng dịch vụ bằng phản xạ (docs/tui-plan.md mục 7.3).
//
// Mỗi trường có thẻ `flag:"tên-cờ"` hoặc `arg:"tên"` (đối số vị trí) thành một ô nhập; thẻ
// `form:"a,b,..."` điều chỉnh: required, advanced (nhóm "Nâng cao" thu gọn), hidden (không hiện),
// multiline, suggest=<nguồn> (gợi ý mờ), choices=a|b|c (chọn một giá trị). Nhãn lấy từ danh mục i18n
// theo khoá `form.<ID hành động>.<tên cờ|đối số>`. Dòng lệnh CLI tương đương sinh từ chính các thẻ này.
package form

import (
	"errors"
	"reflect"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/components/input"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// Kind là kiểu giá trị của một ô.
type Kind int

const (
	String Kind = iota
	Strings
	Bool
	Int
	Float
	Choice
)

// Field mô tả một ô nhập suy ra từ một trường của Options.
type Field struct {
	// Name là tên cờ (không có "--") hoặc tên đối số.
	Name string
	// Arg: đối số vị trí (không phải cờ).
	Arg       bool
	Kind      Kind
	Required  bool
	Advanced  bool
	Multiline bool
	Suggest   string
	Choices   []string
	index     int  // chỉ số trường trong struct
	ptr       bool // trường *float64: ô trống = nil
	unsigned  bool // trường uint64
}

// LabelKey là khoá i18n của nhãn ô.
func LabelKey(actionID string, f Field) string { return "form." + actionID + "." + f.Name }

// Fields suy ra các ô từ kiểu của opts (struct hoặc con trỏ tới struct), theo thứ tự khai báo; trường hidden
// hoặc không có thẻ flag/arg bị bỏ. WithHidden = true trả cả trường hidden (cho test phủ nhãn của cờ).
func Fields(opts any) []Field { return fields(opts, false) }

// AllFields như Fields nhưng gồm cả trường hidden.
func AllFields(opts any) []Field { return fields(opts, true) }

func fields(opts any, withHidden bool) []Field {
	t := reflect.TypeOf(opts)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []Field
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		f := Field{index: i}
		if n := sf.Tag.Get("flag"); n != "" {
			f.Name = n
		} else if n := sf.Tag.Get("arg"); n != "" {
			f.Name, f.Arg = n, true
		} else {
			continue
		}
		hidden := false
		for _, part := range strings.Split(sf.Tag.Get("form"), ",") {
			switch k, v, _ := strings.Cut(part, "="); k {
			case "required":
				f.Required = true
			case "advanced":
				f.Advanced = true
			case "hidden":
				hidden = true
			case "multiline":
				f.Multiline = true
			case "suggest":
				f.Suggest = v
			case "choices":
				f.Choices = strings.Split(v, "|")
			}
		}
		if hidden && !withHidden {
			continue
		}
		switch sf.Type.Kind() {
		case reflect.String:
			f.Kind = String
		case reflect.Slice:
			if sf.Type.Elem().Kind() != reflect.String {
				continue
			}
			f.Kind = Strings
		case reflect.Bool:
			f.Kind = Bool
		case reflect.Int:
			f.Kind = Int
		case reflect.Uint64:
			f.Kind, f.unsigned = Int, true
		case reflect.Float64:
			f.Kind = Float
		case reflect.Pointer:
			if sf.Type.Elem().Kind() != reflect.Float64 {
				continue
			}
			f.Kind, f.ptr = Float, true
		default:
			continue
		}
		if len(f.Choices) > 0 && f.Kind == String {
			f.Kind = Choice
		}
		out = append(out, f)
	}
	return out
}

// Result là kết quả xử lý một phím.
type Result int

const (
	None   Result = iota
	Submit        // người dùng gửi biểu mẫu (đã qua kiểm tra, xem Value)
	Cancel
	Copy // chép dòng lệnh CLI tương đương
)

// Model là trạng thái biểu mẫu.
type Model struct {
	ActionID string
	CLIPath  string
	Fields   []Field
	// Suggestions: nguồn gợi ý theo tên (files, ids).
	Suggestions map[string][]string

	defaults reflect.Value // bản sao Options làm giá trị khởi đầu (giữ cả trường không hiện)
	typ      reflect.Type
	inputs   []input.Model
	choice   []int
	bools    []bool

	Focus    int // chỉ số trong danh sách hàng đang hiện (xem rows)
	Advanced bool
	// Err là lỗi chung (kiểm tra Validate) và FieldErr là lỗi theo ô (chỉ số trong Fields).
	Err      string
	FieldErr map[int]string
}

// New dựng biểu mẫu cho hành động actionID từ Options mẫu defaults (giá trị khởi đầu, struct hoặc con trỏ).
// cliPath là đường lệnh CLI tương đương ("knowledge add").
func New(actionID, cliPath string, defaults any) Model {
	v := reflect.ValueOf(defaults)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	cp := reflect.New(v.Type()).Elem()
	cp.Set(v)
	m := Model{ActionID: actionID, CLIPath: cliPath, Fields: Fields(defaults), defaults: cp, typ: v.Type(),
		Suggestions: map[string][]string{}, FieldErr: map[int]string{}}
	m.inputs = make([]input.Model, len(m.Fields))
	m.choice = make([]int, len(m.Fields))
	m.bools = make([]bool, len(m.Fields))
	for i, f := range m.Fields {
		fv := cp.Field(f.index)
		switch f.Kind {
		case String:
			m.inputs[i] = input.New(fv.String())
		case Strings:
			var parts []string
			for j := 0; j < fv.Len(); j++ {
				parts = append(parts, fv.Index(j).String())
			}
			m.inputs[i] = input.New(strings.Join(parts, ", "))
		case Bool:
			m.bools[i] = fv.Bool()
		case Int:
			if f.unsigned {
				if n := fv.Uint(); n != 0 {
					m.inputs[i] = input.New(strconv.FormatUint(n, 10))
				}
			} else if n := fv.Int(); n != 0 {
				m.inputs[i] = input.New(strconv.FormatInt(n, 10))
			}
		case Float:
			if f.ptr {
				if !fv.IsNil() {
					m.inputs[i] = input.New(strconv.FormatFloat(fv.Elem().Float(), 'f', -1, 64))
				}
			} else if x := fv.Float(); x != 0 {
				m.inputs[i] = input.New(strconv.FormatFloat(x, 'f', -1, 64))
			}
		case Choice:
			for j, c := range f.Choices {
				if c == fv.String() {
					m.choice[i] = j + 1
				}
			}
		}
		m.inputs[i].Multiline = f.Multiline
		if f.Suggest != "" {
			i, f := i, f
			m.inputs[i].Ghost = func(s string) string { return m.ghost(f, s) }
		}
	}
	return m
}

// ghost trả phần gợi ý nối sau mục đang gõ (sau dấu phẩy cuối) của ô f.
func (m *Model) ghost(f Field, s string) string {
	tok := s
	if i := strings.LastIndex(tok, ","); i >= 0 {
		tok = tok[i+1:]
	}
	tok = strings.TrimLeft(tok, " ")
	if tok == "" {
		return ""
	}
	low := strings.ToLower(tok)
	for _, c := range m.Suggestions[f.Suggest] {
		if len(c) > len(tok) && strings.HasPrefix(strings.ToLower(c), low) {
			return c[len(tok):]
		}
	}
	return ""
}

// row là một hàng hiện trên biểu mẫu: một ô (field >= 0) hoặc tiêu đề nhóm Nâng cao (field = -1).
type row struct{ field int }

func (m *Model) rows() []row {
	var out []row
	hasAdv := false
	for i, f := range m.Fields {
		if f.Advanced {
			hasAdv = true
			continue
		}
		out = append(out, row{i})
	}
	if hasAdv {
		out = append(out, row{-1})
		if m.Advanced {
			for i, f := range m.Fields {
				if f.Advanced {
					out = append(out, row{i})
				}
			}
		}
	}
	return out
}

// SetSuggestions đặt danh sách gợi ý cho nguồn name (files, ids); phải gọi qua hàm này để các ô đã dựng thấy.
func (m *Model) SetSuggestions(name string, list []string) { m.Suggestions[name] = list }

// SetChoices thay danh sách lựa chọn của ô name (vd. chỉ các trạng thái đích hợp lệ theo bảng chuyển). Lựa chọn
// hiện tại được giữ nếu còn trong danh sách mới, không thì bỏ chọn.
func (m *Model) SetChoices(name string, list []string) {
	for i, f := range m.Fields {
		if f.Name != name || f.Kind != Choice {
			continue
		}
		cur := ""
		if c := m.choice[i]; c > 0 && c <= len(f.Choices) {
			cur = f.Choices[c-1]
		}
		m.Fields[i].Choices = append([]string(nil), list...)
		m.choice[i] = 0
		for j, c := range list {
			if c == cur {
				m.choice[i] = j + 1
			}
		}
	}
}

// Capturing luôn đúng: khi biểu mẫu mở, mọi phím chữ thuộc về ô nhập.
func (m *Model) Capturing() bool { return true }

func (m *Model) cur() (row, bool) {
	rs := m.rows()
	if len(rs) == 0 {
		return row{}, false
	}
	m.Focus = max(0, min(m.Focus, len(rs)-1))
	return rs[m.Focus], true
}

// Update xử lý một phím.
func (m *Model) Update(msg tea.KeyPressMsg) Result {
	s := msg.String()
	switch s {
	case "esc", "ctrl+c":
		return Cancel
	case "ctrl+y":
		return Copy
	case "ctrl+s":
		return m.submit()
	case "tab", "down":
		m.Focus++
		m.clampFocus()
		return None
	case "shift+tab", "up":
		m.Focus--
		m.clampFocus()
		return None
	case "ctrl+o":
		m.Advanced = !m.Advanced
		m.clampFocus()
		return None
	}
	r, ok := m.cur()
	if !ok {
		return None
	}
	if r.field < 0 { // tiêu đề nhóm Nâng cao
		if s == "enter" || s == "space" || s == " " || s == "right" || s == "left" {
			m.Advanced = !m.Advanced
		}
		return None
	}
	f := m.Fields[r.field]
	delete(m.FieldErr, r.field)
	m.Err = ""
	switch f.Kind {
	case Bool:
		switch s {
		case "space", " ", "left", "right", "x":
			m.bools[r.field] = !m.bools[r.field]
		case "enter":
			return m.submit()
		}
	case Choice:
		n := len(f.Choices) + 1 // 0 = chưa chọn
		switch s {
		case "right", "space", " ":
			m.choice[r.field] = (m.choice[r.field] + 1) % n
		case "left":
			m.choice[r.field] = (m.choice[r.field] - 1 + n) % n
		case "enter":
			return m.submit()
		}
	default:
		if s == "enter" && !f.Multiline {
			return m.submit()
		}
		m.inputs[r.field].Update(msg)
	}
	return None
}

func (m *Model) clampFocus() { m.Focus = max(0, min(m.Focus, len(m.rows())-1)) }

// Strings tách giá trị ô nhiều giá trị: phân cách bằng dấu phẩy, bỏ phần trống.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Errors của biểu mẫu mà người gọi có thể nhận biết để dịch.
var (
	ErrRequired = errors.New("form: required")
	ErrNumber   = errors.New("form: number")
)

// Build dựng Options từ giá trị các ô (bản sao của defaults, ghi đè các ô hiện). Lỗi theo ô (thiếu giá trị
// bắt buộc, số sai) được ghi vào FieldErr và trả về dưới dạng lỗi.
func (m *Model) Build() (any, error) {
	out := reflect.New(m.typ).Elem()
	out.Set(m.defaults)
	m.FieldErr = map[int]string{}
	var bad error
	for i, f := range m.Fields {
		fv := out.Field(f.index)
		val := strings.TrimSpace(m.inputs[i].Text)
		switch f.Kind {
		case String:
			if f.Multiline {
				val = strings.TrimSpace(m.inputs[i].Text)
			}
			fv.SetString(val)
		case Strings:
			l := splitList(val)
			sl := reflect.MakeSlice(fv.Type(), 0, len(l))
			for _, x := range l {
				sl = reflect.Append(sl, reflect.ValueOf(x))
			}
			fv.Set(sl)
			val = strings.Join(l, ",")
		case Bool:
			fv.SetBool(m.bools[i])
		case Int:
			if val == "" {
				fv.SetZero()
				break
			}
			if f.unsigned {
				n, err := strconv.ParseUint(val, 10, 64)
				if err != nil {
					m.FieldErr[i] = "number"
					bad = ErrNumber
					continue
				}
				fv.SetUint(n)
				break
			}
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				m.FieldErr[i] = "number"
				bad = ErrNumber
				continue
			}
			fv.SetInt(n)
		case Float:
			if val == "" {
				fv.SetZero() // 0 hoặc nil (trường con trỏ: chưa đặt)
				break
			}
			x, err := strconv.ParseFloat(strings.ReplaceAll(val, ",", "."), 64)
			if err != nil {
				m.FieldErr[i] = "number"
				bad = ErrNumber
				continue
			}
			if f.ptr {
				p := reflect.New(fv.Type().Elem())
				p.Elem().SetFloat(x)
				fv.Set(p)
			} else {
				fv.SetFloat(x)
			}
		case Choice:
			v := ""
			if c := m.choice[i]; c > 0 {
				v = f.Choices[c-1]
			}
			fv.SetString(v)
			val = v
		}
		if f.Required && val == "" && f.Kind != Bool {
			m.FieldErr[i] = "required"
			bad = ErrRequired
		}
	}
	if bad != nil {
		return nil, bad
	}
	return out.Interface(), nil
}

// FocusField đưa tiêu điểm tới ô có tên (cờ hoặc đối số); mở nhóm Nâng cao nếu ô nằm trong đó.
func (m *Model) FocusField(name string) bool {
	for i, f := range m.Fields {
		if f.Name != name {
			continue
		}
		if f.Advanced {
			m.Advanced = true
		}
		for j, r := range m.rows() {
			if r.field == i {
				m.Focus = j
				return true
			}
		}
	}
	return false
}

// submit dựng Options rồi gọi Validate() của nó (nếu có); lỗi mức giá trị hiện ở ô được nhắc tới (theo
// "--tên-cờ" trong thông báo) hoặc ở dòng lỗi chung.
func (m *Model) submit() Result {
	v, err := m.Build()
	if err != nil {
		for i := range m.Fields {
			if _, bad := m.FieldErr[i]; bad {
				m.FocusField(m.Fields[i].Name)
				break
			}
		}
		return None
	}
	if vd, ok := v.(interface{ Validate() error }); ok {
		if err := vd.Validate(); err != nil {
			m.Err = err.Error()
			for _, f := range m.Fields {
				if !f.Arg && strings.Contains(m.Err, "--"+f.Name) {
					m.FocusField(f.Name)
					break
				}
			}
			return None
		}
	}
	return Submit
}

// Value trả Options hiện tại (không kiểm tra); dùng sau Submit.
func (m *Model) Value() any {
	v, _ := m.Build()
	return v
}

// CLI trả dòng lệnh CLI tương đương với giá trị hiện tại: chỉ các cờ khác giá trị khởi đầu, rồi đối số
// vị trí. Giá trị có ký tự đặc biệt được đặt trong dấu nháy đơn. Cờ nhiều giá trị lặp lại cờ.
func (m *Model) CLI() string {
	v, err := m.Build()
	if err != nil {
		m.FieldErr = map[int]string{}
		return "bint " + m.CLIPath
	}
	return CLIFor(m.CLIPath, m.defaults.Interface(), v)
}

// CLIFor dựng dòng lệnh từ Options giá trị v so với giá trị khởi đầu def (cùng kiểu).
func CLIFor(cliPath string, def, v any) string {
	dv, vv := reflect.ValueOf(def), reflect.ValueOf(v)
	var flags, args []string
	for _, f := range AllFields(v) {
		fv, df := vv.Field(f.index), dv.Field(f.index)
		if reflect.DeepEqual(fv.Interface(), df.Interface()) && !f.Arg {
			continue
		}
		switch f.Kind {
		case Bool:
			if fv.Bool() {
				flags = append(flags, "--"+f.Name)
			}
		case Strings:
			for j := 0; j < fv.Len(); j++ {
				if f.Arg {
					args = append(args, Quote(fv.Index(j).String()))
				} else {
					flags = append(flags, "--"+f.Name, Quote(fv.Index(j).String()))
				}
			}
		default:
			var s string
			switch f.Kind {
			case Int:
				if f.unsigned {
					s = strconv.FormatUint(fv.Uint(), 10)
				} else {
					s = strconv.FormatInt(fv.Int(), 10)
				}
			case Float:
				switch {
				case f.ptr && fv.IsNil():
					s = ""
				case f.ptr:
					s = strconv.FormatFloat(fv.Elem().Float(), 'f', -1, 64)
				default:
					s = strconv.FormatFloat(fv.Float(), 'f', -1, 64)
				}
			default:
				s = fv.String()
			}
			switch {
			case s == "" || (s == "0" && (f.Kind == Int || f.Kind == Float)):
			case f.Arg:
				args = append(args, Quote(s))
			default:
				flags = append(flags, "--"+f.Name, Quote(s))
			}
		}
	}
	parts := append([]string{"bint", cliPath}, flags...)
	return strings.Join(append(parts, args...), " ")
}

// Quote đặt s trong dấu nháy đơn khi chứa ký tự mà shell hiểu đặc biệt.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,#-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// View vẽ biểu mẫu vào vùng env.W × env.H: các ô, lỗi, dòng lệnh CLI tương đương và gợi ý phím.
func (m *Model) View(env ui.Env, labelWidth int) string {
	th, w, h := env.Th, env.W, env.H
	rs := m.rows()
	m.clampFocus()
	lw := labelWidth
	if lw <= 0 {
		for _, r := range rs {
			if r.field >= 0 {
				lw = max(lw, text.Width(env.T(LabelKey(m.ActionID, m.Fields[r.field]))))
			}
		}
		lw = min(lw+1, max(w/3, 8))
	}
	vw := max(w-lw-4, 8)
	var lines []string
	focusLine := 0
	for ri, r := range rs {
		focused := ri == m.Focus
		if r.field < 0 {
			arrow := "▸"
			if m.Advanced {
				arrow = "▾"
			}
			l := th.S(theme.Accent, arrow+" "+env.T("form.advanced"))
			if focused {
				l = th.Selected(arrow + " " + env.T("form.advanced"))
				focusLine = len(lines)
			}
			lines = append(lines, "  "+l)
			continue
		}
		f := m.Fields[r.field]
		label := env.T(LabelKey(m.ActionID, f))
		if f.Required {
			label += "*"
		}
		lab := text.Pad(text.Truncate(label, lw), lw)
		marker := "  "
		if focused {
			marker = "▸ "
			lab = th.Selected(lab)
			focusLine = len(lines)
		} else {
			lab = th.S(theme.Accent, lab)
		}
		switch f.Kind {
		case Bool:
			box := "[ ]"
			if m.bools[r.field] {
				box = "[x]"
			}
			lines = append(lines, marker+lab+" "+box)
		case Choice:
			lines = append(lines, marker+lab+" "+m.choiceLine(env, r.field, f, focused, vw))
		default:
			if f.Multiline {
				body := m.inputs[r.field].Lines(th, vw, 4, focused)
				lines = append(lines, marker+lab+" "+body[0])
				for _, b := range body[1:] {
					lines = append(lines, strings.Repeat(" ", 2+lw+1)+b)
				}
			} else {
				lines = append(lines, marker+lab+" "+m.inputs[r.field].Line(th, vw, focused))
			}
		}
		if e, bad := m.FieldErr[r.field]; bad {
			lines = append(lines, strings.Repeat(" ", 2+lw+1)+th.S(theme.Err, "✗ "+env.T("form.error."+e)))
		}
	}
	if m.Err != "" {
		lines = append(lines, "", "  "+th.S(theme.Err, "✗ ")+text.Truncate(m.Err, max(w-6, 8)))
	}

	// Chân: gợi ý của ô đang chọn, lệnh CLI tương đương và phím. Phần trên cuộn theo ô đang chọn nếu thiếu chỗ.
	cli := m.CLI()
	hint := ""
	if r, ok := m.cur(); ok && r.field >= 0 {
		if k := LabelKey(m.ActionID, m.Fields[r.field]) + ".hint"; env.Cat.Has(k) {
			hint = "  " + th.S(theme.Muted, "ⓘ "+env.T(k))
		}
	}
	foot := []string{
		hint,
		"  " + th.S(theme.Muted, env.T("form.cli")+" ") + text.Truncate(strings.ReplaceAll(cli, "\n", "⏎"), max(w-4-text.Width(env.T("form.cli")), 8)),
		"  " + th.S(theme.Key, "Enter") + " " + th.S(theme.Muted, env.T("form.hint.submit")) + "  " + th.S(theme.Key, "Tab") + " " + th.S(theme.Muted, env.T("form.hint.next")) +
			"  " + th.S(theme.Key, "Ctrl+Y") + " " + th.S(theme.Muted, env.T("form.hint.copy")) + "  " + th.S(theme.Key, "Esc") + " " + th.S(theme.Muted, env.T("form.hint.cancel")),
	}
	room := max(h-len(foot)-1, 1)
	top := 0
	if len(lines) > room {
		top = max(0, min(focusLine-room/2, len(lines)-room))
		lines = lines[top : top+room]
	}
	for len(lines) < room {
		lines = append(lines, "")
	}
	out := append(append(lines, ""), foot...)
	for i, l := range out {
		out[i] = text.Truncate(l, w)
	}
	return strings.Join(out, "\n")
}

func (m *Model) choiceLine(env ui.Env, i int, f Field, focused bool, room int) string {
	th := env.Th
	var parts []string
	none := env.T("form.choice.none")
	opts := append([]string{none}, f.Choices...)
	for j, c := range opts {
		label := c
		if j > 0 && env.Cat.Has("form."+m.ActionID+"."+f.Name+"."+c) {
			label = env.T("form." + m.ActionID + "." + f.Name + "." + c)
		}
		switch {
		case j == m.choice[i]:
			s := "(●) " + label
			if focused {
				s = th.Selected(s)
			}
			parts = append(parts, s)
		case j == 0 && f.Required:
			continue // loại bắt buộc: không hiện lựa chọn "để trống"
		default:
			parts = append(parts, th.S(theme.Muted, "( ) "+label))
		}
	}
	if line := strings.Join(parts, " "); text.Width(line) <= room {
		return line
	}
	// Không đủ chỗ cho mọi lựa chọn: chỉ hiện lựa chọn đang chọn, giữa hai mũi tên (←/→ để đổi).
	cur := opts[m.choice[i]]
	if m.choice[i] > 0 && env.Cat.Has("form."+m.ActionID+"."+f.Name+"."+cur) {
		cur = env.T("form." + m.ActionID + "." + f.Name + "." + cur)
	}
	one := "‹ (●) " + cur + " ›"
	if focused {
		one = th.Selected(one)
	}
	return one
}
