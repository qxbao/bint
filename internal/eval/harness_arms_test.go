package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/ctxengine"
	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// ghi chú MCP gốc của nhánh bint trước H (hợp đồng đo: số liệu cũ còn so được).
const oldBintToolsNote = `
## Công cụ tri thức (Bint)

MCP server ` + "`bint`" + ` có tri thức về repository này (quy tắc, điểm dễ sai, hợp đồng hành vi).
Các công cụ có thể chưa được nạp sẵn: nạp bằng ToolSearch với truy vấn
"select:mcp__bint__context_for_task,mcp__bint__context_for_paths,mcp__bint__cite_knowledge".

1. Đầu tác vụ: gọi mcp__bint__context_for_task với mô tả tác vụ (và các tệp đã biết).
2. Trước khi sửa một tệp: gọi mcp__bint__context_for_paths với đường dẫn tệp.
3. Khi áp dụng một mục tri thức: gọi mcp__bint__cite_knowledge với ID của mục.

Trước khi đọc/sửa tệp, quy tắc khớp đường dẫn cũng có thể được chèn tự động.
`

func hmeta(id, status string) string {
	return "id: " + id + "\nscope: global\nstatus: " + status + "\norigin: human\nversion: 1\n"
}

// harnessRepo dựng repository có tri thức, thành phần quy trình trong .bint/harness (chưa commit
// vào lịch sử của tác vụ: nhánh bint đọc bản hiện tại) và một tác vụ.
func harnessRepo(t *testing.T) (Env, *Case) {
	t.Helper()
	r := newKRepo(t)
	write := func(rel, body string) { writeFile(t, r.root, ".bint/harness/"+rel, body) }
	write("guidance/run-tests.md", "---\n"+hmeta("run-tests", "verified")+"title: Chạy test\n---\nChạy test của gói đã sửa trước khi kết thúc.\n")
	write("guardrails/test-before-stop.yml", hmeta("test-before-stop", "verified")+
		"on: stop\nwhen: { edited: \"lib/**\" }\nunless: { ran: test }\naction: remind\nmessage: Chưa chạy test cho phần đã sửa.\n")
	write("guardrails/draft-no-push.yml", hmeta("draft-no-push", "candidate")+
		"on: pre_bash\nwhen: { command: \"git push\" }\naction: deny\nmessage: Không push.\n")
	write("scripts/run-it.yml", hmeta("run-it", "verified")+"description: Chạy test đúng chỗ\nusage: .bint/harness/scripts/run-it.sh <tệp>\n")
	write("scripts/run-it.sh", "#!/bin/sh\necho ok\n")
	env := knowledgeEnv(t, r.root)
	env.BintExe = "/usr/bin/bint"
	return env, &Case{ID: "c1", Source: Source{Base: r.base, Commit: r.sol}, SourceFiles: []string{"lib/a.go"}}
}

type kRepo struct{ root, base, sol string }

func newKRepo(t *testing.T) kRepo {
	t.Helper()
	root := testutil.NewRepo(t, map[string]string{"lib/a.go": "package lib\n\nfunc A() {}\n"})
	it := saveItem(t, root, knowledge.TypeWarning, "A phải giữ chữ ký", knowledge.Evidence{Path: "lib/a.go", Lines: "3"})
	it.Scope.Paths = []string{"lib/**"}
	knowledge.NewStore(root).Save(it)
	base := commitAll(t, root, "knowledge at base")
	writeFile(t, root, "lib/a.go", "package lib\n\nfunc A() {}\n\nfunc C() {}\n")
	sol := commitAll(t, root, "solution")
	return kRepo{root, base, sol}
}

