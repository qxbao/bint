# Yêu cầu

## Mục tiêu và phạm vi
GĐ2 của phiếu cha 01M7PD9SZJAT7R6RTC2V8Y4NC5 (benchmark: b-feat một khối 20–47 phút, bị cắt giữa chừng thì không có gì để giao; review
không hội tụ là cách dừng duy nhất). Với kế hoạch lớn của b-do, Runner tự chia việc thành **lát mỏng** theo nhóm `## Bề mặt bị ảnh hưởng`
của plan.md, mỗi lát một lượt b-feat có trần thời gian, và **chốt một chiều** bằng số đo khách quan của cổng kiểm tra mục tiêu (GĐ1, bước
b-goal): lát làm giảm ô đỏ mà không làm đỏ test cũ thì ghim làm điểm xanh mới; không đạt thì tự quay về điểm xanh và bỏ lát đó. Hết ngân
sách hoặc lỗi thì giao điểm xanh cuối kèm danh sách lát chưa làm.

Chỉ áp cho b-do gốc chạy bằng `bint run` (driver process). Kế hoạch nhỏ và mọi đường khác giữ nguyên hành vi.

## Yêu cầu chức năng
1. **Điều kiện bật lát mỏng** (kiểm ở lần đầu mở bước b-feat của tác vụ, khi đã có `State.GoalTest`): flow b-do gốc, driver process,
   không đường nhanh, kế hoạch lớn theo số đo của cổng cỡ kế hoạch (`planSizeReasons` khác rỗng), plan.md có ≥ 2 nhóm cấp ngoài cùng dưới
   `## Bề mặt bị ảnh hưởng` có nêu `AC-N` (nhóm không nêu AC, ví dụ dòng ghi "Ngoài phạm vi", không thành lát), và
   `workflow.slice_timeout_minutes > 0`. Thiếu một điều kiện thì chạy một b-feat như hiện nay. Đã bật thì danh sách lát ghi vào
   `State.Slices` (giữ nguyên khi resume; plan.md sửa sau đó không chia lại).
2. **Thứ tự lát**: theo thứ tự dòng của nhóm trong plan.md; lát 1 là nhóm đầu và phải là "bản nhỏ nhất đạt mục tiêu". Đề bài b-req của
   b-do (do Bint dựng, không phải skill) nêu luật này; lý do chặn của cổng cỡ kế hoạch (kế hoạch lớn) liệt kê các lát Bint sẽ chạy.
3. **Mỗi lát một bước b-feat riêng**: đề bài nêu "lát k/N", nguyên văn dòng nhóm của lát, các lát đã chốt (đừng làm lại) và lát sau (đừng
   làm trước). Timeout của agent = min(`workflow.timeout_minutes`, `workflow.slice_timeout_minutes`).
4. **Đo sau mỗi lát** (sau kiểm phạm vi, kiểm chứng và cổng mục tiêu như hiện nay):
   - *Tập ô đỏ* của một log kiểm tra = tên các dòng `--- FAIL: <tên>` là lá (không có dòng FAIL nào khác có tên bắt đầu bằng `<tên>/`).
     Log **không đo được** khi lệnh quá giờ, có dấu hỏng biên dịch/khởi tạo (`[build failed]`, `[setup failed]`), có `panic:`, hoặc mã
     thoát khác 0 mà không có dòng `--- FAIL:`. Mã thoát 0 = tập rỗng.
   - *Test cũ xanh*: lần kiểm chứng của lát qua, hoặc scope `none`, hoặc log kiểm chứng đo được và mọi ô đỏ có tên cấp đầu (trước `/` đầu
     tiên) thuộc tên test mục tiêu (hàm `Test*` của tệp đóng băng `*_test.go`, hợp với tên cấp đầu của các ô đỏ trong log cổng mục tiêu cùng lát).
   - *Chốt* khi test cũ xanh **và**: điểm xanh trước không đo được → log cổng mục tiêu của lát đo được; điểm xanh có 0 ô đỏ → lát vẫn 0;
     ngược lại → tập ô đỏ của lát là tập con thực sự của tập ở điểm xanh (giảm, không ô xanh nào chuyển đỏ).
5. **Điểm xanh**: điểm xanh đầu là ảnh chụp cây ngay trước lát 1 (sau b-goal) với số đo của cổng mốc. Lát chốt thì chụp cây, ghim ở
   `refs/bint/tasks/<id>/slice-green`, ghi tree và tập ô đỏ vào `State.Slices`; điểm xanh chỉ tiến, không lùi.
6. **Bỏ lát**: lát không chốt được, hoặc agent lát quá trần thời gian → ghim cây hiện tại ở `refs/bint/tasks/<id>/slice-<N>-discarded`, đưa
   tệp của tác vụ (như `restore-green`: tệp thuộc Touched, hoặc mọi tệp khi tác vụ ở worktree riêng) về nội dung điểm xanh (tệp tạo sau đó
   bị xoá; tệp khác giữ nguyên, liệt kê), dưới khoá độc quyền của cây; lát ghi `discarded` kèm lý do có số đo (vd "ô đỏ 3→3 không giảm",
   "test cũ đỏ: TestX", "quá trần 30 phút"). Lát 1 bị bỏ → tác vụ blocked (không có gì để giao ngoài mốc). Lát sau bị bỏ → chạy lát kế.
