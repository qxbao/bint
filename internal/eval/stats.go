package eval

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"sort"
)

// Mean trả về trung bình; NaN nếu rỗng.
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// SD trả về độ lệch chuẩn mẫu (n-1); NaN nếu ít hơn 2 phần tử.
func SD(xs []float64) float64 {
	if len(xs) < 2 {
		return math.NaN()
	}
	m := Mean(xs)
	s := 0.0
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(xs)-1))
}

// CI là khoảng tin cậy.
type CI struct {
	Lo, Hi float64
}

// MarshalJSON ghi [lo, hi], hoặc null khi không tính được.
func (c CI) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return []byte("null"), nil
	}
	return json.Marshal([2]float64{c.Lo, c.Hi})
}

// Valid cho biết khoảng tin cậy tính được.
func (c CI) Valid() bool { return !math.IsNaN(c.Lo) && !math.IsNaN(c.Hi) }

// Width là độ rộng khoảng.
func (c CI) Width() float64 { return c.Hi - c.Lo }

// BootstrapIters là số lần lấy mẫu lại mặc định.
const BootstrapIters = 10000

// BootstrapMeanCI ước lượng khoảng tin cậy (1-alpha) của trung bình bằng phương
// pháp lấy mẫu lại có hoàn lại theo phân vị. Hạt giống cố định để báo cáo tái lập được.
// Với so sánh theo cặp, xs là chênh lệch trên từng tác vụ, nên việc lấy mẫu lại
// theo tác vụ giữ nguyên cấu trúc cặp.
func BootstrapMeanCI(xs []float64, iters int, alpha float64, seed uint64) CI {
	n := len(xs)
	if n < 2 {
		return CI{math.NaN(), math.NaN()}
	}
	rng := rand.New(rand.NewPCG(seed, 0x62696e74))
	means := make([]float64, iters)
	for i := range means {
		s := 0.0
		for j := 0; j < n; j++ {
			s += xs[rng.IntN(n)]
		}
		means[i] = s / float64(n)
	}
	sort.Float64s(means)
	return CI{Lo: quantile(means, alpha/2), Hi: quantile(means, 1-alpha/2)}
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

const (
	z975 = 1.959963984540054 // alpha = 0.05 hai phía
	z80  = 0.841621233572914 // power = 0.8
)

// PairedSampleSize ước tính số tác vụ cần để phát hiện chênh lệch trung bình
// effect (thang 0..1 với tỷ lệ đạt) giữa hai nhánh theo cặp, với độ lệch chuẩn của
// chênh lệch theo tác vụ sdDiff, alpha=0.05 hai phía, power=0.8 (xấp xỉ chuẩn).
func PairedSampleSize(sdDiff, effect float64) int {
	if effect <= 0 || math.IsNaN(sdDiff) || sdDiff <= 0 {
		return 0
	}
	n := math.Pow((z975+z80)*sdDiff/effect, 2)
	return int(math.Ceil(n))
}

// AssumedSDDiff là độ lệch chuẩn giả định của chênh lệch tỷ lệ đạt theo tác vụ
// khi chưa có dữ liệu: ước lượng thận trọng cho biến nhị phân ghép cặp.
const AssumedSDDiff = 0.5
