package iso

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

// Các loại việc của GC ngoài temp (xem TempAction cho temp).
const (
	GCCache   = "cache"   // tệp trong cache riêng cũ hơn cache_ttl
	GCSession = "session" // tệp phiên cũ hơn sessions_ttl
	GCInstall = "install" // bản cài harness thừa (--deep)
	GCProject = "project" // thư mục .claude/projects/<slug> của thư mục nguồn không còn (--deep)
	GCBusy    = "busy"    // bỏ qua cả profile: đang có phiên (khoá S)
	GCSkip    = "skip"    // bỏ qua phiên và bản cài: còn daemon Codex sống
)

// GCOptions là tuỳ chọn của GC.
type GCOptions struct {
	DryRun bool
	// Deep thêm bản cài harness thừa và thư mục dự án Claude mồ côi. Chỉ người gọi (CLI kiểm RequireHuman).
	Deep bool
	// Only (dạng <tool>/<name>) giới hạn vào một profile.
	Only string
	// Env: xem TempOptions.Env.
	Env []string
	// Now là thời điểm tính tuổi (mặc định time.Now()).
	Now time.Time
	// KeepSession cho biết ID phiên không được xoá (tác vụ chưa kết thúc còn tham chiếu). nil = không giữ gì.
	KeepSession func(id string) bool
}

