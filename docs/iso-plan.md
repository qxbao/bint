# Kế hoạch: profile tách biệt cho harness (`bint iso`)

Trạng thái: **đã chốt toàn bộ với người dùng (2026-11-16); GĐ0 đã đo S1–S4 trên Linux (kết quả ở mục 11, đã sửa D4/D5/D6 và mục 5.2–5.6); S5/S6 chờ nghiệm thu trên Windows/macOS; GĐ1 (lõi `internal/iso`) đã viết mã — xem ghi chú GĐ1 ở 5.2 và 5.9; GĐ2 (CLI cơ bản `bint iso create|login|run|status|gc|remove|shell-init`, Codex LB, vòng đời temp) đã viết mã — xem ghi chú GĐ2 ở 5.3 và 5.8, kết quả đo khoá daemon ở 11.3; GĐ3 (cache/bản cài/`du`/`gc`/GC tự động/doctor) đã viết mã — xem ghi chú GĐ3 ở 5.4, 5.5 và 5.8, chưa nghiệm thu bằng phiên thật (uv/npx MCP) ở GĐ6; GĐ4 (`bint iso sync`: liên kết skill, MCP, trust/hook Codex, hook user, receipt, `--check`) đã viết mã — xem ghi chú GĐ4 ở 5.6 và 5.8, đã đo hook dự án trusted với Codex 0.169.0 thật; GĐ5 (tích hợp workflow/eval/gateway: `harnesses.<h>.profile`, `iso.Begin/End`, handle phiên có profile, gateway `RealEnv`, `knowledge import --profile`, `detect`) đã viết mã — xem ghi chú GĐ5 ở 5.7, chưa chạy Codex thật ở profile `work`; GĐ6 (`bint iso import --from-ai-isolator`, `docs/usage.md`, `docs/configuration.md`, tri thức, mã `NT-ISO-PROF-*` ở `docs/acceptance.md` mục 3) đã viết mã và tài liệu — xem ghi chú GĐ6 ở mục 6; nghiệm thu chạy thật gom vào V1/V2**. Nguồn gốc: `ai-isolator` v0.4.4 bản đóng gói trong bflow
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
| D4 | Temp: `TMPDIR`/`TMP`/`TEMP` = `<os.TempDir()>/bint-iso-<uid>/<tool>--<name>`; khoá phiên `fslock` dùng chung (S); phiên cuối thoát thì xoá sạch **nếu profile không còn daemon sống**; GC dọn thư mục mồ côi khi lấy được khoá X (S3: thư mục phải tồn tại trước mỗi lần spawn, Codex không tự tạo lại) | `/tmp` thật vẫn được OS dọn; bint dọn ngay khi hết phiên; theo quy tắc k:01M7FGC944KVT6NY1TZE7MJ0P8 (không thu hồi theo PID) |
| D5 | Không để harness tự cập nhật trong profile: Claude chạy với **`DISABLE_UPDATES=1`** (cùng `DISABLE_AUTOUPDATER=1`; chỉ `DISABLE_AUTOUPDATER` không chặn `claude update`, S4) và binary tuyệt đối lấy từ PATH thật; Codex `packages/` là symlink tới `~/.codex/packages` thật (S2 đạt khi bản cài đã có sẵn) | Hết 1,2 GB/profile và hết lệch phiên bản |
| D6 | Env ghim đường dẫn của bint: `BINT_STATE_DIR`, `BINT_CONFIG_HOME` = giá trị thật (chỉ đặt nếu chưa có), `BINT_ISO_REAL_HOME`, `BINT_ISO_PROFILE=<tool>/<name>`. **Với MCP stdio của Codex phải ghi vào bảng `env` của chính mục `[mcp_servers.*]` (S1: Codex chỉ chuyển `HOME`, `PATH`, `TMPDIR`)** | Hook/MCP/`bint` lồng nhau dùng đúng state và tìm lại gốc thật |
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
| `DISABLE_UPDATES` / `DISABLE_AUTOUPDATER` | `1` / `1` (claude; S4) |
| `npm_config_cache`, `UV_CACHE_DIR`, `GOMODCACHE`, `GOCACHE`, `PIP_CACHE_DIR`… | theo `share` (bổ sung cho symlink; symlink mới là cơ chế chính vì tiến trình con có thể bỏ env) |
| `BINT_STATE_DIR`, `BINT_CONFIG_HOME` | giá trị thật tính **trước** khi đổi HOME |
| `BINT_ISO_REAL_HOME`, `BINT_ISO_HOME`, `BINT_ISO_PROFILE` | gốc thật, store, `<tool>/<name>` |
| `CODEX_LB_API_KEY` | chuyển tiếp nếu profile `linked` |

`iso.RealHome()`: `BINT_ISO_REAL_HOME` nếu đang trong profile, ngược lại `os.UserHomeDir()`. Mọi chỗ bint cần "home thật"
(state, config, gateway daemon) dùng hàm này. Danh sách `pass_env`/`drop_env` bổ sung qua cấu hình.

**Ghi chú GĐ1 (đã cài đặt, khác chữ trong bảng trên):**

- Chữ ký thật là `iso.Env(p Profile, base []string) ([]string, error)` — trả thêm `error` vì phải tính state/config/home thật;
  không có home thì báo lỗi còn hơn để con rơi về state giả. `Profile` mang `Settings` (từ `config.Iso`) và `StoreRoot`.
- Bản thật của biến đường dẫn lưu **theo từng biến** `BINT_ISO_REAL_<TÊN>` (HOME, USERPROFILE, APPDATA, LOCALAPPDATA, XDG_*,
  TMPDIR/TMP/TEMP, cùng CODEX_HOME, CLAUDE_CONFIG_DIR, DISABLE_UPDATES/DISABLE_AUTOUPDATER và biến cache của `share`), không chỉ
  `BINT_ISO_REAL_HOME`: gateway (5.7) cần `RealEnv` trả cả `XDG_*`/`TMPDIR` tuỳ biến của người dùng. Biến thật không đặt thì không
  có bản REAL và `RealEnv` gỡ biến đó. Credential đã bị gỡ ở `Env` **không** khôi phục lại ở `RealEnv`.
