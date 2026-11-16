package iso

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// fakeAISource dựng store ai-isolator giả: <root>/profiles/<tool>--<tên>/{profile.env,codex-lb.*,home/…}.
type fakeAISource struct{ Root string }

func newFakeAISource(t *testing.T) fakeAISource {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	return fakeAISource{Root: root}
}

// profile ghi các tệp (đường dẫn tương đối theo thư mục profile ai-isolator, dấu '/') và profile.env đúng.
func (s fakeAISource) profile(t *testing.T, tool, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(s.Root, "profiles", tool+"--"+name)
	all := map[string]string{"profile.env": "AI_ISOLATOR_TOOL=" + tool + "\nAI_ISOLATOR_PROFILE=" + name + "\nAI_ISOLATOR_CREATED_AT=2026-10-05T03:55:29Z\n"}
	for k, v := range files {
		all[k] = v
	}
	for rel, body := range all {
		writeFileMode(t, filepath.Join(dir, filepath.FromSlash(rel)), body, 0o644)
	}
	return dir
}

func writeFileMode(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// codexFiles là nội dung đủ nhóm của một profile Codex ai-isolator (cả nhóm được nhập lẫn nhóm bị bỏ).
func codexFiles() map[string]string {
	return map[string]string{
		"codex-lb.mode":                           "linked\n",
		"codex-lb.url":                            "http://127.0.0.1:2455\n",
		"shell-link.conf":                         "x",
		".bflow-profile-sync.json":                "{}",
		"cache/uv/pkg.whl":                        "cache",
		"tmp/session/x":                           "tmp",
		"data/claude/versions/1/bin":              "data",
		"state/s.json":                            "state",
		"config/c.json":                           "cfg",
		"home/.codex/auth.json":                   `{"tokens":"secret"}`,
		"home/.codex/config.toml":                 "model = \"gpt\"\n",
		"home/.codex/rules/default.rules":         "rule",
		"home/.codex/rules/sub/more.rules":        "rule2",
		"home/.codex/sessions/2026/01/r-1.jsonl":  "s1",
		"home/.codex/archived_sessions/r-2.jsonl": "s2",
		"home/.codex/session_index.jsonl":         "idx",
		"home/.codex/history.jsonl":               "hist",
		"home/.codex/packages/standalone/codex":   "bin",
		"home/.codex/.tmp/t":                      "t",
		"home/.codex/tmp/t":                       "t",
		"home/.codex/logs_2.sqlite":               "sqlite",
		"home/.codex/state_5.sqlite":              "sqlite",
		"home/.codex/installation_id":             "id",
		"home/.codex/config.toml.bak":             "bak",
		"home/.gitconfig":                         "[user]\n",
		"home/Projects/app/main.go":               "user data",
		"home/.gemini/settings.json":              "gemini",
		"home/.docker/config.json":                "docker",
		"home/.claude.json":                       "{}",
		"home/.zshrc":                             "rc",
		"home/.local/share/claude/versions/1/c":   "bin",
	}
}

func claudeFiles() map[string]string {
	return map[string]string{
		"home/.claude/.credentials.json":      `{"claudeAiOauth":{}}`,
		"home/.claude/.claude.json":           `{"oauthAccount":{"x":1},"mcpServers":{}}`,
		"home/.claude/settings.json":          "{}",
		"home/.claude/CLAUDE.md":              "# md",
		"home/.claude/projects/p/s.jsonl":     "t1",
		"home/.claude/history.jsonl":          "h",
		"home/.claude/plugins/x/p.json":       "plugin",
		"home/.claude/cache/c":                "cache",
		"home/.claude/backups/b":              "bak",
		"home/.claude/file-history/f":         "fh",
		"home/.gitconfig":                     "[user]\n",
		"home/.local/share/claude/versions/2": "bin",
	}
}

// snapshot là sha256 của mọi tệp thường (và đích của symlink) dưới dir, khoá theo đường dẫn tương đối.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			out[rel] = "-> " + tgt
		case d.IsDir():
			out[rel+"/"] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			out[rel] = hex.EncodeToString(sum[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func filesUnder(m map[string]string) []string {
	var out []string
	for k := range m {
		if !strings.HasSuffix(k, "/") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func importOne1(t *testing.T, st *Store, src fakeAISource, o ImportOptions) ImportResult {
	t.Helper()
	o.Source = src.Root
	res, err := Import(st, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("muốn 1 kết quả, có %d: %+v", len(res), res)
	}
	return res[0]
}

func TestImportAllowlistCodex(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	srcDir := src.profile(t, "codex", "personal", codexFiles())
	// hooks.json là symlink tới tệp thật ngoài store: không được chép, không được đi theo.
	real := filepath.Join(t.TempDir(), "hooks.json")
	writeFileMode(t, real, `{"hooks":{}}`, 0o644)
	if err := os.Symlink(real, filepath.Join(srcDir, "home", ".codex", "hooks.json")); err != nil {
		t.Skipf("không tạo được symlink: %v", err)
	}
	res := importOne1(t, st, src, ImportOptions{})
	if res.Status != ImportImported || res.ID != "codex/personal" {
		t.Fatalf("kết quả: %+v", res)
	}
	got := filesUnder(snapshot(t, res.Profile.Home))
	want := []string{
		".codex/auth.json", ".codex/config.toml", ".codex/rules/default.rules", ".codex/rules/sub/more.rules", ".gitconfig",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tệp đã chép:\n%v\nmuốn:\n%v", got, want)
	}
	if !res.HasCreds {
		t.Error("HasCreds phải true khi có auth.json")
	}
	// Nội dung và quyền.
	b, _ := os.ReadFile(filepath.Join(res.Profile.Home, ".codex", "auth.json"))
	if string(b) != `{"tokens":"secret"}` {
		t.Errorf("auth.json: %q", b)
	}
	if runtime.GOOS != "windows" {
		for _, rel := range []string{".codex/auth.json", ".codex/config.toml", ".gitconfig"} {
			fi, err := os.Stat(filepath.Join(res.Profile.Home, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s: quyền %v, muốn 0600", rel, fi.Mode().Perm())
			}
		}
	}
	// Từng nhóm bị bỏ phải vắng ở đích và được báo kèm dung lượng.
	for _, rel := range []string{
		"cache", "tmp", "data", "state", "config", "Projects", ".gemini", ".docker", ".zshrc", ".claude.json", ".local/share/claude",
		".codex/packages", ".codex/.tmp", ".codex/tmp", ".codex/logs_2.sqlite", ".codex/state_5.sqlite", ".codex/hooks.json",
		".codex/installation_id", ".codex/config.toml.bak", ".codex/sessions", ".codex/archived_sessions",
		".codex/session_index.jsonl", ".codex/history.jsonl",
	} {
		if _, err := os.Lstat(filepath.Join(res.Profile.Home, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s không được có trong profile đã nhập", rel)
		}
	}
	for _, rel := range []string{"cache", "tmp", "data", "state", "config", "home/.local/share/claude/versions", "home/.codex/packages", "home/.codex/.tmp", "home/.codex/tmp", "home/.codex/*.sqlite*"} {
		var found *ImportSkip
		for i := range res.Skipped {
			if res.Skipped[i].Name == rel {
				found = &res.Skipped[i]
			}
		}
		if found == nil || found.Bytes == 0 {
			t.Errorf("nhóm bỏ %q phải được báo kèm dung lượng: %+v", rel, res.Skipped)
		}
	}
	var sym int
	for _, s := range res.Skipped {
		sym += s.Count
	}
	if sym != 0 {
		// hooks.json không nằm trong danh sách cho phép nên không tính là symlink bị bỏ.
		t.Errorf("không có symlink trong mục cho phép, nhận %d", sym)
	}
}

func TestImportWithSessionsCodex(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "personal", codexFiles())
	res := importOne1(t, st, src, ImportOptions{WithSessions: true})
	got := map[string]bool{}
	for _, f := range filesUnder(snapshot(t, res.Profile.Home)) {
		got[f] = true
	}
	for _, rel := range []string{
		".codex/sessions/2026/01/r-1.jsonl", ".codex/archived_sessions/r-2.jsonl", ".codex/session_index.jsonl", ".codex/history.jsonl",
	} {
		if !got[rel] {
			t.Errorf("--with-sessions phải chép %s", rel)
		}
	}
	// Vẫn không chép sqlite, bản cài, temp, dữ liệu người dùng.
	for _, rel := range []string{".codex/state_5.sqlite", ".codex/logs_2.sqlite", ".codex/packages/standalone/codex", "Projects/app/main.go", ".codex/tmp/t"} {
		if got[rel] {
			t.Errorf("%s không được chép kể cả khi có --with-sessions", rel)
		}
	}
}

func TestImportClaude(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "claude", "work", claudeFiles())
	res := importOne1(t, st, src, ImportOptions{})
	got := filesUnder(snapshot(t, res.Profile.Home))
	want := []string{".claude/.claude.json", ".claude/.credentials.json", ".claude/CLAUDE.md", ".claude/settings.json", ".gitconfig"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tệp đã chép:\n%v\nmuốn:\n%v", got, want)
	}
	if res.LB != (LB{}) {
		t.Errorf("Claude không có LB: %+v", res.LB)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(res.Profile.Home, ".claude", ".credentials.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf(".credentials.json: %v %v", fi, err)
		}
	}
	res2 := importOne1(t, testStore(t, nil), src, ImportOptions{WithSessions: true})
	got2 := map[string]bool{}
	for _, f := range filesUnder(snapshot(t, res2.Profile.Home)) {
		got2[f] = true
	}
	if !got2[".claude/projects/p/s.jsonl"] || !got2[".claude/history.jsonl"] {
		t.Errorf("--with-sessions phải chép projects/ và history.jsonl: %v", got2)
	}
	for _, rel := range []string{".claude/plugins/x/p.json", ".claude/cache/c", ".claude/backups/b", ".claude/file-history/f", ".local/share/claude/versions/2"} {
		if got2[rel] {
			t.Errorf("%s không được chép", rel)
		}
	}
}

func TestImportLB(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  LB
		notes bool
	}{
		{"linked", map[string]string{"codex-lb.mode": "linked\n", "codex-lb.url": "http://127.0.0.1:2455/backend-api/codex/\n"}, LB{Mode: LBLinked, URL: "http://127.0.0.1:2455"}, false},
		{"direct", map[string]string{"codex-lb.mode": "direct\n"}, LB{Mode: LBDirect}, false},
		{"thiếu tệp", map[string]string{}, LB{}, false},
		{"linked thiếu URL", map[string]string{"codex-lb.mode": "linked"}, LB{}, true},
		{"linked URL hỏng", map[string]string{"codex-lb.mode": "linked", "codex-lb.url": "ftp://x"}, LB{}, true},
		{"chế độ lạ", map[string]string{"codex-lb.mode": "weird"}, LB{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := testStore(t, nil)
			src := newFakeAISource(t)
			files := map[string]string{"home/.gitconfig": "[user]\n"}
			for k, v := range c.files {
				files[k] = v
			}
			src.profile(t, "codex", "x", files)
			// Cổng 2455 không có ai nghe: nhập không được dò mạng nên vẫn thành công.
			res := importOne1(t, st, src, ImportOptions{})
			if res.Status != ImportImported {
				t.Fatalf("%+v", res)
			}
			if res.LB != c.want {
				t.Errorf("LB = %+v, muốn %+v", res.LB, c.want)
			}
			if (len(res.Notes) > 0) != c.notes {
				t.Errorf("notes = %v, muốn có=%v", res.Notes, c.notes)
			}
			p, err := st.Load(Codex, "x")
			if err != nil {
				t.Fatal(err)
			}
			if p.Meta.LB != c.want {
				t.Errorf("profile.json lb = %+v, muốn %+v", p.Meta.LB, c.want)
			}
		})
	}
}

func TestImportLeavesSourceUntouched(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "personal", codexFiles())
	src.profile(t, "claude", "work", claudeFiles())
	real := filepath.Join(t.TempDir(), "hooks.json")
	writeFileMode(t, real, "{}", 0o644)
	if err := os.Symlink(real, filepath.Join(src.Root, "profiles", "codex--personal", "home", ".codex", "hooks.json")); err != nil {
		t.Skipf("không tạo được symlink: %v", err)
	}
	before, realBefore := snapshot(t, src.Root), snapshot(t, filepath.Dir(real))
	for _, o := range []ImportOptions{{DryRun: true}, {WithSessions: true, DropMCP: map[Tool][]string{Codex: {"a"}, Claude: {"a"}}}} {
		o.Source = src.Root
		res, err := Import(st, o)
		if err != nil || len(res) != 2 {
			t.Fatalf("%v %+v", err, res)
		}
		for _, r := range res {
			if r.Status == ImportFailed || r.Status == ImportSkipped {
				t.Fatalf("%+v", r)
			}
		}
		after := snapshot(t, src.Root)
		if strings.Join(filesUnder(before), ",") != strings.Join(filesUnder(after), ",") {
			t.Fatal("danh sách tệp của store nguồn đổi")
		}
		for k, v := range before {
			if after[k] != v {
				t.Errorf("%s đổi: %s -> %s", k, v, after[k])
			}
		}
		if got := snapshot(t, filepath.Dir(real)); got["hooks.json"] != realBefore["hooks.json"] {
			t.Error("tệp thật mà symlink trỏ tới bị đổi")
		}
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "personal", codexFiles())
	res := importOne1(t, st, src, ImportOptions{DryRun: true, WithSessions: true})
	if res.Status != ImportPlanned || len(res.Files) == 0 || res.Bytes == 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Lstat(st.Root); !errors.Is(err, os.ErrNotExist) {
		// testStore trỏ BINT_ISO_HOME vào thư mục tạm; dry-run không được tạo cả thư mục profiles.
		if _, err := os.Lstat(st.profilesDir()); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("--dry-run không được tạo %s", st.profilesDir())
		}
	}
	if _, err := os.Lstat(st.Dir(Codex, "personal")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("--dry-run đã tạo profile: %v", err)
	}
}

func TestImportExistingProfileSkipped(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "personal", codexFiles())
	p := mustCreate(t, st, Codex, "personal")
	marker := filepath.Join(p.Home, ".codex", "auth.json")
	writeFileMode(t, marker, "mine", 0o600)
	before := snapshot(t, p.Dir)
	res := importOne1(t, st, src, ImportOptions{WithSessions: true})
	if res.Status != ImportSkipped || !strings.Contains(res.Reason, "đã có") {
		t.Fatalf("%+v", res)
	}
	after := snapshot(t, p.Dir)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s đổi", k)
		}
	}
	if len(after) != len(before) {
		t.Errorf("profile đã có bị thêm tệp: %v", filesUnder(after))
	}
}

