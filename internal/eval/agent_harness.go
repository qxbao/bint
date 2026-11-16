package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"bint/internal/iso"
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
	// Profile là profile `bint iso` mặc định của agent (harnesses.codex.profile); nil = tài khoản thật.
	Profile *iso.Profile
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

// ArmUnavailable: nhánh bint dùng MCP + AGENTS.md và PreToolUse(apply_patch).
// Hook vẫn cần trust của Codex; tác vụ tự động phải chủ động chọn cách duyệt hook
// đã kiểm tra (ExtraArgs), adapter không tự bỏ qua trust của người dùng.
func (a *CodexAgent) ArmUnavailable(string) string { return "" }

// codexToolNameRE là khoá TOML trần hợp lệ: tên tool đặt trong `-c mcp_servers.bint.tools.<tên>.…` (không dấu chấm).
var codexToolNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// codexResumeMissingRe khớp thông báo của `codex exec resume <id>` khi thread không còn (đo trên codex-cli 0.167: "Error:
// thread/resume: thread/resume failed: no rollout found for thread id <uuid> (code -32600)"; bản cũ: "No saved session found").
var codexResumeMissingRe = regexp.MustCompile(`(?i)no rollout found for thread|no saved session found`)

// ArgsFor dựng đối số dòng lệnh cho một yêu cầu. req.ResumeSessionID khác rỗng: nối tiếp thread đó bằng
// `codex exec <cờ> resume <id> -`. Lệnh con `resume` KHÔNG nhận --sandbox/--cd/--add-dir (đo trên 0.167: "unexpected argument
// '--sandbox'"), nên toàn bộ cờ của bước gốc đứng ở cấp `exec`, TRƯỚC `resume`, đúng thứ tự như bước gốc (bảng nội tuyến tắt
// MCP vẫn đứng trước mọi -c mcp_servers.*): dãy -c của bước nối tiếp trùng bước gốc.
func (a *CodexAgent) ArgsFor(req AgentRequest) ([]string, error) {
	return a.argsFor(req, nil, nil)
}

// argsFor là ArgsFor với env đã dựng của lượt (nil = đọc CODEX_HOME từ req.Env rồi tiến trình) và bộ cờ Codex LB (lb) của
// profile linked. Có profile hiệu lực (req.Profile hoặc a.Profile) thì bảng MCP của bint mang env (BINT_STATE_DIR,
// BINT_CONFIG_HOME thật): Codex chỉ chuyển HOME/PATH/TMPDIR cho MCP stdio, thiếu bảng này `bint mcp` rơi về state giả.
func (a *CodexAgent) argsFor(req AgentRequest, env []string, lb []string) ([]string, error) {
	prof := req.Profile
	if prof == nil {
		prof = a.Profile
	}
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
		if req.ExclusiveMCP {
			// Phải đứng trước mọi -c mcp_servers.* khác (xem codexDisableMCPArgs).
			off, err := codexDisableMCPArgs(req, env)
			if err != nil {
				return nil, err
			}
			args = append(args, off...)
		}
		args = append(args, "-c", "mcp_servers.bint.command="+tomlString(srv.Command),
			"-c", "mcp_servers.bint.args="+tomlStrings(srv.Args))
		if prof != nil {
			benv, err := iso.BintMCPEnv(cleanEnv(req.Env...))
			if err != nil {
				return nil, fmt.Errorf("profile %s: %w", prof.ID(), err)
			}
			args = append(args, "-c", "mcp_servers.bint.env="+tomlInlineTable(benv))
		}
		args = append(args, "-c", `mcp_servers.bint.tools.cite_knowledge.approval_mode="approve"`)
		// Tool của service ngoài được cấp cho bước (tên chỉ gồm chữ, số, gạch dưới, gạch ngang: không có dấu chấm nên không
		// vướng việc -c tách khoá theo dấu chấm).
		for _, t := range req.MCPTools {
			if !codexToolNameRE.MatchString(t) {
				return nil, fmt.Errorf("tên tool MCP %q không dùng được trong khoá cấu hình Codex", t)
			}
			args = append(args, "-c", "mcp_servers.bint.tools."+t+`.approval_mode="approve"`)
		}
		if req.Settings != "" {
			// Settings là tín hiệu nhánh cần path hook. Dựng dialect Codex từ
			// chính executable Bint của MCP; không chạy lệnh hook Claude trong settings.
			cmd := "'" + strings.ReplaceAll(srv.Command, "'", "'\\''") + "' hook codex"
			if runtime.GOOS == "windows" {
				cmd = `"` + filepath.ToSlash(srv.Command) + `" hook codex`
			}
			hooks := `[{matcher="apply_patch",hooks=[{type="command",command=` + tomlString(cmd) + `,timeout=10}]}]`
			args = append(args, "-c", "hooks.PreToolUse="+hooks)
		}
	}
	// Cờ Codex LB ở cấp exec, trước ExtraArgs và trước `resume` (lệnh con resume không nhận -c riêng của nó).
	args = append(args, lb...)
	args = append(args, a.ExtraArgs...)
	if req.ResumeSessionID != "" {
		return append(args, "resume", req.ResumeSessionID, "-"), nil
	}
	return append(args, "-"), nil
}

