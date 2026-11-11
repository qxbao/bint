package evolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bint/internal/eval"
	"bint/internal/hcomp"
	"bint/internal/store"
)

func hmeta(id, scope, status string) hcomp.Meta {
	return hcomp.Meta{ID: id, Scope: scope, Status: status, Origin: hcomp.OriginHuman, Version: 1}
}

func stopGuardrail(id, action string) hcomp.Component {
	return hcomp.Component{Kind: hcomp.KindGuardrail, Meta: hmeta(id, hcomp.ScopeGlobal, hcomp.StatusVerified),
		On: hcomp.OnStop, When: hcomp.When{Edited: "internal/**"}, Unless: hcomp.Unless{Ran: hcomp.RanTest}, Action: action, Message: "Chạy test."}
}

func saveComps(t *testing.T, root string, cs ...hcomp.Component) *hcomp.Library {
	t.Helper()
	for _, c := range cs {
		if _, err := hcomp.Save(hcomp.Dir(root), c); err != nil {
			t.Fatal(err)
		}
	}
	return hcomp.Load(root)
}

func newHarnessP(changes ...HarnessChange) *Proposal {
	return &Proposal{Schema: ProposalSchema, ID: "ep_h1", Level: LevelL2, TargetType: TargetHarness, Action: ActionCreate,
		HarnessChanges: changes, Status: StatusProposed}
}

func guardDraft(id, scope, on, action string, w hcomp.When) *HarnessDraft {
	return &HarnessDraft{Kind: hcomp.KindGuardrail, ID: id, Scope: scope, On: on, When: w, Action: action, Message: "Lời nhắc."}
}

