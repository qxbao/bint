package app

// Kiểm tra mục tiêu (b-goal) của giai đoạn 0 đại tu TUI: thông tin quan trọng không bị che hay hiển thị sai ở bốn nơi —
// danh sách/tiêu đề/chi tiết Tri thức, "Việc cần bạn" ở Tổng quan (100×30) và thông điệp rỗng ở màn Tác vụ.
//
// Hộp đen qua mô hình gốc (Render đã bỏ màu). Màn Tri thức dùng kiểm tra THẬT: repository tạm với 57 mục tri thức
// có bằng chứng thật, rồi sửa mã để 10 mục lỗi fingerprint, 1 mục có khối bằng chứng chuyển sang tệp khác, 16 mục dời
// dòng (khối dịch xuống, nội dung giữ nguyên), 30 mục sạch. Trạng thái "đang kiểm" (K-PEND) đo ở lần vẽ ngay sau khi
// danh sách nạp xong, trước khi bất kỳ lệnh nền nào của lần nạp đó chạy (yêu cầu: lần vẽ đầu không chờ kết quả).
// Tổng quan dùng bộ nạp thật trên cùng repository (để mọi nguồn việc mới mà bản cài đặt tự tính đều có mặt), rồi bơm
// thêm các nguồn giả: 81 ứng viên đánh giá, 6 tác vụ bị chặn, 6 đề xuất, 6 thành phần, Doctor 1 lỗi + 3 cảnh báo,
// core.lock lệch phiên bản.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/detect"
	"bint/internal/knowledge"
	"bint/internal/task"
	"bint/internal/testutil"
	knscreen "bint/internal/tui/screens/knowledge"
	"bint/internal/tui/screens/overview"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
)

// ---- bơm thông điệp bất đồng bộ (lệnh nền chạy trong goroutine, Update chạy trên goroutine của kiểm thử) ----

type gd0Pump struct {
	t    *testing.T
	m    *Model
	msgs chan tea.Msg
	// hook chạy ngay sau Update của một thông điệp, TRƯỚC khi lệnh trả về được chạy.
	hook func(tea.Msg)
}

