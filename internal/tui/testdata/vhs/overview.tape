# Màn Tổng quan (repository đã init). Chạy từ thư mục gốc repository: vhs internal/tui/testdata/vhs/overview.tape
# Cần bint trong PATH. Xem README.md cùng thư mục.

Output internal/tui/testdata/vhs/out/overview.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 640
Set Padding 12
Set TypingSpeed 40ms

# Dựng repository tạm và khởi tạo Bint (ẩn khỏi ảnh). Trạng thái đặt vào thư mục tạm.
Hide
Type "export BINT_STATE_DIR=$(mktemp -d) BINT_CONFIG_HOME=$(mktemp -d) BINT_LANG=vi"
Enter
Type "cd $(mktemp -d) && git init -q && git config user.name Demo && git config user.email demo@example.com"
Enter
Type "printf 'module demo\n' > go.mod && git add -A && git commit -qm init && bint init --no-hooks >/dev/null"
Enter
Type "clear && stty size"
Enter
Show

# 80×24: bint không đối số mở TUI
Type "bint"
Enter
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/overview-vi.png

# Đổi ngôn ngữ ngay trong phiên (phím L)
Type "L"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/overview-en.png
Type "L"
Sleep 400ms

# Bảng lệnh (:) và tìm mờ
Type ":"
Sleep 400ms
Type "ngon ngu"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/overview-palette.png
Escape
Sleep 300ms

# Trợ giúp (?)
Type "?"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/overview-help.png
Escape
Sleep 300ms

# Chuyển tab: tab chưa làm nói rõ giai đoạn và lệnh CLI tương đương
Type "3"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/overview-tasks-placeholder.png

# Thoát
Type "q"
Sleep 500ms