- Tính `BINT_STATE_DIR`/`BINT_CONFIG_HOME` dùng chung logic với bint: `state.BaseDirFrom(lookup)` và `config.GlobalPathFrom(lookup)`
  (`BaseDir()`/`GlobalPath()` gọi lại chúng) — không chép logic sang `iso` (chính là lỗi gốc rễ #3 của mục 2.2).
  `BINT_CONFIG_HOME` ghim là thư mục chứa `config.yml` toàn cục; cả hai luôn **tuyệt đối**.
- `XDG_CACHE_HOME` = cache thật chỉ khi `cache: shared` **và** `cache` có trong `iso.share`; ngược lại `home/.cache`.
  Biến cache của từng mục `share` chỉ đặt khi mục có hiệu lực; biến cache của máy thật luôn bị gỡ trước (private không lọt cache chung).
- Danh mục `share` ở Windows/macOS là bản tốt nhất có thể (Windows: nhiều liên kết cho `cache` gồm `AppData\Local\{uv\cache,pip\Cache,go-build}`),
  chưa kiểm trên máy thật: thuộc S5/S6.
- `os.Lstat` trên Windows không còn đặt `ModeSymlink` cho junction (Go 1.23+) nên `link_windows.go` nhận diện bằng reparse tag.

### 5.3 Vòng đời temp

1. `bint iso run`: mở `session.lock` khoá **S** (`fslock.Try` không chờ; lỗi thì chờ có giới hạn), `MkdirAll` temp 0700.
2. Chạy con; chuyển tiếp SIGINT/SIGTERM/SIGHUP (Unix: con cùng process group, cha bỏ qua SIGINT trong lúc chờ; Windows: Ctrl-C
   tự tới console group, cha chỉ chờ). Trả đúng mã thoát của con.
3. Con thoát: nhả S, thử **X** không chờ; được X ⇒ không còn phiên nào ⇒ nếu profile không có daemon sống thì
   `RemoveAll(temp)` rồi nhả X. Không được X ⇒ phiên khác còn dùng, để nguyên.
4. GC (5.5) xử lý thư mục mồ côi khi bint bị kill -9: lấy được X (và không có daemon sống) thì xoá.
5. **Kết quả S3:** Codex không tự tạo lại `TMPDIR` đã bị xoá; lệnh shell kế tiếp trong sandbox panic
   (`failed to resolve synthetic mount registry temp directory`). Daemon app-server rảnh vẫn sống nhưng không tạo lại thư mục.
   Vì vậy: (a) `MkdirAll(temp)` mỗi lần `run` là bắt buộc; (b) "daemon sống" = `app-server-daemon/daemon.pid` của profile
   trỏ tới tiến trình còn sống (Linux: kèm `startTicks` khớp `/proc/<pid>/stat`) — **không** dựa vào khoá `daemon.pid.lock`,
   vì daemon rảnh không giữ khoá nào (đo ở 11.3) ⇒ **không xoá thư mục `TMPDIR`**, chỉ xoá mục con cũ hơn `tmp_ttl`
   (mặc định 24h), **bỏ qua mục `codex-*`** (sandbox bwrap của Codex, vd `codex-bwrap-synthetic-mount-targets-<uid>`);
   (c) không xoá được dưới khoá X khi có phiên khác — đã đúng với bước 3.

**Ghi chú GĐ2 (đã cài đặt):**

- `iso.Run` là bước 1–3 của danh sách trên: tìm binary → khoá S (chờ có giới hạn, mặc định 10 s, khi `gc`/`remove` đang giữ X) →
  `Env` → tạo `TMPDIR` lấy từ **chính env đã dựng** (một nguồn, sửa review GĐ1 .9) → kiểm liên kết chia sẻ (xung đột chỉ cảnh báo)
  → spawn **không** gắn `ctx` (Ctrl-C ở `main` không được giết con qua bint) → nhả S rồi `finishSession`. Unix: `SIGTERM`/`SIGHUP`
  tới bint được chuyển cho con (tín hiệu tới trước khi con chạy được giữ lại rồi chuyển), `SIGINT` bị nuốt (con cùng process group
  nhận Ctrl-C trực tiếp từ terminal); con chết vì tín hiệu n ⇒ mã thoát 128+n. Windows: bint chỉ nuốt `os.Interrupt` và chờ.
- Mã thoát của con đi qua `exitError{quiet}`: `Execute` không in "lỗi:" cho nó. Mã 2 = cú pháp/tên sai, 69 = Codex LB không sẵn sàng.
- `daemonAlive` theo 11.3 (`daemon.pid`, không theo khoá). Sai lệch cố ý theo hướng an toàn: không đọc/parse được ⇒ coi là còn sống.
- `CollectTemp` (cho `bint iso gc [--dry-run] [--profile P]`): profile đang có khoá S ⇒ bỏ qua; có X ⇒ xoá cả thư mục hoặc (còn
  daemon) chỉ mục cũ. Thư mục `bint-iso-<uid>/<tool>--<name>` mà store hiện tại không có profile đó chỉ bị xoá khi cũ hơn `tmp_ttl`
  (thư mục cha dùng chung cho mọi store của người dùng nên một store không được xoá temp phiên đang chạy của store khác).
  `gc` ở GĐ2 chỉ dọn temp; cache/phiên/`--deep`/GC tự động đã thêm ở GĐ3 (xem 5.5).
- `remove` từ chối khi có phiên (`ErrBusy`) hoặc còn daemon Codex (`ErrDaemonAlive`); tự duyệt cây bằng `Lstat` và gỡ liên kết
  mà không đi vào (junction Windows), giữ `session.lock` tới cuối vì Windows không xoá được tệp đang mở.

Trên Linux temp vẫn nằm dưới `/tmp` nên `systemd-tmpfiles` là lưới an toàn thứ hai.

### 5.4 Chia sẻ cache và bản cài harness

- `share` mặc định: `cache` (toàn bộ `~/.cache` hoặc `%LOCALAPPDATA%` tương ứng), `npm`, `pnpm-store`, `uv-data`, `go-mod`,
  `cargo-registry`, `bun`. Mỗi mục là (đường dẫn trong HOME giả, đường dẫn thật, biến env tuỳ chọn), định nghĩa theo OS.
- Tạo liên kết lúc `create` và kiểm lại mỗi `run` (tự sửa nếu là thư mục rỗng; **không** ghi đè thư mục có dữ liệu — báo cho
  `bint iso gc --migrate-cache` gộp/xoá có xác nhận).
- Windows: symlink thư mục cần Developer Mode ⇒ dùng **junction** (`internal/iso/link_windows.go`, FSCTL_SET_REPARSE_POINT hoặc
  `cmd /c mklink /J`); không tạo được thì dùng env và báo `status`.
- Claude: binary lấy theo `exec.LookPath` trên PATH thật, bỏ các đường dẫn nằm trong store; `DISABLE_UPDATES=1` +
  `DISABLE_AUTOUPDATER=1`. Bint **không bao giờ** chạy `claude update`/`claude install` trong profile (S4: lệnh đó chép
  349 MB vào `home/.local/share/claude/versions` và `home/.local/bin/claude` dù có `DISABLE_AUTOUPDATER`).
- Codex `packages/` symlink tới bản thật (S2 đạt): `create` chỉ tạo liên kết khi `~/.codex/packages/app-server-daemon/current`
  tồn tại; nếu không thì để trống và `status` cảnh báo (S2: thư mục `packages` trống ⇒ `daemon start` tải lại daemon ~430 MB
  vào `releases/.staging.*`). `codex exec`/TUI không tự tạo `packages/`; chỉ `codex app-server daemon …` mới tạo.
  Phần không đạt được (race cập nhật song song giữa daemon thật và daemon profile) vẫn mở, xem mục 11.
- Cảnh báo (không chặn) khi chia sẻ: `cache` chung nghĩa là log/cache của harness ở `~/.cache/claude-cli-nodejs` trộn giữa tài khoản.

**Ghi chú GĐ3 (đã cài đặt, mục 5.4):**

- `ensureShares` duyệt **toàn bộ** danh mục: mục có hiệu lực → `EnsureLink` như cũ; mục không còn hiệu lực (cache `private` hoặc bị bỏ
  khỏi `iso.share`) mà ở HOME giả là liên kết trỏ **đúng đích của danh mục** → gỡ liên kết (`LinkRemoved`, dữ liệu ở đích còn nguyên).
  Liên kết trỏ chỗ khác, thư mục có dữ liệu hoặc thư mục rỗng giữ nguyên. Cache `private` thì `home/.cache` được tạo (0700) để
  `XDG_CACHE_HOME` có chỗ ghi. `status` (`CheckShares`) chỉ đọc và báo `removed` cho liên kết mà `run` sẽ gỡ.
- Hai điều kiện "xong" của GĐ3 theo danh mục có sẵn: uv cache `~/.cache/uv` (share `cache`), npx `~/.npm/_npx` (`npm`), uv tool
  `~/.local/share/uv/tools` (`uv-data`); test tích hợp dùng harness giả chạy `env -i HOME PATH TMPDIR` (đúng cách MCP stdio của Codex
  bỏ env) và kiểm `du --json` báo cache riêng 0 byte.
- Codex `packages`: `create` và mỗi `run` gọi `EnsureInstall` — liên kết `home/.codex/packages` → `<CODEX_HOME thật>/packages` **chỉ khi**
  `app-server-daemon/current` có ở đó; không có thì không tạo gì và `status` in `! <id>: bản cài: …` (daemon sẽ tải lại ~430 MB). Thư mục
  `packages` có dữ liệu giữ nguyên (xung đột báo qua cảnh báo như liên kết cache). Claude không có gì để chia sẻ (S4).
- `bint iso gc --migrate-cache` (gộp cache riêng cũ vào cache thật) **chưa làm**; thư mục cache có dữ liệu vẫn chỉ được báo, không ghi đè.

### 5.5 GC và quan sát

- `bint iso du [--json]`: theo profile, chia `auth/config`, `sessions`, `cache riêng`, `bản cài harness`, `temp`, `khác`;
  liệt kê thư mục lạ ở gốc HOME giả (vd `Projects/`) là **dữ liệu người dùng — không tự xoá**.
- `bint iso gc [--dry-run] [--profile P] [--deep]`:
  - temp mồ côi (khoá X lấy được) — luôn;
  - cache `private` cũ hơn `cache_ttl`;
  - bản cài harness thừa (`.local/share/claude/versions/*`, `.codex/packages/*/releases/*` trừ `current`) — `--deep`;
  - lịch sử phiên cũ hơn `sessions_ttl` (mặc định 90 ngày, đã chốt; chạy trong GC tự động) **trừ** phiên được tham chiếu bởi `LastStep`/handle của tác vụ còn mở;
  - mục `~/.claude/projects/<slug>` của thư mục không còn tồn tại (repo tạm) — `--deep`.
- GC tự động khi phiên `bint iso run` kết thúc nếu `gc.stamp` cũ hơn `gc_interval` (mặc định 6h; 0 = tắt): chạy `gc` **không `--deep`** (temp, cache riêng quá hạn, phiên quá hạn) rồi ghi lại `gc.stamp`.
- `bint doctor`/tab Hệ thống của TUI: dòng tóm tắt profile + tổng dung lượng + gợi ý `bint iso gc`.

**Ghi chú GĐ3 (đã cài đặt, mục 5.5):**

- Mọi logic nằm ở `internal/iso` (`du.go`, `gc.go`, `sessions_keep.go`); CLI chỉ phân tích cờ và in; `internal/app/doctor_iso.go` gọi lõi.
- **Quyết định gỡ mâu thuẫn**: dòng "GC tự động nhẹ (chỉ temp)" ngược với `sessions_ttl` "chạy trong GC tự động"; chọn GC tự động = mọi bước không
  `--deep` (quyết định 10.4 đã chốt dọn phiên theo tuổi). `--deep` không bao giờ tự chạy, và `gc --deep` trả `ErrAgentForbidden` trong phiên agent.
  GC tự động chạy **sau** khi nhả khoá S (profile vừa dùng cũng được dọn, không làm chậm lúc khởi động harness); lỗi GC chỉ là cảnh báo,
  mã thoát vẫn là của harness; hai `run` cùng lúc chạy GC trùng là vô hại (khoá X từng profile).
- `du`: số khối × 512 như `du` (Linux/macOS), hardlink đếm một lần, **không đi qua liên kết** (cache chia sẻ không bị tính, bản thân liên kết
  không phải "cache riêng"). Tổng thư mục profile và temp (KiB, làm tròn lên) bằng đúng `du -sk` (test bỏ qua khi không có `du`). Windows: kích
  thước tệp, không khử hardlink (không so với `du` được; mã tách `du_unix.go`/`du_windows.go`). Nhóm: `auth_config`, `sessions`, `cache`
  (InHome của danh mục khi là thư mục thật), `install`, `temp`, `other`, `user_data`; bảng tiền tố `usageGroups` dùng chung cho `du` và GC.
- **Dữ liệu người dùng** = mục ở gốc HOME giả không bắt đầu bằng "." và không thuộc danh mục đã biết (`go`, `Library`, `AppData`, thành phần đầu của
  mọi InHome), vd `Projects/`. `du`, `status`, doctor báo (`!`); GC không bao giờ chạm.
- `gc`: mỗi profile lấy khoá X không chờ (đang có phiên ⇒ bỏ qua cả profile; còn daemon Codex ⇒ bỏ qua phiên và bản cài, cache vẫn dọn).
  Tệp cache riêng cũ hơn `cache_ttl` (mọi InHome của danh mục là thư mục thật, trừ `uv-data`: tool/Python đã cài không phải cache); tệp phiên cũ hơn `sessions_ttl` (Codex `sessions|archived_sessions/**/rollout-*.jsonl`, Claude
  `projects/*/<uuid>.jsonl` kèm `<uuid>/`; `sessions_ttl: 0` ⇒ không xoá tệp phiên nào, kể cả của `--deep`) trừ phiên là `LastStep.session_id` của tác vụ chưa
  kết thúc. `--deep` thêm `claude/versions/*` (trừ bản `bin/claude` trỏ tới), `codex packages/*/releases/*` (trừ đích `current`; chỉ khi `packages` không
  phải liên kết) và `.claude/projects/<slug>` mà thư mục nguồn (đọc từ `cwd` trong `.jsonl`) không còn — chỉ khi thư mục cha của nguồn còn (tránh nhầm ổ
  chưa gắn). Không đọc được/không chắc ⇒ giữ. `--dry-run` đi cùng một đường mã, chỉ bỏ bước xoá; mỗi hành động một dòng (`--json` đủ danh sách,
  bản in giới hạn 200 dòng mỗi nhóm).
- Giới hạn: "tác vụ chưa kết thúc" chỉ thấy tác vụ trong `<state thật>/*/runtime/tasks`; repo đặt `state.dir` tuỳ biến ngoài thư mục mặc định không được thấy
  (GĐ5 thu hẹp theo profile). Không đọc được một tác vụ ⇒ giữ mọi phiên. Codex `state_*.sqlite` vẫn liệt kê thread có rollout đã xoá (chưa đo Codex xử lý ra
  sao; chỉ xoá phiên quá 90 ngày) — việc mở cho nghiệm thu GĐ6.
- doctor: dòng `iso` (info; **warn** khi có dữ liệu người dùng trong HOME giả hoặc liên kết chia sẻ xung đột; không thêm dòng khi chưa có profile) hiện ở thẻ
  Doctor của tab Hệ thống; chưa có màn TUI riêng cho `iso …` (`actions.Data.Excluded`).

### 5.6 Đồng bộ cấu hình (thay `profile_sync.py`)

`bint iso sync [--check] [--profile P|--all]`, chạy tự động khi `create` và (dạng `--check`, chỉ cảnh báo) khi `run`:

- Skill/agent/command người dùng: symlink `home/.claude/{skills,agents,commands}`, `home/.codex/skills`, `home/.agents/skills`
  tới bản thật nếu profile chưa có thư mục riêng; có thư mục riêng thì giữ và báo.
- MCP: chép bảng `[mcp_servers.*]` (Codex) / `mcpServers` (Claude, `~/.claude.json` mức user) từ cấu hình thật; khoá do bint
  quản lý ghi trong receipt kèm băm; khoá bị sửa tay ở profile ⇒ xung đột, không đè.
  **Mục `[mcp_servers.bint]` (và mọi MCP stdio của Codex cần state thật) phải mang bảng `env = { BINT_STATE_DIR=…,
  BINT_CONFIG_HOME=… }`** (S1: Codex chỉ chuyển `HOME`, `PATH`, `TMPDIR` cho MCP stdio, bảng `env` và `env_vars` đều tới
  được tiến trình MCP). Không có bảng này `bint mcp` rơi về `$HOME/.local/state` giả và mở `state.sqlite` khác. Claude
  chuyển nguyên env cho MCP/hook nên không cần.
- Codex: chép `[projects.*] trust_level` cho repo hiện tại, gốc Git main và thư mục worktree của bint
  (khớp resolver ở k:01M7EEX78J3Q530NZF2DJDRQ3D); hook trust của `hooks.json`. **Hook chỉ chạy khi có
  `[hooks.state."<đường dẫn tuyệt đối hooks.json>:<sự_kiện>:0:0"] trusted_hash` khớp `currentHash` do app-server `hooks/list`
  trả (S1: thiếu thì hook im lặng, không báo lỗi)**; khoá chứa đường dẫn tuyệt đối trong profile nên `sync` phải tính lại khi
  `hooks.json` hoặc vị trí store đổi (tái dùng cách gọi của `codex.InspectHookTrust`).
- Hook cấp user của bint (`bint init --hooks --scope user`): cài vào `home/.claude/settings.json`/`home/.codex/hooks.json` của
  profile bằng chính `InstallHooks` hiện có (đường dẫn tham số hoá thay cho `os.UserHomeDir()`).
- Không bao giờ chép `auth.json`, `.credentials.json`, `.claude.json` phần tài khoản.

**Ghi chú GĐ4 (đã cài đặt, mục 5.6; khác chữ trong khối trên):**

- Lõi trung lập nằm ở `internal/iso` (`Sync`, `Reconcile`, receipt, backend TOML/JSON, liên kết); phần cần harness
  (đọc cấu hình thật, `InstallHooks`, `hooks/list`) nằm ở `internal/app/iso_sync.go` và truyền vào qua `iso.SyncSource`,
  để `iso` không import `internal/harness` (GĐ5 cho `detect` dùng `iso.RealHome`, tránh vòng import).
- **Round-trip go-toml thay cho khối đánh dấu** (`fsafe.BlockSync`): Codex tự ghi `[projects.*]`/`[hooks.state.*]` vào cùng tệp
  khi người dùng trust qua `/hooks`, nên khối đánh dấu dễ bị sửa tại chỗ. Đổi lại `home/.codex/config.toml` của profile mất
  chú thích và thứ tự khoá (khoá lạ giữ nguyên giá trị); tệp thật không bị chạm. Mọi lần ghi sao lưu vào
  `<profile>/sync-backups/<thời điểm>/…`, chỉ ghi tệp khi có khoá đổi.
- **Receipt** `sync-receipt.json` (0600): mỗi khoá quản lý có `kind` (`mcp`, `trust`, `hook`, `hook-state`), tệp, khoá, sha256 của
  JSON chuẩn của giá trị. Máy trạng thái mỗi khoá (D = bản thật, C = profile, R = receipt): C vắng → ghi; C = D → khớp;
  C có, R vắng, C ≠ D → xung đột (mục riêng); băm(C) = R, D đổi → cập nhật; băm(C) ≠ R và C ≠ D → xung đột (sửa tay);
  D vắng, băm(C) = R → gỡ; D vắng, băm(C) ≠ R → giữ và báo. Xung đột không bao giờ ghi, tệp giữ nguyên từng byte. Receipt hỏng
  coi như rỗng (khoá khác bản thật thành xung đột, không bị đè). Khoá của một nhóm không tính được lần này (repo không xác
  định, hooks.json thật hỏng) bị bỏ qua chứ không bị gỡ.
- Đích: `home/.codex/config.toml` (mcp_servers, projects, hooks.state), `home/.claude/.claude.json` (chỉ `mcpServers`; nguồn
  `$CLAUDE_CONFIG_DIR/.claude.json` thật hoặc `~/.claude.json`), `home/.claude/settings.json` và `home/.codex/hooks.json`
  (hook user, nguồn `~/.claude/settings.json` và `CODEX_HOME thật/hooks.json`). `auth.json`, `.credentials.json` và mọi khoá
  tài khoản của `.claude.json` không bao giờ được đọc ra, có test khẳng định.
- **Trust Codex chỉ nhân bản quyết định thật.** `[projects.*]`: gốc repo của thư mục hiện tại, gốc Git main, và khoá nằm
  dưới thư mục worktree của bint; `[hooks.state.*]`: khoá có đường dẫn hooks.json nằm dưới các thư mục đó (chép nguyên
  `trusted_hash`, đúng vì đường dẫn tuyệt đối giống nhau giữa máy thật và profile; hook sửa sau đó vẫn là `modified`).
  Profile là toàn máy nên trust của repo khác còn ở bản thật (`SyncSource.Retain`) được giữ nguyên trong profile: sync ở repo B
  không gỡ trust của repo A; chỉ khi bản thật bỏ khoá thì sync mới gỡ (`TestIsoSyncTrustSurvivesSwitchingRepos`). Hook
  **user** nằm trong HOME giả nên khoá khác: sau khi ghi, sync chạy `hooks/list` với env của profile và chỉ chép `trusted_hash`
  của bản thật (cùng sự kiện) khi bằng `currentHash` của profile; không khớp thì báo cần trust tay. Đo thật với Codex 0.169.0
  (`BINT_CODEX_CONFIG_LIVE=1`, `TestIsoSyncCodexProjectHooksTrustedLive`): profile mới sync xong có 4/4 hook dự án và 4/4 hook
  user `trusted` không trust tay, profile chưa sync thì chưa; `currentHash` tính trên định nghĩa hook nên không đổi theo
  đường dẫn hooks.json. Bint không tự tính hash để trust hook mà người dùng chưa trust ở máy thật.
- `sync --check` và kiểm tĩnh của `run` **không chạy Codex** (app-server tạo tệp sqlite trong `CODEX_HOME`): không xác minh
  được trust hook user và không báo hook dự án chưa trusted; việc đó chỉ có khi ghi (`create`, `bint iso sync`). Ghi chạy
  `hooks/list` chỉ khi có repo hoặc hook user ở bản thật.
- MCP stdio có lệnh `bint`/`bint.exe` nhận bảng `env` với `BINT_STATE_DIR`/`BINT_CONFIG_HOME` tuyệt đối của máy thật (giữ giá trị
  sẵn có trong bản thật); MCP URL và lệnh khác giữ nguyên. Claude không cần (chuyển nguyên env).
- Giới hạn đã biết: MCP khai ở `.codex/config.toml` cấp dự án không bị sửa (chưa đo việc Codex gộp sâu bảng `env` của lớp user
  vào mục cùng tên của lớp dự án); trust hook trong từng worktree của bint (mỗi worktree có khoá riêng) chưa xử lý; `home/.codex/skills`
  là liên kết tới bản thật nên Codex trong profile có thể ghi skill `.system` vào bản thật; khoá `hooks.state` trên Windows
  (`/` hay `\`) chưa đo (S5), so đường dẫn sau `state.NormalizePath`. Không đo được ở GĐ4: hook chạy thật trong phiên Codex có mô hình,
  macOS/Windows (GĐ6).

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

**Ghi chú GĐ5 (đã cài đặt, khác chữ trong danh sách trên):**

- **Agent mang profile, request chỉ ghi đè.** `harness.NewAgent` giải `harnesses.<h>.profile` (`iso.ResolveProfile`: mở store rồi nạp;
  chưa có thì lỗi nêu `bint iso create <tool> --name <tên>`, bước thất bại thay vì âm thầm dùng tài khoản thật) và đặt vào
  `eval.ClaudeAgent.Profile`/`eval.CodexAgent.Profile`. Mọi nơi dựng agent qua `NewAgent` (bint run, bint eval, evolve, skill test) hưởng
  profile mà không phải tự giải. `AgentRequest.Profile` ghi đè profile của agent; giao diện `eval.ProfileAgent` +
  `eval.EffectiveProfile` cho workflow đọc profile hiệu lực. Antigravity chưa có profile (`Validate` bắt `harnesses.antigravity.profile`
  rỗng).
- **Vòng đời phiên dùng chung với `bint iso run`.** `iso.Run` tách thành `iso.Begin(p, base, logf)` → `*Session{Env, Temp}` và
  `Session.End()` (nhả khoá S, dọn temp khi là phiên cuối, GC tự động; idempotent). `eval.agentEnv` gọi `Begin` cho mọi lượt Claude/Codex
  có profile: khoá S giữ suốt bước (nên `iso gc`/`remove` không động tới profile đang chạy), TMPDIR có trước khi spawn, dọn khi xong.
  Không có profile mà env của bint đang ở trong profile (`BINT_ISO_PROFILE`) thì env bước là `iso.RealEnv` cho phần của tiến trình
  (rồi mới nối `req.Env`): **khác chữ 5.7 ở chỗ này** — cleanEnv trước đây kế thừa nguyên env, nay không lan HOME/CODEX_HOME giả của phiên
  chủ sang bước con. Credential mà `iso.Env` đã gỡ không khôi phục được ở `RealEnv`; đăng nhập dạng tệp (auth.json, .credentials.json
  trong HOME thật) vẫn dùng được.
- **Đối số Codex trong profile.** Bảng MCP của bint nhận `-c mcp_servers.bint.env={BINT_CONFIG_HOME=…,BINT_STATE_DIR=…}` (giá trị thật,
  tuyệt đối, `iso.BintMCPEnv`) ngay sau `mcp_servers.bint.args`: bảng con nên không thay bảng `mcp_servers` (k:01M77JFNA33PQ99TVS9A386HFG).
  Profile `linked`: `iso.HarnessArgs` = `ProbeLB` + `CodexLBArgs`, đặt ở cấp `exec` trước `ExtraArgs` và trước `resume`; LB không sẵn sàng
  thì lượt dừng với `Infra` (lớp `external`, nối tiếp được) và không spawn harness. `bint iso run` dùng lại `HarnessArgs`.
  `codexOtherMCPServers` đọc `CODEX_HOME` (và HOME) của env đã dựng, không phải của tiến trình bint; `codexEvalProfile` lấy home của user
  skill từ `HOME` của env nền và không đòi `auth.json` khi profile linked.
- **Handle phiên.** `task.StepStop.Profile` và `workflow.StepResult.Profile` (`json:"profile,omitempty"`, dạng `codex/work`, rỗng khi không
  dùng profile; subagent luôn rỗng). Kiểm phiên còn và usage nối tiếp đọc rollout/transcript trong `prof.ConfigDir()` (`stepConfigDir`),
  không có profile thì trong `iso.RealCodexHome`/`RealClaudeDir` (khớp env thật của bước). `claude.SessionTranscriptPath` và
  `codex.RolloutPath` đã nhận thư mục tường minh từ trước nên không cần tệp `session_path.go`/`rollout.go` mới.
- **Đổi profile giữa hai lượt** (`ContinueFacts.Profile`, kiểm ngay sau `Via`): bước chạy mới với đề bài đầy đủ, notes nêu cả profile cũ lẫn
  mới ("không nối tiếp phiên cũ: bước dừng trước chạy ở profile codex/work, bước sắp mở chạy không có profile…"); không chụp fingerprint
  khi khác profile. Phiếu cũ (trước GĐ5) không có `profile` nên lần đầu cấu hình profile cũng chạy mới một lần.
- **Gateway.** Shim (`bint mcp serve`) và `bint mcp gateway start` khởi động daemon bằng `iso.RealEnv(os.Environ())` khi đang trong profile
  (ngoài profile: `nil`, kế thừa như trước); `mcpgw.Env` lấy `BaseEnv` (HOME, TMPDIR, PATH…) từ env thật nên upstream dùng chung không giữ
  HOME/TMP của profile đầu tiên kể cả khi daemon cũ đã khởi động trong profile. Daemon cũ còn env profile tự khởi động lại khi binary mới
  (ExeModTime) hoặc `bint mcp gateway stop`.
- **`knowledge import --profile`.** `ImportOptions.Profile` (`<tool>/<tên>` hoặc `<tên>`; tool theo nguồn); chỉ `claude-memory`/
  `codex-memory`; đọc `HOME` giả và thư mục cấu hình của profile. Không cờ: profile của phiên khi cùng tool với nguồn, ngược lại env thật
  (`iso.RealEnv`/`RealHomeOf`). Nguồn khác hoặc tool lệch là lỗi cách dùng; profile chưa tạo là lỗi nêu `bint iso create`.
- **`detect`.** `Harnesses()`/`Find()` tính home từ `iso.RealHome()` (lỗi thì `os.UserHomeDir`).
- **Giới hạn chưa đo.** (1) Codex thật ở profile `work` chưa chạy (nghiệm thu thật ở GĐ6): hook `-c hooks.PreToolUse` của bước có thể bị
  Codex bỏ qua nếu nó đòi `trusted_hash` cho hook từ lớp `-c` trong profile — khi đó hook im lặng và chỉ thấy bằng phiên thật; hook dự án
  đã trusted nhờ `bint iso sync` (GĐ4) không bị ảnh hưởng. (2) Adapter Codex của evolve (`codex_restricted`, app-server với profile tạm
  riêng) vẫn dựng env từ env của tiến trình, chưa theo `harnesses.codex.profile`. (3) macOS/Windows chưa đo (S5/S6).

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
bint iso import --from-ai-isolator [<tool>--<name>...] [--with-sessions] [--dry-run] [--source DIR] [--json]   # chỉ người
bint iso shell-init <bash|zsh|fish|pwsh>
```

**Ghi chú GĐ2 (đã cài đặt, khác chữ trong khối trên):** `run`/`login` nhận cờ của bint (`--name`, `--lb`, `--no-lb`) **sau** `<tool>`; mọi
đối số khác sau `<tool>` được chuyển nguyên văn cho harness, cờ của harness phải đặt sau `--` (shell-init luôn chèn `--`). `login`
là `claude auth login` / `codex login` (`codex` linked: không chạy harness); `create`/`status`/`gc`/`remove`/`shell-init` như trên,
`gc` chỉ có `--dry-run` và `--profile <tool>/<name>` (chưa có `--deep`), `remove` có `--yes`. `iso` đọc cấu hình toàn cục bằng
`config.LoadGlobal()` (mặc định + `config.yml` của máy, bỏ qua `.bint/` của repo). `create codex` dò LB **trước** khi tạo thư mục:
LB tường minh hỏng thì mã 69 và không để lại profile. Mọi lệnh lá `iso …` nằm trong `actions.Data.Excluded` (chưa có màn TUI).

Tên mặc định `default`. `run` khi profile chưa có thì báo lệnh `create`, không tự tạo (khác ai-isolator: tránh tạo profile do gõ
sai). Lệnh mới cần hàng trong `cliRefs` và `litsrUsage` (k:01M78XP5NXWWRAZQHN5PSHNC13). `remove`/`gc --deep`/`import` là thao
tác của người: test phải gọi `asHuman(t)` (k:01M746PHJXDZN6QMA3BEVEDNPP).

**Ghi chú GĐ3 (đã cài đặt, mục 5.8):** thêm `bint iso du [--json] [--profile <tool>/<name>]`; `gc` có thêm `--deep` (chỉ người dùng) và `--json`; `status` có
cột TỔNG, dòng `! <id>: dữ liệu người dùng trong HOME giả: …` và dòng `! <id>: bản cài: …`; `create` in cảnh báo bản cài Codex. Lệnh mới cần hàng trong
`actions.Data.Excluded` ("iso du") và loại trừ literal `bint iso gc` của doctor trong `cliref_inventory_test.go` (k:01M78XP5NXWWRAZQHN5PSHNC13).

**Ghi chú GĐ4 (đã cài đặt, mục 5.8):** `bint iso sync [--check] [--profile <tool>/<name> | --all] [--json]`; không cờ = mọi profile (`--all` chỉ để
rõ nghĩa, không dùng cùng `--profile`: mã 2). Ghi khi profile có phiên chạy thì từ chối (`ErrBusy`, mã 1); `--check` không ghi, không lấy khoá, không
chạy harness, thoát 1 khi lệch hoặc xung đột (không in `lỗi:`), 0 khi khớp. `create` tự sync sau khi tạo (lỗi/xung đột chỉ là cảnh báo); `run` chạy kiểm
tĩnh trước khi spawn và chỉ in cảnh báo `lệch bản thật` (mã thoát của `run` không đổi). Thư mục riêng có dữ liệu ở chỗ liên kết chỉ được báo (`giữ`), không tính
là lệch. Lệnh mới có hàng "iso sync" trong `actions.Data.Excluded` và loại trừ literal `bint iso sync` của `app/iso_sync.go` trong `cliref_inventory_test.go`
(k:01M78XP5NXWWRAZQHN5PSHNC13).

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
  tmp_ttl: 24h          # chỉ áp cho temp của profile còn daemon sống (mục 5.3, S3)
  pass_env: []          # biến credential giữ lại cho harness (không được trùng biến do iso tự đặt)
  drop_env: []          # biến gỡ thêm (GĐ1 bổ sung; 5.2 đã nói "pass_env/drop_env qua cấu hình")
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
trong profile Codex mới mà không cần trust tay; `bint mcp` do Codex khởi chạy trong profile mở đúng `state.sqlite` thật
(nhờ bảng `env` của mục MCP, mục 11 S1); sửa tay một MCP ở profile thì `sync` báo xung đột, không đè.

### GĐ5 — Tích hợp workflow/eval/gateway

`harnesses.<h>.profile`, `AgentRequest.Profile`, handle phiên có profile, gateway `RealEnv`, `knowledge import --profile`,
`detect`. **Xong khi**: `bint run` một tác vụ với Codex ở profile `work` — hook ghi vào state thật, chi phí đọc được, resume nối
tiếp đúng phiên; đổi profile giữa chừng thì chạy mới có ghi lý do.

### GĐ6 — Import từ ai-isolator, tài liệu, nghiệm thu

`import --from-ai-isolator` (auth, config, LB, tuỳ chọn sessions; bỏ cache/tmp/versions), `docs/usage.md`, `docs/configuration.md`,
tri thức (warning: XDG trong profile; ghim BINT_STATE_DIR). Thêm mã `NT-ISO-…` vào `docs/acceptance.md` mục 3 và gom vào vòng có
sẵn (phiên chạy tay + máy Windows), theo quy ước nghiệm thu một nguồn.

**Ghi chú GĐ6 (đã cài đặt):**

- **Danh sách cho phép, không phải danh sách loại trừ.** Profile ai-isolator thật có nhiều thư mục lạ (`.gemini`, `.docker`, `Projects/`, `.orca`…)
  và `home/.codex/hooks.json` là symlink tới `~/.codex/hooks.json` thật: chép "hết trừ rác" dễ lọt rác hoặc để `iso sync` sau đó ghi xuyên symlink vào tệp
  thật. `iso.Import` (`internal/iso/import.go`) chỉ chép tệp thường (Lstat, không đi theo symlink) trong bảng theo tool — Codex `.codex/auth.json`,
  `config.toml`, `rules/`; Claude `.claude/.credentials.json`, `.claude.json`, `settings.json`, `CLAUDE.md`; chung `.gitconfig`; với `--with-sessions` thêm
  `sessions/`, `archived_sessions/`, `session_index.jsonl`, `history.jsonl` (Codex) và `projects/`, `history.jsonl` (Claude). HOME giả của bint trùng đường dẫn
  tương đối với ai-isolator (`home/.codex`, `home/.claude`) nên không cần ánh xạ. Đích 0600/0700, tạo mới O_EXCL trong profile vừa `Create`; đích luôn kiểm nằm trong
  `home` của profile. Nhóm bỏ (`cache/`, `tmp/`, `data/`, `state/`, `config/`, bản cài, temp/sqlite) được báo kèm dung lượng (cận dưới khi quá nhiều mục).
- **LB nhập thẳng, không dò mạng.** `codex-lb.mode|url` → `Meta.LB` (quyết định 10.3: "import mang luôn URL đã lưu"); import phải chạy được khi LB tắt, còn `run`
  đã dừng mã 69 khi LB linked không sẵn sàng. `linked` mà URL thiếu/hỏng thì để trống kèm ghi chú.
- **Không chép sqlite của Codex** (`state_*.sqlite`, `logs_*.sqlite`: hàng chục–trăm MB, có thể đang được phiên thật ghi). Codex có liệt kê/resume rollout đã chép
  khi thiếu `state_*.sqlite` không: đo ở `NT-ISO-PROF-IMPORT`. Copy, không hardlink: harness ghi nối vào rollout khi resume, hardlink sẽ sửa store nguồn.
- **Làm sạch cấu hình đã chép** để `IsoSync` dựng lại theo receipt mà không rơi vào nhánh xung đột "C có, R vắng, C ≠ D" (5.6): bỏ mục MCP trùng tên bản thật
  (`app.IsoImport` tính danh sách từ `config.toml`/`.claude.json` thật) và khoá `hooks.state` trỏ vào thư mục profile ai-isolator (đường dẫn cũ, vô hiệu). MCP riêng
  và `[projects.*]` giữ nguyên.
- **Hoàn thiện dùng chung với `create`:** `app.IsoFinalize` (EnsureShares, EnsureInstall, InstallIssues, IsoSync); cả hai lệnh in qua `printIsoFinalize`.
- **An toàn:** store nguồn chỉ đọc (test so sha256 + danh sách tệp trước/sau); profile bint đã có thì `skipped`, không ghi đè; lỗi giữa chừng gọi `RemoveProfile` (điểm chèn lỗi
  `importFault` cho test); nguồn và store bint không được trùng/lồng nhau; `app.RequireHuman` ở cả CLI và `app.IsoImport` (`ErrAgentForbidden`). Thoát 1 khi có profile lỗi.
- **Mã nghiệm thu dùng tiền tố `NT-ISO-PROF-*`** (`CODEX`, `CLAUDE`, `CACHE`, `IMPORT`, `MAC`, `WIN`): `NT-ISO-*` đã là mã của kế hoạch cô lập worktree (`NT-ISO-G4`, `NT-ISO-RO-FP`…); dòng
  V1 ở `docs/acceptance.md` 4.1 nay liệt kê mã tường minh thay cho ký tự đại diện. Không có vòng riêng: gom vào V1 (bước 7) và V2.
- **Giới hạn chưa đo:** Codex/Claude thật trong profile, đăng nhập còn hiệu lực sau import, `--with-sessions` resume được không có sqlite, Keychain macOS, Windows (S5/S6).

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

## 11. Kết quả spike (GĐ0, đo 2026-11-16 trên Linux)

### 11.0 Môi trường đo và cách làm

| Mục | Giá trị |
|---|---|
| OS | Linux 7.0.0-42-generic, x86_64; sandbox Codex = bwrap (`linux-sandbox`) |
| Claude Code | 2.1.325 (`~/.local/bin/claude` → `~/.local/share/claude/versions/2.1.325`); mô hình `claude-haiku-5-5` |
| Codex | CLI `codex-cli 0.167.0` (`~/.codex/packages/standalone/current`); gói daemon dùng chung 0.169.0; mô hình `gpt-6-luna`, effort `low` |
| Codex LB | `http://127.0.0.1:2455`, `GET /health/ready` trả `ok` |

Quy ước rút gọn: `$S` = thư mục spike dưới `/tmp/iso-spike/<id>` (đã xoá); `$P` = `~/.local/share/ai-isolator/profiles/codex--personal`.
Env chạy luôn bắt đầu bằng `env -i PATH=… HOME=$S/home XDG_CONFIG_HOME=$S/home/.config XDG_DATA_HOME=$S/home/.local/share
XDG_STATE_HOME=$S/home/.local/state XDG_CACHE_HOME=$S/cache TMPDIR=$S/tmp` (bố cục D2 nhưng cache/tmp tách riêng như ai-isolator).
Chương trình dò (`probe.sh`) ghi `env` ra tệp 0600 rồi chỉ in tên biến và giá trị của biến không bí mật.

**Chỗ lệch so với yêu cầu — cần người xem:**

1. **Codex không trỏ `CODEX_HOME` vào `$P/home/.codex`.** Lúc đo, profile `codex--personal` đang được một phiên Codex thật của
   người dùng dùng (`ai-isolator run codex --name personal`, kèm MCP specmatic/uv); chạy thêm Codex trên cùng `CODEX_HOME`
   sẽ ghi đồng thời vào `state_5.sqlite`, `logs_2.sqlite`, `thread_history_1.sqlite` (584 MB) của phiên đó, và không có cách
   khôi phục an toàn. Vì vậy dùng `CODEX_HOME=$S/home/.codex` (cấu hình tối thiểu do spike tự viết, không chép `config.toml`
   của profile). "Đăng nhập sẵn" của profile này **không phải `auth.json`** (profile không có tệp đó, chế độ `linked`) mà là
   khoá Codex LB lấy từ env của phiên (`CODEX_LB_API_KEY`), truyền qua `env_key` đúng bộ cờ `-c model_provider="codex-lb" …`
   của ai-isolator — dùng tại chỗ, không chép, không in.
2. **Claude dùng `CLAUDE_CONFIG_DIR=$P/home/.claude` đúng yêu cầu, nhưng đăng nhập của profile đã hết hạn** (`Failed to
   authenticate: OAuth session expired and could not be refreshed`). Hệ quả: không có lượt gọi mô hình nào của Claude ⇒
   không đo được hook `PreToolUse` của Claude (chỉ đo được `SessionStart` và MCP, chạy trước bước xác thực). Không dùng
   `~/.claude` thật để thay thế.
3. Khôi phục: xem 11.7.

### 11.1 S1 — env của MCP stdio và hook

Lệnh (Codex): `codex-run.sh $S 'Run the shell command: echo hi. Then reply with just OK.' </dev/null` =
`env -i … CODEX_HOME=$S/home/.codex CODEX_LB_API_KEY=… codex exec --skip-git-repo-check -C $S/work -s read-only -c model_provider=… "<prompt>"`
với `config.toml` có `[mcp_servers.probe] command=probe.sh args=["codex-mcp","mcp"]`, `[features] hooks=true` và
`hooks.json` có `PreToolUse` + `SessionStart` trỏ `probe.sh`.
Lệnh (Claude): `claude -p --model claude-haiku-5-5 --strict-mcp-config --mcp-config mcp.json --settings hooks.json
--permission-mode bypassPermissions '<prompt>'` với `HOME`/`XDG_*`/`TMPDIR` giả và `CLAUDE_CONFIG_DIR=$P/home/.claude`.

| Tiến trình con | Biến nhận được (ngoài biến kế thừa không liên quan) |
|---|---|
| Codex — MCP stdio | **chỉ `HOME`, `PATH`, `TMPDIR`** (+ `PWD`). Không có `XDG_*`, `CODEX_HOME`, `CODEX_LB_API_KEY` |
| Codex — hook | đủ `HOME`, `XDG_*`, `TMPDIR`, `CODEX_HOME`, `CODEX_LB_API_KEY` (qua `sh -lc`, thêm `LOGNAME`, `OLDPWD`, `SHLVL`) |
| Claude — MCP stdio | đủ `HOME`, `XDG_*`, `TMPDIR`, `CLAUDE_CONFIG_DIR` + biến `CLAUDE*`/`AI_AGENT` do Claude tự thêm |
| Claude — hook `SessionStart` | như trên; `PreToolUse` **chưa đo** (đăng nhập hết hạn) |

Kiểm tra bổ sung bằng chứng cho D6: thêm vào mục MCP của Codex `env = { BINT_STATE_DIR="/real/state", BINT_CONFIG_HOME="/real/config" }`
và `env_vars = ["SPIKE_FWD"]` ⇒ MCP nhận cả `BINT_STATE_DIR`, `BINT_CONFIG_HOME` lẫn `SPIKE_FWD` (cùng `HOME` giả).
Hook Codex **không chạy** (không dòng lỗi) khi `config.toml` thiếu `[hooks.state."$S/home/.codex/hooks.json:pre_tool_use:0:0"]
trusted_hash`; chạy sau khi ghi `trusted_hash = currentHash` lấy từ app-server `hooks/list` (khoá chứa đường dẫn tuyệt đối).
Lưu ý thao tác: `codex exec` cần `</dev/null`, nếu không chờ stdin ("Reading additional input from stdin").

**Kết luận S1: đạt.** D2 **giữ nguyên và được xác nhận** cho Codex (MCP chỉ giữ `HOME`/`TMPDIR` ⇒ XDG phải nằm đúng chỗ theo
`HOME`). D6 **sửa** (đã sửa mục 4 và 5.6): `BINT_STATE_DIR`/`BINT_CONFIG_HOME` không tới MCP stdio của Codex nếu chỉ đặt ở env
của tiến trình cha ⇒ `sync` phải ghi bảng `env` vào mục MCP bint; thêm điều kiện `trusted_hash` cho hook Codex (5.6).

### 11.2 S2 — dùng chung `packages/` giữa hai `CODEX_HOME`

Dựng `shared/app-server-daemon/{releases/0.169.0-… → ~/.codex/packages/…/releases/0.169.0-…, current → releases/0.169.0-…,
auto-update-version}` và `a|b/home/.codex/packages -> shared` (hai `CODEX_HOME` khác nhau, `~/.codex` thật không bị chạm).
Lệnh: chạy **song song** `codex app-server daemon start` ở a và b; sau đó `daemon version`, `daemon stop` từng bên.

- Cả hai khởi động cùng lúc (cách nhau 1 ms), `status:"started"`, `managedCodexPath = $S/a|b/home/.codex/packages/app-server-daemon/current/bin/codex`,
  `managedCodexVersion = 0.169.0`; `daemon version` báo `running` ở cả hai, `stop` báo `stopped`; không có `install.lock` mới,
  socket/pid/lock của mỗi bên nằm trong `CODEX_HOME` riêng (`app-server-control/`, `app-server-daemon/daemon.*`).
- Trường hợp `packages` **trống**: `daemon start` in `Installing daemon from CLI version 0.167.0 into …/packages/app-server-daemon`,
  tạo `install.lock` và `releases/.staging.*`, tải bản mới (đã dừng ở ~210 MB để khỏi đầy đĩa). Tức là không dùng lại
  `standalone/` của máy thật. `codex exec` trong `CODEX_HOME` mới **không** tạo `packages/`.
- Chưa đo (đĩa trống chỉ ~1–2 GB lúc đó và cần mạng ~430 MB): hai daemon cùng **cập nhật** `current` (`daemon update`,
  vòng `pid-update-loop`) hoặc daemon profile cập nhật bản thật khi daemon thật đang chạy; hành vi khoá `install.lock` khi
  tranh chấp thật.

**Kết luận S2: đạt một phần.** D5 phía Codex **giữ nguyên** (symlink `packages/` tới bản thật chạy song song được khi bản cài đã
có), kèm điều kiện mới ở 5.4: chỉ tạo liên kết khi `~/.codex/packages/app-server-daemon/current` tồn tại, nếu không để trống
và báo trong `status`. Phần cập nhật song song còn mở, đưa vào rủi ro GĐ3.

### 11.3 S3 — xoá `TMPDIR` khi Codex còn sống

Lệnh: `codex exec … 'Run the shell command: sleep 30; echo after-sleep. Then run: echo TMPDIR=$TMPDIR; ls -d $TMPDIR; mktemp; echo done.'`
chạy nền; sau ~8 giây (lúc MCP đã chạy) `rm -rf $S/tmp`.

- Lệnh `sleep 30` đang chạy **hoàn tất bình thường** (`after-sleep`).
- Lệnh shell kế tiếp **panic trước khi chạy**: `thread 'main' panicked at linux-sandbox/src/linux_run_main.rs:970:17: failed to
  resolve synthetic mount registry temp directory $S/tmp: No such file or directory`. Thư mục `$S/tmp` **không** được tạo lại.
- Chạy mới với `TMPDIR` chưa tồn tại từ đầu: cũng panic y như vậy ⇒ thư mục phải có sẵn trước khi spawn.
  (Trong thư mục tạm của Codex có `codex-bwrap-synthetic-mount-targets-<uid>`, do bwrap sandbox dùng.)
- Daemon app-server rảnh (`daemon start`, rồi `rm -rf TMPDIR`): vẫn `running`, `TMPDIR` không được tạo lại. Chưa đo lệnh chạy
  qua daemon sau khi mất `TMPDIR` (cần client app-server + mô hình); suy ra cùng sandbox sẽ cùng lỗi, ghi là giả định.

**Kết luận S3: đạt (xác định được ràng buộc).** D4 **giữ**, nhưng 5.3 **sửa**: `MkdirAll` mỗi lần `run` là bắt buộc; chỉ xoá cả
thư mục khi lấy được X **và** profile không có daemon sống; khi có daemon thì chỉ xoá mục con cũ hơn `tmp_ttl` (24h, bỏ qua
`codex-*`) và giữ thư mục. ~~Daemon sống = khoá `daemon.pid.lock` bị giữ~~ **đã sai, sửa ở 11.3b** (đo GĐ2): dùng `daemon.pid`.
Khoá cấu hình `iso.tmp_ttl` đã có từ GĐ1.

### 11.3b S3 bổ sung (GĐ2) — daemon app-server giữ `daemon.pid.lock` bằng khoá gì?

Đo 2026-11-16, Codex CLI 0.167.0 (CLI) với gói daemon 0.169.0, Linux (ext4, dev `103:03` trong `/proc/locks`, so inode bằng `stat -c %i`).

- **Daemon thật đang chạy** (`~/.codex`, pid 2045593, bản 0.169.0, rảnh): inode `daemon.pid.lock` = 3722328, `daemon.lock` = 3710164,
  `daemon-updater.pid.lock` = 3722399 — **không inode nào có trong `/proc/locks`**. Khoá duy nhất của pid này là `FLOCK ADVISORY WRITE`
  trên inode 4382519 = tệp `.lock` trong `~/.codex/tmp/arg0-linux/codex-arg0XXXX/` (fd 3, khoá theo tiến trình, không thuộc profile
  theo cách ổn định), cùng `.lock` ở `/tmp/codex-daemon-<uid>/<hash>.lock` (fd 31, tệp socket) và các khoá `POSIX READ` của SQLite.
- **Daemon mới trong `CODEX_HOME` tạm** (`codex app-server daemon start`, `packages` symlink tới bản thật, sau đó `daemon stop`): kết quả y hệt
  — `daemon.pid.lock` (inode 1068608) và `daemon.lock` (1068603) không có trong `/proc/locks`; thử `flock(LOCK_SH|LOCK_NB)` từ tiến trình
  khác trên cả hai tệp **thành công ngay** khi daemon đang sống. Tệp `.lock` giữ khoá lúc đó nằm ở `/tmp/codex-daemon-<uid>/` và
  `$CODEX_HOME/.tmp/plugins.sync.lock`, không phải trong `app-server-daemon/`.
- `daemon.pid` là JSON `{pid, processStartTime, processIdentity:{bootId,startTicks}, executableIdentity}`; `startTicks` khớp
  trường 22 của `/proc/<pid>/stat` (84292861 = 84292861 với daemon thử), nên đối chiếu được pid bị dùng lại.

**Kết luận:** khoá trên `daemon.pid.lock` chỉ giữ thoáng qua lúc `start`/`stop` (không phải FLOCK, cũng không phải fcntl, của tiến trình
sống lâu), nên `fslock.Try` trên đó **không** cho biết daemon còn sống — dựa vào nó sẽ xoá `TMPDIR` dưới chân daemon (S3: panic).
Quy tắc `daemonAlive` được chọn: đọc `app-server-daemon/daemon.pid`; tệp không có ⇒ không có daemon; tiến trình pid không sống ⇒ chết; Linux
mà `startTicks` khác `/proc/<pid>/stat` ⇒ pid dùng lại ⇒ chết; còn lại (kể cả không đọc/parse được) ⇒ coi là sống (sai lệch an toàn: chỉ
giữ thư mục và xoá mục cũ theo tuổi). macOS/Windows không đo được `startTicks` nên chỉ kiểm pid còn sống (thuộc S5/S6).

### 11.4 S4 — chặn tự cập nhật của Claude; `claude` từ PATH thật chạy với HOME giả

- `claude` (symlink tuyệt đối tới `versions/2.1.325` thật) chạy bình thường với `HOME` giả: khởi động, nạp MCP và `SessionStart`
  hook, mở transcript trong `$CLAUDE_CONFIG_DIR/projects` (tới bước xác thực thì dừng vì đăng nhập hết hạn).
  `claude -p` không tạo `versions/` hay bản cài nào trong `XDG_DATA_HOME` giả.
- `DISABLE_AUTOUPDATER=1 claude update` (`$S/home` giả) **vẫn chạy cập nhật**: in `Claude Code is up to date (2.1.325)` và chép
  **349 MB** vào `$S/home/.local/share/claude/versions/2.1.325`, tạo `$S/home/.local/bin/claude`, `$S/home/.npm/_logs`,
  `$S/home/.local/state/claude/locks`, kèm cảnh báo `~/.local/bin is not in your PATH`.
- `DISABLE_UPDATES=1 claude update` → `Updates are disabled by your administrator…`, `$S/home` còn 20 KB, không có `versions/`.
  `claude -p` với `DISABLE_UPDATES=1 DISABLE_AUTOUPDATER=1` khởi động bình thường (cùng lỗi xác thực).
  Chuỗi `DISABLE_UPDATES` có trong binary 2.1.325 (11 lần cùng `DISABLE_AUTOUPDATER` 12 lần).
- Chưa chứng minh được: việc **tự** cập nhật nền khi có bản mới hơn (lúc đo 2.1.325 đã là bản mới nhất, và phiên tương tác
  không thử được vì đăng nhập hết hạn). Lý do giữ cả hai biến là để phủ cả đường tự động lẫn lệnh thủ công.

**Kết luận S4: đạt có sửa đổi.** D5 phía Claude **sửa** (mục 4, 5.2, 5.4): đặt `DISABLE_UPDATES=1` (+ `DISABLE_AUTOUPDATER=1`) và không
bao giờ chạy `claude update|install` trong profile.

### 11.5 S5 — Windows (chờ nghiệm thu trên máy Windows)

Chưa đo. Cần đo, ghi phiên bản claude/codex và Windows:

1. `claude`/`codex` đọc `USERPROFILE`, `APPDATA`, `LOCALAPPDATA` hay gọi API thư mục shell (SHGetKnownFolderPath, bỏ qua env)
   khi `HOME`/`USERPROFILE` đổi; MCP stdio và hook của Codex nhận những biến nào (lặp S1 với `probe.cmd`).
2. Junction cho `home\.cache`, `home\.npm`, `home\.codex\packages`: tạo bằng `FSCTL_SET_REPARSE_POINT`/`mklink /J` không cần
   quyền admin hay Developer Mode; Claude/Codex/npm/uv đi qua junction được; `EvalSymlinks`/`os.Lstat` nhận ra junction.
3. `TMPDIR`/`TMP`/`TEMP` do Codex/Claude dùng trên Windows; bwrap không có, nên kiểm lại S3 với sandbox Windows của Codex
   (xoá thư mục tạm khi đang chạy, lệnh kế tiếp có lỗi không); độ dài đường dẫn temp (MAX_PATH).
4. `DISABLE_UPDATES` có tác dụng với `claude update` trên Windows; vị trí bản cài tự động (`%LOCALAPPDATA%`?).
5. Hook Codex: bảng `[hooks.state."<đường dẫn>"]` với đường dẫn có `\` (khoá có dùng `/` hay `\`).

### 11.6 S6 — macOS (chờ nghiệm thu trên máy macOS)

Chưa đo. Cần đo, ghi phiên bản claude/codex và macOS:

1. Claude lưu credential ở Keychain theo khoá nào khi đổi `CLAUDE_CONFIG_DIR`/`HOME` (service name có băm của thư mục cấu
   hình hay dùng chung một mục): nếu dùng chung một mục thì mọi profile cùng đăng nhập một tài khoản ⇒ ảnh hưởng cách ly
   và `import`; nếu theo thư mục thì `import --from-ai-isolator` có phải đăng nhập lại.
2. Codex lưu `auth.json` hay Keychain (`cli_auth_credentials_store`) khi `CODEX_HOME` đổi.
3. Lặp S1 (env của MCP/hook), S3 (xoá `TMPDIR`; sandbox Seatbelt thay cho bwrap), S4 (`DISABLE_UPDATES`) trên macOS;
   `os.TempDir()` trả `/var/folders/...` (alias `/private/var`) ⇒ kiểm `EvalSymlinks` khi so chứa đường dẫn.
4. Symlink tới `~/Library/Caches` cho cache chung (D3) và các cache khác (`~/.npm`, `~/Library/pnpm`).

### 11.7 Khôi phục sau spike và việc còn mở

- Thư mục `/tmp/iso-spike` đã xoá; không còn tiến trình spike nào; `~/.codex/packages` thật không đổi (so danh mục tệp trước/sau);
  `~/.claude` thật không bị chạm (chỉ chạy với `CLAUDE_CONFIG_DIR` của profile và `HOME` giả).
- Profile `codex--personal`: đã khôi phục `home/.claude/.claude.json` về bản chụp trước spike, xoá hai `projects/-tmp-iso-spike-*`,
  hai `backups/.claude.json.backup.*` mới và `state/mcp-discover-verdicts.json` do Claude tạo; sha256 của `.credentials.json`
  và `home/.claude.json` không đổi (không có token nào bị ghi vào tài liệu, nhật ký hay thư mục tạm).
  **Không khôi phục được:** (a) hai bản `home/.claude/backups/.claude.json.backup.1792848381565` và `…1792869982347` đã bị Claude
  tự xoay vòng (xoá bản cũ nhất) khi chạy; (b) `home/.claude/cache/changelog.md` và
  `home/.claude/plugins/store/cc-plugin-agents-md_builtin-….json` được Claude làm mới (cache, nội dung tự sinh lại);
  (c) `home/.codex/logs_2.sqlite*` đổi do phiên Codex thật đang chạy trên profile, không phải do spike.
- Việc còn mở sau GĐ0: (1) đăng nhập lại Claude cho profile rồi đo `PreToolUse` của Claude và S4 phần tự cập nhật nền khi có
  bản mới; (2) S2 phần cập nhật `packages/` song song; (3) S3 lệnh chạy qua daemon app-server mất `TMPDIR`; (4) S5, S6.

**Tóm tắt kết luận thiết kế:** D2 giữ; D4 giữ (sửa 5.3: tạo lại thư mục mỗi lần, không xoá khi còn daemon, `tmp_ttl`);
D5 sửa (Claude: `DISABLE_UPDATES`; Codex: giữ symlink `packages/` có điều kiện); D6 sửa (bảng `env` cho MCP Codex); 5.3, 5.4,
5.6 và GĐ4 đã sửa tương ứng; 5.1, 5.2 (trừ biến cập nhật), 5.5, 5.7, 5.8 giữ nguyên.
