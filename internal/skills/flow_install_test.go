package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/fsafe"
	"gopkg.in/yaml.v3"
)

const (
	flowClaude = ".claude/skills/tdd/SKILL.md"
	flowAgents = ".agents/skills/tdd/SKILL.md"
)

func writeFlow(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, ".bint", "flows", name+".yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasConflict(outs []Outcome, path, detail string) bool {
	for _, o := range outs {
		if o.Path == path && o.Result == fsafe.Conflict && strings.Contains(o.Detail, detail) {
			return true
		}
	}
	return false
}

func TestInstallFlowsPublishesToBothTargets(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{flowClaude, flowAgents} {
		if r, _ := resultOf(outs, rel); r != fsafe.Created {
			t.Fatalf("%s: %v", rel, r)
		}
	}
	claude := readFile(t, root, flowClaude)
	if !strings.Contains(claude, "disable-model-invocation: true") || !strings.Contains(claude, "bint run tdd --task <id> --json") ||
		!strings.Contains(claude, "--workflow tdd") {
		t.Fatalf("claude:\n%s", claude)
	}
	if got := readFile(t, root, flowAgents); strings.Contains(got, "disable-model-invocation") || !strings.HasPrefix(got, "---\nname: tdd\n") {
		t.Fatalf("agents chỉ giữ name/description:\n%s", got)
	}
	lock, _ := ReadLock(root)
	e := lock.Flows["tdd"]
	if len(e.Published) != 2 || e.SHA256 == "" || e.Variants[FormatAgents] == "" {
		t.Fatalf("lock.Flows: %+v", e)
	}
	// Lần hai: không đổi gì, lock giữ nguyên.
	outs, err = Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resultOf(outs, flowClaude); r != fsafe.Unchanged {
		t.Fatalf("lần hai: %v", r)
	}
	// Đổi flow → cập nhật cả hai target.
	writeFlow(t, root, "tdd", strings.Replace(tddSrc, "Viết kiểm thử đỏ trước", "Viết test đỏ trước", 1))
	outs, _ = Install(opts)
	if r, _ := resultOf(outs, flowAgents); r != fsafe.Updated {
		t.Fatalf("flow đổi: %v", r)
	}
	if !strings.Contains(readFile(t, root, flowClaude), "Viết test đỏ trước") {
		t.Fatal("nội dung chưa cập nhật")
	}
}

// Mỗi flow tuỳ biến dùng skill tuỳ biến của bước (đường dẫn đầy đủ qua Install, không qua skill gốc).
func TestInstallFlowsWithCustomStepSkill(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "red-first", "SKILL.md", goodSkill("red-first"))
	writeFlow(t, root, "tdd", "schema: 1\nname: tdd\nsteps:\n  - {id: red, kind: implement, skill: red-first}\n")
	outs, err := Install(customOpts(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := resultOf(outs, flowClaude); r != fsafe.Created {
		t.Fatalf("%v", r)
	}
	if !strings.Contains(readFile(t, root, flowClaude), "| red | implement | red-first | implementer |") {
		t.Fatal("bảng bước")
	}
}

func TestInstallFlowsLintFailureKeepsPublished(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	good := readFile(t, root, flowClaude)
	before, _ := ReadLock(root)
	writeFlow(t, root, "tdd", strings.Replace(tddSrc, "skill: b-review", "skill: khong-co", 1))
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !hasConflict(outs, ".bint/flows/tdd.yml", "giữ bản đã xuất bản") || !hasConflict(outs, ".bint/flows/tdd.yml", "khong-co") {
		t.Fatalf("phải báo xung đột nêu lỗi: %+v", outs)
	}
	if readFile(t, root, flowClaude) != good {
		t.Fatal("bản đã xuất bản bị đổi")
	}
	after, _ := ReadLock(root)
	if a, b := mustYAML(t, after.Flows), mustYAML(t, before.Flows); a != b {
		t.Fatalf("mục lock bị đổi:\n%s\n%s", b, a)
	}
	// Tệp YAML hỏng cũng vậy.
	writeFlow(t, root, "tdd", "schema: [")
	outs, _ = Install(opts)
	if !hasConflict(outs, ".bint/flows/tdd.yml", "giữ bản đã xuất bản") || readFile(t, root, flowClaude) != good {
		t.Fatalf("tệp hỏng: %+v", outs)
	}
	// Flow lỗi chưa từng xuất bản thì không có gì được tạo.
	writeFlow(t, root, "moi", "schema: 1\nname: moi\nsteps: []\n")
	Install(opts)
	if exists(root, ".claude/skills/moi/SKILL.md") {
		t.Fatal("flow lỗi không được xuất bản")
	}
}

func mustYAML(t *testing.T, v any) string {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallFlowsRemoveFlowUnpublishes(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, ".bint/flows/tdd.yml"))
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{flowClaude, flowAgents} {
		if r, _ := resultOf(outs, rel); r != fsafe.Removed || exists(root, rel) {
			t.Fatalf("%s: %v", rel, r)
		}
	}
	if exists(root, ".claude/skills/tdd") {
		t.Fatal("thư mục rỗng phải được dọn")
	}
	if lock, _ := ReadLock(root); len(lock.Flows) != 0 {
		t.Fatalf("lock: %+v", lock.Flows)
	}
}

