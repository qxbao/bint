# Kế hoạch: gateway MCP dùng chung và cấp MCP theo role

Trạng thái: **bản nháp chờ chốt** (2026-11-12), chưa triển khai. Gồm sáu giai đoạn: M0 (cấp MCP của
Bint cho các bước `bint run`, sửa ngay được), M1 (danh mục, cấu hình hai tầng, API/CLI quản lý),
M2 (daemon gateway), M3 (cửa vào gộp và cấp quyền theo role), M4 (màn TUI), M5 (đo).
Các mục ở §3 "Đề xuất chờ chốt" và §10 "Cần xác minh" phải xong trước khi bắt đầu giai đoạn liên quan.

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

## 3. Quyết định

### 3.1. Đã chốt (2026-11-11/12)

| Vấn đề | Quyết định |
|---|---|
| Gom về một gateway | Có. Gateway là điểm quản lý duy nhất (đăng ký, credential, health, log, cấu hình cho từng harness). **Không** có nghĩa một tiến trình dùng chung cho mọi service: mỗi service giữ chính sách chia sẻ riêng (§5.3). |
| Tự cài MCP | Không. Người dùng chủ động thêm qua API/CLI/TUI. |
| Cấp MCP cho agent | Khai báo trong cấu hình dự án, khoá theo role (tên tự do). |
| Sửa H2/H3 | Gộp vào kế hoạch này (giai đoạn M0), không vá riêng. |
| gitnexus, mempalace | Không đưa vào danh mục (codegraph thay gitnexus; mempalace không có ích cho Bint). |

### 3.2. Đề xuất chờ chốt

| # | Vấn đề | Đề xuất |
|---|---|---|
| D1 | Cài đặt | v1 chỉ **đăng ký + kiểm tra** (`initialize` + `tools/list`); người dùng tự cài gói. Bộ cài để giai đoạn sau. |
| D2 | Credential | Chỉ lưu **tên** biến môi trường; form TUI không ghi giá trị bí mật vào YAML. |
| D3 | Thêm/gỡ khi phiên đang chạy | Chỉ có hiệu lực với phiên mới. Không làm `notifications/tools/list_changed` ở v1. |
| D4 | Phiên tương tác (ngoài `bint run`) | Role `interactive`; không khai báo thì được mọi service **đã đăng ký và được dự án dùng** (giữ trải nghiệm hiện nay). |
| D5 | Ai sửa đăng ký và quyền | Chỉ người: thao tác ghi của `bint mcp` trả `ErrAgentForbidden` khi `InAgentSession`. |
| D6 | Hình dạng client | `bint mcp serve` là **cửa vào duy nhất**: phục vụ tool của Bint và chuyển tiếp tool của service với tiền tố (`codegraph_*`, `specmatic_*`…). Tên tool đổi thành `mcp__bint__<service>_<tool>`, quyền trong settings của harness phải sửa một lần. Thay thế: mỗi service một mục `type: http` riêng (giữ tên cũ, không chạy được trên agy). |
| D7 | Kênh client ↔ daemon | HTTP loopback `127.0.0.1:<cổng động>` + bearer token, kiểm Host/Origin. Không dùng Unix socket vì Windows và vì phương án thay thế của D6 cần URL. |

## 4. M0 — Cấp MCP của Bint cho các bước `bint run`

Làm được ngay, không phụ thuộc gateway. Khi có M3, chỗ truyền `MCPConfig` này trở thành chỗ cấp quyền.

1. `workflow.Runner` sinh `<state>/runtime/mcp/<run-id>.json` chỉ chứa server `bint` (cùng dạng
   `eval/arms.go:394`, lệnh là đường dẫn tuyệt đối của binary đang chạy) và truyền qua
   `AgentRequest.MCPConfig`. Claude nhận `--mcp-config` + các tool `mcp__bint__*` trong allowlist (cả bước
   chỉ đọc). Codex nhận `-c mcp_servers.bint.*` sẵn có.
