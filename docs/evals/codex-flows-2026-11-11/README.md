# Nghiệm thu tiếp các flow Codex — 2026-11-11

> **Bản ghi lịch sử:** Báo cáo dưới đây phản ánh số liệu và trạng thái nhánh tại thời điểm chạy đợt đo. Tổng hợp hiện trạng tại `HEAD` xem ở [docs/status.md](../../status.md) và [docs/evals/README.md](../README.md).

**Kết luận:** đã kiểm tra tiếp toàn bộ nhóm F1–F7, hoàn thành các phép thử Codex còn mở của đợt trước và sửa thêm các lỗi chặn flow/sai dữ liệu. **Flow hỗn hợp Claude↔Codex bị chặn do Claude chưa đăng nhập**, được người dùng chọn ghi nhận thay vì đăng nhập. Không kết luận flow này đã đạt. Các giới hạn chưa thể khẳng định bằng phép thử hữu hạn được ghi ở cuối.

Đọc cùng [biên bản đợt đầu](../codex-flows-2026-11-10/README.md). Đợt đầu dùng 15 session; đợt tiếp theo này thêm **15 session** (12 hoàn tất, 1 bị Ctrl-C, 1 timeout, 1 trả 401 trước khi sinh kết quả). Không có lời gọi `--agent-command`, không dùng agent giả để kết luận hành vi Codex. API metadata/debug không chạy model, không tính vào 15 session.

## Thiết lập

- Bint đầu đợt: `9a688b3`, tiếp tục nhánh `fix/codex-flows`; không merge/push. Mỗi lỗi một commit tiếng Việt.
- Codex `0.167.0`, `gpt-6.1-sol`, effort low, đăng nhập ChatGPT; Claude `2.1.302`, `loggedIn: false` ([trạng thái](evidence/claude-auth-status.json)).
- Git repo tạm `/probe/repo` có package Go nhỏ Add/Negate; eval dùng `/probe/eval-project`, case cộng số đã được kiểm chứng fail→pass ở đợt trước.
- Cấu hình thử ở `<repo>/.codex/profile`, dùng `CODEX_HOME` riêng và `XDG_STATE_HOME=/probe/state`. Không thay HOME của phiên người dùng, không sửa cấu hình/auth Codex thật.
- Chỉ sao chép tệp auth vào profile thử mode 0600 và phần cấu hình runtime/provider cần để dùng đăng nhập hiện có. Không đưa auth/config/provider values vào artifact. Đã xoá auth/config riêng khi kết thúc; profile eval xoá tự động theo từng lượt ([cleanup](evidence/credential-cleanup.json)).
- `workspace-write`, approval `never`. Các session tương tác với hook đợt hai dùng trust hash do Codex trả, không cờ bypass. Eval giữ lựa chọn bypass hook đã kiểm tra của fixture cũ, adapter không tự thêm cờ đó.
- [Manifest 15 session](evidence/sessions.json), [kế hoạch](PLAN.md), [quy ước evidence](evidence/README.md).

Nhãn **[đã kiểm chứng thật]**: hành vi của CLI/app-server/payload/transcript thật; phép metadata không gọi model được chỉ rõ. **[suy luận từ code]**: source/unit test, không nâng thành runtime thật. **[không kiểm được + lý do]**: điều kiện bên ngoài hoặc phạm vi chưa quan sát.

## Kết quả theo flow

