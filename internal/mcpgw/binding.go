package mcpgw

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"bint/internal/config"
	"bint/internal/mcpcat"
	"bint/internal/ulid"
)

// RunBinding là bản ghi lượt chạy của một bước `bint run`: nguồn để daemon tự giải role (docs/mcp-gateway-plan.md mục 7,
// M3). Runner ghi trước bước và xoá sau bước. Daemon không mở SQLite và state.json không giữ bước hiện tại của lượt con,
// nên bản ghi này là nguồn đủ thẩm quyền cho "chính sách" (không phải ranh giới bảo mật, mục 9): role không bao giờ lấy
// từ header do client tự khai.
type RunBinding struct {
	RunID  string `json:"run_id"`
	TaskID string `json:"task_id"`
	Step   string `json:"step"`
	// Role là role của bước (harness.StepRole); rỗng nếu bước không có role.
	Role string `json:"role"`
	// ReadOnly: bước chỉ đọc (b-req, b-review): tool ghi vào cây làm việc không được cấp.
	ReadOnly bool   `json:"read_only"`
	Worktree string `json:"worktree,omitempty"`
}

// BindingDirName là thư mục chứa bản ghi, bên trong thư mục runtime của repo.
const BindingDirName = "mcp-runs"

// ErrNoBinding: lượt chạy không có bản ghi (đã xoá, chưa từng ghi, hoặc không phải lượt của bint run).
var ErrNoBinding = errors.New("lượt chạy không có bản ghi cấp MCP")

// RunBindingPath trả <runtimeDir>/mcp-runs/<run_id>.json; run_id phải là ULID (chống path traversal).
func RunBindingPath(runtimeDir, runID string) (string, error) {
	if !ulid.Valid(runID) {
		return "", fmt.Errorf("run_id %q không phải ULID", runID)
	}
	if runtimeDir == "" {
		return "", errors.New("thiếu thư mục runtime")
	}
	return filepath.Join(runtimeDir, BindingDirName, runID+".json"), nil
}

// WriteRunBinding ghi bản ghi nguyên tử (0600) và trả đường dẫn.
func WriteRunBinding(runtimeDir string, b RunBinding) (string, error) {
	path, err := RunBindingPath(runtimeDir, b.RunID)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(path, append(data, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveRunBinding xoá bản ghi (không lỗi nếu đã mất).
func RemoveRunBinding(runtimeDir, runID string) {
	if path, err := RunBindingPath(runtimeDir, runID); err == nil {
		_ = os.Remove(path)
	}
}

// ReadRunBinding đọc bản ghi của run_id; ErrNoBinding (bọc) nếu chưa có.
func ReadRunBinding(runtimeDir, runID string) (RunBinding, error) {
	path, err := RunBindingPath(runtimeDir, runID)
	if err != nil {
		return RunBinding{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RunBinding{}, fmt.Errorf("%w: %s", ErrNoBinding, runID)
	}
	if err != nil {
		return RunBinding{}, err
	}
	var b RunBinding
	if err := json.Unmarshal(data, &b); err != nil {
		return RunBinding{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if b.RunID != runID {
		return RunBinding{}, fmt.Errorf("%s: run_id trong tệp (%q) không khớp tên tệp", filepath.Base(path), b.RunID)
	}
	return b, nil
}

// Identity là danh tính đã giải của một phiên: role và việc bước có chỉ đọc không.
type Identity struct {
	Role     string
	ReadOnly bool
}

// ResolveIdentity giải role của phiên (dùng chung cho daemon và shim, nên không lệch):
//   - không có run_id: role interactive (D4), không chỉ đọc;
//   - có run_id: lấy từ bản ghi lượt chạy trong runtimeDir. Không có bản ghi (hoặc run_id không phải ULID, hoặc task_id
//     khai báo không khớp bản ghi) thì lỗi: phía gọi không cấp service nào (đóng an toàn).
//
// runtimeDir rỗng (không xác định được thư mục trạng thái của repo) cũng là lỗi khi có run_id.
func ResolveIdentity(runtimeDir, runID, taskID string) (Identity, error) {
	if runID == "" {
		return Identity{Role: config.MCPRoleInteractive}, nil
	}
	b, err := ReadRunBinding(runtimeDir, runID)
	if err != nil {
		return Identity{}, err
	}
	if taskID != "" && b.TaskID != "" && taskID != b.TaskID {
		return Identity{}, fmt.Errorf("task_id %q không khớp lượt chạy %s", taskID, runID)
	}
	return Identity{Role: b.Role, ReadOnly: b.ReadOnly}, nil
}

// grantedTools là tập tool (theo tên upstream) mà một phiên được dùng ở một service.
type grantedTools map[string]mcpcat.ToolRef
