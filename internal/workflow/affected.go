package workflow

import (
	"context"
	"fmt"
	"strings"

	"bint/internal/affected"
	"bint/internal/gitrepo"
	"bint/internal/task"
)

// ReasonNoBase: không có commit/ref gốc để so (nới ra full).
const ReasonNoBase = "no_base"

// SelectAffected chọn phạm vi kiểm thử cho mọi thay đổi của cây làm việc ở root so với base (commit/ref/cây): gồm
// thay đổi đã commit, chưa commit và tệp chưa theo dõi (không bị .gitignore loại). Không tính được tập thay đổi thì nới
// ra full kèm lý do, không bao giờ thu hẹp.
func SelectAffected(ctx context.Context, root, base string) affected.Selection {
	return SelectAffectedCfg(ctx, root, base, true)
}

// SelectAffectedCfg như SelectAffected; bintConfigFull là workflow.verify.bint_config_full: false thì thay đổi chỉ trong .bint/** không nới ra full.
func SelectAffectedCfg(ctx context.Context, root, base string, bintConfigFull bool) affected.Selection {
	full := func(head, reason, detail string) affected.Selection {
		return affected.Selection{Scope: affected.TierFull, Base: base, Head: head, Changed: []string{},
			Widening: []affected.Widening{{Tier: affected.TierFull, Reason: reason, Detail: detail}}}
	}
	if strings.TrimSpace(base) == "" {
		return full("", ReasonNoBase, "không có commit gốc để so thay đổi")
	}
	head, changed, err := changedSince(ctx, root, base)
	if err != nil {
		return full(head, affected.ReasonSourceUnavailable, err.Error())
	}
	return affected.Select(ctx, affected.Input{Root: root, Base: base, Head: head, Changed: changed, IgnoreBintConfig: !bintConfigFull})
}

// changedSince trả ảnh chụp cây (head) và tệp đổi của cây làm việc ở root so với base. Không chụp được cây (git add bị chặn ghi .git/objects,
// ví dụ sandbox của agent — đợt 7: 24/24 lần kiểm hẹp bị bỏ vì vậy) thì tính tệp đổi bằng các lệnh git chỉ đọc; khi đó head rỗng.
func changedSince(ctx context.Context, root, base string) (head string, changed []string, err error) {
	head, serr := WorktreeSnapshot(ctx, root)
	if serr == nil {
		changed, err = DiffPaths(ctx, root, base, head)
		if err != nil {
			return head, nil, fmt.Errorf("không tính được tệp đổi so với %s: %v", base, err)
		}
		return head, changed, nil
	}
	changed, err = worktreeChangedReadOnly(ctx, root, base)
	if err != nil {
		return "", nil, fmt.Errorf("không chụp được cây làm việc (%v) và không tính được tệp đổi bằng lệnh chỉ đọc: %v", serr, err)
	}
	return "", changed, nil
}

// worktreeChangedReadOnly: tệp đổi của cây làm việc so với base (đã commit, chưa commit, chưa theo dõi và không bị ignore) mà không ghi gì vào .git.
func worktreeChangedReadOnly(ctx context.Context, root, base string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		for p := range strings.SplitSeq(s, "\x00") {
			if p != "" && !task.IsLedgerPath(p) && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	d, err := gitrepo.Run(ctx, root, "--no-optional-locks", "--literal-pathspecs", "diff", "--name-only", "--no-renames", "-z", base)
	if err != nil {
		return nil, err
	}
	add(d)
	u, err := gitrepo.Run(ctx, root, "--no-optional-locks", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	add(u)
	return out, nil
}
