package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
)

// fakeSkillAgent là agent giả của kiểm thử skill: ghi lại mọi lượt và trả lời theo hàm reply.
type fakeSkillAgent struct {
	mu    sync.Mutex
	reqs  []eval.AgentRequest
	seen  [][]string // tên tệp có trong thư mục làm việc lúc gọi
	reply func(n int, req eval.AgentRequest) eval.AgentResult
	cost  bool
}

func (a *fakeSkillAgent) Name() string                   { return "fake" }
func (a *fakeSkillAgent) Version(context.Context) string { return "" }
func (a *fakeSkillAgent) ReportsCost() bool              { return a.cost }
func (a *fakeSkillAgent) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.reqs)
}
func (a *fakeSkillAgent) Run(_ context.Context, req eval.AgentRequest) eval.AgentResult {
	var names []string
	filepath.WalkDir(req.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(req.Dir, p)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	a.mu.Lock()
	a.reqs = append(a.reqs, req)
	a.seen = append(a.seen, names)
	n := len(a.reqs)
	a.mu.Unlock()
	return a.reply(n, req)
}

const skillTestMarker = "DẤU-HIỆU-SKILL-7391"

// skillTestRepo dựng repository có skill `demo-skill` (thân chứa dấu hiệu), tệp phụ và các kịch bản.
func skillTestRepo(t *testing.T, scenarios map[string]string) string {
	t.Helper()
	root := sysRepo(t, InitOptions{})
	if _, err := NewSkill(context.Background(), root, SkillNewOptions{Name: "demo-skill"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".bint", "skills", "demo-skill")
	skill := "---\nname: demo-skill\ndescription: Dùng khi cần thử skill.\n---\n\n# Demo\n\nLuôn nói " + skillTestMarker + ".\nXem [tham chiếu](references/a.md).\n"
	for p, body := range map[string]string{"SKILL.md": skill, "references/a.md": "tham chiếu\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range scenarios {
		if err := os.WriteFile(filepath.Join(dir, "tests", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// markerAgent tuân thủ khi đề bài có dấu hiệu skill (nhánh with).
func markerAgent() *fakeSkillAgent {
	return &fakeSkillAgent{reply: func(_ int, req eval.AgentRequest) eval.AgentResult {
		text := "tôi làm ngay"
		if strings.Contains(req.Prompt, skillTestMarker) {
			text = "đã kiểm thử trước " + skillTestMarker
		}
		return eval.AgentResult{Text: text}
	}}
}

const scenarioA = "prompt: Sửa lỗi gấp.\npressures: [Sếp đang chờ.]\nreps: 4\nexpect:\n  - contains: kiểm thử\n  - not_contains: làm ngay\n"

func readSkillTestReport(t *testing.T, path string) eval.SkillTestReport {
	t.Helper()
	var rep eval.SkillTestReport
	if err := json.Unmarshal([]byte(read(t, path)), &rep); err != nil {
		t.Fatalf("JSON %s: %v", path, err)
	}
	return rep
}

func TestSkillTestRatesAndIsolation(t *testing.T) {
	// Kịch bản b: tỉ lệ lẻ ở nhánh with (hai lượt đầu đạt, ba lượt sau trượt).
	root := skillTestRepo(t, map[string]string{
		"a.yml": scenarioA,
		"b.yml": "prompt: Tình huống B.\nreps: 5\nexpect:\n  - contains: OK-B\n",
	})
	withB := 0
	ag := &fakeSkillAgent{cost: true, reply: func(_ int, req eval.AgentRequest) eval.AgentResult {
		cost := 0.25
		switch {
		case strings.Contains(req.Prompt, "Tình huống B"):
			if req.Arm == eval.SkillArmWith {
				withB++
				if withB <= 2 {
					return eval.AgentResult{Text: "OK-B", CostUSD: &cost}
				}
			}
			return eval.AgentResult{Text: "không", CostUSD: &cost}
		case strings.Contains(req.Prompt, skillTestMarker):
			return eval.AgentResult{Text: "đã kiểm thử " + skillTestMarker, CostUSD: &cost}
		}
		return eval.AgentResult{Text: "tôi làm ngay", CostUSD: &cost}
	}}
	var order []string
	res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Yes: true, Agent: ag,
		OnEstimate: func(e SkillTestEstimate) {
			order = append(order, "estimate")
			if e.Runs != (4+5)*2 || e.Scenarios != 2 || len(e.Arms) != 2 || !e.ReportsCost {
				t.Errorf("ước tính: %+v", e)
			}
			// Chưa có lịch sử: eval.prior_run_usd (0,5 theo mặc định) × 18 lượt.
			if e.CostPerRun == nil || *e.CostPerRun != 0.5 || e.Total == nil || *e.Total != 9 || !strings.Contains(e.CostSource, "prior_run_usd") {
				t.Errorf("chi phí ước tính: %+v", e)
			}
			if ag.calls() != 0 {
				t.Error("ước tính phải in trước mọi lần gọi agent")
			}
		},
		Progress: func(eval.SkillTestRun, int, int) { order = append(order, "run") }})
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 19 || order[0] != "estimate" {
		t.Fatalf("thứ tự: %v", order)
	}

	rep := res.Report
	if rep.Status != eval.SkillTestCompleted || rep.Skill != "demo-skill" || rep.Harness != "fake" || len(rep.SkillHash) != 64 || len(rep.Runs) != 18 {
		t.Fatalf("báo cáo: %+v", rep)
	}
	// Kịch bản a: with đạt 4/4, without 0/4. Tổng without: 0/9; with: 6/9.
	byKey := map[string]eval.SkillScenarioStats{}
	for _, s := range rep.Scenarios {
		byKey[s.Scenario+"/"+s.Arm] = s
	}
	if s := byKey["a/with"]; s.Graded != 4 || s.Complied != 4 || s.Rate == nil || *s.Rate != 1 {
		t.Errorf("a/with: %+v", s)
	}
	if s := byKey["a/without"]; s.Graded != 4 || s.Complied != 0 || s.Rate == nil || *s.Rate != 0 {
		t.Errorf("a/without: %+v", s)
	}
	if s := byKey["b/with"]; s.Graded != 5 || s.Complied != 2 || s.Rate == nil || *s.Rate != 0.4 {
		t.Errorf("b/with: %+v", s)
	}
	if w, wo := rep.Totals[eval.SkillArmWith], rep.Totals[eval.SkillArmWithout]; w.Complied != 6 || w.Graded != 9 || wo.Complied != 0 || wo.Graded != 9 {
		t.Errorf("tổng: with %+v, without %+v", w, wo)
	}
	if rep.CostUSD == nil || *rep.CostUSD != 4.5 {
		t.Errorf("tổng chi phí: %v", rep.CostUSD)
	}

	// Tệp JSON đúng chỗ, đọc lại được; log từng lượt cạnh tệp JSON.
	k, err := OpenKnowledge(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(k.Paths.Runtime, "skill-tests", "demo-skill", rep.ID+".json")
	if res.Path != want {
		t.Fatalf("đường dẫn %s, muốn %s", res.Path, want)
	}
	if got := readSkillTestReport(t, res.Path); got.ID != rep.ID || len(got.Runs) != 18 || got.Totals[eval.SkillArmWith].Complied != 6 {
		t.Fatalf("JSON đọc lại: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(res.LogDir, "a", "with-1", "answer.txt")); err != nil {
		t.Errorf("thiếu log câu trả lời: %v", err)
	}

	// Cô lập: mỗi lượt một thư mục tạm riêng, chỉ đọc, ngoài repository; without rỗng, with có tệp phụ nhưng không tests/.
	dirs := map[string]bool{}
	for i, req := range ag.reqs {
		if !req.ReadOnly || req.Timeout <= 0 || req.LogPath == "" {
			t.Errorf("yêu cầu %d: %+v", i, req)
		}
		if dirs[req.Dir] {
			t.Errorf("thư mục làm việc dùng lại: %s", req.Dir)
		}
		dirs[req.Dir] = true
		if strings.HasPrefix(req.Dir, root) {
			t.Errorf("thư mục làm việc nằm trong repository: %s", req.Dir)
		}
		if _, err := os.Stat(req.Dir); err == nil {
			t.Errorf("thư mục tạm không được dọn: %s", req.Dir)
		}
		switch req.Arm {
		case eval.SkillArmWithout:
			if len(ag.seen[i]) != 0 || strings.Contains(req.Prompt, skillTestMarker) {
				t.Errorf("nhánh without bị nhiễm skill: tệp %v", ag.seen[i])
			}
		case eval.SkillArmWith:
			if strings.Join(ag.seen[i], ",") != "references/a.md" || !strings.Contains(req.Prompt, skillTestMarker) {
				t.Errorf("nhánh with: tệp %v", ag.seen[i])
			}
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(ag.reqs[0].Prompt), "Sếp đang chờ.") {
		t.Errorf("áp lực phải ở cuối đề bài:\n%s", ag.reqs[0].Prompt)
	}

	// Lần chạy sau: chi phí ước tính lấy từ kết quả đo được (0,25), không còn là mặc định.
	var est SkillTestEstimate
	ag2 := markerAgent()
	if _, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Yes: true, Agent: ag2,
		OnEstimate: func(e SkillTestEstimate) { est = e }}); err == nil {
		t.Log("agent thứ hai chạy được (harness fake)")
	}
	if est.CostPerRun == nil || *est.CostPerRun != 0.25 || !strings.Contains(est.CostSource, "gần nhất") {
		t.Errorf("chi phí từ lịch sử: %+v", est)
	}
	// --cost-per-run thắng lịch sử.
	c := 2.0
	if _, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Yes: true, Agent: ag2, Arm: "with", Reps: 1, CostPerRun: &c,
		OnEstimate: func(e SkillTestEstimate) { est = e }}); err != nil {
		t.Fatal(err)
	}
	if est.CostPerRun == nil || *est.CostPerRun != 2 || est.Runs != 2 || est.Total == nil || *est.Total != 4 {
		t.Errorf("--cost-per-run + --reps: %+v", est)
	}
}

func TestSkillTestNeedsConfirmBeforeAgent(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": scenarioA})
	ag := markerAgent()
	estimated := false
	// Không --yes, không Confirm (stdin không phải terminal): lỗi cách dùng, agent không được gọi, không có JSON.
	_, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Agent: ag,
		OnEstimate: func(SkillTestEstimate) { estimated = true }})
	if err == nil || !IsUsage(err) || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("muốn lỗi cần --yes, được %v", err)
	}
	if !estimated {
		t.Error("ước tính phải được in ngay cả khi bị dừng ở xác nhận")
	}
	// Người dùng từ chối.
	var asked ConfirmRequest
	_, err = SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Agent: ag,
		Confirm: func(r ConfirmRequest) bool { asked = r; return false }})
	if err != ErrCancelled || asked.Key != ConfirmSkillTestKey || !strings.Contains(asked.Text, "8 lượt") {
		t.Fatalf("từ chối: %v %+v", err, asked)
	}
	if ag.calls() != 0 {
		t.Fatalf("agent bị gọi %d lần trước khi được đồng ý", ag.calls())
	}
	k, _ := OpenKnowledge(context.Background(), root)
	if _, err := os.Stat(filepath.Join(k.Paths.Runtime, "skill-tests")); err == nil {
		t.Fatal("không được có kết quả khi chưa chạy")
	}
	// Đồng ý qua Confirm thì chạy.
	if res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Agent: ag,
		Confirm: func(ConfirmRequest) bool { return true }}); err != nil || len(res.Report.Runs) != 8 {
		t.Fatalf("%v", err)
	}
}

