package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/config"
	"bint/internal/harness"
	"bint/internal/hcomp"
	"bint/internal/i18n"
	"bint/internal/tui/actions"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/theme"
)

// sysCapture dựng sổ hành động ghi lại các hành động của màn Hệ thống (không gọi dịch vụ thật).
func sysCapture() (*actions.Registry, *capt) {
	c := &capt{calls: map[string][]any{}, values: map[string]any{}, release: map[string]chan struct{}{}, started: map[string]chan struct{}{}}
	var list []actions.Action
	for _, a := range actions.Default().All() {
		a := a
		if a.Screen == actions.System && a.Do != nil {
			a.Do = func(_ context.Context, _ actions.Env, o any) tea.Msg {
				c.record(a.ID, o)
				return actions.ResultMsg{ID: a.ID, Opts: o, Value: c.values[a.ID]}
			}
		}
		list = append(list, a)
	}
	return actions.NewRegistry(list), c
}

func newSysModel(t *testing.T, o Options, w, h int) (*Model, *capt) {
	t.Helper()
	reg, c := sysCapture()
	o.Registry, o.Mode = reg, theme.Mono
	m := newModel(t, o, sampleData(), w, h)
	press(t, m, "7")
	return m, c
}

func TestSystemSubTabsSwitchWithCommaAndPeriod(t *testing.T) {
	m, _ := newSysModel(t, Options{}, 140, 40)
	names := []string{"Cấu hình", "Harness", "Thành phần", "Skill", "Chỉ mục", "MCP", "Ngôn ngữ", "Doctor"}
	for i := range names {
		if m.sy.Tab() != i {
			t.Fatalf("thẻ %d, muốn %d", m.sy.Tab(), i)
		}
		if got := plain(m.Render()); !strings.Contains(got, names[i]) {
			t.Fatalf("thẻ %d thiếu %q:\n%s", i, names[i], got)
		}
		press(t, m, ".")
	}
	if m.sy.Tab() != 0 {
		t.Fatalf("`.` ở thẻ cuối phải vòng về đầu: %d", m.sy.Tab())
	}
	press(t, m, ",")
	if m.sy.Tab() != 7 {
		t.Fatalf("`,` ở thẻ đầu phải về thẻ cuối: %d", m.sy.Tab())
	}
}

func TestSystemConfigShowsGroupsAndSources(t *testing.T) {
	m, _ := newSysModel(t, Options{}, 140, 40)
	out := plain(m.Render())
	hasAll(t, out, "Nhóm khoá", "Lớp nguồn", "project", "harnesses", "limits", "security", "ui", "toàn cục", "dự án", "strategy", "cục bộ",
		"/work/demo/.bint/config.yml", "sửa được bằng e")
	press(t, m, "j", "j", "j") // harnesses
	hasAll(t, plain(m.Render()), "harnesses", "name: demo")
	// Cấu hình ở worktree phụ: nêu rõ phải sửa ở worktree chính, `e` không mở form.
	press(t, m, "1")
}

// Bảng harness, bảng khả năng, chi tiết và trạng thái hook/MCP khớp harness.Describe và app.HarnessStatus.
func TestSystemHarnessTablesMatchDescribe(t *testing.T) {
	m, _ := newSysModel(t, Options{}, 140, 40)
	press(t, m, ".")
	out := plain(m.Render())
	hasAll(t, out, "HARNESS", "BẬT", "ĐỐI CHIẾU", "CẤP", "claude", "codex", "antigravity", "Bảng khả năng", "✓ hỗ trợ")
	infos := sampleSystemInfos()
	icon := map[harness.Support]string{harness.Yes: "✓", harness.Partial: "~", harness.No: "✗"}
	vi := i18n.MustLoad("vi")
	for _, ck := range harness.AllCapabilities {
		var want []string
		for _, in := range infos {
			for _, c := range in.Capabilities {
				if c.Key == ck.Key {
					want = append(want, icon[c.Support])
				}
			}
		}
		title := vi.T("sy.cap." + ck.Key)
		var row string
		for _, l := range strings.Split(out, "\n") {
			left, _, _ := strings.Cut(l, "││") // khung trái (bảng khả năng); khung phải là chi tiết
			if strings.Contains(left, title) {
				row = left
				break
			}
		}
		if row == "" {
			t.Fatalf("không có hàng khả năng %q:\n%s", title, out)
		}
		// Các ký hiệu của ba harness xuất hiện đúng thứ tự trên hàng (cột trái của bảng).
		pos := strings.Index(row, title) + len(title)
		for i, w := range want {
			j := strings.Index(row[pos:], w)
			if j < 0 {
				t.Fatalf("hàng %q: thiếu %s của harness %d: %q", title, w, i, row)
			}
			pos += j + len(w)
		}
	}
	// Chi tiết claude (con trỏ ở dòng đầu): hook phạm vi project, MCP đã đăng ký, cấp, bảng khả năng chi tiết.
	hasAll(t, out, "Chi tiết · claude", "Hook — phạm vi đã cài: project", "phạm vi project", "MCP server của Bint đã đăng ký")
	press(t, m, "j") // codex: không nằm trong trạng thái hook/MCP (chưa bật) → nói rõ
	hasAll(t, plain(m.Render()), "Chi tiết · codex", "chưa bật trong cấu hình")
}

