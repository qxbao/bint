package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"bint/internal/i18n"
	"bint/internal/tui/ui"
)

// Kiểm kê literal nhắc lệnh CLI của tầng dịch vụ (docs/tui-plan.md mục 9, yêu cầu bổ sung): mọi literal chuỗi (go/ast, bỏ chú thích
// và tệp _test) trong các gói dưới đây (cùng internal/app/evolvesvc) nhắc "bint <lệnh>" HOẶC mang token --cờ đứng rời phải hoặc được
// Rewriter.Rewrite của gói ui đổi hết thành tham chiếu TUI (kết quả không còn "bint" kèm đường lệnh của CLI, không còn --cờ rời), hoặc
// nằm trong danh sách loại trừ kèm lý do. Thêm một literal nhắc lệnh mà hàm viết lại không xử lý được, hoặc một cờ rời mới (kể cả khi
// chuỗi không có chữ "bint"), thì test đỏ.
var inventoryDirs = []string{"../app", "../app/evolvesvc", "../knowledge", "../task", "../workflow", "../eval", "../evolve"}

type lit struct {
	file string // tương đối so với internal/, dùng dấu /
	line int
	text string
}

// exclusion: literal của file có chứa contains được bỏ qua khi hàm viết lại không xử lý hết; reason bắt buộc.
type exclusion struct{ file, contains, reason string }

const (
	rAgent     = "prompt gửi cho agent (không hiện ở TUI; agent cần đúng lệnh CLI)"
	rFile      = "nội dung tệp do Bint sinh ra (người dùng và agent đọc như tệp; TUI không viết lại nội dung tệp)"
	rCLI       = "chuỗi chỉ CLI in ra (hoặc ghi nhận vận hành), không hiện trong TUI"
	rUsage     = "lỗi kiểm tra đối số của dịch vụ (giá trị hoặc tổ hợp cờ sai): form TUI dựng từ cờ của lệnh nên tên cờ trùng tên ô, và ô chọn/số/công tắc đã ngăn phần lớn giá trị sai; chưa có cụm TUI riêng cho từng ô (lệch còn lại ghi ở docs/tui-plan.md mục 12.10)"
	rExtTool   = "cờ của công cụ ngoài (git, claude, codex): không phải cờ của lệnh bint nên không có ô form tương ứng"
	rNotMsg    = "không phải thông điệp: mẫu biểu thức chính quy dùng để so khớp đầu ra của git diff"
	rSplit     = "mảnh của chuỗi nối (\"…`bint evolve evaluate <id>\" + \" --regression --yes`\"): thông điệp đầy đủ được viết lại lúc chạy, cờ nằm trong lệnh bint được đổi thành tham chiếu TUI"
	rAgentGate = "lỗi chỉ trả cho phiên agent (cổng chỉ-dành-cho-người); TUI luôn chạy với người nên không bao giờ hiện"
	rCLIOnly   = "chỉ có ở chế độ không tương tác của CLI; TUI luôn tương tác (xác nhận bằng hộp thoại)"
)

var baseExclusions = []exclusion{
	{"app/hook.go", "bint hook: ", rCLI + ": lỗi của lệnh bint hook (ghi vào hook-errors.log)"},
	{"app/init.go", "bint mcp serve", "lệnh không có hành động trong sổ (máy chủ MCP qua stdio, không có giao diện)"},
	{"app/knowledge.go", "bint knowledge import", "lệnh knowledge import chưa tồn tại (sau v1.0) nên không có hành động trong sổ để chỉ tới"},
	{"app/task_svc.go", "bint task status --force", "tham chiếu TUI của lệnh là ô form “Ép (--force)”: tên ô giữ cờ CLI để người dùng đối chiếu (nhãn do i18n, không phải thông điệp dịch vụ)"},
	{"workflow/run.go", " --verify-command '…' rồi bint run resume ", "mảnh của chuỗi nối (+ mã tác vụ): thông điệp đầy đủ được viết lại lúc chạy (TestRewriteTable ca \"nhiều lệnh một câu\")"},
	{"app/doctor.go", "chưa xác minh cite_knowledge", "hướng dẫn sửa cấu hình Codex bằng tay; lệnh init --mcp được viết lại, phần còn lại là tên khoá cấu hình"},
	{"workflow/run.go", "Bạn đang chạy dưới", rAgent},
	{"workflow/run.go", "bint task criteria <id>", rAgent},
	{"knowledge/render.go", "bint bootstrap publish", rFile + ": khối AGENTS.md"},
	{"knowledge/render.go", "bint knowledge add", rFile + ": khối AGENTS.md"},
	{"task/task.go", "Phiếu tác vụ Bint", rFile + ": chú thích đầu task.yml"},
	{"task/task.go", "verify.command chỉ được Bint chạy", rFile + ": chú thích đầu task.yml"},
	{"eval/case.go", "Ứng viên tác vụ đánh giá", rFile + ": chú thích của tệp ứng viên đánh giá"},
	{"eval/case.go", "Tác vụ đánh giá đã duyệt", rFile + ": chú thích của tệp tác vụ đánh giá"},
	{"workflow/run.go", "(thêm `--stat` để định hướng, `--name-only` để chỉ lấy danh sách tệp)", rAgent},
}

