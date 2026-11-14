package task

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/testutil"
)

func TestFromRequestAndClassify(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"internal/db/db.go": "package db\n", "README.md": "x\n"})
	tk := FromRequest(root, "Sửa lỗi database is locked trong internal/db và README.md, không chạm migrations/**\nchi tiết thêm", "go test ./...")
	if tk.Goal != "Sửa lỗi database is locked trong internal/db và README.md, không chạm migrations/**" || tk.Request == "" {
		t.Fatalf("goal/request: %+v", tk)
	}
	if !slices.Equal(tk.Scope.Allow, []string{"internal/db/**", "README.md", "migrations/**"}) {
		t.Fatalf("scope: %v", tk.Scope.Allow)
	}
	if tk.Verify.Command != "go test ./..." || tk.Autonomy != AutonomyAskOnProtected {
		t.Fatalf("verify/autonomy: %+v", tk)
	}
	if c := Classify("Sửa lỗi chính tả trong README"); c.Plan {
		t.Fatalf("việc nhỏ không cần b-req: %+v", c)
	}
	if c := Classify("Thêm tính năng xuất báo cáo CSV cho trang quản trị"); !c.Plan {
		t.Fatalf("tính năng cần b-req: %+v", c)
	}
	if c := Classify("a\nb\nc"); !c.Plan {
		t.Fatal("nhiều dòng cần b-req")
	}
}

func TestClassifyVietnameseAndPlanWords(t *testing.T) {
	for req, plan := range map[string]bool{
		"Thực hiện plan docs/tui-plan.md":                      true,
		"Triển khai kế hoạch mới":                              true,
		"implementation of the roadmap":                        true,
		"đổi tên hàm Foo thành Bar\nở cả hai gói\nvà kiểm thử": false, // smallWords bắt đầu bằng chữ có dấu
		"Sửa lỗi chính tả\ntrong README\nvà docs":              false, // smallWords kết thúc bằng chữ có dấu
		"Sửa lỗi nil trong handler":                            false,
		"planner chạy chậm":                                    false, // "plan" phải là từ trọn vẹn
	} {
		if c := Classify(req); c.Plan != plan {
			t.Errorf("%q: plan=%v (%s), muốn %v", req, c.Plan, c.Reason, plan)
		}
	}
	if c := Classify("Làm tiếp", "docs/p.md"); !c.Plan {
		t.Fatalf("có tài liệu đầu vào thì cần b-req: %+v", c)
	}
}

func TestFromRequestReferences(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"docs/tui-plan.md": "x\n", "docs/guide.md": "x\n", "internal/db/db.go": "package db\n"})
	tk := FromRequest(root, "Thực hiện plan @docs/tui-plan.md cho internal/db", "")
	if !slices.Equal(tk.References, []string{"docs/tui-plan.md"}) || !slices.Equal(tk.Scope.Allow, []string{"internal/db/**"}) {
		t.Fatalf("refs %v scope %v", tk.References, tk.Scope.Allow)
	}
	// Sửa chính tài liệu (việc nhỏ hoặc không có từ kế hoạch): tài liệu là phạm vi sửa.
	for _, req := range []string{"Sửa chính tả trong docs/guide.md", "Cập nhật docs/guide.md mục cài đặt"} {
		tk = FromRequest(root, req, "")
		if len(tk.References) != 0 || !slices.Equal(tk.Scope.Allow, []string{"docs/guide.md"}) {
			t.Fatalf("%q: refs %v scope %v", req, tk.References, tk.Scope.Allow)
		}
	}
	// Tài liệu đầu vào nằm trong đường dẫn bảo vệ không làm tác vụ thành rủi ro cao.
	tk = FromRequest(root, "Thực hiện plan docs/tui-plan.md", "")
	if a := Assess(tk, []string{"docs/**"}, false); a.Risk != "low" {
		t.Fatalf("%+v", a)
	}
}

