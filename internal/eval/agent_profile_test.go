//go:build !windows

package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/iso"
	"bint/internal/testutil"
)

// profileRig: store tạm + profile + harness giả `<tool>` ghi env/argv vào $OUT.
type profileRig struct {
	P     iso.Profile
	Out   string
	Bin   string
	State string
	Cfg   string
	Home  string
}

const fakeHarnessScript = `#!/bin/sh
{
echo "HOME=$HOME"
echo "TMPDIR=$TMPDIR"
echo "CODEX_HOME=$CODEX_HOME"
echo "CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR"
echo "BINT_STATE_DIR=$BINT_STATE_DIR"
echo "BINT_CONFIG_HOME=$BINT_CONFIG_HOME"
echo "BINT_ISO_PROFILE=$BINT_ISO_PROFILE"
echo "BINT_ISO_REAL_HOME=$BINT_ISO_REAL_HOME"
[ -d "$TMPDIR" ] && echo TMP_EXISTS
} > "$OUT/env.txt"
: > "$OUT/argv.txt"
for a in "$@"; do printf '%s\n' "$a" >> "$OUT/argv.txt"; done
cat > /dev/null
echo '{"type":"result","is_error":false,"result":"ok","num_turns":1,"total_cost_usd":0.01,"session_id":"s1"}'
`

func newProfileRig(t *testing.T, tool iso.Tool, mutIso func(*config.Iso)) profileRig {
	t.Helper()
	state := testutil.Isolate(t)
	t.Setenv("TMPDIR", t.TempDir())
	cfg := config.Default().Iso
	cfg.GCInterval = "0s"
	if mutIso != nil {
		mutIso(&cfg)
	}
	st, err := iso.OpenStore(cfg, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	p, err := st.Create(tool, "work")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, string(tool)), []byte(fakeHarnessScript), 0o755); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	return profileRig{P: p, Out: out, Bin: filepath.Join(bin, string(tool)), State: state, Cfg: os.Getenv("BINT_CONFIG_HOME"), Home: os.Getenv("HOME")}
}

func (r profileRig) env(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Out, "env.txt"))
	if err != nil {
		t.Fatalf("harness giả không chạy: %v", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = v
		} else if l != "" {
			m[l] = "1"
		}
	}
	return m
}

func (r profileRig) argv(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Out, "argv.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func (r profileRig) req() AgentRequest {
	return AgentRequest{Dir: r.Out, Prompt: "làm", Timeout: 30 * time.Second, Env: []string{"OUT=" + r.Out}}
}

// Claude có profile: env do iso.Env dựng (HOME/CLAUDE_CONFIG_DIR của profile, state thật), TMPDIR có sẵn lúc chạy và bị dọn sau.
func TestClaudeAgentRunsInProfileEnv(t *testing.T) {
	r := newProfileRig(t, iso.Claude, nil)
	a := &ClaudeAgent{Binary: r.Bin, Profile: &r.P}
	a.Cfg.PermissionMode = "acceptEdits"
	res := a.Run(context.Background(), r.req())
	if res.Error != "" || res.ExitCode != 0 {
		t.Fatalf("res=%+v", res)
	}
	m := r.env(t)
	if m["HOME"] != r.P.Home || m["CLAUDE_CONFIG_DIR"] != r.P.ConfigDir() {
		t.Fatalf("HOME=%q CLAUDE_CONFIG_DIR=%q, muốn của profile %q %q", m["HOME"], m["CLAUDE_CONFIG_DIR"], r.P.Home, r.P.ConfigDir())
	}
	if m["BINT_STATE_DIR"] != r.State || m["BINT_CONFIG_HOME"] != r.Cfg || m["BINT_ISO_PROFILE"] != "claude/work" {
		t.Fatalf("state/config/profile sai: %v", m)
	}
	if m["TMP_EXISTS"] != "1" || m["TMPDIR"] == "" {
		t.Fatalf("TMPDIR phải tồn tại trước khi spawn: %v", m)
	}
	if _, err := os.Stat(m["TMPDIR"]); !os.IsNotExist(err) {
		t.Fatalf("temp phải bị dọn sau lượt chạy: %v", err)
	}
	if pa := EffectiveProfile(a, AgentRequest{}); pa == nil || pa.ID() != "claude/work" {
		t.Fatalf("EffectiveProfile = %v", pa)
	}
}

// Không cấu hình profile: env là env thật của tiến trình (không đổi so với trước).
func TestAgentWithoutProfileKeepsRealEnv(t *testing.T) {
	r := newProfileRig(t, iso.Claude, nil)
	a := &ClaudeAgent{Binary: r.Bin}
	res := a.Run(context.Background(), r.req())
	if res.Error != "" {
		t.Fatalf("res=%+v", res)
	}
	m := r.env(t)
	if m["HOME"] != r.Home || m["CLAUDE_CONFIG_DIR"] != "" || m["BINT_ISO_PROFILE"] != "" {
		t.Fatalf("env phải là bản thật: %v", m)
	}
	if EffectiveProfile(a, AgentRequest{}) != nil {
		t.Fatal("không có profile thì EffectiveProfile phải nil")
	}
}

