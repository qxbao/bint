# Nghiệm thu Bint — trạng thái, kế hoạch và hướng dẫn

Cập nhật: 2026-11-16, sau V1 chạy lại và kiểm fixture bản sửa trong worktree. Việc kế tiếp: kiểm `NT-MCP-CALLED` trên harness thật, rồi chạy
phần còn lại của V1 trên bản clone (mục 4.4). Kế hoạch profile harness (`bint iso`, GĐ6) thêm mã `NT-ISO-PROF-*` vào V1 (bước 7) và V2.

**Đây là nguồn chuẩn duy nhất về nghiệm thu.** Tài liệu này giữ:

- trạng thái nghiệm thu của mọi giai đoạn và kế hoạch;
- danh mục tiêu chí còn mở, mỗi tiêu chí một mã `NT-…`;
- kế hoạch các **vòng nghiệm thu**, đã gom tiêu chí để mỗi vòng đóng được nhiều mục nhất;
- hướng dẫn chạy từng vòng và bằng chứng cần lưu.

Các tài liệu khác chỉ trỏ về đây bằng mã `NT-…` hoặc tên vòng, không ghi lại trạng thái nghiệm thu.

| Loại tài liệu | Vai trò với nghiệm thu |
|---|---|
| Tài liệu này | Trạng thái, kế hoạch, hướng dẫn. Cập nhật sau mỗi vòng. |
| [gate-1.md](gate-1.md), [gate-2.md](gate-2.md) | Phụ lục giao thức **đã chốt** của hai cổng quyết định (ngưỡng, điều kiện, commit chốt). Không sửa ngưỡng; trạng thái và kế hoạch chạy ghi ở đây. |
| [status.md](status.md) | Trạng thái **triển khai** và giới hạn vận hành, không ghi trạng thái nghiệm thu. |
| Các kế hoạch (`*-plan.md`, [p8-next-experiments.md](p8-next-experiments.md)) | Thiết kế và nhật ký triển khai. Tiêu chí nghiệm thu gốc nằm ở đó; việc còn mở được chép về đây. |
| Biên bản lịch sử (`bint-acceptance-*.md`, `bint-reacceptance-*.md`, [evals/](evals/README.md)) | Bản ghi bất biến của một lần kiểm tra tại một commit. Không cập nhật. |

## 1. Quy tắc chung

1. **Ngưỡng chốt trước khi đo.** Không đổi ngưỡng, thước đo hay điều kiện sau khi thấy kết quả (mục 19 của
   [đặc tả](bint-plan.md)). Thay đổi là một lần chốt mới, có commit, chỉ áp cho lần đo sau đó.
2. **Mọi lần chạy tốn token cần chủ dự án duyệt ngân sách trước.** Gần chạm trần thì dừng và hỏi.
3. **Tập `test` không bao giờ là đầu vào** của agent sinh đề xuất hay bài học. Chẩn đoán chỉ trên `validation`.
4. **Bằng chứng** của mỗi vòng lưu ở `docs/evals/<tên-vòng>-<ngày>/` (README + `evidence/`, đã ẩn thông tin
   nhạy cảm). Báo cáo chỉ nêu số liệu, không nêu tên hay thông tin repository đo.
5. **Chỉ chủ dự án ký nghiệm thu.** Agent chạy vòng, ghi bằng chứng và đề xuất kết luận; cột "Ký" ở mục 2 do
   người điền.
6. Tác vụ `validation`/`test` hiện do agent duyệt (chủ dự án cho phép); mọi báo cáo phải ghi rõ điều này.
7. Sau mỗi vòng: cập nhật mục 2 và mục 3 của tài liệu này trong cùng commit với biên bản.

## 2. Trạng thái nghiệm thu

Ký hiệu: **Đủ điều kiện** = mọi phát hiện đã đóng, có bằng chứng, chờ ký · **Một phần** = còn tiêu chí mở ·
**Chưa đo** = mã xong, chưa chạy phép đo · **Chưa đạt** = đã đo, không qua.

### 2.1. Giai đoạn sản phẩm (mục 16 của đặc tả)

| Giai đoạn | Điều kiện nghiệm thu | Trạng thái | Bằng chứng | Còn mở | Ký |
|---|---|---|---|---|---|
| P0 Nền tảng | Khởi tạo nhiều lần không mất dữ liệu; nhiều worktree ghi về một CSDL không trùng, không mất | Đủ điều kiện (Linux) | [P0–P4](bint-acceptance-p0-p4.md), [tái nghiệm thu](bint-reacceptance-p0-p5-137dbf9.md), [đóng R1–R3](bint-acceptance-open-findings.md) | `NT-WIN-*` | |
| P1 Khung đánh giá | Khai thác tập tác vụ đủ lớn, chạy nhánh `none`, báo dao động giữa các lần lặp | Đủ điều kiện | [pilot Codex](evals/pilot-codex-2026-11-10/README.md), [Claude 30 tác vụ](evals/p6-p7-claude-2026-11-10/README.md) | — | |
| P2 Bootstrap | **Cổng quyết định 1** | Chưa đạt điều kiện đo | [gate-1.md](gate-1.md) (chỉ có pilot) | `NT-GATE-1` | |
| P3 Context Engine | **Cổng quyết định 2** | Chưa mở | [gate-2.md](gate-2.md) (chỉ có pilot) | `NT-GATE-2` | |
| P4 Workflow | Một yêu cầu đi qua `b-do` với đầu ra liên kết bằng task ID; nâng cấp không mất lớp phủ | Đủ điều kiện | [P0–P4](bint-acceptance-p0-p4.md), [Codex đợt 1](evals/codex-flows-2026-11-10/README.md), [đợt 2](evals/codex-flows-2026-11-11/README.md) | — | |
| P5 Multi-Harness | Cùng workflow và tri thức dùng được trên nhiều harness, giới hạn báo đúng | Một phần | [P5](bint-acceptance-p5.md), [hỗn hợp Claude↔Codex](evals/claude-codex-mixed-2026-11-11/README.md), [Codex 2026-11-12](evals/codex-flow-acceptance-2026-11-12/README.md) | `NT-AGY-REAL`, `NT-WIN-CODEX` | |
| P6 Tiến hoá L1 | Đề xuất được chấp nhận có lợi trên tập `test` độc lập | Một phần | [P6](bint-acceptance-p6-dff6570.md), [V0](evals/v0-p6-fixes-2026-11-13/README.md) | `NT-EVO-TEST` | |
| P7 Tiến hoá L2 | Skill đề xuất từ lịch sử chứng minh hữu ích trên tập đối chiếu | Chưa đo | — | `NT-EVO-TEST` | |
| P8 Tiến hoá tự động | Cải thiện trên tập tác vụ mới, giữ điều kiện chất lượng và an toàn | Chưa đạt (vòng đầu `not_supported`) | [Claude 2026-11-10, mục "Mở P8"](evals/p6-p7-claude-2026-11-10/README.md) | `NT-EVO-TEST` | |

### 2.2. Kế hoạch bổ sung

| Kế hoạch | Mã đã xong | Trạng thái nghiệm thu | Còn mở |
|---|---|---|---|
| [E — bài học từ thất bại](p8-harness-evolution-plan.md) | E1–E5, kiểm thử xác định của E6 | Chưa đo | `NT-E6` |
| [H — thành phần quy trình](p8-harness-evolution-plan.md) | H1–H4 | Chưa đo | `NT-H1-STOP`, `NT-H5` |
| [Thử nghiệm A, B](p8-next-experiments.md) | B có đủ mã; A thiếu `eval snapshot import` | Chưa đo | `NT-EXP-A`, `NT-EXP-B` |
| [Cô lập và driver session](isolation-subagent-plan.md) | G1–G4 và việc tồn | G1–G3 đạt (G3 chạy thật 2026-11-12, mục 12.16); G4 chạy lại ở [V1 2026-11-16](evals/v1-real-session-2026-11-16/README.md): đạt kèm điều kiện, chờ ký | `NT-WT-BASE`, `NT-ISO-RO-FP`, `NT-ISO-BYPASS` (vế Bash), `NT-ISO-MAXTURNS`, `NT-WIN-*` |
| [Profile harness](iso-plan.md) | GĐ1–GĐ6 (`bint iso`: lõi, CLI, cache/GC, sync, tích hợp `bint run`/`eval`, nhập từ ai-isolator) | Test tự động đạt (harness giả, store ai-isolator giả, không gọi mạng hay tài khoản thật); chưa chạy Codex/Claude thật trong profile | `NT-ISO-PROF-CODEX`, `NT-ISO-PROF-CLAUDE`, `NT-ISO-PROF-CACHE`, `NT-ISO-PROF-IMPORT`, `NT-ISO-PROF-MAC`, `NT-ISO-PROF-WIN` |
| [Gateway MCP](mcp-gateway-plan.md) | M0–M4; M5 đã đo bộ nhớ | Test tự động đạt cho M0–M4; M1 đạt với service thật ([V1](evals/v1-real-session-2026-11-13/README.md)); M2 đạt, M3/S3 một phần, M5-WT đã đo ([V1 2026-11-16](evals/v1-real-session-2026-11-16/README.md)) | `NT-MCP-CALLED`, `NT-MCP-M0`, `NT-MCP-M3`, `NT-MCP-S3`, `NT-MCP-M5-QUAL` |
| [TUI](tui-plan.md) | Giai đoạn 0–6, màn MCP | Golden và test phủ lệnh đạt; chưa thử với người và terminal thật | `NT-TUI-*` |
| [Nối tiếp phiên của bước](step-continuation-plan.md) | GĐ1 (`810bf2b`), GĐ2 (`d7b59e1`, `f68b41f`), GĐ3 (driver process: Claude `--resume`, Codex `exec resume`) | Tiêu chí bằng test của GĐ1–3 đạt (agent giả/fixture, không gọi harness thật); chưa chạy `claude --resume`/`codex exec resume` thật | `NT-RESUME-CLAUDE`, `NT-RESUME-CODEX` |
| [Custom skill/flow](custom-flow-plan.md) | GĐ1 (`21171cb`), GĐ2 (`f77ae19`), GĐ3 (`f124cd7`), GĐ4 (`7925e7d`), GĐ5 (`1c727f8`), GĐ6 (`25afb78`); sửa review `99083d0`, `b313ec1`, `5ce48e2` | Tiêu chí bằng test của GĐ1–6 đạt; chưa chạy với harness thật | `NT-CF-MANUAL` |

