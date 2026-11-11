package app

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/detect"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/text"
	"bint/internal/tui/jobs"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// Mọi dòng của giao diện rộng đúng w ô và có đúng h dòng: mọi kích thước, chế độ màu và ngôn ngữ,
// cả khi dữ liệu có chữ tiếng Việt dài và locale giả kéo dài 40%.
func TestRenderFitsEverywhere(t *testing.T) {
	datas := map[string]overview.Data{"overview": sampleData(), "init": notInitData()}
	for name, data := range datas {
		for _, sz := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {140, 40}} {
			for _, mode := range allModes() {
				for _, lang := range []string{"vi", "en", "xx"} {
					m := newModel(t, Options{Mode: mode, Lang: lang}, data, sz[0], sz[1])
					for tab := 0; tab < 7; tab++ {
						if name == "init" && tab > 0 {
							break
						}
						out := m.Render()
						ls := strings.Split(out, "\n")
						if len(ls) != sz[1] {
							t.Fatalf("%s %v %s %s tab %d: %d dòng, muốn %d", name, sz, mode, lang, tab, len(ls), sz[1])
						}
						for i, l := range ls {
							if w := text.Width(l); w != sz[0] {
								t.Fatalf("%s %v %s %s tab %d dòng %d rộng %d: %q", name, sz, mode, lang, tab, i, w, plain(l))
							}
						}
						press(t, m, "]")
					}
				}
			}
		}
	}
}

func TestTooSmallShowsMessage(t *testing.T) {
	for _, sz := range [][2]int{{59, 24}, {80, 19}, {40, 10}, {1, 1}} {
		m := newModel(t, Options{Mode: theme.Mono}, sampleData(), sz[0], sz[1])
		out := plain(m.Render())
		if sz[0] >= 20 && !strings.Contains(out, "quá nhỏ") {
			t.Fatalf("%v: thiếu thông báo:\n%s", sz, out)
		}
		if got := len(strings.Split(out, "\n")); got != sz[1] {
			t.Fatalf("%v: %d dòng", sz, got)
		}
	}
	m := newModel(t, Options{Mode: theme.Mono, Lang: "en"}, sampleData(), 50, 12)
	if out := plain(m.Render()); !strings.Contains(out, "too small") || !strings.Contains(out, "60×20") {
		t.Fatalf("%s", out)
	}
	// Phóng to lại thì hiện giao diện.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if out := plain(m.Render()); strings.Contains(out, "too small") || !strings.Contains(out, "Overview") {
		t.Fatalf("%s", out)
	}
}

func TestHeaderHasSevenTabsAndBadge(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 140, 40)
	hdr := lines(m.Render())[1]
	for _, want := range []string{"Tổng quan", "Tri thức (3)", "Tác vụ", "Lượt chạy", "Đánh giá", "Tiến hoá", "Hệ thống"} {
		if !strings.Contains(hdr, want) {
			t.Errorf("thiếu tab %q: %s", want, hdr)
		}
	}
	// Hẹp: tab chưa chọn rút gọn, tab đang chọn giữ nhãn đầy đủ.
	m = newModel(t, Options{Mode: theme.Mono}, sampleData(), 80, 24)
	hdr = lines(m.Render())[1]
	if !strings.Contains(hdr, "Tổng quan") || !strings.Contains(hdr, "TT (3)") || strings.Contains(hdr, "Lượt chạy") {
		t.Fatalf("tab hẹp: %s", hdr)
	}
	press(t, m, "4")
	if hdr = lines(m.Render())[1]; !strings.Contains(hdr, "Lượt chạy") || !strings.Contains(hdr, "TQ") {
		t.Fatalf("tab hẹp sau khi chọn: %s", hdr)
	}
}

func TestTabNavigation(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
	for i, k := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		press(t, m, k)
		if m.Tab() != i {
			t.Fatalf("phím %s → tab %d", k, m.Tab())
		}
		if got := lines(m.Render())[3]; !strings.Contains(got, "│") {
			t.Fatalf("khung chính thiếu tiêu đề: %s", got)
		}
	}
	press(t, m, "]")
	if m.Tab() != 0 {
		t.Fatalf("] từ tab cuối phải vòng về đầu: %d", m.Tab())
	}
	press(t, m, "[")
	if m.Tab() != 6 {
		t.Fatalf("[ từ tab đầu phải về tab cuối: %d", m.Tab())
	}
	press(t, m, "tab") // ở Hệ thống Tab đổi khung (như Tri thức), không đổi tab
	if m.Tab() != 6 {
		t.Fatalf("Tab ở Hệ thống đổi khung, không đổi tab: %d", m.Tab())
	}
	press(t, m, "]")
	if m.Tab() != 0 {
		t.Fatalf("]: %d", m.Tab())
	}
	// Hệ thống là màn thật (giai đoạn 5a), không còn trang giữ chỗ.
	press(t, m, "7")
	out := plain(m.Render())
	if strings.Contains(out, "giai đoạn 5") || !strings.Contains(out, "Lớp nguồn") || !strings.Contains(out, "Cấu hình") {
		t.Fatalf("màn Hệ thống:\n%s", out)
	}
	press(t, m, "5")
	if out := plain(m.Render()); strings.Contains(out, "giai đoạn 4") || !strings.Contains(out, "Ứng viên (2)") {
		t.Fatalf("Đánh giá không còn là trang giữ chỗ:\n%s", out)
	}
	press(t, m, "4")
	if out := plain(m.Render()); strings.Contains(out, "giai đoạn 3") || !strings.Contains(out, "Lượt chạy 3/3") {
		t.Fatalf("Lượt chạy không còn là trang giữ chỗ:\n%s", out)
	}
	press(t, m, "6")
	if out := plain(m.Render()); strings.Contains(out, "giai đoạn 3") || !strings.Contains(out, "Cơ hội (2)") {
		t.Fatalf("Tiến hoá không còn là trang giữ chỗ:\n%s", out)
	}
}

// Hộp thư: Enter nhảy thẳng tới chỗ xử lý.
func TestInboxEnterJumpsToTarget(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
	press(t, m, "enter") // dòng đầu: lô tri thức chờ duyệt
	if m.Tab() != 1 {
		t.Fatalf("tab = %d, muốn 1 (Tri thức)", m.Tab())
	}
	press(t, m, "1", "j", "j", "enter") // dòng thứ ba: đề xuất tiến hoá
	if m.Tab() != 5 {
		t.Fatalf("tab = %d, muốn 5 (Tiến hoá)", m.Tab())
	}
}

