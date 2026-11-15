package workflow

// Lớp lý do dừng của một bước (docs/step-continuation-plan.md). Giai đoạn 1 chỉ ghi nhận.
const (
	StopExternal      = "external"       // hạn mức/5xx, harness chết, huỷ, hết hạn thuê: nối tiếp phiên cũ được
	StopAwaitingHuman = "awaiting_human" // chờ người (câu hỏi mở, đường dẫn bảo vệ)
	StopIntrinsic     = "intrinsic"      // lỗi của chính bước: chạy mới như cũ
	StopTimeout       = "timeout"        // quá workflow.timeout_minutes
)

// StopFacts là các dữ kiện endStep gom được về cách một bước kết thúc.
type StopFacts struct {
	// Failed: agent báo lỗi hoặc thoát mã khác 0 (kể cả agent tự báo thất bại).
	Failed bool
	// AgentFailed: subagent báo thất bại (`bint run complete --failed`); CostCap: bước chạm trần chi phí.
	AgentFailed, CostCap bool
	// Interrupted: người huỷ (ctx). LeaseExpired: driver session hết hạn thuê. TimedOut: driver process quá giờ.
	Interrupted, LeaseExpired, TimedOut bool
	// Infra/QuotaHit: harness báo lỗi hạ tầng (eval.AgentResult.Infra, QuotaHit).
	Infra, QuotaHit bool
	// NoResult: harness thoát mà không có kết quả đọc được.
	NoResult bool
	// ReadonlyViolation: bước chỉ đọc còn sửa cây; IsolationLeak: lời gọi ngoài worktree.
	ReadonlyViolation, IsolationLeak bool
	// ProtectedDenied: hook từ chối đường dẫn bảo vệ; AwaitingHuman: tác vụ dừng chờ người ngay sau bước.
	ProtectedDenied, AwaitingHuman bool
}

// ClassifyStop là hàm thuần xác định lớp dừng; dữ kiện đầu tiên khớp thắng. Trả "" khi bước hoàn thành bình thường và
// không dừng chờ người. Ca không nhận ra rơi vào intrinsic (an toàn: chạy mới như cũ). Lỗi an toàn (rò cô lập, vi phạm
// chỉ đọc) đứng trước mọi lớp nối tiếp được nên không bao giờ được nối tiếp.
func ClassifyStop(f StopFacts) string {
	switch {
	case f.IsolationLeak || f.ReadonlyViolation:
		return StopIntrinsic
	case f.ProtectedDenied || f.AwaitingHuman:
		return StopAwaitingHuman
	case f.Interrupted:
		return StopExternal
	case f.LeaseExpired:
		return StopExternal
	case f.TimedOut:
		return StopTimeout
	case f.Infra || f.QuotaHit:
		return StopExternal
	case f.Failed && f.NoResult && !f.AgentFailed:
		return StopExternal
	case f.Failed || f.AgentFailed || f.CostCap:
		return StopIntrinsic
	}
	return ""
}

// stopContinuable: lớp cho phép nối tiếp, có handle phiên và có dấu vân tay cây lúc dừng.
func stopContinuable(class, handle, fingerprint string) bool {
	switch class {
	case StopExternal, StopAwaitingHuman, StopTimeout:
		return handle != "" && fingerprint != ""
	}
	return false
}
