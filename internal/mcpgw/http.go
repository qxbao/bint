package mcpgw

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
)

func (d *Daemon) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", d.handleMCP)
	mux.HandleFunc("/admin/status", d.handleAdminStatus)
	mux.HandleFunc("/admin/restart", d.handleAdminRestart)
	mux.HandleFunc("/admin/stop", d.handleAdminStop)
	return d.guard(mux)
}

// guard áp cho MỌI route: Host phải là loopback (chống DNS rebinding), từ chối mọi Origin (trình duyệt) rồi mới tới
// bearer token (so sánh hằng thời gian).
func (d *Daemon) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "Host không phải loopback", http.StatusForbidden)
			return
		}
		if len(r.Header.Values("Origin")) > 0 {
			http.Error(w, "gateway từ chối request có Origin", http.StatusForbidden)
			return
		}
		const prefix = "Bearer "
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) || subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(d.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "thiếu hoặc sai bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost cho biết giá trị header Host trỏ tới loopback (localhost, 127.0.0.0/8, ::1), có hoặc không có cổng.
func loopbackHost(hostport string) bool {
	h := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		h = host
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (d *Daemon) session(id string) *session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[id]
}

func (d *Daemon) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		d.handlePost(w, r)
	case http.MethodGet:
		d.handleSSE(w, r)
	case http.MethodDelete:
		d.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method không hỗ trợ", http.StatusMethodNotAllowed)
	}
}

func (d *Daemon) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(d.opts.MaxBody)))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			http.Error(w, fmt.Sprintf("body vượt giới hạn %d byte", d.opts.MaxBody), http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "đọc body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if t := bytes.TrimSpace(body); len(t) > 0 && t[0] == '[' {
		writeJSON(w, http.StatusBadRequest, errorFrame(nil, codeInvalidRequest, "gateway không nhận batch JSON-RPC"))
		return
	}
	var m rpcMsg
	if err := json.Unmarshal(body, &m); err != nil {
		writeJSON(w, http.StatusBadRequest, errorFrame(nil, -32700, "JSON không hợp lệ"))
		return
	}
	if m.isRequest() && m.Method == "initialize" {
		d.handleInitialize(w, r, m)
		return
	}
	sid := r.Header.Get(headerMCPSess)
	if sid == "" {
		writeJSON(w, http.StatusBadRequest, errorFrame(m.ID, codeInvalidRequest, "thiếu header Mcp-Session-Id (gửi initialize trước)"))
		return
	}
	s := d.session(sid)
	if s == nil {
		writeJSON(w, http.StatusNotFound, errorFrame(m.ID, codeInvalidRequest, "phiên không tồn tại hoặc đã hết hạn; mở lại bằng initialize"))
		return
	}
	s.touch()
	switch {
	case m.isRequest():
		writeJSON(w, http.StatusOK, d.handleRequest(r, s, m))
	case m.isNotification():
		d.handleNotification(s, m)
		w.WriteHeader(http.StatusAccepted)
	default:
		// Phản hồi của client cho request của server: gateway không chuyển request server→client nên không có gì chờ.
		w.WriteHeader(http.StatusAccepted)
	}
}

func rpcFail(id json.RawMessage, code int, format string, a ...any) []byte {
	return errorFrame(id, code, fmt.Sprintf(format, a...))
}

func (d *Daemon) handleInitialize(w http.ResponseWriter, r *http.Request, m rpcMsg) {
	fail := func(format string, a ...any) {
		writeJSON(w, http.StatusOK, rpcFail(m.ID, codeInvalidParams, format, a...))
	}
	svcID := r.Header.Get(HeaderService)
	svc, ok := d.opts.Lookup(svcID)
	if !ok {
		fail("service %q (header %s) không có trong danh mục", svcID, HeaderService)
		return
	}
	mc, err := d.opts.Machine()
	if err != nil {
		fail("đọc mcp.yml: %v", err)
		return
	}
	reg, ok := mc.Services[svcID]
	if !ok {
		fail("service %q chưa đăng ký trên máy này (bint mcp add %s)", svcID, svcID)
		return
	}
	needsRoot := svc.Cwd == mcpcat.CwdWorktree || svc.Cwd == mcpcat.CwdState || svc.Share == mcpcat.ShareWorktree
	var ri repoInfo
	if wt := r.Header.Get(HeaderWorktree); wt != "" {
		if !filepath.IsAbs(wt) {
			fail("header %s phải là đường dẫn tuyệt đối", HeaderWorktree)
			return
		}
		ri, err = resolveRepo(r.Context(), wt)
		if err != nil && needsRoot {
			fail("%s cần worktree git: %v", svcID, err)
			return
		}
	} else if needsRoot {
		fail("%s cần header %s (gốc worktree git)", svcID, HeaderWorktree)
		return
	}
	if svc.Cwd == mcpcat.CwdState && ri.stateRoot == "" {
		fail("%s: không xác định được thư mục trạng thái của repo", svcID)
		return
	}
	now := time.Now()
	s := &session{id: newSessionID(), svc: svc, root: ri.root, spool: ri.spool, created: now, lastActive: now,
		done: make(chan struct{}), calls: map[string]*callRef{},
		ctx: sessCtx{RunID: r.Header.Get(HeaderRunID), TaskID: r.Header.Get(HeaderTaskID), Role: r.Header.Get(HeaderRole),
			Harness: r.Header.Get(HeaderHarness), SessionID: r.Header.Get(HeaderSessionID)}}
	rev := revision(svc, reg)
	p := d.partitionFor(partSpec{svc: svc, reg: reg, secretsPath: config.SecretsPath(mc), rev: rev, ri: ri}, s.id)
	s.p = p
	p.attach(s.id)
	entry, ok := d.cacheGet(svcID, rev)
	if !ok {
		// Chưa có cache theo (service, revision): khởi động upstream một lần để lấy initialize và tools/list.
		up, err := p.acquire(r.Context())
		if err != nil {
			p.detach(s.id)
			writeJSON(w, http.StatusOK, errorFromErr(m.ID, err))
			return
		}
		_ = up
		p.release()
		if entry, ok = d.cacheGet(svcID, rev); !ok {
			p.detach(s.id)
			fail("không lấy được kết quả initialize của %s", svcID)
			return
		}
	}
	d.mu.Lock()
	d.sessions[s.id] = s
	d.mu.Unlock()
	w.Header().Set(headerMCPSess, s.id)
	writeJSON(w, http.StatusOK, resultFrame(m.ID, entry.init))
}

