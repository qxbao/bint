package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ToolCall là một lời gọi công cụ của subagent trong transcript, ghép với kết quả của nó (nếu đã có).
type ToolCall struct {
	// ID là tool_use_id; Name là tên công cụ ("Write", "Bash", ...); Input là tham số gọi.
	ID    string
	Name  string
	Input map[string]any
	// Cwd là thư mục hiện tại ghi ở dòng assistant chứa lời gọi ("" nếu transcript không có).
	Cwd string
	// HasResult: đã thấy tool_result của lời gọi. IsError: kết quả đánh dấu lỗi. Result: nội dung kết quả (đã cắt ngắn).
	HasResult bool
	IsError   bool
	Result    string
}

const maxToolResultText = 4096

// SubagentToolCalls đọc transcript JSONL của một subagent (agent_transcript_path của SubagentStop) và trả các lời gọi công cụ
// theo thứ tự xuất hiện, mỗi lời gọi ghép với tool_result cùng id. Bố cục (Claude Code 2.1.x, không công bố): dòng
// type=assistant có message.content[] khối tool_use{id,name,input} và trường cwd cấp dòng; dòng type=user có khối
// tool_result{tool_use_id,is_error,content} với content là chuỗi hoặc mảng khối text. Dòng hỏng bị bỏ qua. Lỗi khi không mở
// được tệp, hoặc khi đọc dở (kèm phần đã đọc được).
func SubagentToolCalls(path string) ([]ToolCall, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var calls []ToolCall
	idx := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	lines := 0
	for sc.Scan() {
		var l struct {
			Type    string `json:"type"`
			Cwd     string `json:"cwd"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		lines++
		if l.Type != "assistant" && l.Type != "user" {
			continue
		}
		var blocks []struct {
			Type      string          `json:"type"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     map[string]any  `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
		}
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			continue // content là chuỗi (lời nhắc/lời đáp thuần văn bản)
		}
		for _, b := range blocks {
			switch {
			case l.Type == "assistant" && b.Type == "tool_use" && b.ID != "":
				if _, dup := idx[b.ID]; dup {
					continue
				}
				idx[b.ID] = len(calls)
				calls = append(calls, ToolCall{ID: b.ID, Name: b.Name, Input: b.Input, Cwd: l.Cwd})
			case l.Type == "user" && b.Type == "tool_result" && b.ToolUseID != "":
				i, ok := idx[b.ToolUseID]
				if !ok {
					continue
				}
				calls[i].HasResult, calls[i].IsError, calls[i].Result = true, b.IsError, resultText(b.Content)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return calls, fmt.Errorf("đọc dở transcript: %w", err)
	}
	if lines == 0 {
		return nil, errors.New("transcript không có dòng JSON hợp lệ")
	}
	return calls, nil
}

// resultText rút nội dung văn bản của một tool_result (chuỗi hoặc mảng khối {type:text,text}), cắt ngắn.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.Text)
			sb.WriteByte('\n')
		}
		s = sb.String()
	}
	if len(s) > maxToolResultText {
		s = s[:maxToolResultText]
	}
	return s
}
