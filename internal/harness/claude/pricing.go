package claude

import (
	"regexp"
	"strings"

	"bint/internal/config"
)

// Bảng giá model Claude dựng sẵn, dùng để quy token của subagent (driver session) ra USD
// (docs/isolation-subagent-plan.md mục 6.6). Ghi đè được bằng cấu hình harnesses.claude.pricing.
//
// Nguồn: https://platform.claude.com/docs/en/about-claude/pricing, đọc ngày 2026-11-12 (giá chuẩn, không phải fast mode, batch
// hay residency). Model không có trong bảng (và không được cấu hình) thì chi phí là unknown, không đoán.
const (
	PricingSource = "https://platform.claude.com/docs/en/about-claude/pricing"
	PricingDate   = "2026-11-12"
	// LongPromptTokens: ngưỡng prompt (input + ghi + đọc bộ nhớ đệm của một lượt gọi API) trên đó model có bậc giá dài
	// (hiện chỉ Haiku 5.5).
	LongPromptTokens = 100_000
)

type builtinPrice struct {
	base config.ModelPrice
	// long: giá khi prompt của lượt gọi vượt LongPromptTokens (nil = một bậc giá).
	long *config.ModelPrice
}

func mp(in, out, cw, cr float64) config.ModelPrice {
	return config.ModelPrice{InputUSDPerMTok: in, OutputUSDPerMTok: out, CacheWriteUSDPerMTok: cw, CacheReadUSDPerMTok: cr}
}

// builtinPricing: khoá = id model không có hậu tố ngày. Giá cache ghi là loại ghi 5 phút.
var builtinPricing = map[string]builtinPrice{
	"claude-fable-5-2":  {base: mp(8, 40, 10, 0.20)},
	"claude-fable-5-1":  {base: mp(10, 50, 12.5, 0.25)},
	"claude-fable-5":    {base: mp(10, 50, 12.5, 1)},
	"claude-opus-5-5":   {base: mp(4, 20, 5, 0.20)},
	"claude-opus-5":     {base: mp(5, 25, 6.25, 0.50)},
	"claude-opus-4-8":   {base: mp(5, 25, 6.25, 0.50)},
	"claude-opus-4-7":   {base: mp(5, 25, 6.25, 0.50)},
	"claude-opus-4-6":   {base: mp(5, 25, 6.25, 0.50)},
	"claude-opus-4-5":   {base: mp(5, 25, 6.25, 0.50)},
	"claude-opus-4-1":   {base: mp(15, 75, 18.75, 1.50)},
	"claude-opus-4":     {base: mp(15, 75, 18.75, 1.50)},
	"claude-sonnet-5-5": {base: mp(2, 10, 2.5, 0.10)},
	"claude-sonnet-5":   {base: mp(2, 10, 2.5, 0.20)},
	"claude-sonnet-4-6": {base: mp(3, 15, 3.75, 0.30)},
	"claude-sonnet-4-5": {base: mp(3, 15, 3.75, 0.30)},
	"claude-sonnet-4":   {base: mp(3, 15, 3.75, 0.30)},
	"claude-haiku-5-5":  {base: mp(0.10, 0.50, 0.125, 0.01), long: ptr(mp(0.50, 2.50, 0.625, 0.05))},
	"claude-haiku-4-5":  {base: mp(1, 5, 1.25, 0.10)},
}

func ptr[T any](v T) *T { return &v }

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// NormalizeModelID bỏ hậu tố ngày (-20251001) và hậu tố ngữ cảnh ([1m]) của id model.
func NormalizeModelID(model string) string {
	model = strings.TrimSpace(model)
	if i := strings.Index(model, "["); i > 0 {
		model = model[:i]
	}
	return dateSuffix.ReplaceAllString(model, "")
}

// withCacheDefaults điền giá cache còn để 0 từ giá input (ghi 1,25 lần, đọc 0,1 lần).
func withCacheDefaults(p config.ModelPrice) config.ModelPrice {
	if p.CacheWriteUSDPerMTok == 0 {
		p.CacheWriteUSDPerMTok = p.InputUSDPerMTok * 1.25
	}
	if p.CacheReadUSDPerMTok == 0 {
		p.CacheReadUSDPerMTok = p.InputUSDPerMTok * 0.1
	}
	return p
}

// PriceFor trả giá của model: cấu hình (override) trước, rồi bảng dựng sẵn; khớp đúng id rồi id bỏ hậu tố ngày. longPrompt
// chọn bậc giá dài của bảng dựng sẵn (cấu hình chỉ có một bậc). ok=false nếu không có giá.
func PriceFor(model string, override map[string]config.ModelPrice, longPrompt bool) (config.ModelPrice, bool) {
	ids := []string{strings.TrimSpace(model)}
	if n := NormalizeModelID(model); n != ids[0] {
		ids = append(ids, n)
	}
	for _, id := range ids {
		if p, ok := override[id]; ok {
			return withCacheDefaults(p), true
		}
	}
	for _, id := range ids {
		if b, ok := builtinPricing[id]; ok {
			if longPrompt && b.long != nil {
				return *b.long, true
			}
			return b.base, true
		}
	}
	return config.ModelPrice{}, false
}
