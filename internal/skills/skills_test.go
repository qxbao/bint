package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/flow"
	"bint/internal/fsafe"
)

func TestCoreSkills(t *testing.T) {
	s, ok := Get("b-bootstrap")
	if !ok {
		t.Fatal("thiếu b-bootstrap")
	}
	if !strings.HasPrefix(string(s.Content), "---\nname: b-bootstrap\n") || strings.Contains(s.Body(), "name: b-bootstrap") ||
		!strings.Contains(s.Body(), "bint knowledge add") {
		t.Fatalf("skill không đúng định dạng:\n%s", s.Body()[:200])
	}
}

func TestInstallNeverOverwritesEdits(t *testing.T) {
	root := t.TempDir()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	opts := InstallOptions{Root: root, Targets: []Target{ClaudeTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now()}
	// boot trả kết quả của b-bootstrap trong một lần Install (không giả định vị trí trong danh sách).
	boot := func(outs []Outcome) Outcome {
		for _, o := range outs {
			if o.Skill == "b-bootstrap" {
				return o
			}
		}
		t.Fatalf("thiếu kết quả b-bootstrap: %+v", outs)
		return Outcome{}
	}
	outs, err := Install(opts)
	if err != nil || len(outs) == 0 || boot(outs).Result != fsafe.Created {
		t.Fatalf("%v %+v", err, outs)
	}
	p := filepath.Join(root, ".claude", "skills", "b-bootstrap", "SKILL.md")
	if outs, _ := Install(opts); boot(outs).Result != fsafe.Unchanged {
		t.Fatalf("lần hai: %+v", outs)
	}

	// Sửa tay → xung đột, nội dung người dùng giữ nguyên.
	os.WriteFile(p, []byte("sửa tay\n"), 0o644)
	if outs, _ := Install(opts); boot(outs).Result != fsafe.Conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
	if b, _ := os.ReadFile(p); string(b) != "sửa tay\n" {
		t.Fatal("không được ghi đè")
	}
	opts.Force = true
	if outs, _ := Install(opts); boot(outs).Result != fsafe.Updated || boot(outs).Backup == "" {
		t.Fatalf("--force phải ghi đè có sao lưu: %+v", outs)
	}

	// Máy khác (manifest trống) với tệp khớp core.lock đã commit: coi là nguyên bản.
	opts.Force = false
	lock, _ := ReadLock(root)
	old := []byte("phiên bản skill cũ do Bint ghi\n")
	os.WriteFile(p, old, 0o644)
	lock.Skills["b-bootstrap"] = LockEntry{SHA256: Skill{Content: old}.Hash()}
	writeLock(root, lock)
	opts.Manifest, _ = fsafe.LoadManifest(filepath.Join(t.TempDir(), "m2.json"))
	if outs, _ := Install(opts); boot(outs).Result != fsafe.Updated {
		t.Fatalf("bản khớp core.lock phải được nâng cấp: %+v", outs)
	}
}

// Overlay cũ gắn vào anchor; sửa khuôn b-req không được làm mất chúng.
func TestCoreSkillBReqKeepsAnchorsAndContract(t *testing.T) {
	s, ok := Get("b-req")
	if !ok {
		t.Fatal("thiếu b-req")
	}
	for _, want := range []string{
		"<!-- bint:anchor project-rules -->", "<!-- bint:anchor-end project-rules -->",
		"<!-- bint:anchor plan-template -->", "<!-- bint:anchor-end plan-template -->",
		"AC-1:", "TSK-1 [AC-1]", "xong khi:", "--clear-criteria", "bint task lint",
	} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
}

// Một skill b-req cho cả hai mức; b-do chuyển giao độ sâu và giữ anchor routing.
func TestCoreSkillsDepthGuidance(t *testing.T) {
	req, _ := Get("b-req")
	for _, want := range []string{"## Độ sâu phân tích", "standard", "deep", "Giả định và mâu thuẫn", "Lựa chọn và lý do",
		"Vùng ảnh hưởng", "Rủi ro và rollback", "Chiến lược kiểm thử", "Checklist nhất quán", "classification.depth"} {
		if !strings.Contains(req.Body(), want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
	do, _ := Get("b-do")
	for _, want := range []string{"<!-- bint:anchor routing -->", "<!-- bint:anchor-end routing -->", "classification.depth", "--depth deep"} {
		if !strings.Contains(do.Body(), want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
	for _, n := range []string{"b-req", "b-do"} {
		if _, ok := Get(n + "-deep"); ok {
			t.Errorf("không được thêm skill %s-deep", n)
		}
	}
}

// Benchmark lần 2: người dùng mơ hồ → kế hoạch phải liệt kê mọi bề mặt, nêu giả định thay người dùng và không
// thu hẹp phạm vi tới mức không đạt mục đích. Một skill cho cả standard lẫn deep.
func TestCoreSkillBReqSurfacesAndAssumptions(t *testing.T) {
	s, _ := Get("b-req")
	body := s.Body()
	flat := strings.Join(strings.Fields(body), " ") // gộp xuống dòng để cụm từ không phụ thuộc cách ngắt dòng
	for _, want := range []string{
		"## Bề mặt bị ảnh hưởng", "## Giả định thay người dùng",
		"từng nơi", "gộp theo nhóm", "lý do nằm ngoài phạm vi", "lưu đệm, sao chép và xuất", "vòng đời",
		"bản nhỏ nhất vẫn đạt mục đích", "không im lặng bỏ", "hậu quả người dùng sẽ thấy",
		"≤ 5 `AC`", "≤ 5 `TSK`", "## Đề xuất tách giai đoạn",
		"không đảo ngược được", "đảo ngược được, ít xâm lấn", "`--dry-run`", "cảnh báo thay vì từ chối",
		"tối đa 2–3 câu", "kết quả mong muốn", "## Câu hỏi mở",
		"người không rành kỹ", "mọi mức phân tích",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
	// Cả hai mức đều bị ràng buộc: mục nằm trước phần tách standard/deep và cả hai gạch đầu dòng nhắc lại.
	i := strings.Index(body, "## Bề mặt bị ảnh hưởng")
	j := strings.Index(body, "## Độ sâu phân tích")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("mục bề mặt phải đứng trước phần độ sâu (%d, %d)", i, j)
	}
	depth := body[j:]
	if !strings.Contains(depth, "- **standard**") || !strings.Contains(depth, "`## Bề mặt bị ảnh hưởng`") {
		t.Error("mức standard phải nhắc mục Bề mặt bị ảnh hưởng")
	}
	if !strings.Contains(body[strings.Index(body, "<!-- bint:anchor plan-template -->"):], "Bề mặt bị ảnh hưởng") {
		t.Error("khuôn plan.md (anchor plan-template) phải nhắc mục Bề mặt bị ảnh hưởng")
	}
}

// b-review đối chiếu yêu cầu gốc, tiêu chí và bề mặt của plan.md để tìm chỗ bị bỏ sót ngoài diff.
func TestCoreSkillBReviewFindsOmissions(t *testing.T) {
	s, _ := Get("b-review")
	body := strings.Join(strings.Fields(s.Body()), " ") // gộp xuống dòng để cụm từ không phụ thuộc cách ngắt dòng
	for _, want := range []string{
		"yêu cầu gốc", "Bề mặt bị ảnh hưởng", "bỏ sót", "ngoài diff", "`criterion`", "tệp ngoài diff",
		// GĐ2 nhóm 2: chấm theo MỤC TIÊU người dùng; high cho chỗ sót có bằng chứng kể cả ngoài kế hoạch, low chỉ cho ngoài mục tiêu/suy đoán;
		// high cũng dành cho AC chưa đạt/hồi quy/lỗi đúng đắn; có điều kiện dừng.
		"MỤC TIÊU của người dùng", "## Kiểm tra mục tiêu", "kể cả khi nằm ngoài kế hoạch", "Chỉ giữ `low`", "suy đoán",
		"kiểm chứng cuối", "không bị nới", "AC chưa đạt, hồi quy", "KHÔNG HỘI TỤ", "không giảm", "Bint tự dừng",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("b-review thiếu %q", want)
		}
	}
	// Luật cũ (benchmark lần 3) hạ chỗ sót ngoài kế hoạch thành low dù kiểm tra mục tiêu bắt được: đã bỏ.
	for _, not := range []string{"ngoài kế hoạch hoặc ngoài mọi AC", "chỉ là `low`/ghi chú, không `high`"} {
		if strings.Contains(body, not) {
			t.Errorf("b-review còn luật cũ %q", not)
		}
	}
}

// GĐ2 nhóm 2: b-req sinh kiểm tra mục tiêu, không tự cắt phạm vi, mỗi giai đoạn một dòng G-N.
func TestCoreSkillBReqGoalCheckAndNoScopeCut(t *testing.T) {
	s, _ := Get("b-req")
	flat := strings.Join(strings.Fields(s.Body()), " ")
	for _, want := range []string{
		"## Kiểm tra mục tiêu", "một kiểm tra chạy được", "từng nhóm", "Chuẩn bị", "Hành động", "Quét/đo", "Điều kiện đạt", "Tên test",
		"không còn X ở đâu", "mỗi loại một mẫu", "giảm/dọn Y", "đo trước và sau", "đúng một AC", "goal_check_missing",
		"bộ test mà `verify.command` chạy",
		"Không tự cắt phạm vi", "cấm tách giai đoạn chỉ vì ngân sách AC/TSK", "toàn bộ", "`- G-N:", "`G-1` là phiếu này", "mới xong 1/N giai đoạn",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
	// Mục kiểm tra mục tiêu đứng trước phần độ sâu (áp mọi mức) và có trong khuôn requirement.md.
	body := s.Body()
	if i, j := strings.Index(body, "## Kiểm tra mục tiêu (mọi mức phân tích)"), strings.Index(body, "## Độ sâu phân tích"); i < 0 || j < 0 || i > j {
		t.Errorf("mục kiểm tra mục tiêu phải đứng trước phần độ sâu (%d, %d)", i, j)
	}
	if !strings.Contains(body[strings.Index(body, "bint task write <id> requirement"):], "## Kiểm tra mục tiêu") {
		t.Error("khuôn requirement.md phải có mục ## Kiểm tra mục tiêu")
	}
}

// GĐ2 nhóm 2: b-feat viết kiểm tra mục tiêu thành test tự động trước khi sửa (khi chưa có bản b-goal đóng băng; có thì không viết lại)
// và không kết thúc khi còn đỏ.
func TestCoreSkillBFeatRunsGoalCheckUntilGreen(t *testing.T) {
	s, _ := Get("b-feat")
	flat := strings.Join(strings.Fields(s.Body()), " ")
	for _, want := range []string{
		"Chưa có kiểm tra mục tiêu đóng băng", "Kiểm tra mục tiêu đã đóng băng thì không viết lại", "ĐÃ ĐƯỢC VIẾT độc lập", "không viết, sửa, xoá hay đổi tên", "`## Kiểm tra mục tiêu`", "test tự động", "bộ test mà `verify.command` của phiếu chạy", "trước khi sửa",
		"sau mỗi nhóm sửa", "Không nới", "Không kết thúc khi kiểm tra mục tiêu còn đỏ", "`unmet`", "đầu ra của test", "workflow.max_review_rounds",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("b-feat thiếu %q", want)
		}
	}
}

// b-do không còn dặn lập phiếu không cờ để Bint tự chọn đường nhanh (người dùng chọn bằng /b-do-fast); vẫn báo "mới xong 1/N giai đoạn"
// nguyên văn và dặn tóm tắt kế hoạch lớn cho người dùng, chỉ resume khi họ đồng ý.
func TestCoreSkillBDoFastPathAndPhaseReport(t *testing.T) {
	s, _ := Get("b-do")
	flat := strings.Join(strings.Fields(s.Body()), " ")
	for _, want := range []string{
		"/b-do-fast", "mới xong 1/N giai đoạn", "nguyên văn", "Bước 1c", "kiểm chứng cuối không qua",
		"`kế hoạch lớn:`", "tóm tắt kế hoạch cho người dùng", "Chỉ khi họ đồng ý", "workflow.small_plan_max_files",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
	for _, gone := range []string{"**Ngoại lệ — yêu cầu ngắn, mơ hồ, rủi ro thấp**", "**không kèm** `--plan`/`--no-plan`/`--depth`", "classification.fast: true"} {
		if strings.Contains(flat, gone) {
			t.Errorf("b-do còn dặn tự chọn đường nhanh: %q", gone)
		}
	}
}

// b-do-fast là skill mỏng gọi `--fast`; b-req dặn ghi mục `## Cỡ kế hoạch`.
func TestCoreSkillBDoFastAndPlanSize(t *testing.T) {
	s, ok := Get("b-do-fast")
	if !ok {
		t.Fatal("thiếu skill b-do-fast")
	}
	if !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Error("b-do-fast chỉ do người gọi")
	}
	flat := strings.Join(strings.Fields(s.Body()), " ")
	for _, want := range []string{"bint task new", "--workflow b-do --fast", "bint run b-do --task <id> --fast", "classification.fast", "/b-do", "bỏ b-req"} {
		if !strings.Contains(flat, want) {
			t.Errorf("b-do-fast thiếu %q", want)
		}
	}
	req, _ := Get("b-req")
	rflat := strings.Join(strings.Fields(req.Body()), " ")
	for _, want := range []string{"## Cỡ kế hoạch", "Kết luận: nhỏ", "Kết luận: lớn", "workflow.small_plan_max_files", "Không dùng `nhỏ` để cắt phạm vi", "G-N"} {
		if !strings.Contains(rflat, want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
}

// b-feat hỏi bằng --question (tác vụ dừng chờ người); b-do chuyển câu hỏi và ba lựa chọn tới người, không tự chấp nhận.
func TestCoreSkillsQuestionAndChoices(t *testing.T) {
	feat, _ := Get("b-feat")
	for _, want := range []string{"--question", "bint run resume <id> --answer", "không đổi tệp nào"} {
		if !strings.Contains(strings.Join(strings.Fields(feat.Body()), " "), want) {
			t.Errorf("b-feat thiếu %q", want)
		}
	}
	do, _ := Get("b-do")
	body := strings.Join(strings.Fields(do.Body()), " ")
	for _, want := range []string{"--answer", "bint run next <id> --resume --answer", "--accept", "--fix-round", "bint task cancel <id>",
		"Không tự chạy `--accept` hay `--fix-round`", "choices", "workflow.max_review_rounds"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
}

// b-knowledge giao việc nhập liệu cho agent nhưng không bao giờ thay người duyệt.
func TestCoreSkillBKnowledgeContract(t *testing.T) {
	s, ok := Get("b-knowledge")
	if !ok {
		t.Fatal("thiếu b-knowledge")
	}
	if !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Error("b-knowledge chỉ do người gọi")
	}
	for _, want := range []string{
		"bint knowledge add", "--tags nguon:", "--evidence", "--third-party", "bint knowledge verify",
		"không bao giờ** chạy `bint knowledge approve`",
		"<!-- bint:anchor ingest-focus -->", "<!-- bint:anchor-end ingest-focus -->",
	} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-knowledge thiếu %q", want)
		}
	}
}

// b-review dùng `bint task diff <id>` làm phạm vi mặc định (không lẫn thay đổi của tác vụ khác).
func TestCoreSkillBReviewUsesTaskDiff(t *testing.T) {
	sk, ok := Get("b-review")
	if !ok {
		t.Fatal("thiếu skill b-review")
	}
	body := sk.Body()
	for _, want := range []string{"bint task diff <id>", "--stat", "--name-only"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-review thiếu %q", want)
		}
	}
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "git diff <base>") {
			t.Errorf("b-review không được chỉ dẫn chạy git diff <base> (lẫn thay đổi của tác vụ khác): %q", l)
		}
	}
}

// G2 (docs/isolation-subagent-plan.md 5.5): b-do chọn chế độ cô lập, báo nơi chạy, xử lý xung đột khi đưa về và
// `task land` trước `done` khi chạy từng bước; b-review và b-feat nhắc cwd là worktree của tác vụ.
func TestCoreSkillsMentionWorktreeIsolation(t *testing.T) {
	want := map[string][]string{
		"b-do": {"isolation_reason", "concurrent_task", "--worktree", "--shared", "bint task land <id>", "xung đột khi đưa về",
			"--3way", "chỉ người", "bint task cancel <id>", "landed", "cây cô lập"},
		"b-review": {"worktree riêng", "bint task diff <id>", "không `bint task land`"},
		"b-feat":   {"worktree riêng", "bint/<id>", "bint run b-do|b-feat --task <id>"},
	}
	for name, subs := range want {
		sk, ok := Get(name)
		if !ok {
			t.Fatalf("thiếu skill %s", name)
		}
		for _, s := range subs {
			if !strings.Contains(sk.Body(), s) {
				t.Errorf("%s thiếu %q", name, s)
			}
		}
	}
	// Orchestrator không được tự chạy --3way / --discard (chỉ người).
	b, _ := Get("b-do")
	for _, l := range strings.Split(b.Body(), "\n") {
		if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "bint task land <id> --3way") || strings.HasPrefix(tl, "bint task cancel <id> --discard") {
			t.Fatalf("b-do chỉ dẫn lệnh chỉ-người như việc của orchestrator: %q", l)
		}
	}
}