2. Bước Codex không còn nạp MCP toàn cục (H3): cách tắt xem S4 (§10). Nếu Codex không cho tắt riêng
   `mcp_servers` thì chạy bằng profile tạm như eval.
3. Skill b-req/b-feat/b-review: ưu tiên `context_for_task`/`context_for_paths` khi có, giữ CLI làm dự
   phòng (agy). Nhắc gọi `cite_knowledge` khi áp dụng một mục.
4. Thêm `bint knowledge cite <id>` để harness không có MCP vẫn trích dẫn được (H5). Lệnh CLI mới phải khai
   báo ở `internal/tui/actions/coverage.go`.
5. `docs/workflows.md`: ghi rõ tập MCP của mỗi bước.

**Nghiệm thu M0:** log bước Claude có `--mcp-config`; một lượt `bint run b-do` thật ghi được ít nhất một
dòng `knowledge_citations` từ bước con; bước Codex không thấy server toàn cục (kiểm bằng `tools/list` hoặc
log JSONL); test hồi quy cho đối số Claude/Codex.

## 5. M1 — Danh mục, cấu hình hai tầng, API/CLI

### 5.1. Danh mục nhúng trong binary (`internal/mcpcat`)

Mỗi mục khai báo: id, kiểu kết nối (`stdio` / `http`), lệnh mặc định gợi ý, chính sách chia sẻ (§5.3),
**nhóm thao tác** (tên nhóm → danh sách tool), tool chỉ đọc (được retry), tool ghi vào cây làm việc
(cấm ở bước chỉ đọc), biến môi trường cần có, tuỳ chọn riêng của dự án.

Quyền trong cấu hình trỏ tới nhóm, không trỏ tên tool thô: upstream đổi tên tool thì chỉ sửa danh mục.

| Service | Kiểu | Chia sẻ | Nhóm thao tác (dự kiến) | Ghi chú |
|---|---|---|---|---|
| `codegraph` | stdio | `worktree` | `read`, `index` (độc quyền) | Bản cụ thể và nơi lưu chỉ mục: S1 |
| `specmatic` | stdio | `worktree` | `compat`, `contract`, `mock` (độc quyền, giữ cổng) | Chỉ hữu ích khi repo có OpenAPI/AsyncAPI; nơi ghi báo cáo: S2 |
| `deepwiki` | http remote | dùng chung, không trạng thái | `read` | Không sinh tiến trình; qua gateway để có quyền theo role và ghi nhận |
| `context7` | http remote | dùng chung | `read` | Tuỳ chọn, trùng vai trò với deepwiki |
| `jira` | stdio (`atlassian-jira-mcp`) | `machine` | `read`, `write` | Tool ghi không bao giờ retry |
| `chrome-devtools` | stdio | `session`, khởi động lười | `read`, `act` | Không dùng chung tiến trình (trạng thái trang đang chọn) |

`bint` không nằm trong danh mục: nó là chính cửa vào.

### 5.2. Hai tầng cấu hình

| Tầng | Tệp | Nội dung | Ai sửa |
|---|---|---|---|
| Máy | `~/.config/bint/mcp.yml` (Windows `%APPDATA%\bint\mcp.yml`), không commit | service đã đăng ký: lệnh/đối số hoặc URL, phiên bản ghim, **tên** biến môi trường credential, giới hạn (idle, restart) | API/CLI/TUI |
| Dự án | `.bint/config.yml`, commit | service dự án dùng (bắt buộc/tuỳ chọn), tuỳ chọn riêng, `grants` theo role | Người sửa tệp (TUI chỉ xem) |

