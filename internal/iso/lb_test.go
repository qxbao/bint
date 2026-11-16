package iso

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func lbServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// closedURL là URL của một cổng vừa đóng.
func closedURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u
}

func writeCodexConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNormalizeLBRoot(t *testing.T) {
	ok := map[string]string{
		"http://127.0.0.1:2455":                           "http://127.0.0.1:2455",
		" http://127.0.0.1:2455/ ":                        "http://127.0.0.1:2455",
		"http://127.0.0.1:2455/backend-api/codex":         "http://127.0.0.1:2455",
		"https://lb.example.com/backend-api/codex/":       "https://lb.example.com",
		"https://lb.example.com/prefix/backend-api/codex": "https://lb.example.com/prefix",
	}
	for in, want := range ok {
		got, err := NormalizeLBRoot(in)
		if err != nil || got != want {
			t.Errorf("NormalizeLBRoot(%q) = %q, %v; muốn %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "  ", "ftp://x", "127.0.0.1:2455", "http://", "http://u:p@host", "http://h/?a=1", "http://h/#f"} {
		if got, err := NormalizeLBRoot(bad); err == nil {
			t.Errorf("NormalizeLBRoot(%q) = %q, muốn lỗi", bad, got)
		}
	}
}

func TestResolveLBSourceOrder(t *testing.T) {
	good, _ := lbServer(t, 200)
	good2, _ := lbServer(t, 200)
	good3, hits3 := lbServer(t, 200)
	good4, _ := lbServer(t, 200)
	cfg := writeCodexConfig(t, `[model_providers.codex-lb]
base_url = "`+good3.URL+`/backend-api/codex"
`)
	ctx := context.Background()
	env := func(kv ...string) []string { return append([]string{"HOME=/nonexistent"}, kv...) }

	cases := []struct {
		name string
		o    LBOptions
		want string
		src  string
	}{
		{"cờ thắng", LBOptions{Flag: good.URL + "/backend-api/codex", Env: env(EnvCodexLBURL + "=" + good2.URL), CodexConfig: cfg, DefaultURL: good4.URL}, good.URL, LBFromFlag},
		{"env thắng config", LBOptions{Env: env(EnvCodexLBURL + "=" + good2.URL), CodexConfig: cfg, DefaultURL: good4.URL}, good2.URL, LBFromEnv},
		{"config thắng mặc định", LBOptions{Env: env(), CodexConfig: cfg, DefaultURL: good4.URL}, good3.URL, LBFromConfig},
		{"mặc định", LBOptions{Env: env(), CodexConfig: filepath.Join(t.TempDir(), "none.toml"), DefaultURL: good4.URL}, good4.URL, LBFromDefault},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lb, src, err := ResolveLB(ctx, c.o)
			if err != nil {
				t.Fatal(err)
			}
			if lb.Mode != LBLinked || lb.URL != c.want || src != c.src {
				t.Fatalf("lb=%+v src=%q, muốn linked %s nguồn %s", lb, src, c.want, c.src)
			}
		})
	}
	if hits3.Load() == 0 {
		t.Fatal("nguồn config phải được probe /health/ready")
	}
}

func TestResolveLBExplicitFailsHard(t *testing.T) {
	good, _ := lbServer(t, 200)
	bad503, _ := lbServer(t, 503)
	closed := closedURL(t)
	ctx := context.Background()
	for name, o := range map[string]LBOptions{
		"cờ 503":        {Flag: bad503.URL, DefaultURL: good.URL},
		"cờ cổng đóng":  {Flag: closed, DefaultURL: good.URL},
		"env 503":       {Env: []string{EnvCodexLBURL + "=" + bad503.URL}, DefaultURL: good.URL},
		"env cổng đóng": {Env: []string{EnvCodexLBURL + "=" + closed}, DefaultURL: good.URL},
	} {
		t.Run(name, func(t *testing.T) {
			lb, _, err := ResolveLB(ctx, o)
			if !errors.Is(err, ErrLBUnavailable) {
				t.Fatalf("err = %v, muốn ErrLBUnavailable (không thử nguồn khác)", err)
			}
			if lb != (LB{}) {
				t.Fatalf("lb = %+v", lb)
			}
		})
	}
	// Cờ sai cú pháp không phải "không sẵn sàng".
	if _, _, err := ResolveLB(ctx, LBOptions{Flag: "ftp://x"}); err == nil || errors.Is(err, ErrLBUnavailable) {
		t.Fatalf("URL sai cú pháp: err = %v", err)
	}
}

