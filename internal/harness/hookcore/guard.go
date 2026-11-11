package hookcore

import (
	"encoding/json"
	"slices"
	"strings"

	"bint/internal/detect"
	"bint/internal/hcomp"
)

// Guardrail kind dùng cho callback ghi nhận (spool ở phiên thật, nhật ký ở lượt eval).
const (
	NoteEligible = "eligible"
	NoteFired    = "fired"
	NoteFollowed = "followed"
	NoteBypass   = "bypass"
)

// maxCmdRecord giới hạn độ dài lệnh lưu trong trạng thái phiên.
const maxCmdRecord = 500

// guard thực thi guardrail của một phiên: đọc trạng thái phiên (JSONL), đánh giá bằng
// hcomp.Evaluate, ghi kết quả. Không mở SQLite, không gọi mô hình, không trả lỗi: lỗi ghi
// trạng thái bị bỏ qua (guardrail chỉ nhắc, không được làm hỏng phiên).
type guard struct {
	set      hcomp.Set
	path     string
	st       *hcomp.State
	preCaps  hcomp.Caps
	stopCaps hcomp.Caps
	isTest   func(string) bool
	note     func(kind string, d hcomp.Decision, on string)
	hasStop  bool
}

// newGuard trả về nil nếu tập thành phần không có guardrail nào (hook thoát ngay).
func newGuard(set hcomp.Set, path string, d Dialect, isTest func(string) bool, note func(string, hcomp.Decision, string)) *guard {
	if len(set.Guardrails()) == 0 {
		return nil
	}
	g := &guard{set: set, path: path, st: hcomp.ReadState(path), isTest: isTest, note: note}
	g.preCaps, g.stopCaps = d.GuardCaps(hcomp.OnPreBash), d.GuardCaps(hcomp.OnStop)
	g.hasStop = d.StopOutput != nil
	return g
}

// GuardCaps cho biết mức hành động guardrail mà harness thực thi được ở sự kiện on. Trước
// công cụ (pre_edit, pre_bash) theo PreToolDecisions/RemindPreTool; ở stop chỉ có "chặn kết
// thúc kèm lý do" (dùng cho cả remind và deny, ask hạ xuống remind) và chỉ khi harness có
// sự kiện Stop — không có thì không giả lập (mục 13.2).
func (d Dialect) GuardCaps(on string) hcomp.Caps {
	if on == hcomp.OnStop {
		return hcomp.Caps{Deny: d.StopOutput != nil, Remind: d.StopOutput != nil}
	}
	return hcomp.Caps{Deny: slices.Contains(d.PreToolDecisions, "deny"), Ask: slices.Contains(d.PreToolDecisions, "ask"),
		Remind: d.RemindPreTool}
}

// guardOut là kết quả guardrail trước công cụ.
type guardOut struct{ Remind, Ask, Deny string }

func tag(id, msg string) string { return "Bint guardrail [" + id + "]: " + msg }

