package config

import (
	"strings"
	"testing"
)

func TestLessonsDefaultsAndValidate(t *testing.T) {
	d := Default()
	if d.Context.Lessons.GlobalK != 3 || d.Context.Lessons.MaxTokens != 600 {
		t.Fatalf("mặc định context.lessons = %+v, muốn global_k 3, max_tokens 600", d.Context.Lessons)
	}
	if d.Roles.Reflector == "" || d.Harnesses.Claude.Models.Reflector != "opus" {
		t.Fatalf("mặc định reflector: role %q, model %q (muốn opus)", d.Roles.Reflector, d.Harnesses.Claude.Models.Reflector)
	}
	bad := Default()
	bad.Context.Lessons.GlobalK = 11
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "context.lessons.global_k") {
		t.Fatalf("global_k 11 phải bị từ chối: %v", err)
	}
	bad = Default()
	bad.Context.Lessons.MaxTokens = -1
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "context.lessons.max_tokens") {
		t.Fatalf("max_tokens âm phải bị từ chối: %v", err)
	}
	bad = Default()
	bad.Roles.Reflector = "vim"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "roles.reflector") {
		t.Fatalf("roles.reflector sai harness phải bị từ chối: %v", err)
	}
}
