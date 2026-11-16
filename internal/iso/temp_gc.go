package iso

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"bint/internal/task"
)

// Vòng đời temp (docs/iso-plan.md mục 5.3): phiên cuối thoát thì xoá thư mục TMPDIR của profile; còn daemon
// Codex thì chỉ xoá mục con cũ hơn tmp_ttl vì daemon không tự tạo lại thư mục (S3).

// sandboxTempPrefix là tiền tố mục con do sandbox của Codex tạo trong TMPDIR (vd
// codex-bwrap-synthetic-mount-targets-1000). Daemon còn sống thì sandbox còn cần chúng cho lệnh kế tiếp, nên
// xoá theo tuổi luôn bỏ qua (review GĐ0 .3).
const sandboxTempPrefix = "codex-"

// daemonPidPath là tệp pid của daemon app-server Codex trong profile.
func (p Profile) daemonPidPath() string {
	return filepath.Join(p.Home, p.Tool.spec().ConfigDir, "app-server-daemon", "daemon.pid")
}

// DaemonAlive cho biết profile còn daemon app-server Codex đang sống.
//
// Đo trên Codex 0.167.0/0.169.0 (docs/iso-plan.md mục 11.3): daemon rảnh KHÔNG giữ khoá nào trên
// daemon.pid.lock hay daemon.lock (không có trong /proc/locks, flock thử được ngay) — hai tệp chỉ bị giữ
// thoáng qua lúc start/stop — nên fslock.Try trên đó không cho biết daemon còn sống. Thay vào đó đọc
// daemon.pid ({pid, processIdentity.startTicks}): tiến trình phải còn, và trên Linux thời điểm khởi động
// (/proc/<pid>/stat) phải khớp startTicks để pid bị dùng lại không bị tính là daemon.
//
// Sai lệch được chọn theo hướng an toàn: không chắc (tệp hỏng, không đọc được, không kiểm được danh tính)
// thì coi là còn sống — hậu quả chỉ là giữ thư mục temp và xoá mục cũ theo tuổi. Claude không có daemon.
func (p Profile) DaemonAlive() bool {
	if p.Tool != Codex {
		return false
	}
	data, err := os.ReadFile(p.daemonPidPath())
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	var d struct {
		PID             int `json:"pid"`
		ProcessIdentity struct {
			StartTicks uint64 `json:"startTicks"`
		} `json:"processIdentity"`
	}
	if json.Unmarshal(data, &d) != nil || d.PID <= 0 {
		return true
	}
	if !task.ProcessAlive(d.PID) {
		return false
	}
	if d.ProcessIdentity.StartTicks != 0 {
		if ticks, ok := procStartTicks(d.PID); ok && ticks != d.ProcessIdentity.StartTicks {
			return false
		}
	}
	return true
}

// procStartTicks đọc trường starttime (22) của /proc/<pid>/stat. Chỉ Linux có; nơi khác ok=false.
func procStartTicks(pid int) (uint64, bool) {
	if runtime.GOOS != "linux" {
		return 0, false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	return parseStatStartTicks(string(data))
}

// parseStatStartTicks tách starttime khỏi dòng /proc/<pid>/stat. Tên lệnh (trường 2) nằm trong ngoặc và có
// thể chứa khoảng trắng hay ngoặc, nên cắt sau dấu ')' cuối cùng; phần còn lại bắt đầu từ trường 3.
func parseStatStartTicks(stat string) (uint64, bool) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(stat[i+1:])
	if len(f) < 20 {
		return 0, false
	}
	v, err := strconv.ParseUint(f[19], 10, 64)
	return v, err == nil
}

// Các loại việc của dọn temp.
const (
	TempRemoveAll = "remove" // xoá cả thư mục temp
	TempPrune     = "prune"  // chỉ xoá mục con cũ hơn tmp_ttl (còn daemon)
	TempOrphan    = "orphan" // thư mục temp của profile không còn trong store
	TempBusy      = "busy"   // bỏ qua: phiên đang giữ khoá S
)

// TempAction là một việc dọn temp đã làm (hoặc sẽ làm khi DryRun).
type TempAction struct {
	Profile string // <tool>/<name>
	Path    string
	Kind    string
	Items   []string // với TempPrune: các mục con bị chọn
	Removed bool     // đã thực sự xoá (luôn false khi DryRun hoặc Busy)
}