## 3. Danh mục tiêu chí còn mở

Cột "Vòng" trỏ tới mục 4. "Token" là token đo (eval/evolve), không tính token của phiên agent chạy tay.

### 3.1. Sửa mã trước khi nghiệm thu (không token)

| Mã | Mức | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-MCP-CALLED` | Trung bình | Lời gọi MCP qua gateway của phiên tương tác và subagent driver session ghi được `mcp.called`. V1 phát hiện shim không truyền Harness + SessionID ([V1 2026-11-16](evals/v1-real-session-2026-11-16/README.md), lỗi 1); fixture xuyên lớp của bản sửa đã qua trên host, còn thiếu bằng chứng harness thật chuyển định danh và ghi sự kiện. | Còn mở, trước khi đo lại `NT-MCP-M3` |
| `NT-ISO-RO-FP` | Thấp–trung bình | `bint-readonly` không chặn lệnh chỉ đọc có `\|` trong chuỗi trích dẫn, `git ls-files`, `grep` trên tệp của tác vụ (lỗi 2); bản sửa và migration đã qua test trong worktree, chưa nghiệm thu trên phiên thật. | Còn mở |
| `NT-CODEX-HOOK` | Trung bình | Phát hiện và nâng matcher cũ thiếu `local_shell`, phân biệt hook đã cài/current với trust. Bốn hook khảo sát trước đã enabled/trusted; chưa xác minh binary và hook sau nâng trên phiên thật. Code Mode chưa có payload để xác nhận tên công cụ. | Còn mở; xem [biên bản sửa](evals/nt-acceptance-fixes-2026-11-16/README.md) |
| `NT-WT-BASE` | Thiết kế | Quyết định: base của worktree có nên chụp thay đổi chưa commit của cây gốc không, và driver session có nên chờ khoá thay vì trả `blocked` (lỗi 3). Ghi quyết định vào isolation-subagent-plan.md | Chủ dự án quyết |
| `NT-MCP-PROBE-CAP` | Trung bình | `bint mcp check codegraph` có trần thời gian tổng cho phần gọi thử tool (review phiếu `01M7H9FZQ6JACTH163E6NSH985` .1), cùng .2–.4 và A.2; phiếu sửa `01M7HDFSQFGFCZYWXN891HVCE8` | Phiếu riêng |

Các mục đã đóng (chờ ký):

- `NT-MCP-SEED` đóng 2026-11-15 (`8fe0577`, tác vụ `01M7FMB6G78VTCCTRPW8R4F545`). `ensureWorktree` gọi
  `workflow.CopyCodegraphIndex`. Hàm này chép `.codegraph/codegraph.db` từ repo gốc bằng `VACUUM INTO` (gồm cả trang
  còn trong WAL) cùng `.codegraph/.gitignore`. Gốc chưa có chỉ mục thì chạy `codegraph init -y` một lần. Test
  `TestCopyCodegraphIndexConsistent` dựng nguồn có `-wal`/`-shm`, `codegraph.pid`, `daemon.sock` và đòi worktree chỉ có
  `.gitignore`, `codegraph.db`. `daemon.log` bị loại vì hàm chỉ chép hai tệp đó; chưa có test riêng cho tệp này.
  Chạy thật đạt ở [V1 2026-11-16](evals/v1-real-session-2026-11-16/README.md): mỗi worktree chỉ nhận hai tệp đó; test
  riêng cho `daemon.log` thêm ở phiếu `01M7H9FPFY3NYKEB3W4MS20FM1`.
- `NT-P6-06` đóng 2026-11-15 (`61222e8`, tác vụ `01M7FGG2H0KZZ6G4CDDPYAE0TT`). Đây là các low còn lại sau V0;
  trạng thái từng phát hiện ở [biên bản V0](evals/v0-p6-fixes-2026-11-13/README.md).

`NT-P6-01..05` đóng ở V0 (2026-11-13), chờ ký. `NT-ISO-LEAK` đóng 2026-11-13 (`e143459` + phiếu sửa review `01M7AC1XXEEG0SWVY75SQJ4XKV`): hậu kiểm transcript ở `bint run complete` đã kiểm trên transcript thật của V1 (bắt đúng lần ghi lọt, không tính lần bị hook chặn); `BINT_HOOK_TRACE` có tài liệu. Hai low còn lại đã đóng: lời nhắc `bint run next <id> --resume` không còn bị TUI đổi nhầm sang phím Chạy (đã loại trừ trong `cliref_inventory_test.go`), và low dựng `paths` đóng 2026-11-15 (tác vụ `01M7FKMZDVQ3XFR0EC1H5P2AXM`) bằng `TestIsolationAuditPathsPayloadIsRedactedList`: payload `paths` của `missed_by_hook` là danh sách đúng từng đường dẫn của lời gọi, mỗi phần tử qua `detect.Redact`, đường dẫn quá 300 ký tự bị cắt kèm dấu ba chấm. Phần chạy thật nằm ở lần chạy lại `NT-ISO-G4`. `NT-ISO-LEAK-FP` đóng 2026-11-13 (tác vụ `01M7AQSQ89E8Z6YHBPCM08ENKT`): hậu kiểm không tính lời gọi bị chặn trước khi chạy (`hookcore.IsPreExecDenial`: hook của Bint, `<tool_use_error>`, từ chối quyền, người dùng từ chối); `Exit code N` và dạng lạ vẫn tính. Còn 1 low: "The user doesn't want to take this action right now" có thể xuất hiện khi người dùng ngắt một lệnh đã bắt đầu chạy (chưa xác định trên transcript thật). Trong lúc sửa, bước b-feat của chính phiếu này cũng bị chặn nhầm một lần vì binary đang cài chưa có bản sửa. `NT-WF-BASELINE` đóng 2026-11-13 (tác vụ `01M7AJMTHSJPN53DRYQN1PR4M8`). Hiểu lại điều kiện: chặn ở Finalize là đúng (kiểm chứng phải chạy trên mã hiện tại), phần hỏng là trải nghiệm; bản sửa: Finalize tự chạy lại lệnh kiểm chứng đã xác nhận đúng một lần khi chỉ tệp ngoài `touched` đổi sau lần kiểm chứng (và không còn lý do chặn khác), `bint task diff` của tác vụ shared mặc định chỉ hiện tệp `touched` (`--all` xem hết; tác vụ worktree luôn hiện toàn bộ — sửa phát hiện review .1), lời nhắc tiếp tục theo driver (`bint run next <id> --resume` cho session). Ba low còn lại của review đã đóng 2026-11-15 (tác vụ `01M7FKMZDVQ3XFR0EC1H5P2AXM`): .2 `bint task diff --all` của tác vụ shared hiện mọi tệp khác `BaseTree`, kể cả tệp `Touched` của tác vụ shared khác đang chạy (`TestDiffTaskAllShowsOtherRunningTaskFiles`); .3 commit chỉ đổi HEAD mà cây giữ nguyên không còn làm Finalize chặn (`DoneInputDetailed` coi lần kiểm chứng còn mới khi ảnh chụp cây trùng `Verification.Tree`; `TestFinalizeIgnoresHeadOnlyChange`); .4 lời nhắc tiếp tục của CLI theo driver (`bint run status`, `bint task show`, `bint task approve`, `bint task land` đều dùng `State.ResumeCommand()`; `TestResumeHintFollowsDriverCLI`, `TestLandResumeHintFollowsDriverCLI` cho cả hai driver). Cùng tác vụ đó sửa ba lỗi gặp khi chạy tác vụ `01M7FGG2H0KZZ6G4CDDPYAE0TT` (driver session, tác vụ worktree): C1 Finalize tự chạy lại lệnh kiểm chứng đã xác nhận đúng một lần khi lần kiểm chứng cuối không qua mà nội dung mã đã đổi kể từ đó (commit cherry-pick vào nhánh tác vụ) và không còn điều chặn khác (`TestFinalizeReverifiesAfterFailedVerifyAndFix`, `TestSessionWorktreeFinalizeReverifiesAfterFix`, `TestFinalizeDoesNotReverifyFailedVerifyWhenGuarded`); C2 `land` không báo xung đột cho tệp mà cây gốc đã giống hệt bản của tác vụ (kể cả cả hai phía cùng xoá): bỏ qua tệp đó khi áp, `Landed.Files` vẫn liệt kê và kết quả có ghi chú; khác quyền thực thi (khi `core.fileMode` bật) vẫn là xung đột — phát hiện review .1 (medium), sửa sau khi land (`TestLandSkipsFilesIdenticalAtOrigin`, `TestLandSkipsFileDeletedOnBothSides`); C3 tác vụ đã đưa về mà thư mục worktree đã gỡ hoặc còn sót nhưng không còn là gốc worktree Git đi tiếp tới done qua cổng done như Finalize (vẫn chặn khi còn dấu xung đột của `land --3way`), không chuyển failed (`TestLandedTaskResumesToDoneWhenWorktreeGone`; tác vụ chưa đưa về vẫn failed, `TestRootForWorktreeGoneFails`). Chưa thử trên phiên điều phối thật. `NT-MCP-CAT` đóng 2026-11-13 (tác vụ `01M7A80F2X6A803CFR491X14GR`); gợi ý low còn lại đóng 2026-11-15 (tác vụ `01M7FSQACD3XBT79JEWYC958YK`): `TestCatalogSpecifics` ghim `Install` của specmatic (`npm i -g specmatic-mcp@1.2.0`) và nhóm `deepwiki.read` đúng ba tool kể cả `ask_wiki_question`. `NT-MCP-PROBE` đóng cùng tác vụ: `bint mcp check codegraph` gọi thử mọi tool của danh mục có trong `tools/list`, tool báo bị tắt làm `degraded` kèm nhóm, tên tool và nguyên văn lỗi; lỗi truyền tải ở bất kỳ tool nào là `fail` nêu đúng tool đó — sửa review .1, .4 sau khi land (`TestMCPCheckCodegraphNarrowEnv`, `TestMCPCheckCodegraphNonStatusOtherErrorIgnored`, `TestMCPCheckCodegraphTransportErrorNamesTool`, `TestMCPCheckCodegraphDisabledDetailQuotesError`). Còn low: tám lời gọi chung hạn `mcpCheckTimeout`, chưa kiểm với daemon codegraph thật chạy env hẹp. Mục tri thức `k:01M746PTBYPSXDT0WJZS38KPFD` lỗi thời sau V0 đã được thay bằng
`k:01M7FGC944KVT6NY1TZE7MJ0P8` (2026-11-15; mục cũ `deprecated`).

### 3.2. Chạy thật trên máy (cần người, không token đo)

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-ISO-BYPASS` | isolation 12.9 (S1, S3) | Vế hook `deny` đã đạt ở V1. Còn: ghi nhận `Bash(<mẫu>)` trong frontmatter subagent chỉ đọc có giới hạn lệnh dưới `bypassPermissions` hay không | V1 (lần sau) |
| `NT-ISO-MAXTURNS` | isolation 12.13 | `maxTurns` trong frontmatter subagent được Claude Code áp (bước dừng đúng trần) | V1 |
| `NT-ISO-PROF-CODEX` | iso-plan GĐ5, mục 5.7 | `bint run` một tác vụ với Codex thật ở profile `work` (`harnesses.codex.profile: work`, Codex LB `linked` hoặc đăng nhập tài khoản thật): hook ghi vào state thật (không phải state giả), chi phí đọc được từ `CODEX_HOME` của profile, resume nối tiếp đúng phiên; đổi profile giữa hai lượt thì lượt chạy mới có ghi lý do. Ghi thêm: Codex có đòi trust cho hook `-c hooks.PreToolUse` ở lớp `-c` trong profile không (hook dự án đã trusted bằng `bint iso sync` thì không bị ảnh hưởng); `bint mcp` do Codex khởi chạy trong profile mở đúng `state.sqlite` thật (bảng `env` của mục MCP) | V1 |
| `NT-ISO-PROF-CLAUDE` | iso-plan GĐ2, mục 5.2 | `bint iso login claude --name work` đăng nhập lại được trong profile (credential không chép từ máy thật); `bint run` với Claude ở profile: hook `PreToolUse` của bint chạy và ghi spool vào state thật; `DISABLE_UPDATES`/`DISABLE_AUTOUPDATER` chặn tự cập nhật khi có bản mới (không tạo `versions/` riêng trong profile) | V1 |
| `NT-ISO-PROF-CACHE` | iso-plan GĐ3 | Profile mới sau một phiên thật có MCP `uv`/`npx`: không tạo bản cache riêng nào (`bint iso du` không có nhóm cache riêng, thư mục `.cache`/`.npm` là liên kết); `bint iso du` khớp `du -sh` thư mục profile (TỔNG = `du -s`) | V1 |
| `NT-ISO-PROF-IMPORT` | iso-plan GĐ6 | Nhập `codex--personal` thật bằng `bint iso import --from-ai-isolator` (trước đó `--dry-run`): `bint iso run codex --name personal` chạy được **không đăng nhập lại**, LB `linked` đúng URL; `--with-sessions` thì `codex resume` thấy và nối tiếp được rollout đã chép dù không chép `state_*.sqlite` (nếu không thấy, ghi lại và đổi quyết định: chép sqlite hay dựng lại chỉ mục); store ai-isolator nguồn không đổi (so `find … -type f | sort` và `sha256sum` trước/sau); `bint iso sync --check` thoát 0 ngay sau import; hook trong `hooks.json` symlink của nguồn không bị ghi xuyên | V1 |
| `NT-ISO-PROF-MAC` | iso-plan S6 | Trên macOS: Claude lưu credential ở Keychain theo khoá nào khi đổi `CLAUDE_CONFIG_DIR` (có tách theo profile không); từ đó kết luận `bint iso import` cho Claude có chuyển được đăng nhập không hay phải `bint iso login claude`; `bint iso create/run` Codex và Claude chạy được, `~/Library/Caches` chia sẻ đúng | V1 (cần máy macOS; chưa có máy) |
| `NT-MCP-M0` | mcp §4 | Một lượt `bint run b-do` thật ghi ≥ 1 dòng `knowledge_citations` từ bước con trên Claude **và** trên Codex (`codex exec` thật); bước Codex không thấy server MCP toàn cục | V1 |
| `NT-MCP-M3` | mcp §7 | Còn lại sau V1 2026-11-16: `b-review` không thấy `spec_manage_mock_server`; lời gọi ngoài quyền bị từ chối (`mcp.called` có `outcome: denied`); spool có `mcp.called` đúng run/role cho từng bước (dùng `bint run`, sau `NT-MCP-CALLED`); một jira cho cả máy (nếu cấu hình jira). Đã đạt: một client codegraph cho mọi phiên cùng worktree, shim khởi động lại daemon sau `gateway stop` | V1 (lần sau) |
| `NT-MCP-S3` | mcp §10 | Còn lại: Codex có chuyển env tới server MCP stdio không; Codex có nhận `approval_mode` cho tool chuyển tiếp không; cách khôi phục trong Codex khi shim chết. Đã ghi: cả Claude Code lẫn Codex không tự khởi động lại server stdio, Claude khôi phục bằng `/mcp` → Reconnect | V1 (lần sau) |
| `NT-AGY-REAL` | P5, workflows | Antigravity thật: một lượt `b-do`, payload hook đúng dạng adapter chờ, MCP cấp người dùng khởi động với cwd là workspace | V1 |
| `NT-HOOK-COMPACT` | tri thức `k:01M74VDJXBG8BF6J1DWB56KW06` | Sau một phiên dài thật có nén ngữ cảnh: spool có `session.started` với `source=compact`, và có dấu vết việc đặt lại bộ nhớ | V1 |
| `NT-H1-STOP` | plan H, H1 | Guardrail `on: stop` (`when: edited`, `unless: ran test`) với Claude Code thật, mỗi lần một `action`: `remind`/`ask` chặn một lần rồi không chặn lại khi `stop_hook_active=true`; `deny` chặn tới `MaxStopDenies` lần rồi bỏ qua; chạy test trước khi dừng thì không chặn. Lưu input Stop thô, phản hồi hook và spool | V1 |
| `NT-TUI-REAL` | tui 12.10 | Terminal thật: `NO_COLOR=1 bint tui`, `bint tui --read-only`, phím `?` ở từng thẻ phụ; màn MCP với service thật | V1 |
| `NT-TUI-VHS` | tui 12.10 | Ảnh VHS các màn chính | V1 |
| `NT-RESUME-CLAUDE` | step-continuation GĐ3 | `claude -p --resume` thật cho một bước driver process (xem V1 bước 6). Đo và ghi: (1) `--resume <id>` giữ nguyên session ID và ghi tiếp vào CÙNG `<id>.jsonl` ở thư mục dự án của cwd (giả định A1; nếu tạo tệp/ID mới thì ghi lại, mốc chi phí khác transcript nên Bint tính cả tệp mới); (2) `total_cost_usd`/`usage` trong JSON của lần resume là cộng dồn cả phiên hay chỉ phần mới (so với phần usage vượt mốc của transcript: Bint không dùng số này nên chỉ để biết); (3) `--max-budget-usd` sau resume tính trên chi phí cộng dồn hay chỉ tiến trình mới (nếu cộng dồn thì bước nối tiếp dừng sớm vì trần: quyết định có hạ trần/bỏ cờ khi nối tiếp); (4) thông báo khi phiên không còn (`No conversation found with session ID`) và mã thoát; (5) đổi cwd (worktree khác) thì `--resume` không thấy phiên; (6) hook/guardrail của lượt con mới thấy `BINT_RUN_ID` mới (nhật ký deny đọc theo lượt mới); (7) Ctrl-C giữa bước rồi `bint run resume` nối tiếp được, agent không làm lại phần đã xong | V1 (bước 6) |
| `NT-RESUME-CODEX` | step-continuation GĐ3 | `codex exec resume` thật (codex-cli ≥ 0.167; kế hoạch ghi 0.169, máy dựng bản này chỉ có 0.167: fixture trợ giúp ở `internal/eval/testdata/codex-0.167/`). Đo và ghi: (1) mọi cờ của bước gốc đặt TRƯỚC `resume` (`--sandbox`, `--cd`, `--add-dir`, `-c mcp_servers.*`, `hooks.PreToolUse`, `--model`) có còn hiệu lực ở lượt resume không (sandbox thật, MCP độc quyền, hook `apply_patch`); (2) `resume` ghi tiếp vào cùng rollout hay tạo tệp mới, `total_token_usage` có cộng dồn theo thread không (giả định A2: Bint trừ mốc bất kể tệp); (3) thông báo khi thread không còn (`no rollout found for thread id`) và mã thoát; (4) `turn.completed.usage` của lần resume là cộng dồn hay chỉ lượt mới; (5) thread id trong `thread.started` của lượt resume có giữ nguyên không | V1 (bước 6) |
| `NT-CF-MANUAL` | custom-flow §7 | GĐ1–6 đã commit. `b-add-skill`, `b-add-flow` và skill điều phối chạy với harness thật; ghi chi phí thật của `bint skill test` | V1 (lần sau) |
| `NT-RESUME-PLAN-FEAT` | workflows (nối tiếp `b-req` → `b-feat`), giả định chưa đo | Chạy một tác vụ `b-do --plan` thật bằng driver process, một lần với Claude và một lần với Codex (cùng harness/model cho `b-req` và `b-feat`). Ghi: (1) `b-feat` có thật sự nối tiếp phiên `b-req` (`notes` "b-feat nối tiếp phiên …", `claude --resume`/`codex exec resume` không báo thiếu phiên); (2) Claude: cờ chỉ đọc của `b-req` (`--disallowedTools`/công cụ chỉ đọc) không còn áp ở lượt resume, `b-feat` sửa được tệp; Codex: sandbox của lượt resume cho phép ghi; (3) lời nhắn nối tiếp (kế hoạch rút gọn) đủ để `b-feat` làm đúng AC/TSK mà không đọc lại cả `task.yml`; (4) usage của `b-feat` chỉ tính phần tăng thêm (so với `usage` của phiên); (5) đổi model giữa hai bước: với Claude/Codex `b-feat` vẫn nối tiếp, xem `NT-CONT-CLAUDE-MODEL`, `NT-CONT-CODEX-MODEL` | V1 (lần sau) |
| `NT-FAST-FLAG` | phiếu `01M7MHWPT8E8NR27XC30SV6S94` (đường nhanh tuỳ chọn) | Trên repo thật: (1) `bint run b-do` không cờ — kể cả yêu cầu nhỏ/rõ như "Sửa chính tả trong README" hay "làm cho nó gọn gàng hơn" — đi `b-req` (JSON `classification.fast: false`, `plan: true`; chỉ `--no-plan`/`--fast` bỏ `b-req`); (2) `bint run b-do --fast "<cùng yêu cầu>"` và `/b-do-fast` đi một phiên `b-feat`, một review, `classification.fast: true`; (3) `--fast` kèm `--plan`/`--no-plan`/`--depth` báo lỗi mã 2; (4) `--fast` với phạm vi chạm `limits.protected_paths` giữ `b-req` kèm lý do; (5) `workflow.fast_path` mặc định `false` ở `bint init` mới và `.bint/config.yml` | V1 |
| `NT-PLAN-SIZE` | phiếu `01M7MHWPT8E8NR27XC30SV6S94` (cổng cỡ kế hoạch, `workflow.small_plan_max_files`) | Trên repo thật với agent thật: (1) yêu cầu nhỏ có `b-req` ghi `Kết luận: nhỏ`, ≤ 5 tệp, kiểm tra mục tiêu nêu tên test: `b-feat` chạy ngay không dừng, `notes` có "cỡ kế hoạch: nhỏ", kiểm chứng và review vẫn chạy; (2) yêu cầu nhiều giai đoạn (dòng `G-N`) hoặc > 5 tệp: tác vụ `blocked` với `reason` mở đầu `kế hoạch lớn:`, `bint run resume <id>` (hoặc `/b-do` sau khi người đồng ý) chạy `b-feat` mà không chạy lại `b-req`; (3) ghi nhận `b-req` thật có ghi mục `## Cỡ kế hoạch` đúng khuôn không (tỷ lệ plan bị coi lớn chỉ vì thiếu mục/khuôn sai) và ngưỡng 5 có hợp lý không | V1 |
| `NT-CONT-CLAUDE-MODEL` | workflows (nối tiếp `b-req` → `b-feat`, khác model) | **Đã kiểm thật bằng tay** (2026-11-17, phiên Claude Code của chủ dự án; chưa có biên bản/transcript lưu kèm): phiên mở bằng `opus`, nối tiếp bằng đúng lệnh `claude -p --model sonnet --resume <id>`. Kết quả: nối tiếp được, nhớ đúng nội dung phiên cũ, báo đúng model mới; cache prompt mất ở lượt đầu sau khi đổi model nhưng ngữ cảnh được giữ. Căn cứ để Bint cho nối tiếp khác model với harness `claude`. Còn mở: chạy lại qua `bint run b-do --plan` có `b-req`/`b-feat` khác model và lưu biên bản | V1 (lần sau) |
| `NT-CONT-CODEX-MODEL` | workflows (nối tiếp `b-req` → `b-feat`, khác model) | **Đã kiểm thật bằng tay** (2026-11-17, phiên Claude Code của chủ dự án; chưa có biên bản/transcript lưu kèm): phiên mở bằng `gpt-6-astra`, nối tiếp bằng đúng lệnh `codex exec -m gpt-6-luna --skip-git-repo-check -s read-only resume <id> -`. Kết quả: nối tiếp được, nhớ đúng nội dung phiên cũ, báo đúng model mới; cache prompt mất ở lượt đầu sau khi đổi model nhưng ngữ cảnh được giữ. Căn cứ để Bint cho nối tiếp khác model với harness `codex`. Còn mở: chạy lại qua `bint run b-do --plan` và lưu biên bản | V1 (lần sau) |

