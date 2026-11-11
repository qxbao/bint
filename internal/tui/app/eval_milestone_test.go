package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/eval"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// evalRepo dựng repository thật đã init, có eval.test_command (sh), một commit lời giải kèm kiểm thử mới và một agent
// lệnh tuỳ chỉnh làm đúng việc đó; mô hình TUI dùng dịch vụ thật cho màn Đánh giá (không loader giả).
func evalRepo(t *testing.T) (f *fixture, agentCmd, goodID string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("sh"); err != nil {
			t.Skip("cần sh")
		}
	}
	testutil.Isolate(t)
	asHuman(t)
	calc := "add() { echo $(($1 + $2)); }\n"
	root := testutil.NewRepo(t, map[string]string{
		"lib/calc.sh":       calc,
		"tests/run.sh":      "set -e\nfor f in tests/test_*.sh; do sh \"$f\"; done\n",
		"tests/test_add.sh": ". lib/calc.sh\n[ \"$(add 2 3)\" = 5 ]\n",
	})
	testutil.Git(t, root, "config", "user.name", "t")
	testutil.Git(t, root, "config", "user.email", "t@example.com")
	commit := func(msg string, files map[string]string) string {
		for name, body := range files {
			p := filepath.Join(root, filepath.FromSlash(name))
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		testutil.Git(t, root, "add", "-A")
		testutil.Git(t, root, "commit", "-q", "-m", msg)
		return testutil.Git(t, root, "rev-parse", "HEAD")
	}
	good := commit("Add a multiplication helper to the calc library\n\nCallers need products, not only sums.",
		map[string]string{"lib/calc.sh": calc + "mul() { echo $(($1 * $2)); }\n", "tests/test_mul.sh": ". lib/calc.sh\n[ \"$(mul 4 5)\" = 20 ]\n"})
	goodID = "c" + good[:12]
	if _, err := bintapp.Init(context.Background(), bintapp.InitOptions{Dir: root, Hooks: boolPtr(false), NoSkills: true}); err != nil {
		t.Fatal(err)
	}
	local := "eval:\n  test_command: \"sh tests/run.sh\"\n  mine:\n    min_source_lines: 1\n"
	if err := os.WriteFile(filepath.Join(root, ".bint", "local.yml"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(t.TempDir(), "agent.sh")
	os.WriteFile(agent, []byte("echo 'mul() { echo $(($1 * $2)); }' >> lib/calc.sh\n"), 0o644)
	agentCmd = "sh " + filepath.ToSlash(agent)

	f = &fixture{t: t, root: root, msgs: make(chan tea.Msg, 256)}
	f.m = New(context.Background(), Options{Dir: root, Version: "test", Lang: "vi", Mode: theme.Mono,
		Loader: func(context.Context, string) (overview.Data, error) {
			return overview.Data{Status: &bintapp.StatusReport{Repo: root}}, nil
		},
		SpinnerFPS: time.Millisecond, TailEvery: 5 * time.Millisecond, ExitGrace: 3 * time.Second})
	f.m.SetSize(140, 40)
	f.pump(func() bool { return !f.m.ov.Loading }, f.m.Init())
	return f, agentCmd, goodID
}

func (f *fixture) typeText(s string) {
	f.t.Helper()
	for _, r := range s {
		f.m.Update(keyMsg(string(r))) // chữ vào ô nhập không phát sinh lệnh: không cần chờ
	}
}

func (f *fixture) screen() string { return plain(f.m.Render()) }

// batches đọc các đợt trong SQLite bằng dịch vụ thật.
func (f *fixture) batches() []eval.Batch {
	f.t.Helper()
	e, err := bintapp.OpenEval(context.Background(), f.root, true)
	if err != nil {
		f.t.Fatal(err)
	}
	defer e.Close()
	bs, err := eval.ListBatches(context.Background(), e.Store.DB, 10)
	if err != nil {
		f.t.Fatal(err)
	}
	return bs
}

// fillRunForm điền form chạy đợt: một nhánh none, hai lần lặp, agent lệnh tuỳ chỉnh (ở nhóm Nâng cao).
func (f *fixture) fillRunForm(agentCmd string) {
	f.t.Helper()
	f.press("r")
	f.press("tab", "tab")                                    // split → case → arms
	f.press("ctrl+u")                                        // xoá "none, minimal, bint"
	f.typeText("none")                                       // 1 nhánh
	f.press("tab", "ctrl+u")                                 // repeats
	f.typeText("2")                                          // 2 lần lặp
	f.press("ctrl+o")                                        // mở nhóm Nâng cao
	f.press("tab", "tab", "tab", "tab", "tab", "tab", "tab") // budget, harness, leakage, tiêu đề nhóm, limit, cost-per-run, agent-command
	f.typeText(agentCmd)
	if out := f.screen(); !strings.Contains(out, "--arms none") || !strings.Contains(out, "--repeats 2") || !strings.Contains(out, "--agent-command") {
		f.t.Fatalf("dòng lệnh tương đương phải khớp form:\n%s", out)
	}
}

// Mốc xong của giai đoạn 4 (docs/tui-plan.md mục 9): chỉ bằng phím trong TUI người khai thác có kiểm chứng, duyệt ứng
// viên, chạy một đợt nhỏ (agent lệnh tuỳ chỉnh, 1 nhánh × 2 lần lặp, đồng ý ở hộp xác nhận) tới completed, rồi thẻ Đợt
// (báo cáo) hiện biểu đồ cột tỷ lệ đạt 100% của nhánh đó.
func TestMilestoneEvalRunReport(t *testing.T) {
	f, agentCmd, goodID := evalRepo(t)
	f.press("5")
	f.pump(func() bool { return f.m.el.Loaded }, nil)
	if out := f.screen(); !strings.Contains(out, "Ứng viên (0)") {
		t.Fatalf("chưa có ứng viên:\n%s", out)
	}

	// 1. Khai thác có kiểm chứng (m, bật "Kiểm chứng fail→pass ngay").
	f.press("m", "tab", "x")
	if out := f.screen(); !strings.Contains(out, "--verify") || !strings.Contains(out, "bint eval mine") {
		t.Fatalf("form khai thác:\n%s", out)
	}
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.el.Tab() == 3 || f.m.jobs.Len() > 0 }, nil)
	f.waitJobsDone()
	f.pump(func() bool { return strings.Contains(f.screen(), "Kết quả khai thác") }, nil)
	hasAll(t, f.screen(), "Đã quét", "1 ứng viên mới")
	f.press("esc", ".") // đóng hộp rồi về thẻ Ứng viên
	f.pump(func() bool { return f.m.el.Cand() != nil }, nil)
	hasAll(t, f.screen(), "Ứng viên (1)", goodID, "đạt (fail→pass)")

	// 2. Duyệt ứng viên: hộp thường (đã kiểm chứng, không rò rỉ).
	f.press("a")
	if open, danger, checks := f.m.el.Dialog(); !open || danger || len(checks) != 0 {
		t.Fatalf("ứng viên đã kiểm chứng: hộp thường: %v %v %v\n%s", open, danger, checks, f.screen())
	}
	f.press("enter")
	f.pump(func() bool { return strings.Contains(f.screen(), "Kết quả duyệt") }, nil)
	hasAll(t, f.screen(), "✓ đã duyệt "+goodID)
	f.press("esc", ".")
	f.pump(func() bool { return f.m.el.Tab() == 1 && len(f.m.el.Data.Cases) == 1 }, nil)
	hasAll(t, f.screen(), "Tác vụ đã duyệt (1)", goodID)

	// 3. Chạy một đợt nhỏ: form → hộp xác nhận kèm ước tính và lưu ý → đồng ý.
	f.fillRunForm(agentCmd)
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.prompt != nil }, nil)
	hasAll(t, f.screen(), "Bắt đầu chạy đợt?", "Ước tính: 2 lượt", "Lưu ý:")
	if len(f.batches()) != 0 {
		t.Fatal("chưa đồng ý thì chưa có đợt")
	}
	f.press("enter")
	f.waitJobsDone()
	f.pump(func() bool { return f.m.el.Tab() == 2 && strings.Contains(f.screen(), "Tỷ lệ đạt theo nhánh") }, nil)

	bs := f.batches()
	if len(bs) != 1 || bs[0].Status != "completed" || len(bs[0].Arms) != 1 || bs[0].Arms[0] != "none" || bs[0].Repeats != 2 {
		t.Fatalf("đợt: %+v", bs)
	}
	out := f.screen()
	hasAll(t, out, "hoàn thành", "Tỷ lệ đạt theo nhánh", "Các nhánh", "none: đạt 100%", "chấm 2/2 lượt")
	chart := false
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "none") && strings.Contains(l, "█") && strings.Contains(l, "100%") {
			chart = true
		}
	}
	if !chart {
		t.Fatalf("thẻ Đợt phải có biểu đồ cột 100%% của nhánh none:\n%s", out)
	}
}