func (p *gd0Pump) run(c tea.Cmd) {
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

func (p *gd0Pump) deliver(msg tea.Msg) {
	_, next := p.m.Update(msg)
	if p.hook != nil {
		p.hook(msg)
	}
	p.run(next)
}

// until nhận thông điệp tới khi cond đúng hoặc hết hạn (hạn chỉ chặn treo); trả cond cuối cùng.
func (p *gd0Pump) until(cond func() bool, limit time.Duration) bool {
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

// settle nhận thông điệp tới khi yên lặng ngắn.
func (p *gd0Pump) settle() {
	end := time.After(2 * time.Second)
	for {
		select {
		case msg := <-p.msgs:
			p.deliver(msg)
		case <-time.After(30 * time.Millisecond):
			return
		case <-end:
			return
		}
	}
}

func (p *gd0Pump) press(keys ...string) {
	for _, k := range keys {
		p.deliver(keyMsg(k))
		p.settle()
	}
}

func (p *gd0Pump) screen() string { return plain(p.m.Render()) }

// ---- dữ liệu Tri thức ----

type gd0Kind string

const (
	kERR   gd0Kind = "K-ERR"
	kMOVED gd0Kind = "K-MOVED"
	kWARN  gd0Kind = "K-WARN"
	kOK    gd0Kind = "K-OK"
)

const gd0Items = 57

// gd0KindOf: phân loại cố định, xen kẽ để thứ tự gốc (theo thời điểm tạo) KHÔNG trùng thứ tự theo sức khoẻ.
func gd0KindOf(i int) gd0Kind {
	switch i {
	case 3, 8, 14, 19, 25, 31, 37, 42, 48, 55:
		return kERR
	case 50:
		return kMOVED
	case 1, 5, 10, 12, 17, 22, 27, 29, 33, 35, 39, 44, 46, 52, 54, 56:
		return kWARN
	}
	return kOK
}

var gd0Titles = []string{
	"Đóng tệp sau khi đọc cấu hình",
	"Không gọi sql.Open trực tiếp",
	"Handler phải đi qua wrap()",
	"Khoá ghi giữ trong suốt giao dịch",
	"Đặt timeout cho mọi lệnh git",
	"Không ghi log chứa token",
	"Dùng fsafe.WriteAtomic khi ghi tệp",
}

func gd0Tag(i int) string   { return fmt.Sprintf("N%02d ·", i) }
func gd0Title(i int) string { return fmt.Sprintf("N%02d · %s", i, gd0Titles[i%len(gd0Titles)]) }
func gd0File(i int) string  { return fmt.Sprintf("src/f%02d.go", i) }

// gd0Block là khối bằng chứng (dòng 5-8 của tệp) — nội dung riêng cho từng mục.
func gd0Block(i int) []string {
	return []string{
		fmt.Sprintf("func open%02d(path string) (*os.File, error) {", i),
		fmt.Sprintf("\tf, err := os.OpenFile(path, os.O_RDONLY, 0o%03d)", 600+i),
		fmt.Sprintf("\treturn f, wrap%02d(err)", i),
		"}",
	}
}

func gd0Source(i int, shift int) string {
	var b strings.Builder
	b.WriteString("package src\n\n")
	for k := range shift {
		fmt.Fprintf(&b, "// dòng chèn thêm %d của tệp %02d\n", k, i)
	}
	fmt.Fprintf(&b, "// Tệp %02d: mở tệp có kiểm tra lỗi.\n", i)
	fmt.Fprintf(&b, "var limit%02d = %d\n", i, 1000+i)
	for _, l := range gd0Block(i) {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\nfunc tail%02d() int { return %d }\n", i, 2000+i)
	return b.String()
}

func gd0Write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gd0KnowledgeRepo dựng repository đã init có 57 mục candidate với bằng chứng thật, rồi sửa mã theo gd0KindOf.
func gd0KnowledgeRepo(t *testing.T) string {
	t.Helper()
	files := map[string]string{"README.md": "# demo\n"}
	for i := range gd0Items {
		files[gd0File(i)] = gd0Source(i, 0)
	}
	root := testutil.NewRepo(t, files)
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	ctx := context.Background()
	if _, err := bintapp.Init(ctx, bintapp.InitOptions{Dir: root, Hooks: boolPtr(false), NoSkills: true}); err != nil {
		t.Fatal(err)
	}
	k, err := bintapp.OpenKnowledge(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	for i := range gd0Items {
		res, err := bintapp.AddKnowledge(ctx, k, bintapp.AddOptions{
			Type: "warning", Title: gd0Title(i),
			Body:     fmt.Sprintf("Quy ước số %02d: hàm mở tệp của mô-đun %02d phải bọc lỗi và đóng tệp ở mọi nhánh trả về.", i, i),
			Evidence: []string{gd0File(i) + ":5-8"}, Origin: "agent", Now: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil || !res.Saved {
			t.Fatalf("thêm mục %d: err=%v saved=%v findings=%+v", i, err, res.Saved, res.Check.Findings)
		}
	}
	// Sửa mã sau khi ghi tri thức (người dùng tiếp tục làm việc).
	for i := range gd0Items {
		switch gd0KindOf(i) {
		case kERR: // sửa tại chỗ dòng giữa khối: fingerprint đổi
			gd0Write(t, root, gd0File(i), strings.Replace(gd0Source(i, 0), fmt.Sprintf("0o%03d", 600+i), "0o644", 1))
		case kWARN: // chèn 3 dòng phía trên: khối dời xuống, nội dung giữ nguyên
			gd0Write(t, root, gd0File(i), gd0Source(i, 3))
		case kMOVED: // khối chuyển sang tệp khác, tệp cũ bị xoá
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(gd0File(i)))); err != nil {
				t.Fatal(err)
			}
			gd0Write(t, root, "src/moved_dest.go", "package src\n\n// Khối chuyển từ tệp cũ sang đây.\n\n"+strings.Join(gd0Block(i), "\n")+"\n")
		}
	}
	// Kiểm chứng dữ liệu giả bằng chính kiểm tra xác định được (như `bint knowledge verify`, không áp): sai ở đây là lỗi
	// của fixture, không phải của TUI.
	rep, err := bintapp.VerifyKnowledge(ctx, k, nil, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		it := rep.Items[r.ID]
		if it == nil {
			continue
		}
		mm := gd0TagRe.FindStringSubmatch(it.Title)
		if mm == nil {
			continue
		}
		i, _ := strconv.Atoi(mm[1])
		var e, w bool
		for _, f := range r.Findings {
			e = e || f.Level == knowledge.LevelError
			w = w || f.Level == knowledge.LevelWarn
		}
		got := kOK
		switch {
		case e && i == 50:
			got = kMOVED
		case e:
			got = kERR
		case w:
			got = kWARN
		}
		if got != gd0KindOf(i) {
			t.Fatalf("fixture: mục %q kiểm ra %s, muốn %s: %+v", it.Title, got, gd0KindOf(i), r.Findings)
		}
	}
	return root
}

// ---- trích thông tin từ khung đã vẽ ----

var gd0TagRe = regexp.MustCompile(`N(\d{2}) ·`)

// leftTag trả chỉ số mục và vị trí byte của thẻ đầu tiên (trái nhất) trên dòng.
func gd0LeftTag(line string) (int, int, bool) {
	loc := gd0TagRe.FindStringSubmatchIndex(line)
	if loc == nil {
		return 0, 0, false
	}
	n, _ := strconv.Atoi(line[loc[2]:loc[3]])
	return n, loc[0], true
}

// listTitleLine: dòng viền trên của khung danh sách Tri thức (dòng có viền và tổng số mục).
func gd0ListTitleLine(screen string) string {
	for l := range strings.SplitSeq(screen, "\n") {
		// "/57)" chỉ có trong số đếm "(shown/57)" của tiêu đề khung; đường dẫn tạm trên thanh tab có thể chứa "57" ngẫu nhiên.
		if strings.Contains(l, "─") && strings.Contains(l, "/"+strconv.Itoa(gd0Items)+")") && !gd0TagRe.MatchString(l) {
			// Chỉ phần của khung danh sách (khung chi tiết đứng cạnh ở bố cục rộng).
			if i := strings.Index(l, "┐"); i > 0 {
				l = l[:i]
			}
			return l
		}
	}
	return ""
}

var (
	gd0ErrCountRe  = regexp.MustCompile(`✗\s?(\d+)`)
	gd0WarnCountRe = regexp.MustCompile(`!\s?(\d+)`)
)

func gd0Count(re *regexp.Regexp, s string) (int, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

type gd0Health struct{ err, warn, ok bool }

func gd0HealthOf(prefix string) gd0Health {
	return gd0Health{err: strings.Contains(prefix, "✗"), warn: strings.Contains(prefix, "!"), ok: strings.Contains(prefix, "✓")}
}

func (h gd0Health) String() string {
	var s []string
	if h.err {
		s = append(s, "✗")
	}
	if h.warn {
		s = append(s, "!")
	}
	if h.ok {
		s = append(s, "✓")
	}
	if len(s) == 0 {
		return "(không có ✗/!/✓)"
	}
	return strings.Join(s, "")
}

// rowPrefixes: với mỗi dòng có thẻ mục, phần đứng trước thẻ trái nhất (các cột ký hiệu của hàng danh sách).
func gd0RowPrefixes(screen string, into map[int]string) {
	for _, l := range strings.Split(screen, "\n") {
		if n, pos, ok := gd0LeftTag(l); ok {
			into[n] = l[:pos]
		}
	}
}

// cursorTag: mục trên hàng có con trỏ ▸ (thẻ đầu tiên sau ▸).
func gd0CursorTag(screen string) (int, bool) {
	for _, l := range strings.Split(screen, "\n") {
		i := strings.Index(l, "▸")
		if i < 0 {
			continue
		}
		if n, _, ok := gd0LeftTag(l[i:]); ok {
			return n, true
		}
	}
	return 0, false
}

var gd0KeyF = regexp.MustCompile(`(^|[^\p{L}\p{N}_/.])f([^\p{L}\p{N}_]|$)`)
var gd0KeyV = regexp.MustCompile(`(^|[^\p{L}\p{N}_/.])v([^\p{L}\p{N}_]|$)`)

// trimBox bỏ viền/khoảng trắng đầu dòng.
func gd0TrimBox(l string) string { return strings.TrimLeft(l, " │┃|▸") }

// ---- kiểm tra mục tiêu ----

func TestGoalGD0TUIVisibility(t *testing.T) {
	testutil.Isolate(t)
	asHuman(t)
	root := gd0KnowledgeRepo(t)
	kinds := map[gd0Kind][]int{}
	for i := range gd0Items {
		kinds[gd0KindOf(i)] = append(kinds[gd0KindOf(i)], i)
	}

	// Tổng quan: bộ nạp thật + các nguồn giả có số lượng như ngoài đời.
	ovLoader := func(ctx context.Context, dir string) (overview.Data, error) {
		d, err := overview.Load(ctx, dir)
		if err != nil {
			return d, err
		}
		if d.Pending == nil {
			d.Pending = &bintapp.PendingWorkReport{}
		}
		p := d.Pending
		p.EvalCandidates = nil
		for i := range 81 {
			p.EvalCandidates = append(p.EvalCandidates, fmt.Sprintf("01M7EVAL%010d", i))
		}
		p.BlockedTasks, p.Proposals, p.Components = nil, nil, nil
		for i := range 6 {
			p.BlockedTasks = append(p.BlockedTasks, bintapp.PendingTask{ID: fmt.Sprintf("01M7TASK%06d", i), Goal: fmt.Sprintf("Thêm cột email số %d", i), Reason: "thay đổi cần người duyệt: migrations/002.sql"})
			p.Proposals = append(p.Proposals, bintapp.PendingProposal{ID: fmt.Sprintf("01M7PROP%06d", i), Action: "update"})
			p.Components = append(p.Components, bintapp.PendingComponent{Kind: "guidance", ID: fmt.Sprintf("guid-%d", i)})
		}
		checks := []bintapp.Check{
			{Name: "hook claude", Level: bintapp.LevelFail, Detail: "tệp hook không chạy được: exit status 127"},
			{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ, thiếu 2 khoá"},
			{Name: "git", Level: bintapp.LevelWarn, Detail: "git 2.20 cũ hơn bản khuyến nghị 2.30"},
			{Name: "harness codex", Level: bintapp.LevelWarn, Detail: "codex chưa đăng nhập"},
		}
		p.Doctor = checks
		d.Doctor = &bintapp.DoctorReport{Checks: append([]bintapp.Check{{Name: "state", Level: bintapp.LevelOK, Detail: "ok"}}, checks...)}
		p.DoctorAll = d.Doctor
		d.LockVersion = "0.9.0"
		return d, nil
	}

	o := Options{Dir: root, Version: "1.0.0", Lang: "vi", Mode: theme.Mono, Loader: ovLoader,
		DetectHarnesses: func() []detect.Harness { return nil }, SpinnerFPS: time.Hour}
	m := New(context.Background(), o)
	m.SetSize(140, 40)
	p := &gd0Pump{t: t, m: m, msgs: make(chan tea.Msg, 4096)}
	p.run(m.Init())
	if !p.until(func() bool { return !m.ov.Loading }, 120*time.Second) {
		t.Fatalf("Tổng quan không nạp xong:\n%s", p.screen())
	}

	// Mở Tri thức; chụp lần vẽ ngay sau khi danh sách nạp xong (trước mọi lệnh nền phát sinh từ lần nạp).
	pend := ""
	p.hook = func(msg tea.Msg) {
		if _, ok := msg.(knscreen.LoadedMsg); ok && pend == "" {
			pend = p.screen()
		}
	}
	p.deliver(keyMsg("2"))
	if !p.until(func() bool { return pend != "" }, 120*time.Second) {
		t.Fatalf("Tri thức không nạp xong:\n%s", p.screen())
	}
	p.hook = nil

	// Chờ kết quả kiểm tra nền: tiêu đề khung có đủ số ✗ và !. Hết hạn không dừng: từng ô tự báo chỗ sót.
	final := func() bool {
		tl := gd0ListTitleLine(p.screen())
		e, okE := gd0Count(gd0ErrCountRe, tl)
		w, okW := gd0Count(gd0WarnCountRe, tl)
		return okE && okW && e >= 10 && w >= 16
	}
	p.until(final, 60*time.Second)
	p.settle()
	listScreen := p.screen()
	titleLine := gd0ListTitleLine(listScreen)

	// Đi hết danh sách bằng con trỏ: thứ tự hàng và cột ký hiệu của từng hàng.
	for i := 0; i < gd0Items+3; i++ {
		p.press("k")
	}
	prefixes := map[int]string{}
	order := []int{}
	seen := map[int]bool{}
	for i := 0; i < gd0Items+3; i++ {
		s := p.screen()
		gd0RowPrefixes(s, prefixes)
		if n, ok := gd0CursorTag(s); ok && !seen[n] {
			seen[n] = true
			order = append(order, n)
		}
		p.press("j")
	}
	pos := map[int]int{}
	for i, n := range order {
		pos[n] = i
	}

	wantGlyph := map[gd0Kind]string{kERR: "✗", kMOVED: "✗", kWARN: "!", kOK: "✓"}
	glyphOK := func(k gd0Kind, h gd0Health) bool {
		switch wantGlyph[k] {
		case "✗":
			return h.err && !h.ok
		case "!":
			return h.warn && !h.err && !h.ok
		}
		return h.ok && !h.err && !h.warn
	}
	rank := map[gd0Kind]int{kERR: 0, kMOVED: 0, kWARN: 1, kOK: 2}

	// ---- N-LIST ----
	for _, k := range []gd0Kind{kERR, kMOVED, kWARN, kOK} {
		t.Run(string(k)+"/N-LIST", func(t *testing.T) {
			for _, i := range kinds[k] {
				pre, ok := prefixes[i]
				if !ok {
					t.Errorf("%s/N-LIST: mục %q không thấy trên danh sách Tri thức", k, gd0Title(i))
					continue
				}
				if h := gd0HealthOf(pre); !glyphOK(k, h) {
					t.Errorf("%s/N-LIST: hàng %q có ký hiệu sức khoẻ %s, muốn %s (hàng: %q)", k, gd0Title(i), h, wantGlyph[k], strings.TrimSpace(pre))
				}
				pi, ok := pos[i]
				if !ok {
					t.Errorf("%s/N-LIST: con trỏ không đi qua mục %q", k, gd0Title(i))
					continue
				}
				for j := range gd0Items {
					pj, ok := pos[j]
					if !ok {
						continue
					}
					kj := gd0KindOf(j)
					if rank[kj] > rank[k] && pj < pi {
						t.Errorf("%s/N-LIST: %q (%s) đứng sau %q (%s) — mặc định phải ✗ trước, rồi !, rồi phần còn lại", k, gd0Title(i), k, gd0Title(j), kj)
						break
					}
					if rank[kj] < rank[k] && pj > pi {
						t.Errorf("%s/N-LIST: %q (%s) đứng trước %q (%s) — mặc định phải ✗ trước, rồi !, rồi phần còn lại", k, gd0Title(i), k, gd0Title(j), kj)
						break
					}
				}
			}
		})
	}
	t.Run("K-PEND/N-LIST", func(t *testing.T) {
		pp := map[int]string{}
		gd0RowPrefixes(pend, pp)
		if len(pp) == 0 {
			t.Fatalf("K-PEND/N-LIST: lần vẽ đầu sau khi nạp không có hàng nào:\n%s", pend)
		}
		for n, pre := range pp {
			if h := gd0HealthOf(pre); h.err || h.warn || h.ok {
				t.Errorf("K-PEND/N-LIST: lần vẽ đầu (chưa có kết quả kiểm tra) hàng %q đã có kết luận %s, muốn ký hiệu \"đang kiểm\" (kiểm tra phải chạy nền, không chặn lần vẽ đầu)", gd0Title(n), h)
			}
		}
	})

	// ---- N-TITLE ----
	errN, hasErr := gd0Count(gd0ErrCountRe, titleLine)
	warnN, hasWarn := gd0Count(gd0WarnCountRe, titleLine)
	t.Run("K-ERR/N-TITLE", func(t *testing.T) {
		if !hasErr || errN < len(kinds[kERR]) {
			t.Errorf("K-ERR/N-TITLE: tiêu đề khung danh sách không có số mục lỗi ✗%d (hoặc hơn): %q", len(kinds[kERR]), titleLine)
		}
	})
	t.Run("K-MOVED/N-TITLE", func(t *testing.T) {
		want := len(kinds[kERR]) + len(kinds[kMOVED])
		if !hasErr || errN != want {
			t.Errorf("K-MOVED/N-TITLE: tiêu đề khung danh sách phải đếm cả mục khối chuyển tệp: muốn ✗%d, dòng tiêu đề %q", want, titleLine)
		}
	})
	t.Run("K-WARN/N-TITLE", func(t *testing.T) {
		if !hasWarn || warnN != len(kinds[kWARN]) {
			t.Errorf("K-WARN/N-TITLE: tiêu đề khung danh sách thiếu số cảnh báo !%d: %q", len(kinds[kWARN]), titleLine)
		}
	})
	t.Run("K-OK/N-TITLE", func(t *testing.T) {
		if !strings.Contains(titleLine, strconv.Itoa(gd0Items)) {
			t.Errorf("K-OK/N-TITLE: tiêu đề khung danh sách không có tổng số mục %d: %q", gd0Items, titleLine)
		}
		if hasErr && errN > len(kinds[kERR])+len(kinds[kMOVED]) || hasWarn && warnN > len(kinds[kWARN]) {
			t.Errorf("K-OK/N-TITLE: mục sạch bị đếm vào lỗi/cảnh báo: %q", titleLine)
		}
	})
	t.Run("K-PEND/N-TITLE", func(t *testing.T) {
		tl := gd0ListTitleLine(pend)
		if tl == "" {
			t.Fatalf("K-PEND/N-TITLE: lần vẽ đầu không có dòng tiêu đề khung danh sách:\n%s", pend)
		}
		if n, ok := gd0Count(gd0ErrCountRe, tl); ok && n > 0 {
			t.Errorf("K-PEND/N-TITLE: lần vẽ đầu (chưa có kết quả) tiêu đề đã báo ✗%d: %q", n, tl)
		}
		if n, ok := gd0Count(gd0WarnCountRe, tl); ok && n > 0 {
			t.Errorf("K-PEND/N-TITLE: lần vẽ đầu (chưa có kết quả) tiêu đề đã báo !%d: %q", n, tl)
		}
	})

	// ---- N-DETAIL (80×24) ----
	m.SetSize(80, 24)
	p.settle()
	loading := "Đang tải…"
	openDetail := func(i int) (string, bool) {
		for k := 0; k < gd0Items+3; k++ {
			p.press("k")
		}
		for k := 0; k < gd0Items+3; k++ {
			if n, ok := gd0CursorTag(p.screen()); ok && n == i {
				p.press("enter")
				p.until(func() bool {
					s := p.screen()
					return strings.Contains(s, gd0Tag(i)) && !strings.Contains(s, loading)
				}, 20*time.Second)
				p.settle()
				s := p.screen()
				p.press("esc")
				return s, true
			}
			p.press("j")
		}
		return p.screen(), false
	}
	detailCell := func(k gd0Kind, i int) {
		t.Run(string(k)+"/N-DETAIL", func(t *testing.T) {
			s, ok := openDetail(i)
			if !ok {
				t.Fatalf("%s/N-DETAIL: không đưa được con trỏ tới %q:\n%s", k, gd0Title(i), s)
			}
			ls := strings.Split(s, "\n")
			ti := -1
			for j, l := range ls {
				if strings.Contains(l, gd0Tag(i)) {
					ti = j
					break
				}
			}
			if ti < 0 {
				t.Fatalf("%s/N-DETAIL: chi tiết không có dòng tiêu đề %q:\n%s", k, gd0Title(i), s)
			}
			near := ls[ti+1 : min(ti+4, len(ls))]
			summary := -1
			for j, l := range near {
				b := gd0TrimBox(l)
				if strings.HasPrefix(b, "✗") || strings.HasPrefix(b, "!") {
					summary = j
					break
				}
			}
			if k == kOK {
				if summary >= 0 {
					t.Errorf("K-OK/N-DETAIL: mục sạch lại có khối báo lỗi/cảnh báo ngay dưới tiêu đề: %q", near[summary])
				}
				return
			}
			want := "✗"
			if k == kWARN {
				want = "!"
			}
			if summary < 0 || !strings.HasPrefix(gd0TrimBox(near[summary]), want) {
				t.Errorf("%s/N-DETAIL: không có dòng tóm tắt Kiểm tra (%s) trong 3 dòng ngay dưới tiêu đề mục; màn hình 80×24:\n%s", k, want, s)
				return
			}
			end := min(ti+1+summary+10, len(ls)-2) // không tính chân phím/thanh trạng thái của màn
			block := strings.Join(ls[ti+1+summary:max(end, ti+2+summary)], "\n")
			re, key := gd0KeyF, "f (làm mới fingerprint)"
			if k == kWARN {
				re, key = gd0KeyV, "v (cập nhật vị trí)"
			}
			if !re.MatchString(block) {
				t.Errorf("%s/N-DETAIL: khối Kiểm tra không có phím xử lý %s; khối:\n%s", k, key, block)
			}
		})
	}
	detailCell(kERR, kinds[kERR][0])
	detailCell(kMOVED, kinds[kMOVED][0])
	detailCell(kWARN, kinds[kWARN][0])
	detailCell(kOK, kinds[kOK][0])
	t.Run("K-PEND/N-DETAIL", func(t *testing.T) {
		// Lần vẽ đầu (140×40, có khung chi tiết của mục dưới con trỏ): chưa có kết quả thì không được báo lỗi giả.
		if strings.Contains(pend, "✗") {
			t.Errorf("K-PEND/N-DETAIL: lần vẽ đầu (chưa có kết quả kiểm tra) đã có ký hiệu lỗi ✗:\n%s", pend)
		}
	})

	// ---- N-OV (Tổng quan 100×30) ----
	m.SetSize(100, 30)
	p.press("1")
	p.settle()
	ovScreen := p.screen()
	gd0OverviewCells(t, ovScreen)

	// ---- N-TASKS (80×24) ----
	gd0TaskCells(t)
}

// ---- Tổng quan ----

type gd0OvRow struct {
	idx   int
	level string // ✗ ! ◌
	text  string
}

func gd0OverviewCells(t *testing.T, screen string) {
	var rows []gd0OvRow
	for _, l := range strings.Split(screen, "\n") {
		b := strings.TrimSpace(strings.TrimLeft(l, " │▸"))
		if !strings.Contains(b, "→") {
			continue
		}
		r, _ := utf8.DecodeRuneInString(b)
		switch r {
		case '✗', '!', '◌':
			rows = append(rows, gd0OvRow{idx: len(rows), level: string(r), text: b})
		}
	}
	classify := func(s string) string {
		l := strings.ToLower(s)
		switch {
		case strings.Contains(l, "lỗi thời"):
			return "O-KSTALE"
		case strings.Contains(l, "dời dòng"):
			return "O-KMOVE"
		case strings.Contains(l, "core.lock"):
			return "O-LOCK"
		case strings.Contains(l, "ứng viên đánh giá"):
			return "O-EVAL"
		case strings.Contains(l, "tri thức") && strings.Contains(l, "chờ duyệt"):
			return "O-KPEND"
		case strings.Contains(l, "tác vụ"):
			return "O-TASK"
		case strings.Contains(l, "đề xuất"):
			return "O-PROP"
		case strings.Contains(l, "thành phần"):
			return "O-COMP"
		case strings.Contains(l, "hook claude") || strings.Contains(l, "doctor") && strings.Contains(l, "lỗi") && !strings.Contains(l, "cảnh báo"):
			return "O-DFAIL"
		case strings.Contains(l, "doctor"):
			return "O-DWARN"
		}
		return ""
	}
	byType := map[string][]gd0OvRow{}
	for _, r := range rows {
		byType[classify(r.text)] = append(byType[classify(r.text)], r)
	}
	type spec struct {
		name, level string
		count       []int // số đếm chấp nhận trên dòng (rỗng = không đòi)
	}
	specs := []spec{
		{"O-KSTALE", "✗", []int{10, 11}}, {"O-DFAIL", "✗", nil},
		{"O-DWARN", "!", []int{3}}, {"O-KMOVE", "!", []int{16}}, {"O-LOCK", "!", nil}, {"O-TASK", "!", []int{6}},
		{"O-KPEND", "◌", []int{gd0Items}}, {"O-EVAL", "◌", []int{81}}, {"O-PROP", "◌", []int{6}}, {"O-COMP", "◌", []int{6}},
	}
	rank := map[string]int{"✗": 0, "!": 1, "◌": 2}
	numRe := regexp.MustCompile(`\d+`)
	for _, sp := range specs {
		t.Run(sp.name+"/N-OV", func(t *testing.T) {
			got := byType[sp.name]
			if len(got) != 1 {
				var txt []string
				for _, r := range got {
					txt = append(txt, r.text)
				}
				t.Errorf("%s/N-OV: muốn đúng 1 dòng ở \"Việc cần bạn\" (100×30), có %d: %q\nmàn hình:\n%s", sp.name, len(got), txt, screen)
				return
			}
			r := got[0]
			if r.level != sp.level {
				t.Errorf("%s/N-OV: dòng %q có mức %s, muốn %s", sp.name, r.text, r.level, sp.level)
			}
			for _, o := range rows {
				if rank[o.level] > rank[sp.level] && o.idx < r.idx {
					t.Errorf("%s/N-OV: dòng %s %q đứng sau dòng mức thấp hơn %q", sp.name, sp.level, r.text, o.text)
					break
				}
				if rank[o.level] < rank[sp.level] && o.idx > r.idx {
					t.Errorf("%s/N-OV: dòng %s %q đứng trước dòng mức cao hơn %q", sp.name, sp.level, r.text, o.text)
					break
				}
			}
			if len(sp.count) > 0 {
				found := false
				for _, s := range numRe.FindAllString(r.text, -1) {
					n, _ := strconv.Atoi(s)
					for _, c := range sp.count {
						found = found || n == c
					}
				}
				if !found {
					t.Errorf("%s/N-OV: dòng gộp không có số đếm %v: %q", sp.name, sp.count, r.text)
				}
			}
		})
	}
	t.Run("O-WINDOW/N-OV", func(t *testing.T) {
		if strings.Contains(screen, "… dòng") {
			t.Errorf("O-WINDOW/N-OV: hộp \"Việc cần bạn\" ở 100×30 bị cửa sổ cuộn che bớt dòng:\n%s", screen)
		}
	})
}

// ---- Tác vụ ----

func gd0Task(id, status, goal, at string) *task.Task {
	return &task.Task{Dir: "/state/tasks/" + id,
		Ticket: task.Ticket{ID: id, Goal: goal, Request: goal, Autonomy: task.AutonomyAskOnProtected,
			Scope: task.Scope{Allow: []string{"internal/**"}}, Verify: task.Verify{Command: "go test ./..."}},
		State: task.State{ID: id, Status: status, Workflow: "b-do", UpdatedAt: at, CreatedAt: at,
			History: []task.Transition{{To: task.StatusCreated, At: at, By: "human"}}}}
}

func gd0TaskCells(t *testing.T) {
	cases := []struct {
		name          string
		done, cancels int
	}{
		{"T-DONE", 50, 4}, // dữ liệu chỉ toàn phiếu đã xong như ngoài đời: 54 phiếu
		{"T-CANC", 0, 4},  // chỉ phiếu huỷ
	}
	for _, c := range cases {
		t.Run(c.name+"/N-TASKS", func(t *testing.T) {
			var tasks []*task.Task
			var goals []string
			for i := range c.done {
				g := fmt.Sprintf("Việc đã xong số %02d", i)
				goals = append(goals, g)
				tasks = append(tasks, gd0Task(fmt.Sprintf("01M7DONE%06d", i), task.StatusDone, g, fmt.Sprintf("2026-11-%02dT10:00:00Z", 1+i%20)))
			}
			for i := range c.cancels {
				g := fmt.Sprintf("Việc đã huỷ số %02d", i)
				goals = append(goals, g)
				tasks = append(tasks, gd0Task(fmt.Sprintf("01M7CANC%06d", i), task.StatusCancelled, g, fmt.Sprintf("2026-11-%02dT09:00:00Z", 1+i)))
			}
			total := len(tasks)
			o := Options{Dir: "/work/demo", Version: "1.0.0", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Hour,
				DetectHarnesses: func() []detect.Harness { return nil },
				Loader: func(context.Context, string) (overview.Data, error) {
					return overview.Data{Status: &bintapp.StatusReport{Repo: "/work/demo", Harnesses: []string{"claude"}}}, nil
				},
				TasksLoader: func(context.Context, string, bool) (tkscreen.Data, error) {
					return tkscreen.Data{Stamp: time.Unix(1, 0), Root: "/work/demo", Tasks: tasks}, nil
				},
				TasksDetailer: func(_ context.Context, _ string, id string, _ bool) (tkscreen.Detail, error) {
					for _, x := range tasks {
						if x.State.ID == id {
							return tkscreen.Detail{TaskDetail: bintapp.TaskDetail{Task: x}}, nil
						}
					}
					return tkscreen.Detail{}, fmt.Errorf("không có %s", id)
				},
				TasksRuns: func(context.Context, string, string, bool) (bintapp.TaskRunsResult, error) {
					return bintapp.TaskRunsResult{}, nil
				},
			}
			m := New(context.Background(), o)
			m.SetSize(80, 24)
			p := &gd0Pump{t: t, m: m, msgs: make(chan tea.Msg, 4096)}
			p.run(m.Init())
			if !p.until(func() bool { return !m.ov.Loading }, 60*time.Second) {
				t.Fatalf("Tổng quan không nạp xong:\n%s", p.screen())
			}
			p.deliver(keyMsg("3"))
			p.until(func() bool { return !strings.Contains(p.screen(), "Đang tải…") }, 30*time.Second)
			p.settle()
			s := p.screen()
			for _, g := range goals {
				if strings.Contains(s, g) {
					t.Errorf("%s/N-TASKS: lọc mặc định phải ẩn phiếu đã xong nhưng thấy %q", c.name, g)
					break
				}
			}
			countRe := regexp.MustCompile(`(^|\D)` + strconv.Itoa(total) + `(\D|$)`)
			keyA := regexp.MustCompile(`(^|[^\p{L}\p{N}_])A([^\p{L}\p{N}_]|$)`)
			found := false
			for _, l := range strings.Split(s, "\n") {
				if countRe.MatchString(l) && keyA.MatchString(l) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s/N-TASKS: danh sách rỗng vì ẩn %d phiếu đã xong nhưng không dòng nào ghi số phiếu bị ẩn (%d) kèm phím A để hiện; màn hình 80×24:\n%s", c.name, total, total, s)
			}
			if strings.Contains(s, "chưa lọc") {
				t.Errorf("%s/N-TASKS: chip lọc ghi \"chưa lọc\" trong khi đang ẩn %d phiếu", c.name, total)
			}
			if strings.Contains(s, "Không có tác vụ nào khớp bộ lọc") {
				t.Errorf("%s/N-TASKS: rỗng do lọc mặc định nhưng ghi như lọc của người dùng (\"Không có tác vụ nào khớp bộ lọc.\")", c.name)
			}
		})
	}
}
