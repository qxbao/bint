package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"bint/internal/detect"
	"bint/internal/spool"
	"bint/internal/ulid"
)

// EnvRunID là biến môi trường chứa run_id của bước `bint run` (cùng BINT_RUN_ID mà workflow đặt cho agent).
const EnvRunID = "BINT_RUN_ID"

// CiteOptions: `bint knowledge cite <id>... [--note <ghi chú>]`.
type CiteOptions struct {
	IDs  []string
	Note string `flag:"note" form:""`
}

func (o CiteOptions) Validate() error {
	if len(o.IDs) == 0 {
		return errors.New("cần ít nhất một ID tri thức")
	}
	return nil
}

// CiteResult là các ID đã ghi nhận trích dẫn và lượt chạy nhận chúng.
type CiteResult struct {
	RunID string   `json:"run_id"`
	IDs   []string `json:"ids"`
}

// CiteKnowledge ghi nhận agent đã áp dụng các mục tri thức (như công cụ MCP cite_knowledge) cho harness không có MCP.
// ID có thể ở dạng [k:<id>]; ID không tồn tại là lỗi. Chỉ ghi một sự kiện knowledge.cited vào spool với run_id từ
// BINT_RUN_ID (nạp vào CSDL ở nơi khác, như sự kiện của hook): lệnh không ghi vào SQLite. Việc kiểm ID dùng OpenContext
// nên có mở kho tri thức và đồng bộ chỉ mục từ tệp (như mọi lệnh `bint knowledge`), nhưng bản ghi trích dẫn chỉ đi qua spool.
func CiteKnowledge(ctx context.Context, dir string, o CiteOptions) (CiteResult, error) {
	var res CiteResult
	if err := o.Validate(); err != nil {
		return res, err
	}
	runID := strings.TrimSpace(os.Getenv(EnvRunID))
	if runID == "" {
		return res, errors.New("không có " + EnvRunID + ": lệnh chỉ dùng trong lượt `bint run` (phiên có MCP của Bint thì gọi công cụ cite_knowledge)")
	}
	c, err := OpenContext(ctx, dir)
	if err != nil {
		return res, err
	}
	defer c.Close()
	// Khác công cụ MCP (chỉ lỗi khi không ID nào khớp): CLI báo lỗi nếu BẤT KỲ ID nào không tồn tại, để agent sửa ngay.
	var ids, bad []string
	for _, ref := range o.IDs {
		ref = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(ref), "[k:"), "]") // như công cụ MCP cite_knowledge
		it, err := c.Engine.Get(ref)
		if err != nil {
			bad = append(bad, ref)
			continue
		}
		ids = append(ids, it.ID)
	}
	if len(bad) > 0 {
		return res, errors.New("không có mục tri thức nào khớp: " + strings.Join(bad, ", "))
	}
	payload := map[string]any{"ids": anyStrings(ids)}
	if n := strings.TrimSpace(o.Note); n != "" {
		payload["note"] = detect.Redact(n, 300)
	}
	if err := os.MkdirAll(c.K.Paths.Spool, 0o700); err != nil {
		return res, err
	}
	ev := spool.Event{SchemaVersion: spool.SchemaVersion, EventID: "ev_cite_" + ulid.New(), Type: spool.KnowledgeCited,
		Source: "cli", Timestamp: time.Now().UTC(), RunID: runID, Payload: payload}
	if _, err := spool.Write(c.K.Paths.Spool, ev); err != nil {
		return res, err
	}
	return CiteResult{RunID: runID, IDs: ids}, nil
}

func anyStrings(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
