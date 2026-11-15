package importer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/knowledge"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noEnv(string) string { return "" }

func TestParseFrom(t *testing.T) {
	cases := []struct {
		from, path, src, p string
		err                bool
	}{
		{"claude-memory", "", "claude-memory", "", false},
		{"cursor-rules", "x", "cursor-rules", "x", false},
		{"notes/a.md", "", "file", "notes/a.md", false},
		{"file", "a.md", "file", "a.md", false},
		{"file", "", "", "", true},
		{"https://x.io/a.md", "", "", "", true},
		{"", "", "", "", true},
		{"a.md", "b.md", "", "", true},
	}
	for _, c := range cases {
		src, p, err := ParseFrom(c.from, c.path)
		if (err != nil) != c.err || src != c.src || p != c.p {
			t.Errorf("ParseFrom(%q,%q) = %q,%q,%v", c.from, c.path, src, p, err)
		}
	}
}

func TestSlugOf(t *testing.T) {
	if got := SlugOf(`D:\Projects\bint`); got != "D--Projects-bint" {
		t.Fatalf("slug = %q", got)
	}
	if got := SlugOf("/home/a b/proj.x"); got != "-home-a-b-proj-x" {
		t.Fatalf("slug = %q", got)
	}
}

func TestLocateClaudeSlugAndMainRoot(t *testing.T) {
	home := t.TempDir()
	repo, main := filepath.Join(t.TempDir(), "wt"), filepath.Join(t.TempDir(), "main")
	// Bộ nhớ nằm ở cây chính, không ở cây làm việc hiện tại.
	write(t, filepath.Join(home, ".claude", "projects", SlugOf(main), "memory", "a.md"), "x")
	write(t, filepath.Join(home, ".claude", "projects", SlugOf(main), "memory", "sub", "skip.md"), "x")
	fs, err := Locate(knowledge.ImportClaudeMemory, LocateOptions{Root: repo, MainRoot: main, Home: home, Getenv: noEnv})
	if err != nil || len(fs) != 1 || fs[0].Name != "a.md" {
		t.Fatalf("Locate = %v, %v", fs, err)
	}
	// CLAUDE_CONFIG_DIR thắng ~/.claude.
	cfg := t.TempDir()
	write(t, filepath.Join(cfg, "projects", SlugOf(repo), "memory", "b.md"), "x")
	fs, err = Locate(knowledge.ImportClaudeMemory, LocateOptions{Root: repo, Home: home, Getenv: func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return cfg
		}
		return ""
	}})
	if err != nil || len(fs) != 1 || fs[0].Name != "b.md" {
		t.Fatalf("Locate cfg = %v, %v", fs, err)
	}
	_, err = Locate(knowledge.ImportClaudeMemory, LocateOptions{Root: t.TempDir(), Home: t.TempDir(), Getenv: noEnv})
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("không có nguồn phải ErrNoSource: %v", err)
	}
}

func TestLocateCodexAndCursorAndFile(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".codex", "memories", "m", "n.md"), "x")
	write(t, filepath.Join(home, ".codex", "memories", "bin.dat"), "x")
	fs, err := Locate(knowledge.ImportCodexMemory, LocateOptions{Home: home, Getenv: noEnv})
	if err != nil || len(fs) != 1 || fs[0].Name != "m/n.md" {
		t.Fatalf("codex = %v, %v", fs, err)
	}
	alt := t.TempDir()
	write(t, filepath.Join(alt, "memories", "z.md"), "x")
	fs, err = Locate(knowledge.ImportCodexMemory, LocateOptions{Home: home, Getenv: func(k string) string {
		if k == "CODEX_HOME" {
			return alt
		}
		return ""
	}})
	if err != nil || len(fs) != 1 || fs[0].Name != "z.md" {
		t.Fatalf("CODEX_HOME = %v, %v", fs, err)
	}

	repo := t.TempDir()
	write(t, filepath.Join(repo, ".cursor", "rules", "go", "a.mdc"), "x")
	write(t, filepath.Join(repo, ".cursorrules"), "x")
	fs, err = Locate(knowledge.ImportCursorRules, LocateOptions{Root: repo})
	if err != nil || len(fs) != 2 || fs[0].Name != ".cursor/rules/go/a.mdc" || fs[1].Name != ".cursorrules" {
		t.Fatalf("cursor = %v, %v", fs, err)
	}
	if _, err := Locate(knowledge.ImportCursorRules, LocateOptions{Root: t.TempDir()}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("cursor trống: %v", err)
	}

	f := filepath.Join(repo, "notes.md")
	write(t, f, "x")
	fs, err = Locate(knowledge.ImportFile, LocateOptions{Path: f})
	if err != nil || len(fs) != 1 || fs[0].Name != "notes.md" {
		t.Fatalf("file = %v, %v", fs, err)
	}
	if _, err := Locate(knowledge.ImportFile, LocateOptions{Path: repo}); err == nil {
		t.Fatal("thư mục không được nhận làm nguồn file")
	}
}

