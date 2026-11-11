package knowledge

import (
	"slices"
	"sort"
	"strings"
)

// BatchTagPrefix là tiền tố nhãn lô do skill b-knowledge gắn (`--tags nguon:<lô>`).
const BatchTagPrefix = "nguon:"

// Batch trả nhãn lô (`nguon:<lô>`) đầu tiên trong thẻ của phạm vi; rỗng nếu mục không thuộc lô nào.
func (s Scope) Batch() string {
	for _, t := range s.Tags {
		if strings.HasPrefix(t, BatchTagPrefix) && len(t) > len(BatchTagPrefix) {
			return t
		}
	}
	return ""
}

// Batches trả các nhãn lô có trong items, sắp tăng dần, không trùng.
func Batches(items []*Item) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		for _, t := range it.Scope.Tags {
			if strings.HasPrefix(t, BatchTagPrefix) && len(t) > len(BatchTagPrefix) && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Pending cho biết mục đang chờ người (candidate hoặc stale).
func (it *Item) Pending() bool {
	return it.Status == StatusCandidate || it.Status == StatusStale
}

// Filter là bộ lọc mục tri thức dùng chung cho CLI (`knowledge list`) và màn Tri thức của TUI.
// Các điều kiện kết hợp bằng "và"; mỗi danh sách là "hoặc" trong nội bộ. Giá trị zero khớp mọi mục
// còn hiệu lực (không deprecated).
type Filter struct {
	// Statuses, Types, Origins: rỗng = không lọc theo chiều đó.
	Statuses []string
	Types    []string
	Origins  []string // human | agent | imported
	// ThirdParty: chỉ mục gắn cờ bên thứ ba (⚑).
	ThirdParty bool
	// Batch: nhãn lô ("nguon:x"); chấp nhận cả dạng rút gọn "x".
	Batch string
	// NoBatch: chỉ mục không thuộc lô nào (không có nhãn nguon:*). Không dùng cùng Batch.
	NoBatch bool
	// Path: đường dẫn tệp (tương đối repository); giữ mục có phạm vi khớp, cùng quy tắc Scope.MatchScope.
	Path string
	// Text: khớp không phân biệt hoa thường với ID, tiêu đề hoặc nội dung.
	Text string
	// All: gồm cả mục deprecated khi Statuses rỗng.
	All bool
}

// Empty cho biết bộ lọc không giới hạn gì (ngoài việc ẩn mục deprecated).
func (f Filter) Empty() bool {
	return len(f.Statuses) == 0 && len(f.Types) == 0 && len(f.Origins) == 0 && !f.ThirdParty &&
		f.Batch == "" && !f.NoBatch && f.Path == "" && f.Text == "" && !f.All
}

// Match cho biết mục khớp bộ lọc.
func (f Filter) Match(it *Item) bool {
	switch {
	case len(f.Statuses) > 0:
		if !slices.Contains(f.Statuses, it.Status) {
			return false
		}
	case !f.All && !it.Active():
		return false
	}
	if len(f.Types) > 0 && !slices.Contains(f.Types, it.Type) {
		return false
	}
	if len(f.Origins) > 0 && !slices.Contains(f.Origins, it.Origin.Kind) {
		return false
	}
	if f.ThirdParty && !it.Origin.ThirdParty {
		return false
	}
	if f.Batch != "" {
		want := f.Batch
		if !strings.HasPrefix(want, BatchTagPrefix) {
			want = BatchTagPrefix + want
		}
		if !slices.Contains(it.Scope.Tags, want) {
			return false
		}
	}
	if f.NoBatch && it.Scope.Batch() != "" {
		return false
	}
	if f.Path != "" && !it.Scope.MatchScope(cleanRel(f.Path)) {
		return false
	}
	if f.Text != "" {
		q := strings.ToLower(strings.TrimSpace(f.Text))
		if !strings.Contains(strings.ToLower(it.ID), q) && !strings.Contains(strings.ToLower(it.Title), q) &&
			!strings.Contains(strings.ToLower(it.Body), q) {
			return false
		}
	}
	return true
}

// Apply trả các mục khớp bộ lọc, giữ thứ tự.
func (f Filter) Apply(items []*Item) []*Item {
	var out []*Item
	for _, it := range items {
		if f.Match(it) {
			out = append(out, it)
		}
	}
	return out
}
