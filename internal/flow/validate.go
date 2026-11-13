package flow

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	minNameLen = 3
	maxNameLen = 40
)

// pseudoSteps là id các bước giả Runner đã dùng cho cổng sau bước; id người dùng không được trùng.
var pseudoSteps = []string{"post-feat", "verify"}

// Validate kiểm một định nghĩa flow và trả danh sách lỗi (rỗng = hợp lệ), mỗi lỗi một câu nêu bước/khoá.
//
// file là tên tệp không đuôi của flow tuỳ biến (tên flow phải trùng); "" nếu flow không đến từ tệp.
// skillExists cho biết skill (gốc hoặc tuỳ biến hợp lệ) có tồn tại không; nil coi mọi skill đều tồn tại (không kiểm
// skill của bước) và cũng bỏ luật "tên flow trùng tên một skill", vì luật đó cần biết skill nào có thật.
//
// Flow nhúng (Def.IsBuiltin) qua cùng hàm này, ngoại lệ duy nhất: không bị luật về tên gốc (trùng workflow gốc,
// tiền tố b-), và nếu chỉ có một bước thì được miễn luật "thiếu bước implement" (b-req, b-review).
func Validate(d Def, file string, skillExists func(string) bool) []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	checkNameVsSkill := skillExists != nil
	if skillExists == nil {
		skillExists = func(string) bool { return true }
	}

	if d.Schema != Schema {
		add("schema: %d không được hỗ trợ (chỉ có schema %d)", d.Schema, Schema)
	}

	// Tên flow.
	switch {
	case !nameRe.MatchString(d.Name) || len(d.Name) < minNameLen || len(d.Name) > maxNameLen:
		add("name: %q không hợp lệ (chữ thường, số, gạch ngang; %d–%d ký tự)", d.Name, minNameLen, maxNameLen)
	case d.IsBuiltin():
	case IsBuiltin(d.Name):
		add("name: %q trùng workflow gốc của Bint", d.Name)
	case strings.HasPrefix(d.Name, "b-"):
		add("name: %q dùng tiền tố b- dành cho workflow và skill gốc của Bint", d.Name)
	case checkNameVsSkill && skillExists(d.Name):
		add("name: %q trùng tên một skill (flow được xuất bản thành skill cùng tên, hai nguồn sẽ đè nhau)", d.Name)
	}
	if file != "" && d.Name != file {
		add("name: %q khác tên tệp %q (tệp phải là %s.yml)", d.Name, file, d.Name)
	}

	if len(d.Steps) == 0 {
		add("steps: flow không có bước nào")
		return errs
	}

	ids := map[string]bool{}
	plans, implements := 0, 0
	lastImplement := -1
	prevRank := -1
	for i, s := range d.Steps {
		at := fmt.Sprintf("bước %d", i+1)
		if s.ID != "" {
			at = fmt.Sprintf("bước %q", s.ID)
		}

		// id
		switch {
		case s.ID == "":
			add("%s: id rỗng", at)
		case !nameRe.MatchString(s.ID) || len(s.ID) > maxNameLen:
			add("%s: id không hợp lệ (chữ thường, số, gạch ngang; tối đa %d ký tự)", at, maxNameLen)
		case slices.Contains(pseudoSteps, s.ID):
			add("%s: id trùng bước giả %s của Bint", at, strings.Join(pseudoSteps, "/"))
		case ids[s.ID]:
			add("%s: id trùng với bước trước", at)
		}
		if s.ID != "" {
			ids[s.ID] = true
		}

		// kind và thứ tự
		rank := s.Kind.rank()
		if rank < 0 {
			add("%s: kind %q không hợp lệ (plan, implement hoặc review)", at, s.Kind)
		} else {
			if rank < prevRank {
				add("%s: kind %s đứng sau %s; thứ tự bắt buộc là plan → implement → review", at, s.Kind, Kinds[prevRank])
			} else {
				prevRank = rank
			}
			switch s.Kind {
			case KindPlan:
				plans++
				if plans == 2 {
					add("%s: chỉ được có một bước plan", at)
				}
			case KindImplement:
				implements++
				lastImplement = i
			}
		}

		// skill, role
		switch {
		case s.Skill == "":
			add("%s: skill rỗng", at)
		case !skillExists(s.Skill):
			add("%s: skill %q không tồn tại (skill gốc hoặc skill tuỳ biến hợp lệ trong .bint/skills)", at, s.Skill)
		}
		if s.Role != "" && (!nameRe.MatchString(s.Role) || len(s.Role) > maxNameLen) {
			add("%s: role %q không hợp lệ (chữ thường, số, gạch ngang; tối đa %d ký tự)", at, s.Role, maxNameLen)
		}

		// khoá riêng theo kind
		if s.Optional && s.Kind != KindPlan {
			add("%s: optional chỉ dùng được ở bước plan", at)
		}
		if s.Fix != "" {
			switch {
			case s.Kind != KindReview:
				add("%s: fix chỉ dùng được ở bước review", at)
			default:
				target, ok := d.StepByID(s.Fix)
				switch {
				case !ok:
					add("%s: fix trỏ tới bước %q không tồn tại", at, s.Fix)
				case target.Kind != KindImplement:
					add("%s: fix phải trỏ tới bước implement, %q là bước %s", at, s.Fix, target.Kind)
				}
			}
		}
	}

	if implements == 0 && !(d.IsBuiltin() && len(d.Steps) == 1) {
		add("steps: thiếu bước implement")
	}
	if lastImplement >= 0 && !d.Steps[lastImplement].VerifyOn() {
		s := d.Steps[lastImplement]
		add("bước %q: verify: false không được đặt ở bước implement cuối (kiểm chứng phải chạy trước review và trước khi done)", s.ID)
	}
	return errs
}

// Warnings trả các điều đáng nhắc nhưng không làm flow bị từ chối.
func Warnings(d Def) []string {
	var out []string
	for _, s := range d.Steps {
		if s.Verify != nil && s.Kind != KindImplement {
			out = append(out, fmt.Sprintf("bước %q: verify chỉ có tác dụng ở bước implement, bị bỏ qua", s.ID))
		}
	}
	return out
}
