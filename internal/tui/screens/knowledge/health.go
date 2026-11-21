package knowledge

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	kn "bint/internal/knowledge"
	"bint/internal/tui/actions"
	"bint/internal/tui/theme"
)

// Checker kiểm tra xác định được các mục (như `bint knowledge verify`, không áp chuyển trạng thái); thay được
// trong kiểm thử. nil: dùng Verifier trên không gian đã nạp (Data.WS).
type Checker func(ctx context.Context, d Data, items []*kn.Item) map[string]kn.Result

// HealthMsg là kết quả kiểm nền: sức khoẻ theo ID mục kèm khoá đệm lúc kiểm.
type HealthMsg struct {
	Entries map[string]healthEntry
	gen     int
}

// Target cho mô hình gốc biết thông điệp thuộc màn Tri thức.
func (HealthMsg) Target() actions.Screen { return actions.Knowledge }

type healthEntry struct {
	Key    string
	Health kn.Health
}

func defaultCheck(ctx context.Context, d Data, items []*kn.Item) map[string]kn.Result {
	out := map[string]kn.Result{}
	if d.WS == nil {
		return out
	}
	v := kn.Verifier{Src: d.WS.Source(), All: d.Items, FindMoved: true}
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		out[it.ID] = v.Verify(it)
	}
	return out
}

// checkable: có cách kiểm các mục của dữ liệu hiện tại.
func (m *Model) checkable() bool { return m.o.Checker != nil || m.Data.WS != nil }

// healthKey là khoá đệm: ID + vị trí/fingerprint từng bằng chứng + kích thước và mtime tệp bằng chứng.
func (m *Model) healthKey(it *kn.Item) string {
	var b strings.Builder
	b.WriteString(it.ID)
	for _, e := range it.Evidence {
		fmt.Fprintf(&b, "|%s:%s:%s:%s", e.Path, e.Lines, e.Symbol, e.Fingerprint)
		if m.Data.WS != nil && e.Path != "" {
			if st, err := os.Stat(filepath.Join(m.Data.WS.Root, filepath.FromSlash(e.Path))); err == nil {
				fmt.Fprintf(&b, ":%d:%d", st.Size(), st.ModTime().UnixNano())
			} else {
				b.WriteString(":-")
			}
		}
	}
	return b.String()
}

// startCheck chạy nền kiểm các mục còn hiệu lực có khoá đệm đổi; mỗi lúc một lượt.
func (m *Model) startCheck() tea.Cmd {
	if !m.checkable() {
		return nil
	}
	if m.checking {
		m.recheck = true
		return nil
	}
	var todo []*kn.Item
	keys := map[string]string{}
	for _, it := range m.Data.Items {
		if !it.Active() {
			continue
		}
		k := m.healthKey(it)
		if e, ok := m.health[it.ID]; ok && e.Key == k && !m.forceCheck {
			continue
		}
		todo = append(todo, it)
		keys[it.ID] = k
	}
	if len(todo) == 0 {
		return nil
	}
	m.checking, m.forceCheck = true, false
	m.checkGen++
	parent := m.o.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m.checkCancel = cancel
	chk, d, gen := m.o.Checker, m.Data, m.checkGen
	if chk == nil {
		chk = defaultCheck
	}
	return func() tea.Msg {
		defer cancel()
		res := chk(ctx, d, todo)
		out := make(map[string]healthEntry, len(res))
		for id, r := range res {
			out[id] = healthEntry{Key: keys[id], Health: kn.HealthOf(r)}
		}
		return HealthMsg{Entries: out, gen: gen}
	}
}

// cancelCheck huỷ lượt kiểm đang bay (nạp lại tay, thoát): kết quả của nó bị bỏ qua khi về.
func (m *Model) cancelCheck() {
	if m.checkCancel != nil {
		m.checkCancel()
		m.checkCancel = nil
	}
	if m.checking {
		m.checking, m.recheck = false, false
		m.checkGen++
	}
}

func (m *Model) onHealth(msg HealthMsg) tea.Cmd {
	if msg.gen != 0 && msg.gen != m.checkGen {
		return nil // lượt đã huỷ
	}
	m.checking, m.checkCancel = false, nil
	if m.health == nil {
		m.health = map[string]healthEntry{}
	}
	maps.Copy(m.health, msg.Entries)
	m.refilter()
	if m.recheck {
		m.recheck = false
		return m.startCheck()
	}
	return nil
}

// healthOf trả mức sức khoẻ đã kiểm của mục (ok=false: chưa có kết quả).
func (m *Model) healthOf(it *kn.Item) (kn.Health, bool) {
	e, ok := m.health[it.ID]
	if !ok {
		return kn.HealthOK, false
	}
	return e.Health, true
}

// healthRank: lỗi 0, cảnh báo 1, còn lại 2.
func (m *Model) healthRank(it *kn.Item) int {
	if h, ok := m.healthOf(it); ok {
		switch h {
		case kn.HealthError:
			return 0
		case kn.HealthWarn:
			return 1
		}
	}
	return 2
}

// healthCounts đếm mục lỗi và cảnh báo theo kết quả kiểm hiện có.
func (m *Model) healthCounts() (errs, warns int) {
	for _, it := range m.Data.Items {
		if !it.Active() {
			continue
		}
		switch m.healthRank(it) {
		case 0:
			errs++
		case 1:
			warns++
		}
	}
	return
}

// sortByHealth sắp ổn định m.view: lỗi, cảnh báo, còn lại.
func (m *Model) sortByHealth() {
	if len(m.health) == 0 {
		return
	}
	sort.SliceStable(m.view, func(a, b int) bool {
		return m.healthRank(m.Data.Items[m.view[a]]) < m.healthRank(m.Data.Items[m.view[b]])
	})
}

// healthCell là ô Sức khoẻ của hàng: ✗ lỗi, ! cảnh báo, ✓ ổn, … đang kiểm, trống khi không áp dụng.
func (m *Model) healthCell(th theme.Theme, it *kn.Item) string {
	if !it.Active() {
		return " "
	}
	if h, ok := m.healthOf(it); ok {
		switch h {
		case kn.HealthError:
			return th.S(theme.Err, "✗")
		case kn.HealthWarn:
			return th.S(theme.Warn, "!")
		}
		return th.S(theme.OK, "✓")
	}
	if m.checkable() {
		return th.S(theme.Muted, "…")
	}
	return " "
}
