// Package store quản lý cơ sở dữ liệu SQLite cục bộ của Bint: mở kết nối với
// WAL, chạy migration có phiên bản và kiểm tra khả năng FTS5.
//
// Driver: modernc.org/sqlite (Go thuần, không cần CGO, có FTS5).
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// TimeFormat là định dạng thời gian lưu trong cơ sở dữ liệu (UTC, mili giây).
const TimeFormat = "2006-01-02T15:04:05.000Z"

// FormatTime chuyển t sang định dạng lưu trữ.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTime đọc thời gian ở định dạng lưu trữ.
func ParseTime(s string) (time.Time, error) { return time.Parse(TimeFormat, s) }

// Store bọc *sql.DB đã qua migration.
type Store struct {
	DB   *sql.DB
	Path string
}

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("tên migration không hợp lệ: %s", name)
		}
		body, err := migrationFS.ReadFile(path.Join("migrations", name))
		if err != nil {
			return nil, err
		}
		ms = append(ms, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration phải đánh số liên tục từ 1, gặp %s", m.name)
		}
	}
	return ms, nil
}

// LatestVersion là phiên bản schema mà bản Bint này biết.
func LatestVersion() int {
	ms, err := loadMigrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

// DSN dựng chuỗi kết nối cho modernc.org/sqlite.
func DSN(dbPath string) string {
	q := []string{
		"_pragma=busy_timeout(10000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=synchronous(NORMAL)",
		"_txlock=immediate",
	}
	return dbPath + "?" + strings.Join(q, "&")
}

// Open mở (hoặc tạo) cơ sở dữ liệu và áp mọi migration còn thiếu.
func Open(ctx context.Context, dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", DSN(dbPath))
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db, Path: dbPath}
	// Khi nhiều tiến trình mở cơ sở dữ liệu mới cùng lúc, việc chuyển sang WAL và
	// migration có thể trả SQLITE_BUSY ngay mà không qua busy_timeout; thử lại có giới hạn.
	deadline := time.Now().Add(15 * time.Second)
	for wait := 10 * time.Millisecond; ; wait = min(wait*2, 500*time.Millisecond) {
		err = s.migrate(ctx)
		if err == nil {
			return s, nil
		}
		if !isBusy(err) || time.Now().After(deadline) {
			db.Close()
			return nil, fmt.Errorf("migration %s: %w", dbPath, err)
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// OpenMemory mở cơ sở dữ liệu trong bộ nhớ đã qua migration (một kết nối duy nhất,
// vì mỗi kết nối :memory: là một cơ sở dữ liệu riêng). Dùng cho chỉ mục tạm của
// Context Engine khi không có thư mục trạng thái (thư mục đánh giá, hook).
func OpenMemory(ctx context.Context) (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(0)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Path: ":memory:"}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func isBusy(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}

// Close đóng cơ sở dữ liệu.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate(ctx context.Context) error {
	ms, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	cur, err := s.Version(ctx)
	if err != nil {
		return err
	}
	if cur > len(ms) {
		return fmt.Errorf("cơ sở dữ liệu ở phiên bản %d, mới hơn bản Bint này (%d); hãy nâng cấp Bint", cur, len(ms))
	}
	for _, m := range ms[cur:] {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// Kiểm tra lại trong giao dịch: tiến trình khác có thể vừa áp migration này.
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&exists); err != nil {
			tx.Rollback()
			return err
		}
		if exists > 0 {
			tx.Rollback()
			continue
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, ?, ?)`,
			m.version, m.name, FormatTime(time.Now())); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Version trả về phiên bản schema hiện tại của cơ sở dữ liệu.
func (s *Store) Version(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

// JournalMode trả về chế độ journal đang dùng (mong đợi "wal").
func (s *Store) JournalMode(ctx context.Context) (string, error) {
	var m string
	err := s.DB.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&m)
	return m, err
}

// CheckFTS5 xác minh driver hỗ trợ FTS5 bằng một bảng ảo tạm.
func CheckFTS5(ctx context.Context) error {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE VIRTUAL TABLE t USING fts5(body)`); err != nil {
		return fmt.Errorf("FTS5 không khả dụng: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t(body) VALUES ('quy tắc bất thường'), ('điểm dễ sai')`); err != nil {
		return err
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM t WHERE t MATCH 'bất'`).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("FTS5 trả về %d kết quả, mong đợi 1", n)
	}
	return nil
}