var litsrUsage = []struct{ file, contains string }{
	{"app/bootstrap.go", "cần mô tả tác vụ hoặc --paths"},
	{"app/eval_cases.go", "--all không đi cùng danh sách ID"},
	{"app/eval_cases.go", "cần ID tác vụ hoặc --all"},
	{"app/eval_run.go", "--repeats phải >= 1"},
	{"app/eval_run.go", "--leakage-mode phải là strict | economy"},
	{"app/eval_run.go", "--bare chỉ dùng với harness claude"},
	{"app/evolvesvc/evaluate.go", "--delivery phải là bint | minimal"},
	{"app/evolvesvc/evaluate.go", "--leakage-mode phải là strict | economy"},
	{"app/evolvesvc/evaluate.go", "--split phải là validation | test (tập build dùng để xây đề xuất, mục 12.2)"},
	{"app/evolvesvc/evaluate.go", "--repeats phải >= 1"},
	{"app/evolvesvc/evaluate.go", "--regression chỉ dùng cho đề xuất đã áp dụng (đề xuất %s: %s)"},
	{"app/evolvesvc/evaluate.go", "--regression chưa hỗ trợ cho đề xuất quy trình"},
	{"app/harness_components.go", "--kind %q: cần guidance, guardrail, script hoặc strategy"},
	{"app/harness_components.go", "--template loại trừ --kind và --file"},
	{"app/harness_components.go", "cần --template hoặc --file"},
	{"app/harness_components.go", "--file cần --kind %s"},
	{"app/harness_components.go", "cần --kind guidance|guardrail|script|strategy, nhận %q"},
	{"app/harness_components.go", "--status %q: cần candidate, verified hoặc deprecated"},
	{"app/init.go", "--git: phải là %s (nhận %q)"},
	{"app/init.go", "--harness khác harnesses.enabled trong cấu hình (%s); Bint không sửa cấu hình đã có — hãy chỉnh tay"},
	{"app/init_svc.go", "--hooks và --no-hooks loại trừ nhau"},
	{"app/init_svc.go", "--mcp và --no-mcp loại trừ nhau"},
	{"app/init_svc.go", "--hooks-scope: %v"},
	{"app/init_svc.go", "--git: phải là %s (nhận %q)"},
	{"app/init_svc.go", "--harness: harness không hỗ trợ %q (hỗ trợ: %s)"},
	{"app/knowledge.go", "thiếu nội dung (đối số, '-' cho stdin, hoặc --body-file)"},
	{"app/knowledge.go", "--type phải là một trong: %s"},
	{"app/knowledge.go", "--from-review chỉ tạo mục warning hoặc lesson"},
	{"app/revalidate.go", "--budget không được âm"},
	{"app/review.go", "--status: %q không hợp lệ (%s)"},
	{"app/run_svc.go", "--plan và --no-plan loại trừ nhau"},
	{"app/run_svc.go", "--worktree và --shared loại trừ nhau"},
	{"app/run_svc.go", "--worktree/--shared chỉ chọn lúc lập phiếu"},
	{"app/task_svc.go", "--worktree và --shared loại trừ nhau"},
	{"workflow/run.go", "--worktree/--shared chỉ chọn lúc lập phiếu"},
	{"app/skills_svc.go", "--from/--to phải ≥ 0"},
	{"app/task_diff.go", "--stat và --name-only loại trừ nhau"},
	{"app/task_svc.go", "--depth standard|deep hàm ý có b-req: không đi cùng --no-plan"},
	{"app/task_svc.go", "--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)"},
	{"app/task_svc.go", "--plan và --no-plan loại trừ nhau"},
	{"app/task_svc.go", "--harness: harness không hỗ trợ %q (%s)"},
	{"app/task_svc.go", "--agent-command không đi cùng --harness/--role"},
	{"task/ticket.go", "--depth: giá trị %q không hợp lệ (auto | standard | deep)"},
	{"workflow/ops.go", "--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)"},
	{"workflow/ops.go", "--depth standard|deep hàm ý có b-req: không đi cùng --no-plan"},
	{"workflow/ops.go", "--path cần tên tệp cụ thể trong repository, không phải glob: %q"},
	{"workflow/run.go", "--depth chỉ dùng với workflow b-do hoặc b-req (không dùng với %s)"},
	{"workflow/run.go", "--depth standard|deep hàm ý có b-req: không đi cùng --no-plan"},
	{"workflow/run.go", "không đổi được độ sâu phân tích khi tác vụ đang %s: --depth chỉ nhận khi tác vụ còn created, "},
}

