package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/workflow"
)

// Lệnh của driver session (docs/isolation-subagent-plan.md mục 6): phiên điều phối (orchestrator) chạy từng bước bằng
// subagent, Bint giữ máy trạng thái và mọi cổng kiểm tra.
//
//	bint run next <id> --json        → {action: agent|process|done|blocked|failed, …}
//	  (Agent chạy subagent với prompt)
//	bint run complete <id> --run <run_id> [--agent-id <id>] [--failed "<lý do>"] --json
//	bint run abort <id>              → bỏ bước dở, gỡ hợp đồng thuê khoá

// sessionExit đổi hành động dừng thành mã thoát: blocked/failed ⇒ 1 (done, agent, process, rỗng ⇒ 0).
func sessionExit(a *workflow.SessionAction) error {
	switch a.Action {
	case workflow.ActionBlocked, workflow.ActionFailed:
		return withCode(ExitError, fmt.Errorf("tác vụ %s: %s", a.TaskID, a.Status))
	}
	return nil
}

func printSessionAction(w io.Writer, a *workflow.SessionAction) {
	act := a.Action
	if act == "" {
		act = "tiếp tục"
	}
	fmt.Fprintf(w, "Tác vụ %s: %s — hành động: %s\n", a.TaskID, a.Status, act)
	if a.Step != "" {
		fmt.Fprintf(w, "  Bước:      %s", a.Step)
		if a.Subagent != "" {
			fmt.Fprintf(w, " (subagent %s", a.Subagent)
			if a.Model != "" {
				fmt.Fprintf(w, ", model %s", a.Model)
			}
			fmt.Fprint(w, ")")
		}
		fmt.Fprintln(w)
	}
	if a.RunID != "" {
		fmt.Fprintf(w, "  Lượt:      %s (hết hạn %s)\n", a.RunID, orDash(fmtRFC(a.LeaseUntil)))
	}
	if a.Worktree != "" && a.Action == workflow.ActionAgent {
		fmt.Fprintf(w, "  Thư mục:   %s\n", a.Worktree)
	}
	if a.Command != "" {
		fmt.Fprintf(w, "  Lệnh:      %s\n", a.Command)
	}
	if a.Reason != "" {
		fmt.Fprintf(w, "  Lý do:     %s\n", a.Reason)
	}
	if r := a.Result; r != nil {
		fmt.Fprintf(w, "  Kết quả:   %s %s (%s)", r.Step, r.Status, r.Duration)
		if r.Error != "" {
			fmt.Fprintf(w, " — %s", r.Error)
		}
		fmt.Fprintln(w)
	}
	for _, q := range a.Questions {
		fmt.Fprintf(w, "? %s\n", q)
	}
	for _, i := range a.PlanningIssues {
		fmt.Fprintf(w, "✗ %s\n", i)
	}
	for _, n := range a.Notes {
		fmt.Fprintf(w, "! %s\n", n)
	}
	if a.Prompt != "" {
		fmt.Fprintf(w, "\n--- prompt cho subagent ---\n%s\n", strings.TrimRight(a.Prompt, "\n"))
	}
}

func finishSession(stdio IO, asJSON bool, a *workflow.SessionAction) error {
	if a == nil { // phòng thủ: lõi không được trả hành động rỗng, nhưng CLI không được panic nếu lỗi đó tái phát
		return withCode(ExitError, fmt.Errorf("lõi không trả hành động nào (lỗi nội bộ); chạy lại bint run next"))
	}
	if asJSON {
		if err := writeJSON(stdio.Out, a); err != nil {
			return err
		}
	} else {
		printSessionAction(stdio.Out, a)
	}
	return sessionExit(a)
}

