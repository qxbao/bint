# Yêu cầu

## Mục tiêu và phạm vi
Thêm hàm `Abs(a int) int` trả về giá trị tuyệt đối vào package `calc` (`calc.go`),
kèm kiểm thử trong `calc_test.go`. Chỉ được sửa hai tệp này.

## Yêu cầu chức năng
1. `Abs(a int) int` được export, có doc comment tiếng Việt theo phong cách hiện có
   (vd. `// Abs trả về giá trị tuyệt đối.`).
2. `Abs(a) == a` khi `a >= 0`; `Abs(a) == -a` khi `a < 0`.
3. Không đổi chữ ký hay hành vi của `Add`, `Negate`; không thêm import vào `calc.go`.

## Tiêu chí chấp nhận
- [ ] Abs(0)==0, Abs(-7)==7, Abs(7)==7 có test riêng (mỗi trường hợp một hàm Test… riêng).
- [ ] `go test ./...` qua, không sửa Add/Negate (diff không chạm dòng 4–8 của calc.go
      và các test hiện có).

## Ngoài phạm vi
- Phiên bản generic / cho kiểu khác (int64, float…).
- Xử lý tràn số ở `math.MinInt`.

## Câu hỏi mở
- `Abs(math.MinInt)`: với số bù hai, `-math.MinInt` tràn và trả lại chính `math.MinInt`
  (âm). Mặc định kế hoạch: giữ hành vi tự nhiên của Go (như `-a`), không panic, không
  test trường hợp này. Nếu cần hành vi khác (panic/saturate) thì người duyệt cho biết.
