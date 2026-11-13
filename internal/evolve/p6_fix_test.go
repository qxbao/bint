package evolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
)

// ---- P6-03: Rollback lỗi giữa chừng khôi phục các tệp đã ghi ----

func rollbackMergeFixture(t *testing.T) (*fixture, *Proposal) {
	t.Helper()
	f := setup(t)
	a := f.item(t, "fact", "verified", "Database open requires context", []string{"internal/db/**"}, "internal/db/db.go:3")
	b := f.item(t, "fact", "verified", "Database open requires a context", []string{"internal/db/**"}, "internal/db/db.go:3", "internal/db/db.go:5")
	res, h := f.scan(t)
	key := "duplicate:" + a.ID + "+" + b.ID
	op, ok := byKey(res)[key]
	if !ok {
		t.Fatalf("không có cơ hội %s", key)
	}
	rep, err := Propose(context.Background(), ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Now: now})
	if err != nil || len(rep.Outcomes) != 1 || rep.Outcomes[0].Proposal == nil {
		t.Fatalf("đề xuất: %+v %v", rep, err)
	}
	p := rep.Outcomes[0].Proposal
	if len(p.Changes) != 2 {
		t.Fatalf("cần hai thay đổi tệp, được %v", p.Changes)
	}
	return f, p
}

func TestRollbackFailureMidwayRestoresAppliedState(t *testing.T) {
	f, p := rollbackMergeFixture(t)
	ctx := context.Background()
	a, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, ConfirmRisk: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Files) != 2 {
		t.Fatalf("cần 2 tệp, được %d", len(a.Files))
	}
	afterApply := map[string]string{}
	for _, ch := range a.Files {
		b, err := os.ReadFile(filepath.Join(f.k.Root, ch.Path))
		if err != nil {
			t.Fatal(err)
		}
		afterApply[ch.Path] = string(b)
	}
	// Tiêm lỗi: tệp thứ hai ghi dở (đã cắt về một nửa) rồi lỗi, như đĩa đầy giữa lúc ghi.
	second := a.Files[1].Path
	old := writeFile
	t.Cleanup(func() { writeFile = old })
	injected := false
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if name == filepath.Join(f.k.Root, second) && !injected {
			injected = true // chỉ lần ghi đầu: bước bù trừ ghi lại phải thành công
			old(name, data[:len(data)/2], perm)
			return errors.New("lỗi tiêm: đĩa đầy")
		}
		return old(name, data, perm)
	}
	_, err = Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: p.ID, Now: now.Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "lỗi tiêm") {
		t.Fatalf("cần lỗi ghi tệp thứ hai, được %v", err)
	}
	if !strings.Contains(err.Error(), "đã trả tệp về") {
		t.Fatalf("thông báo phải nói đã khôi phục: %v", err)
	}
	for path, want := range afterApply {
		b, err := os.ReadFile(filepath.Join(f.k.Root, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if string(b) != want {
			t.Fatalf("%s không còn nội dung sau apply sau khi quay lui lỗi:\n%s", path, b)
		}
	}
	cur, err := GetProposal(ctx, f.db, p.ID)
	if err != nil || cur.Status != StatusApplied {
		t.Fatalf("đề xuất phải còn applied: %v %v", cur, err)
	}
	// Bỏ lỗi tiêm: quay lui lại thành công, vậy trạng thái được khôi phục là trạng thái dùng được.
	writeFile = old
	if _, err := Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: p.ID, Now: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("quay lui lần sau phải thành công: %v", err)
	}
}

// Bù trừ cũng lỗi: báo rõ tệp nào không trả về được.
func TestRollbackCompensationFailureIsReported(t *testing.T) {
	f, p := rollbackMergeFixture(t)
	ctx := context.Background()
	a, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, ConfirmRisk: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(f.k.Root, a.Files[1].Path)
	old := writeFile
	t.Cleanup(func() { writeFile = old })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if name == second {
			return errors.New("lỗi tiêm")
		}
		return old(name, data, perm)
	}
	_, err = Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: p.ID, Now: now.Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "KHÔNG trả hết") || !strings.Contains(err.Error(), a.Files[1].Path) {
		t.Fatalf("phải báo bù trừ thất bại kèm tệp: %v", err)
	}
}

// ---- P6-02: hậu kiểm evolver so nội dung cây Git ----

// goAgent là agent giả viết bằng Go (không cần `sh`, chạy được trên Windows): gọi mutate (nếu có) rồi
// trả answer như câu trả lời cuối.
type goAgent struct {
	answer string
	mutate func()
	runs   int
}

func (a *goAgent) Name() string                   { return "go-agent" }
func (a *goAgent) Version(context.Context) string { return "" }
func (a *goAgent) ReportsCost() bool              { return false }
func (a *goAgent) Run(context.Context, eval.AgentRequest) eval.AgentResult {
	a.runs++
	if a.mutate != nil {
		a.mutate()
	}
	return eval.AgentResult{Text: a.answer}
}

