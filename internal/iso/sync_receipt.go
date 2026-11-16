package iso

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"bint/internal/fsafe"
)

// ReceiptVersion là phiên bản của sync-receipt.json.
const ReceiptVersion = 1

// ReceiptName là tên tệp receipt trong thư mục profile (0600).
const ReceiptName = "sync-receipt.json"

// Loại khoá do sync quản lý.
const (
	KindMCP       = "mcp"        // [mcp_servers.X] (Codex), mcpServers.X (Claude)
	KindTrust     = "trust"      // [projects.DIR], [hooks.state.KEY] của hooks dự án (chép từ bản thật)
	KindHook      = "hook"       // hook cấp user của bint cài bằng InstallHooks
	KindHookState = "hook-state" // [hooks.state.KEY] của hook user (chỉ chép khi currentHash khớp)
)

// ReceiptEntry là một khoá do sync quản lý: sha256 của giá trị chuẩn hoá lần cuối sync ghi hoặc nhận.
type ReceiptEntry struct {
	Kind    string   `json:"kind"`
	File    string   `json:"file"` // tương đối theo home của profile, dấu '/'
	Key     string   `json:"key"`  // dạng đọc được: <section>.<name>
	Section []string `json:"section"`
	Name    string   `json:"name"`
	SHA256  string   `json:"sha256"`
}

// Receipt là nội dung sync-receipt.json.
type Receipt struct {
	Version int            `json:"version"`
	Entries []ReceiptEntry `json:"entries"`
}

// ReceiptPath là đường dẫn sync-receipt.json của profile.
func (p Profile) ReceiptPath() string { return filepath.Join(p.Dir, ReceiptName) }

// LoadReceipt đọc receipt. Chưa có tệp thì trả receipt rỗng, không lỗi. Tệp hỏng (hoặc phiên bản lạ) trả
// receipt rỗng kèm lỗi: coi như rỗng nghĩa là mọi khoá khác bản mong muốn thành xung đột, không bị ghi đè.
func LoadReceipt(path string) (Receipt, error) {
	empty := Receipt{Version: ReceiptVersion}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return empty, fmt.Errorf("%s: %w", path, err)
	}
	if r.Version != ReceiptVersion {
		return empty, fmt.Errorf("%s: phiên bản %d chưa hỗ trợ (cần %d)", path, r.Version, ReceiptVersion)
	}
	return r, nil
}

// SaveReceipt ghi receipt nguyên tử với quyền 0600, các mục sắp theo (tệp, khoá).
func SaveReceipt(path string, r Receipt) error {
	r.Version = ReceiptVersion
	r.Entries = slices.Clone(r.Entries)
	slices.SortFunc(r.Entries, func(a, b ReceiptEntry) int {
		return strings.Compare(a.File+"\x00"+a.Key, b.File+"\x00"+b.Key)
	})
	if r.Entries == nil {
		r.Entries = []ReceiptEntry{}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(path, append(data, '\n'), 0o600)
}

// HashValue là sha256 (hex) của JSON chuẩn của v (khoá map được sắp), nên không phụ thuộc thứ tự khoá hay
// định dạng tệp nguồn (TOML/JSON).
func HashValue(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func entryKey(section []string, name string) string {
	return strings.Join(append(slices.Clone(section), name), ".")
}
