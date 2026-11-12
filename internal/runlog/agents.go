package runlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"bint/internal/spool"
)

// Ghép sự kiện của subagent vào lượt chạy của bước (docs/isolation-subagent-plan.md mục 6.5).
//
// Subagent của Claude Code không có biến môi trường riêng nên hook không biết run_id; sự kiện hook của nó chỉ có
// (harness, session_id) của phiên cha cùng agent_id trong payload. `bint run complete --agent-id X` ghi agent_id vào
// payload của workflow.finished của lượt con (run_id của bước). Ánh xạ agent_id ⇒ run_id được suy từ đúng sự kiện đó, không
// cần bảng mới. Hai thứ tự nạp đều được xử lý:
//   - hook tới SAU workflow.finished: resolveRun tra ánh xạ và gắn thẳng vào lượt con;
//   - hook tới TRƯỚC (hay gặp: `bint task …` của chính subagent đã tự nạp spool): khi nạp workflow.finished có agent_id,
//     adoptAgentEvents chuyển các dòng hook cùng agent_id từ lượt khác sang lượt con.
//
// Chỉ khớp agent_id đúng bằng chữ; sự kiện không có agent_id không bao giờ bị chuyển.

// agentRun tra lượt chạy của subagent theo agent_id ("" nếu chưa có workflow.finished tương ứng).
func agentRun(ctx context.Context, tx *sql.Tx, agentID string) (string, error) {
	if agentID == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT run_id FROM events
		WHERE event_type = ? AND json_extract(payload, '$.agent_id') = ?
		ORDER BY timestamp DESC LIMIT 1`, spool.WorkflowFinished, agentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// adoptAgentEvents chuyển sự kiện hook của subagent agentID đang ở lượt khác sang lượt con runID, cùng các bảng khoá theo
// event_id. file.modified/file.read được upsert lại vào artifacts của lượt con (bản ở lượt phiên cha giữ nguyên). Tín hiệu
// (run_signals) theo lượt không chuyển.
func adoptAgentEvents(ctx context.Context, tx *sql.Tx, runID, agentID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT event_id, event_type, timestamp, payload FROM events
		WHERE run_id != ?1 AND source LIKE 'hook:%' AND json_extract(payload, '$.agent_id') = ?2`, runID, agentID)
	if err != nil {
		return err
	}
	type moved struct {
		id, typ, ts string
		payload     []byte
	}
	var ms []moved
	for rows.Next() {
		var m moved
		var payload string
		if err := rows.Scan(&m.id, &m.typ, &m.ts, &payload); err != nil {
			rows.Close()
			return err
		}
		m.payload = []byte(payload)
		ms = append(ms, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	var last string
	for _, m := range ms {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET run_id = ? WHERE event_id = ?`, runID, m.id); err != nil {
			return err
		}
		for _, tbl := range []string{"run_skill_usages", "context_usages", "knowledge_citations", "observations"} {
			if _, err := tx.ExecContext(ctx, `UPDATE `+tbl+` SET run_id = ? WHERE event_id = ?`, runID, m.id); err != nil {
				return err
			}
		}
		if err := reapplyArtifacts(ctx, tx, runID, m.typ, m.ts, m.payload); err != nil {
			return err
		}
		if m.ts > last {
			last = m.ts
		}
	}
	if last != "" {
		_, err = tx.ExecContext(ctx, `UPDATE runs SET last_event_at = ?
			WHERE run_id = ? AND (last_event_at IS NULL OR last_event_at < ?)`, last, runID, last)
	}
	return err
}

// reapplyArtifacts ghi lại artifacts file.modified/file.read của sự kiện đã chuyển vào lượt con.
func reapplyArtifacts(ctx context.Context, tx *sql.Tx, runID, typ, ts string, raw []byte) error {
	if typ != spool.FileModified && typ != spool.FileRead {
		return nil
	}
	p, err := decodePayload(raw)
	if err != nil {
		return nil // payload hỏng: bỏ qua phần artifacts, sự kiện vẫn được chuyển
	}
	if typ == spool.FileModified {
		if path := str(p, "path"); path != "" {
			return upsertArtifact(ctx, tx, runID, "file.modified", path, ts)
		}
		return nil
	}
	for _, path := range strList(p["paths"]) {
		if err := upsertArtifact(ctx, tx, runID, "file.read", path, ts); err != nil {
			return err
		}
	}
	return nil
}

func decodePayload(raw []byte) (map[string]any, error) {
	var p map[string]any
	err := json.Unmarshal(raw, &p)
	return p, err
}
