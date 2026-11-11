package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"bint/internal/tui"
)

// agentVars: biến báo phiên agent (go test có thể đang chạy trong phiên agent: đóng vai người phải xoá hết).
var agentVars = []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "GEMINI_CLI"}

func noAgentEnv(t *testing.T) {
	t.Helper()
	for _, k := range agentVars {
		t.Setenv(k, "")
	}
}

// stubTUI thay ba điều kiện mở TUI; trả con trỏ tới tuỳ chọn TUI nhận được (nil nếu không mở).
func stubTUI(t *testing.T, tty bool) **tui.Options {
	t.Helper()
	var got *tui.Options
	oldTTY, oldRun := stdioIsTTY, runTUI
	stdioIsTTY = func(IO) bool { return tty }
	runTUI = func(_ context.Context, o tui.Options) error { got = &o; return nil }
	t.Cleanup(func() { stdioIsTTY, runTUI = oldTTY, oldRun })
	return &got
}

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Execute(context.Background(), args, IO{In: strings.NewReader(""), Out: &out, Err: &errb})
	return out.String(), errb.String(), code
}

// `bint` không TTY (đường ống, CI) in đúng đầu ra của `bint --help`, mã 0, không mở TUI.
func TestBareBintWithoutTTYPrintsHelp(t *testing.T) {
	noAgentEnv(t)
	got := stubTUI(t, false)
	bare, _, code := runCLI(t)
	help, _, hcode := runCLI(t, "--help")
	if code != 0 || hcode != 0 || bare != help || !strings.Contains(bare, "Dự án:") {
		t.Fatalf("bint trống phải in trợ giúp như --help (mã %d/%d):\n%s\n---\n%s", code, hcode, bare, help)
	}
	if *got != nil {
		t.Fatal("không TTY thì không được mở TUI")
	}
}

// Trong phiên agent, `bint` trống cũng in trợ giúp dù có TTY (agent không điều khiển được TUI).
func TestBareBintInAgentSessionPrintsHelp(t *testing.T) {
	noAgentEnv(t)
	t.Setenv("CLAUDECODE", "1")
	got := stubTUI(t, true)
	bare, _, code := runCLI(t)
	help, _, _ := runCLI(t, "--help")
	if code != 0 || bare != help {
		t.Fatalf("mã %d:\n%s", code, bare)
	}
	if *got != nil {
		t.Fatal("trong phiên agent không được mở TUI")
	}
}

// TTY giả, ngoài phiên agent: `bint` trống gọi TUI với thư mục hiện tại.
func TestBareBintWithTTYOpensTUI(t *testing.T) {
	noAgentEnv(t)
	got := stubTUI(t, true)
	out, _, code := runCLI(t)
	if code != 0 || out != "" {
		t.Fatalf("mã %d, đầu ra %q", code, out)
	}
	o := *got
	if o == nil || o.Dir == "" || o.Version != Version || o.Agent || o.ReadOnly || o.Gallery || o.Lang != "" {
		t.Fatalf("tuỳ chọn TUI: %+v", o)
	}
}

func TestBareBintTUIErrorExitsNonZero(t *testing.T) {
	noAgentEnv(t)
	stubTUI(t, true)
	runTUI = func(context.Context, tui.Options) error { return errors.New("hỏng") }
	_, errOut, code := runCLI(t)
	if code != ExitError || !strings.Contains(errOut, "lỗi: hỏng") {
		t.Fatalf("mã %d, stderr %q", code, errOut)
	}
}

// Có đối số thì không bao giờ mở TUI (kể cả khi có TTY).
func TestArgsNeverOpenTUI(t *testing.T) {
	noAgentEnv(t)
	got := stubTUI(t, true)
	if _, _, code := runCLI(t, "--help"); code != 0 {
		t.Fatal(code)
	}
	if _, _, code := runCLI(t, "doctor", "--json"); code == ExitUsage {
		t.Fatal("doctor hợp lệ")
	}
	if *got != nil {
		t.Fatal("lệnh có đối số không được mở TUI")
	}
}

func TestTUICommandExplicit(t *testing.T) {
	noAgentEnv(t)
	got := stubTUI(t, true)
	if _, _, code := runCLI(t, "tui", "--lang", "en", "--read-only"); code != 0 {
		t.Fatalf("mã %d", code)
	}
	if o := *got; o == nil || o.Lang != "en" || !o.ReadOnly || o.Gallery {
		t.Fatalf("%+v", o)
	}
	*got = nil
	if _, _, code := runCLI(t, "tui", "--gallery"); code != 0 || *got == nil || !(*got).Gallery {
		t.Fatalf("--gallery: mã %d, %+v", code, *got)
	}
	// Trong phiên agent vẫn mở được (người gõ tường minh) và báo Agent để khoá thao tác chỉ-dành-cho-người.
	t.Setenv("CLAUDECODE", "1")
	*got = nil
	if _, _, code := runCLI(t, "tui"); code != 0 || *got == nil || !(*got).Agent {
		t.Fatalf("trong phiên agent: mã %d, %+v", code, *got)
	}
}

func TestTUICommandRejectsBadInput(t *testing.T) {
	noAgentEnv(t)
	got := stubTUI(t, true)
	if _, errOut, code := runCLI(t, "tui", "--lang", "fr"); code != ExitUsage || !strings.Contains(errOut, "--lang") {
		t.Fatalf("--lang fr: mã %d %q", code, errOut)
	}
	if *got != nil {
		t.Fatal("lỗi dùng sai không được mở TUI")
	}
	stubTUI(t, false)
	if _, errOut, code := runCLI(t, "tui"); code != ExitError || !strings.Contains(errOut, "terminal") {
		t.Fatalf("không TTY: mã %d %q", code, errOut)
	}
}

// Cờ --gallery ẩn khỏi trợ giúp; lệnh tui hiện trong nhóm Dự án.
func TestTUICommandHelp(t *testing.T) {
	out, _, _ := runCLI(t, "tui", "--help")
	if strings.Contains(out, "--gallery") || !strings.Contains(out, "--lang") || !strings.Contains(out, "--read-only") {
		t.Fatalf("%s", out)
	}
	root, _, _ := runCLI(t, "--help")
	if !strings.Contains(root, "tui") {
		t.Fatalf("bint --help phải liệt kê tui:\n%s", root)
	}
}

// isTTY không nhận /dev/null (thiết bị ký tự nhưng không phải terminal) và không nhận bộ đệm.
func TestIsTTYRejectsNonTerminals(t *testing.T) {
	if isTTY(&bytes.Buffer{}) || isTTY(strings.NewReader("")) || isTTY(nil) {
		t.Fatal("bộ đệm không phải TTY")
	}
	devnull, err := openDevNull()
	if err != nil {
		t.Skip(err)
	}
	defer devnull.Close()
	if isTTY(devnull) {
		t.Fatal("/dev/null không phải TTY")
	}
}
