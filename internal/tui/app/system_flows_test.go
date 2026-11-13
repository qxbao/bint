package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	bintapp "bint/internal/app"
	"bint/internal/app/evolvesvc"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/fsafe"
	"bint/internal/skills"
	"bint/internal/testutil"
	"bint/internal/tui/actions"
	syscreen "bint/internal/tui/screens/system"
	"bint/internal/tui/theme"
)

func TestSystemSkillsListAndActions(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	c.values["system.skill_show"] = bintapp.SkillContent{Name: "b-do", Kind: "core", Content: []byte("# b-do\nnội dung skill\n")}
	press(t, m, ".", ".", ".")
	hasAll(t, plain(m.Render()), "Skill (3)", "b-do", "b-feat", "api-guide", "gốc", "t.hoá", "đã xuất bản", "khác bản Bint", "chưa xuất bản",
		"Lớp phủ .bint/skills/overlays/cu.md không ứng với skill gốc nào")
	// Enter: nội dung đã ghép; C: bản gốc (--core).
	press(t, m, "enter")
	if o := c.last(t, "system.skill_show").(bintapp.SkillShowOptions); o.Name != "b-do" || o.Core {
		t.Fatalf("%+v", o)
	}
	hasAll(t, plain(m.Render()), "Nội dung b-do", "nội dung skill")
	press(t, m, "esc", "C")
	if o := c.last(t, "system.skill_show").(bintapp.SkillShowOptions); !o.Core {
		t.Fatalf("C phải đòi bản gốc: %+v", o)
	}
	hasAll(t, plain(m.Render()), "bản gốc (--core)")
	// Lớp phủ: chỉ skill gốc; skill tiến hoá báo lý do và không gửi.
	press(t, m, "esc", "o")
	if o := c.last(t, "system.skill_overlay").(bintapp.SkillOverlayOptions); o.Name != "b-do" {
		t.Fatalf("%+v", o)
	}
	n := c.count("system.skill_overlay")
	press(t, m, "esc", "j", "j", "o")
	if c.count("system.skill_overlay") != n || !strings.Contains(plain(m.Render()), "Lớp phủ chỉ có cho skill gốc") {
		t.Fatalf("lớp phủ cho skill tiến hoá phải bị chặn:\n%s", plain(m.Render()))
	}
	// Versions: chỉ skill tiến hoá.
	press(t, m, "v")
	if o := c.last(t, "system.skill_versions").(bintapp.SkillVersionsOptions); o.Name != "api-guide" {
		t.Fatalf("%+v", o)
	}
	press(t, m, "esc", "k", "k", "v")
	hasAll(t, plain(m.Render()), "Chỉ skill tiến hoá có lịch sử phiên bản")
	// Diff, check, regress mở form điền sẵn tên skill tiến hoá.
	press(t, m, "j", "j", "d")
	hasAll(t, plain(m.Render()), "Skill tiến hoá", "Từ phiên bản", "bint skill diff", "api-guide")
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.skill_diff").(bintapp.SkillDiffOptions); o.Name != "api-guide" {
		t.Fatalf("%+v", o)
	}
	c.values["system.skill_diff"] = bintapp.SkillDiffResult{Name: "api-guide", From: 1, To: 2, Diff: "--- a\n+++ b\n-cũ\n+mới\n"}
	press(t, m, "d", "ctrl+s")
	hasAll(t, plain(m.Render()), "Diff api-guide", "+mới")
	press(t, m, "esc", "c")
	hasAll(t, plain(m.Render()), "Tên skill hoặc ID đề xuất", "bint skill check")
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.skill_check").(evolvesvc.SkillCheckOptions); o.Target != "api-guide" {
		t.Fatalf("%+v", o)
	}
	c.values["system.skill_check"] = evolvesvc.SkillCheckResult{OK: false, Reports: []evolve.TriggerReport{{Skill: "api-guide", Paths: map[string]int{"api/**": 0}, Problems: []string{"mẫu không khớp tệp nào"}}}}
	press(t, m, "c", "ctrl+s")
	hasAll(t, plain(m.Render()), "Kiểm tra điều kiện kích hoạt", "api/**: khớp 0 tệp", "mẫu không khớp tệp nào", "Điều kiện kích hoạt không đạt")
	press(t, m, "esc", "g")
	hasAll(t, plain(m.Render()), "Mô hình hiện dùng", "bint skill regress")
	press(t, m, "ctrl+s")
	if _, ok := c.last(t, "system.skill_regress").(evolvesvc.SkillRegressOptions); !ok {
		t.Fatal("regress phải gửi SkillRegressOptions")
	}
}

