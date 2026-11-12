package mcpgw

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Upstream MCP giả (stdio). Tham số dòng lệnh dạng khoá=giá trị:
//
//	log=<tệp>     mỗi dòng JSON một sự kiện: start (cwd, env, args) và recv (method, id, tool, cancelled…)
//	tools=a,b     tên tool của tools/list
//	child=<tệp>   sinh một tiến trình con (__child, kế thừa nhóm tiến trình) và ghi PID của nó vào tệp
//	errmsg=1      ghi biến JIRA_MCP_PASSWORD ra stderr rồi chạy tiếp bình thường
//	diemsg=1      ghi biến JIRA_MCP_PASSWORD ra stderr rồi thoát 1 ngay khi khởi động
//
// Hành vi của tools/call theo arguments: sleep_ms, exit, stall, roots, sampling, progress, bigframe, rpc_error, echo.
// Kết quả là một khối text JSON {pid, cwd, id, args, max_concurrent, ...}.
func runFake(args []string) {
	opt := map[string]string{}
	for _, a := range args {
		k, v, _ := strings.Cut(a, "=")
		opt[k] = v
	}
	f := &fake{opt: opt, out: bufio.NewWriter(os.Stdout), cancels: map[string]chan struct{}{}, replies: map[string]chan json.RawMessage{}}
	wd, _ := os.Getwd()
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	f.log(map[string]any{"ev": "start", "cwd": wd, "env": env, "args": args})
	if opt["errmsg"] != "" {
		os.Stderr.WriteString("warn " + os.Getenv("JIRA_MCP_PASSWORD") + "\nlỗi thứ hai\n")
	}
	if opt["diemsg"] != "" {
		os.Stderr.WriteString("boom " + os.Getenv("JIRA_MCP_PASSWORD") + "\n")
		os.Exit(1)
	}
	if p := opt["child"]; p != "" {
		c := exec.Command(os.Args[0], "__child")
		if err := c.Start(); err == nil {
			_ = os.WriteFile(p, []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		}
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		var m rpcMsg
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		f.handle(m)
	}
}

type fake struct {
	opt     map[string]string
	wmu     sync.Mutex
	out     *bufio.Writer
	lmu     sync.Mutex
	cur     atomic.Int64
	max     atomic.Int64
	cmu     sync.Mutex
	cancels map[string]chan struct{}
	replies map[string]chan json.RawMessage
	seq     atomic.Int64
}

func (f *fake) log(v map[string]any) {
	p := f.opt["log"]
	if p == "" {
		return
	}
	v["pid"] = os.Getpid()
	f.lmu.Lock()
	defer f.lmu.Unlock()
	fh, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	b, _ := json.Marshal(v)
	_, _ = fh.Write(append(b, '\n'))
}

func (f *fake) send(v any) {
	b, _ := json.Marshal(v)
	f.sendRaw(b)
}

func (f *fake) sendRaw(b []byte) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	f.out.Write(append(b, '\n'))
	f.out.Flush()
}

func (f *fake) handle(m rpcMsg) {
	var idStr string
	_ = json.Unmarshal(m.ID, &idStr)
	rec := map[string]any{"ev": "recv", "method": m.Method, "id": idStr}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		RequestID json.RawMessage `json:"requestId"`
	}
	_ = json.Unmarshal(m.Params, &p)
	if m.Method == "tools/call" {
		rec["tool"] = p.Name
		rec["args"] = p.Arguments
	}
	if m.Method == "notifications/cancelled" {
		var rid string
		_ = json.Unmarshal(p.RequestID, &rid)
		rec["request_id"] = rid
		f.cmu.Lock()
		if ch := f.cancels[rid]; ch != nil {
			close(ch)
			delete(f.cancels, rid)
		}
		f.cmu.Unlock()
	}
	f.log(rec)
	switch {
	case m.isResponse():
		f.cmu.Lock()
		ch := f.replies[idStr]
		delete(f.replies, idStr)
		f.cmu.Unlock()
		if ch != nil {
			raw := m.Result
			if len(m.Error) > 0 {
				raw = m.Error
			}
			ch <- raw
		}
	case m.Method == "initialize":
		f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{
			"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{"name": "fake", "version": "0.0.1", "pid": os.Getpid()}}})
	case m.Method == "tools/list":
		list := []map[string]any{}
		for _, t := range strings.Split(f.opt["tools"], ",") {
			if t = strings.TrimSpace(t); t != "" {
				list = append(list, map[string]any{"name": t, "description": "giả " + t, "inputSchema": map[string]any{"type": "object"}})
			}
		}
		f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"tools": list}})
	case m.Method == "tools/call":
		var st struct {
			Stall bool `json:"stall"`
		}
		if _ = json.Unmarshal(p.Arguments, &st); st.Stall {
			select {} // ngừng đọc stdin ngay trong vòng đọc: hàng đợi ghi của gateway sẽ đầy
		}
		go f.call(m, idStr, p.Name, p.Arguments)
	case m.isRequest():
		f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "không hỗ trợ " + m.Method}})
	}
}

