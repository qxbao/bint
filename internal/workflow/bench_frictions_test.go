package workflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"bint/internal/task"
)

// Các hồi quy từ benchmark Bint vs làm tay ngày 2026-11-16 (docs/evals/bench-bint-vs-plain-20261116, mục "Điểm vướng").

// Điểm vướng 1: lệnh kiểm chứng chạy với TMPDIR là thư mục riêng mới tạo trong thư mục trạng thái (không phải /tmp dùng
// chung với sandbox Codex, nơi /tmp/.git làm `go` báo lỗi VCS), và thư mục đó được dọn sau khi chạy.
func TestVerifyUsesPrivateTempDir(t *testing.T) {
	h := newHarness(t)
	out := filepath.Join(t.TempDir(), "tmpdir.txt")
	// Windows: sh.exe (MSYS) in TMPDIR dạng /c/Users/... hoặc /tmp/...; cygpath -w đổi về đường dẫn Windows.
	show := `printf %s "$TMPDIR"`
	if runtime.GOOS == "windows" {
		show = `printf %s "$(cygpath -w "$TMPDIR")"`
	}
	cmd := `test -f done.txt && test -d "$TMPDIR" && ` + show + ` > '` + strings.ReplaceAll(filepath.ToSlash(out), "'", `'\''`) + `'`
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat() // b-feat → kiểm chứng của Runner
	ran := func() string {
		t.Helper()
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("lệnh kiểm chứng không chạy hoặc thiếu TMPDIR: %v (%+v)", err, h.load(id).State.LastVerification())
		}
		os.Remove(out)
		return strings.TrimSpace(string(b))
	}
	base := verifyTempBase(h.tasks.Dir)
	check := func(who, dir string) {
		t.Helper()
		dir = nativePath(dir) // phòng khi cygpath không có: /c/Users/... → C:\Users\...
		if dir == "" || dir == os.TempDir() || filepath.Dir(filepath.Clean(dir)) != base {
			t.Fatalf("%s: TMPDIR=%q, muốn thư mục con riêng của %s", who, dir, base)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s: thư mục tạm %s phải được dọn sau khi chạy (%v)", who, dir, err)
		}
	}
	first := ran()
	check("Runner", first)
	// bint task verify (Env.Verify): cùng cơ chế, mỗi lần một thư mục mới.
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	second := ran()
	check("bint task verify", second)
	if first == second {
		t.Fatalf("mỗi lần kiểm chứng cần thư mục tạm mới: %q", first)
	}
}

// nativePath đổi dạng MSYS "/c/Users/x" mà sh.exe in ra về "C:\Users\x" trên Windows; nơi khác giữ nguyên.
func nativePath(p string) string {
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == '/' &&
		(p[1] >= 'a' && p[1] <= 'z' || p[1] >= 'A' && p[1] <= 'Z') {
		return filepath.FromSlash(strings.ToUpper(p[1:2]) + ":" + p[2:])
	}
	return p
}

