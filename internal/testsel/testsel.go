// Package testsel chọn test bị ảnh hưởng bởi một thay đổi cho mọi lần chạy test của Bint, bằng bgraph (github.com/qxbao/bgraph): đồ thị
// phụ thuộc mức tệp của cây mốc và cây mới, đi ngược từ tệp đổi, cho Node, Python, Rust, JVM, .NET, Go, PHP và Ruby. Kết quả là affected.Selection (kiểu
// Bint lưu trong state.json) kèm lệnh chạy do bgraph dựng cho runner của dự án.
//
// Tách khỏi internal/affected vì bgraph kéo theo chỉ mục SQLite: các gói chỉ cần kiểu dữ liệu (task, mcpgw) không được phụ thuộc nó.
package testsel

import (
	"context"
	"path"
	"slices"
	"strings"

	"bint/internal/affected"

	"github.com/qxbao/bgraph"
)

// Input là đầu vào của Select.
type Input struct {
	Root string
	// Base: commit/ref mốc. Rỗng → full (no_base).
	Base string
	// Head: commit đích; rỗng thì so với cây làm việc ở Root (gồm phần chưa commit và tệp chưa theo dõi không bị ignore).
	Head string
	// Exclude: mẫu đường dẫn (gạch xuôi, tương đối gốc repo, `dir/**`) bỏ khỏi tập thay đổi: sổ phiếu của Bint, và .bint/** khi
	// workflow.verify.bint_config_full là false.
	Exclude []string
	// StateDir: thư mục chỉ mục bền của bgraph (rỗng: thư mục cache của người dùng).
	StateDir string
	// PreviouslyFailed: test đỏ ở lần trước, luôn chạy lại.
	PreviouslyFailed []string
	// Only: nếu khác rỗng, chỉ giữ các đơn vị test chứa một trong các tệp này (cổng mục tiêu chạy riêng test đóng băng). Không đơn vị nào
	// khớp thì scope full (người gọi chạy lệnh của phiếu).
	Only []string
}

// Select tính phần chọn. Không bao giờ trả lỗi: lỗi hạ tầng (git không chạy, repo hỏng) thành scope full kèm lý do.
func Select(ctx context.Context, in Input) affected.Selection {
	full := func(reason, detail string) affected.Selection {
		return affected.Selection{Scope: affected.TierFull, Base: in.Base, Head: in.Head, Source: "bgraph", Changed: []string{},
			Widening: []affected.Widening{{Tier: affected.TierFull, Reason: reason, Detail: detail}}}
	}
	if strings.TrimSpace(in.Base) == "" {
		return full(affected.ReasonNoBase, "không có commit mốc để so thay đổi")
	}
	ix, err := bgraph.Open(ctx, in.Root, bgraph.Options{StateDir: in.StateDir})
	if err != nil {
		return full(affected.ReasonSourceUnavailable, err.Error())
	}
	defer ix.Close()
	ch := bgraph.Change{Base: in.Base, Head: in.Head, Worktree: in.Head == "", PreviouslyFailed: in.PreviouslyFailed, Exclude: in.Exclude}
	bs, err := ix.Select(ctx, ch)
	if err != nil {
		return full(affected.ReasonSourceUnavailable, err.Error())
	}
	if len(in.Only) > 0 {
		bs = restrict(bs, in.Only)
	}
	sel := FromBgraph(bs)
	if in.Head == "" {
		sel.Head = ""
	}
	return sel
}

