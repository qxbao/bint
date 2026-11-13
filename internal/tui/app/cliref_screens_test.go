package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// Thông điệp của tầng dịch vụ nhắc lệnh CLI khi hiện trong TUI phải chỉ phím/hành động TUI (docs/tui-plan.md mục 9, yêu cầu bổ sung;
// ui.Rewrite). Các test dưới đây đi qua từng màn với thông điệp thật của dịch vụ (dịch vụ chạy trên repository tạm hoặc chuỗi lấy đúng
// từ mã nguồn dịch vụ, có kiểm chuỗi còn trong nguồn) và khẳng định bản vẽ cùng hộp Thông báo không còn "bint <lệnh>" của CLI, ngoài
// dòng lệnh CLI tương đương của form (ui.cli_equivalent) và cột CLI của trợ giúp.

var cliWordsRE = func() *regexp.Regexp {
	seen := map[string]bool{}
	var ws []string
	for _, p := range actions.CLIPaths() {
		if w := strings.Fields(p)[0]; !seen[w] {
			seen[w] = true
			ws = append(ws, regexp.QuoteMeta(w))
		}
	}
	return regexp.MustCompile(`\bbint (?:` + strings.Join(ws, "|") + `)\b`)
}()

// cliLeaks trả các dòng của out nhắc lệnh bint của CLI, bỏ dòng lệnh CLI tương đương của form và cột CLI của trợ giúp.
func cliLeaks(out string) []string {
	var bad []string
	for _, l := range strings.Split(plain(out), "\n") {
		if cliWordsRE.MatchString(l) && !strings.Contains(l, "Lệnh CLI tương đương") && !strings.Contains(l, "CLI:") {
			bad = append(bad, strings.TrimSpace(l))
		}
	}
	return bad
}

// flat bỏ khung và gộp khoảng trắng để tìm cụm bị ngắt dòng.
func flat(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "┃", " ").Replace(plain(s))), " ")
}

// readNotices mở hộp Thông báo (!) và trả nội dung đầy đủ rồi đóng hộp.
func readNotices(t *testing.T, m *Model) string {
	t.Helper()
	press(t, m, "!")
	var all strings.Builder
	for i := 0; i < 12; i++ {
		all.WriteString(plain(m.Render()))
		all.WriteString("\n")
		press(t, m, "pgdown")
	}
	press(t, m, "esc")
	return all.String()
}

// expectTUIRef khẳng định bản vẽ và hộp Thông báo của m không còn lệnh CLI và có tham chiếu TUI want (mọi mục).
func expectTUIRef(t *testing.T, what string, m *Model, want ...string) {
	t.Helper()
	out := plain(m.Render())
	box := readNotices(t, m)
	for _, s := range []string{out, box} {
		if bad := cliLeaks(s); len(bad) > 0 {
			t.Errorf("%s: còn lệnh CLI trên màn hình/hộp Thông báo:\n  %s", what, strings.Join(bad, "\n  "))
		}
	}
	for _, w := range want {
		if !strings.Contains(flat(out), w) && !strings.Contains(flat(box), w) {
			t.Errorf("%s: thiếu tham chiếu TUI %q:\n%s\n--- hộp Thông báo ---\n%s", what, w, out, box)
		}
	}
}

// svcLiteral trả chuỗi frag sau khi kiểm nó còn nằm nguyên văn trong tệp nguồn của dịch vụ (để test không lệch khỏi mã dịch vụ).
func svcLiteral(t *testing.T, file, frag string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), frag) {
		t.Fatalf("%s không còn chứa %q: cập nhật test theo mã dịch vụ", file, frag)
	}
	return frag
}

