package config

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpgradeResult là kết quả nâng cấu hình cũ lên mẫu hiện tại.
type UpgradeResult struct {
	// Content là nội dung mới (bằng nội dung cũ khi không có gì thay đổi).
	Content []byte
	// Added liệt kê khoá đã thêm (đường dẫn dạng a.b.c), theo thứ tự trong mẫu.
	Added []string
	// Notes là ghi chú cho người dùng (giữ model chung đã đặt tay, khoá phải thêm tay…).
	Notes []string
	// Fixed: số dòng chú thích hỏng do mẫu cũ sinh ra đã được sửa.
	Fixed int
	// Migrated liệt kê các giá trị mẫu cũ được nâng có chọn lọc.
	Migrated []string
}

// Changed cho biết nội dung có thay đổi.
func (r UpgradeResult) Changed() bool { return len(r.Added) > 0 || r.Fixed > 0 || len(r.Migrated) > 0 }

// templateFixes sửa chú thích hỏng mà mẫu của bản Bint cũ đã ghi ra (ký tự % đi qua
// Fprintf: "+10 điểm % không" thành "+10 điểm %!k(MISSING)hông").
var templateFixes = strings.NewReplacer("%!k(MISSING)hông", "% không")

// Upgrade thêm vào cấu hình cũ (old) mọi khoá có trong mẫu Render nhưng còn thiếu,
// kèm chú thích của mẫu; khoá mới đứng ngay sau khoá đi trước nó trong mẫu.
// Ngoài việc thêm khoá, sửa chú thích mẫu cũ bị hỏng và nâng readonly_tools khi
// danh sách khớp chính xác mẫu lịch sử. Danh sách tuỳ biến được giữ nguyên; các
// dòng ngoài các vùng thay đổi được giữ nguyên từng byte.
//
// Ngoại lệ giữ hành vi cũ: harness đã đặt tay model chung (workflow.agent.model với
// Claude, harnesses.<h>.model với Codex/Antigravity) mà chưa có khối models thì khối
// models được thêm với giá trị rỗng, để model chung đó vẫn dùng cho mọi role.
func Upgrade(old []byte) (UpgradeResult, error) {
	return UpgradeWith(old, UpgradeSuggestions{})
}

// UpgradeSuggestions là các lệnh phát hiện được ở repository, được thêm như khoá suggested_* còn thiếu khi nâng (hiện không có).
type UpgradeSuggestions struct{}

