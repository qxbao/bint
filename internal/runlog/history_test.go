package runlog

import (
	"context"
	"reflect"
	"testing"
	"time"

	"bint/internal/spool"
	"bint/internal/store"
)

func TestRunsTouchingAndFailedTests(t *testing.T) {
	ctx := context.Background()
	s, sp := setup(t)
	t0 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	write := func(evs ...spool.Event) {
		t.Helper()
		if _, err := spool.Write(sp, evs...); err != nil {
			t.Fatal(err)
		}
	}
	// S1 (cũ nhất): sửa internal/a/x.go, internal/ab/y.go; chạy test hỏng rồi test qua.
	write(ev("a1", spool.SessionStarted, "S1", t0, map[string]any{"model": "m1"}),
		ev("a2", spool.FileModified, "S1", t0.Add(time.Second), map[string]any{"path": "internal/a/x.go"}),
		ev("a3", spool.FileModified, "S1", t0.Add(2*time.Second), map[string]any{"path": "internal/ab/y.go"}),
		ev("a4", spool.TestCompleted, "S1", t0.Add(3*time.Second), map[string]any{"command": "go test ./internal/a", "ok": false}),
		ev("a5", spool.TestCompleted, "S1", t0.Add(4*time.Second), map[string]any{"command": "go test ./internal/a", "ok": false}),
		ev("a6", spool.TestCompleted, "S1", t0.Add(5*time.Second), map[string]any{"command": "go test ./...", "ok": true}))
	// S2: chỉ đọc internal/a/x.go; sửa tệp có ký tự % và _.
	write(ev("b1", spool.SessionStarted, "S2", t0.Add(time.Hour), map[string]any{"model": "m2"}),
		ev("b2", spool.FileRead, "S2", t0.Add(time.Hour+time.Second), map[string]any{"paths": []string{"internal/a/x.go"}}),
		ev("b3", spool.FileModified, "S2", t0.Add(time.Hour+2*time.Second), map[string]any{"path": "dir_1/50%/f.txt"}))
	// S3 (mới nhất): sửa internal/a/z.go.
	write(ev("c1", spool.SessionStarted, "S3", t0.Add(2*time.Hour), map[string]any{"model": "m3"}),
		ev("c2", spool.FileModified, "S3", t0.Add(2*time.Hour+time.Second), map[string]any{"path": "internal/a/z.go"}))
	if _, err := Ingest(ctx, s, sp, t0.Add(3*time.Hour), DefaultStaleAfter); err != nil {
		t.Fatal(err)
	}

	mod := []string{ArtifactModified}
	both := []string{ArtifactModified, ArtifactRead}
	files := func(rs []RunTouch) [][]string {
		var out [][]string
		for _, r := range rs {
			out = append(out, r.Files)
		}
		return out
	}

	// Thư mục internal/a: khớp x.go và z.go, KHÔNG khớp internal/ab; mới nhất trước.
	rs, err := RunsTouching(ctx, s.DB, []string{"internal/a"}, mod, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"internal/a/z.go"}, {"internal/a/x.go"}}; !reflect.DeepEqual(files(rs), want) {
		t.Fatalf("thư mục: files = %v, muốn %v", files(rs), want)
	}
	if rs[0].Model != "m3" || rs[1].Model != "m1" || rs[0].StartedAt == "" || rs[0].Harness != "claude" {
		t.Fatalf("metadata = %+v", rs)
	}
	// Khớp đúng tệp, dấu / đầu và \ được chuẩn hoá.
	rs, _ = RunsTouching(ctx, s.DB, []string{`\internal\ab\y.go`}, mod, 10)
	if len(rs) != 1 || !reflect.DeepEqual(rs[0].Files, []string{"internal/ab/y.go"}) {
		t.Fatalf("tệp: %+v", rs)
	}
	// Tên tiền tố không phải thư mục: "internal/a/x" không khớp x.go.
	if rs, _ = RunsTouching(ctx, s.DB, []string{"internal/a/x"}, both, 10); len(rs) != 0 {
		t.Fatalf("tiền tố tên không được khớp: %+v", rs)
	}
	// include_reads: S2 chỉ đọc x.go.
	if rs, _ = RunsTouching(ctx, s.DB, []string{"internal/a/x.go"}, mod, 10); len(rs) != 1 {
		t.Fatalf("không đọc: %+v", rs)
	}
	if rs, _ = RunsTouching(ctx, s.DB, []string{"internal/a/x.go"}, both, 10); len(rs) != 2 || rs[0].Model != "m2" {
		t.Fatalf("có đọc: %+v", rs)
	}
	// Ký tự % và _ là literal.
	if rs, _ = RunsTouching(ctx, s.DB, []string{"dir_1/50%"}, mod, 10); len(rs) != 1 || rs[0].Model != "m2" {
		t.Fatalf("%% và _: %+v", rs)
	}
	if rs, _ = RunsTouching(ctx, s.DB, []string{"dirX1/50%"}, mod, 10); len(rs) != 0 {
		t.Fatalf("_ không phải ký tự đại diện: %+v", rs)
	}
	if rs, _ = RunsTouching(ctx, s.DB, []string{"dir_1/5%"}, mod, 10); len(rs) != 0 {
		t.Fatalf("%% không phải ký tự đại diện: %+v", rs)
	}
	// limit.
	if rs, _ = RunsTouching(ctx, s.DB, []string{"internal"}, both, 2); len(rs) != 2 || rs[0].Model != "m3" {
		t.Fatalf("limit: %+v", rs)
	}
	// Nhiều đường dẫn.
	if rs, _ = RunsTouching(ctx, s.DB, []string{"internal/ab", "dir_1"}, mod, 10); len(rs) != 2 {
		t.Fatalf("nhiều đường dẫn: %+v", rs)
	}

	// Lệnh kiểm thử hỏng: bỏ trùng, bỏ lệnh qua.
	cmds, err := FailedTestCommands(ctx, s.DB, runIDByModel(t, s, "m1"))
	if err != nil || !reflect.DeepEqual(cmds, []string{"go test ./internal/a"}) {
		t.Fatalf("failed = %v, %v", cmds, err)
	}
	cmds, _ = FailedTestCommands(ctx, s.DB, runIDByModel(t, s, "m3"))
	if len(cmds) != 0 {
		t.Fatalf("failed m3 = %v", cmds)
	}
}

func runIDByModel(t *testing.T, s *store.Store, model string) string {
	t.Helper()
	var id string
	if err := s.DB.QueryRow(`SELECT run_id FROM runs WHERE model = ?`, model).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
