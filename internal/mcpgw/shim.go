package mcpgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"bint/internal/mcpcat"
)

// Shim là phía client của gateway trong `bint mcp serve` (docs/mcp-gateway-plan.md mục 7, D6): cửa vào gộp. Nó chuyển
// tiếp tool của các service được cấp với tên <tiền tố>_<tool> qua HTTP loopback tới daemon, tự khởi động daemon (tách
// rời, chờ có hạn) khi chưa chạy và nối lại khi daemon chết giữa phiên. Shim tối giản (không chứa logic upstream, không
// cache, không quyết định quyền): daemon mới là nơi giải role và lọc tool; shim chỉ tính tập service ứng viên để khỏi
// khởi động daemon vô ích. Shim implement mcpserver.Proxy.
type Shim struct {
	o   ShimOptions
	hc  *http.Client
	dir string

	ready chan struct{} // đóng khi lần kết nối đầu xong (thành công hay không)
	begin sync.Once
	rmu   sync.Mutex // single-flight cho nối lại

	mu     sync.Mutex
	info   Info
	gen    int
	daemon string // lý do không có daemon ("" nếu có)
	svcs   map[string]*shimSvc
	order  []string
	table  map[string]shimTool // tên tới agent → tool
	tools  []json.RawMessage
}

// ShimOptions cấu hình Shim.
type ShimOptions struct {
	// Dir là thư mục gateway ("" = Dir()).
	Dir string
	// Exe và StartArgs khởi động daemon tách rời (mặc định args: mcp gateway start --foreground). Env nil = kế thừa.
	Exe       string
	StartArgs []string
	Env       []string
	// Start thay hàm khởi động daemon (kiểm thử); nil = StartDetached(Exe, StartArgs).
	Start func(ctx context.Context, dir string) (Info, error)
	// Services là tập service ứng viên (đã lọc theo quyền ở phía gọi); rỗng thì Shim không làm gì.
	Services []string
	// Ngữ cảnh gửi lên daemon qua header X-Bint-*; role không bao giờ được gửi (daemon tự giải).
	Worktree, RunID, TaskID, Harness, SessionID string
	// StartWait là thời gian chờ tối đa để daemon lên (mặc định 5 giây); OpenWait cho giai đoạn mở phiên mọi service
	// (initialize + tools/list; service chưa chạy lần nào thì daemon phải spawn upstream một lần), mặc định 20 giây.
	StartWait, OpenWait time.Duration
	// Lookup tra danh mục (mặc định mcpcat.Lookup).
	Lookup func(id string) (mcpcat.Service, bool)
	// Log nhận cảnh báo (nil = bỏ).
	Log io.Writer
}

// Giá trị mặc định của Shim. Tổng thời gian initialize chờ (StartWait + OpenWait + 1 s ≈ 8 s) phải dưới timeout khởi
// động MCP của harness (Codex: startup_timeout_sec mặc định 10 s); quá thì harness bỏ cả server bint, mất luôn tool của
// Bint. Service chưa mở xong trong hạn được nêu trong instructions, phiên đó không có tool của nó.
const (
	DefaultShimStartWait = 3 * time.Second
	DefaultShimOpenWait  = 4 * time.Second
)

type shimSvc struct {
	svc  mcpcat.Service
	sid  string
	fail string // lý do không dùng được ("" nếu phiên đang mở)
	// denied: daemon từ chối vì role không được cấp (không phải lỗi: không báo trong instructions).
	denied bool
}

type shimTool struct {
	service  string
	upstream string
	readOnly bool
}

// NewShim tạo Shim; chưa kết nối gì cho tới Start hoặc Ready.
func NewShim(o ShimOptions) (*Shim, error) {
	dir := o.Dir
	if dir == "" {
		var err error
		if dir, err = Dir(); err != nil {
			return nil, err
		}
	}
	if o.StartWait <= 0 {
		o.StartWait = DefaultShimStartWait
	}
	if o.OpenWait <= 0 {
		o.OpenWait = DefaultShimOpenWait
	}
	if o.Lookup == nil {
		o.Lookup = mcpcat.Lookup
	}
	if o.Log == nil {
		o.Log = io.Discard
	}
	if len(o.StartArgs) == 0 {
		o.StartArgs = []string{"mcp", "gateway", "start", "--foreground"}
	}
	if o.Start == nil {
		exe, args, env := o.Exe, o.StartArgs, o.Env
		o.Start = func(ctx context.Context, dir string) (Info, error) {
			if exe == "" {
				return Info{}, errors.New("không biết binary Bint để khởi động gateway")
			}
			info, _, err := StartDetached(ctx, dir, exe, args, env)
			return info, err
		}
	}
	ids := slices.Clone(o.Services)
	sort.Strings(ids)
	return &Shim{o: o, dir: dir, hc: &http.Client{}, ready: make(chan struct{}), svcs: map[string]*shimSvc{}, order: slices.Compact(ids),
		table: map[string]shimTool{}}, nil
}

