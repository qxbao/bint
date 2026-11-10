package claude

import (
	"bufio"
	"encoding/json"
	"os"
)

// TranscriptUsage cộng token của các lượt trả lời trong transcript JSONL của Claude Code.
// input gồm cả token tạo/đọc bộ nhớ đệm (cùng cách tính với lượt `claude -p`). Một thông
// điệp có thể xuất hiện nhiều dòng (mỗi khối nội dung một dòng): chỉ tính một lần theo
// message.id, lấy bản cuối. Không có dòng usage nào thì trả nil, nil (không biết, không phải 0).
// Chỉ đọc tệp phiên chính; token của subagent nằm ở tệp riêng và không được cộng.
func TranscriptUsage(path string) (in, out *int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	type usage struct{ in, out int64 }
	byID := map[string]usage{}
	var anon usage
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage *struct {
					In    *int64 `json:"input_tokens"`
					Out   *int64 `json:"output_tokens"`
					Cache *int64 `json:"cache_creation_input_tokens"`
					Read  *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "assistant" || l.Message.Usage == nil {
			continue
		}
		u := l.Message.Usage
		if u.In == nil && u.Out == nil {
			continue
		}
		var x usage
		for _, p := range []*int64{u.In, u.Cache, u.Read} {
			if p != nil {
				x.in += *p
			}
		}
		if u.Out != nil {
			x.out = *u.Out
		}
		found = true
		if l.Message.ID == "" {
			anon.in += x.in
			anon.out += x.out
			continue
		}
		byID[l.Message.ID] = x
	}
	if !found {
		return nil, nil
	}
	ti, to := anon.in, anon.out
	for _, x := range byID {
		ti += x.in
		to += x.out
	}
	return &ti, &to
}
