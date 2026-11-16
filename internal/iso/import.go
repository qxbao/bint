package iso

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/pathutil"
	"bint/internal/state"
)

// Nhập profile từ store của ai-isolator (docs/iso-plan.md mục 6, GĐ6). Nhập theo DANH SÁCH CHO PHÉP, không
// phải "chép hết trừ rác": profile ai-isolator thật có nhiều thư mục lạ (.gemini, .docker, Projects/, .orca…)
// và home/.codex/hooks.json là symlink tới ~/.codex/hooks.json thật — chép theo danh sách loại trừ dễ lọt rác
// hoặc để `iso sync` sau đó ghi xuyên symlink vào tệp thật. Chỉ chép tệp thường (Lstat); store nguồn chỉ đọc.
// HOME giả của bint trùng đường dẫn tương đối với ai-isolator (home/.codex, home/.claude) nên không cần ánh xạ.

// Trạng thái của một profile trong kết quả nhập.
const (
	ImportPlanned  = "planned"  // --dry-run: sẽ nhập
	ImportImported = "imported" // đã nhập
	ImportSkipped  = "skipped"  // bỏ qua (đã có, tool không hỗ trợ, profile.env lệch…)
	ImportFailed   = "failed"   // lỗi; profile vừa tạo đã được gỡ
)

// Tên nhóm tệp được nhập.
const (
	ImpAuth      = "auth"
	ImpConfig    = "config"
	ImpRules     = "rules"
	ImpGitconfig = "gitconfig"
	ImpSessions  = "sessions"
)

// importEntry là một mục của danh sách cho phép (đường dẫn tương đối theo home, dấu '/').
type importEntry struct {
	group    string
	rel      string
	dir      bool
	sessions bool // chỉ nhập khi WithSessions
}

var importAllow = map[Tool][]importEntry{
	Codex: {
		{ImpAuth, ".codex/auth.json", false, false},
		{ImpConfig, ".codex/config.toml", false, false},
		{ImpRules, ".codex/rules", true, false},
		{ImpGitconfig, ".gitconfig", false, false},
		{ImpSessions, ".codex/sessions", true, true},
		{ImpSessions, ".codex/archived_sessions", true, true},
		{ImpSessions, ".codex/session_index.jsonl", false, true},
		{ImpSessions, ".codex/history.jsonl", false, true},
	},
	Claude: {
		{ImpAuth, ".claude/.credentials.json", false, false},
		{ImpConfig, ".claude/.claude.json", false, false},
		{ImpConfig, ".claude/settings.json", false, false},
		{ImpConfig, ".claude/CLAUDE.md", false, false},
		{ImpGitconfig, ".gitconfig", false, false},
		{ImpSessions, ".claude/projects", true, true},
		{ImpSessions, ".claude/history.jsonl", false, true},
	},
}

// importSkipGroups là các nhóm cố ý không nhập, chỉ để báo dung lượng (đường dẫn tương đối theo thư mục
// profile ai-isolator; "home/..." là trong HOME giả). Mẫu có '*' là glob trong thư mục cha.
var importSkipGroups = []string{
	"cache", "tmp", "data", "state", "config",
	"home/.local/share/claude/versions",
	"home/.codex/packages", "home/.codex/.tmp", "home/.codex/tmp",
	"home/.codex/*.sqlite*",
}

// importSizeCap chặn số mục duyệt khi tính dung lượng nhóm bỏ (cache có thể hàng trăm nghìn tệp).
const importSizeCap = 20000

// importFault là điểm chèn lỗi của test (sau khi đã tạo profile; stage "copy" với rel của tệp sắp chép).
var importFault = func(stage, rel string) error { return nil }

// ImportOptions là đầu vào của Import.
type ImportOptions struct {
	// Source là gốc store ai-isolator (chứa profiles/); rỗng thì AI_ISOLATOR_HOME của máy thật, rồi
	// <home thật>/.local/share/ai-isolator.
	Source string
	// Only là các profile cần nhập, dạng "<tool>--<tên>" (hoặc "<tool>/<tên>"); rỗng = mọi profile hỗ trợ.
	Only         []string
	WithSessions bool
	DryRun       bool
	// DropMCP là tên mục MCP bị bỏ khỏi cấu hình đã chép (trùng bản thật: để IsoSync dựng lại, không xung đột).
	DropMCP map[Tool][]string
	// DropCodexProjects là khoá [projects."<đường dẫn>"] và DropCodexHookState là khoá [hooks.state."<hooks.json>:
	// <sự kiện>:<i>:<j>"] mà cấu hình Codex thật đã có và IsoSync sẽ tự dựng cho repo hiện tại: bản đã chép
	// (giá trị có thể khác bản thật) bị bỏ để profile mới, chưa có receipt, không rơi vào xung đột. Khoá không
	// có trong danh sách (trust repo khác) được giữ. So đường dẫn theo dạng chuẩn hoá (không giải liên kết
	// thành phần cuối).
	DropCodexProjects  []string
	DropCodexHookState []string
	// Env là env của tiến trình (mặc định os.Environ()).
	Env []string
}

