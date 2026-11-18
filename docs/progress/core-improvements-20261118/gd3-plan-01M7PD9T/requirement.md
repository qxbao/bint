# Yêu cầu

## Mục tiêu và phạm vi
GĐ3 của phiếu cha 01M7PD9SZJAT7R6RTC2V8Y4NC5 (benchmark `docs/evals/bench-bint-vs-plain-tong-hop.md`): (1) hồi quy hiệu năng
(T2 đợt 5: 0,9 s → 14,4 s) không bị cổng nào bắt; (2) b-feat một khối 34 tệp; (3) review không hội tụ vì phát hiện `high` chỉ dựa trên
lập luận. Giai đoạn này thêm hai cổng đo được sau mỗi bước b-feat — **ngân sách diff** và **hồi quy hiệu năng so với mốc** — tính như
kiểm chứng không qua, và buộc phát hiện review ở mức chặn phải kèm **bằng chứng tái hiện**. Dựa trên GĐ1 (b-goal, cổng mục tiêu) và GĐ2
(lát mỏng, `sliceGate`, điểm xanh) đã có trong mã.

## Yêu cầu chức năng
1. **Trường `repro` của phát hiện review** (`bint task review`, JSON/YAML): chuỗi tuỳ chọn mô tả cách tái hiện — lệnh trong backtick kèm
   kết quả quan sát được, hoặc `tệp:dòng` nơi dữ liệu/mã còn sai. Hợp lệ khi có ít nhất một lệnh trong cặp backtick, hoặc một tham chiếu
   `đường-dẫn:dòng` (dạng `a` hoặc `a-b`) tới tệp có thật (tương đối với repo và dòng trong phạm vi tệp, hoặc đường dẫn tuyệt đối tới tệp có
   thật như log kiểm chứng của Bint).
2. **Hạ mức khi thiếu bằng chứng**: lúc ghi vòng review, phát hiện `bug`/`risk` mức `critical`/`high` không có `repro` hợp lệ được ghi ở mức
   `medium`, kèm trường đánh dấu mức gốc (`downgraded_from`); `review.md` và đầu ra `bint task review` nêu rõ phát hiện nào bị hạ và vì sao.
   Không từ chối cả lần ghi. Vòng review đã ghi trước bản này giữ nguyên (không tính lại).
3. **Phát hiện chặn giữ repro tới b-feat**: đề bài vòng sửa / lời nhắn nối tiếp (`blockingFindingsBlock`) và đề bài review vòng sau
   (`previousBlocking`) in kèm `repro` của từng phát hiện chặn.
4. **Đề bài review** (`reviewPrompt` trong `internal/workflow/gate.go`) và **skill b-review** nêu luật: chỉ `critical`/`high` khi có `repro`;
   "bỏ sót bề mặt" chỉ dựa trên lập luận (không lệnh, không tệp:dòng còn sai) là `medium`/`low`; kiểm chứng/cổng đỏ thì `repro` là log
   của Bint (`đường-dẫn-log:dòng`). Skill chỉnh ở `internal/skills/core/b-review/SKILL.md`, xuất bản lại `.claude/skills`, `.agents/skills`
   và `.bint/skills/core.lock` bằng `bint sync` của binary build từ mã mới.
5. **Ngân sách diff** (`workflow.max_diff_lines`, 0 = tắt): sau mỗi bước b-feat (kể cả vòng sửa, đường nhanh, driver session), đếm số dòng
   thêm + xoá (`git diff --numstat`, tệp nhị phân không tính dòng) trên tệp của tác vụ, trừ tệp kiểm tra mục tiêu đóng băng (`State.GoalTest`):
   có lát đang chạy thì so với cây điểm xanh của lát; không có lát thì so với mốc của tác vụ (`BaseTree`). Vượt trần → ghi kết quả đỏ
   (số dòng, trần, 5 tệp lớn nhất).
6. **Cổng hiệu năng** (`workflow.perf.command`, rỗng = tắt; `workflow.perf.max_slowdown`, mặc định 1.5): chỉ chạy khi lệnh kiểm chứng
   của phiếu đã được xác nhận (cùng luật cổng mục tiêu). **Mốc**: một lần, ngay trước bước b-feat đầu tiên của tác vụ (sau b-goal), chạy lệnh
   3 lần dưới khoá độc quyền, lấy thời gian thực nhỏ nhất; mốc thoát khác 0 thì tắt cổng cho tác vụ kèm ghi chú. **Đo lại** sau mỗi bước
   b-feat cũng 3 lần lấy nhỏ nhất; đỏ khi lệnh thoát khác 0, hoặc thời gian > mốc × `max_slowdown` **và** chênh > 0,1 giây (sàn nhiễu).
   Log mỗi lần đo ở `perf-N.log` (thời gian từng lần, mốc, tỷ lệ). Tác vụ đã chạy b-feat trước bản này (không có mốc) thì bỏ qua cổng kèm ghi chú.
