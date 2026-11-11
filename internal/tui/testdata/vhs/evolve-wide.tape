# Màn Tiến hoá (giai đoạn 3): cơ hội, đề xuất, lịch sử, hàng đợi. Chạy từ thư mục gốc repository:
#   vhs internal/tui/testdata/vhs/evolve-wide.tape
# Cần bint trong PATH. Xem README.md cùng thư mục.

Output internal/tui/testdata/vhs/out/evolve-wide.gif

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
Type "6"
Sleep 1500ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-opportunities.png

# Quét (s) và form tạo đề xuất (p) có dòng lệnh tương đương
Type "s"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-scan-form.png
Escape
Type "p"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-propose-form.png
Escape

# Các thẻ: Đề xuất, Lịch sử, Hàng đợi
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-proposals.png
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-history.png
Type "."
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/evolve-wide-queue.png

Type "q"
Sleep 500ms
