package claude

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/harness/hookcore"
)

// Prompt đầu của subagent (guardrail đường dẫn worktree, docs/isolation-subagent-plan.md mục 12.14): Claude Code ghi transcript
// của subagent ở <dirname(transcript_path)>/<session_id>/subagents/agent-<agent_id>.jsonl và dòng ĐẦU là message user chứa
// nguyên prompt. Bố cục này không được công bố (quan sát ở spike S1–S3, 2026-11-12): đổi thì SubagentPrompt trả ok=false và
// guardrail rơi về chính sách fail-closed có giới hạn (hookcore.worktreeDecision), không lỗi.

// maxSubagentPromptLine là trần kích thước dòng đầu (prompt thực tế vài chục KB; vượt trần thì coi như không đọc được).
const maxSubagentPromptLine = 4 << 20

// SubagentTranscriptPath dựng đường dẫn transcript của subagent từ dữ liệu hook ("" nếu thiếu hoặc session_id/agent_id có ký
// tự đường dẫn: chúng đi vào tên tệp).
func SubagentTranscriptPath(parentTranscript, sessionID, agentID string) string {
	if parentTranscript == "" || !hookcore.ValidAgentID(sessionID) || !hookcore.ValidAgentID(agentID) {
		return ""
	}
	return filepath.Join(filepath.Dir(parentTranscript), sessionID, "subagents", "agent-"+agentID+".jsonl")
}

// SubagentPrompt đọc prompt đầu của subagent đang gọi công cụ (hookcore.Dialect.SubagentPrompt).
func SubagentPrompt(hi hookcore.Input) (string, bool) {
	p := SubagentTranscriptPath(hi.TranscriptPath, hi.SessionID, hi.AgentID)
	if p == "" {
		return "", false
	}
	return readFirstUserPrompt(p, hi.AgentID)
}

// readFirstUserPrompt đọc đúng một dòng đầu của tệp (có trần) và rút nội dung message user.
func readFirstUserPrompt(p, agentID string) (string, bool) {
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, maxSubagentPromptLine+1), 64<<10).ReadBytes('\n')
	if err != nil && err != io.EOF {
		return "", false
	}
	if len(line) > maxSubagentPromptLine {
		return "", false
	}
	var rec struct {
		Type    string `json:"type"`
		AgentID string `json:"agentId"`
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return "", false
	}
	if rec.Type != "user" && rec.Message.Role != "user" || rec.AgentID != "" && rec.AgentID != agentID {
		return "", false
	}
	var s string
	if json.Unmarshal(rec.Message.Content, &s) == nil {
		return s, s != ""
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(rec.Message.Content, &parts) != nil {
		return "", false
	}
	var b strings.Builder
	for _, pt := range parts {
		if pt.Type == "text" {
			b.WriteString(pt.Text)
			b.WriteByte('\n')
		}
	}
	return b.String(), b.Len() > 0
}