// Tri thức: duyệt mục có bằng chứng đã đổi (thông điệp thật của ReviewKnowledge) và finding bằng chứng dời vùng dòng (verify thật).
func TestCLIRefKnowledgeScreen(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{
		"Makefile": "gen:\n\tsqlc generate\n\ntest:\n\tgo test ./...\n",
		"src/f.go": "package src\n\n// F làm việc.\nfunc F() {}\n",
	})
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, evidence string) string {
		res, err := bintapp.AddKnowledge(ctx, k, bintapp.AddOptions{Type: knowledge.TypeFact, Title: title, Body: "Nội dung " + title,
			Paths: []string{"**"}, Evidence: []string{evidence}, Origin: knowledge.OriginAgent})
		if err != nil || !res.Saved {
			t.Fatalf("%s: %v %+v", title, err, res.Check.Findings)
		}
		return res.Item.ID
	}
	changed := add("Mục bằng chứng đổi", "Makefile:1-2")
	moved := add("Mục vùng dòng dời", "src/f.go:4")
	os.WriteFile(filepath.Join(root, "Makefile"), []byte("gen:\n\tsqlc generate --strict\n\ntest:\n\tgo test ./...\n"), 0o644)
	os.WriteFile(filepath.Join(root, "src/f.go"), []byte("package src\n\nimport \"fmt\"\n\n// F làm việc.\nfunc F() {}\n"), 0o644)

	// Thông điệp thật: duyệt mục có bằng chứng đã đổi.
	k2, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	outs, err := bintapp.ReviewKnowledge(ctx, k2, bintapp.ReviewAction{Refs: []string{changed}, To: knowledge.StatusVerified})
	if err != nil || len(outs) != 1 || outs[0].Err == nil {
		t.Fatalf("dịch vụ phải từ chối duyệt: %v %+v", err, outs)
	}
	// Lỗi duyệt có cấu trúc (không còn chuỗi nhắc lệnh CLI của dịch vụ): TUI dựng câu và phím từ cấu trúc.
	var ee *bintapp.EvidenceError
	if !errors.As(outs[0].Err, &ee) || ee.Fix() != knowledge.FixRefresh || strings.Contains(outs[0].Err.Error(), "bint ") {
		t.Fatalf("muốn *EvidenceError cách sửa refresh, không có lệnh CLI: %T %v", outs[0].Err, outs[0].Err)
	}
	m := New(ctx, Options{Dir: root, Version: "t", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond})
	m.SetSize(140, 40)
	drive(t, m, m.Init())
	press(t, m, "2")
	_, cmd := m.Update(actions.ResultMsg{ID: "knowledge.approve", Value: outs})
	drive(t, m, cmd)
	expectTUIRef(t, "duyệt mục có bằng chứng đã đổi", m, "phím f ở Tri thức")

	// Finding bằng chứng dời vùng dòng nằm ở khung chi tiết của mục.
	for i := 0; i < 8 && (m.kn.Cursor() == nil || m.kn.Cursor().ID != moved); i++ {
		press(t, m, "j")
	}
	if c := m.kn.Cursor(); c == nil || c.ID != moved {
		t.Fatalf("không chọn được mục dời vùng dòng: %+v", c)
	}
	press(t, m, "enter")
	for i := 0; i < 4 && !strings.Contains(flat(m.Render()), "[ô Áp dụng]"); i++ {
		press(t, m, "pgdown")
	}
	out := plain(m.Render())
	if !strings.Contains(flat(out), "phím v ở Tri thức [ô Áp dụng]") {
		t.Fatalf("finding dời vùng dòng phải chỉ phím v + ô Áp dụng:\n%s", out)
	}
	if bad := cliLeaks(out); len(bad) > 0 {
		t.Fatalf("còn lệnh CLI: %v", bad)
	}
}

