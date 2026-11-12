package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"bint/internal/mcpcat"
)

// Tầng máy của cấu hình MCP (docs/mcp-gateway-plan.md mục 5.2): mcp.yml (service đã đăng ký) và secrets.env
// (giá trị credential) nằm ở thư mục cấu hình user, không commit. mcp.yml KHÔNG BAO GIỜ chứa giá trị credential.

const (
	// MachineMCPFileName là tên tệp đăng ký service trên máy.
	MachineMCPFileName = "mcp.yml"
	// SecretsFileName là tên tệp credential mặc định.
	SecretsFileName = "secrets.env"
	// MachineMCPVersion là phiên bản schema của mcp.yml.
	MachineMCPVersion = 1
)

// EnvNameRE là dạng tên biến môi trường hợp lệ.
var EnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// MachineMCP là nội dung mcp.yml.
type MachineMCP struct {
	Version int `yaml:"version"`
	// Services: id (thuộc danh mục) → đăng ký.
	Services map[string]Registration `yaml:"services"`
	// SecretsFile: đường dẫn secrets.env nếu khác mặc định (cạnh mcp.yml).
	SecretsFile string `yaml:"secrets_file,omitempty"`
}

// Registration là một service đã đăng ký trên máy: lệnh hoặc URL, phiên bản ghim, TÊN biến credential, giới hạn.
type Registration struct {
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	URL     string   `yaml:"url,omitempty"`
	Version string   `yaml:"version,omitempty"`
	// EnvNames là tên biến credential; giá trị lấy từ secrets.env khi khởi động upstream.
	EnvNames []string `yaml:"env,omitempty"`
	// IdleTimeout (vd. "10m") và MaxRestarts: giới hạn của daemon (M2).
	IdleTimeout string `yaml:"idle_timeout,omitempty"`
	MaxRestarts int    `yaml:"max_restarts,omitempty"`
}

// MachineDir là thư mục cấu hình user của Bint (cạnh cấu hình toàn cục); rỗng nếu không xác định được.
func MachineDir() string {
	g := GlobalPath()
	if g == "" {
		return ""
	}
	return filepath.Dir(g)
}

// MCPMachinePath là đường dẫn mcp.yml; rỗng nếu không xác định được thư mục cấu hình.
func MCPMachinePath() string {
	d := MachineDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, MachineMCPFileName)
}

// SecretsPath là đường dẫn secrets.env của cấu hình m (mặc định cạnh mcp.yml); rỗng nếu không xác định được.
func SecretsPath(m MachineMCP) string {
	if m.SecretsFile != "" {
		return m.SecretsFile
	}
	d := MachineDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, SecretsFileName)
}

// Validate kiểm mcp.yml: service thuộc danh mục, tên biến hợp lệ (không chứa giá trị), thời gian nghỉ đọc được.
func (m MachineMCP) Validate() error {
	var errs []string
	if m.Version != 0 && m.Version != MachineMCPVersion {
		errs = append(errs, fmt.Sprintf("version: cần %d, nhận %d", MachineMCPVersion, m.Version))
	}
	for _, id := range sortedKeys(m.Services) {
		svc, ok := mcpcat.Lookup(id)
		if !ok {
			errs = append(errs, fmt.Sprintf("services: service %q không có trong danh mục (%s)", id, strings.Join(mcpcat.IDs(), ", ")))
			continue
		}
		r := m.Services[id]
		switch svc.Transport {
		case mcpcat.HTTP:
			if r.URL == "" {
				errs = append(errs, fmt.Sprintf("services.%s: cần url", id))
			}
			if r.Command != "" || len(r.Args) > 0 {
				errs = append(errs, fmt.Sprintf("services.%s: service http không có command/args", id))
			}
		default:
			if r.Command == "" {
				errs = append(errs, fmt.Sprintf("services.%s: cần command", id))
			}
			if r.URL != "" {
				errs = append(errs, fmt.Sprintf("services.%s: service stdio không có url", id))
			}
		}
		for _, n := range r.EnvNames {
			if !EnvNameRE.MatchString(n) {
				// Không in n: có thể là "TÊN=giá trị" gõ nhầm.
				errs = append(errs, fmt.Sprintf("services.%s.env: tên biến không hợp lệ (chỉ ghi TÊN biến, không ghi giá trị)", id))
			}
		}
		if r.IdleTimeout != "" {
			if d, err := time.ParseDuration(r.IdleTimeout); err != nil || d <= 0 {
				errs = append(errs, fmt.Sprintf("services.%s.idle_timeout: %q không phải khoảng thời gian dương", id, r.IdleTimeout))
			}
		}
		if r.MaxRestarts < 0 {
			errs = append(errs, fmt.Sprintf("services.%s.max_restarts: không được âm", id))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return &ValidationError{Problems: errs}
	}
	return nil
}

// DecodeMachineMCP giải mã mcp.yml, từ chối khoá lạ.
func DecodeMachineMCP(r io.Reader) (MachineMCP, error) {
	var m MachineMCP
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return MachineMCP{}, err
	}
	if m.Services == nil {
		m.Services = map[string]Registration{}
	}
	return m, m.Validate()
}

// LoadMachineMCP đọc mcp.yml; chưa có tệp thì trả cấu hình rỗng.
func LoadMachineMCP() (MachineMCP, error) {
	empty := MachineMCP{Version: MachineMCPVersion, Services: map[string]Registration{}}
	p := MCPMachinePath()
	if p == "" {
		return empty, nil
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	m, err := DecodeMachineMCP(bytes.NewReader(data))
	if err != nil {
		return empty, fmt.Errorf("%s: %w", p, err)
	}
	return m, nil
}

// SaveMachineMCP ghi mcp.yml nguyên tử (tệp tạm cùng thư mục rồi đổi tên), quyền 0600, thư mục 0700.
func SaveMachineMCP(m MachineMCP) error {
	p := MCPMachinePath()
	if p == "" {
		return errors.New("không xác định được thư mục cấu hình user (đặt BINT_CONFIG_HOME)")
	}
	m.Version = MachineMCPVersion
	if m.Services == nil {
		m.Services = map[string]Registration{}
	}
	if err := m.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	header := "# Dịch vụ MCP đã đăng ký trên máy này (bint mcp add/remove). Chỉ ghi lệnh, URL và TÊN biến credential;\n" +
		"# giá trị credential nằm trong secrets.env (quyền 0600), không bao giờ ở đây.\n"
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.yml.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.WriteString(header + string(data)); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, p); err != nil {
		cleanup()
		return err
	}
	return nil
}

// ReadSecrets đọc secrets.env dạng TÊN=giá trị: bỏ dòng trống và dòng chú thích (#), chấp nhận tiền tố
// `export `, cắt cặp nháy đơn hoặc kép bao ngoài giá trị. Chưa có tệp thì trả ánh xạ rỗng.
func ReadSecrets(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, val, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || !EnvNameRE.MatchString(name) {
			// Không in dòng: có thể chứa giá trị bí mật.
			return out, fmt.Errorf("%s:%d: cần dạng TÊN=giá trị", path, n)
		}
		out[name] = unquote(strings.TrimSpace(val))
	}
	return out, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
