package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"
)

// HookTrustReport dùng kết luận của chính Codex về định nghĩa/hash hiện hành.
// Không suy ra trusted từ sự tồn tại của một entry cũ trong config.toml.
type HookTrustReport struct{ Total, Trusted, Untrusted, Modified, Disabled int }

func (r HookTrustReport) AllTrusted() bool {
	return r.Total > 0 && r.Trusted == r.Total && r.Disabled == 0
}

func (r HookTrustReport) String() string {
	return fmt.Sprintf("%d/%d đã trust, %d chưa duyệt, %d đã đổi, %d bị tắt", r.Trusted, r.Total, r.Untrusted, r.Modified, r.Disabled)
}

// InspectHookTrust chỉ gọi initialize và hooks/list của app-server, không mở
// thread, chạy model, duyệt hook hay sửa cấu hình. Bản Codex thiếu API này trả lỗi
// để doctor báo chưa xác minh, thay vì khẳng định hook đã được trust.
func InspectHookTrust(ctx context.Context, binary, root, path string) (HookTrustReport, error) {
	var report HookTrustReport
	if binary == "" {
		binary = "codex"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--no-daemon", "app-server", "--stdio")
	cmd.Dir = root
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	prepareMetadataProcess(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return report, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return report, err
	}
	if err = cmd.Start(); err != nil {
		return report, err
	}
	defer func() {
		in.Close()
		killMetadataProcess(cmd)
		cmd.Wait()
	}()
	enc := json.NewEncoder(in)
	dec := json.NewDecoder(io.LimitReader(out, 8<<20))
	read := func(id int, dst any) error {
		for {
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := dec.Decode(&response); err != nil {
				return fmt.Errorf("Codex hooks/list: %w", err)
			}
			if response.ID != id {
				continue
			}
			if response.Error != nil {
				return fmt.Errorf("Codex hooks/list: %s", response.Error.Message)
			}
			if dst == nil {
				return nil
			}
			return json.Unmarshal(response.Result, dst)
		}
	}
	if err = enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "bint-doctor", "version": "1"}}}); err != nil {
		return report, err
	}
	if err = read(1, nil); err != nil {
		return report, err
	}
	if err = enc.Encode(map[string]any{"method": "initialized"}); err != nil {
		return report, err
	}
	if err = enc.Encode(map[string]any{"id": 2, "method": "hooks/list", "params": map[string]any{"cwds": []string{root}}}); err != nil {
		return report, err
	}
	var result struct {
		Data []struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
			Hooks []struct {
				SourcePath  string `json:"sourcePath"`
				Command     string `json:"command"`
				Enabled     bool   `json:"enabled"`
				TrustStatus string `json:"trustStatus"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err = read(2, &result); err != nil {
		return report, err
	}
	want, err := filepath.Abs(path)
	if err != nil {
		return report, err
	}
	for _, row := range result.Data {
		if len(row.Errors) > 0 {
			return report, fmt.Errorf("Codex không nạp đủ hook: %s", row.Errors[0].Message)
		}
		for _, h := range row.Hooks {
			if filepath.Clean(h.SourcePath) != filepath.Clean(want) || !isBintHook(h.Command) {
				continue
			}
			report.Total++
			if !h.Enabled {
				report.Disabled++
			}
			switch h.TrustStatus {
			case "trusted", "managed":
				report.Trusted++
			case "modified":
				report.Modified++
			default:
				report.Untrusted++
			}
		}
	}
	if report.Total == 0 {
		return report, fmt.Errorf("Codex chưa nạp hook Bint tại %s (kiểm tra project trust và /hooks)", path)
	}
	return report, nil
}
