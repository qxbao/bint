# Kế hoạch: gateway MCP dùng chung và cấp MCP theo role

> **Nghiệm thu:** M0–M4 đạt bằng test tự động; phần chạy với service và harness thật, đo chất lượng và Windows
> được theo dõi ở [acceptance.md](acceptance.md): mã `NT-MCP-*` (vòng V1, V3) và `NT-WIN-MCP` (vòng V2).

Trạng thái: **đã triển khai M0–M4, M5 đo xong bộ nhớ** (2026-11-13). **M0 và M1 đã làm (commit 2e3fb38, 89c85f3); M2 đã làm (2026-11-13, xem "Trạng thái M2" ở §6); M3 đã làm (2026-11-13, xem "Trạng thái M3" ở §7); M4 đã làm (2026-11-13, xem "Trạng thái M4" ở §8); M5: đã đo bộ nhớ (2026-11-13, xem "Trạng thái M5" ở §11), chưa đo chất lượng.** Gồm sáu giai đoạn: M0 (cấp MCP của Bint cho
các bước `bint run`, sửa ngay được), M1 (danh mục, cấu hình hai tầng, API/CLI quản lý), M2 (daemon
gateway), M3 (cửa vào gộp và cấp quyền theo role), M4 (màn TUI), M5 (đo).
Quyết định ở §3, kết quả xác minh ở §10. Chỉ còn S3 (harness xử lý thế nào khi chính shim chết), chưa kiểm
(phần nghiệm thu thật của M3).

Tham khảo: gateway của bflow (`AI-flow-SDLC/packages/mcp-gateway`, `docs/shared-mcp-architecture.md`,
`docs/mcp-memory-benchmark.md`). Kế hoạch này chỉ lấy phần lõi, không chép nguyên.

## 1. Mục tiêu

1. **Một tiến trình MCP cho nhiều phiên.** Mỗi terminal hay mỗi lượt `bint run` không còn tự sinh một bộ
   tiến trình MCP riêng. Service nào chia sẻ được thì chạy một bản (theo máy hoặc theo worktree); service
   có trạng thái theo phiên thì chỉ khởi động khi được gọi.
2. **Một điểm quản lý cho mọi harness** (Claude Code, Codex, Antigravity). Người dùng thêm, gỡ, kiểm tra
   MCP được hỗ trợ qua API/CLI và màn TUI. **Mặc định không tự cài và không cấp gì.**
3. **Agent nào được dùng MCP nào thì khai báo trong cấu hình dự án, theo role.** Khoá là tên role tự do
   để sau này mở rộng sang workflow và skill tuỳ biến, không gắn cứng với b-req/b-feat/b-review/b-do.
4. **Mỗi bước workflow có đúng tập tool được cấp, như nhau trên mọi harness** (sửa khoảng trống ở §2.2).
5. Không nới quy tắc hook [k:01M746NPTW1B304MRQQPBCAFAM]: hook không khởi động daemon, không chờ mạng,
   không trả lỗi.

Ngoài phạm vi: tự cài gói MCP (npm/uv/jar/docker), keyring của hệ điều hành, MCP tuỳ ý ngoài danh mục,
chia sẻ gateway giữa nhiều máy hay nhiều user, cô lập chống agent cùng UID (xem §9).

## 2. Hiện trạng

### 2.1. Mỗi phiên một bộ tiến trình MCP

Đo bằng `ps` ngày 2026-11-11 trên máy phát triển (RSS, đang có 3–4 phiên Claude Code):

| MCP (cấu hình toàn cục) | Tiến trình mỗi phiên | RSS mỗi phiên |
|---|---|---|
| chrome-devtools | `npm exec` + `chrome-devtools-mcp` | 30–220 MB |
| specmatic | `npm exec` + `node specmatic-mcp` | 30–150 MB |
| jira-run | `uv tool uvx` + `python atlassian-jira-mcp` | 35–115 MB |
| bint | `bint mcp serve` | ~16 MB |

Tổng khoảng 260–500 MB RSS mỗi phiên. Tiến trình bọc `npm exec`/`uvx` chiếm 35–45%. RSS đếm trùng trang
dùng chung nên mức tiết kiệm thật phải đo bằng PSS (benchmark của bflow: 6 agent dùng chung một backend
giảm ~83% RSS nhưng 64–80% PSS).

codegraph chưa có trong cấu hình toàn cục. Đo thử ngày 2026-11-12 (§10, S1): mỗi phiên tốn khoảng 170 MB,
cộng một daemon khoảng 310 MB cho mỗi project.

MCP kiểu `http` (deepwiki, context7) không sinh tiến trình cục bộ. MCP `http` trỏ vào daemon tự chạy
(gitnexus :8932, playwright :8931) báo `ECONNREFUSED` khi daemon không chạy, vì không ai quản lý vòng đời.

### 2.2. Các bước `bint run` không nhất quán về MCP

| # | Chỗ | Hệ quả |
|---|---|---|
| H1 | `internal/eval/agent.go:145`: `ClaudeAgent.ArgsFor` luôn thêm `--strict-mcp-config` (từ P1, để lượt eval kín) và chỉ thêm `--mcp-config` khi `AgentRequest.MCPConfig` khác rỗng | — |
| H2 | `internal/workflow/run.go:559`: workflow (P4) dùng lại `ClaudeAgent` qua `harness.Claude.NewAgent` nhưng **không đặt `MCPConfig`** | Bước chạy bằng Claude **không có MCP nào, kể cả `bint`**. Không có tài liệu nào ghi đây là chủ ý. |
| H3 | `harness.go:568`: `Codex.NewAgent` không cô lập profile như eval | `codex exec` của một bước nạp mọi `[mcp_servers.*]` trong `~/.codex/config.toml` (máy phát triển: 6 server, gồm tool ghi của jira) cộng `bint` của dự án. Cùng bước, tập tool khác nhau theo harness. |
| H4 | Skill b-req/b-feat/b-review gọi CLI `bint context` qua `Bash(bint:*)` | Tri thức vẫn tới agent (cùng hook theo đường dẫn, vì `workflow.agent.setting_sources` mặc định rỗng). |
| H5 | CLI không có lệnh tương đương `cite_knowledge`; skill không nhắc trích dẫn | Lượt `bint run` gần như không ghi `knowledge_citations`, nên tín hiệu kết quả và vòng evolve thiếu dữ liệu từ chính các lượt tự động. |
| H6 | `config.Roles` là struct trường cố định (`Planner`, `Implementer`, …) | Không chứa được role của workflow tuỳ biến. |

Antigravity chỉ có MCP cấp user (`docs/workflows.md`), nên bước chạy bằng agy dùng CLI.

### 2.3. Thứ dùng lại được

- `internal/mcpserver`: server JSON-RPC stdio viết tay (197 dòng), đủ để làm cửa vào.
- `eval/arms.go:394` đã sinh `mcp.json` chỉ chứa `bint`; `CodexAgent.ArgsFor` đã nhận `MCPConfig` qua
  `-c mcp_servers.bint.*`.
