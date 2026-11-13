package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bint/internal/fsafe"
)

func writeCustom(t *testing.T, root, name, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(CustomPath(name, rel)))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func goodSkill(name string) string {
	return "---\nname: " + name + "\ndescription: Dùng khi cần kiểm thử skill " + name + ".\n---\n\n# " + name + "\n\nNội dung.\n"
}

func lintOf(t *testing.T, root, name string) (errs, warns []string) {
	t.Helper()
	cs, err := LoadCustomOne(root, name)
	if err != nil {
		t.Fatal(err)
	}
	return CheckCustom(root, cs)
}

func hasErr(errs []string, sub string) bool {
	for _, e := range errs {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

// Mỗi lỗi của AC-3 một ca, cộng ca hợp lệ và ca cảnh báo.
func TestCheckCustomTable(t *testing.T) {
	long := strings.Repeat("a", MaxDescriptionRunes+1)
	exact := strings.Repeat("a", MaxDescriptionRunes-len("Dùng khi "))
	body := "\n# x\n\nthân\n"
	cases := []struct {
		name, dir, content string
		extra              map[string]string // tệp phụ
		wantErr            string            // rỗng = hợp lệ
		wantWarn           bool
	}{
		{name: "hợp lệ", dir: "my-skill", content: goodSkill("my-skill")},
		{name: "mô tả đúng trần", dir: "my-skill", content: "---\nname: my-skill\ndescription: Dùng khi " + exact + "\n---\n" + body},
		{name: "tên chữ hoa", dir: "MySkill", content: goodSkill("MySkill"), wantErr: "không hợp lệ"},
		{name: "tên quá ngắn", dir: "ab", content: goodSkill("ab"), wantErr: "không hợp lệ"},
		{name: "tiền tố b-", dir: "b-mine", content: goodSkill("b-mine"), wantErr: "tiền tố b-"},
		{name: "trùng skill gốc", dir: "b-do", content: goodSkill("b-do"), wantErr: "b-"},
		{name: "trùng skill tiến hoá", dir: "core-retry", content: goodSkill("core-retry"), wantErr: "trùng skill tiến hoá"},
		{name: "name khác thư mục", dir: "my-skill", content: goodSkill("other-skill"), wantErr: "khác tên thư mục"},
		{name: "thiếu name", dir: "my-skill", content: "---\ndescription: Dùng khi x\n---\n" + body, wantErr: "thiếu name"},
		{name: "description rỗng", dir: "my-skill", content: "---\nname: my-skill\ndescription: \"\"\n---\n" + body, wantErr: "description rỗng"},
		{name: "description 1025 ký tự", dir: "my-skill", content: "---\nname: my-skill\ndescription: " + long + "\n---\n" + body, wantErr: "quá dài"},
		{name: "khoá allowed-tools", dir: "my-skill", content: "---\nname: my-skill\ndescription: Dùng khi x\nallowed-tools: Bash\n---\n" + body, wantErr: "allowed-tools"},
		{name: "khoá cho phép", dir: "my-skill", content: "---\nname: my-skill\ndescription: Dùng khi x\ndisable-model-invocation: true\nargument-hint: <x>\n---\n" + body},
		{name: "thân rỗng", dir: "my-skill", content: "---\nname: my-skill\ndescription: Dùng khi x\n---\n\n", wantErr: "thân SKILL.md rỗng"},
		{name: "thân 501 dòng", dir: "my-skill", content: "---\nname: my-skill\ndescription: Dùng khi x\n---\n" + strings.Repeat("dòng\n", 501), wantErr: "quá dài"},
		{name: "frontmatter hỏng", dir: "my-skill", content: "không có frontmatter\n", wantErr: "frontmatter"},
		{name: "liên kết tới tệp phụ thiếu", dir: "my-skill", content: goodSkill("my-skill") + "\nXem [chi tiết](references/x.md).\n", wantErr: "references/x.md"},
		{name: "liên kết tới tệp phụ có", dir: "my-skill", content: goodSkill("my-skill") + "\nXem [chi tiết](references/x.md#muc) và [web](https://a.b/c) và [neo](#top).\n",
			extra: map[string]string{"references/x.md": "x"}},
		{name: "liên kết trong khối mã bị bỏ qua", dir: "my-skill", content: goodSkill("my-skill") + "\n```\n[a](nope.md)\n```\nvà `[b](nope2.md)`\n"},
		{name: "khối mã 4 backtick chứa rào 3 backtick", dir: "my-skill", content: goodSkill("my-skill") + "\n````markdown\n```\n[a](nope.md)\n```\n[b](nope2.md)\n````\n"},
		{name: "khối mã ~~~ chứa rào backtick", dir: "my-skill", content: goodSkill("my-skill") + "\n~~~\n```\n[a](nope.md)\n~~~\n"},
		{name: "liên kết sau khối mã 4 backtick vẫn bị kiểm", dir: "my-skill", content: goodSkill("my-skill") + "\n````\n```\n````\n[x](nope.md)\n", wantErr: "nope.md"},
		{name: "liên kết thoát thư mục", dir: "my-skill", content: goodSkill("my-skill") + "\n[x](../other/SKILL.md)\n", wantErr: "thoát khỏi"},
		{name: "liên kết vào tests", dir: "my-skill", content: goodSkill("my-skill") + "\n[x](tests/a.yml)\n", wantErr: "tests/",
			extra: map[string]string{"tests/a.yml": "x"}},
		{name: "cảnh báo description không mở đầu Dùng khi", dir: "my-skill", wantWarn: true,
			content: "---\nname: my-skill\ndescription: Hướng dẫn làm X\n---\n" + body},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := newEvolved("core-retry", goodDesc, "b", "internal/core/**").Save(root); err != nil {
				t.Fatal(err)
			}
			writeCustom(t, root, c.dir, "SKILL.md", c.content)
			for rel, b := range c.extra {
				writeCustom(t, root, c.dir, rel, b)
			}
			errs, warns := lintOf(t, root, c.dir)
			if c.wantErr == "" && len(errs) > 0 {
				t.Fatalf("không được có lỗi: %v", errs)
			}
			if c.wantErr != "" && !hasErr(errs, c.wantErr) {
				t.Fatalf("muốn lỗi chứa %q, được %v", c.wantErr, errs)
			}
			if c.wantWarn != (len(warns) > 0) {
				t.Fatalf("cảnh báo: muốn %v, được %v", c.wantWarn, warns)
			}
		})
	}
}