| Flow | Kết quả và bằng chứng |
|---|---|
| **F1 — trust** | **[đã kiểm chứng thật, API metadata]** `hooks/list` trả key/hash/trustStatus của từng định nghĩa. Trước sửa, doctor báo ✓ ở trạng thái chỉ trust 1/4 hoặc hook modified. Sau sửa, partial/modified/disabled đều warn, đủ trust mới ✓. Project untrusted làm hook cấp project biến mất; doctor không báo sẵn sàng giả. [partial trước](evidence/doctor-partial-before-fix.txt), [modified trước](evidence/doctor-modified-before-fix.txt), [partial sau](evidence/doctor-partial-after-fix.txt), [modified sau](evidence/doctor-modified-after-fix.txt), [disabled](evidence/doctor-disabled-after-fix.txt), [project untrusted](evidence/doctor-project-untrusted.txt). |
| **F1 — scope** | **[đã kiểm chứng thật]** user scope cài vào home Codex tạm, đủ 4 hook; init lần hai không đổi byte; gỡ Bint giữ nguyên hook foreign. Codex nạp hook `source=user`, cùng các hook project; session r02 chạy với cả hai scope. Local/project đã qua đợt trước. [summary](evidence/user-scope-summary.json), [hooks](evidence/hooks-user-scope.json), [init](evidence/init-user-scope.txt), [gỡ](evidence/uninstall-user-scope.txt). |
| **F1 — onboarding** | **[đã kiểm chứng thật trước sửa + suy luận từ code/test sau sửa]** init chỉ bật Codex vẫn gợi ý `/b-bootstrap trong Claude Code`; đã chuyển thành `$b-bootstrap trong Codex`, giữ nguyên hướng dẫn Claude mặc định. Trust qua `/hooks` vẫn là bước của người dùng; phép thử dùng API config/state trên profile riêng để kiểm hash, chưa thao tác màn hình `/hooks`. |
| **F2 — fail-open** | **[đã kiểm chứng thật]** r01: PreToolUse Bash có ba handler riêng: exit 1, JSON hỏng, sleep 3 s với timeout 1 s. Cả hai shell test vẫn chạy: test cố ý fail rồi test bình thường qua. Có 6 stdin handler thực, không suy từ source. Model không nhắc hook errors, stderr chỉ có cảnh báo alias `/tmp`; không bịa rằng người dùng thấy thông báo lỗi hook. [định nghĩa](evidence/nonfatal-hook-definitions.json), [payload](evidence/hook-failure-payloads.json), [session](evidence/r01-nonfatal.jsonl). |
| **F2 — tính đúng dữ liệu** | **[đã kiểm chứng thật]** Codex PostToolUse gửi stdout cho shell fail và pass, không gửi exit_code; Bint cũ tính 2 test qua, 0 fail. Transcript có `item_completed/CommandExecution` cùng tool_use_id, exit 1/0 và đã có mặt khi hook chạy (recorder của r07). Sau sửa dùng exit code thật; thiếu transcript/ID là unknown, không sinh tín hiệu pass/fail. R08 thật ghi `tests_failed: 1` ([runs](evidence/runs-after-second-round.json)); fixture mới kiểm cả fail/pass/missing. |
| **F2 — test giả do tài liệu** | **[đã kiểm chứng thật đầu vào + test hồi quy]** r02 chỉ viết tài liệu heredoc chứa chuỗi `go test`, vẫn bị Bint cũ coi là test đã qua. Bản sửa Codex chỉ nhận một lệnh test trực tiếp, không heredoc, background, pipe, short-circuit hoặc nhiều lệnh; không áp nhận diện mới cho Claude. Script phức tạp vẫn có file.modified qua snapshot nếu có, nhưng không được nâng thành tín hiệu test. |
| **F3 — MCP** | **[đã kiểm chứng thật]** eval bint gọi mcp:task, mcp:paths, cite và nhận hook:path sau khi tách context user; citation policy riêng giữ hiệu lực. [context log](evidence/eval-c7f6ba68ccf02-bint-1-context.jsonl). **[suy luận có giới hạn]** model có chỉ dẫn trong AGENTS/skill nhưng không được prompt giải pháp trực tiếp yêu cầu gọi tool cụ thể; không lấy một mẫu làm tỷ lệ tự kích hoạt đại diện. |
| **F4 — đủ 5 skill** | **[đã kiểm chứng thật]** r02 `$b-req`: tạo phiếu planned, đủ requirement/plan/tasks, không sửa source. r03 `$b-feat`: implement Negate, test qua, ghi tiêu chí, in_review. r04 prompt không gọi tên b-review: model đọc `.agents/skills/b-review/SKILL.md`, ghi review không sửa source. r05 `$b-bootstrap`: ghi 1 warning candidate có Makefile evidence, verify không lỗi, không tự approve/publish. r06 `$b-do`: cùng task ID, bỏ planning cho việc nhỏ, implement/review/verify và done; không chạy model lồng. [các lệnh đọc skill thật](evidence/skill-invocation-evidence.json), các `r02`–`r06` JSONL. |
| **F4 — policy/event** | **[đã kiểm chứng thật]** đặt b-review `agents/openai.yaml policy.allow_implicit_invocation: false`; r07 `$b-review` vẫn đọc và chạy skill. Metadata đợt trước chứng minh bị bỏ khỏi catalogue tự chọn; đợt này chứng minh gọi tường minh vẫn hoạt động. Không có native Skill tool/event trong payload đã thu, không tổng hợp `skill.activated` từ hành vi đọc file. **[không kiểm được + không chứng minh bằng một mẫu]** bảo đảm mọi prompt/description đều tự chọn đúng skill hoặc không tự chọn khi policy=false; không tuyên bố điều đó. |
| **F5 — readonly** | **[đã kiểm chứng thật]** r08 `bint run b-review`: hook thử được trust ghi canary vào worktree trong lúc Codex chạy. Hậu kiểm Bint phát hiện `readonly-canary.txt`, step/task failed, không tự hoàn tác. Đây là fault injection bên ngoài nội dung code để kiểm ranh giới programmatic, không phải khẳng định model tự ý phá readonly. [kết quả](evidence/r08-readonly.json), [stdin hook](evidence/readonly-hook-stdin.json). |
| **F5 — timeout** | **[đã kiểm chứng thật]** r09 b-feat chạy sleep 90 trước thao tác; workflow timeout 1 phút cho step/task failed, duration 1m0s và lý do dùng được. [result](evidence/r09-timeout-core.json), [control](evidence/r09-timeout-core-control.json). Không hoàn thành hoặc tự coi timeout là pass. |
| **F5 — Ctrl-C/resume** | **[đã kiểm chứng thật]** r10 b-req đang chạy sleep 91; host gửi SIGINT, task interrupted, không còn tiến trình sleep đã quan sát. `run resume` ban đầu lỗi trước khi gọi model vì kiểm b-req trên status interrupted. Sau sửa r12 resume về created, chạy b-req thật 45 s, kết thúc planned, giữ liên kết task/run. [interrupt](evidence/r10-interrupt-core.json), [control](evidence/r10-interrupt-core-control.json), [lỗi trước](evidence/r11-resume.stderr), [sau sửa](evidence/r12-resume.json). |
| **F5 — chưa đăng nhập** | **[đã kiểm chứng thật]** profile Codex rỗng, không auth: HTTP 401 `Missing bearer or basic authentication in header`, step/task failed, lỗi nguyên văn; không logout tài khoản thật. [result](evidence/no-auth.json). Binary thiếu/exit 2 đã kiểm đợt trước; không diễn giải 401 là hết quota. |
| **F5 — role/mixed** | **[không kiểm được + Claude CLI loggedIn=false]** người dùng chọn ghi blocked, không tự login và không giả agent Claude. Source/test chọn role và chuyển giao đã có nhưng chưa thay thế nghiệm thu hỗn hợp thật. |
| **F6 — strict isolate** | **[đã kiểm chứng thật]** profile nguồn có canary AGENTS và skill riêng, positive control thấy cả hai. Ba eval none/minimal/bint thật đều không nhận hai canary, đăng nhập ChatGPT còn dùng được, auth 0600, profile không phải source home và được xoá sau lượt. Đối chiếu metadata CLI riêng trên retained workdir không thấy skill khả dụng (null = danh sách rỗng, không phải chưa đo). Git không có remote/commit lời giải. [positive control](evidence/source-profile-control.json), [catalogue recheck](evidence/profile-catalogue-recheck.json), các `profile-observed`/`codex-profile` JSON, [Git](evidence/eval-isolation-git.json). |
| **F6 — kết quả** | **[đã kiểm chứng thật]** 1 case × 1 repeat × 3 arms, strict: **3/3 passed**, none 22 s, bint 22 s, minimal 20 s. Bint vẫn có mcp/citation/path hook. [run](evidence/eval-run.txt), [report](evidence/eval-report.json). Smoke cơ chế, không phải nghiệm thu hiệu quả/gate. |
| **F7 — context** | **[đã kiểm chứng thật]** đợt trước đo giới hạn AGENTS 32 KiB và marker đầy đủ của tệp nhỏ bằng prompt-input. Đợt này tách user AGENTS/skill, vẫn đưa context theo từng arm đúng ý định. **[suy luận từ code]** không thêm body skill riêng Codex hoặc nhân đôi core; chỉ sửa onboarding/adapter. |

