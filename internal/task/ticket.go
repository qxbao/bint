package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"bint/internal/gitrepo"
	"bint/internal/pathglob"
)

// MaxGoalRunes giới hạn độ dài mục tiêu một dòng.
const MaxGoalRunes = 200

var pathHintRe = regexp.MustCompile(`[A-Za-z0-9_.\-]+/(?:[A-Za-z0-9_.\-*]+/?)*|[A-Za-z0-9_\-]+\.[A-Za-z0-9]{1,5}\b`)

// FromRequest lập phiếu tác vụ từ mô tả tự do (bước Classify, mục 6.7). Bint không
// bắt buộc đầu vào có cấu trúc; phần xác định được do Core làm:
//   - goal: dòng đầu của mô tả (cắt ngắn), request: nguyên văn;
//   - scope.allow: đường dẫn nêu trong mô tả mà có thật trong repository;
//   - verify.command: lệnh kiểm thử đã được người xác nhận (eval.test_command), nếu có.
//
// Agent (b-req) hoặc người hoàn thiện phần còn lại bằng `bint task set`.
func FromRequest(root, request, confirmedTestCommand string) Ticket {
	request = strings.TrimSpace(request)
	t := Ticket{Request: request, Autonomy: AutonomyAskOnProtected}
	first, _, _ := strings.Cut(request, "\n")
	first = strings.TrimSpace(strings.TrimLeft(first, "#-* "))
	if r := []rune(first); len(r) > MaxGoalRunes {
		first = string(r[:MaxGoalRunes-1]) + "…"
	}
	t.Goal = first
	if t.Request == t.Goal {
		t.Request = ""
	}
	seen := map[string]bool{}
	for _, m := range pathHintRe.FindAllString(request, -1) {
		p := strings.TrimSuffix(strings.TrimPrefix(strings.TrimRight(m, ".,:;)"), "./"), "/")
		if p == "" || strings.HasPrefix(p, "../") || seen[p] {
			continue
		}
		seen[p] = true
		if pathglob.HasMeta(p) {
			t.Scope.Allow = append(t.Scope.Allow, p)
			continue
		}
		fi, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		if fi.IsDir() {
			p += "/**"
		}
		t.Scope.Allow = append(t.Scope.Allow, p)
	}
	t.Verify.Command = strings.TrimSpace(confirmedTestCommand)
	return t
}

var (
	planWords  = regexp.MustCompile(`(?i)\b(feature|implement|design|refactor|migrat\w*|api|endpoint|schema|tính năng|thiết kế|tái cấu trúc|kiến trúc|luồng|nghiệp vụ)\b`)
	smallWords = regexp.MustCompile(`(?i)\b(typo|chính tả|rename|đổi tên|bump|comment|chú thích|format|lint|fix typo)\b`)
)

// Classify quyết định có cần b-req không (mục 6.4: bỏ qua bước không cần thiết).
// Heuristic xác định được, chỉ là đề xuất; người dùng ghi đè bằng --plan/--no-plan.
func Classify(request string) Classification {
	words := len(strings.Fields(request))
	lines := 0
	for _, l := range strings.Split(strings.TrimSpace(request), "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	switch {
	case smallWords.MatchString(request) && words <= 25:
		return Classification{Plan: false, Reason: "việc nhỏ (chính tả/đổi tên/định dạng): bỏ b-req"}
	case lines >= 3 || words > 60:
		return Classification{Plan: true, Reason: "mô tả dài hoặc nhiều ý: cần b-req"}
	case planWords.MatchString(request):
		return Classification{Plan: true, Reason: "tính năng/thiết kế/thay đổi giao diện: cần b-req"}
	case words <= 25:
		return Classification{Plan: false, Reason: "yêu cầu ngắn, rõ: bỏ b-req"}
	}
	return Classification{Plan: true, Reason: "mặc định: cần b-req"}
}

// Assessment là đánh giá rủi ro của phiếu.
type Assessment struct {
	Risk string `json:"risk"` // low | high
	// Protected là các mẫu đường dẫn bảo vệ mà phạm vi tác vụ có thể chạm.
	Protected []string `json:"protected,omitempty"`
	// Questions: chỉ hỏi lại khi thiếu trường quan trọng cho tác vụ rủi ro cao.
	Questions []string `json:"questions,omitempty"`
	Notes     []string `json:"notes,omitempty"`
}

// globPrefix trả về phần thư mục cố định trước ký tự glob đầu tiên.
func globPrefix(p string) string {
	if i := strings.IndexAny(p, "*?"); i >= 0 {
		p = p[:i]
	}
	return strings.TrimSuffix(p, "/")
}

// overlaps ước lượng hai mẫu glob có thể khớp chung một tệp không.
func overlaps(a, b string) bool {
	if pathglob.Compile(a).Match(globPrefix(b)) || pathglob.Compile(b).Match(globPrefix(a)) {
		return true
	}
	pa, pb := globPrefix(a), globPrefix(b)
	if pa == "" || pb == "" {
		return true
	}
	return pa == pb || strings.HasPrefix(pa+"/", pb+"/") || strings.HasPrefix(pb+"/", pa+"/")
}

