package workflow

import (
	"context"
	"strings"

	"bint/internal/affected"
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
	head, err := WorktreeSnapshot(ctx, root)
	if err != nil {
		return full("", affected.ReasonSourceUnavailable, "không chụp được cây làm việc: "+err.Error())
	}
	changed, err := DiffPaths(ctx, root, base, head)
	if err != nil {
		return full(head, affected.ReasonSourceUnavailable, "không tính được tệp đổi so với "+base+": "+err.Error())
	}
	return affected.Select(ctx, affected.Input{Root: root, Base: base, Head: head, Changed: changed, IgnoreBintConfig: !bintConfigFull})
}
