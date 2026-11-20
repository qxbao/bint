# Review tác vụ 01M7WP81F0XSXRJXQVQR9KCMZE

## Vòng 1 — 2026-11-20T18:00:28Z

Cổng kiểm tra mục tiêu đã qua theo Bint. Cổng xác minh thất bại vì bản xuất skill và core.lock lệch nguồn, tái hiện bằng lần chạy hẹp. Đối chiếu cả năm tiêu chí còn thấy đường ghi bí mật trong log, tệp tạm, trace, chỉ mục và backup, cùng lỗi bảo toàn xác thực profile và hiệu lực phê duyệt khi dọn dữ liệu cũ. Các phát hiện về đường ghi dựa trên mã nguồn.

### [01M7WP81F0XSXRJXQVQR9KCMZE.1] bug/high — Cổng xác minh thất bại vì bản xuất skill và core.lock lệch nguồn

- Vị trí: `internal/cli/bdo_lean_goal_test.go:704-731`
- Bằng chứng: verify-3.log dòng 4–23 và lần chạy hẹp đều báo hai kiểm thử đỏ. Bản b-feat, b-req và b-do trong .agents/.claude khác bản sinh từ internal/skills/core. Băm core và biến thể agents trong .bint/skills/core.lock cũng lệch. Các tệp skill và lock nằm ngoài diff của tác vụ.
- Repro: `go test -timeout 10m -run "^(TestGoalBDoLeanSlicesAndGates|TestGoalBReqBaselineAndSkillGates)$/^(skills_package_tests_synced|skill_published_copies)$" ./internal/cli`
- Tiêu chí: 5

Đồng bộ các bản xuất và core.lock với nguồn đang dùng để chạy xác minh. Giữ nguyên phép so từng byte và băm trong kiểm thử.

### [01M7WP81F0XSXRJXQVQR9KCMZE.2] bug/high — Log kiểm chứng và eval ghi nguyên mật khẩu từ đối số curl --user hoặc -u

- Vị trí: `internal/eval/shell.go:72-93`
- Bằng chứng: RunShell giao toàn bộ lệnh cho processRedactor. CredentialFlag ở internal/redact/redact.go:135-141 chỉ nhận cờ dài có tên thuộc danh sách bí mật, danh sách ấy thiếu user. Dạng curl -u với giá trị tên người dùng và mật khẩu phân cách bằng dấu hai chấm cũng không khớp quy tắc URL có userinfo. Dòng 81 ghi toàn bộ đối số vào log ngay trước khi khởi chạy, kể cả khi lệnh không kết nối được. Nếu tiến trình in lại mật khẩu riêng, bộ lọc stdout/stderr cũng chưa biết giá trị đó.
- Repro: internal/eval/shell.go:81
- Tiêu chí: 1

Nhận diện dạng xác thực Basic qua đối số curl và đăng ký giá trị mật khẩu với bộ che trước khi ghi cả dòng lệnh lẫn đầu ra. Kiểm bằng dữ liệu giả và tiến trình cục bộ.

### [01M7WP81F0XSXRJXQVQR9KCMZE.3] bug/high — Trình sửa cấu hình tạo bản sao tạm nguyên văn của lệnh kiểm chứng

- Vị trí: `internal/app/config_svc.go:141-161`
- Bằng chứng: Hành động system.config_edit gọi PrepareConfigEdit ở internal/tui/actions/system.go:167. Với lớp cấu hình hiện có chứa mật khẩu trực tiếp trong eval.test_command, PrepareConfigEdit gán nguyên byte từ tệp nguồn cho Seed ở dòng 146 rồi ghi Seed vào bint-config-*.yml trong thư mục tạm. Lối ghi này nằm ngoài diff và bỏ qua WriteDiagnostic. Nếu tiến trình bị dừng khi đang sửa, bản sao tạm vẫn có thể còn trên đĩa.
- Repro: internal/app/config_svc.go:161
- Tiêu chí: 1

Xử lý bản nháp và luồng rebase để bản sao tạm không giữ bí mật của lệnh, đồng thời giữ nguyên ngữ nghĩa cấu hình và các tham chiếu xác thực khi lưu.

### [01M7WP81F0XSXRJXQVQR9KCMZE.4] bug/high — Trace hook giữ nguyên bí mật khi giá trị lặp lại trong trường mô tả

- Vị trí: `internal/app/hook_trace.go:68-85`
- Bằng chứng: Với JSON đầu vào hợp lệ có tool_input.password là một mật khẩu không có tiền tố nhận diện và tool_input.note lặp lại đúng giá trị đó, beginHookTrace chỉ tạo bộ che từ môi trường rồi gọi Data. Nhánh JSON ở internal/redact/redact.go:216-225 và phép duyệt ở dòng 303-310 che trường password theo tên nhưng không thu giá trị vào bộ che của bản ghi. Trường note vẫn nguyên văn trong tệp .in, kể cả nếu hook từ chối sự kiện sau đó.
- Repro: internal/app/hook_trace.go:85
- Tiêu chí: 2

