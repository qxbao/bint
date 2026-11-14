package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Lệnh kiểm chứng không thấy biến định danh theo lượt/repository nhưng vẫn thấy biến đánh dấu agent.
func TestVerifyEnvStripsRunIdentityKeepsAgentMarkers(t *testing.T) {
	stripped := []string{"BINT_RUN_ID", "BINT_TASK_ID", "BINT_APPROVED_PATHS", "BINT_RUNNER_PID", "CLAUDE_CODE_SESSION_ID",
		"CLAUDE_CODE_SSE_PORT", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"}
	kept := []string{"BINT_AGENT", "CLAUDECODE", "AI_AGENT", "CODEX_SANDBOX", "GEMINI_CLI"}
	for _, k := range stripped {
		t.Setenv(k, "giá-trị")
	}
	for _, k := range kept {
		t.Setenv(k, "giá-trị")
	}
	cmd := ""
	for _, k := range stripped {
		cmd += "echo S:" + k + "=${" + k + "-unset}; "
	}
	for _, k := range kept {
		cmd += "echo K:" + k + "=${" + k + "-unset}; "
	}
	r := RunShell(context.Background(), t.TempDir(), cmd, time.Minute, "", VerifyEnv())
	if !r.OK() {
		t.Fatalf("%+v", r)
	}
	for _, k := range stripped {
		if !strings.Contains(r.Tail, "S:"+k+"=unset") {
			t.Errorf("%s không được lọt vào tiến trình con:\n%s", k, r.Tail)
		}
	}
	for _, k := range kept {
		if !strings.Contains(r.Tail, "K:"+k+"=giá-trị") {
			t.Errorf("%s phải được giữ:\n%s", k, r.Tail)
		}
	}
}
