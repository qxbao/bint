# Kế hoạch: benchmark Bint so với không dùng Bint, đợt 6 (task dài hơn một ngữ cảnh, `gpt-6.2-sol`)

Trạng thái: **đã chạy 2026-11-20** (quyết định mục 9: commit 17812a0, trần ngữ cảnh 872k, máy Windows). Biên bản: [evals/bench-bint-vs-plain-v6-20261120/](evals/bench-bint-vs-plain-v6-20261120/README.md). Bản gọn: một task, hai lượt (A, B). Agent chạy kế hoạch này không phải
agent đã viết nó; đọc hết trước khi làm.

Tài liệu gốc:
- Tổng hợp đợt 1–5: [evals/bench-bint-vs-plain-tong-hop.md](evals/bench-bint-vs-plain-tong-hop.md).
- Plan lần 3: [benchmark-bint-vs-plain-v3-plan.md](benchmark-bint-vs-plain-v3-plan.md). Giữ nguyên: gỡ Bint cho bên A (plan lần 1 mục 3),
  người dùng low-tech và quy tắc lối ra (mục 6.1–6.2), review mù + xác minh mù, cách đo token, bẫy đã gặp (mục 10).
- Biên bản đợt 5 (thẻ người dùng, đặc tả rủi ro, phát hiện T2): [evals/bench-bint-vs-plain-v5-20261117/](evals/bench-bint-vs-plain-v5-20261117/README.md).

## 1. Vì sao có đợt 6

Đợt 1–5 đo task vừa một phiên (A xong trong ~20 phút). Ở cỡ đó các cơ chế của Bint (phiếu, kế hoạch lưu trên đĩa, điểm xanh, nối tiếp
phiên, lát mỏng) không có việc để làm, chỉ tốn token. Đợt 6 đo đúng chỗ chúng nhắm tới: **việc phải chia nhiều phiên, cần kế hoạch lưu
lại, cần tiếp tục sau khi bị cắt, chạm nhiều tệp.**

**Câu hỏi:** với cùng ngân sách phiên và cùng một lần bị cắt, Bint có làm xong nhiều hơn và đúng hơn Codex trực tiếp không, tốn thêm bao nhiêu?

Đây cũng là lần đầu đo trên tác vụ thật các cơ chế thêm sau đợt 5: b-goal, lát mỏng chốt một chiều, điểm xanh lát.

## 2. Khác đợt 3–5

| | Đợt 3–5 | Đợt 6 |
|---|---|---|
| Model | `gpt-6.1-sol` | **`gpt-6.2-sol`** (qua codex-lb), mọi bước cả hai bên, kể cả b-goal |
| Commit gốc | `8757340` / `9095646` | `17812a0` (`main`), cả hai bên |
| Task | T1 + T2, vừa một phiên | **Một task dài: L1** (mục 3) |
| Một lượt | Một phiên liền, trần 90 phút | **Tối đa 4 phiên × 30 phút**, có **một lần cắt cứng** (mục 5) |
| Số lượt | 4 (A/B × T1/T2) | **2** (A-L1, B-L1) |
| Chấm chính | Critical/medium gộp test ẩn + review; mỗi nơi lưu một ô nhị phân | **Test ẩn, ma trận loại bí mật × nơi lưu** (mục 6.1); review mù chỉ tìm lỗi chặn |

## 3. Task L1: không lưu bí mật ở bất kỳ nơi nào

T2 của đợt 2–5, giao như một việc dài. Đợt 5 cho thấy làm đủ cần ~34 tệp, 8 nhóm nơi lưu; một b-feat 47 phút vẫn chưa xanh. Dùng lại
được test ẩn `hidden2.py` nên không phải viết test mới.

- **Prompt (cả hai bên):** "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi. Mọi chỗ bint lưu đều phải sạch,
  việc này lớn nên cứ làm từ từ qua nhiều lần cũng được."
- **Thẻ người dùng:** thẻ T2 ở `evals/bench-bint-vs-plain-v5-20261117/log/persona-cards.md`, thêm: "Hỏi về làm tiếp: cứ làm tiếp chỗ đang
  dở, tôi không nhớ chi tiết lần trước."
- **Kiểm trước khi chạy:** ở `17812a0`, `detect.Redact` vẫn chỉ áp ở hook/MCP; các nơi lưu S1–S5 còn thô. Nếu đã che thì dừng, báo chủ dự án.

## 4. Chuẩn bị

1. Workspace như lần 3 (`bin/`, `tools/`, `hidden/`, `log/`, `results/`, `review/`, `tmp/`, `codex-home/`), hai repo `A-L1`, `B-L1` clone ở
   `17812a0`. Bên A gỡ Bint; bên B `bint init --hooks` bằng binary đóng băng (ghi sha256), index codegraph. `go test ./...` xanh trên cả hai.
2. **`CODEX_HOME` riêng** cho cả hai bên: `model = "gpt-6.2-sol"`, `model_reasoning_effort = "high"`, khối provider codex-lb chép từ cấu hình
   người dùng (**không in token ra log/biên bản**), trust hai repo và hook của B, trần ngữ cảnh theo 9.2. Smoke một câu vô hại, kiểm rollout
   ghi `gpt-6.2-sol`.
