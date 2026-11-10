// Package evolve là Evolution Engine ở cấp L1 — tiến hoá tri thức (P6, mục 10–11 của
// kế hoạch): phát hiện cơ hội từ lịch sử lượt chạy có nhãn, tạo đề xuất có cấu trúc
// gắn với phiên bản gốc của đối tượng, đối chiếu base/candidate bằng khung đánh giá
// (đánh giá loại bỏ cho đề xuất bỏ tri thức), áp dụng khi người duyệt và quay lui
// được. Chế độ duy nhất ở P6 là `propose`: Bint không tự áp dụng thay đổi nào.
package evolve

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"sort"
	"strings"
	"time"

	"bint/internal/runlog"
	"bint/internal/store"
)

// Run là một đơn vị công việc dùng cho phân tích: một lượt chạy gốc (không có lượt
// cha) gộp với mọi lượt con của nó (các bước của workflow).
type Run struct {
	ID        string `json:"run_id"`
	Workflow  string `json:"workflow"`
	Harness   string `json:"harness,omitempty"`
	Model     string `json:"model,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	StartedAt string `json:"started_at"`
	// EndedAt: kết thúc (hoặc sự kiện cuối) của lượt gốc và mọi lượt con.
	EndedAt string `json:"ended_at,omitempty"`
	// Commits: commit được gắn với lượt (tín hiệu committed của quét Git).
	Commits     []string `json:"commits,omitempty"`
	Label       string   `json:"label"`
	LabelReason string   `json:"label_reason"`
	Modified    []string `json:"modified,omitempty"`
	Read        []string `json:"read,omitempty"`
	// ReadCount là tổng số lần đọc tệp (kể cả đọc lại cùng tệp).
	ReadCount int      `json:"read_count"`
	Retrieved []string `json:"retrieved,omitempty"`
	Cited     []string `json:"cited,omitempty"`
	CostUSD   *float64 `json:"cost_usd,omitempty"`
	// Module là khu vực mã chính của lượt (xem ModuleOf).
	Module string `json:"module"`
}

// Labeled cho biết lượt có nhãn đủ mạnh để làm bằng chứng (mục 9.6).
func (r *Run) Labeled() bool { return r.Label != runlog.LabelUnlabeled }

// Touched là hợp của tệp được đọc và được sửa.
func (r *Run) Touched() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range append(append([]string{}, r.Modified...), r.Read...) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Observation là nhận xét agent ghi qua report_observation (dữ liệu không đáng tin).
type Observation struct {
	RunID  string   `json:"run_id"`
	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	ItemID string   `json:"item_id,omitempty"`
}

// History là dữ liệu đã lọc mà Evolution Engine dùng.
type History struct {
	Runs         []*Run
	ByID         map[string]*Run
	Observations []Observation
	// Usage theo mục tri thức trên các lượt trong cửa sổ.
	RetrievedRuns map[string][]string // item → run gốc đã truy xuất
	CitedRuns     map[string][]string
	// Revalidation: nhận định gần nhất của agent cho mục stale (mục 8.4).
	Revalidation map[string]string
}

// ModuleOf trả về khu vực mã của một tệp: tối đa hai cấp thư mục đầu ("internal/eval"
// cho internal/eval/report.go, "cmd" cho cmd/main.go, "." cho tệp ở gốc).
func ModuleOf(p string) string {
	dir := path.Dir(p)
	if dir == "." || dir == "/" {
		return "."
	}
	parts := strings.Split(dir, "/")
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, "/")
}

// primaryModule: khu vực có nhiều tệp được sửa nhất; không sửa tệp nào thì theo tệp đọc.
func primaryModule(modified, read []string) string {
	pick := modified
	if len(pick) == 0 {
		pick = read
	}
	counts := map[string]int{}
	for _, p := range pick {
		counts[ModuleOf(p)]++
	}
	best, n := "", 0
	for m, c := range counts {
		if c > n || (c == n && m < best) {
			best, n = m, c
		}
	}
	return best
}

// LoadHistory nạp các lượt chạy đã kết thúc trong cửa sổ lookback, gộp lượt con vào
// lượt gốc, tính nhãn kết quả từ tín hiệu (mục 9.6).
func LoadHistory(ctx context.Context, db *sql.DB, now time.Time, lookback time.Duration) (*History, error) {
	h := &History{ByID: map[string]*Run{}, RetrievedRuns: map[string][]string{}, CitedRuns: map[string][]string{}, Revalidation: map[string]string{}}
	since := store.FormatTime(now.Add(-lookback))
	rows, err := db.QueryContext(ctx, `SELECT run_id, COALESCE(parent_run_id, ''), COALESCE(workflow, ''), COALESCE(harness, ''),
		COALESCE(model, ''), COALESCE(task_id, ''), COALESCE(started_at, ''),
		COALESCE(finished_at, last_event_at, started_at, ''), cost_usd, status
		FROM runs WHERE started_at >= ? ORDER BY started_at, run_id`, since)
	if err != nil {
		return nil, err
	}
	parent := map[string]string{}
	type raw struct {
		r      *Run
		status string
	}
	var all []raw
	for rows.Next() {
		var r Run
		var par, status string
		var cost sql.NullFloat64
		if err := rows.Scan(&r.ID, &par, &r.Workflow, &r.Harness, &r.Model, &r.TaskID, &r.StartedAt, &r.EndedAt, &cost, &status); err != nil {
			rows.Close()
			return nil, err
		}
		if cost.Valid {
			c := cost.Float64
			r.CostUSD = &c
		}
		parent[r.ID] = par
		all = append(all, raw{&r, status})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rootOf := func(id string) string {
		for i := 0; i < 16; i++ {
			p, ok := parent[id]
			if !ok || p == "" {
				return id
			}
			id = p
		}
		return id
	}
	// Lượt gốc phải đã kết thúc; lượt đang chạy chưa có kết quả.
	for _, x := range all {
		if parent[x.r.ID] != "" {
			continue
		}
		if x.status != "completed" && x.status != "failed" && x.status != "interrupted" {
			continue
		}
		h.Runs = append(h.Runs, x.r)
		h.ByID[x.r.ID] = x.r
	}
	signals := map[string][]runlog.Signal{}
	childCost, hasChildCost := map[string]float64{}, map[string]bool{}
	for _, x := range all {
		root := h.ByID[rootOf(x.r.ID)]
		if root == nil {
			continue
		}
		// Lượt gốc của workflow đã ghi tổng chi phí các bước; chỉ cộng lượt con khi gốc không có.
		if x.r.ID != root.ID && x.r.CostUSD != nil {
			childCost[root.ID] += *x.r.CostUSD
			hasChildCost[root.ID] = true
		}
		if root.TaskID == "" {
			root.TaskID = x.r.TaskID
		}
		if root.Model == "" {
			root.Model = x.r.Model
		}
		sigs, err := runlog.RunSignals(ctx, db, x.r.ID)
		if err != nil {
			return nil, err
		}
		signals[root.ID] = append(signals[root.ID], sigs...)
		for _, sg := range sigs {
			if sg.Kind == runlog.SignalCommitted && sg.Detail != "" {
				root.Commits = appendUnique(root.Commits, sg.Detail)
			}
		}
		if x.r.EndedAt > root.EndedAt {
			root.EndedAt = x.r.EndedAt
		}
		arts, err := runlog.RunArtifacts(ctx, db, x.r.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range arts {
			switch a.Kind {
			case "file.modified":
				root.Modified = appendUnique(root.Modified, a.Path)
			case "file.read":
				root.Read = appendUnique(root.Read, a.Path)
				root.ReadCount += a.Count
			}
		}
	}
	for _, r := range h.Runs {
		if r.CostUSD == nil && hasChildCost[r.ID] {
			c := childCost[r.ID]
			r.CostUSD = &c
		}
		sigs := signals[r.ID]
		sort.SliceStable(sigs, func(i, j int) bool { return sigs[i].ObservedAt < sigs[j].ObservedAt })
		r.Label, r.LabelReason = runlog.Label(sigs)
		sort.Strings(r.Modified)
		sort.Strings(r.Read)
		r.Module = primaryModule(r.Modified, r.Read)
	}

	usage := func(q string, into map[string][]string, set func(*Run, string)) error {
		rows, err := db.QueryContext(ctx, q, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var runID, item string
			if err := rows.Scan(&runID, &item); err != nil {
				return err
			}
			root := h.ByID[rootOf(runID)]
			if root == nil {
				continue
			}
			set(root, item)
			if l := into[item]; len(l) == 0 || l[len(l)-1] != root.ID {
				into[item] = appendUnique(into[item], root.ID)
			}
		}
		return rows.Err()
	}
	if err := usage(`SELECT COALESCE(run_id, ''), item_id FROM context_usages WHERE created_at >= ? ORDER BY created_at`,
		h.RetrievedRuns, func(r *Run, id string) { r.Retrieved = appendUnique(r.Retrieved, id) }); err != nil {
		return nil, err
	}
	if err := usage(`SELECT COALESCE(run_id, ''), item_id FROM knowledge_citations WHERE created_at >= ? ORDER BY created_at`,
		h.CitedRuns, func(r *Run, id string) { r.Cited = appendUnique(r.Cited, id) }); err != nil {
		return nil, err
	}

	obs, err := db.QueryContext(ctx, `SELECT COALESCE(run_id, ''), kind, title, COALESCE(body, ''), paths, COALESCE(item_id, '')
		FROM observations WHERE created_at >= ? ORDER BY created_at`, since)
	if err != nil {
		return nil, err
	}
	for obs.Next() {
		var o Observation
		var paths string
		if err := obs.Scan(&o.RunID, &o.Kind, &o.Title, &o.Body, &paths, &o.ItemID); err != nil {
			obs.Close()
			return nil, err
		}
		o.Paths = jsonStrings(paths)
		if root := h.ByID[rootOf(o.RunID)]; root != nil {
			o.RunID = root.ID
		}
		h.Observations = append(h.Observations, o)
	}
	obs.Close()

	rv, err := db.QueryContext(ctx, `SELECT item_id, verdict FROM knowledge_revalidations ORDER BY created_at, revalidation_id`)
	if err != nil {
		return nil, err
	}
	defer rv.Close()
	for rv.Next() {
		var id, v string
		if err := rv.Scan(&id, &v); err != nil {
			return nil, err
		}
		h.Revalidation[id] = v // bản mới nhất ghi đè
	}
	return h, rv.Err()
}

func jsonStrings(s string) []string {
	var out []string
	json.Unmarshal([]byte(s), &out)
	return out
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