func readStr(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Nhánh bint không có .bint/harness, hoặc bint@harness=off: AGENTS.md kết thúc bằng đúng từng byte
// ghi chú MCP cũ, settings không có hook guardrail, không có bản dựng thành phần.
func TestBintArmHarnessOffEqualsLegacy(t *testing.T) {
	ctx := context.Background()
	r := newKRepo(t)
	env := knowledgeEnv(t, r.root)
	env.BintExe = "/usr/bin/bint"
	c := &Case{ID: "c1", Source: Source{Base: r.base, Commit: r.sol}, SourceFiles: []string{"lib/a.go"}}

	legacyDir, legacyLog := t.TempDir(), t.TempDir()
	ls, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, legacyDir, legacyLog)
	if err != nil {
		t.Fatal(err)
	}
	legacyAgents := readStr(t, filepath.Join(legacyDir, "AGENTS.md"))
	if !strings.HasSuffix(legacyAgents, oldBintToolsNote) {
		t.Fatalf("AGENTS.md phải kết thúc bằng đúng ghi chú MCP cũ:\n%s", legacyAgents)
	}
	if ls.HarnessDirSet() || len(ls.Guardrails) != 0 || strings.Contains(readStr(t, ls.Settings), "Stop") {
		t.Fatalf("không có thành phần thì không có hook guardrail: %+v", ls)
	}

	// Repo có thành phần nhưng harness=off → giống hệt (kể cả ContextSource).
	env2, c2 := harnessRepo(t)
	offDir, offLog := t.TempDir(), t.TempDir()
	os2, err := prepareArm(ctx, env2, "bint@harness=off", LeakStrict, c2, nil, offDir, offLog)
	if err != nil {
		t.Fatal(err)
	}
	offAgents := readStr(t, filepath.Join(offDir, "AGENTS.md"))
	if !strings.HasSuffix(offAgents, oldBintToolsNote) || strings.Contains(offAgents, "Chạy test của gói") || strings.Contains(offAgents, "Script hỗ trợ") {
		t.Fatalf("harness=off:\n%s", offAgents)
	}
	if strings.Contains(readStr(t, os2.Settings), "Stop") || os2.HarnessDirSet() || len(os2.Guardrails) != 0 || strings.Contains(os2.ContextSource, "+harness#") {
		t.Fatalf("harness=off: %+v", os2)
	}
	if _, err := os.Stat(filepath.Join(offDir, ".bint", "harness")); err == nil {
		t.Fatal("harness=off không chép script")
	}
	// mcp=off: không có ghi chú MCP.
	nm, err := prepareArm(ctx, env, "bint@mcp=off", LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = nm
}

func (s ArmSetup) HarnessDirSet() bool {
	if s.EvalConfig == "" {
		return false
	}
	ec, err := ctxengine.LoadEvalConfig(s.EvalConfig)
	return err == nil && ec.HarnessDir != ""
}

func TestBintArmWithHarnessComponents(t *testing.T) {
	ctx := context.Background()
	env, c := harnessRepo(t)

	dir, logDir := t.TempDir(), t.TempDir()
	s, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, dir, logDir)
	if err != nil {
		t.Fatal(err)
	}
	agents := readStr(t, filepath.Join(dir, "AGENTS.md"))
	for _, want := range []string{oldBintToolsNote, "## Chạy test\n\nChạy test của gói đã sửa", "## Script hỗ trợ (Bint)", ".bint/harness/scripts/run-it.sh"} {
		if !strings.Contains(agents, want) {
			t.Fatalf("AGENTS.md thiếu %q:\n%s", want, agents)
		}
	}
	if !strings.Contains(s.ContextSource, "+harness#") || len(s.Guardrails) != 1 || s.Guardrails[0] != "test-before-stop" {
		t.Fatalf("setup: %+v", s)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(readStr(t, s.Settings)), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Hooks["Stop"]) != 1 || len(settings.Hooks["PostToolUse"]) != 1 || len(settings.Hooks["PostToolUseFailure"]) != 1 {
		t.Fatalf("settings: %v", settings.Hooks)
	}
	matchers := []string{}
	for _, g := range settings.Hooks["PreToolUse"] {
		matchers = append(matchers, g.Matcher)
	}
	if strings.Join(matchers, "|") != "Read|Edit|Write|MultiEdit|NotebookEdit|Bash" {
		t.Fatalf("PreToolUse: %v", matchers)
	}
	// Bản dựng nằm ngoài thư mục làm việc; script được chép vào thư mục làm việc.
	ec, _ := ctxengine.LoadEvalConfig(s.EvalConfig)
	if !strings.HasPrefix(ec.HarnessDir, logDir) || strings.HasPrefix(ec.HarnessDir, dir) {
		t.Fatalf("HarnessDir = %s", ec.HarnessDir)
	}
	lib := hcomp.LoadDir(ec.HarnessDir)
	if _, ok := lib.Find(hcomp.KindGuardrail, "test-before-stop"); !ok || len(lib.Components) != 3 {
		t.Fatalf("bản dựng: %+v %v", lib.Components, lib.Problems)
	}
	if _, ok := lib.Find(hcomp.KindGuardrail, "draft-no-push"); ok {
		t.Fatal("candidate không vào bản dựng ở chế độ on")
	}
	fi, err := os.Stat(filepath.Join(dir, ".bint", "harness", "scripts", "run-it.sh"))
	if err != nil {
		t.Fatalf("script phải được chép vào thư mục làm việc: %v", err)
	}
	// Windows không có bit thực thi Unix: chỉ kiểm ở nơi hệ tệp biểu diễn được.
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("script phải có bit thực thi: %v", fi.Mode())
	}

	// harness=all: thêm cả candidate.
	_, allLog := t.TempDir(), t.TempDir()
	as, err := prepareArm(ctx, env, "bint@harness=all", LeakStrict, c, nil, t.TempDir(), allLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(as.Guardrails) != 2 {
		t.Fatalf("harness=all: %v", as.Guardrails)
	}
	if _, err := prepareArm(ctx, env, "bint@harness=maybe", LeakStrict, c, nil, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("harness=maybe phải lỗi")
	}
	if _, _, err := SplitArm("minimal@harness=on"); err == nil {
		t.Fatal("chỉ nhánh bint nhận biến thể")
	}
}

