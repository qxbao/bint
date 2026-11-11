package evolve

import (
	"fmt"
	"sort"
	"strings"

	"bint/internal/eval"
)

// Khoá cơ hội quy trình (H3).
const (
	harnessSignalPrefix = "harness:signal:"
	harnessMCPUnusedKey = "harness:mcp_unused"
	// SigMCPUnused là tên (giả) của tín hiệu kênh MCP không được gọi.
	SigMCPUnused = "mcp_unused"
)

// harnessSuggest: loại thành phần phù hợp với từng dấu hiệu hành vi. Đây chỉ là gợi ý cho người
// đề xuất; kiểm tra xác định mới quyết định đề xuất có hợp lệ không.
var harnessSuggest = map[string]string{
	SigNoTest:      "guardrail (ví dụ test-before-stop)",
	SigEditOutside: "guardrail (nhắc/hỏi khi sửa ngoài vùng)",
	SigRepeatCmd:   "chỉ dẫn (cách tránh lặp lệnh)",
	SigTimeout:     "chỉ dẫn (cách chia nhỏ việc để không hết lượt)",
	SigIgnoredCtx:  "chiến lược ngữ cảnh (chọn/đưa tri thức khác đi)",
	SigMCPUnused:   "chỉ dẫn (cách dùng công cụ MCP của Bint)",
}

// harnessOpps gom lượt thất bại trên tập VALIDATION thành cơ hội quy trình: một cơ hội cho mỗi dấu
// hiệu hành vi của E1 (no_test, edit_outside → guardrail; repeat_cmd, timeout → chỉ dẫn;
// ignored_ctx → chiến lược), và một cho kênh MCP không được gọi ở nhánh bint. Dùng cùng ngưỡng như
// bài học: ≥ min tác vụ phân biệt và ≥ e lượt chưa dùng cho đề xuất trước cùng khoá. Lượt thật
// và tập test không bao giờ vào đây (tập test không là đầu vào của agent đề xuất).
func harnessOpps(fails []Failure, min, e int, used map[string]map[string]bool, pending map[string]string) []Opportunity {
	buckets := map[string][]Failure{}
	label := map[string]string{}
	for _, f := range fails {
		if f.Source != "eval" || f.Split != eval.SplitValidation {
			continue
		}
		for _, s := range f.Signals {
			if _, ok := harnessSuggest[s]; ok {
				buckets[harnessSignalPrefix+s] = append(buckets[harnessSignalPrefix+s], f)
				label[harnessSignalPrefix+s] = signalLabels[s]
			}
		}
		if mcpUnused(f) {
			buckets[harnessMCPUnusedKey] = append(buckets[harnessMCPUnusedKey], f)
			label[harnessMCPUnusedKey] = "kênh MCP của Bint không được gọi ở nhánh bint"
		}
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Opportunity
	for _, key := range keys {
		bs := buckets[key]
		ids, units := map[string]bool{}, map[string]bool{}
		for _, f := range bs {
			ids[f.ID] = true
			units[f.Unit] = true
		}
		supporting := sortedKeys(ids)
		newEv := 0
		for _, id := range supporting {
			if !used[key][id] {
				newEv++
			}
		}
		sig := strings.TrimPrefix(key, harnessSignalPrefix)
		if key == harnessMCPUnusedKey {
			sig = SigMCPUnused
		}
		o := Opportunity{Key: key, Kind: KindHarness, Action: ActionCreate, Generator: GenAgent,
			Group: sig, SupportingRuns: supporting, NewEvidence: newEv}
		o.Summary = fmt.Sprintf("%d tác vụ validation thất bại (%d lượt) có dấu hiệu %q; gợi ý: %s", len(units), len(supporting), label[key], harnessSuggest[sig])
		switch {
		case len(units) < min:
			o.Blocked = fmt.Sprintf("chỉ %d tác vụ thất bại (cần ≥ %d, evolution.trigger.min_similar_runs)", len(units), min)
		case newEv < e:
			o.Blocked = fmt.Sprintf("chỉ %d lượt mới so với đề xuất trước cùng dấu hiệu (cần ≥ %d, evolution.trigger.min_new_evidence)", newEv, e)
		default:
			o.Ready = true
		}
		if p, ok := pending[key]; ok {
			o.Ready, o.Blocked = false, "đã có đề xuất "+p+" chờ xử lý"
		}
		out = append(out, o)
	}
	return out
}

// mcpUnused: lượt eval nhánh bint có vết đầy đủ mà không gọi công cụ MCP nào của Bint. Lượt của
// biến thể tắt MCP có chủ đích (`bint@mcp=off`) không tính: MCPCalls luôn 0 ở đó, không phải
// dấu hiệu guidance kém.
func mcpUnused(f Failure) bool {
	base, cv, err := eval.SplitArm(f.Arm)
	return err == nil && !cv.NoMCP && f.Source == "eval" && base == eval.ArmBint && f.Measured && f.Trace.Available && !f.Trace.Truncated && f.Trace.MCPCalls == 0
}
