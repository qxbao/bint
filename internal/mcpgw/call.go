package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"bint/internal/mcpcat"
)

// toolsList trả tools/list từ cache theo (service, revision); chưa có thì khởi động upstream một lần để lấy.
func (d *Daemon) toolsList(r *http.Request, s *session, m rpcMsg) []byte {
	e, ok := d.cacheGet(s.svc.ID, s.p.rev)
	if !ok {
		if _, err := s.p.acquire(r.Context()); err != nil {
			return errorFromErr(m.ID, err)
		}
		s.p.release()
		if e, ok = d.cacheGet(s.svc.ID, s.p.rev); !ok {
			return rpcFail(m.ID, CodeUpstream, "không lấy được tools/list của %s", s.svc.ID)
		}
	}
	return resultFrame(m.ID, e.tools)
}

// groupOf tìm nhóm chứa tool; tool lạ trả nhóm rỗng (ok=false): vẫn chuyển tiếp, coi là không chỉ đọc (M3 mới lọc).
func groupOf(svc mcpcat.Service, tool string) (mcpcat.Group, bool) {
	for _, g := range svc.Groups {
		for _, t := range g.Tools {
			if t == tool {
				return g, true
			}
		}
	}
	return mcpcat.Group{}, false
}

// fillDefaults điền tham số mặc định của danh mục khi agent không truyền (vắng hoặc null); giữ nguyên giá trị đã có.
// {worktree} thay bằng gốc worktree của phân vùng; không có gốc thì không điền tham số dùng chỗ giữ đó.
func fillDefaults(svc mcpcat.Service, tool string, params map[string]json.RawMessage, root string) {
	defs := svc.DefaultArgs[tool]
	if len(defs) == 0 {
		return
	}
	var args map[string]json.RawMessage
	if raw, ok := params["arguments"]; ok && string(raw) != "null" {
		if json.Unmarshal(raw, &args) != nil {
			return // arguments không phải object: để upstream báo lỗi
		}
	}
	if args == nil {
		args = map[string]json.RawMessage{}
	}
	changed := false
	for k, v := range defs {
		if cur, ok := args[k]; ok && string(cur) != "null" {
			continue
		}
		if strings.Contains(v, mcpcat.WorktreePlaceholder) {
			if root == "" {
				continue
			}
			v = strings.ReplaceAll(v, mcpcat.WorktreePlaceholder, root)
		}
		args[k] = marshalRaw(v)
		changed = true
	}
	if changed {
		params["arguments"] = marshalRaw(args)
	}
}

func outcomeOf(res upResult, err error) string {
	if err != nil {
		var re *rpcError
		if errors.As(err, &re) {
			switch re.Code {
			case CodeBusy:
				return OutcomeBusy
			case CodeRestartLimit:
				return OutcomeLimited
			case CodeUnknownOutcome:
				return OutcomeUnknown
			case CodeTimeout:
				return OutcomeTimeout
			}
			return OutcomeUpstream
		}
		if errors.Is(err, context.Canceled) {
			return OutcomeCanceled
		}
		return OutcomeUpstream
	}
	if len(res.rpcErr) > 0 {
		return OutcomeRPCErr
	}
	var r struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(res.result, &r) == nil && r.IsError {
		return OutcomeToolErr
	}
	return OutcomeOK
}

