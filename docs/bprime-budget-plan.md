# Kế hoạch: giảm quỹ thời gian và quỹ token của luồng B′

Trạng thái: bản nháp chờ người dùng duyệt (2026-11-21). Chưa việc nào được làm thử; mọi con số tiết kiệm là ước tính từ một lượt đo
(B′-1 đợt 7, `reports/Đánh giá đợt 7 luồng Bint.md`).

## Tiến độ (2026-11-21, chưa commit)

| Mục | Trạng thái | Chỗ trong mã |
|---|---|---|
| 1.1 nối `autoCheck` | xong (phiên khác) | `bgverify.go` vòng thăm |
| 1.2 cổng theo pha | xong | `check.go` (`coreGate`, `gateAfterVerify`, `wideAfterGate`), `run.go` (`verifyAfterImplement`), `gate.go` (`redVerification`) |
| 1.3 + 1.9 tập đỏ nền, phân loại lần đỏ | xong, dạng theo yêu cầu: chạy lại riêng test đỏ trên cây hiện tại rồi trên mốc tác vụ, chỉ khi lần kiểm chứng đỏ | `internal/testreport`, `redtriage.go`, `Verification.RedClass/RedTests` |
| 1.4 báo đỏ sớm | xong cho kiểm nền: ghi kết quả tạm (`Partial`) ở gói đỏ đầu tiên, lệnh vẫn chạy hết | `bgverify.go` (`firstFail`), `eval.RunShellWatch` |
| 1.5 một khe chạy test, huỷ lần nền lỗi thời | chưa làm: huỷ lần nền đang chạy có thể làm mất tín hiệu theo đồ thị khi agent sửa liên tục; cần số đo trước | — |
| 1.6 thu hẹp tới mức test | chưa làm: cổng lõi đã hẹp theo gói; cần lưu thời lượng từng gói trước | — |
| 1.7 `workflow.verify.done: graph` | xong (mặc định vẫn full) | `config.go`, `verifycover.go`, `run.go` (`reverifyIfStale`) |
| 1.8a lỗi "Reconnecting" | xong (phiên khác) | `agent_harness.go` |
| 1.8b lưu tệp thăm dò | xong (phiên khác) | `bprime_probe.go` |
| 1.8c tóm tắt đỏ của `bint task check` | xong | `check.go` (`redSummary`), `internal/cli/task.go` |
| 1.8d chặn chạy lại toàn bộ bộ test trong b-feat B′ | xong (deny, chỉ lệnh trùng `verify.command` hoặc cùng mẫu `./...`) | `internal/harness/hookcore/widetest_guard.go` |
| 1.8e thăm dò có điều kiện | xong: b-req gọn ghi `Kiểm tra mục tiêu độc lập (b-goal): không — …` | `bprime.go` |
| 1.8f token cache theo bước | chưa làm: cần thêm cột vào bảng `runs` (migration) | — |
| 1.10 phát hiện treo | xong: `workflow.idle_timeout_minutes` (0 = 30 phút, -1 = tắt); trần theo loại bước không làm vì phát hiện treo đã phủ ca 176 phút | `eval/shell.go` (`runProcessIdle`), `eval/agent.go`, `run.go` |
| 1.11 review vòng sau chỉ nhận diff mới | chưa làm: reviewer cần thấy cả diff để bắt bản sửa làm hỏng chỗ khác | — |
| Ngân sách tổng | xong (phiên khác) | `slice.go` (`budgetLeft`) |
| Sau đợt 9 (a): phân loại đỏ của cổng lõi | xong: cổng lõi đỏ thì chạy lại riêng test đỏ (không chạy lại test hết giờ); hết giờ, test hay đỏ, đỏ sẵn ở mốc không mở lượt sửa lõi | `redtriage.go` (`redRerun`, `triageGate`), `check.go` (`coreGate`), `gate.go` (`redVerification`) |
| Sau đợt 9 (b): trần lượt sửa lõi | xong: `slice_timeout_minutes`, không đặt thì 1/4 `budget_minutes` | `slice.go` (`coreFixCap`) |
| Sau đợt 9 (c): lượt bị ngân sách cắt không failed | xong: lượt b-feat trước review bị rút trần (ngân sách, sửa lõi) mà chạm trần thì giữ cây, chạy cổng kiểm rồi vào review; hết ngân sách sau lõi thì bỏ sửa lõi/mở rộng | `run.go` (`softCap`), `slice.go` (`bprimeSoftCap`, `budgetSpent`), `bprime_probe.go` |

