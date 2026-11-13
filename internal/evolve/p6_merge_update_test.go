package evolve

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/knowledge"
	"bint/internal/testutil"
)

// ---- NT-P6-04: update hợp nhất Draft.Inputs vào Origin.Inputs ----

func TestUpdateMergesDraftInputsAndThirdParty(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	base := f.item(t, "rule", "stale", "Use context for database operations", []string{"internal/db/**"}, "internal/db/db.go:3")
	base.Origin.Inputs = []string{"internal/db/db.go"}
	if err := f.k.Store.Save(base); err != nil {
		t.Fatal(err)
	}
	f.run(t, "R1", time.Hour, "good", []string{"internal/db/db.go"}, nil, []string{base.ID})
	res, h := f.scan(t)
	op, ok := byKey(res)["stale:"+base.ID]
	if !ok {
		t.Fatal("không có cơ hội stale")
	}
	const ext = "https://example.invalid/external-guidance"
	answer := `{"proposals":[{"type":"rule","title":"Use context for database operations","body":"Use cancellation context for database calls.","paths":["internal/db/**"],"evidence":["internal/db/db.go:3#Open"],"inputs":["` + ext + `"],"expected_benefit":"keep guidance current","reason":"source review"}]}`
	rep, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Agent: &goAgent{answer: answer}, Now: now})
	if err != nil || len(rep.Outcomes) != 1 || rep.Outcomes[0].Proposal == nil {
		t.Fatalf("đề xuất: %+v %v", rep, err)
	}
	p := rep.Outcomes[0].Proposal
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, ConfirmRisk: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	updated, err := knowledge.ReadFile(f.k.Store.PathFor(base.ID))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"internal/db/db.go", ext}; !slices.Equal(updated.Origin.Inputs, want) {
		t.Fatalf("Origin.Inputs = %v, cần %v (cũ rồi mới)", updated.Origin.Inputs, want)
	}
	if !updated.Origin.ThirdParty {
		t.Fatal("input ngoài của bản sửa phải đặt third_party=true")
	}
}

func TestDraftItemBaseInputs(t *testing.T) {
	f := setup(t)
	src, fp := f.k.Source(), f.k.Config.Context.Freshness.Fingerprint
	mk := func(inputs []string, third bool) *knowledge.Item {
		it := f.item(t, "rule", "verified", "Use context", []string{"internal/db/**"}, "internal/db/db.go:3")
		it.Origin.Inputs, it.Origin.ThirdParty = inputs, third
		return it
	}
	cases := []struct {
		name       string
		base       []string
		baseThird  bool
		draft      []string
		wantInputs []string
		wantThird  bool
	}{
		{"input ngoài mới bật cờ", []string{"internal/db/db.go"}, false, []string{"https://x.invalid/a"}, []string{"internal/db/db.go", "https://x.invalid/a"}, true},
		{"base đã gắn cờ, draft không có input ngoài: giữ cờ", []string{"https://x.invalid/a"}, true, []string{"internal/db/db.go"}, []string{"https://x.invalid/a", "internal/db/db.go"}, true},
		{"input trùng không nhân đôi", []string{"internal/db/db.go"}, false, []string{"internal/db/db.go"}, []string{"internal/db/db.go"}, false},
		{"draft không có input: giữ nguyên của base", []string{"internal/db/db.go"}, false, nil, []string{"internal/db/db.go"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := mk(slices.Clone(tc.base), tc.baseThird)
			d := draftFrom(base)
			d.Inputs = tc.draft
			got, err := DraftItem(d, base.ID, base, src, fp, "ep_x", now)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Origin.Inputs, tc.wantInputs) {
				t.Fatalf("Inputs = %v, cần %v", got.Origin.Inputs, tc.wantInputs)
			}
			if got.Origin.ThirdParty != tc.wantThird {
				t.Fatalf("ThirdParty = %v, cần %v", got.Origin.ThirdParty, tc.wantThird)
			}
			if !slices.Equal(base.Origin.Inputs, tc.base) {
				t.Fatalf("DraftItem không được sửa base: %v", base.Origin.Inputs)
			}
		})
	}
}

