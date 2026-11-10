package evolve

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/store"
	"bint/internal/task"
	"bint/internal/ulid"
)

// ErrLocked: một thao tác tiến hoá khác đang chạy (mục 9.5).
var ErrLocked = errors.New("một thao tác tiến hoá khác đang chạy trên repository này")

// Lock là khoá tiến hoá của repository (dùng chung mọi worktree).
type Lock struct{ path string }

// AcquireLock lấy khoá trong <runtime>/locks/evolve.lock. Khoá của tiến trình đã chết
// được coi là bỏ lại.
func AcquireLock(runtimeDir, owner string) (*Lock, error) {
	dir := filepath.Join(runtimeDir, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "evolve.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n%s\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339), owner)
			f.Close()
			return &Lock{path: p}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		b, _ := os.ReadFile(p)
		lines := strings.Split(string(b), "\n")
		if pid, _ := strconv.Atoi(strings.TrimSpace(lines[0])); pid > 0 && task.ProcessAlive(pid) {
			who := ""
			if len(lines) > 2 {
				who = lines[2]
			}
			return nil, fmt.Errorf("%w (tiến trình %d: %s)", ErrLocked, pid, who)
		}
		os.Remove(p)
	}
	return nil, ErrLocked
}

// Release trả khoá.
func (l *Lock) Release() {
	if l != nil {
		os.Remove(l.path)
	}
}

func requireHuman(op string) error {
	if in, k := app.InAgentSession(); in {
		return fmt.Errorf("%w (`%s`; phát hiện biến %s)", app.ErrAgentForbidden, op, k)
	}
	return nil
}

// FileChange là nội dung một tệp trước/sau khi áp dụng (nil = không tồn tại).
type FileChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// ApplyOptions điều khiển `bint evolve apply`.
type ApplyOptions struct {
	K   *app.KnowledgeWS
	DB  *sql.DB
	Ref string
	// Unverified: người duyệt chấp nhận áp dụng khi chưa có kết quả kiểm chứng trên tập
	// test (mục 10.6); được ghi vào lịch sử.
	Unverified  bool
	ConfirmRisk bool
	Reason      string
	// Cases: tác vụ đánh giá hiện có, để ghi lần đối chiếu làm căn cứ là tiến cứu hay hồi cứu.
	Cases map[string]*eval.Case
	Now   time.Time
	// Auto: áp dụng tự động bởi vòng tiến hoá (P8, `bint evolve cycle`). Chỉ cho phép khi
	// evolution.adoption.auto_low_risk bật, đề xuất rủi ro thấp, chỉ tạo mục mới (mục vào
	// trạng thái candidate, vẫn chờ người duyệt) và đã supported độc lập trên tập test.
	Auto bool
}

// AutoActor là tác nhân ghi trong bản kiểm toán khi vòng tiến hoá tự áp dụng/quay lui.
const AutoActor = "bint-auto"

// AutoApplicable cho biết đề xuất có thuộc loại được tự áp dụng hay không (bỏ qua điều
// kiện kiểm chứng, được kiểm riêng); lý do khi không.
func AutoApplicable(p *Proposal) (bool, string) {
	if p.IsSkill() {
		return false, "đề xuất skill cần người duyệt"
	}
	if p.Risk != RiskLow {
		return false, "rủi ro " + p.Risk
	}
	if len(p.Changes) == 0 {
		return false, "không có thay đổi"
	}
	for _, ch := range p.Changes {
		if ch.Op != ActionCreate {
			return false, "thay đổi " + ch.Op + " mục có sẵn cần người duyệt"
		}
	}
	return true, ""
}

// ApplyResult là kết quả áp dụng.
type ApplyResult struct {
	Proposal   *Proposal   `json:"proposal"`
	AdoptionID string      `json:"adoption_id"`
	Verified   bool        `json:"verified"`
	Evaluation *Evaluation `json:"evaluation,omitempty"`
	// EvidenceKind: prospective | retrospective | "" (đề xuất không có nội dung mới).
	EvidenceKind string       `json:"evidence_kind,omitempty"`
	Files        []FileChange `json:"files"`
	Notes        []string     `json:"notes"`
}