func TestCheckCustomReservedAndMissingSkillMd(t *testing.T) {
	root := t.TempDir()
	if err := ValidName("overlays"); err == nil {
		t.Fatal("overlays là tên dành riêng")
	}
	if err := ValidName("evolved"); err == nil {
		t.Fatal("evolved là tên dành riêng")
	}
	os.MkdirAll(filepath.Join(root, ".bint", "skills", "no-doc"), 0o755)
	errs, _ := lintOf(t, root, "no-doc")
	if !hasErr(errs, "thiếu SKILL.md") {
		t.Fatalf("%v", errs)
	}
}

func TestCustomTemplatePassesLint(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "demo-skill", "SKILL.md", string(CustomTemplate("demo-skill")))
	errs, warns := lintOf(t, root, "demo-skill")
	if len(errs) > 0 || len(warns) > 0 {
		t.Fatalf("mẫu phải sạch: %v %v", errs, warns)
	}
}

func TestLoadCustomSelectsAndSkips(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "alpha", "SKILL.md", goodSkill("alpha"))
	writeCustom(t, root, "alpha", "references/a.md", "a")
	writeCustom(t, root, "alpha", "scripts/run.sh", "#!/bin/sh\n")
	os.Chmod(filepath.Join(root, ".bint/skills/alpha/scripts/run.sh"), 0o755)
	writeCustom(t, root, "alpha", "tests/case.yml", "x")
	writeCustom(t, root, "alpha", ".hidden", "x")
	writeCustom(t, root, "alpha", ".git/x", "x")
	writeCustom(t, root, "overlays", "SKILL.md", goodSkill("overlays"))
	writeCustom(t, root, ".dot", "SKILL.md", goodSkill(".dot"))
	os.WriteFile(filepath.Join(root, ".bint/skills/core.lock"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, ".bint/skills/evolved/e"), 0o755)
	os.MkdirAll(filepath.Join(root, ".bint/skills/empty"), 0o755)
	list, err := LoadCustom(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "empty" {
		t.Fatalf("danh sách: %+v", list)
	}
	var rels []string
	for _, f := range list[0].Files {
		rels = append(rels, f.Rel)
	}
	if strings.Join(rels, ",") != "references/a.md,scripts/run.sh" || len(list[0].Problems) != 0 {
		t.Fatalf("tệp phụ %v, vấn đề %v", rels, list[0].Problems)
	}
	if runtime.GOOS != "windows" && list[0].Files[1].Mode != 0o755 {
		t.Fatalf("giữ bit thực thi: %v", list[0].Files[1].Mode)
	}
	if len(list[1].Problems) != 1 {
		t.Fatalf("thư mục thiếu SKILL.md phải là vấn đề: %v", list[1].Problems)
	}
}