// ---- NT-P6-05: merge nối phần body chỉ có ở mục bị gộp ----

func TestMergeBodiesTable(t *testing.T) {
	cases := []struct {
		name, keep, drop string
		want             string // "" = không đổi (trả keep)
	}{
		{"trùng hoàn toàn", "Open requires context.", "Open requires context.", ""},
		{"trùng khác hoa thường và khoảng trắng", "Open requires context.", "  open   REQUIRES\n context. ", ""},
		{"drop là một phần của keep", "Open requires context. Close after workers stop.", "Close after workers stop.", ""},
		{"câu mới", "Open requires context.", "Close only after workers stop.",
			"Open requires context.\n\nGộp từ ID2:\n\nClose only after workers stop."},
		{"đoạn thiếu một phần", "Open requires context.", "Open requires context. Close only after workers stop. Retry is safe.",
			"Open requires context.\n\nGộp từ ID2:\n\nClose only after workers stop. Retry is safe."},
		{"nhiều đoạn, giữ thứ tự và nguyên đoạn thiếu hết", "A one.", "B two. B three.\n\nA one.\n\nC four.",
			"A one.\n\nGộp từ ID2:\n\nB two. B three.\n\nC four."},
		{"mục danh sách", "- alpha\n- beta", "- beta\n- gamma\n  tiếp theo",
			"- alpha\n- beta\n\nGộp từ ID2:\n\n- gamma\n  tiếp theo"},
		{"khối mã đã có", "Dùng:\n```go\nx := 1\n```", "```go\nx := 1\n```", ""},
		{"khối mã mới giữ nguyên khối, kể cả dòng trống", "Dùng Open.", "```go\na := 1\n\nb := 2\n```",
			"Dùng Open.\n\nGộp từ ID2:\n\n```go\na := 1\n\nb := 2\n```"},
		// Ranh giới đơn vị (review phiếu 2 .1): chuỗi con không phải là "đã có".
		{"câu ngược nghĩa không bị coi là đã có", "Do not retry on timeout.", "Retry on timeout.",
			"Do not retry on timeout.\n\nGộp từ ID2:\n\nRetry on timeout."},
		{"mục danh sách là tiền tố của mục khác", "- cache-busting", "- cache",
			"- cache-busting\n\nGộp từ ID2:\n\n- cache"},
		{"keep rỗng", "", "Chỉ có ở drop.", "Gộp từ ID2:\n\nChỉ có ở drop."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeBodies(tc.keep, tc.drop, "ID2")
			want := tc.want
			if want == "" {
				want = tc.keep
			}
			if got != want {
				t.Fatalf("mergeBodies:\n got %q\nwant %q", got, want)
			}
			if again := mergeBodies(got, tc.drop, "ID2"); again != got {
				t.Fatalf("gọi lại không được nối thêm:\n got %q\nwant %q", again, got)
			}
		})
	}
}

// mergeFixture tạo hai fact cùng tiêu đề/phạm vi với body cho trước; trả hai mục (a tạo trước) và đề xuất merge.
func mergeFixture(t *testing.T, bodyA, bodyB string, evA, evB string, tweak func(a, b *knowledge.Item)) (*fixture, *knowledge.Item, *knowledge.Item, *Proposal) {
	t.Helper()
	f := setup(t)
	a := f.item(t, "fact", "verified", "Database lifecycle requirements", []string{"internal/db/**"}, evA)
	b := f.item(t, "fact", "verified", "Database lifecycle requirements", []string{"internal/db/**"}, evB)
	a.Body, b.Body = bodyA, bodyB
	if tweak != nil {
		tweak(a, b)
	}
	for _, it := range []*knowledge.Item{a, b} {
		if err := f.k.Store.Save(it); err != nil {
			t.Fatal(err)
		}
	}
	res, h := f.scan(t)
	op, ok := byKey(res)["duplicate:"+a.ID+"+"+b.ID]
	if !ok {
		t.Fatal("không có cơ hội duplicate")
	}
	rep, err := Propose(context.Background(), ProposeInput{K: f.k, DB: f.db, Scan: res, History: h, Refs: []string{op.ID}, Now: now})
	if err != nil || len(rep.Outcomes) != 1 || rep.Outcomes[0].Proposal == nil {
		t.Fatalf("đề xuất: %+v %v", rep, err)
	}
	return f, a, b, rep.Outcomes[0].Proposal
}

