package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Layer là lớp cấu hình mà người dùng sửa được từ TUI (lớp toàn cục chỉ là nguồn để đọc).
type Layer string

const (
	// LayerProject: .bint/config.yml (commit, cả nhóm dùng).
	LayerProject Layer = "project"
	// LayerLocal: .bint/local.yml (chỉ máy này, đè lớp dự án).
	LayerLocal Layer = "local"
)

// Valid cho biết giá trị là một lớp sửa được.
func (l Layer) Valid() bool { return l == LayerProject || l == LayerLocal }

// LayerPath trả đường dẫn tệp của lớp trong repository có thư mục gốc root.
func LayerPath(root string, l Layer) string {
	if l == LayerLocal {
		return LocalPath(root)
	}
	return ProjectPath(root)
}

// ValidateWith kiểm tra cấu hình sẽ có hiệu lực nếu nội dung của lớp layer được thay bằng data: ghép lớp
// toàn cục, dự án, strategy và cục bộ đúng như Load rồi chạy Validate(). Trả cấu hình đã ghép; lỗi nêu tệp
// gây ra. Không đọc ghi gì ngoài các tệp cấu hình của repository.
func ValidateWith(root string, layer Layer, data []byte) (Config, error) {
	if !layer.Valid() {
		return Config{}, fmt.Errorf("lớp cấu hình %q không sửa được", layer)
	}
	over := map[string][]byte{LayerPath(root, layer): data}
	cfg, _, err := load(root, false, true, true, over)
	return cfg, err
}

const uiBlockComment = "# giao diện TUI: auto | vi | en (auto: theo BINT_LANG, LANG; mặc định vi)"

// SetUILanguage trả nội dung tệp cấu hình data với ui.language = lang, chỉ sửa đúng dòng cần sửa: giữ mọi
// dòng, chú thích và kiểu xuống dòng khác. Thiếu khoá language thì chèn vào khối ui; thiếu khối ui thì nối
// khối ui có chú thích ở cuối; data rỗng thì tạo mới. Trả (data, false, nil) nếu giá trị đã đúng.
func SetUILanguage(data []byte, lang string) ([]byte, bool, error) {
	ok := false
	for _, l := range UILanguages {
		ok = ok || l == lang
	}
	if !ok {
		return nil, false, fmt.Errorf("ui.language %q: chỉ nhận %s", lang, strings.Join(UILanguages, " | "))
	}
	text := string(data)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("không đọc được YAML: %w", err)
	}
	var root *yaml.Node
	if len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	if root != nil && root.Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("tệp cấu hình phải là ánh xạ khoá: giá trị ở cấp cao nhất")
	}

	lines := strings.SplitAfter(text, "\n") // giữ dấu xuống dòng ở cuối mỗi dòng
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	insertAfter := func(idx int, add ...string) []byte {
		out := append([]string{}, lines[:idx+1]...)
		if !strings.HasSuffix(out[idx], "\n") {
			out[idx] += nl
		}
		for _, a := range add {
			out = append(out, a+nl)
		}
		out = append(out, lines[idx+1:]...)
		return []byte(strings.Join(out, ""))
	}

	ui := -1
	if root != nil {
		ui = mapIndex(root, "ui")
	}
	if ui < 0 { // thiếu khối ui: nối ở cuối
		add := []string{"ui:", "  language: " + lang + "            " + uiBlockComment}
		if len(lines) == 0 {
			return []byte(strings.Join(add, nl) + nl), true, nil
		}
		out := text
		if !strings.HasSuffix(out, "\n") {
			out += nl
		}
		if strings.TrimSpace(lines[len(lines)-1]) != "" {
			out += nl
		}
		return []byte(out + strings.Join(add, nl) + nl), true, nil
	}

	key, val := root.Content[ui], root.Content[ui+1]
	switch {
	case val.Kind == yaml.MappingNode && val.Style&yaml.FlowStyle == 0 && len(val.Content) > 0:
		li := mapIndex(val, "language")
		if li < 0 { // chèn khoá vào cuối khối ui, thụt theo khoá đầu tiên
			end := maxLine(val)
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" && indentOf(lines[end]) > key.Column {
				end++
			}
			indent := strings.Repeat(" ", val.Content[0].Column-1)
			return insertAfter(end-1, indent+"language: "+lang), true, nil
		}
		return replaceScalar(lines, val.Content[li+1], lang)
	case val.Kind == yaml.MappingNode: // dạng flow: {} hoặc {language: vi}
		if li := mapIndex(val, "language"); li >= 0 {
			return replaceScalar(lines, val.Content[li+1], lang)
		}
		ln := val.Line - 1
		line := lines[ln]
		start := runeToByte(line, val.Column-1)
		if start >= len(line) || line[start] != '{' {
			return nil, false, fmt.Errorf("không sửa được ui ở dòng %d (định dạng flow nhiều dòng)", val.Line)
		}
		rest := strings.TrimLeft(line[start+1:], " ")
		repl := "{language: " + lang + ", "
		if strings.HasPrefix(rest, "}") {
			repl = "{language: " + lang
		}
		lines[ln] = line[:start] + repl + rest
		return []byte(strings.Join(lines, "")), true, nil
	case val.Kind == yaml.ScalarNode && val.Tag == "!!null":
		ln := key.Line - 1
		line := lines[ln]
		// "ui:" (có thể kèm ~/null và chú thích): bỏ giá trị null rồi thêm khoá con ngay sau dòng.
		head := runeToByte(line, key.Column-1) + len("ui:")
		tail := strings.TrimLeft(line[head:], " \t")
		if strings.HasPrefix(tail, "~") || strings.HasPrefix(tail, "null") {
			tail = strings.TrimLeft(strings.TrimPrefix(strings.TrimPrefix(tail, "~"), "null"), " \t")
		}
		eol := ""
		if strings.HasSuffix(line, "\r\n") {
			eol = "\r\n"
		} else if strings.HasSuffix(line, "\n") {
			eol = "\n"
		}
		body := strings.TrimRight(tail, "\r\n")
		newLine := line[:head]
		if body != "" {
			newLine += " " + body
		}
		lines[ln] = newLine + eol
		return insertAfter(ln, strings.Repeat(" ", key.Column+1)+"language: "+lang), true, nil
	}
	return nil, false, fmt.Errorf("ui phải là một khối khoá (đang là %s ở dòng %d)", val.Tag, val.Line)
}

