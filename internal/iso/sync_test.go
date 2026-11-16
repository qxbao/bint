package iso

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestReconcileTable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		d, c, r string
		want    Action
	}{
		{"ghi mới", "D", "", "", ActWrite},
		{"khớp", "D", "D", "D", ActKeep},
		{"khớp không có receipt", "D", "D", "", ActKeep},
		{"mục riêng của profile", "D", "X", "", ActConflict},
		{"cập nhật", "D2", "D1", "D1", ActUpdate},
		{"sửa tay", "D2", "X", "D1", ActConflict},
		{"gỡ", "", "D1", "D1", ActRemove},
		{"giữ khi đã sửa tay", "", "X", "D1", ActConflict},
		{"cả hai vắng", "", "", "D1", ActForget},
		{"xoá tay thì ghi lại", "D", "", "D", ActWrite},
	} {
		got, _ := reconcile(tc.d, tc.c, tc.r)
		if got != tc.want {
			t.Errorf("%s: reconcile(%q,%q,%q) = %s, muốn %s", tc.name, tc.d, tc.c, tc.r, got, tc.want)
		}
	}
}

func TestHashValueIgnoresKeyOrder(t *testing.T) {
	a, _ := HashValue(map[string]any{"b": 1, "a": []any{"x", map[string]any{"z": 1, "y": 2}}})
	b, _ := HashValue(map[string]any{"a": []any{"x", map[string]any{"y": 2, "z": 1}}, "b": 1})
	if a != b || len(a) != 64 {
		t.Fatalf("băm phải ổn định khi thứ tự khoá khác: %q %q", a, b)
	}
}

const codexCfg = ".codex/config.toml"

func mcpEntry(name string, v map[string]any) Entry {
	return Entry{Kind: KindMCP, File: codexCfg, Section: []string{"mcp_servers"}, Name: name, Value: v}
}

func syncOpts(r fakeReal, check bool) SyncOptions { return SyncOptions{Check: check, Env: r.Env} }

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func findItem(rep SyncReport, key string) (SyncItem, bool) {
	for _, it := range rep.Items {
		if it.Key == key {
			return it, true
		}
	}
	return SyncItem{}, false
}

