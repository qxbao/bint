# Kịch bản VHS cho màn hình TUI

Mỗi màn hình có một kịch bản [VHS](https://github.com/charmbracelet/vhs) (`.tape`) để xuất ảnh PNG và
GIF cho chủ dự án **duyệt bằng mắt** (xem `docs/tui-plan.md` mục 4.4). Không so pixel tự động: golden
test (`go test ./internal/tui/...`) chỉ chống thụt lùi bố cục, còn "đẹp hay chưa" do người duyệt ảnh.

| Kịch bản | Nội dung | Ảnh xuất ra (trong `out/`) |
|---|---|---|
| `overview.tape` | Tổng quan (repo đã init) ở ~80×24: tiếng Việt rồi tiếng Anh, bảng lệnh, trợ giúp, tab chưa làm | `overview-*.png`, `overview.gif` |
| `overview-wide.tape` | Tổng quan ở ~140×40 (logo điểm ảnh) và chế độ NO_COLOR | `overview-wide-*.png`, `overview-nocolor.png` |
| `init.tape` | Màn tạm "Khởi tạo với mặc định" (repo chưa init), rồi sau khi khởi tạo | `init-*.png`, `init.gif` |
| `gallery.tape` | Trang trưng bày component: tối, sáng, 16 màu, NO_COLOR; vi, en, locale giả | `gallery-*.png`, `gallery.gif` |

## Chạy tay

Cần `vhs`, `ttyd` và `ffmpeg` trong PATH, và `bint` đã build (`go install ./cmd/bint` hoặc `go build -o /tmp/bint/bint ./cmd/bint`
rồi đặt `/tmp/bint` vào PATH). Chạy từ **thư mục gốc repository**:

```sh
go build -o /tmp/bint-vhs/bint ./cmd/bint
export PATH=/tmp/bint-vhs:$PATH
for t in overview overview-wide init gallery; do vhs internal/tui/testdata/vhs/$t.tape; done
ls internal/tui/testdata/vhs/out
```

Kịch bản tự dựng một repository tạm và đặt `BINT_STATE_DIR` vào thư mục tạm nên **không đụng tới trạng thái thật** của máy.

## Ghi chú

- Kích thước cửa sổ đặt theo pixel (`Set Width/Height`) với `Set FontSize 16`; thử lại và chỉnh nếu terminal ảo của VHS
  cho số cột/dòng khác 80×24 và 140×40 (xem `stty size` ở đầu mỗi kịch bản). VHS không đổi kích thước giữa chừng nên cửa sổ rộng có kịch bản riêng.
- CI chưa xuất ảnh làm artifact: `.github/workflows/**` là đường dẫn được bảo vệ, nên việc thêm bước VHS vào CI cần người
  duyệt riêng (xem `docs/tui-plan.md`).