func sampleSystemInfos() []harness.Info {
	var infos []harness.Info
	for _, a := range harness.All() {
		infos = append(infos, harness.Describe(a, config.Default(), nil))
	}
	return infos
}

func TestSystemHarnessVersionKeyRequestsVersion(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	c.values["system.harness_version"] = actions.VersionResult{Name: "claude", Version: "2.1.0"}
	press(t, m, ".", "V")
	if o := c.last(t, "system.harness_version").(actions.HarnessVersionOptions); o.Name != "claude" {
		t.Fatalf("%+v", o)
	}
	hasAll(t, plain(m.Render()), "2.1.0")
}

func TestSystemComponentsListFilterAndDetail(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	press(t, m, ".", ".")
	hasAll(t, plain(m.Render()), "Thành phần (3/3)", "test-before-stop", "api-style", "fmt-check", "1 tệp bị bỏ qua", "guidance/hong.md: thiếu tiêu đề")
	// Lọc bằng chữ.
	press(t, m, "/")
	typeText(t, m, "api")
	press(t, m, "enter")
	out := plain(m.Render())
	hasAll(t, out, "Thành phần (1/3)", "api-style")
	if strings.Contains(out, "fmt-check ") && strings.Contains(out, "▸ ✓ guardrail") {
		t.Fatalf("lọc chữ phải bỏ các mục khác:\n%s", out)
	}
	press(t, m, "esc")
	hasAll(t, plain(m.Render()), "Thành phần (3/3)")
	// Lọc theo trạng thái bằng form: bộ lọc là trạng thái của màn hình, không gọi hành động nào.
	press(t, m, "f")
	hasAll(t, plain(m.Render()), "Trạng thái", "bint harness component list")
	press(t, m, "tab", " ", "ctrl+s") // ô thứ hai (trạng thái): chọn candidate
	out = plain(m.Render())
	hasAll(t, out, "Thành phần (2/3)", "api-style", "fmt-check", "candidate")
	if strings.Contains(out, "test-before-stop  ") && strings.Contains(out, "verified") {
		t.Fatalf("bộ lọc candidate phải bỏ thành phần verified:\n%s", out)
	}
	if c.count("system.component_list") != 0 {
		t.Fatal("lọc không được gọi hành động dịch vụ")
	}
}

func TestSystemComponentDetailShowsContentAndScriptWarning(t *testing.T) {
	m, _ := newSysModel(t, Options{}, 140, 40)
	press(t, m, ".", ".", "j", "j")
	hasAll(t, plain(m.Render()), "Chi tiết · fmt-check", "script", "Rủi ro", "high", "Nội dung", "description: Kiểm tra định dạng",
		"scripts/fmt-check.sh", "Bint không bao giờ tự chạy", "gofmt -l .")
}

func TestSystemComponentAddFormSendsOptionsAndCLI(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	press(t, m, ".", ".", "n")
	hasAll(t, plain(m.Render()), "Loại", "Tệp thành phần", "Mẫu dựng sẵn", "bint harness component add")
	press(t, m, "tab", "tab") // tới ô mẫu (loại → tệp → mẫu)
	typeText(t, m, "test-before-stop")
	if out := plain(m.Render()); !strings.Contains(out, "bint harness component add --template test-before-stop") {
		t.Fatalf("dòng CLI:\n%s", out)
	}
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.component_add").(bintapp.AddComponentOptions); o.Template != "test-before-stop" || o.Kind != "" || o.File != "" {
		t.Fatalf("%+v", o)
	}
	// Thiếu cả mẫu lẫn tệp: Validate() chặn ngay trong form, không gửi.
	n := c.count("system.component_add")
	press(t, m, "n", "ctrl+s")
	if c.count("system.component_add") != n || !strings.Contains(plain(m.Render()), "cần --template hoặc --file") {
		t.Fatalf("Validate phải chặn:\n%s", plain(m.Render()))
	}
}