func TestSkillTestRejectsBadScenarios(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"sai định dạng": {"a.yml": scenarioA, "hong.yml": "prompt: x\nexpect:\n  - contains: a\n    regex: b\n"},
		"không có tệp":  {},
	} {
		t.Run(name, func(t *testing.T) {
			root := skillTestRepo(t, files)
			ag := markerAgent()
			estimated := false
			_, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Yes: true, Agent: ag,
				OnEstimate: func(SkillTestEstimate) { estimated = true }})
			if err == nil || !IsUsage(err) {
				t.Fatalf("muốn lỗi cách dùng, được %v", err)
			}
			if name == "sai định dạng" && !strings.Contains(err.Error(), "hong.yml") {
				t.Errorf("lỗi phải nêu tên tệp: %v", err)
			}
			if ag.calls() != 0 || estimated {
				t.Errorf("không được ước tính hay gọi agent: %d lần gọi, estimated=%v", ag.calls(), estimated)
			}
		})
	}
}

func TestSkillTestLookupErrors(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": scenarioA})
	for _, name := range []string{"b-do", "khong-co", "overlays", ""} {
		if _, err := SkillTest(context.Background(), root, SkillTestOptions{Name: name, Yes: true, Agent: markerAgent()}); err == nil || !IsUsage(err) {
			t.Errorf("%q: muốn lỗi cách dùng, được %v", name, err)
		}
	}
	for _, o := range []SkillTestOptions{{Name: "x", Arm: "xx"}, {Name: "x", Reps: 21}, {Name: "x", Reps: -1},
		{Name: "x", AgentCommand: "true", Harness: "claude"}} {
		if err := o.Validate(); !IsUsage(err) {
			t.Errorf("%+v: muốn lỗi cách dùng, được %v", o, err)
		}
	}
}

