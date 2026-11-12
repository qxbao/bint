// Package spool là hàng đợi sự kiện dạng tệp giữa hook và CLI.
//
// Hook không bao giờ ghi trực tiếp vào SQLite: mỗi lần gọi hook ghi một tệp
// <ulid>.jsonl qua tệp tạm + đổi tên (nguyên tử), nên nhiều phiên song song ở
// nhiều worktree không tranh chấp ghi. CLI nạp các tệp vào cơ sở dữ liệu và loại
// trùng theo event_id (xem package runlog).
package spool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/ulid"
)

// SchemaVersion của định dạng sự kiện trong spool.
const SchemaVersion = 1

// Các loại sự kiện chuẩn hoá (mục 13.2). Chỉ những loại harness thực sự cung cấp
// mới được ghi; không tổng hợp giả.
const (
	SessionStarted = "session.started"
	SessionEnded   = "session.ended"
	FileModified   = "file.modified"
	TestCompleted  = "test.completed"
	// P6: tệp agent đọc (payload.paths), để phát hiện khám phá lặp lại (mục 10.2).
	// Chỉ harness có hook trước công cụ đọc tệp mới cung cấp (Claude Code).
	FileRead = "file.read"
	// P7: agent kích hoạt một skill (payload.skill). Chỉ harness có công cụ gọi skill đi qua
	// hook mới cung cấp (Claude Code: công cụ Skill); Codex và Antigravity không có, và sự
	// kiện không được hỗ trợ thì không được tổng hợp giả (mục 13.2).
	SkillActivated = "skill.activated"
	// H1: guardrail của quy trình (.bint/harness/guardrails) tác động tới agent. payload:
	// guardrail (id), outcome (fired | followed | bypass), on, action, requested, blocked.
	// Chỉ ghi khi guardrail thực sự kích hoạt; ingest lưu như sự kiện thường, không có
	// hiệu ứng riêng lên lượt chạy.
	GuardrailTriggered = "guardrail.triggered"
	// G3b: subagent của Claude Code dừng (SubagentStop). payload: agent_id, agent_type, agent_transcript_path (nguồn
	// token để tính chi phí của bước driver session, docs/isolation-subagent-plan.md mục 6.6). Chỉ harness có sự kiện
	// này mới cung cấp; ingest lưu như sự kiện thường.
	SubagentStopped = "subagent.stopped"

	// P3: Context Engine (mục 9.7). payload.ids là danh sách ID tri thức.
	ContextRetrieved    = "context.retrieved"
	KnowledgeCited      = "knowledge.cited"
	ObservationReported = "observation.reported"
	// P3: tín hiệu kết quả do người chấm (bint runs rate).
	RunRated = "run.rated"

	// P4: workflow do Bint điều phối và tác vụ (mục 6). Sự kiện của `bint run`
	// mang run_id do Bint cấp; sự kiện của `bint task` gắn vào lượt chạy hiện tại
	// (BINT_RUN_ID, hoặc phiên harness) hoặc lượt gần nhất của tác vụ.
	WorkflowStarted  = "workflow.started"
	WorkflowFinished = "workflow.finished"
	StepStarted      = "step.started"
	StepFinished     = "step.finished"
	TaskLinked       = "task.linked"
	TaskStatus       = "task.status"
	TaskArtifact     = "task.artifact"
	TaskVerified     = "task.verified"
	TaskCriteria     = "task.criteria"
	TaskError        = "task.error"
	ReviewCompleted  = "review.completed"
)

// Event là một sự kiện trong spool.
type Event struct {
	SchemaVersion int       `json:"schema_version"`
	EventID       string    `json:"event_id"`
	Type          string    `json:"event_type"`
	Source        string    `json:"source"`
	Timestamp     time.Time `json:"timestamp"`
	// Harness và SessionID xác định lượt chạy khi hook không biết run_id.
	Harness   string `json:"harness,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	StepID    string `json:"step_id,omitempty"`
	// Worktree là thư mục gốc của worktree nơi sự kiện phát sinh.
	Worktree string         `json:"worktree,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// Validate kiểm tra các trường bắt buộc.
func (e Event) Validate() error {
	switch {
	case e.SchemaVersion != SchemaVersion:
		return fmt.Errorf("schema_version %d không hỗ trợ", e.SchemaVersion)
	case e.EventID == "":
		return errors.New("thiếu event_id")
	case e.Type == "":
		return errors.New("thiếu event_type")
	case e.Source == "":
		return errors.New("thiếu source")
	case e.Timestamp.IsZero():
		return errors.New("thiếu timestamp")
	case e.RunID == "" && (e.Harness == "" || e.SessionID == ""):
		return errors.New("cần run_id hoặc cặp harness + session_id")
	}
	return nil
}

const (
	fileExt     = ".jsonl"
	tmpPrefix   = ".tmp-"
	rejectedDir = "rejected"
)

// Write ghi các sự kiện vào một tệp spool mới một cách nguyên tử.
func Write(dir string, events ...Event) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	var buf bytes.Buffer
	for _, e := range events {
		if e.SchemaVersion == 0 {
			e.SchemaVersion = SchemaVersion
		}
		if err := e.Validate(); err != nil {
			return "", err
		}
		line, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	name := ulid.New() + fileExt
	tmp, err := os.CreateTemp(dir, tmpPrefix+"*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp.Name(), final); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return final, nil
}

// Pending liệt kê các tệp spool chờ nạp, theo thứ tự thời gian tạo.
func Pending(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, tmpPrefix) || !strings.HasSuffix(n, fileExt) {
			continue
		}
		files = append(files, filepath.Join(dir, n))
	}
	sort.Strings(files)
	return files, nil
}

// StaleTemps liệt kê tệp tạm cũ hơn maxAge (hook bị ngắt giữa chừng).
func StaleTemps(dir string, maxAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > maxAge {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// ErrCorrupt đánh dấu tệp spool có nội dung hỏng (khác với lỗi I/O tạm thời).
var ErrCorrupt = errors.New("tệp spool hỏng")

// ReadFile đọc mọi sự kiện trong một tệp spool. Dòng hỏng làm cả tệp bị từ chối
// với lỗi bọc ErrCorrupt.
func ReadFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, fmt.Errorf("%w: dòng %d: %v", ErrCorrupt, line, err)
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("%w: dòng %d: %v", ErrCorrupt, line, err)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Reject chuyển tệp hỏng sang spool/rejected/ để người dùng kiểm tra.
func Reject(dir, path string) error {
	rd := filepath.Join(dir, rejectedDir)
	if err := os.MkdirAll(rd, 0o700); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(rd, filepath.Base(path)))
}

// RejectedCount đếm tệp trong spool/rejected/.
func RejectedCount(dir string) int {
	entries, err := os.ReadDir(filepath.Join(dir, rejectedDir))
	if err != nil {
		return 0
	}
	return len(entries)
}