func TestLoadCustomRejectsSymlinksAndLimits(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "alpha", "SKILL.md", goodSkill("alpha"))
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("s"), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, ".bint/skills/alpha/link.md")); err != nil {
		t.Skipf("không tạo được symlink: %v", err)
	}
	if errs, _ := lintOf(t, root, "alpha"); !hasErr(errs, "link.md") {
		t.Fatalf("symlink phải là lỗi: %v", errs)
	}
	// SKILL.md là symlink
	writeCustom(t, root, "beta", "real.md", goodSkill("beta"))
	os.Symlink(filepath.Join(root, ".bint/skills/beta/real.md"), filepath.Join(root, ".bint/skills/beta/SKILL.md"))
	if errs, _ := lintOf(t, root, "beta"); !hasErr(errs, "SKILL.md không phải tệp thường") {
		t.Fatalf("SKILL.md symlink phải là lỗi: %v", errs)
	}
	// vượt trần
	writeCustom(t, root, "gamma", "SKILL.md", goodSkill("gamma"))
	for i := 0; i < MaxCustomFiles+1; i++ {
		writeCustom(t, root, "gamma", "r/f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26))+".md", "x")
	}
	if errs, _ := lintOf(t, root, "gamma"); !hasErr(errs, "tệp phụ") {
		t.Fatalf("quá 50 tệp: %v", errs)
	}
	writeCustom(t, root, "delta", "SKILL.md", goodSkill("delta"))
	writeCustom(t, root, "delta", "big.bin", strings.Repeat("x", MaxCustomFileBytes+1))
	if errs, _ := lintOf(t, root, "delta"); !hasErr(errs, "big.bin quá lớn") {
		t.Fatalf("tệp quá lớn: %v", errs)
	}
	writeCustom(t, root, "eps", "SKILL.md", goodSkill("eps"))
	for i := 0; i < 6; i++ {
		writeCustom(t, root, "eps", "m"+string(rune('a'+i))+".bin", strings.Repeat("x", MaxCustomFileBytes))
	}
	if errs, _ := lintOf(t, root, "eps"); !hasErr(errs, "tổng kích thước") {
		t.Fatalf("tổng quá trần: %v", errs)
	}
}

// ---- Install ----

func customOpts(t *testing.T, root string) InstallOptions {
	t.Helper()
	m, _ := fsafe.LoadManifest(filepath.Join(t.TempDir(), "m.json"))
	return InstallOptions{Root: root, Targets: []Target{ClaudeTarget, AgentsTarget}, BintVersion: "v1", BackupDir: t.TempDir(), Manifest: m, Now: time.Now()}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func resultOf(outs []Outcome, path string) (fsafe.Outcome, bool) {
	for _, o := range outs {
		if o.Path == path {
			return o.Result, true
		}
	}
	return "", false
}

func fullSkill(name string) string {
	return "---\nname: " + name + "\ndescription: Dùng khi cần X.\ndisable-model-invocation: true\n---\n\n# " + name + "\n\nXem [a](references/a.md).\n"
}

func TestInstallCustomPublishesToBothTargets(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", fullSkill("my-skill"))
	writeCustom(t, root, "my-skill", "references/a.md", "tham chiếu")
	writeCustom(t, root, "my-skill", "tests/case.yml", "bỏ")
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, ".claude/skills/my-skill/SKILL.md"); got != fullSkill("my-skill") {
		t.Fatalf("claude:\n%s", got)
	}
	if got := readFile(t, root, ".agents/skills/my-skill/SKILL.md"); strings.Contains(got, "disable-model-invocation") || !strings.Contains(got, "description: Dùng khi cần X.") {
		t.Fatalf("agents phải chỉ giữ name/description:\n%s", got)
	}
	for _, d := range []string{".claude/skills", ".agents/skills"} {
		if readFile(t, root, d+"/my-skill/references/a.md") != "tham chiếu" {
			t.Fatalf("tệp phụ ở %s", d)
		}
		if exists(root, d+"/my-skill/tests") {
			t.Fatalf("tests/ không được xuất bản ở %s", d)
		}
	}
	lock, err := ReadLock(root)
	e := lock.Custom["my-skill"]
	if err != nil || e.SHA256 == "" || len(e.Published) != 4 || len(e.Files) != 4 {
		t.Fatalf("core.lock custom: %v %+v", err, e)
	}
	// Lần hai: không đổi gì.
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		if strings.Contains(o.Path, "my-skill") && o.Result != fsafe.Unchanged {
			t.Fatalf("lần hai phải Unchanged: %+v", o)
		}
	}
}

func TestInstallCustomPreservesExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("không có bit thực thi")
	}
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	p := writeCustom(t, root, "my-skill", "scripts/run.sh", "#!/bin/sh\n")
	os.Chmod(p, 0o755)
	if _, err := Install(customOpts(t, root)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, ".claude/skills/my-skill/scripts/run.sh"))
	if err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("mất bit thực thi: %v %v", err, fi)
	}
}

func TestInstallCustomConflictOnHandEditedAndForce(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", fullSkill("my-skill"))
	writeCustom(t, root, "my-skill", "references/a.md", "v1")
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	// Sửa tay bản đã xuất bản (cả SKILL.md lẫn tệp phụ), và đổi nguồn.
	os.WriteFile(filepath.Join(root, ".claude/skills/my-skill/SKILL.md"), []byte("sửa tay"), 0o644)
	os.WriteFile(filepath.Join(root, ".claude/skills/my-skill/references/a.md"), []byte("sửa tay"), 0o644)
	writeCustom(t, root, "my-skill", "references/a.md", "v2")
	writeCustom(t, root, "my-skill", "SKILL.md", fullSkill("my-skill")+"\nthêm\n")
	// Máy khác (manifest trống): chỉ dựa vào core.lock vẫn phải nhận ra sửa tay.
	opts.Manifest, _ = fsafe.LoadManifest(filepath.Join(t.TempDir(), "m2.json"))
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".claude/skills/my-skill/SKILL.md", ".claude/skills/my-skill/references/a.md"} {
		if r, _ := resultOf(outs, rel); r != fsafe.Conflict {
			t.Fatalf("%s phải xung đột, được %v", rel, r)
		}
		if readFile(t, root, rel) != "sửa tay" {
			t.Fatalf("%s bị ghi đè", rel)
		}
	}
	if r, _ := resultOf(outs, ".agents/skills/my-skill/references/a.md"); r != fsafe.Updated {
		t.Fatalf("target chưa sửa tay phải cập nhật: %v", r)
	}
	var detail string
	for _, o := range outs {
		if o.Result == fsafe.Conflict {
			detail = o.Detail
		}
	}
	if !strings.Contains(detail, ".bint/skills/my-skill") {
		t.Fatalf("thông điệp xung đột phải chỉ về nguồn: %q", detail)
	}
	// Lock giữ băm cũ của tệp xung đột để lần sau vẫn nhận ra.
	lock, _ := ReadLock(root)
	if len(lock.Custom["my-skill"].Files) != 4 {
		t.Fatalf("lock: %+v", lock.Custom["my-skill"])
	}
	// --force ghi đè và sao lưu.
	opts.Force = true
	outs, err = Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resultOf(outs, ".claude/skills/my-skill/references/a.md"); r != fsafe.Updated {
		t.Fatalf("--force phải ghi đè: %v", r)
	}
	if readFile(t, root, ".claude/skills/my-skill/references/a.md") != "v2" {
		t.Fatal("nội dung sau --force")
	}
	var backed bool
	for _, o := range outs {
		backed = backed || (o.Backup != "" && strings.HasSuffix(o.Path, "references/a.md"))
	}
	if !backed {
		t.Fatal("--force phải sao lưu bản sửa tay")
	}
}

