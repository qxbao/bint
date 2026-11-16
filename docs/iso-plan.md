# Kế hoạch: profile tách biệt cho harness (`bint iso`)

Trạng thái: **đã chốt toàn bộ với người dùng (2026-11-16), chưa triển khai**; bắt đầu từ GĐ0 (spike) khi người yêu cầu. Nguồn gốc: `ai-isolator` v0.4.4 bản đóng gói trong bflow
(`/mnt/01DCE9A4D6522BD0/Project/AI/AI-flow-SDLC/apps/cli/isolation/`, engine Bash 1390 dòng + `profile_sync.py` 1145 dòng +
`mcp_sync.py`; phía runtime TS: `packages/runtime/src/platform/isolator-profile.ts`, `packages/agent-providers/src/base-adapter.ts`).

Tên gọi: tính năng này là **profile tài khoản/cấu hình** của harness. Không dùng từ "isolation" trong khoá cấu hình vì
`workflow.isolation` đã có nghĩa là worktree (docs/isolation-subagent-plan.md). Gói Go: `internal/iso`; lệnh: `bint iso`.

---

## 1. Ai-isolator làm gì (tóm tắt để đối chiếu)

- Profile `<tool>--<name>` ở `${AI_ISOLATOR_HOME:-~/.local/share/ai-isolator}/profiles/`, mỗi profile có
  `home/ config/ cache/ data/ state/ tmp/` + `profile.env`, `codex-lb.mode|url`, `shell-link.*`, `.bflow-profile-sync.json`.
- Chạy CLI với `HOME=<p>/home`, `XDG_CONFIG_HOME=<p>/config`, `XDG_CACHE_HOME=<p>/cache`, `XDG_DATA_HOME=<p>/data`,
  `XDG_STATE_HOME=<p>/state`, `TMPDIR=<p>/tmp`, `CLAUDE_CONFIG_DIR=<p>/home/.claude`; gỡ ~17 biến credential
  (`OPENAI_API_KEY`, `ANTHROPIC_*`, `CODEX_HOME`, `GEMINI_API_KEY`…).
- Lệnh: `codex|claude|agy`, `run`, `login`, `status`, `sync-skills`, `remove`; Codex LB (dò theo 6 nguồn, kể cả `docker ps`,
  `pgrep` + `/proc/<pid>/environ`, chốt `linked|direct` một lần cho mỗi profile); hỏi để chèn alias vào RC của shell.
- `profile_sync.py`: đồng bộ MCP, skill, plugin quản lý, hook trust của Codex từ cấu hình gốc sang profile, có receipt 0600 và
  phát hiện xung đột. Cần Python 3.11+ (`tomllib`); không có thì profile **không chạy được**.
- Không phải sandbox: cùng UID, quyền tệp không đổi.

## 2. Báo cáo điều tra

### 2.1 Số đo trên máy (profile `codex--personal`, tạo 2026-10-05, đo 2026-11-16)

Tổng **6,9 GB**. Phần đáng chú ý:

| Thành phần | Dung lượng | Bản chất |
|---|---|---|
| `home/.codex/sessions` | 1,6 GB (1586 tệp, 858 tệp > 30 ngày) | lịch sử phiên Codex — dữ liệu thật, nhưng không có chính sách giữ |
| uv: `home/.cache/uv` + `cache/uv` + `home/.local/share/uv` | 964 MB + 463 MB + ~0 | **hai bản cache uv** trong cùng profile, chưa kể `~/.cache/uv` thật (345 MB) |
| `data/claude/versions` (2.1.296, 2.1.301, 2.1.312) | 781 MB | Claude tự cập nhật **riêng trong profile**; bản ở profile (2.1.312) **cũ hơn** bản thật (2.1.325) |
| `home/.codex/packages/app-server-daemon` | 432 MB | daemon Codex tải riêng theo `CODEX_HOME`; profile đang ở 0.162.0, máy thật 0.169.0 |
| `home/.gemini/antigravity-cli` | 455 MB | Agy chạy trong profile Codex |
| `home/Projects/youjo-senki-vi` | 446 MB | **dữ liệu người dùng** bị tạo trong HOME giả (agent `cd ~`) |
| `home/.npm` | 424 MB | npm không theo XDG |
| pnpm `data/pnpm` + `cache/pnpm` | 198 + 183 MB | store/cache pnpm riêng |
| node: `home/.cache/node` + `cache/node` | 114 + 115 MB | lại hai bản |
| `home/.codex/.tmp` (plugins, marketplaces) | 125 MB | Codex đồng bộ plugin theo từng `CODEX_HOME` |
| `home/.claude/projects` | 51 thư mục, **46 là repo tạm** (`…-tmp-bflow-e2e-…`) | test e2e chạy Claude trong repo tạm; repo đã xoá, lịch sử còn |
| `tmp/` | 2,6 MB | nhỏ, nhưng **không ai dọn** |