func TestInstallFlowsHandEditedConflictAndForce(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, flowClaude), []byte("sửa tay"), 0o644)
	// Máy khác (manifest trống): chỉ dựa vào core.lock vẫn nhận ra sửa tay.
	opts.Manifest, _ = fsafe.LoadManifest(filepath.Join(t.TempDir(), "m2.json"))

	// 1) Sửa nguồn rồi sync: xung đột ở bản sửa tay, bản agents (chưa sửa) vẫn cập nhật.
	writeFlow(t, root, "tdd", strings.Replace(tddSrc, "Viết kiểm thử đỏ trước", "Viết test đỏ trước", 1))
	outs, _ := Install(opts)
	if !hasConflict(outs, flowClaude, ".bint/flows/tdd.yml") || readFile(t, root, flowClaude) != "sửa tay" {
		t.Fatalf("phải xung đột: %+v", outs)
	}
	if r, _ := resultOf(outs, flowAgents); r != fsafe.Updated {
		t.Fatalf("agents: %v", r)
	}
	if lock, _ := ReadLock(root); len(lock.Flows["tdd"].Published) != 2 {
		t.Fatalf("lock phải giữ rel xung đột: %+v", lock.Flows)
	}

	// 2) --force ghi đè kèm sao lưu.
	force := opts
	force.Force = true
	outs, _ = Install(force)
	var backed bool
	for _, o := range outs {
		backed = backed || (o.Path == flowClaude && o.Result == fsafe.Updated && o.Backup != "")
	}
	if !backed || !strings.Contains(readFile(t, root, flowClaude), "Viết test đỏ trước") {
		t.Fatalf("--force: %+v", outs)
	}

	// 3) Sửa tay rồi xoá flow: xung đột, giữ tệp và mục lock; bản không sửa tay bị gỡ.
	os.WriteFile(filepath.Join(root, flowClaude), []byte("sửa tay lần hai"), 0o644)
	os.Remove(filepath.Join(root, ".bint/flows/tdd.yml"))
	outs, _ = Install(opts)
	if !hasConflict(outs, flowClaude, "bị sửa tay") || readFile(t, root, flowClaude) != "sửa tay lần hai" {
		t.Fatalf("xoá flow đã sửa tay: %+v", outs)
	}
	if r, _ := resultOf(outs, flowAgents); r != fsafe.Removed {
		t.Fatalf("agents: %v", r)
	}
	lock, _ := ReadLock(root)
	if got := lock.Flows["tdd"].Published; len(got) != 1 || got[0] != flowClaude {
		t.Fatalf("lock giữ rel xung đột: %+v", lock.Flows)
	}
}

