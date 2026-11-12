package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	bintapp "bint/internal/app"
	"bint/internal/knowledge"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// Bằng chứng lệch có cấu trúc đi hết đường: dịch vụ thật trên repository tạm (khối 1 dòng chuyển tệp, tệp ngắn đi, tệp bị xoá, nội dung đổi,
// khớp nhiều nơi) → khung Detail, hộp kết quả xác minh, toast và hộp Thông báo của TUI, ở vi lẫn en (docs/tui-plan.md mục 12.11).

const flowLine = "\t\t\tif cfg.Security.AllowExternalAgents && !cfg.Security.AllowNetworkInEvals {"

func flowFiller(n int, tag string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "// %s filler line %d\n", tag, i)
	}
	return sb.String()
}

var viLetters = regexp.MustCompile(`(?i)[àáạảãăằắặẳẵâầấậẩẫèéẹẻẽêềếệểễìíịỉĩòóọỏõôồốộổỗơờớợởỡùúụủũưừứựửữỳýỵỷỹđ]`)

type flowCase struct {
	name   string
	mutate func(t *testing.T, root string)
	fix    string
	// parts là các phần dữ liệu (không phụ thuộc ngôn ngữ) phải có trên màn hình/hộp.
	parts []string
	// viParts/enParts là phần chữ theo ngôn ngữ.
	viParts, enParts []string
}

func flowCases() []flowCase {
	w := func(t *testing.T, root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return []flowCase{
		{name: "moved", fix: knowledge.FixApply,
			mutate: func(t *testing.T, root string) {
				w(t, root, "internal/cli/eval.go", flowFiller(4, "cli"))
				w(t, root, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+flowLine+"\n}\n")
			},
			parts:   []string{"internal/cli/eval.go:11", "internal/app/eval_run.go:4"},
			viParts: []string{"vị trí cũ", "vị trí mới", "phím v ở Tri thức [ô Áp dụng]"},
			enParts: []string{"old position", "new position", "key v in Knowledge [field"}},
		{name: "shortened", fix: knowledge.FixEdit,
			mutate:  func(t *testing.T, root string) { w(t, root, "internal/cli/eval.go", flowFiller(4, "cli")) },
			parts:   []string{"internal/cli/eval.go:11"},
			viParts: []string{"tệp hiện có 4 dòng", "phím e ở Tri thức", "phím r ở Tri thức"},
			enParts: []string{"the file now has 4 lines", "key e in Knowledge", "key r in Knowledge"}},
		{name: "deleted", fix: knowledge.FixEdit,
			mutate:  func(t *testing.T, root string) { os.Remove(filepath.Join(root, "internal/cli/eval.go")) },
			parts:   []string{"internal/cli/eval.go:11"},
			viParts: []string{"tệp không còn", "phím e ở Tri thức", "phím r ở Tri thức"},
			enParts: []string{"no longer in the repository", "key e in Knowledge", "key r in Knowledge"}},
		{name: "ambiguous", fix: knowledge.FixEdit,
			mutate: func(t *testing.T, root string) {
				w(t, root, "internal/cli/eval.go", flowFiller(4, "cli"))
				w(t, root, "internal/app/a.go", "package app\n"+flowLine+"\n")
				w(t, root, "internal/app/b.go", "package app\n"+flowLine+"\n")
			},
			parts:   []string{"internal/cli/eval.go:11", "internal/app/a.go:2", "internal/app/b.go:2"},
			viParts: []string{"2 ứng viên", "tệp hiện có 4 dòng", "phím e ở Tri thức", "phím r ở Tri thức"},
			enParts: []string{"2 candidates", "the file now has 4 lines", "key e in Knowledge", "key r in Knowledge"}},
		{name: "changed", fix: knowledge.FixRefresh,
			mutate: func(t *testing.T, root string) {
				w(t, root, "internal/cli/eval.go", flowFiller(10, "cli")+"\t\t\tif cfg.Other() {\n"+flowFiller(5, "cli2"))
			},
			parts:   []string{"internal/cli/eval.go:11"},
			viParts: []string{"đã đổi kể từ khi ghi", "phím Enter ở Tri thức", "phím f ở Tri thức"},
			enParts: []string{"changed since it was recorded", "key Enter in Knowledge", "key f in Knowledge"}},
	}
}

// flowRepo dựng repository và mục bằng chứng internal/cli/eval.go:11, rồi áp mutate. Trả thư mục gốc và ID mục.
func flowRepo(t *testing.T, c flowCase) (root, id string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	ctx := context.Background()
	root = testutil.NewRepo(t, map[string]string{
		"internal/cli/eval.go": flowFiller(10, "cli") + flowLine + "\n" + flowFiller(5, "cli2"),
		"internal/app/doc.go":  "package app\n",
	})
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, BintPath: "bint"}); err != nil {
		t.Fatal(err)
	}
	k, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := bintapp.AddKnowledge(ctx, k, bintapp.AddOptions{Type: knowledge.TypeWarning, Title: "Security flags do not isolate agents",
		Body: "A body that is only for the evidence flow test.", Paths: []string{"internal/**"}, Evidence: []string{"internal/cli/eval.go:11"},
		Origin: knowledge.OriginAgent, ThirdParty: true})
	if err != nil || !res.Saved {
		t.Fatalf("%v %+v", err, res.Check.Findings)
	}
	c.mutate(t, root)
	return root, res.Item.ID
}