func TestSplitClaude(t *testing.T) {
	cases := []struct {
		name, data, skip, title string
		typ                     string
	}{
		{"feedback_a.md", "---\nname: a\ndescription: Dùng `store.Open` để mở SQLite\ntype: feedback\n---\n\nThân `store.Open` có busy_timeout.\n", "", "Dùng `store.Open` để mở SQLite", knowledge.TypeWarning},
		{"user_role.md", "---\nname: role\ndescription: vai trò\ntype: user\n---\nTôi là kỹ sư dữ liệu.\n", SkipPrivate, "vai trò", ""},
		{"MEMORY.md", "- [a](a.md) — hook\n", SkipIndexFile, "MEMORY.md", ""},
		{"project_x.md", "---\nname: x\ndescription: Tiến độ\ntype: project\n---\nĐang làm phần B3 của dự án.\n", SkipWorkState, "Tiến độ", knowledge.TypeFact},
		{"derived.md", "---\nname: d\ntype: reference\n---\nXem [k:01M746NASVKV9XMRHA4QNHJ3R8] về vet.\n", SkipBintDerived, "d", knowledge.TypeFact},
	}
	for _, c := range cases {
		ids := Split(knowledge.ImportClaudeMemory, c.name, []byte(c.data))
		if len(ids) != 1 {
			t.Fatalf("%s: %d ý", c.name, len(ids))
		}
		id := ids[0]
		if id.Skip != c.skip || id.Title != c.title || id.Type != c.typ || id.Ref != "claude-memory:"+c.name {
			t.Errorf("%s: %+v", c.name, id)
		}
		if c.skip == SkipPrivate && id.Body != "" {
			t.Errorf("ý riêng tư không được giữ nội dung: %q", id.Body)
		}
		if c.skip != "" && id.SkipDetail == "" {
			t.Errorf("%s: thiếu lý do", c.name)
		}
	}
}

func TestSplitBulletsParagraphsAndBintBlock(t *testing.T) {
	md := "# Quy ước\n\n- Dùng `store.Open` khi mở SQLite, không dùng sql.Open.\n  Dòng nối của gạch đầu dòng đầu tiên.\n- Ngắn\n- Chạy `go vet` với GOOS=windows trước khi đẩy.\n\nĐoạn văn thường nói về `internal/db/db.go` và cách mở.\n\n<!-- bint:begin -->\n- Mục do bint sinh ra từ tri thức [k:01M746NASVKV9XMRHA4QNHJ3R8]\n<!-- bint:end -->\n"
	ids := Split(knowledge.ImportFile, "notes.md", []byte(md))
	var kept, skipped []Idea
	for _, id := range ids {
		if id.Skip == "" {
			kept = append(kept, id)
		} else {
			skipped = append(skipped, id)
		}
	}
	if len(skipped) != 1 || skipped[0].Skip != SkipBintDerived {
		t.Fatalf("khối bint phải thành một ý bỏ qua: %+v", skipped)
	}
	if len(kept) != 3 {
		t.Fatalf("ý giữ lại = %d: %+v", len(kept), kept)
	}
	if !strings.Contains(kept[0].Body, "Dòng nối") || kept[0].Ref != "file:notes.md#1" || kept[1].Ref != "file:notes.md#2" {
		t.Errorf("ý 1: %+v / %+v", kept[0], kept[1])
	}
	// Khối rào ``` không bị tách theo dòng trống hay gạch đầu dòng bên trong.
	fenced := Split(knowledge.ImportFile, "f.md", []byte("Đoạn mở đầu đủ dài để thành một ý.\n```\n- không tách trong rào\n\nvà dòng trống\n```\n"))
	if len(fenced) != 1 || !strings.Contains(fenced[0].Body, "và dòng trống") {
		t.Errorf("khối rào bị tách: %+v", fenced)
	}
	// Khối bint không đóng: bỏ cả tệp.
	ids = Split(knowledge.ImportFile, "x.md", []byte("<!-- bint:begin -->\n- Một ý khá dài để vượt ngưỡng tối thiểu.\n"))
	if len(ids) != 1 || ids[0].Skip != SkipBintDerived {
		t.Fatalf("khối hở: %+v", ids)
	}
}

