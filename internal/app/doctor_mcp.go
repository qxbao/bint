package app

import (
	"os"
	"path/filepath"

	"bint/internal/config"
)

// doctorMCP báo tình trạng MCP (docs/mcp-gateway-plan.md mục 5): service dự án khai báo trong mcp.services mà máy chưa đăng
// ký, chỉ mục codegraph thiếu ở worktree hiện tại, secrets.env quyền lỏng, mcp.yml hỏng. Chỉ đọc tệp, KHÔNG khởi động tiến
// trình MCP (việc đó của `bint mcp check`).
func doctorMCP(r *DoctorReport, root string, cfg config.Config) {
	m, err := config.LoadMachineMCP()
	if err != nil {
		r.add("mcp.yml", LevelWarn, "%v", err)
		m = config.MachineMCP{}
	}
	for _, id := range cfg.MCP.ServiceIDs() {
		_, registered := m.Services[id]
		switch {
		case registered:
			r.add("MCP "+id, LevelOK, "đã đăng ký trên máy")
		case cfg.MCP.Services[id].IsRequired():
			r.add("MCP "+id, LevelWarn, "dự án khai báo trong mcp.services nhưng máy này chưa đăng ký — bint mcp add %s", id)
		default:
			r.add("MCP "+id, LevelInfo, "dự án khai báo tuỳ chọn trong mcp.services, máy này chưa đăng ký (bint mcp add %s)", id)
		}
	}
	if cfg.MCP.Uses("codegraph") {
		if _, err := os.Stat(filepath.Join(root, codegraphDB)); err != nil {
			r.add("MCP codegraph chỉ mục", LevelWarn, "worktree %s chưa có %s — bint mcp index codegraph", root, filepath.ToSlash(codegraphDB))
		}
	}
	if p := config.SecretsPath(m); p != "" {
		if fi, err := os.Stat(p); err == nil {
			if why := config.SecretsPermProblem(fi); why != "" {
				r.add("secrets.env", LevelWarn, "%s: %s — chạy chmod 600 %s", p, why, p)
			}
		}
	}
}