// Đổi ngôn ngữ vi/en ngay trong phiên, không cần khởi động lại.
func TestLanguageSwitchChangesView(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 120, 30)
	vi := plain(m.Render())
	if !strings.Contains(vi, "Việc cần bạn") || !strings.Contains(vi, "Tổng quan") {
		t.Fatalf("vi:\n%s", vi)
	}
	press(t, m, "L")
	en := plain(m.Render())
	if m.Lang() != "en" || !strings.Contains(en, "Needs you") || !strings.Contains(en, "Overview") || strings.Contains(en, "Việc cần bạn") {
		t.Fatalf("en (lang=%s):\n%s", m.Lang(), en)
	}
	if !strings.Contains(en, "2 knowledge items awaiting review in batch nguon:lo-thu") || !strings.Contains(en, "Quit") {
		t.Fatalf("en thiếu chuỗi dịch (số nhiều, chân phím):\n%s", en)
	}
	press(t, m, "L")
	if m.Lang() != "vi" || !strings.Contains(plain(m.Render()), "Việc cần bạn") {
		t.Fatal("phải quay lại vi")
	}
	// Qua bảng lệnh cũng đổi được.
	press(t, m, ":")
	for _, r := range "doi ngon" {
		press(t, m, string(r))
	}
	press(t, m, "enter")
	if m.Lang() != "en" {
		t.Fatalf("đổi qua bảng lệnh: %s", m.Lang())
	}
}

func TestPaletteSearchesAndRuns(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
	press(t, m, ":")
	if out := plain(m.Render()); !strings.Contains(out, "Bảng lệnh") || !strings.Contains(out, "Chuyển tới Tri thức") {
		t.Fatalf("bảng lệnh:\n%s", out)
	}
	for _, r := range "tri thuc" {
		press(t, m, string(r))
	}
	press(t, m, "enter")
	if m.Tab() != 1 {
		t.Fatalf("chọn 'Chuyển tới Tri thức' → tab %d", m.Tab())
	}
	// Esc đóng bảng lệnh mà không chạy gì; phím khác không rò xuống màn hình.
	press(t, m, ":", "esc")
	if strings.Contains(plain(m.Render()), "Bảng lệnh") {
		t.Fatal("Esc phải đóng bảng lệnh")
	}
	// Ctrl+K cũng mở.
	press(t, m, "ctrl+k")
	if !strings.Contains(plain(m.Render()), "Bảng lệnh") {
		t.Fatal("Ctrl+K phải mở bảng lệnh")
	}
	// Tìm theo đường lệnh CLI.
	press(t, m, "esc", ":")
	for _, r := range "bint status" {
		press(t, m, string(r))
	}
	if out := plain(m.Render()); !strings.Contains(out, "Làm mới trạng thái") {
		t.Fatalf("tìm theo lệnh CLI:\n%s", out)
	}
}

func TestHelpListsActionsAndCLI(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 40)
	press(t, m, "?")
	out := plain(m.Render())
	for _, want := range []string{"Trợ giúp", "Toàn cục", "Thoát Bint", "bint status"} {
		if !strings.Contains(out, want) {
			t.Errorf("trợ giúp thiếu %q", want)
		}
	}
	press(t, m, "esc")
	if strings.Contains(plain(m.Render()), "Toàn cục") {
		t.Fatal("Esc phải đóng trợ giúp")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
		_, cmd := m.Update(keyMsg(k))
		if cmd == nil {
			t.Fatalf("%s phải thoát", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: không phải QuitMsg", k)
		}
	}
}

// Chân phím theo ngữ cảnh: phím riêng của màn hình trước, ? và q luôn còn, không tràn.
func TestFooterContextAndFit(t *testing.T) {
	for _, w := range []int{60, 80, 140} {
		m := newModel(t, Options{Mode: theme.Mono}, sampleData(), w, 30)
		last := lines(m.Render())[29]
		if !strings.Contains(last, "[?→") && !strings.Contains(last, "[?]") || !strings.Contains(last, "[q") {
			t.Fatalf("w=%d chân phím mất ? hoặc q: %s", w, last)
		}
		if text.Width(last) != w {
			t.Fatalf("w=%d chân phím tràn: %d", w, text.Width(last))
		}
	}
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
	if last := lines(m.Render())[29]; !strings.Contains(last, "[r→ Làm mới]") || strings.Index(last, "[r→") > strings.Index(last, "[q→") {
		t.Fatalf("phím của màn hình phải đứng trước: %s", last)
	}
	press(t, m, "3")
	if last := lines(m.Render())[29]; strings.Contains(last, "Làm mới") {
		t.Fatalf("phím của Tổng quan không được hiện ở tab khác: %s", last)
	}
}

// colorRE khớp mã màu chữ/nền (không gồm đậm 1m, đảo màu 7m, gạch chân 4m).
var colorRE = regexp.MustCompile("\x1b\\[(3[0-7]|9[0-7]|4[0-7]|10[0-7]|[34]8)[;m]")

// Chế độ đơn sắc (NO_COLOR) không có mã màu; chế độ tối dùng truecolor; 16 màu dùng mã ANSI cơ bản.
func TestColorModes(t *testing.T) {
	if mono := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30).Render(); colorRE.MatchString(mono) {
		t.Fatalf("NO_COLOR vẫn có mã màu: %q", colorRE.FindString(mono))
	}
	if dark := newModel(t, Options{Mode: theme.Dark}, sampleData(), 100, 30).Render(); !strings.Contains(dark, "\x1b[38;2;") {
		t.Fatal("chế độ tối phải dùng truecolor")
	}
	ansi16 := newModel(t, Options{Mode: theme.ANSI16}, sampleData(), 100, 30).Render()
	if strings.Contains(ansi16, "\x1b[38;2;") || !colorRE.MatchString(ansi16) {
		t.Fatal("chế độ 16 màu phải dùng mã màu ANSI cơ bản, không dùng truecolor")
	}
}

