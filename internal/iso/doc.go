// Package iso là lõi của `bint iso`: profile tài khoản/cấu hình tách biệt cho harness (claude, codex),
// theo docs/iso-plan.md. Gói này chỉ dựng store, profile, env, liên kết chia sẻ cache, khoá phiên và thư
// mục temp; chưa có lệnh CLI (GĐ2) hay tích hợp workflow (GĐ5).
//
// Không phải sandbox: tiến trình con chạy cùng UID, quyền tệp không đổi.
//
// Env của tiến trình con chỉ được dựng ở một nơi (Env). Nơi khác cần "home thật" (state, config, gateway)
// dùng RealHome/RealEnv thay vì đọc HOME/XDG_* của tiến trình, vì trong profile chúng trỏ vào HOME giả.
package iso