func TestInstallCustomRemovesDeletedFileAndSkill(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	writeCustom(t, root, "my-skill", "references/deep/a.md", "a")
	writeCustom(t, root, "my-skill", "b.md", "b")
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	// Xoá tệp phụ sâu: gỡ tệp và các thư mục con rỗng, giữ skill.
	os.RemoveAll(filepath.Join(root, ".bint/skills/my-skill/references"))
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resultOf(outs, ".claude/skills/my-skill/references/deep/a.md"); r != fsafe.Removed {
		t.Fatalf("tệp phụ bị xoá khỏi nguồn phải được gỡ: %v", r)
	}
	if exists(root, ".claude/skills/my-skill/references") || !exists(root, ".claude/skills/my-skill/SKILL.md") || !exists(root, ".claude/skills/my-skill/b.md") {
		t.Fatal("chỉ thư mục rỗng bị dọn, phần còn lại giữ nguyên")
	}
	// Tệp phụ bị sửa tay thì không gỡ.
	os.WriteFile(filepath.Join(root, ".agents/skills/my-skill/b.md"), []byte("sửa tay"), 0o644)
	os.Remove(filepath.Join(root, ".bint/skills/my-skill/b.md"))
	outs, _ = Install(opts)
	if r, _ := resultOf(outs, ".agents/skills/my-skill/b.md"); r != fsafe.Conflict || !exists(root, ".agents/skills/my-skill/b.md") {
		t.Fatalf("tệp sửa tay không được gỡ: %v", r)
	}
	if r, _ := resultOf(outs, ".claude/skills/my-skill/b.md"); r != fsafe.Removed {
		t.Fatalf("tệp chưa sửa phải gỡ: %v", r)
	}
	// Xoá cả skill: gỡ thư mục đã xuất bản (trừ tệp bị sửa tay) và mục lock.
	os.RemoveAll(filepath.Join(root, ".bint/skills/my-skill"))
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if exists(root, ".claude/skills/my-skill") {
		t.Fatal("skill bị xoá phải được gỡ khỏi .claude/skills")
	}
	if !exists(root, ".agents/skills/my-skill/b.md") || exists(root, ".agents/skills/my-skill/SKILL.md") {
		t.Fatal(".agents: giữ tệp sửa tay, gỡ phần còn lại")
	}
	lock, _ := ReadLock(root)
	if e, ok := lock.Custom["my-skill"]; !ok || len(e.Published) != 1 {
		t.Fatalf("lock còn theo dõi tệp xung đột: %+v", lock.Custom)
	}
	// Người dùng xoá tay tệp đó: lần sau lock sạch.
	os.Remove(filepath.Join(root, ".agents/skills/my-skill/b.md"))
	Install(opts)
	if lock, _ = ReadLock(root); len(lock.Custom) != 0 {
		t.Fatalf("lock phải sạch: %+v", lock.Custom)
	}
}

func TestInstallCustomLintFailureKeepsPublished(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	before, _ := ReadLock(root)
	// Sửa dở: description rỗng.
	writeCustom(t, root, "my-skill", "SKILL.md", "---\nname: my-skill\ndescription: \"\"\n---\n\nx\n")
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	var conflict bool
	for _, o := range outs {
		conflict = conflict || (o.Skill == "my-skill" && o.Result == fsafe.Conflict && strings.Contains(o.Detail, "description"))
	}
	if !conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
	if readFile(t, root, ".claude/skills/my-skill/SKILL.md") != goodSkill("my-skill") {
		t.Fatal("phải giữ bản đã xuất bản")
	}
	after, _ := ReadLock(root)
	if after.Custom["my-skill"].SHA256 != before.Custom["my-skill"].SHA256 || len(after.Custom["my-skill"].Published) != 2 {
		t.Fatalf("phải giữ mục core.lock: %+v", after.Custom)
	}
	// Skill lỗi ngay từ đầu (thiếu SKILL.md) không được xuất bản gì.
	os.MkdirAll(filepath.Join(root, ".bint/skills/broken"), 0o755)
	Install(opts)
	if exists(root, ".claude/skills/broken") {
		t.Fatal("skill lỗi không được xuất bản")
	}
}

func TestInstallCustomNameCollisionKeepsEvolved(t *testing.T) {
	root := t.TempDir()
	if err := newEvolved("core-retry", goodDesc, "b", "internal/core/**").Save(root); err != nil {
		t.Fatal(err)
	}
	writeCustom(t, root, "core-retry", "SKILL.md", goodSkill("core-retry"))
	opts := customOpts(t, root)
	opts.Files = []string{"internal/core/x.go"}
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, ".claude/skills/core-retry/SKILL.md"); !strings.Contains(got, goodDesc) {
		t.Fatalf("skill tiến hoá phải giữ đường dẫn:\n%s", got)
	}
	var conflict bool
	for _, o := range outs {
		conflict = conflict || (o.Result == fsafe.Conflict && strings.Contains(o.Detail, "trùng skill tiến hoá"))
	}
	if !conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
}

