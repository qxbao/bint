package workflow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
)

// Bước review gọi `bint task review` từ agent: nếu binary đó không đọc được .bint/config.yml (b-feat vừa thêm khoá cấu hình mới
// mà binary cũ chưa biết, KnownFields từ chối), review thất bại và tác vụ failed với lý do mơ hồ. configSkew phát hiện trước khi
// chạy b-review để không phí lượt review và báo rõ: build lại binary rồi resume.

// cliSkewTimeout là thời gian tối đa cho lệnh `bint` trên PATH khi kiểm.
const cliSkewTimeout = 10 * time.Second

// configSkew trả lý do (rỗng = ổn) khi cấu hình của tác vụ không đọc được bằng binary đang chạy hoặc bằng `bint` trên PATH của agent.
func (r *Runner) configSkew(ctx context.Context, rs *runState) string {
	root, err := r.RootFor(rs.t)
	if err != nil {
		return ""
	}
	id := rs.t.State.ID
	if _, _, err := config.Load(root, false); err != nil {
		return skewMessage(selfExe(), err.Error(), id, r.resumeCmd(rs))
	}
	check := r.CLISkewCheck
	if check == nil {
		check = cliSkewCheck
	}
	exe, msg, skipped := check(ctx, root, id)
	if msg != "" {
		return skewMessage(exe, msg, id, r.resumeCmd(rs))
	}
	if skipped != "" {
		rs.res.Notes = append(rs.res.Notes, "bỏ qua kiểm `bint` trên PATH trước b-review: "+skipped)
	}
	return ""
}

func skewMessage(exe, detail, id, resume string) string {
	return fmt.Sprintf("binary `%s` không đọc được .bint/config.yml của tác vụ (%s): binary cũ hơn cấu hình (b-feat có thể đã thêm khoá mới); "+
		"build lại bint (`go install ./cmd/bint`, bảo đảm `bint` trên PATH là bản mới) rồi `%s`",
		exe, strings.TrimSpace(detail), resume)
}

func selfExe() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "bint"
}

// cliSkewCheck chạy `bint task show <id>` của PATH (chỉ khi khác binary đang chạy) trong root: lỗi giải mã cấu hình ("not found")
// nghĩa là binary đó cũ hơn cấu hình. Không tìm thấy, hết giờ hoặc lỗi khác: không chặn, trả lý do bỏ qua (skipped) để ghi Notes.
func cliSkewCheck(ctx context.Context, root, id string) (exe, msg, skipped string) {
	p, err := exec.LookPath("bint")
	if err != nil {
		return "", "", "không tìm thấy `bint` trên PATH"
	}
	if self, err := os.Executable(); err == nil && sameBinary(self, p) {
		return "", "", "" // cùng binary: lần nạp ở trên đã kiểm
	}
	cctx, cancel := context.WithTimeout(ctx, cliSkewTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, p, "task", "show", id)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()
	if runErr == nil {
		return "", "", ""
	}
	text := out.String()
	if strings.Contains(text, "not found in type") {
		line := text
		for _, l := range strings.Split(text, "\n") {
			if strings.Contains(l, "not found in type") {
				line = l
				break
			}
		}
		return p, line, ""
	}
	if cctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Sprintf("`%s task show` quá %s", p, cliSkewTimeout)
	}
	return "", "", fmt.Sprintf("`%s task show` lỗi không phải lỗi giải mã cấu hình (%v)", p, runErr)
}

func sameBinary(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return os.SameFile(ai, bi)
}
