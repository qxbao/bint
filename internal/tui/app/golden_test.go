package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

var update = flag.Bool("update", false, "ghi lại tệp golden")

// checkGolden so sánh got với testdata/golden/<name>; -update để ghi lại. Golden chỉ chống thụt lùi;
// thẩm mỹ do chủ dự án duyệt bằng ảnh VHS (internal/tui/testdata/vhs).
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("thiếu golden %s (chạy go test ./internal/tui/app -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("golden %s khác kết quả hiện tại (chạy với -update nếu thay đổi là chủ ý):\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

// mainScreens liệt kê các ảnh chụp văn bản của mọi màn hình trừ Hệ thống (systemScreens) ở cỡ w×h, ngôn ngữ lang.
func mainScreens(t *testing.T, lang string, w, h int) map[string]func() *Model {
	return map[string]func() *Model{
		"overview": func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h) },
		"init":     func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, h) },
		"knowledge": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "2")
			return m
		},
		"knowledge_detail": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "2", "j", "enter")
			return m
		},
		"knowledge_filtered": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, fullPending(), w, h)
			press(t, m, "enter", "x", "x")
			return m
		},
		"knowledge_batch": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "2")
			m.Update(actions.ResultMsg{ID: "knowledge.plan_batch", Value: bintapp.BatchPlan{To: knowledge.StatusVerified, Ready: []string{"01KNOW000002", "01KNOW000005"},
				Blocked: []bintapp.BatchBlocked{{ID: "01KNOW000001", Reason: bintapp.BlockedChecks,
					Findings: []knowledge.Finding{{Level: knowledge.LevelError, Code: knowledge.CodeFingerprint, Detail: "bằng chứng đã thay đổi"}}}}}})
			return m
		},
		"knowledge_add": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "2", "n")
			return m
		},
		"tasks": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3")
			return m
		},
		"tasks_detail": func() *Model { // thẻ Phiếu ở khung chi tiết (hẹp: một khung)
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3", "enter")
			return m
		},
		"tasks_docs": func() *Model { // thẻ Tài liệu: Markdown + kết quả lint
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3", ".", "tab")
			return m
		},
		"tasks_review": func() *Model { // thẻ Review: phát hiện xếp critical → low
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3", ".", ".", ".", ".", "tab")
			return m
		},
		"tasks_new": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3", "n")
			return m
		},
		"tasks_approve": func() *Model { // hộp duyệt dựng từ kế hoạch chỉ đọc
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "3")
			m.Update(actions.ResultMsg{ID: "tasks.plan_approve", Value: bintapp.ApprovePlan{Task: m.tk.Cursor(), VerifyCommand: "go test ./...",
				Paths: []string{"migrations/002.sql"}, Resumable: true}})
			return m
		},
		"runs": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "4")
			return m
		},
		"runs_detail": func() *Model { // chi tiết lượt thất bại: nhãn, tín hiệu, đầu ra, sự kiện (hẹp: một khung)
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "4", "j", "enter")
			return m
		},
		"runs_rate": func() *Model { // hộp chấm có ghi chú
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "4", "-")
			return m
		},
		"runs_scan": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "4", "S")
			return m
		},
		"runs_lessons": func() *Model { // thẻ Bài học: thư viện nhóm theo vùng + bản tóm tắt thất bại
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "4", ".")
			m.Update(actions.ResultMsg{ID: "runs.lesson_digest", Opts: evolvesvc.ExperienceDigestOptions{Ref: "latest"}, Value: sampleDigest()})
			return m
		},
		"evolve": func() *Model { // thẻ Cơ hội
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6")
			return m
		},
		"evolve_proposals": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".")
			return m
		},
		"evolve_changes": func() *Model { // diff nội dung tri thức với hiện trạng
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".", "v", "enter")
			return m
		},
		"evolve_script": func() *Model { // khung đề xuất script kèm cảnh báo không tự chạy
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".", "j", "j", "j", "j", "v", "enter")
			return m
		},
		"evolve_apply": func() *Model { // hộp đỏ áp dụng chưa kiểm chứng và rủi ro cao
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".")
			m.Update(actions.ResultMsg{ID: "evolve.plan_apply", Value: samplePlanApply(m.ev.Cursor().Proposal)})
			return m
		},
		"evolve_rollback": func() *Model { // hộp quay lui có tệp xung đột
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".")
			m.Update(actions.ResultMsg{ID: "evolve.plan_rollback", Value: samplePlanRollback(m.ev.Cursor().Proposal)})
			return m
		},
		"evolve_history": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".", ".")
			return m
		},
		"evolve_queue": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "6", ".", ".", ".")
			return m
		},
		"eval": func() *Model { // thẻ Ứng viên
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5")
			return m
		},
		"eval_detail": func() *Model { // chi tiết ứng viên bị chặn (hẹp: một khung)
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", "j", "enter")
			return m
		},
		"eval_cases": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", ".")
			return m
		},
		"eval_batches": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", ".", ".")
			return m
		},
		"eval_report": func() *Model { // báo cáo đợt: biểu đồ cột, bảng nhánh, so sánh cặp (hẹp: một khung)
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", ".", ".", "j", "enter")
			return m
		},
		"eval_approve_red": func() *Model { // hộp đỏ duyệt ứng viên lộ lời giải chưa kiểm chứng
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", "j", "a")
			return m
		},
		"eval_run_form": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "5", "r")
			return m
		},
		"eval_log": func() *Model { // thẻ Log: lưới ô tiến độ và log của việc chạy đợt
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			evalGridModel(t, m)
			return m
		},
		"knowledge_publish": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "2")
			m.kn.EnterSub("publish")
			m.Update(actions.ResultMsg{ID: "bootstrap.publish_preview", Value: bintapp.PublishReport{Changed: true,
				Diff:  "--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -3,2 +3,3 @@\n ## Quy ước\n+- **Endpoint phải có /v1**\n giữ nguyên\n",
				Steps: []bintapp.Step{{Name: "AGENTS.md", Status: bintapp.StepUpdated}}}})
			return m
		},
		// Trình hướng dẫn init (giai đoạn 5b): mỗi bước chính trên repo chưa init.
		"wizard_hooks": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, h)
			press(t, m, "enter")
			return m
		},
		"wizard_git": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, h)
			press(t, m, "enter", "enter", "enter", "enter", "enter", "j", " ")
			return m
		},
		"wizard_preview": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, h)
			press(t, m, "d")
			m.Update(actions.ResultMsg{ID: "init.plan", Value: wizardPlan()})
			return m
		},
		"wizard_report": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, h)
			press(t, m, "d")
			m.Update(actions.ResultMsg{ID: "init.run", Value: wizardReport()})
			return m
		},
		"palette": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, ":")
			return m
		},
		"help": func() *Model {
			m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), w, h)
			press(t, m, "?")
			return m
		},
	}
}

