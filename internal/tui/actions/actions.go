// Package actions là sổ hành động của TUI: mỗi hành động khai báo ID, màn hình, phím, độ ưu tiên ở
// chân phím, khoá nhãn i18n, đường lệnh CLI tương đương, kiểu Options của dịch vụ (để suy ra các cờ),
// cờ Writes (hành động ghi dữ liệu) và HumanOnly (chỉ dành cho người). Bảng lệnh, chân phím và trợ
// giúp đều sinh từ sổ này; chế độ chỉ đọc cũng chỉ dựa vào cờ Writes ở đây — màn hình không tự kiểm.
package actions

import (
	"context"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Screen là màn hình (tab) chứa hành động; rỗng = toàn cục.
type Screen string

const (
	Global    Screen = ""
	Overview  Screen = "overview"
	Knowledge Screen = "knowledge"
	Tasks     Screen = "tasks"
	Runs      Screen = "runs"
	Eval      Screen = "eval"
	Evolve    Screen = "evolve"
	System    Screen = "system"
	// Init là màn tạm "Khởi tạo với mặc định" của repository chưa init (không phải một tab).
	Init Screen = "init"
)

// Screens là bảy tab theo thứ tự hiển thị.
var Screens = []Screen{Overview, Knowledge, Tasks, Runs, Eval, Evolve, System}

// Env là ngữ cảnh chạy một hành động.
type Env struct {
	Dir      string // thư mục repository
	Version  string
	BintPath string // cách gọi bint cho hook
	// ReadOnly: TUI đang ở chế độ chỉ đọc (vd. thống kê không nạp spool vào cơ sở dữ liệu).
	ReadOnly bool
	// Editor dựng lệnh mở một tệp trong trình soạn thảo; nil = theo $VISUAL/$EDITOR (thay được trong kiểm thử).
	Editor func(path string) *exec.Cmd
}

// RunFunc chạy một hành động dịch vụ (chạy ngoài vòng Update, trong tea.Cmd) và trả thông điệp kết quả.
type RunFunc func(ctx context.Context, env Env) tea.Msg

// DoFunc chạy một hành động có tham số: opts là giá trị kiểu Options của hành động (Action.Options).
// Chạy ngoài vòng Update; trả thường là ResultMsg (hoặc ExecMsg để mô hình gốc chạy tiến trình tương tác).
type DoFunc func(ctx context.Context, env Env, opts any) tea.Msg

// RequestMsg là yêu cầu của một màn hình chạy hành động ID với tham số Opts. Mô hình gốc tra sổ
// (Check theo chế độ, applicable) rồi mới gọi Action.Do: sổ là nơi duy nhất quyết định chỉ-đọc/khoá
// (mục 7.4), màn hình không tự kiểm.
type RequestMsg struct {
	ID   string
	Opts any
}

// ResultMsg là kết quả của một RequestMsg đã chạy. Value là kết quả của dịch vụ (kiểu tuỳ hành động).
type ResultMsg struct {
	ID    string
	Opts  any
	Value any
	Err   error
}

// DeniedMsg báo yêu cầu bị từ chối vì hành động ẩn (chỉ đọc) hoặc khoá (chỉ dành cho người).
type DeniedMsg struct {
	ID     string
	Locked bool // true: khoá vì phiên agent; false: ẩn vì chế độ chỉ đọc
}

// ExecMsg yêu cầu mô hình gốc chạy một tiến trình tương tác (vd. $EDITOR) bằng tea.ExecProcess; xong thì
// gửi ResultMsg{ID, Opts, Err}.
type ExecMsg struct {
	ID   string
	Opts any
	Cmd  *exec.Cmd
}

// CancelMsg yêu cầu huỷ việc nền đang chạy của hành động ID (huỷ context đã truyền vào Do).
type CancelMsg struct{ ID string }

// Action là một hành động của TUI.
type Action struct {
	ID     string
	Screen Screen
	// Key là phím tắt hiển thị ("a", "Enter", "ctrl+k"); rỗng = chỉ có trong bảng lệnh.
	Key string
	// Footer: hiện ở chân phím. Priority nhỏ = quan trọng hơn; Pinned = luôn giữ.
	Footer   bool
	Priority int
	Pinned   bool
	// LabelKey là khoá i18n của nhãn ngắn (chân phím); TitleKey của tiêu đề đầy đủ (bảng lệnh, trợ giúp).
	LabelKey string
	TitleKey string
	// CLIPath là đường lệnh CLI tương đương ("knowledge approve"); rỗng = hành động giao diện thuần.
	CLIPath string
	// Options là giá trị mẫu (zero value) của kiểu Options của dịch vụ; nil nếu hành động không có tham số.
	Options any
	// Writes: hành động ghi dữ liệu (bị ẩn khi chế độ chỉ đọc). HumanOnly: chỉ dành cho người (khoá
	// kèm lý do khi TUI mở trong phiên agent).
	Writes    bool
	HumanOnly bool
	// FormID là không gian tên nhãn của form (khoá form.<FormID>.<trường>); rỗng = ID. Các hành động dùng chung
	// một Options (vd. duyệt và duyệt-làm-mới) dùng chung nhãn.
	FormID string
	// Run là hàm dịch vụ không tham số; nil với hành động giao diện thuần do mô hình gốc xử lý theo ID.
	Run RunFunc
	// Do là hàm dịch vụ có tham số (RequestMsg); nil với hành động chỉ có giao diện hoặc chỉ có Run.
	Do DoFunc
}

// FormNS trả không gian tên nhãn của form của hành động.
func (a Action) FormNS() string {
	if a.FormID != "" {
		return a.FormID
	}
	return a.ID
}

// Flags trả tên các cờ CLI (không gồm "--") suy ra từ thẻ `flag` của kiểu Options, kể cả trường nhúng.
func (a Action) Flags() []string { return FlagsOf(a.Options) }

// FlagsOf trả tên cờ trong thẻ `flag` của mọi trường (đệ quy qua trường nhúng/struct), sắp tăng dần.
func FlagsOf(opts any) []string {
	if opts == nil {
		return nil
	}
	t := reflect.TypeOf(opts)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []string
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if name := f.Tag.Get("flag"); name != "" {
				out = append(out, name)
			}
			if f.Anonymous {
				walk(f.Type)
			}
		}
	}
	walk(t)
	sort.Strings(out)
	return slices.Compact(out)
}

