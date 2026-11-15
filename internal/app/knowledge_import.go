package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/knowledge"
	"bint/internal/knowledge/importer"
)

// ImportOptions điều khiển `bint knowledge import` (docs/next-directions.md mục B3). Thẻ `flag`/`form` như AddOptions.
// Mục nhập luôn origin imported, trạng thái candidate; không gọi mô hình, không mạng.
type ImportOptions struct {
	// From: claude-memory | codex-memory | cursor-rules | file, hoặc đường dẫn một tệp Markdown cục bộ.
	From string `flag:"from" form:"required,choices=claude-memory|codex-memory|cursor-rules|file"`
	// Path ghi đè thư mục/tệp nguồn (bắt buộc với file).
	Path string `flag:"path" form:"suggest=files"`
	// Type ghi đè loại mục; rỗng = theo loại gợi ý của nguồn (mặc định warning).
	Type   string `flag:"type" form:"advanced,choices=rule|warning|pattern|fact|decision|lesson"`
	DryRun bool   `flag:"dry-run" form:"hidden"`
	// Expect là băm kế hoạch người đã xem (ImportPlan.Hash). Khác kế hoạch dựng lại lúc ghi thì không ghi gì
	// (ErrImportPlanChanged), như Xuất bản.
	Expect string
	Now    time.Time
}

// ImportDefaults trả giá trị khởi đầu của form nhập.
func ImportDefaults(config.Config) ImportOptions {
	return ImportOptions{From: knowledge.ImportClaudeMemory}
}

// Validate kiểm tra ràng buộc mức giá trị; lỗi là UsageError.
func (o ImportOptions) Validate() error {
	if o.Type != "" && !slices.Contains(knowledge.Types, o.Type) {
		return Usagef("--type phải là một trong: %s", strings.Join(knowledge.Types, ", "))
	}
	if _, _, err := importer.ParseFrom(o.From, o.Path); err != nil {
		return Usagef("%v", err)
	}
	return nil
}

// ErrImportPlanChanged: kế hoạch lúc ghi khác bản người đã xem (nguồn hoặc kho đã đổi).
var ErrImportPlanChanged = errors.New("kế hoạch nhập đã thay đổi từ lúc xem trước; xem trước lại rồi ghi")

// Kết cục của một ý.
const (
	ImportCreate = "create"
	ImportSkip   = "skip"
)

// ImportIdea là một ý trong kế hoạch: kết cục dự kiến kèm lý do (khi skip) hoặc bằng chứng (khi create).
type ImportIdea struct {
	Ref      string   `json:"ref"`
	Title    string   `json:"title"`
	Type     string   `json:"type,omitempty"`
	Outcome  string   `json:"outcome"`
	Reason   string   `json:"reason,omitempty"` // một trong importer.Skip*
	Detail   string   `json:"detail,omitempty"`
	Evidence []string `json:"evidence,omitempty"` // path[:a-b][#symbol]
	Paths    []string `json:"paths,omitempty"`
	ID       string   `json:"id,omitempty"` // chỉ có sau khi ghi

	body string
	ev   []knowledge.Evidence
}

// ImportPlan là kết quả xem trước (và phần dùng chung của kết quả ghi).
type ImportPlan struct {
	Source string       `json:"source"`
	Batch  string       `json:"batch"` // nhãn lô nguon:import-<nguồn>
	Files  int          `json:"files"`
	Ideas  []ImportIdea `json:"ideas"`
	// Hash định danh nội dung kế hoạch (không gồm ID mục sẽ cấp); dùng làm ImportOptions.Expect.
	Hash string `json:"hash"`
}

// Count đếm ý theo kết cục.
func (p ImportPlan) Count(outcome string) int {
	n := 0
	for _, i := range p.Ideas {
		if i.Outcome == outcome {
			n++
		}
	}
	return n
}

