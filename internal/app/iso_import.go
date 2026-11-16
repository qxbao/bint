package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"bint/internal/iso"
	"bint/internal/jsondoc"
)

// IsoFinalizeResult là kết quả các bước hoàn thiện một profile mới (dùng chung cho tạo mới và nhập): liên kết
// chia sẻ cache, bản cài harness, cảnh báo bản cài và đồng bộ cấu hình từ máy thật. Lỗi từng bước chỉ là cảnh
// báo: profile vẫn dùng được.
type IsoFinalizeResult struct {
	// Links là liên kết chia sẻ và bản cài (EnsureShares rồi EnsureInstall).
	Links      []iso.ShareResult
	SharesErr  error
	InstallErr error
	Issues     []string
	Sync       iso.SyncReport
	SyncErr    error
}

// IsoFinalize chạy các bước hoàn thiện cho profile p vừa tạo/nhập: EnsureShares, EnsureInstall, InstallIssues và
// IsoSync (docs/iso-plan.md mục 5.4–5.6). env mặc định là os.Environ().
func IsoFinalize(ctx context.Context, p iso.Profile, env []string) IsoFinalizeResult {
	if env == nil {
		env = os.Environ()
	}
	var r IsoFinalizeResult
	shares, err := iso.EnsureShares(p)
	r.SharesErr = err
	inst, err := iso.EnsureInstall(p, env)
	r.InstallErr = err
	r.Links = append(shares, inst...)
	r.Issues = iso.InstallIssues(p, env)
	r.Sync, r.SyncErr = IsoSync(ctx, p, IsoSyncOptions{Env: env})
	return r
}

// IsoImportOptions là tuỳ chọn của IsoImport.
type IsoImportOptions struct {
	// Source là gốc store ai-isolator ("" = AI_ISOLATOR_HOME thật rồi ~/.local/share/ai-isolator).
	Source       string
	Only         []string
	WithSessions bool
	DryRun       bool
	// Env là env của tiến trình (mặc định os.Environ()).
	Env []string
}

// IsoImportOutcome là một profile đã xử lý; Final chỉ có khi đã nhập thật (không phải --dry-run).
type IsoImportOutcome struct {
	iso.ImportResult
	Final *IsoFinalizeResult `json:"-"`
}

// IsoImport nhập profile từ ai-isolator (iso.Import) rồi chạy bước hoàn thiện như tạo mới cho từng profile đã
// nhập. Mục MCP trùng tên bản thật bị bỏ khỏi cấu hình đã chép để IsoSync dựng lại theo receipt mà không rơi vào
// nhánh xung đột. Chỉ người dùng chạy được (không phải phiên agent): ErrAgentForbidden.
func IsoImport(ctx context.Context, st *iso.Store, o IsoImportOptions) ([]IsoImportOutcome, error) {
	if err := RequireHuman("bint iso import"); err != nil {
		return nil, err
	}
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	drop, notes := isoRealMCPNames(env)
	results, err := iso.Import(st, iso.ImportOptions{
		Source: o.Source, Only: o.Only, WithSessions: o.WithSessions, DryRun: o.DryRun, DropMCP: drop, Env: env,
	})
	if err != nil {
		return nil, err
	}
	out := make([]IsoImportOutcome, 0, len(results))
	for _, r := range results {
		oc := IsoImportOutcome{ImportResult: r}
		if tool, _, ok := splitToolID(r.ID); ok {
			oc.Notes = append(oc.Notes, notes[tool]...)
		}
		if r.Status == iso.ImportImported {
			fin := IsoFinalize(ctx, r.Profile, env)
			oc.Final = &fin
		}
		out = append(out, oc)
	}
	return out, nil
}

func splitToolID(id string) (iso.Tool, string, bool) {
	t, name, err := iso.ParseID(id)
	return t, name, err == nil
}

// isoRealMCPNames là tên mục MCP có ở cấu hình thật của từng tool (Codex: [mcp_servers.*] của config.toml;
// Claude: mcpServers của .claude.json): trùng tên với bản đã chép thì phải bỏ bản chép. Không đọc được thì
// không bỏ gì và ghi chú (IsoSync sẽ báo lại).
func isoRealMCPNames(env []string) (map[iso.Tool][]string, map[iso.Tool][]string) {
	drop := map[iso.Tool][]string{}
	notes := map[iso.Tool][]string{}
	if home, err := iso.RealCodexHome(env); err != nil {
		notes[iso.Codex] = append(notes[iso.Codex], fmt.Sprintf("không xác định được CODEX_HOME thật: %v (không bỏ MCP trùng)", err))
	} else if cfg, err := readTOMLMap(filepath.Join(home, "config.toml")); err != nil {
		notes[iso.Codex] = append(notes[iso.Codex], fmt.Sprintf("không đọc được config.toml của Codex thật: %v (không bỏ MCP trùng)", err))
	} else {
		for name := range tomlSub(cfg, "mcp_servers") {
			drop[iso.Codex] = append(drop[iso.Codex], name)
		}
	}
	path, err := iso.RealClaudeJSON(env)
	if err != nil {
		notes[iso.Claude] = append(notes[iso.Claude], fmt.Sprintf("không xác định được .claude.json thật: %v (không bỏ MCP trùng)", err))
		return drop, notes
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return drop, notes
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
		notes[iso.Claude] = append(notes[iso.Claude], fmt.Sprintf("không đọc được mcpServers của %s: %v (không bỏ MCP trùng)", path, err))
		return drop, notes
	}
	for name := range servers {
		drop[iso.Claude] = append(drop[iso.Claude], name)
	}
	return drop, notes
}
