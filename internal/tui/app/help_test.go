package app

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
	"bint/internal/tui/components/help"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

// helpTabs liệt kê (màn, khoá tên thẻ phụ) của mọi thẻ phụ để test trợ giúp theo ngữ cảnh đi qua từng thẻ.
func helpTabs() map[string][]string {
	return map[string][]string{
		"2": {"kn.sub.list", "kn.sub.search", "kn.sub.stats", "kn.sub.diff", "kn.sub.revalidate", "kn.sub.publish", "kn.sub.survey", "kn.sub.context"},
		"3": {"tk.sub.info", "tk.sub.docs", "tk.sub.criteria", "tk.sub.verify", "tk.sub.review", "tk.sub.errors", "tk.sub.runs", "tk.sub.log"},
		"4": {"rn.tab.runs", "rn.tab.lessons"},
		"5": {"eval.tab.cands", "eval.tab.cases", "eval.tab.batches", "eval.tab.log"},
		"6": {"ev.tab.opps", "ev.tab.proposals", "ev.tab.history", "ev.tab.queue", "ev.tab.log"},
		"7": {"sy.tab.config", "sy.tab.harness", "sy.tab.components", "sy.tab.skills", "sy.tab.index", "sy.tab.mcp", "sy.tab.language", "sy.tab.doctor", "sy.tab.setup", "sy.tab.update"},
	}
}

// openTab đưa m tới thẻ phụ có tên name của màn screenKey bằng cách bấm chuột vào tên thẻ (dưới tiêu đề, nên không nhầm với thanh tab ngoài).
func openTab(t *testing.T, m *Model, screenKey, name string) bool {
	t.Helper()
	press(t, m, screenKey)
	if screenKey == "3" {
		press(t, m, "enter") // thẻ phụ của Tác vụ nằm ở khung chi tiết
	}
	m.Render()
	found := false
	for y, l := range lines(m.Render()) {
		if y < 4 {
			continue
		}
		if i := strings.Index(l, name); i >= 0 {
			clickAt(t, m, len([]rune(l[:i]))+1, y)
			found = true
			break
		}
	}
	st, _ := m.screens[m.Screen()].(ui.SubTabber)
	if st == nil || st.SubTab() != name {
		if !found && strings.HasSuffix(name, "Log") {
			return false // thẻ Log chỉ hiện khi có việc nền
		}
		got := ""
		if st != nil {
			got = st.SubTab()
		}
		t.Fatalf("không tới được thẻ phụ %q của màn %s (đang ở %q):\n%s", name, screenKey, got, plain(m.Render()))
	}
	return true
}

var helpPosRE = regexp.MustCompile(`(\d+)–(\d+)/(\d+) ↕`)

// helpAll đọc hết hộp trợ giúp (cuộn bằng PgDn tới cuối) và trả mọi trang gộp lại (đã bỏ khung và gộp khoảng trắng).
func helpAll(t *testing.T, m *Model) string {
	t.Helper()
	var all strings.Builder
	press(t, m, "home")
	for i := 0; i < 40; i++ {
		all.WriteString(flat(m.Render()) + "\n")
		pos := helpPosRE.FindStringSubmatch(plain(m.Render()))
		if pos == nil || pos[2] == pos[3] {
			break
		}
		press(t, m, "pgdown")
	}
	return all.String()
}

