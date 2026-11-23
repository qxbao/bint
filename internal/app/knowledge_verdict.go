package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/knowledge"
	"bint/internal/store"
	"bint/internal/ulid"
)

// Các nhận định của agent về một mục tri thức cần rà soát. `bint knowledge verdict` (agent lưu),
// `bint knowledge revalidate --budget` (Bint gọi agent) và màn Tri thức của TUI (người áp) dùng chung.
const (
	VerdictValid       = "valid"
	VerdictOutdated    = "outdated"
	VerdictFixEvidence = "fix-evidence"
	VerdictUnsure      = "unsure"
)

// VerdictJSONFormat là dạng JSON agent in ở cuối câu trả lời của `revalidate` (cùng trường với
// `bint knowledge verdict`).
const VerdictJSONFormat = `{"verdicts":[{"id":"<ID>","verdict":"valid|outdated|fix-evidence|unsure","reason":"<một câu, dẫn tệp:dòng>","evidence":{"path":"<tệp>","lines":"<a-b>","symbol":"<ký hiệu>"}}]}
("evidence" chỉ có khi verdict là fix-evidence.)`

// VerdictDefinitions giải thích từng nhận định, dùng chung cho prompt revalidate và hướng dẫn của audit.
const VerdictDefinitions = `- valid: tri thức vẫn đúng nguyên văn với mã hiện tại (chỉ làm mới fingerprint).
- outdated: mã đã đổi làm tri thức sai hoặc thừa.
- fix-evidence: tri thức còn đúng nhưng bằng chứng trỏ sai chỗ; đề xuất path/lines/symbol mới.
- unsure: không đủ căn cứ.
`

func validVerdict(v string) bool {
	switch v {
	case VerdictValid, VerdictOutdated, VerdictFixEvidence, VerdictUnsure:
		return true
	}
	return false
}

// ProposedEvidence là bằng chứng đề xuất của nhận định fix-evidence.
type ProposedEvidence struct {
	Path   string `json:"path,omitempty"`
	Lines  string `json:"lines,omitempty"`
	Symbol string `json:"symbol,omitempty"`
}

// UnmarshalJSON chấp nhận một đối tượng hoặc mảng đối tượng (lấy phần tử đầu), và
// "lines" là chuỗi hoặc số, vì agent không phải lúc nào cũng theo đúng một dạng.
func (p *ProposedEvidence) UnmarshalJSON(b []byte) error {
	type raw struct {
		Path   string          `json:"path"`
		Lines  json.RawMessage `json:"lines"`
		Symbol string          `json:"symbol"`
	}
	var one raw
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '[' {
		var many []raw
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		if len(many) > 0 {
			one = many[0]
		}
	} else if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	p.Path, p.Symbol = one.Path, one.Symbol
	p.Lines = strings.Trim(strings.TrimSpace(string(one.Lines)), `"`)
	if p.Lines == "null" {
		p.Lines = ""
	}
	return nil
}

func (p *ProposedEvidence) empty() bool {
	return p == nil || (p.Path == "" && p.Lines == "" && p.Symbol == "")
}

// needsReview: mục cần rà soát là mục stale, hoặc verified mà bằng chứng đã đổi (fingerprint lệch).
func needsReview(it *knowledge.Item, r knowledge.Result) bool {
	switch it.Status {
	case knowledge.StatusStale:
		return true
	case knowledge.StatusVerified:
		return (r.Has(knowledge.CodeFingerprint) || r.Has(knowledge.CodeEvidence) || r.Has(knowledge.CodeSymbol)) && !r.OK()
	}
	return false
}

// ItemReviewBlock mô tả một mục cho agent: nội dung và mã HIỆN TẠI của từng bằng chứng.
func ItemReviewBlock(it *knowledge.Item, src knowledge.Source) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== Mục %s (%s, %s) ===\nTiêu đề: %s\nPhạm vi: %s\nNội dung:\n%s\n", it.ID, it.Type, it.Status, it.Title,
		strings.Join(it.Scope.Paths, ", "), it.Body)
	if it.StatusReason != "" {
		fmt.Fprintf(&b, "Lý do lỗi thời: %s\n", it.StatusReason)
	}
	for _, e := range it.Evidence {
		fmt.Fprintf(&b, "\nBằng chứng %s — mã HIỆN TẠI:\n%s", e.String(), excerpt(e, src, 60))
	}
	return b.String()
}