### 2.2 Nguyên nhân gốc

1. **TMPDIR nằm trong thư mục dữ liệu bền** (`~/.local/share/…/tmp`). `/tmp` thật được `systemd-tmpfiles-clean` (10 ngày) và
   khởi động lại dọn; thư mục profile thì không ai dọn. Trên Windows `%TEMP%` cũng chỉ Storage Sense dọn.
2. **Cache bị nhân bản và tách khỏi cache thật.** Mỗi profile có cache riêng (uv, npm, pnpm, node, go…), nên dung lượng nhân
   theo số profile và các công cụ dọn cache của người dùng (`uv cache prune`, `npm cache clean`, dọn `~/.cache`) không chạm tới.
3. **Hai bố cục XDG khác nhau cho cùng một profile.** Engine Bash đặt `XDG_CACHE_HOME=<p>/cache`, còn runtime TS ở chỗ khác đặt
   `<home>/.cache` (`base-adapter.ts:1939`, `service.ts:1082`). Thêm vào đó, tiến trình con chỉ giữ `HOME` mà bỏ biến XDG
   (Codex khởi chạy MCP stdio với danh sách biến tối thiểu — HOME, PATH, TMPDIR…; bằng chứng: `home/.cache/uv`,
   `home/.local/share/uv|pnpm|fastmcp|chrome-devtools-mcp` được ghi gần đây trong khi `cache/uv` dừng ở 2026-10-25) ⇒ mỗi công cụ
   rơi vào một trong hai chỗ ⇒ **hai bản cache trong cùng profile**. Gốc rễ: logic dựng env bị viết lại ở 3 nơi (Bash, TS
   runtime, TS adapter) và lệch nhau — chính bflow đã phải ra "ruling D-ROOT-02" vì ba nơi chọn gốc profile khác nhau.
4. **Bản cài tự cập nhật của harness bị nhân bản theo HOME.** Claude native installer ghi vào `$XDG_DATA_HOME/claude/versions`,
   Codex tải `app-server-daemon` vào `$CODEX_HOME/packages`; profile vừa tốn dung lượng vừa **chạy bản cũ** khác bản thật.
5. **Không có công cụ quan sát/dọn.** `status` không báo dung lượng; `remove` là cách duy nhất (xoá cả đăng nhập).

### 2.3 Vấn đề khác phát hiện được (ngoài temp/cache)

- **Trạng thái của chính công cụ bị chuyển hướng vào profile.** bflow có `state/bflow` (4,7 MB), `data/bflow` trong profile vì nó
  đọc XDG. Với bint, lỗi này **nghiêm trọng hơn**: `state.BaseDir()` (`internal/state/state.go:57`) dùng `XDG_STATE_HOME`, và
  `config.GlobalPath()` (`internal/config/config.go:664`) dùng `os.UserConfigDir()` ⇒ hook/MCP `bint` chạy trong profile sẽ mở
  **một state.sqlite khác** và mất `mcp.yml`/`secrets.env` của máy. Phải ghim `BINT_STATE_DIR`/`BINT_CONFIG_HOME` (đã có sẵn).
- **Daemon gateway MCP giữ env của client đầu** (đã ghi ở memory codegraph). Nếu client đầu chạy trong profile, daemon dùng chung
  sẽ mang `HOME`/`TMPDIR` của profile đó cho mọi phiên sau.
- **Skill bị chép (copy) nên cũ dần**: `sync-skills` phải chạy tay; plugin/skill ở profile lệch với bản gốc.
- **Phụ thuộc Python 3.11+** chỉ để đọc TOML; bint đã có `github.com/pelletier/go-toml/v2`.
- **Sửa RC của shell** (`~/.zshrc`) bằng khối đánh dấu, dò shell cha qua `ps` — rủi ro sửa tệp người dùng, không chạy trên Windows.
- **Dò Codex LB bằng `docker ps`, `pgrep`, đọc `/proc/*/environ`** — chậm, chỉ Linux, đọc env tiến trình khác.
- **Phiên nối tiếp gắn với HOME.** Transcript Claude (`~/.claude/projects`) và rollout Codex (`$CODEX_HOME/sessions`) nằm trong
  profile; bước workflow muốn resume (docs/step-continuation-plan.md) phải chạy lại **đúng profile**, nếu không sẽ báo mất phiên.