// Trợ giúp theo ngữ cảnh: ? mở hộp có tiêu đề theo màn và thẻ phụ đang xem, đoạn giới thiệu của thẻ (khoá i18n có ở vi và en), nhóm phím của ngữ
// cảnh hiện tại (chân phím trong khung của thẻ phụ) trước, rồi phím toàn cục, rồi các màn khác. Mọi màn và thẻ phụ, thường và chỉ đọc.
func TestHelpContextEveryScreenAndSubTab(t *testing.T) {
	vi, en := i18n.MustLoad("vi"), i18n.MustLoad("en")
	var writeTitles []string
	for _, a := range actions.Default().All() {
		if a.Writes && a.TitleKey != "" {
			writeTitles = append(writeTitles, vi.T(a.TitleKey))
		}
	}
	states, pairsChecked, writesChecked := 0, 0, 0
	for screenKey, tabs := range helpTabs() {
		for _, tabKey := range tabs {
			name := vi.T(tabKey)
			// Khoá giới thiệu có ở cả vi và en.
			scr := actions.Screens[int(screenKey[0]-'1')]
			code := strings.TrimPrefix(strings.TrimPrefix(tabKey[strings.Index(tabKey, ".")+1:], "sub."), "tab.")
			introKey := "help.ctx." + string(scr) + "." + code
			if !vi.Has(introKey) || !en.Has(introKey) {
				t.Errorf("thiếu khoá giới thiệu %s ở vi hoặc en", introKey)
			}
			for _, ro := range []bool{false, true} {
				m := newModel(t, Options{Mode: theme.Mono, Lang: "vi", ReadOnly: ro}, sampleData(), 200, 40)
				if !openTab(t, m, screenKey, name) {
					continue
				}
				states++
				g, ok := m.helpContext()
				if !ok || g.Intro == "" || !strings.HasPrefix(g.Title, m.groupName(scr)+" › "+name) {
					t.Errorf("%s/%s ro=%v: ngữ cảnh trợ giúp %+v", scr, name, ro, g)
					continue
				}
				if sc := m.screens[m.Screen()]; sc != nil && sc.Capturing() {
					press(t, m, "esc") // ô nhập của thẻ (vd. Tìm kiếm) nhận chữ ? nên thoát ô trước
				}
				// Phím và nhãn lấy từ chân phím trong khung ĐÃ VẼ trước khi mở hộp (không lấy từ helpContext, tránh so sánh vòng tròn).
				drawn := innerFooterPairs(m.Render())
				// Phím của hành động Writes ở cùng trạng thái, lấy từ mô hình thường (chân phím của mô hình chỉ đọc đã lọc mất chúng).
				writes := writeHintPairs(m, collectHints(m))
				if ro {
					nm := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 200, 40)
					openTab(t, nm, screenKey, name)
					if sc := nm.screens[nm.Screen()]; sc != nil && sc.Capturing() {
						press(t, nm, "esc")
					}
					writes = writeHintPairs(nm, collectHints(nm))
				}
				press(t, m, "?")
				if !m.showHelp {
					t.Fatalf("? phải mở trợ giúp ở %s/%s", scr, name)
				}
				first := flat(m.Render())
				if !strings.Contains(first, g.Title) || !strings.Contains(first, strings.Fields(g.Intro)[0]) {
					t.Errorf("%s/%s ro=%v: hộp trợ giúp thiếu tiêu đề theo ngữ cảnh %q hoặc giới thiệu:\n%s", scr, name, ro, g.Title, plain(m.Render()))
				}
				// Nhóm ngữ cảnh đứng đầu, rồi nhóm của màn, rồi toàn cục.
				groups := m.helpGroups()
				if len(groups) < 3 || groups[0].Title != g.Title || groups[1].Title != m.groupName(scr) || groups[2].Title != vi.T("help.global") {
					t.Errorf("%s/%s: thứ tự nhóm sai: %q", scr, name, titlesOf(groups))
				}
				// Từng cặp phím + nhãn của chân phím trong khung phải là MỘT HÀNG của nhóm ngữ cảnh (so cả dòng đã định dạng, không so chữ lẻ).
				group, ok := helpGroupLines(helpBoxLines(m.Render()), g.Title)
				if !ok {
					t.Errorf("%s/%s ro=%v: không đọc được nhóm ngữ cảnh %q trong trang đầu của hộp:\n%s", scr, name, ro, g.Title, plain(m.Render()))
					continue
				}
				for _, p := range missingPairs(pairsInGroup(group, drawn), drawn) {
					t.Errorf("%s/%s ro=%v: hộp trợ giúp thiếu hàng %q của chân phím trong khung (nhóm: %q)", scr, name, ro, p.String(), group)
				}
				pairsChecked += len(drawn)
				if ro {
					for _, p := range leakedPairs(pairsInGroup(group, writes), writes) {
						t.Errorf("%s/%s: chỉ đọc mà nhóm ngữ cảnh còn phím ghi %q", scr, name, p.String())
					}
					writesChecked += len(writes)
					all := helpAll(t, m)
					for _, w := range writeTitles {
						if strings.Contains(all, w) {
							t.Errorf("%s/%s: trợ giúp ở chỉ đọc còn hành động ghi %q", scr, name, w)
						}
					}
				} else {
					// Thường: phím ghi có mặt trong nhóm (để khẳng định ở chỉ đọc không rỗng nghĩa).
					for _, p := range missingPairs(pairsInGroup(group, writes), writes) {
						t.Errorf("%s/%s: chế độ thường thiếu phím ghi %q trong nhóm ngữ cảnh", scr, name, p.String())
					}
				}
			}
		}
	}
	if states < 60 {
		t.Fatalf("quét thiếu trạng thái: %d", states)
	}
	if pairsChecked < 120 || writesChecked < 25 {
		t.Fatalf("khẳng định quá ít để có nghĩa: %d cặp phím, %d phím ghi (chỉ đọc)", pairsChecked, writesChecked)
	}
}