// Active cho biết Shim có service ứng viên (không có thì không bao giờ chạm gateway).
func (s *Shim) Active() bool { return len(s.order) > 0 }

// Start bắt đầu kết nối nền (idempotent); Ready chờ kết quả. Gọi sớm (khi dựng server) để việc khởi động daemon chạy
// song song với harness gửi initialize.
func (s *Shim) Start() {
	s.begin.Do(func() {
		if !s.Active() {
			close(s.ready)
			return
		}
		go func() {
			defer close(s.ready)
			s.connect()
		}()
	})
}

// Ready chờ lần kết nối đầu xong (có hạn riêng StartWait + OpenWait; ctx là cận trên).
func (s *Shim) Ready(ctx context.Context) {
	s.Start()
	t := time.NewTimer(s.o.StartWait + s.o.OpenWait + time.Second)
	defer t.Stop()
	select {
	case <-s.ready:
	case <-ctx.Done():
	case <-t.C:
	}
}

func (s *Shim) logf(format string, a ...any) {
	fmt.Fprintf(s.o.Log, "bint mcp serve: "+format+"\n", a...)
}

// connect: tìm hoặc khởi động daemon, mở phiên từng service và lấy tools/list.
func (s *Shim) connect() {
	ctx, cancel := context.WithTimeout(context.Background(), s.o.StartWait)
	info, err := s.discover(ctx)
	cancel()
	if err != nil {
		s.mu.Lock()
		s.daemon = err.Error()
		s.mu.Unlock()
		s.logf("không có gateway: %v", err)
		return
	}
	s.mu.Lock()
	s.info = info
	s.mu.Unlock()

	octx, ocancel := context.WithTimeout(context.Background(), s.o.OpenWait)
	defer ocancel()
	type opened struct {
		id    string
		sid   string
		tools []json.RawMessage
		err   error
	}
	out := make([]opened, len(s.order))
	var wg sync.WaitGroup
	for i, id := range s.order {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sid, tools, err := s.open(octx, info, id, true)
			out[i] = opened{id: id, sid: sid, tools: tools, err: err}
		}()
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range out {
		svc, _ := s.o.Lookup(o.id)
		st := &shimSvc{svc: svc, sid: o.sid}
		if o.err != nil {
			st.sid, st.fail = "", o.err.Error()
			var re *rpcError
			st.denied = errors.As(o.err, &re) && re.Code == CodeNotGranted
		}
		s.svcs[o.id] = st
		for _, raw := range o.tools {
			s.addTool(svc, raw)
		}
	}
	s.gen++
}

// addTool đổi tên một tool upstream sang tên tới agent và ghi vào bảng (s.mu đang giữ).
func (s *Shim) addTool(svc mcpcat.Service, raw json.RawMessage) {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	var name string
	if json.Unmarshal(doc["name"], &name) != nil || name == "" {
		return
	}
	agent := mcpcat.AgentToolName(svc, name)
	if _, dup := s.table[agent]; dup {
		return
	}
	if len(mcpcat.AgentPrefix)+len(agent) > mcpcat.MaxToolNameLen {
		s.logf("bỏ tool %s của %s: tên %q dài quá giới hạn %d ký tự của harness", name, svc.ID, mcpcat.AgentPrefix+agent, mcpcat.MaxToolNameLen)
		return
	}
	doc["name"] = marshalRaw(agent)
	renamed, err := json.Marshal(doc)
	if err != nil {
		return
	}
	s.table[agent] = shimTool{service: svc.ID, upstream: name, readOnly: svc.IsReadOnly(name)}
	s.tools = append(s.tools, renamed)
}

// discover trả Info của daemon đang sống; không có thì khởi động (tách rời) rồi chờ tới hạn của ctx.
func (s *Shim) discover(ctx context.Context) (Info, error) {
	if c, err := Connect(s.dir); err == nil {
		pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, serr := c.Status(pctx)
		cancel()
		if serr == nil {
			return c.Info, nil
		}
	}
	info, err := s.o.Start(ctx, s.dir)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Info{}, fmt.Errorf("gateway chưa lên sau %s (xem %s)", s.o.StartWait, filepath.Join(s.dir, LogFileName))
		}
		return Info{}, fmt.Errorf("khởi động gateway: %w", err)
	}
	return info, nil
}

// ---- mcpserver.Proxy ----

// Tools trả mô tả tool của các service được cấp (đã đổi tên).
func (s *Shim) Tools() []json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.tools)
}

