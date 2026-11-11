# Kiến trúc và dữ liệu (P0–P8)

Tổng quan cấu trúc package, luồng ghi nhận sự kiện và phân định nguồn dữ liệu của Bint
(chi tiết thiết kế tại [docs/bint-plan.md](bint-plan.md)).

```text
cmd/bint/                     điểm vào
internal/cli/                 cobra, định dạng đầu ra, mã thoát, điều phối evolve cycle
internal/app/                 init, doctor, hook wrapper, workspace
internal/config/              schema v1, mặc định, nạp theo lớp: toàn cục → dự án → .bint/local.yml
internal/state/               thư mục trạng thái ngoài repository, repo-id từ git-common-dir
internal/store/               SQLite (WAL) + migration có phiên bản (migrations/*.sql)
internal/spool/               hàng đợi sự kiện dạng tệp (mỗi lần hook ghi lô sự kiện nguyên tử)
internal/runlog/              nạp spool → events/runs, loại trùng theo event_id; truy vấn, tín hiệu kết quả
internal/harness/             P5: HarnessAdapter, bảng khả năng, chọn harness/mô hình theo role
internal/harness/hookcore/    P5: xử lý hook dùng chung (dialect theo từng harness)
internal/harness/claude/      adapter Claude Code: hook, .mcp.json
internal/harness/codex/       P5: adapter Codex: .codex/hooks.json, .codex/config.toml, apply_patch
internal/harness/antigravity/ P5: adapter Antigravity CLI: .agents/hooks.json, payload camelCase
internal/jsondoc/             sửa tệp JSON của harness, giữ thứ tự khoá
internal/fsafe/               ghi nguyên tử, sao lưu, khối đánh dấu, manifest băm
internal/detect/              phát hiện harness, lệnh test đề xuất, nhận diện lệnh test, che bí mật
internal/eval/                P1: khai thác tác vụ, kiểm tra rò rỉ, môi trường cô lập, nhánh, agent, thống kê
internal/knowledge/           P2: định dạng mục tri thức, bảng chuyển trạng thái, kiểm tra xác định được, dựng AGENTS.md
internal/inspect/             P2: static discovery (ngôn ngữ, manifest, lệnh, công cụ, tài liệu, mã sinh, điểm nóng Git)
internal/skills/              P2/P4/P7: 6 skill gốc nhúng trong tệp thực thi, lớp phủ, skill tiến hoá, core.lock
internal/pathglob/            mẫu glob dùng chung
internal/symbols/             P3: vùng dòng của ký hiệu (go/parser cho Go, heuristic cho ngôn ngữ khác)
internal/kindex/              P3: chỉ mục FTS5 dẫn xuất từ tệp tri thức
internal/ctxengine/           P3: Context Engine — truy xuất theo tác vụ/đường dẫn, bài học, gói ngữ cảnh
internal/mcpserver/           P3: MCP server stdio và sáu công cụ
internal/task/                P4: phiếu tác vụ, bảng chuyển trạng thái, hợp đồng tài liệu v1, kiểm chứng, review
internal/workflow/            P4/P5: thao tác tác vụ dùng chung, khoá ghi, hậu kiểm chỉ đọc, orchestrator `bint run`
internal/hcomp/               P8-H: thành phần quy trình (guidance, guardrail, script, strategy): nạp, kiểm tra, đánh giá guardrail, trạng thái phiên
internal/evolve/              P6/P7/P8: cơ hội, đề xuất L1/L2, Probe tuần tự, áp dụng/quay lui, digest và reflector
```

**Luồng ghi nhận:** harness gọi `bint hook claude`, `bint hook codex` hoặc
`bint hook antigravity <sự kiện>` → hook xác định repository từ `cwd`, ghi một tệp
`<ulid>.jsonl` vào `spool/` rồi thoát 0 (không mở SQLite, không gọi mô hình, lỗi chỉ được
ghi vào `hook-errors.log`; trước khi thoát, hook có thể trả quyết định guardrail đọc từ
`.bint/harness`). Mỗi lệnh đọc (`runs`, `status`) nạp spool vào SQLite trong giao
dịch; `event_id` tất định (từ `session_id` + `tool_use_id`) nên hook bị gọi trùng không tạo
bản ghi trùng.

**Thư mục trạng thái** (`BINT_STATE_DIR` ghi đè gốc; `state.dir` trong `.bint/local.yml`
ghi đè cho một repository):

| Hệ điều hành | Mặc định |
|---|---|
| Linux | `${XDG_STATE_HOME:-~/.local/state}/bint/<repo-id>` |
| macOS | `~/Library/Application Support/bint/state/<repo-id>` (hoặc `$XDG_STATE_HOME/bint`) |
| Windows | `%LOCALAPPDATA%\bint\state\<repo-id>` |

Mọi worktree của một repository có cùng `<repo-id>` nên ghi về cùng một cơ sở dữ liệu.
Worktree phụ khi `.bint/` chưa được commit sẽ dùng cấu hình của worktree chính.

