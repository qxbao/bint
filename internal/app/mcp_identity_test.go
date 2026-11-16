package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpgw"
	"bint/internal/mcpserver"
	"bint/internal/spool"
	"bint/internal/ulid"
)

func TestMCPShimIdentityToSpool(t *testing.T) {
	f := newMCPFx(t, "  services: {codegraph: {}}\n  grants:\n    reviewer: [codegraph.*]\n    implementer: [codegraph.*]\n")
	f.secrets(t, "ok", "codegraph_explore")
	m := config.MachineMCP{Version: 1, Services: map[string]config.Registration{"codegraph": {Command: fakeExe(t), EnvNames: fakeEnvNames}}}
	if err := config.SaveMachineMCP(m); err != nil {
		t.Fatal(err)
	}
	d, err := mcpgw.Start(mcpgw.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	ctx := context.Background()
	ws, err := OpenWorkspace(ctx, f.root, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, harness, session, role string
		process, env                 bool
	}{
		{"explicit", "codex", "thread-1", "interactive", false, false},
		{"environment", "claude", "session-env", "interactive", false, true},
		{"process", "", "", "implementer", true, false},
		{"reviewer", "", "", "reviewer", true, false},
		{"missing", "", "", "interactive", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_SESSION_ID", "")
			o := MCPShimOptions{Harness: tc.harness, SessionID: tc.session}
			if tc.env {
				t.Setenv("CLAUDE_CODE_SESSION_ID", tc.session)
				o.Harness, o.SessionID = "", ""
			}
			if tc.process {
				o.RunID, o.TaskID = ulid.New(), ulid.New()
				if _, err := mcpgw.WriteRunBinding(ws.Paths.Runtime, mcpgw.RunBinding{RunID: o.RunID, TaskID: o.TaskID, Role: tc.role, ReadOnly: tc.role == "reviewer", Step: "b-feat"}); err != nil {
					t.Fatal(err)
				}
				bad := o
				bad.TaskID = ulid.New()
				if sh, err := NewMCPShim(ctx, f.root, bad); err != nil || sh != nil {
					t.Fatalf("mismatched binding: %v %v", sh, err)
				}
			}
			sh, err := NewMCPShim(ctx, f.root, o)
			if err != nil || sh == nil {
				t.Fatalf("shim: %v", err)
			}
			defer sh.Close()
			sh.Ready(ctx)
			srv := mcpserver.New("bint", "test", mcpserver.Instructions)
			srv.Proxy = sh
			inR, inW := io.Pipe()
			outR, outW := io.Pipe()
			serveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			go func() { <-serveCtx.Done(); inW.CloseWithError(serveCtx.Err()); outR.CloseWithError(serveCtx.Err()) }()
			done := make(chan error, 1)
			go func() { err := srv.Serve(serveCtx, inR, outW); outW.Close(); done <- err }()
			defer inW.Close()
			defer outR.Close()
			enc, dec := json.NewEncoder(inW), json.NewDecoder(outR)
			rpc := func(id int, method string, params any) map[string]any {
				t.Helper()
				if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
					t.Fatal(err)
				}
				for {
					var response map[string]any
					if err := dec.Decode(&response); err != nil {
						t.Fatal(err)
					}
					if response["id"] == float64(id) {
						return response
					}
				}
			}
			rpc(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
			list := rpc(2, "tools/list", map[string]any{})
			result, ok := list["result"].(map[string]any)
			if !ok {
				t.Fatalf("tools/list: %v", list)
			}
			var name string
			for _, v := range result["tools"].([]any) {
				tool := v.(map[string]any)
				if tool["name"] == "cg_explore" {
					name = tool["name"].(string)
				}
			}
			if name == "" {
				t.Fatalf("shim tool missing: %v", list)
			}
			response := rpc(3, "tools/call", map[string]any{"name": name, "arguments": map[string]any{"echo": "PRIVATE_MARKER"}})
			if response["error"] != nil || !strings.Contains(fmt.Sprint(response["result"]), "PRIVATE_MARKER") {
				t.Fatalf("upstream did not respond: %v", response)
			}
			inW.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			files, err := spool.Pending(ws.Paths.Spool)
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if tc.name == "missing" {
				want = 0
			}
			if len(files) != want {
				t.Fatalf("events=%d want %d", len(files), want)
			}
			if want == 0 {
				if d.Status().DroppedEvents == 0 {
					t.Fatal("missing dropped event")
				}
				return
			}
			raw, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte("PRIVATE_MARKER")) {
				t.Fatal("arguments or result leaked")
			}
			events, err := spool.ReadFile(files[0])
			if err != nil || len(events) != 1 {
				t.Fatalf("spool: %v %v", events, err)
			}
			e := events[0]
			if e.Harness != tc.harness || e.SessionID != tc.session || e.RunID != o.RunID || e.Payload["task_id"] != o.TaskID || e.Payload["role"] != tc.role {
				t.Fatalf("identity: %+v", e)
			}
			if err := os.Remove(files[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMCPShimSessionIdentity(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "claude-session")
	for _, tc := range []struct {
		name, harness, session, wantHarness, wantSession string
		wantErr                                          bool
	}{
		{"environment", "", "", "claude", "claude-session", false},
		{"explicit wins", "codex", "thread-1", "codex", "thread-1", false},
		{"partial", "codex", "", "", "", true},
		{"unknown", "unknown", "session", "", "", true},
		{"invalid header", "claude", "x\ny", "", "", true},
		{"trailing newline", "claude", "x\n", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s, err := mcpShimSessionIdentity(tc.harness, tc.session)
			if (err != nil) != tc.wantErr || h != tc.wantHarness || s != tc.wantSession {
				t.Fatalf("got %q %q %v", h, s, err)
			}
		})
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if h, s, err := mcpShimSessionIdentity("", ""); h != "" || s != "" || err != nil {
		t.Fatalf("missing identity: %q %q %v", h, s, err)
	}
}
