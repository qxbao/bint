package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/kindex"
	"bint/internal/knowledge"
)

// Dịch vụ tri thức dùng chung cho CLI và TUI (docs/tui-plan.md mục 7.3): mỗi lệnh có kiểu Options
// (thẻ `flag` = tên cờ CLI, `arg` = đối số vị trí, `form` = ô nhập của TUI), Validate() và Defaults().

// ---- knowledge list ----

// KnowledgeListOptions: `bint knowledge list`.
type KnowledgeListOptions struct {
	Status []string `flag:"status" form:"choices=candidate|verified|stale|deprecated"`
	Type   []string `flag:"type" form:"choices=rule|warning|pattern|fact|decision|lesson"`
	All    bool     `flag:"all"`
}

// KnowledgeListDefaults: mặc định không lọc.
func KnowledgeListDefaults(config.Config) KnowledgeListOptions { return KnowledgeListOptions{} }

// Validate: lọc theo giá trị lạ chỉ cho danh sách rỗng (như CLI cũ), không coi là lỗi.
func (o KnowledgeListOptions) Validate() error { return nil }

// Filter đổi tuỳ chọn thành bộ lọc dùng chung.
func (o KnowledgeListOptions) Filter() knowledge.Filter {
	return knowledge.Filter{Statuses: o.Status, Types: o.Type, All: o.All}
}

// KnowledgeList là danh sách mục khớp bộ lọc kèm tệp hỏng không đọc được.
type KnowledgeList struct {
	Items []*knowledge.Item
	Bad   []knowledge.LoadError
}

// ListKnowledge đọc và lọc các mục.
func ListKnowledge(_ context.Context, k *KnowledgeWS, o KnowledgeListOptions) (KnowledgeList, error) {
	items, bad, err := k.Items()
	if err != nil {
		return KnowledgeList{}, err
	}
	return KnowledgeList{Items: o.Filter().Apply(items), Bad: bad}, nil
}

// ---- knowledge show ----

// KnowledgeShowOptions: `bint knowledge show <id>`.
type KnowledgeShowOptions struct {
	Ref string `arg:"id" form:"required,suggest=ids"`
}

func KnowledgeShowDefaults(config.Config) KnowledgeShowOptions { return KnowledgeShowOptions{} }

func (o KnowledgeShowOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return Usagef("cần ID mục tri thức")
	}
	return nil
}

// EvidenceView là một bằng chứng kèm trích đoạn dòng (Missing: tệp không còn).
type EvidenceView struct {
	Evidence knowledge.Evidence
	Missing  bool
	Lines    []string
	// More là số dòng của vùng bằng chứng không nằm trong Lines (bị cắt bớt); CLI và TUI tự định dạng.
	More int
}

// KnowledgeDetail là một mục kèm kết quả kiểm tra và trích đoạn bằng chứng.
type KnowledgeDetail struct {
	Item     *knowledge.Item
	Check    knowledge.Result
	Evidence []EvidenceView
}

// ExcerptLines là số dòng tối đa của một trích đoạn bằng chứng.
const ExcerptLines = 12

// EvidenceExcerpt trích các dòng của vùng bằng chứng e từ nội dung tệp b (tối đa max dòng) và trả thêm
// số dòng còn lại không được trích.
func EvidenceExcerpt(e knowledge.Evidence, b []byte, max int) (out []string, more int) {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	from, to, ok, err := e.LineRange()
	if !ok || err != nil {
		from, to = 1, len(lines)
	}
	if to > len(lines) {
		to = len(lines)
	}
	for i := from; i <= to && i-from < max; i++ {
		out = append(out, fmt.Sprintf("%4d  %s", i, lines[i-1]))
	}
	if to-from+1 > max {
		more = to - from + 1 - max
	}
	return out, more
}

// DetailOf dựng chi tiết của một mục đã có trong danh sách all (kiểm tra và đọc bằng chứng).
func (k *KnowledgeWS) DetailOf(it *knowledge.Item, all []*knowledge.Item) KnowledgeDetail {
	d := KnowledgeDetail{Item: it, Check: knowledge.Verifier{Src: k.Source(), All: all, FindMoved: true}.Verify(it)}
	src := k.Source()
	for _, e := range it.Evidence {
		ev := EvidenceView{Evidence: e}
		if b, ok := src.Read(e.Path); ok {
			ev.Lines, ev.More = EvidenceExcerpt(e, b, ExcerptLines)
		} else {
			ev.Missing = true
		}
		d.Evidence = append(d.Evidence, ev)
	}
	return d
}