// Thành phần chứa mã của lời giải thì nhánh không khả dụng cho tác vụ đó (như skill).
func TestBintArmHarnessLeakCheck(t *testing.T) {
	ctx := context.Background()
	env, c := harnessRepo(t)
	leak := &LeakInput{Commit: "sol", AddedLines: []string{"func C() { return secretSolutionCall(42) }"},
		NewIdents: map[string]bool{"secretSolutionCall": true}, TestIdents: map[string]bool{}}
	if _, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, leak, t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("thành phần sạch phải qua: %v", err)
	}
	writeFile(t, env.BintRoot, ".bint/harness/guidance/leak.md", "---\n"+hmeta("leak", "verified")+"---\nGọi:\n\nfunc C() { return secretSolutionCall(42) }\n")
	_, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, leak, t.TempDir(), t.TempDir())
	if !errors.Is(err, ErrArmUnavailable) {
		t.Fatalf("thành phần chứa mã lời giải phải làm nhánh không khả dụng: %v", err)
	}
	// harness=off bỏ qua thành phần trong repository nên không bị loại.
	if _, err := prepareArm(ctx, env, "bint@harness=off", LeakStrict, c, leak, t.TempDir(), t.TempDir()); err != nil {
		t.Fatalf("harness=off: %v", err)
	}
}

