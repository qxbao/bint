package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/hookcore"
)

func TestPostToolOutcomeNeedsMatchingTranscriptEvidence(t *testing.T) {
	success := `{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"call-1","exit_code":0}}}`
	for _, tc := range []struct {
		name, transcript string
		ok, known        bool
	}{
		{"exit zero", success, true, true},
		{"exit nonzero", strings.Replace(success, `"exit_code":0`, `"exit_code":2`, 1), false, true},
		{"different id", strings.Replace(success, "call-1", "call-other", 1), false, false},
		{"missing exit", strings.Replace(success, `,"exit_code":0`, "", 1), false, false},
		{"tail of large transcript", strings.Repeat("padding\n", (9<<20)/8) + success, true, true},
		{"unparseable", "FAIL, but no exit evidence", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			if err := os.WriteFile(path, []byte(tc.transcript), 0o600); err != nil {
				t.Fatal(err)
			}
			ok, known := postToolOutcome(hookcore.Input{Event: hookcore.EvPostTool, ToolUseID: "call-1", TranscriptPath: path}, hookcore.ToolInfo{Kind: hookcore.ToolShell})
			if ok != tc.ok || known != tc.known {
				t.Fatalf("outcome = (%v, %v), want (%v, %v)", ok, known, tc.ok, tc.known)
			}
		})
	}
}
