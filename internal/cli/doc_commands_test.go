package cli

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Skill gốc và tài liệu hướng dẫn là nguồn agent (và người) chép lệnh theo. Lệnh không tồn tại trong đó (ví dụ
// `bint task done --force` thay cho `bint task status <id> done --force`) bị mọi phiên agent lặp lại, nên test này đối
// chiếu mọi lệnh `bint …` trong các tệp đó với cây lệnh thật: lệnh con và cờ phải có.
//
// Không quét tài liệu kế hoạch/nghiệm thu (docs/*-plan.md, next-directions.md, acceptance…): chúng nhắc lệnh dự kiến.
var docCommandSources = []string{
	"internal/skills/core", // mọi *.md bên dưới
	"AGENTS.md",
	"README.md",
	"docs/README.md",
	"docs/usage.md",
	"docs/workflows.md",
	"docs/configuration.md",
	"docs/knowledge.md",
	"docs/evaluation.md",
	"docs/evolution.md",
	"docs/ci-cd.md",
	"docs/harness-components.md",
	"docs/macos-compatibility.md",
	"docs/architecture.md",
}

// docCommandExclusions: lệnh cố ý không có trong cây lệnh, kèm lý do. Khoá là tiền tố của lệnh đã chuẩn hoá.
var docCommandExclusions = map[string]string{
	"bint base <id>": "thông điệp commit ảnh chụp cây gốc của worktree (docs/workflows.md), không phải lệnh",
}

type docCmdRef struct {
	file string
	line int
	text string
}

var (
	docInlineCodeRe = regexp.MustCompile("`(bint [^`]+)`")
	docFenceLineRe  = regexp.MustCompile(`^\s*(?:\$\s+)?(bint\s.*)$`)
)

func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không tìm thấy go.mod")
		}
		dir = parent
	}
}

