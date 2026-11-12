package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/task"
	"bint/internal/testutil"
)

func rec(path string, at time.Time, agent bool, sum string) WriteRecord {
	return WriteRecord{Path: path, SHA256: sum, At: at, Actor: "test", Agent: agent}
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestWritesAppendOneLinePerRecord(t *testing.T) {
	p := WritesLogPath(t.TempDir())
	now := time.Now()
	if err := AppendWrites(p, []WriteRecord{rec("/a", now, false, "x"), rec("/b", now, true, "")}, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if err := AppendWrites(p, []WriteRecord{rec("/c", now, false, "z")}, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, p)
	if len(lines) != 3 {
		t.Fatalf("muốn 3 dòng, có %d: %v", len(lines), lines)
	}
	var r WriteRecord
	if err := json.Unmarshal([]byte(lines[1]), &r); err != nil || r.Path != "/b" || !r.Agent || r.SHA256 != "" {
		t.Fatalf("dòng 2: %+v %v", r, err)
	}
}

func TestWritesConcurrentAppendersLoseNothing(t *testing.T) {
	p := WritesLogPath(t.TempDir())
	now := time.Now()
	const workers, each = 8, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := AppendWrites(p, []WriteRecord{rec("/f", time.Now(), false, "h")}, time.Hour, now); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if n := len(readLines(t, p)); n != workers*each {
		t.Fatalf("muốn %d dòng, có %d", workers*each, n)
	}
}

func TestWritesCompactsOldLinesKeepsRecent(t *testing.T) {
	p := WritesLogPath(t.TempDir())
	now := time.Now()
	old := now.Add(-3 * time.Hour)
	if err := AppendWrites(p, []WriteRecord{rec("/old", old, false, "o"), rec("/new", now.Add(-time.Minute), false, "n")}, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append([]byte("không phải json\n"), mustRead(t, p)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendWrites(p, []WriteRecord{rec("/now", now, false, "m")}, 2*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadWrites(p, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range recs {
		got = append(got, r.Path)
	}
	if !slices.Equal(got, []string{"/new", "/now"}) {
		t.Fatalf("sau dọn: %v", got)
	}
	if n := len(readLines(t, p)); n != 2 {
		t.Fatalf("dòng hỏng/cũ phải bị bỏ khỏi tệp, còn %d dòng", n)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadWritesSkipsCorruptAndMissing(t *testing.T) {
	dir := t.TempDir()
	if recs, err := ReadWrites(WritesLogPath(dir), time.Time{}); err != nil || len(recs) != 0 {
		t.Fatalf("tệp không có: %v %v", recs, err)
	}
	p := WritesLogPath(dir)
	now := time.Now()
	good, _ := json.Marshal(rec("/g", now, false, "h"))
	early, _ := json.Marshal(rec("/e", now.Add(-time.Hour), false, "h"))
	if err := os.WriteFile(p, []byte("{hỏng\n"+string(good)+"\n"+string(early)+"\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadWrites(p, now.Add(-time.Minute))
	if err != nil || len(recs) != 1 || recs[0].Path != "/g" {
		t.Fatalf("recs=%+v err=%v", recs, err)
	}
}

func TestNormalizeWritePathStableForDeletedFiles(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "lnk")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("không tạo được symlink:", err)
	}
	f := filepath.Join(root, "d", "f.txt")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("x"), 0o644)
	before := NormalizeWritePath(f)
	viaLink := NormalizeWritePath(filepath.Join(link, "d", "f.txt"))
	os.RemoveAll(filepath.Join(root, "d"))
	after := NormalizeWritePath(f)
	afterLink := NormalizeWritePath(filepath.Join(link, "d", "f.txt"))
	if before != viaLink || before != after || before != afterLink {
		t.Fatalf("chuẩn hoá không ổn định: %q %q %q %q", before, viaLink, after, afterLink)
	}
}

// ReadonlyViolations theo bảng: luật dòng mới nhất + agent=false + băm khớp + nằm trong khoảng bước chạy.
func TestReadonlyViolationsHumanWriteTable(t *testing.T) {
	ctx := context.Background()
	const rel = "app.txt"
	cases := []struct {
		name string
		// logf dựng nhật ký sau khi tệp đã đổi; since là mốc bắt đầu bước.
		logf  func(abs, sum string, since time.Time) []WriteRecord
		touch func(abs string) // thay đổi tệp
		want  string           // "human" | "violation"
	}{
		{"người ghi khớp băm", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(time.Second), false, sum)}
		}, nil, "human"},
		{"agent: true", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(time.Second), true, sum)}
		}, nil, "violation"},
		{"băm lệch (agent sửa tiếp)", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(time.Second), false, "deadbeef")}
		}, nil, "violation"},
		{"dòng trước khi bước bắt đầu", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(-time.Second), false, sum)}
		}, nil, "violation"},
		{"không có dòng", func(abs, sum string, s time.Time) []WriteRecord { return nil }, nil, "violation"},
		{"dòng của tệp khác", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs)+".x", s.Add(time.Second), false, sum)}
		}, nil, "violation"},
		{"dòng mới nhất là agent dù dòng trước là người", func(abs, sum string, s time.Time) []WriteRecord {
			p := NormalizeWritePath(abs)
			return []WriteRecord{rec(p, s.Add(time.Second), false, sum), rec(p, s.Add(2*time.Second), true, sum)}
		}, nil, "violation"},
		{"dòng mới nhất là người dù dòng trước là agent", func(abs, sum string, s time.Time) []WriteRecord {
			p := NormalizeWritePath(abs)
			return []WriteRecord{rec(p, s.Add(time.Second), true, sum), rec(p, s.Add(2*time.Second), false, sum)}
		}, nil, "human"},
		{"xoá qua Bint: băm rỗng", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(time.Second), false, "")}
		}, func(abs string) { os.Remove(abs) }, "human"},
		{"băm rỗng nhưng tệp còn", func(abs, sum string, s time.Time) []WriteRecord {
			return []WriteRecord{rec(NormalizeWritePath(abs), s.Add(time.Second), false, "")}
		}, nil, "violation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testutil.Isolate(t)
			root := testutil.NewRepo(t, map[string]string{rel: "v1\n"})
			abs := filepath.Join(root, rel)
			before, err := WorktreeSnapshot(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			since := time.Now()
			if c.touch != nil {
				c.touch(abs)
			} else if err := os.WriteFile(abs, []byte("v2\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			after, err := WorktreeSnapshot(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			logp := WritesLogPath(t.TempDir())
			if recs := c.logf(abs, HashFile(abs), since); len(recs) > 0 {
				if err := AppendWrites(logp, recs, 0, since); err != nil {
					t.Fatal(err)
				}
			}
			chk := ReadonlyViolations(ctx, root, before, after, since, logp)
			switch c.want {
			case "human":
				if !slices.Equal(chk.HumanWrites, []string{rel}) || len(chk.Violations) != 0 {
					t.Fatalf("muốn được miễn: %+v", chk)
				}
			default:
				if !slices.Equal(chk.Violations, []string{rel}) || len(chk.HumanWrites) != 0 {
					t.Fatalf("muốn vi phạm: %+v", chk)
				}
			}
		})
	}
}

