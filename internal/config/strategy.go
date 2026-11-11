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
// Tệp candidate/deprecated không có hiệu lực kể cả khi nội dung hỏng. Tệp hỏng (cú pháp, khoá
// lạ, phạm vi khác global, giá trị không hợp lệ) KHÔNG làm hỏng config.Load — nếu không, một
// tệp do agent ghi sẽ làm mọi lệnh và hook của Bint lỗi: nó bị bỏ qua và lý do trả về ở
// problem (giống hcomp.LoadDir: tệp hỏng "không có hiệu lực"). Chỉ lỗi đọc đĩa mới là lỗi.
func mergeStrategy(cfg *Config, root string) (applied bool, problem string, err error) {
	path := StrategyPath(root)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	// Đọc lỏng trước để biết trạng thái: chỉ tệp verified mới bị kiểm nghiêm.
	var head struct {
		Status string `yaml:"status"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return false, fmt.Sprintf("%s: %v", path, err), nil // không biết trạng thái: không có hiệu lực
	}
	if head.Status != "verified" {
		return false, "", nil
	}
	var d strategyDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Sprintf("%s: %v", path, err), nil
	}
	if d.Scope != "global" {
		return false, fmt.Sprintf("%s: strategy chỉ có phạm vi global, nhận %q", path, d.Scope), nil
	}
	c := d.Context
	if c == nil {
		return false, "", nil
	}
	next := *cfg
	if c.MaxTokens != nil {
		next.Context.MaxTokens = *c.MaxTokens
	}
	if c.PathMaxItems != nil {
		next.Context.PathMaxItems = *c.PathMaxItems
	}
	if l := c.Lessons; l != nil {
		if l.GlobalK != nil {
			next.Context.Lessons.GlobalK = *l.GlobalK
		}
		if l.MaxTokens != nil {
			next.Context.Lessons.MaxTokens = *l.MaxTokens
		}
	}
	if i := c.Interfaces; i != nil {
		if i.MCP != nil {
			next.Context.Interfaces.MCP = *i.MCP
		}
		if i.PathHooks != nil {
			next.Context.Interfaces.PathHooks = *i.PathHooks
		}
	}
	// Giá trị không hợp lệ: bỏ cả tệp thay vì để Validate cuối làm hỏng cấu hình (chỉ khi
	// cấu hình nền hợp lệ, để không che lỗi của chính config.yml).
	if cfg.Validate() == nil {
		if verr := next.Validate(); verr != nil {
			return false, fmt.Sprintf("%s: %v", path, verr), nil
		}
	}
	*cfg = next
	return true, "", nil
}