// UpgradeWith như Upgrade, kèm đề xuất sug.
func UpgradeWith(old []byte, sug UpgradeSuggestions) (UpgradeResult, error) {
	res := UpgradeResult{Content: old}
	cur := Default()
	if err := Decode(bytes.NewReader(old), &cur); err != nil {
		return res, fmt.Errorf("đọc cấu hình hiện có: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(old, &doc); err != nil {
		return res, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return res, errors.New("cấu hình hiện có không phải một ánh xạ YAML")
	}
	p := InitParams{ProjectName: cur.Project.Name, Harnesses: cur.Harnesses.Enabled, DefaultHarness: cur.Harnesses.Default,
		KeepGenericModel: genericModels(cur)}
	tmplText := Render(p)
	var tmpl yaml.Node
	if err := yaml.Unmarshal([]byte(tmplText), &tmpl); err != nil {
		return res, fmt.Errorf("mẫu cấu hình: %w", err)
	}

	u := &upgrader{
		oldLines:  splitLines(string(old)),
		tmplLines: splitLines(tmplText),
		inserts:   map[int][]string{},
	}
	if u.upgradeReadonlySequence(doc.Content[0]) {
		res.Migrated = append(res.Migrated, "workflow.agent.readonly_tools")
		res.Notes = append(res.Notes, "đã nâng danh sách readonly_tools theo mẫu cũ; xem diff trước khi áp dụng")
	}
	for i, l := range u.oldLines {
		if fixed := templateFixes.Replace(l); fixed != l {
			u.oldLines[i] = fixed
			res.Fixed++
		}
	}
	u.merge(doc.Content[0], tmpl.Content[0], "", 0)
	res.Notes = append(res.Notes, u.notes...)
	if keys := cur.Workflow.Verify.DeprecatedVerifyKeys(); len(keys) > 0 {
		res.Notes = append(res.Notes, strings.Join(keys, ", ")+" không còn dùng (bgraph chọn test bị ảnh hưởng cho mọi lần chạy test); có thể xoá")
	}
	if len(u.added) == 0 && res.Fixed == 0 && len(res.Migrated) == 0 {
		return res, nil
	}
	for h, path := range p.KeepGenericModel {
		for _, a := range u.added {
			if a == "harnesses."+h || a == "harnesses."+h+".models" {
				res.Notes = append(res.Notes, fmt.Sprintf("harnesses.%s.models để trống: giữ %s đã đặt cho mọi role; điền models.<role> để chọn model theo bước", h, path))
				break
			}
		}
	}
	sort.Strings(res.Notes)

	out := u.apply()
	if !bytes.HasSuffix(old, []byte("\n")) {
		out = strings.TrimSuffix(out, "\n")
	}
	check := Default()
	if err := Decode(strings.NewReader(out), &check); err != nil {
		return res, fmt.Errorf("cấu hình sau khi nâng không giải mã được: %w", err)
	}
	if err := check.Validate(); err != nil {
		return res, err
	}
	res.Content, res.Added = []byte(out), u.added
	return res, nil
}

// Chỉ nâng sequence khớp toàn bộ mẫu lịch sử, không union quyền tuỳ biến.
func (u *upgrader) upgradeReadonlySequence(root *yaml.Node) bool {
	lines := u.oldLines
	n := root
	for _, key := range []string{"workflow", "agent", "readonly_tools"} {
		if n.Kind != yaml.MappingNode {
			return false
		}
		i := mapIndex(n, key)
		if i < 0 {
			return false
		}
		n = n.Content[i+1]
	}
	if n.Kind != yaml.SequenceNode || n.Line < 1 || n.Line > len(lines) {
		return false
	}
	var got []string
	for _, item := range n.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return false
		}
		got = append(got, item.Value)
	}
	base := []string{"Read", "Glob", "Grep", "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)"}
	legacy := append(slices.Clone(base), "mcp__bint__*")
	legacyGrep := append(slices.Clone(base), "Bash(grep:*)", "Bash(git grep:*)", "mcp__bint__*")
	if !slices.Equal(got, legacy) && !slices.Equal(got, legacyGrep) {
		return false
	}
	// Danh sách khớp mẫu cũ nhưng bố cục không sửa tự động được: báo để người dùng tự nâng, không im lặng bỏ qua.
	manual := func() bool {
		u.notes = append(u.notes, "workflow.agent.readonly_tools theo mẫu cũ nhưng bố cục không tự sửa được — thêm tay "+
			`"Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)" (thiếu thì b-req/b-review bị chặn các lệnh này)`)
		return false
	}
	if n.Style&yaml.FlowStyle == 0 {
		last := n.Content[len(n.Content)-1]
		if last.Line < 1 || last.Line > len(lines) {
			return manual()
		}
		line := lines[last.Line-1]
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if !strings.HasPrefix(line[indent:], "- ") {
			return manual()
		}
		// Giữ kiểu xuống dòng của tệp: apply() nối bằng \n, dòng cũ CRLF còn giữ \r.
		eol := ""
		if strings.HasSuffix(line, "\r") {
			eol = "\r"
		}
		prefix := strings.Repeat(" ", indent) + "- "
		var add []string
		if slices.Equal(got, legacy) {
			add = append(add, prefix+`"Bash(grep:*)"`+eol, prefix+`"Bash(git grep:*)"`+eol)
		}
		add = append(add, prefix+`"Bash(git ls-files:*)"`+eol)
		u.inserts[last.Line-1] = append(u.inserts[last.Line-1], add...)
		return true
	}
	for _, item := range n.Content {
		if item.Line != n.Line {
			return manual()
		}
	}
	line := lines[n.Line-1]
	start := n.Column - 1
	if start < 0 || start >= len(line) || line[start] != '[' {
		return manual()
	}
	end := strings.IndexByte(line[start:], ']')
	if end < 0 {
		return manual()
	}
	const replacement = `[Read, Glob, Grep, "Bash(bint:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(grep:*)", "Bash(git grep:*)", "Bash(git ls-files:*)", "mcp__bint__*"]`
	lines[n.Line-1] = line[:start] + replacement + line[start+end+1:]
	return true
}

// genericModels: harness → đường dẫn khoá model chung đã được đặt tay (khác null/rỗng).
func genericModels(c Config) map[string]string {
	out := map[string]string{}
	set := func(m *string) bool { return m != nil && *m != "" }
	if set(c.Workflow.Agent.Model) {
		out["claude"] = "workflow.agent.model"
	}
	if set(c.Harnesses.Codex.Model) {
		out["codex"] = "harnesses.codex.model"
	}
	if set(c.Harnesses.Antigravity.Model) {
		out["antigravity"] = "harnesses.antigravity.model"
	}
	return out
}

type upgrader struct {
	oldLines, tmplLines []string
	// inserts: chỉ số dòng (0-based) của tệp cũ → các dòng chèn ngay TRƯỚC dòng đó.
	inserts map[int][]string
	added   []string
	notes   []string
}

