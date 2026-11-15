package mcpgw

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"bint/internal/mcpcat"
	"bint/internal/pathutil"
	"bint/internal/state"
)

// rootArgs: service chia sẻ theo worktree mà tool nhận gốc repo qua một tham số; giá trị là tên tham số. Lời gọi có tham số
// này nằm trong worktree tác vụ của Bint được định tuyến sang phân vùng của worktree đó (docs/mcp-gateway-plan.md §5.5).
var rootArgs = map[string]string{"codegraph": "projectPath"}

// samePath giải alias/symlink trước khi so sánh; Windows không phân biệt hoa thường.
func samePath(a, b string) bool {
	a, b = pathutil.BestEffort(a), pathutil.BestEffort(b)
	if isWindows {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// canonPath giải symlink (và trên Windows tên ngắn 8.3) để so đường dẫn: thư mục trạng thái có thể đi qua symlink hoặc
// dạng ngắn (TEMP của runner CI là C:\Users\RUNNER~1\..., /var → /private/var trên macOS) trong khi đường dẫn từ git hay
// tham số của tool là dạng thật. Không giải được (chưa tồn tại) thì chỉ Clean.
func canonPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// underDir cho biết p nằm dưới dir (không tính chính dir).
func underDir(dir, p string) bool {
	rel, err := filepath.Rel(pathutil.BestEffort(dir), pathutil.BestEffort(p))
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(filepath.ToSlash(rel), "../")
}

// routeFor chọn phân vùng cho một tools/call: phân vùng của phiên, trừ khi service có tham số gốc (rootArgs), tham số đó là
// đường dẫn tuyệt đối nằm trong một worktree tác vụ của cùng repo với phiên (dưới <state>/worktrees) và khác gốc phiên —
// khi đó là phân vùng của worktree ấy (upstream có cwd = worktree), để tiến trình của gốc không mở và giữ khoá chỉ mục của
// worktree. Không giải được hoặc không thoả điều kiện nào thì dùng phân vùng của phiên (hành vi cũ).
func (d *Daemon) routeFor(ctx context.Context, s *session, params map[string]json.RawMessage) *partition {
	arg := rootArgs[s.svc.ID]
	if arg == "" || s.svc.Share != mcpcat.ShareWorktree || s.p.stateRoot == "" || s.root == "" {
		return s.p
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(params["arguments"], &args) != nil {
		return s.p
	}
	var pp string
	if json.Unmarshal(args[arg], &pp) != nil || pp == "" || !filepath.IsAbs(pp) {
		return s.p
	}
	// Kiểm chữ trước (không tốn lệnh git): phải nằm dưới thư mục worktrees của thư mục trạng thái phiên.
	ppc, root := canonPath(pp), canonPath(s.root)
	if samePath(ppc, root) || !underDir(canonPath(state.At("", s.p.stateRoot).Worktrees), ppc) {
		return s.p
	}
	ri, err := resolveRepo(ctx, pp)
	if err != nil || ri.root == "" || samePath(canonPath(ri.root), root) || !samePath(canonPath(ri.stateRoot), canonPath(s.p.stateRoot)) ||
		!underDir(canonPath(state.At("", ri.stateRoot).Worktrees), canonPath(ri.root)) {
		return s.p
	}
	return d.partitionFor(partSpec{svc: s.svc, reg: s.p.reg, secretsPath: s.p.secretsPath, rev: s.p.rev, ri: ri}, s.id)
}

// ReleaseWorktree dừng upstream và gỡ mọi phân vùng có gốc là worktree wt (đường dẫn tuyệt đối), đóng các phiên gắn vào
// chúng; trả số phân vùng đã nhả. Phân vùng của worktree khác giữ nguyên.
func (d *Daemon) ReleaseWorktree(wt string) int {
	wt = canonPath(wt)
	d.mu.Lock()
	var ps []*partition
	for _, p := range d.parts {
		if p.root != "" && samePath(canonPath(p.root), wt) {
			ps = append(ps, p)
		}
	}
	var ss []*session
	for _, s := range d.sessions {
		for _, p := range ps {
			if s.p == p {
				ss = append(ss, s)
			}
		}
	}
	d.mu.Unlock()
	for _, s := range ss {
		d.closeSession(s)
	}
	for _, p := range ps {
		p.mu.Lock()
		up := p.up
		p.up = nil
		p.removed = true
		p.cancelIdleLocked()
		p.mu.Unlock()
		if up != nil {
			up.stop()
		}
		d.dropPartition(p)
	}
	return len(ps)
}
