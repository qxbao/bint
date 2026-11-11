# Kế hoạch

## Hướng triển khai
Hàm nhánh đơn `if a < 0 { return -a }; return a` trong `calc.go`, đặt sau `Negate`.
Không dùng `math.Abs` (chỉ cho float64, phải ép kiểu, mất chính xác với số lớn) và không
gọi `Negate` để hàm độc lập, dễ đọc. Không thêm import.

## Tệp/ký hiệu sẽ chạm
- `calc.go`: thêm `func Abs(a int) int` + doc comment.
- `calc_test.go`: thêm `TestAbsZero`, `TestAbsNegative`, `TestAbsPositive`.
- Không chạm `Add`, `Negate`, `TestAdd`, `TestAddNegative`, `TestNegate`.

## Rủi ro
- Tràn số tại `math.MinInt` (xem Câu hỏi mở trong requirement) — theo mặc định đề xuất
  chỉ cần ghi chú trong doc comment.
- Sửa nhầm hàm/test có sẵn — kiểm tra bằng `git diff` chỉ có dòng thêm.

## Chiến lược kiểm thử
- Ba hàm test riêng: `Abs(0)==0`, `Abs(-7)==7`, `Abs(7)==7`, báo lỗi bằng
  `t.Fatalf("Abs(x)=%d", got)` như test hiện có.
- Chạy `go test ./...` (lệnh kiểm chứng đã xác nhận); thêm `gofmt -l .` phải rỗng.
