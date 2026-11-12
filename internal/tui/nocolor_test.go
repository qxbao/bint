package tui

import (
	"testing"

	"github.com/charmbracelet/colorprofile"

	"bint/internal/tui/theme"
)

// NO_COLOR khác rỗng (kể cả "0" hay khoảng trắng, theo no-color.org) → Mono ở mọi color profile; rỗng thì theo profile và nền.
func TestDetectModeNoColor(t *testing.T) {
	profiles := map[string]colorprofile.Profile{"truecolor": colorprofile.TrueColor, "ansi256": colorprofile.ANSI256, "ansi16": colorprofile.ANSI,
		"ascii": colorprofile.ASCII, "notty": colorprofile.NoTTY}
	for name, p := range profiles {
		for _, v := range []string{"1", "0", "true", " ", "x"} {
			for _, dark := range []bool{true, false} {
				got := DetectMode(func(k string) string {
					if k == "NO_COLOR" {
						return v
					}
					return ""
				}, p, dark)
				if got != theme.Mono {
					t.Errorf("NO_COLOR=%q profile=%s dark=%v: %v, muốn mono", v, name, dark, got)
				}
			}
		}
	}
	none := func(string) string { return "" }
	cases := []struct {
		p    colorprofile.Profile
		dark bool
		want theme.Mode
	}{
		{colorprofile.TrueColor, true, theme.Dark}, {colorprofile.TrueColor, false, theme.Light}, {colorprofile.ANSI256, true, theme.Dark},
		{colorprofile.ANSI, true, theme.ANSI16}, {colorprofile.ASCII, true, theme.Mono}, {colorprofile.NoTTY, false, theme.Mono},
	}
	for _, c := range cases {
		if got := DetectMode(none, c.p, c.dark); got != c.want {
			t.Errorf("NO_COLOR rỗng profile=%v dark=%v: %v, muốn %v", c.p, c.dark, got, c.want)
		}
	}
}
