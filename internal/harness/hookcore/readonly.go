package hookcore

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Guardrail chỉ đọc dựng sẵn cho subagent của driver session (docs/isolation-subagent-plan.md mục 6.4a).
//
// Spike S1 (mục 12.9) cho thấy `tools: Bash(<mẫu>)` trong định nghĩa subagent chỉ là luật cho phép, không phải danh sách
// trắng: lệnh nào chế độ quyền của phiên cha tự duyệt vẫn chạy. Nên planner/reviewer (bước chỉ đọc) được chặn ở hook:
// PreToolUse của Bash có agent_type là bint-planner / bint-reviewer mà lệnh không khớp workflow.agent.readonly_tools ⇒ deny,
// và Edit/Write/MultiEdit/NotebookEdit của hai subagent đó ⇒ deny. S3: hook thấy agent_type và deny áp cho subagent.
//
// Hook chỉ đọc cấu hình đã nạp và trả quyết định: không lỗi, không SQLite, không mô hình. Harness không có agent_type thì
// Dialect.SubagentReadonly=false và guardrail không áp. Khớp lệnh cố ý bảo thủ (không phân tích cú pháp shell đầy đủ):
// lệnh nào có dấu hiệu ghép lệnh/chuyển hướng/thay thế thì bị chặn dù hai vế đều chỉ đọc; lý do deny hướng dẫn sửa lệnh.

// Tên subagent chỉ đọc do Bint cài. Phải khớp workflow.SubagentPlanner/SubagentReviewer (kiểm thử so hai bên; hookcore
// không import workflow).
const (
	SubagentPlanner  = "bint-planner"
	SubagentReviewer = "bint-reviewer"
)

// GuardrailReadonly là id của guardrail dựng sẵn trong sự kiện guardrail.triggered.
const GuardrailReadonly = "bint-readonly"

// readonlySubagent: agent_type thuộc nhóm chỉ đọc.
func readonlySubagent(agentType string) bool {
	return agentType == SubagentPlanner || agentType == SubagentReviewer
}

// readonlyDecision trả (deny, lý do) cho một lần gọi công cụ của subagent. agentType không thuộc nhóm chỉ đọc (kể cả
// rỗng) ⇒ không can thiệp. Công cụ sửa tệp ⇒ deny. Shell ⇒ deny nếu lệnh không khớp readonlyTools.
func readonlyDecision(agentType, tool string, info ToolInfo, readonlyTools []string) (bool, string) {
	if !readonlySubagent(agentType) {
		return false, ""
	}
	switch info.Kind {
	case ToolEdit:
		return true, fmt.Sprintf("Bint: %s là bước chỉ đọc, không được dùng công cụ %s. Ghi tài liệu bằng `bint task write <id> <loại> -` (Bash), "+
			"còn sửa mã thuộc bước b-feat.", agentType, tool)
	case ToolShell:
		if ok, why := readonlyShellAllowed(info.Command, readonlyTools); !ok {
			return true, fmt.Sprintf("Bint: %s là bước chỉ đọc, lệnh bị chặn (%s): %s. Chỉ chạy lệnh khớp workflow.agent.readonly_tools "+
				"(ví dụ git status, git diff, git log, bint task show), mỗi lệnh một lần gọi, không pipe, `;`, `&&`, chuyển hướng hay thay thế lệnh "+
				"(ngoại lệ: `cd <thư mục> && lệnh` ở đầu và heredoc cho `bint task write/review`).", agentType, why, snippet(info.Command))
		}
	}
	return false, ""
}

func snippet(cmd string) string {
	cmd = strings.TrimSpace(strings.SplitN(cmd, "\n", 2)[0])
	if r := []rune(cmd); len(r) > 120 {
		cmd = string(r[:120]) + "…"
	}
	return "`" + cmd + "`"
}

