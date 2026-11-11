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
	"strconv"
	"strings"

	"bint/internal/config"
	"bint/internal/ctxengine"
	"bint/internal/knowledge"
	"bint/internal/skills"
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
	// ArmBase / ArmCandidate: đối chiếu một đề xuất tiến hoá (P6, mục 11.2). Cả hai đưa
	// tri thức tới agent theo cùng một cách (Variant.Delivery); base dùng tập tri thức
	// hiện có của tác vụ, candidate dùng tập đó sau khi áp đề xuất. Đánh giá loại bỏ
	// (bỏ một mục hoặc nhóm mục) là trường hợp đề xuất deprecate.
	ArmBase      = "base"
	ArmCandidate = "candidate"
)

// Arms là các nhánh hợp lệ.
var Arms = []string{ArmNone, ArmMinimal, ArmBint, ArmIdeal, ArmBase, ArmCandidate}

// Variant mô tả cặp nhánh base/candidate của một lần đối chiếu đề xuất.
type Variant struct {
	// Delivery: ArmMinimal (AGENTS.md) hoặc ArmBint (AGENTS.md + Context Engine).
	Delivery string
	// Label ghi vào nguồn ngữ cảnh để kiểm toán (ví dụ ID đề xuất).
	Label string
	// Candidate biến tập tri thức của tác vụ (đã lọc chống rò rỉ) thành tập ứng viên.
	// Không được sửa các mục đầu vào. Trả lỗi bọc ErrArmUnavailable khi đề xuất không
	// áp dụng được tại commit gốc của tác vụ.
	// Base biến tập tri thức của nhánh base (nil = dùng nguyên). Chỉ dùng khi kiểm tra hồi quy
	// một đề xuất đã áp dụng: base = tập TRƯỚC đề xuất (dựng lại), candidate = tập hiện tại.
	Base func(ctx context.Context, items []*knowledge.Item, c *Case, leak *LeakInput) ([]*knowledge.Item, error)
	// nil = đề xuất không đổi tri thức (đề xuất skill, P7).
	Candidate func(ctx context.Context, items []*knowledge.Item, c *Case, leak *LeakInput) ([]*knowledge.Item, error)
	// Skills trả về tập skill (tên → nội dung SKILL.md) đặt vào thư mục làm việc của một
	// nhánh (ArmBase hoặc ArmCandidate) cho tác vụ c; nil = không đặt skill nào. Đề xuất
	// skill (L2) đối chiếu hai tập skill, cùng một tập tri thức. Trả lỗi bọc
	// ErrArmUnavailable khi đề xuất không áp dụng được cho tác vụ.
	Skills func(ctx context.Context, arm string, c *Case, leak *LeakInput) (map[string][]byte, error)
}

// DeliveryArm trả về nhánh thực sự quyết định cách đưa ngữ cảnh (base/candidate →
// Variant.Delivery), để kiểm tra khả năng của harness.
func (e Env) DeliveryArm(arm string) string {
	if (arm == ArmBase || arm == ArmCandidate) && e.Variant != nil {
		return e.Variant.Delivery
	}
	return arm
}

// Chế độ chống rò rỉ (mục 12.3).
const (
	LeakStrict  = "strict"
	LeakEconomy = "economy"
)

// ErrArmUnavailable: nhánh không chạy được cho tác vụ này (không phải lỗi hạ tầng).
var ErrArmUnavailable = errors.New("nhánh không khả dụng")

// ContextVariant là chiến lược chọn/đưa ngữ cảnh của nhánh bint (P8: thử nghiệm chiến lược
// chọn ngữ cảnh). Viết sau tên nhánh: "bint@task=off+path=off+mcp=off+tokens=3000+items=3" (dấu + vì --arms tách theo dấu phẩy).
// Hai biến thể cùng đợt được so sánh ghép cặp như mọi nhánh.
type ContextVariant struct {
	NoTaskHook, NoPathHook, NoMCP bool
	MaxTokens, PathMaxItems       int
}

