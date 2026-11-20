# Benchmark Bint vs không Bint — đợt 6 (task dài hơn một ngữ cảnh, `gpt-6.2-sol`, Windows)

Ngày 2026-11-20. Kế hoạch: [../../benchmark-bint-vs-plain-v6-plan.md](../../benchmark-bint-vs-plain-v6-plan.md) (bảng kết luận mục 7 chốt trước khi chạy).
Máy: Windows 11 (máy của chủ dự án), workspace `D:\bench\bench-bint-v6-20261120\` (repo, diff đầy đủ, test ẩn, rollout Codex nằm ở đó, không chép vào repo).

## 1. Kết luận

**Theo bảng chốt: Hoà.** Điểm ma trận chênh 5,5 điểm % (< 15), và cả hai bên đều có lỗi chặn. Chi phí B/A = **2,41×** token vào (dưới ngưỡng 2,5×).

| | A (Codex trực tiếp) | B (`bint run b-do`) |
|---|---|---|
| Điểm ma trận cuối (ô sạch / 36) | **22/36 (61,1 %)** | 20/36 (55,6 %) |
| Chỗ còn lộ | Bỏ trọn 2 nơi lưu: mô tả phiếu (S3), tài liệu phiếu (S6), mọi loại | Phủ mọi nơi lưu nhưng sót 3 loại ở mọi nơi: `aws_key_id`, `aws_secret`, `private_key` |
| N1 (không che quá tay) | Qua | Qua |
| N2 (verify log 20 MB, ngưỡng 3×) | **Trượt: 20,1×** (17,9 s vs 0,89 s) | **Trượt: 5,5×** (4,7 s vs 0,86 s) |
| Test repo (build, vet, staticcheck Linux+Windows) | Qua | Qua |
| `go test ./...` | Qua (2 test đỏ ở lượt đầu, chạy lại qua: chập chờn Windows) | **Đỏ thật 3 test**: `.env` trong worktree bị che, shim MCP trả danh sách tool rỗng, ghi tệp ngoài `repoWriter` |
| Critical đã xác minh (review mù, ngoài "còn lộ") | **0** (3 medium, 1 low) | **3** (Y1–Y3) + 6 medium |
| Kết thúc | Agent báo xong sau 3 phiên (≈ 84 phút agent) | Hết 4 phiên (120 phút), dừng giữa vòng sửa, 10 phát hiện high còn mở, **chưa từng có điểm xanh** |
| Tin nhắn / quyết định của người dùng giả | 0 / 0 | 1 / 1 (cổng kế hoạch lớn) |
| Token vào (cache) | 37,4M (36,5M) | 90,0M (88,5M) |

Đọc thẳng: theo luật thì hoà, nhưng hai bên hỏng theo hai kiểu khác nhau. **A** chậm thảm hoạ ở log lớn và bỏ sót trọn hai nơi lưu.
**B** che đủ nơi hơn nhưng che quá rộng, làm hỏng dữ liệu và luồng (3 critical, test repo đỏ thật), và không kịp sửa trong ngân sách.
Đợt này **không** cho thấy cơ chế việc dài của Bint giúp kết quả cuối tốt hơn.

Độ tin cậy: thấp. Một lượt mỗi bên. Điểm ma trận xác định trên một cây, nhưng dao động giữa các lượt agent thì chưa đo.

## 2. Đường tiến độ (test ẩn sau mỗi phiên)

| Mốc | A | B |
|---|---|---|
| Sau phiên 1 (30 phút) | 15/36 — S1, S2, S5 sạch | 8/36 — b-req 5m29s, cổng kế hoạch lớn, b-goal 19m12s, b-feat 4m25s |
| Ngay sau cắt cứng (phút 15 phiên 2) | 15/36, biên dịch được | **20/36**, biên dịch được |
| Hết phiên 2 | 22/36 (+S4) | 20/36 |
| Hết phiên 3 | 22/36 — agent báo xong, thử dùng không lỗi → dừng | 20/36 — b-feat hỏi mở rộng phạm vi (tin nhắn #1) |
| Hết phiên 4 | — | 20/36 — b-feat 3m47s, verify ≈ 15 phút, b-review 8m25s (10 high), sửa 2m38s rồi hết giờ |

- **Mất việc sau cắt:** không bên nào. Ô đã sạch không lộ lại, cây biên dịch được ngay sau lần giết cứng.
- **Nối tiếp sau cắt (B):** `bint run resume` nối tiếp đúng phiên b-feat ("lớp dừng external, nối tiếp được, đã nối tiếp 2 lần"). Không chạy lại b-req/b-goal.
  Nhưng 15 phút sau cắt và cả phiên 3 không thêm ô sạch nào: B giữ nguyên 20/36 từ phút 45 tới hết.
- **A làm tiếp bằng phiên mới** với lời nhắn cố định. Phiên 2 tự tìm lại chỗ dở và thêm S4, rồi tự kết luận xong ở phiên 3, dù S3/S6 vẫn lộ.

## 3. Lỗi chặn đã xác minh (review mù + xác minh mù, ánh xạ X = A, Y = B, mở sau khi xác minh)

**B (Y), critical:**
- **Y1:** `workflow.worktree.copy` che nội dung tệp khi chép vào worktree. `.env` thành `TOKEN=REDACTED`, YAML `auth: false` đổi kiểu. Test repo `TestWorktreeBaseHasDirtyStateAndCopies` đỏ đúng chỗ này.
- **Y2:** lệnh kiểm chứng hợp lệ (`Bearer ${API_TOKEN}`, `$(gh auth token)`, `${X:-d}`) bị coi là bí mật. Phiếu mới bị từ chối; phiếu cũ âm thầm mất `verify.command` và phê duyệt.
- **Y3:** `bint scrub --apply` ghi đè `.mcp.json`, profile iso, `mcp.yml` của máy mà không sao lưu. `"Bearer ${GITHUB_PAT}"` thành `"Bearer REDACTED}"`.
- **Medium:** Y4 gateway MCP sửa kết quả công cụ trả agent (hạ từ critical), Y5–Y10 (ghi cấu hình che giá trị, tài liệu phiếu/tri thức bị đổi, `gateway.json` mất token, profile mất refresh token, che trên tệp nhị phân, bản sao lưu đã che).

**A (X):** không critical.
- Medium: X1 che theo chuỗi con giá trị biến môi trường làm hỏng khoá sự kiện/đường dẫn khi giá trị yếu (`DB_PASSWORD=test`). X2 `runs scrub` không khoá, không sao lưu. X5 dòng JSON/nháy chưa đóng nuốt toàn bộ log phía sau, kể cả `--- FAIL`.
- Low: X4 (hạ từ medium) và X6–X11, trong đó X10: dữ liệu cũ chỉ sạch khi người dùng tự chạy `bint runs scrub`.

Chi tiết: `review/X-*.json`, `review/Y-*.json`.

## 4. Kiểm giả thuyết của Bint

| Giả thuyết | Kết quả |
|---|---|
| H1: b-req chia lát và chạy b-goal | **b-goal có** (19 phút, đóng băng `internal/cli/secret_persistence_e2e_test.go`). **Không chia lát:** b-req đọc `.bint/config.yml` của repo (`slice_timeout_minutes: 0`), không thấy giá trị 30 ở `.bint/local.yml`, nên ghi "Chia lát: không". Cơ chế lát mỏng **không được đo**. |
| H2: lát chốt một chiều | Không áp dụng (không có lát). `last_green` = None suốt lượt: kiểm chứng duy nhất (`go test` ≈ 15 phút) đỏ. |
| H3: resume sau cắt cứng tiếp đúng chỗ | **Có**: nối tiếp phiên b-feat, không làm lại b-req/b-goal. Bản ghi lượt bị giết vẫn ở trạng thái `running` trong `bint run status` (lỗi nhỏ của Bint). |
| H4: nối tiếp phiên rẻ hơn khởi động lại | Có nối tiếp (external, awaiting_human, review_fix). Không đổi thành tiến độ: 0 ô sạch thêm sau phút 45. |
| H5: test đóng băng của b-goal bắt lỗi test của b-feat bỏ sót | Chưa kết luận được: chưa có lần kiểm chứng xanh, nên cổng mục tiêu chưa chạy trên cây cuối. |
| Review của Bint | b-review vòng 1 tìm 10 high, trùng phần lớn với lỗi chặn của review mù (worktree `.env`, repoWriter, YAML). **Bint bắt được lỗi nhưng hết ngân sách trước khi sửa.** |

## 5. Vì sao B chậm: thời gian từng bước (120 phút agent)

b-req 5,5 phút · cổng kế hoạch lớn (dừng chờ người) · b-goal 19 phút · b-feat cộng dồn ≈ 55 phút (5 lần, 2 lần bị cắt) · kiểm chứng scope `affected` ≈ 15 phút (60 tệp đổi trong 18 gói, nới ra 42 gói; gói chậm nhất `internal/workflow` 852 s)
· b-review 8,5 phút · sửa 2,6 phút. Hơn 1/3 thời gian của B đi vào b-goal và kiểm chứng toàn repo trước khi có vòng sửa đầu.
A không có b-goal hay cổng nào, tự chạy test hẹp.

## 6. Sự cố và thiên lệch

- **Lượt 1 bị huỷ vì nhiễm đáp án** (`log/aborted-1.md`). Repo ở `17812a0` còn `docs/evals/bench-bint-vs-plain-*` và các plan benchmark cũ (đặc tả rủi ro, lỗi che bí mật
  đợt 5, mô tả fixture test ẩn). Rollout của A đọc chúng hơn 2 000 lần, B cũng đọc; A đạt 36/36 sau một phiên.
  Lượt chính chạy trên commit `48dbfad` = `17812a0` + gỡ các tài liệu đó (cả hai bên). Đã kiểm rollout lượt chính: không đọc.
- **Lượt 2 bị huỷ sau 1 phút** (`log/aborted-2.md`): hai `CODEX_HOME` cùng đặt lại mật khẩu tài khoản sandbox Windows `CodexSandboxOffline` → bị khoá 10 phút
  (`CreateProcessWithLogonW 1909`). Sửa bằng cách cho hai bên (và `~/.codex` của máy) dùng chung `.sandbox-secrets/sandbox_users.json`; bản cũ sao lưu ở `tmp/sbx-backup/`.
- **Test ẩn viết lại cho Windows** (`hidden_l1.py`, chỉ ở workspace, không đưa vào repo để khỏi thành đáp án). Không so được số tuyệt đối với `hidden2.py` của đợt 2–5.
  Ma trận đợt 6: 36 ô (S1–S4, S6: 7 loại; S5: chỉ `private_key`, vì hook gốc đã che 6 loại kia).
- **Cấu hình B:** `.bint/config.yml` của repo đặt `slice_timeout_minutes: 0`. Tôi ghi đè bằng 30 trong `.bint/local.yml`, nhưng b-req vẫn chọn không chia lát (xem H1).
  Muốn đo lát mỏng thì phải đổi `.bint/config.yml` của chính repo.
- **Skill b-req** (mã nguồn Bint, có ở cả hai repo) có câu ví dụ chung "không lưu bí mật — cài bí mật giả có dấu dễ grep vào mọi đường nhập". Giữ nguyên.
- Lần thử dùng của A in "repository chưa được khởi tạo Bint". Lỗi này do bên A đã gỡ Bint khỏi repo, nên coi là không lỗi.
- Cắt cứng ở phút 15 phiên 2. 15 phút còn lại của phiên 2 chạy như một lần làm tiếp: A mở phiên mới, B `resume`.
- Windows: `go test ./...` ≈ 10–15 phút, phần lớn thời gian chờ là spawn git và Defender. Kiểm chứng của B đã dùng scope `affected` nhưng diff chạm trực tiếp các gói chậm nhất, nên vẫn ≈ 15 phút.

## 7. Gợi ý cho đợt sau

1. Gỡ tài liệu benchmark khỏi cây trước khi clone. Nên có script kiểm rollout tìm tên tệp đáp án ngay sau phút đầu.
2. Muốn đo lát mỏng thì đặt `slice_timeout_minutes` trong `.bint/config.yml` của repo, không đặt ở `local.yml`; đồng thời cân nhắc cho b-req đọc cấu hình đã hợp nhất.
3. L1 với `gpt-6.2-sol` chưa đủ dài để A tràn ngữ cảnh: A tự kết thúc sau 84 phút, không cần nhớ gì giữa các phiên ngoài cây làm việc.
   Cần task lớn hơn hẳn, ví dụ L2 (giao diện tiếng Anh trọn vẹn trong plan bản đầu), hoặc ngân sách phiên ngắn hơn.
4. Kiểm chứng của B đã dùng phạm vi `affected`; thời gian do diff rộng chạm gói chậm, không do phạm vi kiểm chứng. Giảm bằng cách giữ phạm vi sửa hẹp (b-req), hoặc chạy benchmark trên Linux.