// G3b (docs/isolation-subagent-plan.md 6.7): b-do có nhánh driver session — vòng next → Agent → complete --agent-id, action
// process chạy nền, blocked kèm lệnh complete, không có công cụ Agent thì dùng driver process; ranh giới tin cậy giữ nguyên.
func TestBDoDriverSessionBranch(t *testing.T) {
	b, ok := Get("b-do")
	if !ok {
		t.Fatal("thiếu b-do")
	}
	body := b.Body()
	for _, want := range []string{"--driver session", "bint run next <id> --json --no-wait", "bint run complete <id> --run <run_id> --agent-id <agentId> --json",
		"--failed", "action: \"process\"", "action: \"blocked\"", "driver process", "bint run abort <id>", "subagent_type", "công cụ Agent",
		"**không tự làm**", "không tự sửa mã, không `bint task approve`"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-do thiếu %q", want)
		}
	}
	// G4: chạy song song bằng subagent, rào chắn đường dẫn worktree bằng hook (không còn câu "chưa có").
	for _, want := range []string{"Chạy song song nhiều việc bằng subagent", "`--worktree`", "run_in_background: true", "một phiếu riêng",
		"chỉ tác vụ đó", "`blocked`", "bint-implementer", "cd <worktree> &&"} {
		if !strings.Contains(body, want) {
			t.Errorf("b-do thiếu hướng dẫn chạy song song %q", want)
		}
	}
	if strings.Contains(body, "Rào chắn đường dẫn bằng hook chưa có") {
		t.Error("b-do còn câu cũ 'Rào chắn đường dẫn bằng hook chưa có'")
	}
	if !strings.Contains(string(b.Content), "dùng subagent") {
		t.Error("description phải nhắc kích hoạt bằng 'dùng subagent'")
	}
	// Mọi bản xuất bản (Claude và .agents) mang cùng nhánh.
	for _, tg := range []Target{ClaudeTarget, AgentsTarget} {
		if !strings.Contains(ForTarget(b, tg).Body(), "Bước 2b") {
			t.Errorf("%s thiếu Bước 2b", tg.Dir)
		}
	}
}