// Owns cho biết tên tool thuộc Shim (có trong bảng đã đổi tên).
func (s *Shim) Owns(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.table[name]
	return ok
}

// Instructions nêu service lỗi (gateway không lên, service không mở được) kèm lý do; rỗng nếu mọi thứ bình thường.
func (s *Shim) Instructions() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Active() {
		return ""
	}
	if s.daemon != "" {
		return fmt.Sprintf("Gateway MCP không khởi động được (%s): công cụ của service ngoài (%s) KHÔNG dùng được trong phiên này; chỉ còn công cụ của Bint. Người dùng kiểm tra bằng `bint mcp gateway status` rồi mở lại phiên.",
			s.daemon, strings.Join(s.order, ", "))
	}
	var lines []string
	for _, id := range s.order {
		if st := s.svcs[id]; st != nil && st.fail != "" && !st.denied {
			lines = append(lines, fmt.Sprintf("- %s: %s", id, st.fail))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Service MCP ngoài sau đây KHÔNG dùng được trong phiên này (công cụ của chúng không có trong danh sách):\n" + strings.Join(lines, "\n")
}

// Call chuyển một lời gọi tool tới daemon (tên upstream) và trả nguyên văn phần result. Gặp 404/401 hoặc không kết nối
// được daemon thì nối lại (khởi động daemon nếu cần, mở lại phiên) rồi gửi lại — chỉ khi chắc lời gọi chưa tới daemon,
// hoặc tool chỉ đọc; lệnh ghi không bao giờ bị gửi hai lần.
func (s *Shim) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	s.mu.Lock()
	tool, ok := s.table[name]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("không có công cụ %q (ngoài quyền của phiên này)", name)
	}
	body := requestFrame(json.RawMessage(`1`), "tools/call", marshalRaw(map[string]any{"name": tool.upstream, "arguments": args}))
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		info, sid, gen, reason := s.session(tool.service)
		if sid == "" {
			if reason == "" {
				reason = "chưa có phiên"
			}
			return nil, fmt.Errorf("service %s không dùng được: %s", tool.service, reason)
		}
		status, resp, err := s.post(ctx, info, sid, nil, body)
		switch {
		case err == nil && status == http.StatusOK:
			var m rpcMsg
			if json.Unmarshal(resp, &m) != nil {
				return nil, fmt.Errorf("phản hồi của gateway không phải JSON-RPC")
			}
			if len(m.Error) > 0 {
				var re rpcError
				_ = json.Unmarshal(m.Error, &re)
				return nil, fmt.Errorf("%s (mã %d)", re.Message, re.Code)
			}
			return m.Result, nil
		case err == nil && (status == http.StatusNotFound || status == http.StatusUnauthorized):
			// Phiên hết hạn hoặc daemon mới (token mới): daemon chưa xử lý lời gọi nên gửi lại an toàn.
			lastErr = fmt.Errorf("gateway trả %d", status)
		case err == nil:
			return nil, fmt.Errorf("gateway trả %d: %s", status, strings.TrimSpace(string(resp)))
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case notReached(err):
			lastErr = err
		case tool.readOnly:
			lastErr = err
		default:
			// Lệnh có thể đã tới daemon: không gửi lại. Vẫn nối lại để lời gọi sau dùng được.
			_ = s.reconnect(ctx, gen)
			return nil, fmt.Errorf("không rõ kết quả: kết nối tới gateway đứt giữa lúc gọi %s (lệnh ghi không được tự gọi lại): %v", name, err)
		}
		if rerr := s.reconnect(ctx, gen); rerr != nil {
			return nil, fmt.Errorf("gateway không dùng được (%v); lỗi trước đó: %v", rerr, lastErr)
		}
	}
	return nil, fmt.Errorf("gateway vẫn lỗi sau khi nối lại: %v", lastErr)
}

