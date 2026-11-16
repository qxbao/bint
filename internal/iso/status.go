package iso

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LinkStatus là trạng thái chỉ-đọc của một liên kết chia sẻ. State theo nghĩa của EnsureLink nhưng không thay
// đổi gì: LinkCreated = chưa có (chạy sẽ tạo), LinkReplacedEmpty = thư mục rỗng (chạy sẽ thay).
type LinkStatus = ShareResult

// ProfileStatus là một dòng của `bint iso status` (JSON-able).
type ProfileStatus struct {
	ID          string       `json:"id"`
	Tool        Tool         `json:"tool"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"created_at"`
	LBMode      string       `json:"lb_mode"` // "", linked, direct
	LBURL       string       `json:"lb_url,omitempty"`
	Auth        bool         `json:"auth"`   // có tệp đăng nhập trong profile
	Binary      string       `json:"binary"` // harness trên PATH thật ("" nếu không có)
	Session     bool         `json:"session"`
	Temp        string       `json:"temp"`
	TempExists  bool         `json:"temp_exists"`
	DaemonAlive bool         `json:"daemon_alive"`
	Links       []LinkStatus `json:"-"`
	LinkIssues  []string     `json:"link_issues,omitempty"`
	// Install là cảnh báo về bản cài harness (Codex packages thiếu bản thật hoặc là bản riêng).
	Install []string `json:"install,omitempty"`
	// TotalBytes là dung lượng thư mục profile; UserData là mục lạ ở gốc HOME giả (bint không tự xoá).
	TotalBytes int64       `json:"total_bytes"`
	UserData   []UserEntry `json:"user_data,omitempty"`
}

// AuthFile là tệp đăng nhập của tool trong HOME giả (tương đối theo home). macOS có thể giữ đăng nhập của Claude
// trong Keychain (S6 chưa đo) nên tệp vắng chưa chắc là chưa đăng nhập.
func (t Tool) authFile() string {
	if t == Codex {
		return filepath.Join(".codex", "auth.json")
	}
	return filepath.Join(".claude", ".credentials.json")
}

// SessionActive cho biết có phiên đang giữ khoá S (thử X không chờ rồi nhả). Không tạo session.lock.
func (p Profile) SessionActive() (bool, error) {
	if _, err := os.Lstat(p.LockPath()); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	release, busy, err := p.Lock(true)
	if err != nil {
		return false, err
	}
	if busy {
		return true, nil
	}
	_ = release()
	return false, nil
}

// HasAuth cho biết profile có tệp đăng nhập.
func (p Profile) HasAuth() bool {
	fi, err := os.Stat(filepath.Join(p.Home, p.Tool.authFile()))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// CheckShares kiểm các liên kết chia sẻ của profile mà không ghi gì (không tạo thư mục thật, không tạo liên
// kết). Mục không hiệu lực (cache private…) chỉ xuất hiện khi còn liên kết đúng đích mà `run` sẽ gỡ (LinkRemoved).
func CheckShares(p Profile, env []string) ([]ShareResult, error) {
	return checkShares(runtime.GOOS, p, env)
}

func checkShares(goos string, p Profile, env []string) ([]ShareResult, error) {
	e := newEnvList(goos, env)
	restoreReal(e)
	var res []ShareResult
	var errs []error
	for _, s := range catalog(goos, e) {
		effective := p.Settings.shares(s.Name)
		for _, l := range s.Links {
			link := shareLinkPath(p, l)
			st, err := InspectLink(link, l.Real)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !effective {
				// Chỉ báo liên kết mà chạy sẽ gỡ (LinkRemoved); mọi trạng thái khác của mục không hiệu lực là chuyện của người dùng.
				if st == LinkOK {
					res = append(res, ShareResult{Share: s.Name, Link: link, Target: l.Real, State: LinkRemoved})
				}
				continue
			}
			res = append(res, ShareResult{Share: s.Name, Link: link, Target: l.Real, State: st})
		}
	}
	return res, errors.Join(errs...)
}

// Status trả trạng thái mọi profile của store (lỗi nạp profile hỏng gộp vào error, kèm phần còn lại) và không
// ghi gì vào đĩa. env là env thật để tìm harness và tính đường dẫn temp/cache (mặc định os.Environ()).
func Status(s *Store, env []string) ([]ProfileStatus, error) {
	if env == nil {
		env = os.Environ()
	}
	profiles, lerr := s.List()
	var out []ProfileStatus
	var errs []error
	if lerr != nil {
		errs = append(errs, lerr)
	}
	for _, p := range profiles {
		st := ProfileStatus{
			ID: p.ID(), Tool: p.Tool, Name: p.Name, CreatedAt: p.Meta.CreatedAt,
			LBMode: p.Meta.LB.Mode, LBURL: p.Meta.LB.URL,
			Auth: p.HasAuth(), DaemonAlive: p.DaemonAlive(),
		}
		if bin, err := LookHarness(p.Tool, env, p.StoreRoot); err == nil {
			st.Binary = bin
		}
		if active, err := p.SessionActive(); err != nil {
			errs = append(errs, err)
		} else {
			st.Session = active
		}
		st.Temp = p.tempDirFrom(env)
		if _, err := os.Stat(st.Temp); err == nil {
			st.TempExists = true
		}
		if links, err := checkShares(runtime.GOOS, p, env); err != nil {
			errs = append(errs, err)
		} else {
			st.Links = links
			for _, l := range links {
				if l.State.Conflict() {
					st.LinkIssues = append(st.LinkIssues, l.Link+": "+l.State.String())
				}
			}
		}
		st.Install = installIssues(runtime.GOOS, p, env)
		u, uerr := usageOf(runtime.GOOS, p, env)
		if uerr != nil {
			errs = append(errs, uerr)
		}
		st.TotalBytes, st.UserData = u.Total, u.UserEntries
		out = append(out, st)
	}
	return out, errors.Join(errs...)
}