7. **Hết lát** (mọi lát chốt hoặc bỏ, lát 1 chốt) → chuyển `in_review` và đi tiếp b-review, vòng sửa, finalize như hiện nay.
8. **Giao điểm xanh**: hết ngân sách `limits.workflows.b-do` (`max_cost_usd`, `max_turns`) trước khi mở lát kế hoặc trong khi lát chạy, hay
   agent lỗi trong lát (không phải người huỷ) → lát dở (nếu có) được đưa về điểm xanh như mục 6, tác vụ blocked; lý do và `Result.Slices`
   (JSON `slices`) nêu lát đã chốt, lát đã bỏ (lý do, ref sao lưu) và lát chưa làm. Resume chạy tiếp lát chưa làm đầu tiên.
9. **Kết luận**: mọi lượt có lát mỏng ghi `Result.Slices` và một ghi chú tóm tắt (đã chốt k/N; bỏ: …; chưa làm: …). `LastGreen`
   (GreenReport) và `bint task restore-green` dùng điểm xanh mới hơn trong hai: lần kiểm chứng xanh cuối, điểm xanh lát.
10. **Cấu hình**: khoá `workflow.slice_timeout_minutes` (mặc định 30; 0 = tắt lát mỏng; âm là lỗi). `deleteTaskRefs` xoá thêm các ref lát.

## Tiêu chí chấp nhận
- [ ] AC-1: Chia lát: kế hoạch lớn của b-do chạy bằng bint run (driver process), có từ hai nhóm Bề mặt bị ảnh hưởng gắn AC-N và đã có kiểm tra mục tiêu đóng băng, sau khi được cho chạy b-feat thì Runner ghi State.Slices theo thứ tự nhóm (lát 1 = nhóm đầu) và chạy mỗi lát bằng một bước b-feat riêng có đề bài nêu đúng nhóm của lát và timeout bằng min(workflow.timeout_minutes, workflow.slice_timeout_minutes); lý do chặn của cổng cỡ kế hoạch liệt kê các lát; kế hoạch nhỏ, dưới hai nhóm, đường nhanh, driver session hoặc slice_timeout_minutes = 0 vẫn chạy một b-feat như cũ
- [ ] AC-2: Chốt: sau mỗi lát Runner chốt lát khi kiểm chứng không có test đỏ ngoài các test của kiểm tra mục tiêu và tập ô đỏ của cổng mục tiêu (các dòng --- FAIL lá trong log goal-N.log) là tập con thực sự của tập ở điểm xanh trước (điểm xanh không đo được như biên dịch hỏng thì chỉ cần đo được; điểm xanh đã 0 ô đỏ thì phải giữ 0); lát chốt được ghim ảnh chụp cây ở refs/bint/tasks/ID/slice-green và ghi tập ô đỏ vào State.Slices
- [ ] AC-3: Bỏ lát: lát không đạt điều kiện chốt hoặc quá trần thời gian lát thì Runner ghim cây hiện tại ở refs/bint/tasks/ID/slice-N-discarded, đưa tệp của tác vụ về điểm xanh (tệp tạo sau điểm xanh bị xoá, tệp không thuộc tác vụ giữ nguyên), đánh dấu lát discarded kèm số đo làm lý do rồi chạy lát kế; lát 1 bị bỏ thì tác vụ blocked; hết lát thì sang b-review như cũ
- [ ] AC-4: Giao điểm xanh: hết ngân sách limits.workflows của b-do hoặc agent lỗi trong một lát thì lát dở được đưa về điểm xanh cuối, tác vụ blocked, Result.Slices và lý do nêu lát đã chốt, lát đã bỏ (lý do, ref sao lưu) và lát chưa làm; LastGreen và bint task restore-green dùng điểm xanh của lát; resume chạy tiếp các lát chưa làm
- [ ] AC-5: Cấu hình và tài liệu: khoá workflow.slice_timeout_minutes (mặc định 30, 0 tắt lát mỏng) có trong struct, Default(), Validate() và mẫu Render; docs/workflows.md mô tả lát mỏng, điều kiện chốt và cách lấy lại lát bị bỏ
- [ ] AC-6: kiểm tra mục tiêu TestSliceRatchet xanh

## Kiểm tra mục tiêu
Một test hộp đen ở gói `internal/workflow`, dùng harness agent giả sẵn có của gói (như `newHarness`/`goalBox` trong `goalfreeze_test.go`),
mỗi nhóm bề mặt một subtest. Lệnh kiểm chứng và cổng mục tiêu dùng script giả đọc tệp trong repo tạm và in dòng kiểu `go test`
(`--- FAIL: TestGoal/c1`, `--- FAIL: TestOld`), thoát 1 khi có dòng FAIL.

