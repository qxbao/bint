package claude

import (
	"os"
	"path/filepath"
)

// ProjectSlug là tên thư mục dự án của Claude Code: đường dẫn tuyệt đối với mọi ký tự không phải chữ/số ASCII thành '-'.
func ProjectSlug(abs string) string {
	b := []byte(abs)
	for i, c := range b {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			b[i] = '-'
		}
	}
	return string(b)
}

// ConfigDir là thư mục cấu hình của Claude Code: CLAUDE_CONFIG_DIR, không có thì ~/.claude ("" nếu không xác định được nhà).
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// SessionTranscriptPath tìm transcript của phiên id mà `claude -p` chạy ở thư mục cwd ghi:
// <configDir>/projects/<ProjectSlug(cwd)>/<id>.jsonl (configDir rỗng thì ConfigDir()). `claude --resume <id>` chỉ thấy phiên của
// thư mục dự án ứng với cwd hiện tại, nên cwd phải là thư mục đã chạy bước. Thử cả cwd gốc và cwd đã giải liên kết mềm (Claude
// ghi theo thư mục thật). Trả "" nếu không có tệp (tiến trình chết trước khi ghi phiên, hoặc đổi cwd/cấu hình).
func SessionTranscriptPath(configDir, cwd, id string) string {
	if id == "" || cwd == "" {
		return ""
	}
	if configDir == "" {
		configDir = ConfigDir()
	}
	if configDir == "" {
		return ""
	}
	dirs := []string{cwd}
	if real, err := filepath.EvalSymlinks(cwd); err == nil && real != cwd {
		dirs = append(dirs, real)
	}
	for _, d := range dirs {
		p := filepath.Join(configDir, "projects", ProjectSlug(d), id+".jsonl")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}