- `harness.StepRole` (bước → role) và biến `BINT_RUN_ID`/`BINT_TASK_ID` được đặt cho tiến trình của bước.
- Khoá một bản: mẫu `workflow.AcquireWriteLock` (`tryFlock` unix/windows) [k:01M746PTBYPSXDT0WJZS38KPFD].
- Tầng dịch vụ CLI/TUI (`internal/app/*_svc.go`, thẻ `flag`/`form`, `Validate()`/`Defaults()`) và sổ
  action có `CLIPath` (`internal/tui/actions`).

## 3. Quyết định (chốt 2026-11-11/12)

### 3.1. Hướng chung

| Vấn đề | Quyết định |
|---|---|
| Gom về một gateway | Có. Gateway là điểm quản lý duy nhất (đăng ký, credential, health, log, cấu hình cho từng harness). **Không** có nghĩa một tiến trình dùng chung cho mọi service: mỗi service giữ chính sách chia sẻ riêng (§5.3). |
| Tự cài MCP | Không. Người dùng chủ động thêm qua API/CLI/TUI. |
| Cấp MCP cho agent | Khai báo trong cấu hình dự án, khoá theo role (tên tự do). |
| Sửa H2/H3 | Gộp vào kế hoạch này (giai đoạn M0), không vá riêng. |
| gitnexus, mempalace | Không đưa vào danh mục (codegraph thay gitnexus; mempalace không có ích cho Bint). |

### 3.2. Quyết định chi tiết

| # | Vấn đề | Quyết định | Lý do |
|---|---|---|---|
| D1 | Cài đặt | Chỉ **đăng ký và kiểm tra** (`initialize` + `tools/list`). `bint mcp add` hiện lệnh cài gợi ý để người dùng tự chạy. Lệnh dạng `npx …@latest`/`uvx …` bị cảnh báo; khuyên dùng binary đã cài với phiên bản ghim. Bộ cài để giai đoạn sau. | Bộ cài đa nền tảng tốn công mà ít giá trị. Riêng việc bỏ tiến trình bọc `npm exec`/`uvx` đã giảm 35–45% RSS (§2.1). |
| D2 | Credential | Giá trị nằm trong **`secrets.env` (quyền 0600) ở thư mục cấu hình user**. `mcp.yml` chỉ ghi tên biến (và đường dẫn tệp nếu khác mặc định), không bao giờ ghi giá trị. Daemon đọc tệp này khi khởi động upstream, không lấy env của shell đã khởi động daemon. | Daemon chạy lâu và tách khỏi terminal; lấy env từ shell khởi động nó trước thì dễ lệch (phiên mở từ IDE thiếu biến). Hiện mật khẩu jira đã nằm dạng rõ trong `~/.claude.json` và `~/.codex/config.toml`, nên một tệp 0600 không kém an toàn hơn và gom về một chỗ. |
| D3 | Thêm/gỡ khi phiên đang chạy | Chỉ có hiệu lực với phiên mới. Không làm `notifications/tools/list_changed` ở v1. | `list_changed` phải kiểm trên từng harness. |
| D4 | Phiên tương tác (ngoài `bint run`) | Role `interactive`. Không khai báo thì được mọi service **có trong `mcp.services` của dự án và đã đăng ký trên máy**, không phải mọi service máy có. | Giữ trải nghiệm gần như hiện nay nhưng gắn theo dự án: repo không dùng Jira thì phiên của repo đó không thấy jira. |
| D5 | Ai sửa đăng ký và quyền | Agent được `list`, `check`, `grants`, `gateway status`. Chỉ người được `add`, `remove`, `index`, `gateway stop/restart`: các lệnh này trả `ErrAgentForbidden` khi `InAgentSession`. | Restart một upstream cắt lời gọi đang chạy của phiên khác. |
| D6 | Hình dạng client | `bint mcp serve` là **cửa vào duy nhất (shim gộp)**: phục vụ tool của Bint và chuyển tiếp tool của service với tiền tố ngắn (§5.1). Tool tới harness có dạng `mcp__bint__<tiền tố>_<tool>`; quyền trong settings của harness sửa một lần. | Harness chỉ thấy một tiến trình stdio ổn định; daemon chết thì shim tự nối lại, nên tránh được phần lớn S3. Role lấy từ env chạy giống nhau trên Claude, Codex và agy (agy chỉ có MCP cấp user). Tên dài nhất (`mcp__bint__spec_backward_compatibility_check`, 45 ký tự) dưới giới hạn 64. Cái giá: mỗi phiên vẫn có một tiến trình ~16 MB (vốn đã có). |
| D7 | Kênh shim ↔ daemon | HTTP loopback `127.0.0.1:<cổng động>`, bearer token trong `gateway.json` (0600), kiểm Host/Origin. Không dùng Unix socket. | Chạy được trên Windows. Codex hỗ trợ MCP HTTP với `bearer_token_env_var` nếu sau này cần nối thẳng. |

## 4. M0 — Cấp MCP của Bint cho các bước `bint run`

Làm được ngay, không phụ thuộc gateway. Khi có M3, chỗ truyền `MCPConfig` này trở thành chỗ cấp quyền.

1. `workflow.Runner` sinh `<state>/runtime/mcp/<run-id>.json` chỉ chứa server `bint` (cùng dạng
   `eval/arms.go:394`, lệnh là đường dẫn tuyệt đối của binary đang chạy) và truyền qua
   `AgentRequest.MCPConfig`. Claude nhận `--mcp-config` + các tool `mcp__bint__*` trong allowlist (cả bước
   chỉ đọc). Codex nhận `-c mcp_servers.bint.*` sẵn có.
2. Bước Codex không còn nạp MCP toàn cục (H3, đã xác minh ở S4): Bint đọc tên các bảng
   `[mcp_servers.*]` trong config của Codex (`$CODEX_HOME/config.toml`, mặc định `~/.codex/config.toml`,
   và `.codex/config.toml` của dự án nếu có), rồi thêm `-c mcp_servers.<tên>.enabled=false` cho từng server
   không phải `bint`. **Không** dùng `--ignore-user-config`, vì cờ này bỏ luôn cấu hình model/provider.
3. Skill b-req/b-feat/b-review: ưu tiên `context_for_task`/`context_for_paths` khi có, giữ CLI làm dự
   phòng (agy). Nhắc gọi `cite_knowledge` khi áp dụng một mục.
4. Thêm `bint knowledge cite <id>` để harness không có MCP vẫn trích dẫn được (H5). Lệnh CLI mới phải khai
   báo ở `internal/tui/actions/coverage.go`.
5. `docs/workflows.md`: ghi rõ tập MCP của mỗi bước.

**Nghiệm thu M0:** log bước Claude có `--mcp-config`; một lượt `bint run b-do` thật ghi được ít nhất một
dòng `knowledge_citations` từ bước con; bước Codex không thấy server toàn cục (kiểm bằng `tools/list` hoặc
log JSONL); test hồi quy cho đối số Claude/Codex, kể cả config Codex không có `mcp_servers` và tên server
cần trích dẫn trong TOML (có dấu chấm, dấu gạch).

## 5. M1 — Danh mục, cấu hình hai tầng, API/CLI

### 5.1. Danh mục nhúng trong binary (`internal/mcpcat`)

