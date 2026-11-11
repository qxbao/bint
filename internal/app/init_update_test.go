package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/config"
	"bint/internal/testutil"
)

// Cấu hình kiểu cũ (chưa có models theo role): init thường chỉ báo, init --update
// thêm khoá thiếu, giữ giá trị người dùng, sao lưu bản cũ; chạy lại không đổi gì.
func TestInitUpdateUpgradesOldConfig(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	mustInit(t, InitOptions{Dir: root, Harnesses: []string{"claude"}, Hooks: no(), NoSkills: true})
	cfgPath := config.ProjectPath(root)

	// Biến tệp vừa tạo thành kiểu cũ: bỏ khối harnesses.claude, sửa một giá trị.
	cur := read(t, cfgPath)
	i := strings.Index(cur, "  # models:")
	j := strings.Index(cur, "  codex:")
	if i < 0 || j < i {
		t.Fatalf("mẫu không có khối models của claude:\n%s", cur)
	}
	old := strings.Replace(cur[:i]+cur[j:], "max_review_rounds: 2", "max_review_rounds: 3", 1)
	if err := os.WriteFile(cfgPath, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true})
	if s := lastStep(rep, ".bint/config.yml"); s.Status != StepSkipped || !strings.Contains(s.Detail, "--update") {
		t.Fatalf("init thường phải gợi ý --update: %+v", s)
	}
	if read(t, cfgPath) != old {
		t.Fatal("init thường không được sửa cấu hình")
	}

	rep = mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true, Update: true})
	s := lastStep(rep, ".bint/config.yml")
	if s.Status != StepUpdated || !strings.Contains(s.Detail, "harnesses.claude") {
		t.Fatalf("bước cấu hình: %+v", s)
	}
	got := read(t, cfgPath)
	if !strings.Contains(got, "max_review_rounds: 3") || !strings.Contains(got, `implementer:  "sonnet"`) {
		t.Fatalf("cấu hình sau khi nâng:\n%s", got)
	}
	// Bản cũ được sao lưu, đúng nội dung.
	bak := s.Detail[strings.Index(s.Detail, "bản cũ: ")+len("bản cũ: ") : len(s.Detail)-1]
	if b, err := os.ReadFile(bak); err != nil || string(b) != old || !strings.HasPrefix(bak, rep.StateDir) {
		t.Fatalf("sao lưu %s: %v", bak, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".bint", "config.yml.bak")); err == nil {
		t.Fatal("không được để tệp sao lưu trong repository")
	}

	rep = mustInit(t, InitOptions{Dir: root, Hooks: no(), NoSkills: true, Update: true})
	if s := lastStep(rep, ".bint/config.yml"); s.Status != StepOK || read(t, cfgPath) != got {
		t.Fatalf("chạy lại phải không đổi: %+v", s)
	}
}

// lastStep trả về dòng báo cáo cuối cùng có tên name.
func lastStep(rep *InitReport, name string) Step {
	var out Step
	for _, s := range rep.Steps {
		if s.Name == name {
			out = s
		}
	}
	return out
}