- **Trust/hook của Codex theo `CODEX_HOME`.** `codex.ProjectTrusted`, `HooksPath(user)` đọc `Home()`; profile mới không có
  `[projects.*] trust_level` ⇒ hook dự án của bint bị Codex bỏ qua trong profile.
- **Repo tạm của test vẫn để lại lịch sử** (46/51 mục `projects` của Claude) và lịch sử phiên không có chính sách giữ.

## 3. Mục tiêu và phi mục tiêu

Mục tiêu:

1. Port đủ chức năng dùng thật: tạo/đăng nhập/chạy/trạng thái/xoá profile cho `claude`, `codex`; Codex LB `linked|direct`.
   Agy (`antigravity`) **để đợt sau** (đã chốt): mã dựng sẵn bảng theo tool để thêm sau mà không đổi bố cục.
2. **Temp tự dọn**: không còn thư mục tạm bền trong profile; temp nằm ở temp hệ thống và bint dọn khi hết phiên.
3. **Cache không nhân bản**: cache gói (uv/npm/pnpm/pip/go/cargo/bun/playwright…) dùng chung với người dùng thật; phần riêng còn
   lại có GC theo tuổi; bản cài tự cập nhật của harness không bị tải lại trong profile.
4. **Một nguồn sự thật cho env** (`internal/iso`), dùng chung cho `bint iso run`, workflow, eval, gateway.
5. Bint trong profile vẫn dùng **đúng** state/config/secrets của máy.
6. Gắn profile vào workflow/eval (chọn profile theo harness) và vào handle phiên của bước.
7. Quan sát được: `bint iso status/du/gc`, cảnh báo dữ liệu người dùng nằm trong HOME giả.
8. Không cần Python; chạy Linux/macOS/Windows (cặp `*_unix.go`/`*_windows.go`, qua `GOOS=windows go vet`/staticcheck).

Phi mục tiêu:

- Không phải sandbox (giống ai-isolator; xem tri thức k:01M74PDT2ETJAYRWFV9ZPWVHMR). Không chặn mạng/tệp.
- Không cài harness, không chép/đọc token của tài khoản gốc sang profile (trừ lệnh `import` tường minh từ ai-isolator).
- Không tự sửa RC của shell.
- Không xoá store `~/.local/share/ai-isolator` của bflow.

## 4. Quyết định thiết kế (đề xuất — chờ người chốt ở mục 10)

| # | Quyết định | Lý do |
|---|---|---|
| D1 | Store mới `<UserDataDir>/bint/iso` (Linux `${XDG_DATA_HOME:-~/.local/share}/bint/iso`, macOS `~/Library/Application Support/bint/iso`, Windows `%LOCALAPPDATA%\bint\iso`); đè bằng `BINT_ISO_HOME`. Có `bint iso import --from-ai-isolator` | Dùng chung store với ai-isolator buộc giữ bố cục gây lỗi (`cache/`, `tmp/` riêng); import chỉ mang đăng nhập/cấu hình, bỏ 5+ GB rác |
| D2 | Profile chỉ có **`home/`** + `profile.json`; XDG trỏ vào vị trí chuẩn **bên trong HOME giả** (`home/.config`, `home/.local/share`, `home/.local/state`) | Tiến trình con bỏ biến XDG vẫn rơi đúng chỗ ⇒ hết hai bản cache |
| D3 | Cache: mặc định **`shared`** — `XDG_CACHE_HOME` = cache thật, `home/.cache` là symlink (Windows: junction) tới cache thật; các cache ngoài `.cache` (npm, pnpm store, go mod, cargo registry, uv data…) chia sẻ theo danh sách `share`. Chế độ `private` giữ cache trong profile và GC theo tuổi | Cache gói là dữ liệu dẫn xuất, không chứa tài khoản; profile vốn không phải ranh giới bảo mật (cùng UID) nên chia sẻ không làm yếu đi |
| D4 | Temp: `TMPDIR`/`TMP`/`TEMP` = `<os.TempDir()>/bint-iso-<uid>/<tool>--<name>`; khoá phiên `fslock` dùng chung (S); phiên cuối thoát thì xoá sạch; GC dọn thư mục mồ côi khi lấy được khoá X | `/tmp` thật vẫn được OS dọn; bint dọn ngay khi hết phiên; theo quy tắc k:01M7FGC944KVT6NY1TZE7MJ0P8 (không thu hồi theo PID) |
| D5 | Không để harness tự cập nhật trong profile: Claude chạy với `DISABLE_AUTOUPDATER=1` và binary tuyệt đối lấy từ PATH thật; Codex `packages/` là symlink tới `~/.codex/packages` thật (sau spike S2) | Hết 1,2 GB/profile và hết lệch phiên bản |
| D6 | Env ghim đường dẫn của bint: `BINT_STATE_DIR`, `BINT_CONFIG_HOME` = giá trị thật (chỉ đặt nếu chưa có), `BINT_ISO_REAL_HOME`, `BINT_ISO_PROFILE=<tool>/<name>` | Hook/MCP/`bint` lồng nhau dùng đúng state và tìm lại gốc thật |
| D7 | Skill/agent/command cấp người dùng: **symlink** (junction) tới bản gốc thay vì chép; MCP và trust Codex: đồng bộ khoá được quản lý bằng go-toml, có receipt | Không còn `sync-skills` tay; giữ được cấu hình riêng của profile |
| D8 | Lối tắt shell: `bint iso shell-init <bash|zsh|fish|pwsh>` in hàm/alias để người tự thêm `eval "$(bint iso shell-init zsh)"`; không sửa RC | Bỏ toàn bộ logic khối đánh dấu, chạy được trên Windows |
| D9 | Codex LB: `--lb URL` > `BINT_ISO_CODEX_LB_URL` > `base_url` …`/backend-api/codex` trong `~/.codex/config.toml` thật > `http://127.0.0.1:2455`; kiểm `GET /health/ready` bằng `net/http`. Bỏ dò docker/pgrep/`/proc` (đã chốt) | Đơn giản, đa nền tảng, không đọc env tiến trình khác; máy hiện tại tìm thấy LB qua nguồn 3 và 4 |
| D10 | Bint là tiến trình cha (không `exec` thay thế): giữ khoá S, chuyển tiếp tín hiệu, chờ con rồi dọn temp | Cần sống để dọn temp và nhả khoá |

