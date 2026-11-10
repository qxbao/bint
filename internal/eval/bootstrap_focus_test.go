package eval

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"bint/internal/skills"
	"bint/internal/testutil"
)

func TestHotAreasAndPrompt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("cần git")
	}
	root := testutil.NewRepo(t, map[string]string{"ci/build.yml": "a\n", "pkg/core/x.py": "a\n"})
	for i, v := range []string{"b", "c", "d"} {
		commit(t, root, "sửa core "+v, map[string]string{
			"pkg/core/x.py":       strings.Repeat(v, i+1) + "\n",
			"pkg/core/tests/t.py": v + "\n",
		})
	}
	commit(t, root, "sửa ci", map[string]string{"ci/build.yml": "z\n"})
	got := hotAreas(context.Background(), root, 300, 2)
	if len(got) != 2 || got[0] != "pkg/core" || got[1] != "ci" {
		t.Fatalf("vùng hoạt động: %v", got)
	}
	s, ok := skills.Get("b-bootstrap")
	if !ok {
		t.Skip("thiếu skill b-bootstrap")
	}
	if p := bootstrapPrompt(s, got); !strings.Contains(p, "- pkg/core\n") || !strings.Contains(p, "Ưu tiên") {
		t.Fatalf("đề bài thiếu gợi ý vùng:\n%s", p[:300])
	}
	if p := bootstrapPrompt(s, nil); strings.Contains(p, "Ưu tiên") {
		t.Fatal("không có vùng thì không thêm gợi ý")
	}
}
