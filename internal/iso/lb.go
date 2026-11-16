package iso

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Codex LB (docs/iso-plan.md mục 10.3): bỏ dò docker/pgrep//proc; URL tìm theo bốn nguồn, mỗi nguồn kiểm
// GET <gốc>/health/ready. Khi chốt `linked` URL lưu trong profile.json và các lần sau không dò nữa.

// Biến và đường dẫn liên quan tới LB.
const (
	EnvCodexLBURL    = "BINT_ISO_CODEX_LB_URL"
	EnvCodexLBAPIKey = "CODEX_LB_API_KEY"
	DefaultLBURL     = "http://127.0.0.1:2455"
	lbBackendSuffix  = "/backend-api/codex"
	lbHealthPath     = "/health/ready"
	lbProbeTimeout   = time.Second
)

// Nguồn của URL LB đã chọn.
const (
	LBFromFlag    = "flag"
	LBFromEnv     = "env"
	LBFromConfig  = "codex-config"
	LBFromDefault = "default"
)

// ErrLBUnavailable: LB được chỉ định tường minh (cờ/biến môi trường) hoặc đã chốt cho profile nhưng không
// sẵn sàng. CLI đổi thành mã thoát 69.
var ErrLBUnavailable = errors.New("dịch vụ Codex LB không sẵn sàng")

// NormalizeLBRoot đưa URL về gốc của LB: bỏ khoảng trắng, "/" cuối và hậu tố /backend-api/codex; chỉ nhận
// http/https có host, không kèm userinfo, query hay fragment.
func NormalizeLBRoot(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("URL Codex LB rỗng")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("URL Codex LB %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL Codex LB %q: chỉ nhận http hoặc https", raw)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("URL Codex LB %q: cần dạng http(s)://host[:cổng][/đường dẫn] không kèm tài khoản, query hay fragment", raw)
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, lbBackendSuffix)
	path = strings.TrimRight(path, "/")
	return u.Scheme + "://" + u.Host + path, nil
}

// LBEndpoint là URL base_url đưa cho Codex: <gốc>/backend-api/codex.
func LBEndpoint(root string) string { return strings.TrimRight(root, "/") + lbBackendSuffix }

// LBOptions là đầu vào của ResolveLB. Zero value dùng env, config và HTTP client của tiến trình.
type LBOptions struct {
	Flag string   // --lb
	Env  []string // mặc định os.Environ()
	// Client dùng cho probe (mặc định client riêng với hạn Timeout, không theo chuyển hướng).
	Client  *http.Client
	Timeout time.Duration // mỗi lần probe; mặc định 1 s
	// CodexConfig là đường dẫn config.toml của Codex thật; rỗng thì <CODEX_HOME thật hoặc ~/.codex>/config.toml.
	CodexConfig string
	// DefaultURL thay cho http://127.0.0.1:2455 (test).
	DefaultURL string
}

// ResolveLB chọn URL Codex LB theo thứ tự --lb > BINT_ISO_CODEX_LB_URL > base_url …/backend-api/codex trong
// config.toml thật > http://127.0.0.1:2455, mỗi nguồn kiểm GET /health/ready.
//   - --lb và biến môi trường là chỉ định tường minh: không sẵn sàng thì ErrLBUnavailable, không thử nguồn khác.
//   - Hai nguồn còn lại là tự dò: nguồn nào không sẵn sàng thì bỏ qua; không nguồn nào được thì trả LB rỗng
//     (Mode "") và err nil — người gọi chốt direct.
//
// Kết quả khoẻ mang Mode=linked, URL là gốc đã chuẩn hoá (không có /backend-api/codex).
func ResolveLB(ctx context.Context, o LBOptions) (LB, string, error) {
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	e := newEnvList(runtime.GOOS, env)
	restoreReal(e)
	probe := newLBProber(o)

	for _, src := range []struct{ name, raw string }{{LBFromFlag, o.Flag}, {LBFromEnv, e.val(EnvCodexLBURL)}} {
		if strings.TrimSpace(src.raw) == "" {
			continue
		}
		root, err := NormalizeLBRoot(src.raw)
		if err != nil {
			return LB{}, src.name, err
		}
		if err := probe(ctx, root); err != nil {
			return LB{}, src.name, fmt.Errorf("%w: %s (nguồn %s): %v", ErrLBUnavailable, root, src.name, err)
		}
		return LB{Mode: LBLinked, URL: root}, src.name, nil
	}

	cfgPath := o.CodexConfig
	if cfgPath == "" {
		cfgPath = realCodexConfigPath(e)
	}
	for _, raw := range codexConfigLBURLs(cfgPath) {
		root, err := NormalizeLBRoot(raw)
		if err != nil {
			continue
		}
		if probe(ctx, root) == nil {
			return LB{Mode: LBLinked, URL: root}, LBFromConfig, nil
		}
	}
	def := o.DefaultURL
	if def == "" {
		def = DefaultLBURL
	}
	if root, err := NormalizeLBRoot(def); err == nil && probe(ctx, root) == nil {
		return LB{Mode: LBLinked, URL: root}, LBFromDefault, nil
	}
	return LB{}, "", nil
}