func (a *CodexAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	senv, err := agentEnv(ctx, req, a.Profile)
	if err != nil {
		return profileFailure(err)
	}
	defer senv.End()
	args, err := a.argsFor(req, senv.Env, senv.LB)
	if err != nil {
		return AgentResult{ExitCode: -1, Error: err.Error()}
	}
	env := senv.Env
	if req.Arm != "" {
		for _, arg := range a.ExtraArgs {
			if arg == "--ignore-user-config" {
				return AgentResult{ExitCode: -1, Error: "Codex eval cô lập không dùng --ignore-user-config vì cờ đó bỏ cấu hình chặn user skill"}
			}
		}
		var cleanup func()
		env, cleanup, err = codexEvalProfile(req, env, len(senv.LB) > 0)
		if err != nil {
			return AgentResult{ExitCode: -1, Error: err.Error()}
		}
		defer cleanup()
		args = append([]string{"--no-daemon"}, args...)
	}
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, a.bin(), args, req.Timeout, req.LogPath, env, strings.NewReader(req.Prompt), &stdout)
	res := AgentResult{ExitCode: r.ExitCode, Duration: r.Duration, TimedOut: r.TimedOut, Model: a.Model, SessionID: req.ResumeSessionID}
	if r.Err != nil {
		res.Error = r.Err.Error()
	}
	// Lỗi Codex tự báo (turn.failed/error) cụ thể hơn mã thoát; lỗi toàn vẹn luồng
	// (thiếu turn.completed, dòng hỏng) thì kém cụ thể hơn mã thoát kèm phần cuối log.
	var integrity string
	if res.Error == "" {
		res.Error, integrity = codexStreamErrors(stdout.Bytes(), &res)
	} else {
		codexStreamErrors(stdout.Bytes(), &res)
	}
	if res.Error != "" && res.Infra == "" {
		res.Infra, res.QuotaHit = codexInfra(res.Error)
	}
	if res.Error == "" && r.ExitCode != 0 && !r.TimedOut {
		res.Error = "codex thoát với mã " + strconv.Itoa(r.ExitCode) + ": " + truncate(strings.TrimSpace(r.Tail), 300)
	}
	if res.Error == "" {
		res.Error = integrity
	}
	res.ResumeMissing = req.ResumeSessionID != "" && res.NoResult && r.ExitCode != 0 && codexResumeMissingRe.MatchString(r.Tail)
	if res.ResumeMissing {
		res.Infra, res.QuotaHit = "", false // thiếu thread không phải lỗi hạ tầng
	}
	return res
}

// parseCodexJSONL đọc luồng sự kiện `codex exec --json`: câu trả lời cuối
// (item.completed / agent_message), token (turn.completed.usage), lỗi
// (turn.failed, error). Codex không báo chi phí và số lượt.
func parseCodexJSONL(out []byte, res *AgentResult) {
	reported, integrity := codexStreamErrors(out, res)
	if res.Error == "" {
		res.Error = reported
	}
	if res.Error == "" {
		res.Error = integrity
	}
}

