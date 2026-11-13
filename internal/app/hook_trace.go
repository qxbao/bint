package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/harness/hookcore"
)

// Ghi vết hook để chẩn đoán (NT-ISO-LEAK): BINT_HOOK_TRACE=1 ⇒ mỗi lần `bint hook <harness>` chạy ghi một cặp tệp dưới
// <state>/runtime/hook-trace/: <thời điểm>-<harness>-<sự kiện>.in (đầu vào nguyên văn, cắt ở maxHookTraceInput) và .out (đầu ra,
// thời lượng, kết quả). Có .in mà không có .out nghĩa là hook chết hoặc quá hạn giữa chừng; không có .in nghĩa là hook không được
// gọi. Hook không bao giờ chặn agent (tri thức k:01M746NPTW1B304MRQQPBCAFAM): mọi lỗi ghi vết (kể cả panic) bị nuốt, không đổi
// stdout/stderr/hook-errors.log, không mở SQLite. Đầu vào hook có thể chứa nội dung nhạy cảm (lệnh, đường dẫn, nội dung tệp):
// tệp 0600 trong thư mục trạng thái ngoài repo, tắt biến khi chẩn đoán xong.

// HookTraceEnv là biến môi trường bật ghi vết (giá trị "1").
const HookTraceEnv = "BINT_HOOK_TRACE"

const (
	hookTraceDir      = "hook-trace"
	maxHookTraceInput = 1 << 20 // trần đầu vào ghi vào .in (byte)
)

// maxHookTraceFiles là số tệp tối đa giữ lại (cũ nhất bị xoá trước); biến để kiểm thử hạ trần.
var maxHookTraceFiles = 500

type hookTrace struct {
	dir, base string
	start     time.Time
}

// startHookTrace ghi tệp .in và trả bộ ghi vết; nil khi tắt hoặc không ghi được (không bao giờ lỗi).
func startHookTrace(harness, event string, data []byte) (tr *hookTrace) {
	if os.Getenv(HookTraceEnv) != "1" {
		return nil
	}
	defer func() {
		if recover() != nil {
			tr = nil
		}
	}()
	var in struct {
		Cwd            string   `json:"cwd"`
		HookEventName  string   `json:"hook_event_name"`
		WorkspacePaths []string `json:"workspacePaths"`
	}
	json.Unmarshal(data, &in)
	name := in.HookEventName
	if name == "" {
		name = event
	}
	ws := hookWorkspace(in.Cwd, in.WorkspacePaths)
	if ws == nil {
		return nil
	}
	dir := filepath.Join(ws.Paths.Runtime, hookTraceDir)
	if os.MkdirAll(dir, 0o700) != nil {
		return nil
	}
	body := data
	if len(body) > maxHookTraceInput {
		body = append(append([]byte{}, data[:maxHookTraceInput]...),
			fmt.Sprintf("\n[bint: đầu vào đã bị cắt, gốc %d byte]\n", len(data))...)
	}
	label := "-" + traceLabel(harness) + "-" + traceLabel(name)
	for i := 0; i < 8; i++ {
		now := time.Now().UTC().Add(time.Duration(i) * time.Nanosecond)
		base := filepath.Join(dir, now.Format("20060102T150405.000000000Z")+label)
		f, err := os.OpenFile(base+".in", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return nil
		}
		_, werr := f.Write(body)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			os.Remove(base + ".in")
			return nil
		}
		return &hookTrace{dir: dir, base: base, start: time.Now()}
	}
	return nil
}

// finish ghi tệp .out rồi dọn tệp cũ vượt trần. dur là thời lượng của bộ xử lý hook.
func (t *hookTrace) finish(res hookcore.Result, err error, dur time.Duration) {
	if t == nil {
		return
	}
	defer func() { _ = recover() }()
	out := map[string]any{"output": res.Output, "duration_ms": dur.Milliseconds(), "skipped": res.Skipped,
		"written": res.Written, "spool_file": res.SpoolFile}
	if err != nil {
		out["error"] = err.Error()
	}
	if b, merr := json.MarshalIndent(out, "", "  "); merr == nil {
		if f, ferr := os.OpenFile(t.base+".out", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); ferr == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	pruneHookTrace(t.dir, maxHookTraceFiles)
}

// pruneHookTrace giữ keep tệp mới nhất (tên bắt đầu bằng thời điểm cố định độ rộng nên sắp theo tên là sắp theo thời gian).
func pruneHookTrace(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) <= keep {
		return
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".in") || strings.HasSuffix(e.Name(), ".out")) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for i := 0; i < len(names)-keep; i++ {
		os.Remove(filepath.Join(dir, names[i]))
	}
}

// traceLabel giữ [A-Za-z0-9_-] (tên harness/sự kiện đi vào tên tệp); rỗng ⇒ "unknown".
func traceLabel(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r < 128 && (r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "unknown"
	}
	return sb.String()
}

// hookWorkspace mở workspace của hook (không mở SQLite) từ cwd của payload, rồi workspacePaths[0], rồi cwd tiến trình;
// nil nếu chưa init hoặc không mở được.
func hookWorkspace(cwd string, workspacePaths []string) *Workspace {
	dir := cwd
	if dir == "" && len(workspacePaths) > 0 {
		dir = workspacePaths[0]
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	ws, err := OpenWorkspace(context.Background(), dir, false)
	if err != nil || !ws.Paths.Initialized() {
		return nil
	}
	return ws
}