// Tác vụ 01M7826Q… (nghiệm thu 12.16): trước khi chạy driver session b-do kiểm hook SubagentStop (thiếu thì chi phí subagent
// không đo được), thiếu thì đề nghị người dùng chạy `bint init --hooks` chứ không tự chạy; notes của `bint run next` là lưới an toàn.
func TestBDoChecksSubagentStopHook(t *testing.T) {
	b, ok := Get("b-do")
	if !ok {
		t.Fatal("thiếu b-do")
	}
	for _, tg := range []Target{ClaudeTarget, AgentsTarget} {
		body := ForTarget(b, tg).Body()
		for _, want := range []string{"Kiểm hook `SubagentStop`", "bint doctor --json", "hook claude", "bint init --hooks",
			"không đo được", "Không tự chạy `bint init`", "notes", "chưa cài hook SubagentStop"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: b-do thiếu chỉ dẫn kiểm hook %q", tg.Dir, want)
			}
		}
		// Việc kiểm nằm trước lệnh lập phiếu --driver session.
		if i, j := strings.Index(body, "Kiểm hook `SubagentStop`"), strings.Index(body, "1. Lập phiếu như Bước 1 nhưng thêm `--driver session`"); i < 0 || j < 0 || i > j {
			t.Errorf("%s: kiểm hook phải đứng trước bước lập phiếu driver session", tg.Dir)
		}
		// Orchestrator không được tự chạy bint init.
		for _, l := range strings.Split(body, "\n") {
			if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "bint init") {
				t.Errorf("%s: b-do chỉ dẫn orchestrator tự chạy %q", tg.Dir, tl)
			}
		}
	}
}

