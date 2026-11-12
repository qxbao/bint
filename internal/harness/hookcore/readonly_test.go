package hookcore

import (
	"testing"

	"bint/internal/config"
)

// Mẫu chỉ đọc MẶC ĐỊNH của sản phẩm (gồm grep và git grep): test chạy đúng cấu hình người dùng nhận được.
var testReadonlyTools = config.Default().Workflow.Agent.ReadonlyTools

func TestReadonlyShellAllowed(t *testing.T) {
	cases := []struct {
		name  string
		cmd   string
		allow bool
	}{
		{"git status", "git status", true},
		{"git status có tuỳ chọn", "git status --short", true},
		{"git diff với đường dẫn", "git diff HEAD~1 -- internal/a.go", true},
		{"bint task show", "bint task show 01J0000000000000000000000X", true},
		{"bint theo đường dẫn tuyệt đối", "/usr/local/bin/bint task show X", true},
		{"cd rồi bint", "cd /tmp/wt && bint task diff X", true},
		{"cd có nháy rồi git", `cd "/tmp/my wt" && git status`, true},
		{"2>&1 vô hại", "git log -3 2>&1", true},
		{"/dev/null vô hại", "git status >/dev/null 2>&1", true},
		{"heredoc bint task write", "bint task write X plan - <<'EOF'\nnội dung ; rm -rf x | sh\n$(touch y)\nEOF\n", true},
		{"heredoc sau cd", "cd /wt && bint task review X - <<'EOF'\n{}\nEOF", true},
		{"touch", "touch x", false},
		{"touch sau chấm phẩy", "git status; touch x", false},
		{"&& ngoài cd", "git status && touch x", false},
		{"pipe", "git log | sh", false},
		{"chuyển hướng ghi", "echo a > x", false},
		{"echo không có trong readonly_tools", "echo a", false},
		{"thay thế lệnh", "git log $(touch x)", false},
		{"backtick", "git log `touch x`", false},
		{"nhiều dòng không heredoc", "git status\ntouch x", false},
		{"lệnh sau heredoc", "bint task write X plan - <<'EOF'\nok\nEOF\ntouch x", false},
		{"heredoc không nháy có thay thế", "bint task write X plan - <<EOF\n$(touch x)\nEOF", false},
		{"heredoc không nháy thường", "bint task write X plan - <<EOF\nvăn bản thường\nEOF", true},
		{"git --output ghi tệp", "git diff --output=x", false},
		{"tiền tố git không khớp git statusx", "git statusx", false},
		// Review G3b .1: delimiter nháy một phần / nháy lệch, nháy hoặc \ kéo sang dòng sau.
		{"heredoc nháy một phần E'OF'", "git status <<E'OF'\nEOF\ntouch /tmp/pwned1\nE", false},
		{"heredoc nháy lệch 'EOF\"", "git status <<'EOF\"\n' ; touch /tmp/pwned2\nEOF", false},
		{"heredoc nháy kép khớp", "bint task write X plan - <<\"EOF\"\nok\nEOF", true},
		{"heredoc <<- không nháy", "bint task write X plan - <<-EOF\n\tok\n\tEOF", true},
		{"dòng mở heredoc còn nháy chưa đóng", "bint task write X plan - <<'EOF' '\nEOF\n'; touch x", false},
		{"dòng mở heredoc nối dòng bằng \\", "bint task write X plan - <<EOF \\\n; touch x\nEOF", false},
		{"delimiter dính ký tự sau", "bint task write X plan - <<EOF;touch x\nEOF", false},
		{"hai heredoc", "bint task write X plan - <<'EOF' <<E2\nok\nEOF", false},
		// Review G3b .3: Bash(bint:*) chỉ khớp khi token đầu là bint.
		{"gán biến trước bint", "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.fsmonitor GIT_CONFIG_VALUE_0=/usr/bin/touch bint task diff X", false},
		{"env trước bint", "env bint task diff X", false},
		{"bint.exe kiểu Windows", `C:\tools\bint.exe task show X`, true},
		{"tên giống bint", "/tmp/notbint task show X", false},
		{"cd không có lệnh", "cd /tmp &&", false},
		{"cd hai token", "cd /a b && git status", false},
		{"rỗng", "  ", false},
		// grep / git grep trong readonly_tools mặc định (tác vụ 01M7826Q…): đọc thì qua, ghi tệp hay chạy chương trình thì chặn.
		{"grep -rn", "grep -rn foo internal", true},
		{"git grep -n", "git grep -n foo", true},
		{"grep 2>/dev/null", "grep foo x 2>/dev/null", true},
		{"git grep có đường dẫn", "git grep -n -e foo -- internal/app", true},
		{"cd rồi grep", "cd /tmp/wt && grep -rn foo .", true},
		{"grep chuyển hướng ghi", "grep foo x > out", false},
		{"grep chuyển hướng nối", "grep foo x >> out", false},
		{"git grep chuyển hướng nối", "git grep foo >> out", false},
		{"git grep chuyển hướng ghi", "git grep foo > out", false},
		{"grep pipe sh", "grep foo x | sh", false},
		{"git grep pipe", "git grep foo | sh", false},
		{"grep chấm phẩy", "grep foo x; touch y", false},
		{"grep thay thế lệnh", "grep $(touch y) x", false},
		{"git grep -O", "git grep -O foo", false},
		{"git grep -Ovim", "git grep -Ovim foo", false},
		{"git grep -nO gộp cờ", "git grep -nOtouch foo", false},
		{"git grep -On gộp cờ", "git grep -On foo", false},
		{"git grep --open-files-in-pager", "git grep --open-files-in-pager=vim foo", false},
		{"git grep --open-files-in-pager không giá trị", "git grep --open-files-in-pager foo", false},
		{"git grep viết tắt --op", "git grep --op=vim foo", false},
		{"git grep viết tắt --open-files", "git grep --open-files=vim foo", false},
		{"git grep -O trong nháy kép", `git grep "-O" foo`, false},
		{"git grep -O trong nháy đơn", "git grep '-Ovim' foo", false},
		{"git grep -O nháy giữa cờ", "git grep -n'O'vim foo", false},
		{"git grep -O sau gạch chéo ngược", `git grep -\Ovim foo`, false},
		{"git grep --open sau gạch chéo ngược", `git grep --o\pen-files-in-pager=vim foo`, false},
		{"git grep -O ANSI-C", `git grep $'-O'vim foo`, false},
		{"git grep -O brace", "git grep -{O,n}vim foo", false},
		{"git grep brace đầu token (review .1)", "git grep {-Osh,-e} foo", false},
		{"git grep brace có nháy đầu token", `git grep {"-Otouch x",-e} foo`, false},
		{"brace ghép ký tự", "git grep {,-}O foo", false},
		{"zsh process substitution =(…) (review .2)", "grep foo =(touch x)", false},
		{"zsh glob qualifier", "grep foo *(e:'touch x':)", false},
		{"nháy ANSI-C che ngoặc", `grep $'\'' =(touch x)`, false},
		{"ngoặc trong nháy đơn vẫn qua", "grep -E 'foo(bar)+{2}' x.go", true},
		{"ngoặc trong nháy kép vẫn qua", `git grep -e "func (r \*Runner)" -- internal`, true},
		{"ngoặc thoát bằng gạch chéo ngược", `grep foo\(bar\) x.go`, true},
		{"git grep --only-matching vẫn qua", "git grep --only-matching foo", true},
		{"git grep --or vẫn qua", "git grep -e a --or -e b", true},
		{"git grep -c -w -i vẫn qua", "git grep -c -w -i foo", true},
		{"git -c rồi grep không khớp mẫu", "git -c core.pager=touch grep foo", false},
	}
	for _, c := range cases {
		got, why := readonlyShellAllowed(c.cmd, testReadonlyTools)
		if got != c.allow {
			t.Errorf("%s: %q ⇒ allow=%v (%s), muốn %v", c.name, c.cmd, got, why, c.allow)
		}
	}
}

