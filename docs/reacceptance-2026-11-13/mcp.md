# MCP gateway plan reacceptance

Plan source: [`mcp-gateway-plan.md` (lines 98)](../mcp-gateway-plan.md), with M5 at `:341-342,358`. The plan itself states that the gateway direction is agreed but not implemented (`:3-7`). Existing Context Engine MCP support is separately present.

| Stage / criteria | Plan | Current source search | New evidence / conclusion | Status / deferred |
|---|---|---|---|---|
| M0 — per-run Bint MCP config, Codex global-server suppression, skill citations and workflow documentation | `:98-119` | Current `internal/mcpserver/server.go:1,56` and `internal/cli/mcp.go`; runner search did not find the planned run-scoped `MCPConfig` wiring. Search evidence: `evidence/inventory-search.txt`. Existing eval `MCPConfig` in `internal/eval/arms.go:186,394` is evaluation-only. | No new runtime command exists in this audit for the proposed run-scoped setup; the current package tests cover the old server. | **chưa triển khai** as written in the M0 plan. DEF-007. |
| M1 — embedded service catalog, two-level config, sharing policy, service management API/CLI and index lifecycle | `:121-242` | Search found no `internal/mcpcat`; CLI MCP commands remain current `serve/tools` (see `internal/cli/mcp.go`). | Current `internal/app/mcp_svc.go` serves tool-list defaults; not the proposed catalog service. | **chưa triển khai**. DEF-007. |
| M2 — shared authenticated HTTP daemon, stdio/HTTP upstream multiplexing, lifecycle and process cleanup | `:243-283` | Search found no `internal/mcpgw`, gateway endpoints, or daemon lifecycle. Existing `internal/mcpserver` is stdio server. | No gateway concurrency, auth, Host/Origin, or restart tests exist at this tree search. | **chưa triển khai**. DEF-007. |
| M3 — merged shim, role grants, read-only tool filtering and workflow injection | `:284-313` | No `bint mcp serve` gateway shim or run/task role grant mapping found. Current harness MCP installation helpers in `internal/harness/{claude,codex}` are setup helpers, not role grants. | `evidence/inventory-search.txt` records implementation search. | **chưa triển khai**. DEF-007. |
| M4 — MCP TUI management screen, add/remove/check/index/restart, role × service grants | `:314-328` | Current System screen has an MCP tools view (`internal/tui/app/system_flows_test.go:147`; `internal/app/system_svc_test.go:335`) for existing Bint tool listing. Search did not find a gateway service-management model/API. | Existing view is not equivalent to the planned screen. | **chưa triển khai**. DEF-007. |
| M5 — measure quality and operational savings before default grants | `:341-342,358` | No gateway implementation exists to benchmark. | No paid/live experiment or PSS measurement was run. | **tạm hoãn/thiếu bằng chứng** and blocked on M3 implementation plus approved measurements. DEF-006/007. |

## Scope boundary

The existing `internal/mcpserver` package implements the Context Engine MCP server and its tools. Its existence does not satisfy the new shared gateway/catalog/grants plan. No MCP services were installed, no global configuration was touched, and no real MCP data was mutated.
