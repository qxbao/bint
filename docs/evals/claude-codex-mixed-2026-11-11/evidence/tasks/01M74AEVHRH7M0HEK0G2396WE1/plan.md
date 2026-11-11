# Kế hoạch

## Hướng triển khai
Thêm một hàm thuần, không phụ thuộc, ngay sau `Negate` trong `calc.go`:
nhánh `if a < 0 { return -a }; return a`. Không dùng `math.Abs` (chỉ cho float64,
cần ép kiểu và import) — cách rẽ nhánh đơn giản, đúng với int và khớp phong cách
một-hàm-một-dòng của package.

## Tệp / ký hiệu sẽ chạm
- `calc.go`: thêm `func Abs(a int) int` + doc comment (append cuối tệp).
- `calc_test.go`: thêm `TestAbsZero`, `TestAbsNegative`, `TestAbsPositive`,
  cùng kiểu `if got := …; got != … { t.Fatalf(...) }` như test hiện có.

## Rủi ro
- Thấp. Duy nhất: tràn ở `math.MinInt` (xem Câu hỏi mở trong requirement) — không
  ảnh hưởng tiêu chí chấp nhận.
- Lỡ tay định dạng lại/sửa `Add`, `Negate` → vi phạm tiêu chí 2; chỉ append.

## Chiến lược kiểm thử
- Ba test riêng cho 0, -7, 7.
- Chạy `gofmt -l .` (rỗng) và `go test ./...` (lệnh kiểm chứng đã xác nhận).
- `git diff` chỉ có dòng thêm (+) trong calc.go/calc_test.go.