// Tác vụ: điều kiện chưa thể done (thật, từ phiếu chưa xác nhận lệnh kiểm chứng), lý do blocked (đúng mẫu của workflow) và lỗi chuyển
// trạng thái đều nhắc task approve / task verify / run resume bằng phím TUI.
func TestCLIRefTasksScreen(t *testing.T) {
	f := newFixture(t, Options{})
	id := f.newTask("Đổi cách tính phí", func(o *bintapp.TaskNewOptions) { o.VerifyCommand, o.VerifyCommandSet = "go test ./...", true })
	f.selectTask(id)
	f.press("3")
	f.press("enter")
	// Điều kiện chưa thể done nằm ở thẻ Phiếu (Info).
	out := plain(f.m.Render())
	for i := 0; i < 10 && !strings.Contains(flat(out), "phím a ở Tác vụ"); i++ {
		f.press("pgdown")
		out = plain(f.m.Render())
	}
	if !strings.Contains(flat(out), "phím a ở Tác vụ") {
		t.Fatalf("điều kiện chưa thể done phải chỉ phím a (duyệt lệnh kiểm chứng):\n%s", out)
	}
	if bad := cliLeaks(out); len(bad) > 0 {
		t.Fatalf("điều kiện chưa thể done còn lệnh CLI: %v", bad)
	}
	// Lỗi khi chuyển done thật từ dịch vụ.
	_, err := bintapp.SetTaskStatus(context.Background(), f.ws, bintapp.TaskStatusOptions{Ref: id, To: task.StatusDone, Reason: "thử"})
	if err == nil {
		t.Fatal("chuyển done khi chưa đủ bằng chứng phải lỗi")
	}
	_, cmd := f.m.Update(actions.ResultMsg{ID: "tasks.status", Err: err})
	f.settle(cmd)
	expectTUIRef(t, "lỗi chuyển done", f.m, "phím a ở Tác vụ")

	// Lý do blocked đúng mẫu của workflow (run.go askVerify) và tiếp tục bằng run resume.
	reason := "Cần lệnh kiểm chứng — trả lời: " + svcLiteral(t, "internal/workflow/run.go", "bint task set ") + id + " --verify-command '…' rồi " +
		svcLiteral(t, "internal/workflow/run.go", "bint run resume ") + id
	if _, err := bintapp.SetTaskStatus(context.Background(), f.ws, bintapp.TaskStatusOptions{Ref: id, To: task.StatusBlocked, Reason: reason, Force: true}); err != nil {
		t.Fatal(err)
	}
	f.press("ctrl+r")
	f.pump(func() bool { return strings.Contains(flat(f.m.Render()), "phím c ở Tác vụ") }, nil)
	expectTUIRef(t, "lý do blocked", f.m, "phím e ở Tác vụ [ô Lệnh kiểm chứng]", "phím c ở Tác vụ")
}

var _ = config.Default

// Tổng quan và Hệ thống: doctor thật trên repository tạm (cấu hình theo mẫu cũ, skill bị sửa tay) nhắc init --update và sync bằng phím TUI.
func TestCLIRefOverviewAndSystemDoctor(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, BintPath: "bint", Harnesses: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	// Cấu hình theo mẫu cũ: bỏ một khoá có trong mẫu.
	cfgPath := config.ProjectPath(root)
	b, _ := os.ReadFile(cfgPath)
	var kept []string
	dropped := false
	for _, l := range strings.Split(string(b), "\n") {
		if !dropped && strings.Contains(l, "prior_run_usd") {
			dropped = true
			continue
		}
		kept = append(kept, l)
	}
	if !dropped {
		t.Fatal("không thấy khoá để bỏ khỏi mẫu cấu hình")
	}
	os.WriteFile(cfgPath, []byte(strings.Join(kept, "\n")), 0o644)
	// Skill đã xuất bản bị sửa tay.
	skill := filepath.Join(root, ".claude", "skills", "b-bootstrap", "SKILL.md")
	sb, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(skill, append(sb, []byte("\nSửa tay.\n")...), 0o644)
	rep := bintapp.Doctor(ctx, root)
	var svc strings.Builder
	for _, c := range rep.Checks {
		svc.WriteString(c.Name + ": " + c.Detail + "\n")
	}
	if !strings.Contains(svc.String(), "`bint init --update`") || !strings.Contains(svc.String(), "`bint sync`") {
		t.Skipf("doctor thật không nhắc init --update/sync ở ca này:\n%s", svc.String())
	}

	m := New(ctx, Options{Dir: root, Version: "t", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond})
	m.SetSize(160, 40)
	drive(t, m, m.Init())
	// Mỗi hàng hộp thư hiện nguyên văn dưới danh sách khi được chọn: duyệt qua các hàng rồi gộp.
	var rows strings.Builder
	for i := 0; i < 10; i++ {
		rows.WriteString(flat(m.Render()) + "\n")
		press(t, m, "j")
	}
	for _, w := range []string{"nhấn U ở Hệ thống › Cấu hình", "nhấn s ở Hệ thống › Skill"} {
		if !strings.Contains(rows.String(), w) {
			t.Errorf("hộp thư Tổng quan thiếu %q:\n%s", w, rows.String())
		}
	}
	if bad := cliLeaks(rows.String()); len(bad) > 0 {
		t.Errorf("hộp thư Tổng quan còn lệnh CLI: %v", bad)
	}
	// Hệ thống › Doctor: chi tiết từng kiểm tra (danh sách bên trái, chi tiết nguyên văn bên phải).
	press(t, m, "7")
	for i := 0; i < 12 && m.sy.SubTab() != "Doctor"; i++ {
		press(t, m, ".")
	}
	if m.sy.SubTab() != "Doctor" {
		t.Fatalf("không tới được thẻ Doctor: %q", m.sy.SubTab())
	}
	var det strings.Builder
	for i := 0; i < 24; i++ {
		det.WriteString(flat(m.Render()) + "\n")
		press(t, m, "j")
	}
	for _, w := range []string{"nhấn U ở Hệ thống › Cấu hình", "nhấn s ở Hệ thống › Skill"} {
		if !strings.Contains(det.String(), w) {
			t.Errorf("Hệ thống › Doctor thiếu %q:\n%s", w, det.String())
		}
	}
	if bad := cliLeaks(det.String()); len(bad) > 0 {
		t.Errorf("Hệ thống › Doctor còn lệnh CLI: %v", bad)
	}
}