func TestCoreSkillBAddSkillContract(t *testing.T) {
	s, ok := Get("b-add-skill")
	if !ok {
		t.Fatal("thiếu skill gốc b-add-skill")
	}
	d, err := ParseDoc(s.Content)
	if err != nil || d.Name != "b-add-skill" || !strings.HasPrefix(d.Description, "Dùng khi") {
		t.Fatalf("frontmatter: %v %+v", err, d)
	}
	if len(d.Extra) != 1 || d.Extra[0] != "disable-model-invocation" || !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Fatalf("phải có disable-model-invocation: true, khoá thừa: %v", d.Extra)
	}
	as, err := s.Anchors()
	if err != nil || len(as) != 1 || as[0].Name != "project-rules" {
		t.Fatalf("điểm neo: %v %+v", err, as)
	}
	// description chỉ nêu điều kiện kích hoạt, không tóm tắt cách làm.
	for _, w := range []string{"bint skill", "RED", "bint sync"} {
		if strings.Contains(d.Description, w) {
			t.Fatalf("description không được tóm tắt cách làm (%q): %s", w, d.Description)
		}
	}
	for _, want := range []string{"bint skill new", "bint skill lint", "bint sync", "RED", "GREEN", "REFACTOR", "subagent",
		"bint knowledge add", "bint skill overlay", "Không tự `git commit`", "bint knowledge approve", "bint task approve", "tests/",
		"bint skill test <tên> --arm without", "bint skill test <tên>\n", "--yes", ".bint/skills/<tên>/tests/", "không được xuất bản",
		"runtime/skill-tests/", "hỏi người dùng đồng ý chi phí", "expect:", "pressures:",
		"Quy ước dự án", "context_for_paths", "cite_knowledge"} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-add-skill thiếu %q", want)
		}
	}
	if n := strings.Count(s.Body(), "\n"); n > MaxBodyLines {
		t.Fatalf("thân %d dòng", n)
	}
}

