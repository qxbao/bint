// Package mcpgw là daemon gateway MCP dùng chung (docs/mcp-gateway-plan.md mục 6): một bản cho mỗi user, Streamable
// HTTP trên loopback, ghép nhiều phiên client vào một upstream stdio (đổi id JSON-RPC, cache initialize/tools/list),
// phân vùng tiến trình theo chính sách chia sẻ của danh mục mcpcat, vòng đời lười, ghi nhận lời gọi vào spool.
//
// Viết tay như internal/mcpserver (S5): proxy chuyển nguyên JSON-RPC, không parse rồi serialize lại bằng SDK có kiểu.
// Gói không phụ thuộc store hay SQLite (daemon không mở CSDL) và không phụ thuộc internal/app; hook không bao giờ
// gọi gói này (daemon chỉ do người hoặc shim M3 khởi động).
package mcpgw

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/mcpcat"
	"bint/internal/state"
)

// ErrAlreadyRunning: đã có daemon giữ khoá trong thư mục gateway.
var ErrAlreadyRunning = errors.New("gateway đã chạy (một bản cho mỗi user)")

type cacheEntry struct{ init, tools []byte }

// Daemon là một gateway đang chạy.
type Daemon struct {
	opts    Options
	dir     string
	token   string
	info    Info
	lock    *os.File
	ln      net.Listener
	srv     *http.Server
	started time.Time

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*session
	parts    map[string]*partition
	cache    map[string]cacheEntry

	dropped atomic.Int64 // sự kiện mcp.called không ghi được
	logMu   sync.Mutex

	closeOnce sync.Once
	done      chan struct{}
}

// Start lấy khoá, mở listener loopback cổng động, ghi gateway.json (0600) và bắt đầu phục vụ. Daemon thứ hai cùng thư mục
// nhận ErrAlreadyRunning.
func Start(opts Options) (*Daemon, error) {
	opts.fill()
	dir := opts.Dir
	if dir == "" {
		var err error
		if dir, err = Dir(); err != nil {
			return nil, err
		}
	}
	if err := checkLoopbackAddr(opts.Addr); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lf, err := os.OpenFile(filepath.Join(dir, LockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	busy, err := tryLock(lf)
	if err != nil {
		lf.Close()
		return nil, fmt.Errorf("khoá %s: %w", LockFileName, err)
	}
	if busy {
		lf.Close()
		if i, rerr := ReadInfo(dir); rerr == nil {
			return nil, fmt.Errorf("%w: PID %d, cổng %d", ErrAlreadyRunning, i.PID, i.Port)
		}
		return nil, ErrAlreadyRunning
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		lf.Close()
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		ln.Close()
		lf.Close()
		return nil, err
	}
	d := &Daemon{opts: opts, dir: dir, token: token, lock: lf, ln: ln, started: time.Now(),
		sessions: map[string]*session{}, parts: map[string]*partition{}, cache: map[string]cacheEntry{},
		done: make(chan struct{})}
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.info = Info{PID: os.Getpid(), Port: ln.Addr().(*net.TCPAddr).Port, Token: token, StartedAt: d.started.UTC(), Version: opts.Version}
	if err := writeInfo(dir, d.info); err != nil {
		ln.Close()
		lf.Close()
		return nil, err
	}
	d.srv = &http.Server{Handler: d.handler(), BaseContext: func(net.Listener) context.Context { return d.ctx },
		ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = d.srv.Serve(ln) }()
	go d.janitor()
	d.logf("gateway chạy: PID %d, cổng %d", d.info.PID, d.info.Port)
	return d, nil
}

// Run chạy daemon tới khi ctx kết thúc hoặc có yêu cầu dừng (/admin/stop), rồi tắt êm.
func Run(ctx context.Context, opts Options) error {
	d, err := Start(opts)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-d.Done():
	}
	d.Close()
	return nil
}

func checkLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("địa chỉ nghe %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("địa chỉ nghe %q: chỉ chấp nhận IP loopback", addr)
	}
	return nil
}

// Info trả nội dung gateway.json của daemon này.
func (d *Daemon) Info() Info { return d.info }

// URL là địa chỉ gốc của daemon.
func (d *Daemon) URL() string { return d.info.URL() }

// Token là bearer token.
func (d *Daemon) Token() string { return d.token }

// Done đóng khi daemon đã tắt hẳn.
func (d *Daemon) Done() <-chan struct{} { return d.done }

