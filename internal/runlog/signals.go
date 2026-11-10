package runlog

import (
	"context"
	"database/sql"
	"fmt"

	"bint/internal/ulid"
)

// Loại tín hiệu kết quả (mục 9.6).
const (
	SignalTests     = "tests"     // kiểm thử qua/không qua trong phiên
	SignalRating    = "rating"    // người dùng chấm (bint runs rate)
	SignalCommitted = "committed" // thay đổi của lượt được commit (yếu, tích cực)
	SignalReverted  = "reverted"  // commit của lượt bị hoàn tác (mạnh, tiêu cực)
	// P4.
	SignalVerify   = "verify"   // lệnh kiểm chứng của phiếu tác vụ (mạnh nếu người đã xác nhận)
	SignalCriteria = "criteria" // agent đối chiếu tiêu chí chấp nhận (trung bình)
	SignalReview   = "review"   // b-review có phát hiện nghiêm trọng (trung bình, tiêu cực)
)

// Signal là một tín hiệu kết quả. Không gộp thành một điểm: mỗi tín hiệu giữ
// nguồn và độ mạnh riêng.
type Signal struct {
	RunID      string `json:"-"`
	Kind       string `json:"kind"`
	Positive   bool   `json:"positive"`
	Strength   string `json:"strength"` // strong | medium | weak
	Source     string `json:"source"`
	Detail     string `json:"detail,omitempty"`
	DedupKey   string `json:"-"`
	ObservedAt string `json:"observed_at"`
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UpsertSignal ghi tín hiệu; cùng dedup_key thì cập nhật (ví dụ chấm lại).
func UpsertSignal(ctx context.Context, db execer, s Signal) error {
	pol := "negative"
	if s.Positive {
		pol = "positive"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO outcome_signals
		(signal_id, run_id, kind, polarity, strength, source, detail, dedup_key, observed_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)
		ON CONFLICT(dedup_key) DO UPDATE SET polarity = excluded.polarity, strength = excluded.strength,
			detail = excluded.detail, observed_at = excluded.observed_at`,
		ulid.New(), s.RunID, s.Kind, pol, s.Strength, s.Source, s.Detail, s.DedupKey, s.ObservedAt)
	return err
}

// RunSignals trả về tín hiệu của một lượt chạy theo thời gian.
func RunSignals(ctx context.Context, db *sql.DB, runID string) ([]Signal, error) {
	rows, err := db.QueryContext(ctx, `SELECT kind, polarity, strength, source, COALESCE(detail, ''), dedup_key, observed_at
		FROM outcome_signals WHERE run_id = ? ORDER BY observed_at, signal_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Signal
	for rows.Next() {
		s := Signal{RunID: runID}
		var pol string
		if err := rows.Scan(&s.Kind, &pol, &s.Strength, &s.Source, &s.Detail, &s.DedupKey, &s.ObservedAt); err != nil {
			return nil, err
		}
		s.Positive = pol == "positive"
		out = append(out, s)
	}
	return out, rows.Err()
}

// Nhãn kết quả.
const (
	LabelSuccess   = "success"
	LabelFailure   = "failure"
	LabelUnlabeled = "unlabeled"
)

// Label suy ra nhãn kết quả từ các tín hiệu, kèm lý do. Quy tắc (mục 9.6):
//   - bị hoàn tác, hoặc người chấm "bad" → failure (tín hiệu mạnh tiêu cực thắng);
//   - người chấm "good" → success;
//   - kiểm thử hoặc lệnh kiểm chứng của phiếu tác vụ: chỉ tính lần chạy CUỐI, và chỉ
//     khi là lệnh đã được người xác nhận (strong); qua → success, không qua → failure;
//   - còn lại (chỉ có tín hiệu yếu/trung bình như "được commit") → unlabeled,
//     không được dùng làm bằng chứng cho đề xuất tiến hoá.
func Label(sigs []Signal) (string, string) {
	var lastStrongTest *Signal
	for i := range sigs {
		s := sigs[i]
		if s.Kind == SignalReverted && !s.Positive {
			return LabelFailure, "thay đổi bị hoàn tác: " + s.Detail
		}
		if (s.Kind == SignalTests || s.Kind == SignalVerify) && s.Strength == "strong" {
			lastStrongTest = &sigs[i]
		}
	}
	for _, s := range sigs {
		if s.Kind == SignalRating {
			if s.Positive {
				return LabelSuccess, "người dùng chấm good"
			}
			return LabelFailure, "người dùng chấm bad"
		}
	}
	if lastStrongTest != nil {
		if lastStrongTest.Positive {
			return LabelSuccess, fmt.Sprintf("lần kiểm thử cuối qua (%s)", lastStrongTest.Detail)
		}
		return LabelFailure, fmt.Sprintf("lần kiểm thử cuối không qua (%s)", lastStrongTest.Detail)
	}
	return LabelUnlabeled, "không có tín hiệu đủ mạnh"
}
