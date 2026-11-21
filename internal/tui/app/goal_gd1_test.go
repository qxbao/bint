package app

// Kiểm tra mục tiêu (b-goal) của giai đoạn 1 đại tu TUI: bố cục thoáng, pane hai cột chỉnh được tỉ lệ và lưu theo màn.
//
// Hộp đen qua mô hình gốc (Render đã bỏ màu), báo từng ô (loại/nơi) còn đỏ:
//   - Split × 7 màn hai pane ở 140×40: S-GROW `>`, S-SHRINK `<`, S-ZOOM `z`, S-RESET `=`, S-DRAG (kéo đường chia bằng
//     chuột), S-SAVE (ghi .bint/local.yml chỉ sửa dòng cần sửa, mô hình tạo lại nạp đúng tỉ lệ), S-CLAMP (≤ 80%).
//   - Khung × {knowledge, overview} × {140×40, 80×24}: C-TAB, C-CHROME; C-TITLE ở knowledge 140×40.
//   - Cột: L-UNIFORM (knowledge, runs), L-EVOLVE, L-EVAL, L-RUNS, L-LOGO.
//   - Chi tiết: D-EMAIL (knowledge, system, eval), D-TIME (knowledge, runs, evolve, eval), D-RULE (knowledge, tasks),
//     D-JSON (runs).
//   - GĐ0: G-GATE, G-CTRLR (knowledge).
//
// Repository tạm thật (bint init, tri thức thật có bằng chứng, tác giả "tên <email>"); Tri thức và Tiến hoá dùng bộ nạp
// thật (cơ hội "duplicate" của Tiến hoá trỏ tới hai mục tri thức thật), các màn còn lại dùng dữ liệu giả có hình dạng
// như ngoài đời. Độ rộng pane trái đo bằng cột của `┐` đầu tiên trên dòng viền trên của hai pane.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/runlog"
	"bint/internal/testutil"
	"bint/internal/tui/clipboard"
	elscreen "bint/internal/tui/screens/eval"
	evscreen "bint/internal/tui/screens/evolve"
	knscreen "bint/internal/tui/screens/knowledge"
	mcscreen "bint/internal/tui/screens/mcp"
	"bint/internal/tui/screens/overview"
	rnscreen "bint/internal/tui/screens/runs"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/screens/shared"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
)

const gd1Version = "1.4.2"

// ---- bơm thông điệp bất đồng bộ (lệnh nền chạy trong goroutine, Update chạy trên goroutine của kiểm thử) ----

type gd1Pump struct {
	m      *Model
	msgs   chan tea.Msg
	health int // số HealthMsg (kết quả kiểm sức khoẻ tri thức) đã nhận
}

func (p *gd1Pump) run(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			for _, x := range msg {
				p.run(x)
			}
		default:
			p.msgs <- msg
		}
	}()
}

func (p *gd1Pump) deliver(msg tea.Msg) {
	if _, ok := msg.(knscreen.HealthMsg); ok {
		p.health++
	}
	_, next := p.m.Update(msg)
	p.run(next)
}

// until nhận thông điệp tới khi cond đúng hoặc hết hạn (hạn chỉ chặn treo); trả cond cuối cùng.
func (p *gd1Pump) until(cond func() bool, limit time.Duration) bool {
	deadline := time.After(limit)
	for !cond() {
		select {
		case msg := <-p.msgs:
			p.deliver(msg)
		case <-deadline:
			return cond()
		}
	}
	return true
}

func (p *gd1Pump) settle() {
	end := time.After(3 * time.Second)
	for {
		select {
		case msg := <-p.msgs:
			p.deliver(msg)
		case <-time.After(40 * time.Millisecond):
			return
		case <-end:
			return
		}
	}
}

func (p *gd1Pump) press(keys ...string) {
	for _, k := range keys {
		p.deliver(keyMsg(k))
		p.settle()
	}
}

func (p *gd1Pump) screen() string { return plain(p.m.Render()) }

// ---- đo trên màn hình đã vẽ ----

// gd1Width là độ rộng hiển thị của chuỗi.
func gd1Width(s string) int { return ansi.StringWidth(s) }

// gd1Split trả cột (độ rộng hiển thị tính từ 0) của `┐` đầu tiên trên dòng viền trên của hai pane (dòng đầu tiên có ít nhất
// hai `┐`) và chỉ số dòng đó; (-1, -1) nếu không có hai pane cạnh nhau.
func gd1Split(screen string) (int, int) {
	for y, l := range strings.Split(screen, "\n") {
		if strings.Count(l, "┐") < 2 {
			continue
		}
		i := strings.Index(l, "┐")
		return gd1Width(l[:i]), y
	}
	return -1, -1
}

