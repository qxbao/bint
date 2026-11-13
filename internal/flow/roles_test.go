package flow

import (
	"strings"
	"testing"
)

func TestValidateReservedRoles(t *testing.T) {
	for _, role := range []string{"interactive", "evolver", "reflector"} {
		d := Def{Schema: Schema, Name: "tdd", Steps: []Step{{ID: "build", Kind: KindImplement, Skill: "b-feat", Role: role}}}
		errs := Validate(d, "tdd", nil)
		if len(errs) != 1 || !strings.Contains(errs[0], "dành riêng") {
			t.Errorf("role %s: errs = %v", role, errs)
		}
	}
	d := Def{Schema: Schema, Name: "tdd", Steps: []Step{{ID: "build", Kind: KindImplement, Skill: "b-feat", Role: "test-writer"}}}
	if errs := Validate(d, "tdd", nil); len(errs) != 0 {
		t.Errorf("role tuỳ biến hợp lệ bị từ chối: %v", errs)
	}
}

func TestValidSyntax(t *testing.T) {
	for s, want := range map[string]bool{"tdd": true, "red": true, "qa": true, "Bad_Name": false, "": false, "a--b": false,
		strings.Repeat("a", 41): false} {
		if got := ValidID(s); got != want {
			t.Errorf("ValidID(%q) = %v", s, got)
		}
	}
	if ValidName("qa") || !ValidName("tdd") {
		t.Error("ValidName phải đòi ≥ 3 ký tự")
	}
}
