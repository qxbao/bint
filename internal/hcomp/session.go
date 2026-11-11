package hcomp

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// Loại bản ghi trạng thái phiên.
const (
	RecEdit      = "edit"       // P: tệp đã sửa
	RecCmd       = "cmd"        // C: lệnh shell đã chạy; Test: là lệnh kiểm thử
	RecFired     = "fired"      // ID, A: guardrail đã kích hoạt với hành động A
	RecFollowed  = "followed"   // ID: điều kiện unless đã được thoả sau khi kích hoạt
	RecStopBlock = "stop_block" // ID: guardrail đã chặn kết thúc một lần
	RecBypass    = "bypass"     // ID: deny ở stop vượt trần, cho qua
	RecGuidance  = "guidance"   // ID: guidance riêng đã chèn
	RecEligible  = "eligible"   // ID: điều kiện when đúng lần đầu trong phiên
)

// Record là một dòng JSONL của trạng thái phiên.
type Record struct {
	T    string `json:"t"`
	P    string `json:"p,omitempty"`
	C    string `json:"c,omitempty"`
	Test bool   `json:"test,omitempty"`
	ID   string `json:"id,omitempty"`
	A    string `json:"a,omitempty"`
}

// maxStateBytes giới hạn lượng trạng thái đọc lại (hook phải nhanh).
const maxStateBytes = 4 << 20

// SessionPath trả về tệp trạng thái của một phiên trong thư mục dir (runtime của phiên
// thật, hoặc thư mục log của lượt eval).
func SessionPath(dir, harness, session string) string {
	sum := sha256.Sum256([]byte(harness + "\x00" + session))
	return filepath.Join(dir, "harness", hex.EncodeToString(sum[:])[:24]+".jsonl")
}

// Append nối các bản ghi vào tệp. Mỗi lần hook là một tiến trình riêng nên trạng thái nằm
// trên đĩa; một lần ghi O_APPEND cho cả mảng bản ghi (mỗi dòng nhỏ) nên các hook chạy
// song song không xen kẽ làm hỏng dòng.
func Append(path string, recs ...Record) error {
	if len(recs) == 0 {
		return nil
	}
	var buf []byte
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf = append(append(buf, b...), '\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(buf)
	return err
}

// EditRec và CmdRec giữ thứ tự (Seq) để biết "chạy sau lần sửa cuối".
type EditRec struct {
	Seq  int
	Path string
}

// CmdRec là một lệnh shell đã chạy trong phiên.
type CmdRec struct {
	Seq  int
	Cmd  string
	Test bool
}

// State là trạng thái đã gộp của một phiên.
type State struct {
	Edits      []EditRec
	Cmds       []CmdRec
	Fired      map[string]int
	Followed   map[string]bool
	StopBlocks map[string]int
	Bypassed   map[string]int
	Guidance   map[string]bool
	Eligible   map[string]bool
	seq        int
}

func newState() *State {
	return &State{Fired: map[string]int{}, Followed: map[string]bool{}, StopBlocks: map[string]int{},
		Bypassed: map[string]int{}, Guidance: map[string]bool{}, Eligible: map[string]bool{}}
}

// ReadState đọc trạng thái của phiên; tệp thiếu hoặc dòng hỏng cho trạng thái rỗng/bỏ qua
// dòng đó (không bao giờ trả lỗi).
func ReadState(path string) *State {
	st := newState()
	f, err := os.Open(path)
	if err != nil {
		return st
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, maxStateBytes))
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec Record
			if json.Unmarshal(line, &rec) == nil {
				st.Apply(rec)
			}
		}
		if err != nil {
			break
		}
	}
	return st
}

// Apply cộng một bản ghi vào trạng thái (người gọi tự nối bản ghi vào tệp bằng Append).
func (s *State) Apply(r Record) {
	s.seq++
	switch r.T {
	case RecEdit:
		s.Edits = append(s.Edits, EditRec{Seq: s.seq, Path: r.P})
	case RecCmd:
		s.Cmds = append(s.Cmds, CmdRec{Seq: s.seq, Cmd: r.C, Test: r.Test})
	case RecFired:
		s.Fired[r.ID]++
	case RecFollowed:
		s.Followed[r.ID] = true
	case RecStopBlock:
		s.StopBlocks[r.ID]++
	case RecBypass:
		s.Bypassed[r.ID]++
	case RecGuidance:
		s.Guidance[r.ID] = true
	case RecEligible:
		s.Eligible[r.ID] = true
	}
}

// EditedPaths trả về các tệp đã sửa (không trùng).
func (s *State) EditedPaths() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range s.Edits {
		if !seen[e.Path] {
			seen[e.Path] = true
			out = append(out, e.Path)
		}
	}
	return out
}
