package app

import (
	"fmt"

	"bint/internal/skills"
)

// SetBinaryVersion ghi phiên bản của binary đang chạy (tầng cli gọi lúc khởi động) để doctor biết hướng lệch phiên bản
// với core.lock; rỗng = không biết.
func SetBinaryVersion(v string) { skills.SetBuildVersion(v) }

func runningVersion() string { return skills.CurrentBuild().Version }

// compareVersions: xem skills.CompareVersions.
func compareVersions(a, b string) (cmp int, ok bool) { return skills.CompareVersions(a, b) }

// buildFor trả dấu build của binary đang chạy với phiên bản do nơi gọi truyền (version rỗng thì giữ phiên bản đã đặt).
func buildFor(version string) skills.BuildStamp {
	b := skills.CurrentBuild()
	if version != "" {
		b.Version = version
	}
	return b
}

// staleBinaryAdvice là lời khuyên của doctor khi tệp skill khớp core.lock nhưng core.lock ghi băm skill gốc khác bản
// nhúng: tệp do một bản Bint khác ghi. Hướng lệch quyết định: binary cũ hơn → cập nhật bint (đừng sync vì sẽ hạ cấp);
// binary mới hơn → Bint vừa nâng cấp (chạy sync); không phân biệt được → nêu cả hai.
func staleBinaryAdvice(dir skills.Direction) string {
	switch dir {
	case skills.DirBinOlder:
		return "bint đang chạy cũ hơn: cập nhật bint (`bint update --pull`); đừng chạy `bint sync` bằng binary này (nó coi tệp khớp core.lock là chưa sửa và sẽ hạ cấp skill)"
	case skills.DirBinNewer:
		return "Bint đã nâng cấp: chạy `bint sync`"
	}
	return "chưa xác định được bint đang chạy cũ hay mới hơn bản đã xuất bản: nếu vừa nâng cấp Bint thì chạy `bint sync`; nếu bint đang chạy " +
		"cũ hơn (core.lock do bản mới hơn ghi) thì cập nhật bint trước, đừng sync bằng binary này vì sẽ hạ cấp skill"
}

// versionCheck thêm mục "phiên bản skill": so binary đang chạy với dấu build trong core.lock. Không có lock thì bỏ qua.
func versionCheck(r *DoctorReport, lock skills.Lock) {
	if lock.BintVersion == "" && len(lock.Skills) == 0 {
		return
	}
	const name = "phiên bản skill"
	bin := skills.CurrentBuild()
	if !skills.CoreChangedIn(lock) {
		r.add(name, LevelOK, "khớp: core.lock %s, bint đang chạy %s", lock.Stamp(), bin)
		return
	}
	detail := fmt.Sprintf("core.lock ghi bởi %s, bint đang chạy %s", lock.Stamp(), bin)
	switch skills.CompareBuild(lock, bin) {
	case skills.DirBinOlder:
		r.add(name, LevelWarn, "binary cũ hơn core.lock (%s): chạy `bint update --pull`; đừng `bint sync` bằng binary này vì sẽ hạ cấp skill", detail)
	case skills.DirBinNewer:
		r.addFix(DoctorFix{Kind: FixSync}, name, LevelInfo, "binary mới hơn core.lock (%s): chạy `bint sync`", detail)
	default:
		r.add(name, LevelInfo, "chưa xác định binary cũ hay mới hơn core.lock (%s): thiếu thông tin VCS hoặc binary có thay đổi chưa commit", detail)
	}
}