## Vấn đề

1. **Kiểm chứng ăn khoảng nửa thời gian tường.** Đợt 7, B′: 109,5 trên 218 phút. Sau **mỗi** lượt b-feat (lõi, sửa lõi, mở rộng, sửa
   sau review) `afterStep` chạy `VerifyScoped` ở tiền cảnh (`internal/workflow/run.go:2017-2024`); finalize chạy thêm một lần full.
   Với `scope: affected`, phạm vi tính từ mốc tác vụ và lan theo đồ thị phụ thuộc: 39 gói, 13,5–31 phút mỗi lần.
2. **Kiểm hẹp chưa thay được nó.** `autoCheck` (`internal/workflow/bgverify.go:87`) chỉ được gọi từ test; vòng thăm chỉ gọi
   `b.tick("")` (`bgverify.go:77`). Agent trong sandbox không chụp được cây nên `Tree` rỗng, kiểm nền không khởi chạy,
   `reuseBackground` không bao giờ trúng.
3. **Test không tập trung.** Tầng hẹp dừng ở mức gói (`{test_dirs}`); sửa một tệp trong gói chậm là chạy cả gói.
4. **Cây bẩn làm hỏng kết quả**, theo ba cách:
   - kiểm chứng, kiểm hẹp và cổng mục tiêu chạy trong worktree agent đang sửa; test sinh tệp làm đổi fingerprint;
   - không có tập đỏ nền: test đỏ sẵn ở mốc tác vụ bị quy cho tác vụ và mở vòng sửa vô ích;
   - kiểm nền, kiểm tiền cảnh và test của thăm dò chạy chồng nhau, sinh đỏ giả do tải.
5. **Tín hiệu đỏ dùng muộn.** Bint chỉ dùng kết quả khi cả lệnh kết thúc (đợt 7: gói đầu tiên FAIL ở phút ~40, dùng ở phút 103).
6. **Token bị đốt lại.** Lượt đã có `turn.completed` vẫn bị đánh `failed` khi luồng có một sự kiện `error` tạm thời
   (`internal/eval/agent_harness.go:248-251`, `codexStreamErrors`): 9,6M token ở đợt 7. Thăm dò chạy lại khi tiến trình chết: 6,6M.
7. **Dự án không phải Go không có kiểm hẹp.** `affected.DefaultSources()` chỉ có `GoSource`; dự án khác rơi vào
   `unsupported_language` → `full` → `Check` bỏ qua. Xử lý ở giai đoạn 2.

## Bằng chứng từ lịch sử tác vụ của chính repo này

Nguồn: 60 tác vụ trong `<state>/runtime/tasks/*/state.json`, bảng `runs` của `state.sqlite`, và các `logs/verify-*.log` (đọc ngày
2026-11-21). Toàn bộ là luồng classic; chưa tác vụ nào có `checks` hay `background_verifications`. Thời lượng một lần kiểm chứng
tính từ lúc tạo tệp log tới `at`.

