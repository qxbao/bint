package skills

import (
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"bint/internal/flow"
)

func decodeFlow(t *testing.T, src string) flow.Def {
	t.Helper()
	d, err := flow.Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const tddSrc = `schema: 1
name: tdd
description: Viết kiểm thử đỏ trước rồi mới viết mã
steps:
  - {id: plan,   kind: plan,      skill: b-req, optional: true}
  - {id: red,    kind: implement, skill: b-feat, role: test-writer, verify: false}
  - {id: green,  kind: implement, skill: b-feat}
  - {id: review, kind: review,    skill: b-review, fix: green}
`

func TestFlowSkillTDD(t *testing.T) {
	s := FlowSkill(decodeFlow(t, tddSrc))
	doc, err := ParseDoc(s.Content)
	if err != nil {
		t.Fatalf("frontmatter: %v\n%s", err, s.Content)
	}
	if s.Name != "tdd" || doc.Name != "tdd" || !strings.Contains(doc.Description, "Viết kiểm thử đỏ trước rồi mới viết mã") ||
		!strings.Contains(doc.Description, "Dùng khi người dùng chạy /tdd") {
		t.Fatalf("name/description: %+v", doc)
	}
	if !strings.Contains(string(s.Content), "disable-model-invocation: true") {
		t.Fatal("thiếu disable-model-invocation")
	}
	if len(doc.Extra) != 1 || doc.Extra[0] != "disable-model-invocation" {
		t.Fatalf("frontmatter chỉ có thêm disable-model-invocation: %v", doc.Extra)
	}
	for _, want := range []string{
		"$ARGUMENTS",
		`bint task new "<yêu cầu nguyên văn>" --workflow tdd --plan|--no-plan [--depth deep]`,
		"bint run tdd --task <id> --json",
		"--driver session", "bint run next <id> --json --no-wait", "bint run complete <id> --run <run_id> --agent-id <agentId>",
		"bint run resume <id>", "Flow đổi giữa chừng", ".bint/flows/tdd.yml", "limits.workflows.tdd",
		"| plan | plan | b-req | planner | optional: bỏ qua theo phân loại |",
		"| red | implement | b-feat | test-writer | không kiểm chứng sau bước |",
		"| green | implement | b-feat | implementer |  |",
		"| review | review | b-review | reviewer | vòng sửa quay về green |",
		"bint task approve", // trong ranh giới tin cậy: không bao giờ chạy
	} {
		if !strings.Contains(string(s.Content), want) {
			t.Errorf("thiếu %q", want)
		}
	}
	// Bản .agents chỉ còn name và description.
	a := ForTarget(s, AgentsTarget)
	ad, err := ParseDoc(a.Content)
	if err != nil || len(ad.Extra) != 0 || ad.Name != "tdd" || ad.Description != doc.Description {
		t.Fatalf("bản agents: %v %+v", err, ad)
	}
	if again := FlowSkill(decodeFlow(t, tddSrc)); string(again.Content) != string(s.Content) {
		t.Fatal("dựng hai lần phải giống hệt (băm ổn định)")
	}
}

func TestFlowSkillShapeDecidesPlanFlags(t *testing.T) {
	noPlan := FlowSkill(decodeFlow(t, "schema: 1\nname: quick\nsteps:\n  - {id: build, kind: implement, skill: b-feat}\n"))
	c := string(noPlan.Content)
	for _, bad := range []string{"--no-plan", "--plan", "--depth"} {
		if strings.Contains(c, bad) {
			t.Errorf("flow không có bước plan không được nhắc %q", bad)
		}
	}
	if !strings.Contains(c, "--workflow quick") || !strings.Contains(c, "bint run quick --task") {
		t.Fatalf("thiếu lệnh:\n%s", c)
	}
	if doc, err := ParseDoc(noPlan.Content); err != nil || !strings.Contains(doc.Description, "Chạy flow tuỳ biến quick (1 bước)") {
		t.Fatalf("mô tả mặc định: %v %q", err, doc.Description)
	}

	required := string(FlowSkill(decodeFlow(t, "schema: 1\nname: spec\nsteps:\n  - {id: design, kind: plan, skill: b-req}\n  - {id: build, kind: implement, skill: b-feat}\n")).Content)
	if strings.Contains(required, "--no-plan") || !strings.Contains(required, "--workflow spec [--depth deep]") || !strings.Contains(required, "bint task write") {
		t.Fatalf("bước plan bắt buộc: không --no-plan, có --depth deep và nhắc bint task write:\n%s", required)
	}
}

func TestFlowSkillDescriptionSurvivesSpecialCharacters(t *testing.T) {
	for name, desc := range map[string]string{
		"hai chấm":     "Quy trình: viết test --- rồi mã",
		"nháy":         `Quy trình "kép" và 'đơn' #không-phải-chú-thích`,
		"xuống dòng":   "dòng một\n\ndòng hai\r\ndòng ba",
		"ký tự yaml":   "- [a, b] {c: d} & *e !f | > %g @h `i`",
		"bắt đầu bằng": "---\nname: giả\n---",
		"quá dài":      strings.Repeat("rất dài ", 400),
	} {
		t.Run(name, func(t *testing.T) {
			d := decodeFlow(t, tddSrc)
			d.Description = desc
			s := FlowSkill(d)
			doc, err := ParseDoc(s.Content)
			if err != nil {
				t.Fatalf("frontmatter hỏng: %v\n%s", err, s.Content)
			}
			if doc.Name != "tdd" || len(doc.Extra) != 1 || !strings.HasSuffix(doc.Description, "Dùng khi người dùng chạy /tdd.") {
				t.Fatalf("%+v", doc)
			}
			if n := utf8.RuneCountInString(doc.Description); n > MaxDescriptionRunes {
				t.Fatalf("mô tả %d ký tự, trần %d", n, MaxDescriptionRunes)
			}
			if strings.Contains(doc.Description, "\n") {
				t.Fatalf("mô tả không được xuống dòng: %q", doc.Description)
			}
		})
	}
}

// GĐ5 review .2: name trong frontmatter luôn là chuỗi, kể cả tên dạng số hoặc từ khoá YAML.
func TestFlowSkillNameIsAlwaysYAMLString(t *testing.T) {
	for _, name := range []string{"123", "null", "true", "1e3", "0x1f", "~", "no", "2026-11-13"} {
		t.Run(name, func(t *testing.T) {
			d := decodeFlow(t, tddSrc)
			d.Name = name
			s := FlowSkill(d)
			front := strings.SplitN(string(s.Content), "---\n", 3)[1]
			var m map[string]any
			if err := yaml.Unmarshal([]byte(front), &m); err != nil {
				t.Fatalf("frontmatter hỏng: %v\n%s", err, front)
			}
			if got, ok := m["name"].(string); !ok || got != name {
				t.Fatalf("name phải là chuỗi %q, được %T %v\n%s", name, m["name"], m["name"], front)
			}
			if doc, err := ParseDoc(s.Content); err != nil || doc.Name != name {
				t.Fatalf("ParseDoc: %v %+v", err, doc)
			}
		})
	}
}

// Skill điều phối của flow tuỳ biến phải qua cùng kiểm hình thức như skill tuỳ biến (thân ≤ MaxBodyLines).
func TestFlowSkillBodyWithinLimit(t *testing.T) {
	doc, err := ParseDoc(FlowSkill(decodeFlow(t, tddSrc)).Content)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(doc.Body, "\n") + 1; n > MaxBodyLines {
		t.Fatalf("thân %d dòng, trần %d", n, MaxBodyLines)
	}
}
