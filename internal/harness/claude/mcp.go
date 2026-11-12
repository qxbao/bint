package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bint/internal/fsafe"
	"bint/internal/jsondoc"
)

// MCPServerName là tên server trong cấu hình MCP; công cụ có tiền tố mcp__bint__.
const MCPServerName = "bint"

// MCPConfigPath là tệp .mcp.json cấp dự án của Claude Code (commit được).
func MCPConfigPath(repoRoot string) string { return filepath.Join(repoRoot, ".mcp.json") }

// InstallMCP thêm (hoặc gỡ, khi install = false) mục "bint" trong mcpServers của
// tệp .mcp.json, giữ nguyên mọi server khác.
func InstallMCP(path, bintPath, backupDir string, install bool, now time.Time) (InstallResult, error) {
	return InstallMCPAt(path, bintPath, backupDir, "claude/.mcp.json", install, now)
}

// InstallMCPAt như InstallMCP nhưng đặt tên bản sao lưu (backupRel) cho tệp cấu hình có cùng dạng mcpServers của
// harness khác (Antigravity: mcp_config.json cấp người dùng).
func InstallMCPAt(path, bintPath, backupDir, backupRel string, install bool, now time.Time) (InstallResult, error) {
	res := InstallResult{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	doc, err := jsondoc.Parse(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	servers := map[string]json.RawMessage{}
	if v, ok := doc.Get("mcpServers"); ok {
		if err := json.Unmarshal(v, &servers); err != nil {
			return res, fmt.Errorf("%s: mcpServers không phải object: %w", path, err)
		}
	}
	if install {
		cmd := bintPath
		if cmd == "" {
			cmd = "bint"
		}
		b, _ := json.Marshal(map[string]any{"command": filepath.ToSlash(cmd), "args": []string{"mcp", "serve"}})
		servers[MCPServerName] = b
	} else {
		if _, ok := servers[MCPServerName]; !ok {
			return res, nil
		}
		delete(servers, MCPServerName)
	}
	if len(servers) == 0 {
		doc.Del("mcpServers")
	} else {
		b, err := json.Marshal(servers)
		if err != nil {
			return res, err
		}
		doc.Set("mcpServers", b)
	}
	out, err := doc.MarshalIndent()
	if err != nil {
		return res, err
	}
	if jsondoc.Equal(raw, out) {
		return res, nil
	}
	if doc.Len() == 0 && len(raw) == 0 {
		return res, nil
	}
	if res.BackupPath, err = fsafe.Backup(path, backupDir, backupRel, now); err != nil {
		return res, err
	}
	if err := fsafe.WriteAtomic(path, out, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	return res, nil
}

// MCPInstalled cho biết .mcp.json có mục "bint".
func MCPInstalled(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var s struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	_, ok := s.MCPServers[MCPServerName]
	return ok
}
