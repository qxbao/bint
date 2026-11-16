package iso

import (
	"fmt"
	"slices"
	"time"

	"bint/internal/config"
)

// Settings là khối `iso:` của cấu hình sau khi phân tích thời lượng.
type Settings struct {
	Cache       string // config.IsoCacheModes
	Share       []string
	CacheTTL    time.Duration
	SessionsTTL time.Duration // 0 = không dọn lịch sử phiên
	GCInterval  time.Duration
	TmpTTL      time.Duration
	PassEnv     []string
	DropEnv     []string
}

// Shared cho biết cache dùng chung với máy thật.
func (s Settings) Shared() bool { return s.Cache == "shared" }

// Shares cho biết mục chia sẻ name có hiệu lực (cache shared và có trong iso.share).
func (s Settings) shares(name string) bool { return s.Shared() && slices.Contains(s.Share, name) }

// SettingsFrom phân tích cfg; lỗi khi cache lạ, share lạ hoặc thời lượng không parse được/âm. Cấu hình đã
// qua config.Validate thì không lỗi; hàm vẫn kiểm vì Store có thể được dựng từ config chưa Validate.
func SettingsFrom(cfg config.Iso) (Settings, error) {
	s := Settings{Cache: cfg.Cache, Share: slices.Clone(cfg.Share), PassEnv: slices.Clone(cfg.PassEnv), DropEnv: slices.Clone(cfg.DropEnv)}
	if !slices.Contains(config.IsoCacheModes, cfg.Cache) {
		return s, fmt.Errorf("iso.cache: %q không hợp lệ", cfg.Cache)
	}
	for _, n := range cfg.Share {
		if !slices.Contains(config.IsoShareNames, n) {
			return s, fmt.Errorf("iso.share: mục không hỗ trợ %q", n)
		}
	}
	for _, d := range []struct {
		name, v string
		dst     *time.Duration
	}{
		{"cache_ttl", cfg.CacheTTL, &s.CacheTTL}, {"sessions_ttl", cfg.SessionsTTL, &s.SessionsTTL},
		{"gc_interval", cfg.GCInterval, &s.GCInterval}, {"tmp_ttl", cfg.TmpTTL, &s.TmpTTL},
	} {
		v, err := time.ParseDuration(d.v)
		if err != nil || v < 0 {
			return s, fmt.Errorf("iso.%s: %q không phải khoảng thời gian không âm", d.name, d.v)
		}
		*d.dst = v
	}
	return s, nil
}
