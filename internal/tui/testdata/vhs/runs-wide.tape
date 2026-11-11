# Màn Lượt chạy (giai đoạn 3): danh sách + chi tiết, chấm kết quả, quét Git, thẻ Bài học. Chạy từ thư mục gốc repository:
#   vhs internal/tui/testdata/vhs/runs-wide.tape
# Cần bint trong PATH. Xem README.md cùng thư mục.

Output internal/tui/testdata/vhs/out/runs-wide.gif

Set Shell "bash"
Set FontSize 16
Set Width 1700
Set Height 900
Set Padding 12
Set TypingSpeed 40ms

# Dựng repository tạm và khởi tạo Bint (chưa có lượt chạy nào nên danh sách hiện hướng dẫn nạp spool; ảnh có dữ liệu do chủ
# dự án chụp thêm sau khi chạy agent thật trong repo này).
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
Type "4"
Sleep 1s
Screenshot internal/tui/testdata/vhs/out/runs-wide-empty.png

# Quét Git (S): form có ngày, ngày hoàn tác và dòng lệnh tương đương
Type "S"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/runs-wide-scan-form.png
Escape

# Thẻ Bài học (.) và form tóm tắt thất bại (d)
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/runs-wide-lessons.png
Type "d"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/runs-wide-digest-form.png
Escape

Type "q"
Sleep 500ms
