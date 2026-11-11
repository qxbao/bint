package evolve

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/knowledge"
	"bint/internal/store"
)

// ProposalSchema là phiên bản định dạng đề xuất.
const ProposalSchema = 1

// Trạng thái đề xuất.
const (
	StatusProposed   = "proposed"
	StatusApplied    = "applied"
	StatusRejected   = "rejected"
	StatusRolledBack = "rolled_back"
)

// Mức rủi ro (Bint tính, không do agent tự khai).
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Draft là nội dung một mục tri thức do đề xuất tạo hoặc sửa. Chỉ gồm các trường nội
// dung: đề xuất không đặt được trạng thái, người duyệt, nguồn gốc hay cờ bên thứ ba
// (mục 15.4, 15.5) — các trường đó do Bint quyết định khi áp dụng.
type Draft struct {
	Type     string          `json:"type"`
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Scope    knowledge.Scope `json:"scope"`
	Evidence []string        `json:"evidence"` // path[:a-b][#symbol][@commit]
	// Inputs: tệp agent đã đọc khi soạn (nguồn gốc, mục 15.5).
	Inputs []string `json:"inputs,omitempty"`
	// Lesson: siêu dữ liệu bài học từ thất bại (kế hoạch E). Do Bint đặt sau khi kiểm tra
	// (reflector.go), không đến từ JSON của agent.
	Lesson *LessonMeta `json:"lesson,omitempty"`
}

// LessonMeta là nguồn gốc của bài học rút từ lượt thất bại: provenance, các lượt làm bằng
// chứng và các tác vụ đánh giá nguồn (không được dùng khi chấm lại chính chúng).
type LessonMeta struct {
	Provenance string   `json:"provenance"`
	Runs       []string `json:"runs"`
	EvalCases  []string `json:"eval_cases,omitempty"`
}

