package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

const e2eLine = "\t\t\tif cfg.Security.AllowExternalAgents && !cfg.Security.AllowNetworkInEvals {"

func e2eFiller(n int, tag string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "// %s dòng đệm số %d\n", tag, i)
	}
	return sb.String()
}

// evidenceRepo dựng repository có mục warning (agent ghi) với bằng chứng internal/cli/eval.go:11.
func evidenceRepo(t *testing.T) (root, id string) {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	root = testutil.NewRepo(t, map[string]string{
		"go.mod":               "module demo\n",
		"internal/cli/eval.go": e2eFiller(10, "cli") + e2eLine + "\n" + e2eFiller(5, "cli2"),
		"internal/app/doc.go":  "package app\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	asAgent(t)
	out, code := run(t, root, "Cờ security không thay thế cô lập agent.\n", "knowledge", "add", "--type", "warning", "--title", "Cờ security",
		"--paths", "internal/**", "--evidence", "internal/cli/eval.go:11", "--input", "internal/cli/eval.go", "--third-party", "--json", "-")
	var added struct {
		Saved bool   `json:"saved"`
		ID    string `json:"id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || !added.Saved {
		t.Fatalf("agent add: %d\n%s", code, out)
	}
	asHuman(t)
	return root, added.ID
}

func e2eWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApproveEvidenceCasesCLIWording(t *testing.T) {
	file := ".bint/knowledge/items/%s.md"
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
		want   func(id string) []string // chuỗi phải có
		not    []string                 // chuỗi không được có
	}{
		{"chuyển tệp", func(t *testing.T, root string) {
			e2eWrite(t, root, "internal/cli/eval.go", e2eFiller(4, "cli"))
			e2eWrite(t, root, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+e2eLine+"\n}\n")
		}, func(id string) []string {
			return []string{"internal/cli/eval.go:11", "đã chuyển sang internal/app/eval_run.go:4", "`bint knowledge verify --apply " + id + "`"}
		}, []string{"--refresh", "bằng chứng tham chiếu dòng"}},
		{"tệp ngắn đi", func(t *testing.T, root string) {
			e2eWrite(t, root, "internal/cli/eval.go", e2eFiller(4, "cli"))
		}, func(id string) []string {
			return []string{"chỉ còn 4 dòng", "vùng 11", "sửa bằng chứng trong " + fmt.Sprintf(file, id), "`bint knowledge reject " + id}
		}, []string{"--refresh", "bằng chứng tham chiếu dòng"}},
		{"tệp bị xoá", func(t *testing.T, root string) {
			os.Remove(filepath.Join(root, "internal/cli/eval.go"))
		}, func(id string) []string {
			return []string{"không còn tệp internal/cli/eval.go", "sửa bằng chứng trong " + fmt.Sprintf(file, id), "`bint knowledge reject " + id}
		}, []string{"--refresh"}},
		{"nội dung đổi", func(t *testing.T, root string) {
			e2eWrite(t, root, "internal/cli/eval.go", e2eFiller(10, "cli")+"\t\t\tif cfg.Khac() {\n"+e2eFiller(5, "cli2"))
		}, func(id string) []string {
			return []string{"đã thay đổi kể từ khi ghi", "`bint knowledge show " + id + "`", "`bint knowledge approve --refresh " + id + "`"}
		}, []string{"verify --apply"}},
		{"nhiều nơi khớp", func(t *testing.T, root string) {
			e2eWrite(t, root, "internal/cli/eval.go", e2eFiller(4, "cli"))
			e2eWrite(t, root, "internal/app/a.go", "package app\n"+e2eLine+"\n")
			e2eWrite(t, root, "internal/app/b.go", "package app\n"+e2eLine+"\n")
		}, func(id string) []string {
			return []string{"có ở 2 nơi", "internal/app/a.go:2", "internal/app/b.go:2", "sửa bằng chứng trong " + fmt.Sprintf(file, id),
				"tệp internal/cli/eval.go chỉ còn 4 dòng"}
		}, []string{"--refresh", "verify --apply"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, id := evidenceRepo(t)
			c.mutate(t, root)
			out, code := run(t, root, "", "knowledge", "approve", id)
			if code == 0 {
				t.Fatalf("approve phải lỗi:\n%s", out)
			}
			for _, w := range c.want(id) {
				if !strings.Contains(out, w) {
					t.Errorf("thiếu %q trong:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("không được có %q trong:\n%s", n, out)
				}
			}
			// show và verify dựng cùng câu và cùng gợi ý.
			for _, args := range [][]string{{"knowledge", "show", id}, {"knowledge", "verify", id}} {
				o2, _ := run(t, root, "", args...)
				for _, w := range c.want(id)[:2] {
					if !strings.Contains(o2, w) {
						t.Errorf("%v: thiếu %q trong:\n%s", args, w, o2)
					}
				}
				for _, n := range c.not {
					if strings.Contains(o2, n) {
						t.Errorf("%v: không được có %q trong:\n%s", args, n, o2)
					}
				}
			}
		})
	}
}

// Ca thật: sau verify --apply mục trỏ đúng path/lines mới và approve (không --refresh) thành công.
func TestVerifyApplyMovedThenApproveCLI(t *testing.T) {
	root, id := evidenceRepo(t)
	e2eWrite(t, root, "internal/cli/eval.go", e2eFiller(4, "cli"))
	e2eWrite(t, root, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+e2eLine+"\n}\n")
	out, code := run(t, root, "", "knowledge", "verify", "--apply", id)
	if code != 0 {
		t.Fatalf("verify --apply áp xong phải thoát 0, được %d:\n%s", code, out)
	}
	for _, w := range []string{"⇄ trong đó đổi đường dẫn vì khối chuyển tệp: " + id, "↕ cập nhật vị trí bằng chứng"} {
		if !strings.Contains(out, w) {
			t.Fatalf("thiếu %q:\n%s", w, out)
		}
	}
	for _, n := range []string{"✗", "verify --apply " + id, "đã chuyển sang"} {
		if strings.Contains(out, n) {
			t.Fatalf("sau khi áp không được có %q:\n%s", n, out)
		}
	}
	show, code := run(t, root, "", "knowledge", "show", id, "--json")
	if code != 0 || !strings.Contains(show, `"path": "internal/app/eval_run.go"`) && !strings.Contains(show, `"path":"internal/app/eval_run.go"`) {
		t.Fatalf("show:\n%s", show)
	}
	if out, code := run(t, root, "", "knowledge", "approve", id); code != 0 || !strings.Contains(out, "candidate → verified") {
		t.Fatalf("approve: %d\n%s", code, out)
	}
}

// Trợ giúp của verify nói --apply cũng đổi đường dẫn khi khối chuyển tệp (phát hiện .7).
func TestVerifyHelpMentionsMovedApply(t *testing.T) {
	root, _ := evidenceRepo(t)
	out, code := run(t, root, "", "knowledge", "verify", "--help")
	if code != 0 {
		t.Fatalf("%d\n%s", code, out)
	}
	for _, w := range []string{"chuyển tệp", "cập nhật path và", "trạng thái giữ nguyên", "cả path khi khối chuyển tệp"} {
		if !strings.Contains(out, w) {
			t.Errorf("trợ giúp thiếu %q:\n%s", w, out)
		}
	}
}