// applyMerge áp dụng đề xuất và trả mục active (duy nhất không deprecated) cùng mục bị gộp.
func applyMerge(t *testing.T, f *fixture, p *Proposal) (active, dropped *knowledge.Item) {
	t.Helper()
	if _, err := Apply(context.Background(), ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, ConfirmRisk: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	items, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Status == knowledge.StatusDeprecated {
			dropped = it
		} else if it.Type == "fact" {
			if active != nil {
				t.Fatalf("hai mục active: %s và %s", active.ID, it.ID)
			}
			active = it
		}
	}
	if active == nil || dropped == nil {
		t.Fatalf("cần đúng một mục active và một mục bị gộp: %v %v", active, dropped)
	}
	return active, dropped
}

func TestMergeKeepsDistinctFacts(t *testing.T) {
	const bodyA, bodyB = "Open requires cancellation context propagation.", "Close only after workers have stopped."
	f, a, b, p := mergeFixture(t, bodyA, bodyB, "internal/db/db.go:3", "internal/db/db.go:5", nil)
	pathA, pathB := f.k.Store.PathFor(a.ID), f.k.Store.PathFor(b.ID)
	beforeA, _ := os.ReadFile(pathA)
	beforeB, _ := os.ReadFile(pathB)

	active, dropped := applyMerge(t, f, p)
	if !strings.Contains(active.Body, bodyA) || !strings.Contains(active.Body, bodyB) {
		t.Fatalf("mục active phải chứa cả hai ý, được %q", active.Body)
	}
	if !strings.Contains(active.Body, "Gộp từ "+dropped.ID) {
		t.Fatalf("body phải nêu ID mục bị gộp %s: %q", dropped.ID, active.Body)
	}
	if dropped.Status != knowledge.StatusDeprecated {
		t.Fatalf("mục bị gộp phải deprecated: %s", dropped.Status)
	}

	// Rollback trả đúng cả hai tệp.
	if _, err := Rollback(context.Background(), RollbackOptions{K: f.k, DB: f.db, Ref: p.ID, Now: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{pathA: beforeA, pathB: beforeB} {
		if got, _ := os.ReadFile(path); string(got) != string(want) {
			t.Fatalf("%s không trở về nội dung trước apply:\n%s", path, got)
		}
	}
}

func TestMergeIdenticalBodiesNotDuplicated(t *testing.T) {
	const body = "Open requires cancellation context propagation."
	t.Run("khác bằng chứng: body không nhân đôi", func(t *testing.T) {
		// b khác hoa thường/khoảng trắng nhưng cùng ý.
		f, _, _, p := mergeFixture(t, body, "  open requires   CANCELLATION context propagation. ", "internal/db/db.go:3", "internal/db/db.go:5", nil)
		active, _ := applyMerge(t, f, p)
		if n := strings.Count(strings.ToLower(active.Body), "cancellation context propagation"); n != 1 {
			t.Fatalf("ý xuất hiện %d lần, cần 1: %q", n, active.Body)
		}
		if strings.Contains(active.Body, "Gộp từ") {
			t.Fatalf("body trùng không được thêm khối gộp: %q", active.Body)
		}
	})
	t.Run("trùng cả phạm vi và bằng chứng: chỉ deprecate", func(t *testing.T) {
		_, _, _, p := mergeFixture(t, body, body, "internal/db/db.go:3", "internal/db/db.go:3", nil)
		if len(p.Changes) != 1 || p.Changes[0].Op != ActionDeprecate {
			t.Fatalf("cần đúng một thay đổi deprecate, được %+v", p.Changes)
		}
	})
}

func TestMergeContradictingFactsSurfaced(t *testing.T) {
	const bodyA, bodyB = "Open must be called with a context.", "Open must not be called with a context."
	f, _, _, p := mergeFixture(t, bodyA, bodyB, "internal/db/db.go:3", "internal/db/db.go:5", nil)
	var noted bool
	for _, n := range p.Notes {
		if strings.Contains(n, "mâu thuẫn") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("đề xuất phải nhắc người duyệt kiểm tra mâu thuẫn: %v", p.Notes)
	}
	active, dropped := applyMerge(t, f, p)
	if !strings.Contains(active.Body, bodyA) || !strings.Contains(active.Body, bodyB) {
		t.Fatalf("cả hai câu trái nhau phải còn: %q", active.Body)
	}
	head := strings.Index(active.Body, "Gộp từ "+dropped.ID)
	droppedSentence := bodyB
	if strings.HasPrefix(active.Body, bodyB) {
		droppedSentence = bodyA
	}
	if head < 0 || strings.Index(active.Body, droppedSentence) < head {
		t.Fatalf("câu của mục bị gộp phải nằm sau dòng \"Gộp từ %s\": %q", dropped.ID, active.Body)
	}
}

func TestMergeCarriesThirdParty(t *testing.T) {
	// a (ID nhỏ hơn) được giữ; b bị gộp và mang cờ bên thứ ba.
	f, _, _, p := mergeFixture(t, "Open requires context.", "Close after workers stop.", "internal/db/db.go:3", "internal/db/db.go:5",
		func(a, b *knowledge.Item) { b.Origin.ThirdParty = true })
	active, dropped := applyMerge(t, f, p)
	if !dropped.Origin.ThirdParty || active.ID >= dropped.ID {
		t.Fatalf("giả định của test sai: active=%s dropped=%s third=%v", active.ID, dropped.ID, dropped.Origin.ThirdParty)
	}
	if !active.Origin.ThirdParty {
		t.Fatal("nội dung của mục bên thứ ba đã nối vào mục giữ lại: cờ third_party phải theo sang")
	}
}

// Evaluate dựng mục giữ lại bằng cùng đường với Apply: cờ third_party của mục bị gộp phải theo sang
// (review phiếu 2 .2); đề xuất không phải merge thì không đổi cờ.
func TestEvaluateCandidateCarriesThirdParty(t *testing.T) {
	f, _, _, p := mergeFixture(t, "Open requires context.", "Close after workers stop.", "internal/db/db.go:3", "internal/db/db.go:5",
		func(a, b *knowledge.Item) { b.Origin.ThirdParty = true })
	items, _, err := f.k.Items()
	if err != nil {
		t.Fatal(err)
	}
	var keepID string
	for _, ch := range p.Changes {
		if ch.Op == ActionUpdate {
			keepID = ch.ItemID
		}
	}
	if keepID == "" {
		t.Fatalf("đề xuất merge phải có một thay đổi update: %+v", p.Changes)
	}
	base := strings.TrimSpace(testutil.Git(t, f.root, "rev-parse", "HEAD"))
	c := &eval.Case{ID: "c1", Source: eval.Source{Commit: base, Base: base}}
	got, err := candidateSet(context.Background(), p, items, c, nil, f.root, f.k.Source(), f.k.Config.Context.Freshness.Fingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	var kept *knowledge.Item
	for _, it := range got {
		if it.ID == keepID {
			kept = it
		}
	}
	if kept == nil {
		t.Fatalf("không thấy mục giữ lại %s trong tập ứng viên", keepID)
	}
	if !kept.Origin.ThirdParty {
		t.Fatal("bản đánh giá phải mang third_party như bản được apply")
	}
	other := *p
	other.Action = ActionUpdate
	got, err = candidateSet(context.Background(), &other, items, c, nil, f.root, f.k.Source(), f.k.Config.Context.Freshness.Fingerprint, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range got {
		if it.ID == keepID && it.Origin.ThirdParty {
			t.Fatal("đề xuất không phải merge không được chuyển cờ third_party")
		}
	}
}
