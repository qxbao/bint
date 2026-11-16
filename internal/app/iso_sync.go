package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"bint/internal/harness/claude"
	"bint/internal/harness/codex"
	"bint/internal/iso"
	"bint/internal/jsondoc"
	"bint/internal/state"
)

// Tệp đích (tương đối theo home của profile) của sync.
const (
	isoCodexConfig   = ".codex/config.toml"
	isoCodexHooks    = ".codex/hooks.json"
	isoClaudeJSON    = ".claude/.claude.json"
	isoClaudeSetting = ".claude/settings.json"
)

// IsoSyncOptions là tuỳ chọn của IsoSync.
type IsoSyncOptions struct {
	// Dir là thư mục làm việc để tìm repo cho trust Codex ("" = thư mục hiện tại).
	Dir string
	// Check: chỉ so sánh, không ghi gì. Static: không chạy harness (hooks/list); Check luôn là Static.
	Check, Static bool
	// Env là env của tiến trình (mặc định os.Environ()).
	Env []string
}

// IsoSync đồng bộ cấu hình từ máy thật sang profile p (docs/iso-plan.md mục 5.6): tính khoá mong muốn từ
// cấu hình Codex/Claude thật rồi giao cho iso.Sync (receipt, xung đột, khoá phiên). Không bao giờ đọc hay chép
// auth.json, .credentials.json hay khoá tài khoản của .claude.json.
func IsoSync(ctx context.Context, p iso.Profile, o IsoSyncOptions) (iso.SyncReport, error) {
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	var b isoSyncBuilder
	b.ctx, b.p, b.o, b.env = ctx, p, o, env
	switch p.Tool {
	case iso.Codex:
		b.codex()
	case iso.Claude:
		b.claude()
	}
	return iso.Sync(p, b.src, iso.SyncOptions{Check: o.Check, Env: env, Now: time.Now()})
}

type isoSyncBuilder struct {
	ctx context.Context
	p   iso.Profile
	o   IsoSyncOptions
	env []string
	src iso.SyncSource
	// root là gốc repo của Dir (cwd của hooks/list); rỗng khi không ở trong repo Git.
	root string
}

func (b *isoSyncBuilder) note(format string, a ...any) {
	b.src.Notes = append(b.src.Notes, fmt.Sprintf(format, a...))
}

func (b *isoSyncBuilder) skip(kind string) { b.src.Skip = append(b.src.Skip, kind) }

func (b *isoSyncBuilder) backupDir() string { return filepath.Join(b.p.Dir, "sync-backups") }

// ---- Codex ----

func (b *isoSyncBuilder) codex() {
	realHome, err := iso.RealCodexHome(b.env)
	if err != nil {
		b.note("không xác định được CODEX_HOME thật: %v", err)
		b.skip(iso.KindMCP)
		b.skip(iso.KindTrust)
		b.skip(iso.KindHook)
		b.skip(iso.KindHookState)
		return
	}
	cfg, err := readTOMLMap(filepath.Join(realHome, "config.toml"))
	if err != nil {
		b.note("không đọc được config.toml của Codex thật: %v (bỏ qua MCP và trust)", err)
		b.skip(iso.KindMCP)
		b.skip(iso.KindTrust)
		cfg = nil
	}
	if cfg != nil {
		b.codexMCP(cfg)
		b.codexTrust(cfg)
	}
	b.codexUserHook(realHome, cfg)
}

func (b *isoSyncBuilder) codexMCP(cfg map[string]any) {
	servers := tomlSub(cfg, "mcp_servers")
	if len(servers) == 0 {
		return
	}
	bintEnv, err := iso.BintMCPEnv(b.env)
	if err != nil {
		b.note("không ghim được BINT_STATE_DIR/BINT_CONFIG_HOME cho MCP: %v (bỏ qua MCP)", err)
		b.skip(iso.KindMCP)
		return
	}
	for name, v := range servers {
		b.src.Entries = append(b.src.Entries, iso.Entry{Kind: iso.KindMCP, File: isoCodexConfig,
			Section: []string{"mcp_servers"}, Name: name, Value: iso.AdaptCodexMCP(v, bintEnv)})
	}
}