// ImportFile là một tệp đã (hoặc sẽ) được chép.
type ImportFile struct {
	Rel   string `json:"rel"` // theo home, dấu '/'
	Bytes int64  `json:"bytes"`
	Group string `json:"group"`
}

// ImportSkip là một nhóm cố ý không chép.
type ImportSkip struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	Capped bool   `json:"capped,omitempty"` // dung lượng là cận dưới (duyệt quá nhiều mục)
	Count  int    `json:"count,omitempty"`  // số mục (chỉ cho symlink)
}

// ImportResult là kết quả của một profile nguồn.
type ImportResult struct {
	ID      string       `json:"id"` // <tool>/<tên>; với mục không hiểu được là tên thư mục nguồn
	Status  string       `json:"status"`
	Reason  string       `json:"reason,omitempty"`
	Files   []ImportFile `json:"files,omitempty"`
	Bytes   int64        `json:"bytes"`
	Skipped []ImportSkip `json:"skipped,omitempty"`
	// Notes là điều đáng chú ý nhưng không lỗi (vd URL LB không hợp lệ).
	Notes []string `json:"notes,omitempty"`
	// LB là cấu hình LB nhập từ codex-lb.mode/url (Codex), không dò mạng.
	LB LB `json:"lb"`
	// HasCreds: có tệp đăng nhập trong số tệp nhập.
	HasCreds bool `json:"has_credentials"`
	// Profile chỉ có khi Status = imported.
	Profile Profile `json:"-"`
}

// ResolveImportSource chọn gốc store ai-isolator: source > AI_ISOLATOR_HOME (của máy thật) > mặc định.
func ResolveImportSource(source string, env []string) (string, error) {
	if env == nil {
		env = os.Environ()
	}
	if source != "" {
		return requireAbs("--source", source)
	}
	e := realEnvList(env)
	if v := e.val("AI_ISOLATOR_HOME"); v != "" {
		return requireAbs("AI_ISOLATOR_HOME", v)
	}
	home, err := realHomeOf(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "ai-isolator"), nil
}

