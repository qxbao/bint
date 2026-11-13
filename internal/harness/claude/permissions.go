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

// PermissionRule là quy tắc quyền cho mọi công cụ của MCP server "bint" (kể cả tool của service ngoài đi qua cửa vào gộp
// với tên <tiền tố>_<tool>): quyền chỉnh một lần, không phải sửa mỗi khi danh mục thêm tool.
const PermissionRule = "mcp__bint__*"

// InstallPermissions thêm PermissionRule vào permissions.allow của tệp settings (tạo nếu chưa có), giữ nguyên mọi khoá và
// quy tắc khác; chạy lại không thêm trùng. Có sao lưu trước khi ghi.
func InstallPermissions(settingsPath, backupDir string, now time.Time) (InstallResult, error) {
	return editPermissions(settingsPath, backupDir, now, true)
}

// RemovePermissions gỡ PermissionRule khỏi permissions.allow của tệp settings (bint init --no-mcp); tệp không có hoặc không có
// quy tắc thì không ghi gì. Giữ nguyên mọi khoá và quy tắc khác; có sao lưu trước khi ghi.
func RemovePermissions(settingsPath, backupDir string, now time.Time) (InstallResult, error) {
	if _, err := os.Stat(settingsPath); errors.Is(err, os.ErrNotExist) {
		return InstallResult{Path: settingsPath}, nil
	}
	return editPermissions(settingsPath, backupDir, now, false)
}

func editPermissions(settingsPath, backupDir string, now time.Time, add bool) (InstallResult, error) {
	res := InstallResult{Path: settingsPath}
	raw, err := os.ReadFile(settingsPath)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}
	doc, err := jsondoc.Parse(raw)
	if err != nil {
		return res, fmt.Errorf("%s: %w", settingsPath, err)
	}
	perms := map[string]json.RawMessage{}
	if v, ok := doc.Get("permissions"); ok {
		if err := json.Unmarshal(v, &perms); err != nil {
			return res, fmt.Errorf("%s: khoá permissions không phải object: %w", settingsPath, err)
		}
	}
	var allow []json.RawMessage
	if v, ok := perms["allow"]; ok {
		if err := json.Unmarshal(v, &allow); err != nil {
			return res, fmt.Errorf("%s: permissions.allow không phải mảng: %w", settingsPath, err)
		}
	}
	kept := make([]json.RawMessage, 0, len(allow))
	found := false
	for _, a := range allow {
		var s string
		if json.Unmarshal(a, &s) == nil && s == PermissionRule {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if found == add {
		return res, nil // đã có (thêm) hoặc không có (gỡ): không ghi
	}
	if add {
		rule, _ := json.Marshal(PermissionRule)
		kept = append(kept, rule)
	}
	perms["allow"], _ = json.Marshal(kept)
	if err := doc.SetValue("permissions", perms); err != nil {
		return res, err
	}
	out, err := doc.MarshalIndent()
	if err != nil {
		return res, err
	}
	if res.BackupPath, err = fsafe.Backup(settingsPath, backupDir, "claude/"+filepath.Base(settingsPath), now); err != nil {
		return res, err
	}
	if err := fsafe.WriteAtomic(settingsPath, out, 0o644); err != nil {
		return res, err
	}
	res.Changed = true
	if existed {
		res.Prev = fsafe.Hash(string(raw))
	}
	res.SHA256 = fsafe.Hash(string(out))
	return res, nil
}

// PermissionInstalled cho biết permissions.allow của tệp settings đã có PermissionRule.
func PermissionInstalled(settingsPath string) bool {
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var s struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	for _, a := range s.Permissions.Allow {
		if a == PermissionRule {
			return true
		}
	}
	return false
}
