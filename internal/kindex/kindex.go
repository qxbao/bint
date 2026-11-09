// Package kindex là chỉ mục FTS5 dẫn xuất từ tệp tri thức (mục 8.3, 8.6). Mọi
// dữ liệu trong chỉ mục dựng lại được từ .bint/knowledge/items; chỉ mục không
// bao giờ là nguồn chuẩn.
package kindex

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"bint/internal/knowledge"
	"bint/internal/store"
)

const metaSignature = "items_signature"

// SyncResult là kết quả đồng bộ chỉ mục với tệp.
type SyncResult struct {
	Upserted int
	Removed  int
	Total    int
	Skipped  bool // chữ ký thư mục không đổi, không cần đọc lại
	Errors   []knowledge.LoadError
}

// dirSignature băm danh sách (tên, kích thước, thời điểm sửa) của tệp tri thức.
func dirSignature(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "empty", nil
	}
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", e.Name(), fi.Size(), fi.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Sync cập nhật chỉ mục theo thư mục tệp tri thức: bỏ qua nếu chữ ký thư mục
// không đổi (trừ khi force), ngược lại đọc lại và chỉ ghi mục có nội dung đổi.
// Gọi ở đầu mọi lệnh đọc chỉ mục để bắt thay đổi sau `git pull` hay đổi nhánh.
func Sync(ctx context.Context, db *sql.DB, ks knowledge.Store, force bool) (SyncResult, error) {
	var res SyncResult
	sig, err := dirSignature(ks.Dir)
	if err != nil {
		return res, err
	}
	if !force {
		var old string
		err := db.QueryRowContext(ctx, `SELECT value FROM knowledge_index_meta WHERE key = ?`, metaSignature).Scan(&old)
		if err == nil && old == sig {
			res.Skipped = true
			db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_index`).Scan(&res.Total)
			return res, nil
		}
	}
	items, bad, err := ks.List()
	if err != nil {
		return res, err
	}
	res.Errors = bad
	if err := Load(ctx, db, items, force, &res); err != nil {
		return res, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO knowledge_index_meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaSignature, sig)
	return res, err
}

// Rebuild xoá và dựng lại toàn bộ chỉ mục từ tệp.
func Rebuild(ctx context.Context, db *sql.DB, ks knowledge.Store) (SyncResult, error) {
	return Sync(ctx, db, ks, true)
}

// Load đưa danh sách mục vào chỉ mục (dùng cho cả chỉ mục trong bộ nhớ). Với
// replace = true, mọi dòng cũ bị xoá trước.
func Load(ctx context.Context, db *sql.DB, items []*knowledge.Item, replace bool, res *SyncResult) error {
	if res == nil {
		res = &SyncResult{}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_index`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts`); err != nil {
			return err
		}
	}
	existing := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT item_id, file_sha FROM knowledge_index`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, sha string
		if err := rows.Scan(&id, &sha); err != nil {
			rows.Close()
			return err
		}
		existing[id] = sha
	}
	rows.Close()
	now := store.FormatTime(time.Now())
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.ID] = true
		data, err := it.Marshal()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if existing[it.ID] == sha {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE item_id = ?`, it.ID); err != nil {
			return err
		}
		third := 0
		if it.Origin.ThirdParty {
			third = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_index
			(item_id, type, status, title, scope_paths, symbols, tags, domains, origin_kind, third_party, eval_case, author, file_sha, indexed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)
			ON CONFLICT(item_id) DO UPDATE SET type=excluded.type, status=excluded.status, title=excluded.title,
				scope_paths=excluded.scope_paths, symbols=excluded.symbols, tags=excluded.tags, domains=excluded.domains,
				origin_kind=excluded.origin_kind, third_party=excluded.third_party, eval_case=excluded.eval_case,
				author=excluded.author, file_sha=excluded.file_sha, indexed_at=excluded.indexed_at`,
			it.ID, it.Type, it.Status, it.Title, jsonList(it.Scope.Paths), jsonList(it.Scope.Symbols), jsonList(it.Scope.Tags),
			jsonList(it.Scope.Domains), it.Origin.Kind, third, it.Origin.EvalCase, it.Author, sha, now); err != nil {
			return err
		}
		syms := append(append([]string{}, it.Scope.Symbols...), evidenceSymbols(it)...)
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_fts(item_id, title, body, tags, paths, symbols) VALUES (?, ?, ?, ?, ?, ?)`,
			it.ID, it.Title, it.Body, strings.Join(append(append([]string{}, it.Scope.Tags...), it.Scope.Domains...), " "),
			pathTerms(it), symbolTerms(syms)); err != nil {
			return err
		}
		res.Upserted++
	}
	for id := range existing {
		if seen[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_index WHERE item_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE item_id = ?`, id); err != nil {
			return err
		}
		res.Removed++
	}
	res.Total = len(items)
	return tx.Commit()
}

func evidenceSymbols(it *knowledge.Item) []string {
	var out []string
	for _, e := range it.Evidence {
		if e.Symbol != "" {
			out = append(out, e.Symbol)
		}
	}
	return out
}

func pathTerms(it *knowledge.Item) string {
	var ps []string
	ps = append(ps, it.Scope.Paths...)
	for _, e := range it.Evidence {
		ps = append(ps, e.Path)
	}
	return strings.Join(ps, " ")
}

// symbolTerms thêm dạng tách CamelCase/snake_case để "OpenDB" khớp "open db".
func symbolTerms(syms []string) string {
	var out []string
	for _, s := range syms {
		out = append(out, s)
		if parts := splitIdent(s); len(parts) > 1 {
			out = append(out, strings.Join(parts, " "))
		}
	}
	return strings.Join(out, " ")
}