`NT-MCP-M0`: theo ghi chú phiên làm việc 2026-11-12, Claude đã đạt và Codex mới kiểm ở mức `codex mcp list`;
chưa có biên bản, nên vẫn mở cho cả hai. V1 (2026-11-13): bước Codex qua `bint run` có `knowledge.cited`, bước Claude
hỏng vì lỗi 403 của API, chưa kết luận.

V1 chạy lại 2026-11-16 (buổi A, repo chính, [biên bản](evals/v1-real-session-2026-11-16/README.md)): `NT-ISO-G4` đạt kèm
điều kiện (hai phiếu phải thu hẹp lệnh kiểm chứng vì base chứa lỗi có sẵn từ cây gốc bẩn, xem `NT-WT-BASE`), `NT-MCP-M2` đạt,
`NT-MCP-M5-WT` đã đo (PSS hai worktree); ba mã này rời bảng, chờ ký.

`NT-MCP-M1` đóng ở V1 (2026-11-13). `NT-TUI-USER` bỏ qua theo quyết định của chủ dự án (2026-11-13).
`NT-MCP-M3` không đo được bằng driver session: subagent dùng role `interactive` của phiên cha (giới hạn đã biết của M3),
nên kiểm role/`mcp.called` phải dùng `bint run` (process driver).