// Đối chiếu base/candidate của đề xuất quy trình: hai tập khác nhau, cùng một tập tri thức.
func TestVariantHarnessBaseDiffersFromCandidate(t *testing.T) {
	ctx := context.Background()
	r := newKRepo(t)
	env := knowledgeEnv(t, r.root)
	env.BintExe = "/usr/bin/bint"
	c := &Case{ID: "c1", Source: Source{Base: r.base, Commit: r.sol}, SourceFiles: []string{"lib/a.go"}}
	guard := hcomp.Component{Kind: hcomp.KindGuardrail,
		Meta: hcomp.Meta{ID: "g1", Scope: hcomp.ScopeGlobal, Status: hcomp.StatusCandidate, Origin: hcomp.OriginAgent, Version: 1},
		On:   hcomp.OnStop, When: hcomp.When{Edited: "**"}, Unless: hcomp.Unless{Ran: hcomp.RanTest}, Action: hcomp.ActRemind, Message: "Chạy test."}
	env.Variant = &Variant{Delivery: ArmBint, Label: "p1", Harness: func(_ context.Context, arm string, _ *Case, _ *LeakInput) (hcomp.Set, error) {
		set := (*hcomp.Library)(nil).Select(hcomp.ModeOff)
		if arm == ArmCandidate {
			set.Components = append(set.Components, guard)
		}
		return set, nil
	}}
	bs, err := prepareArm(ctx, env, ArmBase, LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cs, err := prepareArm(ctx, env, ArmCandidate, LeakStrict, c, nil, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(bs.Guardrails) != 0 || strings.Contains(readStr(t, bs.Settings), "Stop") {
		t.Fatalf("base: %+v", bs)
	}
	if len(cs.Guardrails) != 1 || !strings.Contains(readStr(t, cs.Settings), "Stop") || !strings.Contains(cs.ContextSource, "{candidate:p1}") {
		t.Fatalf("candidate: %+v", cs)
	}
	// Đề xuất quy trình bắt buộc delivery bint.
	env.Variant.Delivery = ArmMinimal
	if _, err := prepareArm(ctx, env, ArmCandidate, LeakStrict, c, nil, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("harness variant với delivery minimal phải lỗi")
	}
}

// Hook của lượt eval thực thi guardrail từ bản dựng, ghi nhật ký; số liệu vào CSDL và báo cáo.
func TestEvalHookGuardrailLogAndReport(t *testing.T) {
	ctx := context.Background()
	env, c := harnessRepo(t)
	dir, logDir := t.TempDir(), t.TempDir()
	s, err := prepareArm(ctx, env, ArmBint, LeakStrict, c, nil, dir, logDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ctxengine.EvalEnv, s.EvalConfig)
	call := func(ev string, extra hookcore.Input) hookcore.Result {
		extra.SessionID, extra.Cwd, extra.Event = "S1", dir, ev
		res, err := hookcore.Handle(ctx, extra, claude.Dialect, hookcore.Env{})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// Chưa sửa gì: Stop không chặn.
	if r := call(hookcore.EvStop, hookcore.Input{}); r.Output != "" {
		t.Fatalf("chưa sửa: %q", r.Output)
	}
	call(hookcore.EvPostTool, hookcore.Input{ToolName: "Edit", ToolInput: map[string]any{"file_path": filepath.Join(dir, "lib", "a.go")}})
	r := call(hookcore.EvStop, hookcore.Input{})
	if !strings.Contains(r.Output, `"decision":"block"`) || !strings.Contains(r.Output, "Chưa chạy test") {
		t.Fatalf("Stop phải chặn: %q", r.Output)
	}
	call(hookcore.EvPostTool, hookcore.Input{ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./lib/..."}})
	if r := call(hookcore.EvStop, hookcore.Input{StopHookActive: true}); r.Output != "" {
		t.Fatalf("đã làm theo: %q", r.Output)
	}
	// Bản dựng nằm trong logDir: agent không thấy, nhưng PreToolUse Bash của guardrail candidate
	// (không có hiệu lực ở chế độ on) không chặn.
	if r := call(hookcore.EvPreTool, hookcore.Input{ToolName: "Bash", ToolInput: map[string]any{"command": "git push"}}); r.Output != "" {
		t.Fatalf("candidate không có hiệu lực: %q", r.Output)
	}

	lines, _ := ctxengine.ReadLog(s.ContextLog)
	var outcomes []string
	for _, l := range lines {
		if l.Channel == ctxengine.ChannelGuardrail {
			outcomes = append(outcomes, l.Guardrail+":"+l.Outcome)
		}
	}
	if strings.Join(outcomes, ",") != "test-before-stop:eligible,test-before-stop:fired,test-before-stop:followed" {
		t.Fatalf("nhật ký guardrail: %v", outcomes)
	}

	_, _, db := testEnv(t, env.RepoDir)
	if err := createBatch(ctx, db, Batch{BatchID: "eb_g", Status: "completed", Harness: "command", LeakageMode: LeakStrict,
		Arms: []string{ArmBint}, Repeats: 2, CaseIDs: []string{"c1"}, StartedAt: "2026-01-01T00:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"ea_g1", "ea_g2"} {
		row := AttemptRow{AttemptID: id, BatchID: "eb_g", CaseID: "c1", CaseFingerprint: "x", Arm: ArmBint, Repeat: i + 1,
			Harness: "command", StartedAt: "2026-01-01T00:00:00.000Z"}
		if err := startAttempt(ctx, db, row); err != nil {
			t.Fatal(err)
		}
	}
	// Lượt 1: nhật ký thật ở trên. Lượt 2: guardrail có hiệu lực nhưng không bao giờ đủ điều kiện.
	if err := recordGuardrailUsage(ctx, db, "ea_g1", s); err != nil {
		t.Fatal(err)
	}
	idle := s
	idle.ContextLog = filepath.Join(t.TempDir(), "none.jsonl")
	if err := recordGuardrailUsage(ctx, db, "ea_g2", idle); err != nil {
		t.Fatal(err)
	}
	st, err := batchGuardrails(ctx, db, "eb_g", 0)
	if err != nil || len(st) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	g := st[0]
	if g.Arm != ArmBint || g.Guardrail != "test-before-stop" || g.Attempts != 2 || g.Eligible != 1 || g.Fired != 1 ||
		g.FiredCount != 1 || g.Followed != 1 || g.Blocked != 1 || g.Bypass != 0 {
		t.Fatalf("số liệu: %+v", g)
	}
	if st, _ := batchGuardrails(ctx, db, "eb_g", 1); len(st) != 1 || st[0].Attempts != 1 {
		t.Fatalf("lọc theo lần lặp: %+v", st)
	}
	b, _ := GetBatch(ctx, db, "eb_g")
	rep, err := BuildReport(ctx, db, b, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Guardrails) != 1 || rep.Guardrails[0].Fired != 1 {
		t.Fatalf("báo cáo: %+v", rep.Guardrails)
	}
}

// Chạy thật nhánh base/candidate của đề xuất quy trình (agent giả bằng lệnh shell): runner ghi số
// liệu guardrail cho nhánh có guardrail (kể cả guardrail không bao giờ đủ điều kiện) và không ghi
// gì cho nhánh không có; AGENTS.md của hai nhánh khác nhau đúng ở chỉ dẫn của đề xuất.
func TestRunHarnessVariantRecordsGuardrailUsage(t *testing.T) {
	needSh(t)
	testutil.Isolate(t)
	ctx := context.Background()
	root, solution, _ := fixtureRepo(t)
	env, st, db := testEnv(t, root)
	env.Cfg.Eval.MinimalSource = "knowledge"
	env.BintRoot = root
	env.BintExe = "/usr/bin/bint"
	res, err := Mine(ctx, root, env.Cfg.Eval.Mine, st, MineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var good *Case
	for _, c := range res.New {
		if c.Source.Commit == solution {
			good = c
		}
	}
	if good == nil {
		t.Fatal("thiếu tác vụ")
	}
	if v := VerifyCase(ctx, env, good, 1, testNow()); v.Status != "verified" {
		t.Fatalf("verify: %+v", v)
	}
	good.Verification = &Verification{Status: "verified"}
	good.Split = SplitValidation

	guard := hcomp.Component{Kind: hcomp.KindGuardrail, Meta: hcomp.Meta{ID: "g1", Scope: hcomp.ScopeGlobal, Status: hcomp.StatusVerified, Origin: hcomp.OriginAgent, Version: 1},
		On: hcomp.OnStop, When: hcomp.When{Edited: "**"}, Unless: hcomp.Unless{Ran: hcomp.RanTest}, Action: hcomp.ActRemind, Message: "Chạy test."}
	tips := hcomp.Component{Kind: hcomp.KindGuidance, Meta: hcomp.Meta{ID: "tips", Scope: hcomp.ScopeGlobal, Status: hcomp.StatusVerified, Origin: hcomp.OriginAgent, Version: 1},
		Title: "Mẹo của đề xuất", Body: "Chạy test trước khi báo xong."}
	env.Variant = &Variant{Delivery: ArmBint, Label: "ep_h", Harness: func(_ context.Context, arm string, _ *Case, _ *LeakInput) (hcomp.Set, error) {
		set := (*hcomp.Library)(nil).Select(hcomp.ModeOff)
		if arm == ArmCandidate {
			set.Components = append(set.Components, guard, tips)
		}
		return set, nil
	}}
	agentDir := t.TempDir()
	writeFiles(t, agentDir, map[string]string{"agent.sh": `
cp AGENTS.md "$(dirname "$BINT_EVAL_PROMPT_FILE")/agents.txt"
echo '{"type":"result","num_turns":1,"total_cost_usd":0.01}'
`})
	budget := 10.0
	out, err := Run(ctx, env, db, RunOptions{Cases: []*Case{good}, Arms: []string{ArmBase, ArmCandidate}, Repeats: 1, LeakageMode: LeakEconomy,
		Agent: &CommandAgent{Command: "sh " + filepath.ToSlash(filepath.Join(agentDir, "agent.sh"))}, BudgetUSD: &budget, Seed: 3, BintVersion: "test", Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if out.Done != 2 {
		t.Fatalf("run: %+v", out)
	}
	rows, _ := Attempts(ctx, db, "batch_id = ?", out.BatchID)
	for _, r := range rows {
		agents := readStr(t, filepath.Join(r.LogDir, "agents.txt"))
		hasTips := strings.Contains(agents, "Mẹo của đề xuất")
		if (r.Arm == ArmCandidate) != hasTips || !strings.Contains(agents, "Công cụ tri thức (Bint)") {
			t.Fatalf("%s: AGENTS.md\n%s", r.Arm, agents)
		}
		if r.Arm == ArmCandidate && !strings.Contains(r.ContextSource, "+harness#") || r.Arm == ArmBase && strings.Contains(r.ContextSource, "+harness#") {
			t.Fatalf("%s: nguồn ngữ cảnh %s", r.Arm, r.ContextSource)
		}
	}
	stats, err := batchGuardrails(ctx, db, out.BatchID, 0)
	if err != nil || len(stats) != 1 || stats[0].Arm != ArmCandidate || stats[0].Guardrail != "g1" || stats[0].Attempts != 1 || stats[0].Fired != 0 {
		t.Fatalf("số liệu guardrail: %+v %v", stats, err)
	}
}

// Migration 0009 (H2): bảng số liệu guardrail có đủ cột (store.Open áp mọi migration).
func TestGuardrailUsageTableColumns(t *testing.T) {
	_, _, db := testEnv(t, testutil.NewRepo(t, nil))
	rows, err := db.Query(`SELECT name FROM pragma_table_info('eval_guardrail_usage') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		cols = append(cols, n)
	}
	if got := strings.Join(cols, ","); got != "attempt_id,guardrail,eligible,fired,followed,blocked,bypass" {
		t.Fatalf("cột = %s", got)
	}
}
