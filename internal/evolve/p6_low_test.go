package evolve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// ---- NT-P6-06 .4: guardReadOnly tính gốc repo một lần ----

func TestGuardReadOnlyComputesToplevelOnce(t *testing.T) {
	f := setup(t)
	old := repoToplevelFn
	t.Cleanup(func() { repoToplevelFn = old })
	calls := 0
	repoToplevelFn = func(ctx context.Context, dir string) (string, error) {
		calls++
		return old(ctx, dir)
	}
	if err := guardReadOnly(context.Background(), f.root, func() {}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("repoToplevel được gọi %d lần mỗi lượt, cần đúng 1", calls)
	}
}

// ---- NT-P6-06 .5: sửa bên trong submodule bị phát hiện ----

func TestGuardReadOnlyDetectsSubmoduleChange(t *testing.T) {
	f := setup(t)
	subSrc := testutil.NewRepo(t, map[string]string{"lib.txt": "v0\n"})
	testutil.Git(t, f.root, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subSrc, "libsub")
	testutil.Git(t, f.root, "commit", "-q", "-m", "add submodule")
	libFile := filepath.Join(f.root, "libsub", "lib.txt")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(libFile, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("submodule không đổi", func(t *testing.T) {
		if err := guardReadOnly(context.Background(), f.root, func() {}); err != nil {
			t.Fatalf("không được lỗi: %v", err)
		}
	})
	t.Run("sửa tệp sạch trong submodule", func(t *testing.T) {
		err := guardReadOnly(context.Background(), f.root, func() { write("agent 1\n") })
		if err == nil || !strings.Contains(err.Error(), "sửa cây làm việc") {
			t.Fatalf("phải phát hiện sửa trong submodule, được %v", err)
		}
	})
	t.Run("sửa thêm tệp vốn đã bẩn trong submodule", func(t *testing.T) {
		write("bẩn sẵn\n") // submodule đã bẩn trước khi agent chạy
		if err := guardReadOnly(context.Background(), f.root, func() {}); err != nil {
			t.Fatalf("bẩn sẵn nhưng không đổi thì không được lỗi: %v", err)
		}
		err := guardReadOnly(context.Background(), f.root, func() { write("agent sửa tiếp\n") })
		if err == nil || !strings.Contains(err.Error(), "sửa cây làm việc") {
			t.Fatalf("phải phát hiện sửa tiếp tệp đã bẩn trong submodule, được %v", err)
		}
	})
}

// ---- NT-P6-06 .6: gộp skill giữ nguyên văn chi tiết ----

func TestMergeDraftsKeepsCodeAndListVerbatim(t *testing.T) {
	a := &SkillDraft{Name: "a", Description: "Dùng khi sửa Retry.", Body: "Gọi Reset trước Retry.\n\n```go\nx.Reset()\n```"}
	b := &SkillDraft{Name: "b", Description: "Dùng khi sửa Retry.", Body: "```go\nif err != nil {\n\treturn err\n}\n```\n\n- (tuỳ chọn) ghi log\n- Không bỏ qua lỗi Reset."}
	got := MergeDrafts(a, b)
	for _, want := range []string{"```go\nif err != nil {\n\treturn err\n}\n```", "- (tuỳ chọn) ghi log", "- Không bỏ qua lỗi Reset."} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("mất chi tiết %q:\n%s", want, got.Body)
		}
	}
	if !strings.HasPrefix(got.Body, strings.TrimSpace(a.Body)) {
		t.Fatalf("thân skill giữ lại phải còn nguyên:\n%s", got.Body)
	}
	if again := MergeDrafts(got, b); again.Body != got.Body {
		t.Fatalf("gọi lại không được nối thêm:\n%s\n---\n%s", got.Body, again.Body)
	}
	// Mô tả riêng của b cũng chỉ vào thân một lần.
	b2 := &SkillDraft{Name: "b", Description: "Dùng khi sửa Reset.", Body: "- bước hai"}
	m := MergeDrafts(a, b2)
	if strings.Count(m.Body, "Dùng khi: Dùng khi sửa Reset.") != 1 {
		t.Fatalf("mô tả của b phải vào thân đúng một lần:\n%s", m.Body)
	}
	if again := MergeDrafts(m, b2); again.Body != m.Body {
		t.Fatalf("gọi lại với mô tả khác không được nối thêm:\n%s\n---\n%s", m.Body, again.Body)
	}
}
