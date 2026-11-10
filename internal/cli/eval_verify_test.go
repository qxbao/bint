package cli

import (
	"testing"

	"bint/internal/eval"
)

func TestSettleVerificationKiemChungLai(t *testing.T) {
	ok := &eval.Verification{Status: "verified"}
	bad := &eval.Verification{Status: "failed", Detail: "mới"}
	cases := []struct {
		name       string
		status     string
		reason     string
		v          *eval.Verification
		wantStatus string
		wantReason string
	}{
		{"loại do kiểm chứng, nay đạt", eval.StatusRejected, "kiểm chứng: cũ", ok, eval.StatusCandidate, ""},
		{"loại do kiểm chứng, vẫn hỏng", eval.StatusRejected, "kiểm chứng: cũ", bad, eval.StatusRejected, "kiểm chứng: mới"},
		{"người duyệt loại, nay đạt", eval.StatusRejected, "người duyệt loại", ok, eval.StatusRejected, "người duyệt loại"},
		{"người duyệt loại, vẫn hỏng", eval.StatusRejected, "người duyệt loại", bad, eval.StatusRejected, "người duyệt loại"},
		{"ứng viên hỏng", eval.StatusCandidate, "", bad, eval.StatusRejected, "kiểm chứng: mới"},
		{"ứng viên đạt", eval.StatusCandidate, "", ok, eval.StatusCandidate, ""},
	}
	for _, tc := range cases {
		c := &eval.Case{Status: tc.status, RejectReason: tc.reason, Verification: tc.v}
		settleVerification(c)
		if c.Status != tc.wantStatus || c.RejectReason != tc.wantReason {
			t.Errorf("%s: %s %q", tc.name, c.Status, c.RejectReason)
		}
	}
}
