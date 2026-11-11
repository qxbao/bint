package hcomp

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Giới hạn độ dài (chống thành phần phình to làm tốn ngữ cảnh).
const (
	MaxMessageRunes  = 600
	MaxGuidanceRunes = 6000
)

// Check kiểm tra một thành phần và trả về mọi vấn đề cùng lúc (nil nếu hợp lệ).
func Check(c Component) error {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	switch c.Kind {
	case KindGuidance, KindGuardrail, KindScript, KindStrategy:
	default:
		return fmt.Errorf("loại thành phần không hợp lệ %q", c.Kind)
	}
	if !ValidID(c.ID) {
		add("id %q không hợp lệ (chữ thường, số, '-' hoặc '_', tối đa 64 ký tự)", c.ID)
	}
	if err := ValidScope(c.Scope); err != nil {
		add("%v", err)
	}
	switch c.Status {
	case StatusCandidate, StatusVerified, StatusDeprecated:
	default:
		add("status %q: cần candidate, verified hoặc deprecated", c.Status)
	}
	switch c.Origin {
	case OriginHuman, OriginAgent:
	default:
		add("origin %q: cần human hoặc agent", c.Origin)
	}
	if c.Version < 1 {
		add("version phải ≥ 1")
	}
	switch c.Kind {
	case KindGuidance:
		if strings.TrimSpace(c.Body) == "" {
			add("guidance không có nội dung")
		}
		if utf8.RuneCountInString(c.Body) > MaxGuidanceRunes {
			add("guidance quá dài (> %d ký tự)", MaxGuidanceRunes)
		}
		for _, r := range c.Requires {
			if r != "mcp" {
				add("requires %q không hỗ trợ (chỉ mcp)", r)
			}
		}
	case KindGuardrail:
		checkGuardrail(c, add)
	case KindScript:
		if strings.TrimSpace(c.Description) == "" {
			add("script thiếu description (chỉ dẫn dùng nó nêu mô tả này)")
		}
		if len(c.Source) > 0 && !strings.HasPrefix(string(c.Source), "#!") {
			add("script .sh phải bắt đầu bằng dòng #! (shebang)")
		}
	case KindStrategy:
		checkStrategy(c, add)
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s %q: %s", c.Kind, c.ID, strings.Join(errs, "; "))
}

func checkGuardrail(c Component, add func(string, ...any)) {
	switch c.On {
	case OnPreEdit, OnPreBash, OnStop:
	default:
		add("on %q: cần pre_edit, pre_bash hoặc stop", c.On)
	}
	switch c.Action {
	case ActRemind, ActAsk, ActDeny:
	default:
		add("action %q: cần remind, ask hoặc deny", c.Action)
	}
	if strings.TrimSpace(c.Message) == "" {
		add("message không được rỗng")
	}
	if utf8.RuneCountInString(c.Message) > MaxMessageRunes {
		add("message quá dài (> %d ký tự)", MaxMessageRunes)
	}
	if c.When.Paths != "" && c.On != OnPreEdit {
		add("when.paths chỉ dùng với on: pre_edit")
	}
	if c.When.Command != "" {
		if c.On != OnPreBash {
			add("when.command chỉ dùng với on: pre_bash")
		}
		if err := ValidPattern(c.When.Command); err != nil {
			add("when.command: %v", err)
		}
	}
	if c.When.Edited != "" || c.When.Paths != "" {
		for _, g := range []string{c.When.Edited, c.When.Paths} {
			if g != "" && (strings.HasPrefix(g, "/") || strings.Contains(g, `\`) || strings.Contains(g, "..")) {
				add("glob %q không hợp lệ (đường dẫn tương đối, dấu '/')", g)
			}
		}
	}
	if r := c.Unless.Ran; r != "" && r != RanTest {
		if err := ValidPattern(r); err != nil {
			add("unless.ran: %v", err)
		}
	}
	if c.On == OnPreEdit && c.When.Paths == "" && c.Scope == ScopeGlobal && c.When.Edited == "" && c.Action != ActRemind {
		add("guardrail %s ở pre_edit toàn cục cần when.paths (nếu không sẽ chặn mọi lần sửa)", c.Action)
	}
	if c.On == OnPreBash && c.When.Command == "" && c.Action != ActRemind {
		add("guardrail %s ở pre_bash cần when.command (nếu không sẽ chặn mọi lệnh)", c.Action)
	}
	if c.When == (When{}) && c.Unless == (Unless{}) && c.Action == ActDeny {
		add("guardrail deny không điều kiện (when/unless rỗng) bị từ chối")
	}
}

func checkStrategy(c Component, add func(string, ...any)) {
	if c.ID != StrategyID {
		add("id của strategy phải là %q", StrategyID)
	}
	if c.Scope != ScopeGlobal {
		add("strategy chỉ có phạm vi global")
	}
	x := c.Context
	if x == nil || x.MaxTokens == nil && x.PathMaxItems == nil && x.Lessons == nil && x.Interfaces == nil {
		add("strategy cần ít nhất một khoá trong context")
		return
	}
	pos := func(name string, v *int) {
		if v != nil && *v <= 0 {
			add("context.%s phải > 0", name)
		}
	}
	pos("max_tokens", x.MaxTokens)
	pos("path_max_items", x.PathMaxItems)
	if l := x.Lessons; l != nil {
		if l.GlobalK != nil && *l.GlobalK < 0 {
			add("context.lessons.global_k phải ≥ 0")
		}
		if l.MaxTokens != nil && *l.MaxTokens < 0 {
			add("context.lessons.max_tokens phải ≥ 0")
		}
	}
}

// ValidPattern kiểm tra biểu thức chính quy dùng trong when.command / unless.ran: biên
// dịch được và không khớp chuỗi rỗng (nếu không guardrail sẽ khớp mọi lệnh).
func ValidPattern(p string) error {
	re, err := regexp.Compile(p)
	if err != nil {
		return fmt.Errorf("biểu thức chính quy %q không hợp lệ: %v", p, err)
	}
	if re.MatchString("") {
		return fmt.Errorf("biểu thức %q khớp chuỗi rỗng (sẽ khớp mọi lệnh)", p)
	}
	return nil
}

// Conflict mô tả quan hệ giữa một guardrail mới và guardrail sẵn có.
type Conflict struct {
	// Kind: "duplicate" (cùng điều kiện, cùng hành động) hoặc "contradiction" (cùng điều
	// kiện, khác hành động).
	Kind string
	With Component
}

func (c Conflict) String() string {
	if c.Kind == "duplicate" {
		return fmt.Sprintf("trùng guardrail %q (cùng điều kiện và hành động)", c.With.ID)
	}
	return fmt.Sprintf("mâu thuẫn với guardrail %q (cùng điều kiện nhưng hành động %s)", c.With.ID, c.With.Action)
}

// FindConflict tìm guardrail sẵn có (chưa deprecated, khác id) trùng hoặc mâu thuẫn với g:
// cùng on, when, unless và phạm vi. Hai guardrail chỉ khác message vẫn là trùng.
func FindConflict(existing []Component, g Component) *Conflict {
	if g.Kind != KindGuardrail {
		return nil
	}
	for _, e := range existing {
		if e.Kind != KindGuardrail || e.ID == g.ID || e.Status == StatusDeprecated {
			continue
		}
		if e.On != g.On || e.When != g.When || e.Unless != g.Unless || e.Scope != g.Scope {
			continue
		}
		if e.Action == g.Action {
			return &Conflict{Kind: "duplicate", With: e}
		}
		return &Conflict{Kind: "contradiction", With: e}
	}
	return nil
}

// Mức rủi ro do Bint tính (bảng H3), không do agent tự khai.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Risk tính rủi ro của thành phần: guidance và guardrail remind thấp; guardrail ask và
// strategy trung bình; guardrail deny và script (mã thực thi) cao.
func Risk(c Component) string {
	switch c.Kind {
	case KindScript:
		return RiskHigh
	case KindStrategy:
		return RiskMedium
	case KindGuardrail:
		switch c.Action {
		case ActDeny:
			return RiskHigh
		case ActAsk:
			return RiskMedium
		}
	}
	return RiskLow
}

// MaxRisk trả về mức cao hơn trong hai mức.
func MaxRisk(a, b string) string {
	rank := map[string]int{RiskLow: 0, RiskMedium: 1, RiskHigh: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
