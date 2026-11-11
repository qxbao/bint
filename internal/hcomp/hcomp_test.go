package hcomp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bản gốc của eval.BintToolsNote trước khi chuyển thành guidance dựng sẵn (hợp đồng đo).
const oldBintToolsNote = `
## Công cụ tri thức (Bint)

MCP server ` + "`bint`" + ` có tri thức về repository này (quy tắc, điểm dễ sai, hợp đồng hành vi).
Các công cụ có thể chưa được nạp sẵn: nạp bằng ToolSearch với truy vấn
"select:mcp__bint__context_for_task,mcp__bint__context_for_paths,mcp__bint__cite_knowledge".

1. Đầu tác vụ: gọi mcp__bint__context_for_task với mô tả tác vụ (và các tệp đã biết).
2. Trước khi sửa một tệp: gọi mcp__bint__context_for_paths với đường dẫn tệp.
3. Khi áp dụng một mục tri thức: gọi mcp__bint__cite_knowledge với ID của mục.

Trước khi đọc/sửa tệp, quy tắc khớp đường dẫn cũng có thể được chèn tự động.
`

func TestMCPToolsNoteBytes(t *testing.T) {
	set := (*Library)(nil).Select(ModeOff)
	if got := set.RenderGuidance(RenderOpts{MCP: true}); got != oldBintToolsNote {
		t.Fatalf("guidance dựng sẵn khác byte so với hằng cũ:\n%q\n%q", got, oldBintToolsNote)
	}
	if got := set.RenderGuidance(RenderOpts{MCP: false}); got != "" {
		t.Fatalf("không có MCP thì không có ghi chú MCP: %q", got)
	}
	if MCPToolsNote != oldBintToolsNote {
		t.Fatal("MCPToolsNote đổi")
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func comp(id, status string) string {
	return "id: " + id + "\nscope: global\nstatus: " + status + "\norigin: human\nversion: 1\n"
}

func TestLoadParseAndProblems(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "guidance/g1.md", "---\n"+comp("g1", "verified")+"title: Chỉ dẫn một\n---\n\nNội dung chỉ dẫn.\n")
	write(t, dir, "guardrails/test-before-stop.yml", comp("test-before-stop", "verified")+
		"on: stop\nwhen: { edited: \"src/**\" }\nunless: { ran: test }\naction: remind\nmessage: Chạy test.\n")
	write(t, dir, "scripts/run-it.yml", comp("run-it", "candidate")+"description: Chạy test đúng chỗ\nusage: scripts/run-it.sh <tệp>\n")
	write(t, dir, "scripts/run-it.sh", "#!/bin/sh\necho ok\n")
	write(t, dir, "strategy.yml", comp("strategy", "verified")+"context:\n  max_tokens: 1500\n  lessons: { global_k: 2 }\n")
	// Hỏng: id lệch tên, khoá lạ, regexp hỏng, action sai, script thiếu .yml, thiếu frontmatter.
	write(t, dir, "guidance/wrong.md", "---\n"+comp("other", "verified")+"---\nx\n")
	write(t, dir, "guidance/nofm.md", "chỉ có thân\n")
	write(t, dir, "guardrails/unk.yml", comp("unk", "verified")+"on: stop\naction: remind\nmessage: m\nbogus: 1\n")
	write(t, dir, "guardrails/badre.yml", comp("badre", "verified")+"on: pre_bash\nwhen: { command: \"(\" }\naction: remind\nmessage: m\n")
	write(t, dir, "guardrails/badact.yml", comp("badact", "verified")+"on: stop\naction: explode\nmessage: m\n")
	write(t, dir, "guardrails/empty.yml", comp("empty", "verified")+"on: pre_bash\nwhen: { command: \".*\" }\naction: remind\nmessage: m\n")
	write(t, dir, "scripts/orphan.sh", "#!/bin/sh\n")

	lib := LoadDir(dir)
	var ids []string
	for _, c := range lib.Components {
		ids = append(ids, string(c.Kind)+"/"+c.ID)
	}
	want := "guidance/g1,guardrail/test-before-stop,script/run-it,strategy/strategy"
	if got := strings.Join(ids, ","); got != want {
		t.Fatalf("thành phần = %s, muốn %s\nvấn đề: %v", got, want, lib.Problems)
	}
	if len(lib.Problems) != 7 {
		t.Fatalf("vấn đề = %d, muốn 7: %v", len(lib.Problems), lib.Problems)
	}
	g, _ := lib.Find(KindGuidance, "g1")
	if g.Title != "Chỉ dẫn một" || g.Body != "Nội dung chỉ dẫn." {
		t.Fatalf("guidance: %+v", g)
	}
	s, _ := lib.Find(KindScript, "run-it")
	if string(s.Source) != "#!/bin/sh\necho ok\n" {
		t.Fatalf("script: %+v", s)
	}
}

func TestCheckTable(t *testing.T) {
	good := Component{Kind: KindGuardrail, Meta: Meta{ID: "x", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginAgent, Version: 1},
		On: OnStop, Action: ActRemind, Message: "m"}
	mut := func(f func(*Component)) Component { c := good; f(&c); return c }
	cases := []struct {
		name string
		c    Component
		want string // "" = hợp lệ
	}{
		{"hợp lệ", good, ""},
		{"id sai", mut(func(c *Component) { c.ID = "Có Dấu" }), "id"},
		{"scope sai", mut(func(c *Component) { c.Scope = "everywhere" }), "scope"},
		{"scope domain tuyệt đối", mut(func(c *Component) { c.Scope = "domain:/etc/**" }), "scope"},
		{"status sai", mut(func(c *Component) { c.Status = "ok" }), "status"},
		{"origin sai", mut(func(c *Component) { c.Origin = "bot" }), "origin"},
		{"version 0", mut(func(c *Component) { c.Version = 0 }), "version"},
		{"on sai", mut(func(c *Component) { c.On = "pre_read" }), "on"},
		{"thiếu message", mut(func(c *Component) { c.Message = " " }), "message"},
		{"paths ở stop", mut(func(c *Component) { c.When.Paths = "a/**" }), "when.paths"},
		{"command ở stop", mut(func(c *Component) { c.When.Command = "go" }), "when.command"},
		{"regexp hỏng", mut(func(c *Component) { c.On, c.When.Command = OnPreBash, "[" }), "biểu thức"},
		{"regexp khớp rỗng", mut(func(c *Component) { c.On, c.When.Command = OnPreBash, "a*" }), "rỗng"},
		{"ask pre_bash không điều kiện", mut(func(c *Component) { c.On, c.Action = OnPreBash, ActAsk }), "when.command"},
		{"deny không điều kiện", mut(func(c *Component) { c.Action = ActDeny }), "deny"},
		{"deny có điều kiện", mut(func(c *Component) { c.Action, c.When.Edited = ActDeny, "src/**" }), ""},
		{"unless.ran regexp", mut(func(c *Component) { c.Unless.Ran = "make (test|check)" }), ""},
		{"unless.ran hỏng", mut(func(c *Component) { c.Unless.Ran = "(" }), "unless.ran"},
		{"glob thoát ra ngoài", mut(func(c *Component) { c.When.Edited = "../x" }), "glob"},
		{"guidance rỗng", Component{Kind: KindGuidance, Meta: good.Meta}, "nội dung"},
		{"guidance requires lạ", Component{Kind: KindGuidance, Meta: good.Meta, Body: "x", Requires: []string{"gpu"}}, "requires"},
		{"script thiếu mô tả", Component{Kind: KindScript, Meta: good.Meta}, "description"},
		{"script thiếu shebang", Component{Kind: KindScript, Meta: good.Meta, Description: "d", Source: []byte("echo")}, "shebang"},
		{"strategy sai id", Component{Kind: KindStrategy, Meta: good.Meta, Context: &StrategyContext{MaxTokens: ip(1)}}, "strategy"},
		{"strategy rỗng", Component{Kind: KindStrategy, Meta: Meta{ID: "strategy", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginHuman, Version: 1}}, "context"},
		{"strategy âm", Component{Kind: KindStrategy, Meta: Meta{ID: "strategy", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginHuman, Version: 1},
			Context: &StrategyContext{MaxTokens: ip(-1)}}, "max_tokens"},
	}
	for _, c := range cases {
		err := Check(c.c)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: lỗi bất ngờ %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: lỗi = %v, muốn chứa %q", c.name, err, c.want)
		}
	}
}