func TestCoreSkillBAddFlowContract(t *testing.T) {
	s, ok := Get("b-add-flow")
	if !ok {
		t.Fatal("thiếu skill gốc b-add-flow")
	}
	d, err := ParseDoc(s.Content)
	if err != nil || d.Name != "b-add-flow" || !strings.HasPrefix(d.Description, "Dùng khi") {
		t.Fatalf("frontmatter: %v %+v", err, d)
	}
	if len(d.Extra) != 1 || d.Extra[0] != "disable-model-invocation" || !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Fatalf("phải có disable-model-invocation: true, khoá thừa: %v", d.Extra)
	}
	as, err := s.Anchors()
	if err != nil || len(as) != 1 || as[0].Name != "project-rules" {
		t.Fatalf("điểm neo: %v %+v", err, as)
	}
	// description chỉ nêu điều kiện kích hoạt, không tóm tắt cách làm.
	for _, w := range []string{"bint flow", "bint sync", "lint"} {
		if strings.Contains(d.Description, w) {
			t.Fatalf("description không được tóm tắt cách làm (%q): %s", w, d.Description)
		}
	}
	for _, want := range []string{"kind", "skill", "role", "optional", "verify", "fix", "plan → implement → review",
		"bint task write", "hợp đồng v1", "bint task review", "bint flow lint", "bint flow list", "bint sync", "/b-add-skill",
		"bint task new", "--workflow <tên>", "bint run <tên> --task", "phiếu nhỏ",
		"Không tự `git commit`", "bint knowledge approve", "bint task approve", ".bint/flows/<tên>.yml", "Quy ước dự án"} {
		if !strings.Contains(s.Body(), want) {
			t.Errorf("b-add-flow thiếu %q", want)
		}
	}
	if n := strings.Count(s.Body(), "\n"); n > MaxBodyLines {
		t.Fatalf("thân %d dòng", n)
	}
	// Ví dụ flow trong skill phải qua kiểm của Bint (kể cả skill viet-kiem-thu-do giả định có).
	i := strings.Index(s.Body(), "```yaml\n")
	j := strings.Index(s.Body()[i+8:], "```")
	def, err := flow.Decode([]byte(s.Body()[i+8 : i+8+j]))
	if err != nil {
		t.Fatalf("ví dụ flow: %v", err)
	}
	if errs := flow.Validate(def, "tdd", func(n string) bool { _, ok := Get(n); return ok || n == "viet-kiem-thu-do" }); len(errs) != 0 {
		t.Fatalf("ví dụ flow không qua lint: %v", errs)
	}
}