// appendLine nối một dòng vào tệp (tạo nếu chưa có).
func appendLine(t *testing.T, path, line string) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// makeDirty thêm một chỉnh sửa có sẵn của chủ dự án vào internal/core/x.go (tệp theo dõi, thành M).
func makeDirty(t *testing.T, f *fixture) string {
	t.Helper()
	file := filepath.Join(f.root, "internal/core/x.go")
	appendLine(t, file, "// chỉnh sửa có sẵn của chủ dự án")
	return file
}

const failureAnswer = `{"proposals":[{"type":"warning","title":"Reset before Retry","body":"Retry requires prior Reset in this module.","paths":["internal/core/**"],"evidence":["internal/core/x.go:3-4#Retry"],"inputs":["internal/core/x.go"],"expected_benefit":"avoid repeated failures","reason":"local source evidence"}]}`

// failureOpportunity dựng cơ hội failure:internal/core để Propose gọi agent evolver.
func failureOpportunity(t *testing.T, f *fixture) (ScanResult, *History, Opportunity) {
	t.Helper()
	core := []string{"internal/core/x.go"}
	f.run(t, "R1", 5*time.Hour, "good", core, nil, nil)
	f.run(t, "R2", 4*time.Hour, "bad", core, nil, nil)
	f.run(t, "R3", 3*time.Hour, "bad", core, nil, nil)
	res, h := f.scan(t)
	op, ok := byKey(res)["failure:internal/core"]
	if !ok {
		t.Fatalf("không có cơ hội failure:internal/core")
	}
	return res, h, op
}

func TestEvolverMutationOfAlreadyDirtyFileRejected(t *testing.T) {
	f := setup(t)
	file := makeDirty(t, f)
	res, h, op := failureOpportunity(t, f)
	agent := &goAgent{answer: failureAnswer, mutate: func() { appendLine(t, file, "// evolver mutation") }}
	rep, err := Propose(context.Background(), ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Agent: agent, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "evolver mutation") {
		t.Fatal("agent giả không sửa tệp")
	}
	if len(rep.Outcomes) == 0 || rep.Outcomes[0].Proposal != nil {
		t.Fatalf("đề xuất của evolver đã sửa tệp bẩn phải bị loại: %+v", rep.Outcomes)
	}
	if sk := rep.Outcomes[0].Skipped; !strings.Contains(sk, "sửa cây làm việc") {
		t.Fatalf("lý do loại: %q", sk)
	}
}