// Close tắt êm: dừng nhận request, đóng phiên, kill upstream (cả cây), xoá gateway.json, nhả khoá. An toàn gọi nhiều lần.
func (d *Daemon) Close() {
	d.closeOnce.Do(func() {
		d.cancel()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = d.srv.Shutdown(sctx)
		cancel()
		_ = d.srv.Close()
		d.mu.Lock()
		ss := make([]*session, 0, len(d.sessions))
		for _, s := range d.sessions {
			ss = append(ss, s)
		}
		ps := make([]*partition, 0, len(d.parts))
		for _, p := range d.parts {
			ps = append(ps, p)
		}
		d.mu.Unlock()
		for _, s := range ss {
			d.closeSession(s)
		}
		for _, p := range ps {
			p.shutdown()
		}
		if i, err := ReadInfo(d.dir); err == nil && i.PID == d.info.PID && i.Token == d.token {
			_ = os.Remove(filepath.Join(d.dir, InfoFileName))
		}
		_ = d.lock.Close()
		close(d.done)
	})
}

// requestStop tắt daemon sau khi trả lời request hiện tại.
func (d *Daemon) requestStop() { go d.Close() }

func (d *Daemon) logf(format string, a ...any) {
	d.logMu.Lock()
	defer d.logMu.Unlock()
	fmt.Fprintf(d.opts.Log, time.Now().Format(time.RFC3339)+" "+format+"\n", a...)
}

func (d *Daemon) cachePut(svc, rev string, init, tools []byte) {
	d.mu.Lock()
	k := svc + "|" + rev
	_, had := d.cache[k]
	if !had {
		d.cache[k] = cacheEntry{init: init, tools: tools}
	}
	d.mu.Unlock()
	if !had {
		d.cacheStore(svc, rev, cacheEntry{init: init, tools: tools})
	}
}

// cacheGet tìm cache initialize/tools/list theo (service, revision): bộ nhớ rồi tới đĩa (tools-cache/, sống qua lần khởi
// động lại daemon nên không phải spawn upstream chỉ để lấy danh sách).
func (d *Daemon) cacheGet(svc, rev string) (cacheEntry, bool) {
	d.mu.Lock()
	e, ok := d.cache[svc+"|"+rev]
	d.mu.Unlock()
	if ok {
		return e, true
	}
	e, ok = d.cacheLoad(svc, rev)
	if !ok {
		return cacheEntry{}, false
	}
	d.mu.Lock()
	if cur, had := d.cache[svc+"|"+rev]; had {
		e = cur
	} else {
		d.cache[svc+"|"+rev] = e
	}
	d.mu.Unlock()
	return e, true
}

func (d *Daemon) dropPartition(p *partition) {
	d.mu.Lock()
	if d.parts[p.key] == p {
		delete(d.parts, p.key)
	}
	d.mu.Unlock()
}

// closeSession đóng phiên: nhả khoá, giảm đếm phiên của phân vùng, đóng luồng SSE.
func (d *Daemon) closeSession(s *session) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.done)
	s.mu.Unlock()
	d.mu.Lock()
	delete(d.sessions, s.id)
	d.mu.Unlock()
	s.p.detach(s.id)
}

// janitor đóng phiên không hoạt động quá SessionTTL (client chết không gửi DELETE).
func (d *Daemon) janitor() {
	every := d.opts.SessionTTL / 4
	if every < 50*time.Millisecond {
		every = 50 * time.Millisecond
	}
	if every > time.Minute {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
			cut := time.Now().Add(-d.opts.SessionTTL)
			d.mu.Lock()
			var old []*session
			for _, s := range d.sessions {
				s.mu.Lock()
				if s.lastActive.Before(cut) && !s.sseOpen {
					old = append(old, s)
				}
				s.mu.Unlock()
			}
			d.mu.Unlock()
			for _, s := range old {
				d.closeSession(s)
			}
		}
	}
}

// ---- tìm phân vùng ----

// repoInfo là kết quả giải worktree → repo → thư mục trạng thái.
type repoInfo struct {
	root      string
	stateRoot string
	spool     string // rỗng nếu repo chưa init Bint hoặc không giải được
	runtime   string // thư mục runtime của repo ("" nếu không giải được): chứa bản ghi lượt chạy (binding.go)
	cfg       config.Config
	cfgOK     bool // đọc được cấu hình dự án (cfg.MCP có nghĩa)
	// runCfg là cấu hình dùng giải quyền cho phiên của một lượt bint run: đọc từ worktree CHÍNH trước (như cấu hình của
	// Runner, nơi tính allowlist của bước), nên bước trước sửa .bint/config.yml trong worktree tác vụ không đổi được quyền
	// của bước sau.
	runCfg   config.Config
	runCfgOK bool
}