// replaceScalar thay giá trị vô hướng n (trên một dòng) bằng lang, giữ phần còn lại của dòng.
func replaceScalar(lines []string, n *yaml.Node, lang string) ([]byte, bool, error) {
	if n.Kind != yaml.ScalarNode {
		return nil, false, fmt.Errorf("ui.language ở dòng %d không phải giá trị đơn", n.Line)
	}
	if n.Value == lang {
		return []byte(strings.Join(lines, "")), false, nil
	}
	ln := n.Line - 1
	line := lines[ln]
	start := runeToByte(line, n.Column-1)
	if start > len(line) {
		return nil, false, fmt.Errorf("không định vị được ui.language ở dòng %d", n.Line)
	}
	if n.Tag == "!!null" && n.Value == "" { // `language:` không có giá trị: nút null nằm ngay sau dấu ':'
		lines[ln] = line[:start] + " " + lang + line[start:]
		return []byte(strings.Join(lines, "")), true, nil
	}
	end := start
	if start < len(line) && (line[start] == '"' || line[start] == '\'') {
		q := line[start]
		end = start + 1
		for end < len(line) && line[end] != q {
			end++
		}
		if end >= len(line) {
			return nil, false, fmt.Errorf("ui.language ở dòng %d: chuỗi nhiều dòng không hỗ trợ", n.Line)
		}
		end++
	} else {
		for end < len(line) && !strings.ContainsRune(" \t#\r\n,}", rune(line[end])) {
			end++
		}
	}
	lines[ln] = line[:start] + lang + line[end:]
	return []byte(strings.Join(lines, "")), true, nil
}

// runeToByte đổi chỉ số ký tự (rune) thành chỉ số byte trong s.
func runeToByte(s string, runes int) int {
	if runes <= 0 {
		return 0
	}
	i := 0
	for b := range s {
		if i == runes {
			return b
		}
		i++
	}
	return len(s)
}

