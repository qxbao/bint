# Benchmark Bint vs không Bint — lần 4 (chạy lại B sau bản sửa 72aa43c)

Ngày 2026-11-17. Chạy lại **chỉ phía B** (Bint) bằng binary build từ commit `72aa43c` (sha trong `log/bint.sha256`); phía A (không Bint)
giữ nguyên diff của lần 3 (cùng commit gốc `8757340`, cùng prompt, cùng thẻ người dùng, cùng test ẩn `hidden2.py`).
Review mù và xác minh mù chạy lại trên cặp **A-lần-3 (nhãn W) vs B-lần-4 (nhãn Z)** với cùng một reviewer/verifier, nên hai bên được
đếm cùng một thước. Quy tắc đọc kết quả là bảng chốt ở mục 8 của `docs/benchmark-bint-vs-plain-v3-plan.md` (không đổi sau khi thấy số).

## 1. Kết luận

**Theo bảng chốt: vẫn "Bint bất lợi"** (A tốt hơn ở cả hai task, B không tốt hơn ở task nào), nhưng khoảng cách thu hẹp rất nhiều so với lần 3:

| Task | A (không Bint) | B lần 4 | Kết luận theo bảng | B lần 3 (để so) |
|---|---|---|---|---|
| T1 dọn dữ liệu cũ | 0 critical, 2 medium | 0 critical, 4 medium | A tốt hơn (ít hơn đúng 2 medium — ngưỡng của luật) | 0C / 3M |
| T2 không lưu bí mật | 7 critical, 1 medium | 8 critical, 2 medium | A tốt hơn (ít hơn 1 critical — trong dao động của phép đo) | 6C / 7M |

Chi phí: **2,51× token vào** (36,95M so với 14,70M) — lần 3 là 11,3×; ngưỡng chốt 2,5×. Thời gian tường: B ≈ 88–90 phút mỗi lượt, A ≈ 19–22 phút.

Độ tin cậy: **thấp.** Mỗi ô một lượt; chênh T2 là 1 critical; chênh T1 là đúng ngưỡng 2 medium. Cùng diff của A, reviewer lần 3 đếm 5C/2M (T2) còn
lần này 7C/1M (đếm theo từng nơi lưu) — dao động của người chấm cỡ ±2 critical, lớn hơn khoảng cách giữa A và B ở T2.
Số tuyệt đối không so được giữa lần 3 và lần 4 cho B ở T2 (reviewer khác, độ hạt khác); chỉ so được A với B **trong cùng lần chấm này**.

Điều đo được rõ ràng (không phụ thuộc cách đếm): B tiêu ít token hơn ~4,5×, diff nhỏ hơn 2,6–4,7×, hết lỗi chậm 15× ở N2, `go test` cuối xanh,
và test ẩn của B **giống hệt A** ở cả hai task (T1: D6 + D8i; T2: S1–S5 critical + S6). Điều không đổi: B vẫn không tốt hơn A về chất lượng kết quả, và B vẫn chậm gấp ~4×.

## 2. Chi phí

Token vào, `tools/tokens.py`, mốc `2026-11-17T04:00:00Z`:

| Lượt | Phiên | Token vào | Cache | Token ra |
|---|---|---|---|---|
| B-T1 | 6 | 23,52M | 22,08M | 140k |
| B-T2 | 4 | 13,43M | 12,78M | 147k |
| **B tổng** | | **36,95M** (cộng cả token ra: 37,23M) | | |

A (lần 3): 14,70M (token vào). Tỷ lệ = 36,95/14,70 ≈ **2,51×** (lần 3: 11,3×; ngưỡng 2,5× — sát ngưỡng, nhỉnh hơn 0,01).
- Thời gian: B-T1 xong sau 04:09→05:39Z (88,5 phút chạy Bint, 1 phút chờ người dùng giả), B-T2 **chạm trần 90 phút** (exit 124) giữa vòng sửa.
  b-feat đầu ≈ 30 / 28 phút (lần 3: 47 / 40).
- Diff: B-T1 96 KB (lần 3: 251 KB), B-T2 51 KB (lần 3: 240 KB); A: 40 KB / 37 KB.
- Kế hoạch b-req: T1 11,4 KB (5 AC, 5 TSK), T2 10,9 KB (4 AC, 4 TSK) — hết bùng nổ "mỗi bề mặt một dòng" của lần 3.
- Không còn agent con tự sinh (cờ Codex `features.multi_agent_v2.disable_native_tools=true` cho bước workflow).

## 3. Kết quả từng lượt

### T1 (dọn dữ liệu cũ)
- B: `bint clean` (mặc định chỉ xem trước, `--apply` xoá; mặc định 90 ngày). Review vòng 4: 0 phát hiện; Bint tự `go test ./...` xanh.
  Luồng: b-req (2 câu hỏi mở) → người dùng giả trả lời → b-feat → review 5 high → 1 high → hết vòng sửa → người dùng giả `--fix-round` (lần 1, mục 6.2) → review 0.
- Test ẩn B: D8i (xem trước không liệt kê phiếu cũ), D6 (log phiếu kết thúc cũ không được dọn, giải phóng 0 B) — **cùng hai lỗi với A lần 3**. D1–D5, D7, D9, FX: qua.
  Repo: build/vet/staticcheck Linux+Windows/`go test ./...` qua hết.
- Review mù (đã xác minh): A (W): W1 medium (hạ từ critical: tự dọn ngầm, mặc định 30 ngày, xoá cả backup duy nhất), W5 medium (không dọn `worktrees`/phiếu/log); W2–W4 low.
  B (Z): Z1 medium (backup nhận dạng theo tên → xoá bản duy nhất của `~/.claude/settings.json`), Z2 medium (một tệp nhị phân bất kỳ làm cả lượt dọn = 0 mục),
  Z3 medium (hiệu năng bậc hai: SQLite 20 MB → xem trước 56,6 s, apply 88 s), Z5 medium (không dọn worktree/phiếu/log; D6 và D8i trùng mục này); Z4 low.
