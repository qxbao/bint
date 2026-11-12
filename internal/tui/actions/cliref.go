package actions

import (
	"reflect"
	"sort"
	"strings"
	"sync"
)

// cliRef ánh xạ một lệnh CLI (đường lệnh + cờ bắt buộc) tới hành động TUI tương ứng, kèm thẻ phụ nơi phím đó dùng được
// (phím trong sổ chỉ có nghĩa ở một thẻ phụ của màn). Dùng để viết lại thông điệp của tầng dịch vụ khi hiện trong TUI
// (docs/tui-plan.md mục 9, yêu cầu bổ sung): CLI giữ nguyên chữ, TUI chỉ phím/hành động của TUI.
type cliRef struct {
	path string   // "knowledge verify"
	need []string // cờ bắt buộc để khớp (rỗng = mặc định cho đường lệnh)
	id   string   // ID hành động trong sổ
	tab  string   // khoá i18n của tên thẻ phụ ("" = không có)
	// tabOnly: tham chiếu là chính thẻ phụ (hành động chỉ có trong bảng lệnh, không có phím) thay vì bảng lệnh.
	tabOnly bool
	// writeFlag/writeID: khi lệnh mang cờ writeFlag, phần chạy theo cờ đó là hành động writeID (thường là hành động ghi) chứ không phải id;
	// ghi chú chỉ đọc tính theo hành động đó ("knowledge verify --apply" chạy knowledge.verify_apply). Khác với need: không đổi hành động
	// được trỏ tới và không loại cờ khỏi ô form.
	writeFlag, writeID string
}

// CLITarget là kết quả tra một lệnh CLI trong bảng ánh xạ.
type CLITarget struct {
	Action  Action
	TabKey  string
	TabOnly bool
	// Fields là các cờ của lệnh nhập được ở ô form của hành động (đã lọc theo cờ có thật trong Options).
	Fields []string
	// WriteID: hành động thực sự chạy khi lệnh mang cờ ghi của hàng (rỗng nếu không có).
	WriteID string
}