func titlesOf(gs []help.Group) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Title)
	}
	return out
}

// Chế độ chỉ đọc bỏ phím ghi khỏi nhóm ngữ cảnh: Đánh giá › Ứng viên mất a, x, m, v nhưng vẫn có A.
func TestHelpContextReadOnlyDropsWriteKeys(t *testing.T) {
	keys := func(ro bool) map[string]bool {
		m := newModel(t, Options{Mode: theme.Mono, Lang: "vi", ReadOnly: ro}, sampleData(), 140, 40)
		press(t, m, "5")
		g, _ := m.helpContext()
		out := map[string]bool{}
		for _, r := range g.Rows {
			out[r.Key] = true
		}
		return out
	}
	normal, ro := keys(false), keys(true)
	for _, k := range []string{"a", "x", "m", "v"} {
		if !normal[k] || ro[k] {
			t.Errorf("phím ghi %q: thường=%v chỉ đọc=%v", k, normal[k], ro[k])
		}
	}
	if !ro["A"] {
		t.Errorf("phím đọc A phải còn ở chỉ đọc: %v", ro)
	}
}

// Hộp trợ giúp cuộn được bằng j/k, PgUp/PgDn, Home/End và bánh xe chuột, có vị trí cuộn thay vì cắt; cuộn tới cuối thấy dòng cuối.
func TestHelpScrollKeysAndWheel(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 100, 30)
	press(t, m, "5", "?")
	pos := func() (int, int, int) {
		p := helpPosRE.FindStringSubmatch(plain(m.Render()))
		if p == nil {
			t.Fatalf("hộp trợ giúp phải hiện vị trí cuộn:\n%s", plain(m.Render()))
		}
		var a, b, c int
		for i, dst := range []*int{&a, &b, &c} {
			for _, ch := range p[i+1] {
				*dst = *dst*10 + int(ch-'0')
			}
		}
		return a, b, c
	}
	from, to, total := pos()
	if from != 1 || to >= total {
		t.Fatalf("lúc mở phải ở đầu và còn nội dung phía dưới: %d–%d/%d", from, to, total)
	}
	press(t, m, "j")
	if f, _, _ := pos(); f != 2 {
		t.Fatalf("j cuộn một dòng: %d", f)
	}
	press(t, m, "k")
	if f, _, _ := pos(); f != 1 {
		t.Fatalf("k cuộn ngược: %d", f)
	}
	press(t, m, "pgdown")
	f1, _, _ := pos()
	if f1 <= 2 {
		t.Fatalf("PgDn cuộn cả trang: %d", f1)
	}
	press(t, m, "pgup")
	if f, _, _ := pos(); f != 1 {
		t.Fatalf("PgUp về lại: %d", f)
	}
	// Bánh xe trong hộp cuộn như mũi tên.
	m.Render()
	x, y := cellOf(t, m, "Trợ giúp")
	_, cmd := m.Update(tea.MouseWheelMsg{X: x + 4, Y: y + 3, Button: tea.MouseWheelDown})
	drive(t, m, cmd)
	if f, _, _ := pos(); f <= 1 {
		t.Fatalf("bánh xe trong hộp phải cuộn: %d", f)
	}
	// Tới cuối: dòng cuối của hộp (hàng cuối của nhóm cuối) hiện ra và vị trí báo đã tới đáy.
	groups := m.helpGroups()
	last := groups[len(groups)-1]
	wantLast := last.Rows[len(last.Rows)-1].Title
	press(t, m, "end")
	if _, to, total := pos(); to != total {
		t.Fatalf("End phải tới đáy: %d/%d", to, total)
	}
	if out := flat(m.Render()); !strings.Contains(out, wantLast[:min(len(wantLast), 20)]) {
		t.Fatalf("cuộn tới cuối phải thấy dòng cuối %q:\n%s", wantLast, plain(m.Render()))
	}
	press(t, m, "?")
	if m.showHelp {
		t.Fatal("? đóng hộp trợ giúp")
	}
}