// Mode là chế độ làm việc quyết định hành động nào dùng được.
type Mode struct {
	// ReadOnly: ẩn mọi hành động ghi (Writes). Đây là chế độ tiện dụng, không phải ranh giới bảo mật.
	ReadOnly bool
	// Agent: TUI mở trong phiên agent; hành động HumanOnly bị khoá.
	Agent bool
}

// Availability là kết quả kiểm tra một hành động theo Mode.
type Availability int

const (
	Available Availability = iota
	// Hidden: không hiện (chế độ chỉ đọc ẩn hành động ghi).
	Hidden
	// Locked: hiện nhưng khoá (hành động chỉ dành cho người trong phiên agent).
	Locked
)

// Check cho biết hành động có dùng được trong mode không. Quy tắc chỉ nằm ở đây.
func (a Action) Check(m Mode) Availability {
	if m.ReadOnly && a.Writes {
		return Hidden
	}
	if m.Agent && a.HumanOnly {
		return Locked
	}
	return Available
}

// Registry là tập hành động.
type Registry struct{ list []Action }

// NewRegistry dựng sổ từ danh sách; ID phải duy nhất (panic nếu trùng: lỗi lập trình).
func NewRegistry(list []Action) *Registry {
	seen := map[string]bool{}
	for _, a := range list {
		if a.ID == "" || seen[a.ID] {
			panic("actions: empty or duplicate ID: " + a.ID)
		}
		seen[a.ID] = true
	}
	return &Registry{list: slices.Clone(list)}
}

// All trả mọi hành động theo thứ tự khai báo.
func (r *Registry) All() []Action { return slices.Clone(r.list) }

// ByID tìm hành động theo ID.
func (r *Registry) ByID(id string) (Action, bool) {
	for _, a := range r.list {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

// For trả hành động của màn hình s và toàn cục, đã lọc theo mode (bỏ Hidden; Locked vẫn trả để hiện khoá).
func (r *Registry) For(s Screen, m Mode) []Action {
	var out []Action
	for _, a := range r.list {
		if (a.Screen == s || a.Screen == Global) && a.Check(m) != Hidden {
			out = append(out, a)
		}
	}
	return out
}

// Visible trả mọi hành động thấy được trong mode (cho bảng lệnh và trợ giúp).
func (r *Registry) Visible(m Mode) []Action {
	var out []Action
	for _, a := range r.list {
		if a.Check(m) != Hidden {
			out = append(out, a)
		}
	}
	return out
}

// Keywords là các từ khoá tìm kiếm của hành động ngoài tiêu đề: đường lệnh CLI và tên các cờ
// (--budget), để tìm theo tên cờ cũng ra hành động.
func (a Action) Keywords() string {
	var kw []string
	if a.CLIPath != "" {
		kw = append(kw, "bint "+a.CLIPath)
	}
	for _, f := range a.Flags() {
		kw = append(kw, "--"+f)
	}
	return strings.Join(kw, " ")
}