// gd1Cut tách dòng tại cột hiển thị col: phần trước col và phần sau col (bỏ ký tự ở đúng col).
func gd1Cut(l string, col int) (string, string) {
	w := 0
	for i, r := range l {
		if w >= col {
			rest := l[i:]
			if _, sz := firstRune(rest); sz > 0 {
				return l[:i], rest[sz:]
			}
			return l[:i], ""
		}
		w += gd1Width(string(r))
	}
	return l, ""
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

// gd1Panes trả phần pane trái và pane phải của các dòng nằm dưới dòng viền trên của hai pane. Không có hai pane: cả màn
// hình là pane trái, pane phải rỗng.
func gd1Panes(screen string) (left, right []string) {
	col, y := gd1Split(screen)
	ls := strings.Split(screen, "\n")
	if col < 0 {
		return ls, nil
	}
	for _, l := range ls[y+1:] {
		a, b := gd1Cut(l, col+1)
		left = append(left, a)
		right = append(right, b)
	}
	return left, right
}

// gd1Zoomed: không còn hai pane cạnh nhau và pane còn lại (dòng viền trên đầu tiên) rộng gần trọn màn.
func gd1Zoomed(screen string, w int) (bool, string) {
	if c, _ := gd1Split(screen); c >= 0 {
		return false, fmt.Sprintf("vẫn còn hai pane (đường chia ở cột %d)", c)
	}
	for _, l := range strings.Split(screen, "\n") {
		a, b := strings.IndexAny(l, "┌╭"), strings.LastIndexAny(l, "┐╮")
		if a < 0 || b < 0 || b < a {
			continue
		}
		span := gd1Width(l[:b]) - gd1Width(l[:a]) + 1
		if span >= w-6 {
			return true, ""
		}
		return false, fmt.Sprintf("pane phóng to chỉ rộng %d/%d cột: %q", span, w, l)
	}
	return false, "không thấy viền pane nào"
}

// gd1DividerRow trả một dòng nội dung của pane có ký tự viền dọc đúng ở cột đường chia.
func gd1DividerRow(screen string, col, from int) int {
	ls := strings.Split(screen, "\n")
	for y := from + 2; y < len(ls)-2; y++ {
		w := 0
		for _, r := range ls[y] {
			if w == col {
				if r == '│' || r == '┃' || r == '|' {
					return y
				}
				break
			}
			w += gd1Width(string(r))
		}
	}
	return from + 3
}

// gd1LeadingBars đếm số viền dọc ở đầu dòng (trước ký tự nội dung đầu tiên).
func gd1LeadingBars(l string) int {
	n := 0
	for _, r := range l {
		switch r {
		case '│', '┃', '║':
			n++
		case ' ':
		default:
			return n
		}
	}
	return n
}

func gd1Prefix(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

var (
	gd1EmailRe = regexp.MustCompile(`<[^<>\s@]+@[^<>\s]+>`)
	gd1ISORe   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d`)
	gd1KeyF    = regexp.MustCompile(`(^|[^\p{L}\p{N}_/.])f([^\p{L}\p{N}_]|$)`)
	gd1KeyV    = regexp.MustCompile(`(^|[^\p{L}\p{N}_/.])v([^\p{L}\p{N}_]|$)`)
)

// ---- dữ liệu ----

type gd1Item struct {
	typ, title, body, file, src, paths string
}

// Bốn kiểu mục (rule, warning, pattern, fact) và một cặp warning gần trùng tiêu đề (cơ hội "duplicate" của Tiến hoá).
var gd1Items = []gd1Item{
	{"rule", "Mọi handler HTTP phải đi qua middleware xác thực wrapAuth trước khi đọc body",
		"Handler mới đăng ký trực tiếp vào router sẽ bỏ qua kiểm tra phiên đăng nhập; luôn bọc bằng wrapAuth.",
		"src/api/handler.go", "package api\n\n// Đăng ký handler.\nfunc Register(r Router) {\n\tr.Handle(\"/v1/users\", wrapAuth(users))\n\tr.Handle(\"/v1/orders\", wrapAuth(orders))\n}\n", "src/api/**"},
	{"warning", "Đặt timeout cho mọi lệnh git chạy trong mô-đun gitrepo của dự án",
		"Lệnh git không có timeout làm treo cả TUI khi kho có hook chờ nhập; dùng context có hạn chót.",
		"src/gitrepo/run.go", "package gitrepo\n\n// Chạy git.\nfunc Run(ctx Context, args ...string) error {\n\tctx, cancel := withTimeout(ctx, 30)\n\tdefer cancel()\n\treturn exec(ctx, \"git\", args...)\n}\n", "src/gitrepo/**"},
	{"warning", "Đặt timeout cho mọi lệnh git chạy ở mô-đun gitrepo của dự án",
		"Lệnh git chạy nền phải nhận context để huỷ được khi người dùng thoát giữa chừng.",
		"src/gitrepo/exec.go", "package gitrepo\n\n// Chạy nền.\nfunc Background(ctx Context) error {\n\tctx, cancel := withTimeout(ctx, 60)\n\tdefer cancel()\n\treturn exec(ctx, \"git\", \"fetch\")\n}\n", "src/gitrepo/**"},
	{"pattern", "Ghi tệp cấu hình qua WriteAtomic để không để lại tệp hỏng khi mất điện",
		"Cách ghi và lý do nằm trong `docs/luu-cau-hinh.md`; không tự viết CreateTemp rồi Rename.",
		"src/config/save.go", "package config\n\n// Lưu cấu hình.\nfunc Save(path string, b []byte) error {\n\tif err := validate(b); err != nil {\n\t\treturn err\n\t}\n\treturn WriteAtomic(path, b)\n}\n", "src/config/**"},
	{"fact", "Cổng mặc định của máy chủ phát triển là 8080, đổi được qua biến môi trường",
		"Máy chủ phát triển nghe cổng 8080 khi không đặt biến môi trường cổng.",
		"src/server/port.go", "package server\n\n// Cổng mặc định.\nfunc Port() int {\n\tif p := envPort(); p > 0 {\n\t\treturn p\n\t}\n\treturn 8080\n}\n", "src/server/**"},
}

const (
	gd1ItemDupA    = 1
	gd1ItemDupB    = 2
	gd1ItemPathRef = 3 // mục có đường dẫn nhắc trong nội dung (xoá tệp → cảnh báo path-ref, không phải relocated)
	gd1ItemFact    = 4 // mục bị sửa mã giữ nguyên kích thước và mtime (G-CTRLR)
	gd1AuthorName  = "Nguyễn Văn An"
	gd1AuthorEmail = "an.nguyen@example.com"
	gd1DocRef      = "docs/luu-cau-hinh.md"
)

const gd1LocalYML = "# Cấu hình riêng của máy này (không commit).\nui:\n  # Ngôn ngữ giao diện: auto | vi | en\n  language: vi\n# Ghi chú của người dùng: giữ nguyên khi TUI ghi tỉ lệ pane.\n"

type gd1Fixture struct {
	root  string
	ids   []string
	start time.Time
}

func gd1Repo(t *testing.T) *gd1Fixture {
	t.Helper()
	files := map[string]string{"README.md": "# demo\n", gd1DocRef: "# Lưu cấu hình\n\nGhi qua tệp tạm rồi đổi tên nguyên tử.\n"}
	for _, it := range gd1Items {
		files[it.file] = it.src
	}
	root := testutil.NewRepo(t, files)
	testutil.Git(t, root, "config", "user.name", gd1AuthorName)
	testutil.Git(t, root, "config", "user.email", gd1AuthorEmail)
	ctx := context.Background()
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, Hooks: boolPtr(false), NoSkills: true}); err != nil {
		t.Fatal(err)
	}
	k, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	f := &gd1Fixture{root: root, start: time.Now()}
	for i, it := range gd1Items {
		res, err := bintapp.AddKnowledge(ctx, k, bintapp.AddOptions{Type: it.typ, Title: it.title, Body: it.body, Paths: []string{it.paths},
			Evidence: []string{it.file + ":3-7"}, Origin: "agent", Now: time.Now().Add(-time.Duration(2+i) * time.Hour)})
		if err != nil || !res.Saved {
			t.Fatalf("fixture: thêm mục %q: err=%v saved=%v findings=%+v", it.title, err, res.Saved, res.Check.Findings)
		}
		if !strings.Contains(res.Item.Author, "<"+gd1AuthorEmail+">") {
			t.Fatalf("fixture: tác giả của mục phải dạng tên <email>, có %q", res.Item.Author)
		}
		f.ids = append(f.ids, res.Item.ID)
	}
	rep, err := bintapp.VerifyKnowledge(ctx, k, nil, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		if len(r.Findings) > 0 {
			t.Fatalf("fixture: mục %s phải sạch lúc đầu (L-UNIFORM cần mọi mục ✓): %+v", r.ID, r.Findings)
		}
	}
	d, err := evscreen.Load(ctx, root, evscreen.Query{}, true)
	if err != nil {
		t.Fatal(err)
	}
	dup := false
	for _, o := range d.Opportunities() {
		dup = dup || o.Kind == evolve.KindDuplicate && strings.Contains(strings.Join(o.Targets, " "), f.ids[gd1ItemDupA])
	}
	if !dup {
		t.Fatalf("fixture: quét Tiến hoá phải có cơ hội duplicate trỏ tới %s: %+v (lỗi %v)", f.ids[gd1ItemDupA], d.Opportunities(), d.ScanErr)
	}
	gd1Write(t, root, ".bint/local.yml", gd1LocalYML)
	return f
}

func gd1Stamp(d time.Duration) string { return time.Now().UTC().Add(-d).Format("2006-01-02T15:04:05.000Z") }

const gd1RunDesc = "Chạy lại kiểm thử của gói workflow sau khi sửa cách khôi phục bước bị ngắt giữa chừng trong lúc agent đang ghi tệp"

func gd1Runs() rnscreen.Data {
	mk := func(id, harness string, ago time.Duration, label, why string) bintapp.RunRow {
		return bintapp.RunRow{Label: label, Reason: why, Run: runlog.Run{RunID: id, Status: "completed", Harness: ns(harness), Model: ns("claude-opus-5-5"),
			GitBranch: ns("bint/01M7WY46RBGJHKS7NS7FYX5JV6"), GitBaseCommit: ns("6d62188"), StartedAt: ns(gd1Stamp(ago)), FinishedAt: ns(gd1Stamp(ago - 4*time.Minute)),
			Events: 42, FilesModified: 3, TestsOK: 2, DurationMS: sqlInt(240000), EndReason: ns("exit")}}
	}
	return rnscreen.Data{Limit: 20, Stamp: time.Unix(1, 0), Rows: []bintapp.RunRow{
		mk("01M7XK0TXY97WHD06S4SNSTR6K", "claude", 3*time.Hour, runlog.LabelFailure, "người dùng chấm bad"),
		mk("01M7XJ8Q2C5V0R3D9E1F4G6H7J", "codex", 5*time.Hour, runlog.LabelSuccess, "kiểm thử qua sau khi sửa"),
		mk("01M7XH3W5N8B1M4K7P0R2T6V9X", "claude", 26*time.Hour, runlog.LabelSuccess, "người dùng chấm good"),
	}}
}

func gd1RunDetail(_ context.Context, _ string, id string, _ bool) (*bintapp.RunDetail, error) {
	for _, r := range gd1Runs().Rows {
		if r.Run.RunID != id {
			continue
		}
		payload := `{"tool":"Bash","command":"go test ./internal/workflow/... -count=1","description":"` + gd1RunDesc + `","exit_code":1}`
		return &bintapp.RunDetail{Run: r.Run, Label: r.Label, Reason: r.Reason,
			Events: []runlog.EventRow{{EventID: "e1", Type: "tool.finished", Source: "hook:claude", Timestamp: r.Run.StartedAt.String, Payload: payload}}}, nil
	}
	return nil, fmt.Errorf("không có lượt %s", id)
}

const (
	gd1EvalAuthor = "Lê Minh Châu"
	gd1EvalEmail  = "chau.le@example.com"
)

func gd1EvalCases() []*eval.Case {
	mk := func(id, subject, leak string) *eval.Case {
		c := &eval.Case{Schema: eval.CaseSchemaVersion, ID: id, Status: eval.StatusCandidate,
			Source: eval.Source{Commit: id + "e1f0", Base: id + "a2b3", Author: gd1EvalAuthor + " <" + gd1EvalEmail + ">", Date: time.Now().UTC().Add(-30 * time.Hour).Format("2006-01-02"), Subject: subject},
			Prompt: subject + " và giữ nguyên hành vi cũ của API.", TestFiles: []string{"internal/db/db_test.go"}, SourceFiles: []string{"internal/db/db.go"},
			Stats: eval.Stats{Files: 2, SourceLines: 30, TestLines: 20}, Leak: eval.LeakReport{Status: leak},
			Verification: &eval.Verification{Status: "verified", FailBefore: true, PassAfter: true, AfterRuns: 2,
				VerifiedAt: time.Now().UTC().Add(-5 * time.Hour).Format(time.RFC3339)}}
		if leak == "warn" {
			c.Leak.Findings = []eval.LeakFinding{{Severity: "warn", Kind: "identifier", Detail: "đề bài nhắc định danh mới của lời giải"}}
		}
		return c
	}
	return []*eval.Case{
		mk("c7f3a9c21", "Thêm cột email vào bảng người dùng", "pass"),
		mk("c4b8d2e90", "Sửa lỗi khoá cơ sở dữ liệu khi ghi đồng thời", "warn"),
	}
}

func gd1Evolve() (evscreen.Loader, evscreen.Detailer) {
	fake := func() []*evolve.Proposal {
		ps := sampleEvolveProposals()[:2]
		for i, p := range ps {
			p.CreatedAt, p.UpdatedAt = gd1Stamp(time.Duration(3+i)*time.Hour), gd1Stamp(time.Duration(3+i)*time.Hour)
		}
		return ps
	}
	load := func(ctx context.Context, dir string, q evscreen.Query, ro bool) (evscreen.Data, error) {
		d, err := evscreen.Load(ctx, dir, q, ro)
		if err != nil {
			return d, err
		}
		for _, p := range fake() {
			d.Rows = append(d.Rows, evolvesvc.ListRow{Proposal: p, State: p.Status})
		}
		return d, nil
	}
	detail := func(ctx context.Context, dir, id string, ro bool) (*evolvesvc.ShowResult, error) {
		for _, p := range fake() {
			if p.ID == id {
				return &evolvesvc.ShowResult{Proposal: p, State: p.Status, Cases: map[string]*eval.Case{}}, nil
			}
		}
		return evscreen.LoadDetail(ctx, dir, id, ro)
	}
	return load, detail
}

const (
	gd1SysName  = "Trần Thị Bình"
	gd1SysEmail = "binh.tran@example.com"
)

// gd1Options dựng tuỳ chọn của mô hình gốc trên repository thật: Tri thức và Tiến hoá nạp thật, các màn khác dữ liệu giả.
func gd1Options(f *gd1Fixture) Options {
	evLoad, evDetail := gd1Evolve()
	return Options{Dir: f.root, Version: gd1Version, Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Hour, Clipboard: &clipboard.Fake{},
		DetectHarnesses: func() []detect.Harness { return nil },
		Loader: func(context.Context, string) (overview.Data, error) {
			d := sampleData()
			d.Status.Repo = f.root
			return d, nil
		},
		TasksLoader:   func(context.Context, string, bool) (tkscreen.Data, error) { return sampleTasks(), nil },
		TasksDetailer: sampleTaskDetail,
		TasksRuns: func(context.Context, string, string, bool) (bintapp.TaskRunsResult, error) {
			return sampleTaskRuns(), nil
		},
		RunsLoader:   func(context.Context, string, int, bool) (rnscreen.Data, error) { return gd1Runs(), nil },
		RunsDetailer: gd1RunDetail,
		RunsLessons:  sampleLessons,
		EvalLoader: func(context.Context, string, elscreen.Query) (elscreen.Data, error) {
			return elscreen.Data{Config: config.Default(), Pending: gd1EvalCases(), Stamp: time.Unix(1, 0),
				CasesDir: filepath.Join(f.root, ".bint", "evals", "cases"), CandidatesDir: "/state/demo/evals/candidates"}, nil
		},
		EvalDetailer: func(_ context.Context, _ string, id string) (*bintapp.EvalShowResult, error) {
			for _, c := range gd1EvalCases() {
				if c.ID == id {
					return &bintapp.EvalShowResult{Case: c, Path: "/state/demo/evals/candidates/" + id + ".yml", TestCommand: "go test ./..."}, nil
				}
			}
			return nil, fmt.Errorf("không có ứng viên %s", id)
		},
		EvalReporter:   sampleEvalReport,
		EvolveLoader:   evLoad,
		EvolveDetailer: evDetail,
		SystemLoad: withSampleSystem(syscreen.Loaders{Components: func(context.Context, string) (syscreen.ComponentsData, error) {
			d := sampleComponents()
			d.Details[0].Component.Meta.ApprovedBy = gd1SysName + " <" + gd1SysEmail + ">"
			d.Details[0].Row = bintapp.RowOfComponent(d.Details[0].Component)
			return d, nil
		}}),
		MCPLoad: withSampleMCP(mcscreen.Loaders{}),
	}
}

func gd1Start(t *testing.T, o Options, w, h int) *gd1Pump {
	t.Helper()
	shared.Now = time.Now // dữ liệu của các ô dùng thời điểm thật (newModel đặt đồng hồ cố định cho golden)
	m := New(context.Background(), o)
	m.SetSize(w, h)
	p := &gd1Pump{m: m, msgs: make(chan tea.Msg, 4096)}
	p.run(m.Init())
	if !p.until(func() bool { return !m.ov.Loading && m.layoutReady }, 120*time.Second) {
		t.Fatalf("Tổng quan không nạp xong:\n%s", p.screen())
	}
	p.settle()
	return p
}

type gd1Screen struct {
	name string // tên màn (khoá trong ui.layout)
	key  string // phím chọn tab
	subs int    // số lần "." để tới thẻ phụ cần đo
}

var gd1SplitScreens = []gd1Screen{
	{"knowledge", "2", 0}, {"tasks", "3", 0}, {"runs", "4", 0}, {"eval", "5", 0}, {"evolve", "6", 0}, {"system", "7", 0}, {"mcp", "8", 0},
}

// open chọn tab (và thẻ phụ) rồi chờ tới khi danh sách có con trỏ.
func (p *gd1Pump) open(s gd1Screen, extra func(string) bool) string {
	p.deliver(keyMsg(s.key))
	p.settle()
	for range s.subs {
		p.press(".")
	}
	p.until(func() bool {
		sc := p.screen()
		return strings.Contains(sc, "▸") && !strings.Contains(sc, "Đang tải") && (extra == nil || extra(sc))
	}, 90*time.Second)
	p.settle()
	return p.screen()
}

// ---- kiểm tra mục tiêu ----

func TestGoalGD1Layout(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	f := gd1Repo(t)

	gd1FrameCells(t)
	gd1LogoCells(t)
	gd1ColumnAndDetailCells(t, f)
	gd1SplitCells(t, f)
	gd1GD0Cells(t, f)
}

// ---- Khung: C-TAB, C-CHROME, C-TITLE ----

const gd1LongTitle = "Không gọi sql.Open trực tiếp ngoài gói internal/db/store của dự án"

func gd1FrameModel(t *testing.T, w, h int) *Model {
	kl := func(context.Context, string) (knscreen.Data, error) {
		d := sampleKnowledge()
		d.Items[1].Title = gd1LongTitle
		return d, nil
	}
	return newModel(t, Options{Mode: theme.Mono, Lang: "vi", Version: gd1Version, Clipboard: &clipboard.Fake{}, KnowledgeLoader: kl}, sampleData(), w, h)
}

func gd1FrameCells(t *testing.T) {
	type place struct {
		screen string
		w, h   int
	}
	for _, pl := range []place{{"knowledge", 140, 40}, {"overview", 140, 40}, {"knowledge", 80, 24}, {"overview", 80, 24}} {
		m := gd1FrameModel(t, pl.w, pl.h)
		label := "Tổng quan"
		if pl.screen == "knowledge" {
			press(t, m, "2")
			label = "Tri thức"
		}
		s := plain(m.Render())
		ls := strings.Split(s, "\n")
		where := fmt.Sprintf("%s %dx%d", pl.screen, pl.w, pl.h)

		t.Run(fmt.Sprintf("C-TAB/%s_%dx%d", pl.screen, pl.w, pl.h), func(t *testing.T) {
			ok := false
			for _, l := range ls {
				tr := strings.TrimSpace(l)
				if strings.Contains(l, label) && strings.Contains(l, "MCP") && strings.Contains(l, "/work/demo") && strings.Contains(l, "bint "+gd1Version) &&
					!strings.HasPrefix(tr, "│") && !strings.HasPrefix(tr, "┌") && !strings.HasPrefix(tr, "└") {
					ok = true
				}
			}
			if !ok {
				t.Errorf("C-TAB/%s: không có thanh tab một dòng (không khung) chứa nhãn tab %q, đường dẫn repo /work/demo và \"bint %s\":\n%s", where, label, gd1Version, s)
			}
		})

		t.Run(fmt.Sprintf("C-CHROME/%s_%dx%d", pl.screen, pl.w, pl.h), func(t *testing.T) {
			first, bars := -1, -1
			for y, l := range ls {
				if pl.screen == "knowledge" {
					if strings.Contains(l, "▸") {
						first, bars = y, gd1LeadingBars(l)
						break
					}
					continue
				}
				if first < 0 && !strings.Contains(l, "bint "+gd1Version) &&
					(strings.Contains(l, "█") || strings.Contains(l, "bint.") || strings.Contains(l, "Ghi nhận, đo lường") || strings.Contains(l, "Repository")) {
					first = y
				}
				if bars < 0 && strings.Contains(l, "Harness ") {
					bars = gd1LeadingBars(l)
				}
			}
			if first < 0 {
				t.Errorf("C-CHROME/%s: không thấy dòng nội dung đầu tiên:\n%s", where, s)
				return
			}
			if first > 3 {
				t.Errorf("C-CHROME/%s: có %d dòng khung phía trên dòng nội dung đầu tiên, muốn tối đa 3:\n%s", where, first, s)
			}
			if bars > 1 {
				t.Errorf("C-CHROME/%s: dòng nội dung có %d lớp viền dọc lồng nhau, muốn tối đa 1:\n%s", where, bars, s)
			}
		})

		if pl.screen == "knowledge" && pl.w == 140 {
			t.Run("C-TITLE/knowledge_140x40", func(t *testing.T) {
				want := gd1Prefix(gd1LongTitle, 30)
				left, _ := gd1Panes(s)
				for _, l := range left {
					if strings.Contains(l, want) {
						return
					}
				}
				t.Errorf("C-TITLE/knowledge 140x40: cột tiêu đề không hiện đủ 30 ký tự đầu %q của %q:\n%s", want, gd1LongTitle, s)
			})
		}
	}
}

// ---- L-LOGO ----

// gd1HasLogo: có dòng logo điểm ảnh ("██"), không tính thanh xếp chồng của dòng "Lượt chạy" trong hộp Repository.
func gd1HasLogo(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "██") && !strings.Contains(l, "Lượt chạy") {
			return true
		}
	}
	return false
}

func gd1LogoCells(t *testing.T) {
	t.Run("L-LOGO/overview_140x45", func(t *testing.T) {
		s := plain(gd1FrameModel(t, 140, 45).Render())
		if !gd1HasLogo(s) {
			t.Errorf("L-LOGO/overview 140x45: terminal cao 45 dòng phải hiện logo lớn:\n%s", s)
		}
	})
	t.Run("L-LOGO/overview_140x44", func(t *testing.T) {
		s := plain(gd1FrameModel(t, 140, 44).Render())
		if gd1HasLogo(s) {
			t.Errorf("L-LOGO/overview 140x44: thấp hơn 45 dòng không được hiện logo lớn:\n%s", s)
		}
		n := 0
		for _, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "bint "+gd1Version) {
				continue
			}
			if strings.Contains(l, "bint.") || strings.Contains(l, "Ghi nhận, đo lường") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("L-LOGO/overview 140x44: logo phải thu thành đúng 1 dòng, có %d dòng:\n%s", n, s)
		}
	})
}

// ---- Cột và Chi tiết ----

func gd1RightText(s string) (string, []string) {
	_, right := gd1Panes(s)
	return strings.Join(right, "\n"), right
}

func gd1CheckEmail(t *testing.T, cell, s, name string) {
	t.Helper()
	txt, _ := gd1RightText(s)
	if m := gd1EmailRe.FindString(txt); m != "" {
		t.Errorf("%s: khung chi tiết còn email trong ngoặc nhọn %q:\n%s", cell, m, s)
	}
	if !strings.Contains(txt, name) {
		t.Errorf("%s: khung chi tiết phải giữ tên %q:\n%s", cell, name, s)
	}
}

func gd1CheckTime(t *testing.T, cell, s string) {
	t.Helper()
	txt, _ := gd1RightText(s)
	if !strings.Contains(txt, "trước") {
		t.Errorf("%s: khung chi tiết không có thời gian tương đối (\"… trước\"):\n%s", cell, s)
	}
	if m := gd1ISORe.FindString(txt); m != "" {
		t.Errorf("%s: khung chi tiết còn thời điểm dạng ISO %q:\n%s", cell, m, s)
	}
}

func gd1CheckRule(t *testing.T, cell, s string) {
	t.Helper()
	_, right := gd1RightText(s)
	if len(right) == 0 {
		t.Errorf("%s: không thấy khung chi tiết cạnh danh sách:\n%s", cell, s)
		return
	}
	prev, prevLine := false, ""
	for _, r := range right {
		if strings.ContainsAny(r, "┌┐└┘╭╮╰╯") {
			prev = false
			continue
		}
		tr := strings.Trim(r, " │┃")
		rule := strings.Contains(tr, "──")
		if rule && prev {
			t.Errorf("%s: tiêu đề mục bị kẻ hai đường liên tiếp:\n%q\n%q\nmàn hình:\n%s", cell, prevLine, tr, s)
			return
		}
		prev, prevLine = rule, tr
	}
}

func gd1ColumnAndDetailCells(t *testing.T, f *gd1Fixture) {
	p := gd1Start(t, gd1Options(f), 140, 40)

	// ---- Tri thức ----
	titleA := gd1Prefix(gd1Items[0].title, 12)
	p.deliver(keyMsg("2"))
	p.until(func() bool { return p.health > 0 && strings.Contains(p.screen(), titleA) }, 120*time.Second)
	p.until(func() bool {
		s := p.screen()
		return !strings.Contains(s, "Đang tải") && strings.Contains(s, "Nguồn gốc")
	}, 30*time.Second)
	p.settle()
	kn := p.screen()
	t.Run("L-UNIFORM/knowledge", func(t *testing.T) {
		left, _ := gd1Panes(kn)
		rows := 0
		for _, l := range left {
			for _, it := range gd1Items {
				if i := strings.Index(l, gd1Prefix(it.title, 10)); i >= 0 {
					rows++
					if strings.Contains(l[:i], "✓") {
						t.Errorf("L-UNIFORM/knowledge: mọi mục cùng sức khoẻ ✓ nhưng cột sức khoẻ vẫn hiện ở hàng %q", strings.TrimSpace(l))
					}
					break
				}
			}
		}
		if rows == 0 {
			t.Errorf("L-UNIFORM/knowledge: không thấy hàng tri thức nào:\n%s", kn)
		}
	})
	t.Run("D-EMAIL/knowledge", func(t *testing.T) { gd1CheckEmail(t, "D-EMAIL/knowledge", kn, gd1AuthorName) })
	t.Run("D-TIME/knowledge", func(t *testing.T) { gd1CheckTime(t, "D-TIME/knowledge", kn) })
	t.Run("D-RULE/knowledge", func(t *testing.T) { gd1CheckRule(t, "D-RULE/knowledge", kn) })

	// ---- Tác vụ ----
	tk := p.open(gd1Screen{"tasks", "3", 0}, func(s string) bool { return strings.Contains(s, "Thêm cột email") })
	t.Run("D-RULE/tasks", func(t *testing.T) { gd1CheckRule(t, "D-RULE/tasks", tk) })

	// ---- Lượt chạy ----
	rn := p.open(gd1Screen{"runs", "4", 0}, func(s string) bool {
		txt, _ := gd1RightText(s)
		return strings.Contains(txt, "tool")
	})
	t.Run("L-UNIFORM/runs", func(t *testing.T) {
		left, _ := gd1Panes(rn)
		for _, l := range left {
			if strings.Contains(l, "completed") {
				t.Errorf("L-UNIFORM/runs: mọi lượt đều completed nhưng cột trạng thái vẫn hiện: %q", strings.TrimSpace(l))
				return
			}
		}
	})
	t.Run("L-RUNS/runs", func(t *testing.T) {
		left, _ := gd1Panes(rn)
		seen := false
		for _, l := range left {
			for _, pair := range [][2]string{{"✓", "thành công"}, {"✗", "thất bại"}} {
				hasG, hasT := strings.Contains(l, pair[0]), strings.Contains(l, pair[1])
				seen = seen || hasG || hasT
				if hasG && hasT {
					t.Errorf("L-RUNS/runs: hàng còn hai cột cùng mang nhãn (%s và %q): %q", pair[0], pair[1], strings.TrimSpace(l))
				}
			}
		}
		if !seen {
			t.Errorf("L-RUNS/runs: danh sách không còn hiện nhãn lượt chạy:\n%s", rn)
		}
	})
	t.Run("D-TIME/runs", func(t *testing.T) { gd1CheckTime(t, "D-TIME/runs", rn) })
	t.Run("D-JSON/runs", func(t *testing.T) {
		txt, _ := gd1RightText(rn)
		if strings.Contains(txt, `{"`) {
			t.Errorf("D-JSON/runs: sự kiện còn hiện JSON thô:\n%s", rn)
		}
		for _, re := range []*regexp.Regexp{regexp.MustCompile(`tool:\s+Bash`), regexp.MustCompile(`exit_code:\s+1`)} {
			if !re.MatchString(txt) {
				t.Errorf("D-JSON/runs: sự kiện không có dòng dạng khoá: giá trị %q:\n%s", re.String(), rn)
			}
		}
		tokens := map[string]bool{}
		for _, w := range strings.Fields(strings.NewReplacer("│", " ", "┃", " ").Replace(txt)) {
			tokens[w] = true
		}
		for _, w := range strings.Fields(gd1RunDesc) {
			if !tokens[w] {
				t.Errorf("D-JSON/runs: từ %q của giá trị dài không hiện nguyên vẹn (ngắt giữa từ hoặc bị cắt):\n%s", w, rn)
				break
			}
		}
	})

	// ---- Đánh giá ----
	el := p.open(gd1Screen{"eval", "5", 0}, func(s string) bool { return strings.Contains(s, gd1EvalAuthor) || strings.Contains(s, gd1EvalEmail) })
	t.Run("L-EVAL/eval", func(t *testing.T) {
		letter := regexp.MustCompile(`\p{L}{2,}`)
		for _, l := range strings.Split(el, "\n") {
			if strings.Contains(l, "c7f3a9c21") || strings.Contains(l, "c4b8d2e90") {
				continue
			}
			if strings.Contains(l, "✓") && strings.Contains(l, "!") && strings.Contains(l, "✗") && letter.MatchString(l) {
				return
			}
		}
		t.Errorf("L-EVAL/eval: không có dòng chú giải cho các ký hiệu ✓ ! ✗ của danh sách ứng viên:\n%s", el)
	})
	t.Run("D-EMAIL/eval", func(t *testing.T) { gd1CheckEmail(t, "D-EMAIL/eval", el, gd1EvalAuthor) })
	t.Run("D-TIME/eval", func(t *testing.T) { gd1CheckTime(t, "D-TIME/eval", el) })

	// ---- Tiến hoá ----
	ev := p.open(gd1Screen{"evolve", "6", 0}, nil)
	t.Run("L-EVOLVE/evolve", func(t *testing.T) {
		left, _ := gd1Panes(ev)
		joined := strings.Join(left, "\n")
		for _, id := range f.ids {
			if strings.Contains(joined, id[:10]) {
				t.Errorf("L-EVOLVE/evolve: danh sách cơ hội còn hiện ULID tri thức %s thay cho tiêu đề:\n%s", id, ev)
				break
			}
		}
		if !strings.Contains(joined, gd1Prefix(gd1Items[gd1ItemDupA].title, 10)) {
			t.Errorf("L-EVOLVE/evolve: danh sách cơ hội không hiện tiêu đề mục tri thức %q:\n%s", gd1Items[gd1ItemDupA].title, ev)
		}
	})
	p.press(".")
	p.until(func() bool {
		txt, _ := gd1RightText(p.screen())
		return strings.Contains(txt, "agent:fake") && !strings.Contains(txt, "Đang tải")
	}, 30*time.Second)
	p.settle()
	evp := p.screen()
	t.Run("D-TIME/evolve", func(t *testing.T) { gd1CheckTime(t, "D-TIME/evolve", evp) })

	// ---- Hệ thống ----
	sy := p.open(gd1Screen{"system", "7", 2}, func(s string) bool { return strings.Contains(s, "test-before-stop") })
	t.Run("D-EMAIL/system", func(t *testing.T) { gd1CheckEmail(t, "D-EMAIL/system", sy, gd1SysName) })
}