**Sự kiện được ghi:** `session.started` (nguồn, mô hình, HEAD, nhánh), `session.ended` (lý do),
`file.modified` (đường dẫn tương đối trong repository; tệp ngoài repository bị bỏ qua),
`test.completed` (lệnh đã che bí mật, ok/lỗi theo `PostToolUse`/`PostToolUseFailure` hoặc
`exit_code` trong transcript). Lượt `running` không có sự kiện trong 12 giờ được chuyển
`interrupted`. Giá trị không đo được để `NULL`, không ghi 0. Trên Claude Code còn có `file.read`
(tệp agent đọc) và `skill.activated` (khi công cụ Skill chạy); lệnh shell có ảnh chụp cây làm
việc trước/sau để phát hiện tệp sửa qua shell (trong ngân sách thời gian, bỏ qua tệp bị Git
ignore). Khi có guardrail kích hoạt, hook ghi thêm `guardrail.triggered`. Sự kiện mà harness không cung cấp không bao giờ được tổng hợp giả (mục 13.2).

## Nguồn chuẩn và dữ liệu dẫn xuất

| Dữ liệu | Nơi lưu | Tính chất |
|---|---|---|
| Cấu hình dự án | `.bint/config.yml` | Nguồn chuẩn (commit được); ghi đè theo máy ở `.bint/local.yml` |
| Mục tri thức | `.bint/knowledge/items/<ulid>.md` | Nguồn chuẩn duy nhất (YAML frontmatter + Markdown, commit được) |
| Skill gốc | `internal/skills/core/` | Nhúng trong tệp thực thi; bản xuất cho harness theo dõi qua `.bint/skills/core.lock` |
| Lớp phủ và skill tiến hoá | `.bint/skills/overlays/`, `.bint/skills/evolved/` | Nguồn chuẩn của dự án (commit được) |
| Thành phần quy trình | `.bint/harness/{guidance,guardrails,scripts}/`, `strategy.yml` | Nguồn chuẩn (commit được); chỉ `verified` có hiệu lực ở phiên thật ([harness-components.md](harness-components.md)) |
| Tác vụ đánh giá đã duyệt | `.bint/evals/cases/*.yml` | Corpus chuẩn phục vụ đánh giá và tiến hoá (commit được) |
| Chỉ mục tìm kiếm (`knowledge_index`) | SQLite trong `<state>/` | Dẫn xuất từ `.bint/knowledge/items/`; tự đồng bộ theo chữ ký thư mục hoặc `bint index rebuild` |
| Lượt chạy, sự kiện, tín hiệu, đề xuất, kiểm toán | SQLite trong `<state>/` (`migrations/*.sql`) | Dữ liệu vận hành cục bộ của máy |
| Phiếu tác vụ và tài liệu workflow | `<state>/runtime/tasks/<task-id>/` | `task.yml`, `state.json`, `requirement.md`, `plan.md`, `tasks.md`, `review.*`, `logs/` |
| Theo dõi phiên cho guardrail | `<state>/runtime/harness/` | Tệp JSONL nối thêm (lệnh và tệp đã sửa trong phiên), do hook ghi; không khoá, dòng hỏng bị bỏ qua |
| Kho đệm, thư mục làm việc và nhật ký đánh giá | `<state>/eval-work/` | Dữ liệu cô lập phục vụ `bint eval` và `bint evolve evaluate` |

## Điều phối và tương thích nền tảng

- **Workflow (`internal/workflow`):** điều phối `b-req → b-feat → b-review`, gắn `task_id` và
  `parent_run_id` cho từng bước. Bước có quyền ghi (`b-feat`) giữ khoá hệ điều hành
  (`flock`/`LockFileEx` trong `<state>/runtime/locks/`) để mỗi worktree chỉ chạy một bước ghi tại
  một thời điểm; hệ điều hành tự nhả khoá khi tiến trình chết. Bước chỉ đọc (`b-req`, `b-review`)
  được hậu kiểm bằng chỉ mục Git tạm trước/sau: mọi thay đổi ngoài việc thêm mục tri thức
  `candidate` mới hợp lệ đều làm bước thất bại.
- **Tiến hoá (`internal/evolve`, `internal/cli/evolve_cycle.go`):** phát hiện cơ hội từ lịch sử đã
  lọc, tạo đề xuất L1 (tri thức, bài học từ thất bại) và L2 (skill), đối chiếu bằng Probe tuần tự
  trên tập `validation` rồi `test`, và lưu bản ghi kiểm toán để quay lui (`rollback`). Lưu ý:
  `evolve.AcquireLock` hiện vẫn dùng mẫu `O_EXCL` + kiểm tra PID cũ (xem phát hiện `P6-01` tại
  [docs/status.md](status.md)).
- **Đa nền tảng:** các lời gọi `syscall` đặc thù của hệ điều hành (khoá tệp, nhóm tiến trình,
  dung lượng đĩa) tách thành cặp `*_unix.go` (`//go:build !windows`) và `*_windows.go`
  (`//go:build windows`) có cùng chữ ký hàm; CI kiểm tra `go vet` và `staticcheck` cho cả Linux
  lẫn Windows.