// merge chèn các khoá của src còn thiếu trong dst (đệ quy qua ánh xạ lồng nhau).
// parentEnd là dòng (1-based) cuối của khối chứa dst; 0 = gốc tài liệu.
func (u *upgrader) merge(dst, src *yaml.Node, prefix string, depth int) {
	if dst.Kind != yaml.MappingNode || src.Kind != yaml.MappingNode {
		return
	}
	prev := -1 // chỉ số khoá (trong dst.Content) của khoá có mặt gần nhất phía trước
	for i := 0; i+1 < len(src.Content); i += 2 {
		k, v := src.Content[i], src.Content[i+1]
		path := k.Value
		if prefix != "" {
			path = prefix + "." + k.Value
		}
		if j := mapIndex(dst, k.Value); j >= 0 {
			u.merge(dst.Content[j+1], v, path, depth+1)
			prev = j
			continue
		}
		if dst.Style&yaml.FlowStyle != 0 || len(dst.Content) == 0 {
			u.notes = append(u.notes, fmt.Sprintf("%s: không chèn tự động được vào ánh xạ dạng {…} — thêm tay (xem `bint init` trên repository mới)", path))
			continue
		}
		// Vị trí chèn: sau khối của khoá đứng trước, hoặc trước khoá đầu tiên (kèm chú thích đầu của nó).
		var at int
		if prev >= 0 {
			at = u.blockEnd(dst.Content[prev], dst.Content[prev+1]) // dòng 1-based cuối khối → chèn trước dòng kế
		} else {
			first := dst.Content[0]
			at = first.Line - 1 - commentLines(first.HeadComment)
		}
		col := dst.Content[0].Column
		block := u.templateBlock(k, v, col)
		if depth == 0 {
			block = append([]string{""}, block...)
		}
		u.inserts[at] = append(u.inserts[at], block...)
		u.added = append(u.added, path)
	}
}

// blockEnd trả về dòng cuối (1-based) của khoá k trong tệp cũ: dòng lớn nhất của cây
// con, kéo dài qua các dòng không trống thụt sâu hơn khoá (scalar khối, chú thích nối dòng).
func (u *upgrader) blockEnd(k, v *yaml.Node) int {
	end := maxLine(v)
	if k.Line > end {
		end = k.Line
	}
	for end < len(u.oldLines) {
		next := u.oldLines[end]
		if strings.TrimSpace(next) == "" || indentOf(next) <= k.Column {
			break
		}
		end++
	}
	return end
}

// templateBlock lấy các dòng của khoá k trong mẫu (kèm chú thích đầu), thụt lại cho
// khớp cột col của ánh xạ đích trong tệp cũ.
func (u *upgrader) templateBlock(k, v *yaml.Node, col int) []string {
	start := k.Line - 1 - commentLines(k.HeadComment) // 0-based
	end := maxLine(v)
	if k.Line > end {
		end = k.Line
	}
	for end < len(u.tmplLines) {
		next := u.tmplLines[end]
		if strings.TrimSpace(next) == "" || indentOf(next) <= k.Column {
			break
		}
		end++
	}
	shift := col - k.Column
	var out []string
	for _, l := range u.tmplLines[start:end] {
		switch {
		case shift > 0 && l != "":
			l = strings.Repeat(" ", shift) + l
		case shift < 0:
			l = l[min(-shift, indentOf(l)-1):]
		}
		out = append(out, l)
	}
	return out
}

func (u *upgrader) apply() string {
	var b strings.Builder
	for i := 0; i <= len(u.oldLines); i++ {
		for _, l := range u.inserts[i] {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		if i < len(u.oldLines) {
			b.WriteString(u.oldLines[i])
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func maxLine(n *yaml.Node) int {
	m := n.Line
	for _, c := range n.Content {
		if l := maxLine(c); l > m {
			m = l
		}
	}
	return m
}

func mapIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func commentLines(c string) int {
	if c == "" {
		return 0
	}
	return strings.Count(c, "\n") + 1
}

// indentOf trả về cột (1-based) của ký tự không trắng đầu tiên.
func indentOf(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) + 1 }

// splitLines tách theo '\n' (dòng cuối rỗng do '\n' kết thúc tệp bị bỏ).
func splitLines(s string) []string {
	ls := strings.Split(s, "\n")
	if len(ls) > 0 && ls[len(ls)-1] == "" {
		ls = ls[:len(ls)-1]
	}
	return ls
}

// SummarizeAdded gộp danh sách khoá đã thêm thành chuỗi ngắn (bỏ khoá con khi đã có khoá cha).
func SummarizeAdded(added []string) string {
	var out []string
	for _, a := range added {
		covered := false
		for _, o := range out {
			if strings.HasPrefix(a, o+".") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, a)
		}
	}
	return strings.Join(out, ", ")
}
