package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// TestExperienceEndToEnd: `bint experience show` liệt kê bài học (chung trước, riêng theo vùng)
// và `digest` báo lỗi rõ khi tham chiếu không phải đợt đánh giá hay lượt chạy.
func TestExperienceEndToEnd(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":             "module demo\n",
		"internal/core/x.go": "package core\n\nfunc X() {}\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "add", "--type", "lesson", "--title", "Khi sửa core nhớ gọi Reset",
		"--paths", "internal/core/**", "Luôn gọi Reset trước khi kết thúc."); code != 0 {
		t.Fatalf("knowledge add: %d\n%s", code, out)
	}
	if out, code := run(t, root, "", "knowledge", "add", "--type", "lesson", "--title", "Chạy test trước khi kết thúc",
		"Luôn chạy test của vùng đã sửa."); code != 0 {
		t.Fatalf("knowledge add chung: %d\n%s", code, out)
	}

	out, code := run(t, root, "", "experience", "show", "--json")
	var rows []struct {
		Title    string `json:"title"`
		Area     string `json:"area"`
		Inserted int    `json:"inserted"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
		t.Fatalf("experience show --json: %d\n%s", code, out)
	}
	if rows[0].Area != "" || rows[1].Area != "internal/core" || rows[0].Inserted != 0 {
		t.Fatalf("thứ tự chung→riêng sai: %+v", rows)
	}

	out, code = run(t, root, "", "experience", "show")
	if code != 0 || !strings.Contains(out, "Chung (mọi vùng mã)") || !strings.Contains(out, "Riêng — internal/core") {
		t.Fatalf("experience show text: %d\n%s", code, out)
	}

	out, code = run(t, root, "", "experience", "digest", "không-có-đợt")
	if code == 0 || !strings.Contains(out, "không tìm thấy đợt đánh giá hoặc lượt chạy") {
		t.Fatalf("digest tham chiếu lạ phải báo lỗi rõ: %d\n%s", code, out)
	}
}
