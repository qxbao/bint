package ctxengine

import (
	"context"
	"sort"
	"strings"
	"testing"

	"bint/internal/kindex"
	"bint/internal/knowledge"
	"bint/internal/ulid"
)

type mem map[string]string

func (m mem) Name() string { return "mem" }
func (m mem) Read(rel string) ([]byte, bool) {
	s, ok := m[rel]
	return []byte(s), ok
}
func (m mem) Files() []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func item(typ, status, title, body string, paths ...string) *knowledge.Item {
	return &knowledge.Item{Schema: 1, ID: ulid.New(), Type: typ, Status: status, Title: title, Body: body,
		Scope: knowledge.Scope{Paths: paths}, Origin: knowledge.Origin{Kind: knowledge.OriginHuman}, Version: 1,
		CreatedAt: "2026-01-01T00:00:00Z"}
}

func setup(t *testing.T) (*Engine, map[string]*knowledge.Item) {
	src := mem{"internal/db/db.go": "package db\n\nfunc Open() {}\n", "internal/api/h.go": "package api\n", "README.md": "x"}
	its := map[string]*knowledge.Item{
		"dbRule":   item("rule", "verified", "Không mở kết nối DB trong handler", "Dùng db.Pool dùng chung.", "internal/db/**"),
		"dbWarn":   item("warning", "verified", "Migration chạy theo thứ tự tên tệp", "Đặt tiền tố số.", "internal/db/**"),
		"dbCand":   item("pattern", "candidate", "Mẫu repository cho DB", "Mỗi bảng một struct.", "internal/db/**"),
		"apiRule":  item("rule", "verified", "Handler trả lỗi dạng problem+json", "Dùng api.Problem.", "internal/api/**"),
		"global":   item("rule", "verified", "Chạy make check trước khi commit", "Luôn.", ""),
		"dep":      item("rule", "deprecated", "Quy tắc DB cũ", "Bỏ.", "internal/db/**"),
		"tp":       item("warning", "candidate", "Thư viện ngoài yêu cầu flag", "x", "internal/db/**"),
		"staleRef": item("warning", "verified", "Open phải gọi Ping", "Gọi Ping sau Open.", "internal/db/**"),
	}
	its["global"].Scope.Paths = nil
	its["tp"].Origin.ThirdParty = true
	// Bằng chứng của staleRef đã đổi so với fingerprint lưu.
	its["staleRef"].Evidence = []knowledge.Evidence{{Path: "internal/db/db.go", Lines: "3", Fingerprint: "lines:0000000000000000"}}
	var list []*knowledge.Item
	for _, it := range its {
		list = append(list, it)
	}
	db, err := kindex.Memory(context.Background(), list)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Engine{Items: list, Src: src, DB: db.DB, MaxTokens: 6000, PathMaxItems: 5}, its
}

func TestForPaths(t *testing.T) {
	e, its := setup(t)
	p := e.ForPaths([]string{"./internal/db/db.go"}, nil)
	ids := strings.Join(p.IDs(), ",")
	for _, want := range []string{"dbRule", "dbWarn", "staleRef"} {
		if !strings.Contains(ids, its[want].ID) {
			t.Errorf("thiếu %s: %v", want, p.Entries)
		}
	}
	for _, no := range []string{"apiRule", "global", "dep", "tp", "dbCand"} {
		if strings.Contains(ids, its[no].ID) {
			t.Errorf("không được có %s", no)
		}
	}
	if p.Entries[0].ID != its["dbRule"].ID || p.Entries[0].Label != LabelRule {
		t.Errorf("rule phải đứng đầu: %+v", p.Entries[0])
	}
	for _, en := range p.Entries {
		if en.ID == its["staleRef"].ID && en.Label != LabelReference {
			t.Errorf("bằng chứng đổi phải hạ thành tham khảo: %+v", en)
		}
	}
	out := p.Render()
	if !strings.Contains(out, "DỮ LIỆU THAM KHẢO") || !strings.Contains(out, "[k:"+its["dbRule"].ID+"]") {
		t.Fatal(out)
	}
	// Không lặp lại mục đã chèn trong phiên.
	p2 := e.ForPaths([]string{"internal/db/db.go"}, map[string]bool{its["dbRule"].ID: true})
	if strings.Contains(strings.Join(p2.IDs(), ","), its["dbRule"].ID) {
		t.Fatal("lặp mục đã chèn")
	}
	if p := e.ForPaths([]string{"docs/x.md"}, nil); len(p.Entries) != 0 || p.Render() != "" {
		t.Fatalf("không khớp phải rỗng: %+v", p)
	}
}

func TestForTask(t *testing.T) {
	e, its := setup(t)
	p, err := e.ForTask(context.Background(), "Sửa lỗi handler trong internal/api/h.go trả về problem json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) == 0 || p.Entries[0].ID != its["apiRule"].ID {
		t.Fatalf("%+v", p.Entries)
	}
	if p.WorkKind != "bugfix" || len(p.Paths) != 1 || p.Paths[0] != "internal/api/h.go" {
		t.Fatalf("%+v", p)
	}
	// Mục chưa duyệt được trả về nhưng gắn nhãn tham khảo.
	p, _ = e.ForTask(context.Background(), "thêm mẫu repository cho bảng mới", nil)
	found := false
	for _, en := range p.Entries {
		if en.ID == its["dbCand"].ID {
			found = true
			if en.Label != LabelReference {
				t.Fatalf("%+v", en)
			}
		}
		if en.ID == its["tp"].ID || en.ID == its["dep"].ID {
			t.Fatal("mục bị loại vẫn trả về")
		}
	}
	if !found {
		t.Fatalf("%+v", p.Entries)
	}
	// Ngân sách rất nhỏ: bỏ bớt mục và ghi lý do.
	e.MaxTokens = 120
	p, _ = e.ForTask(context.Background(), "db migration handler problem repository open ping", nil)
	if len(p.Omitted) == 0 {
		t.Fatalf("phải bỏ bớt: %+v", p)
	}
	if p.Tokens > 0 && len(p.Entries) > 0 && p.Tokens > 200 {
		t.Fatalf("vượt ngân sách: %d", p.Tokens)
	}
}