// ---- Split × 7 màn ----

func gd1SplitCells(t *testing.T, f *gd1Fixture) {
	const w, h = 140, 40
	step := w * 5 / 100
	o := gd1Options(f)
	localPath := filepath.Join(f.root, ".bint", "local.yml")
	for _, sc := range gd1SplitScreens {
		p := gd1Start(t, o, w, h)
		s := p.open(sc, nil)
		base, by := gd1Split(s)
		cell := func(kind string) string { return kind + "/" + sc.name }
		if base < 0 {
			for _, k := range []string{"S-GROW", "S-SHRINK", "S-ZOOM", "S-RESET", "S-DRAG", "S-SAVE", "S-CLAMP"} {
				t.Run(cell(k), func(t *testing.T) {
					t.Errorf("%s: màn %s ở %dx%d không có hai pane cạnh nhau:\n%s", cell(k), sc.name, w, h, s)
				})
			}
			continue
		}
		measure := func() int { c, _ := gd1Split(p.screen()); return c }

		p.press(">")
		grown := measure()
		t.Run(cell("S-GROW"), func(t *testing.T) {
			if d := grown - base; d < step-2 || d > step+2 {
				t.Errorf("%s: phím > phải nới pane trái thêm 5%% (~%d cột): cột đường chia %d → %d", cell("S-GROW"), step, base, grown)
			}
		})
		p.press("=")
		reset := measure()
		t.Run(cell("S-RESET"), func(t *testing.T) {
			if reset != base || grown == base {
				t.Errorf("%s: phím = phải đưa tỉ lệ về mặc định: mặc định %d, sau > là %d, sau = là %d", cell("S-RESET"), base, grown, reset)
			}
		})

		p.press("<")
		shrunk := measure()
		t.Run(cell("S-SHRINK"), func(t *testing.T) {
			if d := base - shrunk; d < step-2 || d > step+2 {
				t.Errorf("%s: phím < phải thu pane trái 5%% (~%d cột): cột đường chia %d → %d", cell("S-SHRINK"), step, base, shrunk)
			}
		})
		p.press("=")

		p.press("z")
		zs := p.screen()
		zok, why := gd1Zoomed(zs, w)
		p.press("z")
		back := measure()
		t.Run(cell("S-ZOOM"), func(t *testing.T) {
			if !zok {
				t.Errorf("%s: phím z phải phóng to pane đang chọn gần trọn màn: %s\n%s", cell("S-ZOOM"), why, zs)
			} else if !strings.Contains(zs, "▸") {
				t.Errorf("%s: pane phóng to không phải pane đang chọn (mất danh sách có con trỏ):\n%s", cell("S-ZOOM"), zs)
			}
			if back != base {
				t.Errorf("%s: nhấn z lần nữa phải trả lại bố cục cũ: đường chia %d, muốn %d", cell("S-ZOOM"), back, base)
			}
		})
		p.press("=")

		cur := p.screen()
		c0, y0 := gd1Split(cur)
		if c0 < 0 {
			c0, y0 = base, by
		}
		y := gd1DividerRow(cur, c0, y0)
		p.deliver(tea.MouseClickMsg{X: c0, Y: y, Button: tea.MouseLeft})
		p.deliver(tea.MouseMotionMsg{X: c0 + 5, Y: y, Button: tea.MouseLeft})
		p.deliver(tea.MouseMotionMsg{X: c0 + 10, Y: y, Button: tea.MouseLeft})
		p.deliver(tea.MouseReleaseMsg{X: c0 + 10, Y: y, Button: tea.MouseLeft})
		p.settle()
		dragged := measure()
		t.Run(cell("S-DRAG"), func(t *testing.T) {
			if d := dragged - (c0 + 10); d < -2 || d > 2 {
				t.Errorf("%s: kéo đường chia (cột %d, dòng %d) sang phải 10 ô phải dời đường chia tới ~%d, có %d", cell("S-DRAG"), c0, y, c0+10, dragged)
			}
		})
		p.press("=")

		before, _ := os.ReadFile(localPath)
		p.press(">")
		saved := measure()
		keyRe := regexp.MustCompile(`(^|[\s{,])` + sc.name + `\s*:\s*\d+`)
		var after []byte
		p.until(func() bool {
			after, _ = os.ReadFile(localPath)
			return strings.Contains(string(after), "layout") && keyRe.Match(after)
		}, 10*time.Second)
		t.Run(cell("S-SAVE"), func(t *testing.T) {
			txt := string(after)
			if !strings.Contains(txt, "layout") || !keyRe.MatchString(txt) {
				t.Errorf("%s: sau phím > .bint/local.yml phải có khoá ui.layout với màn %s:\n%s", cell("S-SAVE"), sc.name, txt)
				return
			}
			rest := txt
			for _, l := range strings.Split(strings.TrimRight(string(before), "\n"), "\n") {
				i := strings.Index(rest, l)
				if i < 0 {
					t.Errorf("%s: ghi tỉ lệ làm mất/sửa dòng không liên quan %q của .bint/local.yml:\n--- trước\n%s--- sau\n%s", cell("S-SAVE"), l, before, txt)
					return
				}
				rest = rest[i+len(l):]
			}
			q := gd1Start(t, o, w, h)
			q.open(sc, nil)
			if again, _ := gd1Split(q.screen()); again != saved || saved == base {
				t.Errorf("%s: mô hình mở lại phải nạp tỉ lệ đã lưu: đường chia %d, muốn %d (mặc định %d)", cell("S-SAVE"), again, saved, base)
			}
		})

		for range 20 {
			p.press(">")
		}
		clamped := measure()
		t.Run(cell("S-CLAMP"), func(t *testing.T) {
			if clamped > w*80/100+1 || clamped <= base {
				t.Errorf("%s: nhấn > 20 lần phải dừng ở 80%% (≤ cột %d), mặc định %d, có %d", cell("S-CLAMP"), w*80/100+1, base, clamped)
			}
		})
	}
}

