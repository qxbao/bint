# Màn Tri thức (giai đoạn 1): hộp thư → lô nguon:lo-thu → duyệt theo lô. Chạy từ thư mục gốc repository:
#   vhs internal/tui/testdata/vhs/knowledge-wide.tape
# Cần bint trong PATH. Xem README.md cùng thư mục.

Output internal/tui/testdata/vhs/out/knowledge-wide.gif

Set Shell "bash"
Set FontSize 16
Set Width 1700
Set Height 900
Set Padding 12
Set TypingSpeed 40ms

# Dựng repository tạm, khởi tạo Bint và ghi 6 mục candidate của agent (bên thứ ba) cùng lô nguon:lo-thu
# (ẩn khỏi ảnh). Trạng thái đặt vào thư mục tạm nên không đụng tới trạng thái thật của máy.
Hide
Type "export BINT_STATE_DIR=$(mktemp -d) BINT_CONFIG_HOME=$(mktemp -d) BINT_LANG=vi"
Enter
Type "cd $(mktemp -d) && git init -q && git config user.name Demo && git config user.email demo@example.com"
Enter
Type "for w in alpha bravo charlie delta echo foxtrot; do printf 'package demo\n\nfunc F%s() {}\n' $w > f$w.go; done"
Enter
Type "printf 'module demo\n' > go.mod && git add -A && git commit -qm init && bint init --no-hooks >/dev/null"
Enter
Type "for w in alpha bravo charlie delta echo foxtrot; do BINT_AGENT=1 bint knowledge add --type fact --paths 'f*.go' --evidence f$w.go:3 --third-party --tags nguon:lo-thu \"Ham $w xu ly phan $w rieng biet\" >/dev/null; done"
Enter
Type "clear && stty size"
Enter
Show

Type "bint"
Enter
Sleep 2s
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-inbox.png

# Enter trên dòng lô nhảy tới màn Tri thức đã đặt sẵn bộ lọc chờ duyệt + nhãn lô
Enter
Sleep 1s
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-list.png

# Chọn hai mục (x), rồi xem chi tiết (Enter) và quay lại (Escape)
Type "x"
Type "x"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-selected.png
Escape
Enter
Sleep 600ms
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-detail.png
Escape

# Duyệt cả lô (A): hộp xác nhận nêu số mục; Escape để huỷ trong kịch bản ảnh
Type "A"
Sleep 1s
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-batch.png
Escape
Sleep 300ms

# Lọc nhanh theo chữ (/), form thêm mục (n), bảng chuyển trạng thái (T)
Type "/"
Type "bravo"
Enter
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-filter.png
Type "c"
Type "n"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-add-form.png
Escape
Type "T"
Sleep 400ms
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-transitions.png
Escape

# Thẻ phụ: Tìm kiếm, Xuất bản AGENTS.md (chạy thử rồi xem diff)
Type "."
Sleep 400ms
Type "alpha"
Enter
Sleep 1s
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-search.png
Escape
Type "."
Type "."
Type "."
Type "."
Sleep 400ms
Enter
Enter
Sleep 1s
Screenshot internal/tui/testdata/vhs/out/knowledge-wide-publish.png
Escape

Type "q"
Sleep 500ms