// Hàng đầu của mỗi nhóm là lệnh mặc định; hàng có need được ưu tiên khi lệnh mang đủ các cờ đó.
var cliRefs = []cliRef{
	{path: "status", id: "overview.refresh"},
	{path: "doctor", id: "system.doctor", tab: "sy.tab.doctor", tabOnly: true},

	{path: "init", id: "system.repo_plan", tab: "sy.tab.setup"},
	{path: "init", need: []string{"update"}, id: "system.config_upgrade_plan", tab: "sy.tab.config"},

	{path: "knowledge approve", id: "knowledge.approve"},
	{path: "knowledge approve", need: []string{"refresh"}, id: "knowledge.approve_refresh"},
	{path: "knowledge reject", id: "knowledge.reject"},
	{path: "knowledge deprecate", id: "knowledge.deprecate"},
	{path: "knowledge verify", id: "knowledge.verify", writeFlag: "apply", writeID: "knowledge.verify_apply"},
	{path: "knowledge review", id: "knowledge.review", tab: "kn.sub.list"},
	{path: "knowledge show", id: "knowledge.show"},
	{path: "knowledge list", id: "knowledge.list"},
	{path: "knowledge add", id: "knowledge.add"},
	{path: "knowledge rm", id: "knowledge.rm"},
	{path: "knowledge transitions", id: "knowledge.transitions"},
	{path: "knowledge search", id: "knowledge.search", tab: "kn.sub.search", tabOnly: true},
	{path: "knowledge stats", id: "knowledge.stats", tab: "kn.sub.stats", tabOnly: true},
	{path: "knowledge diff", id: "knowledge.diff", tab: "kn.sub.diff", tabOnly: true},
	{path: "knowledge revalidate", id: "knowledge.revalidate", tab: "kn.sub.revalidate", tabOnly: true},
	{path: "bootstrap publish", id: "bootstrap.publish_preview", tab: "kn.sub.publish", tabOnly: true},
	{path: "bootstrap", id: "bootstrap", tab: "kn.sub.survey", tabOnly: true},
	{path: "inspect", id: "inspect", tab: "kn.sub.survey", tabOnly: true},
	{path: "context", id: "context", tab: "kn.sub.context", tabOnly: true},

	{path: "run", id: "tasks.run"},
	{path: "run resume", id: "tasks.resume"},
	{path: "task approve", id: "tasks.approve"},
	{path: "task status", id: "tasks.status"},
	{path: "task new", id: "tasks.new"},
	{path: "task verify", id: "tasks.verify"},
	{path: "task set", id: "tasks.set"},
	{path: "task write", id: "tasks.edit_doc", tab: "tk.sub.docs"},
	{path: "task criteria", id: "tasks.criteria"},
	{path: "task error", id: "tasks.error"},
	{path: "task review", id: "tasks.review_edit"},
	{path: "task lint", id: "tasks.lint"},
	{path: "task diff", id: "tasks.diff"},
	{path: "task transitions", id: "tasks.transitions"},
	{path: "task list", id: "tasks.list"},
	{path: "task show", id: "tasks.show"},

	{path: "runs rate", id: "runs.rate"},
	{path: "runs scan", id: "runs.scan"},
	{path: "events ingest", id: "runs.ingest"},
	{path: "runs list", id: "runs.list"},
	{path: "runs show", id: "runs.show"},
	{path: "experience digest", id: "runs.lesson_digest", tab: "rn.tab.lessons"},
	{path: "experience show", id: "runs.lesson_show", tab: "rn.tab.lessons"},

	{path: "eval", id: "nav.eval", tabOnly: true},
	{path: "eval mine", id: "eval.mine", tab: "eval.tab.cands"},
	{path: "eval verify", id: "eval.verify"},
	{path: "eval list", id: "eval.list"},
	{path: "eval show", id: "eval.show"},
	{path: "eval spec", id: "eval.spec"},
	{path: "eval approve", id: "eval.approve"},
	{path: "eval reject", id: "eval.reject"},
	{path: "eval estimate", id: "eval.estimate"},
	{path: "eval run", id: "eval.run"},
	{path: "eval run", need: []string{"resume"}, id: "eval.resume"},
	{path: "eval report", id: "eval.report", tab: "eval.tab.batches", tabOnly: true},
	{path: "eval bootstrap", id: "eval.bootstrap"},
	{path: "eval clean", id: "eval.clean"},

	{path: "evolve", id: "nav.evolve", tabOnly: true},
	{path: "evolve scan", id: "evolve.scan"},
	{path: "evolve propose", id: "evolve.propose"},
	{path: "evolve list", id: "evolve.list"},
	{path: "evolve show", id: "evolve.show"},
	{path: "evolve evaluate", id: "evolve.evaluate"},
	{path: "evolve apply", id: "evolve.apply"},
	{path: "evolve reject", id: "evolve.reject"},
	{path: "evolve rollback", id: "evolve.rollback"},
	{path: "evolve history", id: "evolve.history", tab: "ev.tab.history", tabOnly: true},
	{path: "evolve queue", id: "evolve.queue", tab: "ev.tab.queue", tabOnly: true},
	{path: "evolve process", id: "evolve.process"},
	{path: "evolve cycle", id: "evolve.cycle"},

	{path: "harness", id: "system.harness_list", tab: "sy.tab.harness", tabOnly: true},
	{path: "harness list", id: "system.harness_list", tab: "sy.tab.harness", tabOnly: true},
	{path: "harness show", id: "system.harness_show", tab: "sy.tab.harness"},
	{path: "harness component list", id: "system.component_list", tab: "sy.tab.components", tabOnly: true},
	{path: "harness component show", id: "system.component_show", tab: "sy.tab.components"},
	{path: "harness component add", id: "system.component_add", tab: "sy.tab.components"},
	{path: "harness component approve", id: "system.component_approve", tab: "sy.tab.components"},
	{path: "harness component deprecate", id: "system.component_deprecate", tab: "sy.tab.components"},
	{path: "harness render", id: "system.render", tab: "sy.tab.components"},
	{path: "skill list", id: "system.skill_list", tab: "sy.tab.skills", tabOnly: true},
	{path: "skill show", id: "system.skill_show", tab: "sy.tab.skills"},
	{path: "skill overlay", id: "system.skill_overlay", tab: "sy.tab.skills"},
	{path: "skill diff", id: "system.skill_diff", tab: "sy.tab.skills"},
	{path: "skill versions", id: "system.skill_versions", tab: "sy.tab.skills"},
	{path: "skill check", id: "system.skill_check", tab: "sy.tab.skills"},
	{path: "skill regress", id: "system.skill_regress", tab: "sy.tab.skills"},
	{path: "sync", id: "system.sync_plan", tab: "sy.tab.skills"},
	{path: "update", id: "system.update", tab: "sy.tab.update"},
	{path: "index rebuild", id: "system.index_rebuild", tab: "sy.tab.index"},
	{path: "mcp tools", id: "system.mcp_tools", tab: "sy.tab.mcp", tabOnly: true},
}