// Chế độ chỉ đọc: hành động ghi bị ẩn khỏi chân phím, bảng lệnh, trợ giúp và không chạy được qua phím.
func TestReadOnlyHidesAndBlocksWrites(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, ReadOnly: true}, notInitData(), 120, 30)
	out := plain(m.Render())
	if !strings.Contains(out, "CHỈ ĐỌC") {
		t.Fatalf("thiếu nhãn CHỈ ĐỌC:\n%s", out)
	}
	if strings.Contains(lines(m.Render())[29], "Khởi tạo") || !strings.Contains(out, "chế độ chỉ đọc") {
		t.Fatalf("chế độ chỉ đọc phải ẩn nút khởi tạo và nói rõ lý do:\n%s", out)
	}
	press(t, m, ":")
	for _, r := range "khoi tao" {
		press(t, m, string(r))
	}
	if strings.Contains(plain(m.Render()), "Khởi tạo Bint (trình hướng dẫn") {
		t.Fatal("bảng lệnh không được liệt kê hành động ghi")
	}
	press(t, m, "esc", "enter", "d")
	if m.wiz.Running() || m.wiz.Report() != nil || m.wiz.Step() != 0 {
		t.Fatal("chế độ chỉ đọc không được mở trình hướng dẫn hay chạy khởi tạo")
	}
	// Chạy thẳng theo ID cũng bị chặn: sổ hành động là nơi duy nhất quyết định.
	_, cmd := m.run("init.run")
	if cmd != nil || m.wiz.Running() || m.wiz.Step() != 0 {
		t.Fatal("run(init.run) phải bị chặn")
	}
	// Yêu cầu init.run/init.plan thẳng tới mô hình gốc cũng bị từ chối (không gọi dịch vụ).
	if _, cmd := m.Update(actions.RequestMsg{ID: "init.run", Opts: bintapp.InitFlagOptions{}}); cmd != nil && m.jobs.Len() != 0 {
		t.Fatal("RequestMsg init.run phải bị từ chối ở chế độ chỉ đọc")
	}
}

// Hành động chỉ dành cho người bị khoá trong phiên agent, kèm lý do; không chạy.
func TestHumanOnlyLockedInAgentSession(t *testing.T) {
	reg := actions.NewRegistry([]actions.Action{
		{ID: "app.quit", Key: "q", Footer: true, Pinned: true, LabelKey: "action.app.quit.label", TitleKey: "action.app.quit.title"},
		{ID: "app.help", Key: "?", Footer: true, Pinned: true, LabelKey: "action.app.help.label", TitleKey: "action.app.help.title"},
		{ID: "app.palette", Key: ":", TitleKey: "action.app.palette.title"},
		{ID: "overview.refresh", Screen: actions.Overview, Key: "a", Footer: true, Priority: 1, LabelKey: "action.approve",
			TitleKey: "action.approve", HumanOnly: true, Writes: true},
	})
	m := newModel(t, Options{Mode: theme.Mono, Agent: true, Registry: reg}, sampleData(), 120, 30)
	out := plain(m.Render())
	if !strings.Contains(out, "PHIÊN AGENT") {
		t.Fatalf("thiếu nhãn phiên agent:\n%s", out)
	}
	if strings.Contains(lines(m.Render())[29], "Duyệt") {
		t.Fatal("hành động bị khoá không được hiện ở chân phím")
	}
	press(t, m, ":")
	if pal := plain(m.Render()); !strings.Contains(pal, "chỉ dành cho người") {
		t.Fatalf("bảng lệnh phải hiện lý do khoá:\n%s", pal)
	}
	press(t, m, "down", "down", "down", "enter")
	if !strings.Contains(plain(m.Render()), "chỉ dành cho người") {
		t.Fatal("chọn hành động khoá phải báo lý do")
	}
	m.toast = ""
	_, cmd := m.run("overview.refresh")
	if cmd != nil || m.toast == "" {
		t.Fatalf("run phải bị khoá kèm lý do: cmd=%v toast=%q", cmd, m.toast)
	}
	// Ngoài phiên agent thì chạy được.
	h := newModel(t, Options{Mode: theme.Mono, Registry: reg}, sampleData(), 120, 30)
	if _, cmd := h.run("overview.refresh"); cmd == nil {
		t.Fatal("người dùng phải chạy được")
	}
}

// fakeDetect là harness "có trên máy" cho trình hướng dẫn (kiểm thử không phụ thuộc máy đang chạy).
func fakeDetect() []detect.Harness {
	return []detect.Harness{{Name: "claude", Present: true}, {Name: "codex"}, {Name: "antigravity"}}
}

