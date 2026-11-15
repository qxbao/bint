package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"bint/internal/skills"
)

// AutoResync tự xuất lại skill gốc khi binary sạch mới hơn core.lock, băm skill gốc nhúng khác lock và bản xem trước
// sync không có xung đột (tệp đã xuất chưa bị sửa tay). Trả dòng thông báo (rỗng = không làm gì). Bỏ qua khi: trong phiên
// agent hoặc có BINT_RUN_ID (ghi skill giữa phiên agent sinh thay đổi ngoài phạm vi phiếu), binary có vcs.modified, binary
// cũ hơn hoặc không xác định so với core.lock, repository chưa init. Không bao giờ gọi từ hook. Lỗi do nơi gọi quyết
// định (CLI chỉ cảnh báo, không làm lệnh thất bại).
func AutoResync(ctx context.Context, dir, version string) (string, error) {
	if in, _ := InAgentSession(); in || strings.TrimSpace(os.Getenv(EnvRunID)) != "" {
		return "", nil
	}
	bin := buildFor(version)
	if bin.Modified {
		return "", nil
	}
	k, err := openSync(ctx, dir)
	if err != nil {
		return "", nil // chưa init: không phải việc của bước này
	}
	lock, err := skills.ReadLock(k.Root)
	if err != nil || !skills.CoreChangedIn(lock) || skills.CompareBuild(lock, bin) != skills.DirBinNewer {
		return "", err
	}
	plan, err := PlanSync(ctx, dir, version, SyncOptions{})
	if err != nil || plan.NoTargets || plan.Conflicts > 0 || plan.Changes == 0 {
		return "", err
	}
	si, err := Sync(ctx, dir, version, SyncOptions{})
	if err != nil {
		return "", err
	}
	if si.Conflicts > 0 {
		return "", nil
	}
	return fmt.Sprintf("bint: đã tự xuất lại skill gốc theo binary mới hơn core.lock (%d tệp)", plan.Changes), nil
}
