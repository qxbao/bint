package evolvesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"bint/internal/evolve"
)

// Hàm chỉ đọc dựng hộp xác nhận của TUI (docs/tui-plan.md mục 4.3.4): cho biết trước điều kiện áp dụng và
// tệp sẽ khôi phục, không ghi gì. `evolve.Apply`/`Rollback` tự kiểm lại mọi điều kiện lúc ghi nên sai lệch giữa
// lúc xem và lúc ghi chỉ làm thất bại an toàn (riêng rollback --force có thêm ExpectConflicts).

// Lý do một đề xuất không áp dụng được (ApplyPlan.Blocked).
const (
	BlockedStatus = "status" // không ở trạng thái proposed
	BlockedBase   = "base"   // phiên bản gốc đã đổi
)

// ApplyPlan là điều kiện áp dụng một đề xuất, tính từ trạng thái hiện tại.
type ApplyPlan struct {
	Proposal     *evolve.Proposal
	State        string
	BaseProblems []evolve.BaseProblem
	// Blocked: "" nếu áp dụng được; BlockedStatus hoặc BlockedBase. BlockedDetail là mô tả để hiện.
	Blocked       string
	BlockedDetail string
	// Verified: đã có kết quả supported, độc lập trên tập test với phiên bản gốc hiện tại. NeedsUnverified: chưa
	// có, nên áp dụng cần --unverified (người phải chấp nhận tường minh).
	Verified        bool
	Evaluation      *evolve.Evaluation
	NeedsUnverified bool
	// Risk là mức rủi ro Bint tính; NeedsConfirmRisk: rủi ro cao nên cần --confirm-risk.
	Risk             string
	RiskReasons      []string
	NeedsConfirmRisk bool
	IsSkill          bool
	IsHarness        bool
	// Changes liệt kê "thao tác:đối tượng" của đề xuất.
	Changes []string
}

// ChangeNames liệt kê "thao tác:đối tượng" của đề xuất (tri thức, skill hoặc thành phần quy trình).
func ChangeNames(p *evolve.Proposal) []string {
	switch {
	case p.IsSkill():
		return evolve.SkillChangeNames(p)
	case p.IsHarness():
		return evolve.HarnessChangeNames(p)
	}
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Op+":"+c.ItemID)
	}
	return out
}

// PlanApply tính điều kiện áp dụng đề xuất (chỉ đọc). Chỉ dùng ApplyOptions.Ref.
func PlanApply(ctx context.Context, e *Workspace, opts ApplyOptions) (*ApplyPlan, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	p, err := evolve.GetProposal(ctx, e.Store.DB, opts.Ref)
	if err != nil {
		return nil, err
	}
	st, probs := e.ProposalState(p)
	pl := &ApplyPlan{Proposal: p, State: st, BaseProblems: probs, Risk: p.Risk, RiskReasons: p.RiskReasons,
		IsSkill: p.IsSkill(), IsHarness: p.IsHarness(), Changes: ChangeNames(p)}
	switch {
	case p.Status != evolve.StatusProposed:
		pl.Blocked = BlockedStatus
		pl.BlockedDetail = p.Status
	case len(probs) > 0:
		var ds []string
		for _, b := range probs {
			ds = append(ds, b.ID+": "+b.Detail)
		}
		pl.Blocked, pl.BlockedDetail = BlockedBase, strings.Join(ds, "; ")
	}
	evs, err := evolve.Evaluations(ctx, e.Store.DB, p.ID)
	if err != nil {
		return nil, err
	}
	if ev, ok := evolve.VerifiedBy(evs, p); ok {
		pl.Verified, pl.Evaluation = true, &ev
	} else {
		pl.NeedsUnverified = true
	}
	pl.NeedsConfirmRisk = p.Risk == evolve.RiskHigh
	return pl, nil
}

// RollbackFile là một tệp sẽ được khôi phục: Before là nội dung sẽ khôi phục (nil = xoá tệp), Applied là nội dung
// lúc áp dụng, Current là nội dung hiện tại (nil = không tồn tại).
type RollbackFile struct {
	Path     string
	Before   *string
	Applied  *string
	Current  *string
	Conflict bool
}

// RollbackPlan là kế hoạch quay lui: tệp sẽ khôi phục và tệp đã bị sửa sau khi áp dụng (xung đột).
type RollbackPlan struct {
	Proposal  *evolve.Proposal
	Adoption  evolve.Adoption
	Files     []RollbackFile
	Conflicts []string
	IsSkill   bool
}

// PlanRollback tính tệp sẽ khôi phục và xung đột của việc quay lui đề xuất đã áp dụng (chỉ đọc).
func PlanRollback(ctx context.Context, e *Workspace, opts RollbackOptions) (*RollbackPlan, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	p, err := evolve.GetProposal(ctx, e.Store.DB, opts.Ref)
	if err != nil {
		return nil, err
	}
	if p.Status != evolve.StatusApplied {
		return nil, fmt.Errorf("đề xuất %s ở trạng thái %s: chỉ quay lui được đề xuất đã áp dụng", p.ID, p.Status)
	}
	ads, err := evolve.Adoptions(ctx, e.Store.DB, p.ID)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ads, func(a evolve.Adoption) bool { return a.Action == "apply" })
	if i < 0 {
		return nil, fmt.Errorf("không có bản ghi áp dụng của %s", p.ID)
	}
	pl := &RollbackPlan{Proposal: p, Adoption: ads[i], IsSkill: p.IsSkill(), Conflicts: []string{}}
	for _, f := range ads[i].Files {
		cur, err := readOptional(filepath.Join(e.K.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, err
		}
		rf := RollbackFile{Path: f.Path, Before: f.Before, Applied: f.After, Current: cur, Conflict: !sameOptional(cur, f.After)}
		if rf.Conflict {
			pl.Conflicts = append(pl.Conflicts, f.Path)
		}
		pl.Files = append(pl.Files, rf)
	}
	slices.Sort(pl.Conflicts)
	return pl, nil
}

// ContentHash là dấu vân tay nội dung một tệp cho ExpectConflicts: sha256 dạng hex, "-" nếu tệp không tồn tại.
func ContentHash(s *string) string {
	if s == nil {
		return "-"
	}
	h := sha256.Sum256([]byte(*s))
	return hex.EncodeToString(h[:])
}

// SeenConflicts là ExpectConflicts tương ứng với kế hoạch này: mỗi tệp xung đột kèm dấu vân tay nội dung hiện tại
// mà người đang xem (TUI gửi lại khi người đồng ý ghi đè).
func (pl *RollbackPlan) SeenConflicts() map[string]string {
	out := map[string]string{}
	for _, f := range pl.Files {
		if f.Conflict {
			out[f.Path] = ContentHash(f.Current)
		}
	}
	return out
}

// sameConflicts: tập tệp xung đột của kế hoạch hiện tại trùng tập đã xem và nội dung từng tệp chưa đổi.
func sameConflicts(pl *RollbackPlan, seen map[string]string) bool {
	now := pl.SeenConflicts()
	if len(now) != len(seen) {
		return false
	}
	for p, h := range now {
		if seen[p] != h {
			return false
		}
	}
	return true
}

func readOptional(path string) (*string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "(không)"
	}
	return strings.Join(l, ", ")
}
