# Đầu việc

- [ ] Thêm `func Abs(a int) int` có doc comment vào `calc.go` sau `Negate` — xong khi: `go build ./...` qua và `git diff calc.go` chỉ có dòng thêm.
- [ ] Thêm `TestAbsZero`, `TestAbsNegative`, `TestAbsPositive` vào `calc_test.go` — xong khi: mỗi test kiểm đúng một ca (0, -7, 7) và `git diff calc_test.go` chỉ có dòng thêm.
- [ ] Kiểm chứng — xong khi: `go test ./...` qua (đủ 6 test) và `gofmt -l .` không in gì.
