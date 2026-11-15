package mcpgw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/mcpcat"
)

// CodegraphDir là thư mục chỉ mục codegraph so với gốc worktree.
const CodegraphDir = ".codegraph"

// CodegraphDB là đường dẫn chỉ mục codegraph so với gốc worktree.
var CodegraphDB = filepath.Join(CodegraphDir, "codegraph.db")

// CmdBase là tên gốc (chữ thường, bỏ .exe/.cmd) của một lệnh.
func CmdBase(cmd string) string {
	b := strings.ToLower(filepath.Base(cmd))
	return strings.TrimSuffix(strings.TrimSuffix(b, ".exe"), ".cmd")
}

// CodegraphIndexBinary tìm binary codegraph để chạy init/sync. Lệnh đã đăng ký chỉ dùng được khi chính nó là binary
// codegraph (tên gốc codegraph, bỏ .exe/.cmd): lệnh bọc như npx/uvx/node mang đối số đã đăng ký mà init/sync không có,
// nên chạy `npx init -y` sẽ tải nhầm gói `init`. Các trường hợp khác dùng binary của danh mục trên PATH.
func CodegraphIndexBinary(svc mcpcat.Service, reg config.Registration) (string, error) {
	name := svc.Run[0]
	if CmdBase(reg.Command) == name {
		name = reg.Command
	}
	path, err := exec.LookPath(name)
	if err != nil {
		if name == svc.Run[0] && reg.Command != "" && CmdBase(reg.Command) != name {
			return "", fmt.Errorf("index cần binary %s trên PATH, nhưng lệnh đã đăng ký là %q (không phải binary %s); cài: %s", name, reg.Command, name, svc.Install)
		}
		return "", fmt.Errorf("không tìm thấy lệnh %q trên PATH; cài: %s", name, svc.Install)
	}
	return path, nil
}

// EnsureGitExclude thêm dòng pattern vào tệp git info/exclude của repository chứa root (tệp do `git rev-parse --git-path
// info/exclude` trả về: worktree liên kết dùng chung của git common dir) khi chưa có dòng tương đương.
func EnsureGitExclude(ctx context.Context, root, pattern string) (file string, added bool, err error) {
	out, err := gitrepo.Run(ctx, root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", false, err
	}
	file = gitrepo.Clean(out)
	if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	bare := strings.TrimSuffix(pattern, "/")
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return file, false, err
	}
	for _, l := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(l) {
		case pattern, "/" + pattern, bare, "/" + bare:
			return file, false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return file, false, err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return file, false, err
	}
	defer f.Close()
	line := pattern + "\n"
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		line = "\n" + line
	}
	if _, err := f.WriteString(line); err != nil {
		return file, false, err
	}
	return file, true, nil
}

// CodegraphInitResult là kết quả CodegraphInit.
type CodegraphInitResult struct {
	// Action: init (chưa có chỉ mục) hoặc sync.
	Action       string
	Binary       string
	ExcludeFile  string
	ExcludeAdded bool
	// Warning: binary chạy không phải lệnh đã đăng ký và phiên bản khác (hoặc không đọc được so với) phiên bản ghim.
	Warning string
}

// CodegraphInit là lõi của `bint mcp index codegraph` (không kiểm người/agent: người gọi tự kiểm): chạy
// `codegraph init -y` (gốc root chưa có chỉ mục) hoặc `codegraph sync` với cwd = root, rồi thêm `.codegraph/` vào
// git info/exclude. Đầu ra của codegraph chuyển sang out.
func CodegraphInit(ctx context.Context, root string, out io.Writer) (CodegraphInitResult, error) {
	var res CodegraphInitResult
	svc, ok := mcpcat.Lookup("codegraph")
	if !ok {
		return res, errors.New("danh mục không có codegraph")
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return res, err
	}
	reg, ok := m.Services[svc.ID]
	if !ok {
		return res, errors.New("codegraph chưa đăng ký trên máy này (bint mcp add codegraph)")
	}
	secrets, err := config.ReadSecrets(config.SecretsPath(m))
	if err != nil {
		return res, fmt.Errorf("đọc secrets.env: %w", err)
	}
	env, _, _ := Env(svc, reg, secrets)
	path, err := CodegraphIndexBinary(svc, reg)
	if err != nil {
		return res, err
	}
	args := []string{"sync"}
	if _, err := os.Stat(filepath.Join(root, CodegraphDB)); err != nil {
		args = []string{"init", "-y"}
	}
	if out == nil {
		out = io.Discard
	}
	warning := CodegraphVersionWarning(ctx, path, reg, env)
	if warning != "" {
		fmt.Fprintln(out, "cảnh báo: "+warning)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = root, env, out, out
	if err := cmd.Run(); err != nil {
		return res, fmt.Errorf("codegraph %s: %w", strings.Join(args, " "), err)
	}
	file, added, err := EnsureGitExclude(ctx, root, CodegraphDir+"/")
	if err != nil {
		return res, fmt.Errorf("cập nhật git info/exclude: %w", err)
	}
	return CodegraphInitResult{Action: args[0], Binary: path, ExcludeFile: file, ExcludeAdded: added, Warning: warning}, nil
}

// CodegraphVersionWarning: index dùng binary trên PATH thay cho lệnh bọc đã đăng ký (npx/node...) thì so `<bin> --version` với
// phiên bản đã ghim (reg.Version); khác hoặc không đọc được thì trả cảnh báo. Lệnh đã đăng ký chính là binary thì không cảnh báo.
func CodegraphVersionWarning(ctx context.Context, bin string, reg config.Registration, env []string) string {
	if reg.Command == "" || bin == reg.Command || reg.Version == "" {
		return ""
	}
	if p, err := exec.LookPath(reg.Command); err == nil && p == bin {
		return ""
	}
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	vc := exec.CommandContext(vctx, bin, "--version")
	vc.Env = env
	out, err := vc.Output()
	got := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
	if err == nil && got == strings.TrimPrefix(reg.Version, "v") {
		return ""
	}
	if err != nil || got == "" {
		got = "không rõ"
	}
	return fmt.Sprintf("index chạy %s (phiên bản %s) thay cho lệnh đã đăng ký %q (phiên bản ghim %s): chỉ mục có thể do bản khác với server MCP dựng",
		bin, got, reg.Command, reg.Version)
}