```yaml
mcp:
  services:
    codegraph: {}
    specmatic: { required: false, options: { specs: "api/**/*.yaml" } }
    deepwiki: {}
  grants:                  # khoá là tên role tự do; mặc định không cấp gì
    planner:     [codegraph.read, deepwiki.read, specmatic.compat]
    implementer: [codegraph.read, deepwiki.read, specmatic.contract, specmatic.mock]
    reviewer:    [codegraph.read, specmatic.compat]
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
| `bint mcp add <service> [--command … --arg … --env-name …\|--url …]` | đăng ký, rồi kiểm tra | chỉ người (D5) |
| `bint mcp remove <service>` | gỡ đăng ký, dừng tiến trình nếu đang chạy | chỉ người |
| `bint mcp check [<service>]` | `initialize` + `tools/list`, đối chiếu nhóm thao tác với danh mục | — |
| `bint mcp grants [--role <r>]` | quyền đã giải (role → tool thật), báo service thiếu | — |
| `bint mcp gateway start\|stop\|status\|restart <service>` | M2 | stop/restart chỉ người |

Lệnh và cờ mới phải khai báo ở `internal/tui/actions/coverage.go`. Test của thao tác chỉ-dành-cho-người
gọi `asHuman(t)` [k:01M746PHJXDZN6QMA3BEVEDNPP].

**Nghiệm thu M1:** thêm/gỡ/kiểm tra codegraph, specmatic, deepwiki trên máy thật; `doctor` báo đúng
service thiếu; cấu hình lạ bị `Validate()` từ chối; `GOOS=windows go vet ./...` qua.

## 6. M2 — Daemon gateway (`internal/mcpgw`)

1. **Một bản cho mỗi user:** khoá `flock`/`LockFileEx` trong thư mục trạng thái cấp user; tệp
   `gateway.json` (0600) ghi cổng, PID, token. Mã theo nền tảng tách `*_unix.go`/`*_windows.go`
   [k:01M746NASVKV9XMRHA4QNHJ3R8].
2. **Lắng nghe:** Streamable HTTP trên `127.0.0.1`, `POST/GET/DELETE /mcp`; từ chối Host không phải
   loopback và mọi `Origin` của trình duyệt; bearer token cho mọi route; endpoint quản trị `/admin/*`
   (status, restart) dùng cùng token.
3. **Stdio bridge:** ghép nhiều phiên vào một upstream:
   - đổi id JSON-RPC thành `gw_<gen>_<seq>_<phiên>` và đổi ngược; đổi id trong `notifications/cancelled`;
   - gọi `initialize`/`tools/list` một lần cho mỗi upstream, cache theo revision;
   - giới hạn kích thước frame và số lệnh ghi chờ; upstream vượt giới hạn thì coi như chết;
   - request server→client (`roots/list`, `sampling/*`, `elicitation/*`): `roots/list` trả gốc worktree
     của phân vùng; sampling/elicitation bị từ chối (agent không được hỏi người qua đường này).
4. **HTTP upstream** cho deepwiki/context7: giữ `Mcp-Session-Id` nếu server cấp, một handshake chung,
   retry có backoff **chỉ** cho tool chỉ đọc khai báo trong danh mục.
5. **Vòng đời:** lười, đếm phiên, idle, giới hạn restart; dừng thì kill cả cây tiến trình (unix: process
   group; Windows: Job Object). Lệnh ghi bị timeout hoặc upstream chết giữa chừng ⇒ trả lỗi "không rõ kết
   quả", không bao giờ tự gọi lại.
6. **Môi trường:** upstream nhận env tối thiểu + biến credential đã khai báo, đọc từ môi trường của daemon
   (daemon được khởi động bởi shim trong phiên của người dùng nên kế thừa env đó).
7. **Ghi nhận:** mỗi lời gọi ghi một sự kiện `mcp.called` (run/task/role, service, tool, nhóm, kết quả,
   thời gian; không ghi đối số hay kết quả) vào spool của repo tương ứng. Daemon không mở SQLite.
8. `bint mcp gateway status`: upstream đang chạy, PID, RSS/PSS (Linux: `/proc`, macOS: `ps`, Windows:
   API tiến trình), số phiên, lời gọi gần nhất, lỗi.

Không làm (so với bflow): grant theo registration có hạn, journal bền vững, fencing token, unit
systemd/launchd/schtasks, browser runtime riêng.

**Nghiệm thu M2:** 6 client đồng thời gửi cùng id vào một upstream stdio không lẫn phản hồi; hai worktree
không dùng chung tiến trình `worktree`; kill upstream thì lời gọi sau khởi động lại với generation mới; Host/
Origin/token sai bị 403/401; chạy trên Linux và Windows (CI).

## 7. M3 — Cửa vào gộp và cấp quyền theo role

1. `bint mcp serve` (D6):
   - tìm daemon qua `gateway.json`; không có hoặc không sống thì tự khởi động daemon tách rời (detached)
     rồi chờ có giới hạn; không lên được thì vẫn phục vụ tool của Bint và báo service lỗi trong
     `instructions`;
   - lúc `initialize`, lấy danh sách tool đã cache của các service được cấp, **không** bật upstream chỉ để
     lấy danh sách; service chưa từng chạy thì daemon chạy `tools/list` một lần và cache theo revision;
   - đặt tên `<service>_<tool>`, chuyển tiếp lời gọi qua HTTP tới daemon.
2. **Xác định role:** shim đọc `BINT_RUN_ID`/`BINT_TASK_ID` từ môi trường lúc khởi động (agent không đổi
   được env của tiến trình MCP đã chạy) → role của bước; không có thì `interactive` (D4). Daemon lọc
   `tools/list` và từ chối lời gọi ngoài quyền (kể cả tên tool đoán mò).
3. **Bước chỉ đọc:** tool thuộc nhóm "ghi vào cây làm việc" (vd. rebuild chỉ mục nằm trong repo) không
   được cấp cho b-req/b-review dù `grants` có ghi; `bint mcp grants` báo cảnh báo. Lý do: hậu kiểm chụp
   cây làm việc sẽ làm bước thất bại.
4. **`bint run`:** `MCPConfig` của M0 giờ trỏ vào `bint mcp serve` nên tự có tool theo role, như nhau trên
   Claude và Codex. Antigravity (chỉ MCP cấp user): một mục user-level `bint mcp serve`, role lấy theo env
   như trên.
5. `bint init --mcp` không đổi đối với harness; thêm tuỳ chọn ghi mục MCP cấp user cho agy.
6. Hướng dẫn di chuyển: gỡ các mục MCP toàn cục trùng với service đã đăng ký trong Bint (tránh hai bản).

**Nghiệm thu M3:** 4 phiên Claude + 1 Codex cùng repo chỉ có một tiến trình codegraph/specmatic cho mỗi
worktree và một jira cho cả máy; bước b-review không thấy `specmatic_mock`; lời gọi ngoài quyền bị từ chối;
spool có `mcp.called` đúng run/role; daemon chết giữa phiên thì shim khởi động lại được (S3).

## 8. M4 — Màn MCP trong TUI

Màn mới `mcp` (sau `system` trong `actions.Screens`), mọi action có `CLIPath`:

- **Danh sách:** mục danh mục với trạng thái `chưa đăng ký` / `đã đăng ký` / `đang chạy` (PID, RSS, số
  phiên) / `lỗi`; lọc theo "dự án đang dùng".
- **Thao tác:** thêm (form: lệnh, đối số, tên biến môi trường; không có ô nhập giá trị bí mật), gỡ (xác
  nhận), kiểm tra, khởi động lại, xem log gần nhất của upstream.
- **Quyền:** bảng role × service (nhóm thao tác) chỉ để xem, đọc từ cấu hình đã giải; nút mở
  `.bint/config.yml` trong `$EDITOR` như màn Hệ thống.
- Phiên agent: các thao tác ghi bị ẩn hoặc vô hiệu như chế độ chỉ đọc hiện có (`docs/tui-plan.md` §7.4).
- Mục "MCP" trong màn Hệ thống (`mcp tools`) chuyển sang màn này.

**Nghiệm thu M4:** golden test các trạng thái; test phủ lệnh xanh; chụp màn 140×40 và 80×24.

## 9. Rủi ro và giới hạn

| Rủi ro | Xử lý |
|---|---|
| Daemon chết làm mọi phiên mất tool cùng lúc | Shim tự khởi động lại daemon; daemon khởi động lại upstream theo generation; S3 kiểm hành vi kết nối lại của harness |
| Upstream có trạng thái theo kết nối bị dùng chung | Mặc định của danh mục là `worktree`/`session`; chỉ đặt `machine` khi đã kiểm |
| Agent cùng UID đọc `gateway.json` rồi gọi thẳng daemon, vượt quyền theo role | Quyền theo role là **chính sách**, không phải ranh giới bảo mật. Bước chỉ đọc chỉ có `Bash(bint:*)`/`git …` nên không gọi được HTTP; b-feat có Bash đầy đủ thì vượt được. Ghi rõ trong tài liệu |
| Credential ở môi trường của daemon tồn tại lâu hơn phiên | Chỉ nạp biến đã khai báo; `bint mcp gateway stop` xoá; không ghi ra log/spool |
| Tool của service ghi vào cây làm việc trong bước chỉ đọc | Nhóm "ghi vào cây" trong danh mục (§7.3); S1/S2 xác định thư mục ghi |
| Lợi ích chất lượng của codegraph/deepwiki/specmatic chưa được chứng minh | M5 đo trước khi đưa vào `grants` mặc định của mẫu cấu hình |
| Tên tool đổi (D6) làm hỏng quyền đã cấp trong settings của harness | `bint init --update` thêm quy tắc `mcp__bint__*` mới; ghi chú di chuyển |

## 10. Cần xác minh trước khi làm

| # | Câu hỏi | Ảnh hưởng |
|---|---|---|
| S1 | codegraph là bản nào; có dùng `roots/list` hay cwd; chỉ mục lưu ở đâu (trong repo hay ngoài) | chính sách chia sẻ, nhóm "ghi vào cây" |
| S2 | specmatic-mcp ghi báo cáo/tệp tạm ở đâu; `manage_mock_server` giữ cổng thế nào | nhóm `mock` độc quyền, bước chỉ đọc |
| S3 | Claude Code, Codex xử lý thế nào khi server stdio MCP chết giữa phiên, và khi gateway trả 404 cho session cũ | thiết kế shim, phương án thay thế của D6 |
| S4 | Codex 0.167 tắt `mcp_servers` toàn cục cho một lần `codex exec` bằng cách nào (`-c`, profile, `CODEX_HOME`) | M0 bước 2 |
| S5 | Có Go MCP SDK chính thức đủ dùng cho Streamable HTTP không, hay viết tay như `internal/mcpserver` | khối lượng M2 |

## 11. Thứ tự và phụ thuộc

`M0` (độc lập) → `M1` → `M2` → `M3` → `M4`; `M5` chạy sau M3.

**M5 — Đo:**
- Bộ nhớ: 4–6 phiên đồng thời, so PSS trước/sau (cấu hình MCP toàn cục hiện nay ↔ gateway).
- Chất lượng: nhánh eval có/không `grants` cho b-req/b-review trên tác vụ có đổi API (specmatic) và tác
  vụ cần định vị mã (codegraph); chỉ đưa `grants` vào mẫu mặc định khi có cải thiện.

Liên quan [isolation-subagent-plan.md](isolation-subagent-plan.md): G3 (subagent trong phiên) dùng chung
kết nối MCP của phiên cha, nên quyền theo role của subagent phải đặt ở định nghĩa subagent (danh sách tool
`mcp__bint__*`) chứ không qua env như §7.2.
