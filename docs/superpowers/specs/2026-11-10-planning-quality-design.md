# Chất lượng bước lập kế hoạch của Bint — thiết kế

Ngày: 2026-11-10. Trạng thái: đề xuất để triển khai sau khi duyệt plan.

## Mục tiêu

Giữ Bint là hệ thống skill + Core hiện có. Khi một tác vụ đi qua `b-req`, Core phải biết ba tài liệu đã đủ và các liên kết cơ bản giữa tiêu chí chấp nhận và đầu việc không bị đứt. Với tác vụ cần suy xét kỹ, cùng skill `b-req` phải làm rõ giả định và rủi ro trước khi `b-feat` sửa mã. Việc nhỏ vẫn có thể bỏ `b-req`.

## Hiện trạng đã kiểm tra

- `Classify` chỉ quyết định `Plan` bằng từ khóa, độ dài và số dòng; `--plan`/`--no-plan` ghi đè. `Assess.Risk=high` hiện chỉ do phạm vi chạm `limits.protected_paths` (`internal/task/ticket.go`). Hai quyết định này có ý nghĩa khác nhau.
- `b-req` nhúng trong binary xuất `requirement.md`, `plan.md`, `tasks.md`; `bint task write` từ chối nội dung rỗng lúc ghi, nhưng không kiểm nội dung sau đó. `afterStep` của `bint run` chỉ thất bại nếu thiếu cả ba; thiếu một hoặc hai tệp được ghi chú rồi đi tiếp (`internal/workflow/run.go`).
- `b-feat` đọc ba tệp **nếu có**. Chuyển sang `done` kiểm bằng chứng kiểm thử/tiêu chí và fingerprint mã, chưa kiểm tài liệu lập kế hoạch (`internal/skills/core/b-feat/SKILL.md`, `internal/task/status.go`).
- Tài liệu tác vụ nằm ngoài repo trong `<state>/runtime/tasks/`; skill gốc có overlay theo điểm neo. Các thay đổi phải giữ mô hình này (`docs/bint-plan.md`, mục 6.1, 6.6–6.7 và 20).
- Gate đánh giá P1–P3 chưa được nghiệm thu: pilot Codex hiện có một repository và hai tác vụ validation, thiếu số đo USD cần cho các ngưỡng (`docs/gate-1.md`, `docs/gate-2.md`). Hai task này cần kiểm thử xác định được trước, rồi pilot riêng; không suy ra lợi ích chất lượng/chi phí từ việc thêm quy trình.
- Test dùng agent giả với tài liệu rất ngắn; các fixture đó cần đổi khi hợp đồng mới có hiệu lực. Baseline: lượt khảo sát đầu không chạy được test (Go 1.23.4 so với `go.mod` 1.25.1). Ngày 2026-11-11, với Go 1.25.1, `go test -count=1 ./internal/task ./internal/workflow ./internal/cli ./internal/skills` đều qua trước khi triển khai.

## Quyết định chung

1. Không thêm workflow engine, DAG, agent role hay dependency mới. Bint Core tiếp tục điều phối `b-req → b-feat → b-review`.
2. Không bắt người dùng viết yêu cầu có cấu trúc. Khuôn mới là **đầu ra của `b-req`**, không phải định dạng đầu vào.
3. Phân biệt **lỗi kiểm được bằng máy** (thiếu/rỗng tệp, ID trùng, tham chiếu sai, câu hỏi còn mở) với **đánh giá nội dung** (giải pháp có đúng, tiêu chí có đủ). Core chỉ chặn nhóm đầu; `b-review` và người dùng đánh giá nhóm sau.
4. Tác vụ cũ, có `state.json` thiếu trường hợp đồng mới, giữ hành vi hiện tại khi resume hoặc chạy `b-feat`; `bint task lint --strict` cho phép kiểm thủ công. Tác vụ mới có chọn `b-req` mang `planning_contract_version: 1`; tác vụ bỏ `b-req` không cần ba tài liệu.
5. `bint task lint` và ranh giới `b-req`/`b-feat` dùng cùng một bộ kiểm; kết quả có mã lỗi ổn định, đường dẫn artifact và thông điệp sửa được. Không tự sửa tài liệu và không tự tăng số lần gọi agent khi lint thất bại.

