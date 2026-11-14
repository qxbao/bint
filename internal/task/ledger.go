package task

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"bint/internal/fsafe"
	"bint/internal/gitrepo"
	"bint/internal/ulid"
)

// Sổ phiếu trong Git (B1 của docs/next-directions.md): bản chiếu bền của mỗi phiếu nằm trong cây làm việc ở
// `.bint/tasks/<ID>/`, commit cùng mã như `.beads/issues.jsonl` của Beads. Thư mục cục bộ
// `<state>/runtime/tasks/<ID>/` vẫn là bộ đệm làm việc và giữ dữ liệu riêng của máy. Bint không tự commit.

// LedgerDir là thư mục sổ phiếu trong cây làm việc (dấu gạch chéo xuôi).
const LedgerDir = ".bint/tasks"

// LedgerRel là đường dẫn tương đối của sổ một phiếu trong cây làm việc.
func LedgerRel(id string) string { return LedgerDir + "/" + id }

// IsLedgerPath cho biết đường dẫn tương đối (so với gốc cây) nằm trong sổ phiếu: mọi kiểm tra cây làm việc bỏ qua
// nó, vì Bint tự ghi ở đó (kể cả trong bước chỉ-đọc).
func IsLedgerPath(p string) bool {
	p = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"), "./")
	return p == LedgerDir || strings.HasPrefix(p, LedgerDir+"/")
}

// ledgerFiles là các tệp được xuất/nhập (danh sách cho phép).
var ledgerFiles = []string{TicketFile, StateFile, "requirement.md", "plan.md", "tasks.md", ReviewFile, OutcomeFile}

const (
	ledgerMetaFile = ".ledger.json"
	maxLedgerFile  = 1 << 20
)

// Hành động sổ cho một phiếu.
const (
	LedgerNoop      = "noop"
	LedgerExport    = "export"
	LedgerImport    = "import"
	LedgerMerge     = "merge_findings"
	LedgerConflict  = "conflict"
	LedgerPending   = "pending_human"
	LedgerUncommitt = "uncommitted"
)

type decideIn struct {
	lExists, tExists bool
	hL, hT, hB       string
	// tNewer: bản trong cây mới hơn lần đồng bộ trước.
	tNewer bool
	// findingsOnly: hai bên chỉ khác findings.json.
	findingsOnly bool
	// committed: bản trong cây đã commit (sync luôn true: đọc cây làm việc).
	committed bool
	// human: được phép nhập từ cây vào thư mục cục bộ.
	human bool
}

// decide là phép so ba bên thuần (cục bộ L, cây T, băm lần đồng bộ trước B) dùng chung cho xuất, tự nhập và sync.
func decide(in decideIn) string {
	switch {
	case !in.lExists && !in.tExists:
		return LedgerNoop
	case !in.tExists:
		return LedgerExport
	case in.lExists && in.hL == in.hT:
		return LedgerNoop
	case in.lExists && in.hT == in.hB:
		return LedgerExport
	case in.lExists && in.hL != in.hB && in.findingsOnly:
		// Đổi cả hai phía mà chỉ findings.json khác: hợp bản ghi, không mất bản nào. Hợp là ghi bản ghi của cây vào
		// thư mục cục bộ nên chỉ người làm được; phiên agent để chờ người nhập.
		if !in.human {
			return LedgerPending
		}
		return LedgerMerge
	case in.lExists && in.hB == "" && !in.tNewer:
		// Chưa có mốc đồng bộ với cây này (vd. worktree dựng từ nhánh mang bản phiếu cũ): không biết phía nào đổi,
		// bản trong cây không mới hơn bản cục bộ thì coi là cũ. Có mốc thì KHÔNG dựa vào updated_at: sửa tay
		// task.yml/plan.md trong cây (commit rồi git pull, hoặc chưa commit) không đổi updated_at, nên "cây không
		// mới hơn" không có nghĩa là cây không đổi; đổi cả hai phía (hT != hB, hL != hB) là xung đột ở dưới.
		return LedgerExport
	}
	if in.lExists && in.hL != in.hB { // đổi cả hai phía (hB rỗng: chưa từng đồng bộ cũng tính là đổi)
		return LedgerConflict
	}
	if !in.human {
		return LedgerPending
	}
	if !in.committed {
		return LedgerUncommitt
	}
	return LedgerImport
}

