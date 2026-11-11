package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	bintapp "bint/internal/app"
	"bint/internal/knowledge"
	knscreen "bint/internal/tui/screens/knowledge"
	"bint/internal/tui/screens/overview"
	"bint/internal/tui/theme"
)

// sampleData là dữ liệu Tổng quan cố định cho kiểm thử và ảnh chụp.
func sampleData() overview.Data {
	return overview.Data{
		Status: &bintapp.StatusReport{
			Repo: "/work/demo", RepoID: "7f3a9c", StateDir: "/state/demo-7f3a9c", Schema: 6,
			Harnesses: []string{"claude", "codex"}, Runs: map[string]int{"completed": 33, "failed": 3, "interrupted": 1, "running": 5}, TotalRuns: 42,
			PendingProposals: 2,
		},
		Doctor: &bintapp.DoctorReport{Checks: []bintapp.Check{
			{Name: "git", Level: bintapp.LevelOK, Detail: "git version 2.45"},
			{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"},
			{Name: "hook claude", Level: bintapp.LevelOK, Detail: "đã cài"},
		}},
		Pending: &bintapp.PendingWorkReport{
			KnowledgeBatches: []bintapp.PendingBatch{{Tag: "nguon:lo-thu", Candidate: 2}},
			KnowledgeOther:   1,
			Proposals:        []bintapp.PendingProposal{{ID: "01PROP1", Action: "create"}, {ID: "01PROP2", Action: "update"}},
			Doctor:           []bintapp.Check{{Name: "config", Level: bintapp.LevelWarn, Detail: "cấu hình theo mẫu cũ: chạy `bint init --update`"}},
		},
		Candidates: 3,
	}
}

// sampleKnowledge là dữ liệu danh sách tri thức cố định cho kiểm thử và ảnh chụp.
func sampleKnowledge() knscreen.Data {
	mk := func(id, typ, status, title, origin string, third bool, paths []string, tags ...string) *knowledge.Item {
		return &knowledge.Item{ID: id, Type: typ, Status: status, Title: title, Body: "Nội dung của mục " + title + ".\nDòng thứ hai của nội dung.",
			Scope: knowledge.Scope{Paths: paths, Tags: tags}, Origin: knowledge.Origin{Kind: origin, ThirdParty: third},
			Author: "Tester <t@example.com>", CreatedAt: "2026-11-11T00:00:00Z", Evidence: []knowledge.Evidence{{Path: "internal/db/db.go", Lines: "40-52"}},
			Path: "/work/demo/.bint/knowledge/items/" + id + ".md"}
	}
	return knscreen.Data{Stamp: time.Unix(1, 0), Files: []string{"internal/db/db.go", "internal/api/api.go", "README.md"}, Items: []*knowledge.Item{
		mk("01KNOW000001", "rule", "candidate", "Endpoint phải có /v1", "agent", true, []string{"api/**"}, "nguon:lo-thu"),
		mk("01KNOW000002", "warning", "candidate", "Không gọi sql.Open trực tiếp", "agent", true, []string{"internal/db/**"}, "nguon:lo-thu"),
		mk("01KNOW000003", "pattern", "verified", "Handler đi qua wrap()", "human", false, nil),
		mk("01KNOW000004", "fact", "stale", "Cổng mặc định là 8080", "agent", false, []string{"cmd/**"}),
		mk("01KNOW000005", "lesson", "candidate", "Chạy go vet với GOOS=windows", "agent", false, nil),
		mk("01KNOW000006", "decision", "deprecated", "Dùng bbolt thay SQLite", "human", false, nil),
	}}
}

// sampleDetail là chi tiết giả của một mục (bằng chứng có trích đoạn, một finding).
func sampleDetail(_ context.Context, _ string, id string) (bintapp.KnowledgeDetail, error) {
	for _, it := range sampleKnowledge().Items {
		if it.ID != id {
			continue
		}
		d := bintapp.KnowledgeDetail{Item: it, Evidence: []bintapp.EvidenceView{{Evidence: it.Evidence[0], Lines: []string{"  40  func Open() {", "  41  \treturn nil", "  42  }"}}}}
		if it.Status == knowledge.StatusStale {
			d.Check.Findings = []knowledge.Finding{{Level: knowledge.LevelError, Code: knowledge.CodeFingerprint, Detail: "bằng chứng đã thay đổi"}}
		}
		return d, nil
	}
	return bintapp.KnowledgeDetail{}, errors.New("không có mục")
}

func notInitData() overview.Data { return overview.Data{NotInit: true} }

// newModel dựng mô hình với loader giả và đã nạp xong dữ liệu.
func newModel(t *testing.T, o Options, data overview.Data, w, h int) *Model {
	t.Helper()
	if o.Dir == "" {
		o.Dir = "/work/demo"
	}
	if o.Version == "" {
		o.Version = "0.1.0"
	}
	if o.Lang == "" {
		o.Lang = "vi"
	}
	o.Loader = func(context.Context, string) (overview.Data, error) { return data, nil }
	if o.KnowledgeLoader == nil {
		o.KnowledgeLoader = func(context.Context, string) (knscreen.Data, error) { return sampleKnowledge(), nil }
	}
	if o.KnowledgeDetailer == nil {
		o.KnowledgeDetailer = sampleDetail
	}
	o.SpinnerFPS = time.Millisecond
	m := New(context.Background(), o)
	m.SetSize(w, h)
	drive(t, m, m.Init())
	return m
}

// drive chạy tea.Cmd (đồng bộ) và đưa thông điệp kết quả vào Update, tới khi hết lệnh.
func drive(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				drive(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
	}
}

func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		drive(t, m, cmd)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func plain(s string) string { return ansi.Strip(s) }

// lines tách đầu ra thành các dòng (đã bỏ mã ANSI).
func lines(s string) []string { return strings.Split(plain(s), "\n") }

func allModes() []theme.Mode { return theme.Modes }