func TestReadonlyRules(t *testing.T) {
	cmd := "make build"
	toks := []string{"make", "build"}
	for rules, want := range map[string]bool{
		"Bash":               true,
		"Bash(*)":            true,
		"Bash(make:*)":       true,
		"Bash(make build)":   true,
		"Bash(make *)":       true,
		"Bash(make test)":    false,
		"Bash(make)":         false,
		"Read":               false,
		"Bash(makefile:*)":   false,
		"Bash(make build:*)": true,
	} {
		if got := matchesReadonlyRules(toks, cmd, []string{rules}); got != want {
			t.Errorf("%q: %v, muốn %v", rules, got, want)
		}
	}
	if matchesReadonlyRules(toks, cmd, nil) {
		t.Error("không có mẫu thì không khớp")
	}
}

func TestReadonlyDecisionOnlyForReadonlySubagents(t *testing.T) {
	shell := ToolInfo{Kind: ToolShell, Command: "touch x"}
	edit := ToolInfo{Kind: ToolEdit, Paths: []string{"/outside/x"}}
	for _, at := range []string{"", "general-purpose", "bint-implementer"} {
		if deny, _ := readonlyDecision(at, "Bash", shell, testReadonlyTools); deny {
			t.Errorf("agent_type %q không được can thiệp", at)
		}
		if deny, _ := readonlyDecision(at, "Write", edit, testReadonlyTools); deny {
			t.Errorf("agent_type %q: Write không được can thiệp", at)
		}
	}
	for _, at := range []string{SubagentPlanner, SubagentReviewer} {
		if deny, why := readonlyDecision(at, "Bash", shell, testReadonlyTools); !deny || why == "" {
			t.Errorf("%s: touch x phải bị deny kèm lý do", at)
		}
		if deny, _ := readonlyDecision(at, "NotebookEdit", edit, testReadonlyTools); !deny {
			t.Errorf("%s: NotebookEdit phải bị deny", at)
		}
		if deny, _ := readonlyDecision(at, "Read", ToolInfo{Kind: ToolRead}, testReadonlyTools); deny {
			t.Errorf("%s: Read không bị chặn", at)
		}
	}
}