var cliIndex = sync.OnceValue(func() map[string][]cliRef {
	m := map[string][]cliRef{}
	for _, r := range cliRefs {
		m[r.path] = append(m[r.path], r)
	}
	return m
})

// CLIPaths trả mọi đường lệnh CLI có tham chiếu TUI, dài trước (để khớp dài nhất).
func CLIPaths() []string {
	var out []string
	for p := range cliIndex() {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if a, b := strings.Count(out[i], " "), strings.Count(out[j], " "); a != b {
			return a > b
		}
		return out[i] < out[j]
	})
	return out
}

// HasCLIPath cho biết đường lệnh có tham chiếu TUI.
func HasCLIPath(path string) bool { _, ok := cliIndex()[path]; return ok }

// LookupCLI tra lệnh CLI (đường lệnh và các cờ đã gặp) trong bảng ánh xạ; ưu tiên hàng có nhiều cờ bắt buộc khớp nhất.
func LookupCLI(reg *Registry, path string, flags []string) (CLITarget, bool) {
	rows := cliIndex()[path]
	if len(rows) == 0 {
		return CLITarget{}, false
	}
	have := map[string]bool{}
	for _, f := range flags {
		have[f] = true
	}
	best, bestN := -1, -1
	for i, r := range rows {
		ok := true
		for _, n := range r.need {
			ok = ok && have[n]
		}
		if ok && len(r.need) > bestN {
			best, bestN = i, len(r.need)
		}
	}
	if best < 0 {
		return CLITarget{}, false
	}
	r := rows[best]
	a, ok := reg.ByID(r.id)
	if !ok {
		return CLITarget{}, false
	}
	t := CLITarget{Action: a, TabKey: r.tab, TabOnly: r.tabOnly}
	if r.writeFlag != "" && have[r.writeFlag] {
		t.WriteID = r.writeID
	}
	known := map[string]bool{}
	for _, f := range a.Flags() {
		known[f] = true
	}
	for _, f := range flags {
		if known[f] && !slicesContains(r.need, f) {
			t.Fields = append(t.Fields, f)
		}
	}
	return t, true
}

// TargetFor dựng tham chiếu TUI tới hành động id (không qua chuỗi lệnh CLI): thẻ phụ và ghi chú ghi theo hàng của bảng ánh xạ
// có cùng id (hàng đầu tiên), fields là các cờ nhập được ở ô form (lọc theo cờ có thật trong Options). Dùng cho thông điệp có cấu trúc
// biết chính xác cần chỉ tới hành động nào (docs/tui-plan.md mục 12.11).
func TargetFor(reg *Registry, id string, fields ...string) (CLITarget, bool) {
	a, ok := reg.ByID(id)
	if !ok {
		return CLITarget{}, false
	}
	t := CLITarget{Action: a}
	var row *cliRef
	for i := range cliRefs {
		if cliRefs[i].id == id {
			row = &cliRefs[i]
			break
		}
	}
	if row != nil {
		t.TabKey, t.TabOnly = row.tab, row.tabOnly
		if row.writeFlag != "" && slicesContains(fields, row.writeFlag) {
			t.WriteID = row.writeID
		}
	}
	known := map[string]bool{}
	for _, f := range a.Flags() {
		known[f] = true
	}
	for _, f := range fields {
		if known[f] {
			t.Fields = append(t.Fields, f)
		}
	}
	return t, true
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// valueFlags: cờ nhận giá trị (kiểu không phải bool ở ít nhất một Options trong sổ); cờ bool đứng một mình.
var valueFlags = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if n := f.Tag.Get("flag"); n != "" && f.Type.Kind() != reflect.Bool {
				out[n] = true
			}
			if f.Anonymous {
				walk(f.Type)
			}
		}
	}
	for _, a := range Default().All() {
		if a.Options == nil {
			continue
		}
		t := reflect.TypeOf(a.Options)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		walk(t)
	}
	return out
})

// FlagTakesValue cho biết cờ CLI (không gồm "--") nhận một giá trị đi kèm.
func FlagTakesValue(name string) bool { return valueFlags()[name] }
