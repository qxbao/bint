package hookcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/spool"
)

// Tệp sửa qua lệnh shell (sed -i, heredoc, cat > f, mv, rm...) không đi qua công cụ
// sửa tệp của harness nên hook không thấy đường dẫn (mục 13.2). Thay vì đoán cú pháp
// shell, hook chụp trạng thái working tree (git status + băm nội dung) trước và sau
// mỗi lệnh shell; tệp có trạng thái đổi trong khoảng đó được ghi file.modified.
// Tệp đã sửa trước lệnh (kể cả bằng Edit/Write) nằm sẵn trong ảnh chụp trước nên
// không bị ghi trùng.

const (
	maxSnapFiles = 5000
	maxHashBytes = 8 << 20
	cleanState   = "clean"
)

// treeSnapshot: đường dẫn (tương đối repository) → dấu trạng thái. Tệp sạch không có mặt.
type treeSnapshot map[string]string

func takeSnapshot(ctx context.Context, top string) (treeSnapshot, bool) {
	out, err := gitrepo.Run(ctx, top, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, false
	}
	snap := treeSnapshot{}
	parts := strings.Split(out, "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		xy, path := e[:2], e[3:]
		if xy[0] == 'R' || xy[0] == 'C' {
			i++ // mục kế tiếp là đường dẫn nguồn
		}
		if strings.HasPrefix(path, ".bint/") || len(snap) >= maxSnapFiles {
			continue
		}
		snap[path] = xy + ":" + fileStamp(filepath.Join(top, filepath.FromSlash(path)))
	}
	return snap, true
}

func fileStamp(p string) string {
	st, err := os.Lstat(p)
	if err != nil {
		return "deleted"
	}
	if !st.Mode().IsRegular() || st.Size() > maxHashBytes {
		return fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "unreadable"
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// changedPaths trả về tệp có dấu trạng thái khác nhau giữa hai ảnh chụp.
func changedPaths(before, after treeSnapshot) map[string]string {
	out := map[string]string{}
	for p, a := range after {
		if before[p] != a {
			out[p] = a
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out[p] = cleanState // trở lại sạch (git checkout, rm tệp mới...)
		}
	}
	return out
}

func snapPath(runtimeDir, harness, session string) string {
	return filepath.Join(runtimeDir, "shellsnap-"+EventID(harness, session)[3:19]+".json")
}

func saveSnapshot(runtimeDir, harness, session string, s treeSnapshot) {
	if os.MkdirAll(runtimeDir, 0o755) != nil {
		return
	}
	if b, err := json.Marshal(s); err == nil {
		os.WriteFile(snapPath(runtimeDir, harness, session), b, 0o644)
	}
}

func loadSnapshot(runtimeDir, harness, session string) (treeSnapshot, bool) {
	p := snapPath(runtimeDir, harness, session)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	os.Remove(p)
	var s treeSnapshot
	if json.Unmarshal(b, &s) != nil {
		return nil, false
	}
	return s, true
}

// shellModified ghi file.modified cho tệp đổi trong lúc lệnh shell chạy. event_id gồm
// (phiên, đường dẫn, trạng thái sau) nên lệnh song song hay hook gọi lặp không ghi trùng.
func shellModified(ctx context.Context, base spool.Event, spoolDir, runtimeDir, top string) {
	before, ok := loadSnapshot(runtimeDir, base.Harness, base.SessionID)
	if !ok {
		return // không có ảnh chụp trước (hook PreToolUse chưa cài): không tổng hợp giả
	}
	after, ok := takeSnapshot(ctx, top)
	if !ok {
		return
	}
	for p, state := range changedPaths(before, after) {
		e := base
		e.Type = spool.FileModified
		e.Payload = map[string]any{"path": p, "tool": "shell", "via": "git-status"}
		e.EventID = EventID(base.Harness, base.SessionID, "shell-diff", p, state)
		spool.Write(spoolDir, e)
	}
}
