package evolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bint/internal/app"
	"bint/internal/fsafe"
	"bint/internal/hcomp"
)

// relHarnessPath là đường dẫn tệp thành phần tính từ gốc repository (cho FileChange và quay lui).
func relHarnessPath(rel string) string { return ".bint/" + hcomp.DirName + "/" + rel }

// harnessWrite ghi lại nội dung một tệp trước/sau khi áp dụng.
type harnessWrite struct {
	rel    string
	before *string
	after  []byte
}

// applyHarness áp dụng đề xuất quy trình sau khi người duyệt (chế độ propose, mục 11.5):
//   - create ghi thành phần ở trạng thái candidate (origin agent): chưa có hiệu lực tới khi người
//     duyệt bằng `bint harness component approve`;
//   - update giữ trạng thái của bản gốc (verified vẫn verified), tăng phiên bản và ghi người áp dụng
//     là người duyệt — họ đã xem nội dung sửa;
//   - deprecate chuyển thành phần sang deprecated.
//
// Mọi tệp đổi đều nằm trong .bint/harness và được ghi vào FileChange để quay lui.
func applyHarness(ctx context.Context, o ApplyOptions, p *Proposal, res ApplyResult) (ApplyResult, error) {
	who := app.GitIdentity(ctx, o.K.Root)
	if o.Auto {
		who = AutoActor
	}
	lib := hcomp.Load(o.K.Root)
	if err := validateHarnessChanges(p, lib, o.K.Source()); err != nil {
		return res, fmt.Errorf("đề xuất %s không còn qua kiểm tra xác định được: %w", p.ID, err)
	}
	dir := hcomp.Dir(o.K.Root)
	var writes []harnessWrite
	var notes []string
	for _, ch := range p.HarnessChanges {
		var comp hcomp.Component
		switch ch.Op {
		case ActionCreate:
			comp = ch.Draft.Component()
			notes = append(notes, fmt.Sprintf("%s %s ở candidate (do agent soạn, rủi ro %s): xem `bint harness component show %s`, duyệt bằng `bint harness component approve %s`",
				ch.Kind, ch.ID, hcomp.Risk(comp), ch.ID, ch.ID))
		case ActionUpdate:
			cur, _ := lib.Find(ch.Kind, ch.ID)
			comp = ch.Draft.Component()
			comp.Status, comp.Origin, comp.Version, comp.ApprovedBy = cur.Status, cur.Origin, cur.Version+1, ""
			if cur.Status == hcomp.StatusVerified {
				comp.ApprovedBy = who
			}
		case ActionDeprecate:
			comp, _ = lib.Find(ch.Kind, ch.ID)
			comp.Status, comp.ApprovedBy = hcomp.StatusDeprecated, who
		default:
			return res, fmt.Errorf("thao tác %q không hợp lệ", ch.Op)
		}
		files, err := hcomp.Files(comp)
		if err != nil {
			return res, err
		}
		rels := make([]string, 0, len(files))
		for r := range files {
			rels = append(rels, r)
		}
		sort.Strings(rels)
		for _, r := range rels {
			before, err := readOpt(filepath.Join(dir, filepath.FromSlash(r)))
			if err != nil {
				return res, err
			}
			writes = append(writes, harnessWrite{rel: r, before: before, after: files[r]})
		}
	}

	var done []harnessWrite
	restore := func() {
		for i := len(done) - 1; i >= 0; i-- {
			w, abs := done[i], filepath.Join(dir, filepath.FromSlash(done[i].rel))
			if w.before == nil {
				os.Remove(abs)
			} else {
				fsafe.WriteAtomic(abs, []byte(*w.before), 0o644)
			}
		}
	}
	for _, w := range writes {
		if err := fsafe.WriteAtomic(filepath.Join(dir, filepath.FromSlash(w.rel)), w.after, 0o644); err != nil {
			restore()
			return res, fmt.Errorf("ghi %s: %w (đã khôi phục)", w.rel, err)
		}
		done = append(done, w)
	}
	for _, w := range writes {
		af := string(w.after)
		res.Files = append(res.Files, FileChange{Path: relHarnessPath(w.rel), Before: w.before, After: &af})
	}
	reason := strings.TrimSpace(o.Reason)
	if reason == "" {
		reason = "người duyệt chấp nhận"
	}
	if !res.Verified {
		reason += " (chưa kiểm chứng định lượng)"
	} else if res.EvidenceKind = EvidenceKind(p, res.Evaluation.CaseIDs, o.Cases); res.EvidenceKind != "" {
		reason += " (" + KindLabel(res.EvidenceKind) + ")"
	}
	if err := finishApply(ctx, o, p, &res, reason, restore); err != nil {
		return res, err
	}
	res.Notes = append(res.Notes, notes...)
	if !res.Verified {
		res.Notes = append(res.Notes, "đã áp dụng khi CHƯA được kiểm chứng định lượng; lịch sử ghi rõ điều này")
	}
	if p.Pair != nil {
		res.Notes = append(res.Notes, "đề xuất này là một nửa của cặp chung/riêng; bản anh em "+p.Pair.Sibling+" không được áp dụng cùng (gộp hai bản phải là đề xuất mới và đối chiếu lại)")
	}
	res.Notes = append(res.Notes, "tệp trong .bint/"+hcomp.DirName+" đã đổi: xem `git diff`, commit để chia sẻ; quay lui bằng `bint evolve rollback "+p.ID+"`")
	return res, nil
}
