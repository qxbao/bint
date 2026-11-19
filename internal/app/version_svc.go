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
	// ConfigOutdated: cấu hình theo mẫu cũ (config.Upgrade có gì để đổi); ConfigAdded: khoá còn thiếu,
	// ConfigMigrated: khoá có giá trị mẫu cũ sẽ được nâng.
	ConfigOutdated bool
	ConfigAdded    []string
	ConfigMigrated []string
	// LockMismatch: core.lock ghi phiên bản khác binary (skill xuất bản bằng bản Bint khác) → chạy sync.
	LockMismatch bool
	// Direction: hướng lệch của binary so với bản đã ghi core.lock (cùng semver thì xét revision/vcs.time).
	Direction skills.Direction
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
	bin := skills.CurrentBuild()
	bin.Version = binVersion
	vs.Direction = skills.CompareBuild(lock, bin)
	if up, uerr := readUpgrade(root); uerr == nil && up.Changed() {
		vs.ConfigOutdated, vs.ConfigAdded, vs.ConfigMigrated = true, up.Added, up.Migrated
	}
	return vs, nil
}
