# Nghiệm thu flow hỗn hợp Claude Code ↔ Codex CLI — 2026-11-11

**Kết luận ngắn:**

- **A (Claude lập kế hoạch → Codex thực thi → Claude review): đạt thật**, task `done` có bằng chứng verify, sau khi sửa 3 lỗi chặn/sai dữ liệu.
- **B (Codex lập kế hoạch/review → Claude thực thi): đạt thật** ngay lần chạy đầu trên bản đã sửa.
- **C (đường dẫn bảo vệ → chặn → người duyệt → chạy tiếp): đạt thật.** Hook Bint chặn thật `apply_patch` vào đường dẫn bảo vệ (C3); người dùng duyệt đúng một tệp bằng `task approve --path`; Codex chạy tiếp và tạo tệp (C4). Với `b-do`, `run resume` (áp lại cờ role) chạy lại đúng harness Codex, chỉ làm phần còn thiếu (C2) và sau bản sửa `dd2b88a` task **tới `done`** (C5): Codex không làm lại việc đã xong, chỉ ghi lại các tiêu chí; Claude review; `finalize` đóng task. Lỗi tiêu chí cũ ở C2 được tái hiện, sửa và xác nhận bằng chính task đó.
- **Các phép thử bắt buộc A, B, C đều chạy được với Claude/Codex thật và đạt**, với các giới hạn ở mục [Còn blocked](#còn-blocked-và-điều-kiện-thủ-công): chuỗi hook-deny → duyệt → chạy tiếp và chuỗi duyệt → resume → `done` được chứng minh trên **hai task khác nhau** (C3/C4 và C1/C2/C5), chưa có một task duy nhất đi hết cả hai. Phiên bản/đăng nhập khác máy trước (Claude 2.1.322, Codex đăng nhập API key) nên kết luận chỉ áp cho cấu hình đã thử.

Đọc cùng [đợt 2026-11-10](../codex-flows-2026-11-10/README.md) và [đợt 2026-11-11](../codex-flows-2026-11-11/README.md). Đợt này chỉ tập trung phần hỗn hợp, không chạy lại F1–F7.

Nhãn: **[đã kiểm chứng thật]** quan sát trực tiếp ở Claude/Codex CLI thật; **[suy luận từ code]** đọc mã/unit test; **[không kiểm được + lý do]**.

## Thiết lập và phiên bản

| Mục | Đợt trước (máy cũ) | Đợt này | Ghi chú |
|---|---|---|---|
| Claude Code | `2.1.302`, `loggedIn: false` | **`2.1.322`**, đăng nhập `claude.ai`, gói pro | **Khác phiên bản.** Mọi bằng chứng Claude chỉ áp cho 2.1.322, không suy sang 2.1.302 hay phiên bản khác. Model quan sát: `claude-opus-5-5` (bước workflow), haiku (2 phiên chẩn đoán). |
| Codex CLI | `0.167.0`, đăng nhập ChatGPT, `gpt-6.1-sol` | `0.167.0`, đăng nhập bằng **API key** qua provider tuỳ chỉnh, hook báo model `gpt-6-luna` | Cùng phiên bản CLI nhưng **khác kiểu đăng nhập/model**: không nâng nhãn cho đăng nhập ChatGPT hay model cũ. Giá trị key/endpoint không được ghi. |
| Bint | `9a688b3`… | build từ nhánh `fix/claude-codex-mixed` (từ `origin/duc` `e3df292`, đã chứa `2b23472`) | Binary build ra đường dẫn tuyệt đối trong thư mục tạm; hướng dẫn dùng đúng binary đặt trong `AGENTS.md` của repo thử. Bản trên PATH là bản cũ. |
| Repo thử | `/probe/repo` | Git repo tạm có package `calc` (`Add`, `Negate`) và 3 test; không chạy workflow trên repo Bint | `eval.test_command = "go test ./..."` điền và xác nhận trước khi chạy ([doctor](evidence/runs-output/doctor.txt)). |
| Sandbox | | Không bỏ sandbox/approval toàn cục; không `--agent-command`, không agent giả | Codex `workspace-write`; Claude `acceptEdits` + allowlist của Bint. |
| Hook/MCP/skill | | Cả hai harness bật, cài trong repo thử | Claude: `.claude/settings.local.json`, `.mcp.json`. Codex: `.codex/hooks.json`, `.codex/config.toml`. Skill: `.claude/skills`, `.agents/skills`. |
| Trust Codex | | `CODEX_HOME` tạm (chỉ chép auth 0600 và phần provider), trust project + 4 hook duyệt qua app-server bằng hash Codex trả ([rpc](evidence/runs-output/codex-trust-rpc.jsonl)) | Không dùng cờ bypass hook trust. |
| Thay đổi cấu hình người dùng | | **Không có** | `~/.codex/config.toml` mtime 9/11 và `auth.json` có trước khi bắt đầu; `~/.claude.json` không có mục trust cho repo thử. Không logout/đổi tài khoản. Đã xoá auth/config tạm khi kết thúc phần chạy. |

**Điều kiện môi trường ảnh hưởng kết quả (không sửa):** hook/CLAUDE.md toàn cục của người dùng vẫn nạp vào các phiên Claude (`workflow.agent.setting_sources` mặc định rỗng). Hệ quả quan sát được: Claude tự viết lệnh `rtk git …`, `rtk go test …`, và các lệnh ghép dạng đó bị allowlist chỉ đọc từ chối (xem [A1](evidence/logs/)). Với Codex, shell sandbox không ghi được cache Go mặc định nên `go test` đầu tiên luôn lỗi `read-only file system`, agent tự chuyển `GOCACHE` sang `/tmp` (có ghi `bint task error`).

## Ma trận A/B/C

| | Task ID | Chuỗi harness thực tế (từ lệnh + `runs.harness`) | Kết quả | Nhãn |
|---|---|---|---|---|
| **A** `b-do --plan --harness claude --role implementer=codex` | `01M74AT2NVMZBMRB69MH92H6TH` | b-req `claude` → b-feat `codex` → b-review `claude` | `done` ([A.json](evidence/runs-output/A.json)) | [đã kiểm chứng thật] |
| **B** `b-do --plan --harness codex --role implementer=claude` | `01M74B6HCGKJC5DJNMD0Y39X76` | b-req `codex` → b-feat `claude` → b-review `codex` | `done` ([B.json](evidence/runs-output/B.json)) | [đã kiểm chứng thật] |
| **C1** `b-do --no-plan --harness claude --role implementer=codex` (Sub + migration) | `01M74BFD9EM8XEXRVQRW4EQ3XZ` | b-feat `codex` → b-review `claude` | Codex làm phần Go, **tự dừng** trước migration; task `blocked`, tiêu chí 2 `unmet` | [đã kiểm chứng thật] |
| **C2** người duyệt `--path` rồi `run resume … --harness claude --role implementer=codex` | cùng task C | b-feat `codex` (chỉ tạo migration) → b-review `claude` | Migration tạo, test qua; task **vẫn `blocked`** (tiêu chí 2 chưa được ghi lại) | [đã kiểm chứng thật] |
| **C3** hook chặn | `01M74C22PYS7WAC7NZJP89RY3W` | b-feat `codex` | Hook Bint chặn `apply_patch` vào `.github/workflows/ci.yml`, không có tệp/diff; Codex báo chặn và dừng | [đã kiểm chứng thật] |
| **C4** người duyệt `--path .github/workflows/ci.yml` rồi `run b-feat --task … --harness codex` | cùng task C3 | b-feat `codex` (run mới, `parent_run_id` = run b-feat bị chặn) | Hook cho qua đúng tệp đã duyệt, Codex tạo `ci.yml` (1 dòng `name: ci`), sự kiện `file.modified` do `hook:codex` ghi; task `in_review`, `protected_approval` = đúng một tệp. Task đang `in_review` (không paused) nên **dùng `run b-feat --task`, không phải `run resume`** | [đã kiểm chứng thật] |
| **C5** người duyệt lại `--path` (đặt `resume_from = in_progress`), rồi `run resume … --harness claude --role implementer=codex` trên bản có `dd2b88a` | cùng task C (`01M74BFD9EM8XEXRVQRW4EQ3XZ`) | b-feat `codex` → b-review `claude` | **`done`.** Codex không sửa tệp (không có `file_change`), chạy `go test` qua và gọi `bint task criteria` cho cả hai tiêu chí; verify `go test ./...` qua; review vòng 3 chỉ có phát hiện mức `risk/low–medium` và gợi ý ([C5.json](evidence/runs-output/C5.json)) | [đã kiểm chứng thật] |

### Kiểm tra theo đề bài (A/B/C)

| Yêu cầu | Kết quả | Nhãn / bằng chứng |
|---|---|---|
| Harness thực tế từng bước khớp role, không chỉ tin "done" | Khớp ở mọi bước: dòng lệnh đầu mỗi log là `claude -p …` hoặc `codex exec …`, trùng cột `harness` của run và nguồn `hook:claude`/`hook:codex` của sự kiện; không có sự kiện hook sai harness | [đã kiểm chứng thật] [runs.json](evidence/runs.json), [logs](evidence/logs/), [events.json](evidence/events.json) |
| Task ID, run cha/con, harness liên kết đúng | Run `b-do` là cha của từng bước; mỗi bước mang `task_id` và `parent_run_id` | [đã kiểm chứng thật] [runs.json](evidence/runs.json) |
| Tài liệu chuyển giao (requirement/plan/tasks) | Claude (A) hoặc Codex (B) ghi đủ 3 tài liệu qua `bint task write`; implementer làm đúng tên test/hàm/chỗ đặt theo kế hoạch (`TestAbsZero/Negative/Positive`, `TestMax…`); Codex log có đọc skill, `bint task show`, `bint context` | [đã kiểm chứng thật] [A](evidence/tasks/01M74AT2NVMZBMRB69MH92H6TH/), [B](evidence/tasks/01M74B6HCGKJC5DJNMD0Y39X76/) |
| Tiêu chí, bằng chứng, review có cấu trúc | `criteria` có `met` + bằng chứng; `review.json` có phát hiện đánh ID `<task>.<n>`, `kind/severity/path/lines/evidence` | [đã kiểm chứng thật] `state.json`, `review.json` của từng task |
| `done` chỉ khi có bằng chứng verify hợp lệ | A, B: `verifications` có `go test ./...` `confirmed:true`, `exit_code:0` (khớp `eval.test_command` đã xác nhận). C: giữ `blocked` vì tiêu chí chưa đạt | [đã kiểm chứng thật] |
| Planner/reviewer không sửa source; Bint phát hiện nếu có | 9 bước chỉ đọc (b-req/b-review) đều qua hậu kiểm snapshot cây làm việc, không có vi phạm. **Chưa cố ý tạo vi phạm bằng Claude/Codex thật ở đợt này** | Không vi phạm: [đã kiểm chứng thật]. Cơ chế phát hiện: [suy luận từ code] `TestReadOnlyStepModifyingTreeFails` (agent giả); đợt trước đã inject canary vào Codex thật (r08) |
| Token/cost thiếu = null/unknown | Codex: `cost_usd` từng bước `null`, token có. **Phát hiện lỗi**: tổng b-do ghi tổng một phần như tổng thật → đã sửa `ebb11d1`; C1/C2 thật cho `cost_usd: null` + `cost_usd_partial` | [đã kiểm chứng thật] trước và sau sửa |
| Không tự duyệt tri thức | `knowledge_index`, `observations`, `knowledge_citations` đều 0 dòng; không mục tri thức nào được tạo | [đã kiểm chứng thật] |
| Không mất/nhân đôi sự kiện do hai bộ hook | 203 sự kiện / 203 `event_id` khác nhau; mỗi harness chỉ nạp bộ hook của chính nó (Claude không đọc `.codex/hooks.json` và ngược lại) | [đã kiểm chứng thật] cho tập đã quan sát. Mất sự kiện khi hook timeout: không thử ở đợt này |
| Không có hướng dẫn riêng harness khiến harness kia gọi tool không tồn tại/lặp | Log Codex không có `AskUserQuestion/TodoWrite/Skill tool`; không lỗi "tool not found". Đoạn mới thêm vào skill b-review (YAML) viết trung tính, không nêu tên harness | [đã kiểm chứng thật] (grep log) |
| Agent không tự vượt cổng duyệt | Người dùng tự chạy `task approve` ở terminal riêng (`protected_approval` ghi lúc `03:54:18Z` cho C và `04:12:59Z` cho C3, mỗi lần đúng một tệp; lần đầu người dùng chạy nhầm bản `bint` cũ trên PATH nên lệnh lỗi `unknown flag: --path`, đã chạy lại bằng binary tuyệt đối). Khi agent (tôi) định thử `approve`, **bộ phân loại quyền của Claude Code chặn trước**, nên thông báo `ErrAgentForbidden` của Bint không được quan sát ở phiên này | Cổng của người dùng: [đã kiểm chứng thật]. `ErrAgentForbidden` của Bint: [suy luận từ code + test có sẵn] |

## Lỗi tìm thấy và đã sửa (mỗi lỗi một commit, có test hồi quy)

| Commit | Tái hiện (thật) → nguyên nhân → sửa |
|---|---|
| `fcbe5dd` fix(harness): cho bước chỉ đọc của Claude gọi Bint bằng đường dẫn tuyệt đối | **Chặn flow.** A lần 1: b-req của Claude chạy 20 s, `permission_denials` cho mọi `bint context/task show`, không ghi tài liệu, task `failed` ([A0](evidence/runs-output/A0/)). Nguyên nhân: allowlist chỉ đọc mặc định `Bash(bint:*)` là quy tắc tiền tố, không khớp `<đường dẫn tuyệt đối>/bint` (cách gọi mà repo được hướng dẫn dùng khi bản trên PATH cũ). Tái hiện độc lập: `bint --version` qua PATH chạy được, đường dẫn tuyệt đối bị từ chối ([H1](evidence/diagnostics/H1-claude.json)). Sửa: thêm quy tắc cho `os.Executable()` khi người dùng còn giữ rule `bint` mặc định; b-feat/cấu hình gốc không đổi. Hồi quy dùng `permission_denials` thật ([fixture](../../../internal/harness/testdata/claude-2.1.322/permission-denied-absolute-bint.json)). |
| `d96cd0c` fix(task): nhận review dạng YAML khi Claude Code chặn heredoc JSON | **Chặn flow.** A lần 2: Claude review xong nhưng không ghi được, task `failed` "b-review không ghi kết quả" ([A1](evidence/runs-output/A1/)). Nguyên nhân: cách ghi review mà skill hướng dẫn (`bint task review <id> - <<'EOF' {JSON}`) bị Claude Code 2.1.322 chặn bằng kiểm tra an toàn có sẵn, thông báo nguyên văn `Contains brace with quote character (expansion obfuscation)` ([H2](evidence/diagnostics/H2.jsonl)); không phải rule allowlist. Sửa: `bint task review` nhận thêm YAML block (không ngoặc nhọn) khi đầu vào không bắt đầu bằng `{`; JSON giữ nguyên; skill b-review nêu dạng YAML khi shell từ chối. Hồi quy dùng payload review thật bị chặn + bản YAML tương đương ([fixture](../../../internal/task/testdata/claude-2.1.322/)). |
| `f18dfc1` fix(hook): không ghi lệnh gọi CLI của Bint thành lần chạy test | **Sai dữ liệu.** b-req chỉ đọc của Claude chỉ chạy `bint task write …`, nhưng DB có `test.completed ok=true` ×3 và tín hiệu `tests positive` ([events A](evidence/events.json), run `01M74AT2PHFAF3WXNXR650RT8Q`): nội dung tài liệu nhắc `go test ./...` khớp `eval.test_command`. Cùng loại lỗi đợt trước đã sửa cho Codex nhưng chưa cho Claude. Sửa: lệnh có chương trình là `bint` không bao giờ là test, ở `hookcore` (mọi harness; Codex vốn đã không nhận). Lệnh test thật (`go test ./...`, `cd x && go test \| tail`) vẫn ghi. Sau sửa, B không còn sự kiện test giả ở b-req/b-review. Giới hạn: `bint x && go test` cũng bị bỏ qua. Lịch sử A trước sửa giữ nguyên làm bằng chứng. |
| `ebb11d1` fix(workflow): không trình bày tổng chi phí một phần của b-do như tổng thật | **Sai dữ liệu.** Khi một bước Codex không báo chi phí, `cost_usd` của b-do là tổng các bước còn lại (A: `0.6006`, B: `0.2612`, cả hai thiếu một bước). Sửa: `cost_usd` chỉ có khi mọi bước báo; thiếu thì `null` và phần đã biết ở `cost_usd_partial` (JSON kết quả và `workflow.finished`). Giới hạn `max_cost_usd` vẫn đếm phần đã biết. Sau sửa C1/C2 thật cho `cost_usd: null`, `cost_usd_partial`. |
| `dd2b88a` fix(workflow): nhắc b-feat chạy lại ghi lại các tiêu chí còn unmet | **Chặn flow (xác nhận ở C2 → C5).** Sau duyệt và resume, Codex tạo migration, test qua nhưng không gọi `bint task criteria` (hiểu rộng "không gọi `bint task status/approve/verify`"), tiêu chí 2 giữ bằng chứng cũ "chưa tạo", task kẹt `blocked`. Sửa: prompt b-feat chạy lại liệt kê tiêu chí còn `unmet` kèm bằng chứng cũ, dặn làm phần còn thiếu và ghi lại bằng `bint task criteria` (lệnh này không bị cấm); lượt đầu không đổi. Hồi quy `TestFeatPromptListsUnmetCriteriaOnRerun` (agent giả, kiểm prompt). **Xác nhận bằng Codex thật ở C5:** cùng task đó đi tới `done`. Một mẫu thật, không phải thống kê. |

Không sửa gate, ngưỡng, oracle hay test hiện có; không nhân đôi skill core; hành vi mặc định của hai adapter giữ nguyên (chỉ thêm quy tắc/định dạng chấp nhận). Không merge, không push.

## Các bản sửa đã có ở `main` chưa?

**Chưa.** `main` = `origin/main` = `7dd832f`, đã rẽ nhánh khỏi `duc` (35 commit riêng so với 21). Đọc mã ở `main` (**[suy luận từ code]**, không chạy binary `main`): không có `withBintExecutableRule`, `decodeSubmission`, `IsBintInvocation`, `cost_usd_partial`, và prompt b-feat không nhắc tiêu chí `unmet`. Năm bản sửa của đợt này chỉ nằm ở `fix/claude-codex-mixed`; chưa merge, chưa push.

## Phát hiện mở (chưa sửa)

1. **[Đã sửa bằng `dd2b88a`, xác nhận ở C5]** Tiêu chí còn bằng chứng cũ sau resume. Giữ lại để truy vết: nguyên nhân là Codex hiểu rộng câu cấm trong prompt; sau khi prompt nêu rõ tiêu chí `unmet`, Codex ghi lại và task `done`. Chỉ một mẫu Codex thật; chưa đo tỷ lệ.
2. **Role/harness không được lưu theo task** — [suy luận từ code]. Role chỉ đến từ `--harness/--role` của lần chạy (`internal/cli/run.go:117`), `state.json` không lưu. Vì vậy `run resume` **bắt buộc áp lại cờ**; nếu bỏ cờ sẽ rơi về `roles:` trong cấu hình (ở đây là `claude`), tức đổi harness của b-feat âm thầm. Đợt này resume luôn áp lại cờ (C2 đúng). **Chưa chạy resume không cờ bằng Claude/Codex thật** để xác nhận hành vi đổi harness.
3. **Hook deny không được ghi thành trạng thái** — [đã kiểm chứng thật]. C3 chạy `b-feat` đơn lẻ: Codex bị chặn, không diff, nhưng task sang `in_review` ("b-feat xong"). Đợt trước task `blocked` là nhờ bước finalize của `b-do` (tiêu chí/verify chưa đạt), không phải do chính lần chặn. Sau khi duyệt, C4 chạy tiếp được bằng `run b-feat --task` dù không paused. Sửa cần lưu sự kiện deny (thay đổi thiết kế), nên không làm ở đây.
4. **Nhận diện test của Claude vẫn rộng** — [đã kiểm chứng thật]. B ghi `test.completed ok=true` cho một script nhiều lệnh có `| cut` (hook PostToolUse chỉ báo thành công). Giữ hành vi mặc định Claude theo đề bài; không nâng thành kết luận "mọi test Claude đều đúng".
5. **Cô lập nhiễu từ cấu hình người dùng** — Claude nạp hook/CLAUDE.md toàn cục (xem trên); có thể ảnh hưởng lệnh agent tự viết. Không đổi mặc định.

## Số session thật và lý do vượt mục tiêu

**20 phiên model thật** (mục tiêu 8–12): 10 phiên workflow trong A0/A1/A/B (1+3+3+3), 8 phiên C (C1 2, C2 2, C3 1, C4 1, C5 2), cộng 2 phiên haiku chẩn đoán (H1, H2) và 1 lần gọi không đầu vào (lỗi trước khi gọi model, không tính). Vượt vì hai lỗi chặn (`fcbe5dd`, `d96cd0c`) buộc chạy lại A hai lần sau khi sửa; A0/A1 giữ lại làm bằng chứng. Chi phí Claude từng bước nằm trong các `*.json` (USD thật); Codex chỉ có token, không có USD, nên **không cộng tổng**.

## Còn blocked và điều kiện thủ công

- **Chưa có một task duy nhất** đi hết hook-deny → duyệt → `done` (C3/C4 là `b-feat` đơn lẻ dừng ở `in_review`; C1/C2/C5 là `b-do` nhưng agent tự dừng trước migration chứ hook chưa chặn trên task đó). Muốn đóng hẳn: một task `b-do` có đường dẫn bảo vệ mà hook chặn thật rồi `approve --path` → `resume` → `done`. Lệnh duyệt luôn do **người dùng** chạy ở terminal thường, với binary tuyệt đối và `XDG_STATE_HOME` của repo thử (bản `bint` trên PATH là bản cũ, không có `--path`).
- **Duyệt qua UI `/hooks`** chưa thao tác (dùng API app-server như đợt trước).
- **Windows/macOS** không thử; chỉ `GOOS=windows go vet` qua.
- **Resume không cờ** và **vi phạm readonly cố ý bằng Claude thật**: chưa thử (xem trên).
- **Claude 2.1.302** và **Codex đăng nhập ChatGPT** của đợt trước không được thử lại ở đợt này.
- Chỉ thử **1 tác vụ Go nhỏ mỗi hướng**; không suy ra hiệu quả trên repo lớn.

## Kiểm tra

Chạy ở worktree `fix/claude-codex-mixed` sau commit cuối của lỗi, mã thoát đều **0**:

| Lệnh | Đầu ra |
|---|---|
| `gofmt -l .` | [gofmt.txt](evidence/checks/gofmt.txt) (rỗng) |
| `go vet ./...` | [vet.txt](evidence/checks/vet.txt) (rỗng); `GOOS=windows go vet ./...`: [vet-windows.txt](evidence/checks/vet-windows.txt) (rỗng) |
| `go test -count=1 ./...` | [go-test.txt](evidence/checks/go-test.txt): 23 gói `ok`, 0 `FAIL` |
| `go test -race ./internal/harness/... ./internal/app/... ./internal/eval/...` | [go-test-race.txt](evidence/checks/go-test-race.txt) |

Fixture/test hồi quy: `internal/harness/testdata/claude-2.1.322/`, `internal/task/testdata/claude-2.1.322/`, `internal/harness/claude/testdata/claude-2.1.322/` (tiền tố lệnh thật + `requirement.md` thật; toàn văn lệnh gốc không được lưu), và test mới ở `internal/harness/harness_test.go`, `internal/task/task_test.go`, `internal/app/multiharness_test.go`, `internal/detect/template_test.go`, `internal/workflow/run_test.go`.

## Bằng chứng (`evidence/`, đã che)

Đường dẫn → `<repo>`, `<state>`, `<BINT>`, `<scratch>`; email, URL, key, tên máy, session/thread ID bị bỏ hoặc che. Không có auth/config/provider values.

- `runs-output/`: lệnh, JSON kết quả và stderr của A0, A1, A, B, C1, C2, C3, C4, C5; `versions.txt`, `doctor.txt`, `init.txt`, trust RPC.
- `tasks/<id>/`: `task.yml`, `state.json`, `requirement/plan/tasks.md`, `review.json/.md` của mọi task.
- `logs/`: log từng bước (dòng lệnh thật + JSON/JSONL của Claude/Codex).
- `runs.json`, `events.json`: bảng run và 203 sự kiện (đã bỏ session/transcript/cwd).
- `diagnostics/`: H1 (đường dẫn tuyệt đối vs PATH), H2 (thông báo brace-with-quote nguyên văn).
- `checks/`: đầu ra các lệnh kiểm tra.