// ShowKnowledge tìm mục theo ID (hoặc tiền tố) và dựng chi tiết.
func ShowKnowledge(_ context.Context, k *KnowledgeWS, o KnowledgeShowOptions) (KnowledgeDetail, error) {
	items, _, err := k.Items()
	if err != nil {
		return KnowledgeDetail{}, err
	}
	it, err := knowledge.Find(items, o.Ref)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	return k.DetailOf(it, items), nil
}

// DefaultRejectReason là lý do từ chối mặc định khi người dùng bỏ trống (ghi vào tệp mục, nên không
// theo ngôn ngữ giao diện); dùng chung cho CLI và TUI.
const DefaultRejectReason = "người duyệt từ chối"

// ---- knowledge rm / verify / approve / reject / deprecate ----

// RemoveOptions: `bint knowledge rm <id>...`.
type RemoveOptions struct {
	Refs []string `arg:"id" form:"required,suggest=ids"`
}

func RemoveDefaults(config.Config) RemoveOptions { return RemoveOptions{} }

func (o RemoveOptions) Validate() error {
	if len(o.Refs) == 0 {
		return Usagef("cần ít nhất một ID")
	}
	return nil
}

// VerifyOptions: `bint knowledge verify [id...]`.
type VerifyOptions struct {
	Refs  []string `arg:"id" form:"suggest=ids"`
	Apply bool     `flag:"apply"`
}

func VerifyDefaults(config.Config) VerifyOptions { return VerifyOptions{} }
func (o VerifyOptions) Validate() error          { return nil }

// ApproveOptions: `bint knowledge approve <id>...`.
type ApproveOptions struct {
	Refs    []string `arg:"id" form:"required,suggest=ids"`
	Refresh bool     `flag:"refresh"`
	Note    string   `flag:"note"`
}

func ApproveDefaults(config.Config) ApproveOptions { return ApproveOptions{} }

func (o ApproveOptions) Validate() error {
	if len(o.Refs) == 0 {
		return Usagef("cần ít nhất một ID")
	}
	return nil
}

// RejectOptions: `bint knowledge reject|deprecate <id>... --reason`. Lý do bắt buộc; kiểm tra trước
// khi mở kho, như CLI cũ.
type RejectOptions struct {
	Refs   []string `arg:"id" form:"required,suggest=ids"`
	Reason string   `flag:"reason" form:"required"`
}

func RejectDefaults(config.Config) RejectOptions { return RejectOptions{} }

func (o RejectOptions) Validate() error {
	if strings.TrimSpace(o.Reason) == "" {
		return Usagef("cần --reason")
	}
	if len(o.Refs) == 0 {
		return Usagef("cần ít nhất một ID")
	}
	return nil
}

// ApproveKnowledge duyệt (→ verified) các mục. Chỉ dành cho người.
func ApproveKnowledge(ctx context.Context, k *KnowledgeWS, o ApproveOptions) ([]ReviewOutcome, error) {
	return ReviewKnowledge(ctx, k, ReviewAction{Refs: o.Refs, To: knowledge.StatusVerified, Reason: o.Note, Refresh: o.Refresh})
}

// RejectKnowledge từ chối hoặc loại bỏ (→ deprecated) các mục. Chỉ dành cho người.
func RejectKnowledge(ctx context.Context, k *KnowledgeWS, o RejectOptions) ([]ReviewOutcome, error) {
	return ReviewKnowledge(ctx, k, ReviewAction{Refs: o.Refs, To: knowledge.StatusDeprecated, Reason: o.Reason})
}

// ---- duyệt theo lô ----

// Lý do một mục không vào lô (BatchBlocked.Reason); TUI dịch theo mã.
const (
	BlockedChecks      = "checks"       // có finding mức error (kể cả fingerprint)
	BlockedAlreadyDone = "already_done" // đã ở trạng thái đích
	BlockedMissing     = "missing"      // không tìm thấy mục
)