func TestAssess(t *testing.T) {
	prot := []string{"migrations/**", ".github/workflows/**"}
	a := Assess(Ticket{Goal: "Thêm cột email", Scope: Scope{Allow: []string{"migrations/**", "internal/db/**"}}}, prot, false)
	if a.Risk != "high" || len(a.Questions) != 1 || !slices.Equal(a.Protected, []string{"migrations/**"}) {
		t.Fatalf("rủi ro cao thiếu kiểm chứng phải hỏi: %+v", a)
	}
	a = Assess(Ticket{Goal: "Sửa .github/workflows/ci.yml cho đúng phiên bản Go", Verify: Verify{Command: "make ci"}}, prot, false)
	if a.Risk != "high" || len(a.Questions) != 0 {
		t.Fatalf("có lệnh kiểm chứng thì không hỏi: %+v", a)
	}
	a = Assess(Ticket{Goal: "Đổi tên biến", Scope: Scope{Allow: []string{"internal/x/**"}}}, prot, false)
	if a.Risk != "low" || len(a.Questions) != 0 {
		t.Fatalf("rủi ro thấp: %+v", a)
	}
}

func TestCheckScope(t *testing.T) {
	tk := Ticket{Scope: Scope{Allow: []string{"internal/db/**"}, Deny: []string{"internal/db/gen/**"}}}
	c := CheckScope([]string{"internal/db/a.go", "internal/db/gen/x.go", "migrations/001.sql", "cmd/main.go"}, tk, []string{"migrations/**"})
	if !slices.Equal(c.Protected, []string{"migrations/001.sql"}) || !slices.Equal(c.Denied, []string{"internal/db/gen/x.go"}) ||
		!slices.Equal(c.OutsideAllow, []string{"cmd/main.go"}) || len(c.NeedsApproval()) != 2 {
		t.Fatalf("%+v", c)
	}
}

func newStore(t *testing.T) Store { return Store{Dir: t.TempDir()} }

