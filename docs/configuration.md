# Cấu hình Bint

Bint dùng cấu hình YAML (`version: 1`). Mẫu đầy đủ kèm chú thích nằm ở
[`internal/config/template.go`](../internal/config/template.go); định nghĩa struct, giá trị mặc định
và điều kiện kiểm tra nằm ở [`internal/config/config.go`](../internal/config/config.go).

## Tạo, nạp và nâng cấp

```sh
bint init --harness claude,codex --hooks   # tạo .bint/config.yml nếu chưa có
bint init --update                         # bổ sung khoá mới vào .bint/config.yml đã có (có sao lưu)
bint doctor                                # kiểm tra tính hợp lệ của cấu hình và môi trường
```

**Thứ tự nạp theo lớp** (lớp sau ghi đè trường có mặt ở lớp trước; danh sách YAML thay thế toàn bộ
danh sách của lớp trước):
1. Giá trị mặc định tích hợp (`config.Default()`).
2. Cấu hình người dùng: `<UserConfigDir>/bint/config.yml` (`BINT_CONFIG_HOME` ghi đè thư mục gốc).
3. Cấu hình dự án: `.bint/config.yml` (commit để dùng chung trong nhóm).
4. Cấu hình cục bộ theo máy: `.bint/local.yml` (được `bint init` thêm vào `.gitignore` / `exclude`).

Ngoài ra, tệp `.bint/harness/strategy.yml` ở trạng thái `verified` ghi đè một tập con `context.*`
(`max_tokens`, `path_max_items`, `lessons.global_k|max_tokens`, `interfaces.mcp|path_hooks`) và được chèn
**giữa lớp 3 và lớp 4**, nên `.bint/local.yml` vẫn ghi đè được. Tệp `candidate`/`deprecated` không có
hiệu lực; tệp `verified` bị hỏng bị bỏ qua và `bint doctor` cảnh báo lý do. Xem
[harness-components.md](harness-components.md).

Bộ đọc YAML bật `KnownFields(true)` nên mọi khoá lạ hoặc gõ sai tên đều báo lỗi ngay khi nạp.
`bint init` không ghi đè `.bint/config.yml` đã có; khi nâng cấp Bint, chạy `bint init --update`
(hoặc `bint update`) để chèn các khoá mới kèm chú thích mà vẫn giữ nguyên giá trị hiện tại và sao
lưu tệp cũ vào `<state>/backups/`. Nếu cấu hình cũ đã đặt mô hình chung nhưng chưa có bảng `models`
theo role, `init --update` để trống các khoá mô hình theo role mới nhằm giữ nguyên hành vi chọn mô
hình trước đó.

## Dự án và thư mục trạng thái

| Khoá | Mặc định | Ý nghĩa |
|---|---|---|
| `version` | `1` | Phiên bản schema cấu hình (bắt buộc bằng `1`). |
| `project.name` | `""` (`init` điền tên thư mục) | Tên dự án. |
| `project.root` | `"auto"` | Trường dự phòng trong schema; gốc repository luôn xác định qua Git. |
| `state.dir` | `"auto"` | Đường dẫn tuyệt đối tới thư mục trạng thái của repository (ngoài repository), hoặc `"auto"`. |

Với `state.dir: auto`, thư mục trạng thái là `<gốc>/<repo-id>` (trong đó `<repo-id>` tính từ
`git-common-dir` nên mọi worktree của cùng một repository dùng chung trạng thái). Thứ tự chọn `<gốc>`:
`BINT_STATE_DIR` → `$XDG_STATE_HOME/bint` → mặc định theo hệ điều hành (`~/.local/state/bint` trên
Linux, `~/Library/Application Support/bint/state` trên macOS, `%LOCALAPPDATA%\bint\state` trên Windows).
Nếu đặt `state.dir` là đường dẫn tuyệt đối (thường trong `.bint/local.yml`), Bint dùng thẳng đường dẫn
đó mà không nối thêm `<repo-id>`.

## Harness, role và mô hình

