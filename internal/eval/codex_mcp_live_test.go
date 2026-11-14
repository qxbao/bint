package eval

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/testutil"
)

// Opt-in local CLI compatibility check. No model, auth, network, MCP service or
// user configuration is used; mcp list only resolves fixture configuration.
func TestCodexMCPConfigLive(t *testing.T) {
	if os.Getenv("BINT_CODEX_CONFIG_LIVE") != "1" {
		t.Skip("set BINT_CODEX_CONFIG_LIVE=1 to check installed Codex config resolution")
	}
	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"regular", "separate-git-dir", "submodule", "linked-separate-git-dir", "linked-submodule"} {
		t.Run(kind, func(t *testing.T) {
			var repo string
			if kind == "regular" {
				repo = testutil.NewRepo(t, map[string]string{"README.md": "fixture\n"})
			} else {
				repo = newCodexGitFileRepo(t, strings.TrimPrefix(kind, "linked-"))
			}
			if strings.HasPrefix(kind, "linked-") {
				wt := filepath.Join(t.TempDir(), "linked")
				testutil.Git(t, repo, "worktree", "add", "-q", "-b", "task", wt)
				repo = wt
			}
			checkCodexMCPConfigLive(t, bin, repo)
		})
	}
}

func checkCodexMCPConfigLive(t *testing.T, bin, repo string) {
	t.Helper()
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	child, deep := filepath.Join(repo, "child"), filepath.Join(repo, "child", "deep")
	for path, name := range map[string]string{repo: "root_only", child: "child_only", deep: "deep_only", filepath.Dir(repo): "outside_only"} {
		writeMCPTestFile(t, filepath.Join(path, ".codex", "config.toml"), "[mcp_servers."+name+"]\ncommand='fixture'\n")
	}
	home := t.TempDir()
	for _, tc := range []struct {
		name, trust string
		want        []string
	}{
		{"root", trustedProject(repo), []string{"a.b", "c d", "child_only", "deep_only", "root_only"}},
		{"child", trustedProject(child), []string{"a.b", "c d", "child_only"}},
		{"denied child", trustedProject(repo) + "\n[projects." + tomlString(child) + "]\ntrust_level='untrusted'\n", []string{"a.b", "c d", "deep_only", "root_only"}},
		{"untrusted", "", []string{"a.b", "c d"}},
		{"outside ancestor", trustedProject(filepath.Dir(repo)), []string{"a.b", "c d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeMCPTestFile(t, filepath.Join(home, "config.toml"), tc.trust+"\n[mcp_servers.\"a.b\"]\ncommand='fixture'\n[mcp_servers.\"c d\"]\ncommand='fixture'\n")
			req := AgentRequest{Dir: deep, Env: []string{"CODEX_HOME=" + home}}
			list := func(overrides []string, enabledOnly bool) []string {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				args := append([]string{"--no-daemon"}, overrides...)
				args = append(args, "mcp", "list", "--json")
				cmd := exec.CommandContext(ctx, bin, args...)
				cmd.Dir, cmd.Env = deep, cleanEnv(req.Env...)
				raw, err := cmd.Output()
				if err != nil {
					t.Fatalf("Codex mcp list: %v", err)
				}
				var servers []struct {
					Name    string
					Enabled bool
				}
				if err := json.Unmarshal(raw, &servers); err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, s := range servers {
					if !enabledOnly || s.Enabled {
						names = append(names, s.Name)
					}
				}
				slices.Sort(names)
				return names
			}
			if got := list(nil, false); !slices.Equal(got, tc.want) {
				t.Fatalf("CLI configured servers = %v; want %v", got, tc.want)
			}
			if got, err := codexOtherMCPServers(req); err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("Bint discovered servers = %v, %v; want %v", got, err, tc.want)
			}
			off, err := codexDisableMCPArgs(req)
			if err != nil {
				t.Fatal(err)
			}
			off = append(off, "-c", `mcp_servers.bint.command="fixture-bint"`)
			if got := list(off, true); !slices.Equal(got, []string{"bint"}) {
				t.Fatalf("exclusive MCP left enabled servers %v", got)
			}
		})
	}
}
