package flow

// Năm workflow gốc dưới dạng flow nhúng. Id bước giữ đúng tên bước hiện tại của Runner (b-req, b-feat, b-review)
// để GĐ3 chuyển sang đọc flow mà không đổi sự kiện, tên log hay khoá limits.workflows.<bước>.

// BuiltinNames là tên năm flow nhúng theo thứ tự cố định.
func BuiltinNames() []string { return []string{"b-do", "b-req", "b-feat", "b-review", "b-debug"} }

// legacyNames là bốn workflow gốc ban đầu, mỗi workflow một bước đặt tên theo workflow (b-do dùng ba bước đó).
var legacyNames = []string{"b-do", "b-req", "b-feat", "b-review"}

// Legacy cho biết tên là một trong bốn workflow gốc ban đầu (b-do, b-req, b-feat, b-review): id bước của chúng
// trùng tên bước đơn lẻ (b-req/b-feat/b-review) nên Runner chạy được bằng tên workflow. b-debug thì không.
func Legacy(name string) bool {
	for _, n := range legacyNames {
		if n == name {
			return true
		}
	}
	return false
}

// IsBuiltin cho biết tên là một workflow gốc.
func IsBuiltin(name string) bool {
	for _, n := range BuiltinNames() {
		if n == name {
			return true
		}
	}
	return false
}

// Builtin trả về năm flow nhúng (đã chuẩn hoá role). Mỗi lần gọi trả bản mới, bên gọi sửa thoải mái.
func Builtin() []Def {
	plan := Step{ID: "b-req", Kind: KindPlan, Skill: "b-req", Role: DefaultRole(KindPlan)}
	feat := Step{ID: "b-feat", Kind: KindImplement, Skill: "b-feat", Role: DefaultRole(KindImplement)}
	review := Step{ID: "b-review", Kind: KindReview, Skill: "b-review", Role: DefaultRole(KindReview)}

	doPlan := plan
	doPlan.Optional = true
	doReview := review
	doReview.Fix = "b-feat"

	repro := Step{ID: "b-repro", Kind: KindImplement, Skill: "b-repro", Role: DefaultRole(KindImplement), Expect: ExpectFail}
	fix := Step{ID: "b-fix", Kind: KindImplement, Skill: "b-fix", Role: DefaultRole(KindImplement)}
	debugReview := review
	debugReview.Fix = "b-fix"

	return []Def{
		{Schema: Schema, Name: "b-do", builtin: true,
			Description: "Phân loại tác vụ, lập kế hoạch (nếu cần), thực thi rồi review",
			Steps:       []Step{doPlan, feat, doReview}},
		{Schema: Schema, Name: "b-req", builtin: true,
			Description: "Phân tích yêu cầu và lập kế hoạch (requirement, plan, tasks)",
			Steps:       []Step{plan}},
		{Schema: Schema, Name: "b-feat", builtin: true,
			Description: "Thực thi kế hoạch của tác vụ và kiểm chứng",
			Steps:       []Step{feat}},
		{Schema: Schema, Name: "b-review", builtin: true,
			Description: "Đánh giá thay đổi của tác vụ và ghi phát hiện",
			Steps:       []Step{review}},
		{Schema: Schema, Name: "b-debug", builtin: true,
			Description: "Tái hiện lỗi bằng một test hỏng, đưa giả thuyết, sửa cho test qua rồi review",
			Steps:       []Step{doPlan, repro, fix, debugReview}},
	}
}

// GetBuiltin trả về flow nhúng theo tên.
func GetBuiltin(name string) (Def, bool) {
	for _, d := range Builtin() {
		if d.Name == name {
			return d, true
		}
	}
	return Def{}, false
}

// BuiltinStep tra bước của flow nhúng theo id (b-req, b-feat, b-review). Các flow nhúng dùng chung ba bước này
// (khác nhau ở Optional/Fix của b-do, không ảnh hưởng kind hay role). Id không thuộc flow nhúng nào (bước giả
// post-feat/verify, id lạ) → false.
func BuiltinStep(id string) (Step, bool) {
	for _, d := range Builtin() {
		if s, ok := d.StepByID(id); ok {
			return s, true
		}
	}
	return Step{}, false
}
