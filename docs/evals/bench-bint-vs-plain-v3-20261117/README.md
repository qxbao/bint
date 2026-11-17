# Benchmark Bint so với không dùng Bint, lần 3 (2026-11-17)

Plan: [benchmark-bint-vs-plain-v3-plan.md](../../benchmark-bint-vs-plain-v3-plan.md). Workspace chạy: `/home/baodq/bench-bint-v3-20261117/`.
Commit gốc `8757340`; binary đóng băng `bin/bint` sha256 `35a8ebc3…0691` (`log/bint.sha256`). Codex `gpt-6.1-sol` cho mọi bước của cả hai bên.
Một lượt mỗi task mỗi bên (đã duyệt). Chưa commit, chưa push.

## 1. Kết luận

**Theo bảng chốt trước ở mục 8: "Bint bất lợi"** — A (không Bint) tốt hơn ở cả hai task, B (Bint) không tốt hơn ở task nào;
chi phí của B gấp ~11× token vào và chạm trần 90 phút ở cả hai task.

| Task | A (không Bint) | B (Bint) | Kết luận |
|---|---|---|---|
| T1 dọn dữ liệu cũ | 0 critical, 1 medium | 0 critical, 3 medium | A tốt hơn (ít hơn 2 medium) |
| T2 không lưu bí mật | 5 critical, 2 medium | 6 critical, 7 medium | A tốt hơn (ít hơn 1 critical, 5 medium) |

Độ tin cậy: **thấp-trung bình.** Mỗi ô chỉ một lượt; T2 chênh đúng 1 critical (nếu bỏ lỗi critical riêng của B, Y1 ở mục 5, T2 thành 5–5 critical nhưng
A vẫn ít hơn 5 medium nên kết luận T2 không đổi); T1 chênh đúng 2 medium (ngưỡng của luật). Hai bên không ai đạt mục tiêu T2 (cả hai còn ≥ 5 critical).

**Điều quan trọng cần đọc cùng kết luận:** cả hai lượt B **bị cắt ở trần 90 phút khi đang giữa vòng sửa thứ 3** (phiếu `interrupted`),
với review vòng 2 còn 2 phát hiện nghiêm trọng (T1) và 5 (T2) chưa sửa. Kết quả B là trạng thái repo tại lúc cắt, đúng quy tắc trần của plan.
Chưa đo B nếu được chạy tiếp; không suy ra B sẽ tốt hơn hay kém hơn.

So lần 2 (cùng task, cùng thẻ người dùng): T1 hoà → A hơn; T2 B hơn 1 critical → A hơn 1 critical. Lần 2: T1 A 0C/3M, B 0C/2M; T2 A 6C/3M, B 5C/5M.
Lần 3: T1 A 0C/1M, B 0C/3M; T2 A 5C/2M, B 6C/7M. Bản sửa luồng b-do **không** cải thiện kết quả của B trong phép đo này (B xấu đi ở cả hai task),
trong khi A dao động nhẹ theo hướng tốt hơn; với một lượt mỗi ô, chênh này nằm trong dao động có thể có.

## 2. Chi phí

Token vào (tổng `input_tokens` của mọi phiên Codex có `cwd` trong repo, `tools/tokens.py`, mốc `2026-11-16T17:50Z`):

| Lượt | Phiên | Token vào | Trong đó cache | Token ra |
|---|---|---|---|---|
| A-T1 | 2 | 8,77M | 8,38M | 37k |
| A-T2 | 2 | 5,93M | 5,56M | 39k |
| B-T1 | 10 | 95,70M | 92,26M | 295k |
| B-T2 | 8 | 69,99M | 66,44M | 239k |
| **A tổng / B tổng** | | **14,70M / 165,69M** | | |

- **Tốn kém = 11,3×** (ngưỡng chốt 2,5×; lần 2 ~2,1–2,5×). Không cache: A 0,76M, B 6,99M (9,2×).
- Thời gian tường: A-T2 ≈ 19 phút, A-T1 ≈ 22 phút (gồm 1 tin nhắn người dùng, 3 phút); B-T1 và B-T2 đều **90 phút (trần, timeout)**.
- Tách B theo bước (B-T1 / B-T2): b-req ≈ 2,3M / 1,7M (6m43s mỗi lượt). Phần còn lại tập trung ở b-feat: phiên chính 44,1M / 25,6M
  cộng 3 phiên con song song bắt đầu 17:59–18:00Z (27,3M / 32,0M) — ≈ 75% / 82% tổng. Các vòng sửa nối tiếp phiên b-feat chính nên token của chúng nằm trong phiên chính
  (`tools/tokens.py` lấy giá trị cộng dồn lớn nhất mỗi phiên, không tách theo vòng). Mỗi b-review ~2–5M. Bước b-feat đầu mất 42m (T1) / 34m (T2), vòng sửa 1 mất 12m / 14m.
