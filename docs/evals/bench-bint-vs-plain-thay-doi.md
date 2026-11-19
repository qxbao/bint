# Benchmark Bint so với không dùng Bint — thay đổi của Bint giữa các đợt và tác dụng đo được

Mỗi đợt chạy sau khi sửa Bint theo kết quả đợt trước. Bảng dưới ghi thay đổi, ý định, và điều đo được ở đợt kế tiếp.
Kết quả tổng hợp ở [bench-bint-vs-plain-tong-hop.md](bench-bint-vs-plain-tong-hop.md).

## Đợt 1 → 2 (2026-11-16)
- Đợt 1 (người dùng rành kỹ thuật, task trung/lớn) cho "không có tín hiệu", B tốn hơn A ở mọi số đo.
- Đợt 2 đổi sang **người dùng mơ hồ** và hai task T1/T2, kèm test ẩn kiểm hành vi thật (D1–D9 cho T1, S1–S6/N1/N2 cho T2).
  Kết quả "Có lợi, đánh đổi", nhưng yếu: chênh một critical. Đồng thời lộ **ba lỗi thật của luồng b-do với người dùng mơ hồ**.

## Đợt 2 → 3: `9e1296d`, `f816de5` (2026-11-16)
- `9e1296d` feat(workflow): b-do cho người dùng mơ hồ — classify, cổng không đổi tệp, câu hỏi b-feat, lựa chọn khi hết vòng sửa.
- `f816de5` fix(workflow): sửa 7 phát hiện review của phiếu thực hiện thay đổi trên.
- Ý định: sửa các lỗi luồng thấy ở đợt 2. Chủ dự án duyệt nới ngưỡng chi phí 2× → 2,5× vì b-req nay luôn chạy cho yêu cầu mơ hồ.
- Đo ở đợt 3: **không cải thiện, B xấu đi** (T1 0C/1M → 0C/3M, T2 6C/7M, 11,3× token, cả hai lượt chạm trần 90 phút giữa vòng sửa 3).
  Nguyên nhân tìm được: chỉ dẫn b-req "mỗi bề mặt một dòng" làm plan phình; b-feat nhiều lượt trên ngữ cảnh lớn; agent con tự sinh; vòng review không hội tụ.

## Đợt 3 → 4: `72aa43c` (2026-11-17)
fix(workflow): giảm phình kế hoạch b-req, tắt agent con Codex, dừng khi review không hội tụ, ghim điểm verify xanh.
- b-req: ngân sách kế hoạch (11 KB). Codex: tắt agent con tự sinh (`features.multi_agent_v2`). Review: quy tắc 0 + dừng khi không hội tụ (`reviewStalled`, `KHÔNG HỘI TỤ`).
  Ghim điểm xanh cuối (`refs/bint/tasks/<id>/green`, `bint task restore-green`).
- Đo ở đợt 4: **chi phí 11,3× → 2,51×**, N2 hết chậm (1,1–1,5 s), plan gọn 5/4 AC, T2 tự dừng "không hội tụ" 4→4. Nhưng chất lượng không hơn A (T1 0C/4M, T2 8C/2M) và T2 chỉ làm giai đoạn G1 của plan tự chia G1–G4.

## Đợt 4 → 5: `b69839f`, `a8f2f95`, `a0b8581` (+ merge `9095646`) (2026-11-17)
**Nhóm 1 (cho B đủ thời gian):**
- Đường nhanh cho yêu cầu mơ hồ đơn giản (một phiên b-feat, cổng kiểm chứng, một lượt review, tối đa một vòng sửa); nối tiếp phiên b-req → b-feat với kế hoạch rút gọn; trần vòng sửa theo số phát hiện giảm.
**Nhóm 2 (kiểm theo mục tiêu của người dùng):**
- b-req sinh mục "Kiểm tra mục tiêu" chạy được; b-feat/b-review chạy tới khi xanh; không cắt scope âm thầm (kết luận ghi rõ "mới xong 1/N" kèm lệnh lập phiếu giai đoạn sau).
**Sau thấy đường nhanh tự động bị chọn cho cả hai prompt (a8f2f95):**
- `workflow.fast_path` mặc định **tắt**; đường nhanh chỉ khi chủ động: `bint run b-do --fast` hoặc skill `/b-do-fast`. Giữ hai nhánh bỏ b-req cho việc nhỏ/yêu cầu ngắn nêu rõ chỗ sửa (quyết định của người dùng).
- Cổng cỡ kế hoạch (`workflow.small_plan_max_files`, mặc định 5): sau b-req, plan chỉ "nhỏ" (b-feat chạy ngay, không dừng) khi không có giai đoạn hoãn, đủ ít tệp/nhóm bề mặt, có kiểm tra mục tiêu nêu tên test và không chạm đường dẫn bảo vệ.
  Đếm mọi đường dẫn trên mỗi dòng; glob hoặc thiếu mục "Bề mặt bị ảnh hưởng" thì là lớn. Bỏ khoá khỏi `.bint/config.yml` của repo để binary cũ không hỏng khi nạp cấu hình.
**Nối tiếp phiên khác model (a0b8581, đã kiểm thật bằng tay):**
- Bỏ chặn "khác model thì không nối tiếp" cho Claude (`claude -p --model sonnet --resume <id>` trên phiên mở bằng opus) và Codex (`codex exec -m gpt-6-luna … resume <id> -` trên phiên mở bằng gpt-6-astra). Cache prompt mất ở lượt đầu, ngữ cảnh được giữ.

Đo ở đợt 5:
| Giả thuyết | Kết quả |
|---|---|
| Yêu cầu mơ hồ không tự rơi vào đường nhanh | Có: cả hai đi b-req → b-feat → b-review. Đường nhanh opt-in **chưa đo**. |
| b-feat nối tiếp phiên b-req | Có (notes ghi rõ), nhưng b-feat đầu vẫn 20 phút (T1) và 47 phút (T2). |
| b-req sinh kiểm tra mục tiêu | Có. Chưa thấy nó bắt được lỗi ở T2: cây cuối vẫn lộ ở 7 nơi lưu. |
| b-req không tự cắt scope | Có ở mức kế hoạch (34 tệp, 8 nhóm, không giai đoạn hoãn); kết quả xấu hơn đợt 4 (10C/6M, N2 14,4 s, chưa xanh khi hết giờ). |
| Cổng kế hoạch lớn rút ngắn thời gian | Không: cả hai plan "lớn" nên chỉ thêm một lần dừng chờ người. |
| Chi phí ≤ 2,5× | Không: 4,94× (vì phủ hết kế hoạch). |

## Quy trình đo (giữ nguyên qua các đợt)
- Test ẩn: T1 D1–D9 (+ D8i, D6), T2 S1–S6, N1, N2 (3 lần xen kẽ khi máy rảnh), test repo (build, vet, staticcheck Linux/Windows, `go test`).
- Review mù + xác minh mù bằng agent Opus trên cặp A-lần-3 (nhãn W) và B (nhãn Z); chỉ so A với B trong cùng lần chấm.
- Người dùng giả là Opus chỉ thấy thẻ người dùng và đầu ra Bint: hết vòng sửa lần đầu → `--fix-round`, sau đó → `--accept`; trần 90 phút Bint chạy mỗi lượt (thời gian chờ người dùng giả không tính).