// harnessFromEnv đoán tên agent đang chạy lệnh từ biến môi trường harness đặt.
func harnessFromEnv() string {
	for _, c := range []struct{ env, name string }{{"CLAUDECODE", "claude"}, {"CODEX_SANDBOX", "codex"},
		{"GEMINI_CLI", "gemini"}, {"BINT_AGENT", "agent"}, {"AI_AGENT", "agent"}} {
		if v := strings.TrimSpace(os.Getenv(c.env)); v != "" && v != "0" {
			return c.name
		}
	}
	return "manual"
}

// insertVerdict ghi một nhận định vào knowledge_revalidations (evidence_key = fingerprint hiện tại).
func insertVerdict(ctx context.Context, db *store.Store, it *knowledge.Item, src knowledge.Source, rv Revalidation, now time.Time) error {
	var proposed any
	if !rv.Evidence.empty() {
		b, _ := json.Marshal(rv.Evidence)
		proposed = string(b)
	}
	_, err := db.DB.ExecContext(ctx, `INSERT INTO knowledge_revalidations
		(revalidation_id, item_id, evidence_key, verdict, reason, harness, model, cost_usd, created_at, proposed_evidence)
		VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`,
		ulid.New(), it.ID, evidenceKey(it, src), rv.Verdict, rv.Reason, rv.Harness, rv.Model,
		nullCost(rv.Cost), store.FormatTime(now), proposed)
	return err
}

// ---- audit ----

// AuditItem là một mục cần rà soát kèm lý do và mã hiện tại của bằng chứng.
type AuditItem struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	Title   string   `json:"title"`
	Reasons []string `json:"reasons"`
	// Block là mô tả đầy đủ cho agent (ItemReviewBlock).
	Block string `json:"block"`
}

// AuditReport là kết quả `bint knowledge audit`.
type AuditReport struct {
	Items []AuditItem `json:"items"`
	// Relocated: mục chỉ dời dòng/chuyển tệp đã được tự áp (nội dung bằng chứng không đổi) nên không cần rà soát.
	Relocated []string `json:"relocated"`
}

// AuditKnowledge liệt kê mục cần rà soát (stale, verified có fingerprint lệch) và tự áp các mục chỉ dời
// dòng/chuyển tệp như `verify --apply`. Agent gọi được: không đổi trạng thái, chỉ cập nhật vị trí bằng chứng.
func AuditKnowledge(ctx context.Context, k *KnowledgeWS, refs []string) (AuditReport, error) {
	rep := AuditReport{Items: []AuditItem{}, Relocated: []string{}}
	all, _, err := k.Items()
	if err != nil {
		return rep, err
	}
	targets, err := selectItems(all, refs, func(it *knowledge.Item) bool { return it.Active() })
	if err != nil {
		return rep, err
	}
	src := k.Source()
	v := knowledge.Verifier{Src: src, All: all, FindMoved: true}
	w := k.writer("knowledge audit")
	for _, it := range targets {
		r := v.Verify(it)
		if r.ApplyRelocations(it) {
			if err := w.SaveItem(k.Store, it); err != nil {
				return rep, err
			}
			rep.Relocated = append(rep.Relocated, it.ID)
			r = v.Verify(it)
		}
		if !needsReview(it, r) {
			continue
		}
		ai := AuditItem{ID: it.ID, Type: it.Type, Status: it.Status, Title: it.Title, Block: ItemReviewBlock(it, src)}
		if it.StatusReason != "" {
			ai.Reasons = append(ai.Reasons, it.StatusReason)
		}
		for _, f := range r.Findings {
			if f.Level == knowledge.LevelError {
				ai.Reasons = append(ai.Reasons, f.Detail)
			}
		}
		rep.Items = append(rep.Items, ai)
	}
	return rep, nil
}

// ---- verdict ----

// VerdictOptions điều khiển `bint knowledge verdict <id>`.
type VerdictOptions struct {
	Ref     string
	Verdict string
	Reason  string
	// Path, Lines, Symbol: bằng chứng đề xuất (chỉ với fix-evidence).
	Path, Lines, Symbol string
	// Model: mô hình tạo ra nhận định (rỗng thì lấy BINT_AGENT_MODEL/ANTHROPIC_MODEL nếu có).
	Model string
	// Harness: rỗng thì đoán từ môi trường.
	Harness string
	Now     time.Time
}