var litsrExtTool = []struct{ file, contains string }{
	{"app/doctor.go", "phiên bản %s; cần >= 2.31 (rev-parse --path-format)"},
	{"app/evolvesvc/agents.go", "roles.%s=%q: chỉ harness claude khoá được công cụ của %s (--tools \"\"); "},
	{"app/update.go", "git pull --ff-only trong %s: %w"},
	{"workflow/land.go", " worktree remove --force "},
	{"workflow/worktree.go", "dọn tay: git -C %q worktree remove --force %q"},
	{"eval/agent.go", "--disallowedTools Edit,Write,… + Bint kiểm tra cây làm việc"},
	{"eval/agent_harness.go", "Codex eval cô lập không dùng --ignore-user-config vì cờ đó bỏ cấu hình chặn user skill"},
	{"eval/agent_harness.go", "--mode plan + Bint kiểm tra cây làm việc"},
}

var litsrNotMsg = []struct{ file, contains string }{
	{"eval/statement.go", "^(diff --git |index [0-9a-f]+\\.\\.|@@ |\\+\\+\\+ |--- a/)"},
}

var litsrSplit = []struct{ file, contains string }{
	{"evolve/skill_apply.go", " --regression --yes`"},
}

var litsrAgentGate = []struct{ file, contains string }{
	{"app/task_svc.go", " --autonomy auto"},
	{"workflow/ops.go", "%w: --force"},
	{"workflow/ops.go", "%w: --discard"},
}

var litsrCLIOnly = []struct{ file, contains string }{
	{"app/service.go", "cần --yes khi chạy không tương tác"},
}

// inventoryExclusions: loại trừ theo nhóm lý do cộng các loại trừ riêng lẻ ở trên.
var inventoryExclusions = func() []exclusion {
	out := append([]exclusion(nil), baseExclusions...)
	for reason, group := range map[string][]struct{ file, contains string }{
		rUsage: litsrUsage, rExtTool: litsrExtTool, rNotMsg: litsrNotMsg, rSplit: litsrSplit, rAgentGate: litsrAgentGate, rCLIOnly: litsrCLIOnly,
	} {
		for _, g := range group {
			out = append(out, exclusion{g.file, g.contains, reason})
		}
	}
	return out
}()

var topWordsRE = func() *regexp.Regexp {
	ls := leaves(NewRoot(IO{}))
	seen := map[string]bool{}
	var words []string
	for p := range ls {
		if w := strings.Fields(p)[0]; !seen[w] {
			seen[w] = true
			words = append(words, regexp.QuoteMeta(w))
		}
	}
	sort.Strings(words)
	return regexp.MustCompile(`\bbint (?:` + strings.Join(words, "|") + `)\b`)
}()

var anyBintRE = regexp.MustCompile(`\bbint [a-z]`)

// looseFlagRE: cờ CLI đứng rời trong thông điệp ("hoặc dùng --unverified", "(--git local)"): sau khi viết lại không được còn (trừ loại trừ có lý do).
//
// Bộ quét lấy MỌI literal nhắc "bint <lệnh>" hoặc mang token --cờ đứng rời (không chỉ cụm đã biết), nên cờ rời mới thêm vào thông điệp
// dịch vụ làm test đỏ cho tới khi được viết lại (phraseRE của gói ui) hoặc nằm trong loại trừ có lý do.
var looseFlagRE = regexp.MustCompile(`(?:^|[\s(` + "`" + `'"])--[a-z][a-z-]*`)

