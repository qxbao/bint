package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/gitrepo"
	"bint/internal/mcpcat"
	"bint/internal/mcpcat/probe"
	"bint/internal/mcpgw"
	"bint/internal/mcpserver"
	"bint/internal/state"
	"bint/internal/workflow"
)

// Tầng dịch vụ MCP (docs/mcp-gateway-plan.md mục 5.4): list, add, remove, check, index, grants. CLI (và sau này TUI)
// cùng gọi các hàm này. add, remove, index chỉ dành cho người (RequireHuman); list, check, grants chạy được trong phiên agent.

// MCPToolsOptions: `bint mcp tools` (không có tham số).
type MCPToolsOptions struct{}

func MCPToolsDefaults(config.Config) MCPToolsOptions { return MCPToolsOptions{} }
func (o MCPToolsOptions) Validate() error            { return nil }

// MCPTool là một công cụ MCP đang mở cho agent.
type MCPTool struct{ Name, Description string }

// MCPTools liệt kê công cụ của MCP server (không cần repository, không mở backend).
func MCPTools(version string, o MCPToolsOptions) ([]MCPTool, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	srv := mcpserver.New("bint", version, "")
	mcpserver.RegisterTools(srv, nil)
	var out []MCPTool
	for _, t := range srv.Tools() {
		out = append(out, MCPTool{Name: t.Name, Description: t.Description})
	}
	return out, nil
}

// ---- dùng chung ----

// Trạng thái của một lần check.
const (
	MCPStatusOK       = "ok"
	MCPStatusDegraded = "degraded"
	MCPStatusFail     = "fail"
)

// mcpCheckTimeout là thời gian tối đa cho phần khởi tạo của check một service (initialize, tools/list). Các lời gọi
// thử tool có hạn riêng mcpProbeToolTimeout, không bị hạn này cắt.
var mcpCheckTimeout = 30 * time.Second

// mcpProbeToolTimeout là hạn của MỖI lời gọi thử tool (tools/call). Một tool chậm chỉ làm chính nó quá hạn; các tool đứng
// sau vẫn được gọi với hạn mới (trước đây chung hạn mcpCheckTimeout nên tool chậm làm lời gọi sau bị báo sai là lỗi truyền tải).
var mcpProbeToolTimeout = 20 * time.Second

// mcpProject nạp cấu hình dự án nếu dir nằm trong repository đã init; ngược lại (ok=false) trả cấu hình mặc định.
func mcpProject(ctx context.Context, dir string) (cfg config.Config, ok bool, err error) {
	ws, err := OpenWorkspace(ctx, dir, false)
	switch {
	case errors.Is(err, gitrepo.ErrNotRepo), errors.Is(err, ErrNotInitialized):
		return config.Default(), false, nil
	case err != nil:
		return config.Default(), false, err
	}
	return ws.Config, ws.Sources.Project != "", nil
}

func mcpService(id string) (mcpcat.Service, error) {
	if strings.TrimSpace(id) == "" {
		return mcpcat.Service{}, Usagef("cần tên service (%s)", strings.Join(mcpcat.IDs(), ", "))
	}
	svc, ok := mcpcat.Lookup(id)
	if !ok {
		return mcpcat.Service{}, Usagef("service %q không có trong danh mục (%s)", id, strings.Join(mcpcat.IDs(), ", "))
	}
	return svc, nil
}

// ---- mcp list ----

// MCPListOptions: `bint mcp list` (không có tham số; --json do CLI xử lý).
type MCPListOptions struct{}

func MCPListDefaults(config.Config) MCPListOptions { return MCPListOptions{} }
func (o MCPListOptions) Validate() error           { return nil }

// MCPServiceStatus là một mục của danh mục kèm trạng thái đăng ký trên máy và việc dự án dùng.
type MCPServiceStatus struct {
	ID           string               `json:"id"`
	Prefix       string               `json:"prefix"`
	Transport    string               `json:"transport"`
	Share        string               `json:"share"`
	Groups       []string             `json:"groups"`
	Install      string               `json:"install,omitempty"`
	Registered   bool                 `json:"registered"`
	Registration *config.Registration `json:"registration,omitempty"`
	// ProjectUses: dự án khai báo service trong mcp.services; Required chỉ có nghĩa khi ProjectUses.
	ProjectUses bool `json:"project_uses"`
	Required    bool `json:"required"`
	// MissingEnv: tên biến credential service cần (danh mục và mcp.yml) mà secrets.env chưa đặt. Chỉ tên, không giá trị;
	// rỗng với service http (M1 chưa gắn credential vào header).
	MissingEnv []string `json:"missing_env,omitempty"`
}