func ip(n int) *int { return &n }

func TestFindConflict(t *testing.T) {
	base := Component{Kind: KindGuardrail, Meta: Meta{ID: "a", Scope: ScopeGlobal, Status: StatusVerified, Origin: OriginHuman, Version: 1},
		On: OnStop, When: When{Edited: "src/**"}, Unless: Unless{Ran: RanTest}, Action: ActRemind, Message: "m"}
	other := base
	other.ID = "b"
	if c := FindConflict([]Component{base}, other); c == nil || c.Kind != "duplicate" {
		t.Fatalf("trùng: %v", c)
	}
	other.Action = ActDeny
	if c := FindConflict([]Component{base}, other); c == nil || c.Kind != "contradiction" {
		t.Fatalf("mâu thuẫn: %v", c)
	}
	other.When.Edited = "docs/**"
	if c := FindConflict([]Component{base}, other); c != nil {
		t.Fatalf("khác điều kiện không xung đột: %v", c)
	}
	dep := base
	dep.Status = StatusDeprecated
	same := base
	same.ID = "c"
	if c := FindConflict([]Component{dep}, same); c != nil {
		t.Fatalf("deprecated không tính: %v", c)
	}
}

func TestSelectModes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "guidance/v.md", "---\n"+comp("v", "verified")+"---\nverified\n")
	write(t, dir, "guidance/c.md", "---\n"+comp("c", "candidate")+"---\ncandidate\n")
	write(t, dir, "guidance/d.md", "---\n"+comp("d", "deprecated")+"---\ndeprecated\n")
	lib := LoadDir(dir)
	ids := func(m Mode) string {
		var out []string
		for _, c := range lib.Select(m).Guidance() {
			out = append(out, c.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(ModeOff); got != "bint-mcp-tools" {
		t.Errorf("off = %s", got)
	}
	if got := ids(ModeOn); got != "bint-mcp-tools,v" {
		t.Errorf("on = %s", got)
	}
	if got := ids(ModeAll); got != "bint-mcp-tools,c,v" {
		t.Errorf("all = %s", got)
	}
	// Tệp cùng id ghi đè bản dựng sẵn; deprecated thì tắt bản dựng sẵn.
	write(t, dir, "guidance/bint-mcp-tools.md", "---\n"+comp("bint-mcp-tools", "deprecated")+"---\nx\n")
	lib = LoadDir(dir)
	if got := ids(ModeOn); got != "v" {
		t.Errorf("tắt bản dựng sẵn: on = %s", got)
	}
	if got := ids(ModeOff); got != "bint-mcp-tools" {
		t.Errorf("off vẫn giữ bản dựng sẵn: %s", got)
	}
	if lib.Select(ModeOn).Files().Empty() {
		t.Error("Files phải gồm tệp verified")
	}
}

func TestRenderGuidanceAndDomain(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "guidance/glob.md", "---\n"+comp("glob", "verified")+"title: Quy trình chung\n---\nLàm A rồi B.\n")
	write(t, dir, "guidance/dom.md", "---\nid: dom\nscope: \"domain:internal/db/**\"\nstatus: verified\norigin: agent\nversion: 1\n---\nVùng DB: chạy migrate.\n")
	write(t, dir, "scripts/t.yml", comp("t", "verified")+"description: Chạy test\nusage: scripts/t.sh X\n")
	write(t, dir, "scripts/t.sh", "#!/bin/sh\n")
	set := LoadDir(dir).Select(ModeOn).Files()
	got := set.RenderGuidance(RenderOpts{MCP: true})
	if !strings.Contains(got, "\n## Quy trình chung\n\nLàm A rồi B.\n") || !strings.Contains(got, "`.bint/harness/scripts/t.sh` — Chạy test Cách dùng: `scripts/t.sh X`.") {
		t.Fatalf("render:\n%s", got)
	}
	if strings.Contains(got, "Vùng DB") {
		t.Fatal("guidance riêng không vào khối chung")
	}
	d := set.DomainGuidance([]string{"internal/db/x.go"}, RenderOpts{}, nil)
	if len(d) != 1 || d[0].ID != "dom" || !strings.Contains(RenderDomain(d), "Vùng DB") {
		t.Fatalf("domain: %+v", d)
	}
	if len(set.DomainGuidance([]string{"internal/app/x.go"}, RenderOpts{}, nil)) != 0 {
		t.Fatal("không khớp vùng")
	}
	if len(set.DomainGuidance([]string{"internal/db/x.go"}, RenderOpts{}, map[string]bool{"dom": true})) != 0 {
		t.Fatal("đã chèn thì không chèn lại")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cs := []Component{
		{Kind: KindGuidance, Meta: Meta{ID: "g", Scope: "domain:a/**", Status: StatusCandidate, Origin: OriginAgent, Version: 1}, Title: "T", Requires: []string{"mcp"}, Body: "Thân\n\nhai đoạn"},
		{Kind: KindGuardrail, Meta: Meta{ID: "r", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginAgent, Version: 2, ApprovedBy: "x"},
			On: OnPreBash, When: When{Command: `git push`}, Action: ActAsk, Message: "Hỏi trước khi push"},
		{Kind: KindScript, Meta: Meta{ID: "s", Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginHuman, Version: 1}, Description: "d", Source: []byte("#!/bin/sh\n")},
		{Kind: KindStrategy, Meta: Meta{ID: StrategyID, Scope: ScopeGlobal, Status: StatusCandidate, Origin: OriginHuman, Version: 1},
			Context: &StrategyContext{MaxTokens: ip(900), Interfaces: &StrategyInterfaces{MCP: bp(false)}}},
	}
	for _, c := range cs {
		if _, err := Save(dir, c); err != nil {
			t.Fatalf("Save %s: %v", c.ID, err)
		}
	}
	lib := LoadDir(dir)
	if len(lib.Problems) != 0 || len(lib.Components) != 4 {
		t.Fatalf("nạp lại: %v %d", lib.Problems, len(lib.Components))
	}
	g, _ := lib.Find(KindGuidance, "g")
	if g.Body != "Thân\n\nhai đoạn" || g.Scope != "domain:a/**" || g.Requires[0] != "mcp" {
		t.Fatalf("guidance: %+v", g)
	}
	r, _ := lib.Find(KindGuardrail, "r")
	if r.When.Command != "git push" || r.ApprovedBy != "x" || r.Version != 2 {
		t.Fatalf("guardrail: %+v", r)
	}
	st, _ := lib.Find(KindStrategy, StrategyID)
	if *st.Context.MaxTokens != 900 || *st.Context.Interfaces.MCP != false || st.Context.Interfaces.PathHooks != nil {
		t.Fatalf("strategy: %+v", st.Context)
	}
	if _, err := Save(dir, Component{Kind: KindGuardrail}); err == nil {
		t.Fatal("Save phải từ chối thành phần không hợp lệ")
	}
	if err := Remove(dir, cs[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "s.sh")); err == nil {
		t.Fatal("Remove phải xoá .sh")
	}
}

func bp(b bool) *bool { return &b }

func gr(id, on string, w When, u Unless, act string) Component {
	return Component{Kind: KindGuardrail, Meta: Meta{ID: id, Scope: ScopeGlobal, Status: StatusVerified, Origin: OriginHuman, Version: 1},
		On: on, When: w, Unless: u, Action: act, Message: "msg " + id}
}

func TestEvaluateTable(t *testing.T) {
	all := Caps{Deny: true, Ask: true, Remind: true}
	edited := func(paths ...string) *State {
		st := newState()
		for _, p := range paths {
			st.Apply(Record{T: RecEdit, P: p})
		}
		return st
	}
	cases := []struct {
		name string
		g    Component
		ev   Event
		st   *State
		caps Caps
		want string // "fired:<action>" | "block" | "none" | "unless" | "once" | "bypass" | "unsupported"
	}{
		{"stop remind đã sửa chưa test", gr("a", OnStop, When{Edited: "src/**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop}, edited("src/a.go"), all, "block"},
		{"stop: không sửa gì → không đủ điều kiện", gr("a", OnStop, When{Edited: "src/**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop}, edited("docs/a.md"), all, "none"},
		{"stop: đã chạy test sau sửa", gr("a", OnStop, When{Edited: "src/**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop}, func() *State {
				s := edited("src/a.go")
				s.Apply(Record{T: RecCmd, C: "go test ./...", Test: true})
				return s
			}(), all, "unless"},
		{"stop: test chạy TRƯỚC lần sửa cuối không tính", gr("a", OnStop, When{Edited: "src/**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop}, func() *State {
				s := newState()
				s.Apply(Record{T: RecCmd, C: "go test", Test: true})
				s.Apply(Record{T: RecEdit, P: "src/a.go"})
				return s
			}(), all, "block"},
		{"stop remind chỉ một lần", gr("a", OnStop, When{Edited: "**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop}, func() *State { s := edited("a.go"); s.Apply(Record{T: RecStopBlock, ID: "a"}); return s }(), all, "once"},
		{"stop remind: stop_hook_active", gr("a", OnStop, When{Edited: "**"}, Unless{Ran: RanTest}, ActRemind),
			Event{On: OnStop, StopHookActive: true}, edited("a.go"), all, "once"},
		{"stop deny chặn lần 3", gr("a", OnStop, When{Edited: "**"}, Unless{Ran: RanTest}, ActDeny),
			Event{On: OnStop, StopHookActive: true}, func() *State {
				s := edited("a.go")
				s.Apply(Record{T: RecStopBlock, ID: "a"})
				s.Apply(Record{T: RecStopBlock, ID: "a"})
				return s
			}(), all, "block"},
		{"stop deny vượt trần → bypass", gr("a", OnStop, When{Edited: "**"}, Unless{Ran: RanTest}, ActDeny),
			Event{On: OnStop}, func() *State {
				s := edited("a.go")
				for i := 0; i < MaxStopDenies; i++ {
					s.Apply(Record{T: RecStopBlock, ID: "a"})
				}
				return s
			}(), all, "bypass"},
		{"stop ask hạ xuống remind", gr("a", OnStop, When{Edited: "**"}, Unless{}, ActAsk),
			Event{On: OnStop}, edited("a.go"), Caps{Deny: true, Remind: true}, "block"},
		{"stop không hỗ trợ (Codex)", gr("a", OnStop, When{Edited: "**"}, Unless{}, ActRemind),
			Event{On: OnStop}, edited("a.go"), Caps{}, "unsupported"},
		{"pre_bash deny khớp lệnh", gr("a", OnPreBash, When{Command: `git push`}, Unless{}, ActDeny),
			Event{On: OnPreBash, Command: "git push origin main"}, nil, all, "fired:deny"},
		{"pre_bash không khớp", gr("a", OnPreBash, When{Command: `git push`}, Unless{}, ActDeny),
			Event{On: OnPreBash, Command: "git status"}, nil, all, "none"},
		{"pre_bash deny hạ → ask (không có deny)", gr("a", OnPreBash, When{Command: `rm -rf`}, Unless{}, ActDeny),
			Event{On: OnPreBash, Command: "rm -rf x"}, nil, Caps{Ask: true, Remind: true}, "fired:ask"},
		{"pre_bash ask hạ → remind (Codex)", gr("a", OnPreBash, When{Command: `rm -rf`}, Unless{}, ActAsk),
			Event{On: OnPreBash, Command: "rm -rf x"}, nil, Caps{Deny: true, Remind: true}, "fired:remind"},
		{"pre_bash remind bị bỏ (Antigravity)", gr("a", OnPreBash, When{Command: `rm -rf`}, Unless{}, ActRemind),
			Event{On: OnPreBash, Command: "rm -rf x"}, nil, Caps{Deny: true, Ask: true}, "unsupported"},
		{"pre_edit remind theo paths", gr("a", OnPreEdit, When{Paths: "internal/db/**"}, Unless{}, ActRemind),
			Event{On: OnPreEdit, Paths: []string{"internal/db/x.go"}}, nil, all, "fired:remind"},
		{"pre_edit remind chỉ một lần", gr("a", OnPreEdit, When{Paths: "internal/db/**"}, Unless{}, ActRemind),
			Event{On: OnPreEdit, Paths: []string{"internal/db/x.go"}}, func() *State { s := newState(); s.Apply(Record{T: RecFired, ID: "a"}); return s }(), all, "once"},
		{"pre_edit ask lặp mỗi lần", gr("a", OnPreEdit, When{Paths: "internal/db/**"}, Unless{}, ActAsk),
			Event{On: OnPreEdit, Paths: []string{"internal/db/x.go"}}, func() *State { s := newState(); s.Apply(Record{T: RecFired, ID: "a"}); return s }(), all, "fired:ask"},
		{"scope domain khớp tệp", func() Component {
			g := gr("a", OnPreEdit, When{}, Unless{}, ActRemind)
			g.Scope = "domain:internal/db/**"
			return g
		}(), Event{On: OnPreEdit, Paths: []string{"internal/db/x.go"}}, nil, all, "fired:remind"},
		{"scope domain không khớp", func() Component {
			g := gr("a", OnPreEdit, When{}, Unless{}, ActRemind)
			g.Scope = "domain:internal/db/**"
			return g
		}(), Event{On: OnPreEdit, Paths: []string{"internal/app/x.go"}}, nil, all, "none"},
		{"scope domain ở stop theo tệp đã sửa", func() Component {
			g := gr("a", OnStop, When{}, Unless{}, ActRemind)
			g.Scope = "domain:internal/db/**"
			return g
		}(), Event{On: OnStop}, edited("internal/db/x.go"), all, "block"},
		{"sự kiện khác", gr("a", OnStop, When{}, Unless{}, ActRemind), Event{On: OnPreBash}, nil, all, "none"},
		{"unless.ran regexp", gr("a", OnPreEdit, When{Paths: "**"}, Unless{Ran: `make check`}, ActRemind),
			Event{On: OnPreEdit, Paths: []string{"x"}}, func() *State { s := newState(); s.Apply(Record{T: RecCmd, C: "make check"}); return s }(), all, "unless"},
	}
	for _, c := range cases {
		ds := Evaluate([]Component{c.g}, c.ev, c.st, c.caps)
		got := "none"
		if len(ds) == 1 {
			d := ds[0]
			switch {
			case d.Block:
				got = "block"
			case d.Fired:
				got = "fired:" + d.Action
			default:
				got = d.Reason
			}
		} else if len(ds) > 1 {
			t.Errorf("%s: %d quyết định", c.name, len(ds))
		}
		if got != c.want {
			t.Errorf("%s: %s, muốn %s", c.name, got, c.want)
		}
	}
}

func TestFollowed(t *testing.T) {
	g := gr("a", OnStop, When{Edited: "**"}, Unless{Ran: RanTest}, ActRemind)
	st := newState()
	st.Apply(Record{T: RecEdit, P: "a.go"})
	st.Apply(Record{T: RecFired, ID: "a"})
	if got := Followed([]Component{g}, st); len(got) != 0 {
		t.Fatalf("chưa chạy test: %v", got)
	}
	st.Apply(Record{T: RecCmd, C: "go test ./...", Test: true})
	if got := Followed([]Component{g}, st); len(got) != 1 || got[0] != "a" {
		t.Fatalf("đã làm theo: %v", got)
	}
	st.Apply(Record{T: RecFollowed, ID: "a"})
	if got := Followed([]Component{g}, st); len(got) != 0 {
		t.Fatalf("không ghi hai lần: %v", got)
	}
}

func TestSessionStateConcurrentAppend(t *testing.T) {
	p := SessionPath(t.TempDir(), "claude", "sess-1")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Append(p, Record{T: RecEdit, P: "f.go"}, Record{T: RecCmd, C: "go test", Test: true}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	st := ReadState(p)
	if len(st.Edits) != 40 || len(st.Cmds) != 40 {
		t.Fatalf("edits=%d cmds=%d, muốn 40/40 (dòng bị xen kẽ?)", len(st.Edits), len(st.Cmds))
	}
	if got := st.EditedPaths(); len(got) != 1 {
		t.Fatalf("EditedPaths = %v", got)
	}
	if p2 := SessionPath(filepath.Dir(filepath.Dir(p)), "claude", "sess-2"); p2 == p {
		t.Fatal("phiên khác nhau phải khác tệp")
	}
	// Tệp thiếu/hỏng không gây lỗi.
	os.WriteFile(p, []byte("{hỏng\n{\"t\":\"edit\",\"p\":\"a\"}\n"), 0o600)
	if st := ReadState(p); len(st.Edits) != 1 {
		t.Fatalf("bỏ dòng hỏng: %+v", st.Edits)
	}
	if st := ReadState(filepath.Join(t.TempDir(), "none")); len(st.Edits) != 0 {
		t.Fatal("thiếu tệp = rỗng")
	}
}

// Candidate trùng id với bản dựng sẵn không được làm mất bản dựng sẵn ở ModeOn (nó chưa có
// hiệu lực); ModeAll thì thay thế; verified luôn thay; deprecated luôn tắt.
func TestSelectCandidateDoesNotShadowBuiltin(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "guidance/bint-mcp-tools.md", "---\n"+comp("bint-mcp-tools", "candidate")+"---\nbản candidate\n")
	lib := LoadDir(dir)
	bodyOf := func(m Mode) string {
		var out []string
		for _, c := range lib.Select(m).Guidance() {
			out = append(out, c.ID+":"+strings.Fields(c.Body)[0])
		}
		return strings.Join(out, ",")
	}
	if got := bodyOf(ModeOn); got != "bint-mcp-tools:##" {
		t.Errorf("on: candidate trùng id phải để nguyên bản dựng sẵn: %s", got)
	}
	if got := lib.Select(ModeOn).RenderGuidance(RenderOpts{MCP: true}); got != oldBintToolsNote {
		t.Errorf("on: mất ghi chú MCP: %q", got)
	}
	if got := bodyOf(ModeAll); got != "bint-mcp-tools:bản" {
		t.Errorf("all: candidate thay bản dựng sẵn: %s", got)
	}
	write(t, dir, "guidance/bint-mcp-tools.md", "---\n"+comp("bint-mcp-tools", "verified")+"---\nbản verified\n")
	lib = LoadDir(dir)
	if got := bodyOf(ModeOn); got != "bint-mcp-tools:bản" {
		t.Errorf("verified thay bản dựng sẵn: %s", got)
	}
}

// Tệp trạng thái vượt trần: đọc phần đuôi, đánh dấu Truncated, và guardrail deny ở stop không
// chặn nữa (không đếm được số lần đã chặn → sẽ nhốt agent).
func TestReadStateOverCapReadsTailAndStopDoesNotBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	// Phần đầu: một stop_block đã ghi, rồi lấp đầy quá trần bằng bản ghi lệnh.
	recs := []Record{{T: RecStopBlock, ID: "g"}}
	big := strings.Repeat("x", 400)
	for i := 0; i < maxStateBytes/400+10; i++ {
		recs = append(recs, Record{T: RecCmd, C: big})
	}
	recs = append(recs, Record{T: RecEdit, P: "src/a.go"})
	if err := Append(path, recs...); err != nil {
		t.Fatal(err)
	}
	st := ReadState(path)
	if !st.Truncated {
		t.Fatal("tệp vượt trần phải đánh dấu Truncated")
	}
	if len(st.EditedPaths()) != 1 || st.EditedPaths()[0] != "src/a.go" {
		t.Fatalf("bản ghi cuối (ở đuôi) phải được đọc: %v", st.EditedPaths())
	}
	g := gr("g", OnStop, When{Edited: "src/**"}, Unless{Ran: RanTest}, ActDeny)
	ds := Evaluate([]Component{g}, Event{On: OnStop}, st, Caps{Deny: true, Remind: true})
	if len(ds) != 1 || ds[0].Block || ds[0].Fired || ds[0].Reason != "truncated" {
		t.Fatalf("Truncated: không được chặn kết thúc: %+v", ds)
	}
	// Đối chứng: tệp nhỏ vẫn chặn.
	small := filepath.Join(t.TempDir(), "s.jsonl")
	Append(small, Record{T: RecEdit, P: "src/a.go"})
	ds = Evaluate([]Component{g}, Event{On: OnStop}, ReadState(small), Caps{Deny: true, Remind: true})
	if len(ds) != 1 || !ds[0].Block {
		t.Fatalf("tệp nhỏ phải chặn: %+v", ds)
	}
}

// Khoảng giá trị của strategy phải trùng config.Validate (path_max_items 1..30, global_k 0..10):
// nếu không eval áp strategy mà phiên thật lặng lẽ bỏ qua.
func TestLoadStrategyOutOfRangeIsProblem(t *testing.T) {
	for name, ctx := range map[string]string{
		"path_max_items 40": "context:\n  path_max_items: 40\n",
		"global_k 12":       "context:\n  lessons: { global_k: 12 }\n",
	} {
		dir := t.TempDir()
		write(t, dir, "strategy.yml", comp("strategy", "verified")+ctx)
		lib := LoadDir(dir)
		if len(lib.Components) != 0 || len(lib.Problems) != 1 {
			t.Errorf("%s: thành phần=%d vấn đề=%v, muốn 0 thành phần và 1 vấn đề", name, len(lib.Components), lib.Problems)
		}
	}
	dir := t.TempDir()
	write(t, dir, "strategy.yml", comp("strategy", "verified")+"context:\n  path_max_items: 30\n  lessons: { global_k: 10 }\n")
	if lib := LoadDir(dir); len(lib.Components) != 1 || len(lib.Problems) != 0 {
		t.Errorf("biên 30/10 phải hợp lệ: %v", lib.Problems)
	}
}
