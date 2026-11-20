# Mã nguồn luồng B′: mức độ thực hiện khuyến nghị của báo cáo và các khoảng trống lộ ra ở đợt 7

Quy ước nguồn: mọi đường dẫn là tệp cục bộ. Repo mã: `D:\Projects\bint` (HEAD `63a6114`). Dữ liệu thô đợt 7: `D:\bench\bench-v7-20261120\runs\Bp-1\`
(viết tắt `<Bp-1>`), thư mục phiếu B′: `<Bp-1>\state\repo-a253372825da\runtime\tasks\01M7WP81F0XSXRJXQVQR9KCMZE\` (viết tắt `<phiếu>`).
Nhãn: **[Mã]** = tôi đọc trực tiếp trong mã; **[Log]** = tôi đọc trong tệp thô đợt 7; **[README]** = lấy từ README đợt 7, chưa tự kiểm; suy luận để riêng ở mục Inferences.
Tôi không chạy test nào; mọi nhận định về hành vi là từ đọc mã và log.

Hai phát hiện đổi cách đọc README đợt 7 (chi tiết ở câu hỏi 2):

1. Lượt b-feat LÕI của B′ (29 phút) chạy bằng cách nối tiếp phiên b-req, và lời nhắn nối tiếp (`planHandoffPrompt`) **không chứa** khối "LÕI trước" lẫn khối
   "kiểm hẹp theo mốc" của B′. Lượt dài nhất và sớm nhất chưa hề nhận chỉ dẫn `bint task check`.
2. Ở các lượt b-feat sau, agent **có gọi** `bint task check` (ít nhất 4 lần ở lượt sửa lõi, 8 lần ở mỗi lượt kế), nhưng lệnh tự bỏ qua vì sandbox Codex chặn ghi
   snapshot vào `.git/objects`. README ghi "0 lần: agent bỏ qua chỉ dẫn"; số 0 đúng với `state.checks` nhưng nguyên nhân ghi trong README là sai.

## 1. Từng dòng của bảng "Giữ, bỏ, đảo thứ tự" và đoạn "thước đo đóng băng": đã làm đủ, một phần hay chưa?

### Takeaway
Bốn commit dựng đủ khung của B′ (biến thể, b-req gọn, thăm dò hộp đen song song, review một lượt, lõi trước rồi mở rộng), nhưng hai khuyến nghị quan trọng nhất
về tín hiệu chỉ được làm ở mức "dặn agent trong đề bài": kiểm hẹp theo mốc và lối thoát thước đo sai. Phương án "người dùng xác nhận thước đo" và mọi nguồn thông tin
bên ngoài model đều chưa có.

### Cited Findings

Bảng đối chiếu (cột "Đợt 7" nói phần đó có thật sự chạy không):

| Khuyến nghị của báo cáo | Mã làm gì | Mức | Đợt 7 có chạy? |
|---|---|---|---|
| Một agent viết mã, ngữ cảnh liền mạch (Giữ) | b-feat lõi nối tiếp phiên b-req (`internal/workflow/plan_handoff.go:36-47`), vòng sửa nối tiếp phiên b-feat (`internal/workflow/gate.go:217-226`), lượt mở rộng "nối tiếp phiên lõi khi được" (`docs/workflows.md:1716`). Không riêng B′. | Đủ về cơ chế | Một phần: lõi nối tiếp b-req [Log]; có một lượt "không nối tiếp phiên cũ: lớp intrinsic" và một lượt nối tiếp `review_fix` (`<Bp-1>\log\*.out`) |
| Tín hiệu kiểm chứng không do người viết tạo, đưa lên sớm | (a) thăm dò hộp đen: `internal/workflow/bprime_probe.go:97-179`; (b) `bint task check` + kiểm nền: `internal/workflow/check.go:39-107`, `internal/workflow/bgverify.go:45-143` (commit `28ba0fe`); (c) "đo thời gian": chỉ có cổng `workflow.perf.command` có từ trước, mặc định rỗng (`internal/workflow/perf.go:14-29`) | Một phần: (b) phụ thuộc agent gọi lệnh; (c) không nối vào B′ | (a) có; (b) không lần nào ghi được kết quả; (c) tắt (`<Bp-1>\repo\.bint\config.yml:164-166`, `command: ""`) |
| Một lượt review ngữ cảnh sạch, không thấy kế hoạch | Thân chỉ dẫn riêng `bprimeReviewGuide` (`internal/workflow/bprime.go:31-51`); đề bài bỏ đường dẫn plan/tasks (`internal/workflow/run.go:3122-3124`); rào đọc `BINT_DENY_READ` cho plan.md, tasks.md (`run.go:1125-1128`); khối chấm `bprimeReviewBlock` (`internal/workflow/bprime_goal.go:82-86`). Reviewer vẫn đọc `requirement.md` (AC do b-req viết). | Đủ | Có: 8 phát hiện high [README] |
| Trạng thái phiếu trên đĩa, nối tiếp sau khi bị cắt | Có từ trước; B′ thêm `state.bprime_phase`, `bprime_core_fix`, `bprime_core_touched` (`internal/task/task.go:568-574`). Giới hạn tự ghi: thăm dò đã gộp "sống trong bộ nhớ của lượt `bint run`", resume giữa lõi và gộp thì chạy lại thăm dò (`docs/workflows.md:1725`). | Đủ, có một lỗ | Có: sống qua cú giết bằng dlv, nhưng mất lần thăm dò đầu ~6,6M token [README] |
| b-req: bỏ mở rộng bằng giả định; hỏi có điều kiện; danh sách "xong là gì" ngắn | `workflow.variant` (`internal/config/config.go:442-444, 504-509, 751, 1202-1203`), chốt vào phiếu (`internal/workflow/ops.go:250-251`); `bprimeReqGuide`: tối đa 5 AC, hỏi 0 hoặc 2–3 câu khi thiếu đặc tả, cấm thêm mục giả định/bề mặt/kiểm tra mục tiêu (`bprime.go:10-26`); cổng cỡ kế hoạch chỉ dừng khi chạm đường dẫn bảo vệ (`internal/workflow/plansize.go:39-46`). Commit `7df054c`. | Đủ. Mặc định vẫn là `classic` (báo cáo nói "bỏ làm mặc định") | Có: 3m09s + 1 câu hỏi + 1m53s [README]; requirement.md 17 dòng, plan.md 8 dòng, tasks.md 7 dòng [Log] |
| b-goal cùng model viết test rồi đóng băng: bỏ ở dạng hiện tại | Ở B′ thay bằng thăm dò hộp đen (xem câu hỏi 3). Luồng classic giữ nguyên b-goal cũ (`internal/workflow/goalfreeze.go:44-63`). | Một phần: vẫn cùng model, vẫn đóng băng làm cổng | Có |
| Vòng review–sửa nhiều lượt: bỏ; một lượt sửa có trần | B′ dùng lại đường nhanh: `fastPath()` đúng với mọi phiếu B′ (`internal/workflow/fastpath.go:67-70`), review vòng 1 còn phát hiện nặng thì `fastFix` mở đúng một vòng sửa rồi finalize, không review lại (`fastpath.go:80-99`, `run.go:2266-2268`); trần thời gian vòng sửa = `min(timeout_minutes, slice_timeout_minutes)` (`internal/workflow/slice.go:45-54`). Commit `8adbfb1`. | Đủ | Có: vòng sửa mở rồi bị trần 180 phút của benchmark cắt [README]; `fast_fixed: true` [Log] |
| Kiểm chứng: kiểm hẹp theo gói bị chạm sau mỗi mốc, toàn repo chạy nền | `bint task check` là lệnh agent phải tự gọi (`internal/cli/task.go:1385-1439`, đề bài `bprime.go:72-78`); kiểm nền chỉ khởi chạy khi `state.checks` có một lần kiểm hẹp **xanh** (`bgverify.go:75-101`). Commit `28ba0fe`. | Một phần: cơ chế có, kích hoạt giao cho agent | Không: `state.json` không có khoá `checks` lẫn `background_verifications` [Log] |
| Thứ tự: mã chạy được trước, thước đo độc lập song song, mở rộng sau khi lõi xanh | `goalPending` trả false cho B′ (`goalfreeze.go:58-60`); thăm dò khởi chạy cùng b-feat lõi (`run.go:1164-1169`); lõi đỏ được đúng 1 lượt sửa, rồi gộp + đóng băng, rồi đúng 1 lượt mở rộng (`bprime_probe.go:278-337`). Commit `63a6114`. | Đủ về thứ tự; "lõi xanh" không phải điều kiện cứng | Có: dòng mã đầu ở ~3 % [README]; `bprime_core_fix: 1`, `bprime_phase: done` [Log] |

Đoạn "thước đo đóng băng" của báo cáo có ba vế:

- **"Do người dùng xác nhận": chưa làm.** Không có bước nào đưa danh sách loại của thăm dò hay danh sách "xong là gì" cho người duyệt. Cổng kế hoạch của B′ chỉ dừng khi chạm
  đường dẫn bảo vệ hoặc còn câu hỏi mở — `internal/workflow/plansize.go:39-46`, thông điệp commit `7df054c`.
- **"Ngữ cảnh không đọc kế hoạch, thăm dò hộp đen theo loại đầu vào": đã làm** — `internal/workflow/bprime_goal.go:25-47`; chỉ tệp có dòng `Loại đầu vào:` mới được đóng băng
  (`bprime_goal.go:19`, `goalfreeze.go:128-130`).
- **"Lối thoát dừng và báo nếu thước đo có vẻ sai": đã làm, ở mức chỉ dẫn** — `bprime_goal.go:68-79` dặn b-feat chạy `bint task error <id> --step b-feat --question "…"`;
  người trả lời (giữ thước đo) hoặc chạy `bint run resume <id> --drop-goal` (chỉ người; `bprime_goal.go:104-139`). Đợt 7 không kích hoạt: cổng mục tiêu đỏ ở mốc
  (16:41:09Z) rồi xanh (17:53:57Z), không có câu hỏi nào trong `state.json` [Log].

Chi tiết "lõi xanh" không phải điều kiện cứng: sau đúng một lượt sửa lõi, `bprimeAfterCore` đi tiếp sang gộp và mở rộng dù kiểm chứng còn đỏ
(`bprime_probe.go:299-303`: chỉ chặn khi `BPrimeCoreFix < 1`). Ở đợt 7 cả ba lần kiểm chứng của B′ đều đỏ (`passed: False` lúc 16:20:45Z, 16:40:48Z, 17:52:56Z) nhưng ghi chú
vẫn là "lõi xanh, đã gộp và đóng băng thước đo thăm dò" (`bprime_probe.go:335`; `<phiếu>\state.json`, `<Bp-1>\log\*.out`).

### Inferences
- Báo cáo xếp "đo thời gian" vào nhóm tín hiệu không do người viết tạo; B′ không thêm gì cho vế này, nên việc cả ba nhánh hỏng N2 ở đợt 7 không phải là B′ thất bại ở chỗ đã làm, mà là chỗ chưa làm.
- Ghi chú "lõi xanh" in ra cả khi lõi đỏ sẽ làm người đọc log hiểu sai trạng thái trước lượt mở rộng.

### Gaps
- Tôi không đọc toàn bộ `internal/workflow/run.go` (3203 dòng) và `session.go`; hành vi B′ ở driver session chỉ biết qua `docs/workflows.md:1720-1721`.
- Chưa kiểm rào đọc `BINT_DENY_READ` có hiệu lực thật với Codex hay không: `stepDenyReadSettings` có chú thích nhắc Claude (`run.go:1133`), tôi không đọc phần hook phía Codex.

## 2. Kiểm hẹp theo mốc: `bint task check` được kích hoạt thế nào, vì sao đợt 7 không có kết quả, và engine cần gì để tự chạy?

### Takeaway
Kiểm hẹp chỉ là một đoạn văn trong đề bài b-feat, và ở đợt 7 nó hỏng theo ba lớp chồng nhau: lượt lõi không nhận được đoạn văn đó, các lượt sau gọi lệnh thì sandbox làm lệnh tự bỏ qua,
và dù có chạy thì "gói bị chạm" tính ra 39 gói (gần cả repo). Engine đã tự chạy kiểm chứng theo phạm vi sau mỗi lượt b-feat; cái thiếu là kiểm giữa lượt và một tầng thật sự hẹp.

### Cited Findings
- **Kích hoạt chỉ bằng đề bài.** Khối `bprimeCheckBlock` ("sau MỖI nhóm sửa chạy `bint task check <id>`… Đừng tự chạy toàn bộ bộ test") được nối vào đề bài b-feat đầy đủ —
  `internal/workflow/bprime.go:72-78`, `internal/workflow/run.go:3152-3160`. Không có hook theo tệp/commit nào gọi `Check` [Mã: `grep` `IsBPrime`/`Check(` trong `internal/`].
- **Lượt lõi không nhận khối này.** Lượt b-feat đầu của B′ chạy `codex exec … resume 01a1f964-…`, cùng `thread_id` với hai lượt b-req —
  `<phiếu>\logs\b-feat-01M7WPHCVBE9N39JM56B7PZN5P.log` dòng 1–2, `<phiếu>\logs\b-req-*.log`. Lời nhắn của kiểu nối tiếp này do `planHandoffPrompt` dựng: thân skill b-feat, AC, TSK,
  `goalCheckBlock`, `sliceBlock`; **không** có `bprimeCoreBlock` hay `bprimeCheckBlock` — `internal/workflow/plan_handoff.go:73-113`. Trong log lượt đó chuỗi `bint task check`
  xuất hiện 0 lần; ở bốn lượt b-feat sau là 8, 18, 20, 8 lần [Log].
- **Các lượt sau có gọi, lệnh tự bỏ qua.** Agent chạy `bint task check 01M7WP81…` 4 lần ở lượt sửa lõi và 8 lần ở lượt kế, rồi viết: "`bint task check` vẫn bỏ qua kiểm tra hẹp vì sandbox
  chặn ghi snapshot vào `.git/objects`, nên tôi đang chạy trực tiếp các kiểm thử liên…" — `<phiếu>\logs\b-feat-01M7WW5CEENB1BD1TTRECV8Q93.log`,
  `…\b-feat-01M7WXAMJMX449FBSME4KHV9HM.log`. Đường mã khớp: `Check` gọi `SelectAffectedCfg`, hàm này chụp cây bằng `WorktreeSnapshot`; lỗi chụp trả phạm vi `full`
  (`internal/workflow/affected.go:29-32`), và `Check` gặp `full` thì trả "Bỏ qua… để kiểm nền lo", **không ghi gì vào `state.checks`**, mã thoát 0 (`internal/workflow/check.go:63-72`,
  `internal/cli/task.go:1413-1414`). Agent chạy với `--sandbox workspace-write` [Log, dòng lệnh đầu tệp log].
- **README mâu thuẫn với log.** README: "`bint task check`… **0 lần**: agent bỏ qua chỉ dẫn" (`docs/evals/bench-abbp-v7-20261121/README.md:44, 53-55`); log cho thấy agent gọi nhiều lần
  và bị bỏ qua. Ghi chú của Bint cũng gây hiểu sai: `firstResultNote` in "b-feat không chạy kiểm hẹp theo mốc" mỗi khi `state.checks` rỗng, bất kể agent có gọi hay không
  (`internal/workflow/bgverify.go:203-214`).
- **Kiểm nền phụ thuộc kiểm hẹp xanh.** `tick` chỉ tìm lần `Checks` gần nhất có `Passed` và `Tree` — `bgverify.go:75-101`. Không có kiểm hẹp được ghi thì kiểm nền không bao giờ chạy.
- **Engine đã tự chạy kiểm chứng theo phạm vi sau mỗi lượt b-feat.** Sau agent, `afterStep` gọi `VerifyScoped` (`run.go:2017-2024`), dùng `workflow.verify.scope: affected` và
  `affected_command` (`internal/workflow/ops.go:830-840`). Đây là "kiểm chứng đầu tiên ở ~41 %" của đợt 7.
- **"Gói bị chạm" được quyết ở đâu.** `SelectAffectedCfg` lấy diff giữa mốc tác vụ `State.Base` và ảnh chụp cây hiện tại (`internal/workflow/affected.go:21-38`), rồi `affected.Select`
  (`internal/affected/affected.go:135-215`); với Go, chọn mọi gói test mà chính nó hoặc một phụ thuộc của nó nằm trong tập gói đổi (`internal/affected/gosource.go:234-263`).
  Tệp cấu hình gốc/CI, hơn `MaxChanged` tệp, tệp lạ… nới thẳng ra `full` (`affected.go:39-54, 161-165`).
- **Ở đợt 7 phạm vi "hẹp" là 39 gói.** Cả ba lần kiểm chứng của B′ có `scope: graph` và lệnh `go test -timeout 30m ./internal/app … ./internal/workflow` liệt kê 39 gói, kể cả
  `internal/workflow` và `internal/tui/app` — `<phiếu>\state.json` mảng `verifications`. Hai gói đó là đường găng theo ghi chú dự án (`D:\Projects\bint\AGENTS.md`, mục go test trên Windows).
- **`bint task check` dùng cùng phép chọn và trần `verify_timeout_minutes`** — `check.go:63, 82`; cấu hình benchmark đặt 60 phút (`<Bp-1>\repo\.bint\config.yml:175`).

### Inferences
- Nguyên nhân sandbox: tôi suy từ lời agent và từ việc `WorktreeSnapshot` phải ghi object git; tôi không đọc thân `WorktreeSnapshot` và không tái hiện lỗi. Lời agent là bằng chứng gián tiếp.
- Việc đề bài nối tiếp thay hẳn đề bài đầy đủ tôi đọc từ chú thích của `armProcessContinuation` ("đổi sr.prompt thành lời nhắn nối tiếp", `run.go:1197-1199`) và từ số đếm 0 trong log;
  log không lưu nguyên văn đề bài nên không đối chiếu được từng chữ.
- Kể cả khi sandbox cho chạy, `bint task check` ở lượt lõi vẫn sẽ chạy đúng 39 gói ấy (~20 phút mỗi lần theo README), dài hơn giới hạn lệnh thường gặp của agent. Vì diff luôn so với mốc tác vụ
  (cộng dồn), chạm một gói nền như bộ che bí mật là đủ kéo gần hết repo; kiểm hẹp không hẹp dần theo mốc.
- Skill b-feat vẫn nói "Bint tự chạy kiểm chứng sau bước này" và "không chạy cả module lớn" (`internal/skills/core/b-feat/SKILL.md`, anchor `test-strategy`); khối B′ nối sau hàng trăm dòng
  skill là một chỉ dẫn dễ bị lấn át. Đây là phỏng đoán, không có bằng chứng log.
- Để engine tự chạy: tiến trình `bint run` nằm ngoài sandbox nên chụp cây được. `bgVerifier` đã có vòng thăm 5 giây (`bgverify.go:23, 58-69`); đổi đối tượng thăm từ `state.checks` sang
  ảnh chụp cây là đủ để phát hiện "tệp vừa đổi" mà không cần agent.

### Gaps
- Không biết giới hạn thời gian một lệnh của Codex trong cấu hình benchmark, nên không khẳng định được một lần kiểm 39 gói sẽ bị cắt.
- Chưa kiểm `Deps` trong đồ thị Go là phụ thuộc bắc cầu hay trực tiếp (không đọc `gosource.go:44-150`); con số 39 gói gợi ý là bắc cầu.
- Chưa đo thời lượng từng lần `verify-N.log`; "~20 phút" lấy từ README.

## 3. Thăm dò hộp đen: ngữ cảnh thăm dò nhận gì, bị giấu gì, có danh mục ngoài nào không, lối thoát nối dây ra sao?

### Takeaway
Thăm dò bị giấu kế hoạch và mã đang viết, nhưng vẫn nhận tiêu chí do b-req viết và không nhận bất kỳ nguồn tham chiếu nào ngoài model; danh sách loại là do chính model nghĩ ra.
Dòng nhãn của thước đo đợt 7 liệt kê 18 loại, không có khoá riêng PEM.

### Cited Findings
- **Nhận:** thân `bprimeGoalGuide`; toàn bộ `task.yml` dưới dạng YAML (gồm mục tiêu, yêu cầu gốc, tiêu chí, lệnh kiểm chứng, phạm vi); đường dẫn bảo vệ; `bprimeGoalBlock`; khối `<request>`;
  và gói tri thức dự án `r.Context` — `internal/workflow/run.go:3058-3060, 3116-3117, 3130-3136, 3177-3187`.
- **Tiêu chí trong phiếu là của b-req.** b-req B′ ghi tiêu chí bằng `bint task set --criteria` (`internal/workflow/bprime.go:18`), và đề bài thăm dò nói nguồn là "YÊU CẦU GỐC… và tiêu chí
  trong phiếu" (`internal/workflow/bprime_goal.go:30-31, 36`). AC đợt 7 viết "mật khẩu hoặc token" (AC-1 đến AC-4) và "mật khẩu, token và khoá API" (AC-5), không nhắc khoá riêng —
  `<phiếu>\requirement.md:9-13`.
- **Quyền với repo:** thư mục làm việc là worktree tạm dựng từ mốc `State.Base`, tức toàn bộ repo ở trạng thái trước khi sửa — `internal/workflow/bprime_probe.go:103-123`,
  `run.go:3077-3080`.
- **Bị giấu:** mã b-feat đang viết (worktree khác); cả thư mục tác vụ gồm requirement.md, plan.md, tasks.md, log (`probeDenyRead`, `bprime_probe.go:82-85`); đề bài không nêu đường dẫn
  tài liệu nào (`run.go:3119-3121`).
- **Danh mục/checklist ngoài: không có.** Luật 1 chỉ viết: "không hẹp hơn lời người dùng; thêm bộ quy tắc/danh mục sẵn có của dự án cho dữ liệu đó và các biến thể phổ biến ngoài đời" —
  `bprime_goal.go:39`. Không có khoá cấu hình, tệp hay URL tham chiếu nào được chèn [Mã: toàn bộ `bprime_goal.go`, `bprime_probe.go`].
- **Kết quả đợt 7:** dòng nhãn của tệp chính: "env-assignment, password-flag, passwd-flag, token-flag, secret-flag, api-key-flag, auth-flag, bearer, basic, url-userinfo, url-query-token,
  json-password, json-token, github-token, openai-key, aws-secret-key, slack-token, jwt" — `<Bp-1>\repo\internal\cli\secret_storage_goal_test.go:1`; tệp thứ hai: "bearer, json-token,
  github-token, url-query-token" — `<Bp-1>\repo\internal\mcpgw\secret_storage_goal_test.go:1`.
- **Gộp và đóng băng:** tệp thăm dò được chép vào cây, trừ tệp chạm đường dẫn bảo vệ hoặc trùng tệp b-feat đã sửa — `bprime_probe.go:208-275`; thăm dò lỗi, quá trần hoặc không có tệp
  nhãn thì không có thước đo và không có lượt mở rộng (`bprime_probe.go:233-246, 315-321`).
- **Lối thoát:** chỉ nằm trong khối đề bài của lượt có thước đo đóng băng (`bprime_goal.go:68-79`, gọi từ `goalfreeze.go:420-421`); đường ra là câu hỏi b-feat rồi `--drop-goal`
  (`bprime_goal.go:104-139`; CLI ở `internal/cli/run.go`, commit `8adbfb1`). Reviewer được dặn coi thước đo "là một tín hiệu, không phải đặc tả tuyệt đối" (`bprime.go:45`).

### Inferences
- Tính độc lập thông tin của thăm dò yếu hơn mô tả: cùng model, đọc cùng repo (gồm bộ mẫu che bí mật sẵn có), và nhận cách hiểu của b-req qua tiêu chí trong phiếu. Nếu b-req thu hẹp
  thành "mật khẩu, token", thăm dò được mớm đúng phạm vi hẹp đó. Đây là một lời giải thích khả dĩ cho việc PEM sót ở cả hai nhánh; tôi không có phép thử tách nguyên nhân.
- Lối thoát chỉ bắt "thước đo sai", không bắt "thước đo thiếu". Không ai trong luồng được giao việc soát độ phủ của dòng `Loại đầu vào:`; đây đúng là loại lỗi của đợt 7.
- Chỗ chèn tham chiếu ngoài, theo mức ít sửa nhất: (1) gói tri thức `r.Context` đã vào đề bài thăm dò hôm nay, nên một mục tri thức mang danh mục có thể tới được thăm dò mà không sửa mã
  (tuỳ bộ lọc liên quan, chưa kiểm); (2) tham số cho `bprimeGoalBlock()`; (3) một trường của phiếu do người xác nhận. Chỗ chèn "xong là gì" do người xác nhận: cổng sau b-req ở `plansize.go:39-46`.

### Gaps
- Chưa đọc nguyên văn yêu cầu L1 nên không biết lời người dùng có nhắc khoá riêng hay không.
- Chưa đọc nội dung hai tệp thăm dò ngoài dòng 1; "không có PEM" dựa vào dòng nhãn và README.
- Chưa kiểm bộ lọc của `r.Context` có trả mục tri thức loại danh mục cho bước b-goal không.

## 4. Review: một lượt với vòng sửa có trần? Ngân sách ra sao khi review ở cuối?

### Takeaway
Review B′ đúng là một lượt cộng tối đa một vòng sửa có trần, nhưng Bint không có khái niệm ngân sách tổng: không có gì dành sẵn thời gian cho review và vòng sửa.

### Cited Findings
- Một lượt, một vòng sửa, không review lại: `internal/workflow/fastpath.go:67-70, 80-99`; `internal/workflow/run.go:2264-2268`. Ghi chú finalize nêu phát hiện đã sửa nhưng chưa được
  review lại (`fastpath.go:101-125`).
- Trần vòng sửa: `min(workflow.timeout_minutes, workflow.slice_timeout_minutes)` — `internal/workflow/slice.go:45-54`; benchmark đặt `slice_timeout_minutes: 30`
  (`D:\bench\bench-v7-20261120\…\.bint\local.yml`, in ra khi đọc).
- Phát hiện high/critical thiếu `repro` bị hạ xuống medium và không mở vòng sửa — `internal/workflow/bprime_goal.go:85-86`.
- Các trần hiện có đều theo từng bước: `workflow.timeout_minutes` (mặc định 60), `slice_timeout_minutes` (30), `verify_timeout_minutes` (15) — `internal/config/config.go:429-449, 751`;
  ngoài ra chỉ có trần chi phí `limits.per_run.max_cost_usd` (`config.go:556, 1165-1166`). Không có khoá ngân sách thời gian tổng [Mã: `grep` trong `config.go`].
- `firstResultNote` chỉ tính phần trăm sau khi việc đã xảy ra, không dùng để lập lịch — `internal/workflow/bgverify.go:203-221`.
- Đợt 7: review 7 phút, vòng sửa bị trần 180 phút của bộ đo cắt; trần này do kịch bản benchmark áp, không phải Bint — `docs/evals/bench-abbp-v7-20261121/README.md:30, 43`,
  `prereg-and-incidents.md:12`.

### Inferences
- Với lõi 29 phút và mỗi lần kiểm chứng ~20 phút chạy tuần tự ba lần, review tới muộn là hệ quả của thời gian kiểm chứng, không phải của số lượt agent.
- Lập lịch theo ngân sách cần một con số tổng mà Bint chưa có; nếu thêm, điểm áp tự nhiên là `sliceTimeoutFor` (rút trần lượt mở rộng) và `bprimeAfterCore` (bỏ lượt mở rộng khi quỹ còn lại
  nhỏ hơn phần dành cho review + sửa).

### Gaps
- Chưa đọc `reviewRedVerify` và `finalize`, nên chưa mô tả được nhánh "review sạch nhưng kiểm chứng đỏ" ở B′ ngoài chú thích `fastpath.go:86-89`.

## 5. Lát mỏng ở luồng classic: lựa chọn khi "test cũ đỏ", mặc định, và mặc định an toàn hơn

### Takeaway
Khi lát làm đỏ test cũ, Bint dừng và đưa ba lựa chọn, liệt kê "giữ" đầu tiên; "giữ" chốt cây đỏ làm điểm xanh mới và không truyền danh sách test đỏ cho lát sau.

### Cited Findings
- Phát hiện: `oldTestsGreen` coi mọi ô đỏ không thuộc kiểm tra mục tiêu là "test cũ đỏ" — `internal/workflow/slice.go:377-395`; `sliceVerdict` giữ lát (`slice.go:399-402`).
- Ba lựa chọn: `--slice keep` (chốt cây hiện tại làm điểm xanh, sang lát kế) | `discard` (về điểm xanh, sang lát kế) | `more` (chạy lại lát, thêm một trần) — `slice.go:563-566`.
  Danh sách hành động hiển thị theo thứ tự keep, discard, more — `internal/workflow/gate.go:43-45`.
- Mặc định khi resume không nêu `--slice` là `more` — `slice.go:625-628`; `--slice` chỉ người chạy được (`internal/cli/run.go:308-310`).
- `keep`: ghim cây làm `SliceGreen`, lưu ô đỏ của **cổng mục tiêu**; lý do "test cũ đỏ" chỉ nằm trong chuỗi `Reason` của lát — `slice.go:654-662, 688-696`.
- Đợt 7: người dùng giả được cấu hình sẵn "lát giữ → `--slice keep`" — `prereg-and-incidents.md:10`; cả 4 lát B-1 bị giữ vì test cũ đỏ, cây cuối 8 test đỏ [README:27, 58-60].

### Inferences
- `oldTestsGreen` không có tập "đã đỏ từ trước và người đã chấp nhận", nên sau một lần `keep` các test cũ đó sẽ lại làm mọi lát sau bị giữ. Khớp với "cả 4 lát" của B-1, nhưng tôi không
  đọc log B-1 để xác nhận cùng một nhóm test.
- Mặc định an toàn hơn trong mã: (1) đổi thứ tự hiển thị để `more` đứng đầu và gắn nhãn "sửa test cũ đỏ"; (2) khi lý do là test cũ đỏ, `keep` phải mang danh sách test đỏ sang đề bài lát kế
  (`sliceBlock`) như việc bắt buộc làm trước; (3) lưu tập đỏ đã chấp nhận vào `SliceGreen` để lát sau chỉ bị giữ khi có test đỏ **mới**.

### Gaps
- Chưa đọc `sliceBlock` đầy đủ (`slice.go:230-262`) để chắc rằng không có chỗ nào đã nhắc test đỏ của lát trước.

## 6. Commit 39a9b70: quy tắc được thêm, và mức độ riêng cho test ẩn đợt 6

### Takeaway
Lời văn của quy tắc tổng quát và commit còn gỡ các ví dụ riêng của đề benchmark, nhưng nó được viết sau khi đã thấy kết quả test ẩn đợt 6 và nhắm đúng kiểu lỗi của đợt đó; đợt 7 cho
thấy nó không tổng quát hoá sang PEM.

### Cited Findings
- Quy tắc trong b-req (nguyên văn): "**Ma trận loại × nơi**: dòng `Loại:` (từng dạng dữ liệu) và `Nơi:` (nhóm bề mặt); kiểm **mọi cặp**, mỗi cặp một ô. `Loại:` **không được hẹp hơn** loại
  người dùng nêu, bộ quy tắc/danh mục sẵn có của dự án cho dữ liệu đó (mẫu nhận dạng, bảng, enum), và các biến thể phổ biến ngoài đời mà dự án chưa xử lý; ghi nguồn của danh sách;" —
  `git show 39a9b70 -- internal/skills/core/b-req/SKILL.md`.
- Quy tắc trong b-goal (nguyên văn): "**Không hẹp hơn bộ mẫu sẵn có**: lấy loại từ dòng `Loại:` của requirement.md, rồi tìm bộ quy tắc/danh mục sẵn có của dự án cho dữ liệu đó (mẫu nhận
  dạng, bảng, enum). Loại nào có ở đó mà thiếu trong `Loại:` thì vẫn thêm vào test. Dữ liệu giả của mỗi loại phải có **hình dạng thật** như ngoài đời (định dạng, độ dài, cách bọc), không
  chỉ là dạng dễ nhận nhất." — `git show 39a9b70 -- internal/skills/core/b-goal/SKILL.md`.
- Cùng commit gỡ ví dụ riêng của đề ("không lưu bí mật", "dọn dữ liệu cũ", danh sách nơi lưu của Bint, `go test ./...`) khỏi skill b-req, b-feat, b-review và `fastPrompt` — cùng diff;
  thông điệp commit ghi "gỡ overfit".
- Commit lúc 2026-11-20 11:36 +07, cùng lúc đưa chính bản báo cáo và các ghi chú nghiên cứu đợt 6 vào repo (`git show --stat 39a9b70`).
- B′ mang ý này vào luật 1 của thăm dò (`internal/workflow/bprime_goal.go:39`, commit `8adbfb1`); b-req gọn của B′ không có quy tắc nào về danh sách loại (`internal/workflow/bprime.go:10-26`).
- Đợt 7: chênh lệch B đợt 6 → đợt 7 (20 → 30 ô) nằm ở hai loại AWS; `private_key` 0/6 ở cả B và B′ — `docs/evals/bench-abbp-v7-20261121/README.md:14-16, 24`.

### Inferences
- Rủi ro học theo đề nằm ở nguồn gốc chứ không ở câu chữ: cụm "biến thể phổ biến ngoài đời mà dự án chưa xử lý" và "không chỉ là dạng dễ nhận nhất" mô tả đúng lỗi đợt 6 (thiếu dạng
  không nhãn). Quy tắc vẫn giao việc liệt kê biến thể cho trí nhớ của model, nên trúng AWS (vừa bị phạt ở đợt 6) mà trượt PEM là kết quả phù hợp với một quy tắc không có nguồn ngoài.
- Một lượt mỗi nhánh nên không tách được quy tắc này khỏi ngẫu nhiên; README cũng nói vậy (dòng 81-82).

### Gaps
- Chưa đối chiếu test đóng băng của B-1 đợt 7 (`credential_persistence_test.go`) với đợt 6 để xem loại nào được thêm nhờ quy tắc.

## 7. Có cơ chế nào cho yêu cầu phi chức năng (ngân sách hiệu năng) không?

### Takeaway
Có một cổng hồi quy hiệu năng dùng chung, nhưng phải có lệnh đo do người cấu hình và duyệt; mặc định rỗng, benchmark để rỗng, và không bước nào của b-req/b-goal/thăm dò hỏi hay sinh ra nó.

### Cited Findings
- Cổng `workflow.perf.command`: đo mốc trước b-feat đầu tiên, đo lại sau mỗi b-feat, 3 lần lấy nhỏ nhất, đỏ khi chậm quá `max_slowdown` và quá sàn nhiễu 100 ms —
  `internal/workflow/perf.go:14-20, 63-95, 99-130`; gọi ở `internal/workflow/run.go:2005-2015, 2053`.
- Chỉ chạy khi lệnh khác rỗng và đã được người xác nhận (`perf.go:24-29`); mặc định `MaxSlowdown: 1.5`, lệnh rỗng (`internal/config/config.go:753`).
- Benchmark đợt 7: `perf: command: ""` — `<Bp-1>\repo\.bint\config.yml:164-166`; `state.json` của B′ không có `perf_checks` [Log].
- Trong các chỉ dẫn: b-req gọn, thăm dò, lõi/mở rộng của B′ không có từ nào về hiệu năng (`internal/workflow/bprime.go`, `bprime_goal.go`); skill b-review chỉ có một dòng mục "Kiến trúc,
  bảo mật, hiệu năng" (`internal/skills/core/b-review/SKILL.md:77`); `grep` "hiệu năng|performance|phi chức năng" trên skill b-req, b-goal, b-feat không ra kết quả.

### Inferences
- N2 (verify log lớn chậm 11–19 lần) không thể bị bắt bởi luồng hiện tại trừ khi người dùng tự viết lệnh đo đúng kịch bản đó; với người dùng không rành kỹ thuật điều này không xảy ra.
- Thước đo thăm dò có thể mang ca "đầu vào lớn" nếu luật của thăm dò yêu cầu một loại đầu vào theo kích cỡ; hiện luật chỉ nói về loại dữ liệu.

### Gaps
- README đợt 7 nhắc "1 cổng hiệu năng" trong 3 test đỏ của cây B′ cuối; tôi không xác định đó là test nào (có vẻ là test của repo, không phải cổng `perf.command`).

## 8. Chi phí: token của B′ đi đâu về mặt cấu trúc?

### Takeaway
B′ ở đợt 7 dùng ít nhất năm ngữ cảnh model và tám lượt agent; kế hoạch không bị gửi lại nguyên văn, nên phần lãng phí thấy được trong mã là thăm dò chạy hai lần, mỗi ngữ cảnh mới
đọc lại repo, và thân skill dài được gửi lại ở mỗi đề bài đầy đủ. Tôi chưa có số token theo từng bước.

### Cited Findings
- Các lượt agent của B′-1 theo tệp log: b-req ×2 (cùng một thread), b-feat ×5, b-goal ×2, b-review ×1 — `<phiếu>\logs\`.
- Ngữ cảnh: b-req và b-feat lõi chung một thread (`thread_id 01a1f964…`) [Log]; thăm dò là ngữ cảnh riêng trong worktree riêng (`internal/workflow/bprime_probe.go:124-165`); review là
  ngữ cảnh mới (ngữ cảnh sạch theo thiết kế); một lượt b-feat mở mới vì "lớp intrinsic, không nối tiếp được" và một lượt nối tiếp `review_fix` (`<Bp-1>\log\*.out`).
- Thăm dò chạy hai lần: lần đầu mất khi tiến trình bị giết, ~6,6M token — `README.md:79-80`; mã không lưu kết quả thăm dò xuống đĩa trước khi gộp (`docs/workflows.md:1725`;
  `bprimeProbe.files` là map trong bộ nhớ, `bprime_probe.go:38-49`).
- Mỗi đề bài đầy đủ gồm: toàn bộ thân skill của bước, `task.yml`, đường dẫn tài liệu, các khối B′, yêu cầu gốc và gói tri thức — `internal/workflow/run.go:3045-3189`. Kế hoạch chỉ được
  nêu bằng đường dẫn, không chép nội dung (`run.go:3118-3129`); đề bài nối tiếp b-req → b-feat chỉ chép dòng AC và TSK, bỏ `task.yml` và tri thức (`plan_handoff.go:70-113`);
  lời nhắn nối tiếp vòng sửa chỉ vài dòng (`internal/workflow/continuation.go:166-191`).
- Đề bài review kèm diff đầy đủ "khi không quá lớn" (`bprime.go:43`; `reviewDiffBlock`, `internal/workflow/gate.go:519-522`).
- Tổng: B′ 61,5M token vào (≈ 55M sau khi trừ thăm dò chạy lại), A-1 8,0M; ở mốc 9100 giây B′ đã 30/36 với 39,3M — `README.md:29, 63-64`.

### Inferences
- Với 61,5M token vào trên tám lượt, phần lớn gần như chắc chắn là ngữ cảnh tích luỹ được gửi lại ở mỗi lời gọi công cụ trong các phiên b-feat dài, không phải đề bài của Bint (đề bài
  chỉ cỡ chục nghìn byte). Tôi không có số theo bước để khẳng định.
- Ba khoản có thể cắt mà không đổi thiết kế: lưu tệp thăm dò xuống đĩa để resume không chạy lại (≈ 6,6M ở đợt 7); bỏ lượt b-feat mở mới khi phiên cũ còn dùng được; và dừng sau mốc 9100 giây
  không đổi điểm test ẩn, tức ~22M token sau đó chỉ mua review và giảm test đỏ.
- Thăm dò và review mỗi bên đọc lại repo từ đầu là chi phí cố hữu của "ngữ cảnh khác thông tin"; muốn rẻ hơn thì thu hẹp việc của chúng (thăm dò chỉ viết theo danh mục cho sẵn; review nhận
  diff và danh sách nơi lưu) chứ không gộp ngữ cảnh.

### Gaps
- Chưa đọc `<Bp-1>\run.json` và các sự kiện usage trong log để chia token theo bước; đây là số liệu cần cho mọi kết luận về chi phí.
- Chưa biết tỷ lệ token vào được tính là cache.

## 9. Chi phí kiểm chứng trên Windows: lệnh chạy gì, có giới hạn theo gói hay cache không?

### Takeaway
Có giới hạn theo gói (đồ thị phụ thuộc Go), nhưng nó cộng dồn từ mốc tác vụ và lan theo phụ thuộc ngược, nên ở bài chạm gói nền nó chọn 39 gói; cổng done còn chạy toàn repo thêm một lần.

### Cited Findings
- Lệnh của phiếu ở đợt 7: `go test -timeout 30m ./...` (ghi chú "lõi chưa xanh (`go test -timeout 30m ./...`)", `<Bp-1>\log\*.out`); lệnh theo phạm vi:
  `go test -timeout 30m {test_dirs}` với `scope: affected`, `bint_config_full: false` — `<Bp-1>\repo\.bint\config.yml:160-163`.
- Phép chọn và ba tầng `none|graph|package|full`: `internal/affected/affected.go:17-37, 135-215`; ba lần kiểm chứng B′ đều `graph` với cùng 39 gói [Log].
- Cổng mục tiêu được thu hẹp riêng bằng `-run` trên tệp đóng băng (`goalRunOnly`, `internal/workflow/goalfreeze.go:242-245`).
- Kiểm chứng đầy đủ vẫn chạy một lần ở cổng done ("cổng done đòi lần kiểm chứng đầy đủ", `internal/workflow/ops.go:797-799`); B′ có thể dùng lại kết quả kiểm nền xanh trên đúng cây cuối
  (`internal/workflow/bgverify.go:167-200`), điều chưa từng xảy ra ở đợt 7.
- Kiểm chứng chạy với thư mục tạm riêng cho mỗi lần (`verifyTempDir`, `ops.go:843-847`); tài liệu có mục riêng về kiểm chứng chậm/treo trên Windows và thư mục `%TEMP%\bv\`
  (`docs/workflows.md:208-235`).
- Ghi chú dự án: `go test ./...` trên Windows ~10 phút, chủ yếu do spawn git ~25 ms mỗi lần và Defender quét `%TEMP%`; gói workflow (~600 s) và tui/app (~300 s) là đường găng —
  `D:\Projects\bint\AGENTS.md`.

### Inferences
- Bint không có cache kết quả theo gói; thứ duy nhất tránh chạy lại là cache test của Go. Ba lần chạy cùng 39 gói trên ba cây khác nhau gần như không hưởng cache cho các gói phụ thuộc vào
  mã vừa đổi.
- Tầng `package` (chỉ test của chính gói đổi) có trong kiểu dữ liệu nhưng tôi không thấy đường nào cho `check` chọn nó khi đồ thị đã trả `graph`; một kiểm hẹp thật sự nên dùng tầng này
  và so với lần kiểm trước, không so với mốc tác vụ.

### Gaps
- Chưa kiểm `verifyEnviron`/`verifyTempEnv` có đổi `GOCACHE` hay không (nếu có thì mất luôn cache của Go).
- Chưa đọc `Classify` (`internal/affected/classify.go`) để biết khi nào ra `package` thay vì `graph`.

## 10. Các thay đổi mã cụ thể để đóng từng khoảng trống

### Takeaway
Việc có lợi nhất và rẻ nhất là hai lỗi nối dây (đề bài nối tiếp thiếu khối B′; `check` hỏng trong sandbox) cộng với để engine tự kiểm hẹp; các việc về thước đo cần thêm đầu vào từ người
hoặc danh mục ngoài chứ không sửa lời văn.

### Cited Findings
Mỗi dòng gắn với khoảng trống đã dẫn nguồn ở các mục trên. Cỡ là ước lượng của tôi theo số dòng mã (không tính test), chưa được kiểm bằng việc làm thử.

| # | Thiếu hụt đợt 7 | Khoảng trống thiết kế | Thay đổi | Tệp | Cỡ ước lượng |
|---|---|---|---|---|---|
| 1 | Lượt lõi không biết `bint task check`, không biết "LÕI trước" | `planHandoffPrompt` không nối khối B′ | Thêm `bprimeCoreBlock`/`bprimeExtendBlock`/`bprimeCheckBlock` vào đề bài nối tiếp như `prompt()` làm; test hồi quy cho B′ + plan handoff | `internal/workflow/plan_handoff.go`, `bprime_reorder_test.go` | ~15 dòng + 1 test |
| 2 | `bint task check` tự bỏ qua trong sandbox, không để lại dấu | Chụp cây cần ghi `.git/objects`; lỗi chụp bị coi là "phạm vi full" và không ghi trạng thái | Tách lỗi chụp cây khỏi `full`; ghi lần bị bỏ qua vào `state.checks` kèm lý do; sửa `firstResultNote` phân biệt "không gọi" và "gọi nhưng bỏ qua" | `internal/workflow/check.go`, `affected.go`, `bgverify.go`, `internal/task/task.go` | ~40–60 dòng |
| 3 | Kết quả kiểm chứng đầu tiên ở ~41 % | Kiểm hẹp và kiểm nền chỉ chạy khi agent gọi | Cho `bgVerifier` thăm ảnh chụp cây (tiến trình ngoài sandbox) thay vì `state.checks`; cây đổi và đứng yên N giây thì engine tự chạy kiểm hẹp, ghi `state.checks`, rồi kiểm nền như cũ; `bint task check` trở thành lệnh đọc kết quả gần nhất | `internal/workflow/bgverify.go`, `check.go`, `internal/cli/task.go`, `docs/workflows.md` | ~150–250 dòng |
| 4 | "Kiểm hẹp" = 39 gói, ~20 phút | Diff so với mốc tác vụ, lan theo phụ thuộc ngược | Thêm chế độ hẹp cho `Check`: chỉ test của các gói có tệp đổi kể từ lần kiểm trước (tầng `package`), phần lan theo đồ thị để kiểm nền | `internal/workflow/check.go`, `affected.go`, `internal/affected/affected.go` | ~60–100 dòng |
| 5 | Ghi chú "lõi xanh" khi lõi đỏ; mở rộng trên lõi đỏ | `bprimeAfterCore` đi tiếp sau 1 lượt sửa bất kể kết quả | Sửa ghi chú theo trạng thái thật; đưa log đỏ vào đề bài lượt mở rộng | `internal/workflow/bprime_probe.go`, `bprime.go` | ~20 dòng |
| 6 | Thăm dò sót PEM | Danh sách loại do model tự nghĩ; không nguồn ngoài | Khoá cấu hình (ví dụ `workflow.probe.references`: tệp danh mục) chèn vào `bprimeGoalBlock`, kèm luật "mỗi mục danh mục là một loại hoặc ghi lý do loại trừ"; theo quy tắc dự án phải sửa struct, `Default()`, `Validate()` và mẫu `Render` | `internal/config/config.go`, `template.go`, `internal/workflow/bprime_goal.go`, `run.go` | ~80–120 dòng |
| 7 | Thăm dò thừa hưởng cách hiểu hẹp của b-req | Tiêu chí do b-req ghi nằm trong `task.yml` gửi cho thăm dò | Lọc tiêu chí khỏi phiếu trong đề bài thăm dò, hoặc chỉ giữ tiêu chí có từ lúc lập phiếu | `internal/workflow/run.go` (đoạn 3116-3117), `bprime_goal.go` | ~20–30 dòng |
| 8 | Không ai xác nhận "xong là gì"/danh sách loại | Vế "người dùng xác nhận" chưa làm | Cổng tuỳ chọn sau thăm dò: in dòng `Loại đầu vào:` và hỏi người "còn thiếu loại nào"; câu trả lời vào đề bài lượt mở rộng. Dùng lại cơ chế câu hỏi và `markAwaitingHuman` | `internal/workflow/bprime_probe.go`, `goalfreeze.go`, `internal/cli/run.go`, i18n | ~100–150 dòng |
| 9 | Không ai soát thước đo thiếu | Lối thoát chỉ cho "sai"; reviewer không được giao soát độ phủ | Thêm vào `bprimeReviewGuide` một bước: đối chiếu dòng `Loại đầu vào:` với yêu cầu gốc (và danh mục ở #6), loại thiếu là phát hiện có `repro` | `internal/workflow/bprime.go` | ~10 dòng |
| 10 | N2 không ai sửa | Cổng hiệu năng cần lệnh do người cấu hình | b-req gọn đề xuất lệnh đo khi yêu cầu chạm đường nóng (người duyệt như lệnh kiểm chứng); luật thăm dò thêm một ca đầu vào lớn có ngưỡng thời gian tương đối | `internal/workflow/bprime.go`, `bprime_goal.go`, `perf.go`, `internal/cli/task.go` | ~60–100 dòng |
| 11 | Review tới khi hết giờ | Không có ngân sách tổng | Khoá `workflow.budget_minutes`; `bprimeAfterCore` và `sliceTimeoutFor` trừ phần dành cho review + một vòng sửa; hết quỹ thì bỏ lượt mở rộng và sang review | `internal/config/*`, `internal/workflow/slice.go`, `bprime_probe.go`, `run.go` | ~100–150 dòng |
| 12 | 4 lát đều "giữ" với test cũ đỏ | "Giữ" đứng đầu danh sách, không truyền test đỏ sang lát sau | Đổi thứ tự (`more` trước); `keep` khi test cũ đỏ thì ghi tập đỏ vào `SliceGreen` và đưa vào `sliceBlock` lát kế như việc làm trước; lát sau chỉ bị giữ khi có test đỏ mới | `internal/workflow/gate.go`, `slice.go`, `internal/task/task.go` | ~60–90 dòng |
| 13 | Thăm dò chạy lại sau khi tiến trình chết (~6,6M token) | Kết quả thăm dò chỉ ở bộ nhớ | Ghi tệp thăm dò vào thư mục tác vụ ngay khi xong; resume đọc lại thay vì chạy lại | `internal/workflow/bprime_probe.go` | ~50–80 dòng |

### Inferences
- Thứ tự lợi/chi phí theo tôi: #1 và #2 (lỗi nối dây, rẻ, là điều kiện để mọi phép đo B′ sau này có nghĩa), rồi #3–#4 (đúng khuyến nghị 1 của README nhưng cần cả tầng hẹp thật, nếu
  không engine sẽ tự chạy 39 gói mỗi lần), rồi #6–#9 cho thước đo, #12 cho classic.
- #6 và #8 là hai cách khác nhau để đưa thông tin ngoài model vào thước đo; báo cáo gốc chỉ nêu "người dùng xác nhận", còn danh mục ngoài là đề xuất của README đợt 7. Cả hai chưa có
  bằng chứng đo.
- Nếu làm #3 mà không làm #4, mốc "kết quả đầu tiên" vẫn bị chặn dưới bởi ~20 phút của lần chạy 39 gói đầu tiên.

### Gaps
- Các ước lượng cỡ chưa tính test, bản vàng (`flow_golden`, golden TUI) và hai bảng kiểm CLI mà dự án đòi khi thêm lệnh hay cờ (`cliRefs`, `litsrUsage`).
- Chưa kiểm khả năng làm #3 ở driver session (tài liệu ghi kiểm nền chưa có ở driver session, `docs/workflows.md:1725`).
- Không tìm thấy trong mã cơ chế nào đưa kết quả kiểm vào giữa một lượt `codex exec` đang chạy; #3 giả định agent tự đọc kết quả bằng lệnh hoặc nhận ở lượt kế.
