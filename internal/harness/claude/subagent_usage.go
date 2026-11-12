package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"

	"bint/internal/config"
)

// ModelUsage là token của một model trong transcript (một bậc giá).
type ModelUsage struct {
	Model string `json:"model"`
	// Long: prompt của các lượt gọi này vượt LongPromptTokens (bậc giá dài).
	Long       bool  `json:"long,omitempty"`
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
}

// Usage là token của một subagent, cộng từ transcript (docs/isolation-subagent-plan.md mục 6.6).
type Usage struct {
	Models []ModelUsage
	// Turns: số lượt gọi API (số message.id khác nhau).
	Turns int
}

// Totals trả tổng token vào (gồm cả bộ nhớ đệm, như TranscriptUsage) và ra.
func (u Usage) Totals() (in, out int64) {
	for _, m := range u.Models {
		in += m.Input + m.CacheWrite + m.CacheRead
		out += m.Output
	}
	return in, out
}

// Model trả model dùng nhiều token ra nhất ("" nếu rỗng).
func (u Usage) Model() string {
	best, bestOut := "", int64(-1)
	for _, m := range u.Models {
		if m.Output > bestOut {
			best, bestOut = m.Model, m.Output
		}
	}
	return best
}

// Cost quy USD theo bảng giá (override ghi đè bảng dựng sẵn). nil nếu có model không có giá (không đoán) hoặc không có model.
func (u Usage) Cost(override map[string]config.ModelPrice) *float64 {
	if len(u.Models) == 0 {
		return nil
	}
	var total float64
	for _, m := range u.Models {
		p, ok := PriceFor(m.Model, override, m.Long)
		if !ok {
			return nil
		}
		total += (float64(m.Input)*p.InputUSDPerMTok + float64(m.Output)*p.OutputUSDPerMTok +
			float64(m.CacheWrite)*p.CacheWriteUSDPerMTok + float64(m.CacheRead)*p.CacheReadUSDPerMTok) / 1e6
	}
	return &total
}

// SubagentUsage đọc transcript JSONL của một subagent (agent_transcript_path của SubagentStop) và cộng message.usage của
// các dòng assistant, tách theo model. Một thông điệp có thể xuất hiện nhiều dòng (mỗi khối nội dung một dòng) với cùng
// message.id: chỉ tính một lần, lấy bản cuối (usage lũy kế). Trả nil nếu tệp không đọc được hoặc không có dòng usage nào
// (không biết, không phải 0). Dòng không có message.id được tính từng dòng.
func SubagentUsage(path string) *Usage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	type msg struct {
		model                          string
		in, out, cacheWrite, cacheRead int64
	}
	byID := map[string]msg{}
	var order []string
	var anon []msg
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Usage *struct {
					In    *int64 `json:"input_tokens"`
					Out   *int64 `json:"output_tokens"`
					Cache *int64 `json:"cache_creation_input_tokens"`
					Read  *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "assistant" || l.Message.Usage == nil || l.Message.Model == "<synthetic>" {
			continue
		}
		u := l.Message.Usage
		if u.In == nil && u.Out == nil {
			continue
		}
		val := func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		}
		m := msg{model: l.Message.Model, in: val(u.In), out: val(u.Out), cacheWrite: val(u.Cache), cacheRead: val(u.Read)}
		if l.Message.ID == "" {
			anon = append(anon, m)
			continue
		}
		if _, seen := byID[l.Message.ID]; !seen {
			order = append(order, l.Message.ID)
		}
		byID[l.Message.ID] = m
	}
	if len(byID)+len(anon) == 0 {
		return nil
	}
	type key struct {
		model string
		long  bool
	}
	agg := map[key]*ModelUsage{}
	add := func(m msg) {
		k := key{m.model, m.in+m.cacheWrite+m.cacheRead > LongPromptTokens}
		a := agg[k]
		if a == nil {
			a = &ModelUsage{Model: k.model, Long: k.long}
			agg[k] = a
		}
		a.Input += m.in
		a.Output += m.out
		a.CacheWrite += m.cacheWrite
		a.CacheRead += m.cacheRead
	}
	for _, id := range order {
		add(byID[id])
	}
	for _, m := range anon {
		add(m)
	}
	res := &Usage{Turns: len(byID) + len(anon)}
	for _, a := range agg {
		res.Models = append(res.Models, *a)
	}
	sort.Slice(res.Models, func(i, j int) bool {
		if res.Models[i].Model != res.Models[j].Model {
			return res.Models[i].Model < res.Models[j].Model
		}
		return !res.Models[i].Long && res.Models[j].Long
	})
	return res
}
