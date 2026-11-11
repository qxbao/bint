package actions

import (
	"slices"
	"strings"
	"testing"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
)

func TestFlagsOfReadsTagsIncludingEmbedded(t *testing.T) {
	got := FlagsOf(app.EvalRunOptions{})
	for _, want := range []string{"budget", "arms", "repeats", "split", "case", "limit", "resume", "yes", "leakage-mode", "bare"} {
		if !slices.Contains(got, want) {
			t.Errorf("thiếu cờ %q trong %v", want, got)
		}
	}
	if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("cờ phải sắp xếp và không trùng: %v", got)
	}
	// Con trỏ cũng được; nil thì không có cờ.
	if got := FlagsOf(&evolvesvc.ApplyOptions{}); !slices.Equal(got, []string{"confirm-risk", "reason", "unverified"}) {
		t.Errorf("%v", got)
	}
	if FlagsOf(nil) != nil {
		t.Error("nil phải không có cờ")
	}
}

func TestCheckByMode(t *testing.T) {
	read := Action{ID: "a"}
	write := Action{ID: "b", Writes: true}
	human := Action{ID: "c", HumanOnly: true}
	both := Action{ID: "d", Writes: true, HumanOnly: true}
	cases := []struct {
		a    Action
		m    Mode
		want Availability
	}{
		{read, Mode{}, Available}, {read, Mode{ReadOnly: true}, Available}, {read, Mode{Agent: true}, Available},
		{write, Mode{}, Available}, {write, Mode{ReadOnly: true}, Hidden}, {write, Mode{Agent: true}, Available},
		{human, Mode{}, Available}, {human, Mode{Agent: true}, Locked}, {human, Mode{ReadOnly: true}, Available},
		{both, Mode{ReadOnly: true, Agent: true}, Hidden}, // chỉ đọc thắng: ẩn hẳn
		{both, Mode{Agent: true}, Locked},
	}
	for i, tc := range cases {
		if got := tc.a.Check(tc.m); got != tc.want {
			t.Errorf("ca %d (%s, %+v): %v, muốn %v", i, tc.a.ID, tc.m, got, tc.want)
		}
	}
}

func TestRegistryFilters(t *testing.T) {
	r := NewRegistry([]Action{
		{ID: "g"}, {ID: "o", Screen: Overview}, {ID: "k", Screen: Knowledge},
		{ID: "w", Screen: Overview, Writes: true}, {ID: "h", Screen: Overview, HumanOnly: true},
	})
	ids := func(as []Action) string {
		var s []string
		for _, a := range as {
			s = append(s, a.ID)
		}
		return strings.Join(s, ",")
	}
	if got := ids(r.For(Overview, Mode{})); got != "g,o,w,h" {
		t.Errorf("For(Overview): %s", got)
	}
	if got := ids(r.For(Overview, Mode{ReadOnly: true})); got != "g,o,h" {
		t.Errorf("chỉ đọc phải ẩn hành động ghi: %s", got)
	}
	if got := ids(r.Visible(Mode{ReadOnly: true})); got != "g,o,k,h" {
		t.Errorf("Visible chỉ đọc: %s", got)
	}
	if got := ids(r.Visible(Mode{Agent: true})); got != "g,o,k,w,h" {
		t.Errorf("hành động khoá vẫn hiện (kèm khoá): %s", got)
	}
	if _, ok := r.ByID("k"); !ok {
		t.Error("ByID")
	}
	if _, ok := r.ByID("zz"); ok {
		t.Error("ByID lạ")
	}
	defer func() {
		if recover() == nil {
			t.Error("ID trùng phải panic")
		}
	}()
	NewRegistry([]Action{{ID: "x"}, {ID: "x"}})
}

// Từ khoá của hành động gồm đường lệnh CLI và cờ (để tìm theo --cờ).
func TestKeywordsIncludeCLIPathAndFlags(t *testing.T) {
	a := Action{ID: "x", CLIPath: "eval run", Options: app.EvalRunOptions{}}
	kw := a.Keywords()
	if !strings.Contains(kw, "bint eval run") || !strings.Contains(kw, "--budget") || !strings.Contains(kw, "--leakage-mode") {
		t.Fatalf("%s", kw)
	}
	if (Action{ID: "y"}).Keywords() != "" {
		t.Fatal("hành động giao diện thuần không có từ khoá CLI")
	}
}

// Sổ mặc định: ID duy nhất, đủ bảy tab điều hướng, phím ? và q ghim, hành động ghi đánh dấu Writes.
func TestDefaultRegistryShape(t *testing.T) {
	r := Default()
	for _, s := range Screens {
		a, ok := r.ByID(NavID(s))
		if !ok || a.Key == "" {
			t.Errorf("thiếu điều hướng %s", s)
		}
	}
	for _, id := range []string{"app.quit", "app.help"} {
		if a, _ := r.ByID(id); !a.Pinned || !a.Footer {
			t.Errorf("%s phải được ghim ở chân phím", id)
		}
	}
	if a, _ := r.ByID("init.run"); !a.Writes || a.CLIPath != "init" || a.Do == nil || a.Options == nil {
		t.Errorf("init.run phải là hành động ghi có hàm dịch vụ và Options: %+v", a)
	}
	keys := map[string]string{}
	for _, a := range r.All() {
		if a.Screen == Global && a.Key != "" {
			if other, dup := keys[a.Key]; dup {
				t.Errorf("phím toàn cục %q trùng giữa %s và %s", a.Key, other, a.ID)
			}
			keys[a.Key] = a.ID
		}
	}
}