## Phát hiện và commit sửa

| Commit | Vấn đề, mức độ và thay đổi |
|---|---|
| `39cf3ac` | **Chặn/khai báo sai**: doctor dùng dấu vết ít nhất một trust entry cũ. Đổi sang Codex `hooks/list` chỉ đọc, mọi hook Bint hiện hành phải trusted/managed và enabled; API không dùng được thì warn chưa xác minh, không báo ✓. Không tự duyệt. |
| `e67778c` | **Sai dữ liệu mạnh**: PostToolUse shell fail bị coi pass. Đọc tail transcript tối đa 8 MiB theo tool_use_id, yêu cầu item_completed + exit_code; thiếu là unknown. Fixture fail/pass/missing. |
| `9148207`, `320f4ec`, `9dd67ad` | **Sai dữ liệu**: văn bản test trong heredoc/đối số hoặc nhánh shell không chạy test bị coi test pass. Riêng dialect Codex dùng nhận diện bảo thủ lệnh trực tiếp và giữ nguyên token quoted trong regex test. Review tìm thêm short-circuit/comment/background; đã sửa và test thực thi shell cục bộ, không dùng model giả để kết luận Codex. |
| `74fffb2` | **Chặn resume**: planner interrupted bị từ chối trước bước khôi phục. Kiểm trạng thái đích resume đã lưu, sau đó chuyển trạng thái theo cơ chế có sẵn. Fixture state từ Codex bị SIGINT và lượt resume thật xác nhận. |
| `b2c34ba` | **Chống nhiễu/khả năng rò rỉ eval**: CODEX_HOME riêng chưa đủ vì `HOME/.agents/skills` vẫn nạp. Profile eval chỉ giữ runtime/provider/auth cần thiết, không copy instructions/hooks/plugins; tắt user/admin skills theo đường dẫn và symlink, bundled skills; xoá profile mọi đường thoát. Chỉ áp `AgentRequest.Arm` của eval/bootstrap, không đổi bint run/Claude. Dùng parser TOML để giữ đúng provider thay vì cắt chuỗi cấu hình có thể chứa bí mật. |
| `d044b22` | **Lifecycle metadata**: review tái hiện app-server stub sinh child còn chạy sau huỷ. Tạo process group, huỷ/dọn cả cây; Windows có nhánh taskkill riêng nhưng chưa thử runtime Windows. |
| `89e5fe0` | **Khó chịu/onboarding**: init mặc định Codex vẫn nhắc Claude. Chọn `$b-bootstrap trong Codex`, test giữ nguyên hướng dẫn Claude mặc định. |
| `e8b5612` | **Ổn định test mới**: hai test timeout hết hạn trước khi child khởi động khi chạy suite đồng thời. Chờ child.pid trước cancel; test timeout profile vẫn buộc timeout và xác minh thư mục auth đã xoá, cho đủ thời gian startup. Không thay ngưỡng gate hay làm yếu test dự án. |

