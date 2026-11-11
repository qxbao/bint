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
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"bint/internal/config"
)

// AgentRequest là một lần giao tác vụ cho agent.
type AgentRequest struct {
	Dir     string
	Prompt  string
	Timeout time.Duration
	LogPath string
	CaseID  string
	Arm     string
	// Env là biến môi trường bổ sung cho tiến trình agent.
	Env []string
	// MCPConfig / Settings: tệp cấu hình MCP và settings bổ sung (nhánh bint).
	MCPConfig string
	Settings  string
	// ReadOnly: bước không được sửa cây làm việc (b-req, b-review); harness áp ở mức
	// nó hỗ trợ, Bint kiểm tra lại sau bước.
	ReadOnly bool
	// WritableDirs: thư mục ngoài repository mà agent cần ghi (thư mục trạng thái
	// Bint, cho lệnh `bint task ...`) — dùng cho harness có sandbox.
	WritableDirs []string
	// HasSkills: thư mục làm việc có tập skill cấp dự án (.claude/skills). Khi đó harness
	// Claude được cấp công cụ Skill và đầu ra được phân tích để biết skill nào được gọi.
	HasSkills bool
	// Trace: ghi vết công cụ của lượt (stream-json kèm --verbose) vào LogPath, để bản tóm
	// tắt thất bại (bint experience digest) đọc được chuỗi lệnh và tệp đã đọc/sửa. Log
	// kiểu cũ (một đối tượng JSON) vẫn được đọc chi phí như trước; chỉ thiếu vết.
	Trace bool
}

// AgentResult là những gì đo được từ một lần chạy agent. Con trỏ nil = harness
// không báo (không thay bằng 0).
type AgentResult struct {
	ExitCode     int
	Duration     time.Duration
	TimedOut     bool
	Error        string
	NumTurns     *int64
	InputTokens  *int64
	OutputTokens *int64
	CostUSD      *float64
	Model        string
	// Text là câu trả lời cuối của agent (trường result), nếu harness báo.
	Text string
	// SkillsActivated: tên skill agent đã gọi qua công cụ Skill (không trùng, theo thứ tự
	// xuất hiện). Chỉ harness báo tool_use mới điền; nil = không đo được, không phải 0.
	SkillsActivated []string
	// Infra khác rỗng: lượt hỏng vì hạ tầng (hết hạn mức, API lỗi) chứ không phải vì
	// agent làm sai; không được chấm như một lượt trượt. Giá trị là lý do ngắn.
	Infra string
	// QuotaHit: hết hạn mức sử dụng/rate limit; các lượt sau cũng sẽ hỏng nên đợt phải dừng.
	QuotaHit bool
}

// Agent chạy một tác vụ trong thư mục làm việc (adapter cấp 3: thực thi theo chương trình).
type Agent interface {
	Name() string
	Version(ctx context.Context) string
	Run(ctx context.Context, req AgentRequest) AgentResult
	// ReportsCost cho biết harness có báo chi phí không (để áp ngân sách).
	ReportsCost() bool
}

// ClaudeAgent chạy Claude Code ở chế độ không tương tác (`claude -p`).
type ClaudeAgent struct {
	Binary     string
	Cfg        config.EvalAgent
	Model      string   // ghi đè cfg.Model
	MaxCostUSD *float64 // limits.per_run.max_cost_usd
	Bare       bool     // --bare: không nạp CLAUDE.md/bộ nhớ cấp người dùng (cần ANTHROPIC_API_KEY)
	// NoTools: tắt MỌI công cụ bằng cờ tường minh (--tools ""), không dựa vào mặc định quyền của
	// Claude Code (Read/Glob/Grep có thể chạy không cần duyệt). Dùng cho agent chỉ suy luận trên
	// đầu vào đã lọc (reflector): không có công cụ nghĩa là không đọc được tệp nào khác.
	NoTools bool
}

// ModelNamer là agent cho biết model được yêu cầu cho lượt chạy ("" = mặc định của harness).
type ModelNamer interface {
	RequestedModel() string
}

func (a *ClaudeAgent) Name() string { return "claude" }

// RequestedModel: Model ghi đè, không có thì cfg.Model.
func (a *ClaudeAgent) RequestedModel() string {
	if a.Model == "" && a.Cfg.Model != nil {
		return *a.Cfg.Model
	}
	return a.Model
}
func (a *ClaudeAgent) ReportsCost() bool { return true }
func (a *ClaudeAgent) ReadOnlyMode() string {
	return "--disallowedTools Edit,Write,… + Bint kiểm tra cây làm việc"
}

func (a *ClaudeAgent) bin() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "claude"
}

// Version trả về phiên bản Claude Code, hoặc "" nếu không xác định được.
func (a *ClaudeAgent) Version(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, a.bin(), "--version").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// BintMCPTools là các công cụ MCP của Bint được cho phép ở nhánh bint.
var BintMCPTools = []string{"mcp__bint__context_for_task", "mcp__bint__context_for_paths", "mcp__bint__knowledge_search",
	"mcp__bint__knowledge_get", "mcp__bint__cite_knowledge", "mcp__bint__report_observation"}

