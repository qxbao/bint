package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/knowledge"
)

// Các nhánh so sánh (mục 12.4).
const (
	ArmNone    = "none"    // chỉ harness, không có AGENTS.md hay tri thức
	ArmMinimal = "minimal" // AGENTS.md ngắn chứa quy tắc bất thường
	ArmBint    = "bint"    // minimal + Context Engine (MCP) + chèn theo đường dẫn (hook)
	// ArmIdeal là nhánh tham chiếu lý tưởng chỉ dùng để chẩn đoán (mục 12.9): bộ truy
	// xuất được thay bằng tập mục "đúng" (phạm vi chồng lên tệp của lời giải thật).
	// Có lộ gián tiếp vị trí cần sửa nên chỉ là cận trên; không bao giờ là kết quả của Bint.
	ArmIdeal = "ideal"
)

// Arms là các nhánh hợp lệ.
var Arms = []string{ArmNone, ArmMinimal, ArmBint, ArmIdeal}

// Chế độ chống rò rỉ (mục 12.3).
const (
	LeakStrict  = "strict"
	LeakEconomy = "economy"
)

// ErrArmUnavailable: nhánh không chạy được cho tác vụ này (không phải lỗi hạ tầng).
var ErrArmUnavailable = errors.New("nhánh không khả dụng")

// ArmNotImplemented trả lỗi nếu nhánh không hợp lệ.
func ArmNotImplemented(arm string) error {
	for _, a := range Arms {
		if a == arm {
			return nil
		}
	}
	return fmt.Errorf("nhánh không hợp lệ %q (hỗ trợ: %s)", arm, strings.Join(Arms, ", "))
}

// ArmRequirement kiểm tra điều kiện cấu hình của nhánh: bint và ideal dựng từ cùng
// tập tri thức với minimal, nên cần eval.minimal_source: knowledge để phép so sánh
// chỉ khác nhau ở cách đưa tri thức tới agent.
func ArmRequirement(arm string, cfg config.Config) error {
	if (arm == ArmBint || arm == ArmIdeal) && cfg.Eval.MinimalSource != "knowledge" {
		return fmt.Errorf("nhánh %s cần eval.minimal_source: knowledge (cùng tập tri thức với minimal)", arm)
	}
	return nil
}

// ArmSetup mô tả ngữ cảnh đã đặt vào thư mục làm việc.
type ArmSetup struct {
	// ContextSource ghi nguồn gốc ngữ cảnh để kiểm toán, ví dụ
	// "<base>:.bint/evals/minimal.md#<sha256>". Rỗng ở nhánh none.
	ContextSource string
	Stripped      []string
	// Screening = true khi kết quả chỉ dùng để sàng lọc (chế độ economy).
	Screening bool
	// Nhánh bint: tệp cấu hình ngữ cảnh (BINT_EVAL_CONTEXT), cấu hình MCP, settings
	// có hook PreToolUse, và nhật ký truy xuất/trích dẫn.
	EvalConfig string
	MCPConfig  string
	Settings   string
	ContextLog string
	// Ideal là tập ID "đúng" của tác vụ (nhánh bint và ideal), để đo độ phủ truy xuất.
	Ideal []string
}

// knowledgeSet là tập mục mà nhánh minimal dùng cho một tác vụ.
type knowledgeSet struct {
	items      []*knowledge.Item
	src        string
	screening  bool
	candidates bool // gồm mục chưa duyệt (ảnh chụp bootstrap)
}

