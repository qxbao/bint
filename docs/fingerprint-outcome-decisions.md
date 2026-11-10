# Quyết định vận hành: fingerprint và tín hiệu kết quả

Ngày: 2026-11-10. Phạm vi: các đợt đo Bint P1/gate 1/gate 2 trên repository thật.

Đây là quyết định về cấu hình và cách diễn giải bằng chứng, theo yêu cầu của chủ dự án. Không thay thế các ngưỡng đã chốt trong `gate-1.md`, `gate-2.md`; không tuyên bố phần chưa triển khai đã hoàn thành.

## D-FP-01 — Chấp nhận phương án phân tích theo năng lực, dùng vùng dòng cho pilot TypeScript

### Căn cứ

- Kế hoạch mục 8.4/P3 đề xuất tree-sitter theo ký hiệu, rồi vùng dòng, cuối cùng cả tệp.
- Bản đang đo dùng `go/parser` cho Go và heuristic cho các ngôn ngữ khác; `symbols.Region.Exact` phân biệt kết quả phân tích chính xác với heuristic.
- `FingerprintWith` có thể chọn `symbol`, `hunk`, `file`; fingerprint thực lưu là `symbol:`, `lines:` hoặc `file:`. `hunk` chỉ tạo fingerprint vùng dòng khi bằng chứng có dòng; thiếu dòng thì lùi về cả tệp.
- Chưa có bằng chứng trong đợt này để kết luận heuristic TypeScript xử lý đầy đủ overload, tên phương thức trùng, template literal và cấu trúc cú pháp phức tạp.

### Quyết định

1. Giữ mục tiêu biên dịch Go không phụ thuộc CGO. Go có thể tiếp tục dùng `go/parser` cho ký hiệu; không gắn nhãn phương án này là tree-sitter.
2. Pilot Bflow dùng `context.freshness.fingerprint: hunk` trong cấu hình cục bộ của bản sao thí nghiệm. Khi tạo tri thức cho đợt đo, bằng chứng phải có `path:a-b` rõ ràng để thật sự có mức `lines:`. Kiểm tra mức thực tế đã lưu, không suy ra từ tên cấu hình.
3. Ngoài Go, heuristic ký hiệu chỉ là khả năng hỗ trợ hạn chế, không được dùng làm bằng chứng đã đáp ứng trọn yêu cầu tree-sitter/P3. Không đổi mức/băm của tri thức cũ trong lúc đo; mỗi mục được kiểm tra theo fingerprint đã lưu.
4. Khi không xác định được vùng đáng tin cậy, dùng phạm vi tệp hoặc cảnh báo/chuyển stale; không tự tuyên bố mục còn đúng chỉ vì tìm thấy tên ký hiệu.
5. Quy tắc rủi ro cao (`rule`, `warning`) đã stale cần người duyệt lại; nhận định agent không tự thay thế phê duyệt. Các phép đo không được tự nâng candidate bootstrap thành quy tắc bắt buộc đã verified.

### Phạm vi áp dụng và điều kiện xem lại

Chỉ cấu hình bản sao thí nghiệm; không thay mặc định của mọi repository. Sai khác so với mục tree-sitter được ghi nhận là **deviation có giới hạn**, không phải hoàn thành nguyên văn P3. Muốn đóng sai khác phải đánh giá một parser thực cho ngôn ngữ mục tiêu, gồm các ca trùng tên/overload, cú pháp nhiều dòng, literal/chú thích, di chuyển vùng và thay đổi ngoài vùng. Phương án parser/WASM/Go thuần cần đánh giá riêng; quyết định này không khẳng định mọi lựa chọn tree-sitter đều bắt buộc CGO.

## D-SIG-01 — Hoãn quy công “người dùng sửa lại diff”, không tạo nhãn thành công từ tín hiệu thay thế

### Căn cứ

Hiện có các tín hiệu `tests`, `verify`, `rating`, `committed`, `reverted`, `criteria`, `review`. Chưa có nguồn đáng tin phân biệt người dùng sửa lại diff với agent khác, formatter hoặc tác vụ song song. Git commit/mtime một mình không chứng minh tác giả của chỉnh sửa trong phiên.

### Quyết định