Mỗi mục khai báo:

- id, tiền tố tool (D6), kiểu kết nối (`stdio` / `http`), lệnh cài và lệnh chạy gợi ý (phiên bản ghim);
- chính sách chia sẻ (§5.3) và **cwd** của tiến trình (`worktree` = gốc worktree, `state` = thư mục riêng
  trong thư mục trạng thái);
- **nhóm thao tác** (tên nhóm → danh sách tool), nhóm độc quyền, tool chỉ đọc (được retry), tool ghi vào
  cây làm việc (cấm ở bước chỉ đọc);
- **env mặc định** gateway luôn đặt cho upstream;
- **tham số điền mặc định**: tool → tham số → giá trị (`{worktree}`), gateway điền khi agent không truyền;
- biến môi trường credential cần có, tuỳ chọn riêng của dự án.

Quyền trong cấu hình trỏ tới nhóm, không trỏ tên tool thô: upstream đổi tên tool thì chỉ sửa danh mục.
Tên tool tới agent là `<tiền tố>_<tên upstream bỏ tiền tố trùng>` (vd. `codegraph_explore` → `cg_explore`,
`jira_issue_get` → `jira_issue_get`).

| Service | Tiền tố | Kiểu | Chia sẻ, cwd | Nhóm thao tác | Ghi chú |
|---|---|---|---|---|---|
| `codegraph` (`@colbymchenry/codegraph` 1.6.2) | `cg` | stdio | `worktree`, cwd gốc worktree | `explore` (`codegraph_explore`); `query` (node, search, callers, callees, impact, files, status) | Mọi tool chỉ đọc. Env mặc định `CODEGRAPH_TELEMETRY=0`, `CODEGRAPH_MCP_TOOLS=<đủ 8 tool>`. Chi tiết §5.5 |
| `specmatic` (`specmatic-mcp` 1.2.0) | `spec` | stdio | `worktree`, cwd **trong thư mục trạng thái** (`<state>/mcp/specmatic/<worktree-id>`) | `compat` (backward_compatibility_check); `contract` (run_contract_test, run_resiliency_test); `mock` (manage_mock_server, độc quyền theo cổng) | Điền mặc định `repoDir={worktree}` cho `compat`. `contract` chạy tối đa một lời gọi đồng thời mỗi phân vùng (dò báo cáo theo cwd). Chỉ hữu ích khi repo có OpenAPI/AsyncAPI. Chi tiết §10 S2 |
| `deepwiki` | `wiki` | http remote | `remote` | `read` | Không sinh tiến trình; qua gateway để có quyền theo role và ghi nhận |
| `context7` | `ctx7` | http remote | `remote` | `read` | Tuỳ chọn, trùng vai trò với deepwiki |
| `jira` (`atlassian-jira-mcp`) | `jira` | stdio | `machine` | `read`, `write` | Tool ghi không bao giờ retry; credential qua D2 |
| `chrome-devtools` | `chrome` | stdio | `session`, khởi động lười | `read`, `act` | Không dùng chung tiến trình (trạng thái trang đang chọn) |

`bint` không nằm trong danh mục: nó là chính cửa vào.

### 5.2. Hai tầng cấu hình

| Tầng | Tệp | Nội dung | Ai sửa |
|---|---|---|---|
| Máy | `~/.config/bint/mcp.yml` (Windows `%APPDATA%\bint\mcp.yml`), không commit | service đã đăng ký: lệnh/đối số hoặc URL, phiên bản ghim, **tên** biến credential, giới hạn (idle, restart) | API/CLI/TUI |
| Máy | `~/.config/bint/secrets.env` (0600; Windows: ACL chỉ user), không commit | giá trị credential, dạng `TÊN=giá trị` (D2) | Người, bằng trình soạn thảo; `bint mcp check` báo biến thiếu và quyền tệp lỏng |
| Dự án | `.bint/config.yml`, commit | service dự án dùng (bắt buộc/tuỳ chọn), tuỳ chọn riêng, `grants` theo role | Người sửa tệp (TUI chỉ xem) |

```yaml
mcp:
  services:
    codegraph: {}
    specmatic: { required: false, options: { specs: "api/**/*.yaml" } }
    deepwiki: {}
  grants:                  # khoá là tên role tự do; mặc định không cấp gì
    planner:     [codegraph.explore, deepwiki.read, specmatic.compat]
    implementer: [codegraph.explore, codegraph.query, deepwiki.read, specmatic.contract, specmatic.mock]
    reviewer:    [codegraph.explore, specmatic.compat]
    # interactive: [...]   # D4
```

- `grants` là `map[string][]string`, phần tử dạng `<service>.<nhóm>` hoặc `<service>.*`. Bước → role
  lấy từ `harness.StepRole`; workflow tuỳ biến sau này khai báo bước của nó dùng role nào.
- Thêm khoá cấu hình phải sửa struct, `Default()`, `Validate()` và mẫu `Render`
  [k:01M746NPB0Y4D5S2D47EFTJ8T4]; chú thích trong mẫu có `%` thì viết `%%` [k:01M7487MQV2FFZPHJDWEFVZC7Z].
- `Validate()` chỉ kiểm cấu trúc (role rỗng, nhóm không có trong danh mục). Service có trong `grants`
  nhưng máy này chưa đăng ký **không** làm hỏng việc nạp cấu hình (cấu hình dùng chung cả nhóm);
  `bint doctor` báo thiếu.

### 5.3. Chính sách chia sẻ

| Chính sách | Khoá phân vùng tiến trình | Dùng cho |
|---|---|---|
| `machine` | service + revision cấu hình | service không trạng thái, không phụ thuộc cwd |
| `worktree` | service + revision + đường dẫn gốc worktree | chỉ mục mã, spec, mọi service cần biết workspace (`roots/list`, cwd) |
| `session` | service + Mcp-Session-Id | service giữ trạng thái theo kết nối |
| `remote` | không có tiến trình | upstream HTTP |

Mọi tiến trình khởi động lười (lời gọi đầu tiên), dừng sau `idle_timeout` khi không còn phiên dùng và
không còn lời gọi dang dở. Nhóm `exclusive` trong một service dùng khoá trong daemon (một phiên giữ tại
một thời điểm, có TTL, chờ có giới hạn rồi trả `busy`); không làm fencing token như bflow.

### 5.4. API và CLI

Tầng dịch vụ `internal/app/mcp_svc.go` theo mẫu `knowledge_svc.go`; CLI và TUI cùng gọi.

| Lệnh | Việc | Ghi |
|---|---|---|
| `bint mcp list [--json]` | danh mục + trạng thái đăng ký trên máy + dự án dùng gì | — |
| `bint mcp add <service> [--command … --arg … --env-name …\|--url …]` | đăng ký, hiện lệnh cài gợi ý nếu chưa thấy binary, rồi kiểm tra | chỉ người (D5) |
| `bint mcp remove <service>` | gỡ đăng ký, dừng tiến trình nếu đang chạy | chỉ người |
| `bint mcp check [<service>]` | `initialize` + `tools/list`, đối chiếu nhóm thao tác với danh mục, báo biến credential thiếu | — |
| `bint mcp index <service>` | việc chuẩn bị riêng của service cho dự án; v1 chỉ codegraph (§5.5) | chỉ người |
| `bint mcp grants [--role <r>]` | quyền đã giải (role → tool thật), báo service thiếu | — |
| `bint mcp gateway start\|stop\|status\|restart <service>` | M2 | stop/restart chỉ người |

