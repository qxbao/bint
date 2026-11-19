package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/task"
)

// Kiểm tra mục tiêu `TestGateEvidenceAndBudget` của phiếu 01M7QC750YPHTMKHY3MVEBVSB8 (GĐ3: review chặn cần bằng chứng tái hiện, ngân sách
// diff, cổng hồi quy hiệu năng).
//
// Hộp đen, chỉ dùng API đã có: khoá cấu hình mới đặt qua YAML (config.Decode: thiếu khoá là lỗi lúc chạy), kết quả đọc từ JSON thô
// review.json/state.json, review.md, tệp log trong thư mục tác vụ, đề bài agent giả nhận và chuỗi bước. Mỗi chỗ sai báo riêng kèm giá trị đo.

// ---- tiện ích chung ----

// bgSetYAML giải mã một đoạn YAML đè lên cấu hình của harness (khoá mới chưa khai báo → lỗi KnownFields).
func bgSetYAML(h *harness, y string) error {
	if err := config.Decode(strings.NewReader(y), &h.cfg); err != nil {
		return fmt.Errorf("không đặt được khoá cấu hình qua YAML %q: %v", y, err)
	}
	return nil
}

// bgReadJSON đọc tệp JSON thô trong thư mục tác vụ.
func bgReadJSON(t *testing.T, h *harness, id, name string) (any, string) {
	t.Helper()
	tk := loadTask(t, h, id)
	b, err := os.ReadFile(filepath.Join(tk.Dir, name))
	if err != nil {
		t.Fatalf("đọc %s: %v", name, err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s không phải JSON: %v", name, err)
	}
	return v, string(b)
}

// bgFeatPrompts trả đề bài của từng lần b-feat theo thứ tự.
func bgFeatPrompts(h *harness) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i, c := range h.calls {
		if c == StepFeat {
			out = append(out, h.reqs[i].Prompt)
		}
	}
	return out
}

func bgPrompts(h *harness, step string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for i, c := range h.calls {
		if c == step {
			out = append(out, h.reqs[i].Prompt)
		}
	}
	return out
}

func bgCount(h *harness, step string) int { return len(bgPrompts(h, step)) }

// bgLogs liệt kê tệp perf-N.log trong thư mục tác vụ (mọi độ sâu).
var bgPerfLogRe = regexp.MustCompile(`^perf-\d+\.log$`)

func bgPerfLogs(t *testing.T, h *harness, id string) []string {
	t.Helper()
	tk := loadTask(t, h, id)
	var out []string
	filepath.WalkDir(tk.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && bgPerfLogRe.MatchString(d.Name()) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// bgObjects gom mọi object JSON (mọi độ sâu).
func bgObjects(v any, out *[]map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		*out = append(*out, x)
		for _, e := range x {
			bgObjects(e, out)
		}
	case []any:
		for _, e := range x {
			bgObjects(e, out)
		}
	}
}

// bgTempRe khớp đường dẫn thư mục tạm của test (tên test nằm trong đó: "perf_gate", số trong tên subtest) để không khớp nhầm.
var bgTempRe = regexp.MustCompile(`(?i)TestGateEvidenceAndBudget[^\\/"\s]*`)

// bgClean bỏ tên test khỏi văn bản trước khi so khớp.
func bgClean(s string) string { return bgTempRe.ReplaceAllString(s, "") }

// bgMentions: object có khoá hoặc giá trị chuỗi (trực tiếp) chứa sub (không phân biệt hoa thường).
func bgMentions(m map[string]any, sub string) bool {
	sub = strings.ToLower(sub)
	for k, v := range m {
		if strings.Contains(strings.ToLower(k), sub) {
			return true
		}
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(bgClean(s)), sub) {
			return true
		}
	}
	return false
}