func TestSyncMCPLifecycleAndConflict(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	cfg := filepath.Join(p.Home, codexCfg)
	// Khoá lạ có sẵn ở profile phải còn nguyên sau khi ghi.
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("model = \"gpt-x\"\n\n[mcp_servers.own]\ncommand = \"own\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "a-cmd", "args": []any{"1"}})}}
	rep, err := Sync(p, src, syncOpts(r, false))
	if err != nil {
		t.Fatal(err)
	}
	if it, _ := findItem(rep, "mcp_servers.a"); it.Action != ActWrite {
		t.Fatalf("lần đầu phải ghi: %+v", rep.Items)
	}
	var tree map[string]any
	if err := toml.Unmarshal([]byte(readFile(t, cfg)), &tree); err != nil {
		t.Fatal(err)
	}
	if tree["model"] != "gpt-x" || tree["mcp_servers"].(map[string]any)["own"] == nil || tree["mcp_servers"].(map[string]any)["a"] == nil {
		t.Fatalf("khoá lạ/mục riêng phải còn nguyên: %v", tree)
	}
	// Receipt 0600 có sha256 của khoá quản lý và không chứa khoá riêng của profile.
	rec, err := LoadReceipt(p.ReceiptPath())
	if err != nil || len(rec.Entries) != 1 || rec.Entries[0].Key != "mcp_servers.a" || len(rec.Entries[0].SHA256) != 64 {
		t.Fatalf("receipt: %+v %v", rec, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p.ReceiptPath()); fi.Mode().Perm() != 0o600 {
			t.Fatalf("receipt quyền %v, muốn 0600", fi.Mode().Perm())
		}
	}

	// Chạy lại: khớp; --check thoát sạch.
	for _, check := range []bool{false, true} {
		rep, err = Sync(p, src, syncOpts(r, check))
		if err != nil || rep.Drift() {
			t.Fatalf("check=%v: phải khớp: %+v %v", check, rep.Items, err)
		}
	}

	// Bản thật đổi, profile chưa sửa tay → cập nhật.
	src2 := SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "a-cmd", "args": []any{"2"}})}}
	if rep, _ = Sync(p, src2, syncOpts(r, true)); !rep.Drift() {
		t.Fatalf("--check phải báo lệch: %+v", rep.Items)
	}
	if rep, _ = Sync(p, src2, syncOpts(r, false)); mustItem(t, rep, "mcp_servers.a").Action != ActUpdate {
		t.Fatalf("phải cập nhật: %+v", rep.Items)
	}

	// Sửa tay ở profile → xung đột, tệp giữ nguyên từng byte, receipt không đổi, --check lệch.
	hand := strings.Replace(readFile(t, cfg), "command = 'a-cmd'", "command = 'HAND'", 1)
	if !strings.Contains(hand, "HAND") {
		t.Fatalf("không sửa tay được: %s", readFile(t, cfg))
	}
	if err := os.WriteFile(cfg, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	recBefore := readFile(t, p.ReceiptPath())
	src3 := SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "a-cmd", "args": []any{"3"}})}}
	rep, err = Sync(p, src3, syncOpts(r, false))
	if err != nil {
		t.Fatal(err)
	}
	it := mustItem(t, rep, "mcp_servers.a")
	if it.Action != ActConflict || it.File != codexCfg || len(rep.Conflicts()) != 1 {
		t.Fatalf("phải xung đột nêu khoá và tệp: %+v", rep.Items)
	}
	if readFile(t, cfg) != hand {
		t.Fatalf("tệp profile phải giữ nguyên từng byte khi xung đột")
	}
	if readFile(t, p.ReceiptPath()) != recBefore {
		t.Fatal("receipt không đổi khi xung đột")
	}
	if rep, _ = Sync(p, src3, syncOpts(r, true)); !rep.Drift() {
		t.Fatal("--check phải lệch khi có xung đột")
	}

	// Bản thật bỏ khoá mà profile đã sửa tay → giữ, xung đột.
	if rep, _ = Sync(p, SyncSource{}, syncOpts(r, false)); mustItem(t, rep, "mcp_servers.a").Action != ActConflict {
		t.Fatalf("bỏ khoá đã sửa tay phải xung đột: %+v", rep.Items)
	}
	if readFile(t, cfg) != hand {
		t.Fatal("tệp phải giữ nguyên")
	}

	// Bỏ sửa tay (đưa về bản thật gần nhất) → bản thật bỏ khoá → gỡ; mục riêng còn.
	if _, err := Sync(p, src2, syncOpts(r, false)); err == nil { // vẫn đang sửa tay: xung đột, không lỗi
		_ = err
	}
	if err := os.WriteFile(cfg, []byte(strings.Replace(hand, "HAND", "a-cmd", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if rep, _ = Sync(p, src2, syncOpts(r, false)); rep.Drift() {
		t.Fatalf("sau khi bỏ sửa tay phải khớp: %+v", rep.Items)
	}
	if rep, _ = Sync(p, SyncSource{}, syncOpts(r, false)); mustItem(t, rep, "mcp_servers.a").Action != ActRemove {
		t.Fatalf("bản thật bỏ, chưa sửa tay → gỡ: %+v", rep.Items)
	}
	tree = nil
	if err := toml.Unmarshal([]byte(readFile(t, cfg)), &tree); err != nil {
		t.Fatal(err)
	}
	servers, _ := tree["mcp_servers"].(map[string]any)
	if servers["a"] != nil || servers["own"] == nil {
		t.Fatalf("chỉ gỡ khoá của sync: %v", tree)
	}
	if rec, _ = LoadReceipt(p.ReceiptPath()); len(rec.Entries) != 0 {
		t.Fatalf("receipt phải sạch: %+v", rec)
	}
}

func mustItem(t *testing.T, rep SyncReport, key string) SyncItem {
	t.Helper()
	it, ok := findItem(rep, key)
	if !ok {
		t.Fatalf("không có mục %s: %+v", key, rep.Items)
	}
	return it
}

func TestSyncOwnEntryIsConflictNotOverwritten(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	cfg := filepath.Join(p.Home, codexCfg)
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	own := "[mcp_servers.a]\ncommand = \"mine\"\n"
	_ = os.WriteFile(cfg, []byte(own), 0o600)
	rep, err := Sync(p, SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "real"})}}, syncOpts(r, false))
	if err != nil || mustItem(t, rep, "mcp_servers.a").Action != ActConflict {
		t.Fatalf("%+v %v", rep.Items, err)
	}
	if readFile(t, cfg) != own {
		t.Fatal("mục riêng của profile không được bị đè")
	}
	if _, err := os.Stat(p.ReceiptPath()); err == nil {
		t.Fatal("không có gì ghi thì không cần receipt")
	}
}

