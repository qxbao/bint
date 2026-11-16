package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Rollout là tệp phiên của Codex: <CODEX_HOME|~/.codex>/sessions/YYYY/MM/DD/rollout-<thời điểm>-<thread id>.jsonl. Codex ghi
// vào đó mọi sự kiện của thread, kể cả `event_msg` loại `token_count` mang bộ đếm token CỘNG DỒN của thread
// (payload.info.total_token_usage) — nguồn để tính token của một lần nối tiếp (`codex exec resume`) mà không cộng trùng phần của
// các lần chạy trước (đối chiếu trên codex-cli 0.167, xem testdata/rollout-resume.jsonl).

// threadIDRe: thread id hợp lệ để ghép vào mẫu glob (chữ, số, gạch ngang); id lạ bị từ chối thay vì thành ký tự đặc biệt của glob.
var threadIDRe = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

// HomeDir là thư mục cấu hình của Codex: CODEX_HOME, không có thì ~/.codex ("" nếu không xác định được nhà).
func HomeDir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// RolloutPath tìm rollout của thread (codexHome rỗng thì HomeDir()). Có nhiều tệp (thread bị ghi sang tệp mới) thì lấy tệp có tên
// lớn nhất (thời điểm mới nhất). Trả "" nếu không có.
func RolloutPath(codexHome, thread string) string {
	if !threadIDRe.MatchString(thread) {
		return ""
	}
	if codexHome == "" {
		codexHome = HomeDir()
	}
	if codexHome == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*-"+thread+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

// TokenUsage là bộ đếm token cộng dồn của thread (total_token_usage). CachedInput là phần của Input được phục vụ từ bộ nhớ đệm
// (đã nằm trong Input, không cộng thêm).
type TokenUsage struct {
	Input, CachedInput, Output int64
}

// RolloutUsage đọc dòng `token_count` cuối cùng có total_token_usage trong rollout. nil nếu không đọc được tệp hoặc chưa có dòng
// nào (không biết, không phải 0). Dòng rollout có thể rất dài (trạng thái thế giới, ảnh): đọc theo dòng không giới hạn độ dài và
// chỉ giải mã dòng có chữ "token_count".
func RolloutUsage(path string) *TokenUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var last *TokenUsage
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"token_count"`)) {
			var ev struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Info *struct {
						Total *struct {
							Input  int64 `json:"input_tokens"`
							Cached int64 `json:"cached_input_tokens"`
							Output int64 `json:"output_tokens"`
						} `json:"total_token_usage"`
					} `json:"info"`
				} `json:"payload"`
			}
			if json.Unmarshal(bytes.TrimSpace(line), &ev) == nil && ev.Type == "event_msg" && ev.Payload.Type == "token_count" &&
				ev.Payload.Info != nil && ev.Payload.Info.Total != nil {
				t := ev.Payload.Info.Total
				last = &TokenUsage{Input: t.Input, CachedInput: t.Cached, Output: t.Output}
			}
		}
		if err != nil { // io.EOF hoặc lỗi đọc: giữ bộ đếm đọc được tới đó
			return last
		}
	}
}