// Kiểm tra xác định của đề xuất quy trình (H3): thao tác, nội dung, phạm vi khớp tệp thật, trùng và
// mâu thuẫn với guardrail sẵn có.
func TestValidateHarnessChangesTable(t *testing.T) {
	f := setup(t)
	lib := saveComps(t, f.root,
		stopGuardrail("existing-stop", hcomp.ActRemind),
		hcomp.Component{Kind: hcomp.KindGuidance, Meta: hmeta("tips", hcomp.ScopeGlobal, hcomp.StatusVerified), Body: "Chỉ dẫn."},
		hcomp.Component{Kind: hcomp.KindGuidance, Meta: hmeta("old", hcomp.ScopeGlobal, hcomp.StatusDeprecated), Body: "Cũ."})
	src := f.k.Source()
	create := func(d *HarnessDraft) HarnessChange {
		return HarnessChange{Op: ActionCreate, Kind: d.Kind, ID: d.ID, Draft: d}
	}

	cases := []struct {
		name string
		ch   []HarnessChange
		want string // "" = hợp lệ
	}{
		{"guardrail remind hợp lệ", []HarnessChange{create(guardDraft("g1", "global", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{Paths: "internal/core/**"}))}, ""},
		{"scope domain khớp tệp thật", []HarnessChange{create(guardDraft("g1", "domain:internal/core/**", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{}))}, ""},
		{"scope domain không khớp tệp nào", []HarnessChange{create(guardDraft("g1", "domain:internal/ghost/**", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{}))}, "không khớp tệp nào"},
		{"scope là đường dẫn không có thật", []HarnessChange{create(guardDraft("g1", "domain:internal/ghost", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{}))}, "không khớp tệp nào"},
		{"điều kiện when.paths không khớp tệp", []HarnessChange{create(guardDraft("g1", "global", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{Paths: "nowhere/**"}))}, "không khớp tệp nào"},
		{"regexp lệnh hỏng", []HarnessChange{create(guardDraft("g1", "global", hcomp.OnPreBash, hcomp.ActRemind, hcomp.When{Command: "("}))}, "biểu thức"},
		{"deny pre_bash không có when.command", []HarnessChange{create(guardDraft("g1", "global", hcomp.OnPreBash, hcomp.ActDeny, hcomp.When{}))}, "when.command"},
		{"trùng guardrail sẵn có", []HarnessChange{create(func() *HarnessDraft {
			d := guardDraft("dup", "global", hcomp.OnStop, hcomp.ActRemind, hcomp.When{Edited: "internal/**"})
			d.Unless = hcomp.Unless{Ran: hcomp.RanTest}
			return d
		}())}, "trùng guardrail"},
		{"mâu thuẫn guardrail sẵn có", []HarnessChange{create(func() *HarnessDraft {
			d := guardDraft("conflict", "global", hcomp.OnStop, hcomp.ActDeny, hcomp.When{Edited: "internal/**"})
			d.Unless = hcomp.Unless{Ran: hcomp.RanTest}
			return d
		}())}, "mâu thuẫn"},
		{"create thành phần đã có", []HarnessChange{create(&HarnessDraft{Kind: hcomp.KindGuidance, ID: "tips", Scope: "global", Body: "x"})}, "đã tồn tại"},
		{"create trùng bản dựng sẵn", []HarnessChange{create(&HarnessDraft{Kind: hcomp.KindGuidance, ID: "bint-mcp-tools", Scope: "global", Body: "x"})}, "bản dựng sẵn"},
		{"update không tồn tại", []HarnessChange{{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "nope", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "nope", Scope: "global", Body: "x"}}}, "update cần"},
		{"update thành phần deprecated", []HarnessChange{{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "old", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "old", Scope: "global", Body: "x"}}}, "update cần"},
		{"update hợp lệ", []HarnessChange{{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "tips", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "tips", Scope: "global", Body: "Mới."}}}, ""},
		{"update sai id trong nội dung", []HarnessChange{{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "tips", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "other", Scope: "global", Body: "x"}}}, "khác thành phần"},
		{"deprecate hợp lệ", []HarnessChange{{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"}}, ""},
		{"deprecate không tồn tại", []HarnessChange{{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "ghost"}}, "deprecate cần"},
		{"thao tác lạ", []HarnessChange{{Op: "merge", Kind: hcomp.KindGuidance, ID: "tips"}}, "không hợp lệ"},
		{"script thiếu shebang", []HarnessChange{create(&HarnessDraft{Kind: hcomp.KindScript, ID: "s1", Scope: "global", Description: "d", Script: "echo hi"})}, "shebang"},
		{"script hợp lệ", []HarnessChange{create(&HarnessDraft{Kind: hcomp.KindScript, ID: "s1", Scope: "global", Description: "d", Script: "#!/bin/sh\necho hi\n"})}, ""},
		{"hai lần cùng thành phần", []HarnessChange{{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"}, {Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"}}, "hai lần"},
	}
	for _, c := range cases {
		p := newHarnessP(c.ch...)
		err := validateHarnessChanges(p, lib, src)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: lỗi bất ngờ %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: lỗi = %v, muốn chứa %q", c.name, err, c.want)
		}
	}
	// Ranh giới: phải là L2/harness, không lẫn thay đổi tri thức.
	bad := newHarnessP(HarnessChange{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"})
	bad.Level = LevelL1
	if validateHarnessChanges(bad, lib, src) == nil {
		t.Error("level L1 phải bị từ chối")
	}
	bad = newHarnessP(HarnessChange{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"})
	bad.Changes = []Change{{Op: ActionCreate}}
	if validateHarnessChanges(bad, lib, src) == nil {
		t.Error("lẫn thay đổi tri thức phải bị từ chối")
	}
}

// Rủi ro do Bint tính theo bảng H3, không do agent tự khai.
func TestAssessHarnessRiskTable(t *testing.T) {
	f := setup(t)
	lib := saveComps(t, f.root,
		hcomp.Component{Kind: hcomp.KindGuidance, Meta: hmeta("tips", hcomp.ScopeGlobal, hcomp.StatusVerified), Body: "Chỉ dẫn."},
		hcomp.Component{Kind: hcomp.KindGuidance, Meta: hmeta("draft", hcomp.ScopeGlobal, hcomp.StatusCandidate), Body: "Nháp."})
	create := func(d *HarnessDraft) HarnessChange {
		return HarnessChange{Op: ActionCreate, Kind: d.Kind, ID: d.ID, Draft: d}
	}
	strat := &HarnessDraft{Kind: hcomp.KindStrategy, ID: hcomp.StrategyID, Scope: "global", Context: &hcomp.StrategyContext{MaxTokens: new(int)}}
	cases := []struct {
		name string
		ch   HarnessChange
		want string
	}{
		{"guidance tạo mới", create(&HarnessDraft{Kind: hcomp.KindGuidance, ID: "g", Scope: "global", Body: "x"}), hcomp.RiskLow},
		{"guardrail remind", create(guardDraft("g", "global", hcomp.OnStop, hcomp.ActRemind, hcomp.When{})), hcomp.RiskLow},
		{"guardrail ask", create(guardDraft("g", "global", hcomp.OnPreBash, hcomp.ActAsk, hcomp.When{Command: "rm"})), hcomp.RiskMedium},
		{"guardrail deny", create(guardDraft("g", "global", hcomp.OnPreBash, hcomp.ActDeny, hcomp.When{Command: "rm"})), hcomp.RiskHigh},
		{"script", create(&HarnessDraft{Kind: hcomp.KindScript, ID: "s", Scope: "global", Description: "d", Script: "#!/bin/sh\n"}), hcomp.RiskHigh},
		{"strategy", create(strat), hcomp.RiskMedium},
		{"sửa chỉ dẫn đã duyệt ≥ trung bình", HarnessChange{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "tips", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "tips", Scope: "global", Body: "y"}}, hcomp.RiskMedium},
		{"sửa chỉ dẫn candidate vẫn thấp", HarnessChange{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "draft", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "draft", Scope: "global", Body: "y"}}, hcomp.RiskLow},
		{"loại bỏ chỉ dẫn đã duyệt ≥ trung bình", HarnessChange{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "tips"}, hcomp.RiskMedium},
	}
	for _, c := range cases {
		got, why := assessHarnessRisk(newHarnessP(c.ch), lib)
		if got != c.want || len(why) != 1 {
			t.Errorf("%s: rủi ro %s %v, muốn %s", c.name, got, why, c.want)
		}
	}
	// Nhiều thay đổi: lấy mức cao nhất.
	p := newHarnessP(create(&HarnessDraft{Kind: hcomp.KindGuidance, ID: "g", Scope: "global", Body: "x"}),
		create(guardDraft("g2", "global", hcomp.OnPreBash, hcomp.ActDeny, hcomp.When{Command: "rm"})))
	if got, _ := assessHarnessRisk(p, lib); got != hcomp.RiskHigh {
		t.Errorf("nhiều thay đổi: %s", got)
	}
}

// Chỉ create rủi ro thấp được vòng tiến hoá tự áp dụng (và chỉ vào hàng chờ candidate).
func TestAutoApplicableHarness(t *testing.T) {
	low := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "g", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "g"}})
	low.Risk = RiskLow
	if ok, why := AutoApplicable(low); !ok {
		t.Fatalf("create rủi ro thấp: %s", why)
	}
	med := *low
	med.Risk = RiskMedium
	if ok, _ := AutoApplicable(&med); ok {
		t.Fatal("rủi ro trung bình cần người")
	}
	upd := newHarnessP(HarnessChange{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "g"})
	upd.Risk = RiskLow
	if ok, why := AutoApplicable(upd); ok || !strings.Contains(why, "update") {
		t.Fatalf("update cần người: %v %s", ok, why)
	}
	empty := newHarnessP()
	empty.Risk = RiskLow
	if ok, _ := AutoApplicable(empty); ok {
		t.Fatal("đề xuất rỗng không tự áp dụng")
	}
}