// Trả "không" ở hộp xác nhận: không có đợt mới trong SQLite; huỷ khi đang hỏi kết thúc trong hạn giờ.
func TestEvalRunDeclinedAndCancelledWhileAskingCreateNoBatch(t *testing.T) {
	f, agentCmd, goodID := evalRepo(t)
	e, err := bintapp.OpenEval(context.Background(), f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bintapp.EvalMine(context.Background(), e, bintapp.EvalMineDefaults(e.WS.Config)); err != nil {
		t.Fatal(err)
	}
	if _, err := bintapp.EvalApprove(context.Background(), e, bintapp.EvalApproveOptions{IDs: []string{goodID}, Unverified: true}); err != nil {
		t.Fatal(err)
	}
	e.Close()

	f.press("5", ".")
	f.pump(func() bool { return len(f.m.el.Data.Cases) == 1 }, nil)
	// Trả không.
	f.fillRunForm(agentCmd)
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.prompt != nil }, nil)
	f.press("n")
	f.waitJobsDone()
	if bs := f.batches(); len(bs) != 0 {
		t.Fatalf("trả không thì không tạo đợt: %+v", bs)
	}
	hasAll(t, f.screen(), "Đã trả lời không")
	// Huỷ khi đang hỏi: việc kết thúc trong hạn giờ.
	f.press(".", ".", ".") // cands, cases... về thẻ Tác vụ
	for f.m.el.Tab() != 1 {
		f.press(".")
	}
	f.fillRunForm(agentCmd)
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.prompt != nil }, nil)
	start := time.Now()
	f.settle(func() tea.Msg { return actions.CancelMsg{ID: "eval.run"} })
	f.pump(func() bool { return f.m.jobs.Len() == 0 && f.m.prompt == nil }, nil)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("huỷ khi đang hỏi phải kết thúc trong hạn giờ: %v", time.Since(start))
	}
	if bs := f.batches(); len(bs) != 0 {
		t.Fatalf("huỷ khi đang hỏi thì không tạo đợt: %+v", bs)
	}
}

