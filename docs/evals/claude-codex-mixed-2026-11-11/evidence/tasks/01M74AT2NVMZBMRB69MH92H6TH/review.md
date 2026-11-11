# Review tác vụ 01M74AT2NVMZBMRB69MH92H6TH

## Vòng 1 — 2026-11-11T03:38:48Z

Thay đổi nhỏ, đúng yêu cầu: Abs đúng với 0/âm/dương, ba test riêng, Add/Negate không đổi, go test và go vet qua; chỉ còn hành vi tràn tại math.MinInt đã ghi trong doc nhưng chưa có test.

### [01M74AT2NVMZBMRB69MH92H6TH.1] suggestion/low — Hành vi Abs(math.MinInt) đã ghi trong doc comment nhưng chưa có test khóa lại

- Vị trí: `calc.go:10-16`
- Bằng chứng: Doc comment ở calc.go:10 cam kết 'Với math.MinInt, kết quả vẫn là math.MinInt' (do -a tràn số với int bù hai); calc_test.go:22-39 chỉ kiểm 0, -7, 7. Câu hỏi mở trong requirement.md:24-27 chọn giữ hành vi này nhưng chưa có người xác nhận.

Nếu người duyệt chốt giữ hành vi tràn, thêm TestAbsMinInt (Abs(math.MinInt) == math.MinInt) để hợp đồng đã ghi trong doc được kiểm chứng; nếu muốn panic thì cần quyết định trước khi merge. Không ảnh hưởng tiêu chí chấp nhận.