1. Ghi tín hiệu “người dùng sửa lại diff của agent trong phiên” là **chưa triển khai/hoãn**, không suy diễn hoặc sinh sự kiện giả từ commit, thời gian sửa hay kết thúc phiên.
2. Giữ các thành phần tín hiệu và độ mạnh riêng. Agent tự đối chiếu tiêu chí/review là tín hiệu trung bình; commit là yếu. Các tín hiệu này không tự biến lượt chạy thành thành công.
3. Thiếu tín hiệu đủ mạnh thì giữ `unlabeled`. Kiểm thử chỉ có ý nghĩa khi lệnh chấm liên quan và thực sự chạy test; không dùng build thành công hoặc “0 tests” thay cho kiểm thử hành vi.
4. Chấm gate bằng test ẩn/fail→pass và báo cáo thống kê của tập đánh giá, không chấm bằng nhận xét tích cực của agent. Truy xuất và trích dẫn tri thức không đồng nghĩa đã chứng minh tác động nhân quả.
5. Chi phí/token/số lượt mà harness không báo phải để thiếu (`null`), không ghi 0. Codex qua thuê bao không báo USD không đồng nghĩa “chi phí mô hình đo được bằng 0”, dù ngân sách trả thêm API của chủ dự án là 0 USD.
6. Phân biệt không có trích dẫn với việc không được phép ghi trích dẫn. Pilot Codex đã quan sát `context_for_task`/`context_for_paths` thành công nhưng `cite_knowledge` bị chặn: `MCP tool call requires approval, but approval policy is never`. Khi có lỗi này, số cited thấp/0 không đủ để kết luận agent không áp dụng tri thức; phải xem là giới hạn telemetry của cấu hình đợt đo. Không đổi hint của công cụ ghi thành chỉ đọc hoặc tự bỏ chặn để làm đẹp số liệu.

### Điều kiện đóng phần còn thiếu

Cần nguồn sự kiện có danh tính/tác vụ/phiên, revision trước-sau, phạm vi tệp và bằng chứng phân biệt người dùng với các tác nhân khác; bổ sung kiểm thử nhiều worktree, formatter, agent song song và tín hiệu đến muộn. Nếu không chứng minh được thì không thu tín hiệu này.

Phần còn thiếu không ngăn một pilot được chấm bằng kiểm thử độc lập, nhưng **P3 chưa đáp ứng đầy đủ danh sách tín hiệu ở mục 9.6**. Không lấy quyết định hoãn để tự đổi checklist kế hoạch thành hoàn thành.

## Liên hệ với hai gate

- Các quyết định trên không hạ cỡ mẫu, số repository, số lần lặp, yêu cầu strict, điều kiện mở gate 2 hoặc ngưỡng hiệu quả.
- Với Codex hiện tại, gate 1 thiếu chỉ số USD/`num_turns` của Bint; gate 2 thiếu dữ liệu cho ràng buộc chi phí và nhánh Codex chỉ có MCP + AGENTS.md, không có path hook trong đợt đánh giá.
- Có thể chạy pilot đo tỷ lệ đạt/thời gian/token, nhưng không tự thay chúng vào chỉ số gate đã chọn. Muốn thay thước đo phải chốt một giao thức mới trước đợt đo tiếp theo; kết quả đã nhìn thấy chỉ dùng để thăm dò/ước tính, không để hợp thức hoá ngưỡng mới hồi tố.

## Bằng chứng của pilot Bflow

- Source được chốt tại `9d071abe858606b526cffcb087a233763df0a956`, Bint tại `b5896a4c41737f5eea953f0018c400b432d30eaa`.
- Hai snapshot strict tại các commit cha `5cbab0535230…` và `067dbfd57973…`, mỗi snapshot 4 mục; cả 8 mục đều giữ trạng thái candidate.
- Kiểm tra dữ liệu thật đã lưu: 21 bằng chứng đều có fingerprint `lines:`, không có mục tự được nâng verified.
- `go test -count=1 ./internal/symbols ./internal/knowledge ./internal/runlog` qua trên source Bint nói trên. Đây là xác minh cục bộ cho hành vi đang được viện dẫn, không phải chứng minh parser TypeScript chính xác.
- Protocol, kiểm chứng tác vụ và báo cáo lượt chạy được lưu trong `docs/evals/bflow-codex-2026-11-10/`.
