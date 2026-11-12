package ui

import "regexp"

// Dữ liệu nhận dạng chữ của tầng dịch vụ (tiếng Việt, chưa dịch): động từ đứng trước lệnh và các cụm dùng "bint run" như
// danh từ. Đây là mẫu để so khớp, không hiện cho người dùng (bài kiểm tra chuỗi tiếng Việt trong mã bỏ qua tệp này).

// cliVerbs: động từ đứng trước lệnh được gộp vào tham chiếu ("chạy `bint sync`" → "nhấn s ở …"); lead là chữ thay cho động
// từ khi không dùng dạng động từ.
var cliVerbs = []struct {
	word, lead string
	verb       bool
}{
	{"chạy tiếp bằng", "tiếp tục bằng ", false},
	{"chạy lại", "", true},
	{"chạy", "", true},
	{"dùng", "", true},
}

var phraseRE = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile("`?bint run`? điều phối"), "cliref.phrase.orchestrated"},
	{regexp.MustCompile("tiến trình bint run"), "cliref.phrase.run_process"},
	{regexp.MustCompile("chạy `?bint run`? lồng"), "cliref.phrase.run_nested"},
	{regexp.MustCompile("bint run bị huỷ"), "cliref.phrase.run_cancelled"},
	{regexp.MustCompile("chưa `?bint init`? trên máy"), "cliref.phrase.not_init"},
	// Cờ CLI đứng rời (không kèm lệnh bint) trong thông điệp của tầng dịch vụ; neo theo chữ đứng trước để lỗi của công cụ ngoài
	// ("use --force" của git) đi qua không bị đổi.
	{regexp.MustCompile("^--update: "), "cliref.flag.update"},
	{regexp.MustCompile("hoặc dùng --unverified"), "cliref.flag.unverified"},
	{regexp.MustCompile("thêm --confirm-risk"), "cliref.flag.confirm_risk"},
	{regexp.MustCompile("người có thể --force"), "cliref.flag.force"},
	{regexp.MustCompile(`\(--git local\)`), "cliref.flag.git_local"},
	// Cờ rời trong các luồng TUI khác (review 6b-sửa .2); cụm dài/cụ thể đứng trước ("hoặc dùng --force để chạy vẫn" trước "hoặc dùng --force").
	{regexp.MustCompile(`hoặc dùng --force để chạy vẫn`), "cliref.flag.force_evaluate"},
	{regexp.MustCompile(`dùng --force nếu vẫn muốn duyệt`), "cliref.flag.force_approve"},
	{regexp.MustCompile(`hoặc dùng --force\b`), "cliref.flag.force_approve_or"},
	// Chỉ thông điệp quay lui của evolve/apply.go ("… rồi dùng --force để ghi đè bằng bản trước khi áp dụng"); thông điệp fsafe/skills
	// ("dùng --force để ghi đè (bản cũ sẽ được sao lưu)") là xung đột tệp khác nên giữ nguyên (review 6b-sửa-2 .1). Nhóm bắt (nếu có)
	// là đuôi của câu gốc, được giữ lại sau cụm thay.
	{regexp.MustCompile(`dùng --force để ghi đè( bằng bản trước)`), "cliref.flag.force_rollback"},
	{regexp.MustCompile(`bỏ --no-agent`), "cliref.flag.no_agent"},
	{regexp.MustCompile(`tăng --budget`), "cliref.flag.budget"},
	{regexp.MustCompile(`--budget <USD>`), "cliref.flag.budget_field"},
	{regexp.MustCompile(`--budget \(USD\)`), "cliref.flag.budget_usd"},
	{regexp.MustCompile(`--budget > 0`), "cliref.flag.budget_pos"},
	{regexp.MustCompile(`giảm --limit/--repeats/--arms`), "cliref.flag.reduce_run"},
	{regexp.MustCompile(`dùng --bare để cô lập hoàn toàn`), "cliref.flag.bare"},
	{regexp.MustCompile(`thêm --approved`), "cliref.flag.approved"},
	{regexp.MustCompile(`chỉ định bằng --source`), "cliref.flag.source"},
	{regexp.MustCompile(`chạy lại với --hooks`), "cliref.flag.hooks"},
	{regexp.MustCompile(`thì thêm --allow`), "cliref.flag.allow"},
	{regexp.MustCompile(`chạy với --no-plan`), "cliref.flag.no_plan"},
	{regexp.MustCompile(`chạy thêm --arms minimal,bint,ideal`), "cliref.flag.arms"},
	{regexp.MustCompile(`\(hoặc --task <id>\)`), "cliref.flag.task_field"},
	{regexp.MustCompile(`với --delivery bint`), "cliref.flag.delivery_bint"},
	{regexp.MustCompile(`bằng --prospective`), "cliref.flag.prospective"},
	{regexp.MustCompile(`cần --reason`), "cliref.flag.need_reason"},
	{regexp.MustCompile(`cần --evidence`), "cliref.flag.need_evidence"},
	{regexp.MustCompile(`thiếu --type`), "cliref.flag.need_type"},
	{regexp.MustCompile(`chỉ định --kind`), "cliref.flag.pick_kind"},
	{regexp.MustCompile(`chưa cho --paths`), "cliref.flag.paths_none"},
	{regexp.MustCompile(`không khớp --paths`), "cliref.flag.paths_nomatch"},
}