Thu các giá trị được nhận diện là bí mật trong bản ghi trước khi lọc các trường khác. Thêm ca trace có một bản sao không nhãn của cùng giá trị.

### [01M7WP81F0XSXRJXQVQR9KCMZE.5] bug/high — Chỉ mục tri thức lưu nguyên token trong tags và domains

- Vị trí: `internal/kindex/kindex.go:150-165`
- Bằng chứng: Mục tri thức trong cây có thể chứa token dạng GitHub PAT trong Scope.Tags và vẫn hợp lệ. Kindex lọc Title và Body nhưng đưa jsonList(it.Scope.Tags) thẳng vào knowledge_index ở dòng 157 và nối tags/domains nguyên văn vào knowledge_fts ở dòng 163. OpenWorkspace chỉ dọn bản tri thức trong cây ở phiên người dùng, nên phiên agent có thể đọc mục chưa dọn và chèn lại token. Dọn SQLite ở internal/store/redaction.go:23 chỉ chạm cột title của knowledge_index.
- Repro: internal/kindex/kindex.go:157
- Tiêu chí: 3

Che bí mật trong các trường dẫn xuất trước khi ghi cả hai bảng, và đưa cột còn thiếu vào đợt dọn dữ liệu cũ. Kiểm bằng mục trong cây có token giả trong tag.

### [01M7WP81F0XSXRJXQVQR9KCMZE.6] bug/high — Đồng bộ profile làm hỏng xác thực MCP qua đối số header

- Vị trí: `internal/iso/config_redaction.go:34-55`
- Bằng chứng: Profile Claude có MCP stdio dùng mcp-remote với cờ --header rồi đến chuỗi Authorization Bearer. App đưa nguyên mục mcpServers từ internal/app/iso_sync.go:387-394 vào Sync. Sync gọi cleanMCPEntry ở internal/iso/sync.go:268-270 trước khi ghi cấu hình đang dùng. authArgument chỉ giữ các cờ nhận diện theo tên credential, nên chuỗi header kế tiếp bị che ở dòng 42 và chương trình con nhận sai thông tin đăng nhập. Cùng hàm được dùng khi nhập cấu hình profile.
- Repro: internal/iso/config_redaction.go:42
- Tiêu chí: 4

Giữ tham số header thực sự dùng để xác thực trong cấu hình thực thi. Che cùng giá trị khi ghi bản sao chẩn đoán hoặc backup.

### [01M7WP81F0XSXRJXQVQR9KCMZE.7] bug/high — Backup cài hook của iso sync bỏ qua bí mật đã biết trong profile

- Vị trí: `internal/app/iso_sync.go:340-352`
- Bằng chứng: Khi profile đã hoàn tất bước dọn một lần, đặt một bản sao không nhãn của access token trong trường chẩn đoán của settings.json và yêu cầu sync cài hoặc cập nhật hook Claude. Callback ở dòng 347 đi qua EditHooks, gọi fsafe.Backup ở dòng 216. Backup chỉ lấy bí mật từ môi trường tiến trình (internal/fsafe/fsafe.go:63-64), nên không biết access token đọc được từ .claude/.credentials.json. Bản sao settings.json trong sync-backups vẫn chứa token. Đường lưu JSON/TOML thường của iso sync đã nhận bộ che theo profile.
- Repro: internal/harness/claude/install.go:216
- Tiêu chí: 4

Truyền bộ che theo profile đến callback sao lưu khi cài và gỡ hook. Kiểm bằng token giả chỉ có trong tệp xác thực, với bản sao không nhãn trong settings.json.

### [01M7WP81F0XSXRJXQVQR9KCMZE.8] bug/high — Dọn dữ liệu cũ thay lệnh kiểm chứng nhưng vẫn giữ hiệu lực phê duyệt

- Vị trí: `internal/workflow/ops.go:706-715`
- Bằng chứng: Với phiếu cũ có verify.command dùng cờ --token với một giá trị trích dẫn, state.json chứa cùng chuỗi trong verify_approval.command. OpenWorkspace quét cả runtime/tasks (internal/app/workspace.go:36-38), thay giá trị trong YAML và JSON bằng cùng ký hiệu che. VerifyConfirmed ở dòng 715 so hai chuỗi đã đổi và nhận chúng là lệnh đã xác nhận. verify sau đó chuyển lệnh đã đổi cho RunShell ở dòng 835, làm lệnh xác thực bằng giá trị thay thế.
- Repro: internal/workflow/ops.go:835
- Tiêu chí: 5

Dọn lịch sử theo cấu trúc. Khi lệnh thực thi cũ chứa bí mật, làm mất hiệu lực xác nhận cũ và yêu cầu lệnh tham chiếu biến xác thực. Kiểm để lệnh đã bị thay không được xem là lệnh đã duyệt.
