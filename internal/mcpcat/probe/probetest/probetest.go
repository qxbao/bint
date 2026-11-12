// Package probetest là MCP server (và codegraph) giả cho kiểm thử: chạy lại chính tệp thực thi của test
// (re-exec) nên chạy được trên mọi nền tảng. Gói test gọi MaybeRun() đầu TestMain.
//
// Cấu hình qua biến môi trường của tiến trình con:
//
//	BINT_FAKE_MCP     chế độ: ok | status_error | status_rpcerror (tool bị tắt) | status_error_other | status_rpcerror_other (lỗi khác, không phải tool bị tắt) | hang | exit | die (ghi biến JIRA_MCP_PASSWORD ra stderr rồi thoát 1)
//	BINT_FAKE_TOOLS   tên tool, phân tách bằng dấu phẩy (mặc định: không có tool)
//	BINT_FAKE_DUMP    tệp ghi {cwd, args, env} lúc khởi động (và đối số lệnh con init/sync)
//
// Khi argv[1] là "init" hoặc "sync" thì đóng vai CLI codegraph: ghi dump rồi thoát (init tạo .codegraph/codegraph.db).
package probetest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EnvMode là biến chọn chế độ; có mặt thì tiến trình là bản giả.
const (
	EnvMode  = "BINT_FAKE_MCP"
	EnvTools = "BINT_FAKE_TOOLS"
	EnvDump  = "BINT_FAKE_DUMP"
)

// Dump là nội dung tệp ghi lúc khởi động.
type Dump struct {
	Cwd  string            `json:"cwd"`
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
}

// MaybeRun chạy bản giả và thoát nếu EnvMode có mặt; ngược lại trả về ngay (gọi trong TestMain).
func MaybeRun() {
	mode := os.Getenv(EnvMode)
	if mode == "" {
		return
	}
	writeDump()
	if len(os.Args) > 1 && (os.Args[1] == "init" || os.Args[1] == "sync") {
		if os.Args[1] == "init" {
			_ = os.MkdirAll(".codegraph", 0o755)
			_ = os.WriteFile(".codegraph/codegraph.db", []byte("fake"), 0o644)
		}
		fmt.Println("fake codegraph", strings.Join(os.Args[1:], " "))
		os.Exit(0)
	}
	serve(mode)
	os.Exit(0)
}

func writeDump() {
	p := os.Getenv(EnvDump)
	if p == "" {
		return
	}
	wd, _ := os.Getwd()
	d := Dump{Cwd: wd, Args: os.Args[1:], Env: map[string]string{}}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		d.Env[k] = v
	}
	b, _ := json.Marshal(d)
	// Chế độ nối thêm: init rồi sync cùng ghi một tệp; mỗi dòng một lần chạy.
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// ReadDumps đọc các dòng dump trong tệp p.
func ReadDumps(p string) ([]Dump, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var out []Dump
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l == "" {
			continue
		}
		var d Dump
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

type msg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func serve(mode string) {
	if mode == "hang" {
		select {} // không bao giờ trả lời
	}
	if mode == "die" {
		fmt.Fprintln(os.Stderr, "fatal: boom token="+os.Getenv("JIRA_MCP_PASSWORD"))
		os.Exit(1)
	}
	var tools []string
	for _, t := range strings.Split(os.Getenv(EnvTools), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tools = append(tools, t)
		}
	}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	for sc.Scan() {
		var m msg
		if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.ID) == 0 {
			continue // thông báo hoặc rác
		}
		reply := func(result any) { send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}) }
		switch m.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "fake", "version": "0.0.1"}})
		case "tools/list":
			var list []map[string]any
			for _, t := range tools {
				list = append(list, map[string]any{"name": t, "description": "giả " + t, "inputSchema": map[string]any{"type": "object"}})
			}
			if list == nil {
				list = []map[string]any{}
			}
			reply(map[string]any{"tools": list})
		case "tools/call":
			if mode == "exit" {
				os.Exit(3)
			}
			switch mode {
			case "status_error":
				reply(map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "tool disabled (CODEGRAPH_MCP_TOOLS)"}}})
			case "status_rpcerror":
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32000, "message": "tool bị tắt"}})
			case "status_error_other":
				reply(map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "database not initialized"}}})
			case "status_rpcerror_other":
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32603, "message": "internal error: timeout"}})
			default:
				reply(map[string]any{"content": []map[string]any{{"type": "text", "text": "ok " + string(m.Params)}}})
			}
		default:
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "không hỗ trợ " + m.Method}})
		}
	}
}