// errorFromErr đổi lỗi của upstream/gateway thành phản hồi JSON-RPC cho id.
func errorFromErr(id json.RawMessage, err error) []byte {
	var re *rpcError
	if errors.As(err, &re) {
		return errorFrame(id, re.Code, re.Message)
	}
	return errorFrame(id, CodeUpstream, err.Error())
}

// forwardable là các method ngoài tools/* gateway chuyển nguyên văn tới upstream.
var forwardable = map[string]bool{
	"resources/list": true, "resources/read": true, "resources/templates/list": true,
	"prompts/list": true, "prompts/get": true, "completion/complete": true,
}

func (d *Daemon) handleRequest(r *http.Request, s *session, m rpcMsg) []byte {
	switch {
	case m.Method == "ping":
		return resultFrame(m.ID, json.RawMessage(`{}`))
	case m.Method == "tools/list":
		return d.toolsList(r, s, m)
	case m.Method == "tools/call":
		return d.toolsCall(r, s, m)
	case forwardable[m.Method]:
		return d.forward(r, s, m)
	}
	return rpcFail(m.ID, codeMethodNotFound, "gateway không hỗ trợ %s", m.Method)
}

func (d *Daemon) handleNotification(s *session, m rpcMsg) {
	if m.Method != "notifications/cancelled" {
		return // initialized và các thông báo khác: gateway đã handshake với upstream một lần cho mỗi generation
	}
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
		Reason    json.RawMessage `json:"reason"`
	}
	if json.Unmarshal(m.Params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	gw, ok := s.cancelCall(p.RequestID) // kết thúc lời gọi đang chờ (ctx bị huỷ) rồi báo upstream nếu đã gửi
	if !ok || gw == "" {
		return
	}
	s.p.mu.Lock()
	up := s.p.up
	s.p.mu.Unlock()
	if up != nil {
		up.cancel(gw, p.Reason)
	}
}

func (d *Daemon) handleDelete(w http.ResponseWriter, r *http.Request) {
	s := d.session(r.Header.Get(headerMCPSess))
	if s == nil {
		http.Error(w, "phiên không tồn tại", http.StatusNotFound)
		return
	}
	d.closeSession(s)
	w.WriteHeader(http.StatusNoContent)
}

// handleSSE mở luồng SSE của phiên (chở notifications/progress); mỗi phiên một luồng.
func (d *Daemon) handleSSE(w http.ResponseWriter, r *http.Request) {
	s := d.session(r.Header.Get(headerMCPSess))
	if s == nil {
		http.Error(w, "phiên không tồn tại", http.StatusNotFound)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "không hỗ trợ streaming", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	if s.sseOpen {
		s.mu.Unlock()
		http.Error(w, "phiên đã có luồng GET đang mở", http.StatusConflict)
		return
	}
	s.sseOpen, s.sse = true, make(chan []byte, 64)
	ch := s.sse
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.sseOpen = false
		s.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": ok\n\n")
	fl.Flush()
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case f := <-ch:
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", f); err != nil {
				return
			}
		case <-hb.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		case <-s.done:
			return
		case <-r.Context().Done():
			return
		}
		fl.Flush()
	}
}

// ---- /admin ----

func (d *Daemon) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "dùng GET", http.StatusMethodNotAllowed)
		return
	}
	b, _ := json.Marshal(d.Status())
	writeJSON(w, http.StatusOK, b)
}

func (d *Daemon) handleAdminRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "dùng POST", http.StatusMethodNotAllowed)
		return
	}
	svc := r.URL.Query().Get("service")
	if _, ok := d.opts.Lookup(svc); !ok {
		http.Error(w, fmt.Sprintf("service %q không có trong danh mục", svc), http.StatusBadRequest)
		return
	}
	b, _ := json.Marshal(RestartResult{Service: svc, Stopped: d.Restart(svc)})
	writeJSON(w, http.StatusOK, b)
}

func (d *Daemon) handleAdminStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "dùng POST", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, []byte(`{"stopping":true}`))
	d.requestStop()
}