- **57 % số lần kiểm chứng đỏ, và phần lớn không do mã của tác vụ.** 105 lần, 1 644 phút; 60 lần đỏ:

  | Loại đỏ | Số lần | Phút |
  |---|---|---|
  | `panic: test timed out after 10m0s` (lệnh `go test ./...` không có `-timeout`, gói `internal/workflow` và `internal/tui/app`) | 21 | 221 |
  | Có test đỏ riêng của tác vụ | 18 | 314 |
  | Chỉ đỏ ở test hay đỏ (đỏ ở từ 3 tác vụ khác nhau trở lên, ví dụ `TestEvalRunEscCancelsBatchAndShowsResumeCommand` ở 11 tác vụ) | 9 | 165 |
  | Quá `verify_timeout_minutes` | 8 | 210 |
  | Không đọc được test đỏ từ log | 4 | 126 |

  Ít nhất 38 trên 60 lần đỏ (596 phút, 36 % tổng thời gian kiểm chứng) là hết giờ hoặc test hay đỏ. Mỗi lần như vậy còn kéo theo
  một vòng sửa hoặc một lần dừng: finalize bị chặn vì "lần kiểm chứng cuối không qua" 16 lần.
- **`scope: affected` chưa nhanh hơn full ở repo này.** Trung vị lần đỏ tầng `package` là 19,4 phút, full là 10,5 phút; và 7 lần
  phải chạy lại full ở finalize.
- **Bước treo ăn hết trần.** b-review bình thường p50 2,3 phút, p90 4,5, dài nhất 11,1; hai lượt treo 176 phút mỗi lượt. b-feat
  p50 21 phút, p90 55; 6 lượt chạm trần 60/90/180 phút (424 phút). Tiến trình điều phối chết bất thường 11 lần.
- **Chi phí chia ba gần đều**: b-req 120,8 USD (46 lượt, 2,63 USD/lượt, opus), b-feat 119,1 USD (108 lượt, sonnet), b-review
  108,6 USD (94 lượt, opus). Lập kế hoạch và review chiếm ~65 % tiền dù chỉ ~20 % thời gian agent. Khoảng 55 % tác vụ có từ hai
  lượt review trở lên.

## Mục tiêu

- Kiểm chứng chặn luồng giảm từ ~50 % xuống dưới 20 % thời gian tường của một tác vụ B′.
- Kết quả kiểm chứng đầu tiên về trước 25 % ngân sách b-feat.
- Không vòng sửa nào mở vì test đỏ sẵn hoặc đỏ do tải.
- Không lượt agent nào chạy lại vì sự cố mà kết quả của nó đã có trên đĩa.
- Cổng done không yếu đi: mặc định vẫn có đúng một lần kiểm đầy đủ trước khi done.

Ngoài phạm vi: chất lượng thước đo thăm dò (danh mục ngoài, PEM), khung benchmark, cổng hiệu năng.

## Nguyên tắc thiết kế

- **Go trước, tổng quát sau, nhưng tách ngay từ đầu.** `internal/workflow` không được biết tới `go test`. Ba điểm tách:
  bộ đọc kết quả test (interface), chọn test (`affected.Source`, đã có), mẫu lệnh của guardrail (lấy từ cấu hình/phiếu).
- **Cơ chế nào cũng để lại dấu vết tất định trong `state.json`** (bài học đợt 7: cơ chế chỉ là lời dặn trong đề bài thì coi như
  chưa tồn tại).
- **Không bao giờ thu hẹp im lặng.** Lần kiểm bị bỏ qua, bị huỷ hay dùng lại kết quả cũ đều ghi lý do.

## Giai đoạn 1 — Go

### 1.1 Nối `autoCheck` vào vòng thăm

- Sửa: `bgverify.go`, goroutine trong `startBackground` gọi `b.autoCheck(time.Now())` trước `b.tick("")`.
- Test: một test chạy vòng thăm thật (rút `bgPollInterval`, `autoCheckQuiet`), khẳng định `State.Checks` có lần `By: "bint"` và
  kiểm nền khởi chạy từ `Tree` của nó. Test hiện có chỉ gọi `autoCheck` trực tiếp nên không bắt được lỗi nối dây.
- Cỡ: ~5 dòng + 1 test.

### 1.2 Cổng theo pha

Thay lần kiểm rộng sau mỗi lượt b-feat bằng bảng sau (chỉ tác vụ B′; classic giữ nguyên):

