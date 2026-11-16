package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Nhóm dung lượng của `bint iso du` (docs/iso-plan.md mục 5.5).
const (
	GroupAuth     = "auth_config"
	GroupSessions = "sessions"
	GroupCache    = "cache"
	GroupInstall  = "install"
	GroupOther    = "other"
	GroupUserData = "user_data"
)

// UserEntry là một mục dữ liệu người dùng ở gốc HOME giả (vd Projects/): bint không bao giờ tự xoá.
type UserEntry struct {
	Path  string `json:"path"` // tương đối theo HOME giả, kèm "/" cuối khi là thư mục
	Bytes int64  `json:"bytes"`
}

// ProfileUsage là dung lượng đĩa của một profile, tính bằng byte (số khối × 512 như `du`, hardlink đếm một
// lần, không đi qua liên kết). Total là cả thư mục profile và bằng tổng các nhóm (không gồm Temp, nằm ngoài
// thư mục profile); Temp đo riêng ở thư mục temp.
type ProfileUsage struct {
	ID          string      `json:"id"`
	Total       int64       `json:"total"`
	AuthConfig  int64       `json:"auth_config"`
	Sessions    int64       `json:"sessions"`
	Cache       int64       `json:"cache"`
	Install     int64       `json:"install"`
	Temp        int64       `json:"temp"`
	Other       int64       `json:"other"`
	UserData    int64       `json:"user_data"`
	UserEntries []UserEntry `json:"user_entries,omitempty"`
}

// KiB làm tròn lên như `du -sk`.
func KiB(b int64) int64 { return (b + 1023) / 1024 }

// walkUsage duyệt root bằng Lstat, không đi vào liên kết (symlink/junction; chính liên kết vẫn được tính),
// gọi visit cho từng mục với dung lượng đã khử hardlink (seen dùng chung giữa các lần gọi). Lỗi đọc được gộp,
// không dừng.
func walkUsage(root string, seen map[fileID]bool, visit func(path string, fi os.FileInfo, size int64)) error {
	var errs []error
	var walk func(path string)
	walk = func(path string) {
		fi, err := os.Lstat(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
			return
		}
		size, id, dedupe := diskUsage(fi)
		if dedupe {
			if seen[id] {
				size = 0
			} else {
				seen[id] = true
			}
		}
		visit(path, fi, size)
		if !fi.IsDir() {
			return
		}
		if isl, err := isLink(path); err != nil {
			errs = append(errs, err)
			return
		} else if isl {
			return
		}
		ents, err := os.ReadDir(path)
		if err != nil {
			errs = append(errs, err)
			return
		}
		for _, de := range ents {
			walk(filepath.Join(path, de.Name()))
		}
	}
	walk(root)
	return errors.Join(errs...)
}

// treeSize là dung lượng của path (cây hoặc tệp) như một lần `du -s`.
func treeSize(path string) (int64, error) {
	var total int64
	err := walkUsage(path, map[fileID]bool{}, func(_ string, _ os.FileInfo, size int64) { total += size })
	return total, err
}

// usageRule gán một đường dẫn (tương đối theo thư mục profile, dùng "/") và mọi thứ dưới nó vào một nhóm.
type usageRule struct{ prefix, group string }

// usageGroups là bảng phân loại, một nguồn cho `du` và GC. Khớp tiền tố dài nhất thắng; mục không khớp là
// GroupOther. Đường dẫn cache (InHome của danh mục share) được thêm động trong classify.
var usageGroups = []usageRule{
	{"profile.json", GroupAuth}, {"session.lock", GroupAuth}, {"sync-receipt.json", GroupAuth},
	{"home/.codex/auth.json", GroupAuth}, {"home/.codex/config.toml", GroupAuth}, {"home/.codex/hooks.json", GroupAuth},
	{"home/.claude/.credentials.json", GroupAuth}, {"home/.claude/settings.json", GroupAuth},
	{"home/.claude.json", GroupAuth}, {"home/.config", GroupAuth},
	{"home/.codex/sessions", GroupSessions}, {"home/.codex/archived_sessions", GroupSessions},
	{"home/.claude/projects", GroupSessions},
	{"home/.local/share/claude/versions", GroupInstall}, {"home/.local/bin/claude", GroupInstall},
	{"home/.codex/packages", GroupInstall},
}

