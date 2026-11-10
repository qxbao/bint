// Package fsafe ghi tệp trong repository của người dùng một cách an toàn:
// ghi nguyên tử, sao lưu trước khi sửa, chỉ động tới khối do Bint quản lý, và
// không ghi đè nội dung người dùng đã sửa trong khối đó (trừ khi được ép).
package fsafe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WriteAtomic ghi data vào path qua tệp tạm cùng thư mục rồi đổi tên.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".bint-tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Backup chép tệp hiện có vào backupDir/<thời điểm>/<relName>. Trả "" nếu tệp chưa tồn tại.
func Backup(path, backupDir, relName string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	dst := filepath.Join(backupDir, now.UTC().Format("20060102T150405.000Z"), filepath.FromSlash(relName))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	return dst, os.WriteFile(dst, data, 0o600)
}

// Hash trả về sha256 hex của s.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Manifest lưu băm của nội dung Bint đã ghi lần cuối cho từng khoá
// (ví dụ "AGENTS.md#bint"). Nhờ đó Bint biết người dùng đã sửa khối hay chưa.
type Manifest struct {
	path    string
	mu      sync.Mutex
	Entries map[string]string `json:"entries"`
}

// LoadManifest đọc manifest; tệp chưa tồn tại cho manifest rỗng.
func LoadManifest(path string) (*Manifest, error) {
	m := &Manifest{path: path, Entries: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Entries == nil {
		m.Entries = map[string]string{}
	}
	return m, nil
}

// Get trả về băm đã lưu cho key.
func (m *Manifest) Get(key string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.Entries[key]
	return v, ok
}

// Set ghi nhận băm cho key (chưa lưu xuống đĩa).
func (m *Manifest) Set(key, hash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries[key] = hash
}

// Delete xoá key (chưa lưu xuống đĩa).
func (m *Manifest) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Entries, key)
}

// Save lưu manifest xuống đĩa.
func (m *Manifest) Save() error {
	m.mu.Lock()
	data, err := json.MarshalIndent(m, "", "  ")
	m.mu.Unlock()
	if err != nil {
		return err
	}
	return WriteAtomic(m.path, append(data, '\n'), 0o600)
}

// Markers là cặp dòng mở/đóng khối do Bint quản lý.
type Markers struct{ Begin, End string }

var (
	// HTMLMarkers dùng cho Markdown (AGENTS.md, CLAUDE.md).
	HTMLMarkers = Markers{Begin: "<!-- bint:begin -->", End: "<!-- bint:end -->"}
	// HashMarkers dùng cho tệp chú thích bằng '#' (.gitignore).
	HashMarkers = Markers{Begin: "# bint:begin", End: "# bint:end"}
)

// ErrMalformedBlock: tệp có dấu mở mà không có dấu đóng, hoặc có nhiều khối.
var ErrMalformedBlock = errors.New("khối bint không hợp lệ")

type block struct {
	start, end int // vị trí byte: [start, end) bao gồm cả hai dòng đánh dấu
	inner      string
}

func findBlock(content string, mk Markers) (*block, error) {
	lines := splitLinesKeepEnds(content)
	pos := 0
	var b *block
	innerStart := -1
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		switch t {
		case mk.Begin:
			if b != nil || innerStart >= 0 {
				return nil, fmt.Errorf("%w: có nhiều dấu %q", ErrMalformedBlock, mk.Begin)
			}
			innerStart = pos + len(ln)
			b = &block{start: pos}
		case mk.End:
			if innerStart < 0 {
				return nil, fmt.Errorf("%w: dấu %q đứng trước dấu mở", ErrMalformedBlock, mk.End)
			}
			if b.end != 0 {
				return nil, fmt.Errorf("%w: có nhiều dấu %q", ErrMalformedBlock, mk.End)
			}
			b.inner = content[innerStart:pos]
			b.end = pos + len(ln)
		}
		pos += len(ln)
	}
	if b != nil && b.end == 0 {
		return nil, fmt.Errorf("%w: thiếu dấu %q", ErrMalformedBlock, mk.End)
	}
	return b, nil
}