| Sau lượt | Kiểm chặn luồng | Kiểm rộng |
|---|---|---|
| lõi | kiểm hẹp (gói đổi kể từ lần xanh trước) | nền, không chờ |
| sửa lõi | kiểm hẹp | nền, không chờ |
| mở rộng | kiểm hẹp + thước đo đóng băng | **một lần** tầng đồ thị trước review; dùng lại kết quả nền nếu đúng cây |
| sửa sau review | kiểm hẹp + thước đo đóng băng | tầng đồ thị, dùng lại kết quả nền nếu đúng cây |
| finalize | — | full một lần (như hiện nay), trừ khi bật 1.7 |

- Sửa: `run.go` nhánh `flow.KindImplement` của `afterStep`: với B′ ở pha lõi/sửa lõi, gọi `r.check(ctx, id, true)` thay cho
  `VerifyScoped`; `redVerification` (`gate.go:370`) nhận thêm lần kiểm hẹp đỏ của lượt vừa xong để `bprimeAfterCore` vẫn mở lượt
  sửa lõi. `bprime.go`: cập nhật `bprimeCoreBlock`/`bprimeCheckBlock` cho khớp.
- Kiểm hẹp bị bỏ qua (phạm vi full, thiếu `affected_command`) thì lùi về hành vi hiện tại, kèm ghi chú.
- Test: mở rộng `bprime_verify_test.go`, `bprime_reorder_test.go`: đếm số lần `VerifyScoped` trên một tác vụ B′ đủ pha.
- Cỡ: ~80–120 dòng + test.

### 1.3 Bộ đọc kết quả test và tập đỏ nền

- Gói mới `internal/testreport`: `type Parser interface { Parse(log io.Reader) (Report, error) }`,
  `Report{Failed []TestID; Durations map[Unit]time.Duration}`. Bản cài đầu: `go test -json`. Lệnh không xuất định dạng nhận ra
  được thì `Report` rỗng và mọi cổng lùi về mã thoát.
- Khoá cấu hình mới `workflow.verify.report: none | gotest-json` (mặc định `none`). Theo quy tắc dự án: sửa struct, `Default()`,
  `Validate()`, mẫu `Render` (`internal/config/template.go`, ký tự `%` viết `%%`).
- Tập đỏ nền: ngay khi b-req xong và lệnh được xác nhận, chạy `verify.command` trên mốc tác vụ trong worktree tạm, song song với
  lượt lõi; lưu `State.BaselineFailed` (danh sách test) hoặc `BaselineRed: true` (không có báo cáo).
- Cổng: `Verification` thêm `NewFailed []string`. Đỏ mà `NewFailed` rỗng thì không mở vòng sửa, ghi chú "đỏ sẵn ở mốc tác vụ".
  Không có báo cáo và mốc đã đỏ thì dừng chờ người một lần thay vì lặp vòng sửa.
- Rủi ro: đỏ nền chạy trên checkout sạch nên thiếu tệp `.gitignore`; với Go thường không ảnh hưởng, giai đoạn 2 xử lý bằng
  `setup_command`. Test flaky xanh ở lần đo nền sẽ không vào tập nền; chấp nhận, đo lại ở 1.4 nếu cần.
- Cỡ: ~200–300 dòng + test.

### 1.4 Báo đỏ sớm

- Sửa: `eval.RunShell` nhận callback theo dòng; khi `report` khác `none`, bộ đọc phát sự kiện "đơn vị đầu tiên FAIL".
- Hành vi: kiểm hẹp và kiểm chặn ở các pha sửa ghi kết quả đỏ ngay, huỷ phần còn lại; lần kiểm rộng trước review chạy hết (cần
  danh sách đỏ đầy đủ cho reviewer). `Verification` thêm `Partial: true` cho lần bị cắt; cổng done không nhận lần `Partial`.
- Cỡ: ~80–120 dòng + test.

### 1.5 Một khe chạy test nặng, huỷ lần nền đã lỗi thời

- Sửa: `bgverify.go`. Kiểm nền nhường khi kiểm chặn bắt đầu (dừng, chạy lại sau trên cây mới nhất); `tick` huỷ lần đang chạy khi
  có kiểm hẹp xanh mới hơn trên cây khác thay vì chờ nó xong.