Lệnh và cờ mới phải khai báo ở `internal/tui/actions/coverage.go`. Test của thao tác chỉ-dành-cho-người
gọi `asHuman(t)` [k:01M746PHJXDZN6QMA3BEVEDNPP].

### 5.5. codegraph

Dựa trên chạy thử bản 1.6.2 (§10, S1).

- **Cài đặt:** người dùng tự cài CLI (`npm i -g @colbymchenry/codegraph@1.6.2`; gói chiếm ~288 MB vì kèm
  Node 24 riêng). **Không** chạy `codegraph install`: lệnh này ghi mục MCP và quyền vào cấu hình của từng
  harness và chèn khối vào `CLAUDE.md`/`AGENTS.md`, chồng lên phần Bint quản lý.
- **Lập chỉ mục:** `bint mcp index codegraph` chạy `codegraph init -y` (lần đầu) hoặc `codegraph sync`
  trong worktree hiện tại, rồi thêm `.codegraph/` vào `.git/info/exclude`. Không chạy tự động vì `init`
  đỉnh RAM ~822 MB (repo Bint). Lý do thêm vào exclude: `init` tạo `.codegraph/.gitignore` bỏ qua mọi tệp
  trong thư mục trừ chính nó, nên tệp này vẫn là tệp chưa theo dõi và sẽ bị tính khi kiểm phạm vi của
  b-feat. `doctor` báo nếu dự án dùng codegraph mà worktree chưa có chỉ mục.
- **Daemon không thuộc gateway.** codegraph tự chạy một daemon cho mỗi project (watcher, writer duy nhất
  của chỉ mục, tách khỏi client, tự thoát sau 5 phút không có client). Gateway chỉ giữ một client
  `codegraph serve --mcp` cho mỗi worktree; `bint mcp gateway stop` không tắt daemon này.
- **Env của daemon:** daemon lọc tool khi gọi theo `CODEGRAPH_MCP_TOOLS` **trong env của client đầu tiên
  đã khởi động nó**; `tools/list` lại lọc theo env của client. Nếu một phiên ngoài Bint khởi động daemon
  trước với mặc định (chỉ `explore`), các tool của nhóm `query` vẫn có trong danh sách nhưng gọi sẽ báo
  "disabled". Vì vậy:
  - `codegraph.explore` là nhóm mặc định trong ví dụ cấp quyền; nhóm này không bị ảnh hưởng;
  - `bint mcp check codegraph` gọi thử `codegraph_status`; bị từ chối thì báo `degraded` và hướng dẫn dừng
    daemon cũ (`codegraph daemon`, hoặc kill PID trong `.codegraph/daemon.pid`).
- **Worktree mới** (G2 của [isolation-subagent-plan.md](isolation-subagent-plan.md)): chép
  `.codegraph/codegraph.db` và `.codegraph/.gitignore` từ worktree gốc, không chép `*.pid`, `daemon.sock`,
  `daemon.log`, `-wal`/`-shm`; codegraph tự đối chiếu khi có client kết nối. Đo thử: `sync` sau khi chép
  mất 3,6 s, đỉnh 323 MB, so với `init` 9 s, đỉnh 822 MB. Không có chỉ mục gốc thì để người dùng chạy
  `bint mcp index codegraph`.
- Không dùng `projectPath` để một client phục vụ nhiều worktree: chưa rõ đường đó đi qua daemon thế nào.

**Nghiệm thu M1:** thêm/gỡ/kiểm tra codegraph, specmatic, deepwiki trên máy thật; `index codegraph` tạo
chỉ mục và cập nhật `.git/info/exclude`; `check` báo `degraded` khi daemon codegraph chạy với env hẹp;
`doctor` báo đúng service thiếu; cấu hình lạ bị `Validate()` từ chối; `GOOS=windows go vet ./...` qua.

## 6. M2 — Daemon gateway (`internal/mcpgw`)

Viết tay phần proxy như `internal/mcpserver` (S5): proxy phải đi qua nguyên JSON-RPC (đổi id, chuyển tiếp
request server→client, giữ nguyên schema), SDK có kiểu sẽ parse rồi serialize lại đúng chỗ đó.

1. **Một bản cho mỗi user:** khoá `flock`/`LockFileEx` trong thư mục trạng thái cấp user; tệp
   `gateway.json` (0600) ghi cổng, PID, token. Mã theo nền tảng tách `*_unix.go`/`*_windows.go`
   [k:01M746NASVKV9XMRHA4QNHJ3R8].
2. **Lắng nghe:** Streamable HTTP trên `127.0.0.1`, `POST/GET/DELETE /mcp`; từ chối Host không phải
   loopback và mọi `Origin` của trình duyệt; bearer token cho mọi route; endpoint quản trị `/admin/*`
   (status, restart) dùng cùng token.
3. **Stdio bridge:** ghép nhiều phiên vào một upstream:
   - đổi id JSON-RPC thành `gw_<gen>_<seq>_<phiên>` và đổi ngược; đổi id trong `notifications/cancelled`;
   - gọi `initialize`/`tools/list` một lần cho mỗi upstream, cache theo revision;
   - điền tham số mặc định theo danh mục (§5.1) trước khi chuyển lời gọi;
   - giới hạn kích thước frame và số lệnh ghi chờ; upstream vượt giới hạn thì coi như chết;
   - request server→client (`roots/list`, `sampling/*`, `elicitation/*`): `roots/list` trả gốc worktree
     của phân vùng; sampling/elicitation bị từ chối (agent không được hỏi người qua đường này).
4. **HTTP upstream** cho deepwiki/context7: giữ `Mcp-Session-Id` nếu server cấp, một handshake chung,
   retry có backoff **chỉ** cho tool chỉ đọc khai báo trong danh mục.
5. **Vòng đời:** lười, đếm phiên, idle, giới hạn restart; dừng thì kill cả cây tiến trình (unix: process
   group; Windows: Job Object). Tiến trình upstream tự tách ra (daemon của codegraph) không thuộc cây và
   không bị kill. Lệnh ghi bị timeout hoặc upstream chết giữa chừng ⇒ trả lỗi "không rõ kết quả", không
   bao giờ tự gọi lại.
6. **Môi trường:** upstream nhận env tối thiểu (PATH, HOME, ngôn ngữ…) + env mặc định của danh mục + biến
   credential đã khai báo, giá trị đọc từ `secrets.env` (D2) lúc khởi động upstream.
7. **Ghi nhận:** mỗi lời gọi ghi một sự kiện `mcp.called` (run/task/role, service, tool, nhóm, kết quả,
   thời gian; không ghi đối số hay kết quả) vào spool của repo tương ứng. Daemon không mở SQLite.