7. **Đỏ = kiểm chứng không qua**: kết quả đỏ của (5) hoặc (6) — (a) có lát đang chạy: lát bị bỏ (`discardSlice`, cây về điểm xanh), lý do
   nêu số đo; (b) không có lát: được tính như kiểm chứng đỏ (`redVerification`): review thấy nó trong đề bài, mở vòng sửa trong ngân sách,
   không finalize khi còn đỏ, hết vòng thì dừng với ba lựa chọn như hiện nay. Đề bài vòng sửa nêu số đo và cách sửa (thu gọn diff về bản
   nhỏ nhất đạt mục tiêu / tìm chỗ chậm). Cổng `bint task done` và `bint task verify` của người không đổi (`--accept` vẫn là lối ra).
8. **Cấu hình** theo quy ước: struct, `Default()`, `Validate()` (`max_diff_lines` < 0, `max_slowdown` < 1 là lỗi), dòng có chú thích trong mẫu
   `Render` (không `%` trần).

## Tiêu chí chấp nhận
- [ ] AC-1: Review chặn cần bằng chứng: phát hiện bug/risk mức high hoặc critical gửi qua bint task review mà không có trường repro hợp lệ (lệnh trong backtick, hoặc tệp:dòng có thật) được ghi ở mức medium kèm đánh dấu mức gốc (review.json, review.md, đầu ra lệnh), không tính là nghiêm trọng nên không mở vòng sửa và không tính vào đếm hội tụ; phát hiện có repro hợp lệ giữ mức chặn và repro được đưa vào đề bài vòng sửa của b-feat; vòng review cũ không bị tính lại
- [ ] AC-2: Đề bài review do internal/workflow/gate.go dựng và skill b-review (internal/skills/core, .claude/skills, .agents/skills, .bint/skills/core.lock đồng bộ) nêu luật: mức chặn cần repro tái hiện; bỏ sót bề mặt chỉ dựa trên lập luận là mức không chặn; có ví dụ trường repro
- [ ] AC-3: Ngân sách diff: khoá workflow.max_diff_lines (0 = tắt) giới hạn số dòng thêm+xoá trên tệp của tác vụ (không tính tệp kiểm tra mục tiêu đóng băng) của mỗi lát so với điểm xanh, hoặc của phần b-feat đã làm so với mốc tác vụ khi không chia lát; vượt sau một bước b-feat thì ghi kết quả đỏ có số đo, coi như kiểm chứng không qua (lát bị bỏ; không chia lát thì vào vòng sửa, không finalize)
- [ ] AC-4: Cổng hiệu năng: khi workflow.perf.command khác rỗng và lệnh kiểm chứng đã xác nhận, Bint đo mốc trước bước b-feat đầu tiên và đo lại sau mỗi bước b-feat (lấy thời gian nhỏ nhất của 3 lần); chậm hơn mốc quá workflow.perf.max_slowdown lần (và quá sàn nhiễu 0,1 giây) hoặc lệnh thoát khác 0 thì ghi kết quả đỏ có số đo và log perf-N.log, coi như kiểm chứng không qua như ngân sách diff
- [ ] AC-5: Cấu hình và tài liệu: workflow.max_diff_lines (mặc định 0), workflow.perf.command (mặc định rỗng) và workflow.perf.max_slowdown (mặc định 1.5, Validate từ chối nhỏ hơn 1, max_diff_lines âm) có trong struct, Default(), Validate() và mẫu Render; docs/workflows.md mô tả hai cổng và luật bằng chứng tái hiện của review
- [ ] AC-6: kiểm tra mục tiêu TestGateEvidenceAndBudget xanh

## Kiểm tra mục tiêu
Test hộp đen ở gói `internal/workflow`, dùng harness agent giả sẵn có của gói (như trong `goalfreeze_test.go`, `slice_goal_test.go`), chạy
`Run` b-do thật trên repo git tạm; mỗi nhóm bề mặt một subtest. Script giả viết bằng `sh` với đường dẫn dạng `/` (chạy được trên Windows).