- Dùng khoá sẵn có (`lockStep`, `fslock.Try`), không dựng khoá theo PID.
- Cỡ: ~40–60 dòng + test.

### 1.6 Thu hẹp tới mức test cho gói chậm (trong `GoSource`)

- `Report.Durations` của các lần chạy trước lưu theo gói trong state của dự án.
- `affected.Input` thêm `SlowOver time.Duration`; `GoSource.SelectDirect` với gói vượt ngưỡng trả kèm biểu thức `-run` gồm: test
  trong tệp `_test.go` bị đổi và test gọi tới hàm bị đổi (codegraph, nếu chỉ mục có; không thì chỉ tệp test đổi).
- `affected_command` thêm placeholder `{run_filter}`; lệnh không có placeholder thì bỏ qua thu hẹp này.
- Chỉ áp cho tầng hẹp. Tầng đồ thị và full luôn chạy cả gói.
- Cỡ: ~150–200 dòng + test. Làm sau khi 1.1–1.5 có số đo, vì đây là mục dễ sai nhất.

### 1.7 Cổng done nhận lần xanh tầng đồ thị (tuỳ chọn)

- Khoá `workflow.verify.done: full | graph` (mặc định `full`). `graph`: `partialVerifyOnly` không ép chạy full khi lần cuối là
  tầng `graph`/`package` xanh trên đúng cây; full để CI hoặc bước land.
- Mất: phụ thuộc không qua import. `testdata`, embed, `go.mod`, CI đã tự nới ra full nên không thuộc phần mất.
- Cỡ: ~30–50 dòng + test.

### 1.8 Token

| # | Việc | Tệp | Cỡ |
|---|---|---|---|
| a | Lượt có `turn.completed` sau sự kiện `error` thì không ghi `res.Error`; chỉ lỗi sau lần `turn.completed` cuối mới tính | `internal/eval/agent_harness.go` (`codexStreamErrors`) | ~15 dòng + test |
| b | Lưu tệp thăm dò xuống thư mục tác vụ, resume đọc lại | `internal/workflow/bprime_probe.go` | đang làm dở trong cây (`saveProbe`) |
| c | `bint task check` in tóm tắt đỏ: tên test đỏ mới và tối đa N dòng đầu mỗi lỗi, trần ~2 KB; không có báo cáo thì in đuôi log có trần | `internal/cli/task.go`, `check.go` | ~40 dòng |
| d | Guardrail dựng sẵn `pre_bash` cho b-feat B′: chặn lệnh trùng `verify.command` của phiếu và mẫu "chạy toàn bộ" lấy từ cấu hình, trỏ sang `bint task check` | `internal/hcomp`, hook | ~40–60 dòng |
| e | Thăm dò có điều kiện: b-req gọn ghi một dòng `Thăm dò: có/không — <lý do>`; `bprimeSplit` đọc nó | `bprime.go`, `bprime_probe.go`, lint | ~40 dòng |
| f | Ghi token cache và không cache theo bước vào `state.json` | `internal/eval`, `internal/task` | ~50 dòng |

Mục d thêm thông báo lỗi nêu lệnh CLI: kiểm `cliRefs` và `litsrUsage` (`TestServiceMessageCLIInventory`).

### 1.9 Phân loại lần đỏ: hết giờ và test hay đỏ không mở vòng sửa

Bổ sung từ lịch sử tác vụ; dựa trên bộ đọc của 1.3.

- `Report` thêm `TimedOut []Unit` (Go: `panic: test timed out`); `Verification` thêm `RedClass: code | timeout | flaky | unknown`.
- **Hết giờ**: không mở vòng sửa b-feat. Chạy lại riêng các đơn vị hết giờ một lần với trần gấp đôi; vẫn hết giờ thì dừng chờ
  người kèm gợi ý sửa lệnh. Lần quá `verify_timeout_minutes` đã có nhánh riêng ở finalize (`reverifyIfStale`); áp cùng luật cho lần
  kiểm sau b-feat.
