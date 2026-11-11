package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// Ảnh chụp văn bản (đã bỏ mã màu) của từng màn hình ở 80×24 và 140×40, vi/en/locale giả.
func TestGoldenScreens(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, lang := range []string{"vi", "en", "xx"} {
			screens := map[string]func() *Model{
				"overview": func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1]) },
				"init":     func() *Model { return newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), sz[0], sz[1]) },
				"knowledge": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2")
					return m
				},
				"knowledge_detail": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2", "j", "enter")
					return m
				},
				"knowledge_filtered": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, fullPending(), sz[0], sz[1])
					press(t, m, "enter", "x", "x")
					return m
				},
				"knowledge_batch": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2")
					m.Update(actions.ResultMsg{ID: "knowledge.plan_batch", Value: bintapp.BatchPlan{To: knowledge.StatusVerified, Ready: []string{"01KNOW000002", "01KNOW000005"},
						Blocked: []bintapp.BatchBlocked{{ID: "01KNOW000001", Reason: bintapp.BlockedChecks,
							Findings: []knowledge.Finding{{Level: knowledge.LevelError, Code: knowledge.CodeFingerprint, Detail: "bằng chứng đã thay đổi"}}}}}})
					return m
				},
				"knowledge_add": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2", "n")
					return m
				},
				"tasks": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3")
					return m
				},
				"tasks_detail": func() *Model { // thẻ Phiếu ở khung chi tiết (hẹp: một khung)
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3", "enter")
					return m
				},
				"tasks_docs": func() *Model { // thẻ Tài liệu: Markdown + kết quả lint
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3", ".", "tab")
					return m
				},
				"tasks_review": func() *Model { // thẻ Review: phát hiện xếp critical → low
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3", ".", ".", ".", ".", "tab")
					return m
				},
				"tasks_new": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3", "n")
					return m
				},
				"tasks_approve": func() *Model { // hộp duyệt dựng từ kế hoạch chỉ đọc
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "3")
					m.Update(actions.ResultMsg{ID: "tasks.plan_approve", Value: bintapp.ApprovePlan{Task: m.tk.Cursor(), VerifyCommand: "go test ./...",
						Paths: []string{"migrations/002.sql"}, Resumable: true}})
					return m
				},
				"runs": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "4")
					return m
				},
				"runs_detail": func() *Model { // chi tiết lượt thất bại: nhãn, tín hiệu, đầu ra, sự kiện (hẹp: một khung)
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "4", "j", "enter")
					return m
				},
				"runs_rate": func() *Model { // hộp chấm có ghi chú
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "4", "-")
					return m
				},
				"runs_scan": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "4", "S")
					return m
				},
				"runs_lessons": func() *Model { // thẻ Bài học: thư viện nhóm theo vùng + bản tóm tắt thất bại
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "4", ".")
					m.Update(actions.ResultMsg{ID: "runs.lesson_digest", Opts: evolvesvc.ExperienceDigestOptions{Ref: "latest"}, Value: sampleDigest()})
					return m
				},
				"evolve": func() *Model { // thẻ Cơ hội
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6")
					return m
				},
				"evolve_proposals": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".")
					return m
				},
				"evolve_changes": func() *Model { // diff nội dung tri thức với hiện trạng
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".", "v", "enter")
					return m
				},
				"evolve_script": func() *Model { // khung đề xuất script kèm cảnh báo không tự chạy
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".", "j", "j", "j", "j", "v", "enter")
					return m
				},
				"evolve_apply": func() *Model { // hộp đỏ áp dụng chưa kiểm chứng và rủi ro cao
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".")
					m.Update(actions.ResultMsg{ID: "evolve.plan_apply", Value: samplePlanApply(m.ev.Cursor().Proposal)})
					return m
				},
				"evolve_rollback": func() *Model { // hộp quay lui có tệp xung đột
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".")
					m.Update(actions.ResultMsg{ID: "evolve.plan_rollback", Value: samplePlanRollback(m.ev.Cursor().Proposal)})
					return m
				},
				"evolve_history": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".", ".")
					return m
				},
				"evolve_queue": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "6", ".", ".", ".")
					return m
				},
				"knowledge_publish": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "2")
					m.kn.EnterSub("publish")
					m.Update(actions.ResultMsg{ID: "bootstrap.publish_preview", Value: bintapp.PublishReport{Changed: true,
						Diff:  "--- a/AGENTS.md\n+++ b/AGENTS.md\n@@ -3,2 +3,3 @@\n ## Quy ước\n+- **Endpoint phải có /v1**\n giữ nguyên\n",
						Steps: []bintapp.Step{{Name: "AGENTS.md", Status: bintapp.StepUpdated}}}})
					return m
				},
				"palette": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, ":")
					return m
				},
				"help": func() *Model {
					m := newModel(t, Options{Mode: theme.Mono, Lang: lang}, sampleData(), sz[0], sz[1])
					press(t, m, "?")
					return m
				},
			}
			for name, build := range screens {
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