- Sau bỏ trùng: **A 0C/2M, B 0C/4M.** Ưu điểm B ở đây: xem trước mặc định, không tự dọn ngầm, không chạm phiếu chưa kết thúc.

### T2 (không lưu bí mật)
- B: dừng ở **tự dừng do không hội tụ** (review vòng 2: 4 high, vòng trước đó 4 high → cơ chế `reviewStalled` hoạt động), người dùng giả `--fix-round`, rồi chạm trần 90 phút.
  Điểm xanh cuối đã ghim (`refs/bint/tasks/<id>/green`), bản chép tại điểm xanh cho kết quả test ẩn **giống hệt** bản bị cắt.
- Test ẩn B (bản cắt và bản xanh như nhau): S1–S5 critical, S6 medium — **y hệt A lần 3**. N2: **1,1–1,5 s** so với gốc ~1,0 s (A 3,4 s; B lần 3 13,4 s).
  Repo: build/vet/staticcheck/`go test ./...` qua hết (cả hai bản).
- Review mù đã xác minh, sau bỏ trùng: **A (W): 7C/1M/4L; B (Z): 8C/2M/0L.** Cả hai đều chỉ che log tiến trình và lệnh hook; bí mật vẫn thô trong
  `task.yml`, `plan/requirement/tasks.md`, `review.*`, `state.json`, `logs/prompt.txt`, `state.sqlite` (`result_summary`), cả sổ `.bint/tasks`.
  Khác biệt: Z che **kém hơn** ở log kiểm chứng/agent (lộ cả dạng có nhãn: `AWS_SECRET_ACCESS_KEY=`, `Authorization: Basic`, `PGPASSWORD=`…), có **hồi quy** so với gốc
  (`detect.Redact` bỏ mẫu cờ `--access-token/--github-token/--db-password/--client-secret` → lưu thô trong hook) và che sai nuốt dữ liệu (Z10: sau `'Bearer …'` mất các dòng `--- FAIL`);
  W che rộng hơn nhưng chậm hơn (3,7–4,7 s/20 MiB) và che quá tay khi biến môi trường có giá trị ngắn (W10).
- Cả hai bên không đạt mục tiêu của task (≥ 7 critical).

## 4. Kiểm giả thuyết của bản sửa (72aa43c)

| Giả thuyết | Kết quả |
|---|---|
| Plan b-req không phình | **Có**: 11,4 / 10,9 KB, 5/4 AC (lần 3 gấp đôi ở lần 2). |
| Không agent con tự sinh | **Có**: số phiên Codex giảm 10→6 và 8→4 (không còn phiên con song song). |
| Review không hội tụ bị chặn | **Có một phần**: T2 dừng sớm ở vòng 2 (4→4); T1 hội tụ 5→1→0. Khoảng cách chi phí 11,3× → 2,5× chủ yếu từ đây và từ plan nhỏ. |
| Điểm xanh cuối có thể phục hồi | **Có**: `bint task restore-green` chưa chạy `--apply` thật; bản chép tại điểm xanh đã dùng để đo. |
| N2 chậm 15× | **Hết** (1,1–1,5 s so với gốc ~1,0 s). Chưa phân tích nguyên nhân; nhiều khả năng do cách che trong diff B khác lần 3. |
| Chất lượng kết quả tốt hơn A | **Không.** Vẫn A ≥ B ở cả hai task (xem mục 1). |
| H4 (nối tiếp phiên b-feat) / H5 (review "bỏ sót") | Chưa phân tích riêng trong lần này. |

## 5. Lưu ý, thiên lệch

- Một lượt mỗi ô; B-T2 bị cắt bởi trần giữa vòng sửa (đã ghim điểm xanh, kết quả không đổi giữa bản cắt và bản xanh).
- Reviewer là một agent (Opus) mỗi task cho cả hai bên; A và B được chấm cùng lúc nên cùng thước, nhưng không có lượt chấm lặp.
- Người dùng giả là Opus chỉ thấy thẻ người dùng, đầu ra Bint và `requirement.md` (xem `log/user-actions.md`); mỗi bên 1 tin nhắn trả lời + 1 lần `--fix-round`.
- Sự cố môi trường (`log/env-incidents.md`): đĩa cạn hai lần, xoá cache build đã xong; không ảnh hưởng kết quả.
- `~/.codex/config.toml` còn khối trust của v2, v3, v4 — chờ chủ dự án quyết gỡ.
- Lỗi chưa kiểm chứng của bản sửa: `restore-green --apply` trên worktree thật/Windows, đồng thời với phiếu khác, tương thích Codex 0.167.

## 6. Việc kế tiếp có thể làm
1. T2 là task mà B kém A chủ yếu vì cắt giữa vòng sửa và phạm vi hẹp (diff chỉ chạm `detect/redact/eval`, bỏ qua gói `task`): đáng thử b-req bắt buộc liệt kê **mọi nơi lưu** (đã có mục "Bề mặt bị ảnh hưởng" nhưng ngắn gọn ≤ 8–10 dòng).
2. Với T1, cả hai bên cùng hụt D6/D8i (dọn phiếu/log cũ): đây là điểm yêu cầu mơ hồ chưa ai hỏi người dùng "phình ở đâu" — câu hỏi mở của b-req có thể tập trung vào đo dung lượng trước.
3. Chạy thêm ≥ 2 lượt/ô nếu muốn kết luận có lợi/bất lợi vững hơn: chênh hiện tại nằm trong dao động của phép đo.
