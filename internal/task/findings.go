package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"bint/internal/fsafe"
)

// OutcomeFile là sổ kết cục phát hiện review của một tác vụ (chỉ-thêm). Nằm cạnh review.json nhưng tách
// riêng: review.json không bị sửa, nên dữ liệu cũ vẫn đọc được và vòng review mới (AddRound) không làm mất kết cục.
const OutcomeFile = "findings.json"

// Kết cục của phát hiện. Phát hiện không có bản ghi nào mang kết cục hiệu lực open.
const (
	OutcomeOpen     = "open"
	OutcomeFixed    = "fixed"
	OutcomeRejected = "rejected"
	OutcomeDeferred = "deferred"
)

// FindingOutcomes là các kết cục hợp lệ, theo thứ tự hiển thị.
var FindingOutcomes = []string{OutcomeOpen, OutcomeFixed, OutcomeRejected, OutcomeDeferred}

// OutcomeRecord là một lần đặt kết cục (một dòng lịch sử).
type OutcomeRecord struct {
	Finding string `json:"finding"`
	Outcome string `json:"outcome"`
	// Commit là SHA đầy đủ (bắt buộc với fixed).
	Commit string `json:"commit,omitempty"`
	// Reason bắt buộc với rejected và deferred.
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
	At     string `json:"at"`
}

// Outcomes là sổ kết cục của một tác vụ; bản ghi cuối cùng của mỗi phát hiện là kết cục hiệu lực.
type Outcomes struct {
	TaskID  string          `json:"task_id"`
	Records []OutcomeRecord `json:"records"`
}

// Effective trả về kết cục hiệu lực của phát hiện (mặc định open, không bản ghi).
func (o Outcomes) Effective(findingID string) OutcomeRecord {
	for i := len(o.Records) - 1; i >= 0; i-- {
		if strings.EqualFold(o.Records[i].Finding, findingID) {
			return o.Records[i]
		}
	}
	return OutcomeRecord{Finding: findingID, Outcome: OutcomeOpen}
}

// History trả về mọi bản ghi của một phát hiện theo thứ tự thời gian.
func (o Outcomes) History(findingID string) []OutcomeRecord {
	var out []OutcomeRecord
	for _, r := range o.Records {
		if strings.EqualFold(r.Finding, findingID) {
			out = append(out, r)
		}
	}
	return out
}

// ValidOutcome kiểm giá trị kết cục.
func ValidOutcome(s string) bool { return slices.Contains(FindingOutcomes, s) }

// LoadOutcomes đọc findings.json của tác vụ (rỗng nếu chưa có).
func (t *Task) LoadOutcomes() (Outcomes, error) {
	o := Outcomes{TaskID: t.State.ID}
	data, err := os.ReadFile(filepath.Join(t.Dir, OutcomeFile))
	if errors.Is(err, os.ErrNotExist) {
		return o, nil
	}
	if err != nil {
		return o, err
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return Outcomes{TaskID: t.State.ID}, fmt.Errorf("%s: %w", OutcomeFile, err)
	}
	return o, nil
}

// AppendOutcomes thêm bản ghi vào sổ (không bao giờ sửa/xoá bản ghi cũ). Gọi trong Store.Update.
func (t *Task) AppendOutcomes(recs ...OutcomeRecord) error {
	o, err := t.LoadOutcomes()
	if err != nil {
		return err
	}
	o.TaskID = t.State.ID
	o.Records = append(o.Records, recs...)
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(filepath.Join(t.Dir, OutcomeFile), append(data, '\n'), 0o600)
}

// LoadFinding tìm phát hiện theo ID dạng <task-id>.<n> (task-id có thể là tiền tố).
func (s Store) LoadFinding(id string) (*Task, *Finding, *ReviewRound, error) {
	taskRef, ok := SplitFindingID(id)
	if !ok {
		return nil, nil, nil, fmt.Errorf("ID phát hiện %q không hợp lệ (dạng <task-id>.<n>)", id)
	}
	t, err := s.Load(taskRef)
	if err != nil {
		return nil, nil, nil, err
	}
	rev, err := t.LoadReview()
	if err != nil {
		return nil, nil, nil, err
	}
	_, n, _ := strings.Cut(strings.TrimSpace(id), ".")
	f, round := rev.Find(t.State.ID + "." + n)
	if f == nil {
		return nil, nil, nil, fmt.Errorf("không tìm thấy phát hiện %s", id)
	}
	return t, f, round, nil
}