// SkipCounts đếm ý bỏ qua theo lý do.
func (p ImportPlan) SkipCounts() map[string]int {
	m := map[string]int{}
	for _, i := range p.Ideas {
		if i.Outcome == ImportSkip {
			m[i.Reason]++
		}
	}
	return m
}

// ImportResult là kết quả `knowledge import`.
type ImportResult struct {
	Plan    ImportPlan
	Written bool     // false khi DryRun hoặc không có ý nào để tạo
	IDs     []string // ID mục đã ghi
	// Mismatch: có Expect mà kế hoạch dựng lại lúc ghi khác bản đã xem; không ghi gì, Plan là kế hoạch mới.
	Mismatch bool
}

func (k *KnowledgeWS) importLocate(opts ImportOptions) (source string, files []importer.File, err error) {
	source, path, err := importer.ParseFrom(opts.From, opts.Path)
	if err != nil {
		return "", nil, Usagef("%v", err)
	}
	lo := importer.LocateOptions{Root: k.Repo.Toplevel, MainRoot: k.Repo.MainWorktree(), Path: path}
	if lo.Root == "" {
		lo.Root = k.Root
	}
	if k.Root != lo.Root && lo.MainRoot == "" {
		lo.MainRoot = k.Root
	}
	files, err = importer.Locate(source, lo)
	return source, files, err
}

// PlanImport dựng kế hoạch nhập mà không ghi gì. Chỉ dành cho người (cả xem trước): bộ nhớ của harness nằm ngoài repository
// và có thể chứa thông tin riêng tư, nên agent không được đọc nó qua Bint.
func PlanImport(ctx context.Context, k *KnowledgeWS, opts ImportOptions) (*ImportPlan, error) {
	if err := RequireHuman("bint knowledge import"); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	source, files, err := k.importLocate(opts)
	if err != nil {
		return nil, err
	}
	plan := &ImportPlan{Source: source, Batch: knowledge.BatchTagPrefix + "import-" + source, Files: len(files)}
	src := k.Source()
	finder := importer.NewFinder(src.Files(), src.Read)
	all, _, err := k.Items()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{} // ý đã vào kế hoạch (cùng lần nhập)
	for _, f := range files {
		data, rerr := importer.Read(f)
		if rerr != nil {
			plan.Ideas = append(plan.Ideas, ImportIdea{Ref: source + ":" + f.Name, Title: f.Name, Outcome: ImportSkip,
				Reason: importer.SkipCheckFailed, Detail: rerr.Error()})
			continue
		}
		for _, idea := range importer.Split(source, f.Name, data) {
			plan.Ideas = append(plan.Ideas, k.planIdea(ctx, opts, idea, finder, all, seen))
		}
	}
	plan.Hash = planHash(plan)
	return plan, nil
}

func (k *KnowledgeWS) planIdea(ctx context.Context, opts ImportOptions, idea importer.Idea, finder *importer.Finder, all []*knowledge.Item, seen map[string]bool) ImportIdea {
	out := ImportIdea{Ref: idea.Ref, Title: idea.Title, Outcome: ImportSkip, Paths: idea.Paths}
	skip := func(reason, detail string) ImportIdea {
		out.Reason, out.Detail = reason, detail
		return out
	}
	if idea.Skip != "" {
		return skip(idea.Skip, idea.SkipDetail)
	}
	if dup, imported := findDuplicate(all, idea); dup != nil {
		if imported {
			return skip(importer.SkipAlreadyImported, fmt.Sprintf("đã nhập thành %s (%s)", dup.ID, dup.Status))
		}
		return skip(importer.SkipDuplicate, fmt.Sprintf("trùng với %s (%s)", dup.ID, dup.Title))
	}
	key := normKey(idea.Title + "\n" + idea.Body)
	if seen[key] {
		return skip(importer.SkipDuplicate, "trùng với một ý khác trong cùng lần nhập")
	}
	ev := finder.Find(idea.Title + "\n" + idea.Body)
	if len(ev) == 0 {
		return skip(importer.SkipNoEvidence, "không tìm thấy tệp hay ký hiệu nào của repository trong nội dung")
	}
	typ := opts.Type
	if typ == "" {
		typ = idea.Type
	}
	if typ == "" {
		typ = knowledge.TypeWarning
	}
	d := k.importDraft(idea, typ, ev, opts.Now)
	it := k.buildItem(ctx, d)
	if res := (knowledge.Verifier{Src: k.Source(), All: all}).Verify(it); !res.OK() {
		detail := firstError(res)
		if detail == "" {
			detail = "không qua kiểm tra"
		}
		return skip(importer.SkipCheckFailed, detail)
	}
	seen[key] = true
	out.Outcome, out.Type, out.body, out.ev = ImportCreate, typ, idea.Body, ev
	for _, e := range ev {
		out.Evidence = append(out.Evidence, e.String())
	}
	if len(out.Paths) == 0 {
		out.Paths = distinctPaths(ev)
	}
	return out
}