// Repo tạm chưa init: TUI mở trình hướng dẫn nhiều bước (không phải màn tạm "mặc định"); đi hết các bước thì app.Init nhận đúng
// Options đã chọn, báo cáo các bước và "bước tiếp theo" hiện sau khi chạy rồi Tổng quan được nạp lại.
func TestInitWizardFlowOnTemporaryRepo(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	m := New(context.Background(), Options{Dir: root, Version: "0.1.0", BintPath: "bint", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond,
		DetectHarnesses: fakeDetect})
	m.SetSize(100, 30)
	drive(t, m, m.Init())

	if m.Screen() != actions.Init {
		t.Fatalf("repo chưa init phải hiện trình hướng dẫn, nhận %q", m.Screen())
	}
	out := plain(m.Render())
	for _, want := range []string{"Khởi tạo Bint", "Bước 1/7", "claude", "có trên máy", "không thấy trên máy", "codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("bước harness thiếu %q:\n%s", want, out)
		}
	}
	// Bước 1 harness: chọn thêm codex (j xuống rồi Space); bước 2 hook: giữ Có, chọn phạm vi project; bước 3 MCP: Có.
	press(t, m, "j", " ", "enter")
	if out := plain(m.Render()); !strings.Contains(out, "Bước 2/7") || !strings.Contains(out, "Nơi cài hook") {
		t.Fatalf("bước hook:\n%s", out)
	}
	press(t, m, "j", "j", "j", " ", "enter") // con trỏ tới hàng project rồi Space chọn; hook vẫn Có
	press(t, m, "enter")                     // MCP: mặc định Không, sang AGENTS.md
	if m.wiz.Step() != 3 {
		t.Fatalf("phải ở bước AGENTS.md, nhận %d", m.wiz.Step())
	}
	press(t, m, "esc") // lùi về MCP, chọn Có
	if m.wiz.Step() != 2 {
		t.Fatalf("Esc phải lùi về MCP, nhận %d", m.wiz.Step())
	}
	press(t, m, "k", " ", "enter")
	press(t, m, "enter", "enter") // AGENTS.md: Không; skill: Có → bước Git
	press(t, m, "j", " ")         // local
	out = plain(m.Render())
	for _, want := range []string{"Bước 6/7", "track", "local", ".git/info/exclude"} {
		if !strings.Contains(out, want) {
			t.Errorf("bước Git thiếu %q:\n%s", want, out)
		}
	}
	press(t, m, "enter") // sang xem trước
	out = plain(m.Render())
	for _, want := range []string{"Bước 7/7", ".bint/config.yml", "bint init --harness", "--git local", "--mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("xem trước thiếu %q:\n%s", want, out)
		}
	}
	if _, err := bintapp.Status(context.Background(), root); err == nil {
		t.Fatal("xem trước không được khởi tạo gì")
	}
	press(t, m, "enter") // chạy
	if !m.ov.Loaded || m.ov.Data.NotInit || m.ov.Data.Status == nil {
		t.Fatalf("sau khởi tạo phải nạp lại Tổng quan: %+v", m.ov.Data)
	}
	if m.Screen() != actions.Init || !m.wiz.Done() || len(m.wiz.Report().Steps) == 0 {
		t.Fatalf("phải giữ báo cáo đến khi bấm tiếp: screen=%q done=%v", m.Screen(), m.wiz.Done())
	}
	held := plain(m.Render())
	for _, want := range []string{"Đã khởi tạo Bint", "Đã khởi tạo xong", m.wiz.Report().Steps[0].Name, "Bước tiếp theo", "bint doctor"} {
		if !strings.Contains(held, want) {
			t.Errorf("màn báo cáo thiếu %q:\n%s", want, held)
		}
	}
	// Trạng thái thật khớp lựa chọn: harness, hook ở phạm vi project, MCP đã đăng ký, chế độ Git local đã lưu.
	st, err := bintapp.ReadInitState(context.Background(), root)
	if err != nil || st.NotInit {
		t.Fatalf("trạng thái sau init: %+v %v", st, err)
	}
	if !slices.Equal(st.Enabled, []string{"claude", "codex"}) || !st.HooksOn() || st.HooksScope() != "project" || !st.MCPOn() || st.GitMode != bintapp.GitLocal {
		t.Fatalf("trạng thái không khớp lựa chọn: %+v", st)
	}
	// Đang giữ báo cáo thì không chạy lại init; Enter ở báo cáo (bước đầu tiên là doctor) sang Hệ thống › Doctor.
	if _, cmd := m.run("init.run"); cmd != nil || m.wiz.Running() {
		t.Fatal("không được khởi tạo lại khi đang giữ báo cáo")
	}
	press(t, m, "enter") // tiếp tục
	if m.Screen() == actions.Init {
		t.Fatalf("bấm tiếp phải rời trình hướng dẫn, nhận %q", m.Screen())
	}
}

// d dùng mặc định và nhảy thẳng tới xem trước; Esc lùi từ xem trước về bước Git; mặc định đúng như `bint init` tối thiểu.
func TestInitWizardDefaultsAndBack(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	m := New(context.Background(), Options{Dir: root, Version: "0.1.0", BintPath: "bint", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond,
		DetectHarnesses: fakeDetect})
	m.SetSize(100, 30)
	drive(t, m, m.Init())
	press(t, m, "d")
	out := plain(m.Render())
	if m.wiz.Step() != 6 || !strings.Contains(out, "Bước 7/7") || !strings.Contains(out, "bint init --harness claude --hooks --git track") {
		t.Fatalf("d phải tới xem trước với mặc định:\n%s", out)
	}
	o := m.wiz.Options()
	if !o.Hooks || o.NoHooks || o.HooksScope != "local" || o.GitMode != "track" || o.MCP || o.AgentsMD || o.NoSkills || len(o.Harnesses) != 1 || o.Harnesses[0] != "claude" {
		t.Fatalf("mặc định: %+v", o)
	}
	press(t, m, "esc")
	if m.wiz.Step() != 5 {
		t.Fatalf("Esc từ xem trước phải về bước Git, nhận %d", m.wiz.Step())
	}
	press(t, m, "esc", "esc", "esc", "esc", "esc")
	if m.wiz.Step() != 0 {
		t.Fatalf("Esc nhiều lần về bước đầu, nhận %d", m.wiz.Step())
	}
	// Không chọn harness nào thì không đi tiếp.
	press(t, m, " ", "enter")
	if m.wiz.Step() != 0 || !strings.Contains(plain(m.Render()), "Chọn ít nhất một harness") {
		t.Fatalf("phải đòi chọn harness:\n%s", plain(m.Render()))
	}
}

// Bảng lệnh không chứa hành động khởi tạo ngoài màn Khởi tạo, và run() từ chối khi repo đã init.
func TestInitActionsHiddenOutsideInitScreen(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 120, 30)
	for _, it := range m.paletteItems() {
		if strings.HasPrefix(it.ID, "init.") {
			t.Fatalf("bảng lệnh ở Tổng quan (đã init) không được có %s", it.ID)
		}
	}
	press(t, m, ":")
	typed := m.pal
	typed.Type("khởi tạo")
	if out := plain(m.Render()); strings.Contains(out, "cấu hình mặc định") {
		t.Fatalf("tìm 'khởi tạo' không ra hành động ghi:\n%s", out)
	}
	press(t, m, "esc")
	if _, cmd := m.run("init.run"); cmd != nil || m.wiz.Running() || m.wiz.Step() != 0 {
		t.Fatal("run(init.run) phải bị từ chối khi repo đã init")
	}
	// Ở tab khác của repo chưa init cũng không chạy được.
	n := newModel(t, Options{Mode: theme.Mono}, notInitData(), 120, 30)
	press(t, n, "2")
	if _, cmd := n.run("init.run"); cmd != nil || n.wiz.Running() || n.wiz.Step() != 0 {
		t.Fatal("init.run chỉ chạy ở màn Khởi tạo")
	}
	for _, it := range n.paletteItems() {
		if strings.HasPrefix(it.ID, "init.") {
			t.Fatalf("ngoài màn Khởi tạo không có %s trong bảng lệnh", it.ID)
		}
	}
	// Ở màn Khởi tạo thì có trong bảng lệnh.
	press(t, n, "1")
	found := false
	for _, it := range n.paletteItems() {
		found = found || it.ID == "init.run"
		if it.ID == "init.plan" {
			t.Fatal("init.plan là bước nội bộ, không có trong bảng lệnh")
		}
	}
	if !found {
		t.Fatal("màn Khởi tạo phải có init.run trong bảng lệnh")
	}
}

