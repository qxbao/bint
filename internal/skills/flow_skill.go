package skills

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"bint/internal/flow"
)

// flowSkillTmpl là mẫu skill điều phối của flow tuỳ biến. Nằm ngoài core/ nên Core() không coi nó là skill gốc.
//
//go:embed flowskill/SKILL.md.tmpl
var flowSkillTmpl string

var flowSkillTemplate = template.Must(template.New("flowskill").Parse(flowSkillTmpl))

// flowStepView là một dòng của bảng bước trong skill điều phối.
type flowStepView struct {
	ID, Kind, Skill, Role, Notes string
}

type flowSkillData struct {
	Name, File, Frontmatter string
	Steps                   []flowStepView
	HasPlan, PlanOptional   bool
	PlanID, PlanFlags       string
}

// FlowSkill dựng skill điều phối của một flow tuỳ biến đã qua kiểm (bản chuẩn hoá role). Frontmatter có
// disable-model-invocation: true (chỉ chạy khi người gọi /<flow>); bản `.agents/skills` bỏ khoá đó qua ForTarget.
// Cờ lập phiếu theo hình dạng flow: --plan/--no-plan chỉ khi bước plan optional, --depth chỉ khi có bước plan.
func FlowSkill(d flow.Def) Skill {
	d = flow.Normalize(d)
	data := flowSkillData{Name: d.Name, File: flow.Rel + "/" + d.Name + ".yml", Frontmatter: flowFrontmatter(d)}
	if p, ok := d.FirstOfKind(flow.KindPlan); ok {
		data.HasPlan, data.PlanOptional, data.PlanID = true, p.Optional, p.ID
		switch {
		case p.Optional:
			data.PlanFlags = " --plan|--no-plan [--depth deep]"
		default:
			data.PlanFlags = " [--depth deep]"
		}
	}
	for _, s := range d.Steps {
		var notes []string
		if s.Optional {
			notes = append(notes, "optional: bỏ qua theo phân loại")
		}
		if s.Kind == flow.KindImplement && !s.VerifyOn() {
			notes = append(notes, "không kiểm chứng sau bước")
		}
		if s.Fix != "" {
			notes = append(notes, "vòng sửa quay về "+s.Fix)
		}
		data.Steps = append(data.Steps, flowStepView{ID: s.ID, Kind: string(s.Kind), Skill: s.Skill, Role: s.Role, Notes: strings.Join(notes, "; ")})
	}
	var buf bytes.Buffer
	if err := flowSkillTemplate.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("mẫu skill điều phối: %v", err)) // mẫu nhúng, lỗi là lỗi lập trình
	}
	return Skill{Name: d.Name, Content: buf.Bytes()}
}

// flowFrontmatter dựng khối frontmatter ("---" … "---\n"): mô tả được mã hoá bằng YAML nên ký tự đặc biệt, xuống dòng hay
// dấu hai chấm trong mô tả của flow không phá frontmatter.
func flowFrontmatter(d flow.Def) string {
	suffix := fmt.Sprintf("Dùng khi người dùng chạy /%s.", d.Name)
	head := strings.Join(strings.Fields(d.Description), " ")
	if head == "" {
		head = fmt.Sprintf("Chạy flow tuỳ biến %s (%d bước) bằng các agent riêng qua `bint run`.", d.Name, len(d.Steps))
	}
	if room := MaxDescriptionRunes - utf8.RuneCountInString(suffix) - 1; utf8.RuneCountInString(head) > room {
		head = string([]rune(head)[:room-1]) + "…"
	}
	node := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v *yaml.Node) {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, v)
	}
	add("name", &yaml.Node{Kind: yaml.ScalarNode, Value: d.Name})
	add("description", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: head + " " + suffix})
	add("disable-model-invocation", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		panic(err)
	}
	enc.Close()
	return "---\n" + buf.String() + "---\n"
}
