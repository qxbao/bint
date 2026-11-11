// Package evolvesvc là tầng dịch vụ của nhóm lệnh `bint evolve`: phát hiện cơ hội, đề xuất,
// đối chiếu, áp dụng, quay lui, hàng đợi và vòng tự động. CLI và TUI cùng gọi các hàm ở đây
// và nhận lại cấu trúc dữ liệu (không in, không phân tích văn bản, không chạy tiến trình con).
//
// Gói nằm dưới app vì gói evolve import app (không thể đặt dịch vụ này trong app).
package evolvesvc

import (
	"context"
	"fmt"
	"time"

	"bint/internal/app"
	"bint/internal/eval"
	"bint/internal/evolve"
	"bint/internal/runlog"
	"bint/internal/skills"
)

// Workspace là không gian tiến hoá: không gian đánh giá (kèm cơ sở dữ liệu) và kho tri thức.
type Workspace struct {
	*app.EvalWorkspace
	K *app.KnowledgeWS
}

// Open mở không gian tiến hoá: cần repository đã `bint init` (cơ sở dữ liệu trạng thái). Nạp
// spool trước để số liệu gồm cả sự kiện mới nhất. Người gọi phải Close.
func Open(ctx context.Context, dir string) (*Workspace, error) { return OpenIngest(ctx, dir, true) }

// OpenNoIngest như Open nhưng không nạp spool vào cơ sở dữ liệu: cho chế độ chỉ đọc của TUI (mục 7.4). Mở cơ sở
// dữ liệu vẫn có thể áp migration nếu schema cũ.
func OpenNoIngest(ctx context.Context, dir string) (*Workspace, error) {
	return OpenIngest(ctx, dir, false)
}

// OpenIngest mở không gian tiến hoá; ingest quyết định có nạp spool trước hay không.
func OpenIngest(ctx context.Context, dir string, ingest bool) (*Workspace, error) {
	e, err := app.OpenEval(ctx, dir, true)
	if err != nil {
		return nil, err
	}
	k, err := app.OpenKnowledge(ctx, dir)
	if err != nil {
		e.Close()
		return nil, err
	}
	if ingest {
		if _, err := e.WS.Ingest(ctx, e.Store, time.Now()); err != nil {
			e.Close()
			return nil, err
		}
	}
	return &Workspace{EvalWorkspace: e, K: k}, nil
}

// scanData quét cơ hội. days <= 0: cửa sổ mặc định (memory.retention_days); gitScan cập nhật
// tín hiệu commit/hoàn tác từ lịch sử Git trước khi phân tích.
func (e *Workspace) scanData(ctx context.Context, days int, gitScan bool) (evolve.ScanResult, *evolve.History, error) {
	now := time.Now()
	window := time.Duration(days) * 24 * time.Hour
	if days <= 0 {
		window = 0
	}
	if gitScan {
		// Tín hiệu đến muộn (commit, hoàn tác) cập nhật lại nhãn trước khi phân tích (mục 9.6).
		lb := window
		if lb == 0 {
			lb = evolve.DefaultWindow
		}
		if _, err := runlog.ScanGit(ctx, e.Store.DB, e.WS.Repo.Toplevel, runlog.ScanOptions{Lookback: lb}); err != nil {
			return evolve.ScanResult{}, nil, fmt.Errorf("quét lịch sử Git: %w", err)
		}
	}
	items, _, err := e.K.Items()
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	cases, err := e.Cases.Cases()
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	evolved, _, err := skills.LoadEvolved(e.K.Root)
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	in := evolve.ScanInput{DB: e.Store.DB, Cfg: e.K.Config, Items: items, Cases: cases, Now: now, Window: window,
		Evolved: evolved, Files: e.K.Source().Files(), Runtime: e.K.Paths.Runtime}
	if in.Window == 0 {
		in.Window = evolve.DefaultWindow
		if d := e.K.Config.Memory.RetentionDays; d > 0 {
			in.Window = time.Duration(d) * 24 * time.Hour
		}
	}
	h, err := evolve.LoadHistory(ctx, e.Store.DB, now, in.Window)
	if err != nil {
		return evolve.ScanResult{}, nil, err
	}
	in.History = h
	res, err := evolve.Scan(ctx, in)
	return res, h, err
}

// CaseMap trả về tác vụ đánh giá đã duyệt theo ID (rỗng nếu không đọc được).
func (e *Workspace) CaseMap() map[string]*eval.Case {
	m := map[string]*eval.Case{}
	if cs, err := e.Cases.Cases(); err == nil {
		for _, c := range cs {
			m[c.ID] = c
		}
	}
	return m
}

// ProposalState mô tả trạng thái hiển thị: đề xuất chờ có phiên bản gốc đã đổi là "outdated".
func (e *Workspace) ProposalState(p *evolve.Proposal) (string, []evolve.BaseProblem) {
	if p.Status != evolve.StatusProposed {
		return p.Status, nil
	}
	probs, err := p.CheckBaseWS(e.K)
	if err != nil || len(probs) > 0 {
		return "outdated", probs
	}
	return p.Status, nil
}