// argvTokenRE: literal chỉ là MỘT token đối số của lệnh ngoài ("--name-only", "--format=%H") truyền cho git/claude/codex, không phải
// thông điệp cho người đọc; bỏ qua theo cấu trúc, không cần liệt kê từng cờ.
var argvTokenRE = regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9-]*(?:=.*)?$`)

// inventoried: literal s có thuộc phạm vi kiểm kê không (nhắc "bint <lệnh>", hoặc có cờ --x đứng rời và không chỉ là một token argv).
func inventoried(s string) bool {
	if anyBintRE.MatchString(s) {
		return true
	}
	return looseFlagRE.MatchString(s) && !argvTokenRE.MatchString(s)
}

func scanLiterals(t *testing.T, dirs []string) []lit {
	t.Helper()
	var out []lit
	for _, d := range dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			t.Fatalf("đọc %s: %v", d, err)
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(d, e.Name())
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0) // 0: bỏ chú thích
			if err != nil {
				t.Fatalf("đọc %s: %v", path, err)
			}
			rel := strings.TrimPrefix(filepath.ToSlash(path), "../")
			ast.Inspect(f, func(n ast.Node) bool {
				bl, ok := n.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(bl.Value); err == nil && inventoried(s) {
					out = append(out, lit{rel, fset.Position(bl.Pos()).Line, s})
				}
				return true
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].line < out[j].line
	})
	return out
}

// checkInventory trả các vấn đề của kiểm kê (rỗng = đạt).
func checkInventory(cat *i18n.Catalog, lits []lit, excl []exclusion) []string {
	var problems []string
	used := make([]bool, len(excl))
	for _, l := range lits {
		matched := false
		for i, e := range excl {
			if e.reason == "" {
				problems = append(problems, "loại trừ thiếu lý do: "+e.file+" / "+e.contains)
			}
			if e.file == l.file && strings.Contains(l.text, e.contains) {
				used[i], matched = true, true
			}
		}
		if matched {
			continue
		}
		after := (ui.Rewriter{Cat: cat}).Rewrite(l.text)
		if topWordsRE.MatchString(after) {
			problems = append(problems, l.file+":"+strconv.Itoa(l.line)+": literal nhắc lệnh bint mà hàm viết lại không đổi hết (thêm đường lệnh vào bảng cliRefs của actions, hoặc vào danh sách loại trừ kèm lý do): "+strconv.Quote(l.text))
		} else if looseFlagRE.MatchString(after) {
			problems = append(problems, l.file+":"+strconv.Itoa(l.line)+": literal còn cờ CLI rời sau khi viết lại (thêm cụm vào phraseRE của gói ui, hoặc vào danh sách loại trừ kèm lý do): "+strconv.Quote(l.text))
		}
	}
	for i, e := range excl {
		if !used[i] {
			problems = append(problems, "loại trừ thừa (không còn literal nào khớp): "+e.file+" / "+e.contains)
		}
	}
	return problems
}

func TestServiceMessageCLIInventory(t *testing.T) {
	cat := i18n.MustLoad("vi")
	lits := scanLiterals(t, inventoryDirs)
	if len(lits) < 100 {
		t.Fatalf("quét được quá ít literal (%d): bộ quét hỏng?", len(lits))
	}
	if p := checkInventory(cat, lits, inventoryExclusions); len(p) > 0 {
		t.Fatalf("kiểm kê literal nhắc lệnh bint chưa đạt:\n  %s", strings.Join(p, "\n  "))
	}
	// Bản trước/sau của từng literal bị đổi, để người duyệt đọc (go test -v).
	changed := 0
	for _, l := range lits {
		if after := (ui.Rewriter{Cat: cat}).Rewrite(l.text); after != l.text {
			changed++
			t.Logf("%s:%d\n    trước: %s\n    sau:   %s", l.file, l.line, l.text, after)
		}
	}
	t.Logf("%d literal nhắc lệnh bint; %d bị viết lại; %d loại trừ", len(lits), changed, len(inventoryExclusions))
}

// Ca ngược: literal nhắc lệnh mới mà không xử lý thì đỏ; loại trừ thiếu lý do hoặc thừa cũng đỏ.
func TestServiceMessageCLIInventoryCatchesGaps(t *testing.T) {
	cat := i18n.MustLoad("vi")
	known := lit{"app/fake.go", 1, "chạy `bint knowledge verify --apply` để cập nhật"}
	if p := checkInventory(cat, []lit{known}, nil); len(p) != 0 {
		t.Fatalf("lệnh đã có trong bảng phải được xử lý: %v", p)
	}
	// Lệnh CLI thật nhưng chưa có hành động trong sổ (bint hook) → đỏ.
	unknown := lit{"app/fake.go", 2, "xem `bint hook claude` để biết thêm"}
	if p := checkInventory(cat, []lit{unknown}, nil); len(p) != 1 || !strings.Contains(p[0], "không đổi hết") {
		t.Fatalf("literal mới chưa xử lý phải đỏ: %v", p)
	}
	// Cờ CLI rời mà hàm viết lại không đổi → đỏ (.12); cụm cờ đã có khoá cliref.flag.* thì xanh.
	loose := lit{"app/fake.go", 3, "chạy `bint doctor` rồi thêm --xyz"}
	if p := checkInventory(cat, []lit{loose}, nil); len(p) != 1 || !strings.Contains(p[0], "cờ CLI rời") {
		t.Fatalf("cờ rời chưa xử lý phải đỏ: %v", p)
	}
	handled := lit{"app/fake.go", 4, "xem `bint task show X`; người có thể --force, hoặc dùng --unverified"}
	if p := checkInventory(cat, []lit{handled}, nil); len(p) != 0 {
		t.Fatalf("cụm cờ rời đã có khoá phải được viết lại: %v", p)
	}
	// Review .3: literal chỉ có cờ rời MỚI, không kèm "bint" và không thuộc cụm nào đã biết, phải vào phạm vi quét và làm test đỏ.
	frob := lit{"app/fake.go", 5, "dùng --frobnicate nếu muốn"}
	if !inventoried(frob.text) {
		t.Fatalf("literal chỉ có cờ rời mới phải vào phạm vi kiểm kê: %q", frob.text)
	}
	if p := checkInventory(cat, []lit{frob}, nil); len(p) != 1 || !strings.Contains(p[0], "cờ CLI rời") {
		t.Fatalf("cờ rời mới chưa xử lý phải đỏ: %v", p)
	}
	// Cờ rời có cụm (cliref.flag.*) thì xanh dù không có "bint"; loại trừ có lý do cũng xanh.
	if p := checkInventory(cat, []lit{{"app/fake.go", 6, "dùng --force nếu vẫn muốn duyệt"}}, nil); len(p) != 0 {
		t.Fatalf("cụm cờ rời đã có khoá phải được viết lại: %v", p)
	}
	if p := checkInventory(cat, []lit{frob}, []exclusion{{"app/fake.go", "--frobnicate", rUsage}}); len(p) != 0 {
		t.Fatalf("loại trừ có lý do phải xanh: %v", p)
	}
	// Một token argv của lệnh ngoài (--x hoặc --x=giá trị, đứng một mình) được bỏ qua theo cấu trúc; câu có cờ rời thì không.
	for lit, want := range map[string]bool{
		"--name-only": false, "--format=%H": false, "-n": false, "--git-dir=": false,
		"dùng --frobnicate nếu muốn": true, "--name-only rồi tiếp": true, "(--git local)": true, "chạy `bint doctor`": true, "không có gì": false,
	} {
		if got := inventoried(lit); got != want {
			t.Errorf("inventoried(%q) = %v, muốn %v", lit, got, want)
		}
	}
	// Đưa vào loại trừ (có lý do) thì xanh; loại trừ không khớp literal nào thì đỏ; thiếu lý do thì đỏ.
	ok := []exclusion{{"app/fake.go", "bint hook claude", rCLI}}
	if p := checkInventory(cat, []lit{unknown}, ok); len(p) != 0 {
		t.Fatalf("loại trừ có lý do phải xanh: %v", p)
	}
	if p := checkInventory(cat, []lit{known}, ok); len(p) != 1 || !strings.Contains(p[0], "thừa") {
		t.Fatalf("loại trừ thừa phải đỏ: %v", p)
	}
	if p := checkInventory(cat, []lit{unknown}, []exclusion{{"app/fake.go", "bint hook claude", ""}}); len(p) == 0 {
		t.Fatal("loại trừ thiếu lý do phải đỏ")
	}
}