func TestImportFailureRemovesNewProfile(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "aaa", codexFiles())
	src.profile(t, "claude", "work", claudeFiles())
	n := 0
	old := importFault
	t.Cleanup(func() { importFault = old })
	importFault = func(stage, rel string) error {
		if stage == "copy" && strings.HasPrefix(rel, ".codex/") {
			if n++; n == 2 {
				return errors.New("lỗi chèn")
			}
		}
		return nil
	}
	res, err := Import(st, ImportOptions{Source: src.Root})
	if err != nil || len(res) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	byID := map[string]ImportResult{}
	for _, r := range res {
		byID[r.ID] = r
	}
	if r := byID["codex/aaa"]; r.Status != ImportFailed || !strings.Contains(r.Reason, "lỗi chèn") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Lstat(st.Dir(Codex, "aaa")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("profile lỗi giữa chừng phải bị gỡ: %v", err)
	}
	if r := byID["claude/work"]; r.Status != ImportImported {
		t.Errorf("profile khác không bị ảnh hưởng: %+v", r)
	}
	// Chạy lại không báo "đã có".
	importFault = old
	if r := importOne1(t, st, src, ImportOptions{Only: []string{"codex--aaa"}}); r.Status != ImportImported {
		t.Errorf("sau khi gỡ phải nhập lại được: %+v", r)
	}
}