// MCPSecretsFile trả đường dẫn secrets.env của máy này (tệp có thể chưa tồn tại). Không đọc nội dung.
func MCPSecretsFile() (string, error) {
	m, err := config.LoadMachineMCP()
	if err != nil {
		return "", err
	}
	return config.SecretsPath(m), nil
}

// MCPList trả danh mục kèm trạng thái đăng ký (mcp.yml) và khai báo của dự án tại dir. dir không phải repository
// đã init thì ProjectUses luôn false, không lỗi.
func MCPList(ctx context.Context, dir string, o MCPListOptions) ([]MCPServiceStatus, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return nil, err
	}
	cfg, _, err := mcpProject(ctx, dir)
	if err != nil {
		return nil, err
	}
	secrets, err := config.ReadSecrets(config.SecretsPath(m))
	if err != nil {
		secrets = map[string]string{} // tệp đọc lỗi: coi như chưa đặt biến nào (mcp check báo lỗi đọc chi tiết)
	}
	var out []MCPServiceStatus
	for _, svc := range mcpcat.All() {
		st := MCPServiceStatus{ID: svc.ID, Prefix: svc.Prefix, Transport: string(svc.Transport), Share: string(svc.Share),
			Groups: svc.GroupNames(), Install: svc.Install}
		if r, ok := m.Services[svc.ID]; ok {
			r := r
			st.Registered, st.Registration = true, &r
		}
		if ps, ok := cfg.MCP.Services[svc.ID]; ok {
			st.ProjectUses, st.Required = true, ps.IsRequired()
		}
		if svc.Transport == mcpcat.Stdio {
			reg := config.Registration{}
			if st.Registration != nil {
				reg = *st.Registration
			}
			_, st.MissingEnv, _ = mcpgw.Env(svc, reg, secrets)
		}
		out = append(out, st)
	}
	return out, nil
}

// ---- mcp add ----

// MCPAddOptions: `bint mcp add <service> [--command … --arg … --env-name … | --url …]`.
type MCPAddOptions struct {
	Service  string   `arg:"service" form:"required,suggest=mcp_services"`
	Command  string   `flag:"command" form:""`
	Args     []string `flag:"arg" form:""`
	EnvNames []string `flag:"env-name" form:""`
	URL      string   `flag:"url" form:""`
}

func MCPAddDefaults(config.Config) MCPAddOptions { return MCPAddOptions{} }

func (o MCPAddOptions) Validate() error {
	svc, err := mcpService(o.Service)
	if err != nil {
		return err
	}
	if o.URL != "" && (o.Command != "" || len(o.Args) > 0) {
		return Usagef("--url loại trừ --command/--arg")
	}
	if o.URL != "" {
		if svc.Transport != mcpcat.HTTP {
			return Usagef("--url chỉ dùng cho service http; %s chạy qua stdio (dùng --command)", svc.ID)
		}
		if u, err := url.Parse(o.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Usagef("--url %q không phải địa chỉ http(s)", o.URL)
		}
	}
	if (o.Command != "" || len(o.Args) > 0) && svc.Transport != mcpcat.Stdio {
		return Usagef("--command/--arg chỉ dùng cho service stdio; %s là http (dùng --url)", svc.ID)
	}
	if len(o.Args) > 0 && o.Command == "" {
		return Usagef("--arg cần đi cùng --command")
	}
	if len(o.EnvNames) > 0 && svc.Transport == mcpcat.HTTP {
		return Usagef("--env-name không dùng được cho service http (%s): M1 chưa gắn credential vào header của service http", svc.ID)
	}
	for _, n := range o.EnvNames {
		if !config.EnvNameRE.MatchString(n) {
			// Không in n: có thể là "TÊN=giá trị" gõ nhầm.
			return Usagef("--env-name chỉ nhận TÊN biến (chữ, số, gạch dưới), không nhận giá trị")
		}
	}
	return nil
}