func TestInstallCustomLegacyLockReadable(t *testing.T) {
	root := t.TempDir()
	legacy := "# cũ\nbint_version: v0\nskills:\n  b-do:\n    sha256: abc\n"
	if err := os.MkdirAll(filepath.Join(root, ".bint/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, LockRel), []byte(legacy), 0o644)
	lock, err := ReadLock(root)
	if err != nil || lock.Custom != nil || lock.Skills["b-do"].Files != nil {
		t.Fatalf("%v %+v", err, lock)
	}
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	if _, err := Install(customOpts(t, root)); err != nil {
		t.Fatal(err)
	}
	if lock, _ = ReadLock(root); len(lock.Custom["my-skill"].Files) != 2 {
		t.Fatalf("%+v", lock.Custom)
	}
	// Không có skill tuỳ biến: core.lock không có khoá custom.
	root2 := t.TempDir()
	if _, err := Install(customOpts(t, root2)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, root2, LockRel), "custom:") {
		t.Fatal("custom phải omitempty")
	}
}

func TestInstallCustomKeepsFilesOfDisabledTarget(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	opts.Targets = []Target{ClaudeTarget} // tắt codex/antigravity
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if !exists(root, ".agents/skills/my-skill/SKILL.md") {
		t.Fatal("target đã tắt: không gỡ bản đã xuất bản")
	}
	lock, _ := ReadLock(root)
	if len(lock.Custom["my-skill"].Published) != 2 {
		t.Fatalf("vẫn theo dõi: %+v", lock.Custom)
	}
}

// Hồi quy review .1: skill tiến hoá trùng tên skill tuỳ biến đã xuất bản không được đè bản của skill tuỳ biến.
func TestInstallEvolvedDoesNotOverwritePublishedCustom(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "core-retry", "SKILL.md", goodSkill("core-retry"))
	writeCustom(t, root, "core-retry", "references/a.md", "phụ\n")
	opts := customOpts(t, root)
	opts.Files = []string{"internal/core/x.go"}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	custom := readFile(t, root, ".claude/skills/core-retry/SKILL.md")
	// một đề xuất tiến hoá lọt qua kiểm tên (hoặc skill tiến hoá được thêm tay) cùng tên
	if err := newEvolved("core-retry", goodDesc, "b", "internal/core/**").Save(root); err != nil {
		t.Fatal(err)
	}
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, ".claude/skills/core-retry/SKILL.md"); got != custom {
		t.Fatalf("skill tuỳ biến bị đè:\n%s", got)
	}
	var conflict bool
	for _, o := range outs {
		conflict = conflict || (o.Result == fsafe.Conflict && strings.Contains(o.Detail, "trùng tên skill tuỳ biến"))
	}
	if !conflict {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
	lock, _ := ReadLock(root)
	if _, ok := lock.Evolved["core-retry"]; ok {
		t.Fatalf("lock tiến hoá không được ghi mục trùng: %+v", lock.Evolved)
	}
	if _, ok := lock.Custom["core-retry"]; !ok {
		t.Fatalf("mục custom phải còn: %+v", lock.Custom)
	}
}

func TestCustomExists(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	if !CustomExists(root, "my-skill") || CustomExists(root, "other") || CustomExists(root, "overlays") || CustomExists("", "my-skill") {
		t.Fatal("CustomExists sai")
	}
}

// Hồi quy review .2: chmod +x tệp phụ khi nội dung không đổi vẫn được xuất bản.
func TestInstallCustomSyncsExecBitWithoutContentChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("không có bit thực thi")
	}
	root := t.TempDir()
	writeCustom(t, root, "my-skill", "SKILL.md", goodSkill("my-skill"))
	p := writeCustom(t, root, "my-skill", "scripts/run.sh", "#!/bin/sh\n")
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, ".claude/skills/my-skill/scripts/run.sh")
	if fi, _ := os.Stat(dst); fi.Mode()&0o111 != 0 {
		t.Fatalf("chưa chmod mà đã có bit thực thi: %v", fi.Mode())
	}
	os.Chmod(p, 0o755)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dst); fi.Mode()&0o111 == 0 {
		t.Fatalf("bit thực thi không được xuất bản: %v", fi.Mode())
	}
}