// Cơ hội quy trình sinh từ dấu hiệu hành vi và kênh MCP không được gọi, chỉ trên tập validation.
func TestHarnessOpportunitiesFromScan(t *testing.T) {
	f := setup(t)
	_, scan, _ := seedLessonFailures(t, f)
	got := map[string]Opportunity{}
	for _, o := range scan.Opportunities {
		if o.Kind == KindHarness {
			got[o.Key] = o
		}
	}
	noTest, ok := got[harnessSignalPrefix+SigNoTest]
	if !ok || !noTest.Ready || !reflect.DeepEqual(noTest.SupportingRuns, []string{"ea-v1", "ea-v2", "ea-v3"}) {
		t.Fatalf("cơ hội no_test phải sẵn sàng với ba lượt validation: %+v (tất cả: %v)", noTest, got)
	}
	if !strings.Contains(noTest.Summary, "guardrail") {
		t.Fatalf("gợi ý loại thành phần: %s", noTest.Summary)
	}
	mcp, ok := got[harnessMCPUnusedKey]
	if !ok || !mcp.Ready || !strings.Contains(mcp.Summary, "chỉ dẫn") {
		t.Fatalf("cơ hội mcp_unused (nhánh bint không gọi MCP): %+v", mcp)
	}
	for _, o := range got {
		for _, id := range o.SupportingRuns {
			if id == "ea-t1" {
				t.Fatalf("lượt tập test lọt vào cơ hội quy trình: %+v", o)
			}
		}
	}
	// Dấu hiệu không có trong bảng gợi ý (không có) không sinh cơ hội; ngưỡng min tác vụ áp dụng.
	few := harnessOpps(scan.failures[:1], 3, 1, nil, nil)
	for _, o := range few {
		if o.Ready || !strings.Contains(o.Blocked, "cần ≥ 3") {
			t.Fatalf("một tác vụ không đủ ngưỡng: %+v", o)
		}
	}
	// Đã có đề xuất chờ cùng khoá thì chặn.
	pend := harnessOpps(scan.failures, 3, 1, nil, map[string]string{harnessSignalPrefix + SigNoTest: "ep_x"})
	for _, o := range pend {
		if o.Key == harnessSignalPrefix+SigNoTest && (o.Ready || !strings.Contains(o.Blocked, "ep_x")) {
			t.Fatalf("đề xuất đang chờ phải chặn cơ hội: %+v", o)
		}
	}
	// Lượt thật không bao giờ vào cơ hội quy trình.
	real := scan.failures[0]
	real.Source, real.Split = "run", ""
	for _, o := range harnessOpps([]Failure{real, real, real}, 1, 1, nil, nil) {
		t.Fatalf("lượt thật không được sinh cơ hội quy trình: %+v", o)
	}
}