- **Chuẩn bị**:
  - b-req giả ghi plan **lớn** có 3 nhóm `## Bề mặt bị ảnh hưởng` gắn AC (L1, L2, L3) và một dòng "Ngoài phạm vi" không gắn AC; requirement có
    `## Kiểm tra mục tiêu`. b-goal giả ghi tệp kiểm tra có 3 ô (`TestGoal/c1..c3`), ô `ck` đỏ cho tới khi tệp `ck.txt` có nội dung đúng;
    `TestOld` đỏ khi tệp `old.txt` bị sửa.
  - b-feat giả theo lát (đọc "lát k/N" trong đề bài): L1 làm `c1` xanh; L2 làm `c2` xanh nhưng sửa `old.txt` (test cũ đỏ) và tạo tệp mới
    `l2-extra.txt`; L3 làm `c3` xanh.
  - Đối chứng: (a) cùng plan nhưng `## Cỡ kế hoạch` nhỏ ≤ 5 tệp/nhóm; (b) `workflow.slice_timeout_minutes: 0`; (c) plan lớn có 1 nhóm AC;
    (d) agent L2 trả `TimedOut`; (e) `limits.workflows.b-do.max_cost_usd` chỉ đủ cho b-req, b-goal, L1; (f) L1 không làm giảm ô đỏ.
- **Hành động**: `Run` b-do → blocked ở cổng cỡ kế hoạch → resume (như người) → chạy tới khi dừng/xong; ca (e) resume lần nữa sau khi nâng ngân sách.
- **Quét/đo**: chuỗi bước đã gọi và đề bài từng b-feat; `AgentRequest.Timeout` của từng b-feat; lý do chặn của cổng cỡ kế hoạch;
  `State.Slices` (thứ tự, trạng thái, lý do, tập ô đỏ); ref `slice-green` (tree khớp cây sau L1/L3) và `slice-2-discarded` (chứa `old.txt` đã
  sửa và `l2-extra.txt`); nội dung `old.txt`, sự tồn tại của `l2-extra.txt`, tệp ngoài tác vụ do test tạo giữa chừng; `Result.Slices`,
  `Result.Reason`, `Result.LastGreen.Tree`; `RestoreGreen(…, false)` sau khi dừng.
- **Điều kiện đạt**:
  - Chia lát: lý do chặn nêu L1, L2, L3; sau resume có đúng 3 bước b-feat theo thứ tự, đề bài lát k chứa nguyên văn dòng nhóm k và không coi
    dòng "Ngoài phạm vi" là lát; Timeout = min(timeout_minutes, slice_timeout_minutes). Đối chứng (a), (b), (c): đúng một b-feat như cũ,
    `State.Slices` rỗng.
  - Chốt: L1 và L3 `accepted`, tập ô đỏ lần lượt {c2,c3} và {c2}; `slice-green` trỏ cây sau L3.
  - Bỏ lát: L2 `discarded`, lý do nêu `TestOld`; `old.txt` về nội dung gốc, `l2-extra.txt` không còn, tệp ngoài tác vụ còn nguyên; L3 vẫn chạy;
    sau L3 có bước b-review. Ca (d): L2 `discarded` lý do quá trần, L3 vẫn chạy. Ca (f): L1 `discarded`, tác vụ blocked, không có b-feat thứ hai.
  - Giao điểm xanh: ca (e) tác vụ blocked vì ngân sách, `Result.Slices` có L1 `accepted`, L2/L3 chưa làm, lý do liệt kê chúng; `LastGreen.Tree`
    = tree của `slice-green`; `RestoreGreen` xem trước không có tệp cần khôi phục; resume sau khi nâng ngân sách chạy b-feat cho L2 (không chạy lại L1).
  Mỗi điều kiện sai báo riêng tên subtest và giá trị đo được.
- **Tên test và tệp dự kiến**: `TestSliceRatchet` trong `internal/workflow/slice_goal_test.go` (subtest: `split`, `ratchet`, `discard`,
  `deliver`, `controls`); nằm trong `go test ./...` của `verify.command`. Khoá cấu hình kiểm thêm bằng `TestRenderMatchesDefaults` sẵn có.

## Ngoài phạm vi
- Driver session (`bint run next`): lát mỏng chỉ chạy dưới `bint run`. Hậu quả: phiên Claude điều phối bằng `bint run next` vẫn chạy kế hoạch
  lớn bằng một b-feat; tác vụ đã chia lát mà resume bằng driver session thì phần còn lại chạy một b-feat (Bint ghi chú).
- Chốt một chiều cho vòng sửa sau review: vòng sửa vẫn theo luật cũ (ngân sách, `reviewStalled`). Hậu quả: một vòng sửa có thể làm cây kém
  hơn điểm xanh lát; `LastGreen` trong kết quả và `bint task restore-green` (người) vẫn đưa về được.
- Hiển thị lát trong TUI và `bint task show`: xem qua JSON kết quả lượt (`slices`), ghi chú lượt và `state.json`.
- Tự sinh lát khi plan không có nhóm bề mặt gắn AC: không chia, chạy một b-feat.
- Chạy lát song song.

## Câu hỏi mở
Không có.
