package ctxengine

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/spool"
)

// Kênh truy xuất.
const (
	ChannelMCPTask  = "mcp:task"
	ChannelMCPPaths = "mcp:paths"
	ChannelHookPath = "hook:path"
	ChannelCLI      = "cli"
)

// ContextEvent dựng sự kiện context.retrieved cho spool. key phân biệt các lần
// truy xuất trong cùng phiên (ví dụ tool_use_id).
func ContextEvent(harness, session, worktree, channel, key string, p Pack, now time.Time) spool.Event {
	labels := map[string]any{}
	ids := make([]any, 0, len(p.Entries))
	for _, e := range p.Entries {
		ids = append(ids, e.ID)
		labels[e.ID] = e.Label
	}
	payload := map[string]any{"channel": channel, "ids": ids, "labels": labels, "mode": p.Mode}
	if p.Query != "" {
		q := []rune(p.Query)
		if len(q) > 300 {
			q = append(q[:300], '…')
		}
		payload["query"] = string(q)
	}
	if len(p.Paths) > 0 {
		payload["paths"] = p.Paths
	}
	return spool.Event{
		SchemaVersion: spool.SchemaVersion, EventID: EventKey(harness, session, spool.ContextRetrieved, channel, key),
		Type: spool.ContextRetrieved, Source: channel, Timestamp: now.UTC(), Harness: harness, SessionID: session,
		Worktree: worktree, Payload: payload,
	}
}

// EventKey băm các thành phần thành event_id tất định.
func EventKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "ev_" + hex.EncodeToString(h.Sum(nil))[:32]
}

// SessionMemory ghi các mục đã chèn theo đường dẫn trong một phiên (để không lặp
// lại), trong <state>/runtime/context/<băm phiên>.ids. Mỗi lần hook là một tiến
// trình riêng nên trạng thái phải nằm trên đĩa; ghi nối thêm, chấp nhận trùng.
type SessionMemory struct{ Path string }

// NewSessionMemory trả về bộ nhớ phiên trong thư mục runtime.
func NewSessionMemory(runtimeDir, harness, session string) SessionMemory {
	sum := sha256.Sum256([]byte(harness + "\x00" + session))
	return SessionMemory{Path: filepath.Join(runtimeDir, "context", hex.EncodeToString(sum[:])[:24]+".ids")}
}

// Seen trả về tập ID đã chèn.
func (m SessionMemory) Seen() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(m.Path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[l] = true
		}
	}
	return out
}

// Add ghi thêm ID.
func (m SessionMemory) Add(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(m.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(ids, "\n") + "\n")
	return err
}

// PruneSessionMemory xoá tệp phiên cũ hơn maxAge.
func PruneSessionMemory(runtimeDir string, maxAge time.Duration, now time.Time) {
	dir := filepath.Join(runtimeDir, "context")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