// Tên flow trùng skill gốc, skill tuỳ biến (kể cả đang lỗi lint) hoặc skill tiến hoá: không xuất bản.
func TestInstallFlowsRejectsNamesOfOtherSkills(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "mine", "SKILL.md", goodSkill("mine"))
	writeCustom(t, root, "vo-hong", "SKILL.md", "hỏng\n")
	if err := newEvolved("core-retry", goodDesc, "1. Gọi Reset.", "internal/core/**").Save(root); err != nil {
		t.Fatal(err)
	}
	step := "steps:\n  - {id: build, kind: implement, skill: b-feat}\n"
	for _, n := range []string{"mine", "vo-hong", "core-retry", "b-do", "b-mine", "overlays"} {
		writeFlow(t, root, n, "schema: 1\nname: "+n+"\n"+step)
	}
	opts := customOpts(t, root)
	opts.Files = []string{"internal/core/x.go"}
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	custom := map[string]string{"mine": readFile(t, root, ".claude/skills/mine/SKILL.md")}
	if !strings.Contains(custom["mine"], "name: mine") || strings.Contains(custom["mine"], "bint run mine --task") {
		t.Fatalf("skill tuỳ biến không được bị flow đè:\n%s", custom["mine"])
	}
	if strings.Contains(readFile(t, root, ".claude/skills/core-retry/SKILL.md"), "bint run core-retry") {
		t.Fatal("skill tiến hoá không được bị flow đè")
	}
	if exists(root, ".claude/skills/vo-hong/SKILL.md") || exists(root, ".claude/skills/overlays") {
		t.Fatal("flow trùng tên skill lỗi lint/tên dành riêng không được xuất bản")
	}
	if !strings.Contains(readFile(t, root, ".claude/skills/b-do/SKILL.md"), "b-do — điều phối") {
		t.Fatal("skill gốc b-do bị đổi")
	}
	for _, n := range []string{"mine", "vo-hong", "core-retry", "b-do", "b-mine", "overlays"} {
		if !hasConflict(outs, ".bint/flows/"+n+".yml", "") {
			t.Errorf("flow %s phải báo xung đột: %+v", n, outs)
		}
	}
	if lock, _ := ReadLock(root); len(lock.Flows) != 0 {
		t.Fatalf("lock.Flows phải rỗng: %+v", lock.Flows)
	}
}

// Skill tuỳ biến được tạo trùng tên flow đã xuất bản: skill thắng, mục flows bị bỏ khỏi lock mà không gỡ tệp.
func TestInstallFlowsCustomSkillTakesOverPublishedName(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	writeCustom(t, root, "tdd", "SKILL.md", goodSkill("tdd"))
	outs, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, flowClaude); got != goodSkill("tdd") {
		t.Fatalf("skill tuỳ biến phải thắng:\n%s", got)
	}
	lock, _ := ReadLock(root)
	if _, ok := lock.Flows["tdd"]; ok || len(lock.Custom["tdd"].Published) != 2 {
		t.Fatalf("lock: flows=%+v custom=%+v", lock.Flows, lock.Custom)
	}
	if !hasConflict(outs, ".bint/flows/tdd.yml", "trùng một skill khác") {
		t.Fatalf("phải báo xung đột: %+v", outs)
	}
}

// GĐ5 review .1: tên flow đã xuất bản bị một skill CHƯA xuất bản chiếm (tuỳ biến lỗi lint, tiến hoá không active) thì skill
// điều phối cũ vẫn thuộc mục flows; xoá flow ở lần sync sau thì gỡ được, không thành tệp mồ côi.
func TestInstallFlowsNameTakenByUnpublishedSkillKeepsEntryForRemoval(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"tuỳ biến lỗi lint": func(t *testing.T, root string) { writeCustom(t, root, "tdd", "SKILL.md", "hỏng\n") },
		"tiến hoá không active": func(t *testing.T, root string) {
			if err := newEvolved("tdd", goodDesc, "1. Gọi Reset.", "internal/core/**").Save(root); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, claim := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFlow(t, root, "tdd", tddSrc)
			opts := customOpts(t, root)
			opts.Files = []string{"docs/x.md"} // không khớp internal/core/** nên skill tiến hoá không active
			if _, err := Install(opts); err != nil {
				t.Fatal(err)
			}
			if !exists(root, flowClaude) || !exists(root, flowAgents) {
				t.Fatal("flow phải được xuất bản ở lần đầu")
			}
			claim(t, root)
			outs, err := Install(opts)
			if err != nil {
				t.Fatal(err)
			}
			if !hasConflict(outs, ".bint/flows/tdd.yml", "trùng một skill khác") {
				t.Fatalf("phải báo xung đột: %+v", outs)
			}
			lock, _ := ReadLock(root)
			if len(lock.Flows["tdd"].Published) != 2 {
				t.Fatalf("tệp chưa ai nhận phải còn trong mục flows: %+v", lock.Flows)
			}
			if !exists(root, flowClaude) || !exists(root, flowAgents) {
				t.Fatal("không được gỡ tệp khi flow còn")
			}
			// Xoá flow: lần sync sau gỡ skill điều phối cũ và mục lock.
			if err := os.Remove(filepath.Join(root, ".bint/flows/tdd.yml")); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(opts); err != nil {
				t.Fatal(err)
			}
			if exists(root, flowClaude) || exists(root, flowAgents) {
				t.Fatal("skill điều phối cũ thành tệp mồ côi")
			}
			if lock, _ := ReadLock(root); len(lock.Flows) != 0 {
				t.Fatalf("lock.Flows phải rỗng: %+v", lock.Flows)
			}
		})
	}
}