// Import nhập profile từ store ai-isolator vào st. Lỗi trả về chỉ khi không đọc được store nguồn; lỗi từng
// profile nằm trong ImportResult (Status failed) và không cản profile khác. Store nguồn không bị đổi.
func Import(st *Store, o ImportOptions) ([]ImportResult, error) {
	root, err := ResolveImportSource(o.Source, o.Env)
	if err != nil {
		return nil, err
	}
	if err := checkImportRoots(root, st.Root); err != nil {
		return nil, err
	}
	profDir := filepath.Join(root, "profiles")
	ents, err := os.ReadDir(profDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("không thấy store ai-isolator ở %s (đặt AI_ISOLATOR_HOME hoặc --source)", root)
		}
		return nil, err
	}
	want, err := parseImportOnly(o.Only)
	if err != nil {
		return nil, err
	}
	var out []ImportResult
	seen := map[string]bool{}
	for _, de := range ents {
		dn := de.Name()
		if !strings.Contains(dn, "--") {
			continue
		}
		if want != nil && !want[dn] {
			continue
		}
		seen[dn] = true
		out = append(out, importOne(st, filepath.Join(profDir, dn), dn, o))
	}
	var missing []string
	for dn := range want {
		if !seen[dn] {
			missing = append(missing, dn)
		}
	}
	sort.Strings(missing)
	for _, dn := range missing {
		out = append(out, ImportResult{ID: dn, Status: ImportFailed, Reason: "không có trong store ai-isolator"})
	}
	slices.SortStableFunc(out, func(a, b ImportResult) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// parseImportOnly chuẩn hoá danh sách profile cần nhập về tên thư mục "<tool>--<tên>"; nil = tất cả.
func parseImportOnly(only []string) (map[string]bool, error) {
	if len(only) == 0 {
		return nil, nil
	}
	want := map[string]bool{}
	for _, s := range only {
		d := s
		if strings.Contains(s, "/") {
			t, name, err := ParseID(s)
			if err != nil {
				return nil, err
			}
			d = dirName(t, name)
		} else if _, _, err := parseDirName(s); err != nil {
			// Tool lạ (vd agy--x) vẫn cho qua để báo "không hỗ trợ" thay vì lỗi cú pháp; chỉ chặn dạng sai tên.
			ts, name, ok := strings.Cut(s, "--")
			if !ok || ts == "" || ValidName(name) != nil {
				return nil, fmt.Errorf("profile %q không có dạng <tool>--<tên>", s)
			}
		}
		want[d] = true
	}
	return want, nil
}

// checkImportRoots từ chối khi store nguồn và store bint lồng nhau (nhập chéo sẽ chép chính mình).
func checkImportRoots(src, dst string) error {
	cs, err := pathutil.Canonical(src)
	if err != nil {
		return err
	}
	cd, err := pathutil.Canonical(dst)
	if err != nil {
		return err
	}
	if cs == cd || within(cs, cd) || within(cd, cs) {
		return fmt.Errorf("store ai-isolator %s và store bint %s trùng hoặc lồng nhau", src, dst)
	}
	return nil
}

func importOne(st *Store, srcDir, dn string, o ImportOptions) (res ImportResult) {
	res.ID = dn
	skip := func(format string, a ...any) ImportResult {
		res.Status, res.Reason = ImportSkipped, fmt.Sprintf(format, a...)
		return res
	}
	ts, name, _ := strings.Cut(dn, "--")
	t, err := ParseTool(ts)
	if err != nil {
		return skip("tool %q chưa hỗ trợ nhập (hỗ trợ: %s)", ts, toolNames())
	}
	if err := ValidName(name); err != nil {
		return skip("%v", err)
	}
	res.ID = string(t) + "/" + name
	fi, err := os.Lstat(srcDir)
	if err != nil || !fi.IsDir() {
		return skip("%s không phải thư mục thường (symlink hoặc tệp)", srcDir)
	}
	env, err := readImportEnv(filepath.Join(srcDir, "profile.env"))
	if err != nil {
		return skip("profile.env: %v", err)
	}
	if env["AI_ISOLATOR_TOOL"] != string(t) || env["AI_ISOLATOR_PROFILE"] != name {
		return skip("profile.env ghi %s/%s nhưng thư mục là %s/%s", env["AI_ISOLATOR_TOOL"], env["AI_ISOLATOR_PROFILE"], t, name)
	}
	if _, err := os.Lstat(st.Dir(t, name)); err == nil {
		return skip("profile bint %s đã có (không ghi đè)", res.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return skip("%v", err)
	}

	files, dirs, symlinks, err := planImport(srcDir, t, o.WithSessions)
	if err != nil {
		res.Status, res.Reason = ImportFailed, err.Error()
		return res
	}
	res.Files = files
	for _, f := range files {
		res.Bytes += f.Bytes
		if f.Group == ImpAuth {
			res.HasCreds = true
		}
	}
	res.Skipped = skippedGroups(srcDir, o.WithSessions)
	if symlinks > 0 {
		res.Skipped = append(res.Skipped, ImportSkip{Name: "symlink trong danh sách cho phép", Count: symlinks})
	}
	if t == Codex {
		res.LB, res.Notes = readImportLB(srcDir)
	}
	if o.DryRun {
		res.Status = ImportPlanned
		return res
	}

	p, err := st.Create(t, name)
	if err != nil {
		if errors.Is(err, ErrExists) {
			return skip("profile bint %s đã có (không ghi đè)", res.ID)
		}
		res.Status, res.Reason = ImportFailed, err.Error()
		return res
	}
	ok := false
	defer func() {
		if !ok {
			if err := RemoveProfile(p); err != nil {
				res.Notes = append(res.Notes, fmt.Sprintf("không gỡ được profile dở dang %s: %v", p.Dir, err))
			}
		}
	}()
	fail := func(err error) ImportResult {
		res.Status, res.Reason = ImportFailed, err.Error()
		return res
	}
	if err := copyImport(p, filepath.Join(srcDir, "home"), files, dirs); err != nil {
		return fail(err)
	}
	if err := cleanImported(p, srcDir, o.DropMCP[t], o.DropCodexProjects, o.DropCodexHookState); err != nil {
		return fail(err)
	}
	p.Meta.LB = res.LB
	if err := p.SaveMeta(); err != nil {
		return fail(err)
	}
	ok = true
	res.Status, res.Profile = ImportImported, p
	return res
}

func toolNames() string {
	var s []string
	for _, t := range Tools() {
		s = append(s, string(t))
	}
	return strings.Join(s, ", ")
}

// readImportEnv đọc profile.env (KEY=VALUE mỗi dòng, '#' là chú thích; cho phép dấu nháy bao giá trị).
func readImportEnv(path string) (map[string]string, error) {
	b, err := readSmallRegular(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		m[strings.TrimSpace(k)] = v
	}
	return m, nil
}

// readSmallRegular đọc tệp thường nhỏ (≤ 64 KB); symlink hay tệp lớn là lỗi.
func readSmallRegular(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("không phải tệp thường")
	}
	if fi.Size() > 64<<10 {
		return nil, errors.New("tệp quá lớn")
	}
	return os.ReadFile(path)
}

// readImportLB nhập codex-lb.mode/url của ai-isolator thành LB của profile, KHÔNG gọi mạng (import phải chạy
// được khi LB tắt; `run` đã dừng mã 69 khi LB linked không sẵn sàng). Thiếu tệp thì để trống; linked mà URL
// thiếu/hỏng thì để trống kèm ghi chú.
func readImportLB(srcDir string) (LB, []string) {
	modeB, err := readSmallRegular(filepath.Join(srcDir, "codex-lb.mode"))
	if err != nil {
		return LB{}, nil
	}
	mode := strings.TrimSpace(string(modeB))
	switch mode {
	case LBDirect:
		return LB{Mode: LBDirect}, nil
	case LBLinked:
		urlB, err := readSmallRegular(filepath.Join(srcDir, "codex-lb.url"))
		if err != nil {
			return LB{}, []string{"codex-lb.mode là linked nhưng thiếu codex-lb.url: để trống LB (chọn lại bằng `bint iso run codex --lb URL`)"}
		}
		root, err := NormalizeLBRoot(string(urlB))
		if err != nil {
			return LB{}, []string{fmt.Sprintf("codex-lb.url không hợp lệ (%v): để trống LB", err)}
		}
		return LB{Mode: LBLinked, URL: root}, nil
	}
	return LB{}, []string{fmt.Sprintf("codex-lb.mode %q không hiểu được: để trống LB", mode)}
}

// planImport liệt kê tệp sẽ chép (chỉ tệp thường dưới các mục cho phép) và thư mục cần tạo. symlinks là số
// symlink/mục lạ gặp trong danh sách cho phép (bị bỏ).
func planImport(srcDir string, t Tool, withSessions bool) (files []ImportFile, dirs []string, symlinks int, err error) {
	home := filepath.Join(srcDir, "home")
	if fi, lerr := os.Lstat(home); lerr != nil || !fi.IsDir() {
		return nil, nil, 0, fmt.Errorf("%s không phải thư mục thường", home)
	}
	for _, e := range importAllow[t] {
		if e.sessions && !withSessions {
			continue
		}
		abs := filepath.Join(home, filepath.FromSlash(e.rel))
		if !realDirChain(home, abs) {
			if _, lerr := os.Lstat(abs); lerr == nil {
				symlinks++ // thư mục cha là symlink: bỏ cả mục
			}
			continue
		}
		fi, lerr := os.Lstat(abs)
		if errors.Is(lerr, os.ErrNotExist) {
			continue
		}
		if lerr != nil {
			return nil, nil, 0, lerr
		}
		switch {
		case fi.Mode()&os.ModeSymlink != 0:
			symlinks++
		case e.dir && fi.IsDir():
			dirs = append(dirs, e.rel)
			werr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, werr error) error {
				if werr != nil {
					if errors.Is(werr, os.ErrNotExist) {
						return nil
					}
					return werr
				}
				rel, rerr := filepath.Rel(home, p)
				if rerr != nil {
					return rerr
				}
				rel = filepath.ToSlash(rel)
				switch {
				case p == abs:
				case d.Type()&fs.ModeSymlink != 0:
					symlinks++
				case d.IsDir():
					dirs = append(dirs, rel)
				case d.Type().IsRegular():
					info, ierr := d.Info()
					if ierr != nil {
						if errors.Is(ierr, os.ErrNotExist) {
							return nil
						}
						return ierr
					}
					files = append(files, ImportFile{Rel: rel, Bytes: info.Size(), Group: e.group})
				default:
					symlinks++ // socket, thiết bị…
				}
				return nil
			})
			if werr != nil {
				return nil, nil, 0, werr
			}
		case !e.dir && fi.Mode().IsRegular():
			files = append(files, ImportFile{Rel: e.rel, Bytes: fi.Size(), Group: e.group})
		default:
			symlinks++
		}
	}
	return files, dirs, symlinks, nil
}

