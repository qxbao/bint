package claude

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"bint/internal/config"
)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent-x.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func asst(id, model string, in, out, cw, cr int) string {
	return `{"type":"assistant","message":{"id":"` + id + `","model":"` + model + `","usage":{"input_tokens":` + itoa(in) +
		`,"output_tokens":` + itoa(out) + `,"cache_creation_input_tokens":` + itoa(cw) + `,"cache_read_input_tokens":` + itoa(cr) + `}}}`
}

func itoa(n int) string { return strconv.Itoa(n) }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Dòng lặp cùng message.id (mỗi khối nội dung một dòng, usage lũy kế) chỉ đếm một lần, lấy bản cuối.
func TestSubagentUsageDedupByMessageID(t *testing.T) {
	p := writeTranscript(t,
		`{"type":"user","message":{"role":"user","content":"x"}}`,
		asst("m1", "claude-sonnet-5-5", 10, 5, 100, 0),
		asst("m1", "claude-sonnet-5-5", 10, 50, 100, 0), // cùng message.id, usage lũy kế
		asst("m2", "claude-sonnet-5-5", 20, 30, 0, 1000),
		`{"type":"assistant","message":{"id":"m3","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`,
		`không phải JSON`,
	)
	u := SubagentUsage(p)
	if u == nil || u.Turns != 2 || len(u.Models) != 1 {
		t.Fatalf("%+v", u)
	}
	m := u.Models[0]
	if m.Input != 30 || m.Output != 80 || m.CacheWrite != 100 || m.CacheRead != 1000 {
		t.Fatalf("%+v", m)
	}
	if in, out := u.Totals(); in != 1130 || out != 80 {
		t.Fatalf("totals %d %d", in, out)
	}
	// USD theo bảng dựng sẵn Sonnet 5.5: 2 / 10 / ghi 2,5 / đọc 0,1 (USD mỗi triệu token).
	want := (30*2.0 + 80*10.0 + 100*2.5 + 1000*0.1) / 1e6
	if c := u.Cost(nil); c == nil || !near(*c, want) {
		t.Fatalf("cost %v, muốn %v", c, want)
	}
	if u.Model() != "claude-sonnet-5-5" {
		t.Fatalf("model %q", u.Model())
	}
}

func TestSubagentUsageUnknownCases(t *testing.T) {
	if SubagentUsage(filepath.Join(t.TempDir(), "khong-co.jsonl")) != nil {
		t.Fatal("thiếu transcript ⇒ nil")
	}
	if SubagentUsage(writeTranscript(t, `{"type":"user"}`)) != nil {
		t.Fatal("không có dòng usage ⇒ nil")
	}
	u := SubagentUsage(writeTranscript(t, asst("a", "claude-sonnet-5-5", 1, 1, 0, 0), asst("b", "claude-lạ-9", 1, 1, 0, 0)))
	if u == nil || u.Cost(nil) != nil {
		t.Fatalf("model không có giá ⇒ chi phí nil: %+v", u)
	}
	// Cấu hình cho model lạ ⇒ tính được, cache để 0 suy từ giá input.
	over := map[string]config.ModelPrice{"claude-lạ-9": {InputUSDPerMTok: 10, OutputUSDPerMTok: 20}}
	if c := u.Cost(over); c == nil || !near(*c, (1*2.0+1*10.0+1*10+1*20)/1e6) {
		t.Fatalf("cost với override %v", c)
	}
}

func TestPriceForOverrideAndDateSuffix(t *testing.T) {
	if p, ok := PriceFor("claude-haiku-4-5-20251001", nil, false); !ok || p.InputUSDPerMTok != 1 || p.OutputUSDPerMTok != 5 {
		t.Fatalf("hậu tố ngày: %+v %v", p, ok)
	}
	over := map[string]config.ModelPrice{"claude-haiku-4-5": {InputUSDPerMTok: 7, OutputUSDPerMTok: 9, CacheWriteUSDPerMTok: 1, CacheReadUSDPerMTok: 2}}
	if p, _ := PriceFor("claude-haiku-4-5-20251001", over, false); p.InputUSDPerMTok != 7 || p.CacheReadUSDPerMTok != 2 {
		t.Fatalf("cấu hình phải thắng bảng dựng sẵn: %+v", p)
	}
	if p, ok := PriceFor("claude-sonnet-5-5[1m]", nil, false); !ok || p.InputUSDPerMTok != 2 {
		t.Fatalf("hậu tố ngữ cảnh: %+v %v", p, ok)
	}
	if _, ok := PriceFor("gpt-6", nil, false); ok {
		t.Fatal("model ngoài Claude không có giá")
	}
	// Haiku 5.5 có bậc giá dài khi prompt vượt ngưỡng.
	short, _ := PriceFor("claude-haiku-5-5", nil, false)
	long, _ := PriceFor("claude-haiku-5-5", nil, true)
	if short.InputUSDPerMTok != 0.10 || long.InputUSDPerMTok != 0.50 {
		t.Fatalf("bậc giá Haiku 5.5: %+v %+v", short, long)
	}
	p := writeTranscript(t, asst("big", "claude-haiku-5-5", 10, 1000, 0, 150000))
	if c := SubagentUsage(p).Cost(nil); c == nil || !near(*c, (10*0.5+1000*2.5+150000*0.05)/1e6) {
		t.Fatalf("lượt prompt dài phải tính bậc giá dài: %v", c)
	}
}