Không sửa lịch sử dữ liệu cũ: event ID đã ingest không bị ghi đè để “đẹp” báo cáo. Runs trước sửa vẫn có số liệu sai, giữ lại làm bằng chứng. Payload replay chỉ kiểm bản sửa với transcript thật, được gắn nhãn replay, không tính là session model mới.

## Giới hạn thật và những gì không tuyên bố

- **Mixed Claude↔Codex blocked do auth**, theo lựa chọn rõ ràng của người dùng. Đây là hạng mục duy nhất của ma trận còn cần điều kiện bên ngoài để chạy end-to-end. Không tuyên bố toàn bộ harness Claude runtime đã được nghiệm thu lại; unit/integration/race tests của Claude qua.
- **Codex không báo USD/max_turns enforcement**; chi phí và số lượt để null/không đo được, không quy đổi token thành tiền/lượt. Subscription profile dùng keyring mà không có auth.json sẽ được Bint báo không hỗ trợ isolate tự động, cần profile đăng nhập dạng tệp; không lén dùng home thật làm fallback.
- **PostToolUse không đồng nghĩa thành công.** Nếu thiếu/sớm/chậm transcript hoặc dòng matching ngoài giới hạn đọc thì kết quả shell là unknown. Script nhiều lệnh/heredoc/background không nâng thành tín hiệu test vì exit code toàn script không chứng minh kết quả từng test. Đây là giới hạn được khai báo, không mô phỏng.
- **Không coi cô lập context là chặn mọi truy cập hệ thống.** Phép thử chứng minh không tự nạp instructions/plugins/skill nguồn và lịch sử Git đáp án. Sandbox workspace-write vẫn cho đọc nhiều tệp host và có thể dùng mạng theo cấu hình; profile isolation không phải sandbox hermetic/biện pháp chống agent cố tình đọc đường dẫn bí mật. Provider/runtime keys được giữ để auth hoạt động; không có kết luận rằng không còn bất kỳ provider-level tool nào.
- **Trust UI `/hooks`** chưa được bấm trong TUI. Đã dùng API Codex thật để kiểm trạng thái và config state cùng hash, profile riêng; runtime hook chạy không bypass. Quy trình người dùng vẫn phải đọc và duyệt định nghĩa, Bint không làm thay trong init/doctor.
- **Skill events**: đọc SKILL.md trong trace chứng minh lựa chọn/đọc skill ở những lượt đã chạy, không phải native event `skill.activated`. Bint chưa xuất agents/openai.yaml tự động; không nhân đôi bộ skill để che sự khác nhau.
- Không thử Windows/macOS runtime. Các test nền tảng và build hiện hành không được nâng nhãn thành chạy Codex thật trên các hệ đó.

