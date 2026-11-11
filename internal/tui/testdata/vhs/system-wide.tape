# Màn Hệ thống (giai đoạn 5a): Cấu hình, Harness, Thành phần, Skill, Chỉ mục, MCP, Ngôn ngữ, Doctor. Chạy từ thư mục gốc repository:
#   vhs internal/tui/testdata/vhs/system-wide.tape
# Cần bint trong PATH. Xem README.md cùng thư mục. Kịch bản chỉ chụp các màn đọc và form (không ghi cấu hình thật).

Output internal/tui/testdata/vhs/out/system-wide.gif

Set Shell "bash"
Set FontSize 16
Set Width 1700
Set Height 900
Set Padding 12
Set TypingSpeed 40ms

Hide
Type "export BINT_STATE_DIR=$(mktemp -d) BINT_CONFIG_HOME=$(mktemp -d) BINT_LANG=vi"
Enter
Type "cd $(mktemp -d) && git init -q && git config user.name Demo && git config user.email demo@example.com"
Enter
Type "printf 'module demo\n' > go.mod && printf 'package demo\n' > a.go && git add -A && git commit -qm init && bint init --no-hooks >/dev/null"
Enter
Type "clear && stty size"
Enter
Show

Type "bint"
Enter
Sleep 2s
Type "7"
Sleep 1500ms
Screenshot internal/tui/testdata/vhs/out/system-wide-config.png
Type "jjj"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/system-wide-config-group.png

# Harness: bảng harness, bảng khả năng, trạng thái hook/MCP
Type "."
Sleep 1500ms
Screenshot internal/tui/testdata/vhs/out/system-wide-harness.png

# Thành phần quy trình: form thêm từ mẫu
Type "."
Sleep 800ms
Screenshot internal/tui/testdata/vhs/out/system-wide-components.png
Type "n"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/system-wide-component-add.png
Escape

# Skill: danh sách, nội dung, xem trước sync
Type "."
Sleep 800ms
Screenshot internal/tui/testdata/vhs/out/system-wide-skills.png
Type "s"
Sleep 800ms
Screenshot internal/tui/testdata/vhs/out/system-wide-sync.png
Escape

# Chỉ mục, MCP, Ngôn ngữ, Doctor
Type "."
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/system-wide-index.png
Type "."
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/system-wide-mcp.png
Type "."
Sleep 800ms
Screenshot internal/tui/testdata/vhs/out/system-wide-language.png
Type "."
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/system-wide-doctor.png

Type "q"
Sleep 500ms