// GCAction là một việc GC đã làm (hoặc sẽ làm khi DryRun).
type GCAction struct {
	Profile string `json:"profile"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	Removed bool   `json:"removed"`
	Note    string `json:"note,omitempty"`
}

// GCReport là kết quả GC: Temp từ CollectTemp, Actions là phần còn lại.
type GCReport struct {
	Temp    []TempAction `json:"temp"`
	Actions []GCAction   `json:"actions"`
}

// Freed là tổng dung lượng (byte) của các việc xoá trong Actions (không tính temp, vốn xoá cả thư mục).
func (r GCReport) Freed() int64 {
	var n int64
	for _, a := range r.Actions {
		if a.Kind != GCBusy && a.Kind != GCSkip {
			n += a.Bytes
		}
	}
	return n
}

var (
	uuidRE          = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	codexRolloutRE  = regexp.MustCompile(`^rollout-.*?(` + uuidRE + `)\.jsonl$`)
	claudeSessionRE = regexp.MustCompile(`^(` + uuidRE + `)\.jsonl$`)
)

// GC dọn mọi profile của store (docs/iso-plan.md mục 5.5): temp (CollectTemp), tệp cache riêng cũ hơn
// cache_ttl, tệp phiên cũ hơn sessions_ttl (trừ phiên KeepSession giữ), và với Deep: bản cài harness thừa
// cùng thư mục dự án Claude mồ côi. Mỗi profile lấy khoá X không chờ: đang có phiên thì bỏ qua cả profile;
// còn daemon Codex thì bỏ qua phiên và bản cài. Không đi qua liên kết chia sẻ và không bao giờ chạm dữ liệu
// người dùng ở gốc HOME giả. DryRun đi cùng một đường mã, chỉ bỏ bước xoá. Lỗi từng mục được gộp.
func GC(s *Store, o GCOptions) (GCReport, error) {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.KeepSession == nil {
		o.KeepSession = func(string) bool { return false }
	}
	var rep GCReport
	var errs []error
	temp, err := CollectTemp(s, TempOptions{DryRun: o.DryRun, Only: o.Only, Env: o.Env})
	rep.Temp = temp
	if err != nil {
		errs = append(errs, err) // gồm cả lỗi nạp profile (s.List)
	}
	profiles, _ := s.List()
	for _, p := range profiles {
		if o.Only != "" && p.ID() != o.Only {
			continue
		}
		g := &gcRun{p: p, o: o, rep: &rep}
		if err := g.run(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.ID(), err))
		}
	}
	return rep, errors.Join(errs...)
}

type gcRun struct {
	p    Profile
	o    GCOptions
	rep  *GCReport
	errs []error
}

func (g *gcRun) fail(err error) {
	if err != nil {
		g.errs = append(g.errs, err)
	}
}

func (g *gcRun) run() error {
	release, busy, err := g.p.Lock(true)
	if err != nil {
		return err
	}
	if busy {
		g.rep.Actions = append(g.rep.Actions, GCAction{Profile: g.p.ID(), Kind: GCBusy, Path: g.p.Dir, Note: "đang có phiên"})
		return nil
	}
	defer release()
	g.cache()
	if g.p.DaemonAlive() {
		g.rep.Actions = append(g.rep.Actions, GCAction{Profile: g.p.ID(), Kind: GCSkip, Path: g.p.daemonPidPath(),
			Note: "daemon app-server Codex đang chạy: bỏ qua phiên và bản cài"})
		return errors.Join(g.errs...)
	}
	gone := map[string]bool{} // slug .claude/projects sẽ bị xoá cả thư mục
	if g.o.Deep {
		g.installs()
		if g.p.Settings.SessionsTTL > 0 {
			g.goneProjects(gone)
		}
	}
	if g.p.Settings.SessionsTTL > 0 {
		g.sessions(gone)
	}
	return errors.Join(g.errs...)
}

// plainDir: path là thư mục thật (không phải liên kết). Không có thì ok=false, err=nil.
func plainDir(path string) (ok bool, err error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !fi.IsDir() {
		return false, nil
	}
	isl, err := isLink(path)
	if err != nil {
		return false, err
	}
	return !isl, nil
}

// remove xoá path (cây hoặc tệp) dưới thư mục profile và ghi một GCAction. Từ chối đường dẫn ngoài profile.
func (g *gcRun) remove(kind, path string) {
	if !within(path, g.p.Dir) {
		g.fail(fmt.Errorf("từ chối xoá %q: nằm ngoài thư mục profile", path))
		return
	}
	size, _ := treeSize(path)
	act := GCAction{Profile: g.p.ID(), Kind: kind, Path: path, Bytes: size}
	if !g.o.DryRun {
		if err := removeTree(path); err != nil {
			g.fail(err)
		} else {
			act.Removed = true
		}
	}
	g.rep.Actions = append(g.rep.Actions, act)
}

// removeEmptyDir gỡ thư mục vừa được dọn rỗng (DryRun: coi như đã rỗng nên chỉ tính dung lượng của chính nó).
// os.Remove từ chối thư mục còn mục, nên không bao giờ xoá nhầm dữ liệu.
func (g *gcRun) removeEmptyDir(kind, path string) {
	if !within(path, g.p.Dir) {
		g.fail(fmt.Errorf("từ chối xoá %q: nằm ngoài thư mục profile", path))
		return
	}
	act := GCAction{Profile: g.p.ID(), Kind: kind, Path: path}
	if fi, err := os.Lstat(path); err == nil {
		act.Bytes, _, _ = diskUsage(fi)
	}
	if !g.o.DryRun {
		if err := os.Remove(path); err != nil {
			g.fail(err)
		} else {
			act.Removed = true
		}
	}
	g.rep.Actions = append(g.rep.Actions, act)
}

// sweep duyệt root (thư mục thật), xoá mục không phải thư mục/liên kết mà want chọn, rồi gỡ thư mục con đã
// rỗng sau khi xoá (không bao giờ xoá root, không đi vào hay xoá liên kết). Trả số mục còn lại trong root
// (tính cả trong DryRun, để DryRun cho đúng danh sách như chạy thật).
func (g *gcRun) sweep(kind, root string, want func(path string, fi os.FileInfo) bool) int {
	ents, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			g.fail(err)
		}
		return 1 // không rõ: coi như còn mục để không xoá thư mục cha
	}
	remaining := 0
	for _, de := range ents {
		path := filepath.Join(root, de.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				g.fail(err)
				remaining++
			}
			continue
		}
		isl, err := isLink(path)
		if err != nil {
			g.fail(err)
			remaining++
			continue
		}
		switch {
		case isl:
			remaining++
		case fi.IsDir():
			before := len(g.rep.Actions)
			left := g.sweep(kind, path, want)
			if left == 0 && len(g.rep.Actions) > before {
				g.removeEmptyDir(kind, path)
			} else {
				remaining++
			}
		case want(path, fi):
			g.remove(kind, path)
		default:
			remaining++
		}
	}
	return remaining
}

// cache xoá tệp cache riêng (InHome của danh mục khi là thư mục thật, kể cả khi cache private) cũ hơn cache_ttl.
func (g *gcRun) cache() {
	ttl := g.p.Settings.CacheTTL
	if ttl <= 0 {
		return
	}
	cutoff := g.o.Now.Add(-ttl)
	seen := map[string]bool{}
	for _, in := range cacheSweepRoots(runtime.GOOS) {
		root := filepath.Join(g.p.Home, filepath.FromSlash(in))
		if seen[root] {
			continue
		}
		seen[root] = true
		ok, err := plainDir(root)
		if err != nil {
			g.fail(err)
			continue
		}
		if !ok {
			continue
		}
		g.sweep(GCCache, root, func(_ string, fi os.FileInfo) bool { return fi.ModTime().Before(cutoff) })
	}
}

// cacheSweepRoots là InHome của các mục danh mục mà GC được dọn theo tuổi: mọi mục trừ uv-data
// (~/.local/share/uv chứa tool và Python đã cài chứ không phải cache; mtime cũ không có nghĩa là thừa).
func cacheSweepRoots(goos string) []string {
	var out []string
	for _, s := range catalog(goos, newEnvList(goos, []string{"HOME=/h", "USERPROFILE=/h"})) {
		if s.Name == "uv-data" {
			continue
		}
		for _, l := range s.Links {
			out = append(out, strings.ReplaceAll(l.InHome, `\`, "/"))
		}
	}
	return out
}

// sessions xoá tệp phiên cũ hơn sessions_ttl: Codex rollout-*.jsonl trong sessions/archived_sessions, Claude
// projects/<slug>/<uuid>.jsonl (kèm thư mục <uuid>/). Phiên KeepSession giữ thì không đụng.
func (g *gcRun) sessions(gone map[string]bool) {
	cutoff := g.o.Now.Add(-g.p.Settings.SessionsTTL)
	cfg := filepath.Join(g.p.Home, g.p.Tool.spec().ConfigDir)
	switch g.p.Tool {
	case Codex:
		for _, d := range []string{"sessions", "archived_sessions"} {
			root := filepath.Join(cfg, d)
			if ok, err := plainDir(root); err != nil {
				g.fail(err)
			} else if ok {
				g.sweep(GCSession, root, func(path string, fi os.FileInfo) bool {
					m := codexRolloutRE.FindStringSubmatch(fi.Name())
					return fi.Mode().IsRegular() && m != nil && !g.o.KeepSession(m[1]) && fi.ModTime().Before(cutoff)
				})
			}
		}
	case Claude:
		projects := filepath.Join(cfg, "projects")
		ok, err := plainDir(projects)
		if err != nil || !ok {
			g.fail(err)
			return
		}
		slugs, err := os.ReadDir(projects)
		if err != nil {
			g.fail(err)
			return
		}
		for _, sl := range slugs {
			dir := filepath.Join(projects, sl.Name())
			if gone[sl.Name()] {
				continue
			}
			if ok, err := plainDir(dir); err != nil {
				g.fail(err)
				continue
			} else if !ok {
				continue
			}
			g.claudeSlug(dir, cutoff)
		}
	}
}

func (g *gcRun) claudeSlug(dir string, cutoff time.Time) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		g.fail(err)
		return
	}
	removed := 0
	for _, de := range ents {
		m := claudeSessionRE.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		path := filepath.Join(dir, de.Name())
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() || g.o.KeepSession(m[1]) || !fi.ModTime().Before(cutoff) {
			continue
		}
		g.remove(GCSession, path)
		removed++
		companion := filepath.Join(dir, m[1])
		if ok, err := plainDir(companion); err != nil {
			g.fail(err)
		} else if ok {
			g.remove(GCSession, companion)
			removed++
		}
	}
	if removed > 0 && len(ents) == removed {
		g.removeEmptyDir(GCSession, dir)
	}
}

// installs (--deep) xoá bản cài thừa: claude/versions/* trừ bản mà bin/claude trỏ tới; packages/*/releases/*
// của Codex trừ đích của current, chỉ khi packages là thư mục thật (liên kết tới bản của máy thì không đụng).
func (g *gcRun) installs() {
	versions := filepath.Join(g.p.Home, ".local", "share", "claude", "versions")
	if ok, err := plainDir(versions); err != nil {
		g.fail(err)
	} else if ok {
		keep, found := resolveInside(filepath.Join(g.p.Home, ".local", "bin", "claude"), versions)
		if found {
			g.removeExcept(GCInstall, versions, keep)
		}
	}
	if g.p.Tool != Codex {
		return
	}
	pk := g.p.codexPackagesPath()
	ok, err := plainDir(pk)
	if err != nil {
		g.fail(err)
		return
	}
	if !ok {
		return
	}
	pkgs, err := os.ReadDir(pk)
	if err != nil {
		g.fail(err)
		return
	}
	for _, pkg := range pkgs {
		releases := filepath.Join(pk, pkg.Name(), "releases")
		if ok, err := plainDir(releases); err != nil {
			g.fail(err)
			continue
		} else if !ok {
			continue
		}
		keep, found := resolveInside(filepath.Join(pk, pkg.Name(), "current"), releases)
		if found {
			g.removeExcept(GCInstall, releases, keep)
		}
	}
}

// resolveInside đọc liên kết link và trả tên mục con của dir mà nó trỏ tới. Không đọc được hoặc trỏ ra ngoài
// dir thì found=false (người gọi bỏ qua, giữ hết).
func resolveInside(link, dir string) (name string, found bool) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	rel, err := filepath.Rel(dir, filepath.Clean(target))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return first, true
}

// removeExcept xoá mọi mục con trực tiếp của dir trừ keep.
func (g *gcRun) removeExcept(kind, dir, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		g.fail(err)
		return
	}
	for _, de := range ents {
		if de.Name() != keep {
			g.remove(kind, filepath.Join(dir, de.Name()))
		}
	}
}

// goneProjects (--deep) xoá .claude/projects/<slug> mà thư mục nguồn không còn. Slug mã hoá "/" thành "-" nên
// không giải ngược được: lấy cwd từ một dòng JSON trong tệp .jsonl của slug. Không đọc được cwd, thư mục cha
// của nó cũng không còn (ổ chưa gắn?), hoặc slug có phiên KeepSession giữ thì giữ.
func (g *gcRun) goneProjects(gone map[string]bool) {
	if g.p.Tool != Claude {
		return
	}
	projects := filepath.Join(g.p.Home, g.p.Tool.spec().ConfigDir, "projects")
	if ok, err := plainDir(projects); err != nil {
		g.fail(err)
		return
	} else if !ok {
		return
	}
	slugs, err := os.ReadDir(projects)
	if err != nil {
		g.fail(err)
		return
	}
	for _, sl := range slugs {
		dir := filepath.Join(projects, sl.Name())
		if ok, err := plainDir(dir); err != nil {
			g.fail(err)
			continue
		} else if !ok {
			continue
		}
		cwd, ids := projectCwd(dir)
		if cwd == "" || !filepath.IsAbs(cwd) || !pathGone(cwd) {
			continue
		}
		if slices.ContainsFunc(ids, g.o.KeepSession) {
			continue
		}
		gone[sl.Name()] = true
		g.remove(GCProject, dir)
	}
}

// pathGone: path không còn nhưng thư mục cha của nó còn (chắc chắn là đã xoá chứ không phải ổ chưa gắn).
func pathGone(path string) bool {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	_, err := os.Stat(filepath.Dir(path))
	return err == nil
}

// projectCwd trả cwd của dòng JSON đầu tiên có trường cwd trong các tệp .jsonl của slug và ID phiên của slug.
func projectCwd(dir string) (cwd string, ids []string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", nil
	}
	var files []string
	for _, de := range ents {
		if m := claudeSessionRE.FindStringSubmatch(de.Name()); m != nil {
			ids = append(ids, m[1])
			files = append(files, filepath.Join(dir, de.Name()))
		}
	}
	for i, f := range files {
		if i >= 5 {
			break
		}
		if cwd = firstCwd(f); cwd != "" {
			return cwd, ids
		}
	}
	return "", ids
}

func firstCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for i := 0; i < 200 && sc.Scan(); i++ {
		var line struct {
			CWD string `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &line) == nil && line.CWD != "" {
			return line.CWD
		}
	}
	return ""
}
