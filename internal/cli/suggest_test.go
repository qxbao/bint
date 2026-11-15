package cli

import (
	"strings"
	"testing"

	"bint/internal/skills"
)

func TestUnknownSubcommandSuggests(t *testing.T) {
	noAgentEnv(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"task", "stauts"}, "bint task status"},
		{[]string{"tsak"}, "bint task"},
	} {
		out, errOut, code := runCLI(t, tc.args...)
		if code != ExitUsage || out != "" || !strings.Contains(errOut, "lệnh lạ") || !strings.Contains(errOut, "Ý bạn là:") || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: mã %d stdout=%q stderr=%q", tc.args, code, out, errOut)
		}
	}
	// Không đổi: nhóm không đối số, --help, lệnh hợp lệ.
	for _, args := range [][]string{{"task"}, {"task", "--help"}, {"help", "task"}, {"task", "transitions"}} {
		if _, errOut, code := runCLI(t, args...); code != 0 {
			t.Errorf("%v: mã %d stderr=%q", args, code, errOut)
		}
	}
}

func TestVersionShowsBuildStamp(t *testing.T) {
	noAgentEnv(t)
	for _, tc := range []struct {
		stamp skills.BuildStamp
		want  string
	}{
		{skills.BuildStamp{}, "bint version " + Version + "\n"},
		{skills.BuildStamp{Revision: "0123456789abcdef", Time: "2026-11-01T00:00:00Z"}, "bint version " + Version + " (0123456, 2026-11-01T00:00:00Z)\n"},
		{skills.BuildStamp{Revision: "0123456789abcdef", Time: "2026-11-01T00:00:00Z", Modified: true}, "bint version " + Version + " (0123456, 2026-11-01T00:00:00Z, sửa)\n"},
	} {
		restore := skills.SetBuildOverride(&tc.stamp)
		out, _, code := runCLI(t, "--version")
		restore()
		if code != 0 || out != tc.want {
			t.Errorf("%+v: %q, muốn %q", tc.stamp, out, tc.want)
		}
	}
}
