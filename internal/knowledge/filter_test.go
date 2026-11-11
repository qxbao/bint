package knowledge

import (
	"strings"
	"testing"
)

func fItem(id, status string, mod func(*Item)) *Item {
	it := &Item{ID: id, Type: TypeFact, Status: status, Title: "Tiêu đề " + id, Body: "nội dung " + id, Origin: Origin{Kind: OriginAgent}}
	if mod != nil {
		mod(it)
	}
	return it
}

func TestScopeBatchAndBatches(t *testing.T) {
	a := fItem("A", StatusCandidate, func(i *Item) { i.Scope.Tags = []string{"db", "nguon:lo-2", "nguon:lo-1"} })
	b := fItem("B", StatusCandidate, func(i *Item) { i.Scope.Tags = []string{"nguon:lo-1", "nguon:"} })
	c := fItem("C", StatusCandidate, nil)
	if got := a.Scope.Batch(); got != "nguon:lo-2" {
		t.Fatalf("lô đầu tiên: %q", got)
	}
	if got := c.Scope.Batch(); got != "" {
		t.Fatalf("%q", got)
	}
	if got := strings.Join(Batches([]*Item{a, b, c}), ","); got != "nguon:lo-1,nguon:lo-2" {
		t.Fatalf("Batches: %s (nhãn rỗng sau tiền tố không tính)", got)
	}
}

func TestFilterMatchTable(t *testing.T) {
	items := []*Item{
		fItem("1", StatusCandidate, func(i *Item) {
			i.Type = TypeRule
			i.Scope = Scope{Paths: []string{"internal/db/**"}, Tags: []string{"nguon:x"}}
			i.Origin.ThirdParty = true
		}),
		fItem("2", StatusVerified, func(i *Item) { i.Origin.Kind = OriginHuman }),
		fItem("3", StatusDeprecated, nil),
		fItem("4", StatusStale, func(i *Item) { i.Scope.Paths = []string{"cmd"} }),
	}
	ids := func(f Filter) string {
		var s []string
		for _, it := range f.Apply(items) {
			s = append(s, it.ID)
		}
		return strings.Join(s, "")
	}
	cases := []struct {
		name string
		f    Filter
		want string
	}{
		{"mặc định ẩn deprecated", Filter{}, "124"},
		{"All gồm deprecated", Filter{All: true}, "1234"},
		{"trạng thái rõ", Filter{Statuses: []string{StatusDeprecated}}, "3"},
		{"hai trạng thái", Filter{Statuses: []string{StatusCandidate, StatusStale}}, "14"},
		{"loại", Filter{Types: []string{TypeRule}}, "1"},
		{"nguồn", Filter{Origins: []string{OriginHuman}}, "2"},
		{"bên thứ ba", Filter{ThirdParty: true}, "1"},
		{"lô đầy đủ", Filter{Batch: "nguon:x"}, "1"},
		{"lô rút gọn", Filter{Batch: "x"}, "1"},
		{"đường dẫn glob", Filter{Path: "internal/db/db.go"}, "1" + "2"},
		{"đường dẫn thư mục (mục phạm vi không glob)", Filter{Path: "cmd/bint/main.go"}, "24"},
		{"đường dẫn dạng ./", Filter{Path: "./internal/db/db.go"}, "12"},
		{"chữ hoa thường", Filter{Text: "TIÊU ĐỀ 2"}, "2"},
		{"chữ trong nội dung", Filter{Text: "nội dung 4"}, "4"},
		{"tổ hợp", Filter{Statuses: []string{StatusCandidate}, Batch: "x", ThirdParty: true, Types: []string{TypeRule}}, "1"},
		{"không thuộc lô nào", Filter{NoBatch: true}, "24"},
		{"chờ duyệt và không thuộc lô", Filter{Statuses: []string{StatusCandidate, StatusStale}, NoBatch: true}, "4"},
		{"tổ hợp rỗng", Filter{Statuses: []string{StatusVerified}, ThirdParty: true}, ""},
	}
	for _, c := range cases {
		if got := ids(c.f); got != c.want {
			t.Errorf("%s: %q, muốn %q", c.name, got, c.want)
		}
	}
	if !(Filter{}).Empty() || (Filter{Text: "x"}).Empty() || (Filter{All: true}).Empty() {
		t.Error("Empty")
	}
	if !items[0].Pending() || items[1].Pending() || !items[3].Pending() {
		t.Error("Pending: candidate và stale")
	}
}
