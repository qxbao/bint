package workflow

import (
	"context"
	"errors"
	"testing"
)

// Agent (b-req) không được ghi đè verify.command người đã xác nhận: lệnh mới chưa duyệt làm mọi lần kiểm chứng bị bỏ qua
// cho tới bước cuối. Agent vẫn đặt được lệnh khi phiếu chưa có lệnh hay lệnh chưa xác nhận, và đặt lại đúng lệnh cũ.
func TestAgentCannotOverwriteConfirmedVerifyCommand(t *testing.T) {
	h := newHarness(t)
	human := h.runner()
	ctx := context.Background()
	tk, _, err := human.NewTask(ctx, NewTaskOptions{Request: "Sửa app.txt", Workflow: StepDo, Plan: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	id := tk.State.ID
	agent := *human.Env
	agent.Human = false

	// Phiếu mới mang eval.test_command đã xác nhận của cấu hình.
	confirmed := loadTask(t, h, id).Ticket.Verify.Command
	if confirmed == "" || !human.VerifyConfirmed(loadTask(t, h, id)) {
		t.Fatalf("harness phải có lệnh kiểm chứng đã xác nhận, có %q", confirmed)
	}

	// Đã xác nhận: agent đổi sang lệnh khác (kể cả xoá) bị từ chối, phiếu giữ nguyên.
	for _, set := range []func() error{
		func() error {
			_, _, err := agent.SetTicket(id, TicketEdit{VerifyCommand: ptr("go test ./a/...")})
			return err
		},
		func() error {
			_, _, _, err := agent.SetTicketAndDependencies(id, TicketEdit{VerifyCommand: ptr("")}, nil, false)
			return err
		},
	} {
		if err := set(); !errors.Is(err, ErrAgent) {
			t.Fatalf("agent ghi đè lệnh đã xác nhận = %v, mong ErrAgent", err)
		}
		if got := loadTask(t, h, id).Ticket.Verify.Command; got != confirmed {
			t.Fatalf("verify.command bị đổi thành %q", got)
		}
	}
	// Đặt lại đúng lệnh cũ và sửa trường khác vẫn được.
	if _, _, err := agent.SetTicket(id, TicketEdit{VerifyCommand: ptr(" " + confirmed + " "), Criteria: []string{"app.txt đổi"}}); err != nil {
		t.Fatalf("agent đặt lại đúng lệnh đã xác nhận: %v", err)
	}
	// Người vẫn đổi được; lệnh mới chưa xác nhận thì agent lại đổi được.
	if _, _, err := human.SetTicket(id, TicketEdit{VerifyCommand: ptr("go test ./a/...")}); err != nil {
		t.Fatalf("người đổi lệnh: %v", err)
	}
	if _, _, err := agent.SetTicket(id, TicketEdit{VerifyCommand: ptr("go test ./b/...")}); err != nil {
		t.Fatalf("agent đổi lệnh chưa xác nhận: %v", err)
	}
}
