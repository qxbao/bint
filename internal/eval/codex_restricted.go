package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// CodexRestrictedAgent uses an environment-free app-server thread. Native tools
// are removed from the transient model catalog as well as feature configuration:
// model.tool_mode and multi_agent_version otherwise override feature flags in
// Codex 0.167. ReadTools exposes only Bint's bounded filesystem readers, never a
// shell. Before sending user data, the same binary/config/catalog must pass a
// local Responses probe of both tools and additional_tools. An unsupported
// binary, model or new tool surface fails closed, without calling the real model.
type CodexRestrictedAgent struct {
	Binary    string
	Model     string
	ReadTools bool
}

func (a *CodexRestrictedAgent) Name() string           { return "codex" }
func (a *CodexRestrictedAgent) RequestedModel() string { return a.Model }
func (a *CodexRestrictedAgent) ReportsCost() bool      { return false }
func (a *CodexRestrictedAgent) EnforcesTurns() bool    { return false }
func (a *CodexRestrictedAgent) ReadOnlyMode() string {
	return "Codex environments=[] + Bint tool allowlist/probe"
}
func (a *CodexRestrictedAgent) bin() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "codex"
}
func (a *CodexRestrictedAgent) Version(ctx context.Context) string {
	return binaryVersion(ctx, a.bin())
}

func (a *CodexRestrictedAgent) Run(ctx context.Context, req AgentRequest) (res AgentResult) {
	start := time.Now()
	res.ExitCode = -1
	res.Model = a.Model
	if req.Timeout <= 0 {
		req.Timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	defer func() {
		res.Duration = time.Since(start)
		res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		if res.Error != "" {
			res.Text = ""
		}
	}()
	// No project config/instructions are discovered from the source tree. Bint's
	// read handlers alone know req.Dir; app-server always runs in this empty cwd.
	cwd, err := os.MkdirTemp("", "bint-codex-restricted-")
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer os.RemoveAll(cwd)
	isolated := req
	isolated.Dir = cwd
	env, cleanup, err := codexEvalProfile(isolated, cleanEnv(req.Env...))
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer cleanup()
	profile := envValue(env, "CODEX_HOME")
	env = append(env, "XDG_STATE_HOME="+filepath.Join(profile, "state"))
	cfgPath := filepath.Join(profile, "config.toml")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		res.Error = err.Error()
		return
	}
	var cfg map[string]any
	if err = toml.Unmarshal(raw, &cfg); err != nil {
		res.Error = err.Error()
		return
	}
	model := a.Model
	if model == "" {
		model, _ = cfg["model"].(string)
	}
	if model == "" {
		res.Error = "Codex restricted agent requires an explicit model or profile model"
		return
	}
	res.Model = model
	// Read the CLI's own catalog, retaining the requested model's inference
	// metadata and instructions. No substitute model is selected.
	cmd := exec.CommandContext(ctx, a.bin(), "debug", "models")
	cmd.Dir = cwd
	cmd.Env = withoutCodexSession(env)
	prepareProcess(cmd)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	if err != nil {
		res.Error = "cannot read Codex model catalog"
		return
	}
	catalog, err := restrictedCodexCatalog(out, model)
	if err != nil {
		res.Error = err.Error()
		return
	}
	catalogPath := filepath.Join(profile, "models.json")
	if err = os.WriteFile(catalogPath, catalog, 0o600); err != nil {
		res.Error = err.Error()
		return
	}
	cfg["model"] = model
	cfg["model_catalog_json"] = catalogPath
	cfg["web_search"] = "disabled"
	delete(cfg, "projects")
	cfg["features"] = restrictedCodexFeatures()
	cfg["tools"] = map[string]any{"update_plan": map[string]bool{"enabled": false}, "experimental_request_user_input": map[string]bool{"enabled": false}}
	// The old exec-server/host variables are session identity, not authentication.
	env = withoutCodexSession(env)
	if err = writeCodexConfig(cfgPath, cfg); err != nil {
		res.Error = err.Error()
		return
	}
	// Probe only a harmless prompt against a local endpoint. The callback checks
	// the actual model request, not the model's claim about its available tools.
	proof, err := a.probe(ctx, cwd, env, cfgPath, cfg, model)
	if err != nil {
		res.Error = "Codex tool isolation probe: " + err.Error()
		return
	}
	if req.LogPath != "" {
		b, _ := json.Marshal(proof)
		if err = os.MkdirAll(filepath.Dir(req.LogPath), 0o700); err == nil {
			err = os.WriteFile(req.LogPath, append(b, '\n'), 0o600)
		}
		if err != nil {
			res.Error = err.Error()
			return
		}
	}
	res, err = a.runRestrictedThread(ctx, cwd, env, model, req.Prompt, req.Dir)
	if err != nil {
		res.ExitCode = -1
		res.Error = err.Error()
	}
	return
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}
	return ""
}
func withoutCodexSession(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_TOOL_CALL_ID":
			continue
		}
		out = append(out, kv)
	}
	return out
}
func restrictedCodexFeatures() map[string]bool {
	f := map[string]bool{}
	for _, key := range []string{"shell_tool", "unified_exec", "view_image", "apps", "plugins", "hooks", "multi_agent", "multi_agent_v2", "code_mode", "code_mode_host", "code_mode_only", "image_generation", "computer_use", "browser_use", "goals", "sleep_tool", "skill_search", "tool_search", "default_mode_request_user_input", "send_message_to_user_async", "memories"} {
		f[key] = false
	}
	f["skip_host_skill_discovery"] = true
	return f
}
func writeCodexConfig(path string, cfg map[string]any) error {
	b, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
func restrictedCodexCatalog(raw []byte, model string) ([]byte, error) {
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("invalid Codex model catalog: %w", err)
	}
	for _, m := range catalog.Models {
		if m["slug"] != model {
			continue
		}
		for _, key := range []string{"tool_mode", "multi_agent_version", "multi_agent_reasoning_effort", "apply_patch_tool_type"} {
			delete(m, key)
		}
		m["experimental_supported_tools"] = []string{}
		m["supports_search_tool"] = false
		for _, key := range []string{"include_skills_usage_instructions", "include_plugin_usage_instructions", "include_apps_usage_instructions"} {
			m[key] = false
		}
		return json.Marshal(map[string]any{"models": []any{m}})
	}
	return nil, fmt.Errorf("requested Codex model %q is absent from its catalog; refusing fallback", model)
}