const harnessReplyTwoKinds = `Đề xuất:
{"proposals":[
 {"op":"create","kind":"guardrail","id":"test-before-stop","reason":"agent kết thúc mà chưa chạy test",
  "draft":{"kind":"guardrail","id":"test-before-stop","scope":"global","on":"stop","when":{"edited":"internal/**"},
           "unless":{"ran":"test"},"action":"remind","message":"Chưa chạy test cho phần đã sửa; chạy test trước khi kết thúc."}},
 {"op":"create","kind":"guidance","id":"core-testing","reason":"vùng core hay quên test",
  "draft":{"kind":"guidance","id":"core-testing","scope":"domain:internal/core/**","title":"Test vùng core",
           "body":"Sau khi sửa internal/core, chạy go test ./internal/core/... trước khi báo xong."}},
 {"op":"create","kind":"guidance","id":"third-extra","draft":{"kind":"guidance","id":"third-extra","scope":"domain:internal/core/**","body":"thừa"}}
]}`

// E2E với agent giả (H3, không tốn token): đầu vào của người đề xuất không có dấu hiệu của tập
// test, nó chạy trong thư mục rỗng, và hai ứng viên (chung, riêng) thành hai đề xuất có Pair trỏ nhau.
func TestHarnessProposalEndToEnd(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, scan, h := seedLessonFailures(t, f)
	var opp Opportunity
	for _, o := range scan.Opportunities {
		if o.Key == harnessSignalPrefix+SigNoTest {
			opp = o
		}
	}
	proposer := &fakeReflector{reply: harnessReplyTwoKinds}
	pr, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: scan, History: h, Refs: []string{opp.ID}, Max: 1,
		HarnessProposer: proposer, Cases: cases, RepoDir: f.root, Timeout: time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if proposer.reflected != 1 || !proposer.dirEmpty[0] {
		t.Fatalf("người đề xuất gọi %d lần, thư mục rỗng=%v", proposer.reflected, proposer.dirEmpty)
	}
	prompt := proposer.prompts[0]
	if strings.Contains(prompt, testSetMarker) || strings.Contains(prompt, "ea-t1") || strings.Contains(prompt, "c-test-1") {
		t.Fatalf("đầu vào người đề xuất lọt dữ liệu tập test:\n%s", prompt)
	}
	for _, want := range []string{"ea-v1", "ea-v3", "internal/core", "internal/api", "không có công cụ"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("đầu vào thiếu %q", want)
		}
	}
	var made []*Proposal
	for _, oc := range pr.Outcomes {
		if oc.Proposal != nil {
			made = append(made, oc.Proposal)
		}
	}
	if len(made) != 2 {
		t.Fatalf("muốn 2 đề xuất (chung + riêng; ứng viên thứ ba bị bỏ): %+v", pr.Outcomes)
	}
	var g, d *Proposal
	for _, p := range made {
		if p.Pair == nil {
			t.Fatalf("thiếu Pair: %+v", p)
		}
		switch p.Pair.Variant {
		case PairGlobal:
			g = p
		case PairDomain:
			d = p
		}
	}
	if g == nil || d == nil || g.Pair.Sibling != d.ID || d.Pair.Sibling != g.ID || g.Pair.Key != opp.Key || d.Pair.Area != "internal/core/**" {
		t.Fatalf("cặp chung/riêng không trỏ nhau: %+v %+v", g.Pair, d.Pair)
	}
	for _, p := range made {
		if p.TargetType != TargetHarness || p.Level != LevelL2 || p.Risk != RiskLow || len(p.HarnessChanges) != 1 ||
			!reflect.DeepEqual(p.EvalCases, []string{"c-val-1", "c-val-2", "c-val-3"}) || p.Plan.Kind != "superiority" || p.Status != StatusProposed {
			t.Fatalf("đề xuất: %+v", p)
		}
		got, err := GetProposal(ctx, f.db, p.ID)
		if err != nil || !got.IsHarness() || got.Pair == nil {
			t.Fatalf("chưa lưu đúng: %v", err)
		}
	}
	if g.HarnessChanges[0].Kind != hcomp.KindGuardrail || d.HarnessChanges[0].Kind != hcomp.KindGuidance {
		t.Fatalf("loại thành phần: %v %v", g.HarnessChanges, d.HarnessChanges)
	}
	// Không có thay đổi nào được ghi vào .bint/harness trước khi người áp dụng.
	if _, err := os.Stat(hcomp.Dir(f.root)); err == nil {
		t.Fatal("đề xuất không được ghi tệp thành phần")
	}
}