var bgSecRe = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*(ms|s|giây)\b`)

// bgNumbers gom số (giá trị số JSON, và số kèm đơn vị s/ms/giây trong chuỗi) của object và các con của nó.
func bgNumbers(v any, out *[]float64) {
	switch x := v.(type) {
	case float64:
		*out = append(*out, x)
	case string:
		for _, m := range bgSecRe.FindAllStringSubmatch(x, -1) {
			if f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64); err == nil {
				if m[2] == "ms" {
					f /= 1000
				}
				*out = append(*out, f)
			}
		}
	case map[string]any:
		for _, e := range x {
			bgNumbers(e, out)
		}
	case []any:
		for _, e := range x {
			bgNumbers(e, out)
		}
	}
}

// bgSlowMeasured: một số đo khớp lần chạy chậm ~0,6 s (giây, ms hoặc ns).
func bgSlowMeasured(ns []float64) bool {
	for _, n := range ns {
		if (n >= 0.55 && n <= 10) || (n >= 550 && n <= 10000) || (n >= 5.5e8 && n <= 1e10) {
			return true
		}
	}
	return false
}

func bgDump(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 3000 {
		s = s[:3000] + "…"
	}
	return s
}

// bgYAMLPath đọc giá trị theo đường dẫn khoá trong một cây YAML/JSON đã giải mã thành map.
func bgYAMLPath(v any, keys ...string) (any, bool) {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

func bgNum(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// ---- review_evidence ----

const (
	bgP1 = "P1 bỏ sót bề mặt không repro"
	bgP2 = "P2 rủi ro chỉ lập luận"
	bgP3 = "P3 lỗi có lệnh tái hiện"
	// bgP3Cmd là lệnh trong backtick của repro P3 (Bint không chạy, chỉ kiểm hình thức).
	bgP3Cmd = "sh repro-p3.sh"
	bgOld   = "OLD vòng cũ high không repro"
)

func bgFinding(title, kind, sev, repro string) string {
	f := map[string]any{"kind": kind, "severity": sev, "title": title, "path": "app.txt", "lines": "1", "evidence": "lập luận của reviewer"}
	if repro != "" {
		f["repro"] = repro
	}
	b, _ := json.Marshal(f)
	return string(b)
}

// bgReviewBehave: vòng review 1 gửi các phát hiện cho trước, vòng sau sạch.
func (h *harness) bgReviewBehave(first []string) func(context.Context, string, *Env, string) error {
	rounds := 0
	return func(ctx context.Context, step string, e *Env, id string) error {
		if step != StepReview {
			return h.happy(ctx, step, e, id)
		}
		rounds++
		fs := []string{}
		if rounds == 1 {
			fs = first
		}
		_, _, err := e.SubmitReview(id, []byte(`{"summary":"review","findings":[`+strings.Join(fs, ",")+`]}`))
		return err
	}
}

// bgFindings trả các phát hiện (map JSON thô) của review.json theo tiêu đề.
func bgFindings(rev any) map[string]map[string]any {
	out := map[string]map[string]any{}
	rounds, _ := bgYAMLPath(rev, "rounds")
	rs, _ := rounds.([]any)
	for _, r := range rs {
		fs, _ := bgYAMLPath(r, "findings")
		list, _ := fs.([]any)
		for _, f := range list {
			if m, ok := f.(map[string]any); ok {
				if title, _ := m["title"].(string); title != "" {
					out[title] = m
				}
			}
		}
	}
	return out
}

// bgMDSection trả đoạn review.md của phát hiện có tiêu đề title (từ dòng tiêu đề tới tiêu đề kế tiếp).
func bgMDSection(md, title string) string {
	i := strings.Index(md, title)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(md[:i], "\n")
	rest := md[i:]
	if j := strings.Index(rest[1:], "\n#"); j >= 0 {
		rest = rest[:j+1]
	}
	return md[start+1:i] + rest
}

func bgCheckReview(t *testing.T) {
	ctx := context.Background()

	t.Run("hạ mức và giữ mức chặn", func(t *testing.T) {
		h := newHarness(t)
		h.cfg.Workflow.MaxReviewRounds = 3
		h.behave = h.bgReviewBehave([]string{
			bgFinding(bgP1, "bug", "high", ""),
			bgFinding(bgP2, "risk", "critical", "lập luận: luồng đọc cấu hình có thể bỏ sót bề mặt khác"),
			bgFinding(bgP3, "bug", "high", "chạy `"+bgP3Cmd+"` thấy thoát 1"),
		})
		res, err := h.runner().Run(ctx, RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
		if err != nil {
			t.Fatalf("Run: %v (chuỗi bước %v)", err, h.calls)
		}
		if _, err := os.Stat(filepath.Join(loadTask(t, h, res.TaskID).Dir, task.ReviewFile)); err != nil {
			t.Fatalf("không có review.json: lần ghi vòng review (P1 không repro, P2 repro lập luận, P3 repro lệnh) không được từ chối; kết quả %s %q, chuỗi bước %v",
				res.Status, res.Reason, h.calls)
		}
		rev, raw := bgReadJSON(t, h, res.TaskID, task.ReviewFile)
		fs := bgFindings(rev)
		if len(fs) < 3 {
			t.Fatalf("review.json phải có P1, P2, P3 (lần ghi vòng không được từ chối vì repro thiếu/không hợp lệ); kết quả %s %q, review.json:\n%s",
				res.Status, res.Reason, raw)
		}
		for _, c := range []struct{ title, orig string }{{bgP1, "high"}, {bgP2, "critical"}} {
			f := fs[c.title]
			if f["severity"] != "medium" {
				t.Errorf("%s: severity %v trong review.json, muốn medium (mức chặn thiếu repro hợp lệ bị hạ): %s", c.title, f["severity"], bgDump(f))
			}
			if f["downgraded_from"] != c.orig {
				t.Errorf("%s: downgraded_from %v trong review.json, muốn %q: %s", c.title, f["downgraded_from"], c.orig, bgDump(f))
			}
		}
		p3 := fs[bgP3]
		if p3["severity"] != "high" {
			t.Errorf("%s: severity %v, muốn giữ high (repro là lệnh trong backtick): %s", bgP3, p3["severity"], bgDump(p3))
		}
		if d, ok := p3["downgraded_from"]; ok && d != "" && d != nil {
			t.Errorf("%s: không được có downgraded_from (%v)", bgP3, d)
		}
		if r, _ := p3["repro"].(string); !strings.Contains(r, bgP3Cmd) {
			t.Errorf("%s: review.json phải giữ trường repro chứa %q, được %q", bgP3, bgP3Cmd, r)
		}
		mdB, err := os.ReadFile(filepath.Join(loadTask(t, h, res.TaskID).Dir, task.ReviewMD))
		if err != nil {
			t.Fatalf("đọc review.md: %v", err)
		}
		md := string(mdB)
		for _, c := range []struct{ title, orig string }{{bgP1, "high"}, {bgP2, "critical"}} {
			sec := bgMDSection(md, c.title)
			if !strings.Contains(sec, "medium") || !strings.Contains(sec, c.orig) {
				t.Errorf("review.md phải nêu %s bị hạ: có mức medium và mức gốc %s; đoạn:\n%s", c.title, c.orig, sec)
			}
		}
		feats := bgFeatPrompts(h)
		if len(feats) < 2 {
			t.Fatalf("P3 giữ mức chặn: phải mở vòng sửa (b-feat thứ hai), chuỗi bước %v, kết quả %s %q", h.calls, res.Status, res.Reason)
		}
		if !strings.Contains(feats[1], bgP3Cmd) {
			t.Errorf("đề bài vòng sửa của b-feat phải chứa repro của P3 (%q):\n%s", bgP3Cmd, feats[1])
		}
		if strings.Contains(feats[1], bgP1) || strings.Contains(feats[1], bgP2) {
			t.Errorf("đề bài vòng sửa không được đòi sửa P1/P2 (đã hạ xuống medium)")
		}
		if res.Status != task.StatusDone {
			t.Errorf("vòng review 2 sạch thì phải done: %s %q", res.Status, res.Reason)
		}
	})

	t.Run("đối chứng P1+P2 và vòng cũ", func(t *testing.T) {
		h := newHarness(t)
		h.cfg.Workflow.MaxReviewRounds = 3
		h.behave = h.bgReviewBehave([]string{
			bgFinding(bgP1, "bug", "high", ""),
			bgFinding(bgP2, "risk", "critical", "lập luận: luồng đọc cấu hình có thể bỏ sót bề mặt khác"),
		})
		r := h.runner()
		tk, _, err := r.NewTask(ctx, NewTaskOptions{Request: vagueReq, Workflow: StepDo, Plan: ptr(false)})
		if err != nil {
			t.Fatal(err)
		}
		id := tk.State.ID
		// Vòng cũ (ghi trước khi có luật): high không repro.
		old := task.Review{TaskID: id, Rounds: []task.ReviewRound{{Round: 1, At: "2026-11-01T00:00:00Z", By: "agent", Summary: "vòng cũ",
			Findings: []task.Finding{{ID: id + ".1", Kind: "bug", Severity: "high", Title: bgOld, Path: "app.txt", Lines: "1", Evidence: "lập luận cũ"}}}}}
		ob, _ := json.MarshalIndent(old, "", "  ")
		if err := os.WriteFile(filepath.Join(tk.Dir, task.ReviewFile), ob, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := h.tasks.Update(id, time.Now(), func(tk *task.Task) error {
			tk.State.ReviewRounds, tk.State.NextFinding = 1, 1
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var oldRound any
		json.Unmarshal(ob, &oldRound)
		oldR1, _ := bgYAMLPath(oldRound, "rounds")

		res, err := r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id})
		if err != nil {
			t.Fatalf("Run: %v (chuỗi bước %v)", err, h.calls)
		}
		if n := bgCount(h, StepFeat); n != 1 {
			t.Errorf("chỉ P1+P2 (đều bị hạ): không được mở vòng sửa, có %d b-feat; chuỗi bước %v", n, h.calls)
		}
		if res.Status != task.StatusDone {
			t.Errorf("đối chứng phải done (phát hiện bị hạ không tính vào đếm hội tụ/không chặn): %s %q", res.Status, res.Reason)
		}
		rev, raw := bgReadJSON(t, h, id, task.ReviewFile)
		fs := bgFindings(rev)
		for _, c := range []struct{ title, orig string }{{bgP1, "high"}, {bgP2, "critical"}} {
			if f := fs[c.title]; f == nil || f["severity"] != "medium" || f["downgraded_from"] != c.orig {
				t.Errorf("đối chứng: %s phải medium kèm downgraded_from=%s: %s", c.title, c.orig, bgDump(f))
			}
		}
		// Vòng cũ giữ nguyên: số vòng, phát hiện cũ vẫn high, không bị gắn downgraded_from (trường mới rỗng khi ghi lại thì chấp nhận).
		rounds, _ := bgYAMLPath(rev, "rounds")
		rs, _ := rounds.([]any)
		want, _ := oldR1.([]any)
		var oldF map[string]any
		var n any
		if len(rs) > 0 {
			n, _ = bgYAMLPath(rs[0], "round")
			if r1, _ := bgYAMLPath(rs[0], "findings"); r1 != nil {
				if l, _ := r1.([]any); len(l) == 1 {
					oldF, _ = l[0].(map[string]any)
				}
			}
		}
		if len(rs) < 2 || oldF == nil || oldF["title"] != bgOld || oldF["severity"] != "high" || oldF["id"] != id+".1" ||
			(oldF["downgraded_from"] != nil && oldF["downgraded_from"] != "") || n != float64(1) {
			t.Errorf("vòng review cũ không được tính lại/sửa (phát hiện cũ vẫn high, không downgraded_from):\nvòng cũ đã ghi %s\nreview.json:\n%s", bgDump(want), raw)
		}
	})
}

// ---- diff_budget ----

func bgLines(n int, tag string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s dòng %d\n", tag, i)
	}
	return b.String()
}

// bgGoalBig là kiểm tra mục tiêu đóng băng dài hơn trần 20 dòng (không được tính vào ngân sách).
var bgGoalBig = "# kiểm tra mục tiêu đóng băng\n" + strings.Repeat("# chú thích\n", 30) + "test -f done.txt\n"

// bgDiffRun: b-do có b-req (kế hoạch nhỏ) và b-goal ghi test đóng băng 32 dòng; b-feat đầu ghi app.txt 60 dòng + done.txt (62 dòng
// thêm+xoá so với mốc), vòng sửa ghi lại app.txt 9 dòng (11 dòng so với mốc).
func bgDiffRun(t *testing.T, limitYAML string) (*harness, *Result, error) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 3
	if limitYAML != "" {
		if err := bgSetYAML(h, limitYAML); err != nil {
			return h, nil, err
		}
	}
	h.goal = func(_ context.Context, e *Env, _ string) error {
		return os.WriteFile(filepath.Join(e.Root, "goal_check.sh"), []byte(bgGoalBig), 0o644)
	}
	feats := 0
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepReq:
			return h.writePlan(e, id, nil)
		case StepFeat:
			feats++
			body := bgLines(60, "v2")
			if feats > 1 {
				body = bgLines(9, "v2-gọn")
			}
			if err := os.WriteFile(filepath.Join(e.Root, "app.txt"), []byte(body), 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(e.Root, "done.txt"), []byte("ok\n"), 0o644)
		case StepReview:
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		}
		return nil
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(true)})
	return h, res, err
}

var bg62Re = regexp.MustCompile(`\b62\b`)

func bgCheckDiff(t *testing.T) {
	t.Run("không chia lát: diff vượt trần đỏ, vòng sửa xanh", func(t *testing.T) {
		h, res, err := bgDiffRun(t, "workflow:\n  max_diff_lines: 20\n")
		if err != nil {
			t.Fatalf("%v", err)
		}
		if res.TaskID == "" {
			t.Fatalf("không có tác vụ: %+v", res)
		}
		if n := bgCount(h, StepFeat); n < 2 {
			t.Errorf("diff 62 dòng > trần 20 sau b-feat: phải vào vòng sửa (b-feat thứ hai), có %d b-feat; chuỗi bước %v, kết quả %s %q",
				n, h.calls, res.Status, res.Reason)
		}
		if res.Status != task.StatusDone {
			t.Errorf("vòng sửa còn 11 dòng (tệp đóng băng 32 dòng không tính) ≤ 20: phải xanh rồi done, được %s %q", res.Status, res.Reason)
		}
		st, raw := bgReadJSON(t, h, res.TaskID, task.StateFile)
		_ = st
		if !bg62Re.MatchString(bgClean(raw)) {
			t.Errorf("state.json phải ghi kết quả đỏ của ngân sách diff kèm số đo 62 dòng (thêm+xoá, trừ goal_check.sh đóng băng):\n%s", raw)
		}
		// Đỏ phải được nêu cho vòng sửa (như kiểm chứng đỏ): review hoặc b-feat vòng sửa thấy số đo.
		seen := false
		for _, p := range append(bgPrompts(h, StepReview), bgFeatPrompts(h)[min(1, len(bgFeatPrompts(h))):]...) {
			if bg62Re.MatchString(bgClean(p)) {
				seen = true
			}
		}
		if !seen {
			t.Errorf("đề bài review/vòng sửa phải nêu kết quả đỏ của ngân sách diff (62 dòng)")
		}
	})

	t.Run("đối chứng tắt", func(t *testing.T) {
		h, res, err := bgDiffRun(t, "workflow:\n  max_diff_lines: 0\n")
		if err != nil {
			t.Fatalf("%v", err)
		}
		if n := bgCount(h, StepFeat); n != 1 || res.Status != task.StatusDone {
			t.Errorf("max_diff_lines 0 = tắt: một b-feat rồi done, được %d b-feat, %s %q (chuỗi bước %v)", n, res.Status, res.Reason, h.calls)
		}
	})

	t.Run("chia lát: lát hai vượt trần bị bỏ", bgCheckDiffSlice)

	t.Run("validate", func(t *testing.T) {
		cfg := config.Default()
		if err := config.Decode(strings.NewReader("workflow:\n  max_diff_lines: -1\n"), &cfg); err != nil {
			t.Fatalf("khoá workflow.max_diff_lines phải có trong struct: %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_diff_lines") {
			t.Errorf("Validate phải từ chối workflow.max_diff_lines = -1 (nêu tên khoá), được %v", err)
		}
		cfg = config.Default()
		if err := config.Decode(strings.NewReader("workflow:\n  max_diff_lines: 20\n"), &cfg); err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("max_diff_lines 20 hợp lệ, Validate lỗi: %v", err)
		}
	})
}

func bgCheckDiffSlice(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.write("old.txt", slOldBody)
	h.write("verify.sh", slVerifyScript)
	h.commitAll(h.root, "test cũ và lệnh kiểm chứng")
	vc := "sh verify.sh"
	h.cfg.Eval.TestCommand = &vc
	h.cfg.Workflow.Verify.AffectedCommand = "sh {test_files}"
	h.cfg.Workflow.TimeoutMinutes = 45
	if err := bgSetYAML(h, "workflow:\n  max_diff_lines: 20\n"); err != nil {
		t.Fatalf("%v", err)
	}
	var id string
	reviews, slicesRun := 0, 0
	var atL3 struct {
		taken     bool
		c2, extra string
		tree      string
		greenTree string
	}
	h.goal = func(_ context.Context, e *Env, _ string) error { return slWrite(e.Root, slGoalFile, slGoalScript) }
	h.behave = func(ctx context.Context, step string, e *Env, tid string) error {
		switch step {
		case StepReq:
			id = tid
			if _, _, err := e.SetTicket(tid, TicketEdit{ClearCriteria: true, Criteria: []string{"c1.txt ghi ok-c1", "c2.txt ghi ok-c2", "c3.txt ghi ok-c3"}}); err != nil {
				return err
			}
			for n, body := range slDocs(slOpts{}) {
				if _, _, err := e.WriteArtifact(tid, n, body); err != nil {
					return err
				}
			}
			return nil
		case StepReview:
			reviews++
			_, _, err := e.SubmitReview(tid, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		case StepFeat:
			if reviews > 0 || !slLatRe.MatchString(h.slFeatPrompt()) {
				for _, c := range []string{"c1", "c2", "c3"} {
					if err := slWrite(e.Root, c+".txt", "ok-"+c+"\n"); err != nil {
						return err
					}
				}
				return nil
			}
			slicesRun++
			switch slicesRun {
			case 1:
				return slWrite(e.Root, "c1.txt", "ok-c1\n")
			case 2:
				if err := slWrite(e.Root, "c2.txt", "ok-c2\n"); err != nil {
					return err
				}
				return slWrite(e.Root, "l2-extra.txt", bgLines(60, "phụ L2"))
			case 3:
				atL3.taken = true
				atL3.c2, atL3.extra = h.read("c2.txt"), h.read("l2-extra.txt")
				if tr, err := WorktreeSnapshot(ctx, h.root); err == nil {
					atL3.tree = tr
				}
				if id != "" {
					if g, err := h.gitTree(ctx, slSliceRef(id, "slice-green")); err == nil {
						atL3.greenTree = g
					}
				}
				return slWrite(e.Root, "c3.txt", "ok-c3\n")
			}
			return fmt.Errorf("b-feat theo lát lần thứ %d", slicesRun)
		}
		return nil
	}
	r := h.runner()
	res, err := r.Run(ctx, RunOptions{Workflow: StepDo, Request: slReq, Plan: ptr(true)})
	if err != nil {
		t.Fatalf("lượt đầu: %v", err)
	}
	id = res.TaskID
	if res.Status == task.StatusBlocked && strings.HasPrefix(res.Reason, "kế hoạch lớn") {
		if res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true}); err != nil {
			t.Fatalf("resume sau cổng cỡ kế hoạch: %v", err)
		}
	}
	// Lát vượt trần giữ cây và dừng chờ người; người chọn --slice discard thì lát bị bỏ, cây về điểm xanh, lát kế chạy.
	if res.Status == task.StatusBlocked && slicesRun == 2 {
		if res, err = r.Run(ctx, RunOptions{Workflow: StepDo, TaskRef: id, Resume: true, Slice: SliceDiscard}); err != nil {
			t.Fatalf("resume --slice discard: %v", err)
		}
	}
	if slicesRun < 2 {
		t.Fatalf("kịch bản phải chạy theo lát (ít nhất 2 lát), chạy %d lát; chuỗi bước %v, kết quả %s %q", slicesRun, h.calls, res.Status, res.Reason)
	}
	items, ok := stateSlices(loadTask(t, h, id))
	if !ok || len(items) != 3 {
		t.Fatalf("State.Slices: %s", slDump(items))
	}
	txt := slText(items[1])
	if slStatus(items[1]) != "discarded" {
		t.Errorf("lát 2 (61 dòng thêm+xoá so với điểm xanh > trần 20) phải discarded: %s", txt)
	}
	if !strings.Contains(txt, "61") || !strings.Contains(txt, "20") {
		t.Errorf("lý do bỏ lát 2 phải là số đo (61 dòng, trần 20): %s", txt)
	}
	if slStatus(items[0]) != "accepted" {
		t.Errorf("lát 1 (1 dòng) phải accepted: %s", slText(items[0]))
	}
	if !atL3.taken {
		t.Fatalf("lát 3 phải vẫn chạy sau khi bỏ lát 2: chuỗi bước %v", h.calls)
	}
	if atL3.c2 != "<không có>" || atL3.extra != "<không có>" {
		t.Errorf("bỏ lát 2 thì cây phải về điểm xanh: đầu lát 3 c2.txt=%q l2-extra.txt=%q", atL3.c2, bgShort(atL3.extra))
	}
	if atL3.greenTree != "" && atL3.tree != "" && atL3.greenTree != atL3.tree {
		t.Errorf("đầu lát 3: cây %q phải trùng điểm xanh slice-green %q", atL3.tree, atL3.greenTree)
	}
}

func bgShort(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

func (h *harness) gitTree(ctx context.Context, ref string) (string, error) {
	out, err := gitrepo.Run(ctx, h.root, "rev-parse", "--verify", "-q", ref+"^{tree}")
	return strings.TrimSpace(out), err
}

// ---- perf_gate ----

// bgPerfScript: ngủ theo slow.txt (không có = 0 s); có fail.txt thì thoát 1.
const bgPerfScript = `s=0
if [ -f slow.txt ]; then s=$(cat slow.txt); fi
if [ -f fail.txt ]; then echo "perf: lỗi"; exit 1; fi
sleep "$s"
echo "perf ok $s"
`

// bgPerfRun: b-do không b-req; b-feat đầu ghi app/done và gọi first(root), các vòng sửa gọi fix(root). Review luôn sạch.
func bgPerfRun(t *testing.T, perfYAML string, first, fix func(root string) error) (*harness, *Result, error) {
	h := newHarness(t)
	h.cfg.Workflow.MaxReviewRounds = 1
	h.write("perf.sh", bgPerfScript)
	h.commitAll(h.root, "lệnh đo hiệu năng")
	if perfYAML != "" {
		if err := bgSetYAML(h, perfYAML); err != nil {
			return h, nil, err
		}
		// Lệnh đo phải được người xác nhận mới chạy; ở đây coi như đã xác nhận qua eval.test_command (đường approve: TestPerfCommandNeedsApproval).
		if pc := strings.TrimSpace(h.cfg.Workflow.Perf.Command); pc != "" {
			h.cfg.Eval.TestCommand = &pc
		}
	}
	feats := 0
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		switch step {
		case StepFeat:
			feats++
			if err := os.WriteFile(filepath.Join(e.Root, "app.txt"), []byte("v2\n"), 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(e.Root, "done.txt"), []byte("ok\n"), 0o644); err != nil {
				return err
			}
			if feats == 1 {
				return first(e.Root)
			}
			return fix(e.Root)
		case StepReview:
			_, _, err := e.SubmitReview(id, []byte(`{"summary":"ổn","findings":[]}`))
			return err
		}
		return nil
	}
	res, err := h.runner().Run(context.Background(), RunOptions{Workflow: StepDo, Request: vagueReq, Plan: ptr(false)})
	return h, res, err
}

func bgWriteFn(name, body string) func(string) error {
	return func(root string) error { return os.WriteFile(filepath.Join(root, name), []byte(body), 0o644) }
}

// bgPerfObjects: object trong state.json nói tới cổng perf (khoá/chuỗi chứa "perf").
func bgPerfObjects(st any) []map[string]any {
	var all, out []map[string]any
	bgObjects(st, &all)
	for _, m := range all {
		if bgMentions(m, "perf") {
			out = append(out, m)
		}
	}
	return out
}

const bgPerfYAML = "workflow:\n  perf:\n    command: sh perf.sh\n    max_slowdown: 1.5\n"

func bgCheckPerf(t *testing.T) {
	t.Run("chậm đỏ rồi vòng sửa xanh", func(t *testing.T) {
		h, res, err := bgPerfRun(t, bgPerfYAML, bgWriteFn("slow.txt", "0.6\n"), bgWriteFn("slow.txt", "0\n"))
		if err != nil {
			t.Fatalf("%v", err)
		}
		if n := bgCount(h, StepFeat); n < 2 {
			t.Errorf("perf 0,6 s so với mốc ~0 s (> ×1.5 và > 0,1 s): phải đỏ và vào vòng sửa, có %d b-feat; chuỗi bước %v, kết quả %s %q",
				n, h.calls, res.Status, res.Reason)
		}
		if res.Status != task.StatusDone {
			t.Errorf("vòng sửa về 0 s: perf xanh rồi done, được %s %q", res.Status, res.Reason)
		}
		st, raw := bgReadJSON(t, h, res.TaskID, task.StateFile)
		objs := bgPerfObjects(st)
		if len(objs) == 0 {
			t.Fatalf("state.json phải ghi kết quả cổng hiệu năng (không có object nào nói tới perf):\n%s", bgShortN(raw, 4000))
		}
		var ns []float64
		for _, o := range objs {
			bgNumbers(o, &ns)
		}
		if !bgSlowMeasured(ns) {
			t.Errorf("kết quả perf trong state.json phải có số đo của lần chậm (~0,6 s), các số đo được %v: %s", ns, bgDump(objs))
		}
		logs := bgPerfLogs(t, h, res.TaskID)
		if len(logs) == 0 {
			t.Errorf("phải có log perf-N.log trong thư mục tác vụ")
		}
		seen := false
		for _, p := range append(bgPrompts(h, StepReview), bgFeatPrompts(h)[min(1, len(bgFeatPrompts(h))):]...) {
			if strings.Contains(strings.ToLower(bgClean(p)), "perf") {
				seen = true
			}
		}
		if !seen {
			t.Errorf("đề bài review/vòng sửa phải nêu kết quả đỏ của cổng hiệu năng (perf)")
		}
	})

	t.Run("lệnh đo thoát khác không thì đỏ", func(t *testing.T) {
		h, res, err := bgPerfRun(t, bgPerfYAML, bgWriteFn("fail.txt", "x\n"), func(string) error { return nil })
		if err != nil {
			t.Fatalf("%v", err)
		}
		if res.Status == task.StatusDone {
			t.Errorf("lệnh đo hiệu năng thoát 1 sau b-feat: coi như kiểm chứng không qua, không được done (chuỗi bước %v)", h.calls)
		}
		if n := bgCount(h, StepFeat); n < 2 {
			t.Errorf("perf đỏ (thoát 1): phải vào vòng sửa, có %d b-feat; chuỗi bước %v, kết quả %s %q", n, h.calls, res.Status, res.Reason)
		}
		st, raw := bgReadJSON(t, h, res.TaskID, task.StateFile)
		if len(bgPerfObjects(st)) == 0 {
			t.Errorf("state.json phải ghi kết quả đỏ của perf:\n%s", bgShortN(raw, 4000))
		}
		if len(bgPerfLogs(t, h, res.TaskID)) == 0 {
			t.Errorf("phải có log perf-N.log")
		}
	})

	t.Run("đối chứng lệnh rỗng", func(t *testing.T) {
		h, res, err := bgPerfRun(t, "workflow:\n  perf:\n    command: \"\"\n", bgWriteFn("slow.txt", "0.6\n"), bgWriteFn("slow.txt", "0\n"))
		if err != nil {
			t.Fatalf("%v", err)
		}
		if n := bgCount(h, StepFeat); n != 1 || res.Status != task.StatusDone {
			t.Errorf("perf.command rỗng = tắt: một b-feat rồi done, được %d b-feat, %s %q", n, res.Status, res.Reason)
		}
		if logs := bgPerfLogs(t, h, res.TaskID); len(logs) != 0 {
			t.Errorf("perf tắt thì không đo (không có perf-N.log), có %v", logs)
		}
	})

	t.Run("validate", func(t *testing.T) {
		cfg := config.Default()
		if err := config.Decode(strings.NewReader("workflow:\n  perf:\n    max_slowdown: 0.9\n"), &cfg); err != nil {
			t.Fatalf("khoá workflow.perf.max_slowdown phải có trong struct: %v", err)
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "max_slowdown") {
			t.Errorf("Validate phải từ chối workflow.perf.max_slowdown = 0.9 (nêu tên khoá), được %v", err)
		}
		cfg = config.Default()
		if err := config.Decode(strings.NewReader(bgPerfYAML), &cfg); err != nil {
			t.Fatal(err)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("perf.command + max_slowdown 1.5 hợp lệ, Validate lỗi: %v", err)
		}
	})
}

func bgShortN(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// bgCheckTemplate: mẫu Render và Default() có ba khoá mới với giá trị 0 / "" / 1.5.
func bgCheckTemplate(t *testing.T) {
	var tmpl map[string]any
	rendered := config.Render(config.InitParams{ProjectName: "p"})
	if err := yaml.Unmarshal([]byte(rendered), &tmpl); err != nil {
		t.Fatalf("mẫu Render không phải YAML: %v", err)
	}
	if strings.Contains(rendered, "%!") {
		t.Errorf("mẫu Render có ký tự %% trần (fmt sinh %%!…)")
	}
	db, err := yaml.Marshal(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]any
	if err := yaml.Unmarshal(db, &def); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]map[string]any{"mẫu Render": tmpl, "Default()": def} {
		if v, ok := bgYAMLPath(src, "workflow", "max_diff_lines"); !ok {
			t.Errorf("%s thiếu workflow.max_diff_lines", name)
		} else if n, ok := bgNum(v); !ok || n != 0 {
			t.Errorf("%s: workflow.max_diff_lines = %v, muốn 0", name, v)
		}
		if v, ok := bgYAMLPath(src, "workflow", "perf", "command"); !ok {
			t.Errorf("%s thiếu workflow.perf.command", name)
		} else if s, ok := v.(string); !ok || s != "" {
			t.Errorf("%s: workflow.perf.command = %v, muốn rỗng", name, v)
		}
		if v, ok := bgYAMLPath(src, "workflow", "perf", "max_slowdown"); !ok {
			t.Errorf("%s thiếu workflow.perf.max_slowdown", name)
		} else if n, ok := bgNum(v); !ok || n != 1.5 {
			t.Errorf("%s: workflow.perf.max_slowdown = %v, muốn 1.5", name, v)
		}
	}
}

func TestGateEvidenceAndBudget(t *testing.T) {
	t.Run("review_evidence", bgCheckReview)
	t.Run("diff_budget", bgCheckDiff)
	t.Run("perf_gate", bgCheckPerf)
	t.Run("config_template", bgCheckTemplate)
}