8. `bint mcp gateway status`: upstream đang chạy, PID, RSS/PSS (Linux: `/proc`, macOS: `ps`, Windows:
   API tiến trình), số phiên, lời gọi gần nhất, lỗi.

Không làm (so với bflow): grant theo registration có hạn, journal bền vững, fencing token, unit
systemd/launchd/schtasks, browser runtime riêng. SDK Go chính thức (`modelcontextprotocol/go-sdk`) chỉ
cân nhắc dùng trong test làm client kiểm tra tuân thủ, nếu yêu cầu phiên bản Go của nó khớp bản Go đang
ghim.

**Nghiệm thu M2:** 6 client đồng thời gửi cùng id vào một upstream stdio không lẫn phản hồi; hai worktree
không dùng chung tiến trình `worktree`; kill upstream thì lời gọi sau khởi động lại với generation mới;
Host/Origin/token sai bị 403/401; `compat` của specmatic không truyền `repoDir` vẫn chạy đúng worktree;
chạy trên Linux và Windows (CI).

### Trạng thái M2 (đã làm 2026-11-13)

Gói `internal/mcpgw`; lệnh `bint mcp gateway start|stop|status|restart <service>` (`docs/workflows.md`, mục "Quản lý MCP").
Nghiệm thu bằng test tự động với upstream giả (re-exec binary test): 6 client cùng id, hai worktree khác tiến trình,
kill upstream thì generation mới, Host/Origin/token sai bị 403/401, điền `repoDir`, khoá exclusive, retry HTTP, ghi
`mcp.called`. **Chưa làm:** chạy với codegraph/specmatic/deepwiki thật trên máy và Job Object trên Windows thật (CI chỉ
biên dịch và vet `GOOS=windows`) — người làm ở nghiệm thu thật.

Các điểm chốt khi làm (kế hoạch gốc để ngỏ):

- **Client chọn service bằng header lúc `initialize`, không bằng đường dẫn** (route vẫn là `/mcp`): `X-Bint-Service`,
  `X-Bint-Worktree`, `X-Bint-Run-Id`, `X-Bint-Task-Id`, `X-Bint-Role`, `X-Bint-Harness`, `X-Bint-Session-Id`. Phiên do
  gateway cấp (`Mcp-Session-Id`) gắn một service; shim M3 mở một phiên cho mỗi service. Role chỉ để ghi nhận ở M2.
- **`mcp.called` chỉ ghi khi biết lượt chạy**: có `run_id`, hoặc cặp harness + session. Thiếu cả hai, hoặc repo chưa init
  Bint, thì bỏ và đếm trong `status` (`dropped_events`) — tự đặt harness cho daemon sẽ tạo lượt giả khi ingest (mục 13.2).
  Daemon tự giải worktree → repo → thư mục trạng thái (`gitrepo` + `config.Load` + `state.Resolve`), không mở SQLite và
  không nhận đường dẫn spool từ client.
- **Revision** = băm lệnh, đối số, URL, phiên bản, tên biến của đăng ký và `Version` của danh mục; `mcp.yml` đọc lại mỗi
  lần mở phiên (D3). Cache `initialize`/`tools/list` chỉ trong bộ nhớ daemon, khoá (service, revision); daemon khởi
  động lại thì mất.
- **Giới hạn là hằng trong `mcpgw`** (không thêm khoá `.bint/config.yml`): idle 10 phút, `max_restarts` 3 trong cửa sổ
  10 phút (đăng ký ghi đè được), timeout lời gọi 5 phút, khởi động 30 giây, frame 8 MiB, body POST 8 MiB, hàng đợi ghi 64
  frame, TTL phiên 1 giờ không hoạt động, TTL khoá exclusive 10 phút, chờ khoá 30 giây, retry HTTP 3 lần (0,5/1/2 s).
  Khởi động thất bại cũng tính vào `max_restarts`; idle hay `restart` thủ công thì không.
- **Tool lạ** (ngoài mọi nhóm của danh mục) vẫn được chuyển tiếp, coi là không chỉ đọc; lọc theo quyền là M3.
- **Lỗi không rõ kết quả** (`-32012`) khi lệnh không chỉ đọc đã (bắt đầu) được ghi tới upstream mà upstream chết hoặc quá
  hạn; chưa ghi tới upstream (còn trong hàng đợi) thì chỉ là lỗi upstream (`-32013`). Mã khác: busy `-32010`, quá giới hạn
  restart `-32011`, quá hạn tool chỉ đọc `-32014`.
- **Daemon không tự thoát khi rảnh** (chỉ upstream có idle); tắt bằng `stop`, tín hiệu hoặc `/admin/stop`. `stop` không
  gọi được `/admin/stop` mà khoá `gateway.lock` còn bị giữ thì kill PID trong `gateway.json`.
- `start` chạy được trong phiên agent (M3 để shim tự khởi động); `stop`, `restart` chỉ người (D5). Hook không bao giờ
  khởi động daemon (test quét mã `internal/harness/**` và `internal/app/hook.go`).
- Windows: Job Object gán sau `Start` nên tiến trình cháu sinh cực sớm có thể lọt ra ngoài; RSS Windows là working set của
  tiến trình chính (không cộng con cháu, không có PSS).

## 7. M3 — Cửa vào gộp và cấp quyền theo role

1. `bint mcp serve` (D6):
   - tìm daemon qua `gateway.json`; không có hoặc không sống thì tự khởi động daemon tách rời (detached)
     rồi chờ có giới hạn; không lên được thì vẫn phục vụ tool của Bint và báo service lỗi trong
     `instructions`;
   - lúc `initialize`, lấy danh sách tool đã cache của các service được cấp, **không** bật upstream chỉ để
     lấy danh sách; service chưa từng chạy thì daemon chạy `tools/list` một lần và cache theo revision;
   - đặt tên theo tiền tố (§5.1), chuyển tiếp lời gọi qua HTTP tới daemon;
   - giữ shim tối giản (không chứa logic upstream) để giảm khả năng chính nó chết (S3).
2. **Xác định role:** shim đọc `BINT_RUN_ID`/`BINT_TASK_ID` từ môi trường lúc khởi động (agent không đổi
   được env của tiến trình MCP đã chạy) → role của bước; không có thì `interactive` (D4). Daemon lọc
   `tools/list` và từ chối lời gọi ngoài quyền (kể cả tên tool đoán mò).
3. **Bước chỉ đọc:** tool thuộc nhóm "ghi vào cây làm việc" không được cấp cho b-req/b-review dù `grants`
   có ghi; `bint mcp grants` báo cảnh báo. Lý do: hậu kiểm chụp cây làm việc sẽ làm bước thất bại. Danh mục
   khai nhóm này bằng `Service.WritesWorktree`: codegraph không có (chỉ ghi vào `.codegraph/`, bị ignore) còn specmatic
   có `run_contract_test`, `run_resiliency_test`, `manage_mock_server` (ghi báo cáo/trạng thái mà cwd trong thư mục
   trạng thái chỉ giảm nhẹ, không loại hẳn; thận trọng loại cả ba khỏi bước chỉ đọc).
   `backward_compatibility_check` có thao tác git nào ghi vào repo không thì chưa chắc; hậu kiểm sẽ bắt được nếu có.
