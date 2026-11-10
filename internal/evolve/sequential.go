package evolve

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"

	"bint/internal/eval"
)

// Probe tuần tự: ít tác vụ, nhiều lần lặp, dừng sớm.
//
// Sau mỗi điểm nhìn (một ranh giới lần lặp, gộp để có tối đa MaxLooks điểm) đợt được
// đánh giá trên các cặp tác vụ đã cân bằng (mọi tác vụ đã có cùng số lần lặp ở cả hai
// nhánh; xem eval.PlanByRepeat). Quy tắc dừng:
//
//   - Biên "chi tiêu alpha" kiểu O'Brien–Fleming: z_j = c_K·√(K/j), j = 1..K. c_K là
//     hằng số cuối đã hiệu chỉnh để sai số loại I tổng ≈ 5 % hai phía (bảng Jennison &
//     Turnbull: K=2 → 1,977; K=3 → 2,004; K=4 → 2,024; K=1 → 1,96). Công thức thô
//     z_final = 1,96 cho mọi K sẽ hơi lạc quan (sai số loại I thực khoảng 5,5–6 % ở K=4).
//   - Superiority: dừng, kết luận supported nếu cận dưới tại z_j > 0.
//   - Futility: dừng, kết luận not_supported nếu cận trên của CI95 (z = 1,96) < min_effect.
//   - Non-inferiority: dừng, supported nếu cận dưới tại z_j > −margin; futility (not_supported)
//     nếu cận trên của CI95 < −margin.
//   - Điểm nhìn cuối (j = K) dùng biên c_K và luôn kết luận: supported nếu thoả điều kiện
//     tương ứng, ngược lại not_supported.
//
// Khoảng tin cậy là CI chuẩn trên trung bình chênh lệch ghép cặp theo tác vụ với SD mẫu
// (mean ± z·SD/√n), KHÔNG dùng bootstrap: bootstrap với 20–45 tác vụ ở z lớn (3,9) quá
// thô để phân biệt các biên, trong khi xấp xỉ chuẩn đã là giả định của việc tính cỡ mẫu.
// Cần ít nhất MinSeqCases tác vụ chấm được ở cả hai nhánh mới được dừng sớm.

// MaxLooks là số điểm nhìn tối đa.
const MaxLooks = 4

// MinSeqCases: số tác vụ chấm được tối thiểu để một điểm nhìn được phép dừng sớm.
var MinSeqCases = 5

// SeqNotePrefix đánh dấu (trong ghi chú của đợt) rằng đợt chạy theo kế hoạch tuần tự.
const SeqNotePrefix = "probe tuần tự: "

// zCI95 dùng cho cận futility và hiển thị CI95.
const zCI95 = 1.96

// Sequential cho biết kế hoạch đo trên tỷ lệ đạt nên được phân tích tuần tự. Thước đo
// chi phí/số lượt không có quy tắc dừng riêng (chạy đủ R).
func Sequential(p Plan) bool { return p.Kind == "non_inferiority" || p.Metric == "pass_rate" }

// SeqLooks trả về các lần lặp là điểm nhìn: K = min(R, MaxLooks) điểm, điểm thứ j ở lần
// lặp ⌈j·R/K⌉ (điểm cuối luôn là R).
func SeqLooks(repeats int) []int {
	if repeats < 1 {
		return nil
	}
	k := min(repeats, MaxLooks)
	out := make([]int, k)
	for j := 1; j <= k; j++ {
		out[j-1] = (j*repeats + k - 1) / k
	}
	return out
}

// seqFinalZ là hằng số cuối c_K đã hiệu chỉnh của biên O'Brien–Fleming.
func seqFinalZ(k int) float64 {
	switch k {
	case 1:
		return 1.96
	case 2:
		return 1.977
	case 3:
		return 2.004
	default:
		return 2.024
	}
}