// notReached: lỗi xảy ra trước khi request tới daemon (không kết nối được), nên gửi lại an toàn.
func notReached(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

func (s *Shim) session(service string) (info Info, sid string, gen int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.svcs[service]
	if st == nil {
		return s.info, "", s.gen, s.daemon
	}
	return s.info, st.sid, s.gen, st.fail
}

// reconnect nối lại daemon và mở lại phiên mọi service đã có; single-flight: nhiều lời gọi cùng lỗi chỉ nối lại một lần
// (seen là thế hệ kết nối mà lời gọi đã dùng; đã có người nối lại thì trả ngay).
func (s *Shim) reconnect(ctx context.Context, seen int) error {
	s.rmu.Lock()
	defer s.rmu.Unlock()
	s.mu.Lock()
	if s.gen != seen {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	dctx, cancel := context.WithTimeout(ctx, s.o.StartWait)
	info, err := s.discover(dctx)
	cancel()
	if err != nil {
		return err
	}
	octx, ocancel := context.WithTimeout(ctx, s.o.OpenWait)
	defer ocancel()
	s.mu.Lock()
	var ids []string
	for id, st := range s.svcs {
		if !st.denied {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	sort.Strings(ids)
	type opened struct {
		sid string
		err error
	}
	res := make([]opened, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sid, _, err := s.open(octx, info, id, false)
			res[i] = opened{sid, err}
		}()
	}
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info = info
	for i, id := range ids {
		st := s.svcs[id]
		st.sid, st.fail = res[i].sid, ""
		if res[i].err != nil {
			st.sid, st.fail = "", res[i].err.Error()
		}
	}
	s.daemon = ""
	s.gen++
	return nil
}

// ---- HTTP tới daemon ----

// open mở phiên (initialize) cho một service; wantTools thì gửi thêm notifications/initialized và lấy tools/list.
func (s *Shim) open(ctx context.Context, info Info, id string, wantTools bool) (sid string, tools []json.RawMessage, err error) {
	hdr := map[string]string{HeaderService: id}
	set := func(k, v string) {
		if v != "" {
			hdr[k] = v
		}
	}
	set(HeaderWorktree, s.o.Worktree)
	set(HeaderRunID, s.o.RunID)
	set(HeaderTaskID, s.o.TaskID)
	set(HeaderHarness, s.o.Harness)
	set(HeaderSessionID, s.o.SessionID)
	body := requestFrame(json.RawMessage(`1`), "initialize", marshalRaw(map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "bint-shim", "version": "1"}}))
	status, header, resp, err := s.postRaw(ctx, info, "", hdr, body)
	if err != nil {
		return "", nil, err
	}
	if status != http.StatusOK {
		return "", nil, fmt.Errorf("gateway trả %d: %s", status, strings.TrimSpace(string(resp)))
	}
	var m rpcMsg
	if json.Unmarshal(resp, &m) != nil {
		return "", nil, errors.New("phản hồi initialize của gateway không phải JSON-RPC")
	}
	if len(m.Error) > 0 {
		var re rpcError
		_ = json.Unmarshal(m.Error, &re)
		return "", nil, &re
	}
	sid = header.Get(headerMCPSess)
	if sid == "" {
		return "", nil, errors.New("gateway không cấp Mcp-Session-Id")
	}
	if !wantTools {
		return sid, nil, nil
	}
	// notifications/initialized (daemon bỏ qua nhưng đúng giao thức), rồi tools/list.
	_, _, _ = s.post(ctx, info, sid, nil, requestFrame(nil, "notifications/initialized", nil))
	status, resp, err = s.post(ctx, info, sid, nil, requestFrame(json.RawMessage(`2`), "tools/list", nil))
	if err != nil || status != http.StatusOK {
		s.closeRemote(info, sid)
		if err == nil {
			err = fmt.Errorf("gateway trả %d cho tools/list", status)
		}
		return "", nil, err
	}
	var lm rpcMsg
	if json.Unmarshal(resp, &lm) != nil || len(lm.Error) > 0 {
		s.closeRemote(info, sid)
		return "", nil, fmt.Errorf("tools/list lỗi: %s", strings.TrimSpace(string(resp)))
	}
	var r struct {
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(lm.Result, &r)
	return sid, r.Tools, nil
}

func (s *Shim) post(ctx context.Context, info Info, sid string, hdr map[string]string, body []byte) (int, []byte, error) {
	status, _, b, err := s.postRaw(ctx, info, sid, hdr, body)
	return status, b, err
}

func (s *Shim) postRaw(ctx context.Context, info Info, sid string, hdr map[string]string, body []byte) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, info.URL()+"/mcp", bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set(headerMCPSess, sid)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, b, nil
}

func (s *Shim) closeRemote(info Info, sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, info.URL()+"/mcp", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set(headerMCPSess, sid)
	if resp, err := s.hc.Do(req); err == nil {
		resp.Body.Close()
	}
}

// Close đóng các phiên đã mở (DELETE /mcp, có hạn 1 giây mỗi phiên); không khởi động daemon.
func (s *Shim) Close() {
	s.mu.Lock()
	info := s.info
	var sids []string
	for _, st := range s.svcs {
		if st.sid != "" {
			sids = append(sids, st.sid)
		}
		st.sid = ""
	}
	s.mu.Unlock()
	if info.Port == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, sid := range sids {
		wg.Add(1)
		go func() { defer wg.Done(); s.closeRemote(info, sid) }()
	}
	wg.Wait()
}