func TestSplitCursorRule(t *testing.T) {
	data := "---\ndescription: Quy ước Go\nglobs: \"internal/**/*.go, ../x, /abs\"\nalwaysApply: false\n---\n- Luôn bọc lỗi bằng `fmt.Errorf` với %w khi trả lên.\n"
	ids := Split(knowledge.ImportCursorRules, ".cursor/rules/go.mdc", []byte(data))
	if len(ids) != 1 {
		t.Fatalf("%+v", ids)
	}
	id := ids[0]
	if id.Type != knowledge.TypeRule || len(id.Paths) != 1 || id.Paths[0] != "internal/**/*.go" || id.Title != "Quy ước Go" {
		t.Fatalf("%+v", id)
	}
	if id.Ref != "cursor-rules:.cursor/rules/go.mdc" {
		t.Fatalf("ý duy nhất giữ Ref gọn: %q", id.Ref)
	}
}

func TestWorkState(t *testing.T) {
	cases := map[string]bool{
		"Dùng store.Open để mở SQLite":          false,
		"Sửa ở commit 755b544 hôm qua":          true,
		"Hạn 2026-11-13 phải xong":              true,
		"Theo ticket BINT-123 thì":              true,
		"Tiến độ: xong 3/5 mục":                 true,
		"WIP: đang sửa":                         true,
		"Mã deadbeef không có chữ số":           false,
		"Đường dẫn internal/app/knowledge.go":   false,
		"Phiên bản v1.0 của giao thức":          false,
		"Khoá 1234567 chỉ có chữ số không hash": false,
	}
	for s, want := range cases {
		if got := WorkState(s) != ""; got != want {
			t.Errorf("WorkState(%q) = %v, muốn %v", s, got, want)
		}
	}
}

func repoFinder() *Finder {
	files := map[string]string{
		"internal/db/db.go":          "package db\n\n// Open mở SQLite.\nfunc OpenStore(path string) error {\n\treturn nil\n}\n\ntype Conn struct{}\n",
		"internal/db/db_test.go":     "package db\n\nfunc TestOpenStore(t *testing.T) {}\n",
		"internal/app/run.go":        "package app\n\nfunc Run() {}\n",
		"cmd/run.go":                 "package main\n\nfunc Run() {}\nfunc main() {}\n",
		"vendor/x/v.go":              "package x\n\nfunc VendorThing() {}\n",
		".bint/knowledge/items/a.md": "func OpenStore",
		"README.md":                  "# readme\n",
	}
	var list []string
	for p := range files {
		list = append(list, p)
	}
	return NewFinder(list, func(rel string) ([]byte, bool) { s, ok := files[rel]; return []byte(s), ok })
}

func TestFindEvidence(t *testing.T) {
	f := repoFinder()
	ev := f.Find("Mở bằng `OpenStore` thay vì sql.Open; xem internal/db/db.go:1-2.")
	if len(ev) != 2 {
		t.Fatalf("evidence = %+v", ev)
	}
	if ev[0].Symbol != "OpenStore" || ev[0].Path != "internal/db/db.go" || ev[0].Lines != "4-6" {
		t.Errorf("định nghĩa phải đứng đầu: %+v", ev[0])
	}
	if ev[1].Path != "internal/db/db.go" || ev[1].Lines != "1-2" {
		t.Errorf("đường dẫn kèm dòng: %+v", ev[1])
	}

	// Ký hiệu mơ hồ (hai định nghĩa Run) không làm bằng chứng; từ thường và tên ngắn bị bỏ.
	if ev := f.Find("Gọi `Run()` rồi `server` và `Open`."); len(ev) != 0 {
		t.Errorf("ký hiệu mơ hồ/từ thường phải bị bỏ: %+v", ev)
	}
	// Đường dẫn không tồn tại, thư mục, tệp vendor, tệp .bint: không có bằng chứng.
	for _, s := range []string{"Xem `internal/nope/x.go`.", "Thư mục `internal/db/` chứa mã.", "`VendorThing` ở vendor", "xem `.bint/knowledge/items/a.md`"} {
		if ev := f.Find(s); len(ev) != 0 {
			t.Errorf("%q: %+v", s, ev)
		}
	}
	// Tên tệp duy nhất khớp theo tên cơ sở; vùng dòng vượt tệp thì bỏ vùng.
	ev = f.Find("Đọc `README.md:99` trước.")
	if len(ev) != 1 || ev[0].Path != "README.md" || ev[0].Lines != "" {
		t.Errorf("vùng vượt tệp: %+v", ev)
	}
	// Tên cơ sở không duy nhất (run.go ở hai nơi) thì không đoán.
	if ev := f.Find("Sửa `run.go`."); len(ev) != 0 {
		t.Errorf("tên tệp mơ hồ: %+v", ev)
	}
	// Tối đa 3 bằng chứng.
	ev = f.Find("`OpenStore` `Conn` internal/db/db.go:1-2 README.md internal/app/run.go")
	if len(ev) != MaxEvidence {
		t.Errorf("cần đúng %d bằng chứng: %+v", MaxEvidence, ev)
	}
}

