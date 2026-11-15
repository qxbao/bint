package workflow

import "testing"

func TestClassifyStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    StopFacts
		want string
	}{
		{"hoàn thành bình thường", StopFacts{}, ""},
		{"429/hạn mức", StopFacts{Failed: true, Infra: true, QuotaHit: true}, StopExternal},
		{"API 5xx/api_error", StopFacts{Failed: true, Infra: true}, StopExternal},
		{"harness thoát bất thường không có kết quả", StopFacts{Failed: true, NoResult: true}, StopExternal},
		{"thoát mã khác 0 nhưng có kết quả", StopFacts{Failed: true}, StopIntrinsic},
		{"người huỷ (ctx)", StopFacts{Interrupted: true}, StopExternal},
		{"người huỷ kèm lỗi", StopFacts{Interrupted: true, Failed: true, NoResult: true}, StopExternal},
		{"hết hạn thuê driver session", StopFacts{LeaseExpired: true}, StopExternal},
		{"quá workflow.timeout_minutes", StopFacts{TimedOut: true, Failed: true}, StopTimeout},
		{"quá giờ kèm Infra", StopFacts{TimedOut: true, Infra: true}, StopTimeout},
		{"hook deny đường dẫn bảo vệ", StopFacts{ProtectedDenied: true}, StopAwaitingHuman},
		{"dừng chờ người sau bước", StopFacts{AwaitingHuman: true}, StopAwaitingHuman},
		{"vi phạm bước chỉ đọc (held)", StopFacts{ReadonlyViolation: true}, StopIntrinsic},
		{"vi phạm chỉ đọc kèm lỗi hạ tầng", StopFacts{ReadonlyViolation: true, Failed: true, Infra: true}, StopIntrinsic},
		{"vi phạm chỉ đọc kèm hook deny", StopFacts{ReadonlyViolation: true, ProtectedDenied: true}, StopIntrinsic},
		{"rò cô lập", StopFacts{IsolationLeak: true}, StopIntrinsic},
		{"rò cô lập kèm huỷ", StopFacts{IsolationLeak: true, Interrupted: true}, StopIntrinsic},
		{"agent báo thất bại", StopFacts{Failed: true, AgentFailed: true}, StopIntrinsic},
		{"agent báo thất bại, không có kết quả", StopFacts{Failed: true, AgentFailed: true, NoResult: true}, StopIntrinsic},
		{"chạm trần chi phí", StopFacts{Failed: true, CostCap: true}, StopIntrinsic},
		{"lỗi không nhận ra", StopFacts{Failed: true}, StopIntrinsic},
	} {
		if got := ClassifyStop(tc.f); got != tc.want {
			t.Errorf("%s: ClassifyStop(%+v) = %q, muốn %q", tc.name, tc.f, got, tc.want)
		}
	}
}

func TestStopContinuable(t *testing.T) {
	for _, tc := range []struct {
		class, handle, fp string
		want              bool
	}{
		{StopExternal, "h", "fp", true},
		{StopAwaitingHuman, "h", "fp", true},
		{StopTimeout, "h", "fp", true},
		{StopIntrinsic, "h", "fp", false},
		{"", "h", "fp", false},
		{StopExternal, "", "fp", false},
		{StopExternal, "h", "", false},
	} {
		if got := stopContinuable(tc.class, tc.handle, tc.fp); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}
