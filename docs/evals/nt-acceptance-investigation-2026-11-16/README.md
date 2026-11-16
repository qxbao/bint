# Điều tra NT-MCP-CALLED, NT-CODEX-HOOK, NT-ISO-RO-FP — 2026-11-16

Đối chiếu `docs/acceptance.md`, biên bản V1 ngày 2026-11-16 và mã tại `339f13400f3d3150607c79924ccc8ab952eb71aa`.
Codex thực tế: 0.169.0. Binary `bint` trên PATH: `0.1.0-dev (6c6e530, 2026-11-16T01:00:16Z)`, cũ hơn mã đang đọc.
Đây là điều tra, chưa sửa mã sản phẩm/cấu hình hook, chưa đóng tiêu chí và không ký nghiệm thu. Không gọi mô hình hoặc chạy eval.

## Kết luận

| Mục | Kết luận | Mức chắc chắn |
|---|---|---|
| NT-MCP-CALLED | Thiếu truyền định danh từ app shim tới gateway khiến sự kiện của phiên không có RunID bị bỏ | Xác nhận bằng nguồn, test và bằng chứng V1 |
| NT-ISO-RO-FP | Có ba nguyên nhân: kiểm ký tự không hiểu dấu nháy; thiếu allowlist git ls-files; cấu hình dự án cũ ghi đè default đã có grep | Xác nhận từng mẫu bằng probe |
| NT-CODEX-HOOK | Mã này không có trong acceptance.md/repo hay lịch sử acceptance hiện có. Khảo sát đường hook thấy 4 hook đã trusted và có sự kiện thực; file hook cài trên máy thiếu local_shell | Không xác nhận lỗi hook tổng quát; xác nhận cấu hình cài cũ |

## 1. NT-MCP-CALLED

Chuỗi gọi hiện tại:

1. `internal/cli/mcp.go:82` chỉ đưa RunID/TaskID vào `app.MCPShimOptions`.
2. `internal/app/mcp_shim.go:17` không có đầu vào Harness/SessionID; `NewMCPShim` ở dòng 34–40 chỉ đọc BINT_RUN_ID/BINT_TASK_ID.
3. Tại dòng 71–74, lời gọi `mcpgw.NewShim` không đặt Harness/SessionID mặc dù `internal/mcpgw/shim.go:67` đã có hai trường.
4. `Shim.open` ở `shim.go:642` gửi header từ hai trường rỗng; daemon đọc header ở `internal/mcpgw/http.go:186`.
5. `internal/mcpgw/record.go:29` bỏ sự kiện khi không có RunID và không đủ cặp Harness+SessionID, tăng `DroppedEvents`.

Probe đặt cả CODEX_THREAD_ID, CODEX_SESSION_ID và CLAUDE_CODE_SESSION_ID vào môi trường fixture nhưng bốn trường RunID/TaskID/Harness/SessionID của shim vẫn rỗng. Không có mã đọc các biến phiên này trên đường dựng shim. Đây không phải giả thuyết Codex lọc env: ngay cả env đã có, app cũng không sử dụng.

`TestRecordMCPCalled` và `TestRecordSkips` đều qua: record hoạt động khi nhận RunID hoặc cặp Harness+SessionID từ client thử. Các test này tự truyền header nên không phát hiện thiếu dây nối ở app. `TestNewMCPShimCandidates` chỉ kiểm có shim/service, không kiểm định danh/sự kiện.

Phạm vi: phiên tương tác và các subagent driver session dùng kết nối MCP của phiên cha; không được suy thành mọi lời gọi của process driver đều mất. Process driver đã truyền RunID/TaskID bằng argv và có bản ghi role. Không có RunID thì `ResolveIdentity` ở `internal/mcpgw/binding.go:105` chọn role interactive; thêm Harness/SessionID không tự làm subagent nhận role planner/reviewer.