### 3.3. Windows thật (cần máy Windows, không token đo)

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-WIN-TEST` | open-findings (R1), isolation | `go test ./...` qua trên Windows thật | V2 |
| `NT-WIN-LOCK` | isolation 12.1 | Hai `bint run` song song trên cùng repo: khoá chia sẻ/độc quyền `LockFileEx` đúng; hai binary khác phiên bản cùng giữ khoá | V2 |
| `NT-WIN-WT` | isolation 12.6 | `git worktree`, `land`/`cancel` khi tệp khoá bị xoá, đường dẫn dài, `git apply`, đọc hợp đồng thuê trong lúc `os.Rename` | V2 |
| `NT-WIN-MCP` | mcp §6–8 | Gateway: Job Object/detach, RSS trong `gateway status`, shim khởi động lại daemon | V2 |
| `NT-WIN-TUI` | tui 12.10 | TUI trong Windows Terminal: màu, chuột, chế độ hẹp | V2 |
| `NT-WIN-CODEX` | Codex 2026-11-12 | Runtime Codex trên Windows (reader, profile cô lập) | V2 |
| `NT-ISO-PROF-WIN` | iso-plan S5 | `bint iso create/run/sync/gc/remove` trên Windows thật: `claude`/`codex` đọc `USERPROFILE`/`APPDATA`/`LOCALAPPDATA` hay API shell folder (đường dẫn cấu hình có theo HOME giả không); junction cho `.cache`/`.npm` tạo và gỡ đúng (`remove` không xoá dữ liệu đích); temp ngắn dưới `MAX_PATH`; `bint iso import --from-ai-isolator` với `--source` trỏ một bản chép store ai-isolator (đường dẫn Windows trong `hooks.state` bị bỏ đúng) | V2 |

### 3.4. Phép đo tốn token

| Mã | Nguồn | Điều kiện đóng | Vòng |
|---|---|---|---|
| `NT-EXP-B` | experiments B | B0: tỷ lệ đạt của `none` với model rẻ trên `validation` nằm trong 15–60 %; nếu ngoài khoảng thì đổi executor hoặc dừng hướng B | V3 |
| `NT-E6` | plan E, E6 | Có bài học từ thất bại đạt trên `test`; hoặc kết luận âm tính sau 3 đề xuất | V3 (lấy lượt trượt) + V4 |
| `NT-H5` | plan H, H5 | Bước 1: biết guardrail `test-before-stop` có tác động không (`bint` so với `bint@harness=all`); bước 2: một vòng cycle với cơ hội quy trình | V3 (bước 1) + V4 (bước 2) |
| `NT-EVO-TEST` | P6, P7, P8 | Ít nhất một đề xuất tự sinh đạt `supported` trên `test` độc lập và được xác nhận lại với 2 lần lặp; hoặc kết luận âm tính có ghi chép | V4 |
| `NT-MCP-M5-QUAL` | mcp §11 | Nhánh eval có/không `grants` cho `b-req`/`b-review`; chỉ đưa `grants` vào mẫu mặc định khi có cải thiện | V3 nếu có biến thể nhánh, nếu không thì hoãn |
| `NT-EXP-A` | experiments A | Δ(`bint` − `none`) với tri thức do người viết, theo cổng của Kế hoạch A | V5 |
| `NT-FAST-BENCH` | phiếu cha `01M7M90JBJ2FWYGPPPYYXTFPBW`, giai đoạn 1 nhóm 1 (đường nhanh `b-do --fast` — từ phiếu `01M7MHWPT8E8NR27XC30SV6S94` đường nhanh không còn tự chọn, đo bằng `--fast` hoặc `workflow.fast_path: true`; nối tiếp `b-req` → `b-feat`, sửa tiếp khi review hội tụ) | Đo lại **phía B** của T1 và T2 theo đúng quy trình của biên bản bench v4 (`/home/baodq/bint-v1-clone/docs/evals/bench-bint-vs-plain-v4-20261117/README.md`): cùng commit gốc, cùng prompt, cùng thẻ người dùng giả, cùng test ẩn, binary build từ commit chứa giai đoạn 1; phía A giữ nguyên diff lần 3. Ghi cho **từng task**: (1) **thời gian tường** của B (v4: B ≈ 88–90 phút, A ≈ 19–22 phút; mục tiêu đường nhanh cho yêu cầu mơ hồ đơn giản gần plain, 20–30 phút) và có chạm trần 90 phút không; `Classify:` của tác vụ có ghi đường nhanh không (chạy phía B bằng `--fast`; không cờ thì đi `b-req` và kế hoạch nhỏ chạy thẳng `b-feat`, ghi cả hai chế độ nếu đo; T1/T2 là yêu cầu "dọn dữ liệu cũ"/"không lưu bí mật": nếu không rơi vào nhánh mơ hồ ngắn thì ghi rõ vì sao đi đường đầy đủ); (2) **token vào so với A** (v4: 2,51×; A 14,70M; ngưỡng chốt 2,5×) kèm tách `b-req`/`b-feat`/`b-review`, số lượt và ngữ cảnh của `b-feat` (v3: 135–176 lượt × 350k) và `notes` nối tiếp phiên; (3) **kết quả review mù** B so A trên cùng reviewer/verifier và cùng thước đếm (v4: T1 A 0C/2M vs B 0C/4M; T2 A 7C/1M vs B 8C/2M), đặc biệt tác vụ đường nhanh chỉ có một vòng review rồi một vòng sửa không review lại: có phát hiện nghiêm trọng nào lọt sau vòng sửa không; (4) số vòng sửa thực tế và chuỗi phát hiện nghiêm trọng mỗi vòng (T1 v4: 5→1 cần `--fix-round` — giờ có tự sửa tiếp tới trần `2 × max_review_rounds` không; T2 v4: không hội tụ 4→4). Chốt theo bảng ở mục 8 của `docs/benchmark-bint-vs-plain-v3-plan.md`, không đổi luật sau khi thấy số. Trạng thái: **chờ người chạy** (mỗi lượt ~90 phút, tốn token; b-feat không tự chạy) | chưa xếp vòng: gom vào lần benchmark kế tiếp |
| `NT-GOAL-BENCH` | phiếu cha `01M7M90JBJ2FWYGPPPYYXTFPBW`, giai đoạn 2 nhóm 2 (kiểm tra mục tiêu do `b-req` sinh, `b-feat`/`b-review` chạy tới khi xanh, review chấm theo mục tiêu, dòng `mới xong 1/N`) | Đo lại **phía B** của T1 và T2 như `NT-FAST-BENCH` (cùng commit gốc, prompt, thẻ người dùng giả, test ẩn; binary build từ commit chứa giai đoạn 2). Ghi cho từng task: (1) test ẩn của T2 (không lưu bí mật) và T1 (dọn dữ liệu cũ: preview liệt kê, clean giải phóng dung lượng) đạt hay không, so với bench v4; (2) test mục tiêu mà `b-feat` viết có bao phủ mọi đường nhập/nơi lưu của `## Bề mặt bị ảnh hưởng` (hay chỉ phần định sửa); (3) số vòng sửa mở vì kiểm chứng đỏ và có đốt vòng vô ích do test chập chờn; (4) review có xếp `high` cho chỗ sót ngoài plan mà test mục tiêu bắt được, và có phình thêm (diff/thời gian/token so với `NT-FAST-BENCH`); (5) nếu plan chia giai đoạn thì kết quả cuối có dòng `mới xong 1/N giai đoạn`. Kết luận có/không lợi cho từng điểm, ghi ở biên bản vòng. Trạng thái: **chờ người chạy** (tốn token; b-feat không tự chạy) | chưa xếp vòng: gom vào lần benchmark kế tiếp (cùng lượt với `NT-FAST-BENCH`) |
| `NT-GATE-1` | [gate-1.md](gate-1.md) | Ngưỡng và điều kiện ở gate-1.md, trên ≥ 2 repository | V6 |
| `NT-GATE-2` | [gate-2.md](gate-2.md) | Ngưỡng và điều kiện ở gate-2.md | V6 |