// MCPAddResult là kết quả đăng ký.
type MCPAddResult struct {
	Service      string              `json:"service"`
	Registration config.Registration `json:"registration"`
	Replaced     bool                `json:"replaced"`
	// InstallHint: lệnh cài gợi ý khi không tìm thấy binary trên PATH (Bint không tự cài).
	InstallHint string   `json:"install_hint,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	// Check là kết quả kiểm tra ngay sau khi đăng ký; lỗi check không gỡ đăng ký.
	Check MCPCheckResult `json:"check"`
}

// MCPAdd đăng ký service vào mcp.yml (điền mặc định từ danh mục), rồi kiểm tra. Chỉ người (D5).
func MCPAdd(ctx context.Context, dir string, o MCPAddOptions) (MCPAddResult, error) {
	var res MCPAddResult
	if err := RequireHuman("bint mcp add"); err != nil {
		return res, err
	}
	if err := o.Validate(); err != nil {
		return res, err
	}
	svc, _ := mcpcat.Lookup(o.Service)
	m, err := config.LoadMachineMCP()
	if err != nil {
		return res, err
	}
	reg := config.Registration{Version: svc.Version, EnvNames: slices.Clone(o.EnvNames)}
	if len(reg.EnvNames) == 0 {
		reg.EnvNames = slices.Clone(svc.CredentialEnv)
	}
	if svc.Transport == mcpcat.HTTP {
		reg.URL = o.URL
		if reg.URL == "" {
			reg.URL = svc.URL
		}
	} else {
		reg.Command, reg.Args = o.Command, slices.Clone(o.Args)
		if reg.Command == "" {
			reg.Command = svc.Run[0]
			if len(reg.Args) == 0 {
				reg.Args = slices.Clone(svc.Run[1:])
			}
		}
		if _, err := exec.LookPath(reg.Command); err != nil {
			res.InstallHint = svc.Install
		}
		res.Warnings = mcpLauncherWarnings(reg)
	}
	_, res.Replaced = m.Services[svc.ID]
	if m.Services == nil {
		m.Services = map[string]config.Registration{}
	}
	m.Services[svc.ID] = reg
	if err := config.SaveMachineMCP(m); err != nil {
		return res, err
	}
	res.Service, res.Registration = svc.ID, reg
	checks, err := MCPCheck(ctx, dir, MCPCheckOptions{Service: svc.ID})
	if err != nil {
		return res, err
	}
	if len(checks) > 0 {
		res.Check = checks[0]
	}
	return res, nil
}

// mcpLauncherWarnings cảnh báo lệnh chạy dạng npx không ghim phiên bản (không có @<phiên bản>, hoặc @latest), uvx hoặc
// uv tool run: tải gói mỗi lần chạy, không ghim phiên bản (D1).
func mcpLauncherWarnings(r config.Registration) []string {
	base := mcpCmdBase(r.Command)
	var w []string
	switch {
	case base == "npx":
		if pkg := npxPackage(r.Args); pkg != "" && !npxPinned(pkg) {
			w = append(w, "lệnh npx chạy gói "+pkg+" không ghim phiên bản (thiếu @<phiên bản> hoặc dùng @latest): mỗi lần chạy có thể tải bản khác và sinh thêm tiến trình bọc; nên cài binary với phiên bản ghim rồi đăng ký lệnh đó")
		}
	case base == "uvx", base == "uv" && len(r.Args) >= 2 && r.Args[0] == "tool" && r.Args[1] == "run":
		w = append(w, "lệnh uvx/uv tool run chạy qua tiến trình bọc và không ghim phiên bản; nên cài bằng `uv tool install <gói>==<phiên bản>` rồi đăng ký binary đã cài")
	}
	return w
}

// mcpCmdBase là tên gốc (chữ thường, bỏ .exe/.cmd) của một lệnh.
func mcpCmdBase(cmd string) string {
	b := strings.ToLower(filepath.Base(cmd))
	return strings.TrimSuffix(strings.TrimSuffix(b, ".exe"), ".cmd")
}

// npxPackage trả về đối số gói của npx (đối số đầu không phải cờ; -p/--package <gói> đứng trước), rỗng nếu không có.
func npxPackage(args []string) string {
	for i, a := range args {
		switch {
		case a == "-p" || a == "--package":
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		case strings.HasPrefix(a, "--package="):
			return strings.TrimPrefix(a, "--package=")
		case strings.HasPrefix(a, "-"):
			continue
		default:
			return a
		}
	}
	return ""
}

// npxPinned: gói có hậu tố @<phiên bản semver đầy đủ> (vd. @1.6.2, @1.0.0-rc.1; tiền tố @ của scope không tính). Dist-tag
// (latest, next, beta) và khoảng phiên bản (^1, 1, 1.x) vẫn tải bản khác theo thời gian nên không coi là ghim (D1).
func npxPinned(pkg string) bool {
	_, ver, ok := strings.Cut(strings.TrimPrefix(pkg, "@"), "@")
	return ok && semverExactRE.MatchString(ver)
}

var semverExactRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// ---- mcp remove ----

// MCPRemoveOptions: `bint mcp remove <service>`.
type MCPRemoveOptions struct {
	Service string `arg:"service" form:"required"`
}

func MCPRemoveDefaults(config.Config) MCPRemoveOptions { return MCPRemoveOptions{} }
func (o MCPRemoveOptions) Validate() error {
	_, err := mcpService(o.Service)
	return err
}

// MCPRemoveResult là kết quả gỡ đăng ký.
type MCPRemoveResult struct {
	Service string `json:"service"`
	// ProjectUses: dự án tại dir vẫn khai báo service này trong mcp.services.
	ProjectUses bool `json:"project_uses"`
}

// MCPRemove gỡ đăng ký khỏi mcp.yml (không đụng secrets.env). Chỉ người (D5); service chưa đăng ký là lỗi.
func MCPRemove(ctx context.Context, dir string, o MCPRemoveOptions) (MCPRemoveResult, error) {
	var res MCPRemoveResult
	if err := RequireHuman("bint mcp remove"); err != nil {
		return res, err
	}
	if err := o.Validate(); err != nil {
		return res, err
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return res, err
	}
	if _, ok := m.Services[o.Service]; !ok {
		return res, Usagef("service %q chưa đăng ký trên máy này (xem `bint mcp list`)", o.Service)
	}
	delete(m.Services, o.Service)
	if err := config.SaveMachineMCP(m); err != nil {
		return res, err
	}
	cfg, _, err := mcpProject(ctx, dir)
	if err == nil {
		res.ProjectUses = cfg.MCP.Uses(o.Service)
	}
	res.Service = o.Service
	return res, nil
}

// ---- mcp check ----

// MCPCheckOptions: `bint mcp check [<service>]` (rỗng = mọi service đã đăng ký).
type MCPCheckOptions struct {
	Service string `arg:"service" form:""`
}

func MCPCheckDefaults(config.Config) MCPCheckOptions { return MCPCheckOptions{} }
func (o MCPCheckOptions) Validate() error {
	if o.Service == "" {
		return nil
	}
	_, err := mcpService(o.Service)
	return err
}

// MCPCheckResult là kết quả kiểm tra một service. Không bao giờ chứa giá trị credential.
type MCPCheckResult struct {
	Service string `json:"service"`
	// Status: ok | degraded | fail.
	Status string `json:"status"`
	// Tools là tên mọi tool upstream trả về trong tools/list.
	Tools []string `json:"tools,omitempty"`
	// Missing: tool có trong nhóm thao tác của danh mục nhưng upstream không có; Extra: tool upstream có mà danh mục không biết.
	Missing []string `json:"missing,omitempty"`
	Extra   []string `json:"extra,omitempty"`
	// MissingEnv: biến credential cần có mà secrets.env chưa đặt.
	MissingEnv []string `json:"missing_env,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

// MCPCheck gửi initialize rồi tools/list tới từng service đã đăng ký (stdio hoặc http), đối chiếu với danh mục.
// Service chỉ định mà chưa đăng ký là lỗi.
func MCPCheck(ctx context.Context, dir string, o MCPCheckOptions) ([]MCPCheckResult, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return nil, err
	}
	var ids []string
	if o.Service != "" {
		if _, ok := m.Services[o.Service]; !ok {
			return nil, Usagef("service %q chưa đăng ký trên máy này (bint mcp add %s)", o.Service, o.Service)
		}
		ids = []string{o.Service}
	} else {
		for _, svc := range mcpcat.All() {
			if _, ok := m.Services[svc.ID]; ok {
				ids = append(ids, svc.ID)
			}
		}
	}
	secretsPath := config.SecretsPath(m)
	secrets, serr := config.ReadSecrets(secretsPath)
	var permWarn string
	if fi, err := os.Stat(secretsPath); err == nil {
		permWarn = config.SecretsPermProblem(fi)
	}
	var out []MCPCheckResult
	for _, id := range ids {
		svc, _ := mcpcat.Lookup(id)
		r := MCPCheckResult{Service: id}
		if serr != nil {
			r.Status, r.Detail = MCPStatusFail, "đọc secrets.env: "+serr.Error()
		} else {
			r = mcpCheckOne(ctx, dir, svc, m.Services[id], secrets)
		}
		if permWarn != "" {
			r.Warnings = append(r.Warnings, "secrets.env: "+permWarn+" (chạy chmod 600)")
		}
		out = append(out, r)
	}
	return out, nil
}