type ledgerBase struct {
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updated_at"`
}

// ledgerMeta là tệp cục bộ `.ledger.json` trong thư mục tác vụ: cây gốc của phiếu và băm lần đồng bộ theo từng cây.
type ledgerMeta struct {
	Home string                `json:"home,omitempty"`
	Base map[string]ledgerBase `json:"base,omitempty"`
}

func loadMeta(dir string) ledgerMeta {
	var m ledgerMeta
	if data, err := os.ReadFile(filepath.Join(dir, ledgerMetaFile)); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveMeta(dir string, m ledgerMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(filepath.Join(dir, ledgerMetaFile), append(data, '\n'), 0o600)
}

func treeKey(root string) string { return strings.ToLower(filepath.ToSlash(filepath.Clean(root))) }

func (m *ledgerMeta) setBase(root string, b ledgerBase) {
	if m.Base == nil {
		m.Base = map[string]ledgerBase{}
	}
	m.Base[treeKey(root)] = b
}

// bundle là nội dung các tệp sổ của một phiếu (tên → nội dung).
type bundle map[string][]byte

func normNL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// stateDurable giải mã state.json của bundle thành bản bền (lỏng: khoá lạ bị bỏ).
func stateDurable(b []byte) (Durable, error) {
	var d Durable
	err := json.Unmarshal(b, &d)
	return d, err
}

func (b bundle) hash() string {
	names := make([]string, 0, len(b))
	for n := range b {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		c := normNL(b[n])
		if n == StateFile {
			if d, err := stateDurable(c); err == nil {
				d.UpdatedAt = ""
				if j, err := json.Marshal(d); err == nil {
					c = j
				}
			}
		}
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write(c)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b bundle) updated() time.Time {
	d, err := stateDurable(b[StateFile])
	if err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, d.UpdatedAt)
	return t
}

// onlyFindingsDiffer: hai bundle chỉ khác findings.json.
func onlyFindingsDiffer(a, b bundle) bool {
	ha, hb := bundle{}, bundle{}
	for n, v := range a {
		if n != OutcomeFile {
			ha[n] = v
		}
	}
	for n, v := range b {
		if n != OutcomeFile {
			hb[n] = v
		}
	}
	return ha.hash() == hb.hash()
}

// localBundle đọc thư mục cục bộ; state.json được chiếu sang bản bền. ok=false khi thư mục chưa có phiếu.
func localBundle(dir string) (bundle, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, StateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, false, fmt.Errorf("%s: %w", filepath.Join(dir, StateFile), err)
	}
	b := bundle{}
	sj, err := json.MarshalIndent(ToDurable(st), "", "  ")
	if err != nil {
		return nil, false, err
	}
	b[StateFile] = append(sj, '\n')
	for _, n := range ledgerFiles {
		if n == StateFile {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		b[n] = data
	}
	return b, true, nil
}

// LedgerProblem là một mục bị bỏ qua khi đọc cây.
type LedgerProblem struct{ ID, File, Reason string }

func (p LedgerProblem) String() string {
	s := p.ID
	if p.File != "" {
		s += "/" + p.File
	}
	return s + ": " + p.Reason
}

var treeCache sync.Map // thư mục → treeSnap

type treeSnap struct {
	sig string
	b   bundle
	err error
}

// readTree đọc và kiểm bản trong cây `<tree>/.bint/tasks/<id>`. exists=false khi chưa có thư mục. Kết quả có chữ ký
// (kích thước + thời điểm sửa các tệp) để lần gọi sau không đọc lại khi cây không đổi.
func readTree(treeDir, id string) (b bundle, exists bool, err error) {
	if !ulid.Valid(id) {
		return nil, true, errors.New("tên thư mục không phải ULID")
	}
	fi, serr := os.Lstat(treeDir)
	if errors.Is(serr, os.ErrNotExist) {
		return nil, false, nil
	}
	if serr != nil {
		return nil, true, serr
	}
	if !fi.IsDir() {
		return nil, true, errors.New("không phải thư mục")
	}
	var sig strings.Builder
	for _, n := range ledgerFiles {
		if st, e := os.Lstat(filepath.Join(treeDir, n)); e == nil {
			fmt.Fprintf(&sig, "%s:%d:%d:%v;", n, st.Size(), st.ModTime().UnixNano(), st.Mode())
		}
	}
	if v, ok := treeCache.Load(treeDir); ok {
		if s := v.(treeSnap); s.sig == sig.String() {
			// Trả bản sao: người gọi (nhánh hợp findings của sync) có thể gán khoá; bản trong bộ đệm phải giữ đúng
			// nội dung trên đĩa và an toàn khi List/Resolve chạy song song.
			return maps.Clone(s.b), true, s.err
		}
	}
	b, err = readTreeUncached(treeDir, id)
	treeCache.Store(treeDir, treeSnap{sig: sig.String(), b: b, err: err})
	return maps.Clone(b), true, err
}

func readTreeUncached(treeDir, id string) (bundle, error) {
	b := bundle{}
	for _, n := range ledgerFiles {
		p := filepath.Join(treeDir, n)
		st, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: không phải tệp thường (symlink bị từ chối)", n)
		}
		if st.Size() > maxLedgerFile {
			return nil, fmt.Errorf("%s: quá %d byte", n, maxLedgerFile)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if hasConflictMarker(data) {
			return nil, fmt.Errorf("%s: còn dấu xung đột merge", n)
		}
		b[n] = data
	}
	if err := validateBundle(b, id); err != nil {
		return nil, err
	}
	return b, nil
}

func hasConflictMarker(data []byte) bool {
	for _, l := range bytes.Split(normNL(data), []byte("\n")) {
		if bytes.HasPrefix(l, []byte("<<<<<<< ")) || bytes.HasPrefix(l, []byte(">>>>>>> ")) {
			return true
		}
	}
	return false
}

// validateBundle kiểm dữ liệu nhập: task.yml qua Ticket.Validate, id khớp thư mục, JSON hợp lệ, tham chiếu là ULID.
func validateBundle(b bundle, id string) error {
	raw, ok := b[TicketFile]
	if !ok {
		return errors.New(TicketFile + ": thiếu")
	}
	t, err := decodeTicket(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", TicketFile, err)
	}
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%s: %w", TicketFile, err)
	}
	if t.ID != id {
		return fmt.Errorf("%s: id %q không khớp thư mục %q", TicketFile, t.ID, id)
	}
	sraw, ok := b[StateFile]
	if !ok {
		return errors.New(StateFile + ": thiếu")
	}
	d, err := stateDurable(sraw)
	if err != nil {
		return fmt.Errorf("%s: %w", StateFile, err)
	}
	if d.ID != id {
		return fmt.Errorf("%s: id %q không khớp thư mục %q", StateFile, d.ID, id)
	}
	if !contains(Statuses, d.Status) {
		return fmt.Errorf("%s: trạng thái %q không hợp lệ", StateFile, d.Status)
	}
	if d.Parent != "" && !ulid.Valid(d.Parent) {
		return fmt.Errorf("%s: parent %q không phải ULID", StateFile, d.Parent)
	}
	for _, dep := range d.DependsOn {
		if !ulid.Valid(dep) {
			return fmt.Errorf("%s: depends_on %q không phải ULID", StateFile, dep)
		}
	}
	for _, f := range []string{ReviewFile, OutcomeFile} {
		if data, ok := b[f]; ok && !json.Valid(data) {
			return fmt.Errorf("%s: JSON không hợp lệ", f)
		}
	}
	if data, ok := b[ReviewFile]; ok {
		var r Review
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("%s: %w", ReviewFile, err)
		}
	}
	if data, ok := b[OutcomeFile]; ok {
		var o Outcomes
		if err := json.Unmarshal(data, &o); err != nil {
			return fmt.Errorf("%s: %w", OutcomeFile, err)
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// writeTree ghi các tệp khác nội dung (so bỏ CR) của b vào treeDir; trả số tệp đã ghi.
func writeTree(treeDir string, b bundle) (int, error) {
	n := 0
	for _, name := range ledgerFiles {
		data, ok := b[name]
		if !ok {
			continue
		}
		data = normNL(data)
		p := filepath.Join(treeDir, name)
		if cur, err := os.ReadFile(p); err == nil && bytes.Equal(normNL(cur), data) {
			continue
		}
		if err := fsafe.WriteAtomic(p, data, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s Store) warn(msg string) {
	if s.Warn != nil {
		s.Warn(msg)
	}
}

// export ghi bản chiếu bền của phiếu ra cây gốc của phiếu. Gọi trong khoá tác vụ sau khi ghi cục bộ; lỗi chỉ cảnh báo.
func (s Store) export(t *Task) {
	if !s.Ledger {
		return
	}
	if err := s.exportErr(t); err != nil {
		s.warn(fmt.Sprintf("không ghi được %s của phiếu %s: %v (chạy `bint task sync` để ghi bù)", LedgerDir, t.State.ID, err))
	}
}

func (s Store) exportErr(t *Task) error {
	meta := loadMeta(t.Dir)
	home := t.State.Origin
	if home == "" {
		home = meta.Home
	}
	if home == "" {
		home = s.Root
	}
	if home == "" {
		return nil
	}
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return fmt.Errorf("cây gốc %s không còn", home)
	}
	if meta.Home == "" && t.State.Origin == "" {
		meta.Home = home
	}
	L, ok, err := localBundle(t.Dir)
	if err != nil || !ok {
		return err
	}
	treeDir := filepath.Join(home, filepath.FromSlash(LedgerRel(t.State.ID)))
	T, tExists, terr := readTree(treeDir, t.State.ID)
	base := meta.Base[treeKey(home)]
	in := decideIn{lExists: true, tExists: tExists && terr == nil, hL: L.hash(), hB: base.Hash}
	if in.tExists {
		in.hT = T.hash()
		in.tNewer = newer(T.updated(), base, L)
		in.findingsOnly = onlyFindingsDiffer(L, T)
		in.human, in.committed = true, true
	}
	if terr != nil && tExists {
		// bản trong cây hỏng (dấu xung đột, JSON sai...): người phải xử lý, không đè mù quáng.
		return fmt.Errorf("bản trong cây không đọc được (%v); sửa hoặc xoá rồi chạy `bint task sync`", terr)
	}
	switch act := decide(in); act {
	case LedgerNoop, LedgerExport:
		if act == LedgerExport {
			if _, err := writeTree(treeDir, L); err != nil {
				return err
			}
		}
		meta.setBase(home, ledgerBase{Hash: in.hL, UpdatedAt: updatedOf(L)})
	case LedgerConflict:
		return fmt.Errorf("%s/%s đã đổi kể từ lần ghi trước (git pull/merge hoặc sửa tay) và bản cục bộ cũng đổi: không ghi đè, giữ bản cục bộ; chạy `bint task sync`", LedgerDir, t.State.ID)
	default:
		return fmt.Errorf("bản trong cây đã đổi kể từ lần ghi trước (sau git pull/merge?) nên không bị ghi đè; chạy `bint task sync`")
	}
	return saveMeta(t.Dir, meta)
}

func updatedOf(b bundle) string {
	d, _ := stateDurable(b[StateFile])
	return d.UpdatedAt
}

// newer: T mới hơn mốc đồng bộ trước (hoặc, chưa có mốc, mới hơn bản cục bộ).
func newer(tUpdated time.Time, base ledgerBase, L bundle) bool {
	ref := L.updated()
	if base.UpdatedAt != "" {
		if bt, err := time.Parse(time.RFC3339, base.UpdatedAt); err == nil {
			ref = bt
		}
	}
	return tUpdated.After(ref)
}

// committedAt cho biết `.bint/tasks/<id>` trong cây root khớp HEAD (đã theo dõi, không sửa chưa commit).
func committedAt(root, id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel := LedgerRel(id)
	out, err := gitrepo.Run(ctx, root, "status", "--porcelain", "--", rel)
	if err != nil || strings.TrimSpace(out) != "" {
		return false
	}
	out, err = gitrepo.Run(ctx, root, "ls-files", "--", rel)
	return err == nil && strings.TrimSpace(out) != ""
}

// sensitiveChanges liệt kê thay đổi quyền hạn mà bản T trong cây đem lại so với bản cục bộ L (autonomy, phạm vi,
// lệnh kiểm chứng, ràng buộc, chuyển sang done). Các trường này người đặt qua cổng riêng (task set, duyệt) nên
// agent sửa tệp trong cây rồi commit không được lặng lẽ nâng quyền: tự nhập bị chặn, `bint task sync` nhập
// nhưng nêu rõ từng thay đổi.
func sensitiveChanges(L bundle, lExists bool, T bundle) []string {
	var out []string
	nt, err := decodeTicket(T[TicketFile])
	if err != nil {
		return nil
	}
	var ot Ticket
	if lExists {
		ot, _ = decodeTicket(L[TicketFile])
	}
	if !lExists {
		if nt.Autonomy == "auto" {
			out = append(out, "autonomy=auto")
		}
	} else {
		if nt.Autonomy != ot.Autonomy {
			out = append(out, fmt.Sprintf("autonomy %s→%s", ot.Autonomy, nt.Autonomy))
		}
		if !slices.Equal(nt.Scope.Allow, ot.Scope.Allow) || !slices.Equal(nt.Scope.Deny, ot.Scope.Deny) {
			out = append(out, "scope")
		}
		if nt.Verify.Command != ot.Verify.Command {
			out = append(out, "verify.command")
		}
		if !slices.Equal(nt.Constraints, ot.Constraints) {
			out = append(out, "constraints")
		}
	}
	nd, _ := stateDurable(T[StateFile])
	var od Durable
	if lExists {
		od, _ = stateDurable(L[StateFile])
	}
	if nd.Status == StatusDone && od.Status != StatusDone {
		out = append(out, "status→done")
	} else if lExists && Terminal(od.Status) && nd.Status != od.Status {
		// Lùi khỏi trạng thái kết thúc (vd. git checkout sang nhánh mang bản phiếu cũ hơn): không tự nhập.
		out = append(out, fmt.Sprintf("status %s→%s", od.Status, nd.Status))
	}
	return out
}

// importBundle áp bundle T đã kiểm vào thư mục cục bộ dir. Gọi trong khoá của dir.
func (s Store) importBundle(dir, root string, T bundle) error {
	d, err := stateDurable(T[StateFile])
	if err != nil {
		return err
	}
	var st State
	if raw, err := os.ReadFile(filepath.Join(dir, StateFile)); err == nil {
		if err := json.Unmarshal(raw, &st); err != nil {
			return fmt.Errorf("%s: %w", StateFile, err)
		}
	}
	d.ApplyTo(&st)
	if st.Schema == 0 {
		st.Schema = SchemaVersion
	}
	// task.yml đã qua validateBundle; giữ nguyên byte (kể cả chú thích, thứ tự khoá người viết tay) để bản cục bộ
	// khớp băm bản trong cây, lần ghi sau không xuất bản chuẩn hoá đè lên tệp của người.
	if _, err := decodeTicket(T[TicketFile]); err != nil {
		return err
	}
	if err := fsafe.WriteAtomic(filepath.Join(dir, TicketFile), normNL(T[TicketFile]), 0o600); err != nil {
		return err
	}
	for _, file := range Artifacts {
		if data, ok := T[file]; ok {
			if err := fsafe.WriteAtomic(filepath.Join(dir, file), normNL(data), 0o600); err != nil {
				return err
			}
		}
	}
	for _, f := range []string{ReviewFile, OutcomeFile} {
		if data, ok := T[f]; ok {
			if err := fsafe.WriteAtomic(filepath.Join(dir, f), normNL(data), 0o600); err != nil {
				return err
			}
		}
	}
	if data, ok := T[ReviewFile]; ok {
		var rev Review
		if json.Unmarshal(data, &rev) == nil {
			if rev.TaskID == "" {
				rev.TaskID = st.ID
			}
			if err := fsafe.WriteAtomic(filepath.Join(dir, ReviewMD), []byte(RenderReview(&rev)), 0o600); err != nil {
				return err
			}
		}
	}
	sj, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := fsafe.WriteAtomic(filepath.Join(dir, StateFile), append(sj, '\n'), 0o600); err != nil {
		return err
	}
	meta := loadMeta(dir)
	meta.setBase(root, ledgerBase{Hash: T.hash(), UpdatedAt: updatedOf(T)})
	return saveMeta(dir, meta)
}

var warned sync.Map // thông báo đã in trong tiến trình (tránh lặp ở List/TUI)

func (s Store) warnOnce(key, msg string) {
	if _, dup := warned.LoadOrStore(key, true); !dup {
		s.warn(msg)
	}
}

// pullOne tự nhập phiếu id từ cây hiện tại (Store.Root) khi bản đã commit mới hơn và phía cục bộ không đổi.
func (s Store) pullOne(id string) {
	if !s.Ledger || s.Root == "" || !ulid.Valid(id) {
		return
	}
	treeDir := filepath.Join(s.Root, filepath.FromSlash(LedgerRel(id)))
	T, tExists, terr := readTree(treeDir, id)
	if !tExists {
		return
	}
	dir := filepath.Join(s.Dir, id)
	if terr != nil {
		s.warnOnce(id+"|bad|"+terr.Error(), fmt.Sprintf("bỏ qua %s: %v", LedgerRel(id), terr))
		return
	}
	meta := loadMeta(dir)
	base := meta.Base[treeKey(s.Root)]
	hT := T.hash()
	L, lExists, err := localBundle(dir)
	if err != nil {
		return
	}
	if lExists && hT == base.Hash {
		return // đường nhanh: cây không đổi kể từ lần đồng bộ trước, không gọi git
	}
	in := decideIn{lExists: lExists, tExists: true, hT: hT, hB: base.Hash, human: s.Import, committed: true}
	if lExists {
		in.hL = L.hash()
		in.findingsOnly = onlyFindingsDiffer(L, T)
		in.tNewer = newer(T.updated(), base, L)
	} else {
		in.tNewer = true
	}
	act := decide(in)
	if act == LedgerImport && !committedAt(s.Root, id) {
		act = LedgerUncommitt
	}
	if act == LedgerImport {
		if sc := sensitiveChanges(L, lExists, T); len(sc) > 0 {
			s.warnOnce(id+"|sensitive|"+hT, fmt.Sprintf("phiếu %s: %s đổi trường quyền hạn (%s) — không tự nhập; xem diff rồi chạy `bint task sync`", id, LedgerDir, strings.Join(sc, ", ")))
			return
		}
	}
	switch act {
	case LedgerImport:
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return
		}
		unlock, err := lockDir(dir)
		if err != nil {
			return
		}
		defer unlock()
		// đọc lại dưới khoá: phía cục bộ có thể vừa đổi.
		L2, ok2, err := localBundle(dir)
		if err != nil || ok2 != lExists || (ok2 && L2.hash() != in.hL) {
			return
		}
		if err := s.importBundle(dir, s.Root, T); err != nil {
			s.warn(fmt.Sprintf("không nhập được phiếu %s từ %s: %v", id, LedgerDir, err))
			return
		}
		s.warn(fmt.Sprintf("đã nhập phiếu %s từ %s (bản mới sau git pull/merge)", id, LedgerDir))
	case LedgerConflict:
		s.warnOnce(id+"|conflict|"+hT, fmt.Sprintf("phiếu %s: %s và bản cục bộ đều đổi — giữ bản cục bộ; chạy `bint task sync`", id, LedgerDir))
	case LedgerPending:
		s.warnOnce(id+"|pending|"+hT, fmt.Sprintf("phiếu %s: %s có thay đổi chờ người nhập (`bint task sync`)", id, LedgerDir))
	case LedgerUncommitt:
		s.warnOnce(id+"|dirty|"+hT, fmt.Sprintf("phiếu %s: %s sửa chưa commit — chỉ nhập bằng `bint task sync`", id, LedgerDir))
	}
}

// pullMatching tự nhập các phiếu trong cây có ID bắt đầu bằng prefix ("" = tất cả).
func (s Store) pullMatching(prefix string) {
	if !s.Ledger || s.Root == "" {
		return
	}
	entries, err := os.ReadDir(filepath.Join(s.Root, filepath.FromSlash(LedgerDir)))
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) && ulid.Valid(e.Name()) {
			s.pullOne(e.Name())
		}
	}
}