- **Chuẩn bị**:
  - `review_evidence`: reviewer giả vòng 1 gửi 3 phát hiện: P1 `bug/high` "bỏ sót bề mặt" không có `repro`; P2 `risk/critical` có `repro`
    chỉ là lập luận (không backtick, không tệp:dòng); P3 `bug/high` có `repro` là lệnh trong backtick kèm đầu ra. Ca đối chứng: vòng chỉ có
    P1 và P2. Thêm một `review.json` có sẵn vòng cũ với `high` không repro.
  - `diff_budget`: `workflow.max_diff_lines: 20`; b-feat giả lần đầu ghi tệp sản phẩm 60 dòng, vòng sửa thu còn 10 dòng; tệp kiểm tra mục
    tiêu đóng băng dài > 20 dòng. Ca lát: plan lớn 2 nhóm gắn AC, lát 2 ghi 60 dòng (cổng mục tiêu vẫn giảm ô đỏ). Ca đối chứng `max_diff_lines: 0`.
  - `perf_gate`: `workflow.perf.command` là script ngủ theo giá trị trong tệp `slow.txt` của repo (mốc 0 s); b-feat lần đầu đặt 0,6 s,
    vòng sửa đặt lại 0 s; `max_slowdown: 1.5`. Ca lệnh đo thoát 1 sau b-feat. Ca đối chứng: lệnh rỗng; lệnh kiểm chứng chưa xác nhận.
- **Hành động**: `Run` b-do (có b-goal giả) tới khi dừng hoặc done; ca lát resume sau cổng cỡ kế hoạch như người; đọc kết quả qua
  `review.json`, `review.md`, `State`, log, đề bài các bước mà harness giả nhận.
- **Quét/đo**:
  - review: mức và trường đánh dấu mức gốc của P1/P2/P3 trong `review.json`; dòng tương ứng trong `review.md`; số phát hiện nghiêm trọng mà
    Runner tính (có/không vòng sửa); đề bài b-feat vòng sửa có `repro` của P3; vòng cũ giữ nguyên; đề bài bước review chứa luật "repro".
  - diff: kết quả đỏ/xanh sau mỗi b-feat và số dòng ghi trong đó (tệp đóng băng không tính); đề bài review/vòng sửa nêu số đo; tác vụ không
    finalize khi còn đỏ; lát 2 `discarded` với lý do nêu số dòng và trần, cây về điểm xanh.
  - perf: có mốc trước b-feat đầu; `perf-N.log` tồn tại; kết quả đỏ nêu thời gian, mốc, tỷ lệ; vòng sửa mở và lần đo sau xanh; ca đối chứng không
    có lần đo nào.
  - cấu hình: `config.Default()` và `config.Render(...)` giải mã ra cùng giá trị 0 / "" / 1.5; `Validate` báo lỗi với -1 và 0.9.
- **Điều kiện đạt**: P1, P2 thành `medium` có đánh dấu mức gốc `high`/`critical`, P3 giữ `high`; ca đối chứng (chỉ P1, P2) không mở vòng
  sửa và đi tới finalize; ca có P3 mở vòng sửa và đề bài chứa nguyên văn repro của P3; vòng cũ không đổi. Diff 60 > 20 → đỏ, vòng sửa, 10 ≤ 20 →
  xanh rồi done; lát 2 bị bỏ; đối chứng không có kết quả ngân sách. Perf 0,6 s so với mốc ~0 s → đỏ, vòng sửa, sau đó xanh; lệnh thoát 1 → đỏ;
  đối chứng không đo. Mỗi điều kiện sai báo riêng tên subtest và giá trị đo được.
- **Tên test và tệp dự kiến**: `TestGateEvidenceAndBudget` trong `internal/workflow/budget_goal_test.go` (subtest `review_evidence`,
  `diff_budget`, `perf_gate`, `config`), nằm trong `go test -timeout 30m ./...` của `verify.command`.

## Ngoài phạm vi
- Cổng `bint task done`/finalize không đòi ngân sách diff hay hiệu năng: người `--accept` sau khi hết vòng sửa vẫn hoàn tất được (hậu quả:
  thay đổi chậm/lớn có thể được người chấp nhận có ý thức; kết luận lượt vẫn nêu kết quả đỏ cuối).
- Bint không tự chạy lệnh trong `repro` (lệnh do agent đề xuất chưa được người xác nhận): chỉ kiểm hình thức. Hậu quả: reviewer có thể bịa
  lệnh; luật chỉ chặn được phát hiện không có lệnh/tệp:dòng nào.
- Hiển thị hai cổng mới trong TUI và `bint task show` (chỉ có trong `state.json`, log, kết luận lượt).
- Đo hiệu năng trên cây gốc chưa có thay đổi khi mốc bị thiếu (resume tác vụ cũ): bỏ qua cổng kèm ghi chú.

## Câu hỏi mở
Không có.
