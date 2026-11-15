package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"bint/internal/fsafe"
	"bint/internal/ulid"
)

// SyncItem là kết quả đồng bộ một phiếu. Action: exported | imported | merged | conflict | pending | invalid | unchanged.
type SyncItem struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// SyncReport là kết quả `bint task sync`.
type SyncReport struct {
	Items []SyncItem `json:"items"`
	// Skipped là mục trong cây bị bỏ qua (tên không phải ULID, tệp lạ, dữ liệu sai).
	Skipped []string `json:"skipped,omitempty"`
}

// Count đếm số mục theo hành động.
func (r SyncReport) Count(action string) int {
	n := 0
	for _, it := range r.Items {
		if it.Action == action {
			n++
		}
	}
	return n
}

// Sync đồng bộ ba bên mọi phiếu giữa thư mục cục bộ và `.bint/tasks` của cây hiện tại (Store.Root): chỉ-ở-cây thì
// dựng thư mục cục bộ, chỉ-ở-cục-bộ thì ghi ra cây, một phía đổi (theo băm so với lần đồng bộ trước) thì lấy phía đó, chỉ
// findings.json khác thì hợp bản ghi, đổi cả hai phía thì giữ bản cục bộ và báo xung đột. human=false (phiên agent):
// không áp gì từ cây vào thư mục cục bộ, chỉ xuất và liệt kê thay đổi chờ người nhập.
func (s Store) Sync(human bool) (SyncReport, error) {
	var rep SyncReport
	if s.Root == "" {
		return rep, fmt.Errorf("không biết cây làm việc để đồng bộ %s", LedgerDir)
	}
	ledger := filepath.Join(s.Root, filepath.FromSlash(LedgerDir))
	ids := map[string]bool{}
	if entries, err := os.ReadDir(s.Dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && ulid.Valid(e.Name()) {
				ids[e.Name()] = true
			}
		}
	}
	if entries, err := os.ReadDir(ledger); err == nil {
		for _, e := range entries {
			switch {
			case !e.IsDir():
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s/%s: không phải thư mục phiếu", LedgerDir, e.Name()))
			case !ulid.Valid(e.Name()):
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s/%s: tên thư mục không phải ULID", LedgerDir, e.Name()))
			default:
				ids[e.Name()] = true
			}
		}
	}
	order := make([]string, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	sort.Strings(order)
	for _, id := range order {
		it, skipped := s.syncOne(id, human)
		rep.Skipped = append(rep.Skipped, skipped...)
		if it.Action != "" {
			rep.Items = append(rep.Items, it)
		}
	}
	return rep, nil
}

func (s Store) syncOne(id string, human bool) (SyncItem, []string) {
	dir := filepath.Join(s.Dir, id)
	treeDir := filepath.Join(s.Root, filepath.FromSlash(LedgerRel(id)))
	var skipped []string
	if entries, err := os.ReadDir(treeDir); err == nil {
		for _, e := range entries {
			if !contains(ledgerFiles, e.Name()) {
				skipped = append(skipped, fmt.Sprintf("%s/%s: tệp ngoài danh sách cho phép", LedgerRel(id), e.Name()))
			}
		}
	}
	T, tExists, terr := readTree(treeDir, id)
	if terr != nil {
		return SyncItem{ID: id, Action: "invalid", Detail: terr.Error()}, skipped
	}
	if _, err := os.Stat(filepath.Join(dir, StateFile)); err == nil {
		unlock, err := lockDir(dir)
		if err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		defer unlock()
	} else if tExists && human {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		unlock, err := lockDir(dir)
		if err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		defer unlock()
	}
	L, lExists, err := localBundle(dir)
	if err != nil {
		return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
	}
	meta := loadMeta(dir)
	base := meta.Base[treeKey(s.Root)]
	in := decideIn{lExists: lExists, tExists: tExists, hB: base.Hash, human: human, committed: true}
	if lExists {
		in.hL = L.hash()
	}
	if tExists {
		in.hT = T.hash()
		in.tNewer = true
		if lExists {
			in.tNewer = newer(T.updated(), base, L)
			in.findingsOnly = onlyFindingsDiffer(L, T)
		}
	}
	setBase := func(b bundle) {
		nb := ledgerBase{Hash: b.hash(), UpdatedAt: updatedOf(b)}
		if base != nb {
			if lExists || dirExists(dir) {
				meta.setBase(s.Root, nb)
				_ = saveMeta(dir, meta)
			}
		}
	}
	switch act := decide(in); act {
	case LedgerNoop:
		if lExists && tExists {
			setBase(L)
		}
		return SyncItem{ID: id, Action: "unchanged"}, skipped
	case LedgerExport:
		n, err := writeTree(treeDir, L)
		if err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		setBase(L)
		return SyncItem{ID: id, Action: "exported", Detail: fmt.Sprintf("%d tệp", n)}, skipped
	case LedgerImport:
		sc := sensitiveChanges(L, lExists, T)
		if err := s.importBundle(dir, s.Root, T); err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		detail := ""
		if len(sc) > 0 {
			detail = "đã nhập thay đổi quyền hạn: " + strings.Join(sc, ", ") + " (kiểm tra bằng `git diff` và `bint task show`)"
		}
		return SyncItem{ID: id, Action: "imported", Detail: detail}, skipped
	case LedgerMerge:
		merged, err := mergeOutcomes(L[OutcomeFile], T[OutcomeFile])
		if err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		L[OutcomeFile], T[OutcomeFile] = merged, merged
		if err := fsafe.WriteAtomic(filepath.Join(dir, OutcomeFile), merged, 0o600); err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		if _, err := writeTree(treeDir, T); err != nil {
			return SyncItem{ID: id, Action: "invalid", Detail: err.Error()}, skipped
		}
		setBase(L)
		return SyncItem{ID: id, Action: "merged", Detail: OutcomeFile}, skipped
	case LedgerConflict:
		return SyncItem{ID: id, Action: "conflict", Detail: "cả cục bộ và " + LedgerRel(id) + " đều đổi kể từ lần đồng bộ trước; giữ bản cục bộ, không ghi đè. Người giải: áp phần cần giữ của bản trong cây bằng `bint task set`/`bint task write`, rồi xoá " + LedgerRel(id) + " và chạy lại để ghi bản cục bộ ra cây"}, skipped
	default: // pending_human (phiên agent)
		return SyncItem{ID: id, Action: "pending", Detail: "thay đổi trong " + LedgerDir + " chờ người nhập (`bint task sync` ngoài phiên agent)"}, skipped
	}
}

func dirExists(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// mergeOutcomes hợp hai findings.json: giữ mọi bản ghi (chỉ-thêm), bỏ bản trùng, xếp theo thời điểm.
func mergeOutcomes(a, b []byte) ([]byte, error) {
	var oa, ob Outcomes
	if len(a) > 0 {
		if err := json.Unmarshal(a, &oa); err != nil {
			return nil, err
		}
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &ob); err != nil {
			return nil, err
		}
	}
	if oa.TaskID == "" {
		oa.TaskID = ob.TaskID
	}
	seen := map[string]bool{}
	key := func(r OutcomeRecord) string {
		return strings.Join([]string{r.Finding, r.At, r.Outcome, r.Commit, r.Reason, r.By}, "\x00")
	}
	var out []OutcomeRecord
	for _, r := range append(append([]OutcomeRecord{}, oa.Records...), ob.Records...) {
		if k := key(r); !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	oa.Records = out
	data, err := json.MarshalIndent(oa, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