4. **`bint run`:** `MCPConfig` của M0 giờ trỏ vào `bint mcp serve` nên tự có tool theo role, như nhau trên
   Claude và Codex. Antigravity (chỉ MCP cấp user): một mục user-level `bint mcp serve`, role lấy theo env
   như trên.
5. `bint init --mcp` không đổi đối với harness; thêm tuỳ chọn ghi mục MCP cấp user cho agy.
6. Hướng dẫn di chuyển: gỡ các mục MCP toàn cục trùng với service đã đăng ký trong Bint (tránh hai bản);
   `bint init --update` thêm quy tắc quyền `mcp__bint__*`.

**Nghiệm thu M3:** 4 phiên Claude + 1 Codex cùng repo chỉ có một client codegraph và một specmatic cho mỗi
worktree và một jira cho cả máy; bước b-review không thấy `spec_manage_mock_server`; lời gọi ngoài quyền bị
từ chối; spool có `mcp.called` đúng run/role; daemon chết giữa phiên thì shim khởi động lại được; kiểm một
lần Claude/Codex làm gì khi chính shim chết (S3).

### Trạng thái M3 (đã làm 2026-11-13)

Mã: `internal/mcpgw` (`shim.go`, `binding.go`, `toolscache.go`, quyền trong `http.go`/`call.go`), `internal/mcpserver`
(`Proxy`, xử lý `tools/call` đồng thời), `internal/config/mcp.go` (`MCP.Resolve`), `internal/app/mcp_shim.go`,
`internal/workflow/mcp.go`, `internal/eval` (`AgentRequest.MCPTools`), `internal/harness` (quy tắc quyền, MCP cấp user).
Nghiệm thu bằng test tự động với upstream giả (re-exec binary test, daemon thật là tiến trình con): chuyển tiếp đúng tên
tiền tố và nguyên vẹn, tự khởi động daemon tách rời, daemon không lên thì vẫn đủ 6 tool + instructions nêu lý do, kill daemon
giữa phiên thì lời gọi sau thành công (nhiều lời gọi chỉ nối lại một lần), cache đĩa sống qua lần khởi động lại daemon, role
do daemon tự giải, b-review không thấy tool `WritesWorktree`, lời gọi ngoài quyền bị từ chối ở shim và ở daemon (HTTP thẳng),
`mcp.called` đúng run/task/role đã giải, đối số Claude/Codex của bước. **Chưa làm:** chạy với nhiều phiên Claude/Codex thật,
kiểm S3 (harness làm gì khi chính shim chết), Codex có chuyển env tới server MCP stdio không, Codex có nhận `approval_mode`
cho tool chuyển tiếp không, Job Object/detach trên Windows thật — người làm ở nghiệm thu thật.

Điểm chốt khi làm (kế hoạch gốc để ngỏ hoặc mâu thuẫn với mã):

- **Role không lấy từ header.** Daemon không mở SQLite và `state.json` không giữ bước hiện tại của lượt con, nên Runner ghi
  **bản ghi lượt chạy** `<state>/runtime/mcp-runs/<run_id>.json` (`run_id` phải là ULID; xoá sau bước) và daemon đọc theo
  `X-Bint-Run-Id`. Không có `run_id` → `interactive` (D4); `run_id` không có bản ghi, hoặc `X-Bint-Task-Id` lệch bản ghi → không
  cấp gì (đóng an toàn). `X-Bint-Role` bị bỏ qua; `mcp.called` ghi role đã giải. Thẩm quyền của bản ghi là "chính sách,
  không phải ranh giới bảo mật" (§9).
- **Run/task nằm trong đối số của shim** (`bint mcp serve --run-id … --task-id …`, cờ ẩn) vì Codex lọc env của server MCP
  stdio; env `BINT_RUN_ID`/`BINT_TASK_ID` giữ cho Claude và agy.
- **Một hàm giải quyền** `config.MCP.Resolve(role, readOnly, registered)` cho daemon, `bint mcp grants` và allowlist của
  bước: grants của role (hoặc D4 ngầm) ∩ `mcp.services` ∩ đã đăng ký, bỏ `WritesWorktree` khi `readOnly`
  (`workflow.ReadOnlyRole`). Daemon cấp theo **nhóm** (tool của nhóm có tool được cấp, theo danh mục của daemon).
- **Daemon từ chối** bằng `CodeNotGranted` (-32015): `initialize` của service không được cấp (một phiên gắn một service, nên
  shim chỉ mở phiên cho service ứng viên) và `tools/call` ngoài quyền (kể cả tên đoán mò), trước khi chạm upstream; ghi
  `mcp.called` với `outcome: denied`. Không có `X-Bint-Worktree` thì không biết dự án nào → không cấp (đổi so với M2: client
  gọi thẳng phải khai grants hoặc dựa vào D4 ở một repo có `mcp.services`). Tệp cấu hình chọn theo `LoadFirst(ConfigRoots)`
  như `app.OpenWorkspace`.
- **Cache `tools/list` thêm tầng đĩa** `<gateway>/tools-cache/<service>-<revision>.json` (0600, ghi nguyên tử): daemon khởi
  động lại không phải spawn upstream chỉ để lấy danh sách.
- **Shim**: `Shim` implement `mcpserver.Proxy`; tập ứng viên tính ở `app.NewMCPShim` (không có thì không chạm gateway:
  `BINT_MCP_NO_GATEWAY`, repo không khai service, role không có service khả dụng; chế độ `--eval-config` không đi qua đó).
  Chờ daemon 3 giây (`StartWait`), mở phiên mọi service song song (4 giây); tổng `initialize` ≈ 8 giây, dưới timeout khởi động MCP 10 giây của Codex. Nối lại single-flight khi dial lỗi, 404, 401;
  gửi lại chỉ khi chắc chưa tới daemon hoặc tool chỉ đọc. `StartDetached` chờ daemon khác đang lên (hai shim khởi động cùng
  lúc) thay vì báo lỗi. `mcpserver.Serve` xử lý `tools/call` đồng thời (các method khác tuần tự) và `notifications/cancelled`
  huỷ ctx lời gọi.
- **Lỗi trả harness:** lỗi của daemon/upstream thành kết quả `isError: true` kèm mã trong thông điệp.
- **Quyền harness:** `bint init --mcp`/`--update` thêm `mcp__bint__*` vào `permissions.allow` (scope hook, idempotent);
  Codex ở `bint run` có `approval_mode="approve"` cho tập tool được cấp, phiên Codex tương tác giữ hỏi duyệt mặc định.
  `bint init --mcp-user` ghi mục `bint` vào `~/.gemini/config/mcp_config.json` (Antigravity).
- **Chưa làm / giới hạn:** `mcp.called` của phiên tương tác bị bỏ nếu thiếu harness + session (shim chưa đặt
  `X-Bint-Harness`/`X-Bint-Session-Id`); `forward` (resources/prompts) chưa lọc theo quyền; subagent của driver session dùng
  role `interactive` của phiên cha (G3).

## 8. M4 — Màn MCP trong TUI

Màn mới `mcp` (sau `system` trong `actions.Screens`), mọi action có `CLIPath`:

