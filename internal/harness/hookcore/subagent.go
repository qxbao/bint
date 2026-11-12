package hookcore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"bint/internal/fsafe"
)

// SubagentRecord là tệp nhỏ hook ghi khi SubagentStop tới: runtime/subagents/<agent_id>.json. Spool vẫn có sự kiện
// subagent.stopped, nhưng spool bị nạp vào SQLite và xoá bởi bất kỳ lệnh nào (kể cả `bint task …` của chính subagent), nên
// lúc `bint run complete` đã có thể không còn tệp chờ; tệp này cho workflow tìm transcript mà không mở SQLite.
type SubagentRecord struct {
	AgentID        string `json:"agent_id"`
	AgentType      string `json:"agent_type,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	At             string `json:"at"`
}

// ValidAgentID: agent_id chỉ nhận [A-Za-z0-9_-] (làm tên tệp; không cho đi ra ngoài thư mục).
func ValidAgentID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// SubagentRecordPath là đường dẫn tệp của agentID dưới thư mục runtime ("" nếu agentID không hợp lệ).
func SubagentRecordPath(runtimeDir, agentID string) string {
	if !ValidAgentID(agentID) {
		return ""
	}
	return filepath.Join(runtimeDir, "subagents", agentID+".json")
}

// WriteSubagentRecord ghi nguyên tử. Lỗi được trả cho người gọi (hook bỏ qua nó).
func WriteSubagentRecord(runtimeDir string, rec SubagentRecord) error {
	p := SubagentRecordPath(runtimeDir, rec.AgentID)
	if p == "" {
		return os.ErrInvalid
	}
	if rec.At == "" {
		rec.At = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(p, b, 0o600)
}

// ReadSubagentRecord đọc tệp của agentID; ok=false nếu không có hoặc hỏng.
func ReadSubagentRecord(runtimeDir, agentID string) (SubagentRecord, bool) {
	p := SubagentRecordPath(runtimeDir, agentID)
	if p == "" {
		return SubagentRecord{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return SubagentRecord{}, false
	}
	var rec SubagentRecord
	if json.Unmarshal(b, &rec) != nil || rec.AgentID != agentID {
		return SubagentRecord{}, false
	}
	return rec, true
}
