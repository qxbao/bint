package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/knowledge"
	"bint/internal/state"
	"bint/internal/task"
)

// Cô lập các bước chạy trên cùng một worktree (mục 15.2, P5; docs/isolation-subagent-plan.md 4.2):
//
//   - Khoá theo worktree có hai chế độ: chia sẻ (bước chỉ đọc b-req, b-review) và độc quyền
//     (b-feat và kiểm chứng sau b-feat: lệnh kiểm chứng hay ghi tệp). Nhiều bước chia sẻ cùng chạy được; bước độc quyền
//     loại trừ mọi bước khác. Bước phải chờ tới khi khoá được nhả (WaitLock).
//   - Bước chỉ đọc (b-req, b-review) được kiểm tra lại ở tầng chương trình: ảnh chụp cây
//     làm việc trước và sau bước phải trùng nhau, bất kể harness áp chỉ đọc mạnh tới đâu.
//     Giữ khoá chia sẻ suốt bước nên b-feat của tác vụ khác không chen vào giữa hai ảnh chụp.

// ErrLocked: worktree đang bị bước khác giữ khoá không tương thích với chế độ xin.
var ErrLocked = errors.New("worktree đang có bước khác giữ khoá")

// ErrWriteLocked là tên cũ của ErrLocked (khoá ghi độc quyền của worktree).
var ErrWriteLocked = ErrLocked

// LockMode là chế độ khoá.
type LockMode int

const (
	// LockShared: nhiều chủ cùng giữ; chặn LockExclusive.
	LockShared LockMode = iota + 1
	// LockExclusive: một chủ duy nhất, không cùng LockShared.
	LockExclusive
)

func (m LockMode) String() string {
	if m == LockShared {
		return "chỉ đọc"
	}
	return "có quyền ghi"
}

// Lock là khoá của một worktree.
type Lock struct {
	path       string
	holderFile string   // tệp thông tin chủ (chỉ để thông báo)
	f          *os.File // tệp đang giữ khoá hệ điều hành; nil nếu không giữ
	mode       LockMode
}

// WriteLock là tên cũ của Lock.
type WriteLock = Lock

// lockDir là thư mục chứa khoá trong thư mục trạng thái.
func lockDir(tasksDir string) string { return filepath.Join(filepath.Dir(tasksDir), "locks") }

func lockPath(tasksDir, root string) string {
	h := sha256.Sum256([]byte(state.NormalizePath(root)))
	return filepath.Join(lockDir(tasksDir), "write-"+hex.EncodeToString(h[:])[:16]+".lock")
}

// holderInfo là nội dung tệp chủ khoá. Không dùng để quyết định ai giữ khoá (đó là việc
// của hệ điều hành); chỉ để báo "đang chờ ai".
type holderInfo struct {
	PID   int    `json:"pid"`
	At    string `json:"at"`
	Mode  string `json:"mode"`
	Owner string `json:"owner"`
	Root  string `json:"root"`
}

