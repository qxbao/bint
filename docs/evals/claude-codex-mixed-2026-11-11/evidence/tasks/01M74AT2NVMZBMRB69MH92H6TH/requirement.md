# Yêu cầu

## Mục tiêu và phạm vi
Thêm hàm `Abs(a int) int` trả về giá trị tuyệt đối vào package `calc` (`calc.go`),
kèm kiểm thử trong `calc_test.go`. Chỉ được sửa hai tệp này.

## Yêu cầu chức năng
1. `Abs(a int) int` khai báo trong `calc.go`, có doc comment tiếng Việt theo phong cách
   hiện có (vd. `// Abs trả về giá trị tuyệt đối.`).
2. `Abs(a) == a` khi `a >= 0`; `Abs(a) == -a` khi `a < 0`.
3. Không thay đổi `Add`, `Negate` và các test hiện có.
4. Mỗi trường hợp 0, âm, dương có một hàm test riêng (theo kiểu `TestAdd`/`TestNegate`:
   một hàm `TestXxx` cho mỗi ca, báo lỗi bằng `t.Fatalf`).

## Tiêu chí chấp nhận
- [ ] Abs(0)==0, Abs(-7)==7, Abs(7)==7 có test riêng
- [ ] go test ./... qua, không sửa Add/Negate

## Ngoài phạm vi
- Phiên bản cho kiểu khác (int64, float, generic).
- Thay đổi API khác của package.

## Câu hỏi mở
- `Abs(math.MinInt)`: với `int` bù hai, `-math.MinInt` tràn số và trả lại `math.MinInt`
  (âm). Chữ ký `int` không cho trả lỗi. Mặc định đề xuất: giữ hành vi này và ghi rõ trong
  doc comment (giống `Negate` hiện cũng tràn ở `MinInt`); nếu người muốn `panic` thay vào
  đó thì cần quyết định trước khi làm. Không ảnh hưởng tới tiêu chí chấp nhận.
