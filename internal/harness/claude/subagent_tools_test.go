package claude

import (
	"strings"
	"testing"
)

func TestSubagentToolCalls(t *testing.T) {
	p := writeTranscript(t,
		`không phải JSON`,
		`{"type":"user","message":{"role":"user","content":"đề bài"}}`,
		`{"type":"assistant","cwd":"/root","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"Write","input":{"file_path":"/root/a.txt","content":"x"}}]}}`,
		`{"type":"assistant","cwd":"/root","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"assistant","cwd":"/wt","message":{"content":[{"type":"tool_use","id":"t3","name":"Edit","input":{"file_path":"/wt/a.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"File created"},{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"PreToolUse:Bash hook error: Bint: Bash bị chặn"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"khac","content":"bỏ qua"}]}}`,
	)
	calls, err := SubagentToolCalls(p)
	if err != nil || len(calls) != 3 {
		t.Fatalf("%+v %v", calls, err)
	}
	if c := calls[0]; c.ID != "t1" || c.Name != "Write" || c.Cwd != "/root" || !c.HasResult || c.IsError || c.Result != "File created" ||
		c.Input["file_path"] != "/root/a.txt" {
		t.Fatalf("t1: %+v", c)
	}
	if c := calls[1]; !c.HasResult || !c.IsError || !strings.Contains(c.Result, "hook error") {
		t.Fatalf("t2: %+v", c)
	}
	if c := calls[2]; c.HasResult || c.Cwd != "/wt" {
		t.Fatalf("t3 chưa có kết quả: %+v", c)
	}
}

func TestSubagentToolCallsErrors(t *testing.T) {
	if _, err := SubagentToolCalls("/khong/co/tep.jsonl"); err == nil {
		t.Fatal("tệp mất phải lỗi")
	}
	if _, err := SubagentToolCalls(writeTranscript(t, "", "rác", "{hỏng")); err == nil {
		t.Fatal("không có dòng hợp lệ phải lỗi")
	}
	// Có dòng hợp lệ nhưng không có lời gọi công cụ: không lỗi, danh sách rỗng.
	calls, err := SubagentToolCalls(writeTranscript(t, `{"type":"assistant","message":{"content":"chỉ văn bản"}}`))
	if err != nil || len(calls) != 0 {
		t.Fatalf("%+v %v", calls, err)
	}
}