// restrict giữ các đơn vị test của bs chứa một tệp trong only (so theo Files, Unit, hoặc thư mục gói khi đơn vị là cả một gói).
func restrict(bs bgraph.Selection, only []string) bgraph.Selection {
	if bs.Scope == bgraph.TierFull || bs.Scope == bgraph.TierNone && len(bs.Tests) == 0 {
		// full: không biết đơn vị; none: bgraph không thấy tệp nào đổi ảnh hưởng tới chúng, vẫn phải chạy chúng nên lùi về lệnh của phiếu.
		bs.Scope = bgraph.TierFull
		bs.Tests = nil
		bs.Widening = append(bs.Widening, bgraph.Widening{Tier: bgraph.TierFull, Reason: affected.ReasonOnlyUnselected,
			Detail: "không dựng được đơn vị test cho: " + strings.Join(only, ", ")})
		return bs
	}
	match := func(t bgraph.SelectedTest) bool {
		dir, isDir := unitDir(t)
		for _, o := range only {
			if t.Unit == o || slices.Contains(t.Files, o) || isDir && path.Dir(o) == dir {
				return true
			}
		}
		return false
	}
	var keep []bgraph.SelectedTest
	for _, t := range bs.Tests {
		if match(t) {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		return restrict(bgraph.Selection{Scope: bgraph.TierFull, Base: bs.Base, Head: bs.Head, Changed: bs.Changed, Widening: bs.Widening}, only)
	}
	bs.Tests = keep
	return bs
}

// unitDir: thư mục của đơn vị test là cả một gói (Go: SelectedTest.Dir, Files chỉ là nút ảo). ok=false với đơn vị theo tệp.
func unitDir(t bgraph.SelectedTest) (string, bool) {
	return t.Dir, t.Dir != ""
}

// FromBgraph chuyển Selection của bgraph sang dạng Bint, kèm lệnh chạy.
func FromBgraph(bs bgraph.Selection) affected.Selection {
	sel := affected.Selection{Scope: affected.Tier(bs.Scope), Base: bs.Base, Head: bs.Head, Source: "bgraph", Changed: slices.Clone(bs.Changed)}
	if sel.Changed == nil {
		sel.Changed = []string{}
	}
	sel.ChangedTotal = len(sel.Changed)
	if len(sel.Changed) > affected.MaxChanged {
		sel.Changed, sel.Truncated = sel.Changed[:affected.MaxChanged], true
	}
	conv := func(ws []bgraph.Widening) []affected.Widening {
		var out []affected.Widening
		for _, w := range ws {
			out = append(out, affected.Widening{Path: w.Path, Tier: affected.Tier(w.Tier), Reason: w.Reason, Detail: w.Detail, Scope: w.Scope, Suggest: w.Suggest})
		}
		return out
	}
	sel.Widening, sel.Warnings = conv(bs.Widening), conv(bs.Warnings)
	if sel.Scope == affected.TierFull {
		return sel
	}
	seenF, seenD := map[string]bool{}, map[string]bool{}
	addDir := func(d string) {
		if d != "." {
			d = "./" + strings.TrimPrefix(d, "./")
		}
		if !seenD[d] {
			seenD[d] = true
			sel.TestDirs = append(sel.TestDirs, d)
		}
	}
	for _, t := range bs.Tests {
		sel.Tests = append(sel.Tests, affected.Test{Unit: t.Unit, Runner: t.Runner, Project: t.Project, Files: slices.Clone(t.Files), Widened: t.Widened})
		if d, ok := unitDir(t); ok {
			addDir(d) // đơn vị là cả gói (Go): Files chỉ là nút ảo
			continue
		}
		for _, f := range t.Files {
			if !seenF[f] {
				seenF[f] = true
				sel.TestFiles = append(sel.TestFiles, f)
			}
			addDir(path.Dir(f))
		}
	}
	slices.Sort(sel.TestFiles)
	slices.Sort(sel.TestDirs)
	if sel.Scope == affected.TierNone {
		return sel
	}
	plan, err := bgraph.Commands(bs, bgraph.CommandOptions{})
	if err != nil {
		sel.Scope = affected.TierFull
		sel.Widening = append(sel.Widening, affected.Widening{Tier: affected.TierFull, Reason: affected.ReasonSourceUnavailable,
			Detail: "không dựng được lệnh chạy: " + err.Error()})
		return sel
	}
	if affected.Tier(plan.Scope) == affected.TierFull {
		// bgraph không dựng được lệnh chọn lọc (đơn vị không chạy riêng được): chạy cả bộ, ghi lý do.
		sel.Scope = affected.TierFull
		sel.Widening = append(sel.Widening, affected.Widening{Tier: affected.TierFull, Reason: affected.ReasonNoSelectiveCommand,
			Detail: strings.Join(plan.Notes, "; ")})
		return sel
	}
	for _, c := range plan.Commands {
		sel.Commands = append(sel.Commands, affected.Command{Runner: c.Runner, Project: c.Project, Argv: slices.Clone(c.Argv), Env: slices.Clone(c.Env)})
	}
	return sel
}
