package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/ulid"
)

// Hợp đồng thuê khoá theo thời gian (docs/isolation-subagent-plan.md mục 6.3).
//
// flock nhả khi tiến trình `bint run next` thoát, nên một bước chạy bằng subagent (driver session) không giữ
// được khoá hệ điều hành suốt bước. Thay vào đó `next` ghi một hợp đồng {tác vụ, bước, lượt, chế độ, hạn} vào
// tệp cạnh tệp khoá của worktree rồi trả flock; mọi người lấy khoá (AcquireLock, kể cả `bint run` kiểu tiến
// trình) đều tôn trọng hợp đồng còn hạn của tác vụ KHÁC theo cùng bảng chia sẻ/độc quyền như khoá thật.
//
// Hết hạn xác định theo THỜI GIAN, không theo PID còn sống: không có bước "thu hồi dựa trên quan sát chủ đã chết"
// nên không có kiểu race R1 của evolve.AcquireLock (đọc thấy chết rồi gỡ nhầm khoá của chủ mới). Phiên
// orchestrator chết giữa bước ⇒ khoá bị giữ tới hết hạn hoặc tới khi người chạy `bint run abort`.
//
// Bố cục: <tệp khoá>.lease.json (danh sách hợp đồng), đọc-sửa-ghi dưới mutex là flock độc quyền trên
// <tệp khoá>.lease.lk (đoạn găng ngắn, dùng lại tryFlock). Tệp hợp đồng ghi nguyên tử (tệp tạm + đổi tên).

// leaseNow là đồng hồ của hợp đồng thuê; biến để kiểm thử đẩy thời gian qua hạn.
var leaseNow = time.Now

// leaseEntry là một hợp đồng thuê.
type leaseEntry struct {
	Task  string `json:"task"`
	Step  string `json:"step"`
	RunID string `json:"run_id"`
	// Mode: "shared" | "exclusive".
	Mode  string `json:"mode"`
	At    string `json:"at"`
	Until string `json:"until"` // RFC 3339 UTC
}

func leaseModeName(m LockMode) string {
	if m == LockShared {
		return "shared"
	}
	return "exclusive"
}

func (e leaseEntry) lockMode() LockMode {
	if e.Mode == "shared" {
		return LockShared
	}
	return LockExclusive
}

// active: còn hạn tại thời điểm now. Hạn không đọc được thì coi là còn hạn: tệp hỏng không được mở khoá nhầm
// (người gỡ bằng `bint run abort`).
func (e leaseEntry) active(now time.Time) bool {
	until, err := time.Parse(time.RFC3339, e.Until)
	return err != nil || now.Before(until)
}

func (e leaseEntry) describe() string {
	until := e.Until
	if t, err := time.Parse(time.RFC3339, e.Until); err == nil {
		until = t.Local().Format("15:04")
	}
	return fmt.Sprintf("%s %s (subagent, khoá %s, hết hạn %s)", e.Task, e.Step, e.lockMode(), until)
}

func leaseFilePath(lockFile string) string  { return lockFile + ".lease.json" }
func leaseMutexPath(lockFile string) string { return lockFile + ".lease.lk" }

// leaseTaskOf lấy ID tác vụ từ owner "<id> <bước>" của Runner; owner khác dạng (ví dụ "dọn worktree") trả "".
func leaseTaskOf(owner string) string {
	id, _, ok := strings.Cut(owner, " ")
	if ok && ulid.Valid(id) {
		return id
	}
	return ""
}