## 4. Kế hoạch các vòng nghiệm thu

### 4.1. Tổng quan

| Vòng | Nội dung | Đóng các mã | Token đo (trần đề xuất) | Cần người | Điều kiện trước |
|---|---|---|---|---|---|
| **V0** | Sửa mã P6 và hồi quy tự động — **xong 2026-11-13** | `NT-P6-01..05` | 0 | Duyệt phiếu | — |
| **V0b** | Sửa các low workflow (`NT-WF-BASELINE` .2–.4, `NT-ISO-LEAK` dựng `paths`) và lỗi C1–C3 của driver session — phiếu `01M7FKMZDVQ3XFR0EC1H5P2AXM`, **xong 2026-11-15** | low của `NT-WF-BASELINE`, `NT-ISO-LEAK` | 0 | Duyệt phiếu | — |
| **V1** | Chạy thật trên máy, hai buổi (~4–6 giờ): G4 + MCP + TUI + Antigravity + nối tiếp phiên — **một phần 2026-11-13; buổi A chạy lại 2026-11-16 (G4, M2, M5-WT xong); phần còn lại chạy trên bản clone** | `NT-ISO-BYPASS`, `NT-ISO-MAXTURNS`, `NT-ISO-RO-FP`, `NT-ISO-PROF-CODEX`, `NT-ISO-PROF-CLAUDE`, `NT-ISO-PROF-CACHE`, `NT-ISO-PROF-IMPORT`, `NT-ISO-PROF-MAC` (cần máy macOS), `NT-MCP-M0`, `NT-MCP-M3`, `NT-MCP-S3`, `NT-AGY-REAL`, `NT-HOOK-COMPACT`, `NT-H1-STOP`, `NT-TUI-REAL`, `NT-TUI-VHS`, `NT-RESUME-CLAUDE`, `NT-RESUME-CODEX` | 0 (phiên agent ước $15–30) | Ở từng điểm, ~1–1,5 giờ (mục 4.4) | Service MCP cài trên máy |
| **V2** | Một lượt trên máy Windows | `NT-WIN-*`, `NT-ISO-PROF-WIN` | 0 | Có | Máy Windows |
| **V3** | Một đợt eval trên `validation` với model rẻ, ba nhánh | `NT-EXP-B` (B0, B1), bước 1 của `NT-E6`, bước 1 của `NT-H5` | ~$15 | Duyệt ngân sách | Guardrail `test-before-stop` ở `candidate` |
| **V4** | Một vòng `evolve cycle` rồi xác nhận trên `test` | `NT-EVO-TEST`, `NT-E6`, `NT-H5` (bước 2), B2–B3 | ≤ $35 | Duyệt ngân sách | V0, V3 qua cổng B0 |
| **V5** | Kế hoạch A: tri thức do người viết | `NT-EXP-A` | ≤ $20 | Viết 10–20 mục tri thức | Lệnh `eval snapshot import` |
| **V6** | Cổng quyết định 1 rồi 2 trên ≥ 2 repository | `NT-GATE-1`, `NT-GATE-2` | Theo `bint eval estimate` | Duyệt ngân sách, chọn repository | Repository thứ hai có ≥ 20 tác vụ `test` |

Thứ tự đề xuất: V0 → V1 → V3 → V4 → V5 → V6; V2 chạy bất cứ lúc nào có máy Windows.

### 4.2. Vì sao gom như vậy

