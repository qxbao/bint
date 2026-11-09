package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Các nhánh so sánh (mục 12.4).
const (
	ArmNone    = "none"    // chỉ harness, không có AGENTS.md hay tri thức
	ArmMinimal = "minimal" // AGENTS.md ngắn chứa quy tắc bất thường
	ArmBint    = "bint"    // Context Engine + chèn theo đường dẫn + skill (P3)
)

// Chế độ chống rò rỉ (mục 12.3).
const (
	LeakStrict  = "strict"
	LeakEconomy = "economy"
)

// ErrArmUnavailable: nhánh không chạy được cho tác vụ này (không phải lỗi hạ tầng).
var ErrArmUnavailable = errors.New("nhánh không khả dụng")

// ArmNotImplemented trả lỗi nếu nhánh chưa có trong bản này.
func ArmNotImplemented(arm string) error {
	switch arm {
	case ArmNone, ArmMinimal:
		return nil
	case ArmBint:
		return fmt.Errorf("nhánh %q cần Context Engine (dự kiến P3), chưa có trong bản này", arm)
	}
	return fmt.Errorf("nhánh không hợp lệ %q (hỗ trợ: none, minimal)", arm)
}

// ArmSetup mô tả ngữ cảnh đã đặt vào thư mục làm việc.
type ArmSetup struct {
	// ContextSource ghi nguồn gốc ngữ cảnh để kiểm toán, ví dụ
	// "<base>:.bint/evals/minimal.md#<sha256>". Rỗng ở nhánh none.
	ContextSource string
	Stripped      []string
	// Screening = true khi kết quả chỉ dùng để sàng lọc (chế độ economy).
	Screening bool
}

// prepareArm gỡ mọi ngữ cảnh agent khỏi thư mục làm việc rồi đặt ngữ cảnh của
// nhánh, và commit để agent không thấy thay đổi chưa commit nào.
//
// Nhánh minimal dùng tệp eval.minimal_context (mặc định .bint/evals/minimal.md):
//   - strict: chỉ dùng phiên bản tồn tại TẠI commit gốc; không có thì nhánh không
//     khả dụng cho tác vụ đó (nội dung viết sau commit gốc có thể chứa lời giải).
//   - economy: dùng phiên bản hiện tại trong repository nếu nó không lộ lời giải
//     theo CheckLeak; kết quả chỉ dùng để sàng lọc.
//
// Với eval.minimal_source: knowledge, nội dung dựng từ tri thức (xem knowledge.go).
func prepareArm(ctx context.Context, env Env, arm, mode string, c *Case, leakIn *LeakInput, dir string) (ArmSetup, error) {
	var s ArmSetup
	stripped, err := stripContext(dir)
	if err != nil {
		return s, err
	}
	s.Stripped = stripped
	switch arm {
	case ArmNone:
	case ArmMinimal:
		var (
			content   []byte
			src       string
			screening bool
			err       error
		)
		if env.Cfg.Eval.MinimalSource == "file" {
			content, src, screening, err = minimalContent(ctx, mode, env.RepoDir, env.Cfg.Eval.MinimalContext, c, leakIn)
		} else {
			content, src, screening, err = minimalFromKnowledge(ctx, env, mode, c, leakIn)
		}
		if err != nil {
			return s, err
		}
		s.ContextSource, s.Screening = src, screening
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), content, 0o644); err != nil {
			return s, err
		}
		// Claude Code đọc CLAUDE.md, không đọc AGENTS.md; nhập lại như `bint init --agents-md`.
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644); err != nil {
			return s, err
		}
	default:
		if err := ArmNotImplemented(arm); err != nil {
			return s, err
		}
	}
	return s, nil
}

func minimalContent(ctx context.Context, mode, repoDir, minimalPath string, c *Case, leakIn *LeakInput) ([]byte, string, bool, error) {
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(minimalPath)))
	if mode == LeakStrict {
		data, ok := showFile(ctx, repoDir, c.Source.Base, rel)
		if !ok || len(strings.TrimSpace(string(data))) == 0 {
			return nil, "", false, fmt.Errorf("%w: strict yêu cầu %s tồn tại tại commit gốc %s", ErrArmUnavailable, rel, short(c.Source.Base))
		}
		return data, fmt.Sprintf("%s:%s#%s", short(c.Source.Base), rel, hash12(data)), false, nil
	}
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil, "", true, fmt.Errorf("%w: chưa có %s trong repository", ErrArmUnavailable, rel)
	}
	if leakIn != nil {
		if r := CheckLeak(string(data), *leakIn); r.Status == "fail" {
			return nil, "", true, fmt.Errorf("%w: %s chứa mã của lời giải (%s)", ErrArmUnavailable, rel, r.Findings[0].Detail)
		}
	}
	return data, fmt.Sprintf("worktree:%s#%s", rel, hash12(data)), true, nil
}

func hash12(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])[:12]
}