## Hợp đồng artifact v1

`b-req` tạo ba tệp trong thư mục tác vụ hiện tại:

- `requirement.md`: ít nhất một dòng tiêu chí dạng `- [ ] AC-1: <nội dung>` dưới mục `## Tiêu chí chấp nhận`. Mỗi `AC-N` tương ứng đúng tiêu chí thứ N trong `task.yml` sau khi chuẩn hóa khoảng trắng. Skill dùng `bint task set --criteria` để lưu danh sách này.
- `plan.md`: nội dung khác rỗng, nêu hướng triển khai, khu vực mã, rủi ro và cách kiểm thử. Core không chấm chất lượng các đoạn văn này.
- `tasks.md`: ít nhất một dòng dạng `- [ ] TSK-1 [AC-1]: <việc> — xong khi: <điều kiện>`. ID `TSK-N` là duy nhất; mọi tham chiếu `AC-N` tồn tại trong `requirement.md`; mỗi `AC-N` được ít nhất một task bao phủ.

Quy tắc cứng của v1: đủ ba tệp đọc được và không rỗng; có ít nhất một `AC`, một `TSK`, một tiêu chí trong ticket; các ID không trùng; số thứ tự AC liên tục từ 1; nội dung AC trùng tiêu chí ticket sau chuẩn hóa khoảng trắng; mỗi TSK có điều kiện `xong khi:` và ít nhất một AC hợp lệ; mọi AC được TSK bao phủ. Khuôn thiếu hoặc còn placeholder trong chính dòng AC/TSK là lỗi. Số tiêu chí trong ticket phải bằng số `AC` (thừa hay thiếu đều là lỗi). Placeholder là nội dung dòng AC/TSK chứa `<...>`, `…`, `TODO` hoặc `TBD`. Section AC/Q kết thúc ở heading cấp 1–2 kế tiếp; dòng trong code fence bị bỏ qua. Các mục khác, kể cả cách diễn đạt và độ dài, chỉ là cảnh báo hoặc để reviewer xem.

Khi lint lỗi sau `b-req`, task chuyển `planned` rồi `blocked` (không `failed`) để `resume` về `planned` và không chạy lại agent; với workflow `b-req` chạy riêng, task ở lại `planned` và lệnh thoát khác 0. Cổng trước `b-feat` khóa theo `planning_contract_version` (gồm cả `in_progress` sau review, trừ khi task v1 còn `created` gọi thẳng `b-feat`). Mọi chuyển sang `planned` thủ công của task v1 đều phải lint. Chạy lại `b-req` trên task đã có tiêu chí phải dùng `--clear-criteria` vì `--criteria` chỉ nối thêm.

`bint task lint <id>` trả `errors`, `warnings`, `open_questions`; `--json` ổn định cho CLI/agent. Mã thoát 1 khi có `errors` hoặc `open_questions`, 0 khi chỉ có cảnh báo. `--strict` áp hợp đồng v1 lên task cũ để người dùng chủ động nâng cấp; không đổi state. `bint run` chặn chuyển khỏi `b-req` khi có lỗi cấu trúc; trước `b-feat` kiểm lại để bắt tài liệu bị sửa sau lúc lập kế hoạch. Với tác vụ cũ, preflight không tự chặn.

## Độ sâu phân tích

Giữ `Classification.Plan` cho câu hỏi **có cần `b-req` không**. Thêm `Classification.Depth` (`standard` hoặc `deep`) và `DepthReason` cho câu hỏi **nếu có, phân tích kỹ đến đâu**. Giá trị thiếu ở state cũ được hiểu là `standard`. Không gán `Assessment.Risk=high` chỉ vì mô tả dài.

