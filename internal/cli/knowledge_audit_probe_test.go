// Loại đầu vào: mục stale, mục verified có fingerprint lệch, mục verified chỉ dời dòng, mục verified có khối chuyển tệp, mục verified còn khớp, phiên agent gọi audit, nhận định valid, nhận định outdated, nhận định fix-evidence kèm bằng chứng đề xuất, skill hướng dẫn nhận định, ghi mô hình của nhận định
package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bint/internal/testutil"
)

// Thăm dò hộp đen cho `bint knowledge audit`, `revalidate` mở rộng và skill rà soát tri thức.
// Chỉ đi qua lệnh CLI và các tệp mà Bint ghi ra.

const probeMarker = "\t\t\tif cfg.Security.AllowExternalAgents && !cfg.Security.AllowNetworkInEvals {"

type probeRepo struct {
	root string
	ids  map[string]string // tên → ID mục
}

func probeItemFile(t *testing.T, root, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ".bint", "knowledge", "items", id+".md"))
	if err != nil {
		t.Fatalf("đọc tệp mục %s: %v", id, err)
	}
	return string(b)
}

func probeStatus(t *testing.T, root, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^status:\s*(\S+)`).FindStringSubmatch(probeItemFile(t, root, id))
	if m == nil {
		t.Fatalf("tệp mục %s không có status", id)
	}
	return m[1]
}

// probeHumanAdd ghi một mục warning do người ghi (verified) với bằng chứng ev.
func probeHumanAdd(t *testing.T, root, title, body, ev string) string {
	t.Helper()
	asHuman(t)
	out, code := run(t, root, body+"\n", "knowledge", "add", "--type", "warning", "--title", title,
		"--paths", "internal/**", "--evidence", ev, "--json", "-")
	var added struct {
		Saved  bool   `json:"saved"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || !added.Saved || added.ID == "" {
		t.Fatalf("human add %q: %d\n%s", title, code, out)
	}
	if st := probeStatus(t, root, added.ID); st != "verified" {
		t.Fatalf("mục người ghi phải verified, được %s", st)
	}
	return added.ID
}