func TestSkillTestWithArmNeedsCleanSkill(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": scenarioA})
	// Phá skill: thân rỗng → lint lỗi.
	p := filepath.Join(root, ".bint", "skills", "demo-skill", "SKILL.md")
	if err := os.WriteFile(p, []byte("---\nname: demo-skill\ndescription: Dùng khi x.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ag := markerAgent()
	for _, arm := range []string{"", "both", "with"} {
		if _, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: arm, Yes: true, Agent: ag}); err == nil ||
			!IsUsage(err) || !strings.Contains(err.Error(), "--arm without") {
			t.Errorf("arm %q: muốn lỗi lint, được %v", arm, err)
		}
	}
	if ag.calls() != 0 {
		t.Fatalf("agent bị gọi khi skill lỗi lint")
	}
	// Nhánh without vẫn chạy (RED khi skill mới chỉ là bản mẫu).
	res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: "without", Yes: true, Agent: ag})
	if err != nil || len(res.Report.Runs) != 4 || len(res.Report.Arms) != 1 || res.Report.Totals[eval.SkillArmWithout].Runs != 4 {
		t.Fatalf("%v %+v", err, res)
	}
	if _, has := res.Report.Totals[eval.SkillArmWith]; has {
		t.Error("không được có số liệu nhánh with")
	}
}