// SetUILayout trả nội dung tệp cấu hình data với ui.layout.<screen> = pct, chỉ sửa đúng dòng cần sửa (giữ chú thích và các dòng
// khác): thay giá trị đã có, chèn khoá vào khối layout, chèn khối layout vào khối ui, hoặc nối khối ui ở cuối tệp.
func SetUILayout(data []byte, screen string, pct int) ([]byte, bool, error) {
	if !slices.Contains(UILayoutScreens, screen) {
		return nil, false, fmt.Errorf("ui.layout: màn lạ %q, chỉ nhận %s", screen, strings.Join(UILayoutScreens, ", "))
	}
	if pct < UILayoutMin || pct > UILayoutMax {
		return nil, false, fmt.Errorf("ui.layout.%s: phải trong %d–%d, nhận %d", screen, UILayoutMin, UILayoutMax, pct)
	}
	val := strconv.Itoa(pct)
	text := string(data)
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false, fmt.Errorf("không đọc được YAML: %w", err)
	}
	var root *yaml.Node
	if len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	if root != nil && root.Kind != yaml.MappingNode {
		return nil, false, fmt.Errorf("tệp cấu hình phải là ánh xạ khoá: giá trị ở cấp cao nhất")
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	insertAfter := func(idx int, add ...string) []byte {
		out := append([]string{}, lines[:idx+1]...)
		if !strings.HasSuffix(out[idx], "\n") {
			out[idx] += nl
		}
		for _, a := range add {
			out = append(out, a+nl)
		}
		out = append(out, lines[idx+1:]...)
		return []byte(strings.Join(out, ""))
	}
	// blockEnd: chỉ số dòng cuối (0-based) của khối con val của khoá key (gồm dòng thụt sâu hơn key ngay sau).
	blockEnd := func(key, val *yaml.Node) int {
		end := maxLine(val)
		for end < len(lines) && strings.TrimSpace(lines[end]) != "" && indentOf(lines[end]) > key.Column {
			end++
		}
		return end - 1
	}
	// setInMap đặt screen trong khối ánh xạ m (khoá mk): thay giá trị, hoặc chèn khoá mới.
	setInMap := func(mk, m *yaml.Node) ([]byte, bool, error) {
		switch {
		case m.Kind == yaml.MappingNode && m.Style&yaml.FlowStyle == 0 && len(m.Content) > 0:
			if i := mapIndex(m, screen); i >= 0 {
				return replaceScalar(lines, m.Content[i+1], val)
			}
			indent := strings.Repeat(" ", m.Content[0].Column-1)
			return insertAfter(blockEnd(mk, m), indent+screen+": "+val), true, nil
		case m.Kind == yaml.MappingNode && m.Line == maxLine(m) && m.Line == mk.Line: // dạng flow một dòng, vd layout: {} — viết lại {…}
			ln := m.Line - 1
			line := lines[ln]
			start := runeToByte(line, m.Column-1)
			end := strings.IndexByte(line[start:], '}')
			if start >= len(line) || line[start] != '{' || end < 0 {
				return nil, false, fmt.Errorf("không sửa được ui.layout ở dòng %d", m.Line)
			}
			kv := map[string]string{}
			for i := 0; i+1 < len(m.Content); i += 2 {
				kv[m.Content[i].Value] = m.Content[i+1].Value
			}
			if kv[screen] == val {
				return []byte(strings.Join(lines, "")), false, nil
			}
			kv[screen] = val
			var parts []string
			for _, k := range slices.Sorted(maps.Keys(kv)) {
				parts = append(parts, k+": "+kv[k])
			}
			lines[ln] = line[:start] + "{" + strings.Join(parts, ", ") + "}" + line[start+end+1:]
			return []byte(strings.Join(lines, "")), true, nil
		case m.Kind == yaml.ScalarNode && m.Tag == "!!null":
			return insertAfter(mk.Line-1, strings.Repeat(" ", mk.Column+1)+screen+": "+val), true, nil
		}
		return nil, false, fmt.Errorf("%s phải là một khối khoá (dòng %d)", mk.Value, m.Line)
	}

	ui := -1
	if root != nil {
		ui = mapIndex(root, "ui")
	}
	if ui < 0 { // thiếu khối ui: nối ở cuối
		add := []string{"ui:", "  layout:", "    " + screen + ": " + val}
		if len(lines) == 0 {
			return []byte(strings.Join(add, nl) + nl), true, nil
		}
		out := text
		if !strings.HasSuffix(out, "\n") {
			out += nl
		}
		return []byte(out + strings.Join(add, nl) + nl), true, nil
	}
	uk, uv := root.Content[ui], root.Content[ui+1]
	if uv.Kind == yaml.MappingNode && uv.Style&yaml.FlowStyle == 0 && len(uv.Content) > 0 {
		if li := mapIndex(uv, "layout"); li >= 0 {
			return setInMap(uv.Content[li], uv.Content[li+1])
		}
		indent := strings.Repeat(" ", uv.Content[0].Column-1)
		return insertAfter(blockEnd(uk, uv), indent+"layout:", indent+"  "+screen+": "+val), true, nil
	}
	if uv.Kind == yaml.ScalarNode && uv.Tag == "!!null" {
		in := strings.Repeat(" ", uk.Column+1)
		return insertAfter(uk.Line-1, in+"layout:", in+"  "+screen+": "+val), true, nil
	}
	return nil, false, fmt.Errorf("ui phải là một khối khoá nhiều dòng để ghi ui.layout (dòng %d)", uv.Line)
}