func TestImportSkipsUnsupportedAndMismatched(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "agy", "x", map[string]string{"home/.gemini/a": "x"})
	dir := src.profile(t, "codex", "bad", codexFiles())
	writeFileMode(t, filepath.Join(dir, "profile.env"), "AI_ISOLATOR_TOOL=codex\nAI_ISOLATOR_PROFILE=other\n", 0o644)
	noenv := src.profile(t, "codex", "noenv", codexFiles())
	if err := os.Remove(filepath.Join(noenv, "profile.env")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src.Root, "profiles", "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Import(st, ImportOptions{Source: src.Root})
	if err != nil || len(res) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	for _, r := range res {
		if r.Status != ImportSkipped || r.Reason == "" {
			t.Errorf("%s: %+v", r.ID, r)
		}
	}
	for _, d := range []string{"agy--x", "codex--bad", "codex--noenv"} {
		if _, err := os.Lstat(filepath.Join(st.profilesDir(), d)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s không được tạo", d)
		}
	}
}

func TestImportOnly(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	src.profile(t, "codex", "personal", codexFiles())
	src.profile(t, "claude", "work", claudeFiles())
	res, err := Import(st, ImportOptions{Source: src.Root, Only: []string{"claude--work", "codex/nope"}})
	if err != nil || len(res) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
	if res[0].ID != "claude/work" || res[0].Status != ImportImported {
		t.Errorf("%+v", res[0])
	}
	if !strings.HasPrefix(res[1].ID, "codex--nope") || res[1].Status != ImportFailed {
		t.Errorf("tên không có trong nguồn phải báo lỗi: %+v", res[1])
	}
	if _, err := os.Lstat(st.Dir(Codex, "personal")); !errors.Is(err, os.ErrNotExist) {
		t.Error("profile không được chọn không được nhập")
	}
	if _, err := Import(st, ImportOptions{Source: src.Root, Only: []string{"khong-hop-le"}}); err == nil {
		t.Error("tên sai dạng phải là lỗi")
	}
}

