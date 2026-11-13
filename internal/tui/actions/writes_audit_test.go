package actions

import (
	"sort"
	"strings"
	"testing"
)

// Bảng kiểm Writes (docs/tui-plan.md mục 7.4): mọi hành động ghi tệp, ghi cơ sở dữ liệu hoặc mở trình soạn thảo phải có Writes = true để chế độ chỉ
// đọc ẩn được; mọi hành động còn lại (đọc, xem trước/kế hoạch, giao diện thuần) phải Writes = false. Hành động mới chưa có trong bảng làm test đỏ,
// buộc người thêm phân loại.
var writesAudit = map[string]string{
	// ghi tệp / cơ sở dữ liệu / mở trình soạn thảo
	"init.run": "ghi cấu hình, hook, skill", "knowledge.approve": "ghi mục tri thức", "knowledge.approve_refresh": "ghi mục tri thức", "knowledge.reject": "ghi mục tri thức",
	"knowledge.deprecate": "ghi mục tri thức", "knowledge.approve_batch": "ghi mục tri thức", "knowledge.reject_batch": "ghi mục tri thức", "knowledge.edit": "mở trình soạn thảo",
	"knowledge.add": "ghi mục tri thức", "knowledge.rm": "xoá tệp mục", "knowledge.verify_apply": "ghi chuyển trạng thái", "knowledge.revalidate": "ghi mục, tiêu tiền",
	"bootstrap.publish": "ghi AGENTS.md", "bootstrap": "ghi báo cáo khảo sát",
	"tasks.run": "chạy workflow, ghi tác vụ", "tasks.resume": "chạy workflow", "tasks.approve": "ghi xác nhận", "tasks.status": "ghi trạng thái", "tasks.new": "tạo tác vụ",
	"tasks.verify": "ghi kết quả kiểm chứng", "tasks.set": "ghi phiếu", "tasks.write": "ghi tài liệu", "tasks.edit_doc": "mở trình soạn thảo", "tasks.criteria": "ghi tiêu chí",
	"tasks.land": "áp bản vá lên cây gốc, gỡ worktree", "tasks.cancel": "ghi trạng thái, gỡ worktree", "tasks.abort": "ghi trạng thái, gỡ hợp đồng thuê khoá", "tasks.error": "ghi lỗi", "tasks.review": "ghi review", "tasks.review_edit": "mở trình soạn thảo", "tasks.add_knowledge": "ghi mục tri thức", "tasks.open_file": "mở trình soạn thảo",
	"runs.rate": "ghi đánh giá lượt", "runs.scan": "ghi nhãn lượt", "runs.ingest": "nạp spool vào CSDL",
	"eval.mine": "ghi ứng viên", "eval.verify": "ghi kiểm chứng", "eval.spec": "ghi đặc tả", "eval.approve": "ghi tác vụ đã duyệt", "eval.reject": "ghi trạng thái ứng viên",
	"eval.run": "ghi đợt đánh giá", "eval.resume": "ghi đợt đánh giá", "eval.bootstrap": "ghi ảnh chụp tri thức", "eval.clean": "xoá bộ đệm", "eval.open_file": "mở trình soạn thảo",
	"evolve.propose": "ghi đề xuất", "evolve.evaluate": "ghi kết quả đối chiếu", "evolve.apply": "ghi tệp áp dụng", "evolve.reject": "ghi trạng thái đề xuất", "evolve.rollback": "ghi tệp quay lui",
	"evolve.process": "ghi đề xuất, tiêu tiền", "evolve.cycle": "ghi đề xuất, tiêu tiền",
	"system.config_edit": "mở trình soạn thảo", "system.config_save": "ghi cấu hình", "system.language_save": "ghi cấu hình", "system.component_add": "ghi thành phần",
	"system.component_approve": "ghi thành phần", "system.component_deprecate": "ghi thành phần", "system.skill_overlay": "mở trình soạn thảo", "system.skill_new": "ghi skill tuỳ biến, mở trình soạn thảo", "system.open_file": "mở trình soạn thảo",
	"system.sync": "ghi skill", "system.repo_apply": "ghi cấu hình repo", "system.config_upgrade": "ghi cấu hình", "system.update": "cài bản mới", "system.index_rebuild": "ghi chỉ mục",
	"mcp.add": "ghi mcp.yml (đăng ký trên máy)", "mcp.remove": "ghi mcp.yml (gỡ đăng ký)", "mcp.index": "chạy codegraph init/sync, ghi .codegraph và git info/exclude",
	"mcp.restart": "dừng upstream của gateway, cắt lời gọi đang chạy", "mcp.gateway_start": "khởi động daemon, ghi gateway.json/log", "mcp.gateway_stop": "tắt daemon",
	"mcp.config_edit": "mở trình soạn thảo (chuyển sang system.config_edit)",
}