## 5. Thiết kế chi tiết

### 5.1 Bố cục

```
<BINT_ISO_HOME>/
├── profiles/<tool>--<name>/
│   ├── profile.json          # {tool, name, created_at, lb:{mode,url}, version}; 0600
│   ├── sync-receipt.json     # khoá MCP/trust do bint quản lý + băm; 0600
│   ├── session.lock          # fslock: S = phiên đang chạy, X = gc/remove
│   └── home/                 # 0700
│       ├── .cache -> <cache thật>          (cache: shared)
│       ├── .npm   -> ~/.npm thật           (share: npm)
│       ├── .codex/{auth.json,config.toml,sessions/,skills -> …, packages -> ~/.codex/packages}
│       ├── .claude/{.credentials.json,settings.json,projects/,skills -> …,agents -> …,commands -> …}
│       ├── .config/  .local/share/  .local/state/
└── gc.stamp                  # lần GC tự động gần nhất
<os.TempDir()>/bint-iso-<uid>/<tool>--<name>/   # TMPDIR của profile (0700)
```

Tên profile: `^[A-Za-z0-9._-]{1,64}$`, không `.`/`..`. Mọi đường dẫn đều kiểm chứa trong store sau `EvalSymlinks`
(symlink do bint tạo là ngoại lệ có danh sách).

### 5.2 Env (một hàm duy nhất `iso.Env(p Profile, base []string) []string`)

Thứ tự: lấy `base` → gỡ danh sách credential/override (ghim bằng test, gồm danh sách của ai-isolator + `CLAUDE_CODE_OAUTH_TOKEN`,
`AWS_BEARER_TOKEN_BEDROCK`, `CLAUDE_CODE_USE_BEDROCK|VERTEX`, `XDG_*`, `TMPDIR/TMP/TEMP`, `CODEX_HOME`, `CLAUDE_CONFIG_DIR`,
`GEMINI_*`, `AGY_*`) → đặt:

| Biến | Giá trị |
|---|---|
| `HOME` (Windows thêm `USERPROFILE`, `APPDATA`=`home\AppData\Roaming`, `LOCALAPPDATA`=`home\AppData\Local`) | `home/` |
| `XDG_CONFIG_HOME` / `XDG_DATA_HOME` / `XDG_STATE_HOME` | `home/.config` / `home/.local/share` / `home/.local/state` |
| `XDG_CACHE_HOME` | cache thật (`shared`) hoặc `home/.cache` (`private`) |
| `TMPDIR` / `TMP` / `TEMP` | thư mục temp của profile (5.3) |
| `CODEX_HOME` / `CLAUDE_CONFIG_DIR` | `home/.codex` / `home/.claude` (theo tool) |
| `DISABLE_AUTOUPDATER` | `1` (claude) |
| `npm_config_cache`, `UV_CACHE_DIR`, `GOMODCACHE`, `GOCACHE`, `PIP_CACHE_DIR`… | theo `share` (bổ sung cho symlink; symlink mới là cơ chế chính vì tiến trình con có thể bỏ env) |
| `BINT_STATE_DIR`, `BINT_CONFIG_HOME` | giá trị thật tính **trước** khi đổi HOME |
| `BINT_ISO_REAL_HOME`, `BINT_ISO_HOME`, `BINT_ISO_PROFILE` | gốc thật, store, `<tool>/<name>` |
| `CODEX_LB_API_KEY` | chuyển tiếp nếu profile `linked` |