func TestSystemApproveAndDeprecateConfirmThenRequest(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	// guidance candidate (dòng 2): hộp thường.
	press(t, m, ".", ".", "j", "a")
	hasAll(t, plain(m.Render()), "Duyệt api-style?", "[Enter] Duyệt")
	press(t, m, "esc")
	if c.count("system.component_approve") != 0 {
		t.Fatal("Esc không được duyệt")
	}
	press(t, m, "a", "enter")
	if o := c.last(t, "system.component_approve").(bintapp.ComponentReviewOptions); o.ID != "api-style" || o.Kind != hcomp.KindGuidance {
		t.Fatalf("%+v", o)
	}
	// Script candidate: hộp đỏ có cảnh báo "Bint không bao giờ tự chạy".
	press(t, m, "j", "a")
	hasAll(t, plain(m.Render()), "Duyệt fmt-check?", "Bint không bao giờ tự chạy")
	if m.sy == nil || !strings.Contains(plain(m.Render()), "rủi ro high") {
		t.Fatal("hộp phải nêu rủi ro")
	}
	press(t, m, "enter")
	if o := c.last(t, "system.component_approve").(bintapp.ComponentReviewOptions); o.ID != "fmt-check" || o.Kind != hcomp.KindScript {
		t.Fatalf("%+v", o)
	}
	// Loại bỏ thành phần verified.
	press(t, m, "k", "k", "d")
	hasAll(t, plain(m.Render()), "Loại bỏ test-before-stop?")
	press(t, m, "enter")
	if o := c.last(t, "system.component_deprecate").(bintapp.ComponentReviewOptions); o.ID != "test-before-stop" {
		t.Fatalf("%+v", o)
	}
	// Duyệt thành phần không phải candidate bị chặn trước khi gửi.
	n := c.count("system.component_approve")
	press(t, m, "a")
	if c.count("system.component_approve") != n || !strings.Contains(plain(m.Render()), "Chỉ duyệt được thành phần candidate") {
		t.Fatalf("phải báo chỉ duyệt candidate:\n%s", plain(m.Render()))
	}
}

// Duyệt/loại bỏ là HumanOnly: trong phiên agent bị khoá (kể cả CLAUDECODE=1 qua Options.Agent) và không gửi yêu cầu.
func TestSystemReviewLockedInAgentSession(t *testing.T) {
	m, c := newSysModel(t, Options{Agent: true}, 140, 40)
	press(t, m, ".", ".", "j")
	for _, k := range []string{"a", "d"} {
		m.Update(keyMsg("esc"))
		press(t, m, k)
		if m.sy.Capturing() {
			t.Errorf("phím %s mở hộp dù phiên agent", k)
			press(t, m, "esc")
		}
		hasAll(t, plain(m.Render()), "chỉ dành cho người")
	}
	for _, id := range []string{"system.component_approve", "system.component_deprecate"} {
		_, cmd := m.Update(actions.RequestMsg{ID: id, Opts: bintapp.ComponentReviewOptions{ID: "api-style"}})
		drive(t, m, cmd)
		if c.count(id) != 0 {
			t.Errorf("%s đã tới Do trong phiên agent", id)
		}
	}
	// Hành động đọc và thêm (candidate) vẫn dùng được.
	press(t, m, "n")
	if !m.sy.Capturing() {
		t.Fatal("thêm thành phần (candidate) phải dùng được trong phiên agent")
	}
}

func TestSystemRenderFormShowsOutput(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	c.values["system.render"] = "Harness: claude · chế độ: on · 1 thành phần có hiệu lực\n\n── Guardrail ──\n- test-before-stop [global]\n"
	press(t, m, ".", ".", "R")
	hasAll(t, plain(m.Render()), "Harness", "Chế độ", "Tệp giả định", "bint harness render")
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.render").(bintapp.RenderOptions); o.Harness != "claude" || o.Mode != hcomp.ModeOn {
		t.Fatalf("mặc định từ Defaults: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Agent sẽ nhận gì (render)", "── Guardrail ──", "test-before-stop [global]")
	press(t, m, "esc")
	hasAll(t, plain(m.Render()), "Chi tiết · test-before-stop")
}

// Enter trên dòng thành phần candidate của hộp thư mở thẳng thành phần đó ở màn Hệ thống.
func TestInboxEnterOnComponentOpensThatComponent(t *testing.T) {
	d := sampleData()
	d.Pending = &bintapp.PendingWorkReport{Components: []bintapp.PendingComponent{{Kind: "guidance", ID: "api-style"}, {Kind: "script", ID: "fmt-check"}}}
	for i, want := range []string{"api-style", "fmt-check"} {
		m := newModel(t, Options{Mode: theme.Mono}, d, 140, 40)
		for j := 0; j < i; j++ {
			press(t, m, "j")
		}
		press(t, m, "enter")
		if m.Tab() != 6 || m.sy.Tab() != syscreen.TabComponents {
			t.Fatalf("tab = %d, thẻ = %d", m.Tab(), m.sy.Tab())
		}
		if c := m.sy.CursorComponent(); c == nil || c.Component.ID != want {
			t.Fatalf("con trỏ phải ở thành phần %s của dòng hộp thư: %+v", want, c)
		}
		hasAll(t, plain(m.Render()), "Chi tiết · "+want)
	}
}