// Ảnh chụp văn bản (đã bỏ mã màu) của từng màn hình ở 80×24 và 140×40, vi/en/locale giả; riêng 60×20 (chế độ hẹp) chỉ có vi cho
// mỗi màn hình chính (TestGoldenNarrow).
func TestGoldenScreens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			for name, build := range mainScreens(t, lang, sz[0], sz[1]) {
				checkGolden(t, fmt.Sprintf("%s_%dx%d_%s.txt", name, sz[0], sz[1], lang), plain(build().Render())+"\n")
			}
		}
	}
}

// Ảnh chụp có mã màu của Tổng quan ở từng chế độ màu (bắt lỗi đổi vai trò màu).
func TestGoldenColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 80, 24)
		out := strings.ReplaceAll(m.Render(), "\x1b", "␛")
		checkGolden(t, fmt.Sprintf("overview_80x24_vi_%s.ansi.txt", mode), out+"\n")
	}
}

// Ảnh chụp có mã màu của màn Tri thức (danh sách rộng với chi tiết) ở từng chế độ màu.
func TestGoldenKnowledgeColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 140, 40)
		press(t, m, "2", "x", "j")
		out := strings.ReplaceAll(m.Render(), "\x1b", "␛")
		checkGolden(t, fmt.Sprintf("knowledge_140x40_vi_%s.ansi.txt", mode), out+"\n")
	}
}

// Ảnh chụp có mã màu của màn Tác vụ (danh sách rộng với chi tiết ở thẻ Review) ở từng chế độ màu.
func TestGoldenTasksColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 140, 40)
		press(t, m, "3", ".", ".", ".", ".", "tab")
		out := strings.ReplaceAll(m.Render(), "\x1b", "␛")
		checkGolden(t, fmt.Sprintf("tasks_140x40_vi_%s.ansi.txt", mode), out+"\n")
	}
}

// Ảnh chụp có mã màu của màn Đánh giá (báo cáo đợt với biểu đồ cột) ở từng chế độ màu.
func TestGoldenEvalColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 140, 40)
		press(t, m, "5", ".", ".", "j")
		checkGolden(t, fmt.Sprintf("eval_140x40_vi_%s.ansi.txt", mode), strings.ReplaceAll(m.Render(), "\x1b", "␛")+"\n")
	}
}

// Ảnh chụp có mã màu của màn Lượt chạy (khung chi tiết lượt thất bại) và Tiến hoá (diff nội dung) ở từng chế độ màu.
func TestGoldenRunsEvolveColorModes(t *testing.T) {
	for _, mode := range allModes() {
		m := newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 140, 40)
		press(t, m, "4", "j")
		checkGolden(t, fmt.Sprintf("runs_140x40_vi_%s.ansi.txt", mode), strings.ReplaceAll(m.Render(), "\x1b", "␛")+"\n")
		m = newModel(t, Options{Mode: mode, Lang: "vi"}, sampleData(), 140, 40)
		press(t, m, "6", ".", "v", "tab")
		checkGolden(t, fmt.Sprintf("evolve_140x40_vi_%s.ansi.txt", mode), strings.ReplaceAll(m.Render(), "\x1b", "␛")+"\n")
	}
}