// mcpCwd giải cwd của tiến trình upstream theo danh mục: worktree = gốc worktree của dir; state = thư mục riêng
// <state>/mcp/<service>/<worktree-id> (tạo 0700); còn lại để trống (kế thừa).
func mcpCwd(ctx context.Context, dir string, svc mcpcat.Service) (string, error) {
	switch svc.Cwd {
	case mcpcat.CwdWorktree:
		repo, err := gitrepo.Discover(ctx, dir)
		if err != nil {
			return "", fmt.Errorf("%s cần chạy trong một worktree git: %w", svc.ID, err)
		}
		return repo.Toplevel, nil
	case mcpcat.CwdState:
		ws, err := OpenWorkspace(ctx, dir, false)
		if err != nil {
			return "", fmt.Errorf("%s cần chạy trong một worktree git: %w", svc.ID, err)
		}
		p := filepath.Join(ws.Paths.Root, "mcp", svc.ID, state.RepoID(ws.Repo.Toplevel))
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", err
		}
		return p, nil
	}
	return "", nil
}

func mcpCheckOne(ctx context.Context, dir string, svc mcpcat.Service, reg config.Registration, secrets map[string]string) MCPCheckResult {
	r := MCPCheckResult{Service: svc.ID}
	fail := func(format string, a ...any) MCPCheckResult {
		r.Status, r.Detail = MCPStatusFail, fmt.Sprintf(format, a...)
		return r
	}
	var env, secretVals []string
	if svc.Transport == mcpcat.Stdio {
		// Service http chưa gắn credential vào header (M1): biến credential chỉ có nghĩa với tiến trình stdio.
		var missing []string
		env, missing, secretVals = mcpgw.Env(svc, reg, secrets)
		if len(missing) > 0 {
			r.MissingEnv = missing
			return fail("thiếu biến credential trong secrets.env: %s (không khởi động service)", strings.Join(missing, ", "))
		}
	}
	// cctx chỉ bao initialize và tools/list. Tiến trình upstream gắn với ctx (không phải cctx) để hết hạn khởi tạo
	// không giết nó giữa chừng khi đang gọi thử các tool (mỗi lời gọi có hạn riêng); cl.Close dừng nó khi xong.
	cctx, cancel := context.WithTimeout(ctx, mcpCheckTimeout)
	defer cancel()

	var cl *probe.Client
	cwd := ""
	if svc.Transport == mcpcat.HTTP {
		cl = probe.HTTP(reg.URL, nil, nil)
	} else {
		path, err := exec.LookPath(reg.Command)
		if err != nil {
			hint := ""
			if svc.Install != "" {
				hint = "; cài: " + svc.Install
			}
			return fail("không tìm thấy lệnh %q trên PATH%s", reg.Command, hint)
		}
		if cwd, err = mcpCwd(ctx, dir, svc); err != nil {
			return fail("%v", err)
		}
		cmd := exec.CommandContext(ctx, path, reg.Args...)
		cmd.Dir, cmd.Env = cwd, env
		if cl, err = probe.Stdio(cmd); err != nil {
			return fail("không khởi động được %q: %v", reg.Command, err)
		}
	}
	defer cl.Close()
	upstreamFail := func(what string, err error) MCPCheckResult {
		d := what + ": " + mcpgw.ScrubSecrets(err.Error(), secretVals, 300)
		if se := mcpgw.ScrubSecrets(cl.Stderr(), secretVals, 300); se != "" {
			d += " [stderr: " + se + "]"
		}
		return fail("%s", d)
	}
	if _, err := cl.Initialize(cctx); err != nil {
		return upstreamFail("initialize", err)
	}
	tools, err := cl.ListTools(cctx)
	if err != nil {
		return upstreamFail("tools/list", err)
	}
	have := map[string]bool{}
	for _, t := range tools {
		r.Tools = append(r.Tools, t.Name)
		have[t.Name] = true
	}
	known := svc.Tools()
	for _, t := range known {
		if !have[t] {
			r.Missing = append(r.Missing, t)
		}
	}
	for _, t := range r.Tools {
		if !slices.Contains(known, t) {
			r.Extra = append(r.Extra, t)
		}
	}
	r.Status = MCPStatusOK
	if len(r.Missing) > 0 {
		r.Status = MCPStatusDegraded
		r.Detail = "upstream thiếu tool của nhóm thao tác: " + strings.Join(r.Missing, ", ") + " (danh mục lệch phiên bản upstream?)"
	}
	if svc.ID == "codegraph" {
		mcpCodegraphProbe(ctx, cl, svc, have, cwd, secretVals, &r)
	}
	return r
}

