package app

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/testutil"
)

// writeGatewayLog ghi gateway.log với các dòng cho trước (đúng định dạng Daemon.logf).
func writeGatewayLog(t *testing.T, lines ...string) string {
	t.Helper()
	dir, err := mcpgw.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, mcpgw.LogFileName)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func logLine(i int, svc, msg string) string {
	ts := time.Date(2026, 11, 13, 4, 0, i, 0, time.UTC).Format(time.RFC3339)
	return fmt.Sprintf("%s %s (p%d): %s", ts, svc, i, msg)
}

// Không có daemon và không có gateway.log: không lỗi, có Note, danh sách rỗng (không nil).
func TestMCPGatewayLogNoDaemonNoFile(t *testing.T) {
	testutil.Isolate(t)
	r, err := MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Service != "codegraph" || r.Upstreams == nil || r.Lines == nil || len(r.Lines) != 0 || len(r.Upstreams) != 0 {
		t.Errorf("kết quả: %+v", r)
	}
	if !strings.Contains(r.Note, "gateway không chạy") || !strings.Contains(r.Note, "chưa có tệp gateway.log") {
		t.Errorf("Note: %q", r.Note)
	}
	if b, _ := json.Marshal(r); !strings.Contains(string(b), `"lines":[]`) || !strings.Contains(string(b), `"upstreams":[]`) {
		t.Errorf("JSON phải ra [] không phải null: %s", b)
	}
}

// Chỉ giữ dòng của đúng service (không nhầm tiền tố tên), đúng định dạng, và giới hạn --lines (lấy dòng mới nhất).
func TestMCPGatewayLogFiltersAndLimits(t *testing.T) {
	testutil.Isolate(t)
	writeGatewayLog(t,
		"rác không có thời gian",
		logLine(0, "codegraph", "một"),
		logLine(1, "codegraph-x", "tên có tiền tố giống"),
		logLine(2, "jira", "của service khác"),
		logLine(3, "codegraph", "hai"),
		"2026-11-13T04:00:04Z codegraph (p4): ba",
		"gateway chạy: PID 1", // dòng không có tiền tố service
		"khong-phai-gio codegraph (x): sai thời gian",
	)
	r, err := MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 3 || !strings.HasSuffix(r.Lines[0], "một") || !strings.HasSuffix(r.Lines[2], "ba") {
		t.Fatalf("dòng: %q", r.Lines)
	}
	for _, l := range r.Lines {
		if strings.Contains(l, "jira") || strings.Contains(l, "codegraph-x") {
			t.Errorf("lẫn dòng service khác: %q", l)
		}
	}
	r, _ = MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph", Lines: 2})
	if len(r.Lines) != 2 || !strings.HasSuffix(r.Lines[0], "hai") || !strings.HasSuffix(r.Lines[1], "ba") {
		t.Errorf("--lines 2 phải giữ dòng mới nhất: %q", r.Lines)
	}
	if strings.Contains(r.Note, "chưa có tệp") {
		t.Errorf("Note: %q", r.Note)
	}
}

// Chỉ đọc đuôi tệp: tệp lớn hơn giới hạn vẫn trả dòng mới nhất, dòng đầu bị cắt giữa chừng bị bỏ.
func TestMCPGatewayLogReadsOnlyTail(t *testing.T) {
	testutil.Isolate(t)
	var lines []string
	for i := 0; len(strings.Join(lines, "\n")) < mcpGatewayLogTailBytes*2; i++ {
		lines = append(lines, logLine(i%60, "codegraph", fmt.Sprintf("dòng số %d %s", i, strings.Repeat("x", 100))))
	}
	writeGatewayLog(t, lines...)
	r, err := MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph", Lines: mcpGatewayLogMaxLines})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) == 0 || len(r.Lines) >= len(lines) {
		t.Fatalf("số dòng %d trên tổng %d", len(r.Lines), len(lines))
	}
	if r.Lines[len(r.Lines)-1] != lines[len(lines)-1] {
		t.Errorf("thiếu dòng cuối: %q", r.Lines[len(r.Lines)-1])
	}
	for _, l := range r.Lines {
		if !slices.Contains(lines, l) {
			t.Fatalf("dòng bị cắt giữa chừng lọt vào: %q", l)
		}
	}
}

func TestMCPGatewayLogValidate(t *testing.T) {
	for _, o := range []MCPGatewayLogOptions{{}, {Service: "khong-co"}, {Service: "codegraph", Lines: -1}, {Service: "codegraph", Lines: mcpGatewayLogMaxLines + 1}} {
		if err := o.Validate(); !IsUsage(err) {
			t.Errorf("%+v: %v, muốn lỗi cách dùng", o, err)
		}
	}
	if err := (MCPGatewayLogOptions{Service: "codegraph"}).Validate(); err != nil {
		t.Error(err)
	}
	if d := MCPGatewayLogDefaults(config.Default()); d.Lines != 50 {
		t.Errorf("mặc định %+v", d)
	}
}