// BatchBlocked là một mục bị loại khỏi lô.
type BatchBlocked struct {
	ID       string
	Reason   string
	Findings []knowledge.Finding // các finding mức error (khi Reason = BlockedChecks)
}

// BatchPlan là kế hoạch duyệt một lô: mục nào ghi được (Ready), mục nào bị loại và vì sao.
type BatchPlan struct {
	To      string
	Ready   []string
	Blocked []BatchBlocked
}

// PlanBatch lập kế hoạch đưa các mục refs tới trạng thái to (verified hoặc deprecated) mà không ghi
// gì. Với to = verified, mục có bất kỳ finding mức error (kể cả fingerprint của mục stale) không
// bao giờ vào Ready: phải xem bằng chứng rồi duyệt từng mục (`approve --refresh`). Từ chối/loại bỏ
// không bị chặn bởi kết quả kiểm tra.
func PlanBatch(_ context.Context, k *KnowledgeWS, refs []string, to string) (BatchPlan, error) {
	plan := BatchPlan{To: to}
	if to != knowledge.StatusVerified && to != knowledge.StatusDeprecated {
		return plan, Usagef("trạng thái đích phải là verified hoặc deprecated")
	}
	all, _, err := k.Items()
	if err != nil {
		return plan, err
	}
	v := knowledge.Verifier{Src: k.Source(), All: all, FindMoved: true}
	seen := map[string]bool{}
	for _, ref := range refs {
		it, err := knowledge.Find(all, ref)
		if err != nil {
			plan.Blocked = append(plan.Blocked, BatchBlocked{ID: ref, Reason: BlockedMissing})
			continue
		}
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if it.Status == to {
			plan.Blocked = append(plan.Blocked, BatchBlocked{ID: it.ID, Reason: BlockedAlreadyDone})
			continue
		}
		if to == knowledge.StatusVerified {
			if r := v.Verify(it); !r.OK() {
				b := BatchBlocked{ID: it.ID, Reason: BlockedChecks}
				for _, f := range r.Findings {
					if f.Level == knowledge.LevelError {
						b.Findings = append(b.Findings, f)
					}
				}
				plan.Blocked = append(plan.Blocked, b)
				continue
			}
		}
		plan.Ready = append(plan.Ready, it.ID)
	}
	return plan, nil
}

// BatchOptions điều khiển duyệt/từ chối theo lô.
type BatchOptions struct {
	Refs []string `arg:"id" form:"required,suggest=ids"`
	// To: verified (duyệt) hoặc deprecated (từ chối/loại bỏ).
	To     string `form:"hidden"`
	Reason string `flag:"reason"`
}

// BatchResult là kết quả duyệt lô: kế hoạch đã dùng và kết quả ghi từng mục.
type BatchResult struct {
	Plan     BatchPlan
	Outcomes []ReviewOutcome
}

// ReviewBatch duyệt hoặc từ chối nhiều mục một lần: RequireHuman → PlanBatch → ReviewKnowledge(Ready).
// Mục không vào Ready được trả trong Plan.Blocked, không bị ghi.
func ReviewBatch(ctx context.Context, k *KnowledgeWS, o BatchOptions) (BatchResult, error) {
	var res BatchResult
	op := "bint knowledge approve"
	if o.To == knowledge.StatusDeprecated {
		op = "bint knowledge reject/deprecate"
	}
	if err := RequireHuman(op); err != nil {
		return res, err
	}
	if o.To == knowledge.StatusDeprecated && strings.TrimSpace(o.Reason) == "" {
		return res, Usagef("cần --reason")
	}
	plan, err := PlanBatch(ctx, k, o.Refs, o.To)
	if err != nil {
		return res, err
	}
	res.Plan = plan
	if len(plan.Ready) == 0 {
		return res, nil
	}
	res.Outcomes, err = ReviewKnowledge(ctx, k, ReviewAction{Refs: plan.Ready, To: o.To, Reason: o.Reason})
	return res, err
}

// ---- knowledge search / stats / diff ----

// SearchOptions: `bint knowledge search "<truy vấn>"`.
type SearchOptions struct {
	Query string `arg:"query" form:"required"`
	Limit int    `flag:"limit"`
	All   bool   `flag:"all"`
}

