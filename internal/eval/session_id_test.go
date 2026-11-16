package eval

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestNewSessionUUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := NewSessionUUID(), NewSessionUUID()
	if !re.MatchString(a) || a == b {
		t.Fatalf("UUID v4 sai: %q %q", a, b)
	}
}

func TestClaudeArgsSessionID(t *testing.T) {
	a := &ClaudeAgent{}
	a.Cfg.PermissionMode = "acceptEdits"
	def := strings.Join(a.ArgsFor(AgentRequest{}), " ")
	if !strings.Contains(def, "--no-session-persistence") || strings.Contains(def, "--session-id") {
		t.Fatalf("lượt eval phải giữ --no-session-persistence và không có --session-id: %s", def)
	}
	got := strings.Join(a.ArgsFor(AgentRequest{SessionID: "abc"}), " ")
	if strings.Contains(got, "--no-session-persistence") || !strings.Contains(got, "--session-id abc") {
		t.Fatalf("bước workflow: bỏ --no-session-persistence, thêm --session-id: %s", got)
	}
	if !strings.Contains(got, "-p --output-format json --strict-mcp-config") {
		t.Fatalf("các đối số khác không đổi: %s", got)
	}
}

func TestParseClaudeSessionAndNoResult(t *testing.T) {
	var r AgentResult
	r.SessionID = "own"
	parseClaudeJSON([]byte(`{"type":"result","session_id":"from-json","total_cost_usd":1}`), &r)
	if r.SessionID != "from-json" || r.NoResult {
		t.Fatalf("%+v", r)
	}
	var e AgentResult
	parseClaudeJSON([]byte("segfault"), &e)
	if !e.NoResult {
		t.Fatal("không có đối tượng result phải đặt NoResult")
	}
}

func TestCodexThreadID(t *testing.T) {
	var r AgentResult
	parseCodexJSONL([]byte(codexSample), &r)
	if r.SessionID != "01a1c35f-6ec6-78e1-9ff0-14c28dcf01a5" || r.NoResult {
		t.Fatalf("%+v", r)
	}
	b, err := os.ReadFile("testdata/codex-0.167/citation-approved.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var r2 AgentResult
	parseCodexJSONL(b, &r2)
	if r2.SessionID != "01a1c5db-43b7-7162-9761-71ac7be405e1" {
		t.Fatalf("thread_id: %q", r2.SessionID)
	}
	var cut AgentResult
	parseCodexJSONL([]byte(`{"type":"thread.started","thread_id":"t9"}`+"\n"), &cut)
	if cut.SessionID != "t9" || !cut.NoResult {
		t.Fatalf("luồng cụt: %+v", cut)
	}
}

func TestCodexInfra(t *testing.T) {
	for _, tc := range []struct {
		msg   string
		infra bool
		quota bool
	}{
		{"codex báo lỗi: unexpected status 429 Too Many Requests", true, true},
		{"codex báo lỗi: You've hit your usage limit", true, true},
		{"codex báo lỗi: unexpected status 503 Service Unavailable", true, false},
		{"codex báo lỗi: Overloaded", true, false},
		{"codex báo lỗi: ran 5000 tokens", false, false},
		{"codex báo lỗi: patch failed", false, false},
		{"codex báo lỗi: exceeded 512 tokens", false, false},
		{"codex báo lỗi: failed at line 503", false, false},
		{"codex báo lỗi: line 429 invalid", false, false},
		{"codex báo lỗi: HTTP 502 Bad Gateway", true, false},
		{"codex báo lỗi: error: 500", true, false},
		{"codex báo lỗi: stream error: 503.", true, false},
		{"codex báo lỗi: rate_limit_error", true, true},
		{"codex báo lỗi: Rate limit exceeded", true, true},
		{"codex báo lỗi: quota exceeded", true, true},
		{"codex báo lỗi: test đỏ ở internal/quota", false, false},
		{"codex báo lỗi: chưa xong rate limiter", false, false},
		{"codex báo lỗi: error: 500 dòng log bị cắt", false, false},
		{"codex báo lỗi: error: 429 dòng log bị cắt", false, false},
	} {
		reason, q := codexInfra(tc.msg)
		if (reason != "") != tc.infra || q != tc.quota {
			t.Errorf("%q: infra=%q quota=%v", tc.msg, reason, q)
		}
	}
}

func TestClassifyInfraMessage(t *testing.T) {
	for msg, infra := range map[string]bool{
		"API Error: 529 overloaded": true, "API Error: 500 internal server error": true, "429 Too Many Requests": true,
		"You've hit your 5-hour limit": true, "test đỏ ở TestFoo": false, "đọc 512 tokens": false,
		"Claude usage limit reached": true, "Rate limit exceeded": true, "rate_limit_error": true,
		"test đỏ ở internal/quota": false, "chưa xong rate limiter": false, "error: 500 dòng log bị cắt": false,
		"cần thêm quota cho tác vụ": false, "đổi usage limit thành cấu hình": false,
	} {
		if reason, _ := ClassifyInfraMessage(msg); (reason != "") != infra {
			t.Errorf("%q: reason=%q, muốn infra=%v", msg, reason, infra)
		}
	}
	if _, quota := ClassifyInfraMessage("429 Too Many Requests"); !quota {
		t.Error("429 phải là QuotaHit")
	}
}