// Daemon thật (cùng tiến trình): trả phần upstream (rỗng khi chưa có phân vùng) cùng phần gateway.log, không Note.
func TestMCPGatewayLogWithDaemon(t *testing.T) {
	testutil.Isolate(t)
	dir, err := mcpgw.Dir()
	if err != nil {
		t.Fatal(err)
	}
	d, err := mcpgw.Start(mcpgw.Options{Dir: dir, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	writeGatewayLog(t, logLine(1, "codegraph", "đã chết"))
	r, err := MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Note != "" || len(r.Upstreams) != 0 || len(r.Lines) != 1 {
		t.Errorf("kết quả: %+v", r)
	}
}

// Daemon bản cũ (404 ở /admin/log): vẫn trả phần gateway.log, Note nói rõ.
func TestMCPGatewayLogOldDaemon404(t *testing.T) {
	testutil.Isolate(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	var port int
	fmt.Sscan(portStr, &port)
	dir, _ := mcpgw.Dir()
	writeGatewayLog(t, logLine(1, "codegraph", "còn thấy"))
	info, _ := json.Marshal(mcpgw.Info{PID: os.Getpid(), Port: port, Token: "t"})
	if err := os.WriteFile(filepath.Join(dir, mcpgw.InfoFileName), info, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 1 || !strings.Contains(r.Note, "bản cũ") || len(r.Upstreams) != 0 {
		t.Errorf("kết quả: %+v", r)
	}
	// Lỗi khác 404 cũng không làm hỏng phần gateway.log.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "hỏng", 500) })
	r, err = MCPGatewayLog(t.Context(), MCPGatewayLogOptions{Service: "codegraph"})
	if err != nil || len(r.Lines) != 1 || !strings.Contains(r.Note, "500") {
		t.Errorf("lỗi 500: %+v %v", r, err)
	}
}

// MissingEnv của mcp list: chỉ tên biến, đúng theo danh mục và đăng ký; http và service chưa đăng ký.
func TestMCPListMissingEnv(t *testing.T) {
	f := newMCPFx(t, "")
	ctx := t.Context()
	find := func(id string) MCPServiceStatus {
		t.Helper()
		l, err := MCPList(ctx, f.root, MCPListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range l {
			if s.ID == id {
				return s
			}
		}
		t.Fatalf("không có %s", id)
		return MCPServiceStatus{}
	}
	// jira chưa đăng ký: vẫn báo biến cần của danh mục.
	want := []string{"JIRA_MCP_PASSWORD", "JIRA_MCP_URL", "JIRA_MCP_USERNAME"}
	if got := find("jira").MissingEnv; !slices.Equal(got, want) {
		t.Errorf("jira chưa đăng ký: %v, muốn %v", got, want)
	}
	// Đặt đủ một phần.
	sp := filepath.Join(config.MachineDir(), "secrets.env")
	if err := os.MkdirAll(filepath.Dir(sp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sp, []byte("JIRA_MCP_URL=https://j\nJIRA_MCP_USERNAME=bob\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := find("jira").MissingEnv; !slices.Equal(got, []string{"JIRA_MCP_PASSWORD"}) {
		t.Errorf("thiếu một biến: %v", got)
	}
	b, _ := json.Marshal(find("jira"))
	if strings.Contains(string(b), "bob") || strings.Contains(string(b), "https://j") {
		t.Errorf("lộ giá trị: %s", b)
	}
	// Đăng ký thêm tên biến riêng (env-name): biến đó cũng được tính.
	if err := config.SaveMachineMCP(config.MachineMCP{Services: map[string]config.Registration{
		"jira": {Command: "x", EnvNames: []string{"JIRA_MCP_URL", "JIRA_MCP_USERNAME", "JIRA_MCP_PASSWORD", "EXTRA_TOKEN"}}}}); err != nil {
		t.Fatal(err)
	}
	if got := find("jira").MissingEnv; !slices.Equal(got, []string{"EXTRA_TOKEN", "JIRA_MCP_PASSWORD"}) {
		t.Errorf("env-name thêm: %v", got)
	}
	// Service http: không có credential gắn vào.
	for _, id := range []string{"deepwiki"} {
		if got := find(id).MissingEnv; len(got) != 0 {
			t.Errorf("%s: %v", id, got)
		}
	}
	// Đường dẫn secrets.env.
	if got, err := MCPSecretsFile(); err != nil || got != sp {
		t.Errorf("MCPSecretsFile = %q %v, muốn %q", got, err, sp)
	}
}