// ---- GĐ0 còn mở: G-CTRLR, G-GATE ----

// gd1Cursor đưa con trỏ danh sách Tri thức tới mục có tiêu đề title; trả màn hình khi khung chi tiết đã hiện mục đó.
func (p *gd1Pump) gd1Cursor(title string) (string, bool) {
	want := gd1Prefix(title, 12)
	for range len(gd1Items) + 2 {
		p.press("k")
	}
	for range len(gd1Items) + 2 {
		s := p.screen()
		for _, l := range strings.Split(s, "\n") {
			if i := strings.Index(l, "▸"); i >= 0 && strings.Contains(l[i:], want) {
				p.until(func() bool {
					txt, _ := gd1RightText(p.screen())
					return strings.Contains(txt, want) && strings.Contains(txt, "Kiểm tra") && !strings.Contains(txt, "Đang tải")
				}, 30*time.Second)
				p.settle()
				return p.screen(), true
			}
		}
		p.press("j")
	}
	return p.screen(), false
}

// gd1HintLine trả dòng gợi ý phím ("→ …") của khối Kiểm tra ("" nếu không có). Lời giải thích của từng phát hiện có thể nhắc phím
// kèm chú thích "không dùng được ở chế độ chỉ đọc" (bộ viết lại chung), không phải gợi ý.
func gd1HintLine(block string) string {
	for _, l := range strings.Split(block, "\n") {
		if strings.Contains(l, "→") {
			return l
		}
	}
	return ""
}

