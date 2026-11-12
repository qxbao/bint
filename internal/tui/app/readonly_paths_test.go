package app

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/tui/actions"
	"bint/internal/tui/jobs"
	"bint/internal/tui/theme"
)

// countingRegistry thay Do, Run và Job của mọi hành động bằng hàm giả đếm số lần chạy theo ID (trả ngay, không chặn).
func countingRegistry() (*actions.Registry, func(string) int, func()) {
	var mu sync.Mutex
	calls := map[string]int{}
	bump := func(id string) { mu.Lock(); calls[id]++; mu.Unlock() }
	var list []actions.Action
	for _, a := range actions.Default().All() {
		id := a.ID
		if a.Do != nil {
			a.Do = func(_ context.Context, _ actions.Env, o any) tea.Msg {
				bump(id)
				return actions.ResultMsg{ID: id, Opts: o}
			}
		}
		if a.Run != nil {
			a.Run = func(context.Context, actions.Env) tea.Msg { bump(id); return actions.ResultMsg{ID: id} }
		}
		if a.Job != nil {
			a.Job = func(context.Context, actions.Env, any, *jobs.Bridge) (any, error) { bump(id); return nil, nil }
		}
		list = append(list, a)
	}
	get := func(id string) int { mu.Lock(); defer mu.Unlock(); return calls[id] }
	reset := func() { mu.Lock(); clear(calls); mu.Unlock() }
	return actions.NewRegistry(list), get, reset
}

// subStates liệt kê các phím đưa mô hình tới từng (màn, thẻ phụ) để quét chân phím và vùng bấm.
func subStates() [][]string {
	var out [][]string
	for _, s := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		out = append(out, []string{s})
		if s == "3" { // thẻ phụ của Tác vụ nằm ở khung chi tiết: Enter vào khung rồi "." đổi thẻ
			for n := 0; n <= 8; n++ {
				out = append(out, append([]string{s, "enter"}, strings.Split(strings.Repeat(".", n), "")...))
			}
		}
		for n := 1; n <= 10; n++ {
			keys := []string{s}
			for i := 0; i < n; i++ {
				keys = append(keys, ".")
			}
			out = append(out, keys)
		}
	}
	return out
}

// guard chạy f và báo lỗi nếu quá hạn (việc nền giả trả ngay nên không được treo).
func guard(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s: quá hạn", what)
	}
}

