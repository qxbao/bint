package evolve

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/ulid"
)

// skillFile ghi lại nội dung một tệp của skill trước/sau khi áp dụng.
type skillWrite struct {
	rel    string
	before *string
	after  []byte // nil = xoá
}

// applySkill áp dụng đề xuất L2. Người duyệt đã xem nội dung (chế độ propose, mục 11.5):
// skill mới/sửa có hiệu lực ngay (status active) và được xuất bản cho harness ở lần
// `bint sync` tiếp theo (CLI chạy ngay sau khi áp dụng). Mọi tệp đổi đều nằm trong
// .bint/skills/evolved; skill gốc và lớp phủ không bao giờ bị đụng tới.
func applySkill(ctx context.Context, o ApplyOptions, p *Proposal, res ApplyResult) (ApplyResult, error) {
	who := app.GitIdentity(ctx, o.K.Root)
	list, _, err := skills.LoadEvolved(o.K.Root)
	if err != nil {
		return res, err
	}
	byName := indexEvolved(list)
	src := o.K.Source()
	fp := o.K.Config.Context.Freshness.Fingerprint
	harness, model, evalID := "", "", ""
	if res.Evaluation != nil {
		evalID = res.Evaluation.ID
		if harness, model, err = batchModel(ctx, o.DB, res.Evaluation.BatchID); err != nil {
			return res, err
		}
	}
	nowS := knowledge.Now(o.Now)
	after := map[string]*skills.Evolved{}
	var removed []string
	for _, c := range p.SkillChanges {
		cur := byName[c.Name]
		switch c.Op {
		case ActionCreate, ActionUpdate:
			refs, err := evidenceRefs(c.Draft.Evidence, src, fp)
			if err != nil {
				return res, err
			}
			ev := &skills.Evolved{Content: c.Draft.Content()}
			if c.Op == ActionCreate {
				ev.Meta = skills.EvolvedMeta{Schema: skills.EvolvedSchema, ID: ulid.At(o.Now), Name: c.Name, Status: skills.EvActive,
					Version: 1, CreatedAt: nowS, Origin: skills.EvolvedOrigin{Proposal: p.ID, Generator: p.Generator}}
				for _, r := range p.SupportingRuns {
					ev.Meta.Origin.Runs = append(ev.Meta.Origin.Runs, r.RunID)
				}
			} else {
				ev.Meta = cur.Meta
				ev.Meta.Versions = slices.Clone(cur.Meta.Versions)
				ev.Meta.Version = cur.Meta.Version + 1
				ev.Meta.UpdatedAt = nowS
			}
			ev.Meta.Triggers, ev.Meta.Evidence = c.Draft.Triggers, refs
			ev.Meta.Versions = append(ev.Meta.Versions, skills.VersionEntry{N: ev.Meta.Version, SHA256: ev.AsSkill().Hash(), Proposal: p.ID,
				Harness: harness, Model: model, Verified: res.Verified, Evaluation: evalID, At: nowS, By: who, Note: c.Reason})
			after[c.Name] = ev
		case ActionDeprecate:
			cp := *cur
			cp.Meta.Status, cp.Meta.UpdatedAt = skills.EvDeprecated, nowS
			cp.Meta.StatusReason = "đề xuất tiến hoá " + p.ID
			if c.Reason != "" {
				cp.Meta.StatusReason += ": " + c.Reason
			}
			after[c.Name] = &cp
		case ActionDelete:
			removed = append(removed, c.Name)
		}
	}
	// Phép hợp nhất: skill chính ghi lại nó đã nhận kiến thức từ skill nào.
	for _, c := range p.SkillChanges {
		if c.Op == ActionDeprecate && c.MergedInto != "" {
			if t := after[c.MergedInto]; t != nil && !slices.Contains(t.Meta.MergedFrom, c.Name) {
				t.Meta.MergedFrom = append(t.Meta.MergedFrom, c.Name)
				if e := t.Last(); e != nil {
					for i := range t.Meta.Versions {
						if t.Meta.Versions[i].N == t.Meta.Version && !strings.Contains(t.Meta.Versions[i].Note, c.Name) {
							t.Meta.Versions[i].Note += " (gộp " + c.Name + ")"
						}
					}
				}
			}
			after[c.Name].Meta.StatusReason += "; đã gộp vào " + c.MergedInto
		}
	}

	var writes []skillWrite
	names := make([]string, 0, len(after))
	for n := range after {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files, err := after[n].Files()
		if err != nil {
			return res, err
		}
		var rels []string
		for rel := range files {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			b, err := readOpt(filepath.Join(o.K.Root, filepath.FromSlash(rel)))
			if err != nil {
				return res, err
			}
			writes = append(writes, skillWrite{rel: rel, before: b, after: files[rel]})
		}
	}
	for _, n := range removed {
		ents, err := os.ReadDir(skills.EvolvedDir(o.K.Root, n))
		if err != nil {
			return res, err
		}
		var rels []string
		for _, e := range ents {
			if e.IsDir() {
				vs, _ := os.ReadDir(filepath.Join(skills.EvolvedDir(o.K.Root, n), e.Name()))
				for _, v := range vs {
					rels = append(rels, skills.EvolvedRelPath(n, e.Name(), v.Name()))
				}
			} else {
				rels = append(rels, skills.EvolvedRelPath(n, e.Name()))
			}
		}
		for _, rel := range rels {
			b, err := readOpt(filepath.Join(o.K.Root, filepath.FromSlash(rel)))
			if err != nil {
				return res, err
			}
			writes = append(writes, skillWrite{rel: rel, before: b})
		}
	}

	var done []skillWrite
	restore := func() {
		for i := len(done) - 1; i >= 0; i-- {
			w, abs := done[i], filepath.Join(o.K.Root, filepath.FromSlash(done[i].rel))
			if w.before == nil {
				os.Remove(abs)
				pruneEvolvedDirs(o.K.Root, w.rel)
			} else {
				os.MkdirAll(filepath.Dir(abs), 0o755)
				os.WriteFile(abs, []byte(*w.before), 0o644)
			}
		}
	}
	for _, w := range writes {
		abs := filepath.Join(o.K.Root, filepath.FromSlash(w.rel))
		var err error
		if w.after == nil {
			err = os.Remove(abs)
		} else {
			os.MkdirAll(filepath.Dir(abs), 0o755)
			err = os.WriteFile(abs, w.after, 0o644)
		}
		if err != nil {
			restore()
			return res, fmt.Errorf("ghi %s: %w (đã khôi phục)", w.rel, err)
		}
		done = append(done, w)
	}
	for _, n := range removed {
		os.Remove(filepath.Join(skills.EvolvedDir(o.K.Root, n), "versions"))
		os.Remove(skills.EvolvedDir(o.K.Root, n))
	}
	for _, w := range writes {
		var af *string
		if w.after != nil {
			s := string(w.after)
			af = &s
		}
		res.Files = append(res.Files, FileChange{Path: w.rel, Before: w.before, After: af})
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
	res.Notes = append(res.Notes, "skill được xuất bản cho harness bằng `bint sync` (chạy tự động sau khi áp dụng); kiểm tra bản thay đổi bằng `bint skill diff <tên>`")
	if !res.Verified {
		res.Notes = append(res.Notes, "đã áp dụng khi CHƯA được kiểm chứng định lượng; lịch sử ghi rõ điều này")
	}
	if res.EvidenceKind == KindRetrospective {
		res.Notes = append(res.Notes, "căn cứ là đối chiếu hồi cứu (tác vụ trước đề xuất): còn rủi ro rò rỉ gián tiếp; xác nhận lại bằng `bint evolve evaluate --prospective` khi có tác vụ mới")
	}
	res.Notes = append(res.Notes, "tệp trong "+skills.EvolvedRel+" đã đổi: xem `git diff`, commit để chia sẻ; quay lui bằng `bint evolve rollback "+p.ID+"`")
	return res, nil
}

// ---- Phiên bản skill gắn với mô hình; phát hiện hồi quy (mục 12.6) ----

// Trạng thái xác thực của skill với mô hình hiện tại.
const (
	ModelMatch      = "match"     // phiên bản hiện tại được kiểm chứng với mô hình đang dùng
	ModelRevalid    = "revalid"   // chạy lại tập nhỏ với mô hình hiện tại: vẫn có lợi
	ModelRegressed  = "regressed" // chạy lại với mô hình hiện tại: không còn lợi
	ModelInconcl    = "inconclusive"
	ModelStale      = "stale_model" // chưa xác thực với mô hình hiện tại
	ModelUnverified = "unverified"  // phiên bản chưa từng được kiểm chứng định lượng
	ModelUnknown    = "unknown"     // không xác định được mô hình
)

// SkillModelStatus là trạng thái xác thực mô hình của một skill.
type SkillModelStatus struct {
	Skill    string `json:"skill"`
	Version  int    `json:"version"`
	Status   string `json:"status"`
	Verified string `json:"verified_model,omitempty"`
	Current  string `json:"current_model,omitempty"`
	Proposal string `json:"proposal,omitempty"`
	Detail   string `json:"detail"`
}

// ModelStatuses đối chiếu mô hình mà từng skill đang hoạt động được kiểm chứng với mô
// hình hiện dùng. Kết quả đối chiếu cũ không được dùng làm mốc cho mô hình mới: skill được
// đánh dấu "chưa xác thực với mô hình hiện tại" cho tới khi chạy lại tập nhỏ
// (`bint evolve evaluate <đề xuất> --regression`).
func ModelStatuses(ctx context.Context, db *sql.DB, list []*skills.Evolved, currentModel string) ([]SkillModelStatus, error) {
	var out []SkillModelStatus
	for _, e := range list {
		if !e.Active() {
			continue
		}
		st := SkillModelStatus{Skill: e.Meta.Name, Version: e.Meta.Version, Current: currentModel}
		last := e.Last()
		if last == nil {
			st.Status, st.Detail = ModelUnknown, "không có lịch sử phiên bản"
			out = append(out, st)
			continue
		}
		st.Proposal, st.Verified = last.Proposal, last.Model
		same := func(a, b string) bool {
			return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
		}
		switch {
		case !last.Verified:
			st.Status, st.Detail = ModelUnverified, "phiên bản này chưa từng được kiểm chứng định lượng"
		case currentModel == "" || last.Model == "":
			st.Status, st.Detail = ModelUnknown, "không xác định được mô hình để đối chiếu"
		case same(currentModel, last.Model):
			st.Status, st.Detail = ModelMatch, "khớp mô hình đã kiểm chứng"
		default:
			st.Status = ModelStale
			st.Detail = fmt.Sprintf("đã kiểm chứng với %s, mô hình hiện dùng là %s: kết quả cũ không dùng làm mốc cho mô hình mới", last.Model, currentModel)
		}
		if st.Status == ModelStale || st.Status == ModelUnknown {
			if ev, ok, err := latestRegression(ctx, db, last.Proposal, currentModel); err != nil {
				return nil, err
			} else if ok {
				switch ev.Verdict {
				case VerdictSupported:
					st.Status, st.Detail = ModelRevalid, "chạy lại tập nhỏ với "+currentModel+": skill vẫn có lợi ("+ev.Detail+")"
				case VerdictNotSupported:
					st.Status, st.Detail = ModelRegressed, "chạy lại tập nhỏ với "+currentModel+": không còn bằng chứng skill có lợi ("+ev.Detail+"); cân nhắc `bint evolve` loại bỏ hoặc sửa skill"
				default:
					st.Status, st.Detail = ModelInconcl, "chạy lại với "+currentModel+" nhưng chưa kết luận được ("+ev.Detail+")"
				}
			} else if st.Status == ModelStale {
				st.Detail += "; kiểm tra hồi quy: `bint evolve evaluate " + last.Proposal + " --regression --yes`"
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// latestRegression trả về lần kiểm tra hồi quy gần nhất của đề xuất với mô hình đang dùng.
func latestRegression(ctx context.Context, db *sql.DB, proposalID, model string) (Evaluation, bool, error) {
	evs, err := Evaluations(ctx, db, proposalID)
	if err != nil {
		return Evaluation{}, false, err
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind != EvalRegression || evs[i].Verdict == "" {
			continue
		}
		_, m, err := batchModel(ctx, db, evs[i].BatchID)
		if err != nil {
			return Evaluation{}, false, err
		}
		if m != "" && (strings.Contains(model, m) || strings.Contains(m, model)) {
			return evs[i], true, nil
		}
	}
	return Evaluation{}, false, nil
}

// OriginalCases trả về tác vụ của lần đối chiếu supported gần nhất làm căn cứ áp dụng đề xuất
// (tập đại diện cho kiểm tra hồi quy).
func OriginalCases(ctx context.Context, db *sql.DB, p *Proposal) ([]string, error) {
	evs, err := Evaluations(ctx, db, p.ID)
	if err != nil {
		return nil, err
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind != EvalRegression && evs[i].Verdict == VerdictSupported {
			return evs[i].CaseIDs, nil
		}
	}
	return nil, nil
}
