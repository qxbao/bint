package app

import (
	"context"

	"bint/internal/skills"
)

// VersionStatus là phiên bản của binary đang chạy đối chiếu với repository (Tổng quan, hộp thư; docs/tui-plan.md
// mục 7.6). Chỉ đọc.
type VersionStatus struct {
	// Binary là phiên bản Bint đang chạy; Revision là commit của bản build (rỗng nếu không có thông tin VCS).
	Binary   string
	Revision string
	// LockVersion là bint_version trong .bint/skills/core.lock ("" = chưa có lock).
	LockVersion string
	// ConfigOutdated: cấu hình theo mẫu cũ (config.Upgrade có gì để thêm).
	ConfigOutdated bool
	ConfigAdded    []string
	// LockMismatch: core.lock ghi phiên bản khác binary (skill xuất bản bằng bản Bint khác) → chạy sync.
	LockMismatch bool
}

// VersionInfo đối chiếu binary với repository chứa dir. Repository chưa init trả ErrNotInitialized.
func VersionInfo(ctx context.Context, dir, binVersion string) (VersionStatus, error) {
	vs := VersionStatus{Binary: binVersion, Revision: SelfRevision()}
	ws, err := OpenWorkspace(ctx, dir, true)
	if err != nil {
		return vs, err
	}
	root := configRoot(ws.Repo)
	lock, err := skills.ReadLock(root)
	if err != nil {
		return vs, err
	}
	vs.LockVersion = lock.BintVersion
	vs.LockMismatch = vs.LockVersion != "" && vs.LockVersion != binVersion
	if up, uerr := readUpgrade(root); uerr == nil && up.Changed() {
		vs.ConfigOutdated, vs.ConfigAdded = true, up.Added
	}
	return vs, nil
}