// restrictedToolNames rejects every model-visible tool except exactly the
// dynamic readers (or none). It traverses namespaces and additional_tools;
// deferred/tool_search/Code Mode/hosted tools cannot pass as empty namespaces.
func restrictedToolNames(raw []byte, read bool) ([]string, error) {
	// A new top-level surface must be reviewed before a new CLI can send data.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, key := range []string{"model", "stream", "instructions", "input", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "store", "include", "prompt_cache_key", "text", "client_metadata", "service_tier"} {
		known[key] = true
	}
	for key := range fields {
		if !known[key] {
			return nil, fmt.Errorf("unreviewed Responses request field %q", key)
		}
	}
	var req struct {
		Model string            `json:"model"`
		Tools []json.RawMessage `json:"tools"`
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	var names []string
	var visit func(json.RawMessage) error
	visit = func(raw json.RawMessage) error {
		var t struct {
			Type, Name   string
			Tools        []json.RawMessage
			DeferLoading bool `json:"defer_loading"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return err
		}
		if t.Type == "namespace" {
			if t.Name != "functions" || len(t.Tools) == 0 {
				return errors.New("unexpected tool namespace")
			}
			for _, nested := range t.Tools {
				if err := visit(nested); err != nil {
					return err
				}
			}
			return nil
		}
		if !read || t.Type != "function" || t.DeferLoading || !isCodexReader(t.Name) {
			return fmt.Errorf("unexpected tool %q (%s)", t.Name, t.Type)
		}
		for _, name := range names {
			if name == t.Name {
				return errors.New("duplicate reader tool")
			}
		}
		names = append(names, t.Name)
		return nil
	}
	for _, t := range req.Tools {
		if err := visit(t); err != nil {
			return nil, err
		}
	}
	for _, raw := range req.Input {
		var item struct {
			Type  string
			Tools []json.RawMessage
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		switch item.Type {
		case "message":
		case "additional_tools":
			for _, t := range item.Tools {
				if err := visit(t); err != nil {
					return nil, err
				}
			}
		default:
			return nil, fmt.Errorf("unexpected probe input type %q", item.Type)
		}
	}
	if read && len(names) != 3 {
		return nil, errors.New("reader allowlist incomplete")
	}
	return names, nil
}

type codexIsolationProof struct {
	Isolated bool     `json:"isolated"`
	Model    string   `json:"model"`
	Tools    []string `json:"tools"`
	Probe    string   `json:"probe"`
}

func (a *CodexRestrictedAgent) probe(ctx context.Context, cwd string, env []string, cfgPath string, cfg map[string]any, model string) (codexIsolationProof, error) {
	proof := codexIsolationProof{Model: model, Tools: []string{}, Probe: "real Codex CLI; local fixture Responses endpoint, not live model"}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return proof, err
	}
	original, err := os.ReadFile(cfgPath)
	if err != nil {
		listener.Close()
		return proof, err
	}
	defer os.WriteFile(cfgPath, original, 0o600)
	checked := make(chan error, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		r.Body.Close()
		if err == nil {
			var identity struct{ Model string }
			if err = json.Unmarshal(raw, &identity); err == nil && identity.Model != model {
				err = errors.New("probe model changed")
			}
		}
		if err == nil {
			_, err = restrictedToolNames(raw, a.ReadTools)
		}
		select {
		case checked <- err:
		default:
		}
		if err != nil {
			http.Error(w, "tool isolation failed", http.StatusBadRequest)
			return
		}
		// No user input or credentials are recorded or echoed by this endpoint.
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": "bint-probe", "status": "in_progress"}},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"type": "message", "id": "probe-message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "OK", "annotations": []any{}}}}},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "bint-probe", "status": "completed", "output": []any{}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	// Keep every inference/tool setting, changing only the endpoint/auth for the
	// harmless probe. The authoritative catalog and model are identical.
	probeCfg := map[string]any{}
	for k, v := range cfg {
		probeCfg[k] = v
	}
	provider, _ := cfg["model_provider"].(string)
	if provider == "" {
		provider = "openai"
	}
	probeCfg["model_provider"] = provider
	probeCfg["model_providers"] = map[string]any{provider: map[string]any{"name": "Bint isolation probe", "base_url": "http://" + listener.Addr().String() + "/v1", "wire_api": "responses", "requires_openai_auth": false, "supports_websockets": false, "request_max_retries": 0, "stream_max_retries": 0}}
	if err = writeCodexConfig(cfgPath, probeCfg); err != nil {
		return proof, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, runErr := a.runRestrictedThread(probeCtx, cwd, env, model, "Reply OK. Do not use tools.", "")
	select {
	case err = <-checked:
	default:
		err = errors.New("codex did not send a verifiable model request")
	}
	if err != nil {
		return proof, err
	}
	if runErr != nil {
		return proof, runErr
	}
	if err = os.WriteFile(cfgPath, original, 0o600); err != nil {
		return proof, err
	}
	proof.Isolated = true
	if a.ReadTools {
		proof.Tools = []string{"Read", "Grep", "Glob"}
	}
	return proof, nil
}

// app-server JSON-RPC is line-delimited. Unknown server requests are fatal;
// there is no approval fallback and no shell/tool call execution in this client.
type restrictedRPC struct {
	ctx      context.Context
	cmd      *exec.Cmd
	input    io.WriteCloser
	scan     *bufio.Scanner
	readRoot string
	read     bool
	thread   string
	result   AgentResult
}
type codexRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (a *CodexRestrictedAgent) runRestrictedThread(ctx context.Context, cwd string, env []string, model, prompt, readRoot string) (AgentResult, error) {
	c := &restrictedRPC{ctx: ctx, read: a.ReadTools, readRoot: readRoot, result: AgentResult{Model: model}}
	c.cmd = exec.CommandContext(ctx, a.bin(), "--no-daemon", "--strict-config", "app-server", "--listen", "stdio://")
	c.cmd.Dir = cwd
	c.cmd.Env = env
	c.cmd.Stderr = io.Discard
	prepareProcess(c.cmd)
	c.cmd.WaitDelay = 5 * time.Second
	var err error
	c.input, err = c.cmd.StdinPipe()
	if err != nil {
		return c.result, err
	}
	output, err := c.cmd.StdoutPipe()
	if err != nil {
		return c.result, err
	}
	c.scan = bufio.NewScanner(output)
	c.scan.Buffer(make([]byte, 4096), 8<<20)
	if err = c.cmd.Start(); err != nil {
		return c.result, err
	}
	defer func() { c.input.Close(); _ = c.cmd.Cancel(); _ = c.cmd.Wait() }()
	_, err = c.call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "bint_restricted", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err != nil {
		return c.result, err
	}
	if err = c.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return c.result, err
	}
	tools := []any{}
	if a.ReadTools {
		tools = codexReaderSpecs()
	}
	response, err := c.call(2, "thread/start", map[string]any{"model": model, "allowProviderModelFallback": false, "cwd": cwd, "ephemeral": true, "environments": []any{}, "runtimeWorkspaceRoots": []any{}, "dynamicTools": tools, "approvalPolicy": "never", "sandbox": "read-only"})
	if err != nil {
		return c.result, err
	}
	var thread struct {
		Model                 string
		InstructionSources    []string
		RuntimeWorkspaceRoots []string
		Thread                struct {
			ID           string
			Environments json.RawMessage
		}
	}
	if err = json.Unmarshal(response, &thread); err != nil {
		return c.result, err
	}
	if thread.Model != model || string(thread.Thread.Environments) != "[]" || len(thread.InstructionSources) > 0 || len(thread.RuntimeWorkspaceRoots) > 0 || thread.Thread.ID == "" {
		return c.result, errors.New("codex thread did not retain isolated environments/model/instructions")
	}
	c.thread = thread.Thread.ID
	_, err = c.call(3, "turn/start", map[string]any{"threadId": c.thread, "environments": []any{}, "input": []any{map[string]any{"type": "text", "text": prompt}}})
	if err != nil {
		return c.result, err
	}
	for {
		msg, err := c.next()
		if err != nil {
			return c.result, err
		}
		if msg.Method == "turn/completed" {
			var p struct {
				Turn struct {
					Status string
					Items  []struct{ Type, Text, Phase string }
				}
			}
			if err = json.Unmarshal(msg.Params, &p); err != nil {
				return c.result, err
			}
			if p.Turn.Status != "completed" {
				return c.result, fmt.Errorf("codex restricted turn %s", p.Turn.Status)
			}
			for _, item := range p.Turn.Items {
				if item.Type == "agentMessage" && item.Phase != "commentary" {
					c.result.Text = item.Text
				}
			}
			if c.result.Text == "" {
				return c.result, errors.New("codex restricted turn returned no final text")
			}
			return c.result, nil
		}
	}
}
func (c *restrictedRPC) send(v any) error { return json.NewEncoder(c.input).Encode(v) }
func (c *restrictedRPC) call(id int, method string, params any) (json.RawMessage, error) {
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := c.next()
		if err != nil {
			return nil, err
		}
		if string(msg.ID) == fmt.Sprint(id) {
			if len(msg.Error) > 0 && string(msg.Error) != "null" {
				return nil, fmt.Errorf("codex rejected %s: %s", method, truncate(string(msg.Error), 300))
			}
			return msg.Result, nil
		}
	}
}
func (c *restrictedRPC) next() (codexRPCMessage, error) {
	for c.scan.Scan() {
		var m codexRPCMessage
		if err := json.Unmarshal(c.scan.Bytes(), &m); err != nil {
			return m, err
		}
		if m.Method != "" && len(m.ID) > 0 {
			if m.Method != "item/tool/call" || !c.read {
				return m, fmt.Errorf("unexpected Codex server request %q", m.Method)
			}
			var p struct {
				Tool      string
				Arguments json.RawMessage
				ThreadID  string `json:"threadId"`
				Namespace *string
			}
			if err := json.Unmarshal(m.Params, &p); err != nil {
				return m, err
			}
			if p.ThreadID != c.thread || p.Namespace != nil || !isCodexReader(p.Tool) {
				return m, errors.New("unexpected Codex reader request")
			}
			text, err := runCodexReaderContext(c.ctx, c.readRoot, p.Tool, p.Arguments)
			if err != nil {
				text = err.Error()
			}
			if e := c.send(map[string]any{"id": m.ID, "result": map[string]any{"success": err == nil, "contentItems": []any{map[string]string{"type": "inputText", "text": text}}}}); e != nil {
				return m, e
			}
			continue
		}
		if m.Method == "thread/tokenUsage/updated" {
			var p struct {
				TokenUsage struct {
					Total struct{ InputTokens, OutputTokens int64 }
				}
			}
			if json.Unmarshal(m.Params, &p) == nil {
				c.result.InputTokens = &p.TokenUsage.Total.InputTokens
				c.result.OutputTokens = &p.TokenUsage.Total.OutputTokens
			}
		}
		if m.Method == "item/started" || m.Method == "item/completed" {
			var p struct{ Item struct{ Type string } }
			if json.Unmarshal(m.Params, &p) != nil {
				return m, errors.New("invalid Codex item")
			}
			switch p.Item.Type {
			case "agentMessage", "reasoning", "userMessage":
			case "dynamicToolCall":
				if !c.read {
					return m, errors.New("unexpected tool in no-tools thread")
				}
			default:
				return m, fmt.Errorf("unexpected Codex item type %q", p.Item.Type)
			}
		}
		if m.Method == "error" {
			return m, errors.New("codex restricted turn reported an error")
		}
		return m, nil
	}
	if err := c.scan.Err(); err != nil {
		return codexRPCMessage{}, err
	}
	return codexRPCMessage{}, errors.New("codex app-server closed before completion")
}
