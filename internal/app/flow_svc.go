package app

// Dịch vụ flow (GĐ2 của docs/custom-flow-plan.md): đọc và kiểm định nghĩa flow cho `bint flow list|show|lint` và
// các hành động chỉ đọc tương ứng của TUI. Runner chưa đọc flow nên không có thao tác ghi hay chạy.

import (
	"context"
	"errors"
	"os"
	"slices"

	"bint/internal/config"
	"bint/internal/flow"
	"bint/internal/skills"
	"bint/internal/workflow"
)

// Nguồn của một flow.
const (
	FlowBuiltin = "builtin"
	FlowCustom  = "custom"
)

// flowSkillExists: skill gốc, hoặc skill tuỳ biến qua kiểm hình thức không lỗi. Dùng chung với Runner
// (workflow.FlowSkillExists) để `bint flow lint` và `bint run <flow>` cùng một luật.
func flowSkillExists(root string) func(string) bool { return workflow.FlowSkillExists(root) }

// lintFlow kiểm một flow (nhúng hoặc từ tệp). file là tên tệp không đuôi ("" với flow nhúng).
func lintFlow(root, file string, d flow.Def, exists func(string) bool) (errs, warns []string) {
	errs = flow.Validate(d, file, exists)
	// Flow xuất bản thành skill cùng tên: Validate chỉ thấy skill hợp lệ, còn skill tuỳ biến lỗi lint và skill
	// tiến hoá cùng tên cũng sẽ đè lên bản xuất bản của flow.
	if file != "" && d.Name != "" && !exists(d.Name) {
		_, evErr := os.Stat(skills.EvolvedDir(root, d.Name))
		if skills.CustomExists(root, d.Name) || evErr == nil {
			errs = append(errs, "name: \""+d.Name+"\" trùng tên một skill tuỳ biến hoặc skill tiến hoá")
		}
	}
	return errs, flow.Warnings(d)
}

// ---- list

// FlowListOptions: `bint flow list`.
type FlowListOptions struct{}

func FlowListDefaults(config.Config) FlowListOptions { return FlowListOptions{} }
func (o FlowListOptions) Validate() error            { return nil }

// FlowRow là một dòng của `bint flow list`.
type FlowRow struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"` // FlowBuiltin | FlowCustom
	Path        string   `json:"path,omitempty"`
	Description string   `json:"description,omitempty"`
	Steps       int      `json:"steps"`
	OK          bool     `json:"ok"` // qua lint (cảnh báo không tính)
	Problems    []string `json:"problems"`
}

