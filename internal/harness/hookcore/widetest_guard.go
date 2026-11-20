package hookcore

import (
	"os"
	"strings"

	"bint/internal/task"
)

// GuardrailWideTest là id của guardrail dựng sẵn chặn agent tự chạy toàn bộ bộ test trong lượt b-feat của luồng B′ (docs/bprime-budget-plan.md mục
// 1.8d). Đợt 7: lời dặn "đừng tự chạy toàn bộ bộ test" trong đề bài không đủ, lượt lõi tự chạy `go test` 13 lần; mỗi lần trên repo này mất 10–30 phút
// và đầu ra dài đọc lại ở mọi lượt sau. Bint đã có kiểm hẹp (`bint task check`) và kiểm nền toàn repo.
const GuardrailWideTest = "bprime-wide-test"

// wideTestDenial trả lý do từ chối khi lệnh shell command là lệnh kiểm chứng đầy đủ của tác vụ B′ đang chạy b-feat (BINT_TASK_ID của tiến trình
// agent, tác vụ in_progress). Rỗng khi không áp dụng. So khớp: đúng verify.command của phiếu (bỏ khác biệt khoảng trắng), hoặc cùng chương trình
// và cùng mẫu chạy toàn bộ (ví dụ `go test … ./...`) như lệnh đó.
func wideTestDenial(runtimeDir, command string) string {
	id := strings.TrimSpace(os.Getenv("BINT_TASK_ID"))
	if id == "" || runtimeDir == "" || strings.TrimSpace(command) == "" {
		return ""
	}
	t, err := task.NewStore(runtimeDir).Load(id)
	if err != nil || !task.IsBPrime(t) || t.State.Status != task.StatusInProgress {
		return ""
	}
	verify := strings.TrimSpace(t.Ticket.Verify.Command)
	if verify == "" || !sameWideRun(command, verify) {
		return ""
	}
	return "luồng B′: đừng tự chạy toàn bộ bộ test (`" + verify + "`) trong lượt b-feat. Chạy `bint task check " + id + "`: test của các gói vừa đổi, " +
		"kèm kết quả kiểm toàn repo Bint đang chạy nền; Bint tự chạy kiểm chứng đầy đủ sau lượt này."
}

// sameWideRun: cmd là lệnh kiểm chứng verify (bỏ khác biệt khoảng trắng), hoặc chạy cùng chương trình test với mọi mẫu "toàn bộ" (đuôi /... hoặc .)
// của verify. Bảo thủ: chỉ xét lệnh đơn (không ghép bằng &&, ;, |).
func sameWideRun(cmd, verify string) bool {
	norm := func(s string) []string { return strings.Fields(s) }
	c, v := norm(cmd), norm(verify)
	if strings.Join(c, " ") == strings.Join(v, " ") {
		return true
	}
	if strings.ContainsAny(cmd, "&;|") || len(c) < 2 || len(v) < 2 || c[0] != v[0] || c[1] != v[1] {
		return false
	}
	wide := func(args []string) []string {
		var out []string
		for _, a := range args[2:] {
			if strings.HasSuffix(a, "/...") || a == "." || a == "./" {
				out = append(out, a)
			}
		}
		return out
	}
	vw, cw := wide(v), wide(c)
	if len(vw) == 0 || len(cw) == 0 {
		return false
	}
	for _, a := range vw {
		found := false
		for _, b := range cw {
			if a == b {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