// Hành động không ghi: đọc, xem trước (kế hoạch), hoặc giao diện thuần.
var readOnlyAudit = []string{
	"app.quit", "app.help", "app.palette", "app.language", "app.notifications", "app.mouse", "app.next_tab", "app.prev_tab",
	"overview.refresh", "overview.doctor", "init.plan", "init.continue", "init.recheck",
	"knowledge.plan_batch", "knowledge.verify", "knowledge.review", "knowledge.list", "knowledge.show", "knowledge.filter", "knowledge.select", "knowledge.transitions", "knowledge.refresh",
	"knowledge.next_view", "knowledge.prev_view", "knowledge.search", "knowledge.stats", "knowledge.diff", "bootstrap.publish_preview", "inspect", "bootstrap.skill", "context",
	"tasks.plan_approve", "tasks.lint", "tasks.diff", "tasks.transitions", "tasks.list", "tasks.show", "tasks.runs", "tasks.filter", "tasks.refresh", "tasks.next_view", "tasks.prev_view", "tasks.finding_prefill",
	"runs.list", "runs.show", "runs.lesson_show", "runs.lesson_digest", "runs.filter", "runs.refresh", "runs.next_view", "runs.prev_view",
	"eval.list", "eval.show", "eval.estimate", "eval.report", "eval.baseline", "eval.split", "eval.filter", "eval.refresh", "eval.next_view", "eval.prev_view",
	"evolve.scan", "evolve.list", "evolve.show", "evolve.plan_apply", "evolve.plan_rollback", "evolve.history", "evolve.queue", "evolve.next_sub", "evolve.refresh", "evolve.next_view", "evolve.prev_view",
	"system.next_view", "system.prev_view", "system.refresh", "system.filter", "system.config_check", "system.config_rebase", "system.config_view", "system.harness_list", "system.harness_show",
	"system.harness_runtime", "system.harness_version", "system.component_list", "system.component_show", "system.render", "system.skill_list", "system.skill_show", "system.skill_lint", "system.flow_list", "system.flow_show", "system.flow_lint", "system.skill_diff",
	"system.skill_versions", "system.skill_check", "system.skill_regress", "system.sync_plan", "system.repo_state", "system.repo_plan", "system.config_upgrade_plan", "system.restart",
	"system.doctor", "system.doctor_filter",
	// mcp.check không ghi dữ liệu của Bint (chỉ tạo thư mục cwd riêng của service kiểu state) và agent được chạy (D5): chỉ đọc.
	"mcp.next_view", "mcp.prev_view", "mcp.refresh", "mcp.filter", "mcp.list", "mcp.gateway_status", "mcp.grants", "mcp.tools", "mcp.log", "mcp.check",
}

func TestWritesAudit(t *testing.T) {
	reg := Default()
	want := map[string]bool{}
	for id, why := range writesAudit {
		if why == "" {
			t.Errorf("%s: thiếu lý do trong bảng kiểm Writes", id)
		}
		want[id] = true
	}
	for _, id := range readOnlyAudit {
		if want[id] {
			t.Errorf("%s nằm ở cả hai danh sách", id)
		}
		want[id] = false
	}
	for id := range want {
		if _, ok := reg.ByID(id); !ok {
			t.Errorf("bảng kiểm nhắc hành động %s không còn trong sổ", id)
		}
	}
	var unclassified []string
	for _, a := range reg.All() {
		w, ok := want[a.ID]
		switch {
		case !ok && strings.HasPrefix(a.ID, "nav."):
			if a.Writes {
				t.Errorf("%s (điều hướng) không được Writes", a.ID)
			}
		case !ok:
			unclassified = append(unclassified, a.ID)
		case a.Writes != w:
			t.Errorf("%s: Writes = %v, bảng kiểm muốn %v", a.ID, a.Writes, w)
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("hành động chưa phân loại ghi/đọc trong bảng kiểm Writes: %v", unclassified)
	}
}