// askClient gửi một request tới client (gateway) và chờ phản hồi.
func (f *fake) askClient(method string, params any) json.RawMessage {
	id := "srv_" + strconv.FormatInt(f.seq.Add(1), 10)
	ch := make(chan json.RawMessage, 1)
	f.cmu.Lock()
	f.replies[id] = ch
	f.cmu.Unlock()
	f.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		return json.RawMessage(`"timeout"`)
	}
}

func (f *fake) call(m rpcMsg, id, tool string, rawArgs json.RawMessage) {
	n := f.cur.Add(1)
	for {
		old := f.max.Load()
		if n <= old || f.max.CompareAndSwap(old, n) {
			break
		}
	}
	defer f.cur.Add(-1)
	var a struct {
		SleepMS  int             `json:"sleep_ms"`
		Exit     bool            `json:"exit"`
		Stall    bool            `json:"stall"`
		Roots    bool            `json:"roots"`
		Sampling bool            `json:"sampling"`
		Progress bool            `json:"progress"`
		BigFrame int             `json:"bigframe"`
		RPCError bool            `json:"rpc_error"`
		Echo     string          `json:"echo"`
		NoCancel bool            `json:"no_cancel"` // bỏ qua notifications/cancelled (upstream không tôn trọng huỷ)
		Meta     json.RawMessage `json:"_meta"`
	}
	_ = json.Unmarshal(rawArgs, &a)
	var meta struct {
		Meta struct {
			Token json.RawMessage `json:"progressToken"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(m.Params, &meta)
	if a.Exit {
		os.Exit(3)
	}
	if a.BigFrame > 0 {
		f.sendRaw([]byte(strings.Repeat("x", a.BigFrame)))
		select {}
	}
	extra := map[string]any{}
	if a.Roots {
		extra["roots"] = f.askClient("roots/list", map[string]any{})
	}
	if a.Sampling {
		extra["sampling"] = f.askClient("sampling/createMessage", map[string]any{"messages": []any{}})
		extra["elicitation"] = f.askClient("elicitation/create", map[string]any{"message": "?"})
	}
	if a.Progress && len(meta.Meta.Token) > 0 {
		f.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress",
			"params": map[string]any{"progressToken": meta.Meta.Token, "progress": 1, "total": 2}})
	}
	if a.SleepMS > 0 {
		ch := make(chan struct{})
		if !a.NoCancel {
			f.cmu.Lock()
			f.cancels[id] = ch
			f.cmu.Unlock()
		}
		select {
		case <-time.After(time.Duration(a.SleepMS) * time.Millisecond):
			f.cmu.Lock()
			delete(f.cancels, id)
			f.cmu.Unlock()
		case <-ch:
			f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32800, "message": "cancelled"}})
			return
		}
	}
	if a.RPCError {
		f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32000, "message": "lỗi giả"}})
		return
	}
	wd, _ := os.Getwd()
	body := map[string]any{"pid": os.Getpid(), "cwd": wd, "id": id, "tool": tool, "args": json.RawMessage(rawArgsOrNull(rawArgs)),
		"max_concurrent": f.max.Load(), "echo": a.Echo, "extra": extra}
	text, _ := json.Marshal(body)
	f.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}}}})
}

func rawArgsOrNull(r json.RawMessage) []byte {
	if len(r) == 0 {
		return []byte("null")
	}
	return r
}
