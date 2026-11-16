package task

import (
	"strings"
	"testing"
)

// Bản tasks.md b-feat ghi lại ở benchmark B-mid (2026-11-16): giữ TSK-ID/AC, đánh dấu [x] nhưng bỏ `— xong khi: …`
// nên tác vụ bị chặn ở lần lint kế tiếp. RestoreTaskLines lấy lại nội dung dòng của bản trước bước, giữ đánh dấu mới.
const (
	benchTasksBefore = `# Công việc
- [ ] TSK-1 [AC-1, AC-6]: Thêm schema/default/template/Validate cho knowledge.publish.max_bytes. — xong khi: test cấu hình qua.
- [ ] TSK-2 [AC-2, AC-3, AC-5, AC-6]: MeasureAgents đo UTF-8, rune token ước tính, top 5 theo byte. — xong khi: test knowledge qua.
- [ ] TSK-3 [AC-2, AC-3, AC-4, AC-5, AC-6]: PublishReport và CLI có báo cáo cả khi không diff. — xong khi: test app/CLI qua.
- [ ] TSK-4 [AC-2, AC-3, AC-4, AC-5, AC-6]: Doctor dùng chung phép dựng/đo, warn đúng biên. — xong khi: test doctor JSON CLI qua.
- [ ] TSK-5 [AC-1, AC-6]: Cập nhật configuration/knowledge/acceptance. — xong khi: tài liệu có NT-PUBCAP-WARN.
- [ ] TSK-6 [AC-6]: Chạy kiểm thử và kiểm tra tĩnh. — xong khi: go vet/staticcheck Linux và GOOS=windows qua.
`
	// Nguyên văn bench-bint-20261116/log/B-mid.tasks.md.before-fix.
	benchTasksBroken = `# Công việc
- [x] TSK-1 [AC-1, AC-6]: Thêm schema/default/template/Validate cho knowledge.publish.max_bytes. Test cấu hình qua: legacy, null/số dương, merge null ghi đè, 0/âm, float/khóa lạ, RenderMatchesDefaults.
- [x] TSK-2 [AC-2, AC-3, AC-5, AC-6]: MeasureAgents đo UTF-8, rune token ước tính, top 5 theo byte sau render/ID; test knowledge qua, không đổi Selection.
- [x] TSK-3 [AC-2, AC-3, AC-4, AC-5, AC-6]: PublishReport và CLI có báo cáo cả khi không diff; test app/CLI qua, trần thấp không chặn ghi, dry-run không ghi nguồn.
- [x] TSK-4 [AC-2, AC-3, AC-4, AC-5, AC-6]: Doctor dùng chung phép dựng/đo, warn đúng biên, không Fix/mã thoát lỗi; test app và doctor JSON CLI qua, snapshot nguồn không đổi.
- [x] TSK-5 [AC-1, AC-6]: Cập nhật configuration/knowledge/acceptance; NT-PUBCAP-WARN có cách tái hiện và bằng chứng thực đo repository 10.610 byte/3.051 token, trần null; chưa tự ký nghiệm thu.
- [ ] TSK-6 [AC-6]: Test hẹp config/knowledge/app/CLI và inventory qua; go vet/staticcheck Linux và GOOS=windows qua; review độc lập không phát hiện lỗi. Lượt test rộng app bị sandbox cấm socket ở TestMCPGatewayLogWithDaemon và TestMCPGatewayLogOldDaemon404; chờ Core chạy go test -count=1 -timeout 30m ./... đã xác nhận. AC được ghi tự đánh giá kèm bằng chứng, không tự chuyển trạng thái/done.
`
)

func lintTasksText(text string) []ArtifactIssue {
	b := &reportBuilder{}
	lintTasks(b, text)
	return b.r.Errors
}

func TestRestoreTaskLinesBenchBrokenTasks(t *testing.T) {
	if errs := lintTasksText(benchTasksBroken); len(errs) != 6 || errs[0].Code != "task_incomplete" {
		t.Fatalf("bản hỏng phải trượt hợp đồng ở cả 6 TSK: %v", errs)
	}
	out, merged := RestoreTaskLines(benchTasksBefore, benchTasksBroken)
	if !merged {
		t.Fatal("cùng tập TSK-ID: phải ghép được")
	}
	if errs := lintTasksText(out); len(errs) != 0 {
		t.Fatalf("bản phục hồi phải đạt hợp đồng: %v\n%s", errs, out)
	}
	want := strings.Replace(benchTasksBefore, "- [ ] TSK-", "- [x] TSK-", 5) // TSK-1..5 [x], TSK-6 còn [ ]
	if out != want {
		t.Fatalf("bản phục hồi:\n%s\nmuốn:\n%s", out, want)
	}
}

func TestRestoreTaskLinesFallsBackWhenIDsDiffer(t *testing.T) {
	before := "- [ ] TSK-1 [AC-1]: a — xong khi: x\n- [ ] TSK-2 [AC-1]: b — xong khi: y\n"
	for name, after := range map[string]string{
		"thêm TSK":   "- [x] TSK-1 [AC-1]: a\n- [x] TSK-2 [AC-1]: b\n- [ ] TSK-3 [AC-1]: c\n",
		"bỏ TSK":     "- [x] TSK-1 [AC-1]: a\n",
		"TSK trùng":  "- [x] TSK-1 [AC-1]: a\n- [ ] TSK-1 [AC-1]: a\n- [x] TSK-2 [AC-1]: b\n",
		"đổi số TSK": "- [x] TSK-1 [AC-1]: a\n- [x] TSK-3 [AC-1]: b\n",
	} {
		if out, merged := RestoreTaskLines(before, after); merged || out != before {
			t.Errorf("%s: phải trả nguyên bản trước, merged=%v out=%q", name, merged, out)
		}
	}
	// CRLF và dòng trong code fence được giữ; dòng TSK trong fence không tính.
	before = "# Công việc\r\n```\r\n- [ ] TSK-9 ví dụ\r\n```\r\n- [ ] TSK-1 [AC-1]: a — xong khi: x\r\n"
	out, merged := RestoreTaskLines(before, "- [X] TSK-1 [AC-1]: a\n")
	if !merged || out != strings.Replace(before, "- [ ] TSK-1", "- [x] TSK-1", 1) {
		t.Fatalf("CRLF/fence: merged=%v out=%q", merged, out)
	}
}