// startInitJob dựng trạng thái "đang khởi tạo": một việc init.run trong bảng việc nền (như khi RequestMsg init.run đã chạy).
func startInitJob(m *Model) {
	_, cancel := context.WithCancel(context.Background())
	m.jobs.Add(jobs.Job{Action: "init.run", Kind: jobs.KindAction, Label: "bint init", Cancel: cancel})
}

// q khi đang khởi tạo (việc nền ghi) mở hộp thoại thoát đầy đủ thay cho cơ chế "nhấn lần nữa": "Ở lại" không
// thoát; "Huỷ việc rồi thoát" chờ khởi tạo kết thúc (hoặc hết hạn giờ) rồi mới thoát.
func TestQuitDuringInitOpensExitDialog(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, notInitData(), 120, 30)
	startInitJob(m)
	_, cmd := m.Update(keyMsg("q"))
	if cmd != nil || m.quitting || m.exitDlg == nil {
		t.Fatalf("q khi đang khởi tạo phải mở hộp thoại, không thoát: cmd=%v quit=%v dlg=%v", cmd != nil, m.quitting, m.exitDlg != nil)
	}
	out := plain(m.Render())
	for _, want := range []string{"Còn việc đang chạy", "Khởi tạo", "Huỷ việc rồi thoát", "Ở lại"} {
		if !strings.Contains(out, want) {
			t.Errorf("hộp thoại thiếu %q:\n%s", want, out)
		}
	}
	// "Ở lại": đóng hộp, không thoát.
	if _, cmd := m.Update(keyMsg("esc")); cmd != nil || m.exitDlg != nil || m.quitting || m.exiting {
		t.Fatal("Ở lại không được thoát")
	}
	// "Huỷ việc rồi thoát": chưa thoát cho tới khi khởi tạo báo xong.
	press(t, m, "q")
	_, cmd = m.Update(keyMsg("enter"))
	if cmd == nil || m.quitting || !m.exiting {
		t.Fatalf("phải chờ việc kết thúc: cmd=%v quit=%v exiting=%v", cmd != nil, m.quitting, m.exiting)
	}
	if _, cmd = m.Update(actions.ResultMsg{ID: "init.run", Value: &bintapp.InitReport{}}); cmd == nil || !m.quitting {
		t.Fatal("khởi tạo xong khi đang thoát phải thoát")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("phải trả tea.Quit")
	}
	// Quá hạn chờ thì thoát dù việc chưa kết thúc.
	m2 := newModel(t, Options{Mode: theme.Mono}, notInitData(), 120, 30)
	startInitJob(m2)
	m2.Update(keyMsg("q"))
	m2.Update(keyMsg("enter")) // không chạy lệnh tick chờ: kiểm thử gửi exitTimeoutMsg trực tiếp
	if m2.quitting {
		t.Fatal("chưa hết hạn thì chưa thoát")
	}
	if _, cmd := m2.Update(exitTimeoutMsg{}); cmd == nil || !m2.quitting {
		t.Fatal("hết hạn phải thoát")
	}
	// Đang thoát: Ctrl+C lần nữa thoát ngay.
	m3 := newModel(t, Options{Mode: theme.Mono}, notInitData(), 120, 30)
	startInitJob(m3)
	m3.Update(keyMsg("q"))
	m3.Update(keyMsg("enter"))
	if _, cmd := m3.Update(keyMsg("ctrl+c")); cmd == nil || !m3.quitting {
		t.Fatal("Ctrl+C khi đang chờ phải thoát ngay")
	}
	// Không có việc nền thì thoát ngay.
	m4 := newModel(t, Options{Mode: theme.Mono}, sampleData(), 120, 30)
	if _, cmd := m4.Update(keyMsg("q")); cmd == nil || !m4.quitting {
		t.Fatal("không có việc nền thì q thoát ngay")
	}
}

// Nhấn r liên tiếp không khởi thêm lần nạp song song; yêu cầu thêm được nạp lại đúng một lần sau đó.
func TestReloadDoesNotRunConcurrently(t *testing.T) {
	var running, peak, calls int
	o := Options{Mode: theme.Mono, Dir: "/x", Version: "0", Lang: "vi"}
	o.Loader = func(context.Context, string) (overview.Data, error) {
		calls++
		running++
		peak = max(peak, running)
		running--
		return sampleData(), nil
	}
	m := New(context.Background(), o)
	m.SetSize(100, 30)
	first := m.Init()
	if c := m.load(); c != nil {
		t.Fatal("đang nạp thì không khởi lần nạp mới")
	}
	if c := m.load(); c != nil {
		t.Fatal("đang nạp thì không khởi lần nạp mới")
	}
	drive(t, m, first)
	if calls != 2 || peak != 1 {
		t.Fatalf("muốn đúng 2 lần nạp (1 + 1 nạp lại), nạp %d, đồng thời tối đa %d", calls, peak)
	}
	if m.ov.Loading || m.reloadPending {
		t.Fatal("phải nạp xong")
	}
}

func TestOverviewErrorShowsHintAndRetry(t *testing.T) {
	calls := 0
	o := Options{Mode: theme.Mono, Dir: "/x", Version: "0", Lang: "vi"}
	o.Loader = func(context.Context, string) (overview.Data, error) {
		calls++
		if calls == 1 {
			return overview.Data{}, context.DeadlineExceeded
		}
		return sampleData(), nil
	}
	m := New(context.Background(), o)
	m.SetSize(100, 30)
	drive(t, m, m.Init())
	if out := plain(m.Render()); !strings.Contains(out, "Không đọc được") || !strings.Contains(out, "Nhấn r") {
		t.Fatalf("lỗi phải kèm gợi ý:\n%s", out)
	}
	press(t, m, "r")
	if out := plain(m.Render()); !strings.Contains(out, "Việc cần bạn") {
		t.Fatalf("r phải nạp lại:\n%s", out)
	}
}