- **V3 dùng chung nhánh `bint`.** Ba kế hoạch đều cần một đợt trên `validation`: B0 cần `none`, B1 và bước 1
  của E6 cần `bint` (để lấy lượt trượt), bước 1 của H5 cần `bint` so với `bint@harness=all`. Chạy riêng là 5
  nhánh-đợt; gom lại còn 3 nhánh trong một đợt.
- **V4 dùng chung một vòng cycle.** Bước 2–3 của E6 (trần $30), bước 2 của H5 (trần $25) và B2 (trần $25) đều
  là `bint evolve cycle` đối chiếu trên `validation` rồi `test`. Một vòng với `--max 3` nhận cả cơ hội
  `failure_lesson` lẫn cơ hội quy trình, nên một lần chạy trả lời cả ba, và cũng là phép thử cho nghiệm thu
  P6/P7/P8. Trần gộp ≤ $35 (cycle ≤ $25, xác nhận B3 ≤ $10) thay cho tổng trần riêng lẻ khoảng $80.
- **V1 dùng hai tác vụ song song của G4 làm tải cho gateway MCP.** Hai phiếu `--worktree` chạy bằng subagent
  tạo đúng tình huống M2/M3 cần: nhiều phiên, hai worktree, lời gọi theo role. Cùng phiên đó quan sát bằng
  TUI và để phiên đủ dài cho việc nén ngữ cảnh.
- **V2 gom mọi mục Windows** vì chi phí chính là dựng máy, không phải từng phép kiểm.
- **V6 không gom hai cổng vào một đợt.** Chung nhánh `minimal` tiết kiệm một nhánh, nhưng nếu cổng 1 không
  qua thì nhánh `bint` bỏ đi và cổng 2 phải đo lại trên tri thức do người viết. Số nhánh-đợt kỳ vọng: chạy
  tuần tự là 2 + 2p, chạy gộp là 3, với p là xác suất qua cổng 1. Gộp chỉ rẻ hơn khi p > 0,5; số liệu hiện
  có (chênh chi phí +2 %, trong khi cổng 1 đòi giảm ≥ 10 %) cho thấy p thấp. Phần dùng chung thật sự là
  `bint eval bootstrap`, chạy một lần cho cả hai cổng.

### 4.3. V0 — Sửa mã P6 và hồi quy tự động

**Chuẩn bị.** Lập một phiếu `b-do` cho `NT-P6-01..03`, một phiếu cho `NT-P6-04..05`; phạm vi `internal/evolve/**`.

**Kiểm.**

```sh
go test -count=1 ./...
go test -race -count=1 ./internal/evolve/... ./internal/workflow/...
GOOS=windows go vet ./...          # staticcheck cho windows: binary đã cài, đặt GOOS
```

- `NT-P6-01`: theo mẫu `workflow.AcquireWriteLock` (cặp `*_unix.go`/`*_windows.go`), không chép kiểu thu hồi
  theo PID chết. Test tranh chấp chạy với `-count=5`.
- Mỗi bản sửa phải có test **hỏng** khi hoàn nguyên bản sửa.
- Chạy lại bộ tái hiện ở `docs/evals/acceptance-p6-dff6570/`.

**Bằng chứng.** Commit sửa, tên test, kết quả lệnh trên. Ghi vào mục 5.

### 4.4. V1 — Phiên chạy thật trên máy

Một repository thử là **bản clone dùng một lần** của Bint (không chạy trên repo chính: tác vụ `shared` của phiên khác giữ khoá
ghi cây gốc và làm bẩn base của worktree, xem V1 2026-11-16), Claude Code và Codex đã đăng nhập; thêm trust Codex cho
đường dẫn clone. Agent chạy vòng và ghi bằng
chứng; người chỉ cần có mặt ở các điểm đánh dấu **(người)**.

**Vì sao cần người ở các điểm đó.**

- Thao tác chỉ dành cho người: mã chặn bằng `RequireHuman` (`ErrAgentForbidden` trong phiên agent). V1 dùng
  `bint mcp add|remove|index`, `bint mcp gateway stop|restart`, `bint task approve` (khi phiếu dừng ở cổng duyệt),
  `bint task land --3way|--force` (khi land xung đột). Người chạy trong một terminal riêng, không phải bằng `! <lệnh>` trong
  phiên agent: shell của phiên có `CLAUDECODE=1` nên Bint vẫn coi là agent và từ chối.
- Việc agent không với tới: mở thêm phiên Claude/Codex tương tác (subagent dùng chung role `interactive` của phiên
  cha nên không thay được), dùng Antigravity (ứng dụng có giao diện), xem TUI trên terminal thật, quan sát harness
  làm gì khi shim của chính phiên đó chết.

**Lịch hai buổi.** Thời lượng ước theo các lượt `b-do` nhỏ đã chạy (25–45 phút một phiếu).

| Buổi | Bước | Thời lượng | Người |
|---|---|---|---|
| A (~2,5 giờ) | 1. Chuẩn bị MCP | 10–15 phút | Cả bước |
| | 2. G4 + MCP | 60–90 phút | ~15 phút: mở thêm phiên, `gateway stop`, xem phiên có shim bị kill |
| | 3. `bypassPermissions` | 15–20 phút | Không, nếu phiên điều phối đã chạy ở chế độ này |
| | 5. TUI thật, VHS | 15–20 phút | ~10 phút xem TUI |
| B (~2,5 giờ) | 6. Nối tiếp phiên (Claude, Codex) | 1,5–2,5 giờ | Không, trừ `task approve` |
| | 4. Antigravity | 20–30 phút | Cả bước |
| | Biên bản | 30 phút | Không |

Rủi ro kéo dài: lỗi 403 của API (V1 lần trước); `go test ./...` hỏng vì đầy đĩa khi hai worktree chạy song song
(chạy lại gói hỏng với `-p 1`); lỗi mức cao thì dừng vòng (xem "Dừng khi"). Tới cuối buổi A mà phiên chưa nén ngữ
cảnh thì người gõ `/compact` để kiểm `NT-HOOK-COMPACT`.

**Bước 1 — Chuẩn bị (người).**

```sh
go install ./cmd/bint && bint init --hooks && bint doctor     # hook phải có SubagentStop và Stop
bint mcp add codegraph && bint mcp add specmatic && bint mcp add deepwiki
bint mcp index codegraph
bint mcp check                     # ghi kết quả từng service
bint doctor                        # gỡ thử một service rồi chạy lại: doctor phải báo thiếu
```

Đóng `NT-MCP-M1`. Thử thêm: chạy daemon codegraph với env hẹp rồi `bint mcp check` phải báo `degraded`.

**Bước 2 — Hai tác vụ song song bằng subagent.** Trong một phiên Claude Code, cấp `grants` cho role (ví dụ
`planner`, `reviewer`: `codegraph.explore`, `specmatic` nhóm chỉ đọc), rồi lập hai việc nhỏ không đụng tệp nhau:

```sh
bint task new --driver session --worktree "<việc 1>"
bint task new --driver session --worktree "<việc 2>"
```

Chạy cả hai bằng `/b-do --subagent …` cùng lúc. (người) Mở thêm 2 phiên Claude và 1 phiên Codex cùng repo (gọi
một công cụ `mcp__bint__*` mỗi phiên) để đủ 4 Claude + 1 Codex.

Trong lúc chạy, ở terminal khác:

```sh
bint mcp gateway status --json     # mỗi worktree một phân vùng codegraph/specmatic; số phiên đúng
bint tui                           # màn Tác vụ và màn MCP cập nhật đúng (NT-TUI-REAL)
```

Kiểm và ghi:

| Kiểm | Đóng |
|---|---|
| Cả hai phiếu tới `done`, cây gốc có đủ thay đổi | `NT-ISO-G4` |
| Worktree tác vụ có `.codegraph/codegraph.db`, `.codegraph/.gitignore`; không có `*.pid`, `daemon.*`, `-wal`/`-shm` chép từ gốc | chạy thật của `NT-MCP-SEED` |
| Yêu cầu một subagent sửa tệp ở cây gốc: bị `deny`, `bint runs show <run>` có `guardrail.triggered` | `NT-ISO-G4` |
| `gateway status`: hai phân vùng `worktree` khác PID | `NT-MCP-M2` |
| `kill <PID upstream codegraph>` rồi gọi lại: generation tăng, lời gọi thành công | `NT-MCP-M2` |
| Bước `b-review` không liệt kê `spec_manage_mock_server`; gọi thẳng thì bị từ chối, `mcp.called` có `outcome: denied` | `NT-MCP-M3` |
| `mcp.called` trong spool đúng run/role cho từng bước | `NT-MCP-M3` |
| (người) `bint mcp gateway stop` giữa phiên: lời gọi sau vẫn thành công (shim khởi động lại daemon) | `NT-MCP-M3` |
| Kill tiến trình shim của một phiên Claude và một phiên Codex (không phải phiên điều phối); (người) ghi lại harness làm gì | `NT-MCP-S3` |
| Bước con của Claude và của Codex (`roles.implementer: codex` cho một phiếu) có dòng `knowledge_citations` | `NT-MCP-M0` |
| Đo PSS mọi tiến trình MCP (`/proc/<pid>/smaps_rollup`) với hai worktree | `NT-MCP-M5-WT` |
| Một bước có `maxTurns` thấp dừng đúng trần | `NT-ISO-MAXTURNS` |
| Phiên đủ dài để nén ngữ cảnh: spool có `session.started` với `source=compact` | `NT-HOOK-COMPACT` |