`iso.RealHome()`: `BINT_ISO_REAL_HOME` nếu đang trong profile, ngược lại `os.UserHomeDir()`. Mọi chỗ bint cần "home thật"
(state, config, gateway daemon) dùng hàm này. Danh sách `pass_env`/`drop_env` bổ sung qua cấu hình.

### 5.3 Vòng đời temp

1. `bint iso run`: mở `session.lock` khoá **S** (`fslock.Try` không chờ; lỗi thì chờ có giới hạn), `MkdirAll` temp 0700.
2. Chạy con; chuyển tiếp SIGINT/SIGTERM/SIGHUP (Unix: con cùng process group, cha bỏ qua SIGINT trong lúc chờ; Windows: Ctrl-C
   tự tới console group, cha chỉ chờ). Trả đúng mã thoát của con.
3. Con thoát: nhả S, thử **X** không chờ; được X ⇒ không còn phiên nào ⇒ `RemoveAll(temp)` rồi nhả X. Không được ⇒ phiên khác
   còn dùng, để nguyên.
4. GC (5.5) xử lý thư mục mồ côi khi bint bị kill -9: lấy được X thì xoá.
5. Tiến trình nền do agent sinh ra còn sống sau phiên (daemon Codex) có thể mất temp ⇒ spike S3; nếu daemon không tự tạo lại
   thì thêm ngưỡng tuổi (chỉ xoá mục cũ hơn `tmp_ttl`) thay cho xoá sạch.

Trên Linux temp vẫn nằm dưới `/tmp` nên `systemd-tmpfiles` là lưới an toàn thứ hai.

### 5.4 Chia sẻ cache và bản cài harness

- `share` mặc định: `cache` (toàn bộ `~/.cache` hoặc `%LOCALAPPDATA%` tương ứng), `npm`, `pnpm-store`, `uv-data`, `go-mod`,
  `cargo-registry`, `bun`. Mỗi mục là (đường dẫn trong HOME giả, đường dẫn thật, biến env tuỳ chọn), định nghĩa theo OS.
- Tạo liên kết lúc `create` và kiểm lại mỗi `run` (tự sửa nếu là thư mục rỗng; **không** ghi đè thư mục có dữ liệu — báo cho
  `bint iso gc --migrate-cache` gộp/xoá có xác nhận).
- Windows: symlink thư mục cần Developer Mode ⇒ dùng **junction** (`internal/iso/link_windows.go`, FSCTL_SET_REPARSE_POINT hoặc
  `cmd /c mklink /J`); không tạo được thì dùng env và báo `status`.
- Claude: binary lấy theo `exec.LookPath` trên PATH thật, bỏ các đường dẫn nằm trong store; `DISABLE_AUTOUPDATER=1`.
- Codex `packages/` symlink tới bản thật nếu S2 đạt; không đạt thì giữ riêng nhưng GC giữ mỗi loại một bản mới nhất.
- Cảnh báo (không chặn) khi chia sẻ: `cache` chung nghĩa là log/cache của harness ở `~/.cache/claude-cli-nodejs` trộn giữa tài khoản.

### 5.5 GC và quan sát

- `bint iso du [--json]`: theo profile, chia `auth/config`, `sessions`, `cache riêng`, `bản cài harness`, `temp`, `khác`;
  liệt kê thư mục lạ ở gốc HOME giả (vd `Projects/`) là **dữ liệu người dùng — không tự xoá**.
- `bint iso gc [--dry-run] [--profile P] [--deep]`:
  - temp mồ côi (khoá X lấy được) — luôn;
  - cache `private` cũ hơn `cache_ttl`;
  - bản cài harness thừa (`.local/share/claude/versions/*`, `.codex/packages/*/releases/*` trừ `current`) — `--deep`;
  - lịch sử phiên cũ hơn `sessions_ttl` (mặc định 90 ngày, đã chốt; chạy trong GC tự động) **trừ** phiên được tham chiếu bởi `LastStep`/handle của tác vụ còn mở;
  - mục `~/.claude/projects/<slug>` của thư mục không còn tồn tại (repo tạm) — `--deep`.
