package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/config"
	"bint/internal/harness/antigravity"
	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/harness/hookcore"
	"bint/internal/hcomp"
)

// ComponentWS là thư viện thành phần quy trình (.bint/harness) của repository.
type ComponentWS struct {
	Root   string
	Dir    string
	Config config.Config
	Lib    *hcomp.Library
}

// OpenComponents mở thư viện thành phần của repository chứa dir (cần đã `bint init`).
func OpenComponents(ctx context.Context, dir string) (*ComponentWS, error) {
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(filepath.Dir(ws.Sources.Project))
	return &ComponentWS{Root: root, Dir: hcomp.Dir(root), Config: ws.Config, Lib: hcomp.Load(root)}, nil
}

// Reload đọc lại thư viện sau khi ghi.
func (w *ComponentWS) Reload() { w.Lib = hcomp.Load(w.Root) }

// Resolve tìm thành phần theo id; kind rỗng thì tìm ở mọi loại và báo lỗi nếu mơ hồ.
func (w *ComponentWS) Resolve(kind hcomp.Kind, id string) (hcomp.Component, error) {
	var found []hcomp.Component
	for _, c := range w.Lib.Components {
		if c.ID == id && (kind == "" || c.Kind == kind) {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return hcomp.Component{}, fmt.Errorf("không có thành phần %q trong .bint/harness (xem `bint harness component list`)", id)
	case 1:
		return found[0], nil
	}
	var kinds []string
	for _, c := range found {
		kinds = append(kinds, string(c.Kind))
	}
	return hcomp.Component{}, fmt.Errorf("id %q có ở nhiều loại (%s): chỉ định --kind", id, strings.Join(kinds, ", "))
}

// AddComponentOptions điều khiển `bint harness component add`.
type AddComponentOptions struct {
	Kind hcomp.Kind
	// File: tệp thành phần do người soạn (guidance .md; guardrail/script/strategy .yml).
	File string
	// ScriptFile: nội dung .sh của script (mặc định: tệp cùng tên với File, đuôi .sh, nếu có).
	ScriptFile string
	// Template: tên mẫu dựng sẵn (thay cho File).
	Template string
}

// AddComponent thêm một thành phần ở trạng thái candidate. Origin theo người gọi (agent nếu
// đang trong phiên agent); status luôn là candidate dù tệp ghi gì — agent không tự cho
// thành phần có hiệu lực, người duyệt bằng `approve`.
func AddComponent(ctx context.Context, w *ComponentWS, o AddComponentOptions) (hcomp.Component, error) {
	var c hcomp.Component
	switch {
	case o.Template != "":
		t, ok := hcomp.Template(o.Template)
		if !ok {
			return c, fmt.Errorf("không có mẫu %q", o.Template)
		}
		c = t
	case o.File == "":
		return c, errors.New("cần --file <tệp> hoặc --template <tên>")
	default:
		data, err := os.ReadFile(o.File)
		if err != nil {
			return c, err
		}
		switch o.Kind {
		case hcomp.KindGuidance:
			c, err = hcomp.ParseGuidance(data)
		case hcomp.KindGuardrail:
			c, err = hcomp.ParseGuardrail(data)
		case hcomp.KindStrategy:
			c, err = hcomp.ParseStrategy(data)
		case hcomp.KindScript:
			src := o.ScriptFile
			if src == "" {
				if alt := strings.TrimSuffix(o.File, filepath.Ext(o.File)) + ".sh"; fileExists(alt) {
					src = alt
				}
			}
			var body []byte
			if src != "" {
				if body, err = os.ReadFile(src); err != nil {
					return c, err
				}
			}
			c, err = hcomp.ParseScript(data, body)
		default:
			return c, fmt.Errorf("cần --kind guidance|guardrail|script|strategy, nhận %q", o.Kind)
		}
		if err != nil {
			return c, fmt.Errorf("%s: %w", o.File, err)
		}
	}
	c.Status, c.ApprovedBy = hcomp.StatusCandidate, ""
	c.Origin = hcomp.OriginHuman
	if in, _ := InAgentSession(); in {
		c.Origin = hcomp.OriginAgent
	}
	if c.Version < 1 {
		c.Version = 1
	}
	if _, ok := w.Lib.Find(c.Kind, c.ID); ok {
		return c, fmt.Errorf("%s %q đã có; sửa tệp trực tiếp hoặc đề xuất qua `bint evolve`", c.Kind, c.ID)
	}
	if err := hcomp.Check(c); err != nil {
		return c, err
	}
	if cf := hcomp.FindConflict(w.Lib.Components, c); cf != nil {
		return c, errors.New(cf.String())
	}
	if _, err := hcomp.Save(w.Dir, c); err != nil {
		return c, err
	}
	w.Reload()
	return c, nil
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// ReviewComponent duyệt (candidate → verified) hoặc loại bỏ (→ deprecated) một thành phần.
// Chỉ người làm được; chạy từ trong phiên agent bị từ chối.
func ReviewComponent(ctx context.Context, w *ComponentWS, kind hcomp.Kind, id, to string) (hcomp.Component, error) {
	op := "bint harness component approve"
	if to == hcomp.StatusDeprecated {
		op = "bint harness component deprecate"
	}
	var zero hcomp.Component
	if err := requireHuman(op); err != nil {
		return zero, err
	}
	c, err := w.Resolve(kind, id)
	if err != nil {
		return zero, err
	}
	switch {
	case to == hcomp.StatusVerified && c.Status != hcomp.StatusCandidate:
		return zero, fmt.Errorf("%s %q đang %s: chỉ duyệt được thành phần candidate", c.Kind, c.ID, c.Status)
	case to == hcomp.StatusDeprecated && c.Status == hcomp.StatusDeprecated:
		return zero, fmt.Errorf("%s %q đã deprecated", c.Kind, c.ID)
	}
	if to == hcomp.StatusVerified {
		if cf := hcomp.FindConflict(w.verifiedOnly(), c); cf != nil {
			return zero, fmt.Errorf("không duyệt được: %s", cf)
		}
	}
	c.Status, c.ApprovedBy = to, GitIdentity(ctx, w.Root)
	if _, err := hcomp.Save(w.Dir, c); err != nil {
		return zero, err
	}
	w.Reload()
	return c, nil
}

func (w *ComponentWS) verifiedOnly() []hcomp.Component {
	var out []hcomp.Component
	for _, c := range w.Lib.Components {
		if c.Status == hcomp.StatusVerified {
			out = append(out, c)
		}
	}
	return out
}

// dialectOf trả về Dialect của harness (claude | codex | antigravity).
func dialectOf(name string) (hookcore.Dialect, bool) {
	switch name {
	case claude.HarnessName:
		return claude.Dialect, true
	case codex.HarnessName:
		return codex.Dialect, true
	case antigravity.HarnessName:
		return antigravity.Dialect, true
	}
	return hookcore.Dialect{}, false
}

// RenderOptions điều khiển `bint harness render`.
type RenderOptions struct {
	Harness string
	Mode    hcomp.Mode
	// Paths: tệp giả định đang đọc/sửa, để thấy guidance riêng nào được chèn.
	Paths []string
}

// RenderHarness mô tả agent sẽ nhận gì từ thành phần quy trình trên một harness: khối chỉ
// dẫn chung trong AGENTS.md, guidance riêng theo đường dẫn, guardrail (kèm mức hạ theo khả
// năng của harness) và chiến lược ngữ cảnh đang áp.
func RenderHarness(w *ComponentWS, o RenderOptions) (string, error) {
	if o.Harness == "" {
		o.Harness = claude.HarnessName
	}
	d, ok := dialectOf(o.Harness)
	if !ok {
		return "", fmt.Errorf("harness không hỗ trợ %q", o.Harness)
	}
	if o.Mode == "" {
		o.Mode = hcomp.ModeOn
	}
	set := w.Lib.Select(o.Mode).Files()
	ro := hcomp.RenderOpts{MCP: w.Config.Context.Interfaces.MCP}
	var b strings.Builder
	fmt.Fprintf(&b, "Harness: %s · chế độ: %s · %d thành phần có hiệu lực\n", o.Harness, o.Mode, len(set.Components))
	for _, p := range w.Lib.Problems {
		fmt.Fprintf(&b, "! bỏ qua %s\n", p)
	}

	b.WriteString("\n── AGENTS.md (chỉ dẫn quy trình chung) ──\n")
	if g := set.RenderGuidance(ro); g != "" {
		b.WriteString(strings.TrimLeft(g, "\n"))
	} else {
		b.WriteString("(không có)\n")
	}

	b.WriteString("\n── Chỉ dẫn riêng (chèn qua hook khi đường dẫn khớp) ──\n")
	hasDomain := false
	for _, c := range set.Guidance() {
		if c.Domain() == "" {
			continue
		}
		hasDomain = true
		match := "khớp: " + strings.Join(matching(c, o.Paths), ", ")
		if len(o.Paths) == 0 {
			match = "chưa cho --paths"
		} else if len(matching(c, o.Paths)) == 0 {
			match = "không khớp --paths"
		}
		fmt.Fprintf(&b, "- %s (%s) — %s\n", c.ID, c.Scope, match)
	}
	if len(o.Paths) > 0 {
		if text := hcomp.RenderDomain(set.DomainGuidance(o.Paths, ro, nil)); text != "" {
			b.WriteString("\n" + text + "\n")
		}
	}
	if !hasDomain {
		b.WriteString("(không có)\n")
	}

	b.WriteString("\n── Guardrail ──\n")
	gs := set.Guardrails()
	if len(gs) == 0 {
		b.WriteString("(không có)\n")
	}
	for _, g := range gs {
		eff := d.GuardCaps(g.On).Degrade(g.Action)
		switch {
		case eff == "":
			eff = "không thực thi (harness không hỗ trợ)"
		case eff != g.Action:
			eff += " (hạ từ " + g.Action + ")"
		}
		fmt.Fprintf(&b, "- %s [%s] on=%s khai báo=%s → %s · %s\n", g.ID, g.Scope, g.On, g.Action, eff, g.Message)
	}

	b.WriteString("\n── Chiến lược ngữ cảnh ──\n")
	if s := set.Strategy(); s != nil && s.Status == hcomp.StatusVerified {
		fmt.Fprintf(&b, "strategy.yml (verified) đang ghi đè context.*: max_tokens=%d path_max_items=%d lessons.global_k=%d lessons.max_tokens=%d mcp=%v path_hooks=%v\n",
			w.Config.Context.MaxTokens, w.Config.Context.PathMaxItems, w.Config.Context.Lessons.GlobalK,
			w.Config.Context.Lessons.MaxTokens, w.Config.Context.Interfaces.MCP, w.Config.Context.Interfaces.PathHooks)
	} else if s != nil {
		b.WriteString("strategy.yml (candidate) chỉ áp trong eval `bint@harness=all`, không áp ở phiên thật\n")
	} else {
		b.WriteString("(không có strategy.yml hiệu lực; dùng context.* trong cấu hình)\n")
	}
	return b.String(), nil
}

func matching(c hcomp.Component, paths []string) []string {
	var out []string
	for _, p := range paths {
		if c.Matches(p) {
			out = append(out, p)
		}
	}
	return out
}