// gd1CheckBlock trả khối Kiểm tra của khung chi tiết (dòng tiêu đề và tối đa 6 dòng sau).
func gd1CheckBlock(s string) string {
	_, right := gd1RightText(s)
	for i, r := range right {
		if strings.Contains(r, "Kiểm tra") {
			return strings.Join(right[i:min(i+7, len(right))], "\n")
		}
	}
	return ""
}

func gd1GD0Cells(t *testing.T, f *gd1Fixture) {
	gd1Write(t, f.root, ".bint/local.yml", gd1LocalYML) // bỏ tỉ lệ mà các ô Split đã lưu (S-CLAMP để 80%)
	p := gd1Start(t, gd1Options(f), 140, 40)
	p.deliver(keyMsg("2"))
	p.until(func() bool { return p.health > 0 && strings.Contains(p.screen(), gd1Prefix(gd1Items[0].title, 12)) }, 120*time.Second)
	p.settle()

	// Sửa mã của mục fact giữ nguyên kích thước và mtime: chỉ lượt kiểm ép (Ctrl+R) mới phát hiện được.
	fp := filepath.Join(f.root, filepath.FromSlash(gd1Items[gd1ItemFact].file))
	st, err := os.Stat(fp)
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(gd1Items[gd1ItemFact].src, "return p\n", "return 0\n", 1) // dòng 6, trong vùng bằng chứng 3-7
	if len(src) != len(gd1Items[gd1ItemFact].src) {
		t.Fatal("fixture: bản sửa phải giữ nguyên kích thước tệp")
	}
	gd1Write(t, f.root, gd1Items[gd1ItemFact].file, src)
	if err := os.Chtimes(fp, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	n := p.health
	p.deliver(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	factRow := func() string {
		left, _ := gd1Panes(p.screen())
		for _, l := range left {
			if i := strings.Index(l, gd1Prefix(gd1Items[gd1ItemFact].title, 12)); i >= 0 {
				return l
			}
		}
		return ""
	}
	p.until(func() bool { return p.health > n && strings.Contains(factRow(), "✗") }, 30*time.Second)
	p.settle()
	t.Run("G-CTRLR/knowledge", func(t *testing.T) {
		if p.health <= n {
			t.Errorf("G-CTRLR/knowledge: Ctrl+R không chạy lại kiểm sức khoẻ (số lượt kiểm vẫn %d)", n)
		}
		if row := factRow(); !strings.Contains(row, "✗") {
			t.Errorf("G-CTRLR/knowledge: sau Ctrl+R mục có bằng chứng đã đổi phải hiện ✗, hàng: %q\n%s", strings.TrimSpace(row), p.screen())
		}
	})

	// G-GATE: chế độ chỉ đọc không gợi ý f/v; cảnh báo không phải relocated/moved không gợi ý v.
	if err := os.Remove(filepath.Join(f.root, filepath.FromSlash(gd1DocRef))); err != nil {
		t.Fatal(err)
	}
	ro := gd1Options(f)
	ro.ReadOnly = true
	q := gd1Start(t, ro, 140, 40)
	q.deliver(keyMsg("2"))
	q.until(func() bool { return q.health > 0 && strings.Contains(q.screen(), gd1Prefix(gd1Items[0].title, 12)) }, 120*time.Second)
	q.settle()
	t.Run("G-GATE/knowledge_readonly", func(t *testing.T) {
		s, ok := q.gd1Cursor(gd1Items[gd1ItemFact].title)
		if !ok {
			t.Fatalf("G-GATE/knowledge: không đưa được con trỏ tới %q:\n%s", gd1Items[gd1ItemFact].title, s)
		}
		b := gd1CheckBlock(s)
		if !strings.Contains(b, "✗") {
			t.Fatalf("G-GATE/knowledge: khối Kiểm tra của mục lỗi không có ✗:\n%s", s)
		}
		if h := gd1HintLine(b); gd1KeyF.MatchString(h) || gd1KeyV.MatchString(h) {
			t.Errorf("G-GATE/knowledge: --read-only vẫn gợi ý phím f/v ở khối Kiểm tra:\n%s", b)
		}
	})

	r := gd1Start(t, gd1Options(f), 140, 40)
	r.deliver(keyMsg("2"))
	r.until(func() bool { return r.health > 0 && strings.Contains(r.screen(), gd1Prefix(gd1Items[0].title, 12)) }, 120*time.Second)
	r.settle()
	t.Run("G-GATE/knowledge_warn_not_relocated", func(t *testing.T) {
		s, ok := r.gd1Cursor(gd1Items[gd1ItemPathRef].title)
		if !ok {
			t.Fatalf("G-GATE/knowledge: không đưa được con trỏ tới %q:\n%s", gd1Items[gd1ItemPathRef].title, s)
		}
		b := gd1CheckBlock(s)
		if !strings.Contains(b, "!") {
			t.Fatalf("G-GATE/knowledge: khối Kiểm tra của mục cảnh báo path-ref không có !:\n%s", s)
		}
		if gd1KeyV.MatchString(gd1HintLine(b)) {
			t.Errorf("G-GATE/knowledge: cảnh báo không phải relocated/moved vẫn gợi ý phím v:\n%s", b)
		}
	})
}

// sqlInt dựng sql.NullInt64 hợp lệ.
func sqlInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func gd1Write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
