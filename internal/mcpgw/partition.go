package mcpgw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
)

// revision là băm của cấu hình đăng ký và danh mục (không gồm giá trị secrets): đổi lệnh, đối số, URL, phiên bản hay
// tên biến thì cache và phân vùng cũ không dùng lại.
func revision(svc mcpcat.Service, reg config.Registration) string {
	env := slices.Clone(reg.EnvNames)
	slices.Sort(env)
	b, _ := json.Marshal([]any{svc.ID, svc.Version, svc.Share, svc.Cwd, reg.Command, reg.Args, reg.URL, reg.Version, env})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

type callInfo struct {
	At       time.Time
	Tool     string
	Outcome  string
	Duration time.Duration
}

// partition là một tập phiên dùng chung một upstream (khoá theo chính sách chia sẻ của danh mục, mục 5.3).
type partition struct {
	d           *Daemon
	key         string
	svc         mcpcat.Service
	reg         config.Registration
	secretsPath string
	rev         string
	root        string // gốc worktree ("" nếu phân vùng không gắn worktree)
	stateRoot   string // thư mục trạng thái của repo chứa worktree (cwd kiểu state)
	label       string

	startMu sync.Mutex // tuần tự hoá khởi động upstream

	mu         sync.Mutex
	up         upstream
	genNo      int
	deaths     []time.Time
	limited    bool
	sessions   map[string]struct{}
	inflight   int
	idleTimer  *time.Timer
	idleSeq    int
	lastCall   callInfo
	lastErr    string
	secretVals []string
	restarts   int // số lần khởi động lại sau khi chết
	removed    bool

	gmu    sync.Mutex
	groups map[string]*groupState
}

func (p *partition) idleTimeout() time.Duration {
	if p.reg.IdleTimeout != "" {
		if d, err := time.ParseDuration(p.reg.IdleTimeout); err == nil && d > 0 {
			return d
		}
	}
	return p.d.opts.IdleTimeout
}

func (p *partition) maxRestarts() int {
	if p.reg.MaxRestarts > 0 {
		return p.reg.MaxRestarts
	}
	return p.d.opts.MaxRestarts
}

// attach đăng ký một phiên dùng phân vùng (chặn idle).
func (p *partition) attach(sessID string) {
	p.mu.Lock()
	p.sessions[sessID] = struct{}{}
	p.cancelIdleLocked()
	p.mu.Unlock()
}

// detach gỡ phiên: nhả khoá exclusive của phiên; phân vùng `session` thì dừng ngay, còn lại chờ idle.
func (p *partition) detach(sessID string) {
	p.unlockExclusive(sessID)
	p.mu.Lock()
	delete(p.sessions, sessID)
	if p.svc.Share == mcpcat.ShareSession {
		up := p.up
		p.up = nil
		p.removed = true
		p.mu.Unlock()
		if up != nil {
			up.stop()
		}
		p.d.dropPartition(p)
		return
	}
	p.armIdleLocked()
	p.mu.Unlock()
}

func (p *partition) cancelIdleLocked() {
	if p.idleTimer != nil {
		p.idleTimer.Stop()
		p.idleTimer = nil
	}
	p.idleSeq++
}

// armIdleLocked đặt hẹn giờ dừng upstream khi không còn phiên và lời gọi dang dở.
func (p *partition) armIdleLocked() {
	if p.up == nil || p.up.pid() == 0 || p.inflight > 0 || len(p.sessions) > 0 {
		return
	}
	p.cancelIdleLocked()
	seq := p.idleSeq
	p.idleTimer = time.AfterFunc(p.idleTimeout(), func() {
		p.mu.Lock()
		if p.idleSeq != seq || p.inflight > 0 || len(p.sessions) > 0 || p.up == nil {
			p.mu.Unlock()
			return
		}
		up := p.up
		p.up = nil
		p.idleTimer = nil
		p.mu.Unlock()
		up.stop()
	})
}

// release kết thúc một lời gọi dang dở.
func (p *partition) release() {
	p.mu.Lock()
	p.inflight--
	p.armIdleLocked()
	p.mu.Unlock()
}

// acquire trả upstream sẵn sàng (khởi động lười, generation mới nếu chết) và tính một lời gọi dang dở; người gọi phải
// gọi release. Vượt max_restarts trong cửa sổ thì trả CodeRestartLimit tới khi restart service.
func (p *partition) acquire(ctx context.Context) (upstream, error) { return p.acquireWith(ctx, true) }

// acquireLazy như acquire nhưng không ép handshake: upstream HTTP tự handshake (có retry cho tool chỉ đọc) trong
// up.call; dùng cho tools/call và request chuyển tiếp. Upstream stdio đã handshake lúc khởi động nên không khác gì.
func (p *partition) acquireLazy(ctx context.Context) (upstream, error) {
	return p.acquireWith(ctx, false)
}

var errPartitionClosed = &rpcError{Code: CodeUpstream, Message: "phiên đã đóng"}

func (p *partition) acquireWith(ctx context.Context, handshake bool) (upstream, error) {
	if up, ok := p.tryUp(); ok {
		return p.ready(ctx, up, handshake)
	}
	p.startMu.Lock()
	defer p.startMu.Unlock()
	if up, ok := p.tryUp(); ok {
		return p.ready(ctx, up, handshake)
	}
	p.mu.Lock()
	if p.removed {
		p.mu.Unlock()
		return nil, errPartitionClosed
	}
	if p.limited {
		p.mu.Unlock()
		return nil, p.limitErr()
	}
	cut := time.Now().Add(-p.d.opts.RestartWindow)
	p.deaths = slices.DeleteFunc(p.deaths, func(t time.Time) bool { return t.Before(cut) })
	if len(p.deaths) > p.maxRestarts() {
		p.limited = true
		p.mu.Unlock()
		return nil, p.limitErr()
	}
	p.genNo++
	gen := p.genNo
	if len(p.deaths) > 0 {
		p.restarts++
	}
	p.mu.Unlock()

	up, err := p.spawn(ctx, gen)
	if err != nil {
		var ce countedErr
		if !errors.As(err, &ce) {
			p.recordDeath(err.Error())
		}
		return nil, &rpcError{Code: CodeUpstream, Message: fmt.Sprintf("không khởi động được %s: %v", p.svc.ID, err)}
	}
	p.mu.Lock()
	if p.removed { // phiên bị đóng (detach) trong lúc spawn: không gắn upstream vào phân vùng mồ côi
		p.mu.Unlock()
		up.stop()
		return nil, errPartitionClosed
	}
	p.up = up
	// Khởi động lại thành công: lỗi của lần chết trước không còn là trạng thái hiện tại (bộ đếm deaths/restarts vẫn giữ để
	// áp giới hạn; lần chết sau ghi lại lastErr nên limitErr vẫn có lỗi gần nhất).
	p.lastErr = ""
	p.inflight++
	p.cancelIdleLocked()
	p.mu.Unlock()
	return p.ready(ctx, up, handshake)
}

func (p *partition) limitErr() error {
	p.mu.Lock()
	n, last := len(p.deaths), p.lastErr
	p.mu.Unlock()
	msg := fmt.Sprintf("%s đã chết %d lần trong %s (giới hạn khởi động lại %d); chạy `bint mcp gateway restart %s` để thử lại",
		p.svc.ID, n, p.d.opts.RestartWindow, p.maxRestarts(), p.svc.ID)
	if last != "" {
		msg += " (lỗi gần nhất: " + last + ")"
	}
	return &rpcError{Code: CodeRestartLimit, Message: msg}
}

// tryUp trả upstream hiện tại nếu còn sống, đồng thời tính lời gọi dang dở.
func (p *partition) tryUp() (upstream, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.up != nil && p.up.alive() {
		p.inflight++
		p.cancelIdleLocked()
		return p.up, true
	}
	return nil, false
}

// ready hoàn tất handshake (http làm lười) cho upstream đã tính lời gọi dang dở. handshake=false: bỏ qua ensure để
// up.call tự lo (retry cho tool chỉ đọc); chỉ ghi cache khi upstream đã có kết quả initialize.
func (p *partition) ready(ctx context.Context, up upstream, handshake bool) (upstream, error) {
	if !handshake {
		if init, _ := up.hello(); len(init) > 0 {
			_ = p.cacheHello(up)
		}
		return up, nil
	}
	if err := up.ensure(ctx); err != nil {
		p.release()
		return nil, &rpcError{Code: CodeUpstream, Message: fmt.Sprintf("%s: %v", p.svc.ID, err)}
	}
	if err := p.cacheHello(up); err != nil {
		p.release()
		return nil, &rpcError{Code: CodeUpstream, Message: err.Error()}
	}
	return up, nil
}

func (p *partition) cacheHello(up upstream) error {
	init, tools := up.hello()
	if len(init) == 0 {
		return errors.New("upstream chưa có kết quả initialize")
	}
	p.d.cachePut(p.svc.ID, p.rev, init, tools)
	return nil
}

// spawn tạo upstream mới cho generation gen.
func (p *partition) spawn(ctx context.Context, gen int) (upstream, error) {
	if p.svc.Transport == mcpcat.HTTP {
		if p.reg.URL == "" {
			return nil, errors.New("đăng ký thiếu url")
		}
		h := newHTTPUp(p.reg.URL, p.d.opts.Version, p.d.opts)
		h.gen = gen
		return h, nil // handshake làm lười ở ensure (có retry cho tool chỉ đọc)
	}
	secrets, err := config.ReadSecrets(p.secretsPath)
	if err != nil {
		return nil, fmt.Errorf("đọc secrets.env: %w", err)
	}
	env, missing, secretVals := Env(p.svc, p.reg, secrets)
	if len(missing) > 0 {
		return nil, fmt.Errorf("thiếu biến credential trong secrets.env: %s", strings.Join(missing, ", "))
	}
	p.mu.Lock()
	p.secretVals = secretVals
	p.mu.Unlock()
	path, err := exec.LookPath(p.reg.Command)
	if err != nil {
		hint := ""
		if p.svc.Install != "" {
			hint = "; cài: " + p.svc.Install
		}
		return nil, fmt.Errorf("không tìm thấy lệnh %q trên PATH%s", p.reg.Command, hint)
	}
	var dir string
	switch p.svc.Cwd {
	case mcpcat.CwdWorktree:
		if p.root == "" {
			return nil, errors.New("service cần worktree git")
		}
		dir = p.root
	case mcpcat.CwdState:
		if p.root == "" || p.stateRoot == "" {
			return nil, errors.New("service cần worktree git và thư mục trạng thái")
		}
		if dir, err = StateCwd(p.stateRoot, p.svc.ID, p.root); err != nil {
			return nil, err
		}
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.d.opts.StartTimeout)
	defer cancel()
	return startStdio(sctx, stdioCfg{gen: gen, path: path, args: p.reg.Args, dir: dir, env: env, root: p.root,
		maxFrame: p.d.opts.MaxFrame, queue: p.d.opts.WriteQueue, version: p.d.opts.Version, onDead: p.onDead})
}

// onDead nhận thông báo upstream stdio chết hoặc bị dừng.
func (p *partition) onDead(u *stdioUp, cause error) {
	p.mu.Lock()
	if p.up == upstream(u) {
		p.up = nil
	}
	secrets := p.secretVals
	p.mu.Unlock()
	if errors.Is(cause, errStopped) {
		return
	}
	msg := cause.Error()
	if se := strings.TrimSpace(u.stderr.Lines()); se != "" {
		msg += " [stderr: " + se + "]"
	}
	p.recordDeath(ScrubSecrets(msg, secrets, 300))
}

func (p *partition) recordDeath(msg string) {
	p.mu.Lock()
	p.deaths = append(p.deaths, time.Now())
	p.lastErr = msg
	p.mu.Unlock()
	p.d.logf("%s (%s): %s", p.svc.ID, p.label, msg)
}

// note ghi lời gọi gần nhất.
func (p *partition) note(tool, outcome string, dur time.Duration) {
	p.mu.Lock()
	p.lastCall = callInfo{At: time.Now(), Tool: tool, Outcome: outcome, Duration: dur}
	p.mu.Unlock()
}

// reset dừng upstream và xoá trạng thái lỗi, bộ đếm restart (bint mcp gateway restart); trả true nếu có upstream chạy.
func (p *partition) reset() bool {
	p.mu.Lock()
	up := p.up
	p.up = nil
	p.deaths, p.limited, p.lastErr, p.restarts = nil, false, "", 0
	p.cancelIdleLocked()
	p.mu.Unlock()
	if up != nil {
		up.stop()
		return true
	}
	return false
}

// shutdown dừng upstream khi daemon tắt.
func (p *partition) shutdown() {
	p.mu.Lock()
	up := p.up
	p.up = nil
	p.cancelIdleLocked()
	p.mu.Unlock()
	if up != nil {
		up.stop()
	}
}