// Lượt chạy, Đánh giá và Tiến hoá: lỗi của dịch vụ (chuỗi lấy đúng từ nguồn dịch vụ) nhắc lệnh CLI hiện bằng phím TUI ở toast và hộp Thông báo.
func TestCLIRefRunsEvalEvolveScreens(t *testing.T) {
	cases := []struct {
		name, screen, id string
		msg              string
		want             []string
	}{
		{"Lượt chạy", "4", "runs.rate",
			"lượt 01RUN000002 không gắn với tác vụ nào (" + svcLiteral(t, "internal/app/task_svc.go", "bint runs show") + " 01RUN000002)",
			[]string{"phím Enter ở Lượt chạy"}},
		{"Đánh giá (chưa có đợt)", "5", "eval.run",
			svcLiteral(t, "internal/eval/db.go", "chưa có đợt đánh giá nào (chạy `bint eval run`)"),
			[]string{"nhấn r ở Đánh giá"}},
		{"Đánh giá (hết ngân sách)", "5", "eval.run",
			"đã tiêu $1.00 / $1.00 (budget); " + svcLiteral(t, "internal/eval/runner.go", "chạy tiếp bằng `bint eval run --resume ") + "eb_01BATCH --budget <mới>`",
			[]string{"tiếp tục bằng phím R ở Đánh giá"}},
		{"Tiến hoá (chưa kiểm chứng)", "6", "evolve.apply",
			"đề xuất EP1 chưa có kết quả supported; " + svcLiteral(t, "internal/evolve/apply.go", "chạy `bint evolve evaluate ") + "EP1 --split test`, hoặc dùng --unverified",
			[]string{"nhấn E ở Tiến hoá"}},
		{"Tiến hoá (cần agent)", "6", "evolve.process",
			svcLiteral(t, "internal/evolve/deferred.go", "chạy `bint evolve process` với harness đã cấu hình"),
			[]string{"nhấn P ở Tiến hoá"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 160, 40)
			press(t, m, tc.screen)
			_, cmd := m.Update(actions.ResultMsg{ID: tc.id, Err: errString(tc.msg)})
			drive(t, m, cmd)
			expectTUIRef(t, tc.name, m, tc.want...)
		})
	}
}

