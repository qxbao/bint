package workflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
		runID := ""
		for _, kv := range req.Env {
			if v, ok := strings.CutPrefix(kv, EnvRunID+"="); ok {
				runID = v
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
		if len(f.MCPServers) != 1 || !ok || b.Command != "/opt/bint/bint" || strings.Join(b.Args, " ") != "mcp serve" {
			t.Fatalf("%s: nội dung MCP: %s", step, h.mcpBodies[i])
		}
		if _, err := os.Stat(want); !os.IsNotExist(err) {
			t.Fatalf("%s: tệp MCP phải bị xoá sau bước (%v)", step, err)
		}
	}
}

func TestWriteStepMCPConfig(t *testing.T) {
	dir := t.TempDir()
	p, err := writeStepMCPConfig(dir, "run1", "/x/bint")
	if err != nil || p != filepath.Join(dir, "mcp", "run1.json") {
		t.Fatalf("%q %v", p, err)
	}
	if fi, err := os.Stat(p); err != nil || (fi.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows") {
		t.Fatalf("quyền tệp: %v %v", fi, err)
	}
	if _, err := writeStepMCPConfig(dir, "run2", ""); err == nil {
		t.Fatal("thiếu exe phải lỗi")
	}
}

// Thiếu Spool (không có thư mục trạng thái): bước vẫn chạy, không cấp MCP.
func TestRunStepWithoutSpoolSkipsMCP(t *testing.T) {
	path, cleanup := (&Runner{Env: &Env{}, Logf: func(string, ...any) {}}).stepMCPConfig("run1")
	cleanup()
	if path != "" {
		t.Fatalf("không có Spool thì không cấp MCP: %q", path)
	}
}