// SplitArm tách tên nhánh và biến thể ngữ cảnh. Chỉ nhánh bint nhận biến thể.
func SplitArm(arm string) (string, ContextVariant, error) {
	var v ContextVariant
	base, spec, ok := strings.Cut(arm, "@")
	if !ok {
		return arm, v, nil
	}
	if base != ArmBint {
		return base, v, fmt.Errorf("chỉ nhánh bint nhận biến thể ngữ cảnh, nhận %q", arm)
	}
	for _, kv := range strings.Split(spec, "+") {
		k, val, _ := strings.Cut(strings.TrimSpace(kv), "=")
		switch k {
		case "task", "path", "mcp":
			if val != "off" && val != "on" {
				return base, v, fmt.Errorf("biến thể %q: %s chỉ nhận on|off", arm, k)
			}
			off := val == "off"
			switch k {
			case "task":
				v.NoTaskHook = off
			case "path":
				v.NoPathHook = off
			default:
				v.NoMCP = off
			}
		case "tokens", "items":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return base, v, fmt.Errorf("biến thể %q: %s cần số nguyên dương", arm, k)
			}
			if k == "tokens" {
				v.MaxTokens = n
			} else {
				v.PathMaxItems = n
			}
		default:
			return base, v, fmt.Errorf("biến thể %q: khoá %q không hỗ trợ (task, path, mcp, tokens, items)", arm, k)
		}
	}
	return base, v, nil
}

// ArmNotImplemented trả lỗi nếu nhánh không hợp lệ.
func ArmNotImplemented(arm string) error {
	arm, _, err := SplitArm(arm)
	if err != nil {
		return err
	}
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
	arm, _, _ = SplitArm(arm)
	if arm == ArmBase || arm == ArmCandidate {
		return fmt.Errorf("nhánh %s chỉ dùng để đối chiếu đề xuất tiến hoá (`bint evolve evaluate`)", arm)
	}
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
	arm, cv, err := SplitArm(arm)
	if err != nil {
		return s, err
	}
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
	case ArmBase, ArmCandidate:
		if env.Variant == nil {
			return s, ArmRequirement(arm, env.Cfg)
		}
		if d := env.Variant.Delivery; d != ArmMinimal && d != ArmBint {
			return s, fmt.Errorf("Variant.Delivery phải là minimal hoặc bint, nhận %q", d)
		}
	default:
		return s, ArmNotImplemented(arm)
	}

	variant := arm == ArmBase || arm == ArmCandidate
	ks, err := knowledgeFor(ctx, env, mode, c, leakIn, variant)
	if err != nil {
		return s, err
	}
	if variant {
		// Đề xuất được đánh giá như thể đã được áp dụng: cùng tập tri thức, cùng cách đưa
		// tới agent; chỉ khác ở thay đổi của đề xuất.
		if arm == ArmBase && env.Variant.Base != nil {
			if ks.items, err = env.Variant.Base(ctx, ks.items, c, leakIn); err != nil {
				return s, err
			}
		}
		if arm == ArmCandidate && env.Variant.Candidate != nil {
			if ks.items, err = env.Variant.Candidate(ctx, ks.items, c, leakIn); err != nil {
				return s, err
			}
		}
		ks.src = fmt.Sprintf("%s{%s:%s}", ks.src, arm, env.Variant.Label)
		if env.Variant.Skills != nil {
			set, err := env.Variant.Skills(ctx, arm, c, leakIn)
			if err != nil {
				return s, err
			}
			desc, err := writeSkillSet(dir, set)
			if err != nil {
				return s, err
			}
			ks.src += desc
		}
		arm = env.Variant.Delivery
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
		// Bài học từ thất bại đi qua phần riêng (Pack.Lessons) với ngân sách riêng: đặt lớn để
		// không rơi mục nào đã được tính là ideal.
		e := &ctxengine.Engine{Items: ideal, MaxTokens: 1 << 20, PathMaxItems: len(ideal) + 1, PathIncludeCandidates: true,
			LessonMaxTokens: 1 << 20}
		p := e.ForPaths(c.SourceFiles, nil)
		extra := ""
		if len(p.Entries) > 0 || len(p.Lessons) > 0 {
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
		MaxTokens: env.Cfg.Context.MaxTokens, PathMaxItems: env.Cfg.Context.PathMaxItems, IncludeCandidates: ks.candidates,
		LessonGlobalK: env.Cfg.Context.Lessons.GlobalK, LessonMaxTokens: env.Cfg.Context.Lessons.MaxTokens}
	if cv.MaxTokens > 0 {
		ec.MaxTokens = cv.MaxTokens
	}
	if cv.PathMaxItems > 0 {
		ec.PathMaxItems = cv.PathMaxItems
	}
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
	if !cv.NoMCP {
		s.MCPConfig = filepath.Join(logDir, "mcp.json")
		if err := writeJSONFile(s.MCPConfig, map[string]any{"mcpServers": map[string]any{
			"bint": map[string]any{"command": exe, "args": []string{"mcp", "serve", "--eval-config", s.EvalConfig}},
		}}); err != nil {
			return s, err
		}
	}
	s.Settings = filepath.Join(logDir, "settings.json")
	hook := map[string]any{"type": "command", "command": `"` + filepath.ToSlash(exe) + `" hook claude`, "timeout": 10}
	hooks := map[string]any{}
	if !cv.NoPathHook {
		hooks["PreToolUse"] = []any{map[string]any{"matcher": "Read|Edit|Write|MultiEdit|NotebookEdit", "hooks": []any{hook}}}
	}
	if !cv.NoTaskHook {
		hooks["UserPromptSubmit"] = []any{map[string]any{"hooks": []any{hook}}}
	}
	if err := writeJSONFile(s.Settings, map[string]any{"hooks": hooks}); err != nil {
		return s, err
	}
	full := content
	if !cv.NoMCP {
		full = append(content, []byte(BintToolsNote)...)
	}
	s.ContextSource = fmt.Sprintf("%s+engine#%s", ks.src, hash12(full))
	return s, writeAgents(full)
}