// codexTrust chép [projects.*] của gốc repo, gốc Git main và thư mục worktree của bint, cùng [hooks.state.*] của
// các hooks.json nằm trong những thư mục đó, nguyên văn từ cấu hình thật: sync chỉ nhân bản quyết định trust mà
// người dùng đã đưa ra, không tự trust.
func (b *isoSyncBuilder) codexTrust(cfg map[string]any) {
	dir := b.o.Dir
	if dir == "" {
		dir = "."
	}
	ws, err := OpenWorkspace(b.ctx, dir, false)
	if err != nil {
		b.note("không xác định được repo của %s: bỏ qua trust dự án của Codex (%v)", dir, err)
		b.skip(iso.KindTrust)
		return
	}
	b.root = ws.Repo.Toplevel
	exact := []string{ws.Repo.Toplevel}
	if m := ws.Repo.MainWorktree(); m != "" {
		exact = append(exact, m)
	}
	under := []string{ws.Repo.Toplevel}
	if m := ws.Repo.MainWorktree(); m != "" {
		under = append(under, m)
	}
	under = append(under, ws.Paths.Worktrees)
	for key, v := range tomlSub(cfg, "projects") {
		e := iso.Entry{Kind: iso.KindTrust, File: isoCodexConfig, Section: []string{"projects"}, Name: key, Value: v}
		if pathIn(key, exact, false) || pathIn(key, []string{ws.Paths.Worktrees}, true) {
			b.src.Entries = append(b.src.Entries, e)
		} else {
			// Profile dùng chung nhiều repo: trust của repo khác không phải "bản thật đã bỏ", đừng gỡ.
			b.src.Retain = append(b.src.Retain, e)
		}
	}
	for key, v := range tomlSub(tomlSubMap(cfg, "hooks"), "state") {
		e := iso.Entry{Kind: iso.KindTrust, File: isoCodexConfig, Section: []string{"hooks", "state"}, Name: key, Value: v}
		if path, _, ok := splitHookKey(key); ok && pathIn(path, under, true) {
			b.src.Entries = append(b.src.Entries, e)
		} else {
			b.src.Retain = append(b.src.Retain, e)
		}
	}
}

func (b *isoSyncBuilder) codexUserHook(realHome string, cfg map[string]any) {
	realPath := codex.UserHooksPathIn(realHome)
	profPath := codex.UserHooksPathIn(filepath.Join(b.p.Home, ".codex"))
	cmd, ok, err := codex.InstalledHookCommand(realPath)
	if err != nil {
		b.note("không đọc được hooks.json thật: %v (bỏ qua hook user)", err)
		b.skip(iso.KindHook)
		b.skip(iso.KindHookState)
		return
	}
	b.src.Hooks = append(b.src.Hooks, iso.HookTarget{
		File: isoCodexHooks,
		Get: func() (string, bool, error) {
			c, installed, err := codex.InstalledHookCommand(profPath)
			if err != nil || !installed {
				return "", false, err
			}
			current, err := codex.HooksCurrent(profPath)
			return c, current, err
		},
		Install: func(cmd string) error {
			_, err := codex.InstallHooks(profPath, codex.BintPathFromCommand(cmd), b.backupDir(), time.Now())
			return err
		},
		Uninstall: func() error {
			_, err := codex.UninstallHooks(profPath, b.backupDir(), time.Now())
			return err
		},
	})
	if ok {
		b.src.Entries = append(b.src.Entries, iso.Entry{Kind: iso.KindHook, File: isoCodexHooks,
			Section: []string{"hooks"}, Name: "bint", Value: cmd})
	}
	// Trust hook user và xác minh hook dự án cần hooks/list với env của profile: chỉ khi ghi.
	b.skip(iso.KindHookState)
	// Không có hook user ở bản thật và không có repo thì không có gì để xác minh: khỏi chạy Codex.
	if b.o.Check || b.o.Static || (!ok && b.root == "") {
		return
	}
	b.src.Late = func() ([]iso.Entry, []string, error) { return b.codexLate(realPath, profPath, cfg) }
}

