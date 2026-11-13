package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bint/internal/skills"
)

func doctorSkillCheck(t *testing.T, root, skill string) Check {
	t.Helper()
	for _, c := range Doctor(context.Background(), root).Checks {
		if c.Name == "skill "+skill && c.Level != LevelOK {
			return c
		}
	}
	t.Fatalf("doctor không báo cảnh báo cho skill %s", skill)
	return Check{}
}

// doctor phân biệt bản xuất bản do bản Bint khác (mới hơn) ghi — băm tệp khớp core.lock nhưng core.lock có băm skill
// gốc khác bản nhúng — với tệp bị sửa tay.
func TestDoctorDistinguishesStaleBinaryFromHandEdit(t *testing.T) {
	root := sysRepo(t, InitOptions{})
	const name = "b-bootstrap"
	file := filepath.Join(root, ".claude", "skills", name, "SKILL.md")
	orig, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	// Sửa tay: tệp đổi nhưng core.lock giữ băm cũ ⇒ thông báo cũ.
	if err := os.WriteFile(file, append(append([]byte{}, orig...), "\nsửa tay\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	c := doctorSkillCheck(t, root, name)
	if !strings.Contains(c.Detail, "bị sửa tay") || strings.Contains(c.Detail, "cập nhật bint") {
		t.Fatalf("sửa tay phải giữ thông báo cũ: %s", c.Detail)
	}

	// Bản Bint mới hơn ghi: core.lock có băm gốc khác bản nhúng, và băm xuất bản khớp tệp.
	newer := append(append([]byte{}, orig...), "\nbản mới hơn của Bint khác\n"...)
	if err := os.WriteFile(file, newer, 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, filepath.FromSlash(skills.LockRel))
	lock, err := skills.ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	e := lock.Skills[name]
	e.SHA256 = "0123456789abcdef"
	e.Composed = skills.Skill{Content: newer}.Hash()
	lock.Skills[name] = e
	lock.BintVersion = "v9.9.9-khac"
	data, err := yaml.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// Hướng phiên bản quyết định lời khuyên: lock mới hơn binary → cập nhật bint; lock cũ hơn binary (vừa nâng cấp Bint)
	// → chạy bint sync; không phân biệt được → nêu cả hai. Không ca nào được gọi là "sửa tay".
	t.Cleanup(func() { SetBinaryVersion("") })
	for _, tc := range []struct {
		binary string
		want   []string
		not    []string
	}{
		{"v9.0.0", []string{"bản Bint khác ghi", "v9.9.9-khac", "cập nhật bint", "đừng chạy `bint sync`"}, []string{"Bint đã nâng cấp"}},
		{"v10.0.0", []string{"bản Bint khác ghi", "Bint đã nâng cấp: chạy `bint sync`"}, []string{"cập nhật bint", "đừng chạy"}},
		{"v9.9.9-dev", []string{"chưa xác định", "chạy `bint sync`", "cập nhật bint"}, nil},
		{"", []string{"chưa xác định", "chạy `bint sync`", "cập nhật bint"}, nil},
	} {
		SetBinaryVersion(tc.binary)
		c = doctorSkillCheck(t, root, name)
		for _, want := range tc.want {
			if !strings.Contains(c.Detail, want) {
				t.Fatalf("binary %q: thiếu %q: %s", tc.binary, want, c.Detail)
			}
		}
		for _, bad := range append(tc.not, "bị sửa tay") {
			if strings.Contains(c.Detail, bad) {
				t.Fatalf("binary %q: không được chứa %q: %s", tc.binary, bad, c.Detail)
			}
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		cmp  int
		ok   bool
	}{
		{"v1.2.3", "v1.2.4", -1, true},
		{"1.10.0", "1.9.0", 1, true},
		{"v2", "v1.9.9", 1, true},
		{"0.1.0-dev", "v0.2.0", -1, true},
		{"0.1.0-dev", "0.1.0", 0, false},
		{"v1.0.0", "v1.0.0", 0, false},
		{"dev", "v1.0.0", 0, false},
		{"", "v1.0.0", 0, false},
		{"1.2.3.4", "1.2.3", 0, false},
	} {
		if c, ok := compareVersions(tc.a, tc.b); c != tc.cmp || ok != tc.ok {
			t.Errorf("compareVersions(%q, %q) = %d, %v; muốn %d, %v", tc.a, tc.b, c, ok, tc.cmp, tc.ok)
		}
	}
}