// SearchDefaults: tối đa 20 kết quả.
func SearchDefaults(config.Config) SearchOptions { return SearchOptions{Limit: 20} }

func (o SearchOptions) Validate() error {
	if strings.TrimSpace(o.Query) == "" {
		return Usagef("cần truy vấn tìm kiếm")
	}
	return nil
}

// SearchKnowledge tìm tri thức bằng FTS5.
func SearchKnowledge(ctx context.Context, dir string, o SearchOptions) ([]kindex.Hit, error) {
	c, err := OpenContext(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return kindex.Search(ctx, c.Store.DB, o.Query, kindex.SearchOptions{Limit: o.Limit, IncludeDeprecated: o.All})
}

// StatsOptions: `bint knowledge stats`. NoIngest không phải cờ CLI: TUI ở chế độ chỉ đọc đặt nó để
// không nạp spool vào cơ sở dữ liệu.
type StatsOptions struct {
	NoIngest bool `form:"hidden"`
}

func StatsDefaults(config.Config) StatsOptions { return StatsOptions{} }
func (o StatsOptions) Validate() error         { return nil }

// StatsKnowledge đọc thống kê truy xuất/trích dẫn theo mục (nạp spool trước, trừ NoIngest).
func StatsKnowledge(ctx context.Context, dir string, o StatsOptions) ([]kindex.Stats, error) {
	c, err := OpenContext(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if c.Memory {
		return nil, ErrNotInitialized
	}
	if !o.NoIngest {
		if _, err := c.K.Ingest(ctx, c.Store, time.Now()); err != nil {
			return nil, err
		}
	}
	return KnowledgeStats(ctx, c)
}

// DiffOptions: `bint knowledge diff [<từ>..<tới>]`.
type DiffOptions struct {
	Range string `arg:"range"`
	All   bool   `flag:"all"`
}

func DiffDefaults(config.Config) DiffOptions { return DiffOptions{} }
func (o DiffOptions) Validate() error        { return nil }

// DiffReport là các mục thay đổi trong khoảng commit (đã lọc bỏ mục của chính người dùng, trừ All).
type DiffReport struct {
	Range   string
	Entries []DiffEntry
}

// DiffKnowledge liệt kê tri thức thay đổi trong khoảng commit.
func DiffKnowledge(ctx context.Context, k *KnowledgeWS, o DiffOptions) (DiffReport, error) {
	entries, rng, err := KnowledgeDiff(ctx, k, o.Range)
	rep := DiffReport{Range: rng}
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if o.All || !e.Mine {
			rep.Entries = append(rep.Entries, e)
		}
	}
	return rep, nil
}

// TrackedFiles trả các tệp được Git theo dõi (đường dẫn tương đối, dấu '/', đã sắp xếp), tối đa max; dùng
// làm gợi ý cho ô nhập bằng chứng và phạm vi của form.
func TrackedFiles(k *KnowledgeWS, max int) []string {
	files := k.Source().Files()
	if max > 0 && len(files) > max {
		files = files[:max]
	}
	return slices.Clone(files)
}

// EditorArgv trả dòng lệnh mở tệp path trong trình soạn thảo của người dùng: $VISUAL, rồi $EDITOR, rồi
// notepad (Windows) hoặc vi. Phần tử cuối luôn là path.
func EditorArgv(path string) []string {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		if runtime.GOOS == "windows" {
			ed = "notepad"
		} else {
			ed = "vi"
		}
	}
	return append(strings.Fields(ed), path)
}

// EditorArgvAt như EditorArgv nhưng mở tệp tại dòng line (> 0) khi trình soạn thảo có cú pháp đó: `+N` cho vi, vim,
// nvim, nano, emacs, micro; `tệp:N` cho hx/helix. Trình soạn thảo khác chỉ mở tệp.
func EditorArgvAt(path string, line int) []string {
	argv := EditorArgv(path)
	if line <= 0 {
		return argv
	}
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), ".exe"))
	switch name {
	case "vi", "vim", "nvim", "view", "gvim", "nano", "emacs", "micro":
		last := len(argv) - 1
		return append(append(append([]string{}, argv[:last]...), fmt.Sprintf("+%d", line)), argv[last])
	case "hx", "helix":
		argv[len(argv)-1] = fmt.Sprintf("%s:%d", path, line)
	}
	return argv
}