// Args dựng đối số dòng lệnh (tách riêng để kiểm thử).
func (a *ClaudeAgent) Args() []string { return a.ArgsFor(AgentRequest{}) }

// ArgsFor dựng đối số cho một yêu cầu cụ thể (thêm MCP/settings ở nhánh bint).
func (a *ClaudeAgent) ArgsFor(req AgentRequest) []string {
	args := []string{"-p", "--output-format", "json", "--no-session-persistence", "--strict-mcp-config",
		"--permission-mode", a.Cfg.PermissionMode}
	tools := append([]string{}, a.Cfg.AllowedTools...)
	if req.HasSkills || req.Trace {
		// Cần tool_use để biết skill có được gọi không (và để bản tóm tắt thất bại đọc được
		// vết): stream-json (kèm --verbose) vẫn kết thúc bằng dòng type=result cùng trường với
		// json nên phần đọc chi phí không đổi.
		args[2] = "stream-json"
		args = append(args, "--verbose")
	}
	if req.HasSkills && !slices.Contains(tools, "Skill") {
		tools = append(tools, "Skill")
	}
	if req.MCPConfig != "" && !a.NoTools {
		args = append(args, "--mcp-config", req.MCPConfig)
		tools = append(tools, BintMCPTools...)
	}
	if req.Settings != "" {
		args = append(args, "--settings", req.Settings)
	}
	if a.NoTools {
		args = append(args, "--tools", "")
	} else if len(tools) > 0 {
		args = append(args, "--allowedTools", strings.Join(tools, ","))
	}
	if a.Cfg.SettingSources != "" {
		args = append(args, "--setting-sources", a.Cfg.SettingSources)
	}
	model := a.Model
	if model == "" && a.Cfg.Model != nil {
		model = *a.Cfg.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if a.MaxCostUSD != nil {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(*a.MaxCostUSD, 'f', -1, 64))
	}
	if a.Bare {
		args = append(args, "--bare")
	}
	return append(args, a.Cfg.ExtraArgs...)
}

// Run giao đề bài qua stdin và đọc kết quả JSON.
func (a *ClaudeAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, a.bin(), a.ArgsFor(req), req.Timeout, req.LogPath, cleanEnv(req.Env...), strings.NewReader(req.Prompt), &stdout)
	res := AgentResult{ExitCode: r.ExitCode, Duration: r.Duration, TimedOut: r.TimedOut}
	if r.Err != nil {
		res.Error = r.Err.Error()
	}
	parseClaudeJSON(stdout.Bytes(), &res)
	if req.HasSkills {
		res.SkillsActivated = parseClaudeSkills(stdout.Bytes())
	}
	if res.Error == "" && r.ExitCode != 0 && !r.TimedOut {
		res.Error = "agent thoát với mã " + strconv.Itoa(r.ExitCode) + ": " + truncate(strings.TrimSpace(r.Tail), 300)
	}
	return res
}

// parseClaudeJSON đọc kết quả `--output-format json` của Claude Code. Chấp nhận
// cả đầu ra có dòng log phía trước: lấy đối tượng JSON cuối cùng có type=result.
func parseClaudeJSON(out []byte, res *AgentResult) {
	type usage struct {
		InputTokens              *int64 `json:"input_tokens"`
		CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		OutputTokens             *int64 `json:"output_tokens"`
	}
	type modelUse struct {
		CostUSD float64 `json:"costUSD"`
	}
	var r struct {
		Type         string              `json:"type"`
		Subtype      string              `json:"subtype"`
		IsError      bool                `json:"is_error"`
		NumTurns     *int64              `json:"num_turns"`
		TotalCostUSD *float64            `json:"total_cost_usd"`
		Usage        *usage              `json:"usage"`
		ModelUsage   map[string]modelUse `json:"modelUsage"`
		Result       string              `json:"result"`
		APIStatus    int                 `json:"api_error_status"`
		Terminal     string              `json:"terminal_reason"`
	}
	found := false
	for _, cand := range jsonCandidates(out) {
		if json.Unmarshal(cand, &r) == nil && (r.Type == "result" || r.TotalCostUSD != nil || r.NumTurns != nil) {
			found = true
			break
		}
	}
	if !found {
		return
	}
	res.NumTurns = r.NumTurns
	res.CostUSD = r.TotalCostUSD
	res.Text = r.Result
	if u := r.Usage; u != nil {
		if u.InputTokens != nil {
			total := *u.InputTokens
			for _, p := range []*int64{u.CacheCreationInputTokens, u.CacheReadInputTokens} {
				if p != nil {
					total += *p
				}
			}
			res.InputTokens = &total
		}
		res.OutputTokens = u.OutputTokens
	}
	if len(r.ModelUsage) > 0 {
		names := make([]string, 0, len(r.ModelUsage))
		for k := range r.ModelUsage {
			names = append(names, k)
		}
		// Mô hình chính = mô hình tốn nhiều nhất; các mô hình phụ ghi sau.
		sort.Slice(names, func(i, j int) bool {
			ci, cj := r.ModelUsage[names[i]].CostUSD, r.ModelUsage[names[j]].CostUSD
			if ci != cj {
				return ci > cj
			}
			return names[i] < names[j]
		})
		res.Model = strings.Join(names, ",")
	}
	if r.IsError && res.Error == "" {
		res.Error = "agent báo lỗi: " + r.Subtype + " " + truncate(r.Result, 200)
	}
	if r.IsError {
		res.Infra, res.QuotaHit = classifyInfra(r.APIStatus, r.Terminal, r.Result)
	}
}