func splitLinesKeepEnds(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// normalizeInner chuẩn hoá nội dung khối để so sánh: bỏ CR và khoảng trắng đầu/cuối.
func normalizeInner(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

// Outcome là kết quả đồng bộ một tệp.
type Outcome string

const (
	Created   Outcome = "created"   // tạo tệp mới
	Appended  Outcome = "appended"  // thêm khối vào tệp sẵn có
	Updated   Outcome = "updated"   // thay khối do Bint ghi trước đó
	Unchanged Outcome = "unchanged" // không cần thay đổi
	Conflict  Outcome = "conflict"  // khối đã bị người dùng sửa; không ghi
	Removed   Outcome = "removed"   // gỡ tệp do Bint từng xuất bản
)

// BlockSync mô tả yêu cầu đồng bộ một khối trong một tệp.
type BlockSync struct {
	Path      string // đường dẫn tuyệt đối
	RelName   string // tên tương đối dùng cho sao lưu và manifest
	Markers   Markers
	Inner     string // nội dung mong muốn giữa hai dấu (không gồm dấu)
	Force     bool   // ghi đè cả khi người dùng đã sửa khối
	BackupDir string
	Manifest  *Manifest
	Now       time.Time
	Placement Placement
}

// Placement quy định vị trí chèn khối khi tệp chưa có khối.
type Placement int

const (
	AtEnd Placement = iota
	AtStart
)

// Result của SyncBlock.
type Result struct {
	Outcome    Outcome
	BackupPath string
	Detail     string
}

// SyncBlock tạo hoặc cập nhật khối Bint trong tệp, chỉ chạm tới phần giữa hai dấu.
//
// Quy tắc chống ghi đè: khối hiện có chỉ được thay nếu nội dung của nó trùng với
// băm Bint đã lưu ở lần ghi trước (tức người dùng chưa sửa), hoặc khi Force.
func SyncBlock(req BlockSync) (Result, error) {
	key := req.RelName + "#bint"
	want := normalizeInner(req.Inner)
	data, err := os.ReadFile(req.Path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	content := string(data)
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	render := func() string {
		body := strings.ReplaceAll(want, "\n", nl)
		return req.Markers.Begin + nl + body + nl + req.Markers.End + nl
	}

	if !exists {
		if err := WriteAtomic(req.Path, []byte(render()), 0o644); err != nil {
			return Result{}, err
		}
		req.Manifest.Set(key, Hash(want))
		return Result{Outcome: Created}, nil
	}

	b, err := findBlock(content, req.Markers)
	if err != nil {
		return Result{Outcome: Conflict, Detail: err.Error()}, nil
	}

	var next string
	outcome := Updated
	if b == nil {
		outcome = Appended
		switch req.Placement {
		case AtStart:
			next = render() + nl + content
		default:
			sep := ""
			if content != "" && !strings.HasSuffix(content, "\n") {
				sep = nl
			}
			if strings.TrimSpace(content) != "" {
				sep += nl
			}
			next = content + sep + render()
		}
	} else {
		have := normalizeInner(b.inner)
		if have == want {
			req.Manifest.Set(key, Hash(want))
			return Result{Outcome: Unchanged}, nil
		}
		prev, known := req.Manifest.Get(key)
		if !req.Force && (!known || prev != Hash(have)) {
			return Result{Outcome: Conflict, Detail: "nội dung trong khối bint đã bị sửa ngoài Bint; dùng --force để ghi đè (bản cũ sẽ được sao lưu)"}, nil
		}
		next = content[:b.start] + render() + content[b.end:]
	}

	bp, err := Backup(req.Path, req.BackupDir, req.RelName, req.Now)
	if err != nil {
		return Result{}, fmt.Errorf("sao lưu %s: %w", req.Path, err)
	}
	info, err := os.Stat(req.Path)
	perm := os.FileMode(0o644)
	if err == nil {
		perm = info.Mode().Perm()
	}
	if err := WriteAtomic(req.Path, []byte(next), perm); err != nil {
		return Result{}, err
	}
	req.Manifest.Set(key, Hash(want))
	return Result{Outcome: outcome, BackupPath: bp}, nil
}

// ReadBlock trả về nội dung khối Bint trong tệp (nil nếu không có).
func ReadBlock(path string, mk Markers) (*string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := findBlock(string(data), mk)
	if err != nil || b == nil {
		return nil, err
	}
	s := normalizeInner(b.inner)
	return &s, nil
}

// RemoveBlock gỡ khối Bint khỏi tệp (cùng quy tắc chống ghi đè như SyncBlock: khối
// đã bị sửa ngoài Bint chỉ bị gỡ khi Force). Không có khối → Unchanged. Tệp chỉ
// còn khoảng trắng sau khi gỡ thì bị xoá.
func RemoveBlock(req BlockSync) (Result, error) {
	key := req.RelName + "#bint"
	data, err := os.ReadFile(req.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Result{Outcome: Unchanged}, nil
	}
	if err != nil {
		return Result{}, err
	}
	content := string(data)
	b, err := findBlock(content, req.Markers)
	if err != nil {
		return Result{Outcome: Conflict, Detail: err.Error()}, nil
	}
	if b == nil {
		return Result{Outcome: Unchanged}, nil
	}
	have := normalizeInner(b.inner)
	if prev, known := req.Manifest.Get(key); !req.Force && (!known || prev != Hash(have)) {
		return Result{Outcome: Conflict, Detail: "nội dung trong khối bint đã bị sửa ngoài Bint; dùng --force để gỡ (bản cũ sẽ được sao lưu)"}, nil
	}
	next := content[:b.start] + content[b.end:]
	next = strings.TrimRight(next, "\r\n")
	if next != "" {
		next += "\n"
	}
	bp, err := Backup(req.Path, req.BackupDir, req.RelName, req.Now)
	if err != nil {
		return Result{}, fmt.Errorf("sao lưu %s: %w", req.Path, err)
	}
	if strings.TrimSpace(next) == "" {
		if err := os.Remove(req.Path); err != nil {
			return Result{}, err
		}
	} else {
		info, err := os.Stat(req.Path)
		perm := os.FileMode(0o644)
		if err == nil {
			perm = info.Mode().Perm()
		}
		if err := WriteAtomic(req.Path, []byte(next), perm); err != nil {
			return Result{}, err
		}
	}
	req.Manifest.Delete(key)
	return Result{Outcome: Updated, BackupPath: bp}, nil
}