func splitIdent(s string) []string {
	var parts []string
	var cur []rune
	rs := []rune(s)
	flush := func() {
		if len(cur) > 0 {
			parts = append(parts, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return parts
}

func jsonList(s []string) string {
	if s == nil {
		s = []string{}
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// Hit là một kết quả tìm kiếm.
type Hit struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Status string  `json:"status"`
	Title  string  `json:"title"`
	Score  float64 `json:"score"` // càng lớn càng liên quan
}

var tokenRe = regexp.MustCompile(`[\pL\pN_]+`)

// stopwords là từ quá phổ biến trong mô tả tác vụ (Việt, Anh).
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "to": true, "of": true, "in": true, "on": true, "a": true, "an": true,
	"is": true, "be": true, "add": true, "fix": true, "make": true, "use": true, "should": true, "this": true, "that": true,
	"và": true, "của": true, "cho": true, "các": true, "một": true, "những": true, "khi": true, "để": true, "với": true,
	"trong": true, "là": true, "có": true, "không": true, "được": true, "này": true, "thêm": true, "sửa": true, "làm": true,
}

// Terms tách truy vấn thành từ khoá (bỏ từ dừng, trùng lặp).
func Terms(q string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		t = strings.ToLower(t)
		if len([]rune(t)) < 2 || stopwords[t] || seen[t] {
			return
		}
		seen[t] = true
		out = append(out, t)
	}
	for _, t := range tokenRe.FindAllString(q, -1) {
		add(t)
		if parts := splitIdent(t); len(parts) > 1 {
			for _, p := range parts {
				add(p)
			}
		}
	}
	return out
}

// MatchQuery dựng biểu thức FTS5: các từ khoá nối bằng OR (xếp hạng BM25 lo phần
// "khớp nhiều từ hơn thì cao hơn"); mỗi từ được trích dẫn để không bị hiểu là
// toán tử, kèm tiền tố để "migration" khớp "migrations".
func MatchQuery(terms []string) string {
	var qs []string
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, ``)
		if t == "" {
			continue
		}
		q := `"` + t + `"`
		if len([]rune(t)) >= 4 {
			q += "*"
		}
		qs = append(qs, q)
	}
	return strings.Join(qs, " OR ")
}

// SearchOptions điều khiển tìm kiếm.
type SearchOptions struct {
	Limit int
	// IncludeDeprecated trả cả mục đã loại bỏ (mặc định bỏ).
	IncludeDeprecated bool
}

// Search tìm mục theo từ khoá với BM25 (tiêu đề nặng nhất, rồi thẻ/đường dẫn/ký
// hiệu, rồi nội dung).
func Search(ctx context.Context, db *sql.DB, query string, opts SearchOptions) ([]Hit, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	mq := MatchQuery(Terms(query))
	if mq == "" {
		return nil, nil
	}
	cond := `AND i.status <> 'deprecated'`
	if opts.IncludeDeprecated {
		cond = ""
	}
	rows, err := db.QueryContext(ctx, `SELECT i.item_id, i.type, i.status, i.title,
			bm25(knowledge_fts, 0.0, 4.0, 1.0, 2.0, 2.0, 2.0) AS r
		FROM knowledge_fts f JOIN knowledge_index i ON i.item_id = f.item_id
		WHERE knowledge_fts MATCH ? `+cond+`
		ORDER BY r LIMIT ?`, mq, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("tìm kiếm FTS5: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var r float64
		if err := rows.Scan(&h.ID, &h.Type, &h.Status, &h.Title, &r); err != nil {
			return nil, err
		}
		h.Score = -r // bm25 của SQLite: càng âm càng liên quan
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Memory dựng chỉ mục trong bộ nhớ từ danh sách mục (thư mục đánh giá, hook).
func Memory(ctx context.Context, items []*knowledge.Item) (*store.Store, error) {
	s, err := store.OpenMemory(ctx)
	if err != nil {
		return nil, err
	}
	if err := Load(ctx, s.DB, items, true, nil); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Stats là số lần truy xuất / trích dẫn của một mục (mục 8.8, 9.7).
type Stats struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Retrieved int    `json:"retrieved"`
	Cited     int    `json:"cited"`
	Runs      int    `json:"runs"` // số lượt chạy khác nhau đã truy xuất
	LastUsed  string `json:"last_used,omitempty"`
}

// ItemStats tổng hợp số lần truy xuất và trích dẫn của mỗi mục trong chỉ mục.
// Đây chỉ là mức "được truy xuất" và "được áp dụng"; tác động thực chỉ đo được
// bằng đánh giá loại bỏ (mục 9.7).
func ItemStats(ctx context.Context, db *sql.DB) ([]Stats, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.item_id, i.type, i.status, i.title, COALESCE(i.author, ''),
			(SELECT COUNT(*) FROM context_usages u WHERE u.item_id = i.item_id),
			(SELECT COUNT(*) FROM knowledge_citations c WHERE c.item_id = i.item_id),
			(SELECT COUNT(DISTINCT u.run_id) FROM context_usages u WHERE u.item_id = i.item_id),
			COALESCE((SELECT MAX(u.created_at) FROM context_usages u WHERE u.item_id = i.item_id), '')
		FROM knowledge_index i ORDER BY i.item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stats
	for rows.Next() {
		var s Stats
		if err := rows.Scan(&s.ID, &s.Type, &s.Status, &s.Title, &s.Author, &s.Retrieved, &s.Cited, &s.Runs, &s.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Retrieved > out[j].Retrieved })
	return out, rows.Err()
}

// DefaultItemsDir trả về thư mục tri thức của repository.
func DefaultItemsDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(knowledge.ItemsRel))
}
