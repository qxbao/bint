package skills

import (
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BuildStamp là dấu build của binary đang chạy: phiên bản (ldflags) và thông tin VCS do `go build` gắn vào
// (debug.ReadBuildInfo vcs.revision/vcs.time/vcs.modified). Revision/Time rỗng khi bản build không có thông tin VCS
// (go test, go run, build ngoài cây git).
type BuildStamp struct {
	Version  string
	Revision string // băm commit đầy đủ
	Time     string // vcs.time, RFC 3339
	Modified bool   // vcs.modified: cây làm việc có thay đổi chưa commit lúc build
}

// Short là revision rút gọn 7 ký tự.
func (b BuildStamp) Short() string {
	if len(b.Revision) > 7 {
		return b.Revision[:7]
	}
	return b.Revision
}

// String: "0.1.0-dev (abc1234, 2026-11-15T10:00:00Z, sửa)"; không có VCS thì chỉ phiên bản.
func (b BuildStamp) String() string {
	if b.Revision == "" {
		return b.Version
	}
	parts := []string{b.Short()}
	if b.Time != "" {
		parts = append(parts, b.Time)
	}
	if b.Modified {
		parts = append(parts, "sửa")
	}
	return b.Version + " (" + strings.Join(parts, ", ") + ")"
}

var (
	stampMu      sync.RWMutex
	stampVersion string
	stampForced  *BuildStamp // ghi đè cho test: test binary không có thông tin VCS
)

// SetBuildVersion ghi phiên bản binary đang chạy (tầng cli gọi lúc khởi động).
func SetBuildVersion(v string) {
	stampMu.Lock()
	stampVersion = v
	stampMu.Unlock()
}

// SetBuildOverride thay dấu build hiện tại bằng s (nil = bỏ ghi đè); dành cho kiểm thử. Trả hàm khôi phục.
func SetBuildOverride(s *BuildStamp) (restore func()) {
	stampMu.Lock()
	old := stampForced
	stampForced = s
	stampMu.Unlock()
	return func() {
		stampMu.Lock()
		stampForced = old
		stampMu.Unlock()
	}
}

// CurrentBuild trả dấu build của binary đang chạy.
func CurrentBuild() BuildStamp {
	stampMu.RLock()
	forced, v := stampForced, stampVersion
	stampMu.RUnlock()
	if forced != nil {
		return *forced
	}
	b := BuildStamp{Version: v}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.time":
				b.Time = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			}
		}
	}
	return b
}

// Direction là hướng lệch giữa binary đang chạy và bản Bint đã ghi core.lock.
type Direction int

const (
	DirUnknown  Direction = iota // không đủ thông tin (hoặc binary có sửa) để kết luận
	DirSame                      // cùng bản
	DirBinOlder                  // binary cũ hơn bản đã ghi core.lock
	DirBinNewer                  // binary mới hơn bản đã ghi core.lock
)

func (d Direction) String() string {
	switch d {
	case DirSame:
		return "same"
	case DirBinOlder:
		return "bin_older"
	case DirBinNewer:
		return "bin_newer"
	}
	return "unknown"
}

// Stamp trả dấu build ghi trong core.lock.
func (l Lock) Stamp() BuildStamp {
	return BuildStamp{Version: l.BintVersion, Revision: l.BintRevision, Time: l.BintRevisionTime, Modified: l.BintModified}
}

// CompareBuild so binary với bản ghi trong core.lock: semver khác nhau thì theo semver; cùng semver thì theo vcs.time
// của hai revision; cùng revision thì lock có sửa mà binary sạch nghĩa là binary cũ hơn. Thiếu thông tin, binary có sửa
// (không biết nó nằm đâu so với revision) hoặc cùng thời điểm khác revision là không xác định.
func CompareBuild(lock Lock, bin BuildStamp) Direction {
	if c, ok := CompareVersions(lock.BintVersion, bin.Version); ok {
		if c > 0 {
			return DirBinOlder
		}
		return DirBinNewer
	}
	if bin.Modified || bin.Revision == "" || lock.BintRevision == "" {
		return DirUnknown
	}
	if lock.BintRevision == bin.Revision {
		if lock.BintModified {
			return DirBinOlder
		}
		return DirSame
	}
	lt, err1 := time.Parse(time.RFC3339, lock.BintRevisionTime)
	bt, err2 := time.Parse(time.RFC3339, bin.Time)
	if err1 != nil || err2 != nil {
		return DirUnknown
	}
	switch {
	case bt.After(lt):
		return DirBinNewer
	case bt.Before(lt):
		return DirBinOlder
	}
	return DirUnknown
}

// CompareVersions so sánh hai chuỗi phiên bản dạng [v]MAJOR[.MINOR[.PATCH]][-hậu tố]: -1 nếu a < b, 1 nếu a > b.
// ok=false khi một bên không đọc được hoặc hai bên cùng phần số (chỉ khác hậu tố như "-dev").
func CompareVersions(a, b string) (cmp int, ok bool) {
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