// Esc khi đang chạy: dịch vụ thật trả đợt cancelled và màn hiện lệnh chạy tiếp --resume.
func TestEvalRunEscCancelsBatchAndShowsResumeCommand(t *testing.T) {
	f, _, goodID := evalRepo(t)
	e, err := bintapp.OpenEval(context.Background(), f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bintapp.EvalMine(context.Background(), e, bintapp.EvalMineDefaults(e.WS.Config)); err != nil {
		t.Fatal(err)
	}
	if _, err := bintapp.EvalApprove(context.Background(), e, bintapp.EvalApproveOptions{IDs: []string{goodID}, Unverified: true}); err != nil {
		t.Fatal(err)
	}
	e.Close()

	f.press("5", ".")
	f.pump(func() bool { return len(f.m.el.Data.Cases) == 1 }, nil)
	f.fillRunForm("sleep 30") // agent chạy lâu: huỷ giữa chừng
	f.press("ctrl+s")
	f.pump(func() bool { return f.m.prompt != nil }, nil)
	f.press("enter")
	f.pump(func() bool {
		return strings.Contains(f.screen(), "Tiến độ theo ô") && strings.Contains(f.screen(), "0/2 lượt đã xong")
	}, nil)
	f.press("esc") // Esc ở thẻ Log huỷ an toàn
	f.waitJobsDone()
	bs := f.batches()
	if len(bs) != 1 || bs[0].Status != "cancelled" {
		t.Fatalf("đợt phải cancelled: %+v", bs)
	}
	hasAll(t, f.screen(), "Kết quả chạy đợt", "đã huỷ", "Chạy tiếp bằng: bint eval run --resume "+bs[0].BatchID)
	f.press("esc")
	hasAll(t, f.screen(), "Chạy tiếp", "bint eval run --resume "+bs[0].BatchID)
}
