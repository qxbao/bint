package workflow

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/pathglob"
	"bint/internal/task"
)

// Trần của phép đo hiện trạng: quá thời gian hoặc số tệp thì dừng và ghi rõ "đạt trần" (số đo là cận dưới).
const (
	baselineBudget   = 10 * time.Second
	baselineMaxFiles = 50000
)

// BaselineFile là tên tệp hiện trạng Bint ghi vào thư mục tác vụ trước bước b-req.
const BaselineFile = "baseline.md"

type baselineStat struct {
	files, bytes int64
	capped       bool
}

// walkStat đo số tệp và byte dưới root, dừng khi quá hạn hoặc quá số tệp tối đa (budget tệp tính chung qua left).
func walkStat(root string, deadline time.Time, left *int) baselineStat {
	var s baselineStat
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if *left <= 0 || time.Now().After(deadline) {
			s.capped = true
			return fs.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		*left--
		if info, ierr := d.Info(); ierr == nil {
			s.files++
			s.bytes += info.Size()
		}
		return nil
	})
	return s
}

// trackedFiles liệt kê tệp git theo dõi (biến để test thay bằng danh sách giả).
var trackedFiles = func(ctx context.Context, root string) ([]string, error) {
	out, err := gitrepo.Run(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\x00"), "\x00"), nil
}

// BuildBaseline đo hiện trạng: dung lượng/số tệp từng mục cấp một của thư mục trạng thái và số tệp/byte được git theo dõi
// khớp từng mẫu scope.allow (trừ tệp khớp scope.deny). Hai phép đo có trần thời gian riêng và trần số tệp; không bao giờ lỗi
// (mục đo không được ghi rõ lý do).
func BuildBaseline(ctx context.Context, stateDir, root string, allow, deny []string) string {
	return buildBaseline(ctx, stateDir, root, allow, deny, baselineBudget, baselineMaxFiles)
}

func buildBaseline(ctx context.Context, stateDir, root string, allow, deny []string, budget time.Duration, maxFiles int) string {
	var b strings.Builder
	b.WriteString("# Hiện trạng (Bint đo tự động trước b-req)\n\n")
	b.WriteString("Số đo thô để lập kế hoạch; chép các số liên quan vào mục `## Hiện trạng` của requirement.md.\n\n")

	// scope.allow đo trước (git ls-files rẻ) với deadline riêng, để thư mục trạng thái lớn không làm nó hết hạn.
	var scopeB strings.Builder
	scopeB.WriteString("\n## Tệp được git theo dõi khớp scope.allow\n\n")
	files, gerr := trackedFiles(ctx, root)
	switch {
	case gerr != nil:
		fmt.Fprintf(&scopeB, "- Không đo được: %v\n", gerr)
	case len(allow) == 0:
		fmt.Fprintf(&scopeB, "- Phiếu chưa có scope.allow; toàn kho có %d tệp được git theo dõi\n", len(files))
	default:
		scopeDeadline := time.Now().Add(budget)
		denyC := make([]pathglob.Pattern, 0, len(deny))
		for _, d := range deny {
			denyC = append(denyC, pathglob.Compile(d))
		}
		denied := func(f string) bool {
			for _, c := range denyC {
				if c.Match(f) {
					return true
				}
			}
			return false
		}
		if len(deny) > 0 {
			scopeB.WriteString("(đã trừ tệp khớp scope.deny)\n\n")
		}
		for _, pat := range allow {
			var s baselineStat
			c := pathglob.Compile(pat)
			for i, f := range files {
				if i%512 == 0 && time.Now().After(scopeDeadline) {
					s.capped = true
					break
				}
				if f == "" || !c.Match(f) || denied(f) {
					continue
				}
				s.files++
				if info, serr := os.Stat(filepath.Join(root, filepath.FromSlash(f))); serr == nil {
					s.bytes += info.Size()
				}
			}
			fmt.Fprintf(&scopeB, "- `%s`: %d tệp, %d byte%s\n", pat, s.files, s.bytes, cappedNote(s.capped))
		}
	}

	deadline := time.Now().Add(budget)
	left := maxFiles
	fmt.Fprintf(&b, "## Thư mục trạng thái `%s`\n\n", filepath.ToSlash(stateDir))
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		fmt.Fprintf(&b, "- Không đo được: %v\n", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		p := filepath.Join(stateDir, e.Name())
		if !e.IsDir() {
			size := int64(0)
			if info, ierr := e.Info(); ierr == nil {
				size = info.Size()
			}
			fmt.Fprintf(&b, "- `%s`: 1 tệp, %d byte\n", e.Name(), size)
			continue
		}
		s := walkStat(p, deadline, &left)
		fmt.Fprintf(&b, "- `%s/`: %d tệp, %d byte%s\n", e.Name(), s.files, s.bytes, cappedNote(s.capped))
	}
	b.WriteString(scopeB.String())
	return b.String()
}

func cappedNote(capped bool) string {
	if capped {
		return " (đạt trần thời gian/số tệp: là cận dưới)"
	}
	return ""
}

// writeBaseline ghi baseline.md vào thư mục tác vụ và trả đường dẫn ("" khi không ghi được; lỗi chỉ được log).
func (r *Runner) writeBaseline(ctx context.Context, t *task.Task) string {
	taskDir := filepath.Dir(t.ArtifactPath("requirement"))
	stateDir := filepath.Dir(filepath.Dir(filepath.Dir(taskDir)))
	path := filepath.Join(taskDir, BaselineFile)
	body := BuildBaseline(ctx, stateDir, r.Root, t.Ticket.Scope.Allow, t.Ticket.Scope.Deny)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		r.logf("  ! không ghi được %s: %v", BaselineFile, err)
		return ""
	}
	return path
}
