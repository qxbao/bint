package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeTracked(t *testing.T, files []string, err error) {
	t.Helper()
	old := trackedFiles
	trackedFiles = func(context.Context, string) ([]string, error) { return files, err }
	t.Cleanup(func() { trackedFiles = old })
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBaselineScopeMatchAndDeny(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeFiles(t, root, map[string]string{"a/x.go": "12345", "a/y.go": "123", "a/gen/z.go": "1234567", "b/w.txt": "1"})
	fakeTracked(t, []string{"a/x.go", "a/y.go", "a/gen/z.go", "b/w.txt", ""}, nil)
	out := buildBaseline(context.Background(), state, root, []string{"a/**", "b/*.txt"}, []string{"a/gen/**"}, time.Minute, 1000)
	for _, want := range []string{"- `a/**`: 2 tệp, 8 byte\n", "- `b/*.txt`: 1 tệp, 1 byte\n", "scope.deny"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "đạt trần") {
		t.Errorf("không được báo trần:\n%s", out)
	}
	// Không deny: tệp gen được tính.
	out = buildBaseline(context.Background(), state, root, []string{"a/**"}, nil, time.Minute, 1000)
	if !strings.Contains(out, "- `a/**`: 3 tệp, 15 byte\n") {
		t.Errorf("không deny phải đếm 3 tệp:\n%s", out)
	}
}

func TestBaselineStateDirCapDoesNotStarveScope(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeFiles(t, state, map[string]string{"big/1": "a", "big/2": "a", "big/3": "a", "top.txt": "zz"})
	writeFiles(t, root, map[string]string{"a/x.go": "12345"})
	fakeTracked(t, []string{"a/x.go"}, nil)
	// Trần số tệp 1: thư mục trạng thái bị chặn nhưng scope.allow vẫn đo đủ, không có ghi chú trần.
	out := buildBaseline(context.Background(), state, root, []string{"a/**"}, nil, time.Minute, 1)
	if !strings.Contains(out, "`big/`") || !strings.Contains(out, "đạt trần") {
		t.Errorf("thư mục trạng thái phải đạt trần:\n%s", out)
	}
	if !strings.Contains(out, "- `a/**`: 1 tệp, 5 byte\n") {
		t.Errorf("scope.allow phải đo đủ:\n%s", out)
	}
	if !strings.Contains(out, "- `top.txt`: 1 tệp, 2 byte") {
		t.Errorf("tệp cấp một phải được liệt kê:\n%s", out)
	}
}

func TestBaselineTimeBudget(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeFiles(t, state, map[string]string{"d/1": "a"})
	writeFiles(t, root, map[string]string{"a/x.go": "1"})
	fakeTracked(t, []string{"a/x.go"}, nil)
	out := buildBaseline(context.Background(), state, root, []string{"a/**"}, nil, -time.Second, 1000)
	if strings.Count(out, "đạt trần") != 2 || !strings.Contains(out, "- `a/**`: 0 tệp, 0 byte (đạt trần") {
		t.Errorf("hết hạn phải ghi trần cho cả hai phép đo:\n%s", out)
	}
}

func TestBaselineReadErrors(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "không-có")
	fakeTracked(t, nil, errors.New("git hỏng"))
	out := buildBaseline(context.Background(), missing, root, []string{"a/**"}, nil, time.Minute, 1000)
	if strings.Count(out, "Không đo được:") != 2 || !strings.Contains(out, "git hỏng") {
		t.Errorf("phải ghi lỗi của cả hai phép đo:\n%s", out)
	}
	fakeTracked(t, []string{"a", "b"}, nil)
	out = buildBaseline(context.Background(), t.TempDir(), root, nil, nil, time.Minute, 1000)
	if !strings.Contains(out, "chưa có scope.allow; toàn kho có 2 tệp") {
		t.Errorf("thiếu allow:\n%s", out)
	}
}