// BintToolsNote là phần hướng dẫn thêm vào AGENTS.md ở nhánh bint.
const BintToolsNote = `
## Công cụ tri thức (Bint)

MCP server ` + "`bint`" + ` có tri thức về repository này (quy tắc, điểm dễ sai, hợp đồng hành vi).
Các công cụ có thể chưa được nạp sẵn: nạp bằng ToolSearch với truy vấn
"select:mcp__bint__context_for_task,mcp__bint__context_for_paths,mcp__bint__cite_knowledge".

1. Đầu tác vụ: gọi mcp__bint__context_for_task với mô tả tác vụ (và các tệp đã biết).
2. Trước khi sửa một tệp: gọi mcp__bint__context_for_paths với đường dẫn tệp.
3. Khi áp dụng một mục tri thức: gọi mcp__bint__cite_knowledge với ID của mục.

Trước khi đọc/sửa tệp, quy tắc khớp đường dẫn cũng có thể được chèn tự động.
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
		if !it.Active() || it.Scope.Global() || it.Origin.FromEvalCase(c.ID) {
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

// writeSkillSet đặt skill vào thư mục làm việc ở cả hai nơi harness đọc skill cấp dự án
// (.claude/skills và .agents/skills, đúng định dạng từng nơi) và trả về mô tả để kiểm toán.
func writeSkillSet(dir string, set map[string][]byte) (string, error) {
	var names []string
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	var parts []string
	for _, n := range names {
		sk := skills.Skill{Name: n, Content: set[n]}
		for _, t := range []skills.Target{skills.ClaudeTarget, skills.AgentsTarget} {
			p := filepath.Join(dir, filepath.FromSlash(t.Dir), n, "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(p, skills.ForTarget(sk, t).Content, 0o644); err != nil {
				return "", err
			}
		}
		parts = append(parts, n+"@"+sk.Hash())
	}
	if len(parts) == 0 {
		return "+skills[]", nil
	}
	return "+skills[" + strings.Join(parts, ",") + "]", nil
}

func hash12(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:12]
}