// ListFlows liệt kê flow nhúng rồi flow tuỳ biến (theo tên tệp), kèm trạng thái lint của từng flow.
func ListFlows(ctx context.Context, dir string, o FlowListOptions) ([]FlowRow, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	exists := flowSkillExists(k.Root)
	rows := []FlowRow{}
	for _, d := range flow.Builtin() {
		errs, _ := lintFlow(k.Root, "", d, exists)
		rows = append(rows, FlowRow{Name: d.Name, Source: FlowBuiltin, Description: d.Description,
			Steps: len(d.Steps), OK: len(errs) == 0, Problems: nonNil(errs)})
	}
	files, err := flow.Load(k.Root)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		row := FlowRow{Name: f.Name, Source: FlowCustom, Path: f.Path}
		if f.Err != nil {
			row.Problems = []string{f.Err.Error()}
		} else {
			errs, _ := lintFlow(k.Root, f.Name, f.Def, exists)
			row.Description, row.Steps, row.OK, row.Problems = f.Def.Description, len(f.Def.Steps), len(errs) == 0, nonNil(errs)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ---- show

// FlowShowOptions: `bint flow show <tên>`.
type FlowShowOptions struct {
	Name string `arg:"name" form:"required"`
}

func FlowShowDefaults(config.Config) FlowShowOptions { return FlowShowOptions{} }

func (o FlowShowOptions) Validate() error {
	if o.Name == "" {
		return Usagef("cần tên flow")
	}
	return nil
}

// FlowShowResult là định nghĩa đã chuẩn hoá của một flow.
type FlowShowResult struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Path     string   `json:"path,omitempty"`
	Hash     string   `json:"hash"`
	Def      flow.Def `json:"definition"`
	YAML     string   `json:"yaml"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// ShowFlow trả định nghĩa chuẩn hoá (YAML + băm) kèm kết quả lint. Flow tuỳ biến không lint sạch vẫn được in để
// người dùng thấy cái cần sửa; tệp không giải mã được thì trả lỗi.
func ShowFlow(ctx context.Context, dir string, o FlowShowOptions) (FlowShowResult, error) {
	if err := o.Validate(); err != nil {
		return FlowShowResult{}, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return FlowShowResult{}, err
	}
	d, file, path, err := findFlow(k.Root, o.Name)
	if err != nil {
		return FlowShowResult{}, err
	}
	y, err := flow.Marshal(d)
	if err != nil {
		return FlowShowResult{}, err
	}
	res := FlowShowResult{Name: d.Name, Source: FlowCustom, Path: path, Hash: flow.Hash(d), Def: flow.Normalize(d), YAML: string(y)}
	if d.IsBuiltin() {
		res.Source = FlowBuiltin
	}
	errs, warns := lintFlow(k.Root, file, d, flowSkillExists(k.Root))
	res.Errors, res.Warnings = nonNil(errs), nonNil(warns)
	return res, nil
}

// findFlow tra flow theo tên (nhúng trước; cùng đường tra với flow.Get). file là tên tệp không đuôi của flow tuỳ biến,
// path là đường dẫn tệp. Không có flow hoặc tệp không nạp được → lỗi cách dùng.
func findFlow(root, name string) (d flow.Def, file, path string, err error) {
	f, err := flow.Find(root, name)
	switch {
	case errors.Is(err, flow.ErrNotFound):
		return d, "", "", Usagef("không có flow %q (xem `bint flow list`)", name)
	case err != nil:
		return d, "", "", err
	case f.Err != nil:
		return d, "", "", Usagef("%s: %v", f.Path, f.Err)
	case f.Def.IsBuiltin():
		return f.Def, "", "", nil
	}
	return f.Def, f.Name, f.Path, nil
}

// ---- lint

// FlowLintOptions: `bint flow lint [tên]` (không tên = mọi flow tuỳ biến).
type FlowLintOptions struct {
	Name string `arg:"name"`
}

func FlowLintDefaults(config.Config) FlowLintOptions { return FlowLintOptions{} }
func (o FlowLintOptions) Validate() error            { return nil }

// FlowLintResult là kết quả kiểm một flow.
type FlowLintResult struct {
	Name     string   `json:"name"`
	Source   string   `json:"source"`
	Path     string   `json:"path,omitempty"` // rỗng với flow nhúng
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// OK cho biết flow không có lỗi (cảnh báo không tính).
func (r FlowLintResult) OK() bool { return len(r.Errors) == 0 }

// LintFlows kiểm một flow theo tên (nhúng hoặc tuỳ biến), hoặc mọi flow tuỳ biến khi Name rỗng. Tên không có → lỗi
// cách dùng. Tệp không giải mã được (khoá YAML lạ, đuôi .yaml, symlink...) là lỗi của flow đó, không phải lỗi lệnh.
func LintFlows(ctx context.Context, dir string, o FlowLintOptions) ([]FlowLintResult, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	exists := flowSkillExists(k.Root)
	out := []FlowLintResult{}

	if b, ok := flow.GetBuiltin(o.Name); ok {
		// Flow nhúng được tra trước; tệp .bint/flows/<tên>.yml trùng tên (luôn là lỗi) cũng được kiểm ngay sau đó
		// để lint theo tên không che nó.
		errs, warns := lintFlow(k.Root, "", b, exists)
		out = append(out, FlowLintResult{Name: b.Name, Source: FlowBuiltin, Errors: nonNil(errs), Warnings: nonNil(warns)})
	}
	files, err := flow.Load(k.Root)
	if err != nil {
		return nil, err
	}
	if o.Name != "" {
		files = slices.DeleteFunc(files, func(f flow.File) bool { return f.Name != o.Name })
		if len(files) == 0 {
			if len(out) > 0 {
				return out, nil
			}
			return nil, Usagef("không có flow %q (xem `bint flow list`)", o.Name)
		}
	}
	for _, f := range files {
		r := FlowLintResult{Name: f.Name, Source: FlowCustom, Path: f.Path}
		if f.Err != nil {
			r.Errors, r.Warnings = []string{f.Err.Error()}, []string{}
		} else {
			errs, warns := lintFlow(k.Root, f.Name, f.Def, exists)
			r.Errors, r.Warnings = nonNil(errs), nonNil(warns)
		}
		out = append(out, r)
	}
	return out, nil
}
