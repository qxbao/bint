package evolve

// Chế độ thực thi Deferred (mục 13.3–13.4): khi đủ bằng chứng mới, Bint đánh dấu một yêu cầu
// tiến hoá (tệp trong <state>/runtime/evolve/requests) và xử lý nó ở lần gọi CLI phù hợp tiếp
// theo — không bao giờ trong hook đồng bộ. Xử lý kiểm tra khoá và ngân sách trước khi chạy, và
// chỉ tạo ĐỀ XUẤT (chế độ propose): không áp dụng gì. Managed (tiến trình nền) chưa có: không
// giả định mọi harness cho phép tác vụ nền bền vững.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/fsafe"
	"bint/internal/store"
	"bint/internal/ulid"
)

// Trạng thái yêu cầu.
const (
	ReqPending = "pending"
	ReqDone    = "done"
	ReqSkipped = "skipped"
)

// Request là một yêu cầu tiến hoá đã đánh dấu, chờ xử lý.
type Request struct {
	Schema         int      `json:"schema"`
	ID             string   `json:"request_id"`
	Opportunity    string   `json:"opportunity"` // khoá cơ hội
	OpportunityID  string   `json:"opportunity_id"`
	Kind           string   `json:"kind"`
	Level          string   `json:"level"`
	Generator      string   `json:"generator"`
	Summary        string   `json:"summary"`
	SupportingRuns []string `json:"supporting_runs"`
	CreatedAt      string   `json:"created_at"`
	Status         string   `json:"status"`
	Note           string   `json:"note,omitempty"`
	ProposalID     string   `json:"proposal_id,omitempty"`
	ProcessedAt    string   `json:"processed_at,omitempty"`
}

// RequestsDir là thư mục yêu cầu.
func RequestsDir(runtimeDir string) string { return filepath.Join(runtimeDir, "evolve", "requests") }

// ListRequests đọc các yêu cầu (cũ nhất trước); statuses rỗng = mọi trạng thái.
func ListRequests(runtimeDir string, statuses ...string) ([]Request, error) {
	ents, err := os.ReadDir(RequestsDir(runtimeDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Request
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(RequestsDir(runtimeDir), e.Name()))
		if err != nil {
			return nil, err
		}
		var r Request
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			continue // tệp lạ/hỏng: bỏ qua, không làm hỏng hàng đợi
		}
		if len(statuses) == 0 || containsStr(statuses, r.Status) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func saveRequest(runtimeDir string, r Request) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(filepath.Join(RequestsDir(runtimeDir), r.ID+".json"), append(b, '\n'), 0o600)
}

// Enqueue đánh dấu yêu cầu cho mọi cơ hội sẵn sàng chưa có yêu cầu đang chờ (các bước 3–4 của
// mục 13.3). Cơ hội chỉ "sẵn sàng" khi đủ lượt có nhãn và đủ bằng chứng mới so với đề xuất trước
// cùng đối tượng, và chưa có đề xuất chờ — nên không đánh dấu lặp.
func Enqueue(runtimeDir string, scan ScanResult, now time.Time) ([]Request, error) {
	pending, err := ListRequests(runtimeDir, ReqPending)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, r := range pending {
		have[r.Opportunity] = true
	}
	var added []Request
	for _, o := range scan.Opportunities {
		if !o.Ready || have[o.Key] {
			continue
		}
		lvl := o.Level
		if lvl == "" {
			lvl = LevelL1
		}
		r := Request{Schema: 1, ID: "er_" + ulid.At(now), Opportunity: o.Key, OpportunityID: o.ID, Kind: o.Kind, Level: lvl,
			Generator: o.Generator, Summary: o.Summary, SupportingRuns: nonNil(o.SupportingRuns), CreatedAt: store.FormatTime(now), Status: ReqPending}
		if err := saveRequest(runtimeDir, r); err != nil {
			return added, err
		}
		added = append(added, r)
		time.Sleep(time.Millisecond) // ULID tăng dần giữ thứ tự xử lý
	}
	return added, nil
}

// ProcessInput điều khiển ProcessRequests.
type ProcessInput struct {
	K       *app.KnowledgeWS
	DB      *sql.DB
	Scan    ScanResult
	History *History
	// Agent soạn nội dung cho yêu cầu cần mô hình; nil = chỉ xử lý yêu cầu xác định.
	Agent eval.Agent
	// Reflector rút bài học cho yêu cầu failure_lesson; nil = các yêu cầu đó chờ.
	Reflector eval.Agent
	// HarnessProposer soạn đề xuất quy trình cho yêu cầu KindHarness; nil = các yêu cầu đó chờ.
	HarnessProposer eval.Agent
	// Cases và RepoDir: như ProposeInput. Thiếu chúng, yêu cầu failure_lesson bị bỏ TRƯỚC khi gọi
	// reflector (kiểm rò rỉ không dựng được), nên không tốn tiền.
	Cases     []*eval.Case
	RepoDir   string
	BudgetUSD float64
	Max       int
	Timeout   time.Duration
	Now       time.Time
}

// ProcessReport là kết quả xử lý hàng đợi.
type ProcessReport struct {
	Processed []Request `json:"processed"`
	Waiting   []Request `json:"waiting"` // còn chờ (cần ngân sách, hoặc vượt giới hạn mỗi lần)
	SpentUSD  float64   `json:"spent_usd"`
	// Locked: một thao tác tiến hoá khác đang chạy; không xử lý gì (bước 5 của mục 13.3).
	Locked string `json:"locked,omitempty"`
}

