package eval

import (
	"reflect"
	"slices"
	"testing"

	"bint/internal/config"
)

func TestArgsForSkills(t *testing.T) {
	a := &ClaudeAgent{Cfg: config.EvalAgent{PermissionMode: "acceptEdits", AllowedTools: []string{"Read", "Bash"}, SettingSources: "project,local"}}
	plain := a.ArgsFor(AgentRequest{})
	if !slices.Contains(plain, "json") || slices.Contains(plain, "--verbose") || slices.Contains(plain, "Read,Bash,Skill") {
		t.Fatalf("nhánh không skill phải giữ nguyên đối số: %v", plain)
	}
	if !reflect.DeepEqual(plain, a.Args()) {
		t.Fatal("Args() phải bằng ArgsFor rỗng")
	}
	with := a.ArgsFor(AgentRequest{HasSkills: true})
	for _, want := range []string{"stream-json", "--verbose", "Read,Bash,Skill"} {
		if !slices.Contains(with, want) {
			t.Errorf("thiếu %q trong %v", want, with)
		}
	}
	if slices.Contains(with, "json") {
		t.Errorf("không còn --output-format json: %v", with)
	}
	// Không thêm Skill hai lần khi đã cấu hình.
	a.Cfg.AllowedTools = []string{"Read", "Skill"}
	if got := a.ArgsFor(AgentRequest{HasSkills: true}); !slices.Contains(got, "Read,Skill") {
		t.Errorf("Skill bị lặp: %v", got)
	}
}

const streamSample = `{"type":"system","subtype":"init"}
{"type":"assistant","message":{"content":[{"type":"text","text":"ok"},{"type":"tool_use","name":"Skill","input":{"skill":"greet-zed"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"a"}},{"type":"tool_use","name":"Skill","input":{"skill":"greet-zed"}},{"type":"tool_use","name":"Skill","input":{"skill":"other"}}]}}
{"type":"result","subtype":"success","is_error":false,"num_turns":3,"total_cost_usd":0.0017,"result":"ZED-OK","usage":{"input_tokens":4,"output_tokens":9}}
`

func TestParseClaudeStream(t *testing.T) {
	var res AgentResult
	parseClaudeJSON([]byte(streamSample), &res)
	if res.NumTurns == nil || *res.NumTurns != 3 || res.CostUSD == nil || *res.CostUSD != 0.0017 || res.Text != "ZED-OK" {
		t.Fatalf("đọc chi phí/lượt từ stream-json hỏng: %+v", res)
	}
	if got := parseClaudeSkills([]byte(streamSample)); !reflect.DeepEqual(got, []string{"greet-zed", "other"}) {
		t.Errorf("skills = %v", got)
	}
	got := parseClaudeSkills([]byte(`{"type":"result","num_turns":1}`))
	if got == nil || len(got) != 0 {
		t.Errorf("đã đo nhưng không skill nào: muốn slice rỗng không nil, được %#v", got)
	}
}