// AgentRequest.Profile ghi đè profile mặc định của agent.
func TestAgentRequestProfileOverridesAgent(t *testing.T) {
	r := newProfileRig(t, iso.Claude, nil)
	a := &ClaudeAgent{Binary: r.Bin}
	req := r.req()
	req.Profile = &r.P
	if res := a.Run(context.Background(), req); res.Error != "" {
		t.Fatalf("res=%+v", res)
	}
	if m := r.env(t); m["HOME"] != r.P.Home {
		t.Fatalf("HOME=%q, muốn %q", m["HOME"], r.P.Home)
	}
	if p := EffectiveProfile(a, req); p == nil || p.ID() != "claude/work" {
		t.Fatalf("EffectiveProfile = %v", p)
	}
}

// Tiến trình bint đang ở trong profile mà agent không có profile: env của bước là env thật, không lan profile của phiên chủ.
func TestAgentInsideProfileSessionGetsRealEnv(t *testing.T) {
	r := newProfileRig(t, iso.Claude, nil)
	penv, err := iso.Env(r.P, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range penv {
		if k, v, ok := strings.Cut(kv, "="); ok {
			t.Setenv(k, v)
		}
	}
	if os.Getenv("HOME") != r.P.Home || os.Getenv("BINT_ISO_PROFILE") == "" {
		t.Fatal("giả lập phiên trong profile không thành")
	}
	a := &ClaudeAgent{Binary: r.Bin}
	if res := a.Run(context.Background(), r.req()); res.Error != "" {
		t.Fatalf("res=%+v", res)
	}
	m := r.env(t)
	if m["HOME"] != r.Home || m["BINT_ISO_PROFILE"] != "" || m["BINT_ISO_REAL_HOME"] != "" || m["CLAUDE_CONFIG_DIR"] != "" {
		t.Fatalf("env bước phải là iso.RealEnv: %v", m)
	}
	if m["BINT_STATE_DIR"] != r.State {
		t.Fatalf("BINT_STATE_DIR=%q, muốn %q", m["BINT_STATE_DIR"], r.State)
	}
}

func codexProfileRequest(t *testing.T, r profileRig) AgentRequest {
	t.Helper()
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(mcp, []byte(`{"mcpServers":{"bint":{"command":"/x/bint","args":["mcp","serve"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	req := r.req()
	req.MCPConfig = mcp
	req.Settings = "settings.json"
	return req
}

func mcpEnvArg(args []string) string {
	for i, a := range args {
		if i > 0 && args[i-1] == "-c" && strings.HasPrefix(a, "mcp_servers.bint.env=") {
			return a
		}
	}
	return ""
}

// Codex có profile: -c mcp_servers.bint.env mang BINT_STATE_DIR/BINT_CONFIG_HOME thật tuyệt đối ngay sau mcp_servers.bint.args;
// không profile thì đối số không đổi.
func TestCodexProfileAddsMCPEnvArg(t *testing.T) {
	r := newProfileRig(t, iso.Codex, nil)
	req := codexProfileRequest(t, r)
	a := &CodexAgent{Binary: r.Bin, Profile: &r.P}
	args, err := a.ArgsFor(req)
	if err != nil {
		t.Fatal(err)
	}
	want := "mcp_servers.bint.env={BINT_CONFIG_HOME=" + tomlString(r.Cfg) + ",BINT_STATE_DIR=" + tomlString(r.State) + "}"
	if got := mcpEnvArg(args); got != want {
		t.Fatalf("mcp env = %q, muốn %q", got, want)
	}
	i := slices.IndexFunc(args, func(s string) bool { return strings.HasPrefix(s, "mcp_servers.bint.args=") })
	if i < 0 || !strings.HasPrefix(args[i+2], "mcp_servers.bint.env=") {
		t.Fatalf("mcp_servers.bint.env phải đứng ngay sau mcp_servers.bint.args: %v", args)
	}
	if !filepath.IsAbs(r.State) || !filepath.IsAbs(r.Cfg) {
		t.Fatal("giá trị phải tuyệt đối")
	}
	plain, err := (&CodexAgent{Binary: r.Bin}).ArgsFor(req)
	if err != nil || mcpEnvArg(plain) != "" {
		t.Fatalf("không profile thì không có mcp env: %v %v", plain, err)
	}
	// AgentRequest.Profile cũng bật.
	req.Profile = &r.P
	viaReq, _ := (&CodexAgent{Binary: r.Bin}).ArgsFor(req)
	if mcpEnvArg(viaReq) != want {
		t.Fatalf("req.Profile phải bật mcp env: %v", viaReq)
	}
}

// Codex chạy thật (script giả) trong profile: env của profile và -c mcp env tới được tiến trình.
func TestCodexAgentRunInProfile(t *testing.T) {
	r := newProfileRig(t, iso.Codex, nil)
	a := &CodexAgent{Binary: r.Bin, Profile: &r.P}
	res := a.Run(context.Background(), codexProfileRequest(t, r))
	_ = res // script giả không phát luồng codex: chỉ kiểm env/argv
	m := r.env(t)
	if m["HOME"] != r.P.Home || m["CODEX_HOME"] != r.P.ConfigDir() || m["BINT_STATE_DIR"] != r.State || m["BINT_CONFIG_HOME"] != r.Cfg {
		t.Fatalf("env profile sai: %v", m)
	}
	if mcpEnvArg(r.argv(t)) == "" {
		t.Fatalf("thiếu -c mcp_servers.bint.env: %v", r.argv(t))
	}
}

func lbHealthServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(status)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Profile linked: bộ cờ iso.CodexLBArgs đứng ở cấp exec, trước ExtraArgs và trước resume; LB không sẵn sàng thì lượt dừng
// ở lớp hạ tầng (external) và không spawn harness.
func TestCodexProfileLinkedLBArgsBeforeResume(t *testing.T) {
	r := newProfileRig(t, iso.Codex, nil)
	t.Setenv(iso.EnvCodexLBAPIKey, "")
	srv := lbHealthServer(t, 200)
	r.P.Meta.LB = iso.LB{Mode: iso.LBLinked, URL: srv.URL}
	a := &CodexAgent{Binary: r.Bin, Profile: &r.P, ExtraArgs: []string{"--extra-flag"}}
	req := codexProfileRequest(t, r)
	req.ResumeSessionID = "thread-1"
	a.Run(context.Background(), req)
	args := r.argv(t)
	lb := iso.CodexLBArgs(srv.URL, false)
	i := slices.Index(args, lb[1]) - 1
	if i < 0 || !slices.Equal(args[i:i+len(lb)], lb) {
		t.Fatalf("thiếu bộ cờ LB: %v", args)
	}
	resume, extra := slices.Index(args, "resume"), slices.Index(args, "--extra-flag")
	if resume < 0 || i+len(lb) > extra || extra > resume {
		t.Fatalf("thứ tự phải là LB < ExtraArgs < resume: lb@%d extra@%d resume@%d %v", i, extra, resume, args)
	}
	if args[resume+1] != "thread-1" {
		t.Fatalf("resume sai: %v", args)
	}

	// LB chết: lỗi hạ tầng, harness không được chạy.
	_ = os.Remove(filepath.Join(r.Out, "env.txt"))
	bad := lbHealthServer(t, 503)
	r.P.Meta.LB.URL = bad.URL
	res := (&CodexAgent{Binary: r.Bin, Profile: &r.P}).Run(context.Background(), codexProfileRequest(t, r))
	if res.Infra == "" || res.ExitCode == 0 || !strings.Contains(res.Error, "codex/work") {
		t.Fatalf("muốn Infra + lỗi nêu profile, nhận %+v", res)
	}
	if _, err := os.Stat(filepath.Join(r.Out, "env.txt")); err == nil {
		t.Fatal("LB chết thì không được spawn harness")
	}
}

// codexEvalProfile: profile linked không có auth.json thì không đòi auth; không linked vẫn báo lỗi. Home của user skill lấy từ HOME của base.
func TestCodexEvalProfileAuthOptionalForLinked(t *testing.T) {
	testutil.Isolate(t)
	codexHome := t.TempDir()
	base := append(os.Environ(), "CODEX_HOME="+codexHome)
	if _, _, err := codexEvalProfile(AgentRequest{Dir: t.TempDir()}, base, false); err == nil {
		t.Fatal("thiếu auth.json phải lỗi khi không linked")
	}
	env, cleanup, err := codexEvalProfile(AgentRequest{Dir: t.TempDir()}, base, true)
	if err != nil {
		t.Fatalf("linked không đòi auth.json: %v", err)
	}
	defer cleanup()
	dir := envValue(env, "CODEX_HOME")
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("không có auth.json nguồn thì không tạo: %v", err)
	}
}

// codexOtherMCPServersIn đọc config.toml của CODEX_HOME trong env đã dựng (profile), không phải của tiến trình.
func TestCodexOtherMCPServersReadsBuiltEnv(t *testing.T) {
	testutil.Isolate(t)
	procHome, profHome := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(procHome, "config.toml"), []byte("[mcp_servers.real]\ncommand=\"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profHome, "config.toml"), []byte("[mcp_servers.inprofile]\ncommand=\"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", procHome)
	got, err := codexOtherMCPServersIn(AgentRequest{Dir: t.TempDir()}, []string{"CODEX_HOME=" + profHome})
	if err != nil || !slices.Equal(got, []string{"inprofile"}) {
		t.Fatalf("got=%v err=%v, muốn [inprofile]", got, err)
	}
}
