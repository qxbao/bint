package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetUILanguageTable(t *testing.T) {
	cases := []struct {
		name, in, lang, want string
		changed              bool
	}{
		{"đổi giá trị, giữ chú thích",
			"# đầu tệp\nversion: 1\nui:\n  language: auto            # giao diện\n  other: x\n", "vi",
			"# đầu tệp\nversion: 1\nui:\n  language: vi            # giao diện\n  other: x\n", true},
		{"đã đúng thì không đổi", "ui:\n  language: vi\n", "vi", "ui:\n  language: vi\n", false},
		{"giá trị trong dấu nháy", "ui:\n  language: \"auto\" # c\n", "en", "ui:\n  language: en # c\n", true},
		{"thiếu khoá language trong khối ui", "ui:\n  foo: 1\n\nlimits:\n  a: 1\n", "vi", "ui:\n  foo: 1\n  language: vi\n\nlimits:\n  a: 1\n", true},
		{"thiếu khối ui, tệp có nội dung", "version: 1\nproject:\n  name: x\n", "vi",
			"version: 1\nproject:\n  name: x\n\nui:\n  language: vi            " + uiBlockComment + "\n", true},
		{"thiếu khối ui, thiếu dòng cuối", "version: 1", "en",
			"version: 1\n\nui:\n  language: en            " + uiBlockComment + "\n", true},
		{"tệp chưa có", "", "vi", "ui:\n  language: vi            " + uiBlockComment + "\n", true},
		{"ui dạng flow rỗng", "ui: {}\nx: 1\n", "vi", "ui: {language: vi}\nx: 1\n", true},
		{"ui dạng flow có khoá khác", "ui: {a: 1}\n", "en", "ui: {language: en, a: 1}\n", true},
		{"ui dạng flow có language", "ui: {language: auto}\n", "vi", "ui: {language: vi}\n", true},
		{"ui null", "ui:\nx: 1\n", "vi", "ui:\n  language: vi\nx: 1\n", true},
		{"CRLF", "version: 1\r\nui:\r\n  language: auto\r\nx: 1\r\n", "vi", "version: 1\r\nui:\r\n  language: vi\r\nx: 1\r\n", true},
		{"CRLF thiếu khối ui", "version: 1\r\n", "vi", "version: 1\r\n\r\nui:\r\n  language: vi            " + uiBlockComment + "\r\n", true},
		{"tiếng Việt trước khoá", "# Cấu hình dự án — tiếng Việt\nui:\n  language: auto\n", "vi", "# Cấu hình dự án — tiếng Việt\nui:\n  language: vi\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed, err := SetUILanguage([]byte(c.in), c.lang)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want || changed != c.changed {
				t.Fatalf("changed=%v (muốn %v)\n--- got\n%q\n--- want\n%q", changed, c.changed, got, c.want)
			}
			// Kết quả phải vẫn là YAML hợp lệ với ui.language đúng.
			var back struct {
				UI struct {
					Language string `yaml:"language"`
				} `yaml:"ui"`
			}
			if err := yaml.Unmarshal(got, &back); err != nil {
				t.Fatalf("kết quả không giải mã được: %v", err)
			}
			if back.UI.Language != c.lang {
				t.Fatalf("ui.language = %q, muốn %q", back.UI.Language, c.lang)
			}
		})
	}
}

func TestSetUILanguageErrors(t *testing.T) {
	if _, _, err := SetUILanguage([]byte("ui: {}\n"), "fr"); err == nil {
		t.Fatal("ngôn ngữ lạ phải bị từ chối")
	}
	if _, _, err := SetUILanguage([]byte("ui: [1, 2]\n"), "vi"); err == nil {
		t.Fatal("ui không phải khối phải lỗi")
	}
	if _, _, err := SetUILanguage([]byte("- a\n- b\n"), "vi"); err == nil {
		t.Fatal("tệp không phải ánh xạ phải lỗi")
	}
	if _, _, err := SetUILanguage([]byte("a: [\n"), "vi"); err == nil {
		t.Fatal("YAML hỏng phải lỗi")
	}
}

// writeLayer dựng repository giả có .bint/config.yml (mẫu mặc định) và tuỳ chọn local.yml.
func writeLayer(t *testing.T, project, local string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("BINT_CONFIG_HOME", filepath.Join(root, "no-global"))
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectPath(root), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	if local != "" {
		if err := os.WriteFile(LocalPath(root), []byte(local), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestValidateWith(t *testing.T) {
	good := Render(InitParams{})
	root := writeLayer(t, good, "")

	if _, err := ValidateWith(root, LayerProject, []byte(good)); err != nil {
		t.Fatalf("mẫu mặc định phải hợp lệ: %v", err)
	}
	// Giá trị sai ở lớp dự án.
	if _, err := ValidateWith(root, LayerProject, []byte(good+"\nui:\n  language: fr\n")); err == nil {
		// ui đã có trong mẫu nên khoá trùng cũng là lỗi: dùng cách thay dòng cho chắc chắn.
		t.Fatal("khoá trùng/giá trị sai phải lỗi")
	}
	bad, _, _ := SetUILanguage([]byte(good), "auto")
	bad = []byte(strings.Replace(string(bad), "language: auto", "language: fr", 1))
	if _, err := ValidateWith(root, LayerProject, bad); err == nil || !strings.Contains(err.Error(), "language") {
		t.Fatalf("ui.language sai ở lớp dự án phải lỗi: %v", err)
	}
	// Khoá lạ bị từ chối (KnownFields).
	if _, err := ValidateWith(root, LayerLocal, []byte("khong_co_khoa_nay: 1\n")); err == nil {
		t.Fatal("khoá lạ ở lớp cục bộ phải lỗi")
	}
	// Lớp cục bộ đè giá trị sai của lớp dự án: nội dung mới của lớp dự án sai nhưng local.yml sửa lại thì hợp lệ.
	rootL := writeLayer(t, good, "ui:\n  language: vi\n")
	if _, err := ValidateWith(rootL, LayerProject, bad); err != nil {
		t.Fatalf("lớp cục bộ đè giá trị sai thì cấu hình ghép phải hợp lệ: %v", err)
	}
	// Ngược lại: nội dung mới của lớp cục bộ có giá trị sai.
	if _, err := ValidateWith(rootL, LayerLocal, []byte("ui:\n  language: de\n")); err == nil {
		t.Fatal("giá trị sai ở lớp cục bộ phải lỗi")
	}
	// Không ghi gì ra đĩa.
	if b, _ := os.ReadFile(ProjectPath(rootL)); string(b) != good {
		t.Fatal("ValidateWith không được ghi tệp")
	}
	if _, err := ValidateWith(root, Layer("global"), nil); err == nil {
		t.Fatal("lớp toàn cục không sửa được")
	}
}
