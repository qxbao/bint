package ctxengine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"bint/internal/kindex"
	"bint/internal/knowledge"
)

// EvalEnv là biến môi trường trỏ tới tệp cấu hình ngữ cảnh của một lượt đánh giá
// nhánh bint. Hook và MCP server đọc nó để phục vụ tri thức của lượt đó (thư mục
// đánh giá không có .bint và không có thư mục trạng thái).
const EvalEnv = "BINT_EVAL_CONTEXT"

// EvalConfig mô tả tri thức và nơi ghi nhật ký của một lượt đánh giá.
type EvalConfig struct {
	ItemsDir        string `json:"items_dir"`
	Root            string `json:"root"` // thư mục làm việc của agent
	Log             string `json:"log"`  // nhật ký JSONL: truy xuất, trích dẫn
	ExcludeEvalCase string `json:"exclude_eval_case,omitempty"`
	MaxTokens       int    `json:"max_tokens"`
	PathMaxItems    int    `json:"path_max_items"`
	// IncludeCandidates: tập tri thức gồm mục chưa duyệt (ảnh chụp bootstrap); chèn
	// theo đường dẫn cũng trả chúng (nhãn tham khảo) để cùng tập với nhánh minimal.
	IncludeCandidates bool `json:"include_candidates,omitempty"`
	// LessonGlobalK và LessonMaxTokens: phần bài học từ thất bại (xem Engine).
	LessonGlobalK   int `json:"lesson_global_k,omitempty"`
	LessonMaxTokens int `json:"lesson_max_tokens,omitempty"`
}

// LoadEvalConfig đọc tệp cấu hình.
func LoadEvalConfig(path string) (EvalConfig, error) {
	var c EvalConfig
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.ItemsDir == "" || c.Root == "" {
		return c, fmt.Errorf("%s: thiếu items_dir hoặc root", path)
	}
	return c, nil
}

// Engine dựng Engine với chỉ mục trong bộ nhớ.
func (c EvalConfig) Engine(ctx context.Context) (*Engine, func(), error) {
	items, bad, err := knowledge.Store{Dir: c.ItemsDir}.List()
	if err != nil {
		return nil, nil, err
	}
	if len(bad) > 0 {
		return nil, nil, fmt.Errorf("tri thức đánh giá có tệp hỏng: %v", bad[0])
	}
	s, err := kindex.Memory(ctx, items)
	if err != nil {
		return nil, nil, err
	}
	e := &Engine{Items: items, Src: &knowledge.WorktreeSource{Root: c.Root}, DB: s.DB, MaxTokens: c.MaxTokens,
		PathMaxItems: c.PathMaxItems, ExcludeEvalCase: c.ExcludeEvalCase, PathIncludeCandidates: c.IncludeCandidates,
		LessonGlobalK: c.LessonGlobalK, LessonMaxTokens: c.LessonMaxTokens}
	return e, func() { s.Close() }, nil
}

// LogLine là một dòng nhật ký ngữ cảnh của lượt đánh giá.
type LogLine struct {
	Time    string   `json:"t"`
	Channel string   `json:"channel"` // hook:path | mcp:task | mcp:paths | cite | observe
	IDs     []string `json:"ids"`
	Labels  []string `json:"labels,omitempty"`
}

// AppendLog ghi nối một dòng nhật ký.
func AppendLog(path string, l LogLine) error {
	if path == "" {
		return nil
	}
	l.Time = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// PackLog dựng dòng nhật ký từ gói ngữ cảnh.
func PackLog(channel string, p Pack) LogLine {
	l := LogLine{Channel: channel, IDs: p.IDs()}
	for _, e := range p.Entries {
		l.Labels = append(l.Labels, e.Label)
	}
	return l
}

// ReadLog đọc nhật ký; dòng hỏng bị bỏ qua.
func ReadLog(path string) ([]LogLine, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []LogLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		var l LogLine
		if json.Unmarshal(sc.Bytes(), &l) == nil {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

// Reset xoá bảng tra ID (gọi sau khi thay Items).
func (e *Engine) Reset() { e.byID = nil }

// Eligible cho biết mục có được cung cấp cho agent không, kèm lý do.
func (e *Engine) Eligible(it *knowledge.Item) (bool, string) { return e.eligible(it) }
