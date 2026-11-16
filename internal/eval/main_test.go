package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"bint/internal/testutil"
)

// codexFixtureEnv: khi đặt, binary kiểm thử đóng vai `codex exec` giả: đọc hết stdin, phát một sự kiện turn.failed với
// thông điệp lấy từ biến này rồi thoát mã 1 (xem TestCodexRunClassifiesInfraFromTurnFailed).
const codexFixtureEnv = "BINT_CODEX_FIXTURE_FAIL"

// rawFixtureEnv: binary kiểm thử đóng vai claude/codex giả thoát mã 1, in nguyên văn giá trị biến này ra stderr và không in
// JSON nào (thông báo "không tìm thấy phiên" của harness khi nối tiếp, xem resume_test.go).
const rawFixtureEnv = "BINT_AGENT_FIXTURE_RAW"

// TestMain dọn thư mục harness giả của testutil.Isolate khi tiến trình kiểm thử xong.
func TestMain(m *testing.M) {
	if msg, ok := os.LookupEnv(rawFixtureEnv); ok {
		io.Copy(io.Discard, os.Stdin)
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	if msg, ok := os.LookupEnv(codexFixtureEnv); ok {
		io.Copy(io.Discard, os.Stdin)
		line, _ := json.Marshal(map[string]any{"type": "turn.failed", "error": map[string]string{"message": msg}})
		fmt.Printf("{\"type\":\"thread.started\",\"thread_id\":\"t1\"}\n%s\n", line)
		os.Exit(1)
	}
	testutil.Main(m)
}