// SeqBoundary là z_j = c_K·√(K/j) tại điểm nhìn thứ j (1..K) trong K điểm.
func SeqBoundary(k, j int) float64 { return seqFinalZ(k) * math.Sqrt(float64(k)/float64(j)) }

// SeqStep là quyết định tại một điểm nhìn.
type SeqStep struct {
	Stop    bool
	Verdict string
	Reason  string
	Mean    float64
	Lo, Hi  float64 // CI chuẩn tại biên z_j (Lo dùng cho thắng; Hi95 riêng cho futility)
	Z       float64
	Hi95    float64
	N       int
}

// SeqDecide áp quy tắc dừng tại điểm nhìn thứ j trong K điểm, trên chênh lệch ghép cặp
// candidate − base. Điểm nhìn cuối luôn trả Stop = true kèm kết luận cuối. Không đủ dữ
// liệu (ít tác vụ, SD không có) thì không dừng ở điểm giữa, và ở điểm cuối trả
// Verdict rỗng (không kết luận được).
func SeqDecide(p eval.Paired, plan Plan, j, k int) SeqStep {
	st := SeqStep{Mean: p.PassDiff.Mean, N: p.PassDiff.N, Z: SeqBoundary(k, j)}
	final := j >= k
	if p.SDDiff == nil || p.PassDiff.N < 2 || (!final && p.PassDiff.N < MinSeqCases) {
		st.Stop = final
		return st
	}
	se := *p.SDDiff / math.Sqrt(float64(p.PassDiff.N))
	st.Lo = st.Mean - st.Z*se
	st.Hi = st.Mean + st.Z*se
	st.Hi95 = st.Mean + zCI95*se
	threshold, label := 0.0, "tỷ lệ đạt tăng"
	futile, futileWhy := st.Hi95 < plan.MinEffect, fmt.Sprintf("cận trên CI95 %+.3f < mức hiệu ứng tối thiểu %.2f", st.Hi95, plan.MinEffect)
	if plan.Kind == "non_inferiority" {
		threshold, label = -plan.Margin, fmt.Sprintf("tỷ lệ đạt không kém quá %.2f", plan.Margin)
		futile, futileWhy = st.Hi95 < -plan.Margin, fmt.Sprintf("cận trên CI95 %+.3f < −%.2f: chắc chắn kém quá biên", st.Hi95, plan.Margin)
	}
	switch {
	case st.Lo > threshold:
		st.Stop, st.Verdict = true, VerdictSupported
		st.Reason = fmt.Sprintf("%s: cận dưới tại z=%.2f là %+.3f > %+.2f", label, st.Z, st.Lo, threshold)
	case final:
		st.Stop, st.Verdict = true, VerdictNotSupported
		st.Reason = fmt.Sprintf("không có bằng chứng %s: cận dưới tại z=%.2f là %+.3f ≤ %+.2f", label, st.Z, st.Lo, threshold)
	case futile:
		st.Stop, st.Verdict = true, VerdictNotSupported
		st.Reason = "futility: " + futileWhy
	}
	return st
}

func pairedOf(rep eval.Report) *eval.Paired {
	for i := range rep.Paired {
		if rep.Paired[i].Arm == eval.ArmCandidate && rep.Paired[i].Baseline == eval.ArmBase {
			return &rep.Paired[i]
		}
	}
	return nil
}

