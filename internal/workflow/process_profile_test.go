package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/eval"
	"bint/internal/iso"
	"bint/internal/task"
)

// Bước driver process của Codex ở profile `bint iso` (docs/iso-plan.md mục 5.7, GĐ5): handle phiên mang profile, tệp phiên và usage
// đọc ở CODEX_HOME của PROFILE (không phải CODEX_HOME của tiến trình Bint), đổi profile giữa hai lượt thì chạy mới có ghi lý do.

const profThread = "01a17179-5784-7152-8335-f0af387ce18a"

// fakeProfile dựng profile giả (không cần store): ID = <tool>/<name>, thư mục cấu hình = <Home>/.codex.
func fakeProfile(t *testing.T, tool iso.Tool, name string) *iso.Profile {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	return &iso.Profile{Tool: tool, Name: name, Home: home}
}

// codexInProfile: agent Codex giả ở profile p; lần đầu ghi rollout vào CODEX_HOME của profile rồi dừng external, lần nối tiếp ghi
// thêm. Trả số lần gọi qua h.calls.
func codexInProfile(t *testing.T, h *harness, p *iso.Profile) {
	t.Helper()
	h.agentName = "codex"
	h.profile = p
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step != StepFeat {
			return
		}
		cur := h.profile // profile hiện hành của agent (đổi giữa hai lượt ở test đổi profile)
		res.SessionID = profThread
		bogus := int64(777777)
		res.InputTokens, res.OutputTokens = &bogus, &bogus
		home := ""
		if cur != nil {
			home = cur.ConfigDir()
		} else {
			home = os.Getenv("CODEX_HOME")
		}
		if req.ResumeSessionID == "" {
			writeRolloutIn(t, home, profThread, 55000, 20000, 900)
			externalFail(res)
			return
		}
		writeRolloutIn(t, home, profThread, 61000, 41000, 1100)
	}
}

func TestProcessProfileStopResumeSameThread(t *testing.T) {
	_, procCodex := procSetup(t) // CODEX_HOME của tiến trình: chỗ KHÁC thư mục của profile
	h := newHarness(t)
	p := fakeProfile(t, iso.Codex, "work")
	codexInProfile(t, h, p)

	res, id := startDo(t, h)
	ls := loadTask(t, h, id).State.LastStep
	if ls == nil || ls.Profile != "codex/work" || ls.Via != "codex" || ls.StopClass != StopExternal || !ls.Continuable || ls.SessionID != profThread ||
		ls.Usage == nil || len(ls.Usage.Models) != 1 || ls.Usage.Models[0].Input != 55000 {
		t.Fatalf("LastStep phải mang profile và đọc rollout trong CODEX_HOME của profile: %+v", ls)
	}
	if req := lastFeatReq(h); req.Profile != p {
		t.Fatalf("AgentRequest.Profile = %v, muốn profile của agent", req.Profile)
	}
	var stopped *StepResult
	for i := range res.Steps {
		if res.Steps[i].Step == StepFeat {
			stopped = &res.Steps[i]
		}
	}
	if stopped == nil || stopped.Profile != "codex/work" || stopped.StopClass != StopExternal {
		t.Fatalf("StepResult.Profile: %+v", stopped)
	}
	if matches, _ := filepath.Glob(filepath.Join(procCodex, "sessions", "*", "*", "*", "*.jsonl")); len(matches) != 0 {
		t.Fatalf("không được có tệp phiên ở CODEX_HOME của tiến trình: %v", matches)
	}

	res2 := resumeDo(t, h, id, nil)
	second := lastFeatReq(h)
	if second.ResumeSessionID != profThread || second.SessionID != "" || second.Profile != p {
		t.Fatalf("phải nối tiếp đúng thread trong cùng profile: %+v", second)
	}
	var got *StepResult
	for i := range res2.Steps {
		if res2.Steps[i].Step == StepFeat {
			got = &res2.Steps[i]
		}
	}
	if got == nil || got.Profile != "codex/work" {
		t.Fatalf("StepResult của bước nối tiếp: %+v", got)
	}
	// Token = bộ đếm rollout trong profile trừ mốc (6000 vào, 200 ra), không phải 777777 harness báo.
	ev := h.lastFinished(t, got.RunID)
	if ev["input_tokens"] != float64(6000) || ev["output_tokens"] != float64(200) {
		t.Fatalf("token bước nối tiếp: %v", ev)
	}
	if res2.Status != task.StatusDone {
		t.Fatalf("status = %s (%s)", res2.Status, res2.Reason)
	}
}