// TempOptions là tuỳ chọn của CollectTemp.
type TempOptions struct {
	DryRun bool
	// Only (dạng <tool>/<name>) giới hạn vào một profile và bỏ qua thư mục mồ côi.
	Only string
	// Env là env dùng để tìm temp thật của máy (mặc định os.Environ()); Run truyền env của phiên để GC tự
	// động nhìn đúng thư mục temp khi spec.Env khác env tiến trình.
	Env []string
}

// finishSession chạy sau khi phiên thoát và đã nhả khoá S: thử khoá X không chờ; không được tức là còn phiên
// khác dùng temp — để nguyên. Được X thì không còn phiên nào: xoá cả thư mục, hoặc chỉ mục cũ khi còn daemon.
func finishSession(p Profile, temp string) error {
	_, err := cleanProfileTemp(p, temp, false)
	return err
}

// cleanProfileTemp dọn temp của một profile dưới khoá X không chờ.
func cleanProfileTemp(p Profile, temp string, dry bool) (TempAction, error) {
	act := TempAction{Profile: p.ID(), Path: temp}
	if _, err := os.Lstat(temp); errors.Is(err, os.ErrNotExist) {
		return act, nil
	}
	release, busy, err := p.Lock(true)
	if err != nil {
		return act, err
	}
	if busy {
		act.Kind = TempBusy
		return act, nil
	}
	defer release()
	if p.DaemonAlive() {
		act.Kind = TempPrune
		items, err := pruneOld(temp, p.Settings.TmpTTL, dry)
		act.Items = items
		act.Removed = !dry && len(items) > 0 && err == nil
		return act, err
	}
	act.Kind = TempRemoveAll
	if dry {
		return act, nil
	}
	if err := removeTree(temp); err != nil {
		return act, err
	}
	act.Removed = true
	return act, nil
}

// pruneOld xoá mục con trực tiếp của dir có thời điểm sửa cũ hơn ttl, bỏ qua mục sandbox của Codex
// (sandboxTempPrefix). ttl 0 là không xoá theo tuổi. Không đi theo liên kết. dry chỉ liệt kê.
func pruneOld(dir string, ttl time.Duration, dry bool) ([]string, error) {
	if ttl <= 0 {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-ttl)
	var picked []string
	var errs []error
	for _, de := range ents {
		if strings.HasPrefix(de.Name(), sandboxTempPrefix) {
			continue
		}
		path := filepath.Join(dir, de.Name())
		fi, err := os.Lstat(path)
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		picked = append(picked, de.Name())
		if dry {
			continue
		}
		if err := removeTree(path); err != nil {
			errs = append(errs, err)
		}
	}
	return picked, errors.Join(errs...)
}

