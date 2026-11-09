package kindex

import (
	"context"
	"path/filepath"
	"testing"

	"bint/internal/knowledge"
	"bint/internal/store"
	"bint/internal/ulid"
)

func mk(typ, title, body string, paths ...string) *knowledge.Item {
	return &knowledge.Item{Schema: 1, ID: ulid.New(), Type: typ, Status: knowledge.StatusVerified, Title: title, Body: body,
		Scope: knowledge.Scope{Paths: paths}, Origin: knowledge.Origin{Kind: knowledge.OriginHuman}, Version: 1,
		CreatedAt: "2026-01-01T00:00:00Z"}
}

func TestSyncSearchAndIncremental(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := store.Open(ctx, filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ks := knowledge.Store{Dir: filepath.Join(dir, "items")}
	a := mk(knowledge.TypeWarning, "Migration phải chạy qua makeMigrate", "Không sửa trực tiếp tệp sinh ra; chạy `make migrate`.", "db/migrations/**")
	a.Scope.Symbols = []string{"RunMigrations"}
	b := mk(knowledge.TypeRule, "Định dạng tiền tệ", "Luôn dùng money.Format, không dùng fmt cho số tiền.", "internal/billing/**")
	for _, it := range []*knowledge.Item{a, b} {
		if err := ks.Save(it); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Sync(ctx, s.DB, ks, false)
	if err != nil || r.Upserted != 2 || r.Total != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	if r, _ := Sync(ctx, s.DB, ks, false); !r.Skipped {
		t.Fatalf("chữ ký không đổi phải bỏ qua: %+v", r)
	}
	hits, err := Search(ctx, s.DB, "thêm migrations cho bảng users", SearchOptions{})
	if err != nil || len(hits) == 0 || hits[0].ID != a.ID {
		t.Fatalf("%+v %v", hits, err)
	}
	// Không dấu vẫn khớp có dấu; ký hiệu tách CamelCase.
	if hits, _ := Search(ctx, s.DB, "tien te", SearchOptions{}); len(hits) != 1 || hits[0].ID != b.ID {
		t.Fatalf("bỏ dấu: %+v", hits)
	}
	if hits, _ := Search(ctx, s.DB, "run migrations", SearchOptions{}); len(hits) == 0 || hits[0].ID != a.ID {
		t.Fatalf("ký hiệu: %+v", hits)
	}
	// Sửa một mục, xoá một mục → chỉ cập nhật phần đổi.
	b.Title = "Định dạng tiền tệ bằng money"
	ks.Save(b)
	ks.Remove(a.ID)
	r, err = Sync(ctx, s.DB, ks, false)
	if err != nil || r.Upserted != 1 || r.Removed != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if hits, _ := Search(ctx, s.DB, "migration", SearchOptions{}); len(hits) != 0 {
		t.Fatalf("mục đã xoá vẫn còn: %+v", hits)
	}
	// Truy vấn có ký tự đặc biệt của FTS5 không gây lỗi.
	if _, err := Search(ctx, s.DB, `"a" OR (b* NEAR c) -d`, SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	m, err := Memory(ctx, []*knowledge.Item{a, b})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if hits, _ := Search(ctx, m.DB, "money", SearchOptions{}); len(hits) != 1 {
		t.Fatalf("bộ nhớ: %+v", hits)
	}
}

func TestSplitIdent(t *testing.T) {
	got := splitIdent("parseHTTPRequest_v2")
	want := []string{"parse", "http", "request", "v2"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
}
