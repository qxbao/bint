package mcpgw

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bint/internal/testutil"
)

// Định tuyến projectPath sang phân vùng của worktree tác vụ và /admin/release (docs/mcp-gateway-plan.md §5.5).

// taskWorktree tạo worktree git của repo ngay dưới thư mục worktrees của thư mục trạng thái (như Bint làm).
func taskWorktree(t *testing.T, repo, id string) string {
	t.Helper()
	paths := markInit(t, repo)
	p := filepath.Join(paths.Worktrees, id)
	if err := os.MkdirAll(paths.Worktrees, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "bint/"+id, p)
	return evalDir(t, p)
}

func callPath(t *testing.T, c *client, id int, projectPath any) toolOut {
	t.Helper()
	args := map[string]any{}
	if projectPath != nil {
		args["projectPath"] = projectPath
	}
	m, out := c.call(id, "codegraph_status", args)
	if len(m.Error) > 0 {
		t.Fatalf("call lỗi: %s", m.Error)
	}
	return out
}

func samePathT(a, b string) bool { return samePath(evalDir0(a), evalDir0(b)) }

func evalDir0(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestRouteProjectPathToWorktreePartition(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	wt := taskWorktree(t, repo, "T1")
	other := evalDir(t, newRepo(t, false))
	otherWt := taskWorktree(t, other, "T2") // worktree tác vụ của repo KHÁC

	c := h.open("codegraph", worktreeHdr(repo))
	base := callPath(t, c, 1, nil)
	if !samePathT(base.Cwd, repo) {
		t.Fatalf("cwd phiên %q, muốn %q", base.Cwd, repo)
	}

	routed := callPath(t, c, 2, wt)
	if routed.PID == base.PID || !samePathT(routed.Cwd, wt) {
		t.Fatalf("projectPath=worktree phải chạy trên upstream riêng có cwd=worktree: pid %d/%d cwd %q", routed.PID, base.PID, routed.Cwd)
	}
	if again := callPath(t, c, 3, wt); again.PID != routed.PID {
		t.Fatalf("hai lời gọi cùng worktree phải chung phân vùng: %d vs %d", again.PID, routed.PID)
	}
	// Phiên mở thẳng ở worktree dùng chung phân vùng với lời gọi định tuyến.
	direct := h.open("codegraph", worktreeHdr(wt))
	if o := callPath(t, direct, 1, nil); o.PID != routed.PID {
		t.Fatalf("phiên ở worktree phải chung phân vùng định tuyến: %d vs %d", o.PID, routed.PID)
	}

	// Không định tuyến: trỏ gốc phiên, đường dẫn ngoài thư mục worktrees, worktree của repo khác, không phải chuỗi.
	for name, pp := range map[string]any{"gốc phiên": repo, "ngoài worktrees": t.TempDir(), "repo khác": otherWt, "không phải chuỗi": 5,
		"tương đối": "wt", "thư mục con của gốc": filepath.Join(repo, "x")} {
		if o := callPath(t, c, 10, pp); o.PID != base.PID || !samePathT(o.Cwd, repo) {
			t.Errorf("%s: phải dùng phân vùng của phiên (pid %d, cwd %q)", name, o.PID, o.Cwd)
		}
	}
}

func TestAdminReleaseWorktree(t *testing.T) {
	h := newHarness(t, nil)
	h.register("codegraph")
	repo := evalDir(t, newRepo(t, false))
	wt1 := taskWorktree(t, repo, "T1")
	wt2 := taskWorktree(t, repo, "T2")
	c := h.open("codegraph", worktreeHdr(repo))
	p1 := callPath(t, c, 1, wt1)
	p2 := callPath(t, c, 2, wt2)
	d1 := h.open("codegraph", worktreeHdr(wt1)) // phiên gắn vào phân vùng wt1
	if o := callPath(t, d1, 1, nil); o.PID != p1.PID {
		t.Fatalf("phiên wt1 pid %d, muốn %d", o.PID, p1.PID)
	}
	if p1.PID == p2.PID {
		t.Fatal("hai worktree phải có upstream riêng")
	}

	cl, err := Connect(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := cl.ReleaseWorktree(context.Background(), wt1)
	if err != nil || res.Released != 1 {
		t.Fatalf("release: %+v %v", res, err)
	}
	eventually(t, "upstream của wt1 dừng", 5*time.Second, func() bool { return !processAlive(p1.PID) })
	// Phân vùng của worktree khác giữ nguyên upstream và phiên của phiên chính vẫn gọi được.
	if o := callPath(t, c, 3, wt2); o.PID != p2.PID {
		t.Fatalf("phân vùng wt2 bị ảnh hưởng: pid %d, muốn %d", o.PID, p2.PID)
	}
	if o := callPath(t, c, 4, nil); !samePathT(o.Cwd, repo) {
		t.Fatalf("phiên gốc không gọi được sau release: %+v", o)
	}
	// Gọi lại wt1 tạo phân vùng mới (upstream mới).
	if o := callPath(t, c, 5, wt1); o.PID == p1.PID {
		t.Fatal("sau release phải có upstream mới")
	}
	// Nhả lần nữa wt không còn phân vùng: 0; tham số sai: 400.
	if res, err := cl.ReleaseWorktree(context.Background(), filepath.Join(t.TempDir(), "khong-co")); err != nil || res.Released != 0 {
		t.Fatalf("release wt lạ: %+v %v", res, err)
	}
	resp, _ := h.do(http.MethodPost, "/admin/release?worktree=tuong-doi", map[string]string{"Authorization": "Bearer " + h.d.Token()}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("worktree tương đối: %d", resp.StatusCode)
	}
}
