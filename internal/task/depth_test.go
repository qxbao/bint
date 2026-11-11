package task

import (
	"encoding/json"
	"strings"
	"testing"
)

func lines(n int) string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, "dòng việc")
	}
	return strings.Join(out, "\n")
}

func words(n int) string { return strings.TrimSpace(strings.Repeat("từ ", n)) }

func TestChoosePlanningDepth(t *testing.T) {
	low := Assessment{Risk: "low"}
	high := Assessment{Risk: "high", Protected: []string{"migrations/**"}}
	cases := []struct {
		name       string
		req        string
		a          Assessment
		depth, why string
	}{
		{"protected", "đổi tên cột", high, DepthDeep, DepthReasonProtected},
		{"protected ưu tiên khi cũng dài", lines(8), high, DepthDeep, DepthReasonProtected},
		{"4 dòng", lines(4), low, DepthStandard, DepthReasonDefault},
		{"3 dòng có Plan=true", lines(3), low, DepthStandard, DepthReasonDefault},
		{"đúng 100 từ", words(100), low, DepthStandard, DepthReasonDefault},
		{"5 dòng", lines(5), low, DepthDeep, DepthReasonComplex},
		{"101 từ", words(101), low, DepthDeep, DepthReasonComplex},
		{"dòng trống không tính", "a\n\n\nb\n\n", low, DepthStandard, DepthReasonDefault},
		{"ngắn", "thêm cờ --verbose", low, DepthStandard, DepthReasonDefault},
	}
	for _, c := range cases {
		d, why := ChoosePlanningDepth(c.req, c.a)
		if d != c.depth || why != c.why {
			t.Errorf("%s: %s/%s, muốn %s/%s", c.name, d, why, c.depth, c.why)
		}
	}
}

// Ngưỡng deep cao hơn ngưỡng Plan của Classify: không đổi việc chọn có chạy b-req hay không.
func TestDeepThresholdsAboveClassify(t *testing.T) {
	if DeepMinLines <= 3 || DeepMinWords <= 61 {
		t.Fatalf("ngưỡng deep phải cao hơn Classify: %d dòng, %d từ", DeepMinLines, DeepMinWords)
	}
	if c := Classify(lines(3)); !c.Plan {
		t.Fatalf("Classify 3 dòng giữ Plan=true: %+v", c)
	}
	if c := Classify(words(61)); !c.Plan {
		t.Fatalf("Classify >60 từ giữ Plan=true: %+v", c)
	}
}

func TestClassificationDepthJSONRoundTrip(t *testing.T) {
	var old struct{ Classification *Classification }
	if err := json.Unmarshal([]byte(`{"Classification":{"plan":true,"reason":"x"}}`), &old); err != nil {
		t.Fatal(err)
	}
	c := old.Classification
	if c.Depth != "" || c.EffectiveDepth() != DepthStandard {
		t.Fatalf("state cũ thiếu depth phải là standard: %+v", c)
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "depth") {
		t.Fatalf("depth rỗng không được ghi ra: %s", b)
	}
	d := Classification{Plan: true, Depth: DepthDeep, DepthReason: DepthReasonComplex}
	b, _ = json.Marshal(d)
	var back Classification
	if json.Unmarshal(b, &back) != nil || back != d || back.EffectiveDepth() != DepthDeep {
		t.Fatalf("round-trip: %s → %+v", b, back)
	}
}

func TestNormalizeDepth(t *testing.T) {
	for in, want := range map[string]string{"": "", "auto": "", " AUTO ": "", "standard": DepthStandard, "deep": DepthDeep, "Deep": DepthDeep} {
		got, err := NormalizeDepth(in)
		if err != nil || got != want {
			t.Errorf("%q → %q %v", in, got, err)
		}
	}
	if _, err := NormalizeDepth("shallow"); err == nil || !strings.Contains(err.Error(), "auto | standard | deep") {
		t.Fatalf("giá trị ngoài enum: %v", err)
	}
}