// Rollout chỉ có ở CODEX_HOME của tiến trình (không có trong profile) thì không coi là phiên của bước: không nối tiếp được.
func TestProcessProfileSessionLookedUpInProfileOnly(t *testing.T) {
	_, procCodex := procSetup(t)
	h := newHarness(t)
	p := fakeProfile(t, iso.Codex, "work")
	h.agentName, h.profile = "codex", p
	h.mutateReq = func(req eval.AgentRequest, step string, res *eval.AgentResult) {
		if step == StepFeat && countCalls(h, StepFeat) == 1 {
			res.SessionID = profThread
			writeRolloutIn(t, procCodex, profThread, 1000, 0, 10) // sai chỗ
			externalFail(res)
		}
	}
	_, id := startDo(t, h)
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Continuable || ls.Profile != "codex/work" {
		t.Fatalf("rollout ngoài profile ⇒ không nối tiếp được: %+v", ls)
	}
}

// Đổi harnesses.codex.profile giữa hai lượt: bước chạy mới với đề bài đầy đủ, notes nêu cả profile cũ lẫn mới.
func TestProcessProfileChangedRunsFreshWithReason(t *testing.T) {
	for _, tc := range []struct {
		name     string
		next     func(t *testing.T) *iso.Profile
		wantNote []string
	}{
		{"work sang profile khác", func(t *testing.T) *iso.Profile { return fakeProfile(t, iso.Codex, "other") }, []string{"codex/work", "codex/other"}},
		{"work sang không profile", func(*testing.T) *iso.Profile { return nil }, []string{"codex/work", "không có profile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			procSetup(t)
			h := newHarness(t)
			codexInProfile(t, h, fakeProfile(t, iso.Codex, "work"))
			_, id := startDo(t, h)
			if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Profile != "codex/work" || !ls.Continuable {
				t.Fatalf("LastStep: %+v", ls)
			}
			h.profile = tc.next(t)
			res := resumeDo(t, h, id, nil)
			second := lastFeatReq(h)
			if second.ResumeSessionID != "" {
				t.Fatalf("đổi profile phải chạy mới, không resume: %+v", second)
			}
			if !strings.Contains(second.Prompt, "<bint-task") {
				t.Fatalf("phải là đề bài đầy đủ:\n%s", second.Prompt)
			}
			var note string
			for _, n := range res.Notes {
				if strings.Contains(n, "không nối tiếp phiên cũ") {
					note = n
				}
			}
			for _, w := range tc.wantNote {
				if !strings.Contains(note, w) {
					t.Fatalf("notes phải nêu %q: %v", w, res.Notes)
				}
			}
		})
	}
}

// Không có profile: handle không mang profile (phiếu giữ nguyên dạng cũ) và nối tiếp như trước.
func TestProcessNoProfileLastStepHasNoProfile(t *testing.T) {
	procSetup(t)
	h := newHarness(t)
	h.claudeLike(func(req eval.AgentRequest, step string, n int, res *eval.AgentResult) {
		if step == StepFeat && n == 1 {
			externalFail(res)
		}
	})
	_, id := startDo(t, h)
	if ls := loadTask(t, h, id).State.LastStep; ls == nil || ls.Profile != "" || !ls.Continuable {
		t.Fatalf("LastStep: %+v", ls)
	}
}
