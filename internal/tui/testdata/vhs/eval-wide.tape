# Màn Đánh giá (giai đoạn 4): ứng viên, tác vụ, đợt (báo cáo có biểu đồ cột), form chạy đợt. Chạy từ thư mục gốc repository:
#   vhs internal/tui/testdata/vhs/eval-wide.tape
# Cần bint trong PATH. Xem README.md cùng thư mục. Đợt thật cần harness: kịch bản chỉ chụp các màn không tốn chi phí.

Output internal/tui/testdata/vhs/out/eval-wide.gif

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
Type "5"
Sleep 1500ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-candidates.png

# Form khai thác, ước tính và chạy đợt (không gửi)
Type "m"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-mine-form.png
Escape
Type "E"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-estimate-form.png
Escape
Type "r"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-run-form.png
Escape

# Các thẻ: Tác vụ, Đợt
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-cases.png
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/eval-wide-batches.png

Type "q"
Sleep 500ms