// ProcessRequests thực thi các yêu cầu đang chờ: kiểm tra khoá và ngân sách trước, rồi tạo đề
// xuất qua cùng đường với `bint evolve propose`. Yêu cầu mà cơ hội không còn sẵn sàng (đã có đề
// xuất, hoặc bằng chứng đổi) bị đánh dấu skipped, không chạy.
func ProcessRequests(ctx context.Context, in ProcessInput) (ProcessReport, error) {
	var rep ProcessReport
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	if in.Max <= 0 {
		in.Max = in.K.Config.Evolution.Deferred.MaxPerRun
	}
	runtime := in.K.Paths.Runtime
	pending, err := ListRequests(runtime, ReqPending)
	if err != nil || len(pending) == 0 {
		return rep, err
	}
	lock, err := AcquireLock(runtime, "bint evolve (deferred)")
	if err != nil {
		if errors.Is(err, ErrLocked) {
			rep.Locked, rep.Waiting = err.Error(), pending
			return rep, nil
		}
		return rep, err
	}
	defer lock.Release()

	ready := map[string]Opportunity{}
	for _, o := range in.Scan.Opportunities {
		if o.Ready {
			ready[o.Key] = o
		}
	}
	finish := func(r Request, status, note, proposal string) error {
		r.Status, r.Note, r.ProposalID, r.ProcessedAt = status, note, proposal, store.FormatTime(in.Now)
		rep.Processed = append(rep.Processed, r)
		return saveRequest(runtime, r)
	}
	for _, r := range pending {
		if len(rep.Processed) >= in.Max {
			rep.Waiting = append(rep.Waiting, r)
			continue
		}
		o, ok := ready[r.Opportunity]
		if !ok {
			if err := finish(r, ReqSkipped, "cơ hội không còn sẵn sàng (đã có đề xuất, hoặc điều kiện đã đổi)", ""); err != nil {
				return rep, err
			}
			continue
		}
		needsLesson := o.Kind == KindFailureLesson
		needsHarness := o.Kind == KindHarness
		needsAgent := o.Generator == GenAgent && !needsLesson && !needsHarness
		remaining := in.BudgetUSD - rep.SpentUSD
		switch {
		case needsHarness && in.HarnessProposer == nil:
			r.Note = "cần agent đề xuất quy trình (roles.evolver = claude): chạy `bint evolve process` với harness đã cấu hình"
			rep.Waiting = append(rep.Waiting, r)
			continue
		case needsHarness && in.HarnessProposer.ReportsCost() && remaining <= 0:
			r.Note = "hết ngân sách deferred cho lần này"
			rep.Waiting = append(rep.Waiting, r)
			continue
		case needsLesson && in.Reflector == nil:
			r.Note = "cần agent reflector: đặt roles.reflector hoặc chạy `bint evolve process` với harness đã cấu hình"
			rep.Waiting = append(rep.Waiting, r)
			continue
		case needsLesson && in.Reflector.ReportsCost() && remaining <= 0:
			r.Note = "hết ngân sách deferred cho lần này"
			rep.Waiting = append(rep.Waiting, r)
			continue
		case needsAgent && in.Agent == nil:
			r.Note = "cần agent evolver: đặt evolution.deferred.budget_usd hoặc chạy `bint evolve process --budget <USD>`"
			rep.Waiting = append(rep.Waiting, r)
			continue
		case needsAgent && in.Agent.ReportsCost() && remaining <= 0:
			r.Note = "hết ngân sách deferred cho lần này"
			rep.Waiting = append(rep.Waiting, r)
			continue
		}
		pr, err := Propose(ctx, ProposeInput{K: in.K, DB: in.DB, Scan: in.Scan, History: in.History, Refs: []string{o.Key}, Max: 1,
			Agent:           map[bool]eval.Agent{true: in.Agent, false: nil}[needsAgent],
			Reflector:       map[bool]eval.Agent{true: in.Reflector, false: nil}[needsLesson],
			HarnessProposer: map[bool]eval.Agent{true: in.HarnessProposer, false: nil}[needsHarness],
			Cases:           in.Cases, RepoDir: in.RepoDir,
			BudgetUSD: remaining, Timeout: in.Timeout, Now: in.Now})
		rep.SpentUSD += pr.SpentUSD
		if err != nil {
			return rep, err
		}
		// Một cơ hội bài học có thể cho nhiều đề xuất (chung + riêng): mọi đề xuất đã lưu đều là
		// kết quả của yêu cầu, đề xuất đầu là ProposalID, các đề xuất còn lại ghi vào ghi chú.
		var made []string
		for _, oc := range pr.Outcomes {
			if oc.Proposal != nil {
				made = append(made, oc.Proposal.ID)
			}
		}
		switch {
		case len(made) > 0:
			note := ""
			if len(made) > 1 {
				note = "đề xuất thêm: " + strings.Join(made[1:], ", ")
			}
			if err := finish(r, ReqDone, note, made[0]); err != nil {
				return rep, err
			}
		default:
			why := "không tạo được đề xuất"
			if len(pr.Outcomes) == 1 {
				why = pr.Outcomes[0].Skipped
				if len(pr.Outcomes[0].Rejected) > 0 {
					why += " (" + strings.Join(pr.Outcomes[0].Rejected, "; ") + ")"
				}
			}
			if err := finish(r, ReqSkipped, why, ""); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

// Line là một dòng mô tả yêu cầu cho người đọc.
func (r Request) Line() string {
	return fmt.Sprintf("%s [%s %s] %s", r.ID, r.Level, r.Kind, r.Summary)
}