`harnesses.enabled` mặc định là `[claude]` (nhận các giá trị `claude`, `codex`, `antigravity`);
`harnesses.default` mặc định là `claude` và phải nằm trong `harnesses.enabled`.

| Role | Vai trò | Mô hình mặc định (`claude` / `codex` / `antigravity`) |
|---|---|---|
| `roles.planner` | Lập kế hoạch (`b-req`) | `opus` / `gpt-6-astra` / `gemini-4-argon-high` |
| `roles.implementer` | Thực thi (`b-feat`) | `sonnet` / `gpt-6-luna` / `gemini-3.8-flash-medium` |
| `roles.reviewer` | Đánh giá (`b-review`) | `opus` / `gpt-6-astra` / `gemini-4-argon-high` |
| `roles.evolver` | Soạn đề xuất tiến hoá L1/L2 | `opus` / `gpt-6-astra` / `gemini-4-argon-high` |
| `roles.reflector` | Rút bài học từ thất bại (kế hoạch E) | `opus` (chỉ hỗ trợ `claude`) |

- **Thứ tự chọn harness trong workflow:** `--role <role>=<harness>` → `--harness` → `roles.<role>` →
  `harnesses.default` → `claude`. Riêng `roles.reflector` hiện chỉ chạy trên `claude` vì cần khoá
  toàn bộ công cụ bằng `--tools ""`; khi dùng Codex hoặc Antigravity làm harness chính, giữ
  `roles.reflector: claude` nếu muốn rút bài học từ thất bại.
- **Thứ tự chọn mô hình:** `--model` → `harnesses.<harness>.models.<role>` → mô hình chung
  (`workflow.agent.model` cho Claude; `harnesses.<harness>.model` cho Codex/Antigravity) → mặc định
  của CLI. Tên mô hình không được chứa khoảng trắng; đặt chuỗi rỗng `""` ở mô hình theo role để rơi
  xuống mô hình chung.
- **Cấu hình riêng của Codex / Antigravity:** `harnesses.codex` và `harnesses.antigravity` có thêm
  `binary` (`""` = tìm `codex`/`agy` trong `PATH`), `model` (`null`) và `extra_args` (`[]`).

Ví dụ kết hợp Claude (lập kế hoạch, review, tiến hoá) và Codex (thực thi):

```yaml
harnesses:
  enabled: [claude, codex]
  default: claude
  codex:
    models:
      implementer: gpt-6-luna
roles:
  implementer: codex
  reflector: claude
```

## Workflow và giới hạn vận hành

| Khoá | Mặc định | Ý nghĩa và ràng buộc |
|---|---|---|
| `workflow.max_review_rounds` | `2` | Số vòng sửa sau `b-review` tối đa trong `b-do` (`0..10`). |
| `workflow.timeout_minutes` | `60` | Thời gian tối đa cho mỗi bước agent (`>= 1` phút). |
| `workflow.verify_timeout_minutes` | `15` | Thời gian tối đa cho lệnh kiểm chứng tác vụ (`>= 1` phút). |
| `workflow.agent.model` | `null` | Mô hình chung cho Claude Code trong workflow. |
| `workflow.agent.permission_mode` | `"acceptEdits"` | Chế độ quyền của Claude Code (`acceptEdits`, `auto`, `dontAsk`, `bypassPermissions`). |
| `workflow.agent.allowed_tools` | `[Read, Edit, Write, Glob, Grep, Bash]` | Công cụ cho bước có quyền ghi (`b-feat`) trên Claude Code. |
| `workflow.agent.readonly_tools` | `Read, Glob, Grep, Bash(bint:*), Bash(git diff|log|show|status:*)` | Công cụ cho bước chỉ đọc (`b-req`, `b-review`) trên Claude Code. |
| `workflow.agent.setting_sources` | `""` | Nguồn settings truyền cho Claude Code (`""` = mặc định của CLI). |
| `workflow.agent.extra_args` | `[]` | Đối số dòng lệnh bổ sung cho Claude Code. |
| `limits.per_run.max_cost_usd` | `null` | Trần chi phí USD mỗi lượt (`> 0` hoặc `null`). |
| `limits.per_run.max_turns` | `null` | Trần số lượt gọi mô hình mỗi lượt (`>= 1` hoặc `null`). |
| `limits.workflows.<wf>` | `{}` | Ghi đè `max_cost_usd` / `max_turns` cho `b-do`, `b-req`, `b-feat`, `b-review`. |
| `limits.protected_paths` | `["migrations/**", ".github/workflows/**"]` | Mẫu glob đường dẫn nhạy cảm cần người duyệt khi bị sửa. |

