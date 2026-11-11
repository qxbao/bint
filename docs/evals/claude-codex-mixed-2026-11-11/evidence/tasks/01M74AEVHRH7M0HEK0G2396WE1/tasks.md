# Đầu việc

- [ ] Thêm `func Abs(a int) int` có doc comment vào cuối `calc.go` — xong khi: `go build ./...` qua và `Add`/`Negate` không đổi.
- [ ] Thêm `TestAbsZero` (Abs(0)==0) vào `calc_test.go` — xong khi: test chạy và qua.
- [ ] Thêm `TestAbsNegative` (Abs(-7)==7) — xong khi: test chạy và qua.
- [ ] Thêm `TestAbsPositive` (Abs(7)==7) — xong khi: test chạy và qua.
- [ ] Kiểm tra cuối — xong khi: `gofmt -l .` rỗng, `go test ./...` qua, `git diff` chỉ gồm dòng thêm trong calc.go và calc_test.go.