func TestSplitCursorUnquotedGlobs(t *testing.T) {
	for _, value := range []string{"**/*.go", "**/*.go,internal/**", "[**/*.go, internal/**]"} {
		t.Run(value, func(t *testing.T) {
			ideas := Split(knowledge.ImportCursorRules, "rule.mdc", []byte("---\ndescription: Durable Go rule\nglobs: "+value+"\n---\nAlways call `OpenStore` when opening the database.\n"))
			want := []string{"**/*.go"}
			if strings.Contains(value, "internal/**") {
				want = append(want, "internal/**")
			}
			if len(ideas) != 1 || strings.Join(ideas[0].Paths, ",") != strings.Join(want, ",") || ideas[0].Title != "Durable Go rule" {
				t.Fatalf("frontmatter must preserve description and scope: %+v, want %v", ideas, want)
			}
		})
	}
}

func TestWorkStateKeepsDurableExamples(t *testing.T) {
	for _, body := range []string{
		"Always parse dates such as 2026-11-13 with the date parser.",
		"Use BINT-123 as the required ticket format in validation examples.",
		"Always reject sample token 755b544 in authentication fixtures.",
		"Use `Status: active` for durable records; do not omit the field.",
	} {
		if why := WorkState(body); why != "" {
			t.Errorf("durable rule rejected: %q: %s", body, why)
		}
	}
}

func TestReadSourceErrorHidesHome(t *testing.T) {
	home := t.TempDir()
	_, err := Read(File{Name: "note.md", Abs: filepath.Join(home, "note.md")})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source: %v", err)
	}
	if strings.Contains(err.Error(), home) {
		t.Fatalf("read error leaks absolute source: %v", err)
	}
}

func TestSplitMalformedFrontmatterFailClosed(t *testing.T) {
	for _, tc := range []struct{ name, source, data string }{
		{"private unclosed", knowledge.ImportClaudeMemory, "---\ntype: user\ndescription: Private preferences\nMy private preference is to use `OpenStore` for all database operations.\n"},
		{"cursor unclosed", knowledge.ImportCursorRules, "---\nglobs: internal/**\nAlways use `OpenStore` for all database operations.\n"},
		{"cursor malformed array", knowledge.ImportCursorRules, "---\nglobs: [**/*.go\n---\nAlways use `OpenStore` for all database operations.\n"},
		{"cursor malformed quote", knowledge.ImportCursorRules, "---\nglobs: [**/*.go, \"internal/**]\n---\nAlways use `OpenStore` for all database operations.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Split(tc.source, "note.md", []byte(tc.data))
			if len(got) != 1 || got[0].Skip != SkipCheckFailed || got[0].Body != "" {
				t.Fatalf("malformed metadata must not produce importable body: %+v", got)
			}
		})
	}
}
func TestWorkStateKeepsDurableStatusAndDeadline(t *testing.T) {
	for _, body := range []string{
		"Status values must be checked by `ValidateStatus` before storing a record.",
		"Deadline fields must parse ISO dates such as 2026-11-13 with `ParseDate`.",
	} {
		if why := WorkState(body); why != "" {
			t.Errorf("durable rule rejected: %q: %s", body, why)
		}
	}
}

func TestSplitFrontmatterLenientKeepsBody(t *testing.T) {
	const rule = "Always use `OpenStore` for all database operations in this repository."
	t.Run("claude invalid yaml", func(t *testing.T) {
		got := Split(knowledge.ImportClaudeMemory, "note.md", []byte("---\nname: tests\ndescription: Run tests: use go test ./...\n---\n"+rule+"\n"))
		if len(got) != 1 || got[0].Skip == SkipCheckFailed || !strings.Contains(got[0].Body, "OpenStore") {
			t.Fatalf("invalid YAML must keep body: %+v", got)
		}
	})
	t.Run("cursor brace globs", func(t *testing.T) {
		got := Split(knowledge.ImportCursorRules, "r.mdc", []byte("---\nglobs: {src,lib}/**/*.ts\n---\n"+rule+"\n"))
		if len(got) != 1 || got[0].Skip == SkipCheckFailed {
			t.Fatalf("brace globs must import: %+v", got)
		}
		if want := []string{"src/**/*.ts", "lib/**/*.ts"}; strings.Join(got[0].Paths, " ") != strings.Join(want, " ") {
			t.Fatalf("paths = %v, want %v", got[0].Paths, want)
		}
	})
	t.Run("leading horizontal rule", func(t *testing.T) {
		for _, data := range []string{"---\n# Notes\n\n" + rule + "\n", "---\n\n" + rule + "\n\n---\n\nSecond paragraph about `OpenStore` usage rules.\n"} {
			got := Split(knowledge.ImportFile, "notes.md", []byte(data))
			if len(got) == 0 || got[0].Skip == SkipCheckFailed || !strings.Contains(got[0].Body, "OpenStore") {
				t.Fatalf("horizontal rule must not be frontmatter: %q -> %+v", data, got)
			}
		}
	})
}