// Sync: xem trước (trạng thái + diff từng tệp) → xác nhận "không" thì không ghi; "có" thì gửi system.sync; --force là hộp đỏ.
func TestSystemSyncPreviewConfirmAndForce(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	plan := bintapp.SyncPlan{Changes: 1, Conflicts: 1, Files: []bintapp.SyncFile{
		{Skill: "b-do", Path: ".claude/skills/b-do/SKILL.md", Result: fsafe.Updated, Diff: "--- a\n+++ b\n-cũ\n+mới\n"},
		{Skill: "b-feat", Path: ".claude/skills/b-feat/SKILL.md", Result: fsafe.Conflict, Detail: "đã bị sửa ngoài Bint"}}}
	c.values["system.sync_plan"] = plan
	press(t, m, ".", ".", ".", "s")
	if o := c.last(t, "system.sync_plan").(bintapp.SyncOptions); o.Force {
		t.Fatalf("%+v", o)
	}
	hasAll(t, plain(m.Render()), "Xem trước sync", "1 tệp sẽ đổi, 1 xung đột", "~ .claude/skills/b-do/SKILL.md", "✗ .claude/skills/b-feat/SKILL.md", "+mới")
	press(t, m, "esc")
	if c.count("system.sync") != 0 {
		t.Fatal("trả lời không thì không ghi")
	}
	press(t, m, "s", "enter")
	if o := c.last(t, "system.sync").(bintapp.SyncOptions); o.Force {
		t.Fatalf("%+v", o)
	}
	// --force: hộp đỏ nêu rõ điều bị bỏ qua.
	c.values["system.sync_plan"] = plan
	press(t, m, "F")
	if o := c.last(t, "system.sync_plan").(bintapp.SyncOptions); !o.Force {
		t.Fatalf("F phải xem trước với Force: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Sync --force: ghi đè", "ghi đè skill đã bị sửa tay")
	press(t, m, "enter")
	if o := c.last(t, "system.sync").(bintapp.SyncOptions); !o.Force {
		t.Fatalf("--force phải được truyền: %+v", o)
	}
	// Kết quả hiện từng tệp và thông báo.
	m.Update(actions.ResultMsg{ID: "system.sync", Value: bintapp.SkillInstall{Conflicts: 1, Outcomes: []skills.Outcome{{Skill: "b-do", Path: ".claude/skills/b-do/SKILL.md", Result: fsafe.Updated}}}})
	hasAll(t, plain(m.Render()), "Kết quả sync", "b-do: .claude/skills/b-do/SKILL.md", "Đã sync, 1 xung đột")
	// Không có gì để ghi: chỉ báo, không mở hộp.
	c.values["system.sync_plan"] = bintapp.SyncPlan{}
	press(t, m, "esc", "s")
	hasAll(t, plain(m.Render()), "Skill đã khớp")
	if m.sy.Capturing() {
		t.Fatal("không có thay đổi thì không mở hộp")
	}
}

func TestSystemIndexRebuildConfirmAndReport(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	press(t, m, ".", ".", ".", ".")
	hasAll(t, plain(m.Render()), "Chỉ mục tri thức", "FTS5", "bint index rebuild", "Chưa dựng lại")
	press(t, m, "b")
	hasAll(t, plain(m.Render()), "Dựng lại chỉ mục?")
	press(t, m, "esc")
	if c.count("system.index_rebuild") != 0 {
		t.Fatal("Esc không được dựng lại")
	}
	press(t, m, "b", "enter")
	if _, ok := c.last(t, "system.index_rebuild").(bintapp.IndexRebuildOptions); !ok {
		t.Fatal("phải gửi IndexRebuildOptions")
	}
	m.Update(actions.ResultMsg{ID: "system.index_rebuild", Value: bintapp.IndexRebuildResult{Total: 12, Errors: nil}})
	hasAll(t, plain(m.Render()), "Đã dựng lại chỉ mục: 12 mục, 0 tệp hỏng")
}

func TestSystemDoctorShowsAllChecksAndFilters(t *testing.T) {
	var runs atomic.Int32
	m, _ := newSysModel(t, Options{SystemLoad: syscreen.Loaders{Doctor: func(ctx context.Context, dir string) (*bintapp.DoctorReport, error) {
		runs.Add(1)
		return sampleSystemLoaders().Doctor(ctx, dir)
	}}}, 140, 40)
	press(t, m, ",", ",", ",") // lùi qua Cập nhật, Cài đặt tới Doctor
	out := plain(m.Render())
	hasAll(t, out, "Doctor: 1 cảnh báo, 1 lỗi", "git", "cấu hình", "hook claude", "tri thức", "Chi tiết · git", "2.47.0")
	press(t, m, "w")
	out = plain(m.Render())
	hasAll(t, out, "chỉ cảnh báo/lỗi", "cấu hình", "hook claude")
	if strings.Contains(out, "▸ ✓ git") || strings.Contains(out, "tri thức") {
		t.Fatalf("lọc cảnh báo/lỗi phải bỏ mục đạt và thông tin:\n%s", out)
	}
	press(t, m, "j")
	hasAll(t, plain(m.Render()), "Chi tiết · hook claude", "chưa cài")
	before := runs.Load()
	press(t, m, "ctrl+r")
	if runs.Load() != before+1 {
		t.Fatalf("Ctrl+R phải chạy lại doctor: %d → %d", before, runs.Load())
	}
}

// Ngôn ngữ: chọn rồi lưu gửi UILanguageOptions; sau khi ghi giao diện đổi ngay (vi → en), auto chọn theo biến môi trường.
func TestSystemLanguageSaveChangesUIAtOnce(t *testing.T) {
	m, c := newSysModel(t, Options{Getenv: func(k string) string { return map[string]string{"LANG": "en_US.UTF-8"}[k] }}, 140, 40)
	press(t, m, ".", ".", ".", ".", ".")
	hasAll(t, plain(m.Render()), "Ngôn ngữ giao diện", "Đang dùng trong phiên", "auto", "vi", "en", ".bint/local.yml", ".bint/config.yml")
	press(t, m, "j", "j", "enter") // en
	hasAll(t, plain(m.Render()), "Ngôn ngữ", "Ghi vào", "bint")
	press(t, m, "ctrl+s")
	o := c.last(t, "system.language_save").(bintapp.UILanguageOptions)
	if o.Language != "en" || o.Layer != "local" {
		t.Fatalf("%+v", o)
	}
	_, cmd := m.Update(actions.ResultMsg{ID: "system.language_save", Opts: o, Value: bintapp.UILanguageResult{Changed: true, Language: "en", Path: "/work/demo/.bint/local.yml"}})
	drive(t, m, cmd)
	if m.Lang() != "en" {
		t.Fatalf("giao diện phải đổi ngay sang en, đang %s", m.Lang())
	}
	hasAll(t, plain(m.Render()), "Interface language", "ui.language = en")
	// auto → theo LANG (en_US) của môi trường giả.
	_, cmd = m.Update(actions.LanguageMsg{Lang: "vi"})
	drive(t, m, cmd)
	if m.Lang() != "vi" {
		t.Fatal(m.Lang())
	}
	m.Update(actions.LanguageMsg{Lang: "auto"})
	if m.Lang() != "en" {
		t.Fatalf("auto phải theo LANG=en_US: %s", m.Lang())
	}
}

// Ở worktree phụ và --read-only, đổi ngôn ngữ/sửa cấu hình không mở form và không gửi yêu cầu ghi.
func TestSystemLanguageAndConfigRefusedInLinkedWorktreeAndReadOnly(t *testing.T) {
	linked := func(context.Context, string) (bintapp.ConfigViewData, error) {
		d := sampleConfig()
		d.LinkedWorktree, d.Main = true, "/work/main"
		return d, nil
	}
	m, c := newSysModel(t, Options{SystemLoad: syscreen.Loaders{Config: linked}}, 140, 40)
	press(t, m, "e")
	if m.sy.Capturing() {
		t.Fatal("sửa cấu hình ở worktree phụ không được mở form")
	}
	hasAll(t, plain(m.Render()), "worktree phụ", "/work/main")
	press(t, m, ".", ".", ".", ".", ".", "enter")
	if m.sy.Capturing() || c.count("system.language_save") != 0 {
		t.Fatal("lưu ngôn ngữ ở worktree phụ không được gửi")
	}
	hasAll(t, plain(m.Render()), "worktree phụ")
	// Chỉ đọc: cùng kết quả, kèm nhãn chế độ chỉ đọc.
	ro, c2 := newSysModel(t, Options{ReadOnly: true}, 140, 40)
	press(t, ro, "e")
	hasAll(t, plain(ro.Render()), "chế độ chỉ đọc")
	press(t, ro, ".", ".", ".", ".", ".", "enter")
	hasAll(t, plain(ro.Render()), "chế độ chỉ đọc")
	if ro.sy.Capturing() || c2.count("system.language_save") != 0 || c2.count("system.config_edit") != 0 {
		t.Fatal("chỉ đọc: không form, không ghi")
	}
}

// Ở --read-only mọi hành động Writes mới bị từ chối qua phím, bảng lệnh và yêu cầu trực tiếp; đọc vẫn dùng được.
func TestSystemReadOnlyDeniesEveryWritingAction(t *testing.T) {
	m, c := newSysModel(t, Options{ReadOnly: true}, 140, 40)
	keys := map[int][]string{ // thẻ con → phím ghi
		syscreen.TabConfig: {"e"}, syscreen.TabComponents: {"n", "a", "d"}, syscreen.TabSkills: {"o", "s", "F"}, syscreen.TabIndex: {"b", "enter"},
		syscreen.TabLanguage: {"enter"}, syscreen.TabSetup: {"e", "enter", "U"}, syscreen.TabUpdate: {"u", "enter"},
	}
	for tab := 0; tab < 9; tab++ {
		for _, k := range keys[tab] {
			m.toast = ""
			press(t, m, k)
			if m.sy.Capturing() {
				t.Errorf("thẻ %d phím %s mở hộp/form dù chỉ đọc", tab, k)
				press(t, m, "esc")
			}
		}
		press(t, m, ".")
	}
	for _, a := range actions.Default().All() {
		if a.Screen != actions.System || !a.Writes {
			continue
		}
		_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
		drive(t, m, cmd)
		if c.count(a.ID) != 0 {
			t.Errorf("%s đã tới Do ở chế độ chỉ đọc", a.ID)
		}
	}
	// Bảng lệnh ẩn hành động ghi của Hệ thống.
	press(t, m, ":")
	for _, r := range "Lưu ngôn ngữ" {
		press(t, m, string(r))
	}
	if out := plain(m.Render()); strings.Contains(out, "Lưu ngôn ngữ") && strings.Count(out, "Lưu ngôn ngữ") > 1 {
		t.Fatalf("bảng lệnh không được hiện hành động ghi ở chế độ chỉ đọc:\n%s", out)
	}
	press(t, m, "esc")
	// Hành động đọc dùng được (sau vòng 9 thẻ đang ở Cấu hình).
	press(t, m, ".", ".", "R")
	if !m.sy.Capturing() {
		t.Fatal("render là hành động đọc, phải mở được ở chế độ chỉ đọc")
	}
}

// ---- sửa cấu hình với dịch vụ thật và trình soạn thảo giả ----

// sysReal dựng repository thật đã init và mô hình dùng dịch vụ thật, trình soạn thảo giả ghi lại đường dẫn bản tạm.
func sysReal(t *testing.T) (m *Model, root string, execs *[]actions.ExecMsg) {
	t.Helper()
	return sysRealWith(t, nil, nil)
}

// sysRealWith như sysReal; before chạy sau khi init xong (chuẩn bị tệp trong repo), mut chỉnh Options của mô hình.
func sysRealWith(t *testing.T, before func(root string), mut func(*Options)) (m *Model, root string, execs *[]actions.ExecMsg) {
	t.Helper()
	testutil.Isolate(t)
	asHumanTUI(t)
	root = testutil.NewRepo(t, map[string]string{"go.mod": "module demo\n"})
	if _, err := bintapp.Init(context.Background(), bintapp.InitOptions{Dir: root, BintPath: "bint", Harnesses: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	if before != nil {
		before(root)
	}
	var recorded []actions.ExecMsg
	var list []actions.Action
	for _, a := range actions.Default().All() {
		a := a
		if a.ID == "system.config_edit" || a.ID == "system.open_file" {
			orig := a.Do
			a.Do = func(ctx context.Context, env actions.Env, o any) tea.Msg {
				msg := orig(ctx, env, o)
				if em, ok := msg.(actions.ExecMsg); ok {
					recorded = append(recorded, em)
				}
				return msg
			}
		}
		list = append(list, a)
	}
	opts := Options{Dir: root, Mode: theme.Mono, Registry: actions.NewRegistry(list), Editor: func(string) *exec.Cmd { return exec.Command("true") }}
	if mut != nil {
		mut(&opts)
	}
	m = newModel(t, opts, sampleData(), 140, 40)
	press(t, m, "7")
	return m, root, &recorded
}

func asHumanTUI(t *testing.T) {
	t.Helper()
	for _, k := range bintapp.AgentEnvVars() {
		t.Setenv(k, "")
	}
}

// editWith mô phỏng người sửa bản tạm trong $EDITOR rồi thoát: ghi nội dung và gửi kết quả của ExecMsg.
func editWith(t *testing.T, m *Model, em actions.ExecMsg, data string) {
	t.Helper()
	st := em.Opts.(actions.ConfigEditState)
	if err := os.WriteFile(st.Edit.TempPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(actions.ResultMsg{ID: "system.config_edit", Opts: em.Opts})
	drive(t, m, cmd)
}

func TestSystemConfigEditInvalidKeepsOriginalAndShowsError(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	hasAll(t, plain(m.Render()), "Lớp nguồn", "project")
	press(t, m, "e")
	hasAll(t, plain(m.Render()), "Lớp cần sửa")
	press(t, m, "ctrl+s") // lớp mặc định: project
	if len(*execs) != 1 {
		t.Fatalf("phải mở trình soạn thảo một lần: %d", len(*execs))
	}
	st := (*execs)[0].Opts.(actions.ConfigEditState)
	if b, _ := os.ReadFile(st.Edit.TempPath); string(b) != string(orig) {
		t.Fatal("bản tạm phải là bản sao của tệp gốc")
	}
	bad := strings.Replace(string(orig), "language: auto", "language: fr", 1)
	editWith(t, m, (*execs)[0], bad)
	out := plain(m.Render())
	hasAll(t, out, "Cấu hình không hợp lệ", "language", "Sửa lại")
	if b, _ := os.ReadFile(proj); string(b) != string(orig) {
		t.Fatal("tệp gốc không được đổi khi nội dung không hợp lệ")
	}
	// Bỏ: tệp gốc giữ nguyên, bản tạm bị xoá.
	press(t, m, "esc")
	if b, _ := os.ReadFile(proj); string(b) != string(orig) {
		t.Fatal("tệp gốc không được đổi")
	}
	if _, err := os.Stat(st.Edit.TempPath); err == nil {
		t.Fatal("bản tạm phải bị xoá khi bỏ")
	}
	hasAll(t, plain(m.Render()), "tệp gốc giữ nguyên")
}

func TestSystemConfigEditValidWritesAfterConfirmation(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	good := strings.Replace(string(orig), "language: auto", "language: en", 1)
	editWith(t, m, (*execs)[0], good)
	hasAll(t, plain(m.Render()), "Ghi cấu hình?", "-  language: auto", "+  language: en")
	if b, _ := os.ReadFile(proj); string(b) != string(orig) {
		t.Fatal("chưa xác nhận thì chưa ghi")
	}
	press(t, m, "enter")
	if b, _ := os.ReadFile(proj); string(b) != good {
		t.Fatalf("phải ghi bản đã sửa:\n%s", b)
	}
	hasAll(t, plain(m.Render()), "Đã ghi cấu hình")
	// Không đổi gì → không ghi.
	press(t, m, "e", "ctrl+s")
	st := (*execs)[1].Opts.(actions.ConfigEditState)
	b, _ := os.ReadFile(st.Edit.TempPath)
	editWith(t, m, (*execs)[1], string(b))
	hasAll(t, plain(m.Render()), "Không có thay đổi")
}

func TestSystemConfigEditRefusedWhenOriginalChangedMeanwhile(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	st := (*execs)[0].Opts.(actions.ConfigEditState)
	other := string(orig) + "# người khác vừa sửa\n"
	if err := os.WriteFile(proj, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	mine := strings.Replace(string(orig), "language: auto", "language: en", 1)
	editWith(t, m, (*execs)[0], mine)
	press(t, m, "enter") // xác nhận ghi
	if b, _ := os.ReadFile(proj); string(b) != other {
		t.Fatalf("tệp gốc đổi trong lúc sửa thì không được ghi đè:\n%s", b)
	}
	// 5a.1: bản sửa của người dùng KHÔNG bị xoá; hộp cho sửa lại hoặc bỏ.
	if b, err := os.ReadFile(st.Edit.TempPath); err != nil || string(b) != mine {
		t.Fatalf("bản tạm phải còn nguyên sau khi bị từ chối: %v %q", err, b)
	}
	hasAll(t, plain(m.Render()), "đã bị đổi", "Sửa lại")
	// Sửa lại: mở lại bản tạm; người dùng giữ nguyên bản sửa (đã gộp tay) rồi thoát → diff theo tệp gốc MỚI.
	press(t, m, "enter")
	if len(*execs) != 2 {
		t.Fatalf("sửa lại phải mở trình soạn thảo lần nữa: %d", len(*execs))
	}
	merged := strings.Replace(other, "language: auto", "language: en", 1)
	if err := os.WriteFile(st.Edit.TempPath, []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(actions.ResultMsg{ID: "system.open_file", Opts: (*execs)[1].Opts})
	drive(t, m, cmd)
	out := plain(m.Render())
	hasAll(t, out, "Ghi cấu hình?", "+  language: en")
	if strings.Contains(out, "người khác vừa sửa") && strings.Contains(out, "-# người khác") {
		t.Fatalf("diff phải theo tệp gốc mới, không xoá dòng của người khác:\n%s", out)
	}
	press(t, m, "enter")
	if b, _ := os.ReadFile(proj); string(b) != merged {
		t.Fatalf("lần ghi lại phải thành công:\n%s", b)
	}
	if _, err := os.Stat(st.Edit.TempPath); err == nil {
		t.Fatal("ghi thành công thì bản tạm phải bị xoá")
	}
}

// 5a.1: Validate lỗi lúc ghi (không phải lúc kiểm) cũng giữ bản tạm; Bỏ thì xoá.
func TestSystemConfigEditWriteRejectedKeepsTempUntilDiscard(t *testing.T) {
	m, root, execs := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, "e", "ctrl+s")
	st := (*execs)[0].Opts.(actions.ConfigEditState)
	editWith(t, m, (*execs)[0], strings.Replace(string(orig), "language: auto", "language: en", 1))
	// Giữa lúc kiểm và ghi, một lớp khác làm bản sửa không còn hợp lệ: local.yml đặt giá trị sai.
	if err := os.WriteFile(config.LocalPath(root), []byte("ui:\n  language: fr\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	press(t, m, "enter")
	if _, err := os.Stat(st.Edit.TempPath); err != nil {
		t.Fatalf("bản tạm phải còn sau khi ghi bị từ chối: %v", err)
	}
	hasAll(t, plain(m.Render()), "Sửa lại")
	press(t, m, "esc")
	if _, err := os.Stat(st.Edit.TempPath); err == nil {
		t.Fatal("Bỏ thì bản tạm phải bị xoá")
	}
}

// Ghi ngôn ngữ bằng dịch vụ thật qua màn hình: local.yml được tạo, config.yml không đổi, giao diện đổi ngay.
func TestSystemLanguageSaveRealServiceChangesLanguage(t *testing.T) {
	m, root, _ := sysReal(t)
	proj := config.ProjectPath(root)
	orig, _ := os.ReadFile(proj)
	press(t, m, ".", ".", ".", ".", ".", "j", "j", "enter", "ctrl+s") // en, local
	if m.Lang() != "en" {
		t.Fatalf("giao diện phải đổi ngay: %s", m.Lang())
	}
	b, err := os.ReadFile(config.LocalPath(root))
	if err != nil || !strings.Contains(string(b), "language: en") {
		t.Fatalf("local.yml: %v %q", err, b)
	}
	if now, _ := os.ReadFile(proj); string(now) != string(orig) {
		t.Fatal("config.yml không được đổi khi ghi vào local")
	}
}

// Hộp duyệt script và hộp sync --force là hộp ĐỎ (nút xác nhận tô màu lỗi); hộp duyệt guidance thì không.
func TestSystemRedBoxesUseErrorRole(t *testing.T) {
	mode := theme.Modes[0]
	for _, m := range theme.Modes {
		if m != theme.Mono {
			mode = m
			break
		}
	}
	th := theme.New(mode)
	marker, _, _ := strings.Cut(th.S(theme.Err, "x"), "x")
	if marker == "" {
		t.Skip("chế độ màu không có mã màu")
	}
	button := func(keys ...string) string { // dòng chứa nút xác nhận của hộp đang mở
		m := sysAt(t, Options{Mode: mode}, 140, 40, 2, keys...)
		for _, l := range strings.Split(m.Render(), "\n") {
			if strings.Contains(plain(l), "[Enter] Duyệt") {
				return l
			}
		}
		t.Fatalf("không thấy nút Duyệt:\n%s", plain(m.Render()))
		return ""
	}
	if !strings.Contains(button("j", "j", "a"), marker) {
		t.Error("hộp duyệt script phải tô màu lỗi (hộp đỏ)")
	}
	if strings.Contains(button("j", "a"), marker) {
		t.Error("hộp duyệt guidance không được là hộp đỏ")
	}
	// sync --force: hộp đỏ.
	m := sysAt(t, Options{Mode: mode}, 140, 40, 3)
	m.Update(actions.ResultMsg{ID: "system.sync_plan", Opts: bintapp.SyncOptions{Force: true}, Value: bintapp.SyncPlan{Changes: 1, Files: []bintapp.SyncFile{{Path: "a", Result: fsafe.Updated}}}})
	found := false
	for _, l := range strings.Split(m.Render(), "\n") {
		if strings.Contains(plain(l), "[Enter] Ghi") && strings.Contains(l, marker) {
			found = true
		}
	}
	if !found {
		t.Error("hộp sync --force phải là hộp đỏ")
	}
}

// 5a.5: sau khi ghi ui.language, ngôn ngữ trong phiên theo đúng thứ tự --lang > BINT_LANG > ui.language hiệu lực (đã ghép lớp) > LC_ALL/LANG;
// khi lớp cao hơn hoặc BINT_LANG/--lang che giá trị vừa ghi thì toast cảnh báo nêu nguồn che.
func TestSystemLanguageShadowed(t *testing.T) {
	t.Run("ghi vào project khi local.yml đặt ngôn ngữ khác", func(t *testing.T) {
		m, root, _ := sysRealWith(t, func(root string) {
			if err := os.WriteFile(config.LocalPath(root), []byte("ui:\n  language: vi\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, func(o *Options) { o.Lang, o.SystemLoad.Config = "en", bintapp.ConfigView })
		press(t, m, ".", ".", ".", ".", ".", "j", "enter", "tab", " ", "ctrl+s") // chọn en, ghi vào project
		if b, _ := os.ReadFile(config.ProjectPath(root)); !strings.Contains(string(b), "language: en") {
			t.Fatalf("config.yml phải được ghi en:\n%s", b)
		}
		if m.Lang() != "vi" {
			t.Fatalf("local.yml (vi) che giá trị vừa ghi: giao diện phải là vi, đang %s", m.Lang())
		}
		hasAll(t, plain(m.Render()), "ui.language = en", ".bint/local.yml")
	})
	for _, c := range []struct{ name, source string }{{"BINT_LANG", "BINT_LANG"}, {"--lang", "--lang"}} {
		t.Run(c.name+" che giá trị vừa ghi", func(t *testing.T) {
			m, root, _ := sysRealWith(t, nil, func(o *Options) {
				o.Lang, o.SystemLoad.Config = "en", bintapp.ConfigView
				if c.name == "BINT_LANG" {
					o.Getenv = func(k string) string {
						if k == "BINT_LANG" {
							return "en"
						}
						return ""
					}
				} else {
					o.LangFlag = "en"
				}
			})
			press(t, m, ".", ".", ".", ".", ".", "j", "enter", "ctrl+s") // chọn vi, ghi vào local
			if b, _ := os.ReadFile(config.LocalPath(root)); !strings.Contains(string(b), "language: vi") {
				t.Fatalf("local.yml phải được ghi vi:\n%s", b)
			}
			if m.Lang() != "en" {
				t.Fatalf("%s che giá trị vừa ghi: giao diện phải giữ en, đang %s", c.source, m.Lang())
			}
			hasAll(t, plain(m.Render()), "ui.language = vi", c.source)
		})
	}
	t.Run("không bị che thì đổi ngay", func(t *testing.T) {
		m, _, _ := sysRealWith(t, nil, func(o *Options) {
			o.Getenv, o.SystemLoad.Config = func(string) string { return "" }, bintapp.ConfigView
		})
		press(t, m, ".", ".", ".", ".", ".", "j", "j", "enter", "ctrl+s")
		if m.Lang() != "en" {
			t.Fatalf("giao diện phải đổi ngay: %s", m.Lang())
		}
		if out := plain(m.Render()); strings.Contains(out, "takes precedence") || strings.Contains(out, "ưu tiên hơn") {
			t.Fatalf("không bị che thì không có cảnh báo:\n%s", plain(m.Render()))
		}
	})
}

// Flow (GĐ2 của custom-flow-plan): ba hành động chỉ đọc, không phím, gọi từ bảng lệnh; kết quả ở khung đầu ra.
func TestSystemFlowActionsFromPalette(t *testing.T) {
	m, c := newSysModel(t, Options{}, 140, 40)
	palette := func(label string) {
		t.Helper()
		press(t, m, ":")
		for _, r := range label {
			press(t, m, string(r))
		}
		press(t, m, "enter")
	}

	c.values["system.flow_list"] = []bintapp.FlowRow{
		{Name: "b-do", Source: bintapp.FlowBuiltin, Steps: 3, OK: true, Problems: []string{}, Description: "luồng gốc"},
		{Name: "tdd", Source: bintapp.FlowCustom, Path: ".bint/flows/tdd.yml", Steps: 4, Problems: []string{"skill \"x\" không tồn tại"}},
	}
	palette("Liệt kê flow nhúng")
	hasAll(t, plain(m.Render()), "Danh sách flow", "b-do", "nhúng", "3 bước", "tdd", ".bint/flows/tdd.yml", "4 bước", "lỗi:", "không tồn tại")

	c.values["system.flow_show"] = bintapp.FlowShowResult{Name: "tdd", Source: bintapp.FlowCustom, Path: ".bint/flows/tdd.yml", Hash: "sha256:abc",
		YAML: "schema: 1\nname: tdd\nsteps:\n  - id: green\n", Errors: []string{}, Warnings: []string{"bước \"x\": verify bị bỏ qua"}}
	palette("In định nghĩa chuẩn hoá")
	hasAll(t, plain(m.Render()), "Tên flow", "bint flow show")
	for _, r := range "tdd" {
		press(t, m, string(r))
	}
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.flow_show").(bintapp.FlowShowOptions); o.Name != "tdd" {
		t.Fatalf("%+v", o)
	}
	hasAll(t, plain(m.Render()), "Flow tdd", "nguồn: .bint/flows/tdd.yml", "băm: sha256:abc", "id: green", "cảnh báo:")

	c.values["system.flow_lint"] = []bintapp.FlowLintResult{{Name: "b-do", Source: bintapp.FlowBuiltin, Errors: []string{}, Warnings: []string{}}}
	palette("Kiểm định nghĩa flow")
	hasAll(t, plain(m.Render()), "bint flow lint")
	press(t, m, "ctrl+s")
	if o := c.last(t, "system.flow_lint").(bintapp.FlowLintOptions); o.Name != "" {
		t.Fatalf("lint không tên phải kiểm mọi flow: %+v", o)
	}
	hasAll(t, plain(m.Render()), "Kiểm flow", "b-do (nhúng)")
}