// collectDocCommands trích lệnh bint trong code span một dòng và trong khối mã (nối dòng kết thúc bằng `\`).
func collectDocCommands(name string, data []byte) []docCmdRef {
	var out []docCmdRef
	inFence := false
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			m := docFenceLineRe.FindStringSubmatch(ln)
			if m == nil {
				continue
			}
			cmd, start := m[1], i+1
			for strings.HasSuffix(strings.TrimSpace(cmd), `\`) && i+1 < len(lines) {
				cmd = strings.TrimSuffix(strings.TrimSpace(cmd), `\`) + " " + strings.TrimSpace(lines[i+1])
				i++
			}
			out = append(out, docCmdRef{name, start, cmd})
			continue
		}
		for _, m := range docInlineCodeRe.FindAllStringSubmatch(ln, -1) {
			out = append(out, docCmdRef{name, i + 1, m[1]})
		}
	}
	return out
}

// docTokens tách lệnh thành token kiểu shell (đơn giản: nháy đơn/kép), dừng ở toán tử shell hoặc chú thích.
func docTokens(s string) []string {
	var toks []string
	var cur strings.Builder
	var quote rune
	has := false
	flush := func() {
		if has {
			toks = append(toks, cur.String())
		}
		cur.Reset()
		has = false
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, has = r, true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	flush()
	for i, t := range toks {
		if t == "|" || t == "&&" || t == "||" || t == ";" || t == "#" || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "2>") {
			return toks[:i]
		}
	}
	return toks
}

// placeholder: token minh hoạ (<id>, …, [cờ], X) — không kiểm và dừng phân tích lệnh con.
func docPlaceholder(t string) bool {
	return strings.ContainsAny(t, "<>[]{}…") || strings.Contains(t, "...") || t == "X"
}

func findSub(c *cobra.Command, name string) *cobra.Command {
	for _, s := range c.Commands() {
		if s.Name() == name || s.HasAlias(name) {
			return s
		}
	}
	return nil
}

func lookupFlag(c *cobra.Command, tok string) bool {
	name := strings.TrimLeft(tok, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	if name == "" || name == "help" || (strings.HasPrefix(tok, "--") && name == "version" && !c.HasParent()) {
		return true
	}
	fs := []func(string) bool{
		func(n string) bool { return c.Flags().Lookup(n) != nil },
		func(n string) bool { return c.InheritedFlags().Lookup(n) != nil },
		func(n string) bool { return c.PersistentFlags().Lookup(n) != nil },
	}
	if !strings.HasPrefix(tok, "--") {
		// -abc: mỗi ký tự là một cờ ngắn.
		for _, r := range name {
			if c.Flags().ShorthandLookup(string(r)) == nil && c.InheritedFlags().ShorthandLookup(string(r)) == nil {
				return false
			}
		}
		return true
	}
	for _, f := range fs {
		if f(name) {
			return true
		}
	}
	return false
}

// checkDocCommand trả mô tả lỗi ("" nếu lệnh hợp lệ). Lệnh con phải có khi lệnh cha là nhóm thuần (không tự chạy được);
// cờ phải có ở lệnh sâu nhất đã xác định.
func checkDocCommand(root *cobra.Command, text string) string {
	toks := docTokens(text)
	if len(toks) == 0 || toks[0] != "bint" {
		return ""
	}
	cmd := root
	path := "bint"
	descend := true
	for _, t := range toks[1:] {
		if t == "--" {
			break
		}
		if strings.HasPrefix(t, "-") && len(t) > 1 {
			if docPlaceholder(t) {
				continue
			}
			// `--a|--b`: cách viết "chọn một" của tài liệu, mọi lựa chọn phải có.
			for alt := range strings.SplitSeq(t, "|") {
				if !lookupFlag(cmd, alt) {
					return "`" + path + "` không có cờ " + alt
				}
			}
			continue
		}
		if !descend || docPlaceholder(t) {
			descend = false
			continue
		}
		if sub := findSub(cmd, t); sub != nil {
			cmd, path = sub, path+" "+t
			continue
		}
		// `list|show` hoặc `write/review` ở vị trí lệnh con: mọi lựa chọn phải là lệnh con; dừng phân tích sau đó.
		if alts := strings.FieldsFunc(t, func(r rune) bool { return r == '|' || r == '/' }); len(alts) > 1 && cmd.HasSubCommands() {
			if findSub(cmd, alts[0]) != nil || !cmd.Runnable() {
				for _, a := range alts {
					if findSub(cmd, a) == nil {
						return "`" + path + "` không có lệnh con " + a
					}
				}
				descend = false
				continue
			}
		}
		if cmd.HasSubCommands() && !cmd.Runnable() {
			return "`" + path + "` không có lệnh con " + t
		}
		descend = false // đối số vị trí của lệnh lá
	}
	return ""
}

func TestDocCommandsExist(t *testing.T) {
	root := NewRoot(IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	base := repoRootForTest(t)
	var refs []docCmdRef
	for _, src := range docCommandSources {
		p := filepath.Join(base, filepath.FromSlash(src))
		err := filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, path)
			refs = append(refs, collectDocCommands(filepath.ToSlash(rel), data)...)
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
	}
	if len(refs) < 100 {
		t.Fatalf("quét được quá ít lệnh (%d): bộ quét hỏng?", len(refs))
	}
	used := map[string]bool{}
	var bad []string
	for _, r := range refs {
		msg := checkDocCommand(root, r.text)
		if msg == "" {
			continue
		}
		excluded := false
		for k := range docCommandExclusions {
			if strings.HasPrefix(r.text, k) {
				used[k], excluded = true, true
			}
		}
		if !excluded {
			bad = append(bad, r.file+":"+strconv.Itoa(r.line)+": "+msg+"  ←  "+r.text)
		}
	}
	for k := range docCommandExclusions {
		if !used[k] {
			bad = append(bad, "loại trừ thừa (không còn lệnh nào khớp): "+k)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("lệnh bint trong skill/tài liệu không khớp CLI (%d):\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
	t.Logf("đã kiểm %d lệnh bint trong skill/tài liệu", len(refs))
}

// Ca ngược: bộ kiểm phải bắt lệnh con và cờ không tồn tại, và không bắt nhầm đối số, chỗ giữ chỗ, lệnh hợp lệ.
func TestDocCommandCheckerCatchesMistakes(t *testing.T) {
	root := NewRoot(IO{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	for text, wantBad := range map[string]bool{
		"bint task done --force":                       false,
		"bint task done --bogus":                       true,
		"bint task status <id> done --force -m lý-do":  false,
		"bint task set X --json":                       false,
		"bint task new 'mô tả' --workflow b-do --json": false,
		"bint run b-do --task <id> --json":             false,
		"bint mcp index codegraph":                     false,
		"bint config set x y":                          true,
		"bint knowledge add --type warning --paths 'a/**' --evidence a.go:1 \"x\"": false,
		"bint run b-do --task <id> | jq .":                                         false,
		"bint task new 'x' --plan|--no-plan":                                       false,
		"bint task new 'x' --plan|--no-pla":                                        true,
		"bint flow list|show|lint":                                                 false,
		"bint flow list|frob":                                                      true,
		"bint task write/review <id>":                                              false,
		"bint context internal/app/x.go":                                           false,
		"bint …":                                                                   false,
	} {
		if got := checkDocCommand(root, text) != ""; got != wantBad {
			t.Errorf("%q: lỗi=%v, muốn %v (%s)", text, got, wantBad, checkDocCommand(root, text))
		}
	}
}