Trong `b-do`, `max_cost_usd` và `max_turns` là **tổng ngân sách của toàn chuỗi**; mỗi bước con nhận
`min(giới hạn riêng của bước, phần ngân sách tổng còn lại)`. Claude Code thực thi trực tiếp cả hai
trần qua `--max-budget-usd` và `--max-turns`; với Codex và Antigravity (chỉ báo token, không báo USD
và không có cờ giới hạn lượt), các giới hạn này chỉ mang tính chỉ dẫn.

## Ngữ cảnh, tri thức và bộ nhớ

| Khoá | Mặc định | Ý nghĩa và ràng buộc |
|---|---|---|
| `context.max_tokens` | `6000` | Ngân sách token cho gói ngữ cảnh tri thức (`> 0`). |
| `context.interfaces.mcp` | `true` | Bật kênh MCP server (kiểm tra trong `bint doctor`, cài qua `init --mcp`). |
| `context.interfaces.path_hooks` | `true` | Bật hook chèn ngữ cảnh theo đề bài và đường dẫn tệp. |
| `context.interfaces.cli` | `true` | Cho phép workflow dựng gói ngữ cảnh qua Context Engine. |
| `context.path_max_items` | `5` | Số mục tối đa khi chèn ngữ cảnh theo đường dẫn (`1..30`). |
| `context.path_hook_timeout_ms` | `1500` | Hạn thời gian xử lý của hook chèn ngữ cảnh (`100..10000` ms); quá hạn thì bỏ qua. |
| `context.freshness.fingerprint` | `"symbol"` | Mức lấy fingerprint bằng chứng: `symbol`, `hunk`, `file`. |
| `context.lessons.global_k` | `3` | Số bài học phạm vi `global` tối đa chèn vào mỗi lượt (`0..10`; `0` = tắt bài học chung). |
| `context.lessons.max_tokens` | `600` | Ngân sách token riêng cho phần bài học từ thất bại (`>= 0`; `0` = tắt). |
| `knowledge.require_human_approval_for` | `[rule]` | Danh sách loại mục bắt buộc người duyệt (luôn phải chứa `rule`). |
| `knowledge.agents_md_max_items` | `15` | Số mục `rule`/`warning` tối đa đưa vào khối Bint trong `AGENTS.md` (`1..50`). |
| `knowledge.stale_deprecate_days` | `30` | Số ngày ở trạng thái `stale` trước khi được đề xuất loại bỏ (`>= 1`). |
| `memory.scope` | `"project"` | Phạm vi bộ nhớ (schema v1 chỉ nhận `project`). |
| `memory.retention_days` | `90` | Cửa sổ lịch sử (ngày) khi quét cơ hội tiến hoá (`>= 0`; `0` = không giới hạn). |

**Khoá dự phòng trong schema:** `context.retrieval.{lexical, structural, semantic}`,
`context.freshness.verify_changed_sources` và `memory.enabled` đã được khai báo trong schema v1 theo
kế hoạch thiết kế nhưng chưa gắn nhánh xử lý riêng (Context Engine hiện luôn kết hợp FTS5 + đường dẫn;
để tắt tích hợp ghi nhận/MCP, dùng `bint init --no-hooks` hoặc `--no-mcp`).

## Khung đánh giá (`eval`)

