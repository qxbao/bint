# Benchmark Bint vs không Bint — lần 5 (chạy lại B sau đường nhanh opt-in, cổng cỡ kế hoạch, kiểm tra mục tiêu, nối tiếp phiên)

Ngày 2026-11-17. Chạy lại **chỉ phía B** (Bint) bằng binary build từ commit `9095646` của `main` (sha trong `log/bint.sha256`); phía A (không Bint)
giữ nguyên diff của lần 3 (cùng commit gốc `8757340`, cùng prompt, cùng thẻ người dùng, cùng test ẩn `hidden2.py`).
Review mù và xác minh mù chạy lại trên cặp **A-lần-3 (nhãn W) vs B-lần-5 (nhãn Z)** bằng một reviewer và một verifier Opus cho mỗi task,
nên hai bên được đếm cùng một thước. Quy tắc đọc kết quả là bảng chốt ở mục 8 của `docs/benchmark-bint-vs-plain-v3-plan.md` (không đổi sau khi thấy số).

## 1. Kết luận

**Theo bảng chốt: vẫn "Bint bất lợi"** (A tốt hơn ở T2, T1 hoà, B không tốt hơn ở task nào). So với lần 4, T1 tiến từ "A tốt hơn" lên "Hoà";
T2 xấu hơn; chi phí tăng gấp đôi.

| Task | A (không Bint) | B lần 5 | Kết luận theo bảng | B lần 4 | B lần 3 |
|---|---|---|---|---|---|
| T1 dọn dữ liệu cũ | 0 critical, 3 medium | 0 critical, 2 medium | **Hoà** (ít hơn 1 medium, luật cần ≥ 2) | 0C / 4M (A tốt hơn) | 0C / 3M |
| T2 không lưu bí mật | 7 critical, 1 medium | 10 critical, 6 medium | **A tốt hơn** (ít hơn 3 critical) | 8C / 2M | 6C / 7M |

Chi phí: **4,94× token vào** (72,60M so với 14,70M; ngưỡng chốt 2,5×) — lần 4: 2,51×, lần 3: 11,3×. Thời gian tường: B-T1 ≈ 77 phút Bint (đã trừ thời gian chờ người dùng giả),
B-T2 **chạm trần 90 phút** giữa vòng sửa đầu tiên (exit 124); A ≈ 19–22 phút.

Độ tin cậy: **thấp.** Một lượt mỗi ô; chênh T1 là 1 medium; reviewer khác nhau cho cùng diff của A cho số khác nhau (A T1: lần 4 là 0C/2M, lần này 0C/3M; W1 cùng
là phát hiện critical của reviewer rồi được verifier hạ xuống medium). Chênh T2 là 3 critical trên tổng 7–10 nên lớn hơn dao động của người chấm (±2).
Chỉ so được A với B **trong cùng lần chấm này**.

## 2. Chi phí

Token vào (`tools/tokens.py`, mốc `2026-11-17T14:00:00Z`; `results/B-T*.tokens.json`):

| Lượt | Số phiên Codex | Token vào | Cache | Token ra |
|---|---|---|---|---|
| B-T1 | 5 | 25,50M | 24,49M | 96k |
| B-T2 | 2 | 47,10M | 45,87M | 147k |
| **B tổng** | | **72,60M** | | |

A (lần 3): 8,77M + 5,93M = 14,70M. Tỷ lệ = 72,60 / 14,70 ≈ **4,94×**.
- B-T1: 25,5M (lần 4: 23,5M) — gần như không đổi dù thêm một điểm dừng cổng kế hoạch. B-T2: 47,1M (lần 4: 13,4M, lần 3 gấp nhiều lần) — phần chênh nằm ở **một b-feat 47 phút** trên kế hoạch 34 tệp.
- Diff: B-T1 68 KB (lần 4: 96 KB), B-T2 175 KB (lần 4: 51 KB); A: 41 KB / 37 KB. B-T2 lớn hơn lần 4 vì lần này kế hoạch phủ cả 8 nhóm bề mặt thay vì chỉ giai đoạn G1.
- Mọi bước b-feat đầu đều **nối tiếp phiên b-req** (`notes`: "b-feat nối tiếp phiên … của b-req (kế hoạch rút gọn, không nạp lại ngữ cảnh từ đầu)"); các vòng sửa nối tiếp phiên b-feat. Mọi bước cùng model `gpt-6.1-sol` nên không dùng tới bản sửa "khác model".