func TestSyncCheckWritesNothing(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	// Bản thật có skills để liên kết.
	if err := os.MkdirAll(filepath.Join(r.Home, ".codex", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, s.Root)
	src := SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "x"})}}
	rep, err := Sync(p, src, syncOpts(r, true))
	if err != nil || !rep.Drift() {
		t.Fatalf("phải lệch: %+v %v", rep.Items, err)
	}
	after := snapshotTree(t, s.Root)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("--check không được tạo/đổi gì:\nTRƯỚC %v\nSAU %v", before, after)
	}
	if _, err := os.Lstat(p.LockPath()); err == nil {
		// session.lock đã có từ Create thì thôi; không có thì --check không được tạo.
		t.Log("session.lock có sẵn")
	}
}

func TestSyncBusy(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	release, busy, err := p.Lock(false) // khoá S của một phiên đang chạy
	if err != nil || busy {
		t.Fatalf("%v %v", busy, err)
	}
	defer release()
	cfg := filepath.Join(p.Home, codexCfg)
	_, err = Sync(p, SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "x"})}}, syncOpts(r, false))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, muốn ErrBusy", err)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Fatal("không được ghi khi bận")
	}
	// --check vẫn chạy được khi có phiên.
	if _, err := Sync(p, SyncSource{}, syncOpts(r, true)); err != nil {
		t.Fatalf("--check không cần khoá: %v", err)
	}
}

func TestSyncLinks(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Claude, "w")
	for _, d := range []string{".claude/skills", ".claude/agents", ".agents/skills"} {
		if err := os.MkdirAll(filepath.Join(r.Home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// commands: bản thật không có → bỏ qua. agents: profile có thư mục riêng có dữ liệu → giữ, báo.
	own := filepath.Join(p.Home, ".claude", "agents")
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "mine.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// skills: thư mục rỗng → thay bằng liên kết.
	if err := os.MkdirAll(filepath.Join(p.Home, ".claude", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	rep, err := Sync(p, SyncSource{}, syncOpts(r, false))
	if err != nil {
		t.Fatal(err)
	}
	if it := mustItem(t, rep, filepath.Join(r.Home, ".claude", "skills")); it.Action != ActWrite {
		t.Fatalf("%+v", rep.Items)
	}
	if it := mustItem(t, rep, filepath.Join(r.Home, ".claude", "agents")); it.Action != ActSkip || !strings.Contains(it.Detail, "giữ nguyên") {
		t.Fatalf("%+v", rep.Items)
	}
	if _, ok := findItem(rep, filepath.Join(r.Home, ".claude", "commands")); ok {
		t.Fatalf("bản thật không có thì không có mục: %+v", rep.Items)
	}
	if _, err := os.Stat(filepath.Join(own, "mine.md")); err != nil {
		t.Fatal("dữ liệu riêng phải còn")
	}
	for _, l := range []string{".claude/skills", ".agents/skills"} {
		if isl, _ := IsLink(filepath.Join(p.Home, l)); !isl {
			t.Errorf("%s phải là liên kết", l)
		}
	}
	// Liên kết giữ nguyên không phải lệch; dữ liệu riêng cũng không (chỉ báo).
	if rep, _ = Sync(p, SyncSource{}, syncOpts(r, true)); rep.Drift() {
		t.Fatalf("đã khớp: %+v", rep.Items)
	}
}

func TestSyncClaudeJSONOnlyTouchesMCPServers(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Claude, "w")
	cj := filepath.Join(p.Home, ".claude", ".claude.json")
	_ = os.MkdirAll(filepath.Dir(cj), 0o700)
	orig := "{\n  \"oauthAccount\": {\"emailAddress\": \"mine@example.com\"},\n  \"numStartups\": 3,\n  \"mcpServers\": {\n    \"own\": {\"command\": \"own\"}\n  }\n}\n"
	_ = os.WriteFile(cj, []byte(orig), 0o600)
	e := Entry{Kind: KindMCP, File: ".claude/.claude.json", Section: []string{"mcpServers"}, Name: "bint", Value: map[string]any{"command": "bint", "args": []any{"mcp"}}}
	rep, err := Sync(p, SyncSource{Entries: []Entry{e}}, syncOpts(r, false))
	if err != nil || mustItem(t, rep, "mcpServers.bint").Action != ActWrite {
		t.Fatalf("%+v %v", rep.Items, err)
	}
	got := readFile(t, cj)
	for _, want := range []string{"mine@example.com", "numStartups", "\"own\"", "\"bint\""} {
		if !strings.Contains(got, want) {
			t.Errorf("thiếu %q:\n%s", want, got)
		}
	}
	// Không có tệp đăng nhập nào do sync tạo.
	for _, f := range []string{".claude/.credentials.json", ".codex/auth.json"} {
		if _, err := os.Stat(filepath.Join(p.Home, f)); err == nil {
			t.Errorf("sync không được tạo %s", f)
		}
	}
	// Gỡ khi bản thật bỏ.
	if rep, _ = Sync(p, SyncSource{}, syncOpts(r, false)); mustItem(t, rep, "mcpServers.bint").Action != ActRemove {
		t.Fatalf("%+v", rep.Items)
	}
	if got = readFile(t, cj); strings.Contains(got, "\"bint\"") || !strings.Contains(got, "\"own\"") || !strings.Contains(got, "mine@example.com") {
		t.Fatalf("chỉ gỡ khoá của sync:\n%s", got)
	}
}

func TestSyncNeverCopiesCredentials(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	for _, f := range []string{".codex/auth.json", ".claude/.credentials.json", ".claude.json"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(r.Home, f)), 0o755)
		_ = os.WriteFile(filepath.Join(r.Home, f), []byte(`{"oauthAccount":{"emailAddress":"real@example.com"},"token":"SECRET"}`), 0o600)
	}
	for _, tool := range []Tool{Codex, Claude} {
		p := mustCreate(t, s, tool, "w")
		src := SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "x"})}}
		if tool == Claude {
			src = SyncSource{Entries: []Entry{{Kind: KindMCP, File: ".claude/.claude.json", Section: []string{"mcpServers"}, Name: "a", Value: map[string]any{"command": "x"}}}}
		}
		if _, err := Sync(p, src, syncOpts(r, false)); err != nil {
			t.Fatal(err)
		}
		_ = filepath.WalkDir(p.Dir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(path); strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "real@example.com") {
					t.Errorf("%s chứa dữ liệu đăng nhập của bản thật", path)
				}
			}
			return nil
		})
		for _, f := range []string{".codex/auth.json", ".claude/.credentials.json"} {
			if _, err := os.Stat(filepath.Join(p.Home, f)); err == nil {
				t.Errorf("%s: sync không được tạo %s", p.ID(), f)
			}
		}
	}
}