// Ứng viên chứa định danh của lời giải bị loại (rò rỉ, fail đóng); nửa còn lại vẫn thành đề xuất, không có Pair.
func TestHarnessProposalLeakAndNoTools(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, scan, h := seedLessonFailures(t, f)
	var opp Opportunity
	for _, o := range scan.Opportunities {
		if o.Key == harnessSignalPrefix+SigNoTest {
			opp = o
		}
	}
	reply := `{"proposals":[
 {"op":"create","kind":"guardrail","id":"g-ok","draft":{"kind":"guardrail","id":"g-ok","scope":"global","on":"stop","when":{"edited":"internal/**"},"unless":{"ran":"test"},"action":"remind","message":"Chạy test."}},
 {"op":"create","kind":"guidance","id":"g-leak","draft":{"kind":"guidance","id":"g-leak","scope":"domain:internal/core/**","body":"Hãy gọi computeQuotaBudget trước khi trả kết quả."}}]}`
	proposer := &fakeReflector{reply: reply}
	pr, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: scan, History: h, Refs: []string{opp.ID}, Max: 1,
		HarnessProposer: proposer, Cases: cases, RepoDir: f.root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var made []*Proposal
	var rejected []string
	for _, oc := range pr.Outcomes {
		rejected = append(rejected, oc.Rejected...)
		if oc.Proposal != nil {
			made = append(made, oc.Proposal)
		}
	}
	if len(made) != 1 || made[0].HarnessChanges[0].ID != "g-ok" || made[0].Pair != nil {
		t.Fatalf("chỉ ứng viên sạch thành đề xuất, không có Pair: %+v", made)
	}
	if !anyContains(rejected, "computeQuotaBudget") {
		t.Fatalf("ứng viên nêu định danh lời giải phải bị loại: %v", rejected)
	}
	// Không có agent đề xuất: bỏ qua có lý do, không lỗi, không gọi mô hình.
	pr, err = Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: scan, History: h, Refs: []string{opp.ID}, Max: 1, Cases: cases, RepoDir: f.root, Now: now})
	if err != nil || len(pr.Outcomes) != 1 || !strings.Contains(pr.Outcomes[0].Skipped, "agent đề xuất quy trình") {
		t.Fatalf("thiếu agent: %+v %v", pr.Outcomes, err)
	}
}