- Diff của B lớn gấp ~6 lần: B-T1 251 KB so với A-T1 40 KB; B-T2 240 KB so với A-T2 37 KB (`diffs/`).

## 3. Chuẩn bị và đối chứng (trước khi chạy agent)

- Repo: 4 bản clone `8757340`. A: gỡ Bint + commit `bench:` sửa `TestDocCommandsExist` (bỏ qua nguồn tài liệu đã gỡ, `log/baseline-fix-A-T*.patch`).
  B: `bint init --hooks`, codegraph index, trust project + hook trong `~/.codex/config.toml` (khối `bench-bint-v3-20261117`, sao lưu `log/codex-config.toml.before-bench`).
  `go test ./...` xanh trên cả 4 bản sau sửa. Baseline B xanh sẵn.
- Đối chứng âm trên binary gốc (`results/controls/`): `noop`, `naive`, `lockhog`, `t2-orig` **khớp lần 2** sau một thay đổi fixture duy nhất:
  agent giả của D5 ghi thêm `slow_changed.txt` vì cổng "không đổi tệp" mới (`log/fixture-changes.md`; kết quả trước khi sửa ở `results/controls-v2fixture/`).
- Chủ dự án duyệt đối chứng trước khi chạy lượt agent (câu hỏi duyệt ngay trong phiên).

## 4. Kết quả từng lượt

Ánh xạ nhãn mù (mở niêm phong sau khi xác minh xong): **X = A, Y = B** cho cả T1 và T2 (`results/blind-mapping.sealed.json`).

### T1 (dọn dữ liệu cũ)

| | A | B |
|---|---|---|
| Cách dùng | `bint gc --dry-run` / `bint gc`, mặc định giữ 30 ngày, dọn khi nạp sự kiện (tối đa mỗi ngày) | không có lệnh; dọn tự động chạy sau mỗi lần nạp spool (kích hoạt bằng `bint status`), mặc định 90 ngày |
| build/vet/staticcheck Linux+Windows | PASS | PASS |
| `go test ./...` | PASS | FAIL (`TestCollectDenialBatchAndSymlinks`, 5/5 lần) |
| Test ẩn D1–D9 | D1–D5, D7, D9 qua; D6 medium; D8i (xem trước không liệt kê phiếu cũ) | D1–D5, D7, D9 qua; D6 medium; D8 medium (không xem trước) |
| Review mù đã xác minh | X2 medium (≡ D6), X1 hạ xuống low, X3–X5 low | Y1 medium (≡ D8), Y5 medium (≡ `go test` đỏ), Y2–Y4, Y6–Y8 low |

Lỗi sau khi bỏ trùng (test ẩn + review mù + xác minh mù + `go test`):
- **A: 0 critical, 1 medium** — dọn gần như không lấy lại được dung lượng trên dữ liệu thực tế: log phiếu đã xong/đã huỷ, `runtime/*`, SQLite không bị động tới (D6 ≡ X2: fixture 685 KB chỉ thu hồi 144 byte).
- **B: 0 critical, 3 medium** — (1) D6: log phiếu `done` cũ không được dọn (thiết kế giữ tài liệu công việc); (2) D8 ≡ Y1: không xem trước, xoá vĩnh viễn ngay lần `bint status` đầu; (3) Y5 ≡ `go test` đỏ: test mới của chính B hỏng ổn định nên CI đỏ.
- Lỗi low đáng nhớ của B: SQLite không co file (Y2), collector không bao giờ báo thành công trên dữ liệu nhiều phiếu (Y3), xoá `test.completed` làm lịch sử lượt chạy cũ báo 0 test (Y4).
- Cả hai: không phiếu chưa kết thúc, worktree hay symlink nào bị đụng (D1–D5 qua).

### T2 (không lưu bí mật)