// withLeaseMutex chạy fn dưới mutex của tệp hợp đồng. Đoạn găng chỉ đọc-sửa-ghi một tệp nhỏ nên thăm dò ngắn
// (tối đa vài giây); không lấy được thì trả lỗi thay vì chạy không có mutex.
func withLeaseMutex(lockFile string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(leaseMutexPath(lockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close() // đóng ⇒ hệ điều hành nhả khoá
	deadline := time.Now().Add(5 * time.Second)
	for {
		busy, err := tryFlock(f, LockExclusive)
		if err != nil {
			return err
		}
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("không lấy được mutex của hợp đồng thuê khoá (có tiến trình khác đang ghi)")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fn()
}

// readLeases đọc hợp đồng (gọi dưới mutex khi sắp ghi). Tệp thiếu hoặc hỏng ⇒ không có hợp đồng.
func readLeases(lockFile string) []leaseEntry {
	b, err := os.ReadFile(leaseFilePath(lockFile))
	if err != nil {
		return nil
	}
	var f struct {
		Leases []leaseEntry `json:"leases"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return f.Leases
}

// writeLeases ghi nguyên tử; rỗng thì xoá tệp.
func writeLeases(lockFile string, es []leaseEntry) error {
	p := leaseFilePath(lockFile)
	if len(es) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(struct {
		Leases []leaseEntry `json:"leases"`
	}{es}, "", "  ")
	if err != nil {
		return err
	}
	return fsafe.WriteAtomic(p, b, 0o600)
}

// activeLeases là hợp đồng còn hạn tại now (đọc dưới mutex).
func activeLeases(lockFile string, now time.Time) []leaseEntry {
	// Không có tệp hợp đồng (trường hợp thường gặp): khỏi tạo mutex, nên đường chỉ đọc (TUI, ActiveSteps) không ghi gì.
	if _, err := os.Stat(leaseFilePath(lockFile)); err != nil {
		return nil
	}
	var out []leaseEntry
	_ = withLeaseMutex(lockFile, func() error {
		for _, e := range readLeases(lockFile) {
			if e.active(now) {
				out = append(out, e)
			}
		}
		return nil
	})
	return out
}

// leaseConflicts trả các hợp đồng còn hạn của tác vụ KHÁC xung đột với khoá chế độ mode do owner xin: bảng giống
// khoá thật (chia sẻ + chia sẻ cùng được; độc quyền loại trừ mọi thứ). Hợp đồng của chính tác vụ không xung đột.
func leaseConflicts(lockFile, owner string, mode LockMode) []leaseEntry {
	self := leaseTaskOf(owner)
	var out []leaseEntry
	for _, e := range activeLeases(lockFile, leaseNow()) {
		if self != "" && e.Task == self {
			continue
		}
		if mode == LockExclusive || e.lockMode() == LockExclusive {
			out = append(out, e)
		}
	}
	return out
}

// leaseHolders mô tả các hợp đồng còn hạn (để báo "chờ khoá do …").
func leaseHolders(es []leaseEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.describe())
	}
	sort.Strings(out)
	return out
}

// grantLease ghi hợp đồng thuê cho một bước chạy bằng subagent. Phải gọi khi đang giữ flock của bước (cùng chế
// độ): flock loại trừ các người lấy khoá kiểu tiến trình trong lúc kiểm xung đột và ghi, còn mutex hợp đồng loại
// trừ hai `next` chia sẻ ghi đồng thời. Xung đột với hợp đồng còn hạn của tác vụ khác thì trả lỗi bọc ErrLocked.
// Hợp đồng cũ của cùng tác vụ bị thay.
func grantLease(tasksDir, root string, e leaseEntry) error {
	lockFile := lockPath(tasksDir, root)
	return withLeaseMutex(lockFile, func() error {
		now := leaseNow()
		var keep []leaseEntry
		var conflicts []leaseEntry
		for _, old := range readLeases(lockFile) {
			switch {
			case !old.active(now):
				// hết hạn: dọn khi ghi
			case old.Task == e.Task:
				// thay hợp đồng cũ của chính tác vụ
			default:
				keep = append(keep, old)
				if e.lockMode() == LockExclusive || old.lockMode() == LockExclusive {
					conflicts = append(conflicts, old)
				}
			}
		}
		if len(conflicts) > 0 {
			return &lockedError{mode: e.lockMode(), holders: leaseHolders(conflicts)}
		}
		return writeLeases(lockFile, append(keep, e))
	})
}

// releaseLease xoá hợp đồng của tác vụ (theo lượt nếu runID khác rỗng) và dọn hợp đồng hết hạn.
func releaseLease(tasksDir, root, taskID, runID string) error {
	lockFile := lockPath(tasksDir, root)
	return withLeaseMutex(lockFile, func() error {
		now := leaseNow()
		var keep []leaseEntry
		for _, e := range readLeases(lockFile) {
			if e.Task == taskID && (runID == "" || e.RunID == runID) {
				continue
			}
			if e.active(now) {
				keep = append(keep, e)
			}
		}
		return writeLeases(lockFile, keep)
	})
}

// removeLeaseFiles xoá tệp hợp đồng và mutex của worktree (khi land/cancel dọn tệp khoá).
func removeLeaseFiles(lockFile string) {
	os.Remove(leaseFilePath(lockFile))
	os.Remove(leaseMutexPath(lockFile))
}