func newRunNextCmd(stdio IO) *cobra.Command {
	var o app.RunNextOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "next <task-id>",
		Short: "Hành động kế tiếp của tác vụ driver session (chạy bước bằng subagent)",
		Long: `Dành cho phiên điều phối (orchestrator) chạy các bước của tác vụ bằng subagent thay vì để bint run gọi harness.
Mở bước kế tiếp (chuyển trạng thái, giới hạn, khoá worktree, ảnh chụp, đề bài) và trả hành động dạng JSON (--json):

  {"action":"agent","step","run_id","subagent","model","prompt","worktree","readonly","max_turns","lease_until"}
      chạy subagent với prompt rồi gọi bint run complete <id> --run <run_id>
  {"action":"process","command":"bint run b-feat --task <id> --json"}
      role gán harness khác Claude: chạy lệnh đó (nền được), rồi gọi lại bint run next
  {"action":"done"|"blocked"|"failed","reason","questions"}
      dừng và báo người dùng; blocked/failed có mã thoát 1. Người xử lý xong thì bint run next <id> --resume

Gọi lại khi bước còn hạn trả đúng bước đó (cùng run_id), không mở bước mới. Bước giữ khoá worktree bằng hợp đồng thuê
theo thời gian (workflow.timeout_minutes); phiên điều phối chết giữa bước thì bint run abort <id>. Khoá bị bước khác
giữ thì chờ tối đa workflow.timeout_minutes, --no-wait để dừng ngay (blocked).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			a, err := app.RunNext(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			return finishSession(stdio, asJSON, a)
		},
	}
	c.Flags().BoolVar(&o.Resume, "resume", false, "tiếp tục tác vụ blocked/interrupted/failed sau khi đã xử lý nguyên nhân")
	c.Flags().BoolVar(&o.Rerun, "rerun", false, "cùng --resume: bỏ kết quả bước chỉ đọc đang giữ và chạy lại bước đó")
	c.Flags().BoolVar(&o.NoWait, "no-wait", false, "khoá worktree đang bị bước khác giữ thì dừng ngay (blocked) thay vì chờ")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func newRunCompleteCmd(stdio IO) *cobra.Command {
	var o app.RunCompleteOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "complete <task-id> --run <run_id>",
		Short: "Đóng bước subagent vừa chạy (driver session)",
		Long: `Báo subagent của bước do bint run next trả về đã xong. Bint làm phần sau của bước như bint run: kiểm bước chỉ đọc
không sửa cây làm việc, phạm vi và đường dẫn bảo vệ, kiểm chứng sau b-feat, tài liệu của b-req, quyết định sau review
(vòng sửa / dừng ở workflow.max_review_rounds / finalize). Gỡ bước dở và hợp đồng thuê khoá.

--run phải khớp run_id của bước đang dở (sai ⇒ lỗi, không đổi gì). --agent-id là agentId của kết quả công cụ Agent:
Bint ghép sự kiện hook của subagent vào lượt chạy của bước và tính chi phí từ transcript của nó (cost_usd null = chưa
biết; thiếu --agent-id thì chi phí unknown). Chi phí cộng dồn vào ngân sách limits.workflows.b-do. --failed "<lý do>" báo subagent thất bại: bước bị đánh failed.

Kết quả JSON có "result" (kết quả bước). Tác vụ còn tiếp tục thì không có "action": gọi bint run next. Dừng thì "action" là
done | blocked | failed (blocked/failed có mã thoát 1). Có thể chạy lâu (kiểm chứng chạy trong lệnh này): chạy nền nếu
công cụ có giới hạn thời gian.`,
		Example: `  bint run complete 01J… --run 01K… --agent-id a1b2c3 --json
  bint run complete 01J… --run 01K… --failed "subagent hết lượt" --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil { // lỗi dùng sai (mã 2) thắng lỗi "chưa init"
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			a, err := app.RunComplete(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			return finishSession(stdio, asJSON, a)
		},
	}
	c.Flags().StringVar(&o.Run, "run", "", "run_id của bước (do bint run next trả về)")
	c.Flags().StringVar(&o.AgentID, "agent-id", "", "agentId của subagent (kết quả công cụ Agent), lưu vào lượt chạy")
	c.Flags().StringVar(&o.Failed, "failed", "", "lý do subagent thất bại: bước bị đánh failed")
	c.Flags().BoolVar(&o.NoWait, "no-wait", false, "khoá worktree đang bị bước khác giữ thì dừng ngay (blocked) thay vì chờ")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}

func newRunAbortCmd(stdio IO) *cobra.Command {
	var o app.RunAbortOptions
	var asJSON bool
	c := &cobra.Command{
		Use:   "abort <task-id>",
		Short: "Bỏ bước subagent đang dở, gỡ hợp đồng thuê khoá (driver session)",
		Long: `Dùng khi phiên điều phối chết giữa bước (khoá worktree bị giữ tới hết hạn) hoặc muốn dừng: xoá bước dở và hợp đồng
thuê khoá, đóng lượt của bước là interrupted, đưa tác vụ về interrupted. Tệp b-feat đã sửa vẫn được ghi vào tập tệp của
tác vụ. Tiếp tục bằng bint run next <id> --resume (bước dở chạy lại từ đầu). Huỷ tác vụ (bint task cancel) cũng gỡ bước dở.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Ref = args[0]
			if err := o.Validate(); err != nil {
				return err
			}
			ws, err := openTasks(cmd)
			if err != nil {
				return err
			}
			a, err := app.RunAbort(cmd.Context(), ws, o)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdio.Out, a)
			}
			printSessionAction(stdio.Out, a)
			return nil
		},
	}
	c.Flags().StringVar(&o.Reason, "reason", "", "lý do bỏ bước (ghi vào lỗi của tác vụ)")
	c.Flags().BoolVar(&asJSON, "json", false, "in kết quả dạng JSON")
	return c
}