func (k *KnowledgeWS) importDraft(idea importer.Idea, typ string, ev []knowledge.Evidence, now time.Time) itemDraft {
	paths := idea.Paths
	if len(paths) == 0 {
		paths = distinctPaths(ev)
	}
	return itemDraft{
		Type: typ, Title: idea.Title, Body: idea.Body,
		Scope:      knowledge.Scope{Paths: paths, Tags: []string{"import", knowledge.BatchTagPrefix + "import-" + idea.Source}},
		Origin:     knowledge.Origin{Kind: knowledge.OriginImported, Source: idea.Source, Inputs: []string{idea.Ref}},
		Evidence:   ev,
		Confidence: "low",
		Now:        now,
	}
}

func distinctPaths(ev []knowledge.Evidence) []string {
	var out []string
	for _, e := range ev {
		if !slices.Contains(out, e.Path) {
			out = append(out, e.Path)
		}
	}
	return out
}

func normKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// findDuplicate tìm mục đã có cùng tiêu đề hoặc nội dung. Mục còn hiệu lực là trùng; mục imported (kể cả đã bị từ chối,
// deprecated) là "đã nhập": chạy lại không tạo lại mục người đã từ chối.
// Ref chỉ ghi vị trí nguồn; thứ tự/ý tại vị trí đó có thể đổi nên Ref một mình không là khoá trùng. Cùng Ref và nội
// dung gần giống (sửa lỗi chính tả, đổi vài từ) thì vẫn là "đã nhập", để sửa nhỏ ở nguồn không hồi sinh mục đã từ chối.
func findDuplicate(all []*knowledge.Item, idea importer.Idea) (it *knowledge.Item, imported bool) {
	nt, nb := normKey(idea.Title), normKey(idea.Body)
	for _, o := range all {
		same := (nt != "" && normKey(o.Title) == nt) || (nb != "" && normKey(o.Body) == nb)
		if !same && o.Origin.Kind == knowledge.OriginImported && slices.Contains(o.Origin.Inputs, idea.Ref) &&
			nearText(o.Body, idea.Body) {
			return o, true
		}
		if !same {
			continue
		}
		if o.Origin.Kind == knowledge.OriginImported {
			return o, true
		}
		if same && o.Active() {
			return o, false
		}
	}
	return nil, false
}

// nearText cho biết hai văn bản gần như cùng một ý: hệ số Jaccard của tập từ (chữ thường) từ 0.75 trở lên.
func nearText(a, b string) bool {
	wa, wb := map[string]bool{}, map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(a)) {
		wa[w] = true
	}
	for _, w := range strings.Fields(strings.ToLower(b)) {
		wb[w] = true
	}
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter) >= 0.75*float64(len(wa)+len(wb)-inter)
}

