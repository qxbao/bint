# Đăng ký trước và nhật ký bench v8

- 2026-11-21T03:56:39+07:00 — Người dùng đổi số lượt chính thành A, B và B′ mỗi nhánh một lượt, bỏ A+. Dùng thống kê mô tả vì n=1 mỗi nhánh.
- 2026-11-21T04:00:03+07:00 — Người dùng xác nhận giữ đầy đủ N1 khi chấm A, B, B′, miễn yêu cầu N1 đạt cho binary gốc ở cổng hiệu chuẩn §4. Tái hiện N1.5 trên binary gốc: status sau chuyển mã 4, repo mới ở đường dẫn cũ đọc được tác vụ cũ, repo đã chuyển không đọc được (controls/n1-baseline-path-reuse.json).
- 2026-11-21T04:09:25+07:00 — Binary gốc chép từ v7 mang metadata 4db2657. Dựng lại từ checkout detached đúng 48dbfad và dùng bản đó cho oracle, hiệu chuẩn và N2. Đã đồng bộ tmpl-B bằng bint sync; cây theo dõi sạch.
- 2026-11-21T04:21:04+07:00 — Hiệu chuẩn thử 48dbfad đạt 12/36. Tri thức chờ duyệt đi theo cây repo nên xanh ở cả bốn cột. Theo §4.1, thay D8 trong ma trận bằng đánh giá good của người trên lượt chạy, có canary riêng trong ghi chú runs rate, xem lại qua runs show --json. N1 tiếp tục kiểm tri thức chờ duyệt, bên cạnh D8 mới. Đề bài và plan-v8.md giữ nguyên.
- 2026-11-21T04:28:30+07:00 — Test nền tmpl-B tại e41bb69 vẫn đỏ hai kiểm đồng bộ vì tệp skill nhúng từ 48dbfad khác skill xuất bản đúng theo binary điều phối. Đã cập nhật chỉ sáu tệp Markdown nhúng b-do, b-feat, b-fix, b-goal, b-req, b-review theo 0e35d20, chạy lại bint sync và ghi commit mẫu mới 9c4915056f2d6f70e09a9ccc890bc7aafbb42cde. Không thay đổi mã Go hay test của mẫu. Chạy lại test nền trên cây mẫu mới.
- 2026-11-21T04:32:45+07:00 — Sau khi đồng bộ sáu skill, kiểm hẹp xác nhận nội dung xuất bản khớp. Còn kiểm thử mẫu cũ đòi nguyên chữ gói/go test ./... và trần byte cũ. Đồng bộ đúng hai tệp assertion của skill từ 0e35d20: bdo_lean_goal_test.go và breq_baseline_goal_test.go. Diff chỉ gồm regex/cụm từ và trần byte khớp skill phát hành. Cây mẫu B/B′ 7ed9c5735898eba27cf744e94e6392971ac89e04; không sửa mã Go sản phẩm.
- 2026-11-21T05:08:43+07:00 — Chạy đầy đủ tại 7ed9c57 cho thấy chép skill nhúng từ binary điều phối làm đổi nội dung fixture CLI/golden của Go 48dbfad. Hoàn tác các bản chép trước. Mẫu B/B′ cuối e63cbbcb4b64367752da65cf85c5b14780e445b7 chỉ đổi hai kiểm tra bản skill đã cài để kiểm băm theo core.lock đã đồng bộ; tất cả mã sản phẩm, skill nhúng, kiểm hợp đồng và golden giữ từ e41bb69. Tái dùng go test ./... đầy đủ đã chạy tại e41bb69 cho mặt bằng không đổi; grade.py --baseline --refresh-baseline build lại mẫu cuối và chạy lại các subtest có thay đổi. Tách các test đỏ nền của Bint theo mẫu.

- 2026-11-21T05:10:19+07:00 — Khóa đề. Bản gốc [9, 9, 9]/36, oracle 36/36 trong ba lần, N2 đạt cả ba lần. N2 đo tỷ lệ thời gian theo kế hoạch và lưu riêng mã thoát của lệnh đo. Ảnh tại mốc 60/120/180 được chụp trong lúc chạy và chấm sau khi mọi lượt dừng để máy rảnh.

  - `plan-v8.md` `a219b2efea3ab5a5282a09f72bfd5a31f35812469588cc84f6e8d0274a580244`
  - `log/persona-card.md` `5126c5a2646a94eb10b0868a3e2c228db7674ca0ca10d1d4fea4f145ccf1df53`
  - `bin/bint.exe` `f6e5cba054db643eda4e404cf1267046cff95d43e019653dcd61323d0329dbca`
  - `bin/bint-base.exe` `68fc1389987cc74e4214b71d91fdf50611aca1d88fbd995e9bf861ca3005fa3a`
  - `hidden/hidden_l2.py` `08f77287019438154bfd7d69b9b5f29873a420c78440fe7f5cd34992cb519097`
  - `controls/l2_oracle.py` `33f8612b6b39f8428f7bcfb6babcc9bcb54b166b0e19e1c57b33a5f0690e0da0`
  - `controls/gotest-baseline-4db265768477.json` `ef88d268a630cd0979540d16a466187a1952cdd020df579e0f3c58f9943cda8a`
  - `controls/gotest-baseline-e63cbbcb4b64.json` `190ac7032b6c85f511af28df587518de8616eef39c6c00a588cd8d37e5acdc21`
  - `controls/template-baseline-comparison.json` `715c2ffc9bfc66dd61745add855a3be3570a1bbc0bfb63f09575ecc313705b78`
  - `controls/l2-oracle.json` `5547f90cb5661654b84feddb5581eca5684cfa85eab161f792a33061583f67aa`
  - `controls/l2-calibration.json` `9ab16546cc3a68c898e45067a68f47bb49c220d68b0b44b6c13c83feb2887295`
  - `controls/n2-decoys/complete.json` `f0caebcfbda4fbaab6feeec45890015b682742ae59957f5812bcac0d094697d7`
  - `controls/l2-base-1.json` `0e361aad1fc378e1f09a58897cf3f3682da74409333a08bcca74d4040b49808e`
  - `controls/l2-base-2.json` `0e361aad1fc378e1f09a58897cf3f3682da74409333a08bcca74d4040b49808e`
  - `controls/l2-base-3.json` `0e361aad1fc378e1f09a58897cf3f3682da74409333a08bcca74d4040b49808e`
  - `controls/l2-base-n2-1.json` `cab568ab1a2c20e46554ea3581b0a936ac2c3f85d9a37be1c3e375721fb71940`
  - `controls/l2-base-n2-2.json` `16c904cf13925b8e73156a506982ad0612754ae227866f813b00e87f01629629`
  - `controls/l2-base-n2-3.json` `ae26e4f5219cf1e734b7d91e84ee27fe63662b3c7bb6fb001cf70746b2d4df00`
  - `controls/l2-oracle-1.json` `9985eaa4ed7e7cc70c6fa48707612c4f7181bec4805a54a569010691ed7f52d3`
  - `controls/l2-oracle-2.json` `9985eaa4ed7e7cc70c6fa48707612c4f7181bec4805a54a569010691ed7f52d3`
  - `controls/l2-oracle-3.json` `9985eaa4ed7e7cc70c6fa48707612c4f7181bec4805a54a569010691ed7f52d3`

- 2026-11-21T05:30:46+07:00 — Lượt khói B′ kết thúc cap. Số kiểm hẹp tự động quan sát trong lượt khói 1. Kiểm thử tự động độc lập True. Người dùng giả đã qua kiểm tra cổng kế hoạch, --slice more và --answer.
