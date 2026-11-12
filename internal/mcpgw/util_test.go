package mcpgw

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"bint/internal/config"
	"bint/internal/mcpcat"
	"bint/internal/testutil"
)

func testutilIsolate(t *testing.T) { testutil.Isolate(t) }

func itoaPort(h *harness) string { return strconv.Itoa(h.d.Info().Port) }

func timeAfter(sec int) <-chan time.Time { return time.After(time.Duration(sec) * time.Second) }

func osArgs0() string { return os.Args[0] }
func osGetpid() int   { return os.Getpid() }

func h0Lookup(id string) (mcpcat.Service, bool) { return mcpcat.Lookup(id) }

// writeSecrets ghi secrets.env của harness.
func writeSecrets(t *testing.T, h *harness, lines ...string) {
	t.Helper()
	if err := os.WriteFile(h.secrets, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func itoaI(n int) string { return strconv.Itoa(n) }

func regHTTP(url string) config.Registration { return config.Registration{URL: url} }

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