// Chế độ chỉ đọc (mục 7.4): mọi hành động Writes — dù khai báo bằng Do, Run hay Job — không chạy được qua phím tắt, bảng lệnh, bấm chuột
// vào phím ở chân phím và RequestMsg; bảng lệnh không liệt kê chúng.
func TestReadOnlyNeverRunsWritesAnyPath(t *testing.T) {
	reg, called, reset := countingRegistry()
	var writes []actions.Action
	kinds := map[string]int{}
	for _, a := range reg.All() {
		if !a.Writes {
			continue
		}
		writes = append(writes, a)
		switch {
		case a.Job != nil:
			kinds["job"]++
		case a.Run != nil:
			kinds["run"]++
		case a.Do != nil:
			kinds["do"]++
		}
	}
	if len(writes) < 40 || kinds["job"] == 0 || kinds["do"] == 0 {
		t.Fatalf("quét thiếu hành động ghi: %d %v", len(writes), kinds)
	}
	newRO := func() *Model {
		return newModel(t, Options{Mode: theme.Mono, ReadOnly: true, Registry: reg}, sampleData(), 140, 40)
	}
	assertNone := func(path string) {
		t.Helper()
		for _, a := range writes {
			if n := called(a.ID); n != 0 {
				t.Errorf("%s: hành động ghi %s đã chạy %d lần ở chế độ chỉ đọc", path, a.ID, n)
				reset()
			}
		}
	}
	// 1. RequestMsg và chọn từ bảng lệnh (run).
	m := newRO()
	for _, a := range writes {
		guard(t, "RequestMsg "+a.ID, func() {
			_, cmd := m.Update(actions.RequestMsg{ID: a.ID, Opts: a.Options})
			drive(t, m, cmd)
		})
		if m.jobs.Len() != 0 {
			t.Errorf("RequestMsg %s bắt đầu việc nền ở chế độ chỉ đọc", a.ID)
		}
		guard(t, "run "+a.ID, func() {
			_, cmd := m.run(a.ID)
			drive(t, m, cmd)
		})
	}
	assertNone("RequestMsg/bảng lệnh")
	inPalette := map[string]bool{}
	for _, it := range m.paletteItems() {
		inPalette[it.ID] = true
	}
	for _, a := range writes {
		if inPalette[a.ID] {
			t.Errorf("%s (Writes) có trong bảng lệnh ở chế độ chỉ đọc", a.ID)
		}
	}
	// 2. Phím tắt của mọi hành động ghi, ở mọi (màn, thẻ phụ).
	for _, st := range subStates() {
		m := newRO()
		guard(t, "keys", func() {
			press(t, m, st...)
			for _, a := range writes {
				if a.Screen != actions.Global && a.Screen != actions.Screens[m.Tab()] {
					continue
				}
				k := strings.ToLower(a.Key)
				if a.Key == "" || strings.ContainsAny(a.Key, "+/") || (len(a.Key) > 1 && k != "enter" && k != "delete") {
					continue
				}
				key := a.Key
				if k == "enter" {
					key = "enter"
				}
				if k == "delete" {
					key = "delete"
				}
				press(t, m, key)
				press(t, m, "esc") // chỉ đọc không mở được hộp/form; esc để chắc không lan sang phím sau
			}
		})
		assertNone("phím tắt " + strings.Join(st, ","))
	}
	// 3. Bấm chuột vào mọi phím hiện ra ở chân phím (ngoài và trong khung), ở mọi (màn, thẻ phụ).
	for _, st := range subStates() {
		m := newRO()
		press(t, m, st...)
		m.Render()
		var ids []string
		seen := map[string]bool{}
		for _, z := range m.zones {
			if strings.HasPrefix(z.ID, "key:") && !seen[z.ID] && z.ID != "key:!" && z.ID != "key:q" {
				seen[z.ID] = true
				ids = append(ids, z.ID)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			guard(t, "click "+id, func() {
				m := newRO()
				press(t, m, st...)
				m.Render()
				for _, z := range m.zones {
					if z.ID == id {
						_, cmd := m.Update(tea.MouseClickMsg{X: z.X0, Y: z.Y, Button: tea.MouseLeft})
						drive(t, m, cmd)
						break
					}
				}
			})
		}
		assertNone("chuột " + strings.Join(st, ","))
	}
}

// Chân phím trong khung của mọi màn và thẻ phụ (sinh từ subStates, không bảng tay): ở chỉ đọc không còn phím của hành động Writes (tra sổ qua
// ui.Env.Allowed), phím không ghi vẫn còn. Phím ghi được xác định theo ActionID của hint ở mô hình thường (không theo chữ cái của phím).
func TestInnerFooterHidesWritesInReadOnly(t *testing.T) {
	writesChecked, keptChecked, statesWithWrites := 0, 0, 0
	for _, st := range subStates() {
		normal := newModel(t, Options{Mode: theme.Mono}, sampleData(), 200, 40)
		ro := newModel(t, Options{Mode: theme.Mono, ReadOnly: true}, sampleData(), 200, 40)
		press(t, normal, st...)
		press(t, ro, st...)
		writes := writeHintPairs(normal, collectHints(normal))
		nd, rd := innerFooterPairs(normal.Render()), innerFooterPairs(ro.Render())
		// Phím ghi hiện ở mô hình thường thì phải biến mất ở chỉ đọc.
		for _, p := range writes {
			if len(missingPairs(nd, []fkv{p})) != 0 {
				continue // phím bị bỏ do chân phím hẹp: không có gì để so
			}
			writesChecked++
			if l := leakedPairs(rd, []fkv{p}); len(l) != 0 {
				t.Errorf("%v: chế độ chỉ đọc còn phím ghi %q ở chân phím trong khung", st, p.String())
			}
		}
		if len(writes) > 0 {
			statesWithWrites++
		}
		// Phím không ghi (ActionID rỗng hoặc hành động đọc) hiện ở mô hình thường thì còn ở chỉ đọc.
		isWrite := map[fkv]bool{}
		for _, p := range writes {
			isWrite[p] = true
		}
		for _, p := range nd {
			if isWrite[p] {
				continue
			}
			keptChecked++
			if len(missingPairs(rd, []fkv{p})) != 0 {
				t.Errorf("%v: chế độ chỉ đọc mất phím đọc %q khỏi chân phím trong khung (có %v)", st, p.String(), rd)
			}
		}
	}
	if writesChecked < 30 || keptChecked < 60 || statesWithWrites < 15 {
		t.Fatalf("khẳng định quá ít để có nghĩa: %d phím ghi ở %d trạng thái, %d phím đọc", writesChecked, statesWithWrites, keptChecked)
	}
}

// Ca ngược: nếu chỉ đọc để lọt một phím ghi, hoặc làm mất một phím đọc, các hàm so sánh dùng ở trên phải báo.
func TestInnerFooterAssertionCatchesLeakAndLoss(t *testing.T) {
	normal := []fkv{{"a", "duyệt"}, {"A", "gồm đã loại"}}
	writes := []fkv{{"a", "duyệt"}}
	if l := leakedPairs(normal, writes); len(l) != 1 {
		t.Fatalf("phím ghi lọt ở chỉ đọc phải bị bắt: %v", l)
	}
	if l := leakedPairs([]fkv{{"A", "gồm đã loại"}}, writes); len(l) != 0 {
		t.Fatalf("chỉ đọc đúng mà báo lọt: %v", l)
	}
	if m := missingPairs([]fkv{{"a", "duyệt"}}, normal); len(m) != 1 || m[0].Key != "A" {
		t.Fatalf("mất phím đọc phải bị bắt: %v", m)
	}
}

// Chân phím ngoài của mọi màn và thẻ phụ ở chỉ đọc không còn nhãn của hành động Writes (hành động Writes không bao giờ nằm trong Hints).
func TestOuterFooterHidesWritesInReadOnly(t *testing.T) {
	m := newModel(t, Options{Mode: theme.Mono, Lang: "vi", ReadOnly: true}, sampleData(), 160, 40)
	writeLabel := map[string]string{}
	for _, a := range actions.Default().All() {
		if a.Writes && a.Footer && a.LabelKey != "" {
			writeLabel[m.cat.T(a.LabelKey)] = a.ID
		}
	}
	if len(writeLabel) < 10 {
		t.Fatalf("quét thiếu nhãn hành động ghi: %v", writeLabel)
	}
	for _, st := range subStates() {
		m := newModel(t, Options{Mode: theme.Mono, Lang: "vi", ReadOnly: true}, sampleData(), 160, 40)
		press(t, m, st...)
		sc := m.Screen()
		for _, h := range m.Hints() {
			if id, ok := writeLabel[h.Label]; ok {
				if a, _ := actions.Default().ByID(id); a.Screen == sc || a.Screen == actions.Global {
					t.Errorf("%v: chân phím ngoài ở chỉ đọc còn %q của hành động ghi %s", st, h.Label, id)
				}
			}
		}
	}
}