func TestAdaptCodexMCP(t *testing.T) {
	env := map[string]string{"BINT_STATE_DIR": "/real/state", "BINT_CONFIG_HOME": "/real/cfg"}
	in := map[string]any{"command": "/usr/local/bin/bint", "args": []any{"mcp"}, "env": map[string]any{"BINT_STATE_DIR": "/mine", "X": "1"}}
	out := AdaptCodexMCP(in, env).(map[string]any)
	e := out["env"].(map[string]any)
	if e["BINT_STATE_DIR"] != "/mine" || e["BINT_CONFIG_HOME"] != "/real/cfg" || e["X"] != "1" {
		t.Fatalf("giữ giá trị sẵn có, thêm phần thiếu: %v", e)
	}
	if _, mutated := in["env"].(map[string]any)["BINT_CONFIG_HOME"]; mutated {
		t.Fatal("không được sửa bản gốc")
	}
	win := AdaptCodexMCP(map[string]any{"command": `C:\tools\bint.exe`}, env).(map[string]any)
	if win["env"].(map[string]any)["BINT_STATE_DIR"] != "/real/state" {
		t.Fatalf("bint.exe cũng nhận env: %v", win)
	}
	for _, other := range []any{
		map[string]any{"command": "uvx", "args": []any{"x"}},
		map[string]any{"url": "https://example.com/mcp", "command": "bint"},
	} {
		if m, _ := AdaptCodexMCP(other, env).(map[string]any); m["env"] != nil {
			t.Fatalf("không phải bint stdio thì không thêm env: %v", m)
		}
	}
}