func TestWorktreeWarningInHeader(t *testing.T) {
	d := sampleData()
	d.Status.LinkedWorktree, d.Status.MainWorktree = true, "/work/main"
	m := newModel(t, Options{Mode: theme.Mono}, d, 120, 30)
	if third := lines(m.Render())[2]; !strings.Contains(third, "worktree phụ") || !strings.Contains(third, "/work/main") {
		t.Fatalf("cảnh báo worktree phụ: %s", third)
	}
}

// Trong lúc nạp dữ liệu hiện vòng quay (Bubbles spinner) và chuỗi tick dừng khi hết việc.
func TestLoadingShowsSpinnerAndTickStops(t *testing.T) {
	o := Options{Mode: theme.Mono, Dir: "/x", Version: "0", Lang: "vi", SpinnerFPS: time.Millisecond,
		Loader: func(context.Context, string) (overview.Data, error) { return sampleData(), nil }}
	m := New(context.Background(), o)
	m.SetSize(100, 30)
	cmd := m.Init()
	if cmd == nil || !m.busy() {
		t.Fatal("Init phải bắt đầu nạp")
	}
	out := plain(m.Render())
	if !strings.Contains(out, "Đang tải") || !strings.ContainsAny(out, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("thiếu vòng quay khi đang tải:\n%s", out)
	}
	drive(t, m, cmd)
	if m.busy() || m.ticking {
		t.Fatalf("hết việc phải dừng tick: busy=%v ticking=%v", m.busy(), m.ticking)
	}
	if out := plain(m.Render()); strings.Contains(out, "Đang tải") || !strings.Contains(out, "Việc cần bạn") {
		t.Fatalf("sau khi nạp xong:\n%s", out)
	}
	// Tick lẻ sau khi hết việc bị bỏ qua, không sinh chuỗi mới.
	if _, c := m.Update(m.spin.Tick()); c != nil {
		t.Fatal("tick thừa không được kéo dài chuỗi")
	}
}

// Khởi tạo thất bại: tiêu đề và đoạn mở đầu nói lỗi, không còn câu "chưa khởi tạo" như trước khi thử.
func TestInitErrorScreenSaysFailure(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono}, notInitData(), 100, 30)
	m.Update(actions.ResultMsg{ID: "init.run", Err: errors.New("ghi cấu hình: từ chối quyền")})
	out := plain(m.Render())
	for _, want := range []string{"Khởi tạo Bint không thành công", "Không khởi tạo được Bint trong", "Khởi tạo thất bại.", "từ chối quyền"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "chưa khởi tạo") {
		t.Errorf("không được nói 'chưa khởi tạo' khi đã thử và lỗi:\n%s", out)
	}
}

// Phát hiện .13: nạp lại sau khi khởi tạo bị lỗi thì lỗi phải hiện ra, và "Tiếp tục" không được đưa về
// màn chưa khởi tạo để chạy lại init.run trên repository đã init.
func TestReloadFailureAfterInitKeepsRepoInitialized(t *testing.T) {
	testutil.Isolate(t)
	root := testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n", "a.go": "package a\n"})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	calls := 0
	m := New(context.Background(), Options{Dir: root, Version: "0.1.0", BintPath: "bint", Lang: "vi", Mode: theme.Mono,
		SpinnerFPS: time.Millisecond,
		Loader: func(context.Context, string) (overview.Data, error) {
			calls++
			if calls == 1 {
				return notInitData(), nil
			}
			return overview.Data{}, errors.New("sqlite: database is locked")
		}})
	m.SetSize(100, 30)
	drive(t, m, m.Init())
	if m.Screen() != actions.Init {
		t.Fatalf("lần nạp đầu báo chưa init: %q", m.Screen())
	}
	press(t, m, "d", "enter")
	if calls != 2 || !m.wiz.Done() {
		t.Fatalf("calls=%d done=%v", calls, m.wiz.Done())
	}
	held := plain(m.Render())
	if !strings.Contains(held, "database is locked") || !strings.Contains(held, "Đã khởi tạo xong") {
		t.Fatalf("màn báo cáo phải nói rõ lỗi nạp lại:\n%s", held)
	}
	press(t, m, "enter") // Tiếp tục
	if m.Screen() == actions.Init {
		t.Fatal("sau khi khởi tạo thành công không được quay về màn chưa khởi tạo")
	}
	if out := plain(m.Render()); !strings.Contains(out, "database is locked") || !strings.Contains(out, "Không đọc được trạng thái") {
		t.Fatalf("lỗi nạp phải hiện ra ở Tổng quan:\n%s", out)
	}
	if _, cmd := m.run("init.run"); cmd != nil || m.wiz.Running() {
		t.Fatal("không được chạy lại init.run trên repository đã init")
	}
	for _, it := range m.paletteItems() {
		if strings.HasPrefix(it.ID, "init.") {
			t.Fatalf("bảng lệnh không được có %s sau khi đã init", it.ID)
		}
	}
	// Nạp lại thành công thì Tổng quan hiện bình thường.
	calls = 100
	m.opts.Loader = func(context.Context, string) (overview.Data, error) { return sampleData(), nil }
	press(t, m, "r")
	if m.ov.Err != nil || !strings.Contains(plain(m.Render()), "Việc cần bạn") {
		t.Fatalf("r phải nạp lại được:\n%s", plain(m.Render()))
	}
}

// Phát hiện .14: Ctrl+C đóng bảng trợ giúp như bảng lệnh (không bị nuốt, không thoát).
func TestCtrlCClosesHelpAndPalette(t *testing.T) {
	for _, open := range []string{"?", ":"} {
		m := newModel(t, Options{Mode: theme.Mono}, sampleData(), 100, 30)
		press(t, m, open)
		if !m.showHelp && m.pal == nil {
			t.Fatalf("%s phải mở hộp phủ", open)
		}
		_, cmd := m.Update(keyMsg("ctrl+c"))
		if m.showHelp || m.pal != nil || cmd != nil || m.quitting {
			t.Fatalf("%s: ctrl+c phải đóng hộp phủ và không thoát (help=%v pal=%v quit=%v)", open, m.showHelp, m.pal != nil, m.quitting)
		}
	}
}