// codegraphDisabledRE khớp văn bản lỗi báo tool bị tắt bởi bộ lọc CODEGRAPH_MCP_TOOLS của daemon.
var codegraphDisabledRE = regexp.MustCompile(`(?i)\b(disabled|not enabled)\b`)

// mcpCodegraphProbe gọi thử mọi tool của danh mục codegraph có trong tools/list: daemon codegraph lọc tool theo env của
// client ĐẦU TIÊN đã khởi động nó, nên một phiên ngoài Bint khởi động daemon với env hẹp làm tool của nhóm bị tắt bị từ
// chối dù tools/list vẫn liệt kê (kể cả codegraph_status; mục 5.5). Tool báo bị tắt (isError hoặc lỗi JSON-RPC) được gom theo
// nhóm thao tác để báo nhóm nào thiếu. Lỗi khác của tool ngoài codegraph_status (vd thiếu tham số vì gọi không đối số) bị bỏ
// qua; lỗi khác của codegraph_status báo degraded kèm nguyên văn đã che (worktree chưa có chỉ mục thì nhắc `bint mcp index codegraph`).
func mcpCodegraphProbe(ctx context.Context, cl *probe.Client, svc mcpcat.Service, have map[string]bool, cwd string, secretVals []string, r *MCPCheckResult) {
	disabled := map[string]string{} // tool -> lý do (đã che)
	var statusOther string
	var hung []string // tool quá hạn riêng mcpProbeToolTimeout
	// finish gộp phần báo tool quá hạn vào Detail cuối cùng (Status tối thiểu degraded).
	finish := func() {
		if len(hung) == 0 {
			return
		}
		d := "tool codegraph quá hạn " + mcpProbeToolTimeout.String() + " khi gọi thử: " + strings.Join(hung, ", ")
		if r.Detail != "" {
			d += "; " + r.Detail
		}
		r.Detail = d
		if r.Status == MCPStatusOK {
			r.Status = MCPStatusDegraded
		}
	}
	defer finish()
	for _, name := range svc.Tools() {
		if !have[name] {
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, mcpProbeToolTimeout)
		res, err := cl.CallTool(tctx, name, nil)
		toolExpired := tctx.Err() != nil && ctx.Err() == nil // hạn riêng của tool, không phải ctx của check
		cancel()
		var rpc *probe.RPCError
		var reason string
		switch {
		case errors.As(err, &rpc):
			reason = rpc.Error()
		case err != nil && toolExpired && errors.Is(err, context.DeadlineExceeded):
			// Chỉ tool này quá hạn: ghi lại rồi gọi tiếp các tool sau (phản hồi muộn của nó, nếu có, bị client bỏ qua).
			hung = append(hung, name)
			continue
		case err != nil:
			// Lỗi truyền tải (hết hạn, daemon chết) của bất kỳ tool nào: dừng và nêu đúng tool gây lỗi; bỏ qua thì
			// lời gọi sau (thường là codegraph_status) hỏng theo và bị gán nhầm nguyên nhân.
			r.Status = MCPStatusFail
			r.Detail = name + ": " + mcpgw.ScrubSecrets(err.Error(), secretVals, 300)
			return
		case res.IsError:
			reason = "isError: " + res.Text
		default:
			continue
		}
		reason = mcpgw.ScrubSecrets(reason, secretVals, 200)
		switch {
		case codegraphDisabledRE.MatchString(reason):
			disabled[name] = reason
		case name == "codegraph_status":
			statusOther = reason
		}
	}
	if len(disabled) > 0 {
		var parts []string
		sample := "" // nguyên văn (đã che) lỗi của tool bị tắt đầu tiên theo thứ tự danh mục, để đối chiếu với daemon
		for _, g := range svc.Groups {
			var off []string
			for _, t := range g.Tools {
				if why, ok := disabled[t]; ok {
					off = append(off, t)
					if sample == "" {
						sample = why
					}
				}
			}
			if len(off) > 0 {
				parts = append(parts, g.Name+" ("+strings.Join(off, ", ")+")")
			}
		}
		pid := filepath.Join(cwd, ".codegraph", "daemon.pid")
		r.Status = MCPStatusDegraded
		detail := "tool codegraph bị từ chối (disabled) ở nhóm thao tác: " + strings.Join(parts, "; ") +
			" (lỗi: " + sample + ")" +
			": daemon codegraph có thể đang chạy với env hẹp của một phiên khác (CODEGRAPH_MCP_TOOLS thiếu tool). " +
			"Dừng daemon cũ rồi chạy lại check: `codegraph daemon` (xem --help) hoặc kết thúc tiến trình có PID ghi trong " + pid
		if r.Detail != "" {
			detail = r.Detail + "; " + detail
		}
		r.Detail = detail
		return
	}
	if statusOther == "" {
		return
	}
	r.Status = MCPStatusDegraded
	r.Detail = "codegraph_status báo lỗi (" + statusOther + ")"
	if _, err := os.Stat(filepath.Join(cwd, codegraphDB)); err != nil {
		r.Detail += ": worktree chưa có chỉ mục " + filepath.ToSlash(codegraphDB) + ", chạy `bint mcp index codegraph`"
	}
}

