package mcpgw

import (
	"time"

	"bint/internal/spool"
	"bint/internal/ulid"
)

// Kết quả ghi trong sự kiện mcp.called.
const (
	OutcomeOK       = "ok"
	OutcomeToolErr  = "tool_error"
	OutcomeRPCErr   = "rpc_error"
	OutcomeBusy     = "busy"
	OutcomeLimited  = "limited"
	OutcomeUnknown  = "unknown"
	OutcomeTimeout  = "timeout"
	OutcomeUpstream = "upstream_error"
	OutcomeCanceled = "canceled"
)

// record ghi một sự kiện mcp.called vào spool của repo chứa worktree của phiên (daemon không mở SQLite). Không ghi
// đối số hay nội dung kết quả. Repo chưa init Bint, hoặc phiên thiếu cả run_id lẫn cặp harness + session thì không ghi
// (không tổng hợp lượt giả, mục 13.2); số sự kiện bỏ được đếm trong status.
func (d *Daemon) record(s *session, tool, group, outcome string, dur time.Duration) {
	if s.spool == "" || (s.ctx.RunID == "" && (s.ctx.Harness == "" || s.ctx.SessionID == "")) {
		d.dropped.Add(1)
		return
	}
	ev := spool.Event{
		SchemaVersion: spool.SchemaVersion,
		EventID:       ulid.New(),
		Type:          spool.MCPCalled,
		Source:        "mcpgw",
		Timestamp:     time.Now().UTC(),
		Harness:       s.ctx.Harness,
		SessionID:     s.ctx.SessionID,
		RunID:         s.ctx.RunID,
		Worktree:      s.root,
		Payload: map[string]any{
			"task_id":     s.ctx.TaskID,
			"role":        s.ctx.Role,
			"service":     s.svc.ID,
			"tool":        tool,
			"group":       group,
			"outcome":     outcome,
			"duration_ms": dur.Milliseconds(),
		},
	}
	if _, err := spool.Write(s.spool, ev); err != nil {
		d.dropped.Add(1)
		d.logf("ghi spool mcp.called: %v", err)
	}
}