func wizardPlan() bintapp.InitPlan {
	return bintapp.InitPlan{FirstInit: true, Items: []bintapp.PlanItem{
		{Path: ".bint/config.yml", Change: bintapp.PlanCreate, Detail: "harness: claude"},
		{Path: "/state/demo-7f3a9c", Change: bintapp.PlanCreate, Detail: "thư mục trạng thái và SQLite (ngoài repository)"},
		{Path: ".claude/skills", Change: bintapp.PlanCreate, Detail: "6 skill gốc cho claude"},
		{Path: ".claude/settings.local.json", Change: bintapp.PlanCreate, Detail: "cài hook claude"},
		{Path: ".gitignore", Change: bintapp.PlanUpdate, Detail: "thêm khối bint"},
	}}
}

func wizardReport() *bintapp.InitReport {
	return &bintapp.InitReport{FirstInit: true, StateDir: "/state/demo-7f3a9c", Steps: []bintapp.Step{
		{Name: ".bint/config.yml", Status: bintapp.StepCreated, Detail: "harness: claude"},
		{Name: "thư mục trạng thái", Status: bintapp.StepCreated, Detail: "/state/demo-7f3a9c (schema v6)"},
		{Name: "hook claude", Status: bintapp.StepWarn, Detail: "chưa xác minh trust hook"},
		{Name: ".gitignore", Status: bintapp.StepUpdated, Detail: "thêm khối bint"},
	}, NextSteps: []string{"Xác nhận lệnh kiểm thử: điền eval.test_command trong .bint/config.yml", "Kiểm tra môi trường: bint doctor",
		"Xem lượt chạy đã ghi nhận: bint runs list"}}
}

// Trình hướng dẫn init ở mọi bước chính, 80×24 và 140×40, vi/en/xx: không dòng nào vượt chiều rộng, không quá chiều cao và không
// còn khoá i18n chưa dịch.
func TestWizardScreensFitAndHaveNoUntranslatedKeys(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			mk := func(keys ...string) *Model {
				m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), sz[0], sz[1])
				press(t, m, keys...)
				return m
			}
			screens := map[string]*Model{
				"harness": mk(), "hooks": mk("enter"), "mcp": mk("enter", "enter"), "agents": mk("enter", "enter", "enter"),
				"skills": mk("enter", "enter", "enter", "enter"), "git": mk("enter", "enter", "enter", "enter", "enter", "j", " "),
			}
			prev := mk("d")
			prev.Update(actions.ResultMsg{ID: "init.plan", Value: wizardPlan()})
			screens["preview"] = prev
			planning := mk("d")
			screens["planning"] = planning
			failed := mk("d")
			failed.Update(actions.ResultMsg{ID: "init.plan", Err: errors.New("không đọc được cấu hình")})
			screens["preview_error"] = failed
			done := mk("d")
			done.Update(actions.ResultMsg{ID: "init.run", Value: wizardReport()})
			screens["report"] = done
			broke := mk("d")
			broke.Update(actions.ResultMsg{ID: "init.run", Err: errors.New("ghi cấu hình: từ chối quyền")})
			screens["error"] = broke
			ro := newModel(t, Options{Mode: theme.Mono, Lang: lang, ReadOnly: true}, notInitData(), sz[0], sz[1])
			screens["readonly"] = ro
			for name, m := range screens {
				out := plain(m.Render())
				lines := strings.Split(out, "\n")
				if len(lines) > sz[1] {
					t.Errorf("%s %v %s: %d dòng > %d", name, sz, lang, len(lines), sz[1])
				}
				for i, l := range lines {
					if w := ansi.StringWidth(l); w > sz[0] {
						t.Errorf("%s %v %s: dòng %d rộng %d > %d: %q", name, sz, lang, i, w, sz[0], l)
					}
				}
				for _, bad := range []string{"wiz.", "action.init.", "form.init.", "init.group"} {
					if strings.Contains(out, bad) {
						t.Errorf("%s %v %s: còn khoá chưa dịch %q:\n%s", name, sz, lang, bad, out)
					}
				}
			}
		}
	}
}

// Chế độ hẹp 60×20 (vi): ảnh chụp của bảy màn hình chính (Tổng quan, Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá, Hệ thống).
func TestGoldenNarrow(t *testing.T) {
	const w, h = 60, 20
	main := mainScreens(t, "vi", w, h)
	sys := systemScreens(t, "vi", w, h)
	for name, build := range map[string]func() *Model{
		"overview": main["overview"], "knowledge": main["knowledge"], "tasks": main["tasks"], "runs": main["runs"], "eval": main["eval"],
		"evolve": main["evolve"], "system_config": sys["system_config"],
	} {
		checkGolden(t, fmt.Sprintf("%s_%dx%d_vi.txt", name, w, h), plain(build().Render())+"\n")
	}
}
