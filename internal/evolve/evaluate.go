package evolve

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/store"
	"bint/internal/ulid"
)

// evalReviewer đánh dấu mục ứng viên được coi như đã duyệt CHỈ trong bản sao dùng để
// đánh giá: câu hỏi là "nếu áp dụng thì có giúp không". Không bao giờ ghi ra tệp.
const evalReviewer = "bint-evolve-eval"

// Variant dựng cặp nhánh base/candidate cho đề xuất. src là cây làm việc hiện tại:
// fingerprint bằng chứng của nội dung mới được tính trên đó, rồi đối chiếu với commit
// gốc của từng tác vụ (giống chế độ economy, mục 12.3) để không đưa tri thức "từ tương
// lai" vào tác vụ cũ.
func Variant(p *Proposal, delivery, repoDir string, src knowledge.Source, fpPref string, now time.Time) *eval.Variant {
	return &eval.Variant{
		Delivery: delivery,
		Label:    p.ID,
		Candidate: func(ctx context.Context, items []*knowledge.Item, c *eval.Case, leak *eval.LeakInput) ([]*knowledge.Item, error) {
			return candidateSet(ctx, p, items, c, leak, repoDir, src, fpPref, now)
		},
	}
}

func unavailable(f string, a ...any) error {
	return fmt.Errorf("%w: %s", eval.ErrArmUnavailable, fmt.Sprintf(f, a...))
}