// AcquireLock xin khoá của worktree root, không chờ.
//
// Giao thức: tính loại trừ do khoá của hệ điều hành (flock trên Unix, LockFileEx trên
// Windows) trên tệp khoá bảo đảm. Khoá của tiến trình đã chết tự được hệ điều hành nhả
// nên không có bước "thu hồi dựa trên quan sát stale" — thứ từng có thể tạm gỡ khoá của
// chủ đang sống (R1). Tệp khoá không bao giờ bị xoá khi trả: xoá tệp khi người đọc khác
// còn giữ sẽ cho người đến sau mở tệp mới và lấy được khoá độc quyền song song. Thông tin
// chủ nằm ở tệp riêng của từng chủ trong <tệp khoá>.d/ nên nhiều chủ chia sẻ không ghi đè nhau.
func AcquireLock(tasksDir, root, owner string, mode LockMode) (*Lock, error) {
	if mode != LockShared && mode != LockExclusive {
		return nil, fmt.Errorf("chế độ khoá không hợp lệ: %d", mode)
	}
	dir := lockDir(tasksDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := lockPath(tasksDir, root)
	for attempt := 0; attempt < 8; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		busy, err := tryFlock(f, mode)
		if err != nil {
			f.Close()
			return nil, err
		}
		if busy {
			f.Close()
			return nil, &lockedError{mode: mode, holders: describeHolders(p)}
		}
		if !sameFile(f, p) {
			f.Close() // bản cũ vừa xoá tệp khi trả khoá; mở lại
			continue
		}
		// Hợp đồng thuê còn hạn của tác vụ khác (bước chạy bằng subagent, lease.go) cũng là chủ khoá: flock không
		// giữ suốt bước vì tiến trình `bint run next` đã thoát.
		cs, lerr := leaseConflicts(p, owner, mode)
		if lerr != nil {
			// Không biết có hợp đồng còn hạn hay không (mutex quá hạn, tệp hỏng): coi là đang bị giữ, không cấp khoá.
			f.Close()
			return nil, &lockedError{mode: mode, holders: []string{"hợp đồng thuê khoá không đọc được: " + lerr.Error()}, cause: lerr}
		}
		if len(cs) > 0 {
			f.Close()
			return nil, &lockedError{mode: mode, holders: leaseHolders(cs)}
		}
		l := &Lock{path: p, f: f, mode: mode}
		l.writeHolder(root, owner) // chỉ để thông báo: lỗi ghi không ảnh hưởng khoá
		return l, nil
	}
	return nil, &lockedError{mode: mode}
}

// lockedError là lỗi khoá đang bận; errors.Is(err, ErrLocked) đúng.
type lockedError struct {
	mode    LockMode
	holders []string
	// cause là lỗi gốc khi khoá bị coi là đang giữ vì không đọc được hợp đồng thuê (errLeaseUnreadable); nil với chủ khoá thật.
	cause error
}

func (e *lockedError) Error() string {
	msg := ErrLocked.Error()
	if len(e.holders) > 0 {
		msg += " (" + strings.Join(e.holders, "; ") + ")"
	}
	if e.mode == LockExclusive {
		return msg + "; chờ bước đó xong hoặc dùng worktree khác"
	}
	return msg + "; chờ bước có quyền ghi đó xong"
}

func (e *lockedError) Is(target error) bool { return target == ErrLocked }

func (e *lockedError) Unwrap() error { return e.cause }

// WaitOptions điều khiển WaitLock.
type WaitOptions struct {
	// Timeout là thời gian chờ tối đa; <= 0 là chờ vô hạn (tới khi ctx huỷ).
	Timeout time.Duration
	// OnWait được gọi khi bắt đầu chờ và mỗi khi mô tả chủ khoá đổi (nil = bỏ qua).
	OnWait func(holders string)
}

// Khoảng thăm dò khi chờ khoá; biến để kiểm thử rút ngắn.
var (
	lockPollMin = 50 * time.Millisecond
	lockPollMax = 2 * time.Second
)

// WaitLock xin khoá, chờ tới khi lấy được. ctx huỷ thì trả ctx.Err(); hết Timeout thì trả
// lỗi bọc ErrLocked. Thăm dò giãn cách thay vì chặn trong flock để huỷ được ngay bằng ctx.
func WaitLock(ctx context.Context, tasksDir, root, owner string, mode LockMode, o WaitOptions) (*Lock, error) {
	var deadline time.Time
	if o.Timeout > 0 {
		deadline = time.Now().Add(o.Timeout)
	}
	delay := lockPollMin
	lastDesc, waited := "", false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l, err := AcquireLock(tasksDir, root, owner, mode)
		if err == nil {
			return l, nil
		}
		var le *lockedError
		if !errors.As(err, &le) {
			return nil, err
		}
		desc := strings.Join(le.holders, "; ")
		// Chủ vừa trả khoá (tệp chủ đã xoá, khoá chưa nhả hẳn) thì không báo lại "chủ rỗng".
		if !waited || (desc != lastDesc && desc != "") {
			waited, lastDesc = true, desc
			if o.OnWait != nil {
				o.OnWait(desc)
			}
		}
		wait := delay
		if !deadline.IsZero() {
			left := time.Until(deadline)
			if left <= 0 {
				return nil, err
			}
			wait = min(wait, left)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		delay = min(delay*2, lockPollMax)
	}
}