// removeTree xoá path mà không đi theo liên kết: liên kết (symlink/junction) gặp trên đường chỉ bị gỡ chính
// nó, không đụng dữ liệu ở đích. os.RemoveAll đã không theo symlink trên Unix, nhưng junction của Windows
// (Go ≥ 1.23 không còn đặt ModeSymlink) thì có thể bị đi vào, nên tự duyệt.
func removeTree(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	isl, err := isLink(path)
	if err != nil {
		return err
	}
	if isl {
		return removeLink(path)
	}
	if !fi.IsDir() {
		return removeFile(path, fi)
	}
	// Cây chỉ đọc (cây module Go giải nén: thư mục 0555, tệp 0444) không xoá được mục con nếu thư mục cha thiếu
	// quyền ghi/duyệt: cấp lại cho chủ trước. Chỉ chạm thư mục thật vừa Lstat (liên kết đã xử lý ở trên),
	// không bao giờ chạm đích của liên kết.
	if fi.Mode().Perm()&0o700 != 0o700 {
		if err := os.Chmod(path, fi.Mode().Perm()|0o700); err != nil {
			return err
		}
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	var errs []error
	for _, de := range ents {
		if err := removeTree(filepath.Join(path, de.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	return os.Remove(path)
}

// removeFile xoá tệp (không phải thư mục, không phải liên kết): Windows không xoá được tệp có thuộc tính
// read-only, nên cấp lại quyền ghi cho chủ khi thiếu (Unix không cần nhưng vô hại).
func removeFile(path string, fi os.FileInfo) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if fi.Mode().Perm()&0o200 == 0 {
		if cerr := os.Chmod(path, fi.Mode().Perm()|0o200); cerr == nil {
			return os.Remove(path)
		}
	}
	return err
}

// CollectTemp dọn temp của mọi profile (hoặc một profile theo opts.Only): profile đang có phiên (khoá S) bị
// bỏ qua; không còn phiên thì xoá cả thư mục, hoặc chỉ mục cũ khi còn daemon. Thư mục temp mà profile không
// còn trong store (và cũ hơn tmp_ttl, vì thư mục cha bint-iso-<uid> dùng chung cho mọi store của người dùng)
// bị xoá như thư mục mồ côi. DryRun chỉ liệt kê. Lỗi từng mục được gộp, không dừng giữa chừng.
func CollectTemp(s *Store, o TempOptions) ([]TempAction, error) {
	profiles, lerr := s.List()
	var (
		out  []TempAction
		errs []error
		seen = map[string]bool{}
	)
	if lerr != nil {
		errs = append(errs, lerr)
	}
	for _, p := range profiles {
		seen[dirName(p.Tool, p.Name)] = true
		if o.Only != "" && p.ID() != o.Only {
			continue
		}
		act, err := cleanProfileTemp(p, p.tempDirFrom(o.envOrProcess()), o.DryRun)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.ID(), err))
		}
		if act.Kind != "" {
			out = append(out, act)
		}
	}
	if o.Only != "" {
		return out, errors.Join(errs...)
	}
	// Mồ côi: tên thư mục có dạng <tool>--<name> hợp lệ mà store này không có profile đó. Chỉ quét thư mục
	// của chính store này (<bint-iso-uid>/<StoreKey>); thư mục của store khác không thuộc quyền của nó.
	// Thêm bố cục cũ (<bint-iso-uid>/<tool>--<name>, trước khi temp gắn với store): đường chuyển tiếp, chỉ
	// xoá khi cũ hơn tmp_ttl.
	env := o.envOrProcess()
	dir := storeTempDir(s, env)
	// Bố cục cũ không phân biệt store, nên mục của profile có trong store này cũng được dọn khi đã cũ.
	for _, sc := range []struct {
		dir  string
		seen map[string]bool
	}{{dir, seen}, {filepath.Dir(dir), nil}} {
		acts, err := sweepOrphans(sc.dir, sc.seen, s.Settings.TmpTTL, o.DryRun)
		out = append(out, acts...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	slices.SortFunc(out, func(a, b TempAction) int { return strings.Compare(a.Path, b.Path) })
	return out, errors.Join(errs...)
}

// sweepOrphans xoá mục con <tool>--<name> của dir mà profile không có trong seen và cũ hơn ttl.
func sweepOrphans(dir string, seen map[string]bool, ttl time.Duration, dry bool) ([]TempAction, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var (
		out  []TempAction
		errs []error
	)
	cutoff := time.Now().Add(-ttl)
	for _, de := range ents {
		t, name, perr := parseDirName(de.Name())
		if perr != nil || seen[de.Name()] || ttl <= 0 {
			continue
		}
		path := filepath.Join(dir, de.Name())
		fi, err := os.Lstat(path)
		if err != nil || !fi.IsDir() || !fi.ModTime().Before(cutoff) {
			continue
		}
		act := TempAction{Profile: string(t) + "/" + name, Path: path, Kind: TempOrphan}
		if !dry {
			if err := removeTree(path); err != nil {
				errs = append(errs, err)
			} else {
				act.Removed = true
			}
		}
		out = append(out, act)
	}
	return out, errors.Join(errs...)
}

// storeTempDir là thư mục chứa temp của mọi profile thuộc store s (theo temp thật của máy).
func storeTempDir(s *Store, env []string) string {
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	return Profile{StoreRoot: s.Root}.storeTempDirIn(tempBase(e))
}

func (o TempOptions) envOrProcess() []string {
	if o.Env == nil {
		return os.Environ()
	}
	return o.Env
}
