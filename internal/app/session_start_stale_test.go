package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/harness/claude"
	"bint/internal/harness/hookcore"
	"bint/internal/skills"
)

// Hook SessionStart chèn đúng một dòng nhắc `bint update --pull` khi binary cũ hơn core.lock; không xuất gì khi khớp,
// mới hơn, không xác định hoặc core.lock hỏng; luôn trả err nil.
func TestSessionStartRemindsWhenBinaryOlder(t *testing.T) {
	root, _ := staleRepo(t) // core.lock ghi bởi autoOldBin
	start := func(bin skills.BuildStamp) string {
		t.Helper()
		in := hookInput("S1", "SessionStart", root, map[string]any{"source": "startup"})
		res, err := claude.HandleHook(context.Background(), strings.NewReader(in), claude.HookEnv{BintVersion: bin.Version, Build: func() skills.BuildStamp { return bin }})
		if err != nil {
			t.Fatalf("hook không được trả lỗi: %v", err)
		}
		return res.Output
	}
	lockPath := filepath.Join(root, filepath.FromSlash(skills.LockRel))
	l, _ := skills.ReadLock(root)
	l.BintRevision, l.BintRevisionTime = "cccccccccccc", "2026-12-01T00:00:00Z" // lock mới hơn autoOldBin
	writeTestLock(t, root, l)

	out := start(autoOldBin)
	var got struct {
		H struct {
			Event string `json:"hookEventName"`
			Ctx   string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.H.Event != "SessionStart" ||
		got.H.Ctx != hookcore.StaleBinaryReminder || strings.Contains(got.H.Ctx, "\n") || !strings.Contains(got.H.Ctx, "bint update --pull") {
		t.Fatalf("đầu ra: %q (%v)", out, err)
	}
	for name, bin := range map[string]skills.BuildStamp{
		"mới hơn":        {Version: "0.1.0-dev", Revision: "dddddddddddd", Time: "2027-01-01T00:00:00Z"},
		"không xác định": {Version: "0.1.0-dev"},
		"khớp":           {Version: "0.1.0-dev", Revision: "cccccccccccc", Time: "2026-12-01T00:00:00Z"},
	} {
		if o := start(bin); o != "" {
			t.Errorf("%s: không được xuất gì: %q", name, o)
		}
	}
	if err := os.WriteFile(lockPath, []byte("skills: [không phải map\n  : :"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o := start(autoOldBin); o != "" {
		t.Errorf("core.lock hỏng: không được xuất gì: %q", o)
	}
}