Khi `depth=auto`, chọn `deep` nếu phạm vi chạm đường dẫn bảo vệ theo `Assess`, hoặc yêu cầu có ít nhất 5 dòng không trống / hơn 100 từ. Ngưỡng `deep` cố ý cao hơn ngưỡng `Plan` của `Classify` (3 dòng / 60 từ, giữ nguyên): mô tả 3–4 dòng hay 61–100 từ vẫn chạy `b-req` ở mức `standard`, tránh gần như mọi task có `b-req` thành `deep`. Số cụ thể chỉnh lại bằng dữ liệu pilot, không chỉnh âm thầm; nếu không, chọn `standard`. Lưu lý do cụ thể là `protected_paths` hoặc `complex_request`. Depth chỉ được chọn và lưu khi `Plan=true`. Depth chọn một lần lúc tạo task từ mô tả và phạm vi suy ra được lúc đó; nếu `b-req` phát hiện sau đó là chạm đường dẫn bảo vệ, depth không đổi và người dùng chọn lại bằng `--depth deep` trên task `created`/`planned`. Nếu `Assess.Risk=high`, auto cũng chọn `Plan=true`, trừ khi người dùng đã chỉ định `--no-plan`. `--depth standard|deep` là ghi đè của người dùng và hàm ý có `b-req`; `--no-plan` đi cùng depth tường minh là lỗi cú pháp. Depth tường minh chỉ dùng với `b-do`/`b-req`, không dùng với `b-feat`/`b-review`. `resume` dùng depth đã lưu, không tính lại theo mô tả đã đổi. Tác vụ cũ không có depth giữ `standard`; muốn chọn lại trước `b-feat` thì chạy lại `b-req` trên task đó.

`standard` tiếp tục dùng ba artifact v1. `deep` dùng **cùng skill**, thêm các mục giả định/mâu thuẫn, lựa chọn và lý do, ảnh hưởng, rủi ro/rollback, checklist tính nhất quán. Core đưa depth và lý do vào prompt; không sinh thêm artifact hay trạng thái workflow.

Câu hỏi thực sự cản trở triển khai được ghi trong `requirement.md` dưới `## Câu hỏi mở` dạng `- [ ] Q-1: <câu hỏi>`. Khi có dòng này, task ở `planned` nhưng `b-feat` bị chặn; người dùng hoặc agent ghi lại câu trả lời đã nhận theo dạng `- [x] Q-1: <câu hỏi> — Trả lời: <nội dung>` rồi chạy `bint task lint` và `bint run resume`. Core chỉ xác minh có câu trả lời được ghi, không tự xác nhận nội dung là đúng. Câu hỏi không cản trở có thể ghi trong văn xuôi và không bị máy chặn. Không thêm cổng duyệt của người cho mọi tác vụ.

## Tương thích và ranh giới

- Tác vụ `--no-plan` và lời gọi trực tiếp `b-feat` không cần artifact. `--plan` tiếp tục có nghĩa như trước; nó không đồng nghĩa `deep`.
- `bint run b-req --task` trên tác vụ cũ chủ động áp hợp đồng v1 cho **lượt lập kế hoạch mới**; dữ liệu cũ không bị sửa cho tới khi bước này chạy.
- Overlay `b-req` vẫn dùng các anchor hiện có. Overlay thay khuôn AC/TSK phải cập nhật cho hợp đồng v1; `bint sync` vẫn bảo vệ bản đã sửa tay.
- Không sao chép `@trace(...)` vào mã, không tự commit, không xuất tài liệu sang Git trong phạm vi hai task này.
- Đây là cải tiến workflow, không thay đổi ngưỡng hay kết luận của gate đánh giá P1–P3. Lợi ích về chất lượng/chi phí cần đo riêng sau triển khai.

## Điều kiện nghiệm thu chung

1. Agent giả bỏ sót một artifact hoặc tạo AC/TSK sai bị dừng **trước** `b-feat`, với lỗi chỉ rõ cách sửa.
2. Tác vụ v1 hợp lệ chạy hết `b-do`; sửa artifact sau `b-req` làm preflight báo sai lệch; sửa lại và resume không bỏ qua kiểm tra.
3. Task cũ và việc `--no-plan` giữ đường chạy cũ. `--depth deep` không đổi `Assessment.Risk` hay quyền duyệt.
4. Câu hỏi mở chặn `b-feat`; ghi câu trả lời rồi resume đi tiếp, không chạy lại `b-req` chỉ để vượt cổng.
5. Các kiểm tra Go, build đa nền tảng và test E2E được chạy bằng Go 1.25.1 trong môi trường có sẵn module cache. Không tuyên bố pass từ lượt khảo sát này.