func planHash(p *ImportPlan) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d\n", p.Source, p.Batch, p.Files)
	for _, i := range p.Ideas {
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s|%s\n", i.Ref, i.Outcome, i.Reason, i.Type, i.Title, i.body, strings.Join(i.Evidence, ";"), strings.Join(i.Paths, ";"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ImportKnowledge nhập bộ nhớ ngoài thành mục candidate. DryRun chỉ trả kế hoạch. Có Expect thì kế hoạch dựng lại lúc ghi
// phải trùng (Hash) bản đã xem, nếu không trả ErrImportPlanChanged và không ghi gì. Mỗi mục qua Verifier trước khi ghi.
func ImportKnowledge(ctx context.Context, k *KnowledgeWS, opts ImportOptions) (*ImportResult, error) {
	plan, err := PlanImport(ctx, k, opts)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Plan: *plan}
	if opts.DryRun {
		return res, nil
	}
	if opts.Expect != "" && opts.Expect != plan.Hash {
		res.Mismatch = true
		return res, ErrImportPlanChanged
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	w := k.writer("bint knowledge import")
	for i := range res.Plan.Ideas {
		idea := &res.Plan.Ideas[i]
		if idea.Outcome != ImportCreate {
			continue
		}
		src := importer.Idea{Source: plan.Source, Ref: idea.Ref, Title: idea.Title, Body: idea.body, Paths: idea.Paths}
		it := k.buildItem(ctx, k.importDraft(src, idea.Type, idea.ev, opts.Now))
		if err := w.SaveItem(k.Store, it); err != nil {
			return res, err
		}
		idea.ID = it.ID
		res.IDs = append(res.IDs, it.ID)
		res.Written = true
	}
	return res, nil
}

// ImportSourceInfo là một nguồn nhập phát hiện được ở máy này.
type ImportSourceInfo struct {
	Source string `json:"source"`
	Files  int    `json:"files"`
}

// DetectImportSources liệt kê nguồn nhập có sẵn cho repository ở dir và số tệp của từng nguồn. Chỉ định vị và đếm tệp,
// không đọc nội dung (onboarding gợi ý chứ không tự nhập). Codex memories là bộ nhớ global,
// nên chỉ nhập khi người chỉ định nguồn, không dùng để gợi ý cho từng repository.
// Không cần .bint/ và không cần là người dùng thật.
func DetectImportSources(ctx context.Context, dir string) []ImportSourceInfo {
	repo, err := gitrepo.Discover(ctx, dir)
	root := dir
	lo := importer.LocateOptions{Root: root}
	if err == nil {
		lo.Root, lo.MainRoot = repo.Toplevel, repo.MainWorktree()
	}
	lo.Root = filepath.Clean(lo.Root)
	var out []ImportSourceInfo
	for _, s := range []string{knowledge.ImportClaudeMemory, knowledge.ImportCursorRules} {
		files, err := importer.Locate(s, lo)
		if err != nil {
			continue
		}
		n := 0
		for _, f := range files {
			if !strings.EqualFold(filepath.Base(f.Name), "MEMORY.md") {
				n++
			}
		}
		if n > 0 {
			out = append(out, ImportSourceInfo{Source: s, Files: n})
		}
	}
	return out
}

// importNextStep là dòng "bước tiếp theo" của báo cáo init khi máy này có bộ nhớ harness của repository (Claude, Codex, Cursor);
// rỗng khi không có nguồn nào. Chỉ gợi ý, không đọc nội dung và không nhập gì: nhập là thao tác của người, cần xem trước từng ý.
func importNextStep(ctx context.Context, root string) string {
	srcs := DetectImportSources(ctx, root)
	if len(srcs) == 0 {
		return ""
	}
	parts := make([]string, len(srcs))
	for i, s := range srcs {
		parts[i] = fmt.Sprintf("%s: %d tệp", s.Source, s.Files)
	}
	return fmt.Sprintf("Nhập bộ nhớ harness đã có (%s): bint knowledge import --from %s --dry-run (xem trước từng ý; chạy lại không có cờ này để ghi thành mục chờ duyệt)",
		strings.Join(parts, ", "), srcs[0].Source)
}