- GC tự động nhẹ (chỉ temp mồ côi) khi `bint iso run` nếu `gc.stamp` cũ hơn `gc_interval` (mặc định 6h).
- `bint doctor`/tab Hệ thống của TUI: dòng tóm tắt profile + tổng dung lượng + gợi ý `bint iso gc`.

### 5.6 Đồng bộ cấu hình (thay `profile_sync.py`)

`bint iso sync [--check] [--profile P|--all]`, chạy tự động khi `create` và (dạng `--check`, chỉ cảnh báo) khi `run`:

- Skill/agent/command người dùng: symlink `home/.claude/{skills,agents,commands}`, `home/.codex/skills`, `home/.agents/skills`
  tới bản thật nếu profile chưa có thư mục riêng; có thư mục riêng thì giữ và báo.
- MCP: chép bảng `[mcp_servers.*]` (Codex) / `mcpServers` (Claude, `~/.claude.json` mức user) từ cấu hình thật; khoá do bint
  quản lý ghi trong receipt kèm băm; khoá bị sửa tay ở profile ⇒ xung đột, không đè.
- Codex: chép `[projects.*] trust_level` cho repo hiện tại, gốc Git main và thư mục worktree của bint
  (khớp resolver ở k:01M7EEX78J3Q530NZF2DJDRQ3D); hook trust của `hooks.json`.
- Hook cấp user của bint (`bint init --hooks --scope user`): cài vào `home/.claude/settings.json`/`home/.codex/hooks.json` của
  profile bằng chính `InstallHooks` hiện có (đường dẫn tham số hoá thay cho `os.UserHomeDir()`).
- Không bao giờ chép `auth.json`, `.credentials.json`, `.claude.json` phần tài khoản.

### 5.7 Tích hợp workflow / eval / gateway

- Cấu hình: `harnesses.claude.profile`, `harnesses.codex.profile`, `harnesses.antigravity.profile` (chuỗi rỗng = không dùng
  profile). Nên đặt ở cấu hình toàn cục hoặc `local.yml` (tên profile là của máy, không commit). Biến `BINT_ISO_PROFILE` của
  phiên chủ **không** tự lan sang bước con trừ khi cấu hình chỉ định (tránh dùng nhầm tài khoản).
- `eval.AgentRequest` thêm `Profile`; `runProcess` dựng env bằng `iso.Env(profile, cleanEnv(req.Env...))`. `codexEvalProfile`
  lấy nguồn `auth.json` từ `CODEX_HOME` của profile (đã đúng vì đọc từ `baseEnv`).
- Handle phiên của bước (`task.StepStop`, docs/step-continuation-plan.md) lưu thêm `profile`; resume khác profile ⇒ chạy mới, ghi
  lý do. `claude.ConfigDir()`/`codex.HomeDir()` (tệp mới `session_path.go`, `rollout.go`) nhận thư mục tường minh thay cho đọc env.
- Gateway MCP: daemon dựng `BaseEnv` từ `iso.RealEnv()` (HOME/TMP thật) khi phát hiện `BINT_ISO_REAL_HOME`, để daemon dùng chung
  không giữ env của profile đầu tiên.
- `knowledge import claude-memory|codex-memory`: thêm `--profile P` (mặc định: profile của phiên nếu có, nếu không là gốc thật).
- `detect.Harnesses()`: dùng `iso.RealHome()` để báo cài đặt thật.

### 5.8 CLI

```
bint iso create <claude|codex> [--name N] [--lb URL|--no-lb]
bint iso login  <tool> [--name N]
bint iso run    <tool> [--name N] [--lb URL|--no-lb] [--] [ARGS...]
bint iso status [--json]            # đăng nhập, backend, liên kết cache, dung lượng tóm tắt
bint iso du     [--json]
bint iso gc     [--dry-run] [--deep] [--profile P]
bint iso sync   [--check] [--profile P|--all]
bint iso remove <tool> [--name N]   # chỉ người (ErrAgentForbidden khi InAgentSession)
bint iso import --from-ai-isolator [<tool>--<name>...] [--with-sessions]
bint iso shell-init <bash|zsh|fish|pwsh>
```