// Áp dụng đề xuất create ghi thành phần ở candidate; quay lui trả lại đúng như cũ. update giữ trạng
// thái verified và ghi người duyệt; quay lui trả tệp y nguyên.
func TestApplyHarnessThenRollback(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	dir := hcomp.Dir(f.root)

	create := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuardrail, ID: "test-before-stop",
		Draft: guardDraft("test-before-stop", "global", hcomp.OnStop, hcomp.ActRemind, hcomp.When{Edited: "internal/**"})})
	create.Base, create.Risk, create.Plan = []BaseVersion{}, RiskLow, defaultPlan(ActionCreate, f.k.Config.Evolution.Evaluation)
	create.CreatedAt, create.UpdatedAt = "2026-11-10T12:00:00.000Z", "2026-11-10T12:00:00.000Z"
	if err := SaveProposal(ctx, f.db, create); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: create.ID, Unverified: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := hcomp.Load(f.root).Find(hcomp.KindGuardrail, "test-before-stop")
	if !ok || c.Status != hcomp.StatusCandidate || c.Origin != hcomp.OriginAgent || c.Version != 1 {
		t.Fatalf("create phải ghi candidate/agent: %+v", c)
	}
	if len(res.Files) != 1 || res.Files[0].Before != nil || res.Files[0].Path != ".bint/harness/guardrails/test-before-stop.yml" {
		t.Fatalf("FileChange: %+v", res.Files)
	}
	if hcomp.Load(f.root).Select(hcomp.ModeOn).Files().Guardrails() != nil {
		t.Fatal("candidate chưa có hiệu lực")
	}
	if _, err := Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: create.ID, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "guardrails", "test-before-stop.yml")); err == nil {
		t.Fatal("quay lui phải xoá tệp do đề xuất tạo")
	}

	// update thành phần đã duyệt.
	saveComps(t, f.root, hcomp.Component{Kind: hcomp.KindGuidance, Meta: hcomp.Meta{ID: "tips", Scope: hcomp.ScopeGlobal,
		Status: hcomp.StatusVerified, Origin: hcomp.OriginHuman, Version: 3, ApprovedBy: "Ai Đó"}, Title: "Mẹo", Body: "Bản cũ."})
	before, _ := os.ReadFile(filepath.Join(dir, "guidance", "tips.md"))
	lib := hcomp.Load(f.root)
	cur, _ := lib.Find(hcomp.KindGuidance, "tips")
	upd := newHarnessP(HarnessChange{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "tips",
		Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "tips", Scope: "global", Title: "Mẹo", Body: "Bản mới."}})
	upd.ID, upd.Action, upd.Risk = "ep_h2", ActionUpdate, RiskMedium
	upd.Plan = defaultPlan(ActionUpdate, f.k.Config.Evolution.Evaluation)
	upd.CreatedAt, upd.UpdatedAt = "2026-11-10T12:00:00.000Z", "2026-11-10T12:00:00.000Z"
	upd.Base, err = HarnessBaseOf(dir, []hcomp.Component{cur})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveProposal(ctx, f.db, upd); err != nil {
		t.Fatal(err)
	}
	if probs, err := upd.CheckBaseWS(f.k); err != nil || len(probs) != 0 {
		t.Fatalf("base: %v %v", probs, err)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: upd.ID, Unverified: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	got, _ := hcomp.Load(f.root).Find(hcomp.KindGuidance, "tips")
	if got.Status != hcomp.StatusVerified || got.Version != 4 || got.Body != "Bản mới." || !strings.Contains(got.ApprovedBy, "Rev") {
		t.Fatalf("update giữ verified, tăng phiên bản, ghi người duyệt: %+v", got)
	}
	if _, err := Rollback(ctx, RollbackOptions{K: f.k, DB: f.db, Ref: upd.ID, Now: now}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "guidance", "tips.md"))
	if string(after) != string(before) {
		t.Fatalf("quay lui phải trả lại tệp y nguyên:\n%s\n---\n%s", before, after)
	}

	// Phiên bản gốc đã đổi → không áp dụng được.
	os.WriteFile(filepath.Join(dir, "guidance", "tips.md"), []byte("---\nid: tips\nscope: global\nstatus: verified\norigin: human\nversion: 9\n---\n\nSửa tay.\n"), 0o644)
	stale := *upd
	stale.ID, stale.Status = "ep_h3", StatusProposed
	if err := SaveProposal(ctx, f.db, &stale); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: stale.ID, Unverified: true, Now: now}); err == nil || !strings.Contains(err.Error(), "phiên bản gốc đã đổi") {
		t.Fatalf("phiên bản gốc đã đổi: %v", err)
	}
}

// Agent không áp dụng được đề xuất quy trình (requireHuman).
func TestApplyHarnessRequiresHuman(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "g", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "g", Scope: "global", Body: "x"}})
	p.Base, p.Risk, p.Plan = []BaseVersion{}, RiskLow, defaultPlan(ActionCreate, f.k.Config.Evolution.Evaluation)
	p.CreatedAt, p.UpdatedAt = "2026-11-10T12:00:00.000Z", "2026-11-10T12:00:00.000Z"
	if err := SaveProposal(ctx, f.db, p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BINT_AGENT", "1")
	if _, err := Apply(ctx, ApplyOptions{K: f.k, DB: f.db, Ref: p.ID, Unverified: true, Now: now}); err == nil || !strings.Contains(err.Error(), "chỉ dành cho người") {
		t.Fatalf("agent bị chặn: %v", err)
	}
}

// Chọn tác vụ: bản chung đo trên mọi tác vụ trừ tác vụ nguồn; bản riêng chỉ trên tác vụ khớp vùng.
func TestSelectHarnessCasesGlobalVsDomain(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, _, _ := seedLessonFailures(t, f)
	cases = cases[:3] // ba tác vụ validation: core, core, api
	env := eval.Env{RepoDir: f.root, WorkRoot: f.k.Paths.EvalWork, BintRoot: f.root, Cfg: f.k.Config}
	lib := hcomp.Load(f.root)

	run := func(p *Proposal) map[string]CaseImpact {
		before, after, err := HarnessSets(lib, p)
		if err != nil {
			t.Fatal(err)
		}
		env.Variant = HarnessVariant(p, before, after)
		impacts, err := SelectHarnessCases(ctx, env, eval.LeakEconomy, p, nil, cases, before, after)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]CaseImpact{}
		for _, ci := range impacts {
			m[ci.CaseID] = ci
		}
		return m
	}
	global := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "g",
		Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "g", Scope: "global", Body: "Chạy test."}})
	global.EvalCases = []string{"c-val-3"} // tác vụ nguồn
	m := run(global)
	if !m["c-val-1"].Affected || !m["c-val-2"].Affected || m["c-val-3"].Affected || !strings.Contains(m["c-val-3"].Reason, "nguồn") {
		t.Fatalf("bản chung: %+v", m)
	}
	domain := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "d",
		Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "d", Scope: "domain:internal/core/**", Body: "Chạy test core."}})
	m = run(domain)
	if !m["c-val-1"].Affected || !m["c-val-2"].Affected || m["c-val-3"].Affected || !strings.Contains(m["c-val-3"].Reason, "không áp dụng") {
		t.Fatalf("bản riêng chỉ trên tác vụ khớp vùng: %+v", m)
	}
	// Guardrail có điều kiện glob: chỉ tác vụ có tệp khớp.
	cond := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuardrail, ID: "c",
		Draft: guardDraft("c", "global", hcomp.OnPreEdit, hcomp.ActRemind, hcomp.When{Paths: "internal/api/**"})})
	m = run(cond)
	if m["c-val-1"].Affected || !m["c-val-3"].Affected {
		t.Fatalf("điều kiện glob: %+v", m)
	}
}