// Benchmark lần 3: b-feat dặn không sinh agent con; b-do biết dừng vòng sửa khi review báo không hội tụ.
func TestCoreSkillsNoSubagentsAndNonConvergence(t *testing.T) {
	feat, _ := Get("b-feat")
	if !strings.Contains(feat.Body(), "không sinh agent con") {
		t.Error("b-feat phải dặn không sinh agent con")
	}
	do, _ := Get("b-do")
	if !strings.Contains(do.Body(), "KHÔNG HỘI TỤ") {
		t.Error("b-do phải nêu điều kiện dừng khi review báo KHÔNG HỘI TỤ")
	}
	for _, want := range []string{"last_green", "open_findings", "bint task restore-green", "chỉ người dùng chạy"} {
		if !strings.Contains(strings.Join(strings.Fields(do.Body()), " "), want) {
			t.Errorf("b-do thiếu %q (điểm verify xanh)", want)
		}
	}
}

// Benchmark đợt 6: kiểm tra mục tiêu đo theo ma trận loại × nơi, không hẹp hơn bộ mẫu sẵn có; b-req giữ phạm vi theo lời người
// dùng (phần mở rộng tách G-N hoặc lát cuối) và đọc cấu hình đã hợp nhất.
func TestCoreSkillsGoalMatrixScopeAndMergedConfig(t *testing.T) {
	flat := func(name string) string {
		s, _ := Get(name)
		return strings.Join(strings.Fields(s.Body()), " ")
	}
	req := flat("b-req")
	for _, want := range []string{
		"**Ma trận loại × nơi**", "`Loại:`", "`Nơi:`", "**mọi cặp**", "**không được hẹp hơn** loại người dùng nêu, bộ quy tắc/danh mục sẵn có của dự án",
		"biến thể phổ biến ngoài đời", "**Giữ phạm vi theo lời người dùng**", "**Phần mở rộng**", "không vào `AC`: ghi một dòng `G-N`",
		"nhóm bề mặt **cuối cùng** ghi `(mở rộng)`", "**Cấu hình hiệu lực**", "đừng suy từ một tệp `.bint/*.yml` riêng lẻ",
		"`.bint/local.yml` thắng `.bint/config.yml`",
	} {
		if !strings.Contains(req, want) {
			t.Errorf("b-req thiếu %q", want)
		}
	}
	goal := flat("b-goal")
	for _, want := range []string{
		"**Phủ ma trận loại × nơi**", "subtest `<loại>/<nơi>`", "**Không hẹp hơn bộ mẫu sẵn có**", "dòng `Loại:` của requirement.md",
		"vẫn thêm vào test", "**hình dạng thật**", "danh sách loại × nơi đã phủ",
	} {
		if !strings.Contains(goal, want) {
			t.Errorf("b-goal thiếu %q", want)
		}
	}
}
