package app

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/tui/jobs"
	"bint/internal/workflow"
)

// finishJob đăng ký một việc nền rồi cho nó kết thúc với (value, err) qua đúng đường onJobDone.
func finishJob(t *testing.T, m *Model, value any, err error) {
	t.Helper()
	j := m.jobs.Add(jobs.Job{Action: "test.job", Label: "việc thử"})
	m.onJobDone(jobs.DoneMsg{Job: j.ID, Value: value, Err: err})
}

// Review 6b .2/.8: toast kết thúc việc nền mang cờ lỗi đúng theo kết quả — lỗi hoặc dừng ở blocked/failed/interrupted là lỗi, thành công
// là toast thường kể cả ngay sau một toast lỗi (cờ cũ không được dùng lại).
func TestBackgroundJobDoneToastErrFlag(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		err     error
		wantErr bool
		wantTxt string
	}{
		{"lỗi", nil, errors.New("hỏng rồi"), true, "lỗi"},
		{"blocked", &workflow.Result{Status: "blocked"}, nil, true, "blocked"},
		{"failed", &workflow.Result{Status: "failed"}, nil, true, "failed"},
		{"interrupted", &workflow.Result{Status: "interrupted"}, nil, true, "interrupted"},
		{"xong có trạng thái", &workflow.Result{Status: "in_review"}, nil, false, "in_review"},
		{"xong không giá trị", nil, nil, false, "xong"},
	}
	for _, c := range cases {
		for _, prior := range []bool{false, true} { // cờ cũ (từ một toast lỗi trước đó) không được ảnh hưởng
			m := mouseModel(t, 140, 40)
			if prior {
				m.setToast("bị khoá", true)
			}
			finishJob(t, m, c.value, c.err)
			if m.toastErr != c.wantErr {
				t.Errorf("%s (cờ cũ=%v): toastErr=%v, muốn %v (toast %q)", c.name, prior, m.toastErr, c.wantErr, m.toast)
			}
			if !strings.Contains(m.toast, c.wantTxt) {
				t.Errorf("%s: toast %q thiếu %q", c.name, m.toast, c.wantTxt)
			}
			// Mono: lỗi có dấu ✗, thông báo thường thì không.
			label := plain(m.toastLabel())
			if has := strings.Contains(label, "✗"); has != c.wantErr {
				t.Errorf("%s: nhãn toast %q có ✗=%v, muốn %v", c.name, label, has, c.wantErr)
			}
			// Hộp Thông báo ghi đúng loại.
			m.harvestNotices()
			if n := m.notes.hist[len(m.notes.hist)-1]; n.err != c.wantErr || !strings.Contains(n.text, c.wantTxt) {
				t.Errorf("%s: lịch sử thông báo %+v, muốn err=%v", c.name, n, c.wantErr)
			}
		}
	}
}

// Việc nền thành công ngay sau một toast lỗi do một việc nền khác: toast thường (không thừa hưởng cờ lỗi).
func TestBackgroundJobDoneSuccessAfterErrorToast(t *testing.T) {
	m := mouseModel(t, 140, 40)
	finishJob(t, m, nil, errors.New("hỏng rồi"))
	if !m.toastErr {
		t.Fatal("việc lỗi phải cho toast lỗi")
	}
	finishJob(t, m, nil, nil)
	if m.toastErr {
		t.Fatalf("việc thành công ngay sau toast lỗi phải là toast thường: %q", m.toast)
	}
	if strings.Contains(plain(m.toastLabel()), "✗") {
		t.Fatalf("toast thường không có ✗: %q", plain(m.toastLabel()))
	}
}

// exit.stopping là toast thường dù trước đó cờ lỗi đang bật; xoá toast cũng xoá cờ.
func TestExitStoppingToastIsPlainAndClearResetsFlag(t *testing.T) {
	m := mouseModel(t, 140, 40)
	m.jobs.Add(jobs.Job{Action: "test.job", Label: "việc thử"})
	press(t, m, "q")
	if m.exitDlg == nil {
		t.Fatal("hộp thoát phải mở khi còn việc nền")
	}
	m.setToast("bị khoá", true)
	m.exitKey(keyMsg("enter"))
	if m.toastErr || m.toast == "" {
		t.Fatalf("exit.stopping phải là toast thường: %q err=%v", m.toast, m.toastErr)
	}
	m.setToast("lỗi", true)
	press(t, m, "1") // bấm phím khác xoá toast
	if m.toast != "" || m.toastErr {
		t.Fatalf("xoá toast phải xoá cả cờ lỗi: %q err=%v", m.toast, m.toastErr)
	}
}

// toastAssignments liệt kê vị trí các phép gán vào trường toast/toastErr của một nguồn Go ngoài hàm setToast.
func toastAssignments(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name == "setToast" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, l := range as.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && (sel.Sel.Name == "toast" || sel.Sel.Name == "toastErr") {
					bad = append(bad, fset.Position(as.Pos()).String()+" trong "+fn.Name.Name)
					break
				}
			}
			return true
		})
	}
	return bad
}

// Mọi chỗ gán toast của mô hình gốc đi qua setToast (văn bản + cờ lỗi cùng lúc); không chỗ nào gán m.toast/m.toastErr riêng.
func TestToastOnlyAssignedInSetToast(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("không đọc được nguồn của gói: %v", err)
	}
	scanned := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		scanned[f] = true
		if bad := toastAssignments(t, f, string(b)); len(bad) > 0 {
			t.Errorf("gán toast ngoài setToast: %s", strings.Join(bad, "; "))
		}
	}
	if !scanned["app.go"] || !scanned["jobs.go"] || !scanned["mouse.go"] {
		t.Fatalf("bộ quét bỏ sót tệp nguồn của gói: %v", scanned)
	}
	// Ca ngược: bộ quét phải bắt được phép gán trực tiếp.
	bad := toastAssignments(t, "x.go", "package app\nfunc (m *Model) f() { m.toast = \"x\" }\nfunc (m *Model) g() { m.toast, m.toastErr = \"x\", true }\nfunc (m *Model) setToast(a string, e bool) { m.toast, m.toastErr = a, e }\n")
	if len(bad) != 2 {
		t.Fatalf("bộ quét phải bắt đúng 2 phép gán ngoài setToast, có %v", bad)
	}
}
