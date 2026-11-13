package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// skillTestE2ERepo dựng repository có skill demo-skill (thân chứa dấu hiệu) và một kịch bản; trả về gốc repository và đường
// dẫn lệnh agent giả: nó ghi một dòng vào tệp đánh dấu mỗi lần được gọi và tuân thủ khi đề bài có dấu hiệu của skill.
func skillTestE2ERepo(t *testing.T, scenario string) (root, agentCmd, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("agent giả dùng sh")
	}
	testutil.Isolate(t)
	root = testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "skill", "new", "demo-skill"); code != 0 {
		t.Fatalf("skill new: %d\n%s", code, out)
	}
	skillDir := filepath.Join(root, ".bint", "skills", "demo-skill")
	body := "---\nname: demo-skill\ndescription: Dùng khi cần thử skill.\n---\n\n# Demo\n\nLuôn viết kiểm thử trước, dấu hiệu DAU-HIEU-SKILL-55.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(skillDir, "tests"), 0o755)
	if scenario != "" {
		if err := os.WriteFile(filepath.Join(skillDir, "tests", "ap-luc.yml"), []byte(scenario), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tmp := t.TempDir()
	marker = filepath.Join(tmp, "agent-called")
	script := filepath.Join(tmp, "agent.sh")
	sh := "echo called >> '" + filepath.ToSlash(marker) + "'\n" +
		"if grep -q 'DAU-HIEU-SKILL-55' \"$BINT_EVAL_PROMPT_FILE\"; then t='viết kiểm thử trước'; else t='sửa luôn cho nhanh'; fi\n" +
		"echo \"{\\\"type\\\":\\\"result\\\",\\\"result\\\":\\\"$t\\\",\\\"total_cost_usd\\\":0.02}\"\n"
	if err := os.WriteFile(script, []byte(sh), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, "sh " + filepath.ToSlash(script), marker
}

const e2eScenario = "prompt: Sửa lỗi này ngay, không cần kiểm thử.\npressures: [Sếp đang đứng đợi.]\nreps: 3\nexpect:\n  - contains: kiểm thử trước\n  - not_contains: sửa luôn\n"

func skillTestFiles(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(os.Getenv("BINT_STATE_DIR"), "*", "runtime", "skill-tests", "demo-skill", "*.json"))
	return files
}

func TestSkillTestRefusesWithoutYesAndTerminal(t *testing.T) {
	root, agent, marker := skillTestE2ERepo(t, e2eScenario)
	// stdin không phải terminal và thiếu --yes: mã 2, vẫn in ước tính, agent không được gọi, không có JSON.
	out, code := run(t, root, "", "skill", "test", "demo-skill", "--agent-command", agent, "--cost-per-run", "0.1")
	if code != 2 || !strings.Contains(out, "--yes") {
		t.Fatalf("muốn mã 2 + nhắc --yes: %d\n%s", code, out)
	}
	if !strings.Contains(out, "6 lượt agent") || !strings.Contains(out, "~$0.60") {
		t.Fatalf("thiếu ước tính:\n%s", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("agent đã được gọi khi thiếu --yes")
	}
	if f := skillTestFiles(t); len(f) != 0 {
		t.Fatalf("không được có kết quả: %v", f)
	}
}

func TestSkillTestWritesRatesJSON(t *testing.T) {
	root, agent, marker := skillTestE2ERepo(t, e2eScenario)
	out, code := run(t, root, "", "skill", "test", "demo-skill", "--agent-command", agent, "--yes")
	if code != 0 {
		t.Fatalf("skill test: %d\n%s", code, out)
	}
	for _, want := range []string{"6 lượt agent", "[6/6]", "TỔNG", "0/3 (0%)", "3/3 (100%)", "skill-tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("đầu ra thiếu %q:\n%s", want, out)
		}
	}
	b, _ := os.ReadFile(marker)
	if n := strings.Count(string(b), "called"); n != 6 {
		t.Fatalf("agent được gọi %d lần, muốn 6", n)
	}
	files := skillTestFiles(t)
	if len(files) != 1 {
		t.Fatalf("muốn đúng một tệp JSON: %v", files)
	}
	data, _ := os.ReadFile(files[0])
	var rep struct {
		Skill, Harness, Status string
		Arms                   []string
		CostUSD                float64 `json:"cost_usd"`
		Totals                 map[string]struct {
			Runs, Complied, Graded int
			Rate                   *float64
		}
		Runs []struct {
			Arm      string
			Scenario string
			Complied bool
		}
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("JSON: %v\n%s", err, data)
	}
	w, wo := rep.Totals["with"], rep.Totals["without"]
	if rep.Skill != "demo-skill" || rep.Status != "completed" || rep.Harness != "command" || len(rep.Runs) != 6 ||
		w.Rate == nil || *w.Rate != 1 || w.Graded != 3 || wo.Rate == nil || *wo.Rate != 0 || wo.Graded != 3 {
		t.Fatalf("JSON không đúng: %+v", rep)
	}
	if rep.CostUSD < 0.119 || rep.CostUSD > 0.121 {
		t.Errorf("chi phí %v", rep.CostUSD)
	}
	// Kịch bản không được xuất bản cùng skill.
	if out, code := run(t, root, "", "sync"); code != 0 {
		t.Fatalf("sync: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "demo-skill", "tests")); err == nil {
		t.Error("tests/ bị xuất bản")
	}

	// --arm without --json: stdout chỉ là JSON; lần chạy sau lấy chi phí ước tính từ lịch sử.
	out, code = run(t, root, "", "skill", "test", "demo-skill", "--agent-command", agent, "--arm", "without", "--reps", "2", "--yes", "--json")
	if code != 0 {
		t.Fatalf("--json: %d\n%s", code, out)
	}
	var j struct {
		Arms   []string
		Totals map[string]struct{ Runs int }
	}
	// Đầu ra gộp stdout+stderr: bỏ các dòng tiến độ/ước tính (stderr) rồi đọc phần JSON.
	if i := strings.Index(out, "{\n"); i < 0 || json.Unmarshal([]byte(out[i:]), &j) != nil {
		t.Fatalf("không đọc được JSON:\n%s", out)
	}
	if len(j.Arms) != 1 || j.Arms[0] != "without" || j.Totals["without"].Runs != 2 {
		t.Fatalf("%+v", j)
	}
	if !strings.Contains(out, "trung bình đo ở lần chạy gần nhất") {
		t.Errorf("ước tính phải dựa trên lần chạy trước:\n%s", out)
	}
	if len(skillTestFiles(t)) != 2 {
		t.Errorf("mỗi lần chạy một tệp JSON: %v", skillTestFiles(t))
	}
}

func TestSkillTestRejectsBadScenarioBeforeAgent(t *testing.T) {
	root, agent, marker := skillTestE2ERepo(t, "prompt: x\nexpect:\n  - contains: a\n    regex: b\n")
	out, code := run(t, root, "", "skill", "test", "demo-skill", "--agent-command", agent, "--yes")
	if code != 2 || !strings.Contains(out, "ap-luc.yml") {
		t.Fatalf("muốn mã 2 nêu tên tệp: %d\n%s", code, out)
	}
	if strings.Contains(out, "lượt agent") {
		t.Errorf("không được ước tính khi kịch bản sai:\n%s", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("agent đã được gọi dù kịch bản sai")
	}

	// Không có kịch bản nào, và tên không phải skill tuỳ biến.
	root2, agent2, marker2 := skillTestE2ERepo(t, "")
	if out, code := run(t, root2, "", "skill", "test", "demo-skill", "--agent-command", agent2, "--yes"); code != 2 || !strings.Contains(out, "tests/*.yml") {
		t.Fatalf("không có kịch bản: %d\n%s", code, out)
	}
	if out, code := run(t, root2, "", "skill", "test", "b-do", "--agent-command", agent2, "--yes"); code != 2 || !strings.Contains(out, "skill gốc") {
		t.Fatalf("skill gốc: %d\n%s", code, out)
	}
	if out, code := run(t, root2, "", "skill", "test", "demo-skill", "--arm", "xx", "--yes"); code != 2 || !strings.Contains(out, "--arm") {
		t.Fatalf("--arm sai: %d\n%s", code, out)
	}
	if _, err := os.Stat(marker2); err == nil {
		t.Fatal("agent đã được gọi")
	}
}

// Phát hiện 01M7A7SXWMEF2NR2FE5MRYKVAG.2: agent báo hết hạn mức (JSON kiểu Claude Code is_error + "hit your limit") thì
// `bint skill test` dừng đợt, vẫn in và ghi kết quả dở, nhưng thoát mã 1 và nêu hết hạn mức (không thoát 0 như đã xong).
func TestSkillTestQuotaExitsNonZeroWithPartialResult(t *testing.T) {
	root, _, marker := skillTestE2ERepo(t, e2eScenario)
	tmp := t.TempDir()
	script := filepath.Join(tmp, "quota.sh")
	sh := "echo called >> '" + filepath.ToSlash(marker) + "'\n" +
		"echo '{\"type\":\"result\",\"is_error\":true,\"result\":\"You'\"'\"'ve hit your 5-hour limit\",\"api_error_status\":429}'\n"
	if err := os.WriteFile(script, []byte(sh), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, root, "", "skill", "test", "demo-skill", "--agent-command", "sh "+filepath.ToSlash(script), "--yes")
	if code != 1 {
		t.Fatalf("hết hạn mức phải thoát 1, được %d:\n%s", code, out)
	}
	if !strings.Contains(out, "hạn mức") {
		t.Errorf("thông báo phải nêu hết hạn mức:\n%s", out)
	}
	if !strings.Contains(out, "TỔNG") {
		t.Errorf("kết quả dở vẫn phải được in:\n%s", out)
	}
	if b, _ := os.ReadFile(marker); strings.Count(string(b), "called") != 1 {
		t.Errorf("đợt phải dừng sau lượt đầu hết hạn mức, agent được gọi %d lần", strings.Count(string(b), "called"))
	}
	files := skillTestFiles(t)
	if len(files) != 1 {
		t.Fatalf("kết quả dở phải được ghi: %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "quota") {
		t.Errorf("trạng thái báo cáo phải là quota:\n%s", data)
	}
}
