package app

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// binaryVersion là phiên bản của binary đang chạy, do tầng cli đặt lúc khởi động (SetBinaryVersion). Doctor dùng nó để
// biết hướng lệch phiên bản với core.lock; rỗng = không biết.
var binaryVersion atomic.Value // string

// SetBinaryVersion ghi phiên bản của binary đang chạy cho Doctor.
func SetBinaryVersion(v string) { binaryVersion.Store(v) }

func runningVersion() string {
	v, _ := binaryVersion.Load().(string)
	return v
}

// compareVersions so sánh hai chuỗi phiên bản dạng [v]MAJOR[.MINOR[.PATCH]][-hậu tố]: -1 nếu a < b, 0 nếu bằng nhau,
// 1 nếu a > b. ok=false khi một bên không đọc được hoặc hai bên cùng phần số (chỉ khác hậu tố như "-dev": không suy ra
// được bên nào mới hơn).
func compareVersions(a, b string) (cmp int, ok bool) {
	pa, oka := parseVersionNumbers(a)
	pb, okb := parseVersionNumbers(b)
	if !oka || !okb {
		return 0, false
	}
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1, true
		case pa[i] > pb[i]:
			return 1, true
		}
	}
	return 0, false
}

func parseVersionNumbers(v string) (n [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// staleBinaryAdvice là lời khuyên của doctor khi tệp skill khớp core.lock nhưng core.lock ghi băm skill gốc khác bản
// nhúng: tệp do một bản Bint khác ghi. Hướng phiên bản quyết định: lock mới hơn binary → binary cũ (cập nhật bint, đừng
// sync vì sẽ hạ cấp); lock cũ hơn binary → Bint vừa nâng cấp (chạy sync); không phân biệt được → nêu cả hai.
func staleBinaryAdvice(lockVersion, binVersion string) string {
	if c, ok := compareVersions(lockVersion, binVersion); ok {
		if c > 0 {
			return "bint đang chạy cũ hơn: cập nhật bint; đừng chạy `bint sync` bằng binary này (nó coi tệp khớp core.lock là chưa sửa và sẽ hạ cấp skill)"
		}
		return "Bint đã nâng cấp: chạy `bint sync`"
	}
	return "chưa xác định được bint đang chạy cũ hay mới hơn bản đã xuất bản: nếu vừa nâng cấp Bint thì chạy `bint sync`; nếu bint đang chạy " +
		"cũ hơn (core.lock do bản mới hơn ghi) thì cập nhật bint trước, đừng sync bằng binary này vì sẽ hạ cấp skill"
}