Tên mặc định `default`. `run` khi profile chưa có thì báo lệnh `create`, không tự tạo (khác ai-isolator: tránh tạo profile do gõ
sai). Lệnh mới cần hàng trong `cliRefs` và `litsrUsage` (k:01M78XP5NXWWRAZQHN5PSHNC13). `remove`/`gc --deep`/`import` là thao
tác của người: test phải gọi `asHuman(t)` (k:01M746PHJXDZN6QMA3BEVEDNPP).

### 5.9 Cấu hình mới

Theo k:01M746NPB0Y4D5S2D47EFTJ8T4 (struct + `Default()` + `Validate()` + `Render`, `%%` trong chú thích):

```yaml
iso:
  home: auto            # auto = <UserDataDir>/bint/iso
  cache: shared         # shared | private
  share: [cache, npm, pnpm-store, uv-data, go-mod, cargo-registry, bun]
  cache_ttl: 720h       # chỉ áp cho cache private
  sessions_ttl: 2160h   # 90 ngày; 0 = không dọn. Chỉ dọn phiên TRONG profile, bỏ qua phiên tác vụ còn mở tham chiếu
  gc_interval: 6h
  pass_env: []
harnesses:
  claude: {profile: ""}
  codex:  {profile: ""}
  # antigravity: {profile: ""}   # đợt sau (Agy hoãn)
```

## 6. Giai đoạn

Mỗi giai đoạn một phiếu b-do; GĐ5 chờ step-continuation (đang dở trong cây làm việc) vào main.

### GĐ0 — Spike (không mã sản phẩm, ghi kết quả vào mục 11)

- **S1** Codex/Claude khởi chạy MCP stdio và hook với env nào (xác nhận Codex bỏ XDG ⇒ D2 cần thiết).
- **S2** Hai `CODEX_HOME` dùng chung `packages/` qua symlink: `install.lock`, `current`, chạy song song hai profile.
- **S3** Xoá `TMPDIR` khi daemon/app-server Codex còn sống: tự tạo lại hay lỗi.
- **S4** `DISABLE_AUTOUPDATER=1` có chặn Claude native installer ghi `versions/` không; `claude` từ PATH thật chạy được với HOME giả.
- **S5** Windows: claude/codex đọc `USERPROFILE`/`APPDATA`/`LOCALAPPDATA` hay API shell folder; junction cho `.cache`/`.npm`.
- **S6** macOS: Claude lưu credential ở Keychain theo khoá nào khi đổi `CLAUDE_CONFIG_DIR` (ảnh hưởng `import` và cách ly).

### GĐ1 — Lõi `internal/iso`

Store/tên/đường dẫn, `profile.json`, `iso.Env`, `RealHome`/`RealEnv`, liên kết chia sẻ (`link_unix.go`/`link_windows.go`),
khoá phiên bằng `fslock`. Test bảng cho env (gỡ credential, ghim `BINT_STATE_DIR`/`BINT_CONFIG_HOME`, lồng profile, Windows).
**Xong khi**: `bint` chạy với env profile mở đúng `state.sqlite` thật; `GOOS=windows go vet` + staticcheck sạch.

### GĐ2 — CLI cơ bản

`create/login/run/status/remove/shell-init`, Codex LB (`linked|direct`, health probe), chuyển tiếp tín hiệu, mã thoát, temp
theo 5.3. Test bằng harness giả (script in env), không gọi tài khoản thật.
**Xong khi**: chạy hai phiên cùng profile, phiên cuối thoát thì temp biến mất; kill -9 cha để lại temp, `gc` dọn được.

### GĐ3 — Cache, bản cài harness, GC, quan sát

`share`, `cache: private`, `du`, `gc` (+`--deep`, `--dry-run`), GC tự động, cảnh báo dữ liệu người dùng trong HOME giả, dòng
`doctor`/TUI System. **Xong khi**: profile mới sau một phiên thật (uv/npx MCP) không tạo bản cache riêng nào; `du` khớp `du -sh`.

### GĐ4 — Đồng bộ cấu hình

`sync` (skill symlink, MCP, trust/hook Codex, hook user của bint) có receipt và `--check`. **Xong khi**: hook dự án của bint chạy
trong profile Codex mới mà không cần trust tay; sửa tay một MCP ở profile thì `sync` báo xung đột, không đè.

### GĐ5 — Tích hợp workflow/eval/gateway

`harnesses.<h>.profile`, `AgentRequest.Profile`, handle phiên có profile, gateway `RealEnv`, `knowledge import --profile`,
`detect`. **Xong khi**: `bint run` một tác vụ với Codex ở profile `work` — hook ghi vào state thật, chi phí đọc được, resume nối
tiếp đúng phiên; đổi profile giữa chừng thì chạy mới có ghi lý do.