- **Kiểm lệnh lúc xác nhận**: `bint task approve` và b-req cảnh báo khi lệnh kiểm chứng không đặt trần mà lịch sử thời lượng
  (1.6) có đơn vị vượt 70 % trần mặc định của runner. Mẫu "trần mặc định" nằm trong bộ đọc, không trong `internal/workflow`.
- **Test hay đỏ**: lần đỏ có danh sách test thì chạy lại **riêng các test đỏ** một lần trước khi kết luận (cần `{run_filter}` của
  1.6); xanh ở lần hai thì ghi `RedClass: flaky`, không mở vòng sửa, và ghi quan sát vào tri thức (mục chờ người duyệt). Kết hợp
  với tập đỏ nền của 1.3.
- Cỡ: ~120–160 dòng + test.

### 1.10 Trần thời gian theo loại bước và phát hiện treo

- Khoá `workflow.step_timeout_minutes` theo loại bước (`plan`, `implement`, `review`, `goal`); thiếu thì dùng `timeout_minutes`.
  Mặc định đề xuất: review 20, plan 30. Theo quy tắc thêm khoá cấu hình.
- Phát hiện treo: không có sự kiện nào từ harness trong N phút (`runs.last_event_at` đã có) thì dừng bước với
  `stop_class` ngoại cảnh để lần sau nối tiếp phiên, không chờ hết trần.
- Cỡ: ~80–120 dòng + test.

### 1.11 Giảm tiền của b-req và b-review

- B′ đã có sẵn hai đòn bẩy: b-req gọn và một lượt review. Số liệu lịch sử cho biết chúng nhắm đúng ~65 % chi phí.
- Lượt review thứ hai trở đi (classic, và vòng sửa của B′ nếu có kiểm lại): chỉ đưa diff kể từ vòng trước và các phát hiện còn mở.
- Thử model rẻ hơn cho b-req gọn; đo bằng tỷ lệ tác vụ phải hỏi lại hoặc bị `stopOnPlanning`.

### Thứ tự và phụ thuộc

`1.1 → 1.2 → 1.5` (một nhóm, cùng tệp) → `1.10` → `1.8a, 1.8c, 1.8f` → `1.3 → 1.9 → 1.4` → **đo lại** → `1.6`, `1.7`, `1.8d`,
`1.8e`, `1.11`.

Việc không cần mã, làm được ngay: thêm `-timeout` vào `verify.command` của các phiếu và mẫu lệnh mà b-req đề xuất.

Kiểm cho mọi mục: `go vet ./...`, `GOOS=windows go vet ./...`, staticcheck hai nền tảng, test gói `internal/workflow`. Test đóng
vai người gọi `asHuman(t)`.

## Giai đoạn 2 — ngôn ngữ khác

1. Placeholder `{changed_files}` cho `affected_command`; lệnh có nó thì nguồn "không hỗ trợ" không nới ra full
   (`jest --findRelatedTests`, `pytest --testmon`, `nx affected`, `cargo test -p`).
2. Nguồn dự phòng theo quy ước tên (tệp test đổi, test cùng tên, test cùng thư mục); chỉ dùng cho tầng hẹp.
3. Bộ đọc JUnit XML cho `workflow.verify.report`, kèm khoá đường dẫn báo cáo.
4. `bint init` dò hệ sinh thái và ghi `suggested_affected_command`.
5. `workflow.verify.setup_command` cho worktree kiểm chứng sạch (đỏ nền, kiểm nền); thiếu nó thì hai cơ chế này chạy trên cây
   sống lúc cây đứng yên.

Nếu B′ thành mặc định trước khi xong giai đoạn 2 thì kéo mục 1 lên giai đoạn 1.

## Ước tính tiết kiệm

Mốc so sánh: B′-1 đợt 7 đã bỏ phần do sự cố, giả định chạy tới done. Tác nhân ~86 phút, kiểm chứng chặn luồng ~95 phút (bốn lần:
31 + 13,5 + 25,3 và ~25 cho lần sau vòng sửa review), tổng ~185 phút; token vào 45,3M. Mọi số dưới đây là suy luận từ một lượt đo
trên máy đang tải, chưa có phép đo nào của chính các thay đổi.