3. **B:** `.bint/local.yml` ghim `harnesses.codex.models.{planner,implementer,reviewer}: gpt-6.2-sol`; mọi khoá workflow giữ mặc định của commit gốc.
4. Script và test ẩn (`hidden2.py`, `hidden/controls/`, `human`, `tokens.py`, `mkdiff.py`, …) lấy từ workspace Linux `/home/baodq/bench-bint-v3-*/`
   (không có trong repo). Thêm `segment.sh` (một phiên có trần, giết cả cây tiến trình) và `snapshot.sh` (chụp cây sau mỗi phiên, không đụng
   index của repo).
5. Đối chứng âm trên binary gốc: S1–S5 lộ, N1/N2 qua (như lần 3 mục 4.7). Từ kết quả này dựng và chốt ma trận ô (mục 6.1,
   `results/controls/matrix-base.json`); viết `tools/matrix.py` và kiểm nó cho đúng 12/33 và 22/33 với ma trận 33 ô đợt 3 trên
   `docs/evals/bench-bint-vs-plain-v3-20261117/results/A-T2.task.json` (A) và `…-v5-20261117/results/B-T2/task.txt` (B; đợt 5 không lưu
   `task.json`, dòng S1–S6 của tệp này không bị cắt). Đưa chủ dự án xem trước khi chạy agent.

## 5. Giao thức

- **Phiên:** A là một `codex exec`; B là một `bint run b-do` / `bint run resume`. Trần **30 phút tường** mỗi phiên (thời gian chờ người dùng
  giả không tính), **tối đa 4 phiên**. Hết giờ: SIGINT, sau 30 giây giết cả cây. Dừng sớm khi agent báo xong và người thử dùng không thấy lỗi.
- **Cắt cứng:** phút 15 của phiên 2, giết cả cây không báo trước (mô phỏng mất điện), như nhau cho hai bên.
- **Phiên kế:** A mở phiên Codex **mới** (không `resume`) với lời nhắn cố định: "Lần trước bạn đang làm dở việc tôi nhờ: «<prompt>». Làm tiếp
  cho xong, đừng làm lại phần đã xong." B chạy lệnh Bint in ra, không có thì `bint run resume <id>`; không `--fresh`/`--rerun`.
- **Người dùng:** như lần 3 mục 6.1–6.2. Thêm: cổng kế hoạch lớn → chạy `resume` Bint in ra; lát bị giữ lần đầu → `--slice more`, lần hai →
  `--slice discard`. Trần 6 tin nhắn cả lượt (lời nhắn nối tiếp của A không tính).

## 6. Cách đo

### 6.1. Điểm chính: ma trận loại bí mật × nơi lưu (test ẩn)

Đợt 1–5 chấm mỗi nơi lưu S1–S6 là một ô nhị phân: còn lộ **một** loại bí mật cũng là critical như lộ tất cả. Cách đó che mất tiến bộ thật.
Ví dụ: B đợt 5 đã hết lộ `github_token` và `aws_key_id` ở `task.yml` (S3) và review (S4), trong khi A đợt 3 vẫn lộ, nhưng cả hai đều bị tính
critical ở S3, S4 như nhau. Đợt 6 lấy **test ẩn làm điểm chính** và chấm từng ô **(loại bí mật, nơi lưu)**:

- **Ô của ma trận** = mọi cặp (loại, nơi lưu) mà **đối chứng âm trên binary gốc** cho là lộ (fixture đã cài loại đó vào nơi đó). Bảy loại của
  `hidden2.py`: `github_token`, `aws_key_id`, `aws_secret`, `bearer`, `url_creds`, `password`, `private_key`. Với fixture đợt 3, binary gốc
  lộ 33 ô: S1 (log kiểm chứng) 7, S2 (log agent) 7, S3 (mô tả phiếu) 6, S4 (review) 4, S5 (sự kiện hook/SQLite) 3, S6 (tài liệu phiếu) 6.
  Ma trận đợt 6 lấy từ đối chứng âm của chính đợt 6 (mục 4.5), chốt **trước** khi chạy agent, ghi `results/controls/matrix-base.json`.
- **Một ô sạch** khi loại đó không xuất hiện thô ở **bất kỳ tệp nào** của nơi lưu đó (cả `.bint/tasks/` trong repo lẫn thư mục trạng thái).
  Một ô lộ ở một tệp là lộ.
- **Điểm** = số ô sạch / tổng số ô (%). Đọc từ trường `detail` của `task.json` (`<loại>@<tệp>,…; <loại>@…`; không dùng `task.txt` vì dòng bị
  cắt). Script `tools/matrix.py <task.json> <matrix-base.json>` in ma trận và điểm.
- Mọi ô cùng trọng số. Báo kèm điểm theo từng nơi lưu và từng loại để thấy lộ còn tập trung ở đâu.