// Hộp phủ (trợ giúp, bảng lệnh) che hẳn nền trong khung nội dung: kết quả không phụ thuộc dữ liệu của
// màn bên dưới, nên không còn chữ nền bị cắt ngang lộ ra hai bên hộp.
func TestOverlayHidesBackground(t *testing.T) {
	other := sampleData()
	other.Candidates = 99
	other.Status.Repo, other.Status.StateDir = "/khac/hoan/toan", "/khac/trang-thai"
	other.Doctor = nil
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		for _, open := range []string{"?", ":"} {
			a := newModel(t, Options{Mode: theme.Mono}, sampleData(), sz[0], sz[1])
			b := newModel(t, Options{Mode: theme.Mono}, other, sz[0], sz[1])
			press(t, a, open)
			press(t, b, open)
			la, lb := lines(a.Render()), lines(b.Render())
			// Bỏ thanh trên (3 dòng): huy hiệu tab và đường dẫn là của chính thanh trên.
			if strings.Join(la[3:], "\n") != strings.Join(lb[3:], "\n") {
				t.Fatalf("%v %s: nền của Tổng quan còn lộ qua hộp phủ:\n%s", sz, open, strings.Join(la, "\n"))
			}
		}
	}
}

// Cảnh báo doctor chỉ hiện một lần (ở hộp thư); mục Doctor chỉ có số đếm.
func TestDoctorWarningNotDuplicated(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {140, 40}} {
		out := plain(newModel(t, Options{Mode: theme.Mono}, sampleData(), sz[0], sz[1]).Render())
		if n := strings.Count(out, "cấu hình theo mẫu cũ"); n != 1 {
			t.Errorf("%v: cảnh báo doctor xuất hiện %d lần, muốn 1:\n%s", sz, n, out)
		}
		if !strings.Contains(out, "✓ 2  ! 1  ✗ 0") {
			t.Errorf("%v: mục Doctor phải còn số đếm:\n%s", sz, out)
		}
	}
}

// Cửa sổ cao: nội dung được căn giữa theo chiều dọc thay vì dồn lên trên, chừa chỗ cân đối hai đầu.
func TestOverviewCentersVerticallyInTallWindow(t *testing.T) {
	ls := lines(newModel(t, Options{Mode: theme.Mono}, sampleData(), 140, 40).Render())
	body := ls[4 : len(ls)-1] // trong khung nội dung (bỏ viền trên/dưới)
	blank := func(l string) bool { return strings.Trim(l, "│ ") == "" }
	top, bottom := 0, 0
	for i := 0; i < len(body) && blank(body[i]); i++ {
		top++
	}
	for i := len(body) - 1; i >= 0 && blank(body[i]); i-- {
		bottom++
	}
	if top < 3 || bottom < 3 || top-bottom > 2 || bottom-top > 2 {
		t.Fatalf("khoảng trống trên=%d dưới=%d không cân đối:\n%s", top, bottom, strings.Join(ls, "\n"))
	}
	// Cửa sổ vừa đủ thì vẫn bắt đầu sát trên (không đệm thừa).
	small := lines(newModel(t, Options{Mode: theme.Mono}, sampleData(), 80, 24).Render())
	if strings.Trim(small[4], "│ ") == "" && strings.Trim(small[5], "│ ") == "" {
		t.Fatalf("cửa sổ nhỏ không được đệm đầu:\n%s", strings.Join(small, "\n"))
	}
}

// Trạng thái lượt chạy được dịch (vi/en), không còn key=value tiếng Anh.
func TestRunStatusesTranslated(t *testing.T) {
	vi := plain(newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 100, 30).Render())
	if !strings.Contains(vi, "33 xong · 3 lỗi · 1 bị ngắt · 5 đang chạy") || strings.Contains(vi, "completed=") {
		t.Errorf("vi:\n%s", vi)
	}
	en := plain(newModel(t, Options{Mode: theme.Mono, Lang: "en"}, sampleData(), 100, 30).Render())
	if !strings.Contains(en, "33 done · 3 failed · 1 interrupted · 5 running") {
		t.Errorf("en:\n%s", en)
	}
	// Trạng thái lạ hiện nguyên tên; trạng thái 0 bị bỏ.
	d := sampleData()
	d.Status.Runs = map[string]int{"completed": 2, "failed": 0, "paused": 4}
	if out := plain(newModel(t, Options{Mode: theme.Mono}, d, 100, 30).Render()); !strings.Contains(out, "2 xong · 4 paused") {
		t.Errorf("trạng thái lạ:\n%s", out)
	}
}

// Đường dẫn hiển thị đi qua cùng hàm rút gọn: home thành ~, đường dẫn quá dài bỏ phần đầu bằng …
func TestPathsShortenedConsistently(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d := sampleData()
	d.Status.Repo = home + "/code/demo"
	d.Status.StateDir = home + "/.local/state/bint/demo-7f3a9c"
	m := newModel(t, Options{Mode: theme.Mono, Dir: home + "/code/demo"}, d, 100, 30)
	out := plain(m.Render())
	if strings.Contains(out, home) {
		t.Fatalf("còn đường dẫn home chưa rút gọn:\n%s", out)
	}
	for _, want := range []string{"~/code/demo", "~/.local/state/bint/demo-7f3a9c"} {
		if !strings.Contains(out, want) {
			t.Errorf("thiếu %q:\n%s", want, out)
		}
	}
	// Đường dẫn rất dài: bỏ phần đầu, không tràn khung.
	long := "/srv/" + strings.Repeat("rat-dai/", 20) + "demo"
	d.Status.Repo = long
	n := newModel(t, Options{Mode: theme.Mono, Dir: long}, d, 80, 24)
	for i, l := range lines(n.Render()) {
		if w := text.Width(l); w != 80 {
			t.Fatalf("dòng %d rộng %d: %q", i, w, l)
		}
	}
	if out := plain(n.Render()); !strings.Contains(out, "…") || !strings.Contains(out, "demo") || strings.Contains(out, "/srv/rat-dai") {
		t.Fatalf("đường dẫn dài phải rút phần đầu bằng …:\n%s", out)
	}
}