Hướng sửa: nối định danh phiên thật đã xác minh từ harness vào app → ShimOptions → headers, hoặc thay mô hình ghi sự kiện và khai báo rõ giới hạn theo thiết kế đã chốt. Không tự đặt UUID làm phiên harness và không chỉ xóa điều kiện record: mục 13.2 cấm tạo lượt giả. MCP initialize hiện chỉ đọc protocolVersion (`internal/mcpserver/server.go:287`), không có đường cấp session ID thật từ đó. Cần test xuyên app→shim→gateway→spool cho cả thiếu định danh, interactive và process driver; kiểm riêng role của session driver.

## 2. NT-ISO-RO-FP

| Mẫu V1 | Cấu hình Default hiện tại | Cấu hình dự án hiện tại | Nguyên nhân |
|---|---|---|---|
| `grep -n "a\|b" file` | Chặn | Chặn | readonly.go:144 kiểm `|` trên chuỗi thô, không phân biệt trong dấu nháy |
| `git ls-files internal/mcpcat/probe` | Chặn | Chặn | Chưa có Bash(git ls-files:*) trong allowlist |
| `grep -n "NT-MCP-PROBE" docs/acceptance.md` | Cho phép | Chặn | Default có Bash(grep:*), nhưng .bint/config.yml:171 ghi đè bằng danh sách cũ không có grep |
| `grep -n -E "^(--- FAIL|FAIL|ok|panic)|CopyCodegraph" <task-log>` | Chặn | Chặn | Ký tự pipe trong regex bị coi là toán tử shell; cấu hình dự án còn thiếu grep |
| `grep -n FAIL <task-log>` | Cho phép | Chặn | Thiếu grep trong cấu hình dự án; đường dẫn log ngoài worktree không phải lý do readonlyShellAllowed từ chối |
| `git log | sh` | Chặn | Chặn | Đúng: đây là pipe shell thật |

Nguồn: `internal/harness/hookcore/readonly.go:142–180`, `internal/config/config.go:627`, `internal/config/template.go:193`, `.bint/config.yml:171`.
Bằng chứng V1 là `docs/evals/v1-real-session-2026-11-16/evidence/step2-guardrail-events.txt`. Lệnh reviewer `go test ...` trong tệp đó bị deny đúng theo chính sách bước chỉ đọc; không tính ca này là false positive.

Hướng sửa: tách kiểm toán tử shell ngoài dấu nháy khỏi nội dung literal; vẫn phát hiện command substitution có hiệu lực trong nháy kép, pipe/chuyển hướng thật, heredoc và các cách chạy chương trình qua git grep. Thêm git ls-files vào default/template và có đường nâng cấp cấu hình cũ phù hợp; chỉ sửa Default không giúp repo đang ghi đè danh sách. Test cần cả config.Default và config.Load thực tế, các mẫu V1 và ca cố chạy/ghi thật. Chưa chỉnh danh sách của người dùng trong lượt điều tra.

## 3. Khảo sát Codex hook (NT-CODEX-HOOK)

Không tìm được mã NT-CODEX-HOOK trong repo, `.bint`, hoặc `git log --all -S NT-CODEX-HOOK -- docs/acceptance.md`; chưa có điều kiện đóng riêng để nghiệm thu. Theo yêu cầu người dùng đã khảo sát đường hook hiện có:

- Gọi API metadata `initialize` → `hooks/list` của Codex 0.169.0, không mở thread/chạy model: 4 hook Bint tại `.codex/hooks.json` đều enabled=true, trustStatus=trusted, không có lỗi tải hook.
- Truy vấn SQLite chỉ đọc cho ngày 2026-11-16 thấy 6 loại sự kiện thực từ `source=hook:codex`: session.started, session.ended, context.retrieved, file.modified, test.completed, knowledge.cited. Số liệu là ảnh chụp tại thời điểm điều tra; không kết luận mọi phiên/tool đều được bao phủ.
- `.codex/hooks.json` đã cài thiếu local_shell ở PreToolUse/PostToolUse, trong khi `internal/harness/codex/codex.go:205` đã bổ sung và `Classify` ở dòng 64 nhận tool này. Probe `HooksCurrent(...)` trả false. Test cài hook từ source vẫn qua vì nó dựng tệp mới, không kiểm file người dùng đang cài.
- Matcher đang cài có exec_command/apply_patch/MCP Bint; các tên exec hoặc functions.exec không match và Classify trả ToolUnknown. Đây là khoảng trống tiềm tàng với Code Mode, chưa chứng minh raw hook của Codex thực tế dùng các tên đó: cần payload hook thật để kết luận. Không suy tên hook từ tên tool nhìn thấy trong chat.
- `mcpEvent` tại `internal/harness/hookcore/hook.go:706` chỉ chuyển các tool context/cite/report của Bint thành sự kiện tương ứng; tool chuyển tiếp như cg_explore đi nhánh default dòng 741, cố ý không ghi hook event. Sự kiện mcp.called của tool upstream thuộc gateway, nên mất nó không chứng minh hook Codex không chạy.
- Binary bint trên PATH cũ hơn checkout, và hook gọi tên trần `bint hook codex`. Vì vậy kết quả test source không tự chứng minh binary/hook cài trên máy đã cập nhật. Cần đồng bộ binary và cấu hình bằng quy trình cập nhật hiện có, sau đó kiểm lại metadata/trust vì định nghĩa hook thay đổi có thể cần người duyệt lại.

Để chốt phần này: trước hết đặt điều kiện rõ cho NT-CODEX-HOOK (tool nào, sự kiện nào thiếu), sau đó bắt payload thô bằng BINT_HOOK_TRACE trong phiên thử cô lập. Đối chiếu input→Classify→output→spool và transcript thật, nhất là shell thành công/thất bại, apply_patch, MCP context/cite và Code Mode. Không tự trust hook hoặc suy test pass khi thiếu exit-code evidence.

## Kiểm chứng và giới hạn

Các probe chạy bằng Go overlay nằm ngoài repo, không chèn test vào cây sản phẩm. Bản source trước/sau mỗi lượt được ghi hash trong log. Hai lệnh chọn test hẹp:

- `go test -overlay <overlay.json> -count=1 -timeout 3m -json ./internal/harness/hookcore ./internal/harness/codex -run '^(TestAcceptanceInvestigation.*|TestReadonlyShellAllowed|TestInstalledHooksMatchSupportedShellTools|TestHooksTrustedUsesCurrentCodexDefinition)$'`: exit 0.
- Lệnh tương tự với `./internal/app ./internal/mcpgw`, chọn `TestAcceptanceInvestigation.*|TestNewMCPShimCandidates|TestRecordMCPCalled|TestRecordSkips`: exit 0.

Các probe khẳng định hành vi từ chối/thiếu định danh hiện tại để tái hiện nguyên nhân; PASS của probe không có nghĩa các NT đã được sửa. Mọi test dùng fixture, không gọi gateway/upstream của phiên người dùng. Chạy qua transient user service có RAM tối đa 2 GiB, swap 0, PID tối đa 256, CPU 200%; không chạy full suite và không đo lại lỗi treo host.

Bằng chứng chọn lọc ở `evidence/readonly-codex-probes.txt`, `shim-record-probes.txt`, `codex-hooks-metadata.json`, `codex-hook-event-counts.json`. Mã probe lưu với đuôi `.go.txt`; `<repo>` là placeholder đã thay đường dẫn máy. Log đầy đủ và overlay gốc nằm trong thư mục runtime `nt-acceptance-investigation-20261116` của trạng thái Bint.

Thứ tự đề xuất: sửa NT-MCP-CALLED và xác minh session identity trước M3; sửa parser/allowlist/migration của NT-ISO-RO-FP; đồng bộ binary/hook Codex và thu payload thử để xác định phần NT-CODEX-HOOK còn thiếu. Giữ chữ ký nghiệm thu trống.