// codexLate chạy hooks/list của Codex với env của profile (sau khi trust và hook đã ghi) để: (1) chép trusted_hash
// của hook user của bint từ bản thật khi currentHash ở profile khớp, (2) báo hook dự án của bint đã trusted chưa.
func (b *isoSyncBuilder) codexLate(realHooks, profHooks string, cfg map[string]any) ([]iso.Entry, []string, error) {
	bin, err := iso.LookHarness(iso.Codex, b.env, b.p.StoreRoot)
	if err != nil {
		return nil, []string{fmt.Sprintf("không xác minh được trust hook (hooks/list): %v", err)}, nil
	}
	penv, cleanup, err := iso.ProbeEnv(b.p, b.env)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	root := b.root
	if root == "" {
		root = b.p.Home
	}
	ctx, cancel := context.WithTimeout(b.ctx, 20*time.Second)
	defer cancel()
	list, err := codex.ListHooks(ctx, bin, root, penv)
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	for _, e := range list.Errors {
		notes = append(notes, fmt.Sprintf("Codex báo lỗi nạp hook trong profile: %s", e))
	}
	var entries []iso.Entry
	proj, projTrusted := 0, 0
	realState := tomlSub(tomlSubMap(cfg, "hooks"), "state")
	for _, h := range list.Hooks {
		if !codex.IsBintHookCommand(h.Command) {
			continue
		}
		trusted := h.TrustStatus == "trusted" || h.TrustStatus == "managed"
		if samePathStr(h.SourcePath, profHooks) {
			if v, ok := matchUserHookState(realState, realHooks, h); ok {
				entries = append(entries, iso.Entry{Kind: iso.KindHookState, File: isoCodexConfig,
					Section: []string{"hooks", "state"}, Name: h.Key, Value: v})
			} else if !trusted {
				notes = append(notes, fmt.Sprintf("hook user %s của bint chưa trust trong profile (trusted_hash ở máy thật không khớp currentHash): cần trust tay trong profile (/hooks của Codex)", h.EventName))
			}
			continue
		}
		proj++
		if trusted {
			projTrusted++
		}
	}
	switch {
	case proj == 0 && b.root != "":
		notes = append(notes, "Codex trong profile chưa nạp hook dự án của bint (repo chưa trust hoặc chưa cài `bint init --hooks`)")
	case proj > 0 && projTrusted < proj:
		notes = append(notes, fmt.Sprintf("hook dự án của bint trong profile: %d/%d trusted — trust ở máy thật (/hooks của Codex) rồi chạy lại bint iso sync; bint không tự trust", projTrusted, proj))
	case proj > 0:
		notes = append(notes, fmt.Sprintf("hook dự án của bint trong profile: %d/%d trusted", projTrusted, proj))
	}
	return entries, notes, nil
}

// matchUserHookState tìm [hooks.state] của bản thật cho hook user h của profile: cùng hooks.json thật, cùng sự
// kiện, và trusted_hash bằng currentHash của h (bản thật đã trust đúng định nghĩa này).
func matchUserHookState(realState map[string]any, realHooks string, h codex.HookInfo) (any, bool) {
	_, ev, ok := splitHookKey(h.Key)
	if !ok || h.CurrentHash == "" {
		return nil, false
	}
	for key, v := range realState {
		path, rev, ok := splitHookKey(key)
		if !ok || rev != ev || !samePathStr(path, realHooks) {
			continue
		}
		if m, _ := v.(map[string]any); m != nil && m["trusted_hash"] == h.CurrentHash {
			return v, true
		}
	}
	return nil, false
}

// ---- Claude ----