`eval.test_command` và `eval.setup_command` mặc định là `null`. `bint init` chỉ ghi lệnh phát hiện tự
động vào `suggested_test_command` / `suggested_setup_command`; Bint không chạy lệnh kiểm thử cho tới
khi bạn chép sang `eval.test_command`.

| Khoá | Mặc định | Ý nghĩa và ràng buộc |
|---|---|---|
| `eval.repeats` | `3` | Số lần lặp cho mỗi cặp (tác vụ, nhánh), `>= 1`. |
| `eval.arms` | `[none, minimal, bint]` | Danh sách nhánh đánh giá (chấp nhận thêm `ideal` và biến thể `bint@…`). |
| `eval.leakage_mode` | `"strict"` | Chế độ chống rò rỉ: `strict` hoặc `economy`. |
| `eval.budget_usd` | `null` | Ngân sách USD cho đợt chạy (`>= 0`; bắt buộc với Claude qua đây hoặc `--budget`). |
| `eval.prior_run_usd` | `0.5` | Chi phí tiên nghiệm mỗi lượt khi chưa có lịch sử (`>= 0`; `0` = tắt dự báo lượt đầu). |
| `eval.harness` | `"claude"` | Harness chạy đánh giá (`claude`, `codex`, `antigravity`). |
| `eval.timeout_minutes` | `30` | Thời gian tối đa cho mỗi lượt đánh giá (`>= 1` phút). |
| `eval.minimal_source` | `"knowledge"` | Nguồn dựng `AGENTS.md` ở nhánh `minimal`: `knowledge` hoặc `file`. |
| `eval.minimal_context` | `".bint/evals/minimal.md"` | Đường dẫn tệp trong repository khi `minimal_source: file`. |
| `eval.minimal_include_candidates` | `false` | Đưa cả mục `candidate` vào nhánh `minimal` (ảnh chụp `eval bootstrap` luôn bật). |
| `eval.keep_bases` | `false` | Giữ lại kho đệm commit gốc sau khi chạy xong. |
| `eval.base_cache_max_mb` | `0` | Giới hạn dung lượng kho đệm theo LRU khi `keep_bases: true` (`0` = không giới hạn). |
| `eval.base_depth` | `0` | Độ sâu clone lịch sử (`0` = đầy đủ lịch sử; `> 0` = clone nông). |
| `eval.min_free_mb` | `1024` | Dung lượng đĩa trống tối thiểu (MB) trước khi nạp kho đệm (`>= 0`). |
| `eval.agent.*` | tương tự `workflow.agent` | Cấu hình Claude Code trong eval (`setting_sources` mặc định `"project,local"`). |
| `eval.mine.*` | `max_commits: 2000`, `min_source_lines: 3`, `max_source_lines: 400`, `max_files: 12`, `min_message_chars: 20` | Bộ lọc kích thước commit và mẫu tệp (`test_patterns`, `ignore_patterns`) cho `bint eval mine`. |

## Tiến hoá (`evolution`)