## Kiểm thử, lỗi nguyên văn và review

Các phép thử hồi quy đều tái hiện lỗi trước sửa. Bộ kiểm tra rộng lần đầu gặp hai lỗi timing của **test mới**:

- `TestMetadataCancellationKillsDescendants`: `open .../child.pid: no such file or directory`.
- `TestCodexEvalProfileRemovedAfterTimeout`: đã timeout nhưng chưa có output chứa profile vì shell chưa khởi động, `Text:` rỗng.

Giữ [go test lần đầu](evidence/go-test-first-failure.txt) và [race lần đầu](evidence/go-test-race-first-failure.txt), không xoá bằng chứng thất bại. Đã sửa đồng bộ/đủ ngân sách startup và chạy lại nguyên bộ; **mã thoát cuối đều 0**:

| Lệnh | Đầu ra nguyên văn |
|---|---|
| `gofmt -l .` | [gofmt.txt](evidence/gofmt.txt), rỗng |
| `go vet ./...` | [vet.txt](evidence/vet.txt), rỗng |
| `go test ./...` | [go-test.txt](evidence/go-test.txt) |
| `go test -race ./internal/harness/... ./internal/app/... ./internal/eval/...` | [go-test-race.txt](evidence/go-test-race.txt) |

Review độc lập không gọi model Codex, phát hiện symlink skill, false positive shell, cleanup child; đã sửa kèm test. Không đổi gate, test dự án, hành vi Claude mặc định hoặc thước đo hồi tố.

Fixture dùng trực tiếp trong Go test ở `internal/harness/codex/testdata/codex-0.167/trust-*.json`, `test-{failed,passed}-{hook,transcript}.*`, `planning-command.txt`, và `internal/workflow/testdata/codex-interrupted-planner.json`. Lệnh ghi nhận/status/usage thật nằm ở manifest/log đợt này. Tài liệu tham chiếu: [OpenAI Hooks](https://learn.chatgpt.com/docs/hooks), [Build skills](https://learn.chatgpt.com/docs/build-skills), [Advanced configuration](https://learn.chatgpt.com/docs/config-file/config-advanced).