func TestSkillTestInfraErrorsAreNotGraded(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": "prompt: x\nreps: 4\nexpect:\n  - contains: OK\n"})
	ag := &fakeSkillAgent{reply: func(n int, _ eval.AgentRequest) eval.AgentResult {
		switch n {
		case 1:
			return eval.AgentResult{Text: "OK"}
		case 2:
			return eval.AgentResult{Error: "agent thoát với mã 1"}
		case 3:
			return eval.AgentResult{TimedOut: true}
		}
		return eval.AgentResult{} // không có câu trả lời
	}}
	res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: "without", Yes: true, Agent: ag})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Report.Totals[eval.SkillArmWithout]
	if s.Runs != 4 || s.Graded != 1 || s.Errors != 3 || s.Complied != 1 || s.Rate == nil || *s.Rate != 1 {
		t.Fatalf("lượt lỗi không được vào mẫu số: %+v", s)
	}
	for _, r := range res.Report.Runs[1:] {
		if r.Error == "" || r.Complied {
			t.Errorf("lượt lỗi phải có Error: %+v", r)
		}
	}
	// Mọi lượt lỗi → không có tỉ lệ (không báo 0 giả).
	ag2 := &fakeSkillAgent{reply: func(int, eval.AgentRequest) eval.AgentResult { return eval.AgentResult{Infra: "lỗi API (mã 500)"} }}
	res, err = SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: "without", Yes: true, Agent: ag2})
	if err != nil || res.Report.Totals[eval.SkillArmWithout].Rate != nil || res.Report.Totals[eval.SkillArmWithout].Errors != 4 {
		t.Fatalf("%v %+v", err, res.Report.Totals)
	}
}

