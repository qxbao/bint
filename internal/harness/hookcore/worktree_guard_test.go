package hookcore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wtDirs(t *testing.T) (root, wt, wt2 string) {
	t.Helper()
	base := t.TempDir()
	root, wt, wt2 = filepath.Join(base, "root"), filepath.Join(base, "wt"), filepath.Join(base, "wt2")
	for _, d := range []string{root, wt, wt2, filepath.Join(wt, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func editInfo(p string) ToolInfo  { return ToolInfo{Kind: ToolEdit, Paths: []string{p}} }
func shellInfo(c string) ToolInfo { return ToolInfo{Kind: ToolShell, Command: c} }

func TestWorktreeDecisionMapped(t *testing.T) {
	root, wt, wt2 := wtDirs(t)
	iso := &SessionStepRecord{RunID: "r1", TaskID: "T1", Worktree: wt, Isolated: true}
	shared := &SessionStepRecord{RunID: "r2", TaskID: "T2", Worktree: root}
	link := filepath.Join(filepath.Dir(wt), "link")
	if err := os.Symlink(wt, link); err != nil {
		t.Skip("không tạo được symlink:", err)
	}
	cases := []struct {
		name string
		at   string
		step *SessionStepRecord
		info ToolInfo
		cwd  string
		deny bool
	}{
		{"edit trong worktree", SubagentImplementer, iso, editInfo(filepath.Join(wt, "a.go")), root, false},
		{"edit thư mục con", SubagentImplementer, iso, editInfo(filepath.Join(wt, "sub", "a.go")), root, false},
		{"edit cây gốc", SubagentImplementer, iso, editInfo(filepath.Join(root, "a.go")), root, true},
		{"edit tương đối theo cwd gốc", SubagentImplementer, iso, editInfo("a.go"), root, true},
		{"edit tương đối theo cwd worktree", SubagentImplementer, iso, editInfo("a.go"), wt, false},
		{"edit .. thoát worktree", SubagentImplementer, iso, editInfo(filepath.Join(wt, "..", "root", "a.go")), root, true},
		{"edit worktree khác", SubagentImplementer, iso, editInfo(filepath.Join(wt2, "a.go")), root, true},
		{"edit tiền tố trùng tên", SubagentImplementer, iso, editInfo(wt + "2/a.go"), root, true},
		{"edit qua symlink vào worktree", SubagentImplementer, iso, editInfo(filepath.Join(link, "a.go")), root, false},
		{"edit shared", SubagentImplementer, shared, editInfo(filepath.Join(root, "a.go")), root, false},
		{"agent_type khác", "general-purpose", iso, editInfo(filepath.Join(root, "a.go")), root, false},
		{"agent_type rỗng", "", iso, editInfo(filepath.Join(root, "a.go")), root, false},
		{"planner không thuộc guardrail này", SubagentPlanner, iso, editInfo(filepath.Join(root, "a.go")), root, false},
		{"công cụ khác", SubagentImplementer, iso, ToolInfo{Kind: ToolRead, Paths: []string{filepath.Join(root, "a.go")}}, root, false},
		{"bash cd worktree", SubagentImplementer, iso, shellInfo("cd " + wt + " && go test ./..."), root, false},
		{"bash cd worktree nháy kép", SubagentImplementer, iso, shellInfo(`cd "` + wt + `" && ls`), root, false},
		{"bash cd thư mục con worktree", SubagentImplementer, iso, shellInfo("cd " + wt + "/sub && ls"), root, false},
		{"bash cd tương đối trong worktree", SubagentImplementer, iso, shellInfo("cd sub && ls"), wt, false},
		{"bash không cd, cwd gốc", SubagentImplementer, iso, shellInfo("go test ./..."), root, true},
		{"bash không cd, cwd worktree", SubagentImplementer, iso, shellInfo("go test ./..."), wt, false},
		{"bash cd cây gốc", SubagentImplementer, iso, shellInfo("cd " + root + " && touch x"), root, true},
		{"bash cd cây gốc dù cwd trong worktree", SubagentImplementer, iso, shellInfo("cd " + root + " && touch x"), wt, true},
		{"bash cd worktree khác", SubagentImplementer, iso, shellInfo("cd " + wt2 + " && touch x"), root, true},
		{"bash cd tiền tố trùng tên", SubagentImplementer, iso, shellInfo("cd " + wt + "2 && touch x"), root, true},
		{"bash cd .. ra ngoài", SubagentImplementer, iso, shellInfo("cd " + wt + "/.. && touch x"), root, true},
		{"bash shared", SubagentImplementer, shared, shellInfo("touch x"), root, false},
	}
	for _, c := range cases {
		v := worktreeDecision(worktreeCheck{AgentType: c.at, Tool: "T", Info: c.info, Cwd: c.cwd, Step: c.step})
		if v.Deny != c.deny {
			t.Errorf("%s: deny=%v, muốn %v (%s)", c.name, v.Deny, c.deny, v.Reason)
		}
		if v.Deny && (v.Outcome != OutcomeFired || !strings.Contains(v.Reason, c.step.Worktree) || !strings.Contains(v.Reason, "đường dẫn tuyệt đối")) {
			t.Errorf("%s: outcome/lý do: %+v", c.name, v)
		}
	}
}

func TestWorktreeDecisionUnmapped(t *testing.T) {
	root, wt, wt2 := wtDirs(t)
	live := []SessionStepRecord{{RunID: "r1", TaskID: "T1", Worktree: wt, Isolated: true}, {RunID: "r3", TaskID: "T3", Worktree: wt2, Isolated: true}}
	cases := []struct {
		name string
		live []SessionStepRecord
		info ToolInfo
		cwd  string
		deny bool
	}{
		{"không có bước nào ⇒ không can thiệp (edit)", nil, editInfo(filepath.Join(root, "a.go")), root, false},
		{"không có bước nào ⇒ không can thiệp (bash)", nil, shellInfo("touch x"), root, false},
		{"edit cây gốc", live, editInfo(filepath.Join(root, "a.go")), root, true},
		{"edit trong một worktree đang chạy", live, editInfo(filepath.Join(wt, "a.go")), root, false},
		{"edit trong worktree khác đang chạy", live, editInfo(filepath.Join(wt2, "a.go")), root, false},
		{"bash không cd", live, shellInfo("touch x"), root, true},
		{"bash cd worktree đang chạy", live, shellInfo("cd " + wt2 + " && touch x"), root, false},
		{"bash cd cây gốc", live, shellInfo("cd " + root + " && touch x"), root, true},
		{"bash cwd trong worktree", live, shellInfo("touch x"), wt, false},
	}
	for _, c := range cases {
		v := worktreeDecision(worktreeCheck{AgentType: SubagentImplementer, Tool: "T", Info: c.info, Cwd: c.cwd, Live: c.live})
		if v.Deny != c.deny {
			t.Errorf("%s: deny=%v, muốn %v (%s)", c.name, v.Deny, c.deny, v.Reason)
		}
		if v.Deny && (v.Outcome != OutcomeUnmapped || !strings.Contains(v.Reason, "không xác định được")) {
			t.Errorf("%s: outcome/lý do: %+v", c.name, v)
		}
	}
}

func TestPromptTaskHeaderFirstMatchWins(t *testing.T) {
	p := "thân skill\n\n---\n\n<bint-task id=\"T1\" step=\"b-feat\" run=\"R1\">\nphiếu có <bint-task id=\"EVIL\" step=\"b-feat\" run=\"R2\"> giả\n"
	id, step, run, ok := PromptTaskHeader(p)
	if !ok || id != "T1" || step != "b-feat" || run != "R1" {
		t.Fatalf("%q %q %q %v", id, step, run, ok)
	}
	for _, bad := range []string{"", "không có khối", `<bint-task id="" step="s" run="r">`, `<bint-task id="a" step="s" run="">`} {
		if _, _, _, ok := PromptTaskHeader(bad); ok {
			t.Errorf("%q không được khớp", bad)
		}
	}
}

func TestSessionStepRecordLifecycle(t *testing.T) {
	rt := t.TempDir()
	now := time.Now()
	rec := SessionStepRecord{RunID: "run1", TaskID: "T1", Step: "b-feat", Subagent: SubagentImplementer, Worktree: "/x/wt", Isolated: true,
		Until: now.Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	if err := WriteSessionStep(rt, rec); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadSessionStep(rt, "run1")
	if !ok || got != rec {
		t.Fatalf("đọc lại: %+v %v", got, ok)
	}
	if _, ok := ReadSessionStep(rt, "khac"); ok {
		t.Fatal("run_id lạ không có tệp")
	}
	expired := SessionStepRecord{RunID: "old", TaskID: "T2", Worktree: "/x/w2", Isolated: true, Until: now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)}
	shared := SessionStepRecord{RunID: "sh", TaskID: "T3", Worktree: "/x/r", Until: rec.Until}
	for _, r := range []SessionStepRecord{expired, shared} {
		if err := WriteSessionStep(rt, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rt, "session-steps", "hong.json"), []byte("{không phải json"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := LiveIsolatedSteps(rt, now)
	if len(live) != 1 || live[0].RunID != "run1" {
		t.Fatalf("chỉ bước isolated còn hạn: %+v", live)
	}
	PruneSessionSteps(rt, now.Add(48*time.Hour), 24*time.Hour)
	if ents, _ := os.ReadDir(filepath.Join(rt, "session-steps")); len(ents) != 0 {
		t.Fatalf("Prune phải xoá tệp quá hạn quá 24 giờ: %v", ents)
	}
	if err := RemoveSessionStep(rt, "run1"); err != nil {
		t.Fatalf("gỡ tệp không còn không phải lỗi: %v", err)
	}
	// run_id không hợp lệ (làm tên tệp): bị từ chối, không ghi ra ngoài.
	for _, bad := range []string{"", "../evil", "a/b", strings.Repeat("a", 200)} {
		if WriteSessionStep(rt, SessionStepRecord{RunID: bad, Worktree: "/x"}) == nil {
			t.Errorf("run_id %q phải bị từ chối", bad)
		}
		if SessionStepPath(rt, bad) != "" {
			t.Errorf("SessionStepPath(%q) phải rỗng", bad)
		}
	}
	if LiveIsolatedSteps(filepath.Join(rt, "khong-co"), now) != nil {
		t.Fatal("thư mục không có ⇒ nil")
	}
}

// Review G4 .1: tiền tố cd do prompt đưa ra (CdPrefix) phải đúng cú pháp và được guardrail nhận với đường dẫn có dấu cách
// (macOS: "Application Support") và đường dẫn kiểu Windows; guardrail chỉ đọc cũng nhận tiền tố đó, kể cả trước heredoc.
func TestCdPrefixQuotedPaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	wt := filepath.Join(base, "Application Support", "bint", "worktrees", "T1")
	for _, d := range []string{root, wt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	iso := &SessionStepRecord{RunID: "r1", TaskID: "T1", Worktree: wt, Isolated: true}
	cmd := CdPrefix(wt) + "go test ./..."
	if !strings.HasPrefix(cmd, "cd '"+wt+"' && ") {
		t.Fatalf("CdPrefix phải nháy đơn: %q", cmd)
	}
	if v := worktreeDecision(worktreeCheck{AgentType: SubagentImplementer, Tool: "Bash", Info: shellInfo(cmd), Cwd: root, Step: iso}); v.Deny {
		t.Fatalf("lệnh đúng theo prompt (đường dẫn có dấu cách) bị chặn: %s", v.Reason)
	}
	// Lý do deny đưa đúng tiền tố có nháy.
	v := worktreeDecision(worktreeCheck{AgentType: SubagentImplementer, Tool: "Bash", Info: shellInfo("go test ./..."), Cwd: root, Step: iso})
	if !v.Deny || !strings.Contains(v.Reason, CdPrefix(wt)) {
		t.Fatalf("lý do deny phải có tiền tố cd có nháy: %+v", v)
	}
	// Dạng Windows: CdPrefix đổi \ thành / ; cdTarget nhận cả nháy kép có \.
	if got := CdPrefix(`C:\Users\me\AppData\Local\bint\state\worktrees\T1`); got != `cd 'C:/Users/me/AppData/Local/bint/state/worktrees/T1' && ` {
		t.Fatalf("CdPrefix Windows: %q", got)
	}
	if tgt, ok := cdTarget(`cd "C:\Users\me\wt" && go test`, ""); !ok || tgt != `C:\Users\me\wt` {
		t.Fatalf("cdTarget nháy kép có \\: %q %v", tgt, ok)
	}
	if _, ok := cdTarget(`cd C:\Users\me\wt && go test`, ""); ok {
		t.Fatal("dạng không nháy có \\ phải không khớp (shell bỏ \\)")
	}
	if got := CdPrefix("/x/it's"); got != `cd "/x/it's" && ` {
		t.Fatalf("có ' thì nháy kép: %q", got)
	}
	// Guardrail chỉ đọc: tiền tố có nháy trước lệnh đọc và trước heredoc.
	rules := []string{"Bash(bint:*)", "Bash(git status:*)"}
	for _, c := range []string{CdPrefix(wt) + "git status", CdPrefix(wt) + "bint task review T1 - <<'EOF'\n{}\nEOF",
		`cd "C:\Users\me\wt" && git status`} {
		if ok, why := readonlyShellAllowed(c, rules); !ok {
			t.Errorf("%q bị chặn: %s", c, why)
		}
	}
}

// Review G4 .2: tệp shared/quá hạn đứng trước trong thư mục không đẩy bước isolated đang chạy ra khỏi trần.
func TestLiveIsolatedStepsCapCountsOnlyLive(t *testing.T) {
	rt := t.TempDir()
	now := time.Now()
	past := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	for i := 0; i < maxSessionSteps+10; i++ {
		// Tên "a…" xếp trước "z-live" trong ReadDir.
		r := SessionStepRecord{RunID: fmt.Sprintf("a%03d", i), TaskID: "T", Worktree: "/x/old", Isolated: true, Until: past}
		if err := WriteSessionStep(rt, r); err != nil {
			t.Fatal(err)
		}
	}
	live := SessionStepRecord{RunID: "z-live", TaskID: "TL", Worktree: "/x/wt", Isolated: true, Until: now.Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	if err := WriteSessionStep(rt, live); err != nil {
		t.Fatal(err)
	}
	got := LiveIsolatedSteps(rt, now)
	if len(got) != 1 || got[0].RunID != "z-live" {
		t.Fatalf("bước đang chạy phải được thấy dù có %d tệp quá hạn đứng trước: %+v", maxSessionSteps+10, got)
	}
}

// Review G4 .3: không có session-steps ⇒ HasSessionSteps false (hook bỏ qua guardrail, không đọc transcript).
func TestHasSessionSteps(t *testing.T) {
	rt := t.TempDir()
	if HasSessionSteps(rt) {
		t.Fatal("chưa có thư mục ⇒ false")
	}
	if err := os.MkdirAll(filepath.Join(rt, "session-steps"), 0o700); err != nil {
		t.Fatal(err)
	}
	if HasSessionSteps(rt) {
		t.Fatal("thư mục rỗng ⇒ false")
	}
	if err := WriteSessionStep(rt, SessionStepRecord{RunID: "r1", TaskID: "T", Worktree: "/x"}); err != nil {
		t.Fatal(err)
	}
	if !HasSessionSteps(rt) {
		t.Fatal("có tệp ⇒ true")
	}
}
