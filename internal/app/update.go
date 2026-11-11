package app

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"bint/internal/gitrepo"
)

// SourceEnv ghi đè thư mục mã nguồn Bint cho `bint update`.
const SourceEnv = "BINT_SOURCE_DIR"

// ErrNoSource: không xác định được thư mục mã nguồn Bint.
var ErrNoSource = errors.New("không tìm thấy thư mục mã nguồn Bint")

// buildSourceFile là đường dẫn tệp này lúc biên dịch. Khi build không có -trimpath
// (mặc định của `go install` từ thư mục mã nguồn), đây là đường dẫn tuyệt đối, từ
// đó suy ra thư mục gốc của mã nguồn.
var buildSourceFile = func() string {
	_, f, _, _ := runtime.Caller(0)
	return f
}()

// IsBintSource cho biết dir có phải thư mục gốc mã nguồn Bint (go.mod "module bint"
// và cmd/bint).
func IsBintSource(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "module" {
			if f[1] != "bint" {
				return false
			}
			st, err := os.Stat(filepath.Join(dir, "cmd", "bint"))
			return err == nil && st.IsDir()
		}
	}
	return false
}

// FindSource tìm thư mục mã nguồn Bint: tham số explicit, rồi biến BINT_SOURCE_DIR,
// rồi đường dẫn lúc biên dịch.
func FindSource(explicit string) (string, error) {
	try := func(dir, from string) (string, error) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		if !IsBintSource(abs) {
			return "", fmt.Errorf("%w: %s (%s) không phải mã nguồn Bint (cần go.mod \"module bint\" và cmd/bint)", ErrNoSource, abs, from)
		}
		return abs, nil
	}
	if explicit != "" {
		return try(explicit, "--source")
	}
	if v := strings.TrimSpace(os.Getenv(SourceEnv)); v != "" {
		return try(v, SourceEnv)
	}
	if filepath.IsAbs(buildSourceFile) {
		// <gốc>/internal/app/update.go
		dir := filepath.Dir(filepath.Dir(filepath.Dir(buildSourceFile)))
		if IsBintSource(dir) {
			return dir, nil
		}
		return "", fmt.Errorf("%w: thư mục lúc biên dịch %s không còn; chỉ định bằng --source hoặc %s", ErrNoSource, dir, SourceEnv)
	}
	return "", fmt.Errorf("%w: bản này được build với -trimpath; chỉ định bằng --source hoặc %s", ErrNoSource, SourceEnv)
}

// UpdateOptions là tham số của UpdateSelf.
type UpdateOptions struct {
	Source string `flag:"source" form:"suggest=bint_source"` // rỗng = FindSource
	Pull   bool   `flag:"pull"`                              // git pull --ff-only trước khi build
	Out    io.Writer
}

// UpdateDefaults: thư mục mã nguồn tìm được (nếu có) và không pull.
func UpdateDefaults() UpdateOptions {
	o := UpdateOptions{}
	if src, err := FindSource(""); err == nil {
		o.Source = src
	}
	return o
}

// Validate kiểm thư mục mã nguồn chỉ định rõ (không rỗng) đúng là mã nguồn Bint; lỗi là UsageError (mã thoát 2).
func (o UpdateOptions) Validate() error {
	if o.Source == "" {
		return nil
	}
	if _, err := FindSource(o.Source); err != nil {
		return UsageError{Err: err}
	}
	return nil
}

// UpdateResult là kết quả UpdateSelf.
type UpdateResult struct {
	Source    string
	Target    string // tệp go install đã ghi
	Current   string // tệp thực thi đang chạy
	OldRev    string
	NewRev    string
	Pulled    string // đầu ra git pull (nếu có)
	Mismatch  bool   // Target khác Current: lệnh `bint` trong PATH có thể không phải bản mới
	OldBackup string // Windows: bản cũ được đổi tên để ghi được bản mới
}

