package hookcore

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/qxbao/bgraph/agent"
)

// GuardrailWideTestNudge là id của lời nhắc dựng sẵn khi agent ngoài tác vụ sắp chạy cả bộ test: trỏ tới `bint test affected --run`. Chỉ
// nhắc, không chặn; trong tác vụ B′ đang b-feat thì wideTestDenial chặn thay (widetest_guard.go).
const GuardrailWideTestNudge = "wide-test-nudge"

// WideTestNudge là câu chèn vào ngữ cảnh. Câu nêu sự thật, không ra lệnh: agent tự quyết (cùng tinh thần agent.Nudge của bgraph).
const WideTestNudge = "Bint: lệnh này chạy cả bộ test. `bint test affected --run` chỉ chạy các test mà thay đổi chưa commit có thể " +
	"ảnh hưởng (bgraph chọn theo đồ thị phụ thuộc; không thu hẹp được thì chạy cả bộ); `--base <ref>` để so với commit khác. " +
	"CI vẫn là lần kiểm cuối."

// wideTestNudge trả true khi nên nhắc: lệnh chạy cả bộ test (agent.IsFullSuiteCommand của bgraph), lần gọi không thuộc tác vụ Bint,
// và là lần đầu của phiên (tệp dấu cạnh trạng thái phiên sessionPath). Thuộc tác vụ là tiến trình có BINT_TASK_ID (lượt `bint run`) hoặc
// công cụ của subagent bint-* (driver session không đặt BINT_TASK_ID): ở đó đã có `bint task check` và kiểm chứng của Bint. Lỗi ghi dấu
// thì không nhắc: thiếu một lời nhắc không gây hại, nhắc lặp mỗi lệnh thì có.
func wideTestNudge(sessionPath, agentType, command string) bool {
	if strings.TrimSpace(os.Getenv("BINT_TASK_ID")) != "" || strings.HasPrefix(agentType, "bint-") || sessionPath == "" ||
		!agent.IsFullSuiteCommand(command) {
		return false
	}
	marker := sessionPath + ".widetest-nudge"
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return false
	}
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false // đã nhắc trong phiên này, hoặc không ghi được dấu
	}
	_ = f.Close()
	return true
}