// NewLook dựng callback eval.RunOptions.Look cho một đợt đối chiếu: tại mỗi điểm nhìn
// (không phải điểm cuối) tính báo cáo chỉ gồm các lần lặp đã xong rồi áp SeqDecide. Chọn
// điểm nhìn theo b.Repeats của đợt (đọc từ cơ sở dữ liệu nên dùng được cả khi tiếp tục).
// Điểm nhìn cuối trả stop = false: đợt chạy xong bình thường và ComputeVerdict kết luận.
func NewLook(ctx context.Context, db *sql.DB, plan Plan, batchID func() string, current map[string]*eval.Case) func(int) (bool, string) {
	return func(repeatDone int) (bool, string) {
		if !Sequential(plan) {
			return false, ""
		}
		b, err := eval.GetBatch(ctx, db, batchID())
		if err != nil {
			return false, ""
		}
		looks := SeqLooks(b.Repeats)
		j := slices.Index(looks, repeatDone)
		if j < 0 || j == len(looks)-1 {
			return false, ""
		}
		rep, err := eval.BuildReportUpTo(ctx, db, b, eval.ArmBase, current, repeatDone)
		if err != nil {
			return false, ""
		}
		p := pairedOf(rep)
		if p == nil {
			return false, ""
		}
		st := SeqDecide(*p, plan, j+1, len(looks))
		if !st.Stop {
			return false, ""
		}
		return true, fmt.Sprintf("điểm nhìn %d/%d (lần lặp %d/%d), %s: %s", j+1, len(looks), repeatDone, b.Repeats, st.Verdict, st.Reason)
	}
}

// SeqInfo mô tả điểm nhìn mà kết luận dựa vào.
type SeqInfo struct {
	K, J    int // điểm nhìn J trong K điểm
	Repeat  int // lần lặp tương ứng
	Repeats int // R của đợt
}

// IsSequential cho biết đợt được chạy theo kế hoạch tuần tự (xem SeqNotePrefix).
func IsSequential(b eval.Batch) bool {
	for _, n := range b.Notes {
		if strings.HasPrefix(n, SeqNotePrefix) {
			return true
		}
	}
	return false
}

// sequentialReport tìm điểm nhìn mà đợt đã dừng (hoặc điểm cuối nếu chạy đủ) và dựng báo
// cáo tại đó. Dùng đúng SeqDecide như Look nên cho cùng quyết định kể cả khi tiến trình bị
// ngắt trước khi ghi nhận. Trả info = nil nếu chưa có điểm nhìn nào hoàn chỉnh hoặc đợt dở
// dang mà chưa đủ bằng chứng; khi đó dùng báo cáo toàn đợt và kết luận cố định (bị
// chặn bởi cảnh báo incomplete nếu đợt chưa xong).
func sequentialReport(ctx context.Context, db *sql.DB, b eval.Batch, plan Plan, current map[string]*eval.Case) (eval.Report, *SeqInfo, error) {
	rows, err := eval.Attempts(ctx, db, "batch_id = ?", b.BatchID)
	if err != nil {
		return eval.Report{}, nil, err
	}
	perRepeat := map[int]int{}
	for _, r := range rows {
		if r.Status != "running" {
			perRepeat[r.Repeat]++
		}
	}
	full := len(b.CaseIDs) * len(b.Arms)
	complete := func(k int) bool {
		for r := 1; r <= k; r++ {
			if perRepeat[r] < full {
				return false
			}
		}
		return true
	}
	looks := SeqLooks(b.Repeats)
	for j, k := range looks {
		if !complete(k) {
			break
		}
		bk := b
		bk.Status = "completed" // điểm nhìn hoàn chỉnh: phần sau không chạy là theo kế hoạch
		if j < len(looks)-1 {
			bk.Notes = append(slices.Clone(bk.Notes), eval.StoppedEarlyNote+fmt.Sprintf("tại điểm nhìn %d/%d", j+1, len(looks)))
		}
		rep, err := eval.BuildReportUpTo(ctx, db, bk, eval.ArmBase, current, k)
		if err != nil {
			return rep, nil, err
		}
		p := pairedOf(rep)
		if p == nil {
			continue
		}
		if st := SeqDecide(*p, plan, j+1, len(looks)); st.Stop {
			return rep, &SeqInfo{K: len(looks), J: j + 1, Repeat: k, Repeats: b.Repeats}, nil
		}
	}
	rep, err := eval.BuildReport(ctx, db, b, eval.ArmBase, current)
	return rep, nil, err
}
