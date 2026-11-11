package app

import (
	"bint/internal/config"
	"bint/internal/mcpserver"
)

// MCPToolsOptions: `bint mcp tools` (không có tham số).
type MCPToolsOptions struct{}

func MCPToolsDefaults(config.Config) MCPToolsOptions { return MCPToolsOptions{} }
func (o MCPToolsOptions) Validate() error            { return nil }

// MCPTool là một công cụ MCP đang mở cho agent.
type MCPTool struct{ Name, Description string }

// MCPTools liệt kê công cụ của MCP server (không cần repository, không mở backend).
func MCPTools(version string, o MCPToolsOptions) ([]MCPTool, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	srv := mcpserver.New("bint", version, "")
	mcpserver.RegisterTools(srv, nil)
	var out []MCPTool
	for _, t := range srv.Tools() {
		out = append(out, MCPTool{Name: t.Name, Description: t.Description})
	}
	return out, nil
}