func TestResolveLBAutoSkipsDeadAndFallsBackToDirect(t *testing.T) {
	dead, _ := lbServer(t, 500)
	good, _ := lbServer(t, 200)
	cfg := writeCodexConfig(t, `[model_providers.a]
base_url = "`+dead.URL+`/backend-api/codex"
[model_providers.b]
base_url = "https://api.openai.com/v1"
[model_providers.c]
base_url = "`+good.URL+`/backend-api/codex/"
`)
	lb, src, err := ResolveLB(context.Background(), LBOptions{Env: []string{"HOME=/x"}, CodexConfig: cfg, DefaultURL: closedURL(t)})
	if err != nil || lb.URL != good.URL || src != LBFromConfig {
		t.Fatalf("lb=%+v src=%q err=%v: bỏ qua provider hỏng và provider không phải LB, nhận provider c", lb, src, err)
	}
	lb, src, err = ResolveLB(context.Background(), LBOptions{Env: []string{"HOME=/x"}, CodexConfig: writeCodexConfig(t, "không phải toml ["), DefaultURL: closedURL(t)})
	if err != nil || lb != (LB{}) || src != "" {
		t.Fatalf("không thấy LB nào: lb=%+v src=%q err=%v, muốn rỗng và không lỗi", lb, src, err)
	}
}

func TestResolveLBReadsRealCodexConfigByDefault(t *testing.T) {
	good, _ := lbServer(t, 200)
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[model_providers.x]\nbase_url = \""+good.URL+"/backend-api/codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lb, src, err := ResolveLB(context.Background(), LBOptions{Env: []string{"HOME=" + home, "USERPROFILE=" + home}, DefaultURL: closedURL(t)})
	if err != nil || lb.URL != good.URL || src != LBFromConfig {
		t.Fatalf("lb=%+v src=%q err=%v", lb, src, err)
	}
}

func TestProbeLBTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	start := time.Now()
	err := ProbeLB(context.Background(), slow.URL, LBOptions{Timeout: 100 * time.Millisecond})
	if !errors.Is(err, ErrLBUnavailable) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v sau %v: phải hết hạn nhanh", err, time.Since(start))
	}
}

func TestCodexLBArgs(t *testing.T) {
	join := func(a []string) string { return strings.Join(a, "\x00") }
	base := []string{
		"-c", `model_provider="codex-lb"`,
		"-c", `model_providers.codex-lb.name="openai"`,
		"-c", `model_providers.codex-lb.base_url="http://127.0.0.1:2455/backend-api/codex"`,
		"-c", `model_providers.codex-lb.wire_api="responses"`,
		"-c", `model_providers.codex-lb.supports_websockets=true`,
		"-c", `model_providers.codex-lb.requires_openai_auth=false`,
		"-c", `model_providers.codex-lb.http_headers={"x-openai-actor-authorization"="codex-lb"}`,
	}
	if got := CodexLBArgs("http://127.0.0.1:2455", false); join(got) != join(base) {
		t.Fatalf("không khoá:\n%q\nmuốn\n%q", got, base)
	}
	if got := CodexLBArgs("http://127.0.0.1:2455/", true); join(got) != join(append(append([]string{}, base...), "-c", `model_providers.codex-lb.env_key="CODEX_LB_API_KEY"`)) {
		t.Fatalf("có khoá: %q", got)
	}
}