// AcquireWriteLock lấy khoá độc quyền (bước có quyền ghi) của worktree root, không chờ.
func AcquireWriteLock(tasksDir, root, owner string) (*Lock, error) {
	return AcquireLock(tasksDir, root, owner, LockExclusive)
}

func (l *Lock) holderDir() string { return l.path + ".d" }

func (l *Lock) writeHolder(root, owner string) {
	dir := l.holderDir()
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return
	}
	info := holderInfo{PID: os.Getpid(), At: time.Now().UTC().Format(time.RFC3339), Mode: l.mode.String(), Owner: owner, Root: root}
	b, err := json.Marshal(info)
	if err != nil {
		return
	}
	name := filepath.Join(dir, fmt.Sprintf("%d-%s.json", os.Getpid(), hex.EncodeToString(nonce[:])))
	if os.WriteFile(name, b, 0o600) == nil {
		l.holderFile = name
	}
}

// describeHolders mô tả các chủ đang giữ khoá, từ tệp chủ (chỉ để báo lỗi). Mục của tiến
// trình đã chết bị ẩn (và dọn): hệ điều hành đã nhả khoá của chúng.
func describeHolders(lockFile string) []string {
	dir := lockFile + ".d"
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var h holderInfo
		if json.Unmarshal(b, &h) != nil || h.PID <= 0 {
			continue
		}
		if !task.ProcessAlive(h.PID) {
			os.Remove(p)
			continue
		}
		out = append(out, fmt.Sprintf("%s giữ khoá %s, tiến trình %d", h.Owner, h.Mode, h.PID))
	}
	sort.Strings(out)
	return out
}

// Release trả khoá. Không xoá tệp khoá (xem AcquireLock).
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	if l.holderFile != "" {
		os.Remove(l.holderFile)
	}
	l.f.Close() // nhả khoá hệ điều hành
	l.f = nil
}

// WorktreeSnapshot trả về mã cây (git tree) đại diện cho toàn bộ cây làm việc: tệp
// đã theo dõi và tệp mới chưa bị .gitignore loại, kể cả .bint/knowledge/: việc tạo
// ứng viên tri thức mới được phân loại riêng bởi ReadonlyViolations, còn sửa/xoá mục
// đã có vẫn bị tính là vi phạm. Dùng chỉ mục tạm nên không đụng tới chỉ mục của người dùng.
func WorktreeSnapshot(ctx context.Context, root string) (string, error) {
	tmp, err := os.CreateTemp("", "bint-index-*")
	if err != nil {
		return "", err
	}
	idx := tmp.Name()
	tmp.Close()
	os.Remove(idx) // git tạo chỉ mục mới
	defer os.Remove(idx)
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+idx)
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
			}
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	if _, err := run("add", "-A", "--", "."); err != nil {
		return "", err
	}
	return run("write-tree")
}

// ReadonlyCheck là kết quả đối chiếu ảnh chụp của bước chỉ đọc.
type ReadonlyCheck struct {
	// Violations: đường dẫn (tương đối với gốc repo) bước chỉ đọc không được phép đổi.
	Violations []string
	// HumanWrites: đường dẫn đổi nhưng được miễn vì chính Bint ghi thay người (nhật ký ghi).
	HumanWrites []string
}

