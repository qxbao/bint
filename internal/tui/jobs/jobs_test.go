package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"bint/internal/workflow"
)

// Huỷ khi câu hỏi còn chờ: hàm hỏi trả "không"/rỗng ngay (không treo goroutine) cả khi không ai đọc kênh.
func TestBridgeCancelWhileAskingReturnsImmediately(t *testing.T) {
	out := make(chan tea.Msg) // không đệm và không ai đọc: gửi sẽ chặn tới khi huỷ
	ctx, cancel := context.WithCancel(context.Background())
	br := NewBridge(ctx, 1, out)
	done := make(chan [2]any, 1)
	go func() {
		done <- [2]any{br.ConfirmQuestion(workflow.Question{Key: "k"}), br.AskQuestion(workflow.Question{Key: "k"})}
	}()
	time.AfterFunc(20*time.Millisecond, cancel)
	select {
	case r := <-done:
		if r[0] != false || r[1] != "" {
			t.Fatalf("phải trả không/rỗng: %v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hàm hỏi treo sau khi huỷ")
	}
}

// Câu hỏi đã gửi lên mà bị huỷ trong lúc chờ trả lời cũng kết thúc; câu trả lời đến muộn không chặn Update.
func TestBridgeCancelWhileWaitingForReply(t *testing.T) {
	out := make(chan tea.Msg, 1)
	ctx, cancel := context.WithCancel(context.Background())
	br := NewBridge(ctx, 7, out)
	res := make(chan bool, 1)
	go func() { res <- br.ConfirmQuestion(workflow.Question{Key: "q", Paths: []string{"a"}}) }()
	pm := (<-out).(PromptMsg)
	if pm.Job != 7 || pm.Prompt.Kind != Confirm || pm.Prompt.Key != "q" || len(pm.Prompt.Paths) != 1 {
		t.Fatalf("PromptMsg: %+v", pm)
	}
	cancel()
	if got := <-res; got {
		t.Fatal("huỷ phải trả không")
	}
	pm.Reply <- Answer{Yes: true} // Reply có đệm 1: Update không bao giờ bị chặn
}

func TestBridgeAnswersAndStreams(t *testing.T) {
	out := make(chan tea.Msg, 8)
	br := NewBridge(context.Background(), 2, out)
	go func() {
		pm := (<-out).(PromptMsg)
		pm.Reply <- Answer{Yes: true, Text: "lệnh"}
	}()
	if !br.ConfirmQuestion(workflow.Question{}) {
		t.Fatal("trả có")
	}
	go func() {
		pm := (<-out).(PromptMsg)
		pm.Reply <- Answer{Text: "go test"}
	}()
	if got := br.AskQuestion(workflow.Question{Key: workflow.QAskVerifyCommand}); got != "go test" {
		t.Fatalf("ask: %q", got)
	}
	br.Log("dòng")
	br.OnStep(workflow.StepEvent{Step: "b-feat"})
	br.Finish("v", nil)
	if m := (<-out).(LogMsg); m.Line != "dòng" || m.Job != 2 {
		t.Fatalf("%+v", m)
	}
	if m := (<-out).(StepMsg); m.Event.Step != "b-feat" {
		t.Fatalf("%+v", m)
	}
	if m := (<-out).(DoneMsg); m.Value != "v" || m.Err != nil {
		t.Fatalf("%+v", m)
	}
	cb := br.Callbacks()
	if cb.Confirm == nil || cb.Ask == nil || cb.Log == nil || cb.OnStep == nil {
		t.Fatal("Callbacks thiếu hàm")
	}
}

func TestTableLifecycle(t *testing.T) {
	var tb Table
	a := tb.Add(Job{Action: "tasks.run", Kind: KindWorkflow, TaskID: "T1"})
	b := tb.Add(Job{Action: "tasks.verify", Kind: KindVerify, TaskID: "T1"})
	c := tb.Add(Job{Action: "x", Kind: KindAction})
	u := tb.Add(Job{Action: "tasks.run2", Kind: KindWorkflow})
	if a.ID != 1 || tb.Len() != 4 || tb.Running("tasks.run") != a || tb.OfKind(KindWorkflow) != a || tb.ForTask("T1") != a {
		t.Fatalf("bảng: %+v", tb.All())
	}
	if tb.Unassigned() != u {
		t.Fatal("việc workflow chưa biết tác vụ")
	}
	tb.Remove(a.ID, nil)
	tb.Remove(c.ID, nil)
	if tb.Last("T1") != a || tb.Last("T1").Err != nil || tb.ForTask("T1") != b || tb.Len() != 2 {
		t.Fatalf("sau khi xong: last=%v forTask=%v", tb.Last("T1"), tb.ForTask("T1"))
	}
	if tb.Last("") != nil {
		t.Fatal("việc thường không giữ lại")
	}
	cancelled := 0
	b.Cancel = func() { cancelled++ }
	tb.CancelAll()
	if cancelled != 1 {
		t.Fatal("CancelAll phải huỷ mọi việc")
	}
	for i := 0; i < maxDone+3; i++ {
		j := tb.Add(Job{Action: "w", Kind: KindWorkflow, TaskID: "T2"})
		tb.Remove(j.ID, context.Canceled)
	}
	if len(tb.done) != maxDone || tb.Last("T2").Err == nil {
		t.Fatalf("danh sách đã xong chỉ giữ %d việc gần nhất: %d", maxDone, len(tb.done))
	}
}

func TestLinesCap(t *testing.T) {
	l := NewLines(3)
	l.Add("1", "2")
	l.Add("3", "4", "5")
	if len(l.Lines) != 3 || l.Lines[0] != "3" || l.Dropped != 2 {
		t.Fatalf("%+v", l)
	}
}

// Đuôi log chỉ trả dòng đã hoàn chỉnh khi tệp đang được ghi dần; phần dở dang nối với phần sau; tệp bị cắt thì đọc lại.
func TestTailReadsGrowingFileByCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "step.log")
	j := &Job{Log: NewLines(10), Tail: NewLines(10)}
	j.StartTail(workflow.StepEvent{Step: "b-feat", Log: path}) // tệp chưa tồn tại: không lỗi
	if len(j.Tail.Lines) != 0 || !j.Tailing() {
		t.Fatal("chưa có gì")
	}
	f, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	defer f.Close()
	f.WriteString("một\nhai\nba chưa xong")
	j.ReadTail()
	if got := j.Tail.Lines; len(got) != 2 || got[0] != "một" || got[1] != "hai" {
		t.Fatalf("dòng dở dang chưa được hiện: %v", got)
	}
	f.WriteString(" nay xong\r\nbốn\n")
	j.ReadTail()
	if got := j.Tail.Lines; len(got) != 4 || got[2] != "ba chưa xong nay xong" || got[3] != "bốn" {
		t.Fatalf("nối phần dở dang, bỏ CR: %v", got)
	}
	f.WriteString("cuối không xuống dòng")
	j.StopTail()
	if got := j.Tail.Lines; got[len(got)-1] != "cuối không xuống dòng" || j.Tailing() {
		t.Fatalf("StopTail phải đọc nốt phần dở: %v", got)
	}
	// Tệp bị ghi lại ngắn hơn trong lúc theo dõi: đọc lại từ đầu (dòng mới nối vào sau các dòng đã có).
	j.StartTail(workflow.StepEvent{Log: path})
	n := len(j.Tail.Lines)
	os.WriteFile(path, []byte("m\n"), 0o600)
	j.ReadTail()
	if got := j.Tail.Lines; len(got) != n+1 || got[len(got)-1] != "m" {
		t.Fatalf("tệp bị cắt: %v", got)
	}
}