// ---- mcp index ----

// MCPIndexOptions: `bint mcp index <service>` (v1 chỉ codegraph).
type MCPIndexOptions struct {
	Service string `arg:"service" form:"required"`
}

func MCPIndexDefaults(config.Config) MCPIndexOptions { return MCPIndexOptions{} }
func (o MCPIndexOptions) Validate() error {
	if _, err := mcpService(o.Service); err != nil {
		return err
	}
	if o.Service != "codegraph" {
		return Usagef("index chỉ hỗ trợ codegraph (service %q không có bước chuẩn bị riêng)", o.Service)
	}
	return nil
}

// MCPIndexResult là kết quả lập chỉ mục.
type MCPIndexResult struct {
	Service string `json:"service"`
	// Action: init (chưa có .codegraph/codegraph.db) hoặc sync.
	Action string `json:"action"`
	Cwd    string `json:"cwd"`
	// ExcludeFile là tệp git info/exclude đã đảm bảo có dòng .codegraph/; ExcludeAdded: lần này mới thêm.
	ExcludeFile  string `json:"exclude_file"`
	ExcludeAdded bool   `json:"exclude_added"`
	// Binary là binary codegraph đã chạy init/sync; Warning (nếu có): binary đó không phải lệnh đã đăng ký và phiên bản
	// của nó khác (hoặc không đọc được so với) phiên bản đã ghim, nên chỉ mục có thể do bản khác với server MCP dựng.
	Binary  string `json:"binary"`
	Warning string `json:"warning,omitempty"`
}

