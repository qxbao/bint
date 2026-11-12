package workflow

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"bint/internal/state"
)

// Nhật ký ghi của Bint (docs/isolation-subagent-plan.md 4.5, H8).
//
// Khi người sửa cấu hình, duyệt tri thức... qua TUI/CLI trong lúc một bước chỉ đọc đang chạy, tệp đổi
// trông y hệt việc agent sửa cây làm việc. Tầng dịch vụ (internal/app) nối một dòng vào
// <runtime>/writes.jsonl cho mỗi tệp repo nó ghi; ReadonlyViolations đối chiếu để không tính các thay
// đổi đó là vi phạm. Tệp nằm trong thư mục trạng thái mà bước chỉ đọc được ghi nên agent có thể chèn dòng
// giả: chấp nhận (mục đích là chặn nhầm, không chống agent cố tình), kết quả bước liệt kê tệp được miễn.
// Hook không ghi vào nhật ký này.

// WriteRecord là một dòng nhật ký ghi.
type WriteRecord struct {
	// Path là đường dẫn tuyệt đối đã chuẩn hoá (NormalizeWritePath).
	Path string `json:"path"`
	// SHA256 là băm nội dung tệp sau khi ghi; rỗng nếu tệp đã bị xoá.
	SHA256 string `json:"sha256"`
	// Prev là băm nội dung tệp NGAY TRƯỚC khi Bint ghi ("" = tệp chưa tồn tại); nil = không biết (dòng cũ không
	// có khoá prev_sha256, hoặc đường ghi không có trạng thái trước). Con trỏ để phân biệt hai trường hợp đó.
	// ReadonlyViolations chỉ miễn khi chuỗi Prev→SHA256 của đường dẫn nối liền từ ảnh trước: ghi đọc-sửa-ghi
	// của người không hợp thức hoá sửa đổi mà agent làm trước đó trên cùng tệp.
	Prev *string   `json:"prev_sha256,omitempty"`
	At   time.Time `json:"at"`
	// Actor là tên thao tác ("bint config set"...), chỉ để người đọc.
	Actor string `json:"actor,omitempty"`
	// Agent: thao tác chạy trong phiên agent (app.InAgentSession).
	Agent bool `json:"agent"`
}

const (
	writesLogName  = "writes.jsonl"
	writesLockWait = 2 * time.Second
)

// WritesLogPath trả về đường dẫn nhật ký ghi trong thư mục runtime.
func WritesLogPath(runtimeDir string) string { return filepath.Join(runtimeDir, writesLogName) }

// NormalizeWritePath: xem state.NormalizeWritePath (hookcore dùng chung, không import workflow).
func NormalizeWritePath(p string) string { return state.NormalizeWritePath(p) }

// HashBytes trả về sha256 (hex) của data, cùng dạng với HashFile.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile trả về sha256 (hex) nội dung tệp; chuỗi rỗng nếu tệp không tồn tại hoặc không đọc được.
func HashFile(abs string) string {
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AppendWrites nối recs vào nhật ký: mỗi bản ghi một lệnh Write một dòng trên tệp mở O_APPEND. Trước khi
// nối, nếu có dòng cũ hơn now-retain thì viết lại tệp chỉ giữ dòng còn hạn; việc dọn chỉ làm khi lấy được
// khoá writes.jsonl.lock (chờ tối đa 2 giây) — không lấy được thì chỉ nối. retain <= 0: không dọn.
func AppendWrites(logPath string, recs []WriteRecord, retain time.Duration, now time.Time) error {
	if len(recs) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	lines := make([][]byte, 0, len(recs))
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		lines = append(lines, append(b, '\n'))
	}
	if lk := lockWrites(logPath); lk != nil {
		defer lk.Close()
		if retain > 0 {
			compactWrites(logPath, now.Add(-retain)) // lỗi: bỏ dọn lần này
		}
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.Write(l); err != nil {
			return err
		}
	}
	return nil
}

// lockWrites lấy khoá độc quyền của nhật ký (thăm dò tối đa writesLockWait); nil nếu không lấy được.
// Khoá chỉ để loại trừ hai lần dọn đồng thời; người nối không có khoá vẫn nối được.
func lockWrites(logPath string) *os.File {
	f, err := os.OpenFile(logPath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil
	}
	deadline := time.Now().Add(writesLockWait)
	for {
		busy, err := tryFlock(f, LockExclusive)
		if err != nil {
			f.Close()
			return nil
		}
		if !busy {
			return f
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// compactWrites viết lại nhật ký, bỏ dòng hỏng và dòng có At < cutoff. Không làm gì nếu không có dòng nào
// cần bỏ. Rename lỗi (Windows: tệp đang mở) thì giữ nguyên tệp cũ.
func compactWrites(logPath string, cutoff time.Time) {
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return
	}
	var keep bytes.Buffer
	dropped := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r WriteRecord
		if json.Unmarshal(line, &r) != nil || r.At.Before(cutoff) {
			dropped = true
			continue
		}
		keep.Write(line)
		keep.WriteByte('\n')
	}
	if !dropped {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(logPath), writesLogName+".tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	_, werr := tmp.Write(keep.Bytes())
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(name, logPath) != nil {
		os.Remove(name)
	}
}

// ReadWrites đọc các dòng nhật ký có At >= since theo thứ tự trong tệp (thứ tự nối). Dòng hỏng bị bỏ qua;
// tệp không có thì trả rỗng.
func ReadWrites(logPath string, since time.Time) ([]WriteRecord, error) {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []WriteRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var r WriteRecord
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Path == "" || r.At.Before(since) {
			continue
		}
		out = append(out, r)
	}
	return out, sc.Err()
}