Test ẩn: cả hai **y hệt đối chứng gốc ở mức tổng**: S1–S5 critical và S6 medium đều đỏ; chi tiết đã khác bản gốc (ví dụ S1/S2 chỉ còn rò `private_key`), tức cả hai che được một phần nhưng bỏ sót nơi lưu.
Build/vet/staticcheck Linux+Windows qua cả hai; `go test ./...`: A PASS, B FAIL (3 test ở `internal/cli`: `TestEvalMinimalFromBootstrapSnapshot`, `TestSkillTestWritesRatesJSON`, `TestWorkflowEndToEnd`).
N1 (che quá tay làm hỏng hash/ID) qua cả hai ở test ẩn; N2 (hiệu năng, 20 MB, 3 lần xen kẽ, máy rảnh): A 3,5/3,4/3,4 s, B 13,5/13,4/13,6 s, gốc 0,9 s → **A qua, B hỏng (~15×)**.

Lỗi sau khi bỏ trùng theo bề mặt:
- **A: 5 critical, 2 medium.** Critical = S1–S5 (log kiểm chứng, đầu ra agent, mô tả phiếu, review, SQLite hook; X1/X2 của review là cùng các bề mặt này). Medium = S6 (tài liệu phiếu) và X4 (biến môi trường "nhạy cảm" giá trị ngắn như `1`/`true` bị che khắp nơi, làm hỏng số/JSON trong log chẩn đoán). X3 (bí mật không nhãn: `ghp_` trần, `AKIA…`, PEM, token trong URL) được xác minh hạ từ critical xuống medium vì bản gốc cũng không che, nhưng test ẩn S1/S2 đã chốt critical cho PEM nên đếm theo S1/S2.
- **B: 6 critical, 7 medium.** Critical = S1–S5 + **Y1**: che dữ liệu vận hành rồi ghi lại `state.json` làm hỏng vĩnh viễn commit base/đường dẫn worktree/timestamp khi có biến môi trường nhạy cảm giá trị ngắn (ví dụ `HF_HUB_DISABLE_IMPLICIT_TOKEN=1`); sau đó `bint task verify` lỗi kể cả khi bỏ biến. (Y2: hồi quy — `PGPASSWORD=`, `AWS_SECRET_ACCESS_KEY=`, `GITHUB_PAT=` trong sự kiện hook không còn được che, bản gốc có che; Y-E1: dạng `nhãn: giá trị` giữa dòng không bị che trong log kiểm chứng và `task.yml` — hai mục này cùng bề mặt S5/S1/S3 nên không cộng thêm.)
  Medium = S6, N2/Y8 (che chậm 15–88×), Y4 (dấu nháy không đóng nuốt hết phần còn lại của log), Y5 (dòng log bắt đầu bằng `[1/3]` hoặc `[2026-…]` bị thay bằng thông báo che), Y6 (CLI đệm stdout theo dòng: câu hỏi tương tác như `[y/N]` chỉ hiện sau khi gõ), Y7 (từ chối lệnh kiểm chứng chứa credential giả `DB_PASSWORD=postgres go test`, phiếu cũ ngừng chạy), `go test` đỏ.
- Hai bên đều không đạt mục tiêu "không để bí mật ở bất kỳ chỗ nào bint lưu".

## 5. Kiểm giả thuyết của bản sửa (chỉ B)

| | Kết quả |
|---|---|
| H1 Classify ra "cần b-req"; b-req ghi "Bề mặt bị ảnh hưởng" | **Đạt cả hai.** Classify: "yêu cầu ngắn nhưng chưa nêu tệp, hành vi hay tiêu chí cụ thể: cần b-req". `plan.md` có `## Bề mặt bị ảnh hưởng` và `## Giả định thay người dùng` (không nằm trong `requirement.md`). Bề mặt vẫn thiếu: T2 B còn lọt S1–S5 |
| H2 Không `done` với diff rỗng | **Đạt** (cả hai chưa `done`, diff lớn) |
| H3 Không kẹt ở "hết vòng sửa" | **Không đo được**: cả hai lượt chạm trần 90 phút khi đang vòng sửa 3, trước khi tới lối ra |
| H4 Vòng sửa nối tiếp phiên b-feat với Codex 0.169 | **Có**: mỗi vòng sửa in "↻ nối tiếp phiên 01a1e582… của bước b-feat (lớp dừng review_fix, lần 0)". Vòng sửa 1 mất 11m51s (T1) / 14m22s (T2) so với b-feat đầu 42m / 34m; token của vòng sửa nằm trong phiên chính |
| H5 Review của B có ghi phát hiện "bỏ sót" | **Có**: T1 vòng 1 "Bỏ sót log verify trước nâng cấp", vòng 2 "Bỏ sót journal spool/denials"; T2 vòng 1 "Bỏ sót CLI quản trị MCP", vòng 2 "TUI vẫn đọc nguyên tài liệu và log kiểm chứng cũ ngoài lớp che". Chưa xác minh riêng từng phát hiện này; review mù độc lập vẫn tìm ra nhiều chỗ bỏ sót còn lại |