Dòng thời gian (`log/timeline-B-T*.txt`, `log/B-T*.n.out`):
- B-T1: b-req 1m32s → 2 câu hỏi mở → người dùng giả trả lời → b-req nối tiếp 3m03s → **cổng kế hoạch lớn** (15 tệp, 8 nhóm bề mặt) dừng chờ → người dùng giả chạy `bint run resume`
  → b-feat 20m02s, review (kiểm chứng qua) → sửa 10m36s → sửa 7m41s → hết vòng sửa còn 1 high → `--fix-round` (mục 6.2) → b-feat 2m24s → review vòng 4: **0 phát hiện** → done.
- B-T2: b-req 1m59s → 2 câu hỏi mở → trả lời → b-req nối tiếp 3m46s → **Q-3 mới** (đăng nhập profile) → trả lời → b-req nối tiếp 1m27s → **cổng kế hoạch lớn** (34 tệp, 8 nhóm, không giai đoạn hoãn)
  → người dùng giả chạy `resume` → **b-feat 47m06s** → dừng hỏi về `BINT_EVAL_PROMPT_FILE` → trả lời bằng `--answer` → b-feat 11m25s, kiểm chứng **không qua** → review vòng 1: **6 high** →
  b-feat sửa 13m10s → **bị cắt bởi trần 90 phút** trước khi kiểm chứng/review lại. Phiếu `interrupted`, `last_green = None` (chưa từng có điểm xanh), nên chỉ đo được đúng cây hiện tại.