// prepareArm gỡ mọi ngữ cảnh agent khỏi thư mục làm việc rồi đặt ngữ cảnh của
// nhánh. Thư mục làm việc được commit sau đó để agent không thấy thay đổi nào.
//
// Nhánh minimal dùng tệp eval.minimal_context (minimal_source: file):
//   - strict: chỉ dùng phiên bản tồn tại TẠI commit gốc; không có thì nhánh không
//     khả dụng cho tác vụ đó (nội dung viết sau commit gốc có thể chứa lời giải).
//   - economy: dùng phiên bản hiện tại nếu không lộ lời giải theo CheckLeak.
//
// Với minimal_source: knowledge, nội dung dựng từ tri thức (xem knowledge.go). Nhánh
// bint và ideal dùng CÙNG tập tri thức đó; khác ở cách đưa tới agent.
func prepareArm(ctx context.Context, env Env, arm, mode string, c *Case, leakIn *LeakInput, dir, logDir string) (ArmSetup, error) {
	var s ArmSetup
	stripped, err := stripContext(dir)
	if err != nil {
		return s, err
	}
	s.Stripped = stripped
	writeAgents := func(content []byte) error {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), content, 0o644); err != nil {
			return err
		}
		// Claude Code đọc CLAUDE.md, không đọc AGENTS.md; nhập lại như `bint init --agents-md`.
		return os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644)
	}
	switch arm {
	case ArmNone:
		return s, nil
	case ArmMinimal:
		if env.Cfg.Eval.MinimalSource == "file" {
			content, src, screening, err := minimalContent(ctx, mode, env.RepoDir, env.Cfg.Eval.MinimalContext, c, leakIn)
			if err != nil {
				return s, err
			}
			s.ContextSource, s.Screening = src, screening
			return s, writeAgents(content)
		}
	case ArmBint, ArmIdeal:
		if err := ArmRequirement(arm, env.Cfg); err != nil {
			return s, err
		}
	default:
		return s, ArmNotImplemented(arm)
	}

	ks, err := knowledgeFor(ctx, env, mode, c, leakIn)
	if err != nil {
		return s, err
	}
	s.Screening = ks.screening
	opts := knowledge.RenderOptions{MaxItems: env.Cfg.Knowledge.AgentsMDMaxItems, IncludeCandidates: ks.candidates}
	content := renderMinimal(ks.items, opts)
	ideal := idealItems(ks.items, c, ks.candidates)
	for _, it := range ideal {
		s.Ideal = append(s.Ideal, it.ID)
	}
	switch arm {
	case ArmMinimal:
		s.ContextSource = fmt.Sprintf("%s#%s", ks.src, hash12(content))
		return s, writeAgents(content)
	case ArmIdeal:
		// Bộ truy xuất "hoàn hảo": mọi mục đúng được đặt sẵn trong AGENTS.md.
		e := &ctxengine.Engine{Items: ideal, MaxTokens: 1 << 20, PathMaxItems: len(ideal) + 1, PathIncludeCandidates: true}
		p := e.ForPaths(c.SourceFiles, nil)
		extra := ""
		if len(p.Entries) > 0 {
			extra = "\n## Tri thức liên quan tới phần mã của tác vụ\n\n" + p.Render()
		}
		full := append(content, []byte(extra)...)
		if leakIn != nil {
			if r := CheckLeak(string(full), *leakIn); r.Status == "fail" {
				return s, fmt.Errorf("%w: tri thức lý tưởng chứa mã của lời giải (%s)", ErrArmUnavailable, r.Findings[0].Detail)
			}
		}
		s.ContextSource = fmt.Sprintf("%s[ideal=%d]#%s", ks.src, len(ideal), hash12(full))
		return s, writeAgents(full)
	}

	// ArmBint: AGENTS.md giống minimal + hướng dẫn công cụ; Context Engine chạy ngoài
	// thư mục làm việc, đọc bản sao tri thức trong logDir.
	itemsDir := filepath.Join(logDir, "knowledge", "items")
	if err := os.MkdirAll(itemsDir, 0o700); err != nil {
		return s, err
	}
	for _, it := range ks.items {
		data, err := it.Marshal()
		if err != nil {
			return s, err
		}
		if err := os.WriteFile(filepath.Join(itemsDir, it.ID+".md"), data, 0o600); err != nil {
			return s, err
		}
	}
	s.ContextLog = filepath.Join(logDir, "context.jsonl")
	ec := ctxengine.EvalConfig{ItemsDir: itemsDir, Root: dir, Log: s.ContextLog, ExcludeEvalCase: c.ID,
		MaxTokens: env.Cfg.Context.MaxTokens, PathMaxItems: env.Cfg.Context.PathMaxItems, IncludeCandidates: ks.candidates}
	s.EvalConfig = filepath.Join(logDir, "context.json")
	if err := writeJSONFile(s.EvalConfig, ec); err != nil {
		return s, err
	}
	exe := env.BintExe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return s, err
		}
	}
	s.MCPConfig = filepath.Join(logDir, "mcp.json")
	if err := writeJSONFile(s.MCPConfig, map[string]any{"mcpServers": map[string]any{
		"bint": map[string]any{"command": exe, "args": []string{"mcp", "serve", "--eval-config", s.EvalConfig}},
	}}); err != nil {
		return s, err
	}
	s.Settings = filepath.Join(logDir, "settings.json")
	hook := map[string]any{"type": "command", "command": `"` + filepath.ToSlash(exe) + `" hook claude`, "timeout": 10}
	if err := writeJSONFile(s.Settings, map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"matcher": "Read|Edit|Write|MultiEdit|NotebookEdit", "hooks": []any{hook}}},
	}}); err != nil {
		return s, err
	}
	full := append(content, []byte(BintToolsNote)...)
	s.ContextSource = fmt.Sprintf("%s+engine#%s", ks.src, hash12(full))
	return s, writeAgents(full)
}

