# Màn Tổng quan ở cửa sổ rộng (140×40): logo điểm ảnh; và chế độ không màu (NO_COLOR).
# Chạy từ thư mục gốc repository: vhs internal/tui/testdata/vhs/overview-wide.tape

Output internal/tui/testdata/vhs/out/overview-wide.gif

Set Shell "bash"
Set FontSize 16
Set Width 1800
Set Height 1000
Set Padding 12
Set TypingSpeed 40ms

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

Type "bint"
Enter
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/overview-wide-vi.png
Type "L"
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/overview-wide-en.png
Type "q"
Sleep 400ms

# NO_COLOR: chỉ còn đậm, gạch chân, đảo màu
Type "clear && NO_COLOR=1 bint"
Enter
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/overview-nocolor.png
Type "q"
Sleep 300ms