func TestImportCleansConfig(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	srcDir := filepath.Join(src.Root, "profiles", "codex--personal")
	hooks := filepath.ToSlash(filepath.Join(srcDir, "home", ".codex", "hooks.json"))
	other := "/home/someone/.codex/hooks.json"
	cfg := `model = "gpt"
[mcp_servers.dup]
command = "uvx"
[mcp_servers.mine]
command = "npx"
[projects."/work/repo"]
trust_level = "trusted"
[hooks.state."` + hooks + `:pre_tool_use:0:0"]
trusted_hash = "sha256:a"
[hooks.state."` + other + `:pre_tool_use:0:0"]
trusted_hash = "sha256:b"
`
	src.profile(t, "codex", "personal", map[string]string{"home/.codex/config.toml": cfg})
	res := importOne1(t, st, src, ImportOptions{DropMCP: map[Tool][]string{Codex: {"dup", "khong-co"}}})
	if res.Status != ImportImported {
		t.Fatalf("%+v", res)
	}
	f, err := openTOML(filepath.Join(res.Profile.ConfigDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.get([]string{"mcp_servers"}, "dup"); ok {
		t.Error("MCP trùng bản thật phải bị bỏ")
	}
	if _, ok, _ := f.get([]string{"mcp_servers"}, "mine"); !ok {
		t.Error("MCP riêng phải giữ")
	}
	if _, ok, _ := f.get([]string{"projects"}, "/work/repo"); !ok {
		t.Error("[projects.*] phải giữ")
	}
	if _, ok, _ := f.get([]string{"hooks", "state"}, hooks+":pre_tool_use:0:0"); ok {
		t.Error("hooks.state trỏ vào profile ai-isolator phải bị bỏ")
	}
	if _, ok, _ := f.get([]string{"hooks", "state"}, other+":pre_tool_use:0:0"); !ok {
		t.Error("hooks.state trỏ nơi khác phải giữ")
	}
	if m, _, _ := f.get([]string{}, "model"); m != "gpt" {
		t.Errorf("khoá khác phải giữ: %v", m)
	}
	// Không để lại bản sao lưu trong profile.
	if _, err := os.Lstat(filepath.Join(res.Profile.Dir, "sync-backups")); err == nil {
		t.Error("không được để sync-backups trong profile vừa nhập")
	}
}

func TestImportCleansClaudeJSON(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	files := claudeFiles()
	files["home/.claude/.claude.json"] = `{"oauthAccount":{"emailAddress":"a@b"},"mcpServers":{"dup":{"command":"x"},"mine":{"command":"y"}}}`
	src.profile(t, "claude", "work", files)
	res := importOne1(t, st, src, ImportOptions{DropMCP: map[Tool][]string{Claude: {"dup"}}})
	b, err := os.ReadFile(filepath.Join(res.Profile.ConfigDir(), ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"dup"`) || !strings.Contains(s, `"mine"`) || !strings.Contains(s, "emailAddress") {
		t.Errorf(".claude.json sau khi làm sạch: %s", s)
	}
}

func TestImportSourceOverlapRejected(t *testing.T) {
	st := testStore(t, nil)
	for _, src := range []string{st.Root, filepath.Join(st.Root, "profiles"), filepath.Dir(st.Root)} {
		if _, err := Import(st, ImportOptions{Source: src}); err == nil || !strings.Contains(err.Error(), "lồng nhau") {
			t.Errorf("source %s: %v", src, err)
		}
	}
	if _, err := Import(st, ImportOptions{Source: filepath.Join(t.TempDir(), "khong-co")}); err == nil || !strings.Contains(err.Error(), "không thấy store") {
		t.Errorf("store vắng: %v", err)
	}
}

func TestResolveImportSource(t *testing.T) {
	home := t.TempDir()
	env := []string{"HOME=" + home}
	if runtime.GOOS == "windows" {
		env = []string{"USERPROFILE=" + home}
	}
	got, err := ResolveImportSource("", env)
	if err != nil || got != filepath.Join(home, ".local", "share", "ai-isolator") {
		t.Errorf("mặc định: %q %v", got, err)
	}
	abs := t.TempDir()
	if got, _ := ResolveImportSource("", append(env, "AI_ISOLATOR_HOME="+abs)); got != abs {
		t.Errorf("AI_ISOLATOR_HOME: %q", got)
	}
	if got, _ := ResolveImportSource(abs, append(env, "AI_ISOLATOR_HOME=/khac")); got != abs {
		t.Errorf("--source phải thắng: %q", got)
	}
	if _, err := ResolveImportSource("tuong/doi", env); err == nil {
		t.Error("--source tương đối phải lỗi")
	}
	// Trong profile iso: dùng home thật, không phải HOME giả.
	inProf := append(append([]string{}, env...), EnvProfile+"=codex/x", realPrefix+"HOME="+home, "HOME=/gia")
	if runtime.GOOS == "windows" {
		inProf = append(append([]string{}, env...), EnvProfile+"=codex/x", realPrefix+"USERPROFILE="+home, "USERPROFILE=/gia")
	}
	if got, _ := ResolveImportSource("", inProf); got != filepath.Join(home, ".local", "share", "ai-isolator") {
		t.Errorf("trong profile: %q", got)
	}
}

func TestImportSymlinkInsideAllowedDirSkipped(t *testing.T) {
	st := testStore(t, nil)
	src := newFakeAISource(t)
	dir := src.profile(t, "codex", "personal", map[string]string{"home/.codex/rules/default.rules": "r"})
	secret := filepath.Join(t.TempDir(), "secret")
	writeFileMode(t, secret, "bí mật", 0o600)
	if err := os.Symlink(secret, filepath.Join(dir, "home", ".codex", "rules", "link.rules")); err != nil {
		t.Skipf("không tạo được symlink: %v", err)
	}
	// auth.json cũng là symlink: không chép.
	if err := os.Symlink(secret, filepath.Join(dir, "home", ".codex", "auth.json")); err != nil {
		t.Fatal(err)
	}
	res := importOne1(t, st, src, ImportOptions{})
	got := filesUnder(snapshot(t, res.Profile.Home))
	if strings.Join(got, ",") != ".codex/rules/default.rules" {
		t.Errorf("chỉ tệp thường: %v", got)
	}
	if res.HasCreds {
		t.Error("auth.json là symlink nên không có credential")
	}
	n := 0
	for _, s := range res.Skipped {
		n += s.Count
	}
	if n != 2 {
		t.Errorf("phải báo 2 symlink bị bỏ, nhận %d (%+v)", n, res.Skipped)
	}
}