func relItemPath(id string) string { return knowledge.ItemsRel + "/" + id + ".md" }

func readOpt(p string) (*string, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// Apply áp dụng đề xuất sau khi người duyệt (chế độ propose, mục 11.5).
func Apply(ctx context.Context, o ApplyOptions) (ApplyResult, error) {
	var res ApplyResult
	if !o.Auto {
		if err := requireHuman("bint evolve apply"); err != nil {
			return res, err
		}
	} else if !o.K.Config.Evolution.Adoption.AutoLowRisk {
		return res, fmt.Errorf("tự áp dụng bị tắt (evolution.adoption.auto_low_risk = false)")
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	lock, err := AcquireLock(o.K.Paths.Runtime, "bint evolve apply "+o.Ref)
	if err != nil {
		return res, err
	}
	defer lock.Release()
	p, err := GetProposal(ctx, o.DB, o.Ref)
	if err != nil {
		return res, err
	}
	res.Proposal = p
	if p.Status != StatusProposed {
		return res, fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ áp dụng được đề xuất đang chờ (proposed)", p.ID, p.Status)
	}
	if probs, err := p.CheckBaseWS(o.K); err != nil {
		return res, err
	} else if len(probs) > 0 {
		var ds []string
		for _, b := range probs {
			ds = append(ds, b.ID+": "+b.Detail)
		}
		return res, fmt.Errorf("đề xuất %s không còn hợp lệ vì phiên bản gốc đã đổi (%s); bác bỏ nó và đề xuất lại trên phiên bản mới", p.ID, strings.Join(ds, "; "))
	}
	evs, err := Evaluations(ctx, o.DB, p.ID)
	if err != nil {
		return res, err
	}
	if o.Auto {
		if ok, why := AutoApplicable(p); !ok {
			return res, fmt.Errorf("đề xuất %s không được tự áp dụng: %s", p.ID, why)
		}
		o.Unverified, o.ConfirmRisk = false, false
	}
	if e, ok := VerifiedBy(evs, p); ok {
		res.Verified, res.Evaluation = true, &e
	} else if !o.Unverified {
		return res, fmt.Errorf("đề xuất %s chưa có kết quả supported, độc lập trên tập test với phiên bản gốc hiện tại; chạy `bint evolve evaluate %s --split test`, hoặc dùng --unverified để áp dụng như đề xuất chưa kiểm chứng (được ghi lại)", p.ID, p.ID)
	}
	if p.Risk == RiskHigh && !o.ConfirmRisk {
		return res, fmt.Errorf("đề xuất %s có rủi ro cao (%s); xem lại rồi thêm --confirm-risk", p.ID, strings.Join(p.RiskReasons, "; "))
	}

	if p.IsSkill() {
		return applySkill(ctx, o, p, res)
	}
	who := app.GitIdentity(ctx, o.K.Root)
	if o.Auto {
		who = AutoActor
	}
	items, _, err := o.K.Items()
	if err != nil {
		return res, err
	}
	byID := indexItems(items)
	src := o.K.Source()
	fp := o.K.Config.Context.Freshness.Fingerprint
	all := append([]*knowledge.Item{}, items...)
	type write struct {
		item   *knowledge.Item // nil = xoá
		id     string
		before *string
	}
	var writes []write
	reason := strings.TrimSpace(o.Reason)
	for _, ch := range p.Changes {
		w := write{id: ch.ItemID}
		if w.before, err = readOpt(o.K.Store.PathFor(ch.ItemID)); err != nil {
			return res, err
		}
		switch ch.Op {
		case ActionCreate:
			it, err := DraftItem(ch.Draft, ch.ItemID, nil, src, fp, p.ID, o.Now)
			if err != nil {
				return res, err
			}
			if r := (knowledge.Verifier{Src: src, All: all}).Verify(it); !r.OK() {
				return res, fmt.Errorf("mục mới %s không qua kiểm tra xác định được: %s", it.ID, firstError(r))
			}
			all = append(all, it)
			w.item = it
		case ActionUpdate:
			base := byID[ch.ItemID]
			it, err := DraftItem(ch.Draft, ch.ItemID, base, src, fp, p.ID, o.Now)
			if err != nil {
				return res, err
			}
			r := (knowledge.Verifier{Src: src, All: all}).Verify(it)
			if !r.OK() {
				return res, fmt.Errorf("bản sửa của %s không qua kiểm tra xác định được: %s", it.ID, firstError(r))
			}
			switch it.Status {
			case knowledge.StatusStale:
				if err := knowledge.Transition(it, knowledge.TransitionInput{To: knowledge.StatusVerified,
					Actor: knowledge.Actor{Kind: knowledge.ActorHuman, Name: who}, ChecksPassed: true,
					RequireHuman: o.K.Config.Knowledge.RequireHumanApprovalFor, Reason: "cập nhật theo đề xuất " + p.ID, Now: o.Now}); err != nil {
					return res, err
				}
			case knowledge.StatusVerified:
				// Người áp dụng đã xem nội dung sửa: là người duyệt của phiên bản mới.
				it.ReviewedBy, it.ReviewedAt, it.VerifiedAt = who, knowledge.Now(o.Now), knowledge.Now(o.Now)
				it.StatusReason = "cập nhật theo đề xuất " + p.ID
			}
			w.item = it
		case ActionDeprecate:
			cp := *byID[ch.ItemID]
			why := "đề xuất tiến hoá " + p.ID
			if ch.Reason != "" {
				why += ": " + ch.Reason
			}
			if err := knowledge.Transition(&cp, knowledge.TransitionInput{To: knowledge.StatusDeprecated,
				Actor: knowledge.Actor{Kind: knowledge.ActorHuman, Name: who}, Reason: why, Now: o.Now}); err != nil {
				return res, err
			}
			if cp.ReviewedBy == "" {
				cp.ReviewedBy, cp.ReviewedAt = who, knowledge.Now(o.Now)
			}
			w.item = &cp
		case ActionDelete:
			// w.item = nil
		}
		writes = append(writes, w)
	}

	// Ghi; lỗi giữa chừng → khôi phục những gì đã ghi.
	var done []write
	restore := func() {
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			if d.before == nil {
				os.Remove(o.K.Store.PathFor(d.id))
			} else {
				os.WriteFile(o.K.Store.PathFor(d.id), []byte(*d.before), 0o644)
			}
		}
	}
	for _, w := range writes {
		var err error
		if w.item == nil {
			err = o.K.Store.Remove(w.id)
		} else {
			err = o.K.Store.Save(w.item)
		}
		if err != nil {
			restore()
			return res, fmt.Errorf("ghi %s: %w (đã khôi phục)", w.id, err)
		}
		done = append(done, w)
	}
	for _, w := range writes {
		after, err := readOpt(o.K.Store.PathFor(w.id))
		if err != nil {
			return res, err
		}
		res.Files = append(res.Files, FileChange{Path: relItemPath(w.id), Before: w.before, After: after})
	}
	if err := finishApply(ctx, o, p, &res, reason, restore); err != nil {
		return res, err
	}
	for _, ch := range p.Changes {
		if ch.Op == ActionCreate {
			res.Notes = append(res.Notes, fmt.Sprintf("mục mới %s ở candidate (do agent soạn): duyệt bằng `bint knowledge review` hoặc `bint knowledge approve %s`", ch.ItemID, ch.ItemID))
		}
	}
	if !res.Verified {
		res.Notes = append(res.Notes, "đã áp dụng khi CHƯA được kiểm chứng định lượng; lịch sử ghi rõ điều này")
	}
	if res.EvidenceKind == KindRetrospective {
		res.Notes = append(res.Notes, "căn cứ là đối chiếu hồi cứu (tác vụ trước đề xuất): còn rủi ro rò rỉ gián tiếp; xác nhận lại bằng `bint evolve evaluate --prospective` khi có tác vụ mới")
	}
	res.Notes = append(res.Notes, "tệp trong "+knowledge.ItemsRel+" đã đổi: xem `git diff`, commit để chia sẻ; quay lui bằng `bint evolve rollback "+p.ID+"`")
	return res, nil
}

func firstError(r knowledge.Result) string {
	for _, f := range r.Findings {
		if f.Level == knowledge.LevelError {
			return f.Code + ": " + f.Detail
		}
	}
	return ""
}

// batchModel trả về harness và mô hình chính của đợt đánh giá (mục 12.6).
func batchModel(ctx context.Context, db *sql.DB, batchID string) (string, string, error) {
	var h string
	var m sql.NullString
	err := db.QueryRowContext(ctx, `SELECT harness, (SELECT model FROM evaluation_runs r WHERE r.batch_id = b.batch_id
		AND model IS NOT NULL GROUP BY model ORDER BY COUNT(*) DESC LIMIT 1) FROM eval_batches b WHERE batch_id = ?`, batchID).Scan(&h, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	primary, _, _ := strings.Cut(m.String, ",")
	return h, primary, err
}

// Reject bác bỏ đề xuất đang chờ.
func Reject(ctx context.Context, db *sql.DB, ref, reason string, now time.Time) (*Proposal, error) {
	if err := requireHuman("bint evolve reject"); err != nil {
		return nil, err
	}
	p, err := GetProposal(ctx, db, ref)
	if err != nil {
		return nil, err
	}
	if p.Status != StatusProposed {
		return p, fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ bác bỏ được đề xuất đang chờ", p.ID, p.Status)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "người duyệt bác bỏ"
	}
	p.Status, p.StatusReason = StatusRejected, reason
	return p, SetStatus(ctx, db, p.ID, StatusRejected, reason, now)
}

// Adoption là một bản ghi áp dụng/quay lui.
type Adoption struct {
	ID           string       `json:"adoption_id"`
	ProposalID   string       `json:"proposal_id"`
	Action       string       `json:"action"`
	Actor        string       `json:"actor"`
	Verified     bool         `json:"verified"`
	EvaluationID string       `json:"evaluation_id,omitempty"`
	Harness      string       `json:"harness,omitempty"`
	Model        string       `json:"model,omitempty"`
	Files        []FileChange `json:"-"`
	Reason       string       `json:"reason"`
	CreatedAt    string       `json:"created_at"`
}

// Adoptions liệt kê bản ghi áp dụng/quay lui, mới nhất trước; proposalID rỗng = mọi đề xuất.
func Adoptions(ctx context.Context, db *sql.DB, proposalID string) ([]Adoption, error) {
	q, args := `SELECT adoption_id, proposal_id, action, actor, verified, COALESCE(evaluation_id, ''), COALESCE(harness, ''),
		COALESCE(model, ''), files, COALESCE(reason, ''), created_at FROM evolution_adoptions`, []any{}
	if proposalID != "" {
		q += ` WHERE proposal_id = ?`
		args = append(args, proposalID)
	}
	rows, err := db.QueryContext(ctx, q+` ORDER BY created_at DESC, rowid DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Adoption
	for rows.Next() {
		var a Adoption
		var v int
		var files string
		if err := rows.Scan(&a.ID, &a.ProposalID, &a.Action, &a.Actor, &v, &a.EvaluationID, &a.Harness, &a.Model, &files, &a.Reason, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Verified = v == 1
		if err := json.Unmarshal([]byte(files), &a.Files); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RollbackOptions điều khiển `bint evolve rollback`.
type RollbackOptions struct {
	K      *app.KnowledgeWS
	DB     *sql.DB
	Ref    string
	Force  bool // ghi đè cả khi tệp đã bị sửa sau khi áp dụng
	Reason string
	Now    time.Time
	// Auto: vòng tiến hoá tự quay lui khi phát hiện hồi quy; chỉ với đề xuất do chính
	// vòng tiến hoá tự áp dụng (tác nhân AutoActor).
	Auto bool
}

// RollbackResult là kết quả quay lui.
type RollbackResult struct {
	Proposal   *Proposal    `json:"proposal"`
	AdoptionID string       `json:"adoption_id"`
	Files      []FileChange `json:"files"`
}

// Rollback khôi phục các tệp tri thức về trạng thái trước lần áp dụng đề xuất.
func Rollback(ctx context.Context, o RollbackOptions) (RollbackResult, error) {
	var res RollbackResult
	if !o.Auto {
		if err := requireHuman("bint evolve rollback"); err != nil {
			return res, err
		}
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	lock, err := AcquireLock(o.K.Paths.Runtime, "bint evolve rollback "+o.Ref)
	if err != nil {
		return res, err
	}
	defer lock.Release()
	p, err := GetProposal(ctx, o.DB, o.Ref)
	if err != nil {
		return res, err
	}
	res.Proposal = p
	if p.Status != StatusApplied {
		return res, fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ quay lui được đề xuất đã áp dụng", p.ID, p.Status)
	}
	ads, err := Adoptions(ctx, o.DB, p.ID)
	if err != nil {
		return res, err
	}
	var applied *Adoption
	for i := range ads {
		if ads[i].Action == "apply" {
			applied = &ads[i]
			break
		}
	}
	if applied == nil {
		return res, fmt.Errorf("không có bản ghi áp dụng của %s", p.ID)
	}
	if o.Auto && applied.Actor != AutoActor {
		return res, fmt.Errorf("đề xuất %s do %s áp dụng: chỉ người dùng được quay lui", p.ID, applied.Actor)
	}
	var conflicts []string
	for _, f := range applied.Files {
		cur, err := readOpt(filepath.Join(o.K.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return res, err
		}
		if !sameOpt(cur, f.After) {
			conflicts = append(conflicts, f.Path)
		}
	}
	if len(conflicts) > 0 && !o.Force {
		return res, fmt.Errorf("tệp đã bị sửa sau khi áp dụng: %s; xem lại rồi dùng --force để ghi đè bằng bản trước khi áp dụng", strings.Join(conflicts, ", "))
	}
	for _, f := range applied.Files {
		abs := filepath.Join(o.K.Root, filepath.FromSlash(f.Path))
		cur, _ := readOpt(abs)
		if f.Before == nil {
			if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
				return res, err
			}
			pruneEvolvedDirs(o.K.Root, f.Path)
		} else {
			os.MkdirAll(filepath.Dir(abs), 0o755)
			if err := os.WriteFile(abs, []byte(*f.Before), 0o644); err != nil {
				return res, err
			}
		}
		res.Files = append(res.Files, FileChange{Path: f.Path, Before: cur, After: f.Before})
	}
	who := app.GitIdentity(ctx, o.K.Root)
	if o.Auto {
		who = AutoActor
	}
	reason := strings.TrimSpace(o.Reason)
	if reason == "" {
		reason = "người dùng quay lui"
	}
	res.AdoptionID = "ead_" + ulid.At(o.Now)
	// Không ghi được bản kiểm toán → trả tệp về trạng thái trước khi quay lui.
	undo := func(err error) (RollbackResult, error) {
		for _, f := range res.Files {
			abs := filepath.Join(o.K.Root, filepath.FromSlash(f.Path))
			if f.Before == nil {
				os.Remove(abs)
			} else {
				os.WriteFile(abs, []byte(*f.Before), 0o644)
			}
		}
		return res, fmt.Errorf("ghi bản kiểm toán: %w (đã trả tệp về như trước khi quay lui)", err)
	}
	files, _ := json.Marshal(res.Files)
	tx, err := o.DB.BeginTx(ctx, nil)
	if err != nil {
		return undo(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO evolution_adoptions (adoption_id, proposal_id, action, actor, verified,
		files, reason, created_at) VALUES (?, ?, 'rollback', ?, 0, ?, ?, ?)`,
		res.AdoptionID, p.ID, who, string(files), reason, store.FormatTime(o.Now)); err != nil {
		return undo(err)
	}
	if err := SetStatus(ctx, tx, p.ID, StatusRolledBack, reason, o.Now); err != nil {
		return undo(err)
	}
	if err := tx.Commit(); err != nil {
		return undo(err)
	}
	p.Status = StatusRolledBack
	return res, nil
}

func sameOpt(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// HistoryEntry là một dòng của `bint evolve history` (mục 11.4, 12.6, 12.8).
type HistoryEntry struct {
	Adoption
	ProposalAction string `json:"proposal_action"`
	Opportunity    string `json:"opportunity"`
	// ModelStatus: so mô hình của lần đánh giá làm căn cứ với mô hình hiện dùng.
	ModelStatus string `json:"model_status,omitempty"`
	// EvidenceKind: lần đối chiếu làm căn cứ là tiến cứu hay hồi cứu.
	EvidenceKind string   `json:"evidence_kind,omitempty"`
	Items        []string `json:"items"`
	// Theo dõi sau áp dụng (mức "được truy xuất"/"được áp dụng", không phải tác động).
	RetrievedSince int `json:"retrieved_since"`
	CitedSince     int `json:"cited_since"`
	// Chi phí tiến hoá theo dõi riêng (mục 12.8).
	GenerationUSD *float64 `json:"generation_cost_usd"`
	ProbeUSD      *float64 `json:"probe_cost_usd"`
}

// HistoryOf trả về lịch sử áp dụng/quay lui. currentModel là mô hình đang dùng (rỗng =
// không biết); bindModel = evolution.adoption.bind_to_model_version.
func HistoryOf(ctx context.Context, db *sql.DB, currentModel string, bindModel bool, cases map[string]*eval.Case) ([]HistoryEntry, error) {
	ads, err := Adoptions(ctx, db, "")
	if err != nil {
		return nil, err
	}
	var out []HistoryEntry
	for _, a := range ads {
		e := HistoryEntry{Adoption: a, Items: []string{}}
		p, err := GetProposal(ctx, db, a.ProposalID)
		if err != nil {
			return nil, err
		}
		e.ProposalAction, e.Opportunity, e.GenerationUSD = p.Action, p.Opportunity, p.CostUSD
		for _, ch := range p.Changes {
			e.Items = append(e.Items, ch.ItemID)
		}
		var probe sql.NullFloat64
		if err := db.QueryRowContext(ctx, `SELECT SUM(r.cost_usd) FROM evaluation_runs r JOIN evolution_evaluations e
			ON e.batch_id = r.batch_id WHERE e.proposal_id = ?`, p.ID).Scan(&probe); err != nil {
			return nil, err
		}
		if probe.Valid {
			v := probe.Float64
			e.ProbeUSD = &v
		}
		if a.Action == "apply" && a.EvaluationID != "" {
			evs, err := Evaluations(ctx, db, p.ID)
			if err != nil {
				return nil, err
			}
			for _, ev := range evs {
				if ev.ID == a.EvaluationID {
					e.EvidenceKind = EvidenceKind(p, ev.CaseIDs, cases)
				}
			}
		}
		if a.Action == "apply" {
			if bindModel && a.Verified {
				switch {
				case currentModel == "" || a.Model == "":
					e.ModelStatus = "không xác định được mô hình để đối chiếu"
				case !strings.Contains(currentModel, a.Model) && !strings.Contains(a.Model, currentModel):
					e.ModelStatus = fmt.Sprintf("chưa xác thực với mô hình hiện tại (%s; đã kiểm chứng với %s): kết quả cũ không dùng làm mốc cho mô hình mới; kiểm tra hồi quy: `bint evolve evaluate %s --regression --yes`", currentModel, a.Model, p.ID)
					if rv, ok, err := latestRegression(ctx, db, p.ID, currentModel); err != nil {
						return nil, err
					} else if ok {
						switch rv.Verdict {
						case VerdictSupported:
							e.ModelStatus = "đã xác thực lại với " + currentModel + " (kiểm tra hồi quy): " + rv.Detail
						case VerdictNotSupported:
							e.ModelStatus = "HỒI QUY với " + currentModel + ": không còn bằng chứng cải tiến có lợi (" + rv.Detail + "); cân nhắc quay lui"
						default:
							e.ModelStatus = "kiểm tra hồi quy với " + currentModel + " chưa kết luận: " + rv.Detail
						}
					}
				default:
					e.ModelStatus = "khớp mô hình hiện tại"
				}
			}
			for _, id := range e.Items {
				var n, c int
				db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT COALESCE(run_id, event_id)) FROM context_usages WHERE item_id = ? AND created_at >= ?`, id, a.CreatedAt).Scan(&n)
				db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT COALESCE(run_id, event_id)) FROM knowledge_citations WHERE item_id = ? AND created_at >= ?`, id, a.CreatedAt).Scan(&c)
				e.RetrievedSince += n
				e.CitedSince += c
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// CurrentModel trả về mô hình của lượt chạy gần nhất có ghi mô hình (để kiểm tra mục 12.6).
func CurrentModel(ctx context.Context, db *sql.DB, configured *string) string {
	if configured != nil && *configured != "" {
		return *configured
	}
	var m sql.NullString
	db.QueryRowContext(ctx, `SELECT model FROM runs WHERE model IS NOT NULL AND model <> '' ORDER BY started_at DESC LIMIT 1`).Scan(&m)
	primary, _, _ := strings.Cut(m.String, ",")
	return primary
}

// finishApply ghi bản kiểm toán áp dụng và đổi trạng thái đề xuất (một giao dịch). Lỗi →
// restore trả tệp về như trước khi áp dụng. Dùng chung cho đề xuất tri thức và skill.
func finishApply(ctx context.Context, o ApplyOptions, p *Proposal, resp *ApplyResult, reason string, restore func()) error {
	res := *resp
	who := app.GitIdentity(ctx, o.K.Root)
	if o.Auto {
		who = AutoActor
	}
	var err error
	res.AdoptionID = "ead_" + ulid.At(o.Now)
	if reason == "" {
		reason = "người duyệt chấp nhận"
	}
	if !res.Verified {
		reason += " (chưa kiểm chứng định lượng)"
	} else if res.EvidenceKind = EvidenceKind(p, res.Evaluation.CaseIDs, o.Cases); res.EvidenceKind != "" {
		reason += " (" + KindLabel(res.EvidenceKind) + ")"
	}
	harness, model, evalID := "", "", ""
	if res.Evaluation != nil {
		evalID = res.Evaluation.ID
		harness, model, err = batchModel(ctx, o.DB, res.Evaluation.BatchID)
		if err != nil {
			return err
		}
	}
	files, _ := json.Marshal(res.Files)
	tx, err := o.DB.BeginTx(ctx, nil)
	if err != nil {
		restore()
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO evolution_adoptions (adoption_id, proposal_id, action, actor, verified,
		evaluation_id, harness, model, files, reason, created_at) VALUES (?, ?, 'apply', ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?)`,
		res.AdoptionID, p.ID, who, map[bool]int{true: 1, false: 0}[res.Verified], evalID, harness, model, string(files), reason,
		store.FormatTime(o.Now)); err != nil {
		restore()
		return err
	}
	if err := SetStatus(ctx, tx, p.ID, StatusApplied, reason, o.Now); err != nil {
		restore()
		return err
	}
	if err := tx.Commit(); err != nil {
		restore()
		return err
	}
	p.Status = StatusApplied
	*resp = res
	return nil
}

// pruneEvolvedDirs xoá thư mục skill tiến hoá còn trống sau khi tệp bị gỡ (quay lui đề xuất tạo
// skill). Chỉ động tới .bint/skills/evolved: thư mục tri thức được giữ nguyên dù trống.
func pruneEvolvedDirs(root, rel string) {
	if !strings.HasPrefix(rel, skills.EvolvedRel+"/") {
		return
	}
	stop := filepath.Join(root, filepath.FromSlash(skills.EvolvedRel))
	for dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rel))); dir != stop && strings.HasPrefix(dir, stop); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil { // không rỗng
			return
		}
	}
}
