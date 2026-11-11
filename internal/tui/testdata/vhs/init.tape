# Màn tạm "Khởi tạo với mặc định" (repository chưa init). Chạy từ thư mục gốc: vhs internal/tui/testdata/vhs/init.tape

Output internal/tui/testdata/vhs/out/init.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 640
Set Padding 12
Set TypingSpeed 40ms

Hide
Type "export BINT_STATE_DIR=$(mktemp -d) BINT_CONFIG_HOME=$(mktemp -d) BINT_LANG=vi"
Enter
Type "cd $(mktemp -d) && git init -q && git config user.name Demo && git config user.email demo@example.com"
Enter
Type "printf 'module demo\n' > go.mod && git add -A && git commit -qm init && clear"
Enter
Show

# Repository chưa init: bint không đối số vào thẳng màn khởi tạo
Type "bint"
Enter
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/init-vi.png

# Tiếng Anh
Type "L"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/init-en.png
Type "L"
Sleep 300ms

# Khởi tạo với mặc định (Enter), rồi xem báo cáo từng bước và bước tiếp theo
Enter
Sleep 4s
Screenshot internal/tui/testdata/vhs/out/init-done.png

# Nạp lại: Tổng quan hiện bảng điều khiển
Type "r"
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/init-after.png
Type "q"
Sleep 300ms