// Gạch đầu dòng ở màn Khởi tạo: dòng gói tiếp thụt treo theo chữ sau "• "; lệnh trong dấu huyền
// không bị ngắt (kể cả tại dấu gạch nối của cờ) ở mọi độ rộng và ngôn ngữ.
func TestInitBulletsHangAndKeepCodeWhole(t *testing.T) {
	for w := 60; w <= 100; w += 4 {
		for _, lang := range []string{"vi", "en"} {
			ls := lines(newModel(t, Options{Mode: theme.Mono, Lang: lang}, notInitData(), w, 30).Render())
			inBullet := false
			for _, l := range ls {
				body := strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│")
				switch {
				case strings.HasPrefix(body, "   • "):
					inBullet = true
				case strings.HasPrefix(body, "     ") && inBullet && strings.TrimSpace(body) != "":
					// dòng gói tiếp: thụt đúng 5 ô (2 đệm + "• " + 1) trước chữ
					if strings.HasPrefix(body, "      ") {
						t.Errorf("w=%d %s: thụt treo sai: %q", w, lang, body)
					}
				default:
					inBullet = false
				}
				if strings.Count(l, "`")%2 != 0 {
					t.Errorf("w=%d %s: đoạn mã trong dấu huyền bị ngắt: %q", w, lang, l)
				}
			}
		}
	}
}

// initFlowModel dựng mô hình ở màn Khởi tạo (repo chưa init) với loader có thể đổi kết quả giữa chừng.
func initFlowModel(t *testing.T, load *func() (overview.Data, error)) *Model {
	t.Helper()
	m := New(context.Background(), Options{Dir: "/work/demo", Version: "0.1.0", Lang: "vi", Mode: theme.Mono, SpinnerFPS: time.Millisecond,
		Loader: func(context.Context, string) (overview.Data, error) { return (*load)() }})
	m.SetSize(100, 30)
	drive(t, m, m.Init())
	if m.Screen() != actions.Init {
		t.Fatalf("phải ở màn Khởi tạo, nhận %q", m.Screen())
	}
	return m
}

// initDone chỉ che NotInit khi lần nạp sau init LỖI; lần nạp THÀNH CÔNG vẫn trả NotInit thì hiện lại
// màn Khởi tạo (không rơi vào Tổng quan trống).
func TestInitDoneMasksNotInitOnlyOnLoadError(t *testing.T) {
	load := func() (overview.Data, error) { return notInitData(), nil }
	m := initFlowModel(t, &load)
	_, cmd := m.Update(actions.ResultMsg{ID: "init.run", Value: &bintapp.InitReport{}})
	drive(t, m, cmd)
	press(t, m, "enter") // init.continue
	if m.holdInit {
		t.Fatal("phải thoát chế độ giữ báo cáo sau khi bấm tiếp")
	}
	if m.Screen() != actions.Init || !m.needsInit() {
		t.Fatalf("nạp thành công vẫn NotInit phải hiện lại màn Khởi tạo, nhận %q", m.Screen())
	}

	if m.initDone {
		t.Fatal("lần nạp thành công trả NotInit phải xoá initDone")
	}
	// Sau đó một lần nạp lỗi không được che NotInit: kết quả đáng tin gần nhất là chưa init.
	load = func() (overview.Data, error) { return overview.Data{}, errors.New("đĩa lỗi") }
	press(t, m, "r")
	if m.ov.Err == nil || !m.needsInit() || m.Screen() != actions.Init {
		t.Fatalf("nạp lỗi sau khi đã biết chưa init phải vẫn ở màn Khởi tạo: err=%v screen=%q", m.ov.Err, m.Screen())
	}

	// Lần nạp lỗi: dữ liệu cũ còn NotInit nhưng đã init xong trong phiên → không coi là chưa init.
	load = func() (overview.Data, error) { return notInitData(), nil }
	m = initFlowModel(t, &load)
	load = func() (overview.Data, error) { return overview.Data{}, errors.New("đĩa lỗi") }
	_, cmd = m.Update(actions.ResultMsg{ID: "init.run", Value: &bintapp.InitReport{}})
	drive(t, m, cmd)
	if m.ov.Err == nil || !m.ov.Data.NotInit {
		t.Fatalf("điều kiện thử: cần Err và dữ liệu cũ NotInit: err=%v data=%+v", m.ov.Err, m.ov.Data)
	}
	press(t, m, "enter")
	if m.needsInit() || m.Screen() == actions.Init {
		t.Fatalf("nạp lỗi sau init không được đưa về màn Khởi tạo, nhận %q", m.Screen())
	}
}

// LoadErr trên báo cáo init không hiện lỗi cũ trong lúc lần nạp sau init chưa xong.
func TestInitReportIgnoresStaleLoadErrWhileLoading(t *testing.T) {
	fail := false
	load := func() (overview.Data, error) {
		if fail {
			return overview.Data{}, errors.New("lỗi cũ XYZ")
		}
		return notInitData(), nil
	}
	m := initFlowModel(t, &load)
	fail = true
	press(t, m, "r") // Kiểm tra lại: nạp lỗi, ov.Err đặt, Data giữ NotInit
	if m.ov.Err == nil {
		t.Fatal("điều kiện thử: cần ov.Err cũ")
	}
	// Khởi tạo xong: bắt đầu nạp lại nhưng chưa nhận kết quả.
	_, cmd := m.Update(actions.ResultMsg{ID: "init.run", Value: &bintapp.InitReport{}})
	if !m.ov.Loading || cmd == nil {
		t.Fatalf("điều kiện thử: phải đang nạp lại (loading=%v)", m.ov.Loading)
	}
	if out := plain(m.Render()); strings.Contains(out, "lỗi cũ XYZ") {
		t.Errorf("đang nạp lại mà còn hiện lỗi cũ:\n%s", out)
	}
	// Lần nạp mới lỗi thật thì phải báo.
	drive(t, m, cmd)
	if out := plain(m.Render()); !strings.Contains(out, "lỗi cũ XYZ") {
		t.Errorf("nạp xong mà lỗi thì phải báo lỗi:\n%s", out)
	}
}