// codegraphDB là đường dẫn chỉ mục codegraph so với gốc worktree.
var codegraphDB = mcpgw.CodegraphDB

// MCPIndex chạy `codegraph init -y` (worktree chưa có chỉ mục) hoặc `codegraph sync` với cwd là gốc worktree của dir,
// rồi thêm `.codegraph/` vào git info/exclude (mục 5.5). Đầu ra của codegraph chuyển sang out. Chỉ người (D5).
// Lõi chạy ở mcpgw.CodegraphInit (dùng chung với bước chuẩn bị worktree của workflow, không qua RequireHuman).
func MCPIndex(ctx context.Context, dir string, o MCPIndexOptions, out io.Writer) (MCPIndexResult, error) {
	var res MCPIndexResult
	if err := RequireHuman("bint mcp index"); err != nil {
		return res, err
	}
	if err := o.Validate(); err != nil {
		return res, err
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return res, err
	}
	if _, ok := m.Services["codegraph"]; !ok {
		return res, Usagef("codegraph chưa đăng ký trên máy này (bint mcp add codegraph)")
	}
	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		return res, fmt.Errorf("index cần chạy trong một worktree git: %w", err)
	}
	r, err := mcpgw.CodegraphInit(ctx, repo.Toplevel, out)
	if err != nil {
		return res, err
	}
	return MCPIndexResult{Service: "codegraph", Action: r.Action, Cwd: repo.Toplevel, ExcludeFile: r.ExcludeFile,
		ExcludeAdded: r.ExcludeAdded, Binary: r.Binary, Warning: r.Warning}, nil
}

// ensureGitExclude giữ tên cũ cho mã gọi trong gói app.
func ensureGitExclude(ctx context.Context, root, pattern string) (string, bool, error) {
	return mcpgw.EnsureGitExclude(ctx, root, pattern)
}

// ---- mcp grants ----

// MCPGrantsOptions: `bint mcp grants [--role <r>]`.
type MCPGrantsOptions struct {
	Role string `flag:"role" form:""`
}