func joinText(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// record nối các bản ghi vào tệp và vào trạng thái trong bộ nhớ.
func (g *guard) record(recs ...hcomp.Record) {
	_ = hcomp.Append(g.path, recs...)
	for _, r := range recs {
		g.st.Apply(r)
	}
}

// settle ghi các quyết định (kích hoạt, vượt trần, đủ điều kiện) và gọi callback.
func (g *guard) settle(ds []hcomp.Decision, on string) (fired []hcomp.Decision) {
	var recs []hcomp.Record
	for _, d := range ds {
		if d.Eligible && !g.st.Eligible[d.ID] {
			recs = append(recs, hcomp.Record{T: hcomp.RecEligible, ID: d.ID})
			g.st.Eligible[d.ID] = true // tránh ghi hai lần trong cùng một lượt đánh giá
			g.note(NoteEligible, d, on)
		}
		if d.Bypass {
			recs = append(recs, hcomp.Record{T: hcomp.RecBypass, ID: d.ID})
			g.note(NoteBypass, d, on)
		}
		if !d.Fired {
			continue
		}
		recs = append(recs, hcomp.Record{T: hcomp.RecFired, ID: d.ID, A: d.Action})
		if d.Block {
			recs = append(recs, hcomp.Record{T: hcomp.RecStopBlock, ID: d.ID})
		}
		g.note(NoteFired, d, on)
		fired = append(fired, d)
	}
	g.record(recs...)
	return fired
}

// pre đánh giá guardrail trước một lần sửa tệp (pre_edit) hoặc lệnh shell (pre_bash).
func (g *guard) pre(on string, paths []string, command string) guardOut {
	ds := hcomp.Evaluate(g.set.Guardrails(), hcomp.Event{On: on, Paths: paths, Command: command}, g.st, g.preCaps)
	var o guardOut
	for _, d := range g.settle(ds, on) {
		msg := tag(d.ID, d.Message)
		switch d.Action {
		case hcomp.ActDeny:
			o.Deny = joinText(o.Deny, msg)
		case hcomp.ActAsk:
			o.Ask = joinText(o.Ask, msg)
		default:
			o.Remind = joinText(o.Remind, msg)
		}
	}
	return o
}

// stop đánh giá guardrail khi agent định kết thúc; trả về lý do chặn (rỗng = cho kết thúc).
func (g *guard) stop(active bool) string {
	if !g.hasStop {
		return ""
	}
	ds := hcomp.Evaluate(g.set.Guardrails(), hcomp.Event{On: hcomp.OnStop, StopHookActive: active}, g.st, g.stopCaps)
	var msgs []string
	for _, d := range g.settle(ds, hcomp.OnStop) {
		if d.Block {
			msgs = append(msgs, tag(d.ID, d.Message))
		}
	}
	return joinText(msgs...)
}

// post ghi tệp đã sửa và lệnh đã chạy vào trạng thái phiên, rồi ghi nhận các guardrail
// đã được làm theo (điều kiện unless thoả sau khi kích hoạt).
func (g *guard) post(edits []string, cmd string, hasCmd bool) {
	var recs []hcomp.Record
	for _, p := range edits {
		recs = append(recs, hcomp.Record{T: hcomp.RecEdit, P: p})
	}
	if hasCmd {
		recs = append(recs, hcomp.Record{T: hcomp.RecCmd, C: detect.Redact(cmd, maxCmdRecord), Test: g.isTest(cmd)})
	}
	if len(recs) == 0 {
		return
	}
	g.record(recs...)
	var follow []hcomp.Record
	for _, id := range hcomp.Followed(g.set.Guardrails(), g.st) {
		follow = append(follow, hcomp.Record{T: hcomp.RecFollowed, ID: id})
		g.note(NoteFollowed, hcomp.Decision{ID: id}, "")
	}
	g.record(follow...)
}

// merge gộp kết quả guardrail với ngữ cảnh chèn (text) và yêu cầu hỏi của đường dẫn được
// bảo vệ (ask) thành đầu ra PreToolUse của harness. Rỗng nếu không có gì để nói.
func (o guardOut) merge(d Dialect, text, ask string) string {
	text = joinText(text, o.Remind)
	switch {
	case o.Deny != "" && d.PreToolDecide != nil:
		return d.PreToolDecide(text, "deny", joinText(o.Deny, ask))
	case o.Deny != "" || o.Ask != "":
		ask = joinText(o.Deny, o.Ask, ask)
	}
	if text == "" && ask == "" {
		return ""
	}
	return d.PreToolOutput(text, ask)
}

// ClaudeStylePreToolDecide dựng JSON PreToolUse với quyết định tường minh ("ask" | "deny"
// | "" chỉ chèn ngữ cảnh) cho Claude Code và Codex.
func ClaudeStylePreToolDecide(text, decision, reason string) string {
	out := map[string]any{"hookEventName": "PreToolUse"}
	if text != "" {
		out["additionalContext"] = text
	}
	if decision != "" {
		out["permissionDecision"] = decision
		out["permissionDecisionReason"] = reason
	}
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": out})
	return string(b)
}

// ClaudeStyleStopOutput dựng JSON chặn kết thúc phiên của Claude Code (sự kiện Stop):
// agent nhận lý do và làm tiếp.
func ClaudeStyleStopOutput(reason string) string {
	b, _ := json.Marshal(map[string]any{"decision": "block", "reason": reason})
	return string(b)
}

// domainGuidanceText trả về văn bản chèn cho guidance riêng (domain:<glob>) khớp rels mà
// phiên chưa nhận, và ghi chúng vào trạng thái phiên để không chèn lại. Lỗi ghi bị bỏ qua.
func domainGuidanceText(set hcomp.Set, statePath string, rels []string, o hcomp.RenderOpts) string {
	if len(set.Guidance()) == 0 {
		return ""
	}
	st := hcomp.ReadState(statePath)
	cs := set.DomainGuidance(rels, o, st.Guidance)
	if len(cs) == 0 {
		return ""
	}
	var recs []hcomp.Record
	for _, c := range cs {
		recs = append(recs, hcomp.Record{T: hcomp.RecGuidance, ID: c.ID})
	}
	_ = hcomp.Append(statePath, recs...)
	return hcomp.RenderDomain(cs)
}