### GĐ6 — Import từ ai-isolator, tài liệu, nghiệm thu

`import --from-ai-isolator` (auth, config, LB, tuỳ chọn sessions; bỏ cache/tmp/versions), `docs/usage.md`, `docs/configuration.md`,
tri thức (warning: XDG trong profile; ghim BINT_STATE_DIR). Thêm mã `NT-ISO-…` vào `docs/acceptance.md` mục 3 và gom vào vòng có
sẵn (phiên chạy tay + máy Windows), theo quy ước nghiệm thu một nguồn.

## 7. Kiểm thử

- Đơn vị: env (bảng), tên/chứa đường dẫn, liên kết (thư mục rỗng/có dữ liệu/symlink lạ), khoá S/X, receipt/xung đột TOML.
- Tích hợp: harness giả in env + tạo tệp vào `$TMPDIR`, `$XDG_CACHE_HOME`, `$HOME/.cache` ⇒ kiểm vị trí cuối cùng.
- Mọi test dùng `testutil.Isolate` + `BINT_ISO_HOME` tạm; không đụng store thật hay RC thật.
- Windows: test liên kết/junction có `//go:build windows`; temp dài trên Windows (MAX_PATH) — giữ tên ngắn.

## 8. Rủi ro

| Rủi ro | Giảm thiểu |
|---|---|
| Chia sẻ `~/.cache` trộn cache của công cụ dùng cache để giữ trạng thái đăng nhập | danh sách loại trừ (vd `gh`, trình duyệt headless); chế độ `private`; spike khi gặp |
| Xoá temp khi daemon con còn dùng | S3; khoá S; ngưỡng tuổi dự phòng |
| Claude/Codex đổi chỗ lưu (phiên bản mới) | `bint iso status` kiểm và báo; spike ghi phiên bản đã đo |
| Windows junction/USERPROFILE | S5; dự phòng env; nghiệm thu trên máy Windows |
| Dùng nhầm tài khoản ở bước workflow | profile ghi vào handle phiên và log bước; không lan `BINT_ISO_PROFILE` ngầm |

## 9. Ước lượng

GĐ0 nhỏ (đo tay); GĐ1 ~600 dòng + test; GĐ2 ~700; GĐ3 ~600; GĐ4 ~700; GĐ5 ~400 (chạm `eval`, `workflow`, `mcpgw`, `harness`);
GĐ6 ~300 + tài liệu.

## 10. Quyết định của người dùng (2026-11-16)

1. **Store**: store mới `<UserDataDir>/bint/iso` + `bint iso import --from-ai-isolator` (D1). Đã chốt.
2. **Cache**: `shared` toàn bộ `~/.cache` + danh sách `share` (D3). Đã chốt.
3. **Codex LB**: bỏ dò docker/pgrep/`/proc`, vẫn tích hợp LB đầy đủ. Đã chốt.
   - Tích hợp LB = truyền `-c model_provider=codex-lb -c model_providers.codex-lb.base_url=<URL>/backend-api/codex …`
     (cùng bộ cờ ai-isolator đang dùng) và chuyển tiếp `CODEX_LB_API_KEY`. Việc "dò" chỉ để **tìm URL lần đầu**; sau khi chốt
     `linked`, URL lưu trong `profile.json` và mọi lần chạy sau dùng URL đó, không dò nữa (LB chết thì dừng, không tự đổi).
   - Nguồn tìm URL giữ lại: (1) `--lb URL`, (2) `BINT_ISO_CODEX_LB_URL`, (3) `base_url` …`/backend-api/codex` trong
     `~/.codex/config.toml` thật, (4) `http://127.0.0.1:2455`; mỗi nguồn kiểm `GET /health/ready`.
   - Máy hiện tại (đo 2026-11-16): container `codex-lb` publish `2455`, `~/.codex/config.toml` có
     `[model_providers.codex-lb] base_url = "http://127.0.0.1:2455/backend-api/codex"`, profile cũ lưu `http://127.0.0.1:2455`,
     `/health/ready` trả `ok` ⇒ nguồn 3 và 4 đều tìm thấy; `import` mang luôn URL đã lưu.
   - Docker/pgrep chỉ có ích khi LB chạy ở **cổng khác 2455** mà không khai ở đâu cả; khi đó dùng `--lb URL` một lần.
4. **Lịch sử phiên**: dọn theo tuổi, `sessions_ttl` mặc định 90 ngày, chỉ trong profile, giữ phiên tác vụ còn mở. Đã chốt.
5. **Agy**: để đợt sau. Đã chốt.

## 11. Kết quả spike

(chưa chạy)
