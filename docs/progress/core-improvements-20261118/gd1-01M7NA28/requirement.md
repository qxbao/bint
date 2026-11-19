# Yêu cầu

## Mục tiêu và phạm vi
Giai đoạn 1 của phiếu cha 01M7NA1PJHQJR6SE3BVCH05BQC. Benchmark (docs/evals/bench-bint-vs-plain-tong-hop.md mục 4, ý 3) cho thấy
test Kiểm tra mục tiêu do chính b-feat viết có cùng điểm mù với mã của nó. Mục tiêu: test mục tiêu do một phiên **độc lập** viết chỉ
từ `requirement.md`, được Bint **đóng băng** (b-feat không sửa được) và chạy như **cổng riêng** có log và kết quả riêng.

Phạm vi: workflow gốc `b-do` đầy đủ (có b-req), driver process và driver session. Đường nhanh (`--fast`) và phiếu không có mục
`## Kiểm tra mục tiêu` giữ hành vi cũ.

## Yêu cầu chức năng
1. Flow `b-do` có bước mới `b-goal` (kind mới `goal`) giữa `b-req` và `b-feat`. Bước chỉ chạy khi: không đường nhanh, `requirement.md`
   có mục `## Kiểm tra mục tiêu`, và chưa có tệp đóng băng. Chạy sau cổng cỡ kế hoạch, câu hỏi mở, xác nhận lệnh kiểm chứng.
2. `b-goal` luôn mở **phiên mới** (không nối tiếp phiên b-req, vì phiên đó đã thấy plan/tasks), dùng harness/model của role `planner`
   (mặc định khác model implementer). Đề bài chỉ gồm skill `b-goal`, phiếu (mục tiêu, yêu cầu gốc, lệnh kiểm chứng, phạm vi) và nguyên
   văn `requirement.md`; không chép, không nêu đường dẫn `plan.md`/`tasks.md`.
3. `b-goal` không làm `b-feat` mất nối tiếp phiên `b-req` (State.LastPlan giữ nguyên, không đặt LastStep).
4. Sau `b-goal`, Bint tự xác định tệp bước đó tạo/sửa (không tin khai báo của agent), ghi `{đường dẫn, sha256}` vào trạng thái phiếu,
   nối đường dẫn vào `scope.deny`. Không tệp nào → blocked. Phạm vi của tệp (protected/ngoài allow) qua kiểm phạm vi như bước ghi khác.
5. Cổng đóng băng sau mỗi bước implement, chạy **trước** kiểm phạm vi, kiểm chứng và review: tệp đóng băng bị sửa/xoá/đổi tên → blocked,
   nêu tệp và cách khôi phục; duyệt đường dẫn không bỏ qua cổng này. Tệp còn nguyên không bị kiểm phạm vi hỏi duyệt dù khớp `scope.deny`.
6. Cổng mục tiêu: lệnh là `workflow.verify.affected_command` mở rộng trên tệp đóng băng (khi đã đặt và `verify.command` đã xác nhận),
   không thì `verify.command`; cùng luật xác nhận lệnh như kiểm chứng. Chạy sau `b-goal` (mốc ban đầu, không chặn; xanh ngay thì ghi chú
   "test có thể quá yếu") và sau kiểm chứng của mỗi bước implement. Log `goal-N.log`, kết quả riêng trong `State`, hiện ở `bint task show`.
   Đỏ được xử lý như kiểm chứng đỏ (review thấy, vòng sửa); finalize chạy lại cổng khi cây đổi và không cho done khi lần mới nhất đỏ.
7. Khi đã có tệp đóng băng, đề bài `b-feat` (đầy đủ, nối tiếp b-req, vòng sửa) thay khối "viết test mục tiêu" (`goalCheckBlock`) bằng danh
   sách tệp đóng băng phải làm cho xanh và không được sửa; skill `b-feat` đổi tương ứng; đề bài `b-review` đối chiếu test đóng băng.
8. Đường nhanh, phiếu không có mục Kiểm tra mục tiêu, flow một bước `b-feat` và flow tuỳ biến: không chạy `b-goal`, đề bài như cũ.