func TestReadonlyViolationsNewCandidateStillAllowed(t *testing.T) {
	ctx := context.Background()
	testutil.Isolate(t)
	h := newHarness(t)
	before, _ := WorktreeSnapshot(ctx, h.root)
	h.write("app.txt", "sửa lén\n")
	after, _ := WorktreeSnapshot(ctx, h.root)
	chk := ReadonlyViolations(ctx, h.root, before, after, time.Now().Add(-time.Minute), "")
	if !slices.Equal(chk.Violations, []string{"app.txt"}) {
		t.Fatalf("không có nhật ký thì vi phạm: %+v", chk)
	}
}

// Runner: bước chỉ đọc có người ghi qua Bint (dòng nhật ký khớp băm) thì completed, kết quả liệt kê tệp và in dòng báo.
func TestRunnerReadonlyStepSkipsHumanWrites(t *testing.T) {
	h := newHarness(t)
	runtime := filepath.Dir(h.tasks.Dir)
	var logs []string
	h.behave = func(ctx context.Context, step string, e *Env, id string) error {
		if step == StepReview {
			h.write("app.txt", "người sửa\n")
			abs := filepath.Join(h.root, "app.txt")
			if err := AppendWrites(WritesLogPath(runtime), []WriteRecord{rec(NormalizeWritePath(abs), time.Now(), false, HashFile(abs))}, 0, time.Now()); err != nil {
				return err
			}
		}
		return h.happy(ctx, step, e, id)
	}
	r := h.runner()
	r.Logf = func(f string, a ...any) { logs = append(logs, strings.TrimSpace(fmt.Sprintf(f, a...))) }
	res, err := r.Run(context.Background(), RunOptions{Workflow: StepDo, Request: "Thêm tính năng phiên bản v2 cho app.txt"})
	if err != nil {
		t.Fatal(err)
	}
	var review *StepResult
	for i := range res.Steps {
		if res.Steps[i].Step == StepReview {
			review = &res.Steps[i]
		}
	}
	if review == nil || review.Status != "completed" || !slices.Equal(review.HumanWrites, []string{"app.txt"}) {
		t.Fatalf("bước review: %+v (task %s)", review, res.Status)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "bỏ qua thay đổi do người ghi qua Bint: app.txt") {
		t.Fatalf("thiếu dòng báo trong log:\n%s", strings.Join(logs, "\n"))
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "bỏ qua thay đổi do người ghi qua Bint") {
		t.Fatalf("Notes: %v", res.Notes)
	}
	if res.Status == task.StatusFailed {
		t.Fatalf("tác vụ không được failed: %+v", res)
	}
}

// ActiveSteps chỉ đọc tệp chủ khoá: thấy bước đang giữ khoá, không thấy sau khi trả.
func TestActiveSteps(t *testing.T) {
	tasks := filepath.Join(t.TempDir(), "runtime", "tasks")
	root := t.TempDir()
	if got := ActiveSteps(tasks, root); len(got) != 0 {
		t.Fatalf("chưa có khoá: %+v", got)
	}
	l := mustLock(t, tasks, root, "01M772P7DKB3PBRBH7VPE614X6 b-review", LockShared)
	got := ActiveSteps(tasks, root)
	if len(got) != 1 || got[0].TaskID != "01M772P7DKB3PBRBH7VPE614X6" || got[0].Step != "b-review" || got[0].PID != os.Getpid() {
		t.Fatalf("đang giữ khoá: %+v", got)
	}
	if other := ActiveSteps(tasks, t.TempDir()); len(other) != 0 {
		t.Fatalf("cây khác không được thấy: %+v", other)
	}
	l.Release()
	if got := ActiveSteps(tasks, root); len(got) != 0 {
		t.Fatalf("sau khi trả: %+v", got)
	}
}
