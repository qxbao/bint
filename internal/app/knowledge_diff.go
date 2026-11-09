package app

import (
	"context"
	"fmt"
	"path"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/knowledge"
)

// DiffEntry là một mục tri thức mới, bị sửa hoặc bị xoá trong một khoảng commit.
type DiffEntry struct {
	ID      string          `json:"id"`
	Change  string          `json:"change"` // added | modified | deleted
	Authors []string        `json:"authors"`
	Mine    bool            `json:"mine"` // mọi commit trong khoảng là của chính người dùng
	Item    *knowledge.Item `json:"item,omitempty"`
}

// DefaultDiffRange chọn khoảng mặc định: ORIG_HEAD..HEAD (sau pull/merge/rebase),
// nếu không có thì HEAD@{1}..HEAD.
func DefaultDiffRange(ctx context.Context, dir string) string {
	if _, err := gitrepo.Run(ctx, dir, "rev-parse", "-q", "--verify", "ORIG_HEAD"); err == nil {
		return "ORIG_HEAD..HEAD"
	}
	return "HEAD@{1}..HEAD"
}

// KnowledgeDiff liệt kê tri thức mới hoặc bị sửa trong khoảng commit rng (mục 8.8,
// 15.5): thay đổi từ người khác qua `git pull` là đầu vào cần xem lại.
func KnowledgeDiff(ctx context.Context, k *KnowledgeWS, rng string) ([]DiffEntry, string, error) {
	if rng == "" {
		rng = DefaultDiffRange(ctx, k.Root)
	}
	a, b, ok := strings.Cut(rng, "..")
	if !ok || a == "" {
		return nil, rng, fmt.Errorf("khoảng %q không hợp lệ (dạng <từ>..<tới>)", rng)
	}
	if b == "" {
		b = "HEAD"
	}
	out, err := gitrepo.Run(ctx, k.Root, "-c", "core.quotepath=off", "diff", "--name-status", "--no-renames", a, b, "--", knowledge.ItemsRel)
	if err != nil {
		return nil, rng, fmt.Errorf("git diff %s: %w", rng, err)
	}
	me := GitIdentity(ctx, k.Root)
	var res []DiffEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		st, file, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasSuffix(file, ".md") {
			continue
		}
		id := strings.TrimSuffix(path.Base(file), ".md")
		e := DiffEntry{ID: id, Change: map[string]string{"A": "added", "M": "modified", "D": "deleted"}[st[:1]]}
		if e.Change == "" {
			e.Change = "modified"
		}
		authors, _ := gitrepo.Run(ctx, k.Root, "log", "--format=%an <%ae>", a+".."+b, "--", file)
		seen := map[string]bool{}
		e.Mine = true
		for _, au := range strings.Split(strings.TrimSpace(authors), "\n") {
			if au = strings.TrimSpace(au); au != "" && !seen[au] {
				seen[au] = true
				e.Authors = append(e.Authors, au)
				if au != me {
					e.Mine = false
				}
			}
		}
		if len(e.Authors) == 0 {
			e.Mine = false
		}
		if e.Change != "deleted" {
			if it, err := knowledge.ReadFile(k.Store.PathFor(id)); err == nil {
				e.Item = it
			}
		}
		res = append(res, e)
	}
	return res, rng, nil
}
