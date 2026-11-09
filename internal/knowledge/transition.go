package knowledge

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Actor là bên yêu cầu chuyển trạng thái.
type Actor struct {
	// Kind: "human" (người qua CLI/terminal), "agent" (trong phiên agent),
	// "system" (kiểm tra xác định được của Bint).
	Kind string
	// Name là danh tính (Git) của người duyệt.
	Name string
}

// Các bên.
const (
	ActorHuman  = "human"
	ActorAgent  = "agent"
	ActorSystem = "system"
)

// TransitionInput là ngữ cảnh để quyết định một lần chuyển.
type TransitionInput struct {
	To     string
	Actor  Actor
	Reason string
	// ChecksPassed: kết quả kiểm tra xác định được (Verify) không có lỗi.
	ChecksPassed bool
	// RequireHuman là knowledge.require_human_approval_for (luôn chứa "rule").
	RequireHuman []string
	// Conflicts: mục verified khác mâu thuẫn (chưa giải quyết) với mục này.
	Conflicts []string
	Now       time.Time
}

// HighRisk cho biết loại tri thức có rủi ro cao khi sai (mục 8.4): xác thực lại
// cần người, và chỉ những loại này mới đáng tốn agent để nhận định lại.
func HighRisk(typ string) bool { return typ == TypeRule || typ == TypeWarning }

// ErrTransition: chuyển trạng thái không được phép.
var ErrTransition = errors.New("không được chuyển trạng thái")

// Rule là một dòng của bảng chuyển trạng thái (mục 8.1), dùng cho tài liệu
// (`bint knowledge transitions`) và để kiểm thử khớp với CanTransition.
type Rule struct {
	From, To  string
	Condition string
}

// TransitionTable là bảng chuyển trạng thái được thực thi bởi Transition.
var TransitionTable = []Rule{
	{StatusCandidate, StatusVerified, "loại trong require_human_approval_for (luôn có rule), hoặc mục bị gắn cờ bên thứ ba: người duyệt qua `bint knowledge approve`"},
	{StatusCandidate, StatusVerified, "loại khác: qua kiểm tra xác định được và không mâu thuẫn với mục verified (`bint knowledge verify --apply`), hoặc người duyệt"},
	{StatusCandidate, StatusVerified, "origin: human: được tạo ở verified ngay (tác giả là người duyệt); xem xét tiếp qua review mã"},
	{StatusVerified, StatusStale, "fingerprint của bằng chứng thay đổi (`bint knowledge verify --apply`)"},
	{StatusStale, StatusVerified, "xác thực lại, loại rủi ro cao (rule, warning) hoặc cần người duyệt: kiểm tra xác định được qua, rồi người duyệt (`bint knowledge approve --refresh`; `bint knowledge revalidate` cho agent nhận định trước)"},
	{StatusStale, StatusVerified, "xác thực lại, loại khác: mọi kiểm tra xác định được (trừ fingerprint) qua → fingerprint được làm mới (`bint knowledge verify --apply`)"},
	{"*", StatusDeprecated, "người dùng (`bint knowledge reject` / `deprecate`), hoặc đề xuất tiến hoá đã được chấp nhận (P6)"},
}

// Transition áp một lần chuyển trạng thái lên mục (chỉ trong bộ nhớ). Trả lỗi
// ErrTransition kèm lý do nếu không được phép.
func Transition(it *Item, in TransitionInput) error {
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	deny := func(format string, a ...any) error {
		return fmt.Errorf("%w %s → %s: %s", ErrTransition, it.Status, in.To, fmt.Sprintf(format, a...))
	}
	from := it.Status
	if from == in.To {
		return deny("mục đã ở trạng thái này")
	}
	switch in.To {
	case StatusDeprecated:
		if in.Actor.Kind == ActorAgent {
			return deny("agent không được loại bỏ tri thức; đề xuất qua người dùng")
		}
		if in.Reason == "" {
			return deny("cần lý do")
		}
	case StatusVerified:
		if from != StatusCandidate && from != StatusStale {
			return deny("chỉ chuyển từ candidate hoặc stale")
		}
		if in.Actor.Kind == ActorAgent {
			// Mục 15.5 / quyết định 14: agent không tự xác thực tri thức.
			return deny("agent không được xác thực tri thức; người dùng chạy `bint knowledge approve` hoặc `bint knowledge review`")
		}
		if !in.ChecksPassed {
			return deny("kiểm tra xác định được chưa qua (`bint knowledge verify %s`)", it.ID)
		}
		if len(in.Conflicts) > 0 {
			return deny("mâu thuẫn với mục verified %v; giải quyết trước", in.Conflicts)
		}
		// Xác thực lại (mục 8.4): loại rủi ro cao (rule, warning) cần người — agent
		// chỉ đưa nhận định; loại khác qua kiểm tra xác định được là đủ.
		needHuman := slices.Contains(in.RequireHuman, it.Type) || it.Type == TypeRule || it.Origin.ThirdParty ||
			(from == StatusStale && HighRisk(it.Type))
		if in.Actor.Kind == ActorSystem && needHuman {
			return deny("loại %q%s cần người duyệt", it.Type, map[bool]string{true: " (gắn cờ bên thứ ba)", false: ""}[it.Origin.ThirdParty])
		}
		if in.Actor.Kind == ActorHuman {
			it.ReviewedBy = in.Actor.Name
			it.ReviewedAt = Now(in.Now)
		}
		it.VerifiedAt = Now(in.Now)
	case StatusStale:
		if from != StatusVerified {
			return deny("chỉ mục verified mới chuyển sang stale")
		}
		if in.Actor.Kind == ActorAgent {
			return deny("chỉ Bint (kiểm tra fingerprint) hoặc người dùng đánh dấu lỗi thời")
		}
	case StatusCandidate:
		return deny("không chuyển ngược về candidate; tạo mục mới hoặc loại bỏ mục này")
	default:
		return deny("trạng thái đích không hợp lệ")
	}
	it.Status = in.To
	it.StatusReason = in.Reason
	it.UpdatedAt = Now(in.Now)
	return nil
}
