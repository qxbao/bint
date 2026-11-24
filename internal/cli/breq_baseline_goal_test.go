package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/skills"
	"bint/internal/task"
	"bint/internal/testutil"
)

// Kiểm tra mục tiêu GĐ4 (hộp đen): b-req đo hiện trạng (baseline.md, mục ## Hiện trạng, AC có số đo) và luật kiểm
// được của skill b-req/b-do thành cổng mã (lint v2, chặn task new/status dưới bint run), skill ngắn lại và ba bản đồng bộ.

const goalCritMeasured = "lệnh list in đủ 3 mục của src/app.go"
const goalCritGoal = "Kiểm tra mục tiêu TestGoalList xanh"

const goalReqValid = "# Yêu cầu\n" +
	"## Hiện trạng\n" +
	"- `src/app.go`: 120 byte; 2 tệp được git theo dõi khớp `src/**`.\n" +
	"## Tiêu chí chấp nhận\n" +
	"- [ ] AC-1: " + goalCritMeasured + "\n" +
	"- [ ] AC-2: " + goalCritGoal + "\n" +
	"## Kiểm tra mục tiêu\n" +
	"`TestGoalList` cài 3 mục giả rồi chạy list, đạt khi in đủ 3 mục.\n" +
	"## Câu hỏi mở\n"

const goalPlanValid = "# Kế hoạch\n" +
	"Sửa hàm List trong src/app.go.\n" +
	"## Cỡ kế hoạch\n" +
	"Kết luận: nhỏ\n" +
	"- `src/app.go`\n" +
	"- `src/app_test.go`\n" +
	"## Bề mặt bị ảnh hưởng\n" +
	"- Lệnh list (src/app.go): AC-1, AC-2\n" +
	"- Tài liệu hướng dẫn: ngoài phạm vi\n" +
	"## Giả định thay người dùng\n" +
	"- Giữ nguyên định dạng in hiện tại của list.\n"

const goalTasksValid = "- [ ] TSK-1 [AC-1, AC-2]: sửa List trong src/app.go — xong khi: TestGoalList xanh\n"

type goalDocs struct{ req, plan, tasks string }

func goalValidDocs() goalDocs { return goalDocs{goalReqValid, goalPlanValid, goalTasksValid} }

func goalWriteDocs(t *testing.T, root, id string, d goalDocs) {
	t.Helper()
	writeDoc(t, root, id, "requirement", d.req)
	writeDoc(t, root, id, "plan", d.plan)
	writeDoc(t, root, id, "tasks", d.tasks)
}

// goalLint chạy `bint task lint <id> --strict --json` và trả mã lỗi trong errors, warnings cùng phiên bản hợp đồng.
func goalLint(t *testing.T, root, id string) (errs, warns []string, version int, raw string) {
	t.Helper()
	out, _ := run(t, root, "", "task", "lint", id, "--strict", "--json")
	var r lintJSON
	if err := json.Unmarshal(jsonPart(out), &r); err != nil {
		t.Fatalf("lint --json không ra JSON: %v\n%s", err, out)
	}
	for _, e := range r.Errors {
		errs = append(errs, e.Code)
	}
	for _, w := range r.Warnings {
		warns = append(warns, w.Code)
	}
	return errs, warns, r.ContractVersion, out
}

type goalCreated struct {
	ID     string `json:"id"`
	Dir    string `json:"dir"`
	Status string `json:"status"`
}

func goalNewTask(t *testing.T, root string, criteria ...string) goalCreated {
	t.Helper()
	args := []string{"task", "new", "--workflow", "b-req", "--json", "--allow", "src/**", "--deny", "src/secret/**"}
	for _, c := range criteria {
		args = append(args, "--criteria", c)
	}
	args = append(args, "Cho lệnh list in đủ các mục của src/app.go")
	out, code := run(t, root, "", args...)
	var c goalCreated
	if code != 0 || json.Unmarshal(jsonPart(out), &c) != nil || c.ID == "" || c.Dir == "" {
		t.Fatalf("task new: %d\n%s", code, out)
	}
	return c
}

func goalShowStatus(t *testing.T, root, id string) string {
	t.Helper()
	out, code := run(t, root, "", "task", "show", id, "--json")
	var s struct {
		Status string `json:"status"`
		State  struct {
			Status string `json:"status"`
		} `json:"state"`
	}
	if code != 0 || json.Unmarshal(jsonPart(out), &s) != nil {
		t.Fatalf("task show: %d\n%s", code, out)
	}
	if s.Status != "" {
		return s.Status
	}
	return s.State.Status
}

func normEOL(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }

func TestGoalBReqBaselineAndSkillGates(t *testing.T) {
	// --- Skill: cỡ và bản xuất bản (không cần repo tạm). ---
	t.Run("skill_size", func(t *testing.T) {
		// Nâng trần (26000→26400, 15500→16800) cho mục Chiến lược thực thi, lựa chọn --slice và luật test hẹp (phiếu 01M7RZQ7).
		// b-do 26400→28200 cho mục "Luồng B′" (giai đoạn 4, phiếu 01M7VM1C4Z).
		// Nâng 16800→18000 cho ma trận loại × nơi, giữ phạm vi theo lời người dùng và cấu hình hiệu lực (benchmark đợt 6).
		// b-req 18000→18800 cho câu hỏi Q-N kèm `Đề xuất:`, `## Seam kiểm thử`, lát dọc/expand–contract (mattpocock-skills-plan TSK-3.1).
		// b-do 28200→28600 cho action `wait` (tự thử lại lỗi ngoại cảnh, workflow.external_retry).
		limits := map[string]int{"b-do": 28600, "b-req": 18800}
		for _, name := range []string{"b-do", "b-req"} {
			s, ok := skills.Get(name)
			if !ok {
				t.Errorf("skills.Get(%q) không có", name)
				continue
			}
			if n := len(s.Content); n > limits[name] {
				t.Errorf("skill %s: %d byte, muốn ≤ %d", name, n, limits[name])
			}
		}
	})
	t.Run("skill_published_copies", func(t *testing.T) {
		repo := filepath.Join("..", "..")
		for _, name := range []string{"b-do", "b-req"} {
			s, ok := skills.Get(name)
			if !ok {
				t.Errorf("skills.Get(%q) không có", name)
				continue
			}
			for _, tg := range []skills.Target{skills.ClaudeTarget, skills.AgentsTarget} {
				p := filepath.Join(repo, filepath.FromSlash(tg.Dir), name, "SKILL.md")
				got, err := os.ReadFile(p)
				if err != nil {
					t.Errorf("%s: %v", p, err)
					continue
				}
				// Chỉ chuẩn hoá xuống dòng (Git for Windows có thể checkout CRLF); phần còn lại phải khớp từng byte.
				if want := skills.ForTarget(s, tg).Content; normEOL(got) != normEOL(want) {
					t.Errorf("%s khác bản xuất bản từ core (%d byte so với %d); chạy bint sync", p, len(got), len(want))
				}
			}
		}
	})

	// --- Repo tạm cho lint, BINT_RUN_ID và baseline. ---
	root := newLintRepo(t)
	for rel, body := range map[string]string{
		"src/app.go":        "package src\n\nfunc List() []string { return nil }\n",
		"src/app_test.go":   "package src\n",
		"src/secret/k.go":   "package secret\n",
		"docs/x.md":         "# x\n",
		"README.md":         "demo\n",
		"src/app_extra.txt": "phụ\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, root)

	mt := goalNewTask(t, root, goalCritMeasured, goalCritGoal)

	t.Run("lint_valid_set_has_no_errors", func(t *testing.T) {
		d := goalValidDocs()
		d.plan += bdoStrategyValid // hợp đồng v2 bắt buộc `## Chiến lược thực thi`
		goalWriteDocs(t, root, mt.ID, d)
		errs, _, ver, raw := goalLint(t, root, mt.ID)
		if ver < 2 {
			t.Errorf("tác vụ mới phải theo hợp đồng v2 trở lên, có contract_version=%d", ver)
		}
		if len(errs) != 0 {
			t.Errorf("bộ hợp lệ không được có lỗi, có %v\n%s", errs, raw)
		}
	})

	type fault struct {
		name string
		code string
		docs func(goalDocs) goalDocs
	}
	replace := func(s, old, repl string) string {
		if !strings.Contains(s, old) {
			panic("mẫu không chứa: " + old)
		}
		return strings.Replace(s, old, repl, 1)
	}
	faults := []fault{
		{"requirement_missing_baseline_section", "baseline_missing", func(d goalDocs) goalDocs {
			d.req = replace(d.req, "## Hiện trạng\n- `src/app.go`: 120 byte; 2 tệp được git theo dõi khớp `src/**`.\n", "")
			return d
		}},
		{"baseline_section_without_digits", "baseline_missing", func(d goalDocs) goalDocs {
			d.req = replace(d.req, "- `src/app.go`: 120 byte; 2 tệp được git theo dõi khớp `src/**`.\n", "- Mã list nằm trong `src/app.go`, chưa rõ dung lượng.\n")
			return d
		}},
		{"plan_missing_surface", "surface_missing", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "## Bề mặt bị ảnh hưởng\n- Lệnh list (src/app.go): AC-1, AC-2\n- Tài liệu hướng dẫn: ngoài phạm vi\n", "")
			return d
		}},
		{"surface_group_without_ac", "surface_unmapped", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "- Tài liệu hướng dẫn: ngoài phạm vi\n", "- Tài liệu hướng dẫn: cập nhật ví dụ\n")
			return d
		}},
		{"surface_group_unknown_ac", "surface_unmapped", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "- Tài liệu hướng dẫn: ngoài phạm vi\n", "- Tài liệu hướng dẫn: AC-9\n")
			return d
		}},
		{"plan_missing_assumptions", "assumptions_missing", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "## Giả định thay người dùng\n- Giữ nguyên định dạng in hiện tại của list.\n", "")
			return d
		}},
		{"plan_file_outside_allow", "plan_file_out_of_scope", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "- `src/app_test.go`\n", "- `src/app_test.go`\n- `docs/x.md`\n")
			return d
		}},
		{"plan_file_matches_deny", "plan_file_out_of_scope", func(d goalDocs) goalDocs {
			d.plan = replace(d.plan, "- `src/app_test.go`\n", "- `src/app_test.go`\n- `src/secret/k.go`\n")
			return d
		}},
	}
	for _, f := range faults {
		t.Run("lint_"+f.name, func(t *testing.T) {
			goalWriteDocs(t, root, mt.ID, f.docs(goalValidDocs()))
			errs, _, _, raw := goalLint(t, root, mt.ID)
			if !slices.Contains(errs, f.code) {
				t.Errorf("thiếu lỗi %s trong errors (có %v)\n%s", f.code, errs, raw)
			}
		})
	}

	// AC không chứa số đo: cần phiếu có tiêu chí không chữ số (AC phải khớp tiêu chí của phiếu).
	t.Run("lint_ac_without_measure", func(t *testing.T) {
		const c1 = "lệnh list in đủ các mục của src/app.go"
		tk := goalNewTask(t, root, c1, goalCritGoal)
		d := goalValidDocs()
		d.req = replace(d.req, "- [ ] AC-1: "+goalCritMeasured+"\n", "- [ ] AC-1: "+c1+"\n")
		goalWriteDocs(t, root, tk.ID, d)
		errs, _, _, raw := goalLint(t, root, tk.ID)
		if !slices.Contains(errs, "ac_without_measure") {
			t.Errorf("thiếu lỗi ac_without_measure trong errors (có %v)\n%s", errs, raw)
		}
	})

	// Tác vụ hợp đồng v1: cùng mã nhưng chỉ là cảnh báo, kể cả --strict.
	t.Run("lint_v1_baseline_is_warning", func(t *testing.T) {
		tk := goalNewTask(t, root, goalCritMeasured, goalCritGoal)
		store := task.Store{Dir: filepath.Dir(tk.Dir)}
		if _, err := store.Update(tk.ID, time.Now(), func(x *task.Task) error {
			x.State.PlanningContractVersion = 1
			return nil
		}); err != nil {
			t.Fatalf("hạ hợp đồng về v1: %v", err)
		}
		d := goalValidDocs()
		d.req = replace(d.req, "## Hiện trạng\n- `src/app.go`: 120 byte; 2 tệp được git theo dõi khớp `src/**`.\n", "")
		goalWriteDocs(t, root, tk.ID, d)
		errs, warns, ver, raw := goalLint(t, root, tk.ID)
		if ver != 1 {
			t.Fatalf("contract_version=%d, muốn 1\n%s", ver, raw)
		}
		if slices.Contains(errs, "baseline_missing") {
			t.Errorf("v1: baseline_missing không được nằm trong errors\n%s", raw)
		}
		if !slices.Contains(warns, "baseline_missing") {
			t.Errorf("v1: thiếu cảnh báo baseline_missing (warnings %v)\n%s", warns, raw)
		}
	})

	// Dưới bint run (BINT_RUN_ID có giá trị): task new và task status bị từ chối, không đổi gì.
	t.Run("run_id_blocks_task_new", func(t *testing.T) {
		before := taskCount(t, root)
		t.Setenv("BINT_RUN_ID", "run-x")
		out, code := run(t, root, "", "task", "new", "--json", "Tác vụ con tự lập dưới bint run")
		t.Setenv("BINT_RUN_ID", "")
		if code == 0 {
			t.Errorf("task new dưới BINT_RUN_ID phải thoát khác 0\n%s", out)
		}
		if after := taskCount(t, root); after != before {
			t.Errorf("task new dưới BINT_RUN_ID đã tạo tác vụ (%d → %d)\n%s", before, after, out)
		}
	})
	t.Run("run_id_blocks_task_status", func(t *testing.T) {
		// Tài liệu hợp lệ để chuyển created → planned chỉ có thể bị chặn bởi BINT_RUN_ID.
		tk := goalNewTask(t, root, goalCritMeasured, goalCritGoal)
		d := goalValidDocs()
		d.plan += bdoStrategyValid
		goalWriteDocs(t, root, tk.ID, d)
		before := goalShowStatus(t, root, tk.ID)
		t.Setenv("BINT_RUN_ID", "run-x")
		out, code := run(t, root, "", "task", "status", tk.ID, "planned")
		t.Setenv("BINT_RUN_ID", "")
		if code == 0 {
			t.Errorf("task status dưới BINT_RUN_ID phải thoát khác 0\n%s", out)
		}
		if after := goalShowStatus(t, root, tk.ID); after != before {
			t.Errorf("task status dưới BINT_RUN_ID đã đổi trạng thái %q → %q\n%s", before, after, out)
		}
		// Đối chứng: người chạy cùng lệnh ngoài bint run thì được (lỗi trên là do BINT_RUN_ID, không do chuyển trạng thái sai).
		if out, code := run(t, root, "", "task", "status", tk.ID, "planned"); code != 0 {
			t.Errorf("đối chứng: task status planned không có BINT_RUN_ID phải chạy được: %d\n%s", code, out)
		}
	})

	// Trước b-req, Bint ghi baseline.md vào thư mục tác vụ và đề bài b-req nêu đường dẫn tệp đó.
	t.Run("baseline_written_before_breq", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("tác nhân giả viết bằng sh")
		}
		tk := goalNewTask(t, root, goalCritMeasured, goalCritGoal)
		docs := t.TempDir()
		for name, body := range map[string]string{"requirement.md": goalReqValid, "plan.md": goalPlanValid + bdoStrategyValid, "tasks.md": goalTasksValid} {
			if err := os.WriteFile(filepath.Join(docs, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		pdir := t.TempDir()
		script := `set -e
cp "$BINT_EVAL_PROMPT_FILE" "$PDIR/prompt.txt"
if [ -f "$TASK_DIR/baseline.md" ]; then cp "$TASK_DIR/baseline.md" "$PDIR/baseline-seen.md"; fi
"$BINT" task write "$BINT_TASK_ID" requirement - < "$DOCS/requirement.md"
"$BINT" task write "$BINT_TASK_ID" plan - < "$DOCS/plan.md"
"$BINT" task write "$BINT_TASK_ID" tasks - < "$DOCS/tasks.md"
echo '{"type":"result","total_cost_usd":0.01,"num_turns":2,"result":"xong"}'
`
		t.Setenv("BINT", bintBin)
		t.Setenv("PDIR", pdir)
		t.Setenv("DOCS", docs)
		t.Setenv("TASK_DIR", tk.Dir)
		out, _ := run(t, root, "", "run", "b-req", "--task", tk.ID, "--agent-command", writeScript(t, script))

		prompt, err := os.ReadFile(filepath.Join(pdir, "prompt.txt"))
		if err != nil {
			t.Fatalf("agent b-req không chạy (không có đề bài chép lại): %v\n%s", err, out)
		}
		if _, err := os.Stat(filepath.Join(pdir, "baseline-seen.md")); err != nil {
			t.Errorf("baseline.md chưa có trong thư mục tác vụ lúc agent b-req chạy\n%s", out)
		}
		baselinePath := filepath.Join(tk.Dir, "baseline.md")
		body, err := os.ReadFile(baselinePath)
		if err != nil {
			t.Fatalf("không có %s: %v\n%s", baselinePath, err, out)
		}
		rel := filepath.ToSlash(filepath.Join(filepath.Base(tk.Dir), "baseline.md"))
		if !strings.Contains(filepath.ToSlash(string(prompt)), rel) {
			t.Errorf("đề bài b-req không nêu đường dẫn baseline.md (%s)\n%.2000s", rel, prompt)
		}

		hasDigit := func(s string) bool { return strings.ContainsAny(s, "0123456789") }
		lines := strings.Split(normEOL(body), "\n")
		// Thư mục trạng thái: cha của runtime/tasks/<id>.
		stateDir := filepath.Dir(filepath.Dir(filepath.Dir(tk.Dir)))
		entries, err := os.ReadDir(stateDir)
		if err != nil {
			t.Fatalf("đọc thư mục trạng thái %s: %v", stateDir, err)
		}
		stateLine := false
		for _, l := range lines {
			for _, e := range entries {
				if hasDigit(l) && strings.Contains(l, e.Name()) {
					stateLine = true
				}
			}
		}
		if !stateLine {
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("baseline.md không có dòng số đo cho mục cấp một nào của thư mục trạng thái %v\n%s", names, body)
		}
		scopeLine := false
		for _, l := range lines {
			if strings.Contains(l, "src/**") && hasDigit(strings.ReplaceAll(l, "src/**", "")) {
				scopeLine = true
			}
		}
		if !scopeLine {
			t.Errorf("baseline.md không có dòng số tệp được git theo dõi khớp src/**\n%s", body)
		}
	})
}

func gitCommitAll(t *testing.T, root string) {
	t.Helper()
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "fixture")
}
