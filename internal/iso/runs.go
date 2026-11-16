package iso

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/task"
)

// Hồ sơ harness đang chạy (docs/iso-plan.md mục 5.3): khoá S nằm trên fd của bint, nên bint chết (kill -9) thì
// khoá mất dù harness con còn chạy — gc/remove/phiên khác sẽ xoá TMPDIR và HOME giả dưới chân nó. Để chặn
// việc đó, mỗi harness đang chạy có một hồ sơ <profile>/runs/<pid>.json; Lock(true) coi profile là bận khi còn
// hồ sơ của tiến trình đang sống.
//
// Hợp với quy tắc khoá liên tiến trình (k:01M7FGC944KVT6NY1TZE7MJ0P8): quyết định khoá vẫn hoàn toàn do
// fslock. Hồ sơ PID chỉ có thể THÊM trạng thái bận, không bao giờ thu hồi hay xoá khoá của ai. Hồ sơ chỉ bị
// xoá khi đang giữ X, mà Track cần S, nên lúc đó không có phiên nào đang ghi hồ sơ (không có race R1). Sai
// lệch theo hướng an toàn: PID bị dùng lại thì profile chỉ bận lâu hơn.

// runRecord là nội dung runs/<pid>.json.
type runRecord struct {
	PID        int       `json:"pid"`
	StartTicks uint64    `json:"start_ticks,omitempty"` // Linux: starttime của /proc/<pid>/stat; nơi khác 0
	BintPID    int       `json:"bint_pid"`
	Started    time.Time `json:"started"`
}

// RunsDir là thư mục hồ sơ harness đang chạy của profile.
func (p Profile) RunsDir() string { return filepath.Join(p.Dir, "runs") }

func (p Profile) runPath(pid int) string {
	return filepath.Join(p.RunsDir(), strconv.Itoa(pid)+".json")
}

// Track ghi hồ sơ cho harness pid đang chạy trong phiên (phiên còn giữ S). Gọi ngay sau khi spawn; End xoá hồ
// sơ trước khi nhả S. Gọi nhiều lần với nhiều pid cũng được.
func (s *Session) Track(pid int) error {
	if pid <= 0 {
		return errors.New("pid không hợp lệ")
	}
	p := s.Profile
	if err := os.MkdirAll(p.RunsDir(), 0o700); err != nil {
		return err
	}
	rec := runRecord{PID: pid, BintPID: os.Getpid(), Started: time.Now().UTC()}
	if ticks, ok := procStartTicks(pid); ok {
		rec.StartTicks = ticks
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := fsafe.WriteAtomic(p.runPath(pid), data, 0o600); err != nil {
		return err
	}
	s.tracked = append(s.tracked, pid)
	return nil
}

// untrack xoá hồ sơ do phiên này ghi.
func (s *Session) untrack() {
	for _, pid := range s.tracked {
		_ = os.Remove(s.Profile.runPath(pid))
	}
	s.tracked = nil
}

// liveRuns trả hồ sơ của harness còn sống và xoá hồ sơ của tiến trình đã chết hoặc hỏng không giải mã được.
// Chỉ gọi khi đang giữ X.
func (p Profile) liveRuns() []string {
	ents, err := os.ReadDir(p.RunsDir())
	if err != nil {
		return nil
	}
	var live []string
	for _, de := range ents {
		path := filepath.Join(p.RunsDir(), de.Name())
		if !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		if runAlive(path) {
			live = append(live, path)
			continue
		}
		_ = os.Remove(path)
	}
	return live
}

// runAlive: hồ sơ ở path còn tiến trình sống. Không kiểm được danh tính (không phải Linux) thì coi là sống;
// hồ sơ hỏng hoặc tiến trình đã chết thì không.
func runAlive(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist) // không đọc được: giữ an toàn; mất rồi: không còn gì
	}
	var rec runRecord
	if json.Unmarshal(data, &rec) != nil || rec.PID <= 0 {
		return false
	}
	if !task.ProcessAlive(rec.PID) || procZombie(rec.PID) {
		return false
	}
	if rec.StartTicks != 0 {
		if ticks, ok := procStartTicks(rec.PID); ok && ticks != rec.StartTicks {
			return false
		}
	}
	return true
}

// procZombie: tiến trình đã thoát nhưng chưa được cha gặt (trạng thái Z trong /proc/<pid>/stat, chỉ Linux);
// kill(pid, 0) vẫn báo "sống" với nó, trong khi nó không còn dùng TMPDIR hay HOME.
func procZombie(pid int) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return false
	}
	f := strings.Fields(s[i+1:])
	return len(f) > 0 && f[0] == "Z"
}
