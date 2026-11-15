package workflow

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/mcpgw"

	_ "modernc.org/sqlite" // trình điều khiển "sqlite" cho VACUUM INTO (đặt ở workflow, không phải mcpgw: daemon không mở CSDL)
)

// CopyCodegraphIndex chép bản nhất quán của chỉ mục codegraph ở origin sang worktree wt: `VACUUM INTO` trong một giao
// dịch đọc nên gồm cả trang còn trong WAL, không phụ thuộc checkpoint, và không chép -wal/-shm/*.pid/daemon.*. Chép
// thêm .codegraph/.gitignore nếu có. Đích đã có codegraph.db thì thay (ghi tệp tạm rồi đổi tên).
func CopyCodegraphIndex(ctx context.Context, origin, wt string) error {
	src := filepath.Join(origin, mcpgw.CodegraphDB)
	if _, err := os.Stat(src); err != nil {
		return err
	}
	dstDir := filepath.Join(wt, mcpgw.CodegraphDir)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	if data, err := os.ReadFile(filepath.Join(origin, mcpgw.CodegraphDir, ".gitignore")); err == nil {
		if err := os.WriteFile(filepath.Join(dstDir, ".gitignore"), data, 0o644); err != nil {
			return err
		}
	}
	tmp := filepath.Join(dstDir, "codegraph.db.tmp")
	_ = os.Remove(tmp)
	db, err := sql.Open("sqlite", src+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(tmp, "'", "''")+"'")
	if cerr := db.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chép chỉ mục codegraph: %w", err)
	}
	dst := filepath.Join(dstDir, "codegraph.db")
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
