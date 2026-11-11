# Trang trưng bày component (bint tui --gallery, ẩn). Chạy từ thư mục gốc: vhs internal/tui/testdata/vhs/gallery.tape

Output internal/tui/testdata/vhs/out/gallery.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 900
Set Padding 12
Set TypingSpeed 40ms

Hide
Type "export BINT_LANG=vi && clear"
Enter
Show

Type "bint tui --gallery"
Enter
Sleep 1500ms
Screenshot internal/tui/testdata/vhs/out/gallery-dark-vi.png

# m: đổi chế độ màu (tối → sáng → 16 màu → đơn sắc); l: đổi ngôn ngữ (vi → en → locale giả)
Type "m"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-light-vi.png
Type "m"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-ansi16-vi.png
Type "m"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-mono-vi.png
Type "m"
Sleep 300ms

Type "l"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-dark-en.png
Type "l"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-dark-pseudo.png

# Cuộn xuống xem logo và bảng lệnh
Type "jjjjjjjjjjjjjjjjjjjj"
Sleep 500ms
Screenshot internal/tui/testdata/vhs/out/gallery-scrolled.png
Type "q"
Sleep 300ms