// ProbeLB kiểm một LB đã chốt: lỗi bọc ErrLBUnavailable khi không sẵn sàng.
func ProbeLB(ctx context.Context, root string, o LBOptions) error {
	if err := newLBProber(o)(ctx, root); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrLBUnavailable, root, err)
	}
	return nil
}

func newLBProber(o LBOptions) func(context.Context, string) error {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = lbProbeTimeout
	}
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return func(ctx context.Context, root string) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, root+lbHealthPath, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("%s trả HTTP %d", lbHealthPath, resp.StatusCode)
		}
		return nil
	}
}

// realCodexConfigPath là config.toml của Codex thật: CODEX_HOME thật (env đã khôi phục) hoặc <home thật>/.codex.
func realCodexConfigPath(e *envList) string {
	if d := e.val("CODEX_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "config.toml")
	}
	home, err := realHomeOf(e)
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

// codexConfigLBURLs đọc mọi model_providers.*.base_url kết thúc /backend-api/codex trong config.toml, theo thứ
// tự tên provider (ổn định). Tệp thiếu hay hỏng thì trả rỗng: đây là nguồn dò, không phải điều kiện bắt buộc.
func codexConfigLBURLs(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		ModelProviders map[string]struct {
			BaseURL string `toml:"base_url"`
		} `toml:"model_providers"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	names := make([]string, 0, len(doc.ModelProviders))
	for n := range doc.ModelProviders {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		u := strings.TrimRight(strings.TrimSpace(doc.ModelProviders[n].BaseURL), "/")
		if strings.HasSuffix(u, lbBackendSuffix) {
			out = append(out, u)
		}
	}
	return out
}

// CodexLBArgs là bộ cờ -c của ai-isolator để Codex đi qua LB tới gốc root (chèn trước đối số của người
// dùng). env_key chỉ khi có CODEX_LB_API_KEY, để Codex không báo thiếu biến khi LB không đòi khoá.
func CodexLBArgs(root string, haveKey bool) []string {
	args := []string{
		"-c", `model_provider="codex-lb"`,
		"-c", `model_providers.codex-lb.name="openai"`,
		"-c", `model_providers.codex-lb.base_url="` + LBEndpoint(root) + `"`,
		"-c", `model_providers.codex-lb.wire_api="responses"`,
		"-c", `model_providers.codex-lb.supports_websockets=true`,
		"-c", `model_providers.codex-lb.requires_openai_auth=false`,
		"-c", `model_providers.codex-lb.http_headers={"x-openai-actor-authorization"="codex-lb"}`,
	}
	if haveKey {
		args = append(args, "-c", `model_providers.codex-lb.env_key="CODEX_LB_API_KEY"`)
	}
	return args
}

// HarnessArgs là các đối số cấp đầu của harness mà profile đòi, chèn trước đối số của người dùng và trước
// resume: Codex ở chế độ linked thì kiểm LB (ProbeLB; không sẵn sàng thì lỗi bọc ErrLBUnavailable) rồi trả
// CodexLBArgs; mọi trường hợp khác trả rỗng. env là env gốc đưa cho Env (CODEX_LB_API_KEY trong đó mới được
// chuyển tiếp, nên cũng quyết định có env_key hay không).
func HarnessArgs(ctx context.Context, p Profile, env []string) ([]string, error) {
	if p.Tool != Codex || p.Meta.LB.Mode != LBLinked {
		return nil, nil
	}
	if err := ProbeLB(ctx, p.Meta.LB.URL, LBOptions{}); err != nil {
		return nil, err
	}
	if env == nil {
		env = os.Environ()
	}
	key := strings.TrimSpace(newEnvList(runtime.GOOS, env).val(EnvCodexLBAPIKey)) != ""
	return CodexLBArgs(p.Meta.LB.URL, key), nil
}