// realDirChain kiểm mọi thành phần từ home tới cha của abs là thư mục thật (không symlink).
func realDirChain(home, abs string) bool {
	rel, err := filepath.Rel(home, filepath.Dir(abs))
	if err != nil {
		return false
	}
	cur := home
	if rel == "." {
		return true
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return errors.Is(err, os.ErrNotExist) // vắng thì để Lstat của mục báo vắng
		}
		if !fi.IsDir() {
			return false
		}
	}
	return true
}

// skippedGroups báo các nhóm cố ý không chép mà có mặt ở nguồn, kèm dung lượng.
func skippedGroups(srcDir string, withSessions bool) []ImportSkip {
	var out []ImportSkip
	for _, g := range importSkipGroups {
		var paths []string
		if strings.ContainsAny(g, "*?[") {
			paths, _ = filepath.Glob(filepath.Join(srcDir, filepath.FromSlash(g)))
		} else {
			paths = []string{filepath.Join(srcDir, filepath.FromSlash(g))}
		}
		sk := ImportSkip{Name: g}
		found := false
		for _, p := range paths {
			if _, err := os.Lstat(p); err != nil {
				continue
			}
			found = true
			n, capped := sizeCapped(p)
			sk.Bytes += n
			sk.Capped = sk.Capped || capped
		}
		if found {
			out = append(out, sk)
		}
	}
	return out
}