// Validate kiểm tra ràng buộc mức giá trị.
func (o VerdictOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID mục")
	}
	if !validVerdict(o.Verdict) {
		return Usagef("--verdict phải là valid, outdated, fix-evidence hoặc unsure")
	}
	if strings.TrimSpace(o.Reason) == "" {
		return Usagef("cần --reason (một câu, dẫn tệp:dòng)")
	}
	pe := &ProposedEvidence{Path: o.Path, Lines: o.Lines, Symbol: o.Symbol}
	if o.Verdict == VerdictFixEvidence && pe.empty() {
		return Usagef("fix-evidence cần bằng chứng đề xuất (--path/--lines/--symbol)")
	}
	if o.Verdict != VerdictFixEvidence && !pe.empty() {
		return Usagef("--path/--lines/--symbol chỉ dùng với fix-evidence")
	}
	return nil
}

// RecordVerdict lưu nhận định của agent về một mục. Không đổi trạng thái hay tệp tri thức (agent gọi được);
// áp nhận định (approve/deprecate/sửa bằng chứng) là việc của người (ApplyVerdicts).
func RecordVerdict(ctx context.Context, k *KnowledgeWS, o VerdictOptions) (Revalidation, error) {
	var rv Revalidation
	if err := o.Validate(); err != nil {
		return rv, err
	}
	if !k.Paths.Initialized() {
		return rv, errors.New("chưa khởi tạo thư mục trạng thái của Bint: không lưu được nhận định")
	}
	all, _, err := k.Items()
	if err != nil {
		return rv, err
	}
	it, err := knowledge.Find(all, o.Ref)
	if err != nil {
		return rv, err
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	model := strings.TrimSpace(o.Model)
	if model == "" {
		for _, e := range []string{"BINT_AGENT_MODEL", "ANTHROPIC_MODEL"} {
			if model = strings.TrimSpace(os.Getenv(e)); model != "" {
				break
			}
		}
	}
	harness := strings.TrimSpace(o.Harness)
	if harness == "" {
		harness = harnessFromEnv()
	}
	rv = Revalidation{ID: it.ID, Verdict: o.Verdict, Reason: strings.TrimSpace(o.Reason), Harness: harness, Model: model}
	if o.Verdict == VerdictFixEvidence {
		rv.Evidence = &ProposedEvidence{Path: o.Path, Lines: o.Lines, Symbol: o.Symbol}
	}
	db, err := k.OpenStore(ctx)
	if err != nil {
		return rv, err
	}
	defer db.Close()
	return rv, insertVerdict(ctx, db, it, k.Source(), rv, o.Now)
}

// StoredVerdict là nhận định gần nhất của một mục.
type StoredVerdict struct {
	Revalidation
	At string `json:"at"`
	// Stale: mã nguồn của bằng chứng đã đổi kể từ lúc nhận định, nên không áp được nữa.
	Stale bool `json:"stale,omitempty"`
}

// LatestVerdicts trả nhận định gần nhất của mọi mục (theo ID).
func LatestVerdicts(ctx context.Context, k *KnowledgeWS) (map[string]StoredVerdict, error) {
	out := map[string]StoredVerdict{}
	if !k.Paths.Initialized() {
		return out, nil
	}
	db, err := k.OpenStore(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.DB.QueryContext(ctx, `SELECT item_id, evidence_key, verdict, COALESCE(reason, ''), COALESCE(harness, ''),
		COALESCE(model, ''), COALESCE(proposed_evidence, ''), created_at
		FROM knowledge_revalidations ORDER BY created_at DESC, revalidation_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := map[string]string{}
	for rows.Next() {
		var id, key, proposed string
		var sv StoredVerdict
		if err := rows.Scan(&id, &key, &sv.Verdict, &sv.Reason, &sv.Harness, &sv.Model, &proposed, &sv.At); err != nil {
			return nil, err
		}
		if _, ok := out[id]; ok {
			continue
		}
		sv.ID = id
		if proposed != "" {
			var pe ProposedEvidence
			if json.Unmarshal([]byte(proposed), &pe) == nil && !pe.empty() {
				sv.Evidence = &pe
			}
		}
		out[id], keys[id] = sv, key
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	all, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	src := k.Source()
	for _, it := range all {
		if sv, ok := out[it.ID]; ok {
			sv.Stale = evidenceKey(it, src) != keys[it.ID]
			out[it.ID] = sv
		}
	}
	return out, nil
}

// ---- apply ----

// VerdictApplyOptions điều khiển việc người áp nhận định theo lô.
type VerdictApplyOptions struct {
	// Refs rỗng = mọi mục có nhận định áp được.
	Refs []string
	Now  time.Time
}

// VerdictApplied là kết quả áp cho một mục.
type VerdictApplied struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
	Applied bool   `json:"applied"`
	// Detail: việc đã làm hoặc lý do bỏ qua/lỗi.
	Detail string `json:"detail"`
}

// ApplyVerdicts áp nhận định đã lưu (chỉ người): valid → làm mới fingerprint, outdated → deprecate,
// fix-evidence → sửa bằng chứng rồi làm mới. unsure và nhận định đã cũ (mã đổi tiếp) bị bỏ qua.
func ApplyVerdicts(ctx context.Context, k *KnowledgeWS, o VerdictApplyOptions) ([]VerdictApplied, error) {
	if err := RequireHuman("bint knowledge verdicts apply"); err != nil {
		return nil, err
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	latest, err := LatestVerdicts(ctx, k)
	if err != nil {
		return nil, err
	}
	all, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	targets, err := selectItems(all, o.Refs, func(it *knowledge.Item) bool {
		_, ok := latest[it.ID]
		return ok && it.Active() && (it.Status == knowledge.StatusStale || it.Status == knowledge.StatusVerified)
	})
	if err != nil {
		return nil, err
	}
	src := k.Source()
	w := k.writer("bint knowledge verdicts apply")
	var outs []VerdictApplied
	for _, it := range targets {
		sv, ok := latest[it.ID]
		out := VerdictApplied{ID: it.ID, Verdict: sv.Verdict}
		skip := func(why string) { out.Detail = why; outs = append(outs, out) }
		switch {
		case !ok:
			skip("chưa có nhận định")
			continue
		case sv.Verdict == VerdictUnsure:
			skip("nhận định unsure: người tự xem")
			continue
		case sv.Stale:
			skip("nhận định đã cũ (mã đổi sau nhận định): chạy lại audit")
			continue
		}
		var act ReviewAction
		var restore func()
		switch sv.Verdict {
		case VerdictValid:
			act = ReviewAction{To: knowledge.StatusVerified, Refresh: true, Reason: "nhận định " + sv.Verdict + ": " + sv.Reason}
		case VerdictOutdated:
			act = ReviewAction{To: knowledge.StatusDeprecated, Reason: "nhận định outdated: " + sv.Reason}
		case VerdictFixEvidence:
			orig := slices.Clone(it.Evidence)
			if err := fixEvidence(it, sv.Evidence, src, w, k); err != nil {
				skip(err.Error())
				continue
			}
			// fixEvidence lưu bằng chứng mới với fingerprint rỗng để bước làm mới (đọc lại tệp) điền; làm mới không qua thì trả bằng
			// chứng cũ, kẻo mục giữ verified với bằng chứng không fingerprint và không bao giờ bị phát hiện lỗi thời nữa.
			restore = func() {
				it.Evidence = orig
				_ = w.SaveItem(k.Store, it)
			}
			act = ReviewAction{To: knowledge.StatusVerified, Refresh: true, Reason: "sửa bằng chứng theo nhận định: " + sv.Reason}
		}
		act.Refs, act.Now = []string{it.ID}, o.Now
		res, err := ReviewKnowledge(ctx, k, act)
		if err != nil {
			if restore != nil {
				restore()
			}
			return outs, err
		}
		if len(res) == 1 && res[0].Err != nil {
			if restore != nil {
				restore()
			}
			skip(res[0].Err.Error())
			continue
		}
		out.Applied = true
		out.Detail = map[string]string{VerdictValid: "làm mới fingerprint", VerdictOutdated: "deprecate",
			VerdictFixEvidence: "sửa bằng chứng và làm mới fingerprint"}[sv.Verdict]
		outs = append(outs, out)
	}
	return outs, nil
}

// fixEvidence thay bằng chứng của it theo đề xuất (mục khớp đường dẫn, nếu không thì mục đầu tiên đang lỗi, nếu
// không thì mục đầu) và lưu tệp; fingerprint để trống cho bước làm mới điền. Kiểm trước rằng đề xuất băm được.
func fixEvidence(it *knowledge.Item, pe *ProposedEvidence, src knowledge.Source, w *repoWriter, k *KnowledgeWS) error {
	if pe.empty() || len(it.Evidence) == 0 {
		return errors.New("fix-evidence không có bằng chứng đề xuất hoặc mục không có bằng chứng để sửa")
	}
	idx := -1
	for i, e := range it.Evidence {
		if pe.Path != "" && e.Path == pe.Path {
			idx = i
			break
		}
	}
	if idx < 0 {
		all, _, _ := k.Items()
		v := knowledge.Verifier{Src: src, All: all}
		for _, f := range v.Verify(it).Findings {
			if f.Level == knowledge.LevelError && f.Evidence != nil && f.Evidence.Index >= 0 && f.Evidence.Index < len(it.Evidence) {
				idx = f.Evidence.Index
				break
			}
		}
	}
	if idx < 0 {
		idx = 0
	}
	ne := it.Evidence[idx]
	if pe.Path != "" {
		ne.Path = pe.Path
	}
	ne.Lines, ne.Symbol, ne.Fingerprint = pe.Lines, pe.Symbol, ""
	b, ok := src.Read(ne.Path)
	if !ok {
		return fmt.Errorf("đề xuất trỏ tới %s nhưng tệp không tồn tại", ne.Path)
	}
	if _, err := knowledge.Fingerprint(ne, b); err != nil {
		return fmt.Errorf("bằng chứng đề xuất không hợp lệ: %w", err)
	}
	it.Evidence[idx] = ne
	return w.SaveItem(k.Store, it)
}

// ---- verdicts (người xem và áp) ----

// VerdictsOptions: `bint knowledge verdicts [id...] [--apply]`.
type VerdictsOptions struct {
	Refs  []string `arg:"id" form:"suggest=ids"`
	Apply bool     `flag:"apply"`
}

func VerdictsDefaults(config.Config) VerdictsOptions { return VerdictsOptions{} }
func (o VerdictsOptions) Validate() error            { return nil }

// VerdictRow là một nhận định kèm mục tương ứng.
type VerdictRow struct {
	StoredVerdict
	Title  string `json:"title"`
	Type   string `json:"type"`
	Status string `json:"status"`
	// Actionable: áp được ngay (không unsure, không cũ, mục còn stale/verified).
	Actionable bool `json:"actionable"`
}

// VerdictsReport là kết quả `bint knowledge verdicts`.
type VerdictsReport struct {
	Rows    []VerdictRow     `json:"rows"`
	Applied []VerdictApplied `json:"applied,omitempty"`
}

// KnowledgeVerdicts liệt kê nhận định gần nhất của các mục còn hiệu lực; Apply thì người áp luôn
// (valid → làm mới fingerprint, outdated → deprecate, fix-evidence → sửa bằng chứng rồi làm mới).
func KnowledgeVerdicts(ctx context.Context, k *KnowledgeWS, o VerdictsOptions) (VerdictsReport, error) {
	rep := VerdictsReport{Rows: []VerdictRow{}}
	if o.Apply {
		applied, err := ApplyVerdicts(ctx, k, VerdictApplyOptions{Refs: o.Refs, Now: time.Now()})
		if err != nil {
			return rep, err
		}
		rep.Applied = applied
	}
	latest, err := LatestVerdicts(ctx, k)
	if err != nil {
		return rep, err
	}
	all, _, err := k.Items()
	if err != nil {
		return rep, err
	}
	targets, err := selectItems(all, o.Refs, func(it *knowledge.Item) bool { _, ok := latest[it.ID]; return ok && it.Active() })
	if err != nil {
		return rep, err
	}
	for _, it := range targets {
		sv, ok := latest[it.ID]
		if !ok {
			continue
		}
		rep.Rows = append(rep.Rows, VerdictRow{StoredVerdict: sv, Title: it.Title, Type: it.Type, Status: it.Status,
			Actionable: sv.Verdict != VerdictUnsure && !sv.Stale && (it.Status == knowledge.StatusStale || it.Status == knowledge.StatusVerified)})
	}
	return rep, nil
}