**Bước 3 — `bypassPermissions`.** Phiên Claude Code ở chế độ `bypassPermissions` (người chọn chế độ khi mở phiên;
nếu phiên điều phối đã ở chế độ này thì agent tự chạy). Chạy một phiếu `--driver session` nhỏ, yêu cầu planner sửa
tệp và chạy lệnh ngoài `readonly_tools`. Vế hook `deny` đã đạt ở V1 lần trước; còn ghi `Bash(<mẫu>)` trong frontmatter
subagent chỉ đọc có giới hạn lệnh không, kể cả với subagent chạy nền. Đóng `NT-ISO-BYPASS`.

**Bước 4 — Antigravity (người).** `bint init --mcp-user`, mở `agy` trong workspace, gọi một công cụ Bint, chạy
một `b-do` nhỏ. Lưu một payload hook thật để so với adapter. Đóng `NT-AGY-REAL`.

**Bước 5 — TUI thật.** (người) Chạy `NO_COLOR=1 bint tui`, `bint tui --read-only`, phím `?` ở từng thẻ phụ, màn
MCP với service thật (`NT-TUI-REAL`). Agent chụp VHS các màn chính (`NT-TUI-VHS`). `NT-TUI-USER` đã bỏ (mục 3.2).

**Bước 6 — Nối tiếp phiên của bước, driver process.** Agent chạy; dùng `kill` harness thay cho Ctrl-C. Người chỉ
cần khi phiếu dừng ở `bint task approve`. Một phiếu nhỏ `bint run b-do --task <id>` (một lần với
`roles.implementer: claude`, một lần với `codex`). Giữa b-feat, Ctrl-C (hoặc `kill` harness) rồi `bint run resume <id>`;
kiểm `bint run status <id>` hiện `last_step` (`via claude|codex`, `stop_class: external`, `continuable`) trước và `notes` "nối
tiếp phiên …" sau. Lặp với: (a) phiên bị xoá trước resume (note "không còn"), (b) `--fresh`, (c) một vòng câu hỏi mở b-req
(`bint task write <id> requirement -` ghi trả lời rồi `bint run resume <id>`: planner được nối tiếp, thêm Q-2 thì dừng lần nữa).
Ghi cho từng harness: số liệu của `NT-RESUME-CLAUDE`/`NT-RESUME-CODEX`, chi phí Bint tính cho bước nối tiếp so với phần chênh của
tệp phiên, và việc agent có làm lại phần đã xong không. Đóng `NT-RESUME-CLAUDE`, `NT-RESUME-CODEX`.

**Bước 7 — Profile harness (`bint iso`).** (người ở bước đăng nhập) Trong bản clone: `bint iso import --from-ai-isolator --dry-run`
rồi nhập `codex--personal` (`NT-ISO-PROF-IMPORT`; thêm `--with-sessions` và `codex resume` trong profile); `bint iso create codex --name work`
rồi `bint run b-do --task <id>` với `harnesses.codex.profile: work` trong `.bint/local.yml` (`NT-ISO-PROF-CODEX`); `bint iso login claude
--name work` rồi một bước Claude trong profile (`NT-ISO-PROF-CLAUDE`); sau đó `bint iso du` và `du -sh` thư mục profile, kiểm không có cache
riêng sau phiên có MCP `uv`/`npx` (`NT-ISO-PROF-CACHE`). Lưu `bint iso status --json`, `bint iso du --json`, `bint iso sync --check`, `sha256sum` store
ai-isolator trước/sau. `NT-ISO-PROF-MAC` chỉ chạy được trên máy macOS: cùng các lệnh `create`/`login claude`/`import`.

**Bằng chứng.** `docs/evals/v1-real-session-<ngày>/`: `gateway status --json`, trích spool cho từng kiểm,
`runs show`, bảng PSS, ghi chú S3, ảnh VHS, số liệu nối tiếp phiên.

**Dừng khi.** Một kiểm lộ lỗi mức cao: dừng vòng, lập phiếu sửa, chạy lại riêng kiểm đó.

### 4.5. V2 — Windows thật

```powershell
go test -count=1 ./...
go test -count=1 ./internal/workflow/ ./internal/mcpgw/ ./internal/tui/...
```

Rồi chạy tay: hai `bint run` song song trên cùng repo (`NT-WIN-LOCK`); một phiếu `--worktree` tới `land`, một
phiếu `cancel`, một repo có đường dẫn dài (`NT-WIN-WT`); `bint mcp gateway start`, `status`, kill daemon rồi
gọi lại (`NT-WIN-MCP`); `bint tui` trong Windows Terminal (`NT-WIN-TUI`); một `bint run b-feat` với Codex
(`NT-WIN-CODEX`).

Thêm `bint iso create/run/sync/gc/remove` và `bint iso import --from-ai-isolator --source <bản chép store>` (`NT-ISO-PROF-WIN`).

**Bằng chứng.** `docs/evals/v2-windows-<ngày>/`: phiên bản Windows, Go, git; đầu ra test; ghi chú từng kiểm.

### 4.6. V3 — Một đợt eval trên `validation`

**Chuẩn bị.**

- (người) Thêm guardrail mẫu ở `candidate`: `bint harness component add --template test-before-stop`.
- Chốt model thực thi rẻ (theo Kế hoạch B: Haiku) và ghi vào biên bản **trước khi chạy**.
- `bint eval estimate --split validation --arms none,bint,bint@harness=all`.

**Chạy.**

```sh
bint eval run --split validation --arms 'none,bint,bint@harness=all' \
  --repeats 1 --model <model rẻ> --budget 15 --yes
bint eval report <batch> --json > v3.json
```

**Đọc kết quả.**

| Câu hỏi | Lấy từ | Quyết định |
|---|---|---|
| B0: tỷ lệ đạt của `none` trong 15–60 %? | nhánh `none` | Ngoài khoảng: đổi executor (model mạnh hơn một bậc) hoặc dừng hướng B; không chạy V4 với model này |
| B1 và bước 1 của E6: có lượt trượt để rút bài học? | nhánh `bint`; `bint experience digest <batch>` | Thiếu nhóm thất bại (dưới `evolution.trigger.min_similar_runs` tác vụ phân biệt): ghi nhận, V4 chỉ còn cơ hội quy trình |
| Bước 1 của H5: guardrail có tác động? | `bint` so với `bint@harness=all`, bảng `eval_guardrail_usage` | Agent đã tự chạy test ở hầu hết lượt: guardrail này vô tác dụng, đổi dấu hiệu khác trước V4 |

`NT-MCP-M5-QUAL`: hiện chưa có biến thể nhánh bật/tắt `grants`. Nếu thêm được trước V3 thì chạy thành nhánh
thứ tư của cùng đợt; nếu không thì hoãn, không chạy đợt riêng chỉ cho mục này.

**Bằng chứng.** `docs/evals/v3-validation-<ngày>/`: `v3.json`, bản tóm tắt thất bại, bảng guardrail.

### 4.7. V4 — Một vòng `evolve cycle` và xác nhận trên `test`

**Điều kiện.** V0 xong (khoá và hậu kiểm của evolve đúng); V3 qua cổng B0.

**Chuẩn bị.** `roles.reflector: claude`, `harnesses.claude.models.reflector: opus`; evolution máy đang tắt nên
bật theo [evolution.md](evolution.md) cho riêng vòng này và ghi lại.

**Chạy.**

```sh
bint evolve scan                                  # liệt kê cơ hội: failure_lesson, harness, L1/L2
bint evolve cycle --model <model rẻ> --budget 25 --max 3 --json > v4-cycle.json
```

Vòng tự đối chiếu trên `validation` rồi `test` với Probe tuần tự (dừng sớm). Nếu có đề xuất `supported`
trên `test`, xác nhận lại:

```sh
bint evolve evaluate <proposal> --split test --model <model rẻ> --budget 10    # 2 lần lặp
```

**Kết luận.**

| Kết quả | Ghi |
|---|---|
| Có đề xuất `supported` trên `test` và xác nhận lại đạt | `NT-EVO-TEST` đạt. Loại đề xuất quyết định mục đi kèm: `failure_lesson` → `NT-E6`; `harness` → `NT-H5`; tri thức L1 → P6; skill L2 → P7. P8 đạt khi vòng tự động cho ra đề xuất đó. |
| Không đề xuất nào đạt sau 3 đề xuất | Kết luận âm tính cho cấu hình này. `NT-E6`, `NT-H5`, B đóng với kết quả âm; P6/P7/P8 giữ "Chưa đạt". Không chạy lại cùng cấu hình. |
| Gần chạm trần | Dừng, hỏi chủ dự án. |

**Bằng chứng.** `docs/evals/v4-cycle-<ngày>/`: `v4-cycle.json`, đề xuất và kết quả đối chiếu, chi phí từng phần.

### 4.8. V5 — Kế hoạch A: tri thức do người viết

Thiết kế và ràng buộc chống rò rỉ: [p8-next-experiments.md, Kế hoạch A](p8-next-experiments.md).

1. Viết mã `bint eval snapshot import --base <commit> --from <thư mục>` và nhãn `knowledge_origin: human`
   (không token).
2. Chọn vùng mã có ≥ 20 tác vụ `validation`/`test` với commit gốc gần nhau. Xem tỷ lệ đạt của `none` trên vùng
   đó từ kết quả V3: dưới 10 % hoặc trên 70 % thì chọn vùng khác **trước khi** người viết tri thức.
3. (người) Checkout commit gốc sớm nhất trong một worktree riêng, viết 10–20 mục, không mở danh sách tác vụ
   hay commit sửa lỗi.