// ComposePair (H4): năm ca.
func TestComposePairTable(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name   string
		g, d   PairProbe
		margin float64
		want   string
	}{
		{"chỉ bản chung đạt", PairProbe{Supported: true}, PairProbe{}, 0.05, PairGlobal},
		{"chỉ bản riêng đạt", PairProbe{}, PairProbe{Supported: true}, 0.05, PairDomain},
		{"cả hai đạt, chung không kém", PairProbe{true, f(0.20)}, PairProbe{true, f(0.22)}, 0.05, PairGlobal},
		{"cả hai đạt, chung kém hơn", PairProbe{true, f(0.05)}, PairProbe{true, f(0.25)}, 0.05, PairDomain},
		{"cả hai đạt, không đo được bản chung trên vùng", PairProbe{true, nil}, PairProbe{true, f(0.10)}, 0.05, PairDomain},
		{"không bản nào đạt", PairProbe{}, PairProbe{}, 0.05, ""},
		{"đúng biên: kém đúng margin vẫn giữ chung", PairProbe{true, f(0.20)}, PairProbe{true, f(0.25)}, 0.05, PairGlobal},
	}
	for _, c := range cases {
		got, why := ComposePair(c.g, c.d, c.margin)
		if got != c.want || why == "" {
			t.Errorf("%s: %q (%s), muốn %q", c.name, got, why, c.want)
		}
	}
}

// Các hàm thuần của quy trình không bao giờ gộp: ApplyHarnessChanges trên tập before không sửa before.
func TestApplyHarnessChangesDoesNotMutate(t *testing.T) {
	before := []hcomp.Component{{Kind: hcomp.KindGuidance, Meta: hmeta("a", hcomp.ScopeGlobal, hcomp.StatusVerified), Body: "A"}}
	p := newHarnessP(HarnessChange{Op: ActionUpdate, Kind: hcomp.KindGuidance, ID: "a", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "a", Scope: "global", Body: "B"}},
		HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "b", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "b", Scope: "global", Body: "C"}})
	after, err := ApplyHarnessChanges(p, before, true)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Body != "A" || len(before) != 1 {
		t.Fatalf("before bị sửa: %+v", before)
	}
	if len(after) != 2 || after[0].Body != "B" || after[0].Version != 2 || after[1].Status != hcomp.StatusVerified {
		t.Fatalf("after: %+v", after)
	}
	dep := newHarnessP(HarnessChange{Op: ActionDeprecate, Kind: hcomp.KindGuidance, ID: "a"})
	if a, _ := ApplyHarnessChanges(dep, before, true); len(a) != 0 {
		t.Fatalf("deprecate gỡ khỏi tập: %+v", a)
	}
}

