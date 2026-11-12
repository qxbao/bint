package mcpgw

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecondDaemonRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, err := Start(Options{Dir: h.dir})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("daemon thứ hai cùng thư mục: %v, muốn ErrAlreadyRunning", err)
	}
	if h.d.Info().PID == 0 || !errors.Is(err, ErrAlreadyRunning) {
		t.Fatal("thiếu PID")
	}
	// Sau khi daemon đầu tắt, khoá được nhả và daemon mới lên được.
	h.d.Close()
	d2, err := Start(Options{Dir: h.dir})
	if err != nil {
		t.Fatalf("Start sau Close: %v", err)
	}
	d2.Close()
}

func TestInfoFile(t *testing.T) {
	h := newHarness(t, nil)
	p := filepath.Join(h.dir, InfoFileName)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("gateway.json quyền %v, muốn 0600", fi.Mode().Perm())
	}
	i, err := ReadInfo(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if i.PID != os.Getpid() || i.Port != h.d.Info().Port || i.Token != h.d.Token() || len(i.Token) < 32 {
		t.Errorf("gateway.json %+v không khớp daemon", i)
	}
	addr := h.d.ln.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() || addr.Port == 0 {
		t.Errorf("nghe %v, muốn 127.0.0.1 cổng động", addr)
	}
	h.d.Close()
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gateway.json còn sau khi dừng: %v", err)
	}
}

func TestListenOnlyLoopback(t *testing.T) {
	testutilIsolate(t)
	if _, err := Start(Options{Dir: t.TempDir(), Addr: "0.0.0.0:0"}); err == nil {
		t.Error("địa chỉ không phải loopback phải bị từ chối")
	}
}

// Mọi route đòi token; Host không loopback và mọi Origin bị 403 trước cả token (mục 6.2).
func TestAuthTable(t *testing.T) {
	h := newHarness(t, nil)
	routes := []struct{ method, path string }{
		{"POST", "/mcp"}, {"GET", "/mcp"}, {"DELETE", "/mcp"},
		{"GET", "/admin/status"}, {"GET", "/admin/log"}, {"POST", "/admin/restart"}, {"POST", "/admin/stop"},
	}
	send := func(method, path string, mod func(*http.Request)) int {
		req, _ := http.NewRequest(method, h.d.URL()+path, nil)
		if mod != nil {
			mod(req)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	good := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+h.d.Token()) }
	for _, rt := range routes {
		name := rt.method + " " + rt.path
		if c := send(rt.method, rt.path, nil); c != 401 {
			t.Errorf("%s thiếu token: %d, muốn 401", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { r.Header.Set("Authorization", "Bearer sai") }); c != 401 {
			t.Errorf("%s sai token: %d, muốn 401", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { r.Header.Set("Authorization", h.d.Token()) }); c != 401 {
			t.Errorf("%s token không có Bearer: %d, muốn 401", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { good(r); r.Host = "evil.example.com" }); c != 403 {
			t.Errorf("%s Host lạ: %d, muốn 403", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { good(r); r.Host = "127.0.0.1.evil.com:80" }); c != 403 {
			t.Errorf("%s Host giả loopback: %d, muốn 403", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { good(r); r.Header.Set("Origin", "http://localhost") }); c != 403 {
			t.Errorf("%s có Origin: %d, muốn 403", name, c)
		}
		if c := send(rt.method, rt.path, func(r *http.Request) { r.Host = "evil.example.com" }); c != 403 {
			t.Errorf("%s Host lạ không token: %d, muốn 403 (kiểm Host trước token)", name, c)
		}
		if rt.path == "/admin/stop" {
			continue // ca hợp lệ sẽ tắt daemon: có test riêng
		}
		if c := send(rt.method, rt.path, good); c == 401 || c == 403 {
			t.Errorf("%s hợp lệ bị chặn: %d", name, c)
		}
		for _, host := range []string{"localhost:" + itoaPort(h), "[::1]:" + itoaPort(h)} {
			if c := send(rt.method, rt.path, func(r *http.Request) { good(r); r.Host = host }); c == 401 || c == 403 {
				t.Errorf("%s Host %s bị chặn: %d", name, host, c)
			}
		}
	}
	// Phương thức ngoài GET/POST/DELETE trên /mcp bị từ chối sau khi qua xác thực.
	if c := send("PUT", "/mcp", good); c != 405 {
		t.Errorf("PUT /mcp: %d, muốn 405", c)
	}
}

func TestAdminStopShutsDown(t *testing.T) {
	h := newHarness(t, nil)
	c, err := Connect(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.d.Done():
	case <-timeAfter(5):
		t.Fatal("daemon không dừng sau /admin/stop")
	}
	if _, err := os.Stat(filepath.Join(h.dir, InfoFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("gateway.json còn sau /admin/stop: %v", err)
	}
}