func TestBintMCPEnvUsesRealAbsoluteValues(t *testing.T) {
	r := newFakeReal(t)
	got, err := BintMCPEnv(r.Env)
	if err != nil || got["BINT_STATE_DIR"] != r.State || got["BINT_CONFIG_HOME"] != r.Cfg {
		t.Fatalf("%v %v", got, err)
	}
	// Đang trong profile: vẫn trả giá trị thật.
	s := testStore(t, nil)
	p := mustCreate(t, s, Codex, "w")
	penv, err := Env(p, r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if got2, err := BintMCPEnv(penv); err != nil || got2["BINT_STATE_DIR"] != r.State {
		t.Fatalf("%v %v", got2, err)
	}
}

func TestSyncHookTarget(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	installed := ""
	tgt := HookTarget{
		File: ".codex/hooks.json",
		Get: func() (string, bool, error) {
			return installed, installed != "", nil
		},
		Install:   func(cmd string) error { installed = cmd; return nil },
		Uninstall: func() error { installed = ""; return nil },
	}
	he := func(cmd string) Entry {
		return Entry{Kind: KindHook, File: tgt.File, Section: []string{"hooks"}, Name: "bint", Value: cmd}
	}
	src := SyncSource{Hooks: []HookTarget{tgt}, Entries: []Entry{he(`"/x/bint" hook codex`)}}
	if rep, _ := Sync(p, src, syncOpts(r, true)); !rep.Drift() || installed != "" {
		t.Fatalf("--check không cài: %+v", rep.Items)
	}
	if rep, _ := Sync(p, src, syncOpts(r, false)); mustItem(t, rep, "hooks.bint").Action != ActWrite || installed != `"/x/bint" hook codex` {
		t.Fatalf("%+v %q", rep.Items, installed)
	}
	// Hook bị gỡ ở bản thật, profile chưa sửa tay → gỡ.
	src.Entries = nil
	if rep, _ := Sync(p, src, syncOpts(r, false)); mustItem(t, rep, "hooks.bint").Action != ActRemove || installed != "" {
		t.Fatalf("%+v %q", rep.Items, installed)
	}
}

func TestSyncLateHookState(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	late := func() ([]Entry, []string, error) {
		return []Entry{{Kind: KindHookState, File: codexCfg, Section: []string{"hooks", "state"}, Name: "/h/hooks.json:pre_tool_use:0:0",
			Value: map[string]any{"trusted_hash": "sha256:abc"}}}, []string{"ghi chú late"}, nil
	}
	src := SyncSource{Skip: []string{KindHookState}, Late: late}
	if rep, _ := Sync(p, src, syncOpts(r, true)); rep.Drift() || len(rep.Notes) != 0 {
		t.Fatalf("--check không chạy Late: %+v", rep)
	}
	rep, err := Sync(p, src, syncOpts(r, false))
	if err != nil || len(rep.Notes) != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if !strings.Contains(readFile(t, filepath.Join(p.Home, codexCfg)), "sha256:abc") {
		t.Fatal("trust hook user phải được ghi")
	}
	// Lần sau không Late (ví dụ --check/run): khoá hook-state không bị gỡ nhầm.
	if _, err := Sync(p, SyncSource{Skip: []string{KindHookState}}, syncOpts(r, false)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, filepath.Join(p.Home, codexCfg)), "sha256:abc") {
		t.Fatal("kind bị Skip phải giữ nguyên")
	}
}

func TestSyncCorruptReceiptIsEmptyNoOverwrite(t *testing.T) {
	s := testStore(t, nil)
	r := newFakeReal(t)
	p := mustCreate(t, s, Codex, "w")
	cfg := filepath.Join(p.Home, codexCfg)
	_ = os.MkdirAll(filepath.Dir(cfg), 0o700)
	own := "[mcp_servers.a]\ncommand = \"old\"\n"
	_ = os.WriteFile(cfg, []byte(own), 0o600)
	_ = os.WriteFile(p.ReceiptPath(), []byte("{hỏng"), 0o600)
	rep, err := Sync(p, SyncSource{Entries: []Entry{mcpEntry("a", map[string]any{"command": "new"})}}, SyncOptions{Env: r.Env, Now: time.Now()})
	if err != nil || mustItem(t, rep, "mcp_servers.a").Action != ActConflict || len(rep.Notes) == 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	if readFile(t, cfg) != own {
		t.Fatal("receipt hỏng không được dẫn tới ghi đè")
	}
}
