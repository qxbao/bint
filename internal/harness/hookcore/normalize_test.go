package hookcore

import "testing"

func TestNormalizeClaudeStyleSubagentFields(t *testing.T) {
	raw := `{"session_id":"s","hook_event_name":"SubagentStop","cwd":"/r","agent_id":"a1b2","agent_type":"bint-reviewer",` +
		`"agent_transcript_path":"/t/subagents/agent-a1b2.jsonl","transcript_path":"/t/main.jsonl"}`
	in, err := NormalizeClaudeStyle([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if in.Event != EvSubagentStop || in.AgentID != "a1b2" || in.AgentType != "bint-reviewer" ||
		in.AgentTranscriptPath != "/t/subagents/agent-a1b2.jsonl" || in.TranscriptPath != "/t/main.jsonl" {
		t.Fatalf("%+v", in)
	}
	pre, err := NormalizeClaudeStyle([]byte(`{"session_id":"s","hook_event_name":"PreToolUse","agent_id":"x"}`))
	if err != nil || pre.Event != EvPreTool || pre.AgentID != "x" {
		t.Fatalf("%+v %v", pre, err)
	}
}

func TestValidAgentID(t *testing.T) {
	for id, want := range map[string]bool{"a1b2": true, "agent-X_9": true, "": false, "../x": false, "a/b": false, "a.b": false, "a b": false} {
		if got := ValidAgentID(id); got != want {
			t.Errorf("%q: %v", id, got)
		}
	}
}
