// Package state xác định và quản lý thư mục trạng thái cục bộ của Bint.
//
// Thư mục trạng thái nằm NGOÀI repository theo mặc định và được định danh bằng
// git-common-dir, nên mọi worktree của cùng một repository dùng chung một nơi:
//
//	<base>/<repo-id>/
//	├── state.sqlite
//	├── spool/       hàng đợi sự kiện từ hook (JSONL, mỗi sự kiện một tệp)
//	├── runtime/
//	├── eval-work/
//	├── generated/
//	└── backups/
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
)

// EnvStateDir ghi đè thư mục gốc chứa trạng thái của mọi repository.
const EnvStateDir = "BINT_STATE_DIR"

// Paths là các đường dẫn trong thư mục trạng thái của một repository.
type Paths struct {
	RepoID    string
	Root      string
	DB        string
	Spool     string
	Runtime   string
	EvalWork  string
	Generated string
	Backups   string
	Manifest  string // managed.json: băm nội dung các tệp do Bint quản lý
	RepoInfo  string // repo.json: thông tin định danh repository
}

// BaseDir trả về thư mục gốc chứa trạng thái của mọi repository.
func BaseDir() (string, error) {
	if d := os.Getenv(EnvStateDir); d != "" {
		return filepath.Abs(d)
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "bint"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("không xác định được thư mục home: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "bint", "state"), nil
		}
		return filepath.Join(home, "AppData", "Local", "bint", "state"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "bint", "state"), nil
	default:
		return filepath.Join(home, ".local", "state", "bint"), nil
	}
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// RepoID tính định danh ổn định từ git-common-dir: <tên-repo>-<12 hex sha256>.
func RepoID(commonDir string) string {
	norm := NormalizePath(commonDir)
	sum := sha256.Sum256([]byte(norm))
	name := filepath.Base(commonDir)
	if strings.EqualFold(name, ".git") {
		name = filepath.Base(filepath.Dir(commonDir))
	}
	name = strings.TrimSuffix(name, ".git")
	name = strings.Trim(unsafeChars.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		name = "repo"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return name + "-" + hex.EncodeToString(sum[:])[:12]
}

// NormalizePath chuẩn hoá đường dẫn để băm: tuyệt đối, giải symlink nếu được,
// dấu '/', và không phân biệt hoa thường trên Windows/macOS.
func NormalizePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}

// Resolve xác định thư mục trạng thái cho repository theo cấu hình.
func Resolve(repo gitrepo.Repo, cfg config.Config) (Paths, error) {
	id := RepoID(repo.CommonDir)
	var root string
	if cfg.State.Dir != "" && cfg.State.Dir != "auto" {
		root = filepath.Clean(cfg.State.Dir)
	} else {
		base, err := BaseDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(base, id)
	}
	if isInside(root, repo.Toplevel) {
		return Paths{}, fmt.Errorf("thư mục trạng thái %s nằm trong repository; đặt state.dir ra ngoài", root)
	}
	return At(id, root), nil
}

// At dựng Paths cho thư mục gốc root.
func At(id, root string) Paths {
	return Paths{
		RepoID:    id,
		Root:      root,
		DB:        filepath.Join(root, "state.sqlite"),
		Spool:     filepath.Join(root, "spool"),
		Runtime:   filepath.Join(root, "runtime"),
		EvalWork:  filepath.Join(root, "eval-work"),
		Generated: filepath.Join(root, "generated"),
		Backups:   filepath.Join(root, "backups"),
		Manifest:  filepath.Join(root, "managed.json"),
		RepoInfo:  filepath.Join(root, "repo.json"),
	}
}

func isInside(p, dir string) bool {
	rel, err := filepath.Rel(NormalizePath(dir), NormalizePath(p))
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// Initialized cho biết thư mục trạng thái đã được `bint init` tạo hay chưa.
func (p Paths) Initialized() bool {
	_, err := os.Stat(p.RepoInfo)
	return err == nil
}

// RepoInfo được ghi vào repo.json để phát hiện va chạm định danh và hỗ trợ doctor.
type RepoInfo struct {
	RepoID    string    `json:"repo_id"`
	CommonDir string    `json:"common_dir"`
	CreatedAt time.Time `json:"created_at"`
	Schema    int       `json:"schema"`
}

// ErrForeignState: thư mục trạng thái thuộc về repository khác.
var ErrForeignState = errors.New("thư mục trạng thái thuộc về repository khác")

// Ensure tạo cây thư mục trạng thái (quyền 0700) và repo.json nếu chưa có.
func (p Paths) Ensure(repo gitrepo.Repo) error {
	for _, d := range []string{p.Root, p.Spool, p.Runtime, p.EvalWork, p.Generated, p.Backups} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	info, err := p.ReadRepoInfo()
	switch {
	case err == nil:
		if NormalizePath(info.CommonDir) != NormalizePath(repo.CommonDir) {
			return fmt.Errorf("%w: %s ghi %s", ErrForeignState, p.RepoInfo, info.CommonDir)
		}
		return nil
	case errors.Is(err, os.ErrNotExist):
		data, _ := json.MarshalIndent(RepoInfo{
			RepoID: p.RepoID, CommonDir: repo.CommonDir, CreatedAt: time.Now().UTC(), Schema: 1,
		}, "", "  ")
		return os.WriteFile(p.RepoInfo, append(data, '\n'), 0o600)
	default:
		return err
	}
}

// ReadRepoInfo đọc repo.json.
func (p Paths) ReadRepoInfo() (RepoInfo, error) {
	var info RepoInfo
	data, err := os.ReadFile(p.RepoInfo)
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}