Trong output của B không có câu hỏi cho người dùng hay lối ra `--answer`/`--fix-round`/`--accept` (không tới được trước trần), nên quy tắc 6.2 của plan không được dùng ở lần này.

## 6. Hành động người dùng và can thiệp

`log/user-actions.md` (nguyên văn):
- **A-T1:** 2 lượt. Lượt 1 xong; người dùng thử `bint gc --dry-run` trên binary đóng băng → "lệnh lạ"; tin nhắn #1 dán nguyên đầu ra; agent bảo build lại và ghi đè binary → chạy nguyên văn nhưng **đích build đổi sang `tmp/user-A-T1/bin`** (mục 6.3), build exit 0, `bint gc --dry-run` ra "Sẽ dọn 0 mục, 0 byte". Tổng 1 tin nhắn, 1 lệnh chạy nguyên văn (đổi đích), 0 thao tác kỹ thuật, 0 kẹt.
- **A-T2:** 0 tin nhắn (agent không hỏi, không đưa lệnh nào để thử).
- **B-T1, B-T2:** 0 tin nhắn, 0 lệnh (Bint không in lệnh nào cho người; lượt chạm trần).

Sự cố môi trường (`log/env-incidents.md`):
1. 17:51Z hai lượt B thoát ngay exit=4: chuẩn bị `bint init` dùng state tạm thay vì state mặc định như lần 2. Sửa và chạy lại lúc 17:52Z (log gốc ở `log/aborted/`); lượt A không bị ảnh hưởng.
2. 19:22Z B-T1/B-T2 timeout; một `codex exec` mồ côi của B-T2 (pid 1873584) vẫn sống sau timeout nên bị dừng thủ công để trần có hiệu lực; kết quả B = trạng thái repo tại đó (không resume).
3. Reviewer mù T1 đọc (chỉ đọc) `~/.local/state/bint`, chép một thư mục state thật (33 MB) sang thư mục tạm để mô phỏng và lỡ chạy `git status` trong 4 worktree của một state khác; không đổi nội dung, không ảnh hưởng 4 repo benchmark; bước xác minh được dặn không lặp lại.
4. Đĩa `/` còn 13–16 GB trong lúc chạy (yêu cầu ≥ 30 GB trước khi chạy: có 32 GB lúc bắt đầu); không có lỗi hết đĩa.
5. Mốc `--since` của `tools/tokens.py` ban đầu loại nhầm phiên chính của A (so sánh chuỗi `…27Z` với `…27.2Z`): đã sửa mốc về `17:50Z` và đo lại; số ở mục 2 là số đã sửa.
6. `mkdiff.py` xoá ID dạng ULID nên một số dòng `k:<ID>` trong diff mù không áp dụng được nguyên văn; verifier T2 tự phục hồi ID khi áp diff.

## 7. Rủi ro và thiên lệch

- Một lượt mỗi ô; chênh 1 critical (T2) và 2 medium (T1) là kết luận yếu. Đã đếm bỏ trùng theo bề mặt, có chỗ phải phán đoán (ví dụ coi Y2/Y-E1 trùng S5/S1/S3; hạ X3/Y3 vì bản gốc cũng không che). Chưa tính lại bằng cách đếm thay thế (không gộp các mục cùng bề mặt).
- B bị cắt ở 90 phút giữa vòng sửa; số lỗi của B đo ở một thời điểm chưa "xong". Điều này thuộc luật trần của plan nhưng cũng là một kết quả: trong 90 phút Bint chưa tới trạng thái hoàn tất cho yêu cầu mơ hồ ở cả hai task.
- Reviewer mù biết kích thước diff (B lớn gấp ~6×), không biết nhãn A/B. Người điều phối biết T1-X là bản có lệnh `gc` khi điền trigger nhưng không đổi cách chấm.
- Mục "Bề mặt bị ảnh hưởng" của b-req khiến B làm rộng hơn nhiều (T1: xoay log gateway, VACUUM, collector có ngân sách; T2: CLI, MCP, TUI, spool); phạm vi rộng sinh thêm bề mặt lỗi (Y1, Y5, N2, `go test` đỏ). Đây là giả thuyết giải thích, chưa kiểm chứng.
- Lợi thế tri thức của B (mục knowledge về fslock, Windows): hợp lệ; cả hai qua build/vet/staticcheck Windows.