// newProbeRepo dựng repo có năm mục verified trỏ tới năm tệp khác nhau, rồi biến đổi mã
// để có: stale, verified lệch fingerprint, chỉ dời dòng, chuyển tệp, còn khớp.
func newProbeRepo(t *testing.T) probeRepo {
	t.Helper()
	testutil.Isolate(t)
	asHuman(t)
	filler := func(n int, tag string) string { return e2eFiller(n, tag) }
	root := testutil.NewRepo(t, map[string]string{
		"go.mod":                 "module demo\n",
		"internal/cli/stale.go":  filler(10, "stale") + "\tstaleCheck := legacyGate()\n" + filler(5, "s2"),
		"internal/cli/drift.go":  filler(10, "drift") + "\tdriftCheck := oldBehaviour()\n" + filler(5, "d2"),
		"internal/cli/shift.go":  filler(10, "shift") + "\tshiftCheck := stableBehaviour()\n" + filler(5, "h2"),
		"internal/cli/eval.go":   filler(10, "cli") + probeMarker + "\n" + filler(5, "cli2"),
		"internal/cli/intact.go": filler(10, "intact") + "\tintactCheck := keep()\n" + filler(5, "i2"),
		"internal/app/doc.go":    "package app\n",
	})
	testutil.Git(t, root, "config", "user.name", "Rev")
	testutil.Git(t, root, "config", "user.email", "rev@example.com")
	if out, code := run(t, root, "", "init", "--no-hooks"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	p := probeRepo{root: root, ids: map[string]string{}}
	p.ids["stale"] = probeHumanAdd(t, root, "Cổng legacy", "legacyGate chặn lượt chạy cũ.", "internal/cli/stale.go:11")
	p.ids["drift"] = probeHumanAdd(t, root, "Hành vi cũ", "oldBehaviour phải gọi trước khi ghi.", "internal/cli/drift.go:11")
	p.ids["shift"] = probeHumanAdd(t, root, "Hành vi ổn định", "stableBehaviour giữ nguyên thứ tự.", "internal/cli/shift.go:11")
	p.ids["moved"] = probeHumanAdd(t, root, "Cờ security", "Cờ security không thay thế cô lập agent.", "internal/cli/eval.go:11")
	p.ids["intact"] = probeHumanAdd(t, root, "Giữ nguyên", "keep() không được bỏ.", "internal/cli/intact.go:11")

	// stale: nội dung đổi rồi người chạy verify --apply (verified → stale).
	e2eWrite(t, root, "internal/cli/stale.go", filler(10, "stale")+"\tstaleCheck := newGate()\n"+filler(5, "s2"))
	if out, code := run(t, root, "", "knowledge", "verify", "--apply", p.ids["stale"]); !strings.Contains(out, "stale") {
		t.Fatalf("verify --apply để tạo mục stale: %d\n%s", code, out)
	}
	if st := probeStatus(t, root, p.ids["stale"]); st != "stale" {
		t.Fatalf("chuẩn bị: mục phải stale, được %s", st)
	}
	// verified lệch fingerprint: nội dung đổi, chưa ai chạy verify --apply.
	e2eWrite(t, root, "internal/cli/drift.go", filler(10, "drift")+"\tdriftCheck := newBehaviour()\n"+filler(5, "d2"))
	// chỉ dời dòng: chèn 3 dòng phía trên, dòng bằng chứng sang dòng 14.
	e2eWrite(t, root, "internal/cli/shift.go", filler(13, "shift")+"\tshiftCheck := stableBehaviour()\n"+filler(5, "h2"))
	// chuyển tệp: khối sang internal/app/eval_run.go:4.
	e2eWrite(t, root, "internal/cli/eval.go", filler(4, "cli"))
	e2eWrite(t, root, "internal/app/eval_run.go", "package app\n\nfunc run() {\n"+probeMarker+"\n}\n")
	return p
}

func TestKnowledgeAuditProbe(t *testing.T) {
	p := newProbeRepo(t)
	root := p.root
	before := map[string]string{}
	for k, id := range p.ids {
		before[k] = probeItemFile(t, root, id)
	}

	// Agent gọi audit trong phiên agent: không bị chặn như thao tác chỉ-dành-cho-người.
	asAgent(t)
	out, code := run(t, root, "", "knowledge", "audit")
	if strings.Contains(out, "phiên agent") {
		t.Fatalf("audit phải cho agent gọi: %d\n%s", code, out)
	}
	if strings.Contains(out, "unknown command") || strings.Contains(out, "lệnh lạ") {
		t.Fatalf("thiếu lệnh knowledge audit: %d\n%s", code, out)
	}

	reasonRe := regexp.MustCompile(`(?i)fingerprint|thay đổi|thay doi|changed|lệch|mismatch`)

	t.Run("mục stale", func(t *testing.T) {
		id := p.ids["stale"]
		if !strings.Contains(out, id) {
			t.Fatalf("audit phải liệt kê mục stale %s:\n%s", id, out)
		}
		if !strings.Contains(out, "stale") {
			t.Errorf("audit phải nêu lý do (stale) cho %s:\n%s", id, out)
		}
		if !strings.Contains(out, "newGate()") {
			t.Errorf("audit phải trích đoạn bằng chứng hiện tại (newGate()) cho %s:\n%s", id, out)
		}
		if !strings.Contains(out, "internal/cli/stale.go") {
			t.Errorf("audit phải nêu tệp bằng chứng internal/cli/stale.go:\n%s", out)
		}
		if st := probeStatus(t, root, id); st != "stale" {
			t.Errorf("audit không được đổi trạng thái mục stale, được %s", st)
		}
	})

	t.Run("mục verified có fingerprint lệch", func(t *testing.T) {
		id := p.ids["drift"]
		if !strings.Contains(out, id) {
			t.Fatalf("audit phải liệt kê mục verified lệch fingerprint %s:\n%s", id, out)
		}
		if !reasonRe.MatchString(out) {
			t.Errorf("audit phải nêu lý do (bằng chứng đổi/fingerprint lệch):\n%s", out)
		}
		if !strings.Contains(out, "newBehaviour()") {
			t.Errorf("audit phải trích đoạn bằng chứng hiện tại (newBehaviour()) cho %s:\n%s", id, out)
		}
		if !strings.Contains(out, "internal/cli/drift.go") {
			t.Errorf("audit phải nêu tệp bằng chứng internal/cli/drift.go:\n%s", out)
		}
	})

	t.Run("mục verified chỉ dời dòng", func(t *testing.T) {
		id := p.ids["shift"]
		f := probeItemFile(t, root, id)
		if f == before["shift"] {
			t.Fatalf("audit phải tự áp mục chỉ dời dòng (như verify --apply), tệp mục không đổi:\n%s\n--- audit:\n%s", f, out)
		}
		if !regexp.MustCompile(`lines:\s*['"]?14['"]?`).MatchString(f) {
			t.Errorf("bằng chứng phải trỏ dòng 14 sau khi tự áp:\n%s", f)
		}
		if st := probeStatus(t, root, id); st != "verified" {
			t.Errorf("tự áp dời dòng giữ nguyên verified, được %s", st)
		}
	})

	t.Run("mục verified có khối chuyển tệp", func(t *testing.T) {
		id := p.ids["moved"]
		f := probeItemFile(t, root, id)
		if !strings.Contains(f, "internal/app/eval_run.go") {
			t.Fatalf("audit phải tự áp mục chuyển tệp sang internal/app/eval_run.go:\n%s\n--- audit:\n%s", f, out)
		}
		if !regexp.MustCompile(`lines:\s*['"]?4['"]?`).MatchString(f) {
			t.Errorf("bằng chứng phải trỏ dòng 4 của tệp mới:\n%s", f)
		}
		if st := probeStatus(t, root, id); st != "verified" {
			t.Errorf("tự áp chuyển tệp giữ nguyên verified, được %s", st)
		}
	})

	t.Run("mục verified còn khớp", func(t *testing.T) {
		id := p.ids["intact"]
		if strings.Contains(out, id) {
			t.Errorf("mục còn khớp %s không cần xem, không được liệt kê:\n%s", id, out)
		}
		if probeItemFile(t, root, id) != before["intact"] {
			t.Errorf("audit không được sửa mục còn khớp")
		}
	})

	t.Run("phiên agent gọi audit", func(t *testing.T) {
		// Audit lần hai (sau khi đã tự áp) vẫn liệt kê mục cần người/agent nhận định, và không
		// tự đổi trạng thái hay fingerprint của mục lệch nội dung.
		out2, code2 := run(t, root, "", "knowledge", "audit")
		if strings.Contains(out2, "phiên agent") {
			t.Fatalf("audit lần hai bị chặn trong phiên agent: %d\n%s", code2, out2)
		}
		for _, k := range []string{"stale", "drift"} {
			if !strings.Contains(out2, p.ids[k]) {
				t.Errorf("audit lần hai phải vẫn liệt kê %s (%s):\n%s", k, p.ids[k], out2)
			}
		}
		for _, k := range []string{"shift", "moved"} {
			if strings.Contains(out2, p.ids[k]) && !strings.Contains(out, p.ids[k]) {
				t.Errorf("mục đã tự áp %s không được trở thành mục cần xem:\n%s", k, out2)
			}
		}
		fp := regexp.MustCompile(`(?m)^\s*fingerprint:.*$`)
		if a, b := fp.FindAllString(before["drift"], -1), fp.FindAllString(probeItemFile(t, root, p.ids["drift"]), -1); strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("audit không được làm mới fingerprint của mục lệch nội dung (việc của người):\n%v\n→\n%v", a, b)
		}
	})
}