func MCPGrantsDefaults(config.Config) MCPGrantsOptions { return MCPGrantsOptions{} }
func (o MCPGrantsOptions) Validate() error {
	if o.Role != "" && strings.TrimSpace(o.Role) == "" {
		return Usagef("--role không được chỉ gồm khoảng trắng")
	}
	return nil
}

// MCPGrantTool là một tool đã giải từ quyền: tên upstream và tên tới agent (mcp__bint__<Agent>).
type MCPGrantTool struct {
	Service  string `json:"service"`
	Group    string `json:"group"`
	Upstream string `json:"upstream"`
	Agent    string `json:"agent"`
}

// MCPRoleGrants là quyền đã giải của một role.
type MCPRoleGrants struct {
	Role  string         `json:"role"`
	Tools []MCPGrantTool `json:"tools"`
	// Implicit: role interactive không khai báo, dựng theo D4 (mọi nhóm của service dự án khai báo và máy đã đăng ký).
	Implicit bool     `json:"implicit,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// MCPGrants giải quyền trong mcp.grants của dự án tại dir thành tên tool tới agent theo từng role (kèm role
// interactive theo D4 khi không khai báo). Báo service được cấp mà máy chưa đăng ký hoặc dự án không khai báo.
func MCPGrants(ctx context.Context, dir string, o MCPGrantsOptions) ([]MCPRoleGrants, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	cfg, ok, err := mcpProject(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotInitialized
	}
	m, err := config.LoadMachineMCP()
	if err != nil {
		return nil, err
	}
	roles := cfg.MCP.GrantRoles()
	if !slices.Contains(roles, config.MCPRoleInteractive) {
		roles = append(roles, config.MCPRoleInteractive)
	}
	if o.Role != "" {
		if !slices.Contains(roles, o.Role) {
			return nil, Usagef("role %q không có trong mcp.grants (có: %s)", o.Role, strings.Join(roles, ", "))
		}
		roles = []string{o.Role}
	}
	registered := map[string]bool{}
	for id := range m.Services {
		registered[id] = true
	}
	var out []MCPRoleGrants
	for _, role := range roles {
		// Cùng config.MCP.Resolve với daemon gateway và allowlist của bint run nên kết quả không lệch.
		res := cfg.MCP.Resolve(role, workflow.ReadOnlyRole(role), registered)
		g := MCPRoleGrants{Role: role, Tools: []MCPGrantTool{}, Implicit: res.Implicit}
		add := func(t mcpcat.ToolRef) {
			g.Tools = append(g.Tools, MCPGrantTool{Service: t.Service, Group: t.Group, Upstream: t.Upstream, Agent: t.Agent})
		}
		for _, t := range res.Granted {
			add(t)
		}
		var writes []string
		warned := map[string]bool{}
		for _, ex := range res.Excluded {
			if res.Implicit {
				continue // D4: chỉ service dự án khai báo và máy đã đăng ký; phần còn lại không phải "được cấp"
			}
			notReg, notProj := slices.Contains(ex.Reasons, config.MCPExcludeNotRegistered), slices.Contains(ex.Reasons, config.MCPExcludeNotInProject)
			readOnlyExcluded := slices.Contains(ex.Reasons, config.MCPExcludeReadOnlyStep)
			if readOnlyExcluded {
				writes = append(writes, ex.Tool.Agent)
			}
			if !notReg && !notProj {
				continue // chỉ bị loại vì bước chỉ đọc
			}
			if !readOnlyExcluded {
				add(ex.Tool) // vẫn liệt kê (kèm cảnh báo) tool được cấp mà máy chưa đăng ký / dự án chưa khai báo
			}
			if notReg && !warned["r"+ex.Tool.Service] {
				warned["r"+ex.Tool.Service] = true
				g.Warnings = append(g.Warnings, fmt.Sprintf("service %s được cấp nhưng máy này chưa đăng ký (bint mcp add %s)", ex.Tool.Service, ex.Tool.Service))
			}
			if notProj && !warned["p"+ex.Tool.Service] {
				warned["p"+ex.Tool.Service] = true
				g.Warnings = append(g.Warnings, fmt.Sprintf("service %s được cấp nhưng dự án không khai báo trong mcp.services", ex.Tool.Service))
			}
		}
		if len(writes) > 0 {
			g.Warnings = append(g.Warnings, fmt.Sprintf("role %s chạy ở bước chỉ đọc: tool ghi vào cây làm việc không được cấp dù mcp.grants có ghi (%s)", role, strings.Join(writes, ", ")))
		}
		out = append(out, g)
	}
	return out, nil
}