## 8. Việc còn lại (cần chủ dự án quyết)

1. Gỡ khối trust `bench-bint-v3-20261117` khỏi `~/.codex/config.toml` (chưa gỡ; khối `bench-bint-v2-20261116` vẫn còn). Xoá `/tmp/.git` (hiện không tồn tại).
2. Có chạy lại B với trần dài hơn (hoặc cho người dùng thật bấm `--fix-round`/`--accept` theo mục 6.2) để đo B khi xong hẳn không; token dự kiến lớn hơn ~165M.
3. Điều tra nguồn token của B: 3 phiên con song song trong b-feat chiếm ~25% tổng; phạm vi b-req quá rộng cho yêu cầu mơ hồ.
4. Chưa commit/push gì (theo mục 11.6 của plan). Dữ liệu thô ở workspace chạy; bản sao biên bản ở thư mục này.

## 9. Phân tích nguyên nhân (điều tra sau lượt chạy, 2026-11-17)

Nguồn: `plan.md`/`review.json`/`state.json` của hai phiếu B, phiên Codex trong `~/.codex/sessions`, skill b-req/b-review của repo B. Đây là phân tích sau khi có số, không đổi bảng chốt ở mục 8.

**Đính chính số liệu.** `go test` đỏ của B (T1 `TestCollectDenialBatchAndSymlinks`; T2 3 test ở `internal/cli`) là do trần 90 phút cắt giữa lúc sửa vòng 2, không phải trạng thái B tự coi là xong: `denials.go` sửa lúc 19:21:40Z, `denials_test.go` lúc 19:20:45Z, kill lúc 19:21:58Z; còn `verify-2.log` (cuối vòng sửa 1, đầu vòng sửa 2) của cả hai phiếu xanh toàn bộ `go test ./...`. Trừ mục này: T1 B = 0C/2M (A 0C/1M, chênh 1 medium < 2) → **T1 thành "Hoà"**; T2 B = 6C/6M, A vẫn tốt hơn. Kết luận chung vẫn "Bint bất lợi" (A hơn ở T2, B không hơn ở task nào), nhưng biên mỏng hơn đã báo ở mục 1.