// TestHelperPrintTempDir là tiến trình Go con của TestVerifyChildProcessTempDir: ghi os.TempDir() mà nó thấy.
func TestHelperPrintTempDir(t *testing.T) {
	out := os.Getenv("BINT_HELPER_TMPOUT")
	if out == "" {
		t.Skip("chỉ chạy như tiến trình con của TestVerifyChildProcessTempDir")
	}
	if err := os.WriteFile(out, []byte(os.TempDir()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Kiểm tra mục tiêu: tiến trình Go con của lệnh kiểm chứng (qua sh) thấy os.TempDir() là thư mục riêng, dạng đường dẫn
// gốc của hệ điều hành, nằm dưới base của nền tảng, ngắn trên Windows (MAX_PATH), mỗi lần một thư mục, và được dọn.
func TestVerifyChildProcessTempDir(t *testing.T) {
	h := newHarness(t)
	out := filepath.Join(t.TempDir(), "childtmp.txt")
	cmd := `test -f done.txt && BINT_HELPER_TMPOUT="` + filepath.ToSlash(out) + `" "` + filepath.ToSlash(os.Args[0]) +
		`" -test.run='^TestHelperPrintTempDir$'`
	h.cfg.Eval.TestCommand = &cmd
	id := h.finishFeat()
	base := verifyTempBase(h.tasks.Dir)
	read := func(who string) string {
		t.Helper()
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("%s: tiến trình con không ghi os.TempDir(): %v (%+v)", who, err, h.load(id).State.LastVerification())
		}
		os.Remove(out)
		dir := string(b)
		if !filepath.IsAbs(dir) || strings.HasPrefix(dir, "/c/") || strings.HasPrefix(dir, "/C/") {
			t.Fatalf("%s: os.TempDir()=%q không phải đường dẫn gốc của hệ điều hành", who, dir)
		}
		if filepath.Dir(filepath.Clean(dir)) != base {
			t.Fatalf("%s: os.TempDir()=%q, muốn thư mục con của %s", who, dir, base)
		}
		if runtime.GOOS == "windows" {
			if max := len(os.TempDir()) + 16; len(dir) > max {
				t.Fatalf("%s: thư mục tạm %q dài %d > %d ký tự", who, dir, len(dir), max)
			}
		} else if !strings.HasPrefix(filepath.Base(dir), "verify-") {
			t.Fatalf("%s: tên %q muốn verify-*", who, dir)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s: thư mục tạm %s phải được dọn (%v)", who, dir, err)
		}
		return dir
	}
	first := read("Runner")
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("verify: %+v %v", vr, err)
	}
	if second := read("bint task verify"); second == first {
		t.Fatalf("mỗi lần kiểm chứng cần thư mục tạm mới: %q", first)
	}
}

// Không tạo được thư mục tạm riêng thì lần kiểm chứng vẫn chạy (TMPDIR kế thừa), không hỏng.
func TestVerifyTempDirFailureDoesNotBreakVerify(t *testing.T) {
	h := newHarness(t)
	id := h.finishFeat()
	// Chiếm chỗ thư mục cha bằng một tệp: MkdirAll lỗi. Windows: base nằm trong os.TempDir() nên trỏ nó vào thư mục riêng.
	t.Setenv("TMP", t.TempDir())
	t.Setenv("TEMP", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	base := verifyTempBase(h.tasks.Dir)
	os.RemoveAll(base)
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, vr, err := h.runner().Verify(context.Background(), id); err != nil || !vr.Verification.Passed {
		t.Fatalf("lỗi tạo thư mục tạm không được làm hỏng kiểm chứng: %+v %v", vr, err)
	}
}

// Điểm vướng 2: b-feat ghi lại tasks.md làm mất `— xong khi: …` (dạng bản hỏng của B-mid: giữ TSK-ID/AC, đánh dấu [x]).
// Runner phục hồi các dòng TSK của bản trước bước, giữ đánh dấu, ghi chú; tác vụ không bị chặn vì hợp đồng.
func TestFeatRewritingTasksKeepsPlanningContract(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			if _, _, err := e.WriteArtifact(id, "tasks", "- [x] TSK-1 [AC-1]: sửa app.txt thành v2; test qua.\n"); err != nil {
				return err
			}
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != task.StatusDone {
		t.Fatalf("không được chặn vì hợp đồng artifact: %+v", res)
	}
	got := h.load(res.TaskID)
	b, _ := os.ReadFile(got.ArtifactPath("tasks"))
	if string(b) != "- [x] TSK-1 [AC-1]: sửa app.txt — xong khi: app.txt ghi v2\n" {
		t.Fatalf("tasks.md sau phục hồi: %q", b)
	}
	if rep := task.LintPlanningTask(got, true); rep.Blocking() {
		t.Fatalf("tasks.md phải đạt hợp đồng: %v", rep.Errors)
	}
	if !slices.ContainsFunc(res.Notes, func(n string) bool { return strings.Contains(n, "tasks.md không đạt hợp đồng v1") }) {
		t.Fatalf("thiếu ghi chú phục hồi: %v", res.Notes)
	}
}

// Agent thêm TSK mới khi ghi lại (không ghép được theo TSK-ID): phục hồi nguyên bản trước bước.
func TestFeatRewritingTasksRestoresWholeWhenIDsDiffer(t *testing.T) {
	h := newHarness(t)
	base := h.happy
	var before string
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepFeat {
			tk, _ := e.Tasks.Load(id)
			b, _ := os.ReadFile(tk.ArtifactPath("tasks"))
			before = string(b)
			if _, _, err := e.WriteArtifact(id, "tasks", "- [x] TSK-1 [AC-1]: sửa app.txt\n- [x] TSK-2 [AC-1]: thêm test\n"); err != nil {
				return err
			}
		}
		return base(ctx, step, e, id)
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ := os.ReadFile(h.load(res.TaskID).ArtifactPath("tasks"))
	if before == "" || string(b) != before {
		t.Fatalf("phải phục hồi nguyên bản trước bước: %q, muốn %q", b, before)
	}
}

// Resume sau khi finalize bị chặn vì kiểm chứng hỏng, nhưng người đã chạy lại kiểm chứng và qua đầy đủ trên cùng cây:
// không chạy lại lệnh kiểm chứng lần nữa.
func TestFinalizeResumeSkipsReverifyWhenLatestPassed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, flag := h.flagCommand()
	id := h.finishFeat()
	setFlag(t, flag, true)
	h.runner().VerifyWith(ctx, id, "sự cố ngoài mã")
	if res := h.resumeDo(id); res.Status != task.StatusBlocked || h.load(id).State.FinalizeFP == "" {
		t.Fatalf("phải chặn ở finalize: %+v", res)
	}
	setFlag(t, flag, false)
	if _, vr, err := h.runner().Verify(ctx, id); err != nil || !vr.Verification.Passed {
		t.Fatalf("%+v %v", vr, err)
	}
	nv := len(h.load(id).State.Verifications)
	res := h.resumeDo(id)
	if res.Status != task.StatusDone || slices.Contains(stepNames(res), StepReview) {
		t.Fatalf("%+v steps=%v", res, stepNames(res))
	}
	if n := len(h.load(id).State.Verifications); n != nv {
		t.Fatalf("không được kiểm chứng lại: %d → %d", nv, n)
	}
}
