# Kế hoạch
## Hướng triển khai
Thêm hàm Max cạnh các hàm số nguyên trong calc.go; dùng so sánh trực tiếp để xử lý cả hai thứ tự đầu vào và trường hợp bằng nhau. Thêm ba test riêng trong calc_test.go theo các trường hợp đã thống nhất.

## Khu vực mã
- calc.go: thêm Max(a, b int) int; giữ nguyên các hàm hiện có.
- calc_test.go: thêm test riêng cho (2,9), (9,2), và (4,4).

## Rủi ro
Thay đổi cục bộ chưa commit hiện có bổ sung Abs trong cả hai tệp. Cần giữ nguyên các thay đổi này và chỉ thêm phần phục vụ Max.

## Chiến lược kiểm thử
Chạy go test ./... sau khi hoàn tất; lệnh này đã được xác nhận trên phiếu.
