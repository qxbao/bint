package codex

import (
	"bufio"
	"encoding/json"
	"os"

	"bint/internal/harness/hookcore"
)

// PostToolUse của Codex 0.167 có cả lệnh shell fail, chỉ trả stdout, không có
// exit_code. Transcript item_completed đã được ghi trước hook và có cùng id.
// Thiếu dòng xác định được thì kết quả unknown, tuyệt đối không đoán từ chữ FAIL.
func postToolOutcome(in hookcore.Input, info hookcore.ToolInfo) (bool, bool) {
	if in.Event == hookcore.EvPostToolFailure {
		return false, true
	}
	if info.Kind != hookcore.ToolShell {
		return in.Event == hookcore.EvPostTool, true
	}
	if in.TranscriptPath == "" || in.ToolUseID == "" {
		return false, false
	}
	f, err := os.Open(in.TranscriptPath)
	if err != nil {
		return false, false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return false, false
	}
	const limit = 8 << 20
	if stat.Size() > limit {
		if _, err = f.Seek(-limit, 2); err != nil {
			return false, false
		}
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), limit)
	for scan.Scan() {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Item struct {
					Type     string `json:"type"`
					ID       string `json:"id"`
					ExitCode *int   `json:"exit_code"`
				} `json:"item"`
			} `json:"payload"`
		}
		if json.Unmarshal(scan.Bytes(), &row) != nil {
			continue
		}
		if row.Type == "event_msg" && row.Payload.Type == "item_completed" && row.Payload.Item.Type == "CommandExecution" && row.Payload.Item.ID == in.ToolUseID && row.Payload.Item.ExitCode != nil {
			return *row.Payload.Item.ExitCode == 0, true
		}
	}
	return false, false
}
