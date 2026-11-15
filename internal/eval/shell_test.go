package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Removing synchronization from the shared diagnostic tail must fail under
// -race when exec copies the captured stdout and stderr through separate pipes.
func TestRunProcessConcurrentOutput(t *testing.T) {
	const rows = 8192
	if os.Getenv("BINT_OUTPUT_FIXTURE") == "1" {
		var wg sync.WaitGroup
		for _, stream := range []struct {
			file *os.File
			row  string
		}{{os.Stdout, "stdout-row\n"}, {os.Stderr, "stderr-row\n"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range rows {
					if _, err := stream.file.WriteString(stream.row); err != nil {
						os.Exit(2)
					}
				}
			}()
		}
		wg.Wait()
		os.Exit(0)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	log := filepath.Join(t.TempDir(), "output.log")
	res := runProcess(context.Background(), t.TempDir(), bin,
		[]string{"-test.run=^TestRunProcessConcurrentOutput$"}, 30*time.Second, log,
		append(os.Environ(), "BINT_OUTPUT_FIXTURE=1"), nil, &stdout)
	if !res.OK() {
		t.Fatalf("subprocess failed: %+v", res)
	}
	if got := stdout.String(); got != strings.Repeat("stdout-row\n", rows) {
		t.Fatalf("stdout capture lost or mixed rows: %d bytes", len(got))
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	// exec copies the two pipes in arbitrary chunks, including chunks that
	// split a row. Check the complete payload without assuming line boundaries
	// survive the merge. The separate stdout assertion above still checks order.
	header, payload, ok := bytes.Cut(raw, []byte{'\n'})
	if !ok || !bytes.HasPrefix(header, []byte("$ ")) {
		t.Fatalf("missing command header in log: %q", raw)
	}
	want := strings.Repeat("stdout-row\n", rows) + strings.Repeat("stderr-row\n", rows)
	if len(payload) != len(want) {
		t.Fatalf("log payload has %d bytes, want %d", len(payload), len(want))
	}
	var gotBytes, wantBytes [256]int
	for _, b := range payload {
		gotBytes[b]++
	}
	for i := range want {
		wantBytes[want[i]]++
	}
	if gotBytes != wantBytes {
		t.Fatalf("log payload byte counts differ: got %v, want %v", gotBytes, wantBytes)
	}
	if len(res.Tail) > tailBytes || len(res.Tail) == 0 {
		t.Fatalf("invalid diagnostic tail length: %d", len(res.Tail))
	}
}