// sizeCapped là tổng kích thước tệp thường dưới p (không đi theo symlink), dừng sau importSizeCap mục.
func sizeCapped(p string) (int64, bool) {
	var total int64
	n := 0
	capped := false
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > importSizeCap {
			capped = true
			return filepath.SkipAll
		}
		if d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total, capped
}

// copyImport tạo thư mục và chép tệp vào home của profile p (mọi đích phải nằm trong p.Home). Tệp 0600,
// thư mục 0700. Tệp biến mất giữa chừng (phiên nguồn đang ghi/xoá) thì bỏ qua.
func copyImport(p Profile, srcHome string, files []ImportFile, dirs []string) error {
	dst := func(rel string) (string, error) {
		d := filepath.Join(p.Home, filepath.FromSlash(rel))
		if !within(d, p.Home) {
			return "", fmt.Errorf("đích %s nằm ngoài home của profile", rel)
		}
		return d, nil
	}
	for _, rel := range dirs {
		d, err := dst(rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := importFault("copy", f.Rel); err != nil {
			return err
		}
		d, err := dst(f.Rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
			return err
		}
		if err := copyRegular(filepath.Join(srcHome, filepath.FromSlash(f.Rel)), d); err != nil {
			return fmt.Errorf("chép %s: %w", f.Rel, err)
		}
	}
	return nil
}

// copyRegular chép một tệp thường (nguồn mở chỉ đọc), đích tạo mới với O_EXCL và 0600.
func copyRegular(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return nil // đã thành symlink/thiết bị sau lúc lập kế hoạch: bỏ
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// cleanImported bỏ khỏi cấu hình đã chép những gì làm sync xung đột hoặc vô hiệu: mục MCP trùng tên bản thật
// (dropMCP), với Codex khoá hooks.state trỏ vào thư mục profile ai-isolator (đường dẫn cũ, vô hiệu) và khoá
// projects/hooks.state mà bản thật cũng có (dropProjects, dropHookState: IsoSync dựng lại từ bản thật).
func cleanImported(p Profile, srcDir string, dropMCP, dropProjects, dropHookState []string) error {
	bak, err := os.MkdirTemp("", "bint-iso-import-bak-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(bak)
	now := time.Now()
	switch p.Tool {
	case Codex:
		path := filepath.Join(p.ConfigDir(), "config.toml")
		if _, err := os.Lstat(path); err != nil {
			return nil
		}
		f, err := openTOML(path)
		if err != nil {
			return err
		}
		for _, n := range dropMCP {
			if err := f.del([]string{"mcp_servers"}, n); err != nil {
				return err
			}
		}
		if ps, ok, _ := f.get([]string{}, "projects"); ok && len(dropProjects) > 0 {
			if m, isMap := ps.(map[string]any); isMap {
				drop := map[string]bool{}
				for _, k := range dropProjects {
					drop[normKeyPath(k)] = true
				}
				for key := range m {
					if drop[normKeyPath(key)] {
						if err := f.del([]string{"projects"}, key); err != nil {
							return err
						}
					}
				}
			}
		}
		if hs, ok, _ := f.get([]string{"hooks"}, "state"); ok {
			if m, isMap := hs.(map[string]any); isMap {
				dirs := sourceDirs(srcDir)
				drop := map[string]bool{}
				for _, k := range dropHookState {
					drop[normHookKey(k)] = true
				}
				for key := range m {
					if hookStateInside(key, dirs) || drop[normHookKey(key)] {
						if err := f.del([]string{"hooks", "state"}, key); err != nil {
							return err
						}
					}
				}
			}
		}
		_, err = f.save(bak, "config.toml", now)
		return err
	case Claude:
		path := filepath.Join(p.ConfigDir(), ".claude.json")
		if _, err := os.Lstat(path); err != nil {
			return nil
		}
		f, err := openJSON(path)
		if err != nil {
			return err
		}
		for _, n := range dropMCP {
			if err := f.del([]string{"mcpServers"}, n); err != nil {
				return err
			}
		}
		_, err = f.save(bak, ".claude.json", now)
		return err
	}
	return nil
}

// sourceDirs là thư mục profile ai-isolator ở dạng gốc và đã giải symlink.
func sourceDirs(srcDir string) []string {
	out := []string{srcDir}
	if r, err := filepath.EvalSymlinks(srcDir); err == nil && r != srcDir {
		out = append(out, r)
	}
	return out
}

// normKeyPath chuẩn hoá đường dẫn trong khoá TOML để so sánh: giải liên kết thư mục cha nhưng KHÔNG giải thành
// phần cuối (khác state.NormalizePath, vốn chạy EvalSymlinks trên cả đường dẫn). Trong profile ai-isolator thật
// home/.codex/hooks.json là symlink tới ~/.codex/hooks.json: giải thành phần cuối làm khoá của nó trông như nằm
// ngoài thư mục nguồn. Dấu '/', không phân biệt hoa thường trên Windows/macOS (như state.NormalizePath).
func normKeyPath(p string) string {
	p = filepath.Clean(p)
	q := filepath.ToSlash(filepath.Join(pathutil.BestEffort(filepath.Dir(p)), filepath.Base(p)))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		q = strings.ToLower(q)
	}
	return q
}

// splitHookStateKey tách khoá hooks.state "<đường dẫn hooks.json>:<sự kiện>:<i>:<j>" từ bên phải (đường dẫn
// Windows có ':'): path và phần đuôi "<sự kiện>:<i>:<j>".
func splitHookStateKey(key string) (path, tail string, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) < 4 {
		return "", "", false
	}
	n := len(parts) - 3
	return strings.Join(parts[:n], ":"), strings.Join(parts[n:], ":"), true
}

// normHookKey là dạng chuẩn hoá của khoá hooks.state (đường dẫn theo normKeyPath, giữ nguyên phần đuôi).
func normHookKey(key string) string {
	path, tail, ok := splitHookStateKey(key)
	if !ok {
		return key
	}
	return normKeyPath(path) + ":" + tail
}

// hookStateInside: khoá hooks.state có dạng "<đường dẫn hooks.json>:<sự kiện>:<i>:<j>" (tách từ bên phải vì
// đường dẫn Windows có ':'); đúng khi đường dẫn nằm dưới một trong dirs. Đường dẫn là symlink (hooks.json của
// ai-isolator) vẫn tính theo vị trí của chính liên kết, không theo đích.
func hookStateInside(key string, dirs []string) bool {
	path, _, ok := splitHookStateKey(key)
	if !ok {
		return false
	}
	np := normKeyPath(path)
	for _, d := range dirs {
		nd := strings.TrimSuffix(state.NormalizePath(d), "/")
		if np == nd || strings.HasPrefix(np, nd+"/") {
			return true
		}
	}
	return false
}