// Xoá flow X và tạo skill tuỳ biến X trong cùng một lần Install: bản tuỳ biến còn, không bị gỡ nhầm.
func TestInstallFlowsDeletedFlowDoesNotRemoveNewCustomSkill(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, ".bint/flows/tdd.yml"))
	writeCustom(t, root, "tdd", "SKILL.md", goodSkill("tdd"))
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, root, flowClaude); got != goodSkill("tdd") {
		t.Fatalf("bản tuỳ biến bị gỡ/đè:\n%s", got)
	}
	if !exists(root, flowAgents) {
		t.Fatal("bản agents bị gỡ nhầm")
	}
	lock, _ := ReadLock(root)
	if _, ok := lock.Flows["tdd"]; ok || len(lock.Custom["tdd"].Published) != 2 {
		t.Fatalf("lock: flows=%+v custom=%+v", lock.Flows, lock.Custom)
	}
}

// GĐ1 .7: xoá skill tuỳ biến X rồi tạo skill tiến hoá X trong cùng một lần Install: sync không gỡ bản vừa xuất bản.
func TestInstallCustomDeletedDoesNotRemoveNewEvolvedOfSameName(t *testing.T) {
	root := t.TempDir()
	writeCustom(t, root, "core-retry", "SKILL.md", goodSkill("core-retry"))
	opts := customOpts(t, root)
	opts.Files = []string{"internal/core/x.go"}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".bint/skills/core-retry")); err != nil {
		t.Fatal(err)
	}
	ev := newEvolved("core-retry", goodDesc, "1. Gọi Reset.", "internal/core/**")
	if err := ev.Save(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".claude/skills/core-retry/SKILL.md", ".agents/skills/core-retry/SKILL.md"} {
		if !strings.Contains(readFile(t, root, rel), "1. Gọi Reset.") {
			t.Fatalf("%s phải là nội dung tiến hoá:\n%s", rel, readFile(t, root, rel))
		}
	}
	lock, _ := ReadLock(root)
	if len(lock.Evolved["core-retry"].Published) != 2 {
		t.Fatalf("lock.Evolved: %+v", lock.Evolved)
	}
	if _, ok := lock.Custom["core-retry"]; ok {
		t.Fatalf("lock.Custom không còn mục: %+v", lock.Custom)
	}
}

// core.lock viết trước GĐ5 (không có `flows`) đọc được và sync không làm hỏng nó.
func TestInstallFlowsLegacyLockReadable(t *testing.T) {
	root := t.TempDir()
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	lock, _ := ReadLock(root)
	if len(lock.Flows) != 0 || strings.Contains(readFile(t, root, LockRel), "flows:") {
		t.Fatalf("không có flow thì lock không có mục flows:\n%s", readFile(t, root, LockRel))
	}
	writeFlow(t, root, "tdd", tddSrc)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, root, LockRel), "flows:") {
		t.Fatal("thiếu mục flows")
	}
}

// Tắt một harness không làm mất mục đã xuất bản ở target đó.
func TestInstallFlowsKeepsEntryOfDisabledTarget(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "tdd", tddSrc)
	opts := customOpts(t, root)
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	opts.Targets = []Target{ClaudeTarget}
	if _, err := Install(opts); err != nil {
		t.Fatal(err)
	}
	lock, _ := ReadLock(root)
	if e := lock.Flows["tdd"]; len(e.Published) != 2 || e.Variants[FormatAgents] == "" {
		t.Fatalf("lock phải giữ mục của target đã tắt: %+v", e)
	}
	if !exists(root, flowAgents) {
		t.Fatal("tệp ở target đã tắt không được đụng tới")
	}
}