func (b *isoSyncBuilder) claude() {
	b.claudeMCP()
	home, err := iso.RealHomeOf(b.env)
	if err != nil {
		b.note("không xác định được home thật: %v (bỏ qua hook user)", err)
		b.skip(iso.KindHook)
		return
	}
	realPath := claude.UserSettingsPathIn(home)
	profPath := claude.UserSettingsPathIn(b.p.Home)
	cmd, ok, err := claude.InstalledHookCommand(realPath)
	if err != nil {
		b.note("không đọc được settings.json thật: %v (bỏ qua hook user)", err)
		b.skip(iso.KindHook)
		return
	}
	b.src.Hooks = append(b.src.Hooks, iso.HookTarget{
		File: isoClaudeSetting,
		Get: func() (string, bool, error) {
			c, installed, err := claude.InstalledHookCommand(profPath)
			if err != nil || !installed {
				return "", false, err
			}
			current, err := claude.HooksUpToDate(profPath)
			return c, current, err
		},
		Install: func(cmd string) error {
			_, err := claude.InstallHooks(profPath, claude.BintPathFromCommand(cmd), b.backupDir(), time.Now())
			return err
		},
		Uninstall: func() error {
			_, err := claude.UninstallHooks(profPath, b.backupDir(), time.Now())
			return err
		},
	})
	if ok {
		b.src.Entries = append(b.src.Entries, iso.Entry{Kind: iso.KindHook, File: isoClaudeSetting,
			Section: []string{"hooks"}, Name: "bint", Value: cmd})
	}
}

// claudeMCP chép mcpServers (chỉ khoá này) của .claude.json thật; oauthAccount và mọi khoá khác không bao giờ được đọc ra.
func (b *isoSyncBuilder) claudeMCP() {
	path, err := iso.RealClaudeJSON(b.env)
	if err != nil {
		b.note("không xác định được .claude.json thật: %v (bỏ qua MCP)", err)
		b.skip(iso.KindMCP)
		return
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var servers map[string]json.RawMessage
	if err == nil {
		var doc *jsondoc.Doc
		if doc, err = jsondoc.Parse(raw); err == nil {
			if sec, ok := doc.Get("mcpServers"); ok {
				err = json.Unmarshal(sec, &servers)
			}
		}
	}
	if err != nil {
		b.note("không đọc được mcpServers của %s: %v (bỏ qua MCP)", path, err)
		b.skip(iso.KindMCP)
		return
	}
	for name, rawV := range servers {
		var v any
		if err := json.Unmarshal(rawV, &v); err != nil {
			b.note("mcpServers.%s trong %s không hợp lệ: %v (bỏ qua mục này)", name, path, err)
			continue
		}
		b.src.Entries = append(b.src.Entries, iso.Entry{Kind: iso.KindMCP, File: isoClaudeJSON,
			Section: []string{"mcpServers"}, Name: name, Value: v})
	}
}

// ---- tiện ích ----

func readTOMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func tomlSubMap(m map[string]any, key string) map[string]any {
	sub, _ := m[key].(map[string]any)
	return sub
}

// tomlSub là bảng con m[key] (nil nếu không phải bảng).
func tomlSub(m map[string]any, key string) map[string]any { return tomlSubMap(m, key) }

// splitHookKey tách khoá [hooks.state."<hooks.json>:<sự_kiện>:<i>:<j>"] từ bên phải (đường dẫn Windows có ':').
func splitHookKey(key string) (path, event string, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) < 4 {
		return "", "", false
	}
	n := len(parts)
	return strings.Join(parts[:n-3], ":"), parts[n-3], true
}

func samePathStr(a, b string) bool { return state.NormalizePath(a) == state.NormalizePath(b) }

// pathIn cho biết path bằng (hoặc, khi sub, nằm dưới) một trong các thư mục dirs (so sau NormalizePath).
func pathIn(path string, dirs []string, sub bool) bool {
	np := state.NormalizePath(path)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		nd := state.NormalizePath(d)
		if np == nd || (sub && strings.HasPrefix(np, strings.TrimSuffix(nd, "/")+"/")) {
			return true
		}
	}
	return false
}