func TestSkillTestQuotaStopsAndCancelKeepsPartial(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": "prompt: x\nreps: 6\nexpect:\n  - contains: OK\n"})
	quota := &fakeSkillAgent{reply: func(n int, _ eval.AgentRequest) eval.AgentResult {
		if n == 3 {
			return eval.AgentResult{Infra: "hết hạn mức sử dụng", QuotaHit: true}
		}
		return eval.AgentResult{Text: "OK"}
	}}
	res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: "without", Yes: true, Agent: quota})
	if err != nil || res.Report.Status != eval.SkillTestQuota || quota.calls() != 3 || len(res.Report.Runs) != 3 {
		t.Fatalf("quota: %v %s %d", err, res.Report.Status, quota.calls())
	}
	if got := readSkillTestReport(t, res.Path); got.Status != eval.SkillTestQuota {
		t.Errorf("JSON: %+v", got.Status)
	}

	// Huỷ giữa chừng: ghi phần đã chạy với trạng thái cancelled và trả lỗi huỷ.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelling := &fakeSkillAgent{reply: func(n int, _ eval.AgentRequest) eval.AgentResult {
		if n == 3 {
			cancel()
		}
		return eval.AgentResult{Text: "OK"}
	}}
	res, err = SkillTest(ctx, root, SkillTestOptions{Name: "demo-skill", Arm: "without", Yes: true, Agent: cancelling})
	if !IsSkillTestCancelled(err) || res == nil || res.Report.Status != eval.SkillTestCancelled || len(res.Report.Runs) != 2 {
		t.Fatalf("huỷ: %v %+v", err, res)
	}
	if got := readSkillTestReport(t, res.Path); got.Status != eval.SkillTestCancelled || len(got.Runs) != 2 {
		t.Errorf("JSON: %+v", got)
	}
}

func TestSkillTesterRoleSelection(t *testing.T) {
	cfg := config.Default()
	cfg.Harnesses.Enabled = []string{"claude", "codex", "antigravity"}

	// Không đặt gì: theo harnesses.default.
	h, _, err := skillTesterHarness(cfg, "")
	if err != nil || h != cfg.Harnesses.Default && h != "claude" {
		t.Fatalf("mặc định: %q %v", h, err)
	}
	// Theo planner khi roles.skill-tester chưa đặt.
	cfg.Roles.Planner = "codex"
	if h, src, err := skillTesterHarness(cfg, ""); err != nil || h != "codex" || src != "roles.planner" {
		t.Fatalf("theo planner: %q %q %v", h, src, err)
	}
	// roles.skill-tester thắng planner.
	cfg.Roles.Extra = map[string]string{config.RoleSkillTester: "antigravity"}
	if h, src, err := skillTesterHarness(cfg, ""); err != nil || h != "antigravity" || src != "roles."+config.RoleSkillTester {
		t.Fatalf("roles.skill-tester: %q %q %v", h, src, err)
	}
	// --harness thắng tất cả; harness chưa bật là lỗi.
	if h, _, err := skillTesterHarness(cfg, "claude"); err != nil || h != "claude" {
		t.Fatalf("--harness: %q %v", h, err)
	}
	cfg.Harnesses.Enabled = []string{"claude"}
	if _, _, err := skillTesterHarness(cfg, "codex"); err == nil {
		t.Fatal("harness chưa bật phải lỗi")
	}

	// Model: --model → models.skill-tester → models.planner.
	cfg.Harnesses.Codex.Models.Planner = "mp"
	if m := skillTesterModel(cfg, "codex", ""); m != "mp" {
		t.Fatalf("theo planner: %q", m)
	}
	cfg.Harnesses.Codex.Models.Extra = map[string]string{config.RoleSkillTester: "mt"}
	if m := skillTesterModel(cfg, "codex", ""); m != "mt" {
		t.Fatalf("models.skill-tester: %q", m)
	}
	if m := skillTesterModel(cfg, "codex", "cli"); m != "cli" {
		t.Fatalf("--model: %q", m)
	}
}

