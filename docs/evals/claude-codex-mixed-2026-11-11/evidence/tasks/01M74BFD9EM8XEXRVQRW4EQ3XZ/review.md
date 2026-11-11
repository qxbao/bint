# Review tác vụ 01M74BFD9EM8XEXRVQRW4EQ3XZ

## Vòng 1 — 2026-11-11T03:50:20Z

Phần (1) Sub/TestSub đúng, gọn và nằm trong scope; phần (2) migrations/002_calc_log.sql chưa được tạo nên tiêu chí 2 chưa đạt, cần người quyết định vì đường dẫn được bảo vệ và nằm ngoài scope.allow.

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.1] risk/medium — Tiêu chí 2 chưa đạt: migrations/002_calc_log.sql không tồn tại

- Vị trí: `migrations/002_calc_log.sql`
- Bằng chứng: git status --short chỉ có calc.go, calc_test.go; ls migrations báo 'No such file or directory'. Goal yêu cầu tạo tệp này nhưng scope.allow chỉ gồm calc.go, calc_test.go và migrations/** là đường dẫn được bảo vệ (autonomy ask_on_protected). Agent dừng lại là đúng chính sách, nhưng phiếu tự mâu thuẫn giữa goal và scope.

Người duyệt cần quyết định: mở rộng scope.allow thêm migrations/002_calc_log.sql và cho phép ghi đường dẫn được bảo vệ, hoặc bỏ tiêu chí 2 khỏi phiếu. Không nên coi tác vụ là done khi tiêu chí 2 còn ✗.

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.2] suggestion/low — TestSub chỉ có một trường hợp

- Vị trí: `calc_test.go:17-21`
- Bằng chứng: TestSub chỉ kiểm Sub(5,3)==2; không có trường hợp kết quả âm (Sub(3,5)), số 0 hay đối số âm, nên không phát hiện được lỗi đảo thứ tự toán hạng kiểu b - a nếu chỉ dựa vào đúng một ca dương... thực tế Sub(5,3) với b-a cho -2 nên ca này vẫn bắt được đảo toán hạng, nhưng chưa phủ biên.

Cân nhắc chuyển sang bảng kiểm thử gồm (3,5)->-2, (0,0)->0, (-2,-3)->1.

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.3] suggestion/low — Chú thích Sub không nêu hành vi tràn số

- Vị trí: `calc.go:7-8`
- Bằng chứng: Sub(math.MinInt, 1) tràn và quay vòng thành math.MaxInt; Abs ở dòng 13 đã ghi rõ hành vi biên với math.MinInt, Sub thì không.

Thêm một câu vào doc comment, ví dụ: 'Tràn số quay vòng theo ngữ nghĩa int của Go.' để nhất quán với Abs.

## Vòng 2 — 2026-11-11T03:56:48Z

Sub/TestSub đúng và gọn; migrations/002_calc_log.sql đã tồn tại với đúng một dòng '-- calc log' nên cả hai tiêu chí đạt về nội dung, nhưng bằng chứng tiêu chí 2 trong phiếu còn cũ và tệp nằm ngoài scope.allow (đã có người duyệt).

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.4] risk/low — Bằng chứng tiêu chí 2 trong phiếu đã lỗi thời

- Vị trí: `migrations/002_calc_log.sql:1`
- Bằng chứng: bint task show vẫn ghi '✗ 2. ... migrations/002_calc_log.sql chưa tạo', trong khi git status có '?? migrations/' và tệp chứa đúng '-- calc log' + LF (12 byte, 1 dòng). Mục 'Chưa thể done: tiêu chí 2 chưa đạt' có thể chặn hoàn thành dù tiêu chí thực tế đã đạt.

Cập nhật bằng chứng tiêu chí 2 (tệp tồn tại, nội dung đúng một dòng) trước khi Bint Core quyết định done.

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.5] risk/low — Tệp migrations nằm ngoài scope.allow của phiếu

- Vị trí: `migrations/002_calc_log.sql:1`
- Bằng chứng: scope.allow chỉ gồm calc.go, calc_test.go; migrations/** là đường dẫn được bảo vệ. Phiếu ghi 'Đã duyệt bởi probe: migrations/002_calc_log.sql' nên thay đổi được người duyệt, nhưng scope trong task.yml chưa phản ánh điều đó.

Không cần sửa mã; nếu muốn hồ sơ nhất quán, người phụ trách có thể bổ sung tệp này vào scope.allow.

### [01M74BFD9EM8XEXRVQRW4EQ3XZ.6] suggestion/low — TestSub chỉ có một trường hợp dương

- Vị trí: `calc_test.go:17-21`
- Bằng chứng: TestSub chỉ kiểm Sub(5,3)==2; không có ca kết quả âm (3-5) hay số âm (-2-(-3)).

Chuyển sang table-driven test với vài ca biên, tương tự các test Add hiện có.