// Assess đánh giá rủi ro: tác vụ rủi ro cao khi phạm vi chạm đường dẫn được bảo vệ
// (mục 5) hoặc mục tiêu nêu đường dẫn được bảo vệ.
func Assess(t Ticket, protected []string, verifyConfirmed bool) Assessment {
	a := Assessment{Risk: "low"}
	text := t.Goal + "\n" + t.Request
	for _, p := range protected {
		hit := false
		for _, al := range t.Scope.Allow {
			if overlaps(al, p) {
				hit = true
			}
		}
		for _, raw := range pathHintRe.FindAllString(text, -1) {
			m := strings.TrimSuffix(strings.TrimPrefix(raw, "./"), "/")
			if pathglob.Compile(p).Match(m) || (strings.Contains(raw, "/") && overlaps(m, p)) {
				hit = true
			}
		}
		if hit && !slices.Contains(a.Protected, p) {
			a.Protected = append(a.Protected, p)
		}
	}
	if len(a.Protected) > 0 {
		a.Risk = "high"
	}
	if len(t.Scope.Allow) == 0 {
		a.Notes = append(a.Notes, "chưa xác định phạm vi (scope.allow): truy xuất theo đường dẫn và giới hạn ghi kém chính xác")
	}
	cmd := strings.TrimSpace(t.Verify.Command)
	if a.Risk == "high" {
		if cmd == "" {
			a.Questions = append(a.Questions, "Tác vụ chạm đường dẫn được bảo vệ ("+strings.Join(a.Protected, ", ")+"): kiểm chứng thay đổi bằng lệnh nào?")
		} else if !verifyConfirmed {
			a.Notes = append(a.Notes, "lệnh kiểm chứng chưa được người xác nhận (bint task approve)")
		}
		a.Notes = append(a.Notes, "mọi thay đổi chạm đường dẫn được bảo vệ cần người duyệt")
	}
	if cmd == "" && len(t.Verify.Criteria) == 0 {
		a.Notes = append(a.Notes, "chưa có lệnh kiểm chứng hay tiêu chí chấp nhận: không thể tự xác nhận hoàn thành")
	}
	return a
}

// ScopeCheck phân loại tệp đã sửa theo phạm vi phiếu và đường dẫn bảo vệ.
type ScopeCheck struct {
	Protected    []string `json:"protected,omitempty"`     // chạm limits.protected_paths
	Denied       []string `json:"denied,omitempty"`        // chạm scope.deny
	OutsideAllow []string `json:"outside_allow,omitempty"` // ngoài scope.allow (khi allow khác rỗng)
}

// NeedsApproval là các tệp luôn cần người duyệt (bảo vệ hoặc deny), không phụ thuộc autonomy.
func (c ScopeCheck) NeedsApproval() []string {
	out := append(append([]string{}, c.Protected...), c.Denied...)
	sort.Strings(out)
	return slices.Compact(out)
}

// CheckScope phân loại danh sách tệp đã sửa.
func CheckScope(files []string, t Ticket, protected []string) ScopeCheck {
	var c ScopeCheck
	for _, f := range files {
		switch {
		case pathglob.MatchAny(protected, f):
			c.Protected = append(c.Protected, f)
		case pathglob.MatchAny(t.Scope.Deny, f):
			c.Denied = append(c.Denied, f)
		case len(t.Scope.Allow) > 0 && !pathglob.MatchAny(t.Scope.Allow, f):
			c.OutsideAllow = append(c.OutsideAllow, f)
		}
	}
	return c
}

// ChangedFiles trả về các tệp khác với commit base (gồm commit mới, thay đổi chưa
// commit và tệp chưa theo dõi), đường dẫn tương đối dấu '/'.
func ChangedFiles(ctx context.Context, root, base string) ([]string, error) {
	if base == "" {
		base = "HEAD"
	}
	set := map[string]bool{}
	for _, args := range [][]string{
		{"diff", "--name-only", "--no-renames", "-z", base},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		out, err := gitrepo.Run(ctx, root, args...)
		if err != nil {
			return nil, err
		}
		for l := range strings.SplitSeq(out, "\x00") {
			if l = strings.TrimSpace(l); l != "" {
				set[path.Clean(l)] = true
			}
		}
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}

// WorktreeFingerprint băm trạng thái mã hiện tại: HEAD, thay đổi đã/chưa stage và
// nội dung tệp chưa theo dõi. Dùng để biết lần kiểm chứng cuối có còn ứng với mã
// hiện tại không. Trả "" nếu không xác định được.
func WorktreeFingerprint(ctx context.Context, root string) string {
	h := sha256.New()
	head := gitrepo.HeadCommit(ctx, root)
	h.Write([]byte(head + "\x00"))
	ref := "HEAD"
	if head == "" {
		ref = "--cached"
	}
	diff, err := gitrepo.Run(ctx, root, "diff", "--binary", "--no-ext-diff", ref)
	if err != nil {
		return ""
	}
	h.Write([]byte(diff))
	others, err := gitrepo.Run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return ""
	}
	names := strings.Split(others, "\x00")
	sort.Strings(names)
	for _, n := range names {
		if n = strings.TrimSpace(n); n == "" {
			continue
		}
		h.Write([]byte("\x00" + n + "\x00"))
		if f, err := os.Open(filepath.Join(root, filepath.FromSlash(n))); err == nil {
			io.Copy(h, f)
			f.Close()
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