// Agent giả: ghi đề bài (stdin) ra tệp rồi in kết quả dạng Claude Code với khối nhận định.
func probeFakeAgent(t *testing.T, verdicts []map[string]any, model string) (cmd, promptCopy string) {
	t.Helper()
	dir := t.TempDir()
	text, _ := json.Marshal(map[string]any{"verdicts": verdicts})
	resp, _ := json.Marshal(map[string]any{
		"type": "result", "num_turns": 1, "result": "Đã đọc mã.\n" + string(text),
		"modelUsage": map[string]any{model: map[string]any{"costUSD": 0}},
	})
	respFile := filepath.Join(dir, "resp.json")
	promptCopy = filepath.Join(dir, "prompt-copy.txt")
	os.WriteFile(respFile, resp, 0o644)
	script := filepath.Join(dir, "agent.sh")
	os.WriteFile(script, []byte("cat > '"+filepath.ToSlash(promptCopy)+"'\ncat '"+filepath.ToSlash(respFile)+"'\n"), 0o644)
	return "sh " + filepath.ToSlash(script), promptCopy
}

func TestKnowledgeRevalidateVerdictsProbe(t *testing.T) {
	needShell(t)
	p := newProbeRepo(t)
	root := p.root
	const model = "claude-probe-model-7q"
	verdicts := []map[string]any{
		{"id": p.ids["stale"], "verdict": "outdated", "reason": "newGate thay legacyGate ở internal/cli/stale.go:11"},
		{"id": p.ids["drift"], "verdict": "valid", "reason": "Vẫn đúng, xem internal/cli/drift.go:11"},
	}
	agentCmd, promptCopy := probeFakeAgent(t, verdicts, model)
	stFile := map[string]string{}
	statusBefore := map[string]string{}
	for k, id := range p.ids {
		stFile[k] = probeItemFile(t, root, id)
		statusBefore[k] = probeStatus(t, root, id)
	}

	asHuman(t)
	out, code := run(t, root, "", "knowledge", "revalidate", "--agent-command", agentCmd, "--json",
		p.ids["stale"], p.ids["drift"])
	if code != 0 {
		t.Fatalf("revalidate (mục stale + mục verified lệch fingerprint): %d\n%s", code, out)
	}
	var rep struct {
		Results []struct {
			ID      string `json:"id"`
			Verdict string `json:"verdict"`
			Reason  string `json:"reason"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("revalidate --json: %v\n%s", err, out)
	}
	got := map[string]string{}
	for _, r := range rep.Results {
		got[r.ID] = r.Verdict
	}

	t.Run("nhận định outdated", func(t *testing.T) {
		if got[p.ids["stale"]] != "outdated" {
			t.Errorf("mục stale: nhận định %q, muốn outdated\n%s", got[p.ids["stale"]], out)
		}
		if st := probeStatus(t, root, p.ids["stale"]); st != statusBefore["stale"] {
			t.Errorf("nhận định không được đổi trạng thái: %s → %s", statusBefore["stale"], st)
		}
	})

	t.Run("nhận định valid", func(t *testing.T) {
		// revalidate mở rộng: nhận cả mục verified có fingerprint lệch.
		if got[p.ids["drift"]] != "valid" {
			t.Errorf("mục verified lệch fingerprint: nhận định %q, muốn valid\n%s", got[p.ids["drift"]], out)
		}
		if st := probeStatus(t, root, p.ids["drift"]); st != "verified" {
			t.Errorf("nhận định valid không được đổi trạng thái, được %s", st)
		}
		fp := regexp.MustCompile(`(?m)^\s*fingerprint:.*$`)
		if a, b := fp.FindAllString(stFile["drift"], -1), fp.FindAllString(probeItemFile(t, root, p.ids["drift"]), -1); strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Errorf("nhận định valid không được tự làm mới fingerprint (người áp):\n%v\n→\n%v", a, b)
		}
	})

	t.Run("skill hướng dẫn nhận định", func(t *testing.T) {
		// Đề bài revalidate dùng chung định dạng nhận định với skill: có đủ bốn loại.
		prompt, err := os.ReadFile(promptCopy)
		if err != nil {
			t.Fatalf("agent giả không nhận được đề bài: %v", err)
		}
		for _, v := range []string{"valid", "outdated", "fix-evidence", "unsure"} {
			if !strings.Contains(string(prompt), v) {
				t.Errorf("đề bài revalidate thiếu nhận định %q:\n%s", v, prompt)
			}
		}
		if !strings.Contains(string(prompt), p.ids["drift"]) {
			t.Errorf("đề bài phải có mục verified lệch fingerprint %s", p.ids["drift"])
		}
		// Skill cài sẵn (Claude Code hoặc Codex) hướng dẫn agent chạy audit và lưu nhận định.
		var skills []string
		for _, dir := range []string{filepath.Join(root, ".claude", "skills"), filepath.Join(root, ".agents", "skills")} {
			filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.EqualFold(info.Name(), "SKILL.md") {
					if b, e := os.ReadFile(path); e == nil && strings.Contains(string(b), "knowledge audit") {
						skills = append(skills, string(b))
					}
				}
				return nil
			})
		}
		if len(skills) == 0 {
			t.Fatalf("không có skill nào (sau init) hướng dẫn `bint knowledge audit`")
		}
		s := skills[0]
		for _, v := range []string{"valid", "outdated", "fix-evidence", "unsure", "verdict"} {
			if !strings.Contains(s, v) {
				t.Errorf("skill rà soát thiếu %q:\n%s", v, s)
			}
		}
	})

	t.Run("nhận định fix-evidence kèm bằng chứng đề xuất", func(t *testing.T) {
		// Khối của mục drift: agent đề xuất bằng chứng mới; định dạng chung phải giữ fix-evidence,
		// không ép về unsure.
		fixCmd, _ := probeFakeAgent(t, []map[string]any{{
			"id": p.ids["drift"], "verdict": "fix-evidence", "reason": "Khối dời sang internal/cli/drift.go:11 với tên mới",
			"evidence": []map[string]any{{"path": "internal/cli/drift.go", "lines": "11"}},
		}}, model)
		out, code := run(t, root, "", "knowledge", "revalidate", "--agent-command", fixCmd, "--json", p.ids["drift"])
		if code != 0 {
			t.Fatalf("revalidate fix-evidence: %d\n%s", code, out)
		}
		var rep struct {
			Results []struct {
				ID      string `json:"id"`
				Verdict string `json:"verdict"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatalf("revalidate --json: %v\n%s", err, out)
		}
		ok := false
		for _, r := range rep.Results {
			if r.ID == p.ids["drift"] && r.Verdict == "fix-evidence" {
				ok = true
			}
		}
		if !ok {
			t.Errorf("nhận định fix-evidence phải được giữ nguyên:\n%s", out)
		}
		if st := probeStatus(t, root, p.ids["drift"]); st != "verified" {
			t.Errorf("fix-evidence không được đổi trạng thái, được %s", st)
		}
	})

	t.Run("ghi mô hình của nhận định", func(t *testing.T) {
		// Nhận định ghi agent/mô hình đã tạo ra nó: thấy được qua lệnh xem mục/audit hoặc nơi lưu trạng thái.
		var seen strings.Builder
		seen.WriteString(out)
		for _, args := range [][]string{
			{"knowledge", "show", p.ids["drift"]}, {"knowledge", "show", p.ids["drift"], "--json"},
			{"knowledge", "audit"}, {"knowledge", "audit", "--json"},
		} {
			o, _ := run(t, root, "", args...)
			seen.WriteString(o)
		}
		if sd := os.Getenv("BINT_STATE_DIR"); sd != "" {
			filepath.Walk(sd, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.HasPrefix(info.Name(), "state.sqlite") {
					if b, e := os.ReadFile(path); e == nil {
						seen.Write(b)
					}
				}
				return nil
			})
		}
		if !strings.Contains(seen.String(), model) {
			t.Errorf("không thấy mô hình %q đã tạo nhận định ở đâu (show/audit/nơi lưu)", model)
		}
	})
}