4. Chạy `bint` với tri thức người viết trên các tác vụ của vùng, 2 lần lặp. Nhánh `none` dùng lại từ V3 nếu
   cùng model và cùng tác vụ; chủ dự án chốt việc dùng lại này trước khi chạy (Kế hoạch A gốc ghi Sonnet).
5. Cổng: theo Kế hoạch A (Δ ≥ +15 điểm % và khoảng tin cậy không chứa 0 → ủng hộ; Δ ≤ 0 → kết luận âm).

### 4.9. V6 — Cổng quyết định 1 và 2

Ngưỡng và điều kiện: [gate-1.md](gate-1.md), [gate-2.md](gate-2.md). Không chép lại ở đây để tránh hai nguồn.

**Điều kiện để phép đo được tính** (tóm tắt, bản đầy đủ ở phụ lục): ≥ 2 repository khác hệ sinh thái, mỗi
repository ≥ 20 tác vụ `test` đã kiểm chứng fail→pass, 3 lần lặp, cùng harness và model trong một đợt.

**Harness.** Cổng 1 cần chi phí USD hoặc `num_turns`; Codex và Antigravity không báo hai số này, nên chạy trên
Claude Code.

**Trình tự trên mỗi repository.**

```sh
bint init --no-hooks
bint eval mine --verify && bint eval approve <id>...
bint eval bootstrap --split test --budget <USD> --yes            # một lần, dùng cho cả hai cổng
bint eval estimate --split test --arms none,minimal --effect 0.1
bint eval run --split test --arms none,minimal --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate1-<repo>.json
```

Cổng 1 qua trên cả hai repository thì mới chạy cổng 2:

```sh
bint eval run --split test --arms minimal,bint --repeats 3 --leakage-mode strict --budget <USD> --yes
bint eval report <batch> --json > gate2-<repo>.json
```

Cổng 1 không qua: theo mục "Khi không qua" của gate-1.md (chẩn đoán trên `validation`, tối đa 2 vòng); sau đó
cổng 2 đo trên tri thức do người viết, dùng lại kết quả V5 làm điểm xuất phát.

## 5. Nhật ký nghiệm thu

Thêm một dòng sau mỗi vòng. Kết luận do chủ dự án ký.

| Ngày | Vòng | Commit | Mã đã đóng | Mã còn mở sau vòng | Biên bản | Chi phí | Ký |
|---|---|---|---|---|---|---|---|
| 2026-11-10 | (trước khi có tài liệu này) | `8b5cc9f` | R1–R3, F1, F3, F7, F8 (một phần), F11, F12 | — | [open-findings](bint-acceptance-open-findings.md) | 0 | |
| 2026-11-12 | (trước khi có tài liệu này) | `2d1d901` | G3 chạy thật | G4, bypassPermissions, Windows | [isolation 12.16](isolation-subagent-plan.md) | — | |
| 2026-11-13 | (trước khi có tài liệu này) | `b958512` | M5 bộ nhớ (một worktree) | M5 chất lượng, nhiều worktree | [mcp §11](mcp-gateway-plan.md) | 0 | |
| 2026-11-16 | V1 chạy lại, buổi A (repo chính) | `6e84247` + (commit này) | `NT-ISO-G4` (kèm điều kiện), `NT-MCP-M2`, `NT-MCP-M5-WT` (đã đo); `NT-MCP-SEED` chạy thật | `NT-MCP-CALLED`, `NT-ISO-RO-FP`, `NT-WT-BASE`, `NT-MCP-PROBE-CAP` (mới); `NT-MCP-M3`, `NT-MCP-S3` một phần; còn lại của V1 chưa chạy | [v1-real-session-2026-11-16](evals/v1-real-session-2026-11-16/README.md) | 0 token đo; phiên agent 3,20 USD cho hai phiếu (chưa tính phiên điều phối) | |
| 2026-11-16 | Cập nhật tài liệu | `6e84247` | — (ghi nhận `NT-MCP-SEED`, `NT-P6-06` đã đóng; chia V1 thành hai buổi) | V1 còn lại, V2–V6 | mục 3.1, 4.4 | 0 | |
| 2026-11-15 | Phiếu `NT-MCP-PROBE` | `2ac7e13` | `NT-MCP-PROBE`; low của `NT-MCP-CAT` | low: hạn chung `mcpCheckTimeout`, chưa kiểm daemon thật env hẹp | tác vụ `01M7FSQACD3XBT79JEWYC958YK` | 0 token đo | |
| 2026-11-15 | Phiếu chỉ mục codegraph cho worktree | `8fe0577` | `NT-MCP-SEED` (test; chạy thật ở V1) | — | tác vụ `01M7FMB6G78VTCCTRPW8R4F545` | 0 token đo | |
| 2026-11-15 | V0b | `765ae91` | low của `NT-WF-BASELINE` (.2–.4), `NT-ISO-LEAK` (dựng `paths`); lỗi C1–C3 driver session | C1–C3 chưa thử trên phiên điều phối thật | tác vụ `01M7FKMZDVQ3XFR0EC1H5P2AXM` | 0 token đo | |
| 2026-11-15 | Phiếu `NT-P6-06` | `61222e8` | `NT-P6-06` | — | tác vụ `01M7FGG2H0KZZ6G4CDDPYAE0TT`, [biên bản V0](evals/v0-p6-fixes-2026-11-13/README.md) | 0 token đo | |
| 2026-11-13 | Phiếu sửa `NT-ISO-LEAK-FP` | (commit này) | `NT-ISO-LEAK-FP` | `NT-ISO-G4` (chạy lại), `NT-MCP-PROBE`, `NT-P6-06` | tác vụ `01M7AQSQ89E8Z6YHBPCM08ENKT` | 0 token đo; phiên agent $1,01 (b-feat 0,25 bị chặn nhầm + 0,18, b-review 0,59) | |
| 2026-11-13 | Phiếu sửa `NT-WF-BASELINE` | (commit này) | `NT-WF-BASELINE` | `NT-ISO-LEAK-FP` (mới), `NT-ISO-G4` (chạy lại), `NT-MCP-PROBE`, `NT-P6-06` | tác vụ `01M7AJMTHSJPN53DRYQN1PR4M8` | 0 token đo; phiên agent $6,04 (b-req 2,68, b-feat 1,77 + 0,15 chạy lại do `NT-ISO-LEAK-FP`, b-review 1,44) | |
| 2026-11-13 | Phiếu sửa sau V1 | `4419157`, `e143459`, (commit này) | `NT-MCP-CAT`, `NT-ISO-LEAK` | `NT-ISO-G4` (chạy lại), `NT-WF-BASELINE`, `NT-MCP-PROBE` | tác vụ `01M7A80F2X6A803CFR491X14GR`, `01M7A9HJX8164HVWP2VKKWP0FG`, `01M7AC1XXEEG0SWVY75SQJ4XKV` | 0 token đo; phiên agent ≈ $9 | |
| 2026-11-13 | V1 (một phần, dừng sớm) | clone `1c727f8` | `NT-MCP-M1`; `NT-ISO-BYPASS` (vế deny) | `NT-ISO-LEAK`, `NT-MCP-CAT`, `NT-MCP-PROBE` (mới); `NT-ISO-G4` không đạt; còn lại của V1 chưa chạy | [v1-real-session](evals/v1-real-session-2026-11-13/README.md) | 0 token đo; phiên agent < $5 (ước) | |
| 2026-11-13 | V0 | `f1467a0` | `NT-P6-01..05` | `NT-P6-06`, `NT-WF-BASELINE` (mới), `NT-EVO-TEST` | [v0-p6-fixes](evals/v0-p6-fixes-2026-11-13/README.md) | 0 token đo; $10,06 phiên agent | |

## 6. Mẫu biên bản một vòng

```markdown
# <Vòng> — <ngày>

- Commit kiểm tra: <sha>. Môi trường: <OS, Go, phiên bản harness, model>.
- Ngân sách được duyệt: <USD>, người duyệt: <tên>. Chi phí thực: <USD>.
- Tác vụ validation/test do agent duyệt: có/không.

| Mã | Kết quả | Bằng chứng | Ghi chú |
|---|---|---|---|
| NT-… | đạt / không đạt / không kết luận | evidence/<tệp> | |

## Lỗi tìm được
## Chưa kiểm được và lý do
## Đề xuất kết luận (chờ chủ dự án ký)
```

## 7. Biên bản lịch sử

Bản ghi bất biến; hiện trạng lấy ở mục 2 và 3.

| Biên bản | Commit | Kết luận tại thời điểm đó |
|---|---|---|
| [P0–P4](bint-acceptance-p0-p4.md) | `c526334` | A1–A4, đã đóng sau đó |
| [P5](bint-acceptance-p5.md) | `25202fb` | Chưa đủ điều kiện; P5-01..03 đã sửa ở `8ff7416` |
| [Tái nghiệm thu P0–P5](bint-reacceptance-p0-p5-137dbf9.md) | `137dbf9` | Chưa đủ điều kiện; R1–R3 |
| [Đóng lỗi còn mở](bint-acceptance-open-findings.md) | `8b5cc9f` | Đóng R1–R3 và F1, F3, F7, F8 (một phần), F11, F12 |
| [P6](bint-acceptance-p6-dff6570.md) | `dff6570` | Chưa đủ điều kiện; P6-01..05 |
| [Các đợt chạy harness thật](evals/README.md) | theo đợt | Pilot Codex, Claude 30 tác vụ, flow Codex, hỗn hợp, nghiệm thu Codex 2026-11-12 |