- **Danh sách:** mục danh mục với trạng thái `chưa đăng ký` / `đã đăng ký` / `đang chạy` (PID, RSS, số
  phiên) / `degraded` / `lỗi`; lọc theo "dự án đang dùng".
- **Thao tác:** thêm (form: lệnh, đối số, tên biến môi trường; không có ô nhập giá trị bí mật, chỉ hiện
  đường dẫn `secrets.env` và biến còn thiếu), gỡ (xác nhận), kiểm tra, lập chỉ mục (codegraph), khởi động
  lại, xem log gần nhất của upstream.
- **Quyền:** bảng role × service (nhóm thao tác) chỉ để xem, đọc từ cấu hình đã giải; nút mở
  `.bint/config.yml` trong `$EDITOR` như màn Hệ thống.
- Phiên agent: các thao tác ghi bị ẩn hoặc vô hiệu như chế độ chỉ đọc hiện có (`docs/tui-plan.md` §7.4).
- Mục "MCP" trong màn Hệ thống (`mcp tools`) chuyển sang màn này.

**Nghiệm thu M4:** golden test các trạng thái; test phủ lệnh xanh; chụp màn 140×40 và 80×24.

### Trạng thái M4 (đã làm 2026-11-13)

- **Màn:** `internal/tui/screens/mcp` (tab thứ tám, phím `8`, ngay sau Hệ thống), ba thẻ phụ — **Dịch vụ**, **Quyền**, **Công cụ Bint** —
  mô tả ở `docs/tui-plan.md` mục 5.8 và `docs/workflows.md` ("Màn MCP trong TUI"). Hành động ở `internal/tui/actions/mcp.go`; mọi hành động gọi
  dịch vụ có `CLIPath` (`mcp.list`, `mcp.gateway_status`, `mcp.grants`, `mcp.tools`, `mcp.log`, `mcp.check`, `mcp.index`, `mcp.restart`,
  `mcp.add`, `mcp.remove`, `mcp.gateway_start`, `mcp.gateway_stop`); hành động giao diện thuần (`mcp.filter`, `mcp.refresh`,
  `mcp.next_view`, `mcp.prev_view`, `mcp.config_edit`) không có. Trong nhóm `mcp` chỉ còn `mcp serve` ở `Excluded` của test phủ lệnh.
- **Trạng thái service:** hàm thuần `screens/mcp.State(st, gateway, check, worktree)` theo đúng thứ tự ưu tiên: chưa đăng ký → (check fail, hoặc
  phân vùng `Limited`, hoặc phân vùng không chạy mà còn `LastError` khi không phân vùng nào của service đang chạy; gateway xoá `LastError` khi
  khởi động lại thành công) lỗi → (check degraded) degraded → (có phân vùng đang chạy; ưu tiên phân vùng của
  worktree hiện tại, `+N` khi còn phân vùng khác) đang chạy → đã đăng ký. Danh sách không tự check nên degraded/lỗi do check chỉ có sau khi người dùng
  chạy check hoặc thêm service; kết quả check sống trong phiên.
- **Quyền thao tác:** `add`, `remove`, `index`, `gateway restart`, `gateway stop` là `Writes` + `HumanOnly` (khoá trong phiên agent, ẩn ở chế độ chỉ đọc);
  `gateway start` là `Writes` nhưng agent được chạy (D5); `check` không `Writes` (chỉ tạo thư mục cwd riêng của service kiểu `state`, D5 cho agent
  chạy). Kiểm tra (`check`), lập chỉ mục và khởi động lại là `Action.Job` có log trong bảng việc nền.
- **Dịch vụ mới ở `internal/app`:** `MCPServiceStatus.MissingEnv` (tên biến credential còn thiếu, không giá trị; `mcp list --json` có thêm trường
  `missing_env`), `MCPSecretsFile()` và `MCPGatewayLog` (`bint mcp gateway log <service> [--lines N] [--json]`).
- **`internal/mcpgw`:** endpoint `GET /admin/log?service=` (cùng bearer token và kiểm Host/Origin như các route `/admin`) trả
  `[]PartitionLog{Partition, Running, Stderr}`, stderr che từng dòng bằng `ScrubSecrets`; `AdminClient.Log` và lỗi có kiểu `StatusError`. Daemon bản cũ
  trả 404: `MCPGatewayLog` vẫn trả phần `gateway.log` kèm ghi chú.
- **Sửa cấu hình:** `mcp.config_edit` gửi `overview.NavMsg{ConfigEdit}`; mô hình gốc chuyển sang Hệ thống và chạy `system.config_edit` (không chép luồng).
- **Hệ thống:** thẻ MCP (`system.mcp_tools`, khoá `sy.tab.mcp`, `sy.mcp.*`) đã gỡ; thẻ con của Hệ thống còn chín thẻ.
- **Thanh tab:** thêm tab thứ tám làm 80 cột không đủ chỗ ở locale giả (nhãn đầy đủ của tab đang chọn + nhãn rút gọn của bảy tab còn lại): `tabs.Render` thu
  hẹp dấu ngăn (`" │ "` → `" │"` → `"│"`) trước khi cắt, có test ở mọi tab đang chọn.
- **Kiểm thử:** golden `mcp_*` ở 140×40 và 80×24 × vi/en/xx; golden của màn khác chỉ đổi dòng thanh tab (và dòng thẻ con của Hệ thống, thêm `help_*` vì
  phím `1-8` và các hành động MCP mới); test chỉ-đọc/HumanOnly cho cả `Job`; test `State` dạng bảng.
- **Chưa làm / giới hạn:** chưa chạy với codegraph/specmatic/deepwiki thật trên máy và RSS trên Windows thật (chỉ người nghiệm thu); chưa chụp VHS (mục 12.10 của
  `docs/tui-plan.md` vẫn là việc của người). Danh sách trạng thái đọc `gateway status` mỗi lần nạp lại (`Ctrl+R`), không tự làm mới nền.

## 9. Rủi ro và giới hạn

| Rủi ro | Xử lý |
|---|---|
| Daemon chết làm mọi phiên mất tool cùng lúc | Shim tự khởi động lại daemon; daemon khởi động lại upstream theo generation |
| Upstream có trạng thái theo kết nối bị dùng chung | Mặc định của danh mục là `worktree`/`session`; chỉ đặt `machine` khi đã kiểm |
| Agent cùng UID đọc `gateway.json` rồi gọi thẳng daemon, vượt quyền theo role | Quyền theo role là **chính sách**, không phải ranh giới bảo mật. Bước chỉ đọc chỉ có `Bash(bint:*)`/`git …` nên không gọi được HTTP; b-feat có Bash đầy đủ thì vượt được. Ghi rõ trong tài liệu |
| Credential trong `secrets.env` và trong env của upstream sống lâu | Tệp 0600; chỉ nạp biến đã khai báo; `bint mcp gateway stop` dừng upstream; không ghi ra log/spool |
| Daemon codegraph bị phiên ngoài Bint khởi động với env hẹp | Nhóm mặc định chỉ `explore`; `check` báo `degraded` (§5.5) |
| codegraph trên ổ FUSE/mạng (WAL, socket) | Đã chạy được trên ntfs-3g (§10, S1). Ổ mạng hoặc `/mnt` của WSL2 thì upstream khuyên `--no-watch`/`CODEGRAPH_NO_DAEMON`; `check` báo Journal khác `wal` |
| specmatic gọi `npx specmatic@latest` (JVM) mỗi lời gọi | Phần nặng nằm ở lời gọi, gateway không giảm được; giới hạn đồng thời theo nhóm |
| Lợi ích chất lượng của codegraph/deepwiki/specmatic chưa được chứng minh | M5 đo trước khi đưa vào `grants` mặc định của mẫu cấu hình |
| Tên tool đổi (D6) làm hỏng quyền đã cấp trong settings của harness | `bint init --update` thêm quy tắc `mcp__bint__*` mới; ghi chú di chuyển |