func TestTransitionsAndDoneBlockers(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	tk, err := s.Create(Ticket{Goal: "g", Verify: Verify{Command: "true", Criteria: []string{"c1"}}}, State{Workflow: "b-do"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(Ticket{Goal: " "}, State{}, now); err == nil {
		t.Fatal("phiếu thiếu goal phải lỗi")
	}
	st := &tk.State
	if err := st.Apply(StatusDone, "x", "", Now(now)); err == nil {
		t.Fatal("created → done không hợp lệ")
	}
	for _, to := range []string{StatusPlanned, StatusInProgress, StatusBlocked} {
		if err := st.Apply(to, "x", "r", Now(now)); err != nil {
			t.Fatal(err)
		}
	}
	if st.ResumeFrom != StatusInProgress {
		t.Fatalf("resume_from = %q", st.ResumeFrom)
	}
	st.Apply(StatusInProgress, "x", "", Now(now))
	if st.ResumeFrom != "" || len(st.History) != 5 {
		t.Fatalf("%+v", st)
	}

	in := DoneInput{Worktree: "fp1", VerifyConfirmed: false}
	if b := DoneBlockers(tk, in); len(b) != 1 || !strings.Contains(b[0], "chưa được người xác nhận") {
		t.Fatalf("blockers: %v", b)
	}
	in.VerifyConfirmed = true
	st.Verifications = append(st.Verifications, Verification{Command: "true", Passed: true, Worktree: "fp0"})
	if b := DoneBlockers(tk, in); len(b) != 1 || !strings.Contains(b[0], "mã đã thay đổi") {
		t.Fatalf("mã đổi sau kiểm chứng: %v", b)
	}
	st.Verifications[0].Worktree = "fp1"
	if b := DoneBlockers(tk, in); len(b) != 0 {
		t.Fatalf("đủ bằng chứng: %v", b)
	}
	st.Criteria = append(st.Criteria, CriterionCheck{Index: 1, Met: false, Evidence: "x"})
	if b := DoneBlockers(tk, in); len(b) != 1 || !strings.Contains(b[0], "tiêu chí 1 chưa đạt") {
		t.Fatalf("tiêu chí chưa đạt phải chặn: %v", b)
	}
	st.PendingPaths = []string{"migrations/1.sql"}
	if b := DoneBlockers(tk, in); len(b) != 2 {
		t.Fatalf("đường dẫn chờ duyệt phải chặn: %v", b)
	}

	// Không có lệnh kiểm chứng: cần mọi tiêu chí đạt; không có gì thì không có bằng chứng.
	tk2 := &Task{Ticket: Ticket{Goal: "g", Verify: Verify{Criteria: []string{"a", "b"}}}}
	tk2.State.Criteria = []CriterionCheck{{Index: 1, Met: true}}
	if b := DoneBlockers(tk2, DoneInput{}); len(b) != 1 || !strings.Contains(b[0], "tiêu chí 2 chưa được đối chiếu") {
		t.Fatalf("%v", b)
	}
	if b := DoneBlockers(&Task{Ticket: Ticket{Goal: "g"}}, DoneInput{}); len(b) != 1 {
		t.Fatalf("không bằng chứng: %v", b)
	}
}

func TestStoreUpdateResolveAndReconcile(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	a, _ := s.Create(Ticket{Goal: "a"}, State{}, now)
	if got, err := s.Resolve(strings.ToLower(a.State.ID[:20])); err != nil || got != a.State.ID {
		t.Fatalf("resolve: %v %v", got, err)
	}
	// Phiếu sửa tay sai cú pháp/khoá lạ bị từ chối khi nạp.
	os.WriteFile(filepath.Join(a.Dir, TicketFile), []byte("goal: x\nbogus: 1\n"), 0o600)
	if _, err := s.Load(a.State.ID); err == nil {
		t.Fatal("khoá lạ phải lỗi")
	}
	b, _ := s.Create(Ticket{Goal: "b"}, State{}, now)
	upd, err := s.Update(b.State.ID, now, func(t *Task) error {
		t.Ticket.Verify.Command = "make test"
		return t.State.Apply(StatusInProgress, "x", "", Now(now))
	})
	if err != nil || upd.Ticket.Verify.Command != "make test" {
		t.Fatal(err)
	}
	re, _ := s.Load(b.State.ID)
	if re.Ticket.Verify.Command != "make test" || re.State.Status != StatusInProgress {
		t.Fatalf("không được lưu: %+v", re)
	}
	// Tiến trình điều phối đã chết → interrupted.
	s.Update(b.State.ID, now, func(t *Task) error {
		host, _ := os.Hostname()
		t.State.Runner = &Runner{PID: 999999999, Host: host, RunID: "R1"}
		return nil
	})
	re, _ = s.Load(b.State.ID)
	if run, changed := re.Reconcile(now); !changed || run != "R1" || re.State.Status != StatusInterrupted || re.State.ResumeFrom != StatusInProgress {
		t.Fatalf("reconcile: %v %v %+v", run, changed, re.State)
	}
	// Tiến trình còn sống (chính mình) thì không đổi.
	re.ClaimRunner("R2", now)
	if _, changed := re.Reconcile(now); changed {
		t.Fatal("tiến trình còn sống không được đổi")
	}
}

func TestReviewSubmission(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"a.go": "l1\nl2\nl3\n"})
	sub, errs := ParseSubmission([]byte(`{"summary":"ok","findings":[
		{"kind":"bug","severity":"high","title":"t","path":"a.go","lines":"2-3","evidence":"e"},
		{"kind":"suggestion","severity":"low","title":"s","evidence":"e"}]}`), root)
	if len(errs) != 0 || len(sub.Findings) != 2 {
		t.Fatalf("%v", errs)
	}
	_, errs = ParseSubmission([]byte(`{"summary":"","findings":[{"kind":"nit","severity":"x","title":"","path":"a.go","lines":"2-9","evidence":""}]}`), root)
	if len(errs) != 6 {
		t.Fatalf("phải có 6 lỗi: %v", errs)
	}
	if _, errs = ParseSubmission([]byte(`{"summary":"x","extra":1}`), root); len(errs) != 1 {
		t.Fatal("khoá lạ phải lỗi")
	}

	s := newStore(t)
	tk, _ := s.Create(Ticket{Goal: "g"}, State{}, time.Now())
	r1, err := tk.AddRound(sub, "agent", "RUN1", "t")
	if err != nil || r1.Round != 1 || r1.Findings[0].ID != tk.State.ID+".1" || len(r1.Blocking()) != 1 {
		t.Fatalf("%+v %v", r1, err)
	}
	r2, _ := tk.AddRound(Submission{Summary: "sạch"}, "agent", "RUN2", "t")
	if r2.Round != 2 || len(r2.Findings) != 0 {
		t.Fatalf("%+v", r2)
	}
	rev, _ := tk.LoadReview()
	if f, round := rev.Find(tk.State.ID + ".2"); f == nil || round.RunID != "RUN1" || f.Kind != "suggestion" {
		t.Fatal("không tìm thấy phát hiện")
	}
	md, _ := os.ReadFile(filepath.Join(tk.Dir, ReviewMD))
	if !strings.Contains(string(md), "["+tk.State.ID+".1] bug/high") || !strings.Contains(string(md), "Không có phát hiện") {
		t.Fatalf("review.md:\n%s", md)
	}
	if ref, ok := SplitFindingID(tk.State.ID[:10] + ".2"); !ok || ref != tk.State.ID[:10] {
		t.Fatal("split")
	}
}