// ReadonlyViolations đối chiếu các đường dẫn đổi giữa hai ảnh chụp với những gì bước chỉ đọc được phép đổi:
//
//   - Tệp tri thức MỚI (chưa có ở ảnh trước) trong thư mục items, hợp lệ và ở trạng thái candidate.
//   - Đường dẫn có chuỗi dòng nhật ký ghi (writesLog, At >= since, theo thứ tự trong tệp) nối liền từ nội dung
//     ở ảnh trước tới nội dung hiện tại: mỗi dòng có agent=false và prev_sha256 bằng băm hiện hành (ban đầu: băm
//     blob của ảnh trước, đọc qua `git cat-file --filters`; tệp chưa có ở ảnh trước: rỗng), rồi băm hiện hành
//     thành sha256 của dòng; dòng ghi trùng trạng thái hiện hành (ghi giữa mốc since và ảnh trước) bỏ qua; cuối
//     chuỗi băm hiện hành phải bằng nội dung hiện tại của tệp (tệp đã xoá: rỗng). Đó là thay đổi do người ghi qua
//     Bint (docs/isolation-subagent-plan.md 4.5). Agent gọi lệnh Bint (agent=true), dòng thiếu prev_sha256, mắt
//     xích đứt (agent đã sửa tệp trước khi người ghi đọc-sửa-ghi), sửa tiếp sau người (băm lệch), hoặc sửa không
//     qua Bint (không có dòng) vẫn là vi phạm.
//
// Sửa/xoá mục tri thức có sẵn, hoặc tệp mới tự gán status khác candidate đều là vi phạm. writesLog rỗng:
// không có miễn trừ nhật ký.
func ReadonlyViolations(ctx context.Context, root, before, after string, since time.Time, writesLog string) ReadonlyCheck {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-status", "--no-renames", "-z", before, after)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		ps, _ := DiffPaths(ctx, root, before, after)
		return ReadonlyCheck{Violations: ps}
	}
	var chains map[string][]WriteRecord
	if writesLog != "" {
		if recs, err := ReadWrites(writesLog, since); err == nil {
			chains = make(map[string][]WriteRecord, len(recs))
			for _, r := range recs {
				chains[r.Path] = append(chains[r.Path], r) // giữ thứ tự trong tệp
			}
		}
	}
	var res ReadonlyCheck
	for _, ch := range parseNameStatusZ(out) {
		st, p := ch.status, ch.path
		if newCandidateItem(root, st, p) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(p))
		if chain := chains[NormalizeWritePath(abs)]; len(chain) > 0 && humanChain(ctx, root, before, st, p, abs, chain) {
			res.HumanWrites = append(res.HumanWrites, p)
			continue
		}
		res.Violations = append(res.Violations, p)
	}
	return res
}

// nameStatus là một dòng của `git diff --name-status --no-renames -z`.
type nameStatus struct{ status, path string }

func parseNameStatusZ(out []byte) []nameStatus {
	var res []nameStatus
	f := strings.Split(string(out), "\x00")
	for i := 0; i+1 < len(f); i += 2 {
		res = append(res, nameStatus{f[i], f[i+1]})
	}
	return res
}

// newCandidateItem: thay đổi (status git st, đường dẫn p) là thêm một mục tri thức MỚI hợp lệ ở trạng thái
// candidate dưới thư mục items. Đây là thay đổi duy nhất bước chỉ đọc được phép (ReadonlyViolations) và cũng là
// thay đổi duy nhất một phiếu chỉ lập kế hoạch ở worktree được mang theo mà không qua cổng done (planGateFor).
func newCandidateItem(root, st, p string) bool {
	return st == "A" && strings.HasPrefix(p, knowledge.ItemsRel+"/") && newCandidateFile(root, p)
}

// nonCandidateChanges liệt kê đường dẫn đổi giữa hai ảnh chụp mà không phải thêm mục tri thức candidate mới.
func nonCandidateChanges(ctx context.Context, root, before, after string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.quotepath=off", "diff", "--name-status", "--no-renames", "-z", before, after)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s %s: %w", before, after, err)
	}
	var res []string
	for _, ch := range parseNameStatusZ(out) {
		if !newCandidateItem(root, ch.status, ch.path) {
			res = append(res, ch.path)
		}
	}
	return res, nil
}