var (
	// heredoc ở dòng đầu: <<EOF, <<'EOF', <<"EOF", <<-EOF. Delimiter phải là MỘT từ trọn vẹn: không nháy, hoặc nháy đơn/kép
	// khớp hai đầu, theo sau là khoảng trắng hoặc hết dòng. Nháy một phần (E'OF') hay lệch ('EOF") không khớp: shell bỏ nháy
	// trên cả từ nên delimiter thật khác với từ checker đọc được (review G3b .1). Nhóm 1: dấu gạch; 2/3/4: từ khoá trong nháy
	// đơn / nháy kép / không nháy.
	heredocRe = regexp.MustCompile(`<<(-?)[ \t]*(?:'([A-Za-z_][A-Za-z0-9_]*)'|"([A-Za-z_][A-Za-z0-9_]*)"|([A-Za-z_][A-Za-z0-9_]*))(?:[ \t]|$)`)
	// Dấu hiệu heredoc bất kỳ (kể cả dạng heredocRe không nhận): còn sót sau khi bỏ token hợp lệ ⇒ chặn.
	heredocAnyRe = regexp.MustCompile(`<<`)
	// Chuyển hướng vô hại được phép: 2>&1, >/dev/null, 2>/dev/null.
	harmlessRedirectRe = regexp.MustCompile(`\s*(?:\d?>\s*/dev/null|2>&1)`)
	// Tiền tố `cd MỘT_TOKEN && ` (prompt của bước chạy trên worktree riêng bắt buộc nó).
	// Dạng nháy kép nhận `\` (đường dẫn Windows, xem CdPrefix/cdTargetRe ở worktree_guard.go).
	cdPrefixRe = regexp.MustCompile(`^\s*cd\s+(?:"[^"$` + "`" + `]*"|'[^']*'|[^\s;&|<>$` + "`" + `()\\"']+)\s*&&\s*`)
)

// readonlyShellAllowed cho biết lệnh shell có được phép với bước chỉ đọc. Trả lý do ngắn khi không.
func readonlyShellAllowed(cmd string, readonlyTools []string) (bool, string) {
	cmd = strings.TrimRight(cmd, " \t\r\n")
	if strings.TrimSpace(cmd) == "" {
		return false, "lệnh rỗng"
	}
	first, body, multi := strings.Cut(cmd, "\n")
	if multi {
		// Nhiều dòng chỉ hợp lệ khi dòng đầu mở heredoc: thân heredoc là dữ liệu, không phải lệnh.
		m := heredocRe.FindStringSubmatchIndex(first)
		if m == nil {
			return false, "nhiều dòng lệnh"
		}
		var delim string
		quoted := true
		switch {
		case m[4] >= 0:
			delim = first[m[4]:m[5]]
		case m[6] >= 0:
			delim = first[m[6]:m[7]]
		default:
			delim, quoted = first[m[8]:m[9]], false
		}
		dash := first[m[2]:m[3]] != ""
		first = first[:m[0]] + " " + first[m[1]:] // bỏ token heredoc, còn lại là lệnh thật
		// Phần còn lại của dòng mở heredoc không được có nháy hay gạch chéo ngược: chuỗi nháy chưa đóng kéo sang dòng sau,
		// `\` cuối dòng nối dòng, cả hai đều đưa nội dung "thân heredoc" thành lệnh thật (review G3b .1).
		// Tiền tố `cd '<worktree>' &&` (CdPrefix, có nháy) hợp lệ: bỏ nó trước khi kiểm.
		if strings.ContainsAny(cdPrefixRe.ReplaceAllString(first, ""), `'"\`) {
			return false, "dòng mở heredoc có dấu nháy hoặc \\"
		}
		lines := strings.Split(body, "\n")
		end := -1
		for i, l := range lines {
			if dash {
				l = strings.TrimLeft(l, "\t")
			}
			if l == delim {
				end = i
				break
			}
		}
		data := lines
		if end >= 0 {
			data = lines[:end]
			for _, l := range lines[end+1:] {
				if strings.TrimSpace(l) != "" {
					return false, "có lệnh sau heredoc"
				}
			}
		}
		if !quoted {
			// Heredoc không nháy: shell vẫn thay thế lệnh trong thân.
			for _, l := range data {
				if strings.Contains(l, "$(") || strings.Contains(l, "`") {
					return false, "thân heredoc không nháy có thay thế lệnh"
				}
			}
		}
		if heredocAnyRe.MatchString(first) {
			return false, "nhiều heredoc"
		}
	}
	rest := harmlessRedirectRe.ReplaceAllString(first, "")
	rest = cdPrefixRe.ReplaceAllString(rest, "")
	if strings.ContainsAny(rest, ";&|<>`\n\r") || strings.Contains(rest, "$(") {
		return false, "có `;`, `&`, `|`, chuyển hướng hoặc thay thế lệnh"
	}
	if strings.Contains(rest, "$'") {
		// Nháy ANSI-C cho phép \' bên trong, nên unquoted (đọc theo nháy đơn thường) sẽ hiểu sai biên nháy.
		return false, "nháy ANSI-C ($'…') không được hỗ trợ trong lệnh chỉ đọc"
	}
	if strings.ContainsAny(unquoted(rest), "{}()") {
		// Ngoặc ngoài nháy: brace expansion (`{-Osh,-e}` thành cờ -O của git grep), process substitution `=(…)` và
		// glob qualifier `*(e:…:)` của zsh đều chạy hoặc ghép được lệnh mà không cần ký tự nào ở trên (review .1/.2 của
		// tác vụ 01M7826Q…). Trong nháy thì ngoặc là chữ thường (mẫu regex của grep vẫn dùng được khi đặt trong nháy).
		return false, "có ngoặc `{}`/`()` ngoài dấu nháy (brace expansion, `=(…)`, glob qualifier); đặt mẫu trong nháy"
	}
	toks := strings.Fields(rest)
	if len(toks) == 0 {
		return false, "không có lệnh sau cd"
	}
	if toks[0] == "git" || path.Base(toks[0]) == "git" {
		for _, t := range toks[1:] {
			if strings.HasPrefix(t, "--output") { // git diff/log --output=<tệp> ghi tệp
				return false, "tuỳ chọn --output ghi tệp"
			}
		}
		if why := gitGrepRunsProgram(rest); why != "" {
			return false, why
		}
	}
	if !matchesReadonlyRules(toks, rest, readonlyTools) {
		return false, "không khớp workflow.agent.readonly_tools"
	}
	return true, ""
}