// quotaRe khớp thông báo hết hạn mức của Claude Code ("You've hit your 5-hour limit",
// "usage limit reached", ...).
var quotaRe = regexp.MustCompile(`(?i)hit your .{0,40}limit|usage limit|rate limit|quota`)

// classifyInfra phân loại lỗi agent báo: hạn mức (429 hoặc thông báo hết hạn mức) và lỗi
// API phía máy chủ là lỗi hạ tầng. Trả về lý do rỗng nếu là lỗi bình thường của agent.
func classifyInfra(apiStatus int, terminal, result string) (string, bool) {
	switch {
	case apiStatus == 429 || quotaRe.MatchString(result):
		return "hết hạn mức sử dụng: " + truncate(strings.TrimSpace(result), 160), true
	case apiStatus >= 500 || terminal == "api_error":
		return fmt.Sprintf("lỗi API (mã %d): %s", apiStatus, truncate(strings.TrimSpace(result), 160)), false
	}
	return "", false
}

// parseClaudeSkills đọc đầu ra `--output-format stream-json`: mỗi dòng type=assistant có
// thể chứa khối tool_use tên Skill, đầu vào {"skill": "<tên>"}. Trả về slice không nil
// (rỗng = đã đo, không skill nào được gọi) để phân biệt với "không đo được".
func parseClaudeSkills(out []byte) []string {
	names := []string{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Skill string `json:"skill"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "assistant" {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" && c.Name == "Skill" && c.Input.Skill != "" && !slices.Contains(names, c.Input.Skill) {
				names = append(names, c.Input.Skill)
			}
		}
	}
	return names
}

func jsonCandidates(out []byte) [][]byte {
	t := bytes.TrimSpace(out)
	cands := [][]byte{t}
	lines := bytes.Split(t, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		l := bytes.TrimSpace(lines[i])
		if len(l) > 0 && l[0] == '{' {
			cands = append(cands, l)
		}
	}
	return cands
}

// CommandAgent chạy một lệnh tuỳ chỉnh làm agent (ví dụ harness khác, hoặc bộ
// giả lập trong kiểm thử). Đề bài được truyền qua stdin, biến BINT_EVAL_PROMPT_FILE,
// BINT_EVAL_CASE và BINT_EVAL_ARM. Nếu stdout là JSON cùng định dạng Claude Code
// thì số liệu được đọc từ đó.
type CommandAgent struct {
	Command string
}

func (a *CommandAgent) Name() string                   { return "command" }
func (a *CommandAgent) Version(context.Context) string { return "" }
func (a *CommandAgent) ReportsCost() bool              { return false }

func (a *CommandAgent) Run(ctx context.Context, req AgentRequest) AgentResult {
	promptFile := filepath.Join(filepath.Dir(req.LogPath), "prompt.txt")
	os.MkdirAll(filepath.Dir(promptFile), 0o700)
	if err := os.WriteFile(promptFile, []byte(req.Prompt), 0o600); err != nil {
		return AgentResult{ExitCode: -1, Error: err.Error()}
	}
	extra := []string{"BINT_EVAL_PROMPT_FILE=" + promptFile, "BINT_EVAL_CASE=" + req.CaseID, "BINT_EVAL_ARM=" + req.Arm}
	if req.MCPConfig != "" {
		extra = append(extra, "BINT_EVAL_MCP_CONFIG="+req.MCPConfig)
	}
	env := cleanEnv(append(extra, req.Env...)...)
	prog, args := shellCommand(a.Command)
	var stdout bytes.Buffer
	r := runProcess(ctx, req.Dir, prog, args, req.Timeout, req.LogPath, env, strings.NewReader(req.Prompt), &stdout)
	res := AgentResult{ExitCode: r.ExitCode, Duration: r.Duration, TimedOut: r.TimedOut}
	if r.Err != nil {
		res.Error = r.Err.Error()
	}
	parseClaudeJSON(stdout.Bytes(), &res)
	if res.Error == "" && r.ExitCode != 0 && !r.TimedOut {
		res.Error = "agent thoát với mã " + strconv.Itoa(r.ExitCode)
	}
	return res
}
