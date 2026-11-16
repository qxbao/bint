//go:build !windows

package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Harness giả của bint iso: script sh đứng đầu PATH, ghi vào $OUT thứ nó thấy. Chạy bint thật (bintBin).

type isoEnv struct {
	Bin   string // thư mục chứa claude/codex giả
	Out   string // $OUT: nơi script ghi
	Store string // BINT_ISO_HOME
	Tmp   string // TMPDIR thật của kiểm thử
}

const fakeScript = `#!/bin/sh
n=$(ls "$OUT" 2>/dev/null | grep -c '^run\.' )
f="$OUT/run.$(basename "$0").$$"
{
  echo "HOME=$HOME"
  echo "TMPDIR=$TMPDIR"
  echo "STATE=$BINT_STATE_DIR"
  echo "PROFILE=$BINT_ISO_PROFILE"
  echo "LBKEY=${CODEX_LB_API_KEY:-}"
  if [ -d "$TMPDIR" ]; then echo TMP_EXISTS; fi
  for a in "$@"; do echo "ARG:$a"; done
} > "$f"
%s
`

func newIsoEnv(t *testing.T, scripts map[string]string) isoEnv {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	e := isoEnv{Bin: t.TempDir(), Out: t.TempDir(), Tmp: t.TempDir()}
	t.Setenv("TMPDIR", e.Tmp)
	t.Setenv("OUT", e.Out)
	t.Setenv("CODEX_LB_API_KEY", "")
	t.Setenv("BINT_ISO_CODEX_LB_URL", "")
	e.Store = os.Getenv("BINT_ISO_HOME")
	for _, tool := range []string{"claude", "codex"} {
		body, ok := scripts[tool]
		if !ok {
			body = "exit 0"
		}
		if err := os.WriteFile(filepath.Join(e.Bin, tool), []byte(strings.Replace(fakeScript, "%s", body, 1)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", e.Bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return e
}

// runs trả nội dung các tệp run.<tool>.<pid> mà harness giả đã ghi.
func (e isoEnv) runs(t *testing.T, tool string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.Out, "run."+tool+".*"))
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

func (e isoEnv) profileDir(tool, name string) string {
	return filepath.Join(e.Store, "profiles", tool+"--"+name)
}

func (e isoEnv) meta(t *testing.T, tool, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.profileDir(tool, name), "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func lbOf(m map[string]any) (mode, url string) {
	lb, _ := m["lb"].(map[string]any)
	mode, _ = lb["mode"].(string)
	url, _ = lb["url"].(string)
	return
}

func lbServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			_, _ = w.Write([]byte("ok"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func argsOf(run string) []string {
	var out []string
	for _, l := range strings.Split(run, "\n") {
		if a, ok := strings.CutPrefix(l, "ARG:"); ok {
			out = append(out, a)
		}
	}
	return out
}

func TestIsoCreate(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()

	out, code := run(t, dir, "", "iso", "create", "claude")
	if code != 0 || !strings.Contains(out, "claude/default") {
		t.Fatalf("create: code %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.profileDir("claude", "default"), "home", ".claude")); err != nil {
		t.Fatalf("profile không đúng bố cục: %v", err)
	}
	if out, code := run(t, dir, "", "iso", "create", "claude"); code != 1 || !strings.Contains(out, "đã tồn tại") {
		t.Fatalf("trùng: code %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"iso", "create", "agy"},
		{"iso", "create", "codex", "--name", "a b"},
		{"iso", "create", "codex", "--name", ".."},
		{"iso", "create", "codex", "--lb", "http://127.0.0.1:1", "--no-lb"},
		{"iso", "create", "claude", "--no-lb"},
		{"iso", "create"},
	} {
		if out, code := run(t, dir, "", args...); code != 2 {
			t.Errorf("%v: code %d, muốn 2\n%s", args, code, out)
		}
	}

	if out, code := run(t, dir, "", "iso", "create", "codex", "--name", "d", "--no-lb"); code != 0 {
		t.Fatalf("codex --no-lb: code %d\n%s", code, out)
	}
	if mode, url := lbOf(e.meta(t, "codex", "d")); mode != "direct" || url != "" {
		t.Fatalf("lb = %q %q, muốn direct", mode, url)
	}
}

func TestIsoCreateCodexLinkedAndDeadLB(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	srv := lbServer(t)

	if out, code := run(t, dir, "", "iso", "create", "codex", "--name", "l", "--lb", srv.URL+"/backend-api/codex"); code != 0 {
		t.Fatalf("code %d\n%s", code, out)
	}
	if mode, url := lbOf(e.meta(t, "codex", "l")); mode != "linked" || url != srv.URL {
		t.Fatalf("lb = %q %q, muốn linked %s", mode, url, srv.URL)
	}
	// Biến môi trường tường minh cũng chốt linked.
	t.Setenv("BINT_ISO_CODEX_LB_URL", srv.URL)
	if _, code := run(t, dir, "", "iso", "create", "codex", "--name", "env"); code != 0 {
		t.Fatalf("env: code %d", code)
	}
	if mode, _ := lbOf(e.meta(t, "codex", "env")); mode != "linked" {
		t.Fatalf("mode = %q", mode)
	}
	t.Setenv("BINT_ISO_CODEX_LB_URL", "")

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	out, code := run(t, dir, "", "iso", "create", "codex", "--name", "x", "--lb", deadURL)
	if code != 69 {
		t.Fatalf("LB chết: code %d, muốn 69\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("codex", "x")); !os.IsNotExist(err) {
		t.Fatalf("LB tường minh hỏng không được để lại profile: %v", err)
	}
}

func TestIsoRunMissingProfileDoesNotCreate(t *testing.T) {
	e := newIsoEnv(t, nil)
	out, code := run(t, t.TempDir(), "", "iso", "run", "codex", "--name", "nope", "--", "exec")
	if code == 0 || !strings.Contains(out, "bint iso create codex --name nope") {
		t.Fatalf("code %d\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("codex", "nope")); !os.IsNotExist(err) {
		t.Fatalf("run không được tự tạo profile: %v", err)
	}
	if len(e.runs(t, "codex")) != 0 {
		t.Fatal("harness không được chạy")
	}
	if _, code := run(t, t.TempDir(), "", "iso", "run", "agy"); code != 2 {
		t.Fatalf("tool lạ: code %d", code)
	}
	if _, code := run(t, t.TempDir(), "", "iso", "run"); code != 2 {
		t.Fatalf("thiếu tool: code %d", code)
	}
}

func TestIsoRunEnvArgsAndExitCode(t *testing.T) {
	e := newIsoEnv(t, map[string]string{"claude": "exit 5"})
	dir := t.TempDir()
	if _, code := run(t, dir, "", "iso", "create", "claude", "--name", "w"); code != 0 {
		t.Fatal("create")
	}
	out, code := run(t, dir, "", "iso", "run", "claude", "--name", "w", "--", "-p", "a b", "--model=x", "--", "z")
	if code != 5 {
		t.Fatalf("code %d, muốn mã thoát 5 của con\n%s", code, out)
	}
	if strings.Contains(out, "lỗi:") || strings.TrimSpace(out) != "" {
		t.Fatalf("mã thoát của con không được in dòng lỗi:\n%s", out)
	}
	runs := e.runs(t, "claude")
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	r := runs[0]
	home := filepath.Join(e.profileDir("claude", "w"), "home")
	tmp := filepath.Join(e.Tmp, "bint-iso-"+itoa(os.Getuid()), "claude--w")
	for _, want := range []string{"HOME=" + home, "TMPDIR=" + tmp, "STATE=" + os.Getenv("BINT_STATE_DIR"), "PROFILE=claude/w", "TMP_EXISTS"} {
		if !strings.Contains(r, want+"\n") {
			t.Errorf("thiếu %q:\n%s", want, r)
		}
	}
	if got := strings.Join(argsOf(r), "|"); got != "-p|a b|--model=x|--|z" {
		t.Fatalf("đối số = %q", got)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("hết phiên thì temp phải biến mất: %v", err)
	}
	// Không có `--`: mọi thứ sau tool (không phải cờ của bint) là đối số.
	if _, code := run(t, dir, "", "iso", "run", "claude", "--name", "w", "doctor"); code != 5 {
		t.Fatal(code)
	}
	if got := argsOf(e.runs(t, "claude")[0]); len(got) == 0 {
		t.Fatal("không có lần chạy")
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestIsoRunCodexLB(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	srv := lbServer(t)
	if _, code := run(t, dir, "", "iso", "create", "codex", "--name", "l", "--lb", srv.URL); code != 0 {
		t.Fatal("create")
	}
	lastArgs := func() []string {
		t.Helper()
		runs := e.runs(t, "codex")
		if len(runs) == 0 {
			t.Fatal("harness không chạy")
		}
		got := argsOf(runs[len(runs)-1])
		for _, f := range mustGlob(t, filepath.Join(e.Out, "run.codex.*")) {
			os.Remove(f)
		}
		return got
	}

	if out, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--", "exec", "hi"); code != 0 {
		t.Fatalf("code %d\n%s", code, out)
	}
	args := lastArgs()
	want := []string{"-c", `model_provider="codex-lb"`, "-c", `model_providers.codex-lb.name="openai"`,
		"-c", `model_providers.codex-lb.base_url="` + srv.URL + `/backend-api/codex"`,
		"-c", `model_providers.codex-lb.wire_api="responses"`, "-c", `model_providers.codex-lb.supports_websockets=true`,
		"-c", `model_providers.codex-lb.requires_openai_auth=false`,
		"-c", `model_providers.codex-lb.http_headers={"x-openai-actor-authorization"="codex-lb"}`, "exec", "hi"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("đối số:\n%q\nmuốn\n%q", args, want)
	}

	// Có CODEX_LB_API_KEY: thêm env_key và chuyển tiếp biến.
	t.Setenv("CODEX_LB_API_KEY", "sekret")
	if _, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--", "exec"); code != 0 {
		t.Fatal(code)
	}
	runs := e.runs(t, "codex")
	if !strings.Contains(runs[0], "LBKEY=sekret\n") || !strings.Contains(runs[0], `ARG:model_providers.codex-lb.env_key="CODEX_LB_API_KEY"`) {
		t.Fatalf("thiếu env_key/khoá:\n%s", runs[0])
	}
	lastArgs()
	t.Setenv("CODEX_LB_API_KEY", "")

	// --no-lb chốt lại direct: không chèn cờ LB.
	if _, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--no-lb", "--", "exec"); code != 0 {
		t.Fatal(code)
	}
	if got := lastArgs(); strings.Join(got, "|") != "exec" {
		t.Fatalf("direct: %q", got)
	}
	if mode, _ := lbOf(e.meta(t, "codex", "l")); mode != "direct" {
		t.Fatalf("mode = %q, --no-lb phải chốt direct", mode)
	}
	// --lb URL chốt lại linked.
	if _, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--lb", srv.URL, "--", "exec"); code != 0 {
		t.Fatal(code)
	}
	lastArgs()
	if mode, url := lbOf(e.meta(t, "codex", "l")); mode != "linked" || url != srv.URL {
		t.Fatalf("lb = %q %q", mode, url)
	}

	// LB chết: dừng mã 69, không chạy harness.
	srv.Close()
	out, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--", "exec")
	if code != 69 {
		t.Fatalf("code %d, muốn 69\n%s", code, out)
	}
	if len(e.runs(t, "codex")) != 0 {
		t.Fatal("LB chết thì không được chạy harness")
	}
	if mode, _ := lbOf(e.meta(t, "codex", "l")); mode != "linked" {
		t.Fatal("LB chết không được tự đổi chế độ")
	}
	// --lb tường minh không sẵn sàng: cũng 69.
	if _, code := run(t, dir, "", "iso", "run", "codex", "--name", "l", "--lb", srv.URL, "--", "exec"); code != 69 {
		t.Fatalf("--lb chết: code %d", code)
	}
	if _, code := run(t, dir, "", "iso", "run", "claude", "--name", "l", "--no-lb"); code != 2 {
		t.Fatalf("--no-lb với claude: code %d, muốn 2", code)
	}
}

func mustGlob(t *testing.T, pat string) []string {
	t.Helper()
	m, err := filepath.Glob(pat)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestIsoLogin(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	srv := lbServer(t)
	for _, a := range [][]string{
		{"iso", "create", "codex", "--name", "l", "--lb", srv.URL},
		{"iso", "create", "codex", "--name", "d", "--no-lb"},
		{"iso", "create", "claude", "--name", "c"},
	} {
		if out, code := run(t, dir, "", a...); code != 0 {
			t.Fatalf("%v: %d\n%s", a, code, out)
		}
	}
	// codex linked: không chạy harness.
	out, code := run(t, dir, "", "iso", "login", "codex", "--name", "l")
	if code != 0 || !strings.Contains(out, "linked") || len(e.runs(t, "codex")) != 0 {
		t.Fatalf("linked: code %d runs=%d\n%s", code, len(e.runs(t, "codex")), out)
	}
	// codex direct: codex login.
	if out, code := run(t, dir, "", "iso", "login", "codex", "--name", "d"); code != 0 {
		t.Fatalf("direct: %d\n%s", code, out)
	}
	if got := argsOf(e.runs(t, "codex")[0]); strings.Join(got, "|") != "login" {
		t.Fatalf("codex login: %q", got)
	}
	// claude: claude auth login (+ đối số thêm).
	if out, code := run(t, dir, "", "iso", "login", "claude", "--name", "c", "--", "--console"); code != 0 {
		t.Fatalf("claude: %d\n%s", code, out)
	}
	if got := argsOf(e.runs(t, "claude")[0]); strings.Join(got, "|") != "auth|login|--console" {
		t.Fatalf("claude auth login: %q", got)
	}
	if out, code := run(t, dir, "", "iso", "login", "claude", "--name", "none"); code == 0 || !strings.Contains(out, "bint iso create claude --name none") {
		t.Fatalf("thiếu profile: %d\n%s", code, out)
	}
}

func TestIsoStatus(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	if out, code := run(t, dir, "", "iso", "status"); code != 0 || !strings.Contains(out, "Chưa có profile") {
		t.Fatalf("rỗng: %d\n%s", code, out)
	}
	if out, code := run(t, dir, "", "iso", "status", "--json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("json rỗng: %d %q", code, out)
	}
	srv := lbServer(t)
	run(t, dir, "", "iso", "create", "codex", "--name", "l", "--lb", srv.URL)
	run(t, dir, "", "iso", "create", "claude", "--name", "c")
	if err := os.WriteFile(filepath.Join(e.profileDir("claude", "c"), "home", ".claude", ".credentials.json"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, dir, "", "iso", "status", "--json")
	if code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	var raw []map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&raw); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	byID := map[string]map[string]any{}
	for _, r := range raw {
		byID[r["id"].(string)] = r
	}
	if r := byID["codex/l"]; r == nil || r["lb_mode"] != "linked" || r["lb_url"] != srv.URL || r["auth"] != false || r["session"] != false {
		t.Fatalf("codex/l: %v", r)
	}
	if r := byID["claude/c"]; r == nil || r["auth"] != true || r["tool"] != "claude" || r["name"] != "c" {
		t.Fatalf("claude/c: %v", r)
	}
	out, code = run(t, dir, "", "iso", "status")
	if code != 0 || !strings.Contains(out, "codex/l") || !strings.Contains(out, "linked "+srv.URL) || !strings.Contains(out, "claude/c") {
		t.Fatalf("bảng: %d\n%s", code, out)
	}
}

func TestIsoRemove(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "r")
	canary := t.TempDir()
	if err := os.WriteFile(filepath.Join(canary, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(e.profileDir("claude", "r"), "home", ".cache")
	_ = os.Remove(link)
	if err := os.Symlink(canary, link); err != nil {
		t.Fatal(err)
	}

	asAgent(t)
	out, code := run(t, dir, "", "iso", "remove", "claude", "--name", "r", "--yes")
	if code == 0 || !strings.Contains(out, "chỉ dành cho người dùng") {
		t.Fatalf("agent: %d\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("claude", "r")); err != nil {
		t.Fatal("agent không được xoá profile")
	}
	asHuman(t)
	if out, code := run(t, dir, "", "iso", "remove", "claude", "--name", "r"); code != 2 || !strings.Contains(out, "--yes") {
		t.Fatalf("không tương tác cần --yes: %d\n%s", code, out)
	}
	if out, code := run(t, dir, "", "iso", "remove", "claude", "--name", "none", "--yes"); code == 0 || !strings.Contains(out, "bint iso create") {
		t.Fatalf("không có profile: %d\n%s", code, out)
	}
	if out, code := run(t, dir, "", "iso", "remove", "claude", "--name", "r", "--yes"); code != 0 {
		t.Fatalf("remove: %d\n%s", code, out)
	}
	if _, err := os.Stat(e.profileDir("claude", "r")); !os.IsNotExist(err) {
		t.Fatalf("profile phải bị xoá: %v", err)
	}
	if _, err := os.Stat(filepath.Join(canary, "keep")); err != nil {
		t.Fatalf("remove đã chạm dữ liệu đích của liên kết: %v", err)
	}
}

func TestIsoShellInit(t *testing.T) {
	e := newIsoEnv(t, nil)
	dir := t.TempDir()
	run(t, dir, "", "iso", "create", "claude", "--name", "w")
	run(t, dir, "", "iso", "create", "codex", "--name", "a.b", "--no-lb")
	before := treeOf(t, e.Store)
	out, code := run(t, dir, "", "iso", "shell-init", "zsh")
	if code != 0 || !strings.Contains(out, `claude-w() { command bint iso run claude --name w -- "$@"; }`) {
		t.Fatalf("%d\n%s", code, out)
	}
	if strings.Contains(out, "codex-a.b()") || !strings.Contains(out, "bỏ qua codex/a.b") {
		t.Fatalf("tên có dấu chấm phải bị bỏ qua:\n%s", out)
	}
	for _, sh := range []string{"bash", "fish", "pwsh"} {
		if out, code := run(t, dir, "", "iso", "shell-init", sh); code != 0 || !strings.Contains(out, "claude-w") {
			t.Fatalf("%s: %d\n%s", sh, code, out)
		}
	}
	if _, code := run(t, dir, "", "iso", "shell-init", "tcsh"); code != 2 {
		t.Fatalf("shell lạ: code %d", code)
	}
	if after := treeOf(t, e.Store); after != before {
		t.Fatalf("shell-init không được ghi tệp nào:\n%s\n---\n%s", before, after)
	}
}

func treeOf(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			fi, _ := os.Lstat(p)
			rel, _ := filepath.Rel(root, p)
			b.WriteString(rel + "|" + fi.Mode().String() + "\n")
		}
		return nil
	})
	return b.String()
}

// bgRun chạy bint nền; kết quả lấy bằng wait.
type bgRun struct {
	cmd *exec.Cmd
	out *bytes.Buffer
}

func startBint(t *testing.T, dir string, args ...string) *bgRun {
	t.Helper()
	cmd := exec.Command(bintBin, args...)
	cmd.Dir = dir
	out := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	b := &bgRun{cmd: cmd, out: out}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return b
}

func (b *bgRun) wait(t *testing.T) int {
	t.Helper()
	err := b.cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0
}