func TestChangedFilesAndFingerprint(t *testing.T) {
	ctx := context.Background()
	root := testutil.NewRepo(t, map[string]string{"a.go": "x\n"})
	base := testutil.Git(t, root, "rev-parse", "HEAD")
	fp0 := WorktreeFingerprint(ctx, root)
	os.WriteFile(filepath.Join(root, "a.go"), []byte("y\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "migrations"), 0o755)
	os.WriteFile(filepath.Join(root, "migrations", "1 init.sql"), []byte("--\n"), 0o644)
	files, err := ChangedFiles(ctx, root, base)
	if err != nil || !slices.Equal(files, []string{"a.go", "migrations/1 init.sql"}) {
		t.Fatalf("%v %v", files, err)
	}
	fp1 := WorktreeFingerprint(ctx, root)
	if fp1 == fp0 || fp1 == "" {
		t.Fatal("fingerprint phải đổi khi mã đổi")
	}
	os.WriteFile(filepath.Join(root, "migrations", "1 init.sql"), []byte("-- khác\n"), 0o644)
	if WorktreeFingerprint(ctx, root) == fp1 {
		t.Fatal("fingerprint phải đổi khi tệp chưa theo dõi đổi")
	}
	testutil.Git(t, root, "add", "-A")
	testutil.Git(t, root, "commit", "-q", "-m", "c")
	if files, _ := ChangedFiles(ctx, root, base); len(files) != 2 {
		t.Fatalf("commit mới vẫn tính so với base: %v", files)
	}
}

// Agent trong sandbox (không gian PID riêng) không thấy PID của runner: tiến trình
// con của chính runner đó (BINT_RUNNER_PID) không được đánh dấu tác vụ interrupted.
func TestRunnerAliveFromSandboxedChild(t *testing.T) {
	host, _ := os.Hostname()
	r := &Runner{PID: 2147483646, Host: host, RunID: "r1"}
	t.Setenv(EnvRunnerPID, "")
	if RunnerAlive(r) {
		t.Fatal("PID không tồn tại phải coi là chết")
	}
	t.Setenv(EnvRunnerPID, "2147483646")
	if !RunnerAlive(r) {
		t.Fatal("tiến trình con của runner phải coi runner còn sống")
	}
	t.Setenv(EnvRunnerPID, "123")
	if RunnerAlive(r) {
		t.Fatal("runner khác không được coi là còn sống")
	}
}

// Claude Code 2.1.322 thật chặn heredoc JSON của `bint task review` với lỗi
// "Contains brace with quote character (expansion obfuscation)" (testdata/claude-2.1.322).
// Dạng YAML block không có `{"` phải cho cùng kết quả; JSON giữ nguyên hành vi.
func TestParseSubmissionYAMLBlockEquivalentToClaudeJSON(t *testing.T) {
	dir := filepath.Join("testdata", "claude-2.1.322")
	cmd, err := os.ReadFile(filepath.Join(dir, "review-denied-command.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cmd), `{"`) {
		t.Fatal("fixture lệnh bị Claude Code chặn phải chứa {\"")
	}
	jsonData, _ := os.ReadFile(filepath.Join(dir, "review-denied-heredoc.json"))
	yamlData, err := os.ReadFile(filepath.Join(dir, "review-brace-free.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yamlData), `{"`) || strings.Contains(string(yamlData), "{") {
		t.Fatal("dạng YAML không được chứa ngoặc nhọn")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "calc.go"), []byte(strings.Repeat("x\n", 12)), 0o644); err != nil {
		t.Fatal(err)
	}
	fromJSON, errs := ParseSubmission(jsonData, root)
	if len(errs) != 0 {
		t.Fatalf("JSON thật: %v", errs)
	}
	fromYAML, errs := ParseSubmission(yamlData, root)
	if len(errs) != 0 {
		t.Fatalf("YAML: %v", errs)
	}
	if !reflect.DeepEqual(fromJSON, fromYAML) || len(fromYAML.Findings) != 1 || fromYAML.Findings[0].Lines != "10-11" {
		t.Fatalf("JSON %+v khác YAML %+v", fromJSON, fromYAML)
	}
	// YAML vẫn bị kiểm tra như JSON: trường lạ và dữ liệu thiếu bị từ chối.
	if _, errs := ParseSubmission([]byte("summary: x\nextra: 1\n"), root); len(errs) != 1 {
		t.Fatalf("trường lạ: %v", errs)
	}
	if _, errs := ParseSubmission([]byte("summary: ok\nfindings:\n  - kind: nit\n    severity: x\n"), root); len(errs) < 3 {
		t.Fatalf("thiếu kiểm tra: %v", errs)
	}
	if _, errs := ParseSubmission([]byte("không phải review\n"), root); len(errs) != 1 {
		t.Fatalf("rác: %v", errs)
	}
}

// TSK-10: khi mã đổi sau lần kiểm chứng, thông báo liệt kê tệp ngoài tác vụ nếu có; không thì giữ câu cũ.
func TestDoneBlockersOutsideSinceVerify(t *testing.T) {
	tk := &Task{Ticket: Ticket{Goal: "g", Verify: Verify{Command: "true"}}, State: State{ID: "T1"}}
	tk.State.Verifications = []Verification{{Command: "true", Passed: true, Worktree: "fp0"}}
	in := DoneInput{Worktree: "fp1", VerifyConfirmed: true}

	if b := DoneBlockers(tk, in); len(b) != 1 || !strings.Contains(b[0], "mã đã thay đổi sau lần kiểm chứng cuối") {
		t.Fatalf("không có tệp ngoài: giữ câu cũ: %v", b)
	}
	in.OutsideSinceVerify = []string{"a.go", "b/c.go"}
	b := DoneBlockers(tk, in)
	if len(b) != 1 || !strings.Contains(b[0], "thay đổi không thuộc tác vụ: a.go, b/c.go") || !strings.Contains(b[0], "chạy lại bint task verify T1") {
		t.Fatalf("có tệp ngoài: %v", b)
	}
	// Mã không đổi thì không chặn dù OutsideSinceVerify có dữ liệu.
	tk.State.Verifications[0].Worktree = "fp1"
	if b := DoneBlockers(tk, in); len(b) != 0 {
		t.Fatalf("%v", b)
	}
}

func TestStateTracksTouchedAndEverInProgress(t *testing.T) {
	var s State
	if s.TracksTouched() || s.EverInProgress() {
		t.Fatal("trạng thái rỗng (phiếu cũ) không theo dõi")
	}
	s.BaseTree = "abc"
	if !s.TracksTouched() {
		t.Fatal("có BaseTree thì theo dõi")
	}
	s.History = []Transition{{From: StatusCreated, To: StatusPlanned}}
	if s.EverInProgress() {
		t.Fatal("planned chưa phải in_progress")
	}
	s.History = append(s.History, Transition{From: StatusPlanned, To: StatusInProgress})
	if !s.EverInProgress() {
		t.Fatal("đã vào in_progress")
	}
}

func TestClassificationEffectiveIsolation(t *testing.T) {
	for in, want := range map[string]string{"": IsolationShared, IsolationShared: IsolationShared, IsolationWorktree: IsolationWorktree, "lạ": IsolationShared} {
		if got := (Classification{Isolation: in}).EffectiveIsolation(); got != want {
			t.Errorf("%q → %q, mong %q", in, got, want)
		}
	}
	// State không có Classification (phiếu cũ) là shared.
	if got := (&State{}).Isolation(); got != IsolationShared {
		t.Fatalf("phiếu cũ: %q", got)
	}
	// JSON: trường mới omitempty nên phiếu cũ đọc được và không ghi trường rỗng.
	b, _ := json.Marshal(Classification{Plan: true, Reason: "x"})
	if strings.Contains(string(b), "isolation") {
		t.Fatalf("trường cô lập rỗng không được ghi ra: %s", b)
	}
	var back Classification
	in := Classification{Plan: true, Reason: "x", Isolation: IsolationWorktree, IsolationReason: IsolationReasonConcurrent, IsolationTasks: []string{"A", "B"}}
	b, _ = json.Marshal(in)
	if json.Unmarshal(b, &back) != nil || !reflect.DeepEqual(back, in) {
		t.Fatalf("round-trip: %s → %+v", b, back)
	}
}

// Tác vụ worktree chưa đưa về không được done; IgnoreLanding dùng cho việc kiểm trước khi đưa về; đã đưa về thì qua.
func TestDoneBlockersNeedsLanding(t *testing.T) {
	tk := &Task{Ticket: Ticket{Goal: "g", Verify: Verify{Criteria: []string{"c1"}}}, State: State{ID: "T1", Origin: "/gốc"}}
	tk.State.Criteria = []CriterionCheck{{Index: 1, Met: true, Evidence: "e"}}
	if !tk.State.NeedsLand() || !tk.State.InWorktree() {
		t.Fatal("có Origin, chưa Landed ⇒ cần đưa về")
	}
	b := DoneBlockers(tk, DoneInput{})
	if len(b) != 1 || !strings.Contains(b[0], "chưa đưa thay đổi về cây gốc /gốc") || !strings.Contains(b[0], "bint task land T1") {
		t.Fatalf("chưa đưa về: %v", b)
	}
	if b := DoneBlockers(tk, DoneInput{IgnoreLanding: true}); len(b) != 0 {
		t.Fatalf("IgnoreLanding: %v", b)
	}
	tk.State.Landed = &Landing{At: "t", By: "x"}
	if tk.State.NeedsLand() || tk.State.InWorktree() {
		t.Fatal("đã Landed thì không cần đưa về nữa")
	}
	if b := DoneBlockers(tk, DoneInput{}); len(b) != 0 {
		t.Fatalf("đã đưa về: %v", b)
	}
	// Tác vụ shared (Origin rỗng) không bao giờ cần đưa về.
	if (&State{}).NeedsLand() {
		t.Fatal("shared không cần đưa về")
	}
}

func TestMergedTaskStateRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	want := State{AgentChoices: map[string]AgentChoice{"implementer": {Harness: "codex", Model: ""}}, BaseTree: "tree", Touched: []string{"app.txt"}, PendingStep: &PendingStep{RunID: "child", Step: "b-feat"}, SessionSpend: &SessionSpend{RunID: "parent", Cost: 1.25, Turns: 3}}
	created, err := s.Create(Ticket{Goal: "test"}, want, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(created.State.ID, time.Now(), func(tk *Task) error { tk.State.Reason = "updated"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(created.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	st := got.State
	if !reflect.DeepEqual(st.AgentChoices, want.AgentChoices) || st.BaseTree != want.BaseTree || !reflect.DeepEqual(st.Touched, want.Touched) || !reflect.DeepEqual(st.PendingStep, want.PendingStep) || !reflect.DeepEqual(st.SessionSpend, want.SessionSpend) {
		t.Fatalf("state lost fields: %+v", st)
	}
}

// NT-WF-BASELINE: lời nhắc tiếp tục theo driver; state.json cũ (Verification không có reason) vẫn nạp.
func TestStateResumeCommandFollowsDriver(t *testing.T) {
	s := State{ID: "T1"}
	if got := s.ResumeCommand(); got != "bint run resume T1" {
		t.Fatalf("chưa phân loại (phiếu cũ): %q", got)
	}
	s.Classification = &Classification{Driver: DriverProcess}
	if got := s.ResumeCommand(); got != "bint run resume T1" {
		t.Fatalf("process: %q", got)
	}
	s.Classification = &Classification{Driver: DriverSession}
	if got := s.ResumeCommand(); got != "bint run next T1 --resume" {
		t.Fatalf("session: %q", got)
	}
}

func TestDoneBlockersLandConflictResumeHintFollowsDriver(t *testing.T) {
	tk := &Task{Ticket: Ticket{Goal: "g", Verify: Verify{Command: "true"}}, State: State{ID: "T1"}}
	in := DoneInput{LandConflicts: []string{"a.go"}}
	if b := strings.Join(DoneBlockers(tk, in), "\n"); !strings.Contains(b, "bint run resume T1") || strings.Contains(b, "bint run next") {
		t.Fatalf("process: %s", b)
	}
	tk.State.Classification = &Classification{Driver: DriverSession}
	if b := strings.Join(DoneBlockers(tk, in), "\n"); !strings.Contains(b, "bint run next T1 --resume") || strings.Contains(b, "bint run resume") {
		t.Fatalf("session: %s", b)
	}
}

func TestVerificationReasonBackwardCompatible(t *testing.T) {
	var v Verification
	if err := json.Unmarshal([]byte(`{"at":"x","command":"true","confirmed":true,"exit_code":0,"passed":true,"by":"agent"}`), &v); err != nil || v.Reason != "" {
		t.Fatalf("state.json cũ: %+v %v", v, err)
	}
	b, _ := json.Marshal(Verification{Command: "true"})
	if strings.Contains(string(b), "reason") {
		t.Fatalf("reason rỗng không được ghi ra: %s", b)
	}
	b, _ = json.Marshal(Verification{Command: "true", Reason: "mã khác đổi sau lần kiểm chứng: a.go"})
	if !strings.Contains(string(b), `"reason":"mã khác đổi sau lần kiểm chứng: a.go"`) {
		t.Fatalf("%s", b)
	}
}

// path của phát hiện thành scope.allow của phiếu sửa (--from-findings): không được có ký tự glob hay đoạn "..";
// tiêu đề được gộp về một dòng.
func TestReviewSubmissionPathAndTitle(t *testing.T) {
	root := testutil.NewRepo(t, map[string]string{"a.go": "l1\n"})
	for _, p := range []string{"**", "src/*.go", "a/../../x", "a/[ab].go", "C:/x"} {
		body := `{"summary":"x","findings":[{"kind":"bug","severity":"low","title":"t","path":"` + p + `","evidence":"e"}]}`
		if _, errs := ParseSubmission([]byte(body), root); len(errs) != 1 || !strings.Contains(errs[0], ".path") {
			t.Errorf("path %q phải bị từ chối: %v", p, errs)
		}
	}
	sub, errs := ParseSubmission([]byte(`{"summary":"x","findings":[{"kind":"bug","severity":"low","title":"dòng một\n- [X.9] giả","path":"a.go","evidence":"e"}]}`), root)
	if len(errs) != 0 || sub.Findings[0].Title != "dòng một - [X.9] giả" {
		t.Fatalf("tiêu đề phải một dòng: %q %v", sub.Findings[0].Title, errs)
	}
}