// resolveRepo giải worktree dir (gitrepo + config + state, không mở SQLite).
func resolveRepo(ctx context.Context, dir string) (repoInfo, error) {
	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		return repoInfo{}, err
	}
	ri := repoInfo{root: repo.Toplevel}
	cfg, _, err := config.LoadFirst(repo.ConfigRoots(), false) // cùng cách chọn tệp cấu hình với app.OpenWorkspace
	if err != nil {
		return ri, nil
	}
	ri.cfg, ri.cfgOK = cfg, true
	ri.runCfg, ri.runCfgOK = cfg, true
	if roots := repo.ConfigRoots(); len(roots) > 1 {
		main := slices.Clone(roots)
		slices.Reverse(main)
		if mc, _, err := config.LoadFirst(main, false); err == nil {
			ri.runCfg = mc
		}
	}
	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		return ri, nil
	}
	ri.stateRoot = paths.Root
	ri.runtime = paths.Runtime
	if paths.Initialized() {
		ri.spool = paths.Spool
	}
	return ri, nil
}

// partSpec gom đầu vào dựng phân vùng.
type partSpec struct {
	svc         mcpcat.Service
	reg         config.Registration
	secretsPath string
	rev         string
	ri          repoInfo
}

// partitionFor trả phân vùng của (service, revision, worktree, phiên) theo chính sách chia sẻ (mục 5.3), tạo nếu chưa có.
func (d *Daemon) partitionFor(sp partSpec, sessID string) *partition {
	key := sp.svc.ID + "|" + sp.rev
	root := ""
	var label string
	switch sp.svc.Share {
	case mcpcat.ShareWorktree:
		key += "|" + sp.ri.root
		root, label = sp.ri.root, sp.ri.root
	case mcpcat.ShareSession:
		key += "|" + sessID
		root, label = sp.ri.root, "phiên "+sessID[:8]
	case mcpcat.ShareRemote:
		label = "remote"
	default: // machine: không phụ thuộc worktree, roots/list trống, không cwd
		label = "machine"
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if p := d.parts[key]; p != nil {
		return p
	}
	p := &partition{d: d, key: key, svc: sp.svc, reg: sp.reg, secretsPath: sp.secretsPath, rev: sp.rev,
		root: root, stateRoot: sp.ri.stateRoot, label: label, sessions: map[string]struct{}{}}
	d.parts[key] = p
	return p
}

// Status trả ảnh chụp trạng thái daemon.
func (d *Daemon) Status() Status {
	d.mu.Lock()
	ps := make([]*partition, 0, len(d.parts))
	for _, p := range d.parts {
		ps = append(ps, p)
	}
	nsess := len(d.sessions)
	d.mu.Unlock()
	sort.Slice(ps, func(i, j int) bool { return ps[i].key < ps[j].key })
	st := Status{PID: d.info.PID, Port: d.info.Port, Version: d.opts.Version, StartedAt: d.started.UTC(),
		UptimeSec: int64(time.Since(d.started).Seconds()), Sessions: nsess, DroppedEvents: d.dropped.Load(),
		Upstreams: []UpstreamStatus{}}
	for _, p := range ps {
		st.Upstreams = append(st.Upstreams, p.status())
	}
	return st
}

// Restart dừng mọi phân vùng của service và xoá trạng thái lỗi; trả số phân vùng có upstream đang chạy.
func (d *Daemon) Restart(service string) int {
	d.mu.Lock()
	var ps []*partition
	for _, p := range d.parts {
		if p.svc.ID == service {
			ps = append(ps, p)
		}
	}
	d.mu.Unlock()
	n := 0
	for _, p := range ps {
		if p.reset() {
			n++
		}
	}
	return n
}

// Log trả stderr gần nhất (đã che credential) của mọi phân vùng thuộc service, sắp theo khoá phân vùng. Service lạ hoặc
// chưa có phân vùng thì trả danh sách rỗng (không phải nil, để JSON ra []).
func (d *Daemon) Log(service string) []PartitionLog {
	d.mu.Lock()
	var ps []*partition
	for _, p := range d.parts {
		if p.svc.ID == service {
			ps = append(ps, p)
		}
	}
	d.mu.Unlock()
	sort.Slice(ps, func(i, j int) bool { return ps[i].key < ps[j].key })
	out := []PartitionLog{}
	for _, p := range ps {
		out = append(out, p.log())
	}
	return out
}