// Change là một thay đổi trong đề xuất.
type Change struct {
	Op     string `json:"op"`      // create | update | deprecate | delete
	ItemID string `json:"item_id"` // create: ID được cấp sẵn để đánh giá và áp dụng dùng cùng một mục
	Draft  *Draft `json:"draft,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// BaseVersion là phiên bản gốc của một mục mà đề xuất sửa (mục 8.7).
type BaseVersion struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Status  string `json:"status"`
	SHA     string `json:"sha256"` // băm nội dung tệp; "" = mục chưa tồn tại (create)
}

// RunRef là một lượt chạy làm căn cứ, kèm nhãn tại thời điểm đề xuất (để phát hiện
// tín hiệu đến muộn làm đổi nhãn, mục 9.6).
type RunRef struct {
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

// Plan là kế hoạch đánh giá, chốt khi tạo đề xuất (mục 12.7: thước đo chọn trước).
type Plan struct {
	// Kind: superiority (phải tốt hơn trên Metric) | non_inferiority (không kém tỷ lệ
	// đạt quá Margin — dùng cho loại bỏ tri thức).
	Kind       string  `json:"kind"`
	Metric     string  `json:"metric"` // pass_rate | cost_usd | turns
	MinEffect  float64 `json:"min_effect"`
	Margin     float64 `json:"non_inferiority_margin"`
	Hypothesis string  `json:"hypothesis"`
	// Splits: xác thực ứng viên trên validation, kết luận trên test (mục 12.2).
	Splits []string `json:"splits"`
}

// Proposal là một đề xuất tiến hoá (mục 10.4).
type Proposal struct {
	Schema     int      `json:"schema"`
	ID         string   `json:"proposal_id"`
	Level      string   `json:"level"`
	TargetType string   `json:"target_type"`
	Action     string   `json:"action"`
	TargetIDs  []string `json:"target_ids"`
	// EvalCases: tác vụ đánh giá là nguồn của đề xuất (bài học từ thất bại), gồm mọi tác vụ có
	// bản tóm tắt trong prompt của reflector chứ không chỉ các lượt làm bằng chứng; SelectCases
	// không chấm lại đề xuất trên các tác vụ này (mục 12.3).
	EvalCases       []string      `json:"eval_cases,omitempty"`
	Base            []BaseVersion `json:"base"`
	Opportunity     string        `json:"opportunity"`
	Reason          string        `json:"reason"`
	SupportingRuns  []RunRef      `json:"supporting_runs"`
	ExpectedBenefit string        `json:"expected_benefit"`
	Changes         []Change      `json:"candidate_changes"`
	// SkillChanges: thay đổi skill của đề xuất L2 (target_type skill); Changes rỗng.
	SkillChanges []SkillChange `json:"skill_changes,omitempty"`
	// HarnessChanges: thay đổi thành phần quy trình của đề xuất L2 (target_type harness, H3);
	// Changes và SkillChanges rỗng.
	HarnessChanges []HarnessChange `json:"harness_changes,omitempty"`
	// Pair: đề xuất này là một nửa của cặp chung/riêng sinh từ cùng một cơ hội (H4).
	Pair        *Pair    `json:"pair,omitempty"`
	Risk        string   `json:"risk_level"`
	RiskReasons []string `json:"risk_reasons"`
	Plan        Plan     `json:"evaluation_plan"`
	// Quantitative: điều kiện tiên quyết (mục 10.6) đạt lúc đề xuất. Nếu không, đề xuất
	// chỉ là L1 chờ người duyệt và chưa được kiểm chứng định lượng.
	Quantitative bool     `json:"quantitative"`
	Notes        []string `json:"notes"`
	Generator    string   `json:"generator"`
	Harness      string   `json:"harness,omitempty"`
	Model        string   `json:"model,omitempty"`
	CostUSD      *float64 `json:"cost_usd"`
	CreatedAt    string   `json:"created_at"`

	// Trạng thái (cột riêng trong DB, không thuộc phần thân bất biến).
	Status       string `json:"status"`
	StatusReason string `json:"status_reason,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

// fileSHA băm nội dung tệp tri thức hiện tại; "" nếu tệp không tồn tại.
func fileSHA(st knowledge.Store, id string) (string, error) {
	b, err := os.ReadFile(st.PathFor(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

// BaseOf ghi phiên bản gốc của các mục.
func BaseOf(st knowledge.Store, items []*knowledge.Item) ([]BaseVersion, error) {
	var out []BaseVersion
	for _, it := range items {
		sha, err := fileSHA(st, it.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, BaseVersion{ID: it.ID, Version: it.Version, Status: it.Status, SHA: sha})
	}
	return out, nil
}

// BaseProblem là một mục có phiên bản gốc đã đổi.
type BaseProblem struct {
	ID     string `json:"id"`
	Detail string `json:"detail"`
}

// CheckBase so phiên bản gốc với tệp hiện tại. Đề xuất có phiên bản gốc đã đổi (do
// người khác áp dụng thay đổi khác, hoặc sửa tay) là không hợp lệ và phải được đề
// xuất/đánh giá lại trên phiên bản mới (mục 8.7).
func (p *Proposal) CheckBase(st knowledge.Store) ([]BaseProblem, error) {
	var out []BaseProblem
	for _, b := range p.Base {
		sha, err := fileSHA(st, b.ID)
		if err != nil {
			return nil, err
		}
		switch {
		case sha == "" && b.SHA != "":
			out = append(out, BaseProblem{b.ID, "mục đã bị xoá"})
		case sha != b.SHA:
			out = append(out, BaseProblem{b.ID, fmt.Sprintf("mục đã thay đổi kể từ khi đề xuất (phiên bản gốc %d)", b.Version)})
		}
	}
	for _, c := range p.Changes {
		if c.Op == ActionCreate {
			if sha, err := fileSHA(st, c.ItemID); err != nil {
				return nil, err
			} else if sha != "" {
				out = append(out, BaseProblem{c.ItemID, "mục cần tạo đã tồn tại"})
			}
		}
	}
	return out, nil
}

// BaseKey là khoá phiên bản gốc, ghi cùng mỗi lần đánh giá: kết quả đánh giá chỉ có
// giá trị khi khoá này còn khớp.
func (p *Proposal) BaseKey() string {
	parts := []string{p.ID}
	for _, b := range p.Base {
		parts = append(parts, fmt.Sprintf("%s:%d:%s", b.ID, b.Version, b.SHA))
	}
	s := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(s[:])[:16]
}

// assessRisk tính mức rủi ro từ nội dung thay đổi (không tin mức agent tự khai).
func assessRisk(p *Proposal, byID map[string]*knowledge.Item) (string, []string) {
	risk, why := RiskLow, []string{}
	raise := func(r, reason string) {
		order := map[string]int{RiskLow: 0, RiskMedium: 1, RiskHigh: 2}
		if order[r] > order[risk] {
			risk = r
		}
		why = append(why, reason)
	}
	for _, c := range p.Changes {
		it := byID[c.ItemID]
		switch c.Op {
		case ActionCreate:
			if c.Draft != nil && knowledge.HighRisk(c.Draft.Type) {
				raise(RiskMedium, fmt.Sprintf("tạo mục loại %s (vẫn ở candidate, cần người duyệt)", c.Draft.Type))
			}
		case ActionUpdate:
			if it != nil && it.Status != knowledge.StatusCandidate {
				r := RiskMedium
				if knowledge.HighRisk(it.Type) {
					r = RiskHigh
				}
				raise(r, fmt.Sprintf("sửa mục %s đã duyệt (%s)", it.Type, it.ID))
			}
		case ActionDeprecate, ActionDelete:
			if it != nil && it.Status != knowledge.StatusCandidate {
				r := RiskMedium
				if it.Type == knowledge.TypeRule {
					r = RiskHigh // mục 12.7: không làm mất quy tắc bắt buộc
				}
				raise(r, fmt.Sprintf("loại bỏ mục %s đã duyệt (%s)", it.Type, it.ID))
			}
		}
	}
	return risk, why
}

// validateChanges kiểm tra ranh giới của đề xuất L1 (mục 15.4): chỉ đụng tới mục tri
// thức, chỉ các thao tác cho phép, không tự đặt trạng thái hay nguồn gốc.
func validateChanges(p *Proposal, byID map[string]*knowledge.Item) error {
	if p.TargetType != "knowledge" || p.Level != "L1" {
		return fmt.Errorf("P6 chỉ hỗ trợ đề xuất L1 cho tri thức")
	}
	if !slices.Contains(Actions, p.Action) {
		return fmt.Errorf("hành động %q không hợp lệ", p.Action)
	}
	if len(p.Changes) == 0 {
		return fmt.Errorf("đề xuất không có thay đổi nào")
	}
	for _, c := range p.Changes {
		switch c.Op {
		case ActionCreate:
			if c.Draft == nil || c.ItemID == "" {
				return fmt.Errorf("create cần item_id và nội dung")
			}
		case ActionUpdate:
			if c.Draft == nil || byID[c.ItemID] == nil {
				return fmt.Errorf("update cần mục tồn tại và nội dung (%s)", c.ItemID)
			}
			if c.Draft.Type != byID[c.ItemID].Type {
				return fmt.Errorf("update không được đổi loại mục %s (%s → %s); đề xuất mục mới thay thế", c.ItemID, byID[c.ItemID].Type, c.Draft.Type)
			}
		case ActionDeprecate:
			if byID[c.ItemID] == nil {
				return fmt.Errorf("deprecate: không có mục %s", c.ItemID)
			}
		case ActionDelete:
			it := byID[c.ItemID]
			if it == nil || (it.Status != knowledge.StatusCandidate && it.Status != knowledge.StatusDeprecated) {
				return fmt.Errorf("delete chỉ áp dụng cho mục candidate hoặc deprecated (%s); mục đã duyệt dùng deprecate để giữ dấu vết", c.ItemID)
			}
		default:
			return fmt.Errorf("thao tác %q không hợp lệ", c.Op)
		}
		if d := c.Draft; d != nil {
			if !slices.Contains(knowledge.Types, d.Type) {
				return fmt.Errorf("loại %q không hợp lệ", d.Type)
			}
			if strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.Body) == "" {
				return fmt.Errorf("nội dung đề xuất thiếu tiêu đề hoặc thân")
			}
		}
	}
	return nil
}

// defaultPlan chốt kế hoạch đánh giá theo hành động và cấu hình (mục 12.7).
func defaultPlan(action string, ev config.EvolutionEvaluation) Plan {
	p := Plan{Metric: ev.Metric, MinEffect: ev.MinEffect, Margin: ev.NonInferiorityMargin, Splits: []string{"validation", "test"}}
	switch action {
	case ActionDeprecate, ActionDelete:
		p.Kind, p.Metric = "non_inferiority", "pass_rate"
		p.Hypothesis = fmt.Sprintf("bỏ mục không làm tỷ lệ đạt giảm quá %.0f điểm %% (cận dưới CI95 của chênh lệch candidate − base > −%.2f)", 100*p.Margin, p.Margin)
	case ActionMerge:
		p.Kind, p.Metric = "non_inferiority", "pass_rate"
		p.Hypothesis = fmt.Sprintf("gộp mục trùng không làm tỷ lệ đạt giảm quá %.0f điểm %%", 100*p.Margin)
	default:
		p.Kind = "superiority"
		switch ev.Metric {
		case "pass_rate":
			p.Hypothesis = "tri thức mới/sửa làm tăng tỷ lệ đạt trên tác vụ chạm phạm vi của nó (cận dưới CI95 > 0)"
		default:
			p.Hypothesis = fmt.Sprintf("tri thức mới/sửa làm giảm %s (cận trên CI95 < 0) mà tỷ lệ đạt không giảm quá %.0f điểm %%", ev.Metric, 100*p.Margin)
		}
	}
	return p
}

// ---- Lưu trữ ----

// ErrNoProposal: không tìm thấy đề xuất.
var ErrNoProposal = errors.New("không tìm thấy đề xuất")

// SaveProposal ghi đề xuất mới.
func SaveProposal(ctx context.Context, db *sql.DB, p *Proposal) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	targets, _ := json.Marshal(nonNil(p.TargetIDs))
	_, err = db.ExecContext(ctx, `INSERT INTO evolution_proposals (proposal_id, level, target_type, action, target_ids,
		opportunity_key, status, risk_level, body, generator, model, cost_usd, status_reason, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?)`,
		p.ID, p.Level, p.TargetType, p.Action, string(targets), p.Opportunity, p.Status, p.Risk, string(body),
		p.Generator, p.Model, nullF(p.CostUSD), p.StatusReason, p.CreatedAt, p.UpdatedAt)
	return err
}

// SetStatus đổi trạng thái đề xuất.
func SetStatus(ctx context.Context, db execer, id, status, reason string, now time.Time) error {
	_, err := db.ExecContext(ctx, `UPDATE evolution_proposals SET status = ?, status_reason = NULLIF(?, ''), updated_at = ?
		WHERE proposal_id = ?`, status, reason, store.FormatTime(now), id)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func scanProposal(sc interface{ Scan(...any) error }) (*Proposal, error) {
	var body, status, updated string
	var reason sql.NullString
	if err := sc.Scan(&body, &status, &reason, &updated); err != nil {
		return nil, err
	}
	var p Proposal
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, err
	}
	if p.Schema != ProposalSchema {
		return nil, fmt.Errorf("đề xuất %s: schema %d không hỗ trợ", p.ID, p.Schema)
	}
	p.Status, p.StatusReason, p.UpdatedAt = status, reason.String, updated
	return &p, nil
}

// GetProposal đọc đề xuất theo ID hoặc tiền tố/hậu tố (≥ 4 ký tự).
func GetProposal(ctx context.Context, db *sql.DB, ref string) (*Proposal, error) {
	ref = strings.TrimSpace(ref)
	if len(ref) < 4 {
		return nil, fmt.Errorf("ID đề xuất %q quá ngắn", ref)
	}
	rows, err := db.QueryContext(ctx, `SELECT body, status, status_reason, updated_at FROM evolution_proposals
		WHERE proposal_id = ? OR proposal_id LIKE ? OR proposal_id LIKE ? ORDER BY proposal_id LIMIT 3`,
		ref, ref+"%", "%"+strings.ToUpper(ref))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		if p.ID == ref {
			return p, nil
		}
		found = append(found, p)
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%w %q", ErrNoProposal, ref)
	case 1:
		return found[0], rows.Err()
	}
	return nil, fmt.Errorf("ID %q khớp nhiều đề xuất", ref)
}

// ListProposals liệt kê đề xuất mới nhất trước; statuses rỗng = mọi trạng thái.
func ListProposals(ctx context.Context, db *sql.DB, statuses ...string) ([]*Proposal, error) {
	q := `SELECT body, status, status_reason, updated_at FROM evolution_proposals`
	var args []any
	if len(statuses) > 0 {
		q += ` WHERE status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, s := range statuses {
			args = append(args, s)
		}
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY created_at DESC, rowid DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Proposal
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// proposalEvidence: lượt đã dùng làm căn cứ theo khoá cơ hội (để đếm bằng chứng mới),
// và đề xuất đang chờ xử lý theo khoá.
func proposalEvidence(ctx context.Context, db *sql.DB) (map[string]map[string]bool, map[string]string, error) {
	used, pending := map[string]map[string]bool{}, map[string]string{}
	ps, err := ListProposals(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	for _, p := range ps {
		if used[p.Opportunity] == nil {
			used[p.Opportunity] = map[string]bool{}
		}
		for _, r := range p.SupportingRuns {
			used[p.Opportunity][r.RunID] = true
		}
		if p.Status == StatusProposed {
			if _, ok := pending[p.Opportunity]; !ok {
				pending[p.Opportunity] = p.ID
			}
		}
	}
	return used, pending, nil
}

// LabelDrift trả về các lượt làm căn cứ có nhãn đã đổi kể từ khi đề xuất (tín hiệu đến
// muộn, mục 9.6): đề xuất dùng nhãn cũ phải được xem xét lại.
func LabelDrift(p *Proposal, h *History) []string {
	var out []string
	for _, r := range p.SupportingRuns {
		cur, ok := h.ByID[r.RunID]
		if ok && cur.Label != r.Label {
			out = append(out, fmt.Sprintf("%s: %s → %s (%s)", r.RunID, r.Label, cur.Label, cur.LabelReason))
		}
	}
	sort.Strings(out)
	return out
}

// IsLesson cho biết đề xuất có thay đổi là bài học từ thất bại (có siêu dữ liệu Lesson).
func (p *Proposal) IsLesson() bool {
	for _, c := range p.Changes {
		if c.Draft != nil && c.Draft.Lesson != nil {
			return true
		}
	}
	return false
}

func nullF(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