## Tiêu chí chấp nhận
- [ ] AC-1: b-do đầy đủ (không --fast) có mục Kiểm tra mục tiêu: sau b-req và cổng cỡ kế hoạch Bint chạy bước b-goal trong phiên mới (không nối tiếp phiên b-req), bằng harness/model của role planner; đề bài b-goal chứa mục Kiểm tra mục tiêu và không chứa nội dung hay đường dẫn plan.md, tasks.md; b-feat vẫn nối tiếp phiên b-req như trước
- [ ] AC-2: Sau b-goal Bint ghi đường dẫn và sha256 của mọi tệp b-goal tạo hoặc sửa vào trạng thái phiếu và thêm chúng vào scope.deny; sau mỗi bước implement, diff sửa, xoá hoặc đổi tên tệp đóng băng làm tác vụ blocked trước kiểm chứng và review với lý do nêu tệp, bint task approve không bỏ qua được; tệp đóng băng còn nguyên không bị hỏi duyệt
- [ ] AC-3: Bint chạy cổng mục tiêu riêng (affected_command mở rộng trên tệp đóng băng khi có, không thì verify.command) sau b-goal và sau kiểm chứng của mỗi bước implement, ghi log goal-N.log và kết quả riêng trong trạng thái phiếu hiện ở bint task show; tác vụ không done khi lần cổng mục tiêu mới nhất trên cây cuối không qua
- [ ] AC-4: Đề bài b-feat (đầy đủ và nối tiếp) và skill b-feat không còn bảo b-feat viết test mục tiêu khi đã có tệp đóng băng mà nêu tệp đó không được sửa; --fast và phiếu không có mục Kiểm tra mục tiêu không chạy b-goal và giữ đề bài như trước
- [ ] AC-5: Kiểm tra mục tiêu TestGoalFreezeBlackBox xanh

## Kiểm tra mục tiêu
Mục tiêu người dùng: test mục tiêu do phiên độc lập viết từ requirement.md, b-feat không sửa được, chạy như cổng riêng; đường cũ không đổi.

- **Chuẩn bị**: repo tạm (testutil, git kín), phiếu b-do có `verify.command` đã xác nhận dạng `sh` và `affected_command` có `{test_files}`.
  Agent giả theo bước: planner ghi requirement có `## Kiểm tra mục tiêu` (dấu `GOAL-SPEC-7731`) và plan/tasks chứa dấu `PLAN-ONLY-4410`;
  agent b-goal ghi tệp `goal/check.sh` (dấu `FROZEN-1`) và ghi lại đề bài nhận được; agent b-feat có ba biến thể: (a) chỉ sửa mã cho
  `goal/check.sh` xanh, (b) sửa nội dung `goal/check.sh`, (c) xoá `goal/check.sh`. Thêm hai phiếu đối chứng: `--fast`, và requirement
  không có mục Kiểm tra mục tiêu.
- **Hành động**: `Runner.Run` b-do tới khi dừng cho từng biến thể (driver process); một ca driver session qua `bint run next`.
- **Quét/đo**: đề bài b-goal (có `GOAL-SPEC-7731`, không có `PLAN-ONLY-4410`, không có `plan.md`/`tasks.md`), handle phiên b-goal khác
  b-req và b-feat nối tiếp b-req; harness/model b-goal = role planner; `State` có tệp đóng băng + sha256; `scope.deny` có
  `goal/check.sh`; trạng thái/lý do sau b-feat; tồn tại `goal-1.log`, `goal-2.log` và kết quả cổng mục tiêu riêng trong `State` và
  `bint task show`; danh sách bước đã chạy; đề bài b-feat (không có câu "viết thành test tự động", có `goal/check.sh`).
- **Điều kiện đạt**: (a) done, cổng mục tiêu mốc đỏ rồi xanh, b-feat nối tiếp b-req; (b) và (c) blocked trước kiểm chứng, lý do nêu
  `goal/check.sh`, `bint task approve` rồi resume vẫn blocked tới khi khôi phục tệp; ca `--fast` và ca thiếu mục: không có bước b-goal,
  không có `goal-*.log`, đề bài b-feat giống bản trước thay đổi. Mỗi điều kiện hỏng báo riêng (subtest theo biến thể).
- **Tên test và tệp dự kiến**: `TestGoalFreezeBlackBox` trong `internal/workflow/goalfreeze_test.go` (nằm trong `go test ./...` của
  `verify.command`).

## Ngoài phạm vi
- Chặn cứng việc phiên b-goal tự đọc plan.md/tasks.md trên đĩa (hook chặn đọc): giai đoạn này chỉ không đưa chúng vào đề bài. Hậu quả:
  agent b-goal cố tình đi tìm (vd `bint task show`) vẫn đọc được.
- Bước goal trong flow tuỳ biến (`.bint/flows`): validate từ chối kind `goal` ngoài b-do gốc.
- Chia lát b-feat, cổng hiệu năng, b-req đo hiện trạng: giai đoạn 2–4 của phiếu cha.

## Câu hỏi mở
Không có.