// Cây vốn bẩn mà agent không sửa gì: không được báo nhầm là agent đã sửa cây.
func TestEvolverDirtyTreeUnchangedNotFlagged(t *testing.T) {
	f := setup(t)
	makeDirty(t, f)
	appendLine(t, filepath.Join(f.root, "chua-theo-doi.txt"), "tệp chưa theo dõi có sẵn")
	res, h, op := failureOpportunity(t, f)
	agent := &goAgent{answer: failureAnswer}
	rep, err := Propose(context.Background(), ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Agent: agent, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if agent.runs == 0 {
		t.Fatal("agent giả không được gọi")
	}
	if len(rep.Outcomes) != 1 || rep.Outcomes[0].Proposal == nil || rep.Outcomes[0].Skipped != "" {
		t.Fatalf("cây bẩn nhưng không đổi phải cho ra đề xuất: %+v", rep.Outcomes)
	}
}

func TestSkillEvolverMutationOfAlreadyDirtyFileRejected(t *testing.T) {
	f := setup(t)
	file := makeDirty(t, f)
	answer := `{"skills":[{"name":"retry-reset","description":"Khi sửa Retry","body":"Gọi Reset trước.","paths":["internal/core/**"],"evidence":["internal/core/x.go:3-4#Retry"],"inputs":["internal/core/x.go"],"expected_benefit":"x","reason":"y"}]}`
	agent := &goAgent{answer: answer, mutate: func() { appendLine(t, file, "// evolver mutation") }}
	_, h := f.scan(t)
	in := ProposeInput{K: f.k, DB: f.db, Agent: agent, History: h, Now: now}
	p, _, _, err := agentSkillProposal(context.Background(), in, Opportunity{Level: LevelL2, Kind: KindSkillCandidate, Summary: "x"}, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "sửa cây làm việc") || p != nil {
		t.Fatalf("evolver skill sửa tệp bẩn phải bị loại: %v %v", p, err)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "evolver mutation") {
		t.Fatal("agent giả không sửa tệp")
	}
}

func TestGuardReadOnly(t *testing.T) {
	x := func(f *fixture) string { return filepath.Join(f.root, "internal/core/x.go") }
	cases := []struct {
		name    string
		prepare func(t *testing.T, f *fixture) // cây trước khi chạy agent
		mutate  func(t *testing.T, f *fixture) // việc agent làm
		wantErr string                         // rỗng = không lỗi
		noRepo  bool
	}{
		{name: "cây sạch, không đổi"},
		{name: "cây vốn bẩn, không đổi",
			prepare: func(t *testing.T, f *fixture) { makeDirty(t, f) }},
		{name: "sửa tiếp tệp đã bẩn (vẫn M)", wantErr: "sửa cây làm việc",
			prepare: func(t *testing.T, f *fixture) { makeDirty(t, f) },
			mutate:  func(t *testing.T, f *fixture) { appendLine(t, x(f), "// thêm") }},
		{name: "sửa tệp theo dõi sạch", wantErr: "sửa cây làm việc",
			mutate: func(t *testing.T, f *fixture) { appendLine(t, x(f), "// thêm") }},
		{name: "sửa tệp chưa theo dõi có sẵn", wantErr: "sửa cây làm việc",
			prepare: func(t *testing.T, f *fixture) { appendLine(t, filepath.Join(f.root, "moi.txt"), "a") },
			mutate:  func(t *testing.T, f *fixture) { appendLine(t, filepath.Join(f.root, "moi.txt"), "b") }},
		{name: "tạo tệp mới", wantErr: "sửa cây làm việc",
			mutate: func(t *testing.T, f *fixture) { appendLine(t, filepath.Join(f.root, "tao-moi.txt"), "a") }},
		{name: "xoá tệp theo dõi", wantErr: "sửa cây làm việc",
			mutate: func(t *testing.T, f *fixture) {
				if err := os.Remove(filepath.Join(f.root, "internal/core/y.go")); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "ngoài kho Git", wantErr: "trước khi chạy agent evolver", noRepo: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			if tc.prepare != nil {
				tc.prepare(t, f)
			}
			dir := f.root
			if tc.noRepo {
				dir = t.TempDir()
			}
			calls := 0
			err := guardReadOnly(context.Background(), dir, func() {
				calls++
				if tc.mutate != nil {
					tc.mutate(t, f)
				}
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("không được lỗi: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("cần lỗi chứa %q, được %v", tc.wantErr, err)
			}
			if tc.noRepo && calls != 0 {
				t.Fatalf("không chụp được cây trước thì run không được gọi, gọi %d lần", calls)
			}
			if !tc.noRepo && calls != 1 {
				t.Fatalf("run phải được gọi đúng 1 lần, gọi %d", calls)
			}
		})
	}
}

// ---- Rollback không xoá nhầm tệp khi lỗi đọc (review .3) ----

func TestRollbackReadErrorDoesNotDeleteFile(t *testing.T) {
	f, p := rollbackMergeFixture(t)
	ctx := context.Background()
	a, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, ConfirmRisk: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(f.k.Root, a.Files[0].Path)
	second := filepath.Join(f.k.Root, a.Files[1].Path)
	want, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	oldRead, oldWrite := readFile, writeFile
	t.Cleanup(func() { readFile, writeFile = oldRead, oldWrite })
	reads := 0
	readFile = func(name string) ([]byte, error) {
		if name == first {
			reads++
			if reads >= 2 { // lần đọc lại (nếu có) lỗi: không được thành "tệp không có"
				return nil, errors.New("lỗi tiêm: không đọc được")
			}
		}
		return oldRead(name)
	}
	injected := false
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if name == second && !injected {
			injected = true // buộc nhánh undo chạy
			return errors.New("lỗi tiêm: đĩa đầy")
		}
		return oldWrite(name, data, perm)
	}
	_, err = Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: p.ID, Now: now.Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "lỗi tiêm") {
		t.Fatalf("cần lỗi ghi tệp thứ hai, được %v", err)
	}
	got, rerr := os.ReadFile(first)
	if rerr != nil {
		t.Fatalf("tệp thứ nhất bị xoá nhầm: %v", rerr)
	}
	if string(got) != string(want) {
		t.Fatalf("tệp thứ nhất không còn nội dung sau apply:\n%s", got)
	}
	if cur, err := GetProposal(ctx, f.db, p.ID); err != nil || cur.Status != StatusApplied {
		t.Fatalf("đề xuất phải còn applied: %v %v", cur, err)
	}
}

// .bint nằm trong thư mục con của repo: agent sửa tệp ở gốc repo (ngoài thư mục chứa .bint) vẫn bị
// phát hiện (review phiếu 2 .3).
func TestGuardReadOnlyCoversRepoRootWhenBintInSubdir(t *testing.T) {
	f := setup(t)
	sub := filepath.Join(f.root, "internal", "core") // giả lập thư mục chứa .bint
	rootFile := filepath.Join(f.root, "go.mod")
	err := guardReadOnly(context.Background(), sub, func() { appendLine(t, rootFile, "// agent sửa ở gốc repo") })
	if err == nil || !strings.Contains(err.Error(), "sửa cây làm việc") {
		t.Fatalf("sửa tệp ngoài thư mục con phải bị phát hiện, được %v", err)
	}
	if err := guardReadOnly(context.Background(), sub, func() {}); err != nil {
		t.Fatalf("không đổi gì thì không được lỗi: %v", err)
	}
}