// flowModel dựng repository (flowRepo) rồi mở màn Tri thức ở ngôn ngữ lang. Trả mô hình, ID mục và các kết quả thật của dịch vụ: lỗi duyệt,
// báo cáo verify (không --apply).
func flowModel(t *testing.T, c flowCase, lang string) (m *Model, id string, outs []bintapp.ReviewOutcome, rep bintapp.VerifyReport) {
	t.Helper()
	ctx := context.Background()
	root, id := flowRepo(t, c)
	k2, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	outs, err = bintapp.ReviewKnowledge(ctx, k2, bintapp.ReviewAction{Refs: []string{id}, To: knowledge.StatusVerified})
	if err != nil || len(outs) != 1 || outs[0].Err == nil {
		t.Fatalf("dịch vụ phải từ chối duyệt: %v %+v", err, outs)
	}
	rep, err = bintapp.VerifyKnowledge(ctx, k2, []string{id}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	m = New(ctx, Options{Dir: root, Version: "t", Lang: lang, Mode: theme.Mono, SpinnerFPS: time.Millisecond})
	m.SetSize(150, 45)
	drive(t, m, m.Init())
	press(t, m, "2")
	return m, id, outs, rep
}

// boxText cắt riêng nội dung hộp thoại có tiêu đề title khỏi bản vẽ (hộp đè lên các khung phía sau nên gộp cả dòng sẽ lẫn chữ của khung khác).
func boxText(t *testing.T, out, title string) string {
	t.Helper()
	var rows []string
	col, width := -1, 0
	for _, l := range strings.Split(plain(out), "\n") {
		r := []rune(l)
		if col < 0 {
			if i := strings.Index(l, "┌─│"+title+"│"); i >= 0 {
				col = len([]rune(l[:i]))
				for j := col; j < len(r); j++ {
					if r[j] == '┐' {
						width = j - col + 1
						break
					}
				}
			}
			continue
		}
		if len(r) < col+width {
			break
		}
		seg := string(r[col : col+width])
		if strings.HasPrefix(seg, "└") {
			break
		}
		rows = append(rows, strings.Trim(seg, "│ "))
	}
	if col < 0 {
		t.Fatalf("không thấy hộp %q:\n%s", title, out)
	}
	return strings.Join(rows, "\n")
}

func checkParts(t *testing.T, what, got string, c flowCase, lang string) {
	t.Helper()
	f := flat(got)
	want := append([]string(nil), c.parts...)
	notKey := "phím f ở"
	if lang == "en" {
		want = append(want, c.enParts...)
		notKey = "key f in"
	} else {
		want = append(want, c.viParts...)
	}
	for _, w := range want {
		if !strings.Contains(f, w) {
			t.Errorf("%s/%s/%s: thiếu %q:\n%s", c.name, lang, what, w, got)
		}
	}
	if c.fix != knowledge.FixRefresh && strings.Contains(f, notKey) {
		t.Errorf("%s/%s/%s: cách sửa %s không được gợi ý phím f:\n%s", c.name, lang, what, c.fix, got)
	}
	if strings.Contains(f, "bint knowledge") || strings.Contains(f, "bint ") && cliWordsRE.MatchString(f) {
		t.Errorf("%s/%s/%s: còn lệnh CLI:\n%s", c.name, lang, what, got)
	}
	if lang == "en" {
		if loc := viLetters.FindString(f); loc != "" {
			t.Errorf("%s/en/%s: có chữ tiếng Việt %q:\n%s", c.name, what, loc, got)
		}
	}
}

func TestEvidenceFlowAllSurfaces(t *testing.T) {
	for _, c := range flowCases() {
		for _, lang := range []string{"vi", "en"} {
			t.Run(c.name+"/"+lang, func(t *testing.T) {
				m, id, outs, rep := flowModel(t, c, lang)

				// Khung Detail của mục.
				if cur := m.kn.Cursor(); cur == nil || cur.ID != id {
					t.Fatalf("không chọn được mục: %+v", cur)
				}
				press(t, m, "enter")
				var detail strings.Builder
				for i := 0; i < 8; i++ {
					detail.WriteString(plain(m.Render()) + "\n")
					press(t, m, "pgdown")
				}
				checkParts(t, "Detail", detail.String(), c, lang)
				press(t, m, "esc")

				// Hộp kết quả xác minh.
				_, cmd := m.Update(actions.ResultMsg{ID: "knowledge.verify", Value: rep})
				drive(t, m, cmd)
				title := map[string]string{"vi": "Kết quả kiểm tra", "en": "Check results"}[lang]
				checkParts(t, "hộp xác minh", boxText(t, m.Render(), title), c, lang)
				press(t, m, "esc")

				// Toast của lỗi duyệt, rồi hộp Thông báo.
				_, cmd = m.Update(actions.ResultMsg{ID: "knowledge.approve", Value: outs})
				drive(t, m, cmd)
				checkParts(t, "toast", m.Render(), c, lang)
				checkParts(t, "hộp Thông báo", readNotices(t, m), c, lang)
			})
		}
	}
}

// Hộp kết quả của verify --apply khi khối đã chuyển tệp và được áp: kết quả là kết quả sau khi áp nên không có ✗, không gợi ý phím cập nhật vị
// trí, và có dòng báo đã cập nhật vị trí/đổi đường dẫn, ở vi lẫn en (phát hiện .1).
func TestVerifyApplyMovedBoxAfterApply(t *testing.T) {
	c := flowCases()[0] // moved
	for _, lang := range []string{"vi", "en"} {
		t.Run(lang, func(t *testing.T) {
			root, id := flowRepo(t, c)
			ctx := context.Background()
			k, err := bintapp.OpenKnowledge(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			rep, err := bintapp.VerifyKnowledge(ctx, k, []string{id}, true, time.Now())
			if err != nil || len(rep.Moved) != 1 || len(rep.Results) != 1 || !rep.Results[0].OK() {
				t.Fatalf("%v %+v", err, rep)
			}
			m := New(ctx, Options{Dir: root, Version: "t", Lang: lang, Mode: theme.Mono, SpinnerFPS: time.Millisecond})
			m.SetSize(150, 45)
			drive(t, m, m.Init())
			press(t, m, "2")
			_, cmd := m.Update(actions.ResultMsg{ID: "knowledge.verify_apply", Value: rep})
			drive(t, m, cmd)
			title := map[string]string{"vi": "Kiểm tra và áp dụng (0 chuyển trạng thái)", "en": "Checked and applied (0 state changes)"}[lang]
			box := boxText(t, m.Render(), title)
			f := flat(box)
			want := map[string][]string{
				"vi": {"Đã cập nhật vị trí bằng chứng", "đã đổi đường dẫn"},
				"en": {"Evidence position updated", "path updated"},
			}[lang]
			for _, w := range want {
				if !strings.Contains(f, w) {
					t.Errorf("%s: thiếu %q:\n%s", lang, w, box)
				}
			}
			for _, n := range []string{"✗", "phím v", "key v", "internal/app/eval_run.go:4 "} {
				if strings.Contains(f, n) {
					t.Errorf("%s: không được có %q:\n%s", lang, n, box)
				}
			}
			if lang == "en" {
				if loc := viLetters.FindString(f); loc != "" {
					t.Errorf("en có chữ tiếng Việt %q:\n%s", loc, box)
				}
			}
		})
	}
}