// UpdateSelf build lại và cài Bint từ thư mục mã nguồn bằng `go install ./cmd/bint`.
func UpdateSelf(ctx context.Context, opts UpdateOptions) (UpdateResult, error) {
	var res UpdateResult
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	src, err := FindSource(opts.Source)
	if err != nil {
		return res, err
	}
	res.Source = src
	goBin, err := exec.LookPath("go")
	if err != nil {
		return res, fmt.Errorf("không tìm thấy lệnh go trong PATH: %w", err)
	}
	res.OldRev = SelfRevision()

	if opts.Pull {
		o, err := gitrepo.Run(ctx, src, "pull", "--ff-only")
		if err != nil {
			return res, fmt.Errorf("git pull --ff-only trong %s: %w", src, err)
		}
		res.Pulled = strings.TrimSpace(o)
	}

	res.Target, err = installTarget(ctx, goBin, src)
	if err != nil {
		return res, err
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		res.Current = exe
		res.Mismatch = !samePath(exe, res.Target)
	}

	// Windows không cho ghi đè tệp .exe đang chạy nhưng cho đổi tên.
	if runtime.GOOS == "windows" && !res.Mismatch && res.Current != "" {
		old := res.Current + ".old"
		_ = os.Remove(old)
		if err := os.Rename(res.Current, old); err != nil {
			return res, fmt.Errorf("đổi tên bản đang chạy để cài bản mới: %w", err)
		}
		res.OldBackup = old
	}

	cmd := exec.CommandContext(ctx, goBin, "install", "./cmd/bint")
	cmd.Dir = src
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if res.OldBackup != "" {
			_ = os.Rename(res.OldBackup, res.Current) // build hỏng: trả lại bản cũ
			res.OldBackup = ""
		}
		return res, fmt.Errorf("go install ./cmd/bint trong %s: %w", src, err)
	}
	res.NewRev = binaryRevision(ctx, goBin, res.Target)
	return res, nil
}

// installTarget trả về tệp `go install ./cmd/bint` sẽ ghi: $GOBIN/bint hoặc
// $GOPATH[0]/bin/bint.
func installTarget(ctx context.Context, goBin, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, goBin, "env", "GOBIN", "GOPATH", "GOEXE")
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go env: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	for len(lines) < 3 {
		lines = append(lines, "")
	}
	gobin, gopath, goexe := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), strings.TrimSpace(lines[2])
	if gobin == "" {
		if gopath == "" {
			return "", errors.New("go env: GOBIN và GOPATH đều trống")
		}
		gobin = filepath.Join(filepath.SplitList(gopath)[0], "bin")
	}
	return filepath.Join(gobin, "bint"+goexe), nil
}

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// SelfRevision trả về commit của bản đang chạy (rỗng nếu không có thông tin VCS).
func SelfRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return revisionFromSettings(func(yield func(k, v string)) {
		for _, s := range bi.Settings {
			yield(s.Key, s.Value)
		}
	})
}

// binaryRevision đọc commit của một tệp thực thi qua `go version -m`.
func binaryRevision(ctx context.Context, goBin, path string) string {
	b, err := exec.CommandContext(ctx, goBin, "version", "-m", path).Output()
	if err != nil {
		return ""
	}
	return ParseBuildRevision(string(b))
}

// ParseBuildRevision lấy "abc1234" hoặc "abc1234+sửa" từ đầu ra `go version -m`.
func ParseBuildRevision(out string) string {
	return revisionFromSettings(func(yield func(k, v string)) {
		for line := range strings.SplitSeq(out, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "build" {
				if k, v, ok := strings.Cut(f[1], "="); ok {
					yield(k, v)
				}
			}
		}
	})
}

func revisionFromSettings(each func(func(k, v string))) string {
	var rev string
	var modified bool
	each(func(k, v string) {
		switch k {
		case "vcs.revision":
			rev = v
		case "vcs.modified":
			modified = v == "true"
		}
	})
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev != "" && modified {
		rev += "+sửa"
	}
	return rev
}