func candidateSet(ctx context.Context, p *Proposal, items []*knowledge.Item, c *eval.Case, leak *eval.LeakInput,
	repoDir string, src knowledge.Source, fpPref string, now time.Time) ([]*knowledge.Item, error) {
	byID := indexItems(items)
	removed := map[string]bool{}
	updated := map[string]*knowledge.Item{}
	var created []*knowledge.Item
	// Nội dung của đề xuất mới được viết sau mọi tác vụ cũ, nên không qua được điều kiện
	// nguồn gốc của economy (A3). Thay vào đó: điều mục nói phải đã đúng và nhìn thấy
	// được trong mã tại commit gốc (bằng chứng khớp), và nội dung không chứa mã của lời
	// giải. Rủi ro rò rỉ gián tiếp còn lại được ghi vào kết luận; đánh giá tiến cứu
	// (--prospective: chỉ tác vụ sinh sau đề xuất) không có rủi ro này.
	applicable := func(it *knowledge.Item) error {
		if !eval.EvidenceHoldsAt(ctx, repoDir, c, it) {
			return unavailable("bằng chứng của %s không khớp tại commit gốc %s", it.ID, shortSHA(c.Source.Base))
		}
		if leak != nil {
			if r := eval.CheckLeak(it.Title+"\n"+it.Body, *leak); r.Status == "fail" {
				return unavailable("nội dung %s chứa mã của lời giải (%s)", it.ID, r.Findings[0].Detail)
			}
		}
		return nil
	}
	for _, ch := range p.Changes {
		switch ch.Op {
		case ActionDeprecate, ActionDelete:
			if byID[ch.ItemID] == nil {
				return nil, unavailable("mục %s không có trong tri thức của tác vụ tại commit gốc", ch.ItemID)
			}
			removed[ch.ItemID] = true
		case ActionUpdate:
			base := byID[ch.ItemID]
			if base == nil {
				return nil, unavailable("mục %s không có trong tri thức của tác vụ tại commit gốc", ch.ItemID)
			}
			it, err := DraftItem(ch.Draft, ch.ItemID, base, src, fpPref, p.ID, now)
			if err != nil {
				return nil, err
			}
			if it.Status == knowledge.StatusStale {
				it.Status, it.VerifiedAt = knowledge.StatusVerified, knowledge.Now(now)
			}
			if err := applicable(it); err != nil {
				return nil, err
			}
			updated[ch.ItemID] = it
		case ActionCreate:
			it, err := DraftItem(ch.Draft, ch.ItemID, nil, src, fpPref, p.ID, now)
			if err != nil {
				return nil, err
			}
			it.Status, it.ReviewedBy, it.VerifiedAt = knowledge.StatusVerified, evalReviewer, knowledge.Now(now)
			if err := applicable(it); err != nil {
				return nil, err
			}
			created = append(created, it)
		}
	}
	var out []*knowledge.Item
	for _, it := range items {
		switch {
		case removed[it.ID]:
		case updated[it.ID] != nil:
			out = append(out, updated[it.ID])
		default:
			out = append(out, it)
		}
	}
	return append(out, created...), nil
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// CaseImpact cho biết một tác vụ có chịu ảnh hưởng của đề xuất không.
type CaseImpact struct {
	CaseID   string     `json:"case_id"`
	Split    string     `json:"split"`
	Affected bool       `json:"affected"`
	Reason   string     `json:"reason"`
	Case     *eval.Case `json:"-"`
}

// SelectCases chọn tác vụ mà đề xuất thực sự làm đổi tập tri thức và mục bị đổi có
// liên quan (toàn repository, hoặc phạm vi chồng lên tệp mã của tác vụ). Chạy tác vụ
// không chịu ảnh hưởng chỉ tốn chi phí và làm loãng chênh lệch.
//
// Tác vụ mà lời giải có thể đến từ chính các lượt chạy làm căn cứ của đề xuất cũng bị
// loại (DerivedFrom): đề xuất được rút ra từ việc làm tác vụ đó, chấm lại trên nó là
// rò rỉ. h = nil thì bỏ qua kiểm tra này (chỉ dùng trong kiểm thử).
func SelectCases(ctx context.Context, env eval.Env, mode string, p *Proposal, h *History, cases []*eval.Case) ([]CaseImpact, error) {
	var out []CaseImpact
	for _, c := range cases {
		ci := CaseImpact{CaseID: c.ID, Split: c.Split, Case: c}
		if h != nil {
			if why := DerivedFrom(p, h, c); why != "" {
				ci.Reason = why
				out = append(out, ci)
				continue
			}
		}
		leak, err := eval.BuildLeakInput(ctx, env.RepoDir, c)
		if err != nil {
			return nil, err
		}
		items, _, err := eval.CaseKnowledge(ctx, env, mode, c, &leak)
		if errors.Is(err, eval.ErrArmUnavailable) {
			ci.Reason = err.Error()
			out = append(out, ci)
			continue
		}
		if err != nil {
			return nil, err
		}
		cand, err := env.Variant.Candidate(ctx, items, c, &leak)
		if errors.Is(err, eval.ErrArmUnavailable) {
			ci.Reason = err.Error()
			out = append(out, ci)
			continue
		}
		if err != nil {
			return nil, err
		}
		changed := changedItems(items, cand)
		if len(changed) == 0 {
			ci.Reason = "đề xuất không làm đổi tập tri thức của tác vụ"
			out = append(out, ci)
			continue
		}
		for _, it := range changed {
			if it.Scope.Global() {
				ci.Affected = true
				break
			}
			for _, f := range c.SourceFiles {
				if it.Scope.MatchScope(f) {
					ci.Affected = true
				}
			}
		}
		if ci.Affected {
			ci.Reason = fmt.Sprintf("%d mục thay đổi liên quan tới tệp của tác vụ", len(changed))
		} else {
			ci.Reason = "mục thay đổi không có phạm vi chồng lên tệp của tác vụ"
		}
		out = append(out, ci)
	}
	return out, nil
}

// derivedWindow: commit trong khoảng này sau khi lượt kết thúc được coi là có thể của
// lượt (cùng quy tắc với quét Git, mục 9.6).
const derivedWindow = 24 * time.Hour

// DerivedFrom trả về lý do nếu lời giải của tác vụ có thể đến từ một lượt làm căn cứ
// của đề xuất: commit lời giải được quét Git gắn với lượt đó, hoặc lượt đó sửa tệp của
// lời giải và commit lời giải nằm trong cửa sổ thời gian của lượt. Rỗng = không liên quan.
func DerivedFrom(p *Proposal, h *History, c *eval.Case) string {
	commitAt, _ := time.Parse(time.RFC3339, c.Source.Date)
	// Tiến cứu: commit lời giải sinh sau khi đề xuất được tạo. Đề xuất vẫn thấy NỘI DUNG
	// công việc của lượt căn cứ, nên chỉ bỏ cửa sổ cho lượt đã kết thúc trước đề xuất và
	// đã được gắn commit (công việc của nó đã nằm trong commit khác, không thể là commit
	// này). Lượt chưa gắn commit hoặc không xác định được: giữ cửa sổ (bảo thủ).
	var created time.Time
	afterProposal := false
	if t, err := store.ParseTime(p.CreatedAt); err == nil && !commitAt.IsZero() && commitAt.After(t) {
		created, afterProposal = t, true
	}
	files := append(append([]string{}, c.SourceFiles...), c.TestFiles...)
	for _, ref := range p.SupportingRuns {
		r := h.ByID[ref.RunID]
		if r == nil {
			continue
		}
		for _, sha := range r.Commits {
			if sha == c.Source.Commit || (len(sha) >= 7 && strings.HasPrefix(c.Source.Commit, sha)) {
				return fmt.Sprintf("lời giải (%s) là commit của lượt căn cứ %s", shortSHA(c.Source.Commit), r.ID)
			}
		}
		if commitAt.IsZero() {
			continue
		}
		start, err1 := store.ParseTime(r.StartedAt)
		end, err2 := store.ParseTime(r.EndedAt)
		if err1 != nil {
			continue
		}
		if err2 != nil {
			end = start
		}
		if afterProposal && err2 == nil && !end.After(created) && len(r.Commits) > 0 {
			continue
		}
		if commitAt.Before(start) || commitAt.After(end.Add(derivedWindow)) {
			continue
		}
		for _, f := range files {
			if slices.Contains(r.Modified, f) {
				return fmt.Sprintf("lời giải (%s) có thể đến từ lượt căn cứ %s (cùng sửa %s trong cửa sổ thời gian của lượt)", shortSHA(c.Source.Commit), r.ID, f)
			}
		}
	}
	return ""
}

// Loại bằng chứng của một lần đối chiếu.
const (
	KindProspective   = "prospective"   // mọi tác vụ sinh sau đề xuất
	KindRetrospective = "retrospective" // tác vụ trong lịch sử trước đề xuất
)

// EvidenceKind cho biết lần đối chiếu là tiến cứu hay hồi cứu. Đề xuất chỉ loại bỏ
// tri thức (không có nội dung mới) không có rủi ro rò rỉ từ nội dung: trả "".
func EvidenceKind(p *Proposal, caseIDs []string, cases map[string]*eval.Case) string {
	hasContent := false
	for _, ch := range p.Changes {
		hasContent = hasContent || ch.Draft != nil
	}
	if !hasContent {
		return ""
	}
	if Prospective(p, caseIDs, cases) {
		return KindProspective
	}
	return KindRetrospective
}

// KindLabel là nhãn hiển thị.
func KindLabel(k string) string {
	return map[string]string{KindProspective: "tiến cứu", KindRetrospective: "hồi cứu"}[k]
}

// changedItems trả về mục có trong một tập mà không có (cùng nội dung) trong tập kia.
func changedItems(a, b []*knowledge.Item) []*knowledge.Item {
	hash := func(it *knowledge.Item) string {
		data, _ := it.Marshal()
		s := sha256.Sum256(data)
		return hex.EncodeToString(s[:])
	}
	ha, hb := map[string]string{}, map[string]string{}
	for _, it := range a {
		ha[it.ID] = hash(it)
	}
	for _, it := range b {
		hb[it.ID] = hash(it)
	}
	var out []*knowledge.Item
	for _, it := range a {
		if hb[it.ID] != ha[it.ID] {
			out = append(out, it)
		}
	}
	for _, it := range b {
		if ha[it.ID] != hb[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

// Kết luận của một lần đối chiếu.
const (
	VerdictSupported    = "supported"
	VerdictNotSupported = "not_supported"
	VerdictInconclusive = "inconclusive"
)

// blockingKinds: cảnh báo làm phép đo không đủ tin cậy để kết luận (mục 12.9). Chế độ
// economy không chặn ở đây (đề xuất tiến hoá không phải cổng quyết định) nhưng được ghi.
var blockingKinds = map[string]bool{
	"incomplete": true, "ci_unavailable": true, "model_changed": true, "harness_changed": true,
	"errors": true, "case_changed": true, "unverified": true,
}

// Verdict là kết luận theo kế hoạch đã chốt.
type Verdict struct {
	Verdict  string     `json:"verdict"`
	Detail   string     `json:"detail"`
	PassDiff *eval.Stat `json:"pass_rate_diff,omitempty"`
	Metric   *eval.Stat `json:"metric_diff,omitempty"`
	Notes    []string   `json:"notes,omitempty"`
}

// ComputeVerdict đối chiếu báo cáo (candidate − base, theo cặp) với kế hoạch.
func ComputeVerdict(rep eval.Report, plan Plan) Verdict {
	var v Verdict
	for _, d := range rep.Diagnostics {
		if d.Level != "warn" {
			continue
		}
		if blockingKinds[d.Kind] {
			v.Verdict, v.Detail = VerdictInconclusive, "đo lường không đủ tin cậy: "+d.Detail
			return v
		}
		v.Notes = append(v.Notes, d.Detail)
	}
	var p *eval.Paired
	for i := range rep.Paired {
		if rep.Paired[i].Arm == eval.ArmCandidate && rep.Paired[i].Baseline == eval.ArmBase {
			p = &rep.Paired[i]
		}
	}
	if p == nil || !p.PassDiff.CI.Valid() {
		v.Verdict, v.Detail = VerdictInconclusive, "không đủ tác vụ chấm được ở cả hai nhánh để tính khoảng tin cậy"
		return v
	}
	pd := p.PassDiff
	v.PassDiff = &pd
	fmtCI := func(s eval.Stat) string {
		return fmt.Sprintf("%+.3f, CI95 [%+.3f, %+.3f], %d tác vụ", s.Mean, s.CI.Lo, s.CI.Hi, s.N)
	}
	switch plan.Kind {
	case "non_inferiority":
		if pd.CI.Lo > -plan.Margin {
			v.Verdict, v.Detail = VerdictSupported, fmt.Sprintf("tỷ lệ đạt không kém quá %.2f: %s", plan.Margin, fmtCI(pd))
		} else {
			v.Verdict, v.Detail = VerdictNotSupported, fmt.Sprintf("không loại trừ được việc tỷ lệ đạt giảm quá %.2f: %s", plan.Margin, fmtCI(pd))
		}
	default:
		switch plan.Metric {
		case "pass_rate":
			v.Metric = &pd
			if pd.CI.Lo > 0 {
				v.Verdict, v.Detail = VerdictSupported, "tỷ lệ đạt tăng: "+fmtCI(pd)
			} else {
				v.Verdict, v.Detail = VerdictNotSupported, "không có bằng chứng tỷ lệ đạt tăng: "+fmtCI(pd)
			}
		default:
			m := p.CostDiff
			if plan.Metric == "turns" {
				m = p.TurnDiff
			}
			if m == nil || !m.CI.Valid() {
				v.Verdict, v.Detail = VerdictInconclusive, "harness không báo "+plan.Metric+" cho đủ tác vụ"
				return v
			}
			v.Metric = m
			switch {
			case m.CI.Hi >= 0:
				v.Verdict, v.Detail = VerdictNotSupported, fmt.Sprintf("không có bằng chứng %s giảm: %s", plan.Metric, fmtCI(*m))
			case pd.CI.Lo <= -plan.Margin:
				v.Verdict, v.Detail = VerdictNotSupported, fmt.Sprintf("%s giảm nhưng tỷ lệ đạt có thể giảm quá %.2f: %s", plan.Metric, plan.Margin, fmtCI(pd))
			default:
				v.Verdict, v.Detail = VerdictSupported, fmt.Sprintf("%s giảm (%s), tỷ lệ đạt không kém (%s)", plan.Metric, fmtCI(*m), fmtCI(pd))
			}
		}
	}
	return v
}

// Evaluation là một lần đối chiếu đã ghi.
type Evaluation struct {
	ID          string   `json:"evaluation_id"`
	ProposalID  string   `json:"proposal_id"`
	BatchID     string   `json:"batch_id"`
	Split       string   `json:"split"`
	BaseKey     string   `json:"base_key"`
	Delivery    string   `json:"delivery"`
	CaseIDs     []string `json:"case_ids"`
	Verdict     string   `json:"verdict"`
	Independent *bool    `json:"independent"`
	Detail      string   `json:"detail"`
	CreatedAt   string   `json:"created_at"`
	// Kind: probe (đối chiếu trước khi áp dụng) | regression (kiểm tra lại sau khi đổi mô hình, mục 12.6).
	Kind string `json:"kind"`
}

// Loại lần đối chiếu.
const (
	EvalProbe      = "probe"
	EvalRegression = "regression"
)

// RecordEvaluation ghi liên kết đề xuất ↔ đợt đánh giá trước khi chạy (để tiếp tục được).
func RecordEvaluation(ctx context.Context, db *sql.DB, e *Evaluation) error {
	if e.ID == "" {
		e.ID = "ee_" + ulid.New()
	}
	ids, _ := json.Marshal(nonNil(e.CaseIDs))
	_, err := db.ExecContext(ctx, `INSERT INTO evolution_evaluations (evaluation_id, proposal_id, batch_id, split, base_key,
		delivery, case_ids, created_at, kind) VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		e.ID, e.ProposalID, e.BatchID, e.Split, e.BaseKey, e.Delivery, string(ids), e.CreatedAt, orProbe(e.Kind))
	return err
}

// SetVerdict ghi kết luận của lần đối chiếu.
func SetVerdict(ctx context.Context, db *sql.DB, id string, v Verdict, independent *bool) error {
	var ind any
	if independent != nil {
		ind = map[bool]int{true: 1, false: 0}[*independent]
	}
	detail := v.Detail
	if len(v.Notes) > 0 {
		detail += " | lưu ý: " + strings.Join(v.Notes, "; ")
	}
	_, err := db.ExecContext(ctx, `UPDATE evolution_evaluations SET verdict = ?, independent = ?, detail = ? WHERE evaluation_id = ?`,
		v.Verdict, ind, detail, id)
	return err
}

const evalCols = `evaluation_id, proposal_id, batch_id, COALESCE(split, ''), base_key, delivery, case_ids,
	COALESCE(verdict, ''), independent, COALESCE(detail, ''), created_at, kind`

func scanEvaluation(sc interface{ Scan(...any) error }) (Evaluation, error) {
	var e Evaluation
	var ids string
	var ind sql.NullInt64
	err := sc.Scan(&e.ID, &e.ProposalID, &e.BatchID, &e.Split, &e.BaseKey, &e.Delivery, &ids, &e.Verdict, &ind, &e.Detail, &e.CreatedAt, &e.Kind)
	e.CaseIDs = jsonStrings(ids)
	if ind.Valid {
		b := ind.Int64 == 1
		e.Independent = &b
	}
	return e, err
}

// Evaluations liệt kê các lần đối chiếu của đề xuất (cũ nhất trước); proposalID rỗng = mọi đề xuất.
func Evaluations(ctx context.Context, db *sql.DB, proposalID string) ([]Evaluation, error) {
	q, args := `SELECT `+evalCols+` FROM evolution_evaluations`, []any{}
	if proposalID != "" {
		q += ` WHERE proposal_id = ?`
		args = append(args, proposalID)
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY created_at, rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Evaluation
	for rows.Next() {
		e, err := scanEvaluation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EvaluationByBatch tìm lần đối chiếu theo đợt (để tiếp tục).
func EvaluationByBatch(ctx context.Context, db *sql.DB, batchID string) (Evaluation, error) {
	return scanEvaluation(db.QueryRowContext(ctx, `SELECT `+evalCols+` FROM evolution_evaluations WHERE batch_id = ?`, batchID))
}

// Independent cho biết các tác vụ tập test của lần đối chiếu chưa từng được dùng để
// đối chiếu một đề xuất KHÁC cùng đối tượng (cùng cơ hội hoặc chung mục đích). Nếu đã
// dùng, kết quả trên test phản ánh cả việc chỉnh đề xuất theo chính tập đó (mục 12.2).
func Independent(ctx context.Context, db *sql.DB, p *Proposal, caseIDs []string) (bool, string, error) {
	all, err := ListProposals(ctx, db)
	if err != nil {
		return false, "", err
	}
	for _, o := range all {
		if o.ID == p.ID {
			continue
		}
		related := o.Opportunity == p.Opportunity
		for _, t := range o.TargetIDs {
			related = related || slices.Contains(p.TargetIDs, t)
		}
		for _, ch := range o.Changes {
			for _, pc := range p.Changes {
				related = related || (ch.ItemID == pc.ItemID && ch.ItemID != "")
			}
		}
		if !related {
			continue
		}
		evs, err := Evaluations(ctx, db, o.ID)
		if err != nil {
			return false, "", err
		}
		for _, e := range evs {
			if e.Split != eval.SplitTest || e.Kind == EvalRegression {
				continue
			}
			for _, id := range e.CaseIDs {
				if slices.Contains(caseIDs, id) {
					return false, fmt.Sprintf("tác vụ test %s đã dùng để đối chiếu đề xuất %s cùng đối tượng", id, o.ID), nil
				}
			}
		}
	}
	return true, "", nil
}

// Finalize tính kết luận cho một đợt đã chạy và ghi lại.
func Finalize(ctx context.Context, db *sql.DB, p *Proposal, e Evaluation, current map[string]*eval.Case) (Verdict, eval.Report, error) {
	b, err := eval.GetBatch(ctx, db, e.BatchID)
	if err != nil {
		return Verdict{}, eval.Report{}, err
	}
	rep, err := eval.BuildReport(ctx, db, b, eval.ArmBase, current)
	if err != nil {
		return Verdict{}, rep, err
	}
	v := ComputeVerdict(rep, p.Plan)
	var ind *bool
	if e.Split == eval.SplitTest {
		ok, why, err := Independent(ctx, db, p, e.CaseIDs)
		if err != nil {
			return v, rep, err
		}
		ind = &ok
		if !ok {
			v.Notes = append(v.Notes, "không độc lập: "+why)
		}
	}
	if b.LeakageMode == eval.LeakEconomy {
		v.Notes = append(v.Notes, "chế độ economy: tri thức hiện tại lọc theo commit gốc, còn rủi ro rò rỉ gián tiếp")
	}
	switch EvidenceKind(p, e.CaseIDs, current) {
	case KindProspective:
		v.Notes = append(v.Notes, "tiến cứu: mọi tác vụ sinh sau đề xuất, nội dung mới không thể chứa lời giải của chúng")
	case KindRetrospective:
		v.Notes = append(v.Notes, "hồi cứu: nội dung mới viết sau các tác vụ (đã kiểm bằng chứng tại commit gốc, mã lời giải, và loại tác vụ có lời giải từ lượt căn cứ); còn rủi ro rò rỉ gián tiếp — xác nhận thêm bằng --prospective khi có tác vụ mới")
	}
	return v, rep, SetVerdict(ctx, db, e.ID, v, ind)
}

// Prospective cho biết mọi tác vụ đều có commit lời giải sinh sau khi đề xuất được tạo.
func Prospective(p *Proposal, caseIDs []string, cases map[string]*eval.Case) bool {
	created, err := store.ParseTime(p.CreatedAt)
	if err != nil || len(caseIDs) == 0 {
		return false
	}
	for _, id := range caseIDs {
		c := cases[id]
		if c == nil {
			return false
		}
		t, err := time.Parse(time.RFC3339, c.Source.Date)
		if err != nil || !t.After(created) {
			return false
		}
	}
	return true
}

// VerifiedBy trả về lần đối chiếu cho phép áp dụng đề xuất như đã kiểm chứng: tập
// test, kết luận supported, độc lập, và phiên bản gốc lúc đánh giá còn khớp.
func VerifiedBy(evs []Evaluation, p *Proposal) (Evaluation, bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Kind != EvalRegression && e.Split == eval.SplitTest && e.Verdict == VerdictSupported && e.Independent != nil && *e.Independent && e.BaseKey == p.BaseKey() {
			return e, true
		}
	}
	return Evaluation{}, false
}

func orProbe(k string) string {
	if k == "" {
		return EvalProbe
	}
	return k
}