- Người dùng giả chạm 3 lần ở T2 (tin nhắn #1–#3, trong giới hạn 4) và 1 lần ở T1; thêm các lệnh chạy nguyên văn (cổng kế hoạch lớn, `--fix-round`). Các lần dừng này **không tính vào trần 90 phút**.

## 3. Kết quả từng lượt

### T1 (dọn dữ liệu cũ)
- B: `bint clean` (mặc định chỉ xem trước, `--apply` xoá, `--older-than-days` mặc định 90). Trong Z: không đụng phiếu đang mở, worktree, SQLite, không theo symlink ra ngoài.
- Test ẩn B: D8i (xem trước không liệt kê phiếu cũ) và D6 (log phiếu kết thúc cũ không được dọn, giải phóng 0 B) — **cùng hai lỗi với A lần 3 và B lần 4**. D1–D5, D7, D9, FX: qua.
  Test repo (build, vet, `staticcheck` Linux+Windows, `go test ./...`): qua hết.
- Review mù đã xác minh (`review/T1-*.json`), sau bỏ trùng:
  - **A (W): 0C/3M/4L.** W1 (critical → medium): tự xoá ngầm `backups/` sau 30 ngày, không xem trước, không báo; W2 (medium): xoá log eval mà SQLite vẫn trỏ tới; W7 (medium): thiếu dọn log phiếu đã xong (nguồn phình chính).
  - **B (Z): 0C/2M/2L.** Z1 (medium): gần như không lấy lại dung lượng (chỉ dọn cache base eval và `tmp/verify-*`; một symlink, một phiếu mở hoặc một worktree là còn 0 byte); Z2 (medium): hồi quy — `bint eval clean` in "Đã xoá 0" mã 0 khi có tiến trình giữ khoá chia sẻ;
    Z3, Z4 low (xem trước vẫn tạo tệp khoá, đầu ra tiếng Anh…); Z5 bị bác; Z6 trùng Z1.
- Điểm B hơn A: xem trước mặc định, không tự dọn ngầm. Điểm B kém A: không lấy lại được dung lượng người dùng muốn; cả hai bỏ sót log phiếu đã xong.

### T2 (không lưu bí mật)
- Test ẩn B (cây bị cắt): **S1–S5 critical, S6 medium** — y hệt A lần 3 và B lần 4. N1 (che quá tay): qua. Test repo: qua hết.
- **N2 (hiệu năng): FAIL** — verify 20 MB: **14,4 / 14,4 / 14,3 s** so với gốc 0,9 s (≈ 16×; đo 3 lần xen kẽ khi máy rảnh). Lần 4: 1,1–1,5 s (qua); lần 3: 13,4 s. A: 3,4 s.
- Review mù đã xác minh (`review/T2-*.json`), sau bỏ trùng:
  - **A (W): 7C/1M/2L.** Che ở log tiến trình; dữ liệu phiếu (`task.yml`, requirement/plan/tasks, review, `state.json`) và `state.sqlite` vẫn thô.
  - **B (Z): 10C/6M/3L.** Phủ nhiều nơi lưu hơn A, nhưng (a) **7 nơi lưu vẫn lộ** do lỗi regex (`Cau hinh: password=X` bị nuốt thành giá trị của nhãn trước; `Authorization: Bearer X` chỉ che chữ "Bearer"),
    không che PEM, `aws_secret_access_key`, `SECRET_KEY`, `private_key`; và (b) **3 lỗi che sai làm hỏng dữ liệu/việc chạy**: mục tri thức có `API_TOKEN=…` mất dấu `---` và thân, lệnh kiểm chứng
    `TOKEN=$CI_TOKEN …` bị lưu thành `TOKEN=***` nên verify hỏng, lần mở đầu sau nâng cấp ghi đè credential trong profile iso (`primaryApiKey`, `experimental_bearer_token`).
    Medium: từ chối lệnh kiểm chứng thông dụng, profile rỗng làm mọi lệnh lỗi, hook trace > 1 MiB ghi bản thô, dấu nháy chưa đóng làm mất phần log sau (kể cả dòng FAIL), dòng JSON dính nhau, lời nhắc `knowledge review` không hiện.
- Cả hai bên không đạt mục tiêu (≥ 7 critical). B không hơn A ở chỉ tiêu cốt lõi (còn lộ bí mật) và còn gây thêm hồi quy.

## 4. Kiểm giả thuyết của các bản sửa (`72aa43c`, `b69839f`, `a8f2f95`, `9095646`)

| Giả thuyết | Kết quả |
|---|---|
| Yêu cầu mơ hồ không còn tự rơi vào đường nhanh (fast_path mặc định tắt) | **Có**: cả T1 và T2 đi `b-req → b-feat → b-review`; đường nhanh opt-in (`--fast`, `/b-do-fast`) **không được đo** trong lần này. |
| b-feat nối tiếp phiên b-req | **Có**: mọi b-feat đầu nối tiếp (notes ghi rõ). Nhưng tiết kiệm không thấy rõ: b-feat đầu vẫn 20 phút (T1) và 47 phút (T2). |
| b-req sinh kiểm tra mục tiêu chạy được | **Có**: cả hai `requirement.md` có mục "Kiểm tra mục tiêu" nêu tên test (`TestAutoCleanupStateGoal`…). Chưa thấy nó bắt được lỗi ở T2: cây cuối vẫn lộ ở 7 nơi lưu. |
| b-req không tự cắt scope G1/G2 (lỗi v4 của T2) | **Có ở mức kế hoạch**: 34 tệp, 8 nhóm bề mặt, không giai đoạn hoãn. **Không có ở mức kết quả**: b-feat đầu 47 phút rồi bị cắt trần; mã phủ rộng nhưng regex lỗi. |
| Cổng cỡ kế hoạch (`small_plan_max_files = 5`) | Cả hai kế hoạch là **lớn** (15 và 34 tệp) nên dừng chờ người: thêm một lần chạm người dùng nhưng không rút ngắn thời gian; nó chỉ báo, không giảm công việc. |
| Review không hội tụ bị chặn / giới hạn vòng sửa | T1: hết vòng sửa, `--fix-round` rồi hội tụ 0. T2: chưa tới lượt (cắt trần trước). |
| Chất lượng kết quả tốt hơn A | **Chỉ T1 hoà.** T2 vẫn A tốt hơn. |
| Chi phí ≤ 2,5× | **Không**: 4,94×. |

## 5. Lưu ý, thiên lệch

- Một lượt mỗi ô; B-T2 bị cắt bởi trần 90 phút giữa vòng sửa đầu tiên (chưa kiểm chứng xanh lần nào). Nếu có thời gian dài hơn (uncapped), kết quả T2 có thể khác: **không đo được** từ lần này.
- Reviewer là một agent (Opus) mỗi task cho cả hai bên; không có lượt chấm lặp. T2: các phát hiện "lộ bí mật" tính riêng theo nơi lưu nên số critical của B (10) cao hơn một phần vì B phủ nhiều nơi lưu hơn (đối chiếu với mức độ lộ).
- Người dùng giả là Opus chỉ thấy thẻ người dùng, đầu ra Bint và `requirement.md` (xem `log/user-actions.md`). Quy tắc 6.2 được áp: lần hết vòng sửa đầu tiên → `--fix-round`.
- Sự cố môi trường (`log/env-incidents.md`): đĩa `/` cạn tới 2,1 GB (nguồn không phải benchmark); đã dọn `tmp/` của v3/v4, `go clean -cache` hai lần (cache build chung, tái sinh được), xoá 32 image Docker không dùng (theo yêu cầu của chủ dự án — nhưng Docker ở ổ khác nên không giúp phân vùng `/`).
- Thay đổi ngoài lề lần này: `workflow` điều phối B không dùng binary `b-do` đã sửa cho **nhánh khác model** (chạy cùng model `gpt-6.1-sol`), nên bản sửa nối tiếp khác model chưa được đo.
- `~/.codex/config.toml` còn khối trust của v2, v3, v4, v5 — chờ chủ dự án quyết gỡ.
- Binary đóng băng ghi "sửa" trong phiên bản vì có một mục tri thức ứng viên chưa commit; mã nguồn đúng `9095646`.

## 6. Việc kế tiếp có thể làm
1. **T2: tách phần kiểm chứng khỏi phần viết.** Vấn đề lớn nhất là một b-feat 47 phút trên kế hoạch 34 tệp rồi mới chạy kiểm tra mục tiêu. Cho b-feat chạy kiểm tra mục tiêu giữa chừng (theo từng nhóm bề mặt) hoặc chia việc theo nhóm bề mặt thành nhiều phiên b-feat nhỏ, mỗi phiên có test riêng.
2. **Sửa quy trình đo chi phí:** token B-T2 gấp 3,5× lần 4 chỉ vì phủ hết kế hoạch; nên đặt trần token cho b-feat đầu và dừng sớm để xem các nhóm đã làm.
3. **Chạy thêm ≥ 2 lượt/ô, và một cấu hình không trần 90 phút** để biết B-T2 có tự xanh được không; hiện chênh T1 nằm trong dao động chấm.
4. Đo đường nhanh opt-in (`--fast`) cho T1 để biết giá trị của nhánh "gần như A + một lượt review".
5. Phần "lộ bí mật" của B-T2 là lỗi của chính mã `detect.Redact` mới (regex nuốt giá trị; danh sách khoá thiếu); các lỗi cụ thể trong `review/T2-verified.json` (Z1, Z3, Z5, Z7–Z13) đủ để làm phiếu sửa riêng.