func TestSkillTestCustomCommandAgent(t *testing.T) {
	// Đường --agent-command (CommandAgent): đọc câu trả lời từ JSON kiểu Claude Code trên stdout.
	if testing.Short() || os.PathSeparator == '\\' {
		t.Skip("cần sh")
	}
	root := skillTestRepo(t, map[string]string{"a.yml": "prompt: x\nreps: 1\nexpect:\n  - contains: kiểm thử\n"})
	script := filepath.Join(t.TempDir(), "agent.sh")
	body := "if grep -q '" + skillTestMarker + "' \"$BINT_EVAL_PROMPT_FILE\"; then t='đã kiểm thử'; else t='làm ngay'; fi\n" +
		"echo \"{\\\"type\\\":\\\"result\\\",\\\"result\\\":\\\"$t\\\",\\\"total_cost_usd\\\":0.01}\"\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Yes: true, AgentCommand: "sh " + filepath.ToSlash(script)})
	if err != nil {
		t.Fatal(err)
	}
	w, wo := res.Report.Totals[eval.SkillArmWith], res.Report.Totals[eval.SkillArmWithout]
	if w.Rate == nil || *w.Rate != 1 || wo.Rate == nil || *wo.Rate != 0 || res.Report.Harness != "command" {
		t.Fatalf("with %+v, without %+v, harness %s", w, wo, res.Report.Harness)
	}
	if res.Report.CostUSD == nil || *res.Report.CostUSD != 0.02 {
		t.Errorf("chi phí: %v", res.Report.CostUSD)
	}
}

func TestSkillTestHonoursTimeoutConfig(t *testing.T) {
	root := skillTestRepo(t, map[string]string{"a.yml": scenarioA})
	ag := markerAgent()
	if _, err := SkillTest(context.Background(), root, SkillTestOptions{Name: "demo-skill", Arm: "with", Reps: 1, Yes: true, Agent: ag}); err != nil {
		t.Fatal(err)
	}
	k, _ := OpenKnowledge(context.Background(), root)
	if want := time.Duration(k.Config.Eval.TimeoutMinutes) * time.Minute; ag.reqs[0].Timeout != want {
		t.Fatalf("hạn %v, muốn %v", ag.reqs[0].Timeout, want)
	}
}

// bint doctor: role skill-tester (của `bint skill test`) hợp lệ dù không thuộc bước flow nào; role gõ sai vẫn bị cảnh báo.
func TestDoctorAcceptsSkillTesterRole(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	cfg := config.Default()
	cfg.Roles.Extra = map[string]string{config.RoleSkillTester: "claude", "skill-tster": "claude"}
	cfg.Harnesses.Claude.Models.Extra = map[string]string{config.RoleSkillTester: "haiku"}
	var r DoctorReport
	flowChecks(&r, root, cfg)
	var warned []string
	for _, c := range r.Checks {
		if c.Name == "role tuỳ biến" {
			warned = append(warned, c.Detail)
		}
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "skill-tster") || strings.Contains(warned[0], config.RoleSkillTester+" ") {
		t.Fatalf("chỉ role gõ sai được cảnh báo: %q", warned)
	}
}