// humanChain: chuỗi dòng nhật ký của một đường dẫn đổi (status git st) có nối liền từ nội dung ở ảnh before tới
// nội dung hiện tại của abs và không có dòng nào của agent hoặc thiếu prev_sha256 (xem ReadonlyViolations).
func humanChain(ctx context.Context, root, before, st, rel, abs string, chain []WriteRecord) bool {
	cur := ""
	if st != "A" { // "A": tệp chưa có ở ảnh trước, băm rỗng
		h, ok := blobWorktreeHash(ctx, root, before, rel)
		if !ok {
			return false
		}
		cur = h
	}
	for _, r := range chain {
		switch {
		case r.Agent || r.Prev == nil:
			return false
		case *r.Prev == cur:
			cur = r.SHA256
		case r.SHA256 == cur:
			// ghi trùng trạng thái hiện hành (ghi xong ngay trước ảnh chụp trước): không đổi gì
		default:
			return false
		}
	}
	return cur == HashFile(abs)
}

// blobWorktreeHash là sha256 nội dung tệp rel ở cây before sau khi áp bộ lọc làm việc (eol/smudge) — tức đúng
// byte mà cây làm việc có lúc chụp. Lỗi (kể cả bộ lọc LFS lỗi) trả ok=false: không có chuỗi ⇒ vi phạm.
func blobWorktreeHash(ctx context.Context, root, before, rel string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--filters", before+":"+rel)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return HashBytes(out), true
}

func newCandidateFile(root, rel string) bool {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	it, err := knowledge.Parse(b)
	return err == nil && it.Status == knowledge.StatusCandidate
}

// stateDirs là thư mục trạng thái Bint mà agent cần ghi (lệnh bint task …, spool).
func (e *Env) stateDirs() []string {
	switch {
	case e.Spool != "":
		return []string{filepath.Dir(e.Spool)}
	case e.Tasks.Dir != "":
		return []string{filepath.Dir(e.Tasks.Dir)}
	}
	return nil
}

// ActiveStep là một bước đang giữ khoá worktree (đang chạy trên cây này).
type ActiveStep struct {
	TaskID string
	Step   string
	// Mode là chế độ khoá đã dịch ("chỉ đọc" / "có quyền ghi"), chỉ để hiển thị.
	Mode string
	PID  int
	At   time.Time
}

// ActiveSteps liệt kê các bước đang chạy trên worktree root, đọc từ tệp chủ khoá (<tệp khoá>.d/*.json). Chỉ
// đọc: khác describeHolders, không xoá mục của tiến trình đã chết mà chỉ bỏ qua nó. Chủ khoá không do Runner
// đặt (owner không có dạng "<tác vụ> <bước>") vẫn được liệt kê với TaskID là toàn bộ owner.
func ActiveSteps(tasksDir, root string) []ActiveStep {
	var out []ActiveStep
	// Bước chạy bằng subagent: không có tiến trình giữ khoá, chỉ có hợp đồng thuê còn hạn (PID 0).
	// Lỗi đọc (tệp hỏng, mutex quá hạn) chỉ làm danh sách hiển thị thiếu; AcquireLock mới là nơi coi lỗi là "đang giữ".
	leases, _ := activeLeases(lockPath(tasksDir, root), leaseNow())
	for _, le := range leases {
		s := ActiveStep{TaskID: le.Task, Step: le.Step, Mode: le.lockMode().String()}
		s.At, _ = time.Parse(time.RFC3339, le.At)
		out = append(out, s)
	}
	ents, err := os.ReadDir(lockPath(tasksDir, root) + ".d")
	if err != nil {
		ents = nil
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(lockPath(tasksDir, root)+".d", e.Name()))
		if err != nil {
			continue
		}
		var h holderInfo
		if json.Unmarshal(b, &h) != nil || h.PID <= 0 || !task.ProcessAlive(h.PID) {
			continue
		}
		s := ActiveStep{PID: h.PID, Mode: h.Mode}
		s.TaskID, s.Step, _ = strings.Cut(h.Owner, " ")
		s.At, _ = time.Parse(time.RFC3339, h.At)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].PID < out[j].PID
	})
	return out
}
