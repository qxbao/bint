package detect

import (
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`^\{[a-z_]+\}$`)

// shellSegments tách lệnh shell thành các lệnh đơn (cắt ở &&, ||, ;, |, &, xuống
// dòng nằm ngoài dấu nháy), mỗi lệnh là danh sách token đã bỏ dấu nháy. Đây là
// bộ tách đơn giản, đủ để so khớp theo token; không diễn giải $(...) hay heredoc.
func shellSegments(cmd string) [][]string {
	var segs [][]string
	var cur []string
	var tok strings.Builder
	has := false
	var quote rune
	flushTok := func() {
		if has {
			cur = append(cur, tok.String())
			tok.Reset()
			has = false
		}
	}
	flushSeg := func() {
		flushTok()
		if len(cur) > 0 {
			segs = append(segs, cur)
			cur = nil
		}
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else if r == '\\' && quote == '"' && i+1 < len(rs) {
				i++
				tok.WriteRune(rs[i])
			} else {
				tok.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			tok.WriteRune(rs[i])
			has = true
		case r == ' ' || r == '\t':
			flushTok()
		case r == '&' || r == '|' || r == ';' || r == '\n' || r == '(' || r == ')':
			flushSeg()
		default:
			tok.WriteRune(r)
			has = true
		}
	}
	flushSeg()
	return segs
}

var envToken = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripWrappers bỏ phần bọc phổ biến ở đầu một lệnh đơn: gán biến môi trường,
// env, time, nice, timeout <thời lượng>. Không bóc sh -c, xargs... (không chắc
// an toàn): những lệnh đó không được coi là khớp.
func stripWrappers(t []string) []string {
	for len(t) > 0 {
		switch {
		case envToken.MatchString(t[0]), t[0] == "env", t[0] == "time", t[0] == "nice", t[0] == "command":
			t = t[1:]
		case t[0] == "timeout":
			t = t[1:]
			for len(t) > 0 && strings.HasPrefix(t[0], "-") {
				t = t[1:]
			}
			if len(t) > 0 { // thời lượng
				t = t[1:]
			}
		default:
			return t
		}
	}
	return t
}

// matchTokens: mẫu khớp tiền tố của tok; placeholder khớp một hay nhiều token
// không rỗng và không bắt đầu bằng "-" (đối số đường dẫn, không phải cờ).
func matchTokens(pat, tok []string) bool {
	if len(pat) == 0 {
		return true // đối số thừa phía sau được chấp nhận
	}
	if placeholderRe.MatchString(pat[0]) {
		for n := 1; n <= len(tok); n++ {
			if strings.HasPrefix(tok[n-1], "-") {
				return false
			}
			if matchTokens(pat[1:], tok[n:]) {
				return true
			}
		}
		return false
	}
	return len(tok) > 0 && tok[0] == pat[0] && matchTokens(pat[1:], tok[1:])
}

// MatchesTemplate cho biết lệnh shell thực sự chạy lệnh kiểm thử đã cấu hình.
// tmpl coi là mẫu: phần cố định khớp theo token (không khớp chuỗi con giữa từ,
// không khớp khi nằm trong đối số của lệnh khác như echo "<lệnh>"); placeholder
// như {test_files} khớp một hay nhiều đối số đường dẫn không rỗng. Lệnh có thể
// đứng sau "cd x &&" hoặc sau timeout/env/time; đối số thừa phía sau được chấp nhận.
func MatchesTemplate(cmd, tmpl string) bool {
	pat := firstSegment(tmpl)
	if len(pat) == 0 {
		return false
	}
	for _, seg := range shellSegments(cmd) {
		if matchTokens(pat, stripWrappers(seg)) {
			return true
		}
	}
	return false
}

// IsExecutedTestCommand là lựa chọn bảo thủ cho Codex: chỉ nhận diện ở vị trí
// lệnh, không tìm chữ "go test" trong đối số/tài liệu. Không diễn giải heredoc
// hoặc substitution; script loại đó không được nâng thành bằng chứng test.
// IsTestCommand cũ giữ nguyên cho các adapter khác.
func IsExecutedTestCommand(cmd string, configured ...string) bool {
	if strings.Contains(cmd, "<<") || strings.Contains(cmd, "$(") || strings.ContainsRune(cmd, '`') {
		return false
	}
	for _, tmpl := range configured {
		if strings.TrimSpace(tmpl) != "" && MatchesTemplate(cmd, tmpl) {
			return true
		}
	}
	for _, seg := range shellSegments(cmd) {
		seg = stripWrappers(seg)
		if len(seg) == 0 || strings.ContainsAny(seg[0], " \t\r\n") {
			continue
		}
		if at := testRunner.FindStringIndex(strings.Join(seg, " ")); at != nil && at[0] == 0 {
			return true
		}
	}
	return false
}

func firstSegment(tmpl string) []string {
	segs := shellSegments(tmpl)
	if len(segs) != 1 {
		// Mẫu nhiều lệnh: so khớp nguyên dãy token, bỏ qua toán tử ở mức đơn giản.
		var all []string
		for _, s := range segs {
			all = append(all, s...)
		}
		return all
	}
	return segs[0]
}
