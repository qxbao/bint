package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StrategyPath trả về .bint/harness/strategy.yml của repository (kế hoạch H1).
func StrategyPath(root string) string {
	return filepath.Join(root, DirName, "harness", "strategy.yml")
}

// strategyDoc là tệp strategy.yml. Gói config tự đọc tập con này (không import hcomp để
// tránh vòng import); hcomp kiểm tra cùng khuôn khi người thêm/duyệt thành phần.
type strategyDoc struct {
	ID         string `yaml:"id"`
	Scope      string `yaml:"scope"`
	Status     string `yaml:"status"`
	Origin     string `yaml:"origin"`
	Version    int    `yaml:"version"`
	ApprovedBy string `yaml:"approved_by"`
	Context    *struct {
		MaxTokens    *int `yaml:"max_tokens"`
		PathMaxItems *int `yaml:"path_max_items"`
		Lessons      *struct {
			GlobalK   *int `yaml:"global_k"`
			MaxTokens *int `yaml:"max_tokens"`
		} `yaml:"lessons"`
		Interfaces *struct {
			MCP       *bool `yaml:"mcp"`
			PathHooks *bool `yaml:"path_hooks"`
		} `yaml:"interfaces"`
	} `yaml:"context"`
}

// mergeStrategy ghi đè tập con context.* bằng strategy.yml NẾU tệp ở trạng thái verified.
// Tệp candidate/deprecated không có hiệu lực. Khoá lạ trong tệp là lỗi. Trả true khi đã áp.
func mergeStrategy(cfg *Config, root string) (bool, error) {
	path := StrategyPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var d strategyDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if d.Status != "verified" {
		return false, nil
	}
	if d.Scope != "global" {
		return false, fmt.Errorf("%s: strategy chỉ có phạm vi global, nhận %q", path, d.Scope)
	}
	c := d.Context
	if c == nil {
		return false, nil
	}
	if c.MaxTokens != nil {
		cfg.Context.MaxTokens = *c.MaxTokens
	}
	if c.PathMaxItems != nil {
		cfg.Context.PathMaxItems = *c.PathMaxItems
	}
	if l := c.Lessons; l != nil {
		if l.GlobalK != nil {
			cfg.Context.Lessons.GlobalK = *l.GlobalK
		}
		if l.MaxTokens != nil {
			cfg.Context.Lessons.MaxTokens = *l.MaxTokens
		}
	}
	if i := c.Interfaces; i != nil {
		if i.MCP != nil {
			cfg.Context.Interfaces.MCP = *i.MCP
		}
		if i.PathHooks != nil {
			cfg.Context.Interfaces.PathHooks = *i.PathHooks
		}
	}
	return true, nil
}
