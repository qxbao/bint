package mcpgw

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"bint/internal/state"
)

// Tên các tệp trong thư mục gateway.
const (
	LockFileName = "gateway.lock"
	InfoFileName = "gateway.json"
	LogFileName  = "gateway.log"
)

// Dir là thư mục gateway cấp user: <state.BaseDir>/gateway.
func Dir() (string, error) {
	base, err := state.BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gateway"), nil
}

// Info là nội dung gateway.json (quyền 0600): nơi client tìm daemon.
type Info struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version,omitempty"`
}

// URL là địa chỉ gốc của daemon.
func (i Info) URL() string { return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(i.Port)) }

// ReadInfo đọc gateway.json trong dir; os.ErrNotExist (bọc) nếu chưa có.
func ReadInfo(dir string) (Info, error) {
	data, err := os.ReadFile(filepath.Join(dir, InfoFileName))
	if err != nil {
		return Info{}, err
	}
	var i Info
	if err := json.Unmarshal(data, &i); err != nil {
		return Info{}, fmt.Errorf("%s: %w", InfoFileName, err)
	}
	if i.Port <= 0 || i.Token == "" {
		return Info{}, fmt.Errorf("%s: thiếu cổng hoặc token", InfoFileName)
	}
	return i, nil
}

// writeInfo ghi gateway.json nguyên tử (tệp tạm cùng thư mục, 0600, rồi đổi tên).
func writeInfo(dir string, i Info) error {
	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gateway-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !isWindows {
		return fail(err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, InfoFileName)); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---- client của /admin/* ----

// ErrNotRunning: không có daemon đang chạy (không có gateway.json hoặc không kết nối được).
var ErrNotRunning = errors.New("gateway không chạy")

// StatusError là phản hồi khác 200 của /admin/*; Code 404 nghĩa là daemon là bản cũ chưa có route đó.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("gateway trả %d: %s", e.Code, e.Body) }

// AdminClient gọi /admin/* của daemon.
type AdminClient struct {
	Info Info
	HTTP *http.Client
}

// Connect đọc gateway.json trong dir và trả client; ErrNotRunning nếu chưa có.
func Connect(dir string) (*AdminClient, error) {
	i, err := ReadInfo(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotRunning
	}
	if err != nil {
		return nil, err
	}
	return &AdminClient{Info: i, HTTP: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (c *AdminClient) do(ctx context.Context, method, path string, q url.Values, out any) error {
	u := c.Info.URL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Info.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Code: resp.StatusCode, Body: string(bytes.TrimSpace(body))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// Status lấy trạng thái daemon.
func (c *AdminClient) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.do(ctx, http.MethodGet, "/admin/status", nil, &s)
	return s, err
}

// Log lấy phần đuôi stderr của các upstream đang chạy của service (GET /admin/log). Daemon cũ chưa có route này trả
// lỗi "gateway trả 404".
func (c *AdminClient) Log(ctx context.Context, service string) ([]PartitionLog, error) {
	var l []PartitionLog
	err := c.do(ctx, http.MethodGet, "/admin/log", url.Values{"service": {service}}, &l)
	return l, err
}

// RestartResult là kết quả /admin/restart.
type RestartResult struct {
	Service string `json:"service"`
	Stopped int    `json:"stopped"`
}

// Restart dừng mọi phân vùng của service và xoá trạng thái lỗi; lời gọi sau khởi động mới.
func (c *AdminClient) Restart(ctx context.Context, service string) (RestartResult, error) {
	var r RestartResult
	err := c.do(ctx, http.MethodPost, "/admin/restart", url.Values{"service": {service}}, &r)
	return r, err
}

// Stop yêu cầu daemon tắt êm.
func (c *AdminClient) Stop(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/admin/stop", nil, nil)
}