// toolsCall xử lý một tools/call: nhóm → điền tham số mặc định → khoá exclusive → giới hạn đồng thời → upstream (khởi
// động lười) → đổi id → chờ → đổi id về → ghi mcp.called.
func (d *Daemon) toolsCall(r *http.Request, s *session, m rpcMsg) []byte {
	start := time.Now()
	var params map[string]json.RawMessage
	if json.Unmarshal(m.Params, &params) != nil || params == nil {
		return rpcFail(m.ID, codeInvalidParams, "tools/call cần params là object")
	}
	var tool string
	if json.Unmarshal(params["name"], &tool) != nil || tool == "" {
		return rpcFail(m.ID, codeInvalidParams, "tools/call thiếu name")
	}
	grp, known := groupOf(s.svc, tool)
	readOnly := known && s.svc.IsReadOnly(tool)
	fillDefaults(s.svc, tool, params, s.root)

	var res upResult
	var err error
	defer func() {
		out := outcomeOf(res, err)
		dur := time.Since(start)
		s.p.note(tool, out, dur)
		d.record(s, tool, grp.Name, out, dur)
	}()

	ctx, endCall := s.beginCall(r.Context(), m.ID)
	defer endCall()
	if known && grp.Exclusive {
		if err = s.p.lockExclusive(ctx, s.id, grp); err != nil {
			return errorFromErr(m.ID, err)
		}
	}
	// Chỗ trong nhóm (semaphore) và inflight của phân vùng: nhả khi hàm kết thúc, trừ khi lời gọi bị huỷ sau khi đã tới
	// upstream (heldCancel) — khi đó nhả lúc upstream xong lệnh cũ.
	var held []func()
	defer func() {
		var hc *heldCancel
		if errors.As(err, &hc) {
			go func() {
				<-hc.settled
				for i := len(held) - 1; i >= 0; i-- {
					held[i]()
				}
			}()
			return
		}
		for i := len(held) - 1; i >= 0; i-- {
			held[i]()
		}
	}()
	if known {
		var rel func()
		if rel, err = s.p.acquireSem(ctx, grp); err != nil {
			return errorFromErr(m.ID, err)
		}
		held = append(held, rel)
	}
	up, aerr := s.p.acquireLazy(ctx)
	if aerr != nil {
		err = aerr
		return errorFromErr(m.ID, err)
	}
	held = append(held, s.p.release)

	// progressToken của client đổi sang token của gateway; thông báo tiến độ của upstream đổi ngược rồi đẩy lên SSE.
	if meta := metaOf(params); meta != nil {
		if orig, ok := meta["progressToken"]; ok {
			gwTok := s.nextProgressToken()
			meta["progressToken"] = marshalRaw(gwTok)
			params["_meta"] = marshalRaw(meta)
			unroute := up.routeProgress(gwTok, func(pp json.RawMessage) {
				var pm map[string]json.RawMessage
				if json.Unmarshal(pp, &pm) != nil {
					return
				}
				pm["progressToken"] = orig
				s.push(requestFrame(nil, "notifications/progress", marshalRaw(pm)))
			})
			defer unroute()
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, d.opts.CallTimeout)
	defer cancel()
	res, err = up.call(callCtx, s.id, "tools/call", marshalRaw(params), readOnly, func(gw string) { s.trackCall(m.ID, gw) })
	if err != nil {
		return errorFromErr(m.ID, err)
	}
	if len(res.rpcErr) > 0 {
		b, _ := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: m.ID, Error: res.rpcErr})
		return b
	}
	return resultFrame(m.ID, res.result)
}

func metaOf(params map[string]json.RawMessage) map[string]json.RawMessage {
	raw, ok := params["_meta"]
	if !ok {
		return nil
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(raw, &meta) != nil {
		return nil
	}
	return meta
}

// forward chuyển nguyên văn một request ngoài tools/* (resources, prompts, completion) tới upstream.
func (d *Daemon) forward(r *http.Request, s *session, m rpcMsg) []byte {
	ctx, endCall := s.beginCall(r.Context(), m.ID)
	defer endCall()
	up, err := s.p.acquireLazy(ctx)
	if err != nil {
		return errorFromErr(m.ID, err)
	}
	defer s.p.release()
	callCtx, cancel := context.WithTimeout(ctx, d.opts.CallTimeout)
	defer cancel()
	res, err := up.call(callCtx, s.id, m.Method, m.Params, true, func(gw string) { s.trackCall(m.ID, gw) })
	if err != nil {
		return errorFromErr(m.ID, err)
	}
	if len(res.rpcErr) > 0 {
		b, _ := json.Marshal(rpcMsg{JSONRPC: "2.0", ID: m.ID, Error: res.rpcErr})
		return b
	}
	return resultFrame(m.ID, res.result)
}