**Nguyên nhân, xếp theo mức tin cậy:**
1. *Kế hoạch phình gấp đôi vì chính chỉ dẫn mới của b-req* (đã đo). Lần 2: plan 6,6 KB, 6 AC, 5 TSK (T1). Lần 3: plan 14,2/12,2 KB, 8/7 AC, 7/8 TSK. Skill b-req bắt "liệt kê từng nơi, mỗi nơi một dòng, mỗi dòng một AC hoặc lý do ngoài phạm vi" (~30 dòng), "không thu hẹp phạm vi tới mức không đạt mục đích", và cấm đẩy sang câu hỏi mở. Kết quả T1: một package `internal/maintenance` mới, khoá liên tiến trình xuyên workflow/eval/mcpgw/spool, xoay log gateway. Diff B ~6× A.
2. *Giả định thay người dùng không ai kiểm và trái điều người dùng nói* (đã đo ở nội dung plan). Dưới b-do không có cổng duyệt giữa b-req và b-feat; b-req ghi "Câu hỏi mở: Không có". T1: người dùng "xem trước thì tốt" nhưng plan chốt "Không thêm lệnh CLI", xoá vĩnh viễn (D8). T2: người dùng "không muốn chạy hỏng/chậm" nhưng plan chọn "thay nguyên giá trị dài nhất của mọi env nhạy cảm" (gốc của Y1: giá trị `1` làm hỏng `state.json`), "từ chối lệnh chứa credential" (Y7), che trước cắt + escape JSON/URL + dòng 64 KiB (gốc N2 chậm 15×, Y6).
3. *Chi phí token: nhiều lượt gọi × ngữ cảnh lớn × agent con tự sinh* (đã đo, nguyên nhân sinh chưa xác minh). A: 46–66 lượt gọi công cụ, ngữ cảnh tối đa ~160 k. B b-feat chính: 135–176 lượt, ngữ cảnh 330–370 k → 44,1M / 25,6M token vào; 3 agent con do chính b-feat sinh (`/root/config`, `/root/sqlite`, `/root/logs` ở T1; `process_logs`, `task_records`, `events_hooks` ở T2) mỗi cái 50–105 lượt, ngữ cảnh 165–207 k → 27–32M; vòng sửa sinh thêm 2 con (~150 lượt, ~300 k). Skill b-feat không dặn dùng agent con; Codex tự quyết.
4. *Vòng review–sửa không hội tụ* (đã đo). Quy tắc 0 của b-review xếp "bỏ sót một bề mặt" là `high`; kế hoạch đã liệt kê ~30 bề mặt nên reviewer luôn tìm được thêm. Vòng 1→2 T1: 3 high → 2 high, T2: 5 high → 5 high; phần lớn là lỗi mới trong chính mã vừa thêm (T1: "alias hợp lệ của state khiến gateway không thể khởi động"; T2: biên YAML nhiều tài liệu, command substitution, quote trong tên cờ). Mốc thời gian T1: b-req 7 phút, b-feat 47, review 9, sửa 1 16, review 8, sửa 2 bị cắt ở phút 90. Phần b-feat đầu tiên đã dài hơn cả lượt A (19–22 phút).
5. *Không có trạng thái xanh để quay về khi bị cắt* (đã đo). Bint chỉ để cây làm việc ở trạng thái dở khi interrupted; không có điểm "đã verify xanh gần nhất" để giao.
6. *Chưa giải thích được:* cả hai bên đều đỏ S1–S5 ở T2 dù B liệt kê đủ các bề mặt đó trong plan; chưa điều tra vì sao liệt kê không thành che đúng.

**Giải pháp đề xuất** (chưa làm, chưa chạy lại để kiểm):
- b-req: đặt ngân sách theo yêu cầu mơ hồ (ví dụ ≤ 5 AC, ≤ 5 TSK cho giai đoạn đầu; phần còn lại vào "Đề xuất tách giai đoạn" vốn đã có), gộp bề mặt theo nhóm thay cho "từng nơi một dòng", và ưu tiên bản nhỏ nhất đạt mục đích kèm hậu quả phần để sau.
- b-req/b-do: với yêu cầu mơ hồ hỏi tối đa 2–3 câu về kết quả mong muốn (cần xem trước/bật tay không, điều gì không được mất hay chậm) trước khi lập kế hoạch, thay cho việc tự giả định; giả định không đảo ngược được (xoá vĩnh viễn, từ chối lệnh, đổi giá trị dữ liệu) mặc định chọn phương án đảo ngược được (dry-run/xem trước, cảnh báo thay vì từ chối).
- b-feat/harness: tắt agent con tự sinh ở bước b-feat của Codex (hoặc cho phép nhưng đo token riêng); thêm trần lượt gọi/ngữ cảnh mỗi bước.
- b-review: bề mặt ngoài kế hoạch hoặc ngoài AC chỉ là `low`/ghi chú; `high` dành cho AC chưa đạt và hồi quy. Thêm điều kiện dừng: số high không giảm sau một vòng sửa thì dừng và giao tóm tắt cho người dùng.
- b-do: ghi nhận điểm verify xanh cuối cùng; khi hết giờ/ngân sách thì trả về trạng thái xanh đó cùng danh sách phát hiện còn mở.
- Đo lại: chạy lại B ít nhất 3 lượt mỗi ô với bản sửa; thêm điểm "trạng thái xanh cuối" cho B để không phạt cú cắt giữa chừng.

## Cấu trúc

`diffs/` (4 diff so với commit gốc, đã xoá ID), `blind/` (diff mù + trigger.json), `hidden/` (test ẩn + đối chứng + `hidden2.py.v2` bản gốc trước khi sửa fixture D5), `results/` (test ẩn, đối chứng, N2, token),
`review/` (phát hiện + xác minh, risk-spec, thẻ người dùng), `log/` (timeline, hành động người dùng, đầu ra agent, sự cố, patch baseline), `tools/` (script chạy).