## 10. Kết quả xác minh

| # | Câu hỏi | Kết quả |
|---|---|---|
| S1 | codegraph: bản nào, cwd/`roots`, chỉ mục ở đâu | `@colbymchenry/codegraph` 1.6.2, chạy thử 2026-11-12 trên bản sao repo Bint (ext4 và ntfs-3g). Chỉ mục `.codegraph/codegraph.db` (SQLite WAL) trong repo, tự ignore. Trên ntfs-3g WAL, socket daemon, watcher đều chạy (tự đồng bộ 124 ms). Client ~170 MB/phiên (shim npm 42 + Node 86 + watchdog 43), daemon ~270 MB + 43 MB/project. 8 tool, đều `readOnlyHint`, nhận `projectPath`; `explore` 0,8 s. Daemon giữ env của client đầu tiên. Chép DB sang worktree khác dùng được. Quyết định: §5.5 |
| S2 | specmatic-mcp ghi gì ở đâu | Đọc mã 1.2.0: `run_contract_test` ghi spec ra thư mục tạm nhưng ghi JUnit vào `./build/reports/specmatic` **theo cwd** và dò tệp báo cáo mới bằng so trước/sau (hai lời gọi song song cùng cwd lẫn kết quả); `backward_compatibility_check` chạy trong `repoDir`, mặc định `process.cwd()`; `manage_mock_server` tạo thư mục tạm và giữ cổng; mỗi lời gọi spawn `npx specmatic@latest`. Quyết định: cwd trong thư mục trạng thái, điền `repoDir`, `mock` độc quyền, `contract` giới hạn một lời gọi đồng thời mỗi phân vùng (§5.1) |
| S3 | Harness làm gì khi server stdio MCP chết giữa phiên | Với shim D6, chỉ còn trường hợp chính shim chết. Giữ shim tối giản; kiểm một lần trên Claude/Codex trong nghiệm thu M3 |
| S4 | Codex tắt MCP toàn cục cho một lần `codex exec` | Đã kiểm trên 0.167: `-c mcp_servers.<tên>.enabled=false` làm server đó `disabled`. `--ignore-user-config` có nhưng bỏ luôn cấu hình model/provider. Quyết định: M0 bước 2 |
| S5 | Go MCP SDK hay viết tay | Viết tay proxy; SDK chỉ cân nhắc trong test (§6) |

## 11. Thứ tự và phụ thuộc

`M0` (độc lập) → `M1` → `M2` → `M3` → `M4`; `M5` chạy sau M3.

**M5 — Đo:**
- Bộ nhớ: 4–6 phiên đồng thời, so PSS trước/sau (cấu hình MCP toàn cục hiện nay ↔ gateway). Với
  codegraph, mức tiết kiệm dự kiến là (số phiên − 1) × ~170 MB mỗi worktree.
- Chất lượng: nhánh eval có/không `grants` cho b-req/b-review trên tác vụ có đổi API (specmatic) và tác
  vụ cần định vị mã (codegraph); chỉ đưa `grants` vào mẫu mặc định khi có cải thiện.

### Trạng thái M5 (bộ nhớ đã đo 2026-11-13; chất lượng chưa)

**Bộ nhớ.** Đo trên Linux, codegraph 1.6.2 (`npm i @colbymchenry/codegraph@1.6.2`), bản sao repo Bint ở commit 38a1a9d đã `codegraph init`,
môi trường cô lập (`HOME`, `BINT_CONFIG_HOME`, `BINT_STATE_DIR` tạm; `mcp.yml` đăng ký codegraph; `mcp.services: {codegraph: {}}`,
`grants: {interactive: [codegraph.explore, codegraph.query]}`). Mỗi "phiên" là một client MCP giả lập harness: `initialize`, `tools/list`, một lời
gọi `explore` (~25 nghìn ký tự). Đo PSS (`/proc/<pid>/smaps_rollup`, chia đều trang dùng chung) của mọi tiến trình MCP sau 5 giây; tiến trình
của harness (Claude/Codex) như nhau ở hai bên nên không tính.

- *Toàn cục* (cách hiện nay): mỗi phiên một `bint mcp serve` (`BINT_MCP_NO_GATEWAY=1`) và một `codegraph serve --mcp`; codegraph tự dùng chung
  một daemon theo project.
- *Gateway*: mỗi phiên một shim `bint mcp serve`; daemon gateway giữ một upstream codegraph cho worktree.

| Phiên | Toàn cục (MB) | Gateway (MB) | Chênh |
|---|---|---|---|
| 1 | 360 | 381 | +21 (daemon gateway ~15 MB, shim) |
| 4 | 654 | 491 | −163 (−25 %) |
| 6 | 750 | 511 | −239 (−32 %) |

Mỗi phiên thêm tốn ~78 MB PSS ở cách toàn cục (client codegraph: shim npm ~9, Node ~40–50, watchdog ~11; `bint mcp serve` ~9) và ~26 MB ở gateway
(shim ~10 MB và phần tăng của daemon codegraph khi có thêm lời gọi). Mức tiết kiệm thật là ~50 MB PSS mỗi phiên thêm trên cùng worktree, thấp hơn
con số ~170 MB của §5.5/S1 vì số đó là RSS (trang của Node dùng chung giữa các client bị tính lặp). `initialize` của shim ≤ 0,1 s khi daemon đã
chạy; mọi lời gọi qua gateway thành công.

**Chưa làm:**
- Đo trên Windows (Job Object) và với nhiều worktree (mỗi worktree một phân vùng codegraph).
- **Chất lượng:** nhánh eval có/không `grants` cho b-req/b-review (codegraph trên tác vụ cần định vị mã, specmatic trên tác vụ đổi API). Cần
  service đăng ký thật trên máy (`bint mcp add`, chỉ người) và ngân sách eval; vì vậy mẫu cấu hình mặc định vẫn để `grants: {}`.

Liên quan [isolation-subagent-plan.md](isolation-subagent-plan.md): G3 (subagent trong phiên) dùng chung
kết nối MCP của phiên cha, nên quyền theo role của subagent phải đặt ở định nghĩa subagent (danh sách tool
`mcp__bint__*`) chứ không qua env như §7.2. G2 (worktree riêng cho mỗi lượt) chép chỉ mục codegraph theo
§5.5.