Áp lại cho dữ liệu cũ (cùng fixture đợt 3, 33 ô) để minh hoạ: **A đợt 3 sạch 12/33 (36 %)**, **B đợt 5 sạch 22/33 (67 %)** — trong khi bảng
nhị phân cũ ghi cả hai "S1–S5 critical" và kết luận A tốt hơn. (B đợt 5 còn lộ `private_key` ở S3/S4/S6, là ô fixture đợt 3 không cài, nên
không tính vào 33 ô; fixture đợt 5 có thể đã đổi — đó là lý do ma trận phải lấy từ đối chứng âm của chính đợt chạy.)

### 6.2. Điều kiện chặn (không cộng điểm, chỉ chặn kết luận "tốt hơn")

Che mà làm hỏng thứ khác thì không được tính là tiến bộ. Mỗi điều sau là một **lỗi chặn** của bên đó:
- **N1** che quá tay (lệnh kiểm chứng, hash, ID, giá trị `key=value` thường bị đổi) — test ẩn.
- **N2** hiệu năng: verify log 20 MB chậm quá 3× binary gốc (3 lần xen kẽ khi máy rảnh, trung vị) — test ẩn, chỉ ở cây cuối.
- Test repo đỏ (build, vet, `GOOS=windows go vet`, staticcheck Linux/Windows, `go test ./...`).
- **Critical đã xác minh từ review mù không phải "còn lộ"**: hỏng dữ liệu hoặc việc chạy (ba lỗi kiểu B đợt 5: mục tri thức mất thân, lệnh
  `TOKEN=$CI_TOKEN` thành `TOKEN=***`, ghi đè credential profile iso), bắt người dùng tự bật. Review mù + xác minh mù vẫn chạy (một cặp, như
  lần 3 mục 7) nhưng **chỉ dùng cho mục này**; phát hiện "còn lộ ở nơi X" của reviewer không tính điểm vì ma trận đã đo.

### 6.3. Việc dài và phụ

- **Đường tiến độ:** điểm ma trận sau mỗi phiên (chạy S1–S6 trên snapshot). Ô đã sạch mà phiên sau lộ lại (mất việc); cây sau cắt cứng có
  biên dịch được không; token và thời gian từ đầu phiên kế tới lần sửa tệp đầu tiên.
- **Phụ:** token vào/ra, thời gian, tin nhắn/quyết định/kẹt. Với B: có chia lát và chạy b-goal không, số lát chốt/bỏ, resume sau cắt có
  tiếp đúng lát dở không.

## 7. Đọc kết quả (chốt trước khi chạy)

| Kết luận | Điều kiện |
|---|---|
| B tốt hơn | Điểm ma trận cuối của B cao hơn A **≥ 15 điểm %** và B **không có lỗi chặn** mà A không có |
| A tốt hơn | Ngược lại với cùng ngưỡng |
| Hoà | Còn lại (kể cả khi bên điểm cao hơn có lỗi chặn mà bên kia không có — ghi rõ lỗi chặn nào) |

15 điểm % ≈ 5 ô trên ~33: test ẩn chạy xác định trên một cây nên không có nhiễu người chấm, chỉ còn dao động giữa các lượt agent (một lượt mỗi
bên). Kèm tỷ lệ token vào B/A; quá 2,5× thì "B tốt hơn" ghi là "có lợi, đánh đổi", kèm **token trên mỗi ô sạch thêm so với binary gốc**.
Kết quả vẫn là tín hiệu một lượt, không phải kết luận chắc.

## 8. Rủi ro

- Một lượt mỗi bên: dao động giữa các lượt agent lớn (tổng hợp mục 2). Ngưỡng 15 điểm % (~5 ô) để vài ô lật không đổi kết luận.
- Ma trận cùng trọng số: lộ `private_key` và lộ `url_creds` bằng nhau. Báo kèm điểm từng loại để người đọc tự cân.
- Lời nhắn nối tiếp "phiên mới" của A là lựa chọn thiết kế; người dùng thật có thể `codex resume`.
- Các lỗi che bí mật của đợt 5 đã công khai trong `docs/evals/`; cả hai bên đọc được như nhau.
- Windows: `go test ./...` ~10 phút có thể ăn phần lớn phiên 30 phút của B (tri thức `01M7CJ92ZB2DRYYHGFBQABYM9D`); xem 9.3.

## 9. Chờ chủ dự án quyết

1. Commit gốc `17812a0`.
2. Trần ngữ cảnh chung: 200k (đề xuất, để việc thật sự tràn) hay giữ 872k như cấu hình thật.
3. Máy: Linux (có sẵn script, test ẩn) hay Windows máy này (phải chép test ẩn, kiểm chứng chậm).
4. Biên bản ở `docs/evals/bench-bint-vs-plain-v6-<ngày>/`; không commit/push trừ khi chủ dự án bảo.

## 10. Ước tính

Khoảng 2–3 giờ tường (chuẩn bị ~45 phút, hai lượt song song ≤ 2 giờ, chấm + biên bản ~45 phút). Token Codex khoảng 80–130M vào (A ~20–30M,
B ~60–100M, phần lớn cache); phần Claude ~$15–25.