// Review 6b-sửa .2: cờ CLI rời trong kết quả duyệt ứng viên, kết quả đề xuất và lỗi quay lui (chuỗi thật của dịch vụ) vẽ ra thành cụm TUI
// chỉ tới ô tương ứng, không còn token --force/--no-agent/--budget. Kết quả vào thẳng ResultMsg (không chạy việc nền).
func TestCLIRefLooseFlagsInServiceFlows(t *testing.T) {
	noFlags := func(t *testing.T, what, out string) {
		t.Helper()
		for _, tok := range []string{"--force", "--no-agent", "--budget", "--unverified"} {
			if strings.Contains(out, tok) {
				t.Errorf("%s: còn token cờ %s:\n%s", what, tok, out)
			}
		}
	}
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 160, 40)

	// Đánh giá: hộp kết quả duyệt ứng viên (hai thông điệp thật của EvalApprove).
	press(t, m, "5")
	_, cmd := m.Update(actions.ResultMsg{ID: "eval.approve", Value: []bintapp.EvalApproveOutcome{
		{ID: "c1", Problem: "c1 đã bị loại (trùng); " + svcLiteral(t, "internal/app/eval_cases.go", "dùng --force nếu vẫn muốn duyệt")},
		{ID: "c2", Problem: "c2: đề bài lộ lời giải (x) — sửa `prompt` trong /tmp/c2.yml " + svcLiteral(t, "internal/app/eval_cases.go", "hoặc dùng --force")},
	}})
	drive(t, m, cmd)
	out := flat(m.Render())
	noFlags(t, "duyệt ứng viên", out)
	for _, w := range []string{"nếu vẫn muốn duyệt", "hoặc chọn ô “Vẫn duyệt dù …” trong hộp duyệt"} {
		if !strings.Contains(out, w) {
			t.Errorf("duyệt ứng viên: thiếu %q:\n%s", w, out)
		}
	}
	press(t, m, "esc")

	// Tiến hoá: kết quả đề xuất bỏ qua vì cần agent (ba chuỗi thật), lỗi quay lui (chuỗi thật).
	press(t, m, "6")
	_, cmd = m.Update(actions.ResultMsg{ID: "evolve.propose", Value: &evolvesvc.ProposeResult{ProposeReport: evolve.ProposeReport{Outcomes: []evolve.ProposeOutcome{
		{Opportunity: "op-1", Skipped: svcLiteral(t, "internal/evolve/generate.go", "cần agent evolver (bỏ --no-agent)")},
		{Opportunity: "op-2", Skipped: svcLiteral(t, "internal/evolve/harness_propose.go", "cần agent đề xuất quy trình (roles.evolver = claude; bỏ --no-agent)")},
		{Opportunity: "op-3", Skipped: svcLiteral(t, "internal/evolve/reflector.go", "cần agent reflector (bỏ --no-agent)")},
	}}}})
	drive(t, m, cmd)
	out = flat(m.Render())
	noFlags(t, "kết quả đề xuất", out)
	if n := strings.Count(out, "bỏ chọn ô “Không"); n != 3 {
		t.Errorf("kết quả đề xuất: muốn 3 cụm TUI, có %d:\n%s", n, out)
	}
	press(t, m, "esc")
	_, cmd = m.Update(actions.ResultMsg{ID: "evolve.rollback", Err: errString("tệp đã bị sửa sau khi áp dụng: a.go; xem lại rồi " +
		svcLiteral(t, "internal/evolve/apply.go", "dùng --force để ghi đè") + " bằng bản trước khi áp dụng")})
	drive(t, m, cmd)
	box := flat(readNotices(t, m))
	noFlags(t, "lỗi quay lui (hộp Thông báo)", box)
	noFlags(t, "lỗi quay lui (màn hình)", flat(m.Render()))
	if !strings.Contains(box, "chọn ô ghi đè tệp đã bị sửa trong hộp quay lui để ghi đè") {
		t.Errorf("lỗi quay lui: thiếu cụm TUI:\n%s", box)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Nội dung tệp không bị viết lại: diff xuất bản AGENTS.md hiện nguyên lệnh `bint knowledge add …` (là nội dung của tệp), trong khi chi tiết bước
// (thông điệp của dịch vụ) đi cùng báo cáo thì được viết lại thành phím TUI.
func TestCLIRefLeavesFileContentAlone(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 160, 40)
	press(t, m, "2")
	clickText(t, m, "Xuất bản AGENTS.md")
	if m.kn.SubTab() != "Xuất bản AGENTS.md" {
		t.Fatalf("không tới được thẻ Xuất bản: %q", m.kn.SubTab())
	}
	diff := "--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -1,1 +1,3 @@\n+Ghi bằng `bint knowledge add --type warning --paths '<glob>' \"<nội dung>\"`;\n+rồi chạy `bint bootstrap publish`.\n"
	rep := bintapp.PublishReport{Diff: diff, Steps: []bintapp.Step{{Name: "AGENTS.md", Status: bintapp.StepWarn, Detail: "chưa xuất bản (bint sync)"}}}
	_, cmd := m.Update(actions.ResultMsg{ID: "bootstrap.publish_preview", Value: rep})
	drive(t, m, cmd)
	out := flat(m.Render())
	for _, w := range []string{"`bint knowledge add --type warning", "rồi chạy `bint bootstrap publish`", "chưa xuất bản (phím s ở Hệ thống › Skill)"} {
		if !strings.Contains(out, w) {
			t.Errorf("thiếu %q:\n%s", w, plain(m.Render()))
		}
	}
}