// shareInHomes là InHome (dạng "/") của mọi mục danh mục share trên goos.
func shareInHomes(goos string) []string {
	var out []string
	for _, s := range catalog(goos, newEnvList(goos, []string{"HOME=/h", "USERPROFILE=/h"})) {
		for _, l := range s.Links {
			out = append(out, strings.ReplaceAll(l.InHome, `\`, "/"))
		}
	}
	return out
}

// usageClassifier phân loại đường dẫn trong thư mục profile.
type usageClassifier struct {
	rules      []usageRule
	known      map[string]bool // tên ở gốc HOME giả thuộc danh mục đã biết (không phải dữ liệu người dùng)
	cachePaths []string
}

func newUsageClassifier(goos string) usageClassifier {
	c := usageClassifier{rules: usageGroups, known: map[string]bool{"go": true, "Library": true, "AppData": true}}
	for _, in := range shareInHomes(goos) {
		c.cachePaths = append(c.cachePaths, "home/"+in)
		first, _, _ := strings.Cut(in, "/")
		c.known[first] = true
	}
	return c
}

func hasPathPrefix(rel, prefix string) bool {
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}

// userDataName: mục ở gốc HOME giả là dữ liệu người dùng khi không bắt đầu bằng "." và không thuộc danh mục.
func (c usageClassifier) userDataName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !c.known[name]
}

// classify trả nhóm của rel ("/" làm phân cách, "." là gốc profile) và tên mục dữ liệu người dùng (nếu có).
// Liên kết ở đúng đường dẫn cache là GroupOther: nó chỉ là một con trỏ, dữ liệu nằm ở đích (cache riêng = 0).
func (c usageClassifier) classify(rel string, link bool) (group, userEntry string) {
	if rest, ok := strings.CutPrefix(rel, "home/"); ok {
		name, _, _ := strings.Cut(rest, "/")
		if c.userDataName(name) {
			return GroupUserData, name
		}
	}
	best, group := -1, GroupOther
	for _, r := range c.rules {
		if hasPathPrefix(rel, r.prefix) && len(r.prefix) > best {
			best, group = len(r.prefix), r.group
		}
	}
	for _, cp := range c.cachePaths {
		if hasPathPrefix(rel, cp) && len(cp) > best {
			if link && rel == cp {
				continue
			}
			best, group = len(cp), GroupCache
		}
	}
	return group, ""
}

// Usage đo profile p: một lượt duyệt thư mục profile (khử hardlink trên toàn cây, nên tổng các nhóm đúng
// bằng `du -s` thư mục profile) và một lượt cho thư mục temp theo env (mặc định os.Environ()).
func Usage(p Profile, env []string) (ProfileUsage, error) {
	if env == nil {
		env = os.Environ()
	}
	return usageOf(runtime.GOOS, p, env)
}

func usageOf(goos string, p Profile, env []string) (ProfileUsage, error) {
	u := ProfileUsage{ID: p.ID()}
	c := newUsageClassifier(goos)
	entries := map[string]int64{}
	dirs := map[string]bool{}
	var errs []error
	err := walkUsage(p.Dir, map[fileID]bool{}, func(path string, fi os.FileInfo, size int64) {
		rel, rerr := filepath.Rel(p.Dir, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		isl, _ := isLink(path)
		group, entry := c.classify(rel, isl)
		u.Total += size
		switch group {
		case GroupAuth:
			u.AuthConfig += size
		case GroupSessions:
			u.Sessions += size
		case GroupCache:
			u.Cache += size
		case GroupInstall:
			u.Install += size
		case GroupUserData:
			u.UserData += size
			entries[entry] += size
			if rel == "home/"+entry {
				dirs[entry] = fi.IsDir()
			}
		default:
			u.Other += size
		}
	})
	if err != nil {
		errs = append(errs, err)
	}
	for name, b := range entries {
		path := name
		if dirs[name] {
			path += "/"
		}
		u.UserEntries = append(u.UserEntries, UserEntry{Path: path, Bytes: b})
	}
	slices.SortFunc(u.UserEntries, func(a, b UserEntry) int { return strings.Compare(a.Path, b.Path) })
	if t, terr := treeSize(p.tempDirFrom(env)); terr != nil {
		errs = append(errs, terr)
	} else {
		u.Temp = t
	}
	if len(errs) > 0 {
		return u, fmt.Errorf("đo %s: %w", p.ID(), errors.Join(errs...))
	}
	return u, nil
}

// UsageAll đo mọi profile của store (hoặc một profile theo only, dạng <tool>/<name>). Lỗi nạp/đo gộp lại, kèm
// phần đo được.
func UsageAll(s *Store, env []string, only string) ([]ProfileUsage, error) {
	profiles, lerr := s.List()
	var out []ProfileUsage
	var errs []error
	if lerr != nil {
		errs = append(errs, lerr)
	}
	for _, p := range profiles {
		if only != "" && p.ID() != only {
			continue
		}
		u, err := Usage(p, env)
		if err != nil {
			errs = append(errs, err)
		}
		out = append(out, u)
	}
	return out, errors.Join(errs...)
}