// codexStreamErrors điền Text/token vào res và trả riêng lỗi Codex tự báo (lỗi đầu tiên)
// và lỗi toàn vẹn luồng (thiếu turn.completed hoặc dòng không phải JSON); không ghi res.Error.
func codexStreamErrors(out []byte, res *AgentResult) (reported, integrity string) {
	var in, outTok int64
	haveUsage := false
	completed := false
	malformed := ""
	fail := func(message string) {
		if reported == "" {
			reported = message
		}
	}
	// Run already holds stdout in memory. Slice its lines directly: Scanner
	// both copied the buffer and silently stopped at large tool results.
	for len(out) > 0 {
		var line []byte
		line, out, _ = bytes.Cut(out, []byte{'\n'})
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage *struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
			Error   json.RawMessage `json:"error"` // đối tượng {message} hoặc chuỗi tuỳ phiên bản
			Message string          `json:"message"`
		}
		if !json.Valid(line) {
			if malformed == "" {
				malformed = "codex JSONL không hợp lệ: " + truncate(string(line), 120)
			}
			continue
		}
		if json.Unmarshal(line, &ev) != nil {
			continue // JSON hợp lệ nhưng khác lược đồ (sự kiện mới): bỏ qua như loại sự kiện lạ
		}
		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" && res.SessionID == "" {
				res.SessionID = ev.ThreadID
			}
		case "turn.started":
			completed = false
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				res.Text = ev.Item.Text
			}
		case "turn.completed":
			completed = true
			if ev.Usage != nil {
				haveUsage = true
				in += ev.Usage.InputTokens
				outTok += ev.Usage.OutputTokens
			}
		case "turn.failed":
			message := codexErrorMessage(ev.Error)
			if message == "" {
				message = "turn.failed"
			}
			fail("codex báo lỗi: " + truncate(message, 300))
		case "error":
			message := ev.Message
			if message == "" {
				message = codexErrorMessage(ev.Error)
			}
			if message == "" {
				message = "error"
			}
			fail("codex báo lỗi: " + truncate(message, 300))
		}
	}
	// Dòng hỏng chỉ đáng báo khi lượt không kết thúc: nó có thể là sự kiện kết thúc bị cắt.
	if !completed {
		res.NoResult = true
		integrity = "codex JSONL thiếu turn.completed"
		if malformed != "" {
			integrity = malformed
		}
	}
	if haveUsage {
		res.InputTokens, res.OutputTokens = &in, &outTok
	}
	return reported, integrity
}

// codexErrorMessage đọc trường error dạng {"message": ...} hoặc chuỗi.
func codexErrorMessage(raw json.RawMessage) string {
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
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

// tomlInlineTable dựng bảng nội tuyến TOML {k="v",…} theo thứ tự khoá (giá trị chuỗi).
func tomlInlineTable(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + tomlString(m[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
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
	senv, err := agentEnv(ctx, req, nil)
	if err != nil {
		return profileFailure(err)
	}
	defer senv.End()
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, a.bin(), args, req.Timeout, req.LogPath, senv.Env, strings.NewReader(""), &stdout)
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

// httpCodeTail là phần đứng sau mã HTTP trong thông báo lỗi thật: hết dòng, dấu câu, hoặc tên lỗi chuẩn đi kèm. Số đứng
// trước một từ thường ("error: 500 dòng log bị cắt", "error: 500 (dòng log)") không phải mã trạng thái. Thân JSON
// ("API Error: 503 {...}") theo sau mã là đuôi hợp lệ.
const httpCodeTail = `(?:\s*$|[.,;:)\]"'}]|\s+\{|\s+(?:internal|bad|service|gateway|server|overloaded|upstream|too|from|error)\b)`

// codexServerRe khớp lỗi API phía máy chủ trong thông báo Codex tự báo: mã 5xx sau "status/status code/http/api error/error"
// và theo sau là phần cuối của một thông báo lỗi (httpCodeTail), hoặc kèm tên lỗi chuẩn, hoặc quá tải; số 5xx đứng riêng
// ("512 tokens", "line 503", "error: 500 dòng") không tính.
var codexServerRe = regexp.MustCompile(`(?im)(?:status(?: code)?|http|code|api error|error)[\s:=]+5\d\d\b` + httpCodeTail +
	`|\brequest\s+(?:rejected|failed)\s*\(5\d\d\)|\b5\d\d\s+(?:internal server error|bad gateway|service unavailable|gateway timeout)|overloaded|internal server error|service unavailable|bad gateway`)

// codexRateRe khớp mã 429 trong thông báo Codex tự báo (cùng quy tắc đuôi như codexServerRe) hoặc "too many requests".
var codexRateRe = regexp.MustCompile(`(?im)(?:status(?: code)?|http|code|api error|error)[\s:=]+429\b` + httpCodeTail +
	`|\brequest\s+(?:rejected|failed)\s*\(429\)|too many requests`)

// codexInfra phân loại lỗi Codex tự báo (chỉ có văn bản, không có mã trạng thái riêng): hạn mức/429 → QuotaHit,
// 5xx/quá tải → lỗi hạ tầng; còn lại không phải hạ tầng.
func codexInfra(msg string) (string, bool) {
	if codexRateRe.MatchString(msg) {
		return "hết hạn mức sử dụng: " + truncate(strings.TrimSpace(msg), 160), true
	}
	if reason, quota := classifyInfra(0, "", msg); reason != "" {
		return reason, quota
	}
	if codexServerRe.MatchString(msg) {
		return "lỗi API: " + truncate(strings.TrimSpace(msg), 160), false
	}
	return "", false
}
