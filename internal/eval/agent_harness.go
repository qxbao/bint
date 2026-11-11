package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ArmChecker là giao diện tuỳ chọn: agent báo nhánh đánh giá nào nó không chạy
// được một cách trung thực (ví dụ thiếu MCP / hook theo đường dẫn). Chuỗi rỗng = chạy được.
type ArmChecker interface {
	ArmUnavailable(arm string) string
}

// TurnLimiter là giao diện tuỳ chọn: agent có áp được limits max_turns ở tầng
// harness không. Agent không cài giao diện này được coi là áp được.
type TurnLimiter interface {
	EnforcesTurns() bool
}

// ReadOnlyEnforcer là giao diện tuỳ chọn: agent mô tả cách nó giữ một bước chỉ đọc
// (ở tầng harness). Bint vẫn luôn kiểm tra lại cây làm việc sau bước chỉ đọc.
type ReadOnlyEnforcer interface {
	ReadOnlyMode() string
}

func binaryVersion(ctx context.Context, bin string) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	// "codex-cli 0.167.0", "1.5.3", "2.1.0 (Claude Code)": lấy trường đầu tiên trông như số phiên bản.
	for _, f := range strings.Fields(string(out)) {
		if len(f) > 0 && f[0] >= '0' && f[0] <= '9' && strings.Contains(f, ".") {
			return f
		}
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// Codex

// CodexAgent chạy Codex CLI không tương tác (`codex exec --json`), đề bài qua stdin.
// Bước có quyền ghi và bước chỉ đọc đều chạy trong sandbox workspace-write (lệnh
// `bint` của skill phải ghi được thư mục trạng thái, sandbox read-only chặn việc
// đó); tính chỉ đọc do Bint kiểm tra lại cây làm việc sau bước (mục 15.2).
type CodexAgent struct {
	Binary    string
	Model     string
	ExtraArgs []string
}

func (a *CodexAgent) Name() string           { return "codex" }
func (a *CodexAgent) RequestedModel() string { return a.Model }
func (a *CodexAgent) ReportsCost() bool      { return false }
func (a *CodexAgent) EnforcesTurns() bool    { return false }
func (a *CodexAgent) ReadOnlyMode() string {
	return "sandbox workspace-write + Bint kiểm tra cây làm việc"
}
func (a *CodexAgent) Version(ctx context.Context) string { return binaryVersion(ctx, a.bin()) }

func (a *CodexAgent) bin() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "codex"
}

// ArmUnavailable: nhánh bint trên Codex chỉ có MCP + AGENTS.md (không chèn theo đường
// dẫn, vì hook của Codex cần người duyệt trust); vẫn chạy được, ghi rõ trong báo cáo.
func (a *CodexAgent) ArmUnavailable(string) string { return "" }

// ArgsFor dựng đối số dòng lệnh cho một yêu cầu.
func (a *CodexAgent) ArgsFor(req AgentRequest) ([]string, error) {
	args := []string{"exec", "--json", "--sandbox", "workspace-write", "--skip-git-repo-check"}
	if req.Dir != "" {
		args = append(args, "--cd", req.Dir)
	}
	for _, d := range req.WritableDirs {
		args = append(args, "--add-dir", d)
	}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	if req.MCPConfig != "" {
		srv, err := readMCPServer(req.MCPConfig)
		if err != nil {
			return nil, err
		}
		args = append(args, "-c", "mcp_servers.bint.command="+tomlString(srv.Command),
			"-c", "mcp_servers.bint.args="+tomlStrings(srv.Args))
	}
	args = append(args, a.ExtraArgs...)
	return append(args, "-"), nil
}

func (a *CodexAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	args, err := a.ArgsFor(req)
	if err != nil {
		return AgentResult{ExitCode: -1, Error: err.Error()}
	}
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, a.bin(), args, req.Timeout, req.LogPath, cleanEnv(req.Env...), strings.NewReader(req.Prompt), &stdout)
	res := AgentResult{ExitCode: r.ExitCode, Duration: r.Duration, TimedOut: r.TimedOut, Model: a.Model}
	if r.Err != nil {
		res.Error = r.Err.Error()
	}
	parseCodexJSONL(stdout.Bytes(), &res)
	if res.Error == "" && r.ExitCode != 0 && !r.TimedOut {
		res.Error = "codex thoát với mã " + strconv.Itoa(r.ExitCode) + ": " + truncate(strings.TrimSpace(r.Tail), 300)
	}
	return res
}

// parseCodexJSONL đọc luồng sự kiện `codex exec --json`: câu trả lời cuối
// (item.completed / agent_message), token (turn.completed.usage), lỗi
// (turn.failed, error). Codex không báo chi phí và số lượt.
func parseCodexJSONL(out []byte, res *AgentResult) {
	var in, outTok int64
	haveUsage := false
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage *struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				res.Text = ev.Item.Text
			}
		case "turn.completed":
			if ev.Usage != nil {
				haveUsage = true
				in += ev.Usage.InputTokens
				outTok += ev.Usage.OutputTokens
			}
		case "turn.failed":
			if ev.Error != nil && ev.Error.Message != "" {
				res.Error = "codex báo lỗi: " + truncate(ev.Error.Message, 300)
			}
		case "error":
			if ev.Message != "" && res.Error == "" {
				res.Error = "codex báo lỗi: " + truncate(ev.Message, 300)
			}
		}
	}
	if haveUsage {
		res.InputTokens, res.OutputTokens = &in, &outTok
	}
}

type mcpServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// readMCPServer đọc mục "bint" của tệp cấu hình MCP định dạng mcpServers (do nhánh
// đánh giá bint sinh ra) để dịch sang định dạng của harness khác.
func readMCPServer(path string) (mcpServer, error) {
	var f struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return mcpServer{}, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return mcpServer{}, fmt.Errorf("%s: %w", path, err)
	}
	s, ok := f.MCPServers["bint"]
	if !ok || s.Command == "" {
		return mcpServer{}, fmt.Errorf("%s: thiếu mcpServers.bint", path)
	}
	return s, nil
}

// tomlString mã hoá chuỗi TOML cơ bản (cú pháp thoát của JSON hợp lệ trong TOML).
func tomlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func tomlStrings(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = tomlString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ---------------------------------------------------------------------------
// Antigravity

// AgyAgent chạy Antigravity CLI không tương tác (`agy --print=<đề bài> --output-format
// json`). Bước chỉ đọc chạy ở --mode plan, bước có quyền ghi ở --mode accept-edits.
// Antigravity không có cờ MCP và không chèn được ngữ cảnh theo đường dẫn, không báo
// chi phí và không có giới hạn số lượt.
type AgyAgent struct {
	Binary    string
	Model     string
	ExtraArgs []string
}

// MaxPromptBytes: đề bài đi qua một đối số dòng lệnh (giới hạn của hệ điều hành
// cho một đối số là 128 KiB trên Linux).
const MaxPromptBytes = 120 << 10

func (a *AgyAgent) Name() string                       { return "antigravity" }
func (a *AgyAgent) RequestedModel() string             { return a.Model }
func (a *AgyAgent) ReportsCost() bool                  { return false }
func (a *AgyAgent) EnforcesTurns() bool                { return false }
func (a *AgyAgent) ReadOnlyMode() string               { return "--mode plan + Bint kiểm tra cây làm việc" }
func (a *AgyAgent) Version(ctx context.Context) string { return binaryVersion(ctx, a.bin()) }

func (a *AgyAgent) bin() string {
	if a.Binary != "" {
		return a.Binary
	}
	if _, err := exec.LookPath("agy"); err != nil {
		if _, err := exec.LookPath("antigravity"); err == nil {
			return "antigravity"
		}
	}
	return "agy"
}

// ArmUnavailable: nhánh bint cần MCP hoặc chèn theo đường dẫn — Antigravity không có
// cả hai theo cách Bint cấu hình được cho một lượt, nên không đo nhánh này.
func (a *AgyAgent) ArmUnavailable(arm string) string {
	if arm == "bint" {
		return "antigravity: không cấu hình được MCP cho một lượt và hook không chèn được ngữ cảnh theo đường dẫn"
	}
	return ""
}

// ArgsFor dựng đối số dòng lệnh cho một yêu cầu.
func (a *AgyAgent) ArgsFor(req AgentRequest) ([]string, error) {
	if len(req.Prompt) > MaxPromptBytes {
		return nil, fmt.Errorf("đề bài %d byte vượt giới hạn %d byte của Antigravity (truyền qua đối số)", len(req.Prompt), MaxPromptBytes)
	}
	mode := "accept-edits"
	if req.ReadOnly {
		mode = "plan"
	}
	args := []string{"--output-format", "json", "--mode", mode}
	for _, d := range req.WritableDirs {
		args = append(args, "--add-dir", d)
	}
	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}
	if req.Timeout > 0 {
		args = append(args, "--print-timeout", strconv.Itoa(int(req.Timeout.Seconds()))+"s")
	}
	args = append(args, a.ExtraArgs...)
	// Đề bài gắn liền vào cờ: `-p <đề bài>` dễ bị hiểu nhầm khi đề bài bắt đầu bằng "-".
	return append(args, "--print="+req.Prompt), nil
}

func (a *AgyAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	args, err := a.ArgsFor(req)
	if err != nil {
		return AgentResult{ExitCode: -1, Error: err.Error()}
	}
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, a.bin(), args, req.Timeout, req.LogPath, cleanEnv(req.Env...), strings.NewReader(""), &stdout)
	res := AgentResult{ExitCode: r.ExitCode, Duration: r.Duration, TimedOut: r.TimedOut, Model: a.Model}
	if r.Err != nil {
		res.Error = r.Err.Error()
	}
	parseAgyJSON(stdout.Bytes(), &res)
	if res.Error == "" && r.ExitCode != 0 && !r.TimedOut {
		res.Error = "antigravity thoát với mã " + strconv.Itoa(r.ExitCode) + ": " + truncate(strings.TrimSpace(r.Tail), 300)
	}
	return res
}

// parseAgyJSON đọc kết quả `--output-format json` của Antigravity CLI.
func parseAgyJSON(out []byte, res *AgentResult) {
	var r struct {
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error"`
		NumTurns       *int64 `json:"num_turns"`
		FinishedReason string `json:"finished_reason"`
		Usage          *struct {
			InputTokens  *int64 `json:"input_tokens"`
			OutputTokens *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	found := false
	for _, cand := range jsonCandidates(out) {
		if json.Unmarshal(cand, &r) == nil && r.Status != "" {
			found = true
			break
		}
	}
	if !found {
		return
	}
	res.Text, res.NumTurns = r.Response, r.NumTurns
	if r.Usage != nil {
		res.InputTokens, res.OutputTokens = r.Usage.InputTokens, r.Usage.OutputTokens
	}
	if r.Status != "SUCCESS" || r.FinishedReason != "" {
		msg := r.Error
		if msg == "" {
			msg = r.Status + " " + r.FinishedReason
		}
		res.Error = "antigravity báo lỗi: " + truncate(strings.TrimSpace(msg), 300)
	}
}