// Bài học E (H4): hai ứng viên chung + riêng cùng được tạo mới thì được gắn cặp trỏ nhau.
func TestProposeLessonsAttachesPair(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, scan, h := seedLessonFailures(t, f)
	var opp Opportunity
	for _, o := range scan.Opportunities {
		if o.Key == failureSignalPrefix+SigNoTest {
			opp = o
		}
	}
	pr, err := Propose(ctx, ProposeInput{K: f.k, DB: f.db, Scan: scan, History: h, Refs: []string{opp.ID}, Max: 1,
		Reflector: &fakeReflector{reply: lessonReplyTwoKinds}, Cases: cases, RepoDir: f.root, Timeout: time.Minute, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	var g, d *Proposal
	for _, oc := range pr.Outcomes {
		if oc.Proposal == nil || oc.Proposal.Pair == nil {
			continue
		}
		switch oc.Proposal.Pair.Variant {
		case PairGlobal:
			g = oc.Proposal
		case PairDomain:
			d = oc.Proposal
		}
	}
	if g == nil || d == nil || g.Pair.Sibling != d.ID || d.Pair.Sibling != g.ID || g.Pair.Key != opp.Key || d.Pair.Area != "internal/core/**" {
		t.Fatalf("bài học chung/riêng phải được gắn cặp: %+v", pr.Outcomes)
	}
	if got, err := GetProposal(ctx, f.db, d.ID); err != nil || got.Pair == nil || got.Pair.Sibling != g.ID {
		t.Fatalf("Pair phải được lưu: %v %+v", err, got)
	}
}

// Từ chối nửa cặp không được chọn không cần người và ghi lý do; đề xuất đã xử lý thì không bác bỏ lại.
func TestRejectSibling(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := newHarnessP(HarnessChange{Op: ActionCreate, Kind: hcomp.KindGuidance, ID: "g", Draft: &HarnessDraft{Kind: hcomp.KindGuidance, ID: "g", Scope: "global", Body: "x"}})
	p.Base, p.Risk, p.Plan = []BaseVersion{}, RiskLow, defaultPlan(ActionCreate, f.k.Config.Evolution.Evaluation)
	p.CreatedAt, p.UpdatedAt = "2026-11-10T12:00:00.000Z", "2026-11-10T12:00:00.000Z"
	if err := SaveProposal(ctx, f.db, p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BINT_AGENT", "1") // kể cả khi chạy từ phiên agent: bác bỏ không sửa tệp nào
	if err := RejectSibling(ctx, f.db, p, "ep_other", "bản chung kém hơn", now); err != nil {
		t.Fatal(err)
	}
	got, _ := GetProposal(ctx, f.db, p.ID)
	if got.Status != StatusRejected || !strings.Contains(got.StatusReason, "ep_other") {
		t.Fatalf("%+v", got)
	}
	if err := RejectSibling(ctx, f.db, got, "ep_other", "x", now); err == nil {
		t.Fatal("đã bác bỏ thì không bác bỏ lại")
	}
}

// AreaEffect: hiệu ứng (candidate − base) trên tác vụ thuộc vùng; nil khi vùng không có tác vụ nào.
func TestAreaEffect(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`INSERT INTO eval_batches (batch_id, status, harness, leakage_mode, arms, repeats, case_ids,
		prompt_version, seed, started_at) VALUES ('eb-p', 'completed', 'claude', 'strict', '["base","candidate"]', 2, '[]', 1, 1, ?)`,
		store.FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	n := 0
	add := func(caseID, arm string, pass bool) {
		n++
		if _, err := f.db.Exec(`INSERT INTO evaluation_runs (attempt_id, batch_id, case_id, case_fingerprint, arm, repeat_index, status,
			harness, started_at, test_passed) VALUES (?, 'eb-p', ?, 'fp', ?, ?, 'passed', 'claude', ?, ?)`,
			fmt.Sprintf("ea-%d", n), caseID, arm, n, store.FormatTime(now), map[bool]int{true: 1, false: 0}[pass]); err != nil {
			t.Fatal(err)
		}
	}
	// core-1: base 0/2, candidate 2/2 (+1.0); core-2: base 1/2, candidate 1/2 (0); api-1: base 2/2, candidate 0/2 (−1.0).
	for _, r := range []struct {
		c, a string
		p    bool
	}{{"core-1", "base", false}, {"core-1", "base", false}, {"core-1", "candidate", true}, {"core-1", "candidate", true},
		{"core-2", "base", true}, {"core-2", "base", false}, {"core-2", "candidate", true}, {"core-2", "candidate", false},
		{"api-1", "base", true}, {"api-1", "base", true}, {"api-1", "candidate", false}, {"api-1", "candidate", false}} {
		add(r.c, r.a, r.p)
	}
	cases := map[string]*eval.Case{
		"core-1": {ID: "core-1", SourceFiles: []string{"internal/core/x.go"}},
		"core-2": {ID: "core-2", SourceFiles: []string{"internal/core/y.go"}},
		"api-1":  {ID: "api-1", SourceFiles: []string{"internal/api/h.go"}},
	}
	got, err := AreaEffect(ctx, f.db, "eb-p", "internal/core/**", cases)
	if err != nil || got == nil || *got < 0.499 || *got > 0.501 {
		t.Fatalf("vùng core: %v %v (muốn +0.5)", got, err)
	}
	if all, _ := AreaEffect(ctx, f.db, "eb-p", "", cases); all == nil || *all < -0.001 || *all > 0.001 {
		t.Fatalf("toàn đợt: %v (muốn 0)", all)
	}
	if none, err := AreaEffect(ctx, f.db, "eb-p", "internal/db/**", cases); err != nil || none != nil {
		t.Fatalf("vùng không có tác vụ: %v %v", none, err)
	}
}
