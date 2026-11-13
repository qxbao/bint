package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/task"
)

// Mọi bước agent của driver process nhận MCPConfig chỉ chứa server bint, cả bước chỉ đọc lẫn bước ghi.
func TestRunStepGrantsBintMCP(t *testing.T) {
	h := newHarness(t)
	r := h.runner()
	r.BintExe = "/opt/bint/bint"
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil || res.Status != task.StatusDone {
		t.Fatalf("b-do: %+v %v", res, err)
	}
	if len(h.reqs) != 3 || len(h.mcpBodies) != 3 {
		t.Fatalf("muốn 3 bước (b-req, b-feat, b-review) có MCP, được %d yêu cầu, %d tệp", len(h.reqs), len(h.mcpBodies))
	}
	runtimeDir := filepath.Dir(h.spool)
	for i, req := range h.reqs {
		step := h.calls[i]
		runID, taskID := "", ""
		for _, kv := range req.Env {
			if v, ok := strings.CutPrefix(kv, EnvRunID+"="); ok {
				runID = v
			}
			if v, ok := strings.CutPrefix(kv, EnvTaskID+"="); ok {
				taskID = v
			}
		}
		want := filepath.Join(runtimeDir, "mcp", runID+".json")
		if runID == "" || req.MCPConfig != want || !req.ExclusiveMCP {
			t.Fatalf("%s: MCPConfig=%q exclusive=%v, muốn %q", step, req.MCPConfig, req.ExclusiveMCP, want)
		}
		wantRO := step != StepFeat
		if req.ReadOnly != wantRO {
			t.Fatalf("%s: ReadOnly=%v", step, req.ReadOnly)
		}
		var f struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(h.mcpBodies[i], &f); err != nil {
			t.Fatalf("%s: %v\n%s", step, err, h.mcpBodies[i])
		}
		b, ok := f.MCPServers["bint"]
		if len(f.MCPServers) != 1 || !ok || b.Command != "/opt/bint/bint" || strings.Join(b.Args, " ") != "mcp serve --run-id "+runID+" --task-id "+taskID {
			t.Fatalf("%s: nội dung MCP: %s", step, h.mcpBodies[i])
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Fatalf("%s: tệp MCP phải bị xoá sau bước (%v)", step, err)
		}
	}
}

func TestWriteStepMCPConfig(t *testing.T) {
	dir := t.TempDir()
	p, err := writeStepMCPConfig(dir, "run1", "task1", "/x/bint")
	if err != nil || p != filepath.Join(dir, "mcp", "run1.json") {
		t.Fatalf("%q %v", p, err)
	}
	if fi, err := os.Stat(p); err != nil || (fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows") {
		t.Fatalf("quyền tệp: %v %v", fi, err)
	}
	if _, err := writeStepMCPConfig(dir, "run2", "task1", ""); err == nil {
		t.Fatal("thiếu exe phải lỗi")
	}
}

// Thiếu Spool (không có thư mục trạng thái): bước vẫn chạy, không cấp MCP.
func TestRunStepWithoutSpoolSkipsMCP(t *testing.T) {
	path, tools, cleanup := (&Runner{Env: &Env{}, Logf: func(string, ...any) {}}).stepMCP(&stepRun{child: "run1"}, "t1")
	cleanup()
	if path != "" || len(tools) != 0 {
		t.Fatalf("không có Spool thì không cấp MCP: %q", path)
	}
}

// Bản ghi lượt chạy tồn tại trong lúc agent chạy (đúng step, role, read_only) và mất sau bước; MCPTools theo role: bước
// chỉ đọc không có tool ghi vào cây làm việc, bước ghi có đủ.
func TestRunStepWritesBindingAndTools(t *testing.T) {
	h := newHarness(t)
	h.cfg.MCP = config.MCP{
		Services: map[string]config.MCPService{"specmatic": {}},
		Grants:   map[string][]string{"planner": {"specmatic.*"}, "implementer": {"specmatic.*"}, "reviewer": {"specmatic.*"}},
	}
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{"specmatic": {Command: "specmatic-mcp"}}}); err != nil {
		t.Fatal(err)
	}
	r := h.runner()
	r.BintExe = "/opt/bint/bint"
	if res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"}); err != nil || res.Status != task.StatusDone {
		t.Fatalf("b-do: %+v %v", res, err)
	}
	if len(h.bindings) != 3 {
		t.Fatalf("%d bản ghi lượt chạy, muốn 3", len(h.bindings))
	}
	runtimeDir := filepath.Join(filepath.Dir(h.spool), "runtime")
	roles := map[string]string{StepReq: "planner", StepFeat: "implementer", StepReview: "reviewer"}
	for i, step := range h.calls {
		var b mcpgw.RunBinding
		if err := json.Unmarshal(h.bindings[i], &b); err != nil {
			t.Fatalf("%s: không có bản ghi lúc agent chạy: %q", step, h.bindings[i])
		}
		if b.Step != step || b.Role != roles[step] || b.ReadOnly != (step != StepFeat) || b.TaskID == "" || b.RunID == "" {
			t.Errorf("%s: bản ghi %+v", step, b)
		}
		if _, err := mcpgw.ReadRunBinding(runtimeDir, b.RunID); err == nil {
			t.Errorf("%s: bản ghi phải bị xoá sau bước", step)
		}
		tools := h.reqs[i].MCPTools
		hasWrite := false
		for _, n := range tools {
			if n == "spec_manage_mock_server" || n == "spec_run_contract_test" {
				hasWrite = true
			}
		}
		if step == StepFeat && (!hasWrite || len(tools) != 4) {
			t.Errorf("%s: MCPTools=%v, muốn đủ 4 tool specmatic", step, tools)
		}
		if step != StepFeat && (hasWrite || len(tools) != 1 || tools[0] != "spec_backward_compatibility_check") {
			t.Errorf("%s: MCPTools=%v, muốn chỉ spec_backward_compatibility_check", step, tools)
		}
	}
}

func TestStepMCPToolsEmptyWhenNoServicesOrNotRegistered(t *testing.T) {
	h := newHarness(t)
	if got := StepMCPTools(h.cfg, "implementer", false); len(got) != 0 {
		t.Errorf("không khai mcp.services: %v", got)
	}
	cfg := h.cfg
	cfg.MCP = config.MCP{Services: map[string]config.MCPService{"codegraph": {}}, Grants: map[string][]string{"implementer": {"codegraph.*"}}}
	if got := StepMCPTools(cfg, "implementer", false); len(got) != 0 {
		t.Errorf("chưa đăng ký trên máy: %v", got)
	}
}

// Phát hiện M0 .5: tệp cấu hình MCP/bản ghi lượt chạy của bước bị giết được dọn khi đã quá hạn; tệp mới và tệp không phải
// .json giữ nguyên.
func TestPruneStaleJSON(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("old.json", 48*time.Hour)
	fresh := write("fresh.json", time.Hour)
	other := write("old.txt", 48*time.Hour)
	pruneStaleJSON(dir, staleStepFileAge(60))
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("tệp .json quá hạn phải bị xoá")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s phải còn: %v", p, err)
		}
	}
	pruneStaleJSON(filepath.Join(dir, "khong-co"), time.Hour) // thư mục không có: không lỗi
	if got := staleStepFileAge(60); got != 24*time.Hour {
		t.Errorf("tối thiểu 24 giờ, được %s", got)
	}
	if got := staleStepFileAge(24 * 60); got != 48*time.Hour {
		t.Errorf("gấp đôi timeout, được %s", got)
	}
}