### Thời gian tường

| Sau mục | Kiểm chặn luồng | Tổng | Giảm |
|---|---|---|---|
| Hiện tại | ~95 phút | ~185 phút | — |
| 1.1 + 1.2 + 1.5 | 45–60 phút | 135–150 phút | 20–27 % |
| + 1.6 | 30–35 phút | 118–125 phút | 32–36 % |
| + 1.7 | 8–15 phút | 95–105 phút | 43–49 % |

Giả định: kiểm hẹp mức gói 3–8 phút mỗi lần ở repo này (đợt 7 chạm các gói chậm `internal/cli`, `internal/app`); với `-run` còn
1–3 phút; lần tầng đồ thị ~25 phút và được dùng lại từ kiểm nền khoảng một nửa số lần.

Có điều kiện, không cộng vào bảng:

- 1.3: mỗi vòng sửa vì đỏ sẵn bị bỏ tiết kiệm một lượt agent và một lần kiểm (~20 phút ở đợt 7). Chỉ có khi mốc tác vụ đỏ sẵn.
- 1.4: lần kiểm đỏ ngắn đi 50–80 %. Chỉ có ở các lần đỏ.
- Kết quả kiểm chứng đầu tiên: từ 57 % ngân sách xuống khoảng 15–25 % (lượt lõi 29 phút cộng một lần kiểm hẹp).

### Theo lịch sử 60 tác vụ (classic)

- 1.9 và tập đỏ nền: bỏ được tới 38 lần đỏ không do mã, tức ~596 trên 1 644 phút kiểm chứng (36 %), cùng các vòng sửa và 16 lần
  finalize bị chặn đi kèm. Đây là cận trên: phân loại "test hay đỏ" dựa trên ngưỡng 3 tác vụ, chưa đối chiếu từng log.
- 1.10: hai lượt review treo (353 phút) còn ~40 phút; 6 lượt b-feat chạm trần (424 phút) chỉ được cắt sớm nếu là treo thật.
- 1.11: mỗi lượt review bỏ được tiết kiệm ~1,2 USD, mỗi b-req ~2,6 USD ở giá hiện tại; chưa ước tính được mức giảm của b-req gọn
  vì lịch sử chưa có tác vụ B′.

### Token

| Mục | Giảm | Khi nào |
|---|---|---|
| 1.8a + 1.8b | ~16M (26 % của 61,5M đo được) | chỉ khi có sự cố như đợt 7 |
| 1.8c + 1.8d | 1,5–4,5M (3–10 %) | mọi tác vụ; phỏng đoán, chưa có số đo đầu ra công cụ |
| 1.8e | ~16M (thăm dò 6,6M + lượt mở rộng 9,6M) | chỉ tác vụ được phép bỏ thăm dò |
| 1.3 | một lượt sửa lõi | chỉ khi mốc đỏ sẵn; chưa có số token riêng của lượt này |

Trên một tác vụ kiểu đợt 7 không sự cố, quỹ token chỉ giảm khoảng **5–10 %** (45,3M → ~41–43M). Kế hoạch này chủ yếu giảm thời
gian; token giảm mạnh chỉ ở tác vụ bỏ được thăm dò (~35 %) hoặc khi tránh được sự cố. Muốn giảm token sâu hơn cần đòn bẩy khác
(số lượt b-feat, cỡ ngữ cảnh mỗi lượt), và cần 1.8f trước: hiện phần lớn token vào là cache nên chưa biết chi phí thật nằm ở đâu.

## Câu hỏi mở

1. Mặc định của `workflow.verify.done` có nên là `graph` cho B′ không, hay giữ `full` tới khi có số đo?
2. Đỏ nền khi không có báo cáo test: dừng chờ người một lần, hay đi tiếp kèm ghi chú?
3. Guardrail 1.8d: chặn (`deny`) hay chỉ cảnh báo ở lần đầu?