// gitGrepRunsProgram trả lý do (rỗng = không có) khi lệnh `git grep …` có thể chạy chương trình tuỳ ý: tuỳ chọn
// -O[<pager>] / --open-files-in-pager[=<pager>] mở các tệp khớp bằng một pager do lệnh chỉ định. (GNU grep không có tuỳ
// chọn ghi tệp hay chạy lệnh; chuyển hướng, pipe, `;` đã bị luật chung chặn.)
//
// Kiểm trên lệnh đã bỏ nháy đơn, nháy kép và gạch chéo ngược (shell bỏ chúng trước khi git thấy tham số, nên `"-O"`,
// `-\O`, `-n'O'vim` đều thành -O): token bắt đầu bằng ĐÚNG MỘT gạch ngang và có chữ O hoa ở bất kỳ vị trí nào (cờ gộp như
// -nOvim; cả khai báo brace `-{O,n}`), hoặc token hai gạch là tiền tố từ 4 ký tự của --open-files-in-pager (git nhận viết
// tắt không nhập nhằng, ví dụ --op). ANSI-C quoting ($'…', $"…") tạo được -O từ mã thoát nên bị chặn. Có thể chặn nhầm một
// mẫu tìm có chữ O đứng ngay sau gạch ngang (ví dụ `-e -Ox`); chấp nhận vì bảo thủ.
func gitGrepRunsProgram(cmd string) string {
	if strings.Contains(cmd, "$'") || strings.Contains(cmd, `$"`) {
		return "git grep: nháy ANSI-C ($'…') có thể tạo -O"
	}
	toks := strings.Fields(strings.NewReplacer("'", "", `"`, "", `\`, "").Replace(cmd))
	if len(toks) < 2 || toks[1] != "grep" {
		return ""
	}
	const long = "--open-files-in-pager"
	for _, t := range toks[2:] {
		name, _, _ := strings.Cut(t, "=")
		switch {
		case strings.HasPrefix(t, "--"):
			if len(name) >= 4 && strings.HasPrefix(long, name) {
				return "git grep -O/--open-files-in-pager chạy chương trình tuỳ ý"
			}
		case strings.HasPrefix(t, "-") && strings.Contains(t, "O"):
			return "git grep -O/--open-files-in-pager chạy chương trình tuỳ ý"
		}
	}
	return ""
}

// unquoted trả phần lệnh nằm ngoài dấu nháy: nội dung trong '…' và "…" cùng ký tự đứng sau gạch chéo ngược (ngoài nháy
// đơn) được thay bằng chữ x để vị trí token giữ nguyên. Chỉ dùng để tìm ký tự đặc biệt mà shell còn diễn giải; thay thế
// lệnh trong nháy kép (`$(`, backtick) đã bị luật chung chặn trên toàn chuỗi.
func unquoted(s string) string {
	var b strings.Builder
	var quote rune
	esc := false
	for _, c := range s {
		switch {
		case esc:
			esc = false
			b.WriteRune('x')
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
			b.WriteRune('x')
		case quote == '"':
			if c == '\\' {
				esc = true
			} else if c == '"' {
				quote = 0
			}
			b.WriteRune('x')
		case c == '\\':
			esc = true
			b.WriteRune('x')
		case c == '\'' || c == '"':
			quote = c
			b.WriteRune('x')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// matchesReadonlyRules khớp lệnh (đã tách token) với các mẫu Bash(...) của readonly_tools. Mẫu `Bash(tiền tố:*)` (hoặc
// `Bash(tiền tố *)`) khớp theo token đầu; `Bash(lệnh)` khớp nguyên lệnh; `Bash` / `Bash(*)` khớp mọi lệnh. Mục không
// phải Bash bị bỏ qua. `Bash(bint:*)` khớp cả lời gọi bint theo đường dẫn tuyệt đối (như withBintExecutableRule).
func matchesReadonlyRules(toks []string, cmd string, rules []string) bool {
	for _, r := range rules {
		r = strings.TrimSpace(r)
		if r == "Bash" {
			return true
		}
		inner, ok := strings.CutPrefix(r, "Bash(")
		if !ok || !strings.HasSuffix(inner, ")") {
			continue
		}
		inner = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
		switch {
		case inner == "*" || inner == ":*":
			return true
		case strings.HasSuffix(inner, ":*") || strings.HasSuffix(inner, " *"):
			prefix := strings.Fields(strings.TrimSuffix(strings.TrimSuffix(inner, ":*"), " *"))
			if len(prefix) == 0 {
				return true
			}
			// bint theo đường dẫn tuyệt đối: token ĐẦU phải là bint. Không dùng detect.IsBintInvocation vì nó bỏ qua gán biến
			// môi trường và wrapper (env, timeout…) đứng trước: `GIT_CONFIG_…=… bint task diff` điều khiển được git bên trong
			// bint (review G3b .3).
			if len(prefix) == 1 && prefix[0] == "bint" && isBintExecutable(toks[0]) {
				return true
			}
			if hasTokenPrefix(toks, prefix) {
				return true
			}
		default:
			if want := strings.Fields(inner); len(want) > 0 && len(want) == len(toks) && hasTokenPrefix(toks, want) {
				return true
			}
		}
	}
	return false
}

// isBintExecutable: token là chương trình bint (tên trần hoặc đường dẫn, kể cả bint.exe và đường dẫn kiểu Windows).
func isBintExecutable(tok string) bool {
	if i := strings.LastIndexAny(tok, `/\`); i >= 0 {
		tok = tok[i+1:]
	}
	return tok == "bint" || strings.EqualFold(tok, "bint.exe")
}

func hasTokenPrefix(toks, prefix []string) bool {
	if len(toks) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if toks[i] != p {
			return false
		}
	}
	return true
}