| Khoá | Mặc định | Ý nghĩa và ràng buộc |
|---|---|---|
| `evolution.enabled` | `true` | Bật phát hiện cơ hội và tạo đề xuất tiến hoá. |
| `evolution.mode` | `"propose"` | `propose` hoặc `auto` (việc tự áp dụng đề xuất rủi ro thấp trong `evolve cycle` do `adoption.auto_low_risk` quyết định). |
| `evolution.execution` | `"manual"` | `manual` hoặc `deferred` (xử lý yêu cầu đã đánh dấu ở lần gọi CLI tiếp theo). |
| `evolution.deferred.max_per_run` | `2` | Số yêu cầu tối đa xử lý mỗi lần gọi CLI ở chế độ `deferred` (`>= 1`). |
| `evolution.deferred.budget_usd` | `null` | Ngân sách cho evolver ở chế độ `deferred` (`null` = chỉ chạy yêu cầu xác định không tốn mô hình). |
| `evolution.trigger.min_similar_runs` | `3` | Số lượt chạy (hoặc tác vụ phân biệt với `failure_lesson`) tối thiểu để mở cơ hội. |
| `evolution.trigger.min_new_evidence` | `2` | Số bằng chứng mới tối thiểu kể từ đề xuất trước cùng đối tượng. |
| `evolution.trigger.group_depth` | `0` | Độ sâu gom nhóm thư mục (`0` = mặc định 2 cấp, tự tách sâu hơn nếu nhóm quá lớn). |
| `evolution.adoption.auto_low_risk` | `false` | Cho phép `evolve cycle` tự áp dụng tối đa 1 đề xuất rủi ro thấp chỉ tạo mục `candidate` mới mỗi vòng. |
| `evolution.adoption.bind_to_model_version` | `true` | Gắn kết quả đối chiếu với phiên bản mô hình; báo cần xác thực lại khi đổi mô hình. |
| `evolution.evaluation.enabled` | `true` | Bật đối chiếu định lượng (`false` = `evolve evaluate` từ chối chạy). |
| `evolution.evaluation.max_candidates` | `2` | Số đề xuất tối đa tạo trong một lần `propose` (`>= 1`). |
| `evolution.evaluation.metric` | `"pass_rate"` | Thước đo chính: `pass_rate`, `cost_usd`, `turns`. |
| `evolution.evaluation.min_effect` | `0.15` | Mức cải thiện tối thiểu cần phát hiện khi tính cỡ mẫu Probe, thuộc `(0, 1)`. |
| `evolution.evaluation.non_inferiority_margin` | `0.10` | Biên không kém hơn khi đánh giá loại bỏ/gộp, thuộc `[0, 1)`. |
| `evolution.evaluation.repeats` | `0` | Số lần lặp cố định của Probe (`0` = tự chọn trong `1..max_repeats` để cực tiểu tổng số lượt). |
| `evolution.evaluation.max_repeats` | `8` | Số lần lặp tối đa của Probe (`>= 1`). |
| `evolution.evaluation.effect_sd` | `0.15` | Độ lệch chuẩn hiệu ứng giữa các tác vụ (`σ_b`) khi chưa đủ lịch sử ước lượng, thuộc `(0, 1)`. |
| `evolution.evaluation.budget_usd` | `null` | Ngân sách tối đa cho một lần Probe (`> 0` hoặc `null`). |
| `evolution.evaluation.delivery` | `"bint"` | Kênh đưa tri thức khi đối chiếu: `bint` hoặc `minimal`. |
| `evolution.evaluation.leakage_mode` | `"economy"` | Chế độ chống rò rỉ khi đối chiếu đề xuất (`strict` hoặc `economy`). |

## Bảo mật (`security`)

- `security.require_approval_for_commands` (mặc định `true`): yêu cầu người dùng xác nhận
  `verify.command` của tác vụ trước khi `bint task verify` hoặc `bint run` thực thi.
- `security.allow_external_agents` (`false`) và `security.allow_network_in_evals` (`false`): hiện là
  khoá khai báo trong schema, **chưa có lớp chặn tiến trình hoặc cô lập mạng ở tầng hệ điều hành**;
  quyền thực tế của agent phụ thuộc vào cấu hình sandbox của từng harness.

## Giao diện (`ui`)

- `ui.language` (mặc định `"auto"`): ngôn ngữ giao diện TUI, `auto` | `vi` | `en`. Thứ tự ưu tiên khi
  chọn: cờ `--lang` của `bint tui` > biến `BINT_LANG` > `ui.language` > `LC_ALL`/`LANG` > `vi`. Giá trị
  `auto` bỏ qua bước cấu hình, chuyển sang `LC_ALL`/`LANG` rồi tiếng Việt. Trong TUI, phím `L` đổi
  ngôn ngữ ngay trong phiên (không cần khởi động lại) nhưng **chưa lưu** vào cấu hình; màn Hệ thống ›
  Ngôn ngữ để lưu `ui.language` thuộc giai đoạn 5 của kế hoạch TUI. Repository có cấu hình cũ chưa có khối `ui`
  được thêm bằng `bint init --update`.