// BintToolsNote là phần hướng dẫn thêm vào AGENTS.md ở nhánh bint.
const BintToolsNote = `
## Công cụ tri thức (Bint)

Có MCP server ` + "`bint`" + `: gọi context_for_task với mô tả tác vụ ở đầu tác vụ, và
context_for_paths trước khi sửa tệp lạ. Khi áp dụng một mục tri thức, gọi
cite_knowledge với ID của mục. Trước khi đọc/sửa tệp, quy tắc khớp đường dẫn có thể
được chèn tự động.
`

func writeJSONFile(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// idealItems là tập mục "đúng" của tác vụ (mục 12.4): còn hiệu lực, có phạm vi
// đường dẫn cụ thể chồng lên tệp nguồn của lời giải thật.
func idealItems(items []*knowledge.Item, c *Case, candidates bool) []*knowledge.Item {
	var out []*knowledge.Item
	for _, it := range items {
		if !it.Active() || it.Scope.Global() || it.Origin.EvalCase == c.ID {
			continue
		}
		if it.Status == knowledge.StatusCandidate && !candidates {
			continue
		}
		if it.Origin.ThirdParty && it.Status != knowledge.StatusVerified {
			continue
		}
		for _, f := range c.SourceFiles {
			if it.Scope.MatchScope(f) {
				out = append(out, it)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func minimalContent(ctx context.Context, mode, repoDir, minimalPath string, c *Case, leakIn *LeakInput) ([]byte, string, bool, error) {
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(minimalPath)))
	if mode == LeakStrict {
		data, ok := showFile(ctx, repoDir, c.Source.Base, rel)
		if !ok || len(strings.TrimSpace(string(data))) == 0 {
			return nil, "", false, fmt.Errorf("%w: strict yêu cầu %s tồn tại tại commit gốc %s", ErrArmUnavailable, rel, short(c.Source.Base))
		}
		return data, fmt.Sprintf("%s:%s#%s", short(c.Source.Base), rel, hash12(data)), false, nil
	}
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil, "", true, fmt.Errorf("%w: chưa có %s trong repository", ErrArmUnavailable, rel)
	}
	if leakIn != nil {
		if r := CheckLeak(string(data), *leakIn); r.Status == "fail" {
			return nil, "", true, fmt.Errorf("%w: %s chứa mã của lời giải (%s)", ErrArmUnavailable, rel, r.Findings[0].Detail)
		}
	}
	return data, fmt.Sprintf("worktree:%s#%s", rel, hash12(data)), true, nil
}

func hash12(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:12]
}
