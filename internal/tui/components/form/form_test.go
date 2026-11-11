package form

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/i18n"
	"bint/internal/tui/components/text"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func k(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+y":
		return tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func typeText(m *Model, s string) {
	for _, c := range s {
		m.Update(k(string(c)))
	}
}

func names(fs []Field) string {
	var n []string
	for _, f := range fs {
		n = append(n, f.Name)
	}
	return strings.Join(n, ",")
}

// Trường sinh đúng kiểu, nhóm Nâng cao, ẩn, bắt buộc, lựa chọn từ thẻ của Options.
func TestFieldsFromOptions(t *testing.T) {
	fs := Fields(app.AddOptions{})
	if got := names(fs); got != "type,title,body,paths,symbols,tags,domains,evidence,input,origin,third-party,confidence,run-id" {
		t.Fatalf("AddOptions: %s (eval-case phải ẩn, Now không có thẻ)", got)
	}
	by := map[string]Field{}
	for _, f := range fs {
		by[f.Name] = f
	}
	if f := by["type"]; f.Kind != Choice || !f.Required || len(f.Choices) != 6 {
		t.Errorf("type: %+v", f)
	}
	if f := by["body"]; !f.Arg || !f.Multiline || !f.Required || f.Kind != String {
		t.Errorf("body: %+v", f)
	}
	if f := by["paths"]; f.Kind != Strings || f.Suggest != "files" || f.Advanced {
		t.Errorf("paths: %+v", f)
	}
	if f := by["symbols"]; !f.Advanced {
		t.Errorf("symbols: %+v", f)
	}
	if f := by["third-party"]; f.Kind != Bool {
		t.Errorf("third-party: %+v", f)
	}
	if got := names(AllFields(app.AddOptions{})); !strings.Contains(got, "eval-case") {
		t.Errorf("AllFields phải gồm trường ẩn: %s", got)
	}
	if got := names(Fields(&app.RemoveOptions{})); got != "id" {
		t.Errorf("RemoveOptions (con trỏ): %s", got)
	}
	if got := names(Fields(app.VerifyOptions{})); got != "id,apply" {
		t.Errorf("VerifyOptions: %s", got)
	}
	rv := Fields(app.RevalidateOptions{})
	kinds := map[string]Kind{}
	for _, f := range rv {
		kinds[f.Name] = f.Kind
	}
	if kinds["max-items"] != Int || kinds["budget"] != Float || kinds["model"] != String {
		t.Errorf("RevalidateOptions: %v", kinds)
	}
	if got := names(Fields(app.PublishOptions{})); got != "force" {
		t.Errorf("PublishOptions chỉ còn force (dry-run, yes ẩn): %s", got)
	}
	if Fields(app.StatsOptions{}) != nil {
		t.Error("StatsOptions không có ô nào")
	}
}

// Dòng lệnh CLI tương đương sinh từ giá trị: chỉ cờ khác mặc định, nháy đơn khi cần, rồi đối số vị trí.
func TestCLIEquivalent(t *testing.T) {
	m := New("knowledge.add", "knowledge add", app.AddDefaults(config.Config{}))
	m.Update(k("tab")) // type có sẵn "warning" từ Defaults
	typeText(&m, "")
	m.FocusField("paths")
	typeText(&m, "internal/db/**")
	m.FocusField("evidence")
	typeText(&m, "internal/db/db.go:40-52, README.md")
	m.FocusField("body")
	typeText(&m, "Mở SQLite bằng store.Open")
	want := "bint knowledge add --paths 'internal/db/**' --evidence internal/db/db.go:40-52 --evidence README.md 'Mở SQLite bằng store.Open'"
	if got := m.CLI(); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	m.FocusField("third-party")
	m.Update(k("space"))
	if !strings.Contains(m.CLI(), " --third-party ") {
		t.Fatalf("%s", m.CLI())
	}
	// Giá trị khác mặc định của loại.
	m.FocusField("type")
	m.Update(k("right"))
	v, err := m.Build()
	if err != nil || v.(app.AddOptions).Type != "pattern" {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(m.CLI(), "--type pattern") {
		t.Fatalf("%s", m.CLI())
	}
	if got := Quote("it's"); got != `'it'\''s'` {
		t.Fatalf("%s", got)
	}
}

func TestSubmitValidationShownAtField(t *testing.T) {
	m := New("knowledge.add", "knowledge add", app.AddOptions{Type: "rule"})
	if r := m.Update(k("ctrl+s")); r != None {
		t.Fatalf("thiếu nội dung bắt buộc không được gửi: %v", r)
	}
	if m.FieldErr[2] != "required" { // body
		t.Fatalf("lỗi theo ô: %v", m.FieldErr)
	}
	if m.Fields[m.rows()[m.Focus].field].Name != "body" {
		t.Fatalf("tiêu điểm phải về ô lỗi: %d", m.Focus)
	}
	typeText(&m, "Nội dung")
	if m.FieldErr[2] != "" {
		t.Fatal("gõ vào ô thì lỗi của ô biến mất")
	}
	if r := m.Update(k("ctrl+s")); r != Submit {
		t.Fatalf("%v err=%q", r, m.Err)
	}
	if v := m.Value().(app.AddOptions); v.Body != "Nội dung" || v.Type != "rule" {
		t.Fatalf("%+v", v)
	}
}

// Lỗi mức giá trị từ Validate() của Options hiện ở ô được nhắc tới (theo --tên-cờ) hoặc dòng lỗi chung.
func TestValidateErrorLandsOnNamedField(t *testing.T) {
	m := New("knowledge.revalidate", "knowledge revalidate", app.RevalidateDefaults(config.Config{}))
	m.FocusField("budget")
	typeText(&m, "-3")
	if r := m.Update(k("enter")); r != None || !strings.Contains(m.Err, "--budget") {
		t.Fatalf("%v %q", r, m.Err)
	}
	if got := m.Fields[m.rows()[m.Focus].field].Name; got != "budget" {
		t.Fatalf("tiêu điểm ở %s, muốn budget", got)
	}
	// Số sai kiểu.
	m2 := New("knowledge.search", "knowledge search", app.SearchDefaults(config.Config{}))
	typeText(&m2, "truy vấn")
	m2.FocusField("limit")
	typeText(&m2, "abc")
	if r := m2.Update(k("ctrl+s")); r != None || m2.FieldErr[1] != "number" {
		t.Fatalf("%v %v", r, m2.FieldErr)
	}
}

func TestAdvancedGroupCollapsedByDefault(t *testing.T) {
	m := New("knowledge.add", "knowledge add", app.AddDefaults(config.Config{}))
	shown := func() int { return len(m.rows()) }
	collapsed := shown()
	if collapsed != 6+1 { // 6 ô thường + tiêu đề nhóm
		t.Fatalf("%d hàng", collapsed)
	}
	m.Update(k("ctrl+o"))
	if shown() != 13+1 {
		t.Fatalf("%d hàng sau khi mở nhóm Nâng cao", shown())
	}
	m.Update(k("ctrl+o"))
	m.FocusField("run-id") // ô nằm trong nhóm: tự mở nhóm
	if !m.Advanced {
		t.Fatal("FocusField phải mở nhóm Nâng cao")
	}
}

func TestSuggestionGhostAndAccept(t *testing.T) {
	m := New("knowledge.add", "knowledge add", app.AddDefaults(config.Config{}))
	m.SetSuggestions("files", []string{"README.md", "internal/db/db.go", "internal/api/api.go"})
	m.FocusField("evidence")
	typeText(&m, "README.md:1, internal/d")
	if g := m.ghost(m.Fields[7], m.inputs[7].Text); g != "b/db.go" {
		t.Fatalf("gợi ý %q", g)
	}
	m.Update(k("right"))
	if m.inputs[7].Text != "README.md:1, internal/db/db.go" {
		t.Fatalf("%q", m.inputs[7].Text)
	}
}

func TestCancelAndCopyKeys(t *testing.T) {
	m := New("knowledge.rm", "knowledge rm", app.RemoveOptions{})
	if m.Update(k("esc")) != Cancel || m.Update(k("ctrl+y")) != Copy {
		t.Fatal("Esc huỷ, Ctrl+Y chép")
	}
}

// Form vẽ vừa vùng ở mọi locale (gồm locale giả kéo dài), hiện dấu * cho ô bắt buộc và dòng lệnh CLI.
func TestViewFitsAndShowsCLI(t *testing.T) {
	for _, lang := range []string{"vi", "en", i18n.Pseudo} {
		for _, sz := range [][2]int{{70, 16}, {100, 24}} {
			env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad(lang), W: sz[0], H: sz[1]}
			m := New("knowledge.add", "knowledge add", app.AddDefaults(config.Config{}))
			typeText(&m, "")
			m.FocusField("body")
			typeText(&m, "Ắ ề ộ ữ ợ nội dung dài hơn khung "+strings.Repeat("x", 120))
			m.Update(k("ctrl+s")) // gửi sẽ qua; kiểm vẽ sau Validate
			out := m.View(env, 0)
			lines := strings.Split(out, "\n")
			if len(lines) != sz[1] {
				t.Fatalf("%s %v: %d dòng", lang, sz, len(lines))
			}
			for _, l := range lines {
				if text.Width(l) > sz[0] {
					t.Fatalf("%s %v: dòng rộng %d: %q", lang, sz, text.Width(l), l)
				}
			}
			if !strings.Contains(out, "bint knowledge add") || !strings.Contains(out, "*") {
				t.Fatalf("%s: thiếu dòng lệnh CLI hoặc dấu bắt buộc:\n%s", lang, out)
			}
		}
	}
}
