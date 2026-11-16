# Kế hoạch: vòng chạy dự đoán được và dừng sớm khi không tiến triển

Trạng thái: **bản đề xuất, chưa có phiếu `b-req`** (2026-11-16). Tài liệu đầu vào cho các phiếu theo giai đoạn; mỗi giai đoạn
được chọn thì lập phiếu riêng. Tên lệnh, khoá cấu hình và tên trường dưới đây là phác thảo.

Bối cảnh: [workflows.md](workflows.md) (vòng sửa, giới hạn vận hành), [step-continuation-plan.md](step-continuation-plan.md)
(phân loại lý do dừng), [next-directions.md](next-directions.md) (hướng chiến lược), [acceptance.md](acceptance.md)
(nơi duy nhất ghi nghiệm thu; mã `NT-…` của kế hoạch này được thêm vào đó khi lập phiếu).

## Vấn đề

Một lượt `bint run b-do` hôm nay có ba điểm khó đoán trước:

1. **Không biết trước một phiếu sẽ chạy thế nào.** `bint flow show` và `bint flow lint` chỉ kiểm định nghĩa flow. Không
   lệnh nào in kế hoạch đã giải xong cho một phiếu cụ thể: role nào chạy harness/mô hình nào, quyền MCP, ngân sách chia cho
   từng bước, và những điều sẽ chặn lượt chạy (lệnh kiểm chứng chưa xác nhận, đường dẫn bảo vệ, thiếu hook). Người chỉ biết khi
   lượt chạy đã dừng. Cũng không có chỗ nào nói "một bước loại này thường tốn bao nhiêu" dù Bint đã ghi nhận chi phí và thời
   gian của mọi lượt chạy.
2. **Vòng sửa chỉ bị chặn bằng số đếm.** `reviewDecision` và `fixAfterFullVerify` (`internal/workflow/run.go`) cho phép tối đa
   `workflow.max_review_rounds` vòng (mặc định 2), không xét vòng sửa trước có làm gì không. Một vòng sửa không đổi cây làm
   việc, hoặc để lại đúng các phát hiện và đúng đầu ra kiểm chứng của vòng trước, vẫn tiêu một vòng và tốn tiền như vòng có
   tiến triển. Hiện chưa có số đo nào cho biết chuyện này xảy ra thường hay hiếm.
3. **Khối quy tắc xuất bản vào `AGENTS.md` chỉ bị giới hạn theo số mục** (`RenderOptions.MaxItems`, mặc định 15; cắt thân mục
   theo `MaxBodyLines`), không theo kích thước. Ngữ cảnh luôn bật càng dài thì mọi lượt chạy càng tốn, và kết quả đo của Bint
   ghi nhận tổng quan dài không giúp agent (xem mục 2.3 của next-directions.md).

## Mục tiêu

- Cho người (và `/b-add-flow`) **xem trước** một lượt chạy mà không tốn token, kể cả các điều sẽ chặn nó và khoảng chi phí/thời
  gian ước từ lịch sử của chính repository.
- Nhận biết **vòng sửa không tiến triển** bằng hàm thuần xác định, **đo trước trên dữ liệu có sẵn**, rồi mới (nếu số đo cho
  phép) cho phép dừng sớm.
- Giữ khối xuất bản `AGENTS.md` trong một ngân sách kích thước có thể thấy được.
- Không thêm lời gọi mô hình, không thêm điểm dừng chờ người, không đổi hành vi của lượt chạy hiện có.

## Nguyên tắc ràng buộc (không thương lượng)

Các nguyên tắc này lấy từ đặc tả; mỗi giai đoạn phải qua cả sáu:

1. **Đo trước, bật sau.** Hành vi mới chỉ được bật mặc định khi có số đo đối chiếu với ngưỡng đã chốt từ trước (như
   [gate-1.md](gate-1.md): không đổi ngưỡng sau khi thấy kết quả).
2. **Người duyệt.** Không giai đoạn nào tự ghi cấu hình, tự áp dụng gợi ý hay tự mở lại tác vụ.
3. **Hook không gọi mô hình và không chặn agent.** Mọi tính toán mới chạy trong lệnh CLI hoặc Runner, không trong hook.
4. **Không bịa số.** Thiếu dữ liệu thì `unknown`/`null` kèm số mẫu, không thay bằng 0 hay giá trị mặc định.
5. **Workflow tối thiểu.** Không thêm kind bước, không thêm bước bắt buộc; thêm khoá cấu hình ít nhất có thể.
6. **Một binary Go, ba nền tảng.** Mã theo nền tảng tách `*_unix.go` / `*_windows.go`; qua `go vet` và `staticcheck` với
   `GOOS=windows`.

## Thứ tự và phụ thuộc

| Giai đoạn | Nội dung | Công sức | Tốn token đo | Ảnh hưởng lượt chạy hiện có |
|---|---|---|---|---|
| GĐ1 | Ước lượng từ lịch sử + `bint flow simulate` + gợi ý `max_turns` | M | Không | Không (lệnh chỉ đọc) |
| GĐ2a | Phân tích offline "không tiến triển" trên phiếu đã có | S–M | Không | Không |
| GĐ2b | Chế độ bóng trong Runner (chỉ ghi nhận) | S | Không | Không (mặc định tắt) |
| GĐ2c | Chế độ dừng sớm | S | Không | Chỉ khi người bật và đã đạt ngưỡng |
| GĐ3 | Trần kích thước khối xuất bản (chỉ cảnh báo) | S | Không | Không |
| GĐ4 | Thử nghiệm effort bằng khung đánh giá (tuỳ chọn, có điều kiện) | M | Có, có trần | Không |

GĐ1 và GĐ3 độc lập nhau và độc lập GĐ2. GĐ2c phụ thuộc GĐ2a và GĐ2b; GĐ4 chỉ chạy khi GĐ2 đã cho thấy dữ liệu và có
ngân sách đo.

---

## GĐ1 — Ước lượng từ lịch sử và `bint flow simulate`

### Khái niệm

**Một bản "chạy thử khô"**: Bint giải toàn bộ cấu hình ảnh hưởng tới một phiếu và flow của nó, kiểm các điều kiện sẽ chặn, và ước
chi phí/thời gian từ lịch sử, rồi in ra. Không gọi harness, không tạo worktree, không đổi phiếu.

### Đầu ra

`bint flow simulate <tên flow> [--task <id>] [--json]`. Không `--task` thì mô phỏng theo cấu hình hiện hành với phiếu giả định
(không có phạm vi/lệnh kiểm chứng cụ thể thì các mục phụ thuộc phiếu ghi "cần phiếu").

1. **Dãy bước**: id, kind, skill, role, bước nào tuỳ chọn, bước `fix` của review, bước `expect: fail`.
2. **Giải cấu hình theo role**: harness và mô hình cho từng bước (dùng lại `harness.ForRole`), quyền MCP (`config.MCP.Resolve` theo
   role và chế độ chỉ đọc), công cụ cho phép/chỉ đọc.
3. **Ngân sách**: trần chi phí và số lượt của flow, phần chia cho từng bước theo quy tắc hiện có (nhỏ hơn giữa giới hạn của bước
   và phần còn lại), số vòng sửa tối đa, thời gian tối đa mỗi bước. Harness không báo USD thì ghi rõ "trần chi phí chỉ là chỉ dẫn".
4. **Điều sẽ chặn** (phần giá trị nhất, vì biến "dừng bất ngờ" thành "biết trước"): mỗi mục một dòng, nêu lệnh xử lý:
   - lệnh kiểm chứng của phiếu chưa có hoặc chưa xác nhận (khi `security.require_approval_for_commands` bật);
   - `scope.allow/deny` giao với `limits.protected_paths` (sẽ chờ `task approve`);
   - skill của bước không tồn tại, harness của role chưa bật hoặc chưa cài hook (`bint doctor` đã có kiểm tương ứng);
   - bước `expect: fail` mà phiếu thiếu lệnh kiểm chứng;
   - flow tuỳ biến đổi băm so với băm gắn vào phiếu.
5. **Ước lượng** (xem dưới): chi phí và thời gian theo bước, và tổng.
6. **Gợi ý `max_turns`** (chỉ hiển thị): xem dưới.

Mã thoát: 0 nếu không có điều chặn; 1 nếu có ít nhất một điều chặn chắc chắn (để CI dùng được); lỗi dùng sai theo quy ước
hiện có. Đầu ra `--json` có trường ổn định cho TUI về sau (TUI nằm ngoài phạm vi giai đoạn này).

### Cách ước lượng

- **Nguồn**: bảng `runs` (`duration_ms`, `cost_usd`, token, `harness`, `model`) nối với `run_steps` để biết tên bước. *Việc đầu
  tiên của giai đoạn là xác nhận cách nối* `runs.parent_run_id` ↔ `run_steps.run_id` cho lượt con của workflow, và những cột nào
  thật sự có giá trị với từng harness (Codex và Antigravity không báo USD).
- **Thống kê**: **trung vị và phân vị 80**, không dùng trung bình, vì thời gian và chi phí có đuôi phải dài. Báo khoảng
  `[trung vị, p80]`, không báo một số.
- **Nhóm so sánh**, thử từ hẹp đến rộng và dừng ở nhóm đầu tiên đủ mẫu: (bước, harness, mô hình) → (bước, harness) → (kind
  của bước). Luôn in nhóm đã dùng và số mẫu `n`.
- **Số mẫu tối thiểu** là hằng số trong mã (đề xuất 5), không phải khoá cấu hình. Không nhóm nào đủ thì in `unknown (n=…)`.
- **Giá trị `NULL`**: loại khỏi thống kê chi phí và in số mẫu có giá trị so với tổng ("chi phí: 4/9 lượt có USD"). Tổng của flow
  chỉ cộng các phần đã biết và ghi "một phần", đúng như `cost_usd_partial` hiện có.
- **Lọc**: chỉ lượt `completed`; lượt bị `interrupted` hay `cancelled` làm sai thời gian. Tách lượt nối tiếp phiên
  (`step-continuation`) khỏi lượt thường, vì chi phí của chúng tính cộng dồn theo cách khác.
- **Không dùng tiên nghiệm bịa**: thiếu lịch sử thì in `unknown`, không điền số mặc định.

### Gợi ý `max_turns`

`limits.workflows.<wf>.max_turns` mặc định `null` và chỉ có tác dụng trên Claude Code. Khi có lịch sử, in dòng gợi ý
`⌈1,5 × p90 số lượt⌉ (n=…)` cho bước đó. Quy tắc:

- chỉ gợi ý khi `n` đạt tối thiểu; chỉ cho harness có cờ giới hạn lượt;
- **không bao giờ tự ghi** vào `config.yml`; chỉ in, người tự đặt;
- ghi cảnh báo trong chính dòng gợi ý: đặt quá thấp biến thành bước bị cắt giữa chừng.

### Kiểm thử

- Bảng kiểm thử cho hàm thống kê thuần (trung vị, p80, nhóm dự phòng, `NULL`, mẫu thiếu).
- Golden cho đầu ra văn bản và `--json` của flow gốc và một flow tuỳ biến.
- Kiểm tra **không tác dụng phụ**: sau `simulate`, `state.json`, worktree và `.bint/` không đổi (so băm thư mục).
- Từng điều chặn có một ca dương và một ca âm.

### Ràng buộc theo quy ước của repository

Lệnh mới cần hàng trong `cliRefs` (`internal/tui/actions/cliref.go`) và cờ rời vào `litsrUsage` của `cliref_inventory_test.go`
(`TestServiceMessageCLIInventory`). Thông điệp qua i18n vi/en. Chưa có khoá cấu hình mới ở giai đoạn này.

### Nghiệm thu

`NT-SIM-STATIC` (đầu ra đúng với flow gốc và flow tuỳ biến), `NT-SIM-NOSIDE` (không tác dụng phụ), `NT-SIM-PREDICT` (so dự đoán
điều chặn với kết quả thật trên ít nhất 10 phiếu: mọi điều chặn đã in phải xảy ra khi chạy, và mọi lần chặn thật có điều kiện
tĩnh phải được in), `NT-SIM-CALIB` (so khoảng ước lượng với giá trị thật; báo tỷ lệ nằm trong `[trung vị, p80]`, chỉ báo, không
là ngưỡng đạt/hỏng).

---

## GĐ2 — Phát hiện vòng sửa không tiến triển

### Khái niệm

Vòng sửa **không tiến triển** khi vòng vừa rồi không thay đổi điều gì đo được so với vòng trước. Có ba tín hiệu, đều tính từ dữ
liệu Bint đã có và đều xác định (không mô hình, không ngẫu nhiên):

| Tín hiệu | Định nghĩa | Vai trò |
|---|---|---|
| `no_change` | Dấu vân tay cây làm việc sau bước sửa bằng dấu vân tay trước nó | Chặn được (ở `stop`) |
| `same_failure` | Chữ ký của đầu ra kiểm chứng vòng này bằng vòng trước **và** tập khoá phát hiện nghiêm trọng bằng vòng trước | Chặn được (ở `stop`) |
| `new_scope` | Review nêu phát hiện ở đường dẫn chưa từng xuất hiện ở vòng nào trước | Chỉ ghi chú, **không bao giờ chặn** |

**Chữ ký đầu ra kiểm chứng.** Chuẩn hoá rồi băm: bỏ mốc thời gian, thời lượng ("12ms", "0.3s"), đường dẫn tạm (Unix lẫn Windows),
địa chỉ hex và chuỗi hex dài, dòng trống, khoảng trắng thừa. Hai đầu ra giống nhau sau chuẩn hoá thì chữ ký bằng nhau. Chỉ băm
**toàn bộ** đầu ra (không phân tích số test hỏng), nên không phụ thuộc ngôn ngữ hay bộ test; đổi một dòng đầu ra làm chữ ký khác,
tức là nghiêng về "có tiến triển". Giới hạn đọc phần cuối đầu ra (đề xuất 64 KiB) để chi phí xác định. Đầu ra rỗng → chữ ký rỗng
→ không bao giờ khớp.

**Khoá phát hiện.** Với mỗi phát hiện nghiêm trọng: đường dẫn chuẩn hoá cộng tiêu chí chấp nhận liên quan (`Criterion`) nếu có;
không có đường dẫn thì dùng tiêu đề chuẩn hoá. Hai vòng "giống" khi tập khoá bằng nhau.

**Thiết kế nghiêng về không dừng.** Dừng nhầm một lượt đang tiến triển đắt hơn nhiều so với tiết kiệm một vòng (mặc định chỉ còn
tối đa hai vòng sửa). Vì vậy: mọi nghi ngờ (không đọc được log, không chụp được cây, vòng đầu tiên chưa có vòng trước để so) →
coi là có tiến triển.

### GĐ2a — Phân tích offline trên phiếu đã có (không đổi Runner)

Lệnh chỉ đọc `bint task progress [--json]` (hoặc tương đương, tên chốt khi lập phiếu), áp bộ phát hiện lên **mọi phiếu đã có
trong sổ phiếu**: dựng lại chuỗi vòng từ `review.json` (các `ReviewRound`) và `state.json` (`Verifications` với `Worktree`/`Tree`,
`Log`). Với mỗi phiếu in: số vòng, từng tín hiệu ở từng vòng, "lẽ ra dừng ở vòng N", kết cục cuối (`done`, `blocked`, `failed`…) và
số vòng thực tế đã chạy thêm sau điểm đó.

Việc đầu tiên: xác nhận `Verification.Log` là đường dẫn hay nội dung, và log còn tồn tại bao lâu (có bị dọn không). Phiếu thiếu log
chỉ tính được `no_change` và khoá phát hiện; báo rõ phần nào không tính được, không đoán.

Giá trị của GĐ2a: trả lời "chuyện này có xảy ra không, bao nhiêu lần" **mà không đổi dòng nào của Runner**. Nếu số vòng "lẽ ra
dừng" quá ít thì kết luận sớm là không đáng làm GĐ2c.

### GĐ2b — Chế độ bóng trong Runner

Khoá `workflow.no_progress.mode`: `off` (mặc định) | `shadow` | `stop`. Ở `shadow`, tại hai điểm đang quyết định vòng sửa
(`reviewDecision` khi còn phát hiện nghiêm trọng, và `fixAfterFullVerify`), Runner tính tín hiệu và **chỉ ghi nhận**:

- một dòng trong `Result.Notes` và log của lượt chạy;
- trường thêm tương thích ngược trong `state.json` của phiếu (kiểu `omitempty`, không migration SQLite, như cách các trường phiếu
  khác đã thêm), chứa danh sách `{vòng, tín hiệu, chữ ký, lẽ ra dừng}`;
- hiển thị trong `bint task show --json` và `bint run status`.

Không có thay đổi luồng. `shadow` chạy trong repository của Bint để tích luỹ dữ liệu; repository của người dùng mặc định `off`.

Thêm khoá cấu hình cần đủ bốn việc theo quy ước repository: trường có thẻ `yaml` trong struct, giá trị trong `Default()`, kiểm
trong `Validate()`, một dòng có chú thích trong `Render` của `internal/config/template.go` (nhớ viết `%%` cho ký tự `%` trong chú
thích). `TestRenderMatchesDefaults` ép mẫu khớp `Default()`.

### GĐ2c — Chế độ dừng sớm

Ở `mode: stop`, khi tín hiệu chặn được (`no_change`, hoặc `same_failure`) xuất hiện: **không dùng vòng sửa còn lại**, chuyển tác
vụ sang `blocked` bằng chính đường `r.block` hiện có, với lý do `no_progress: <tín hiệu>` và lệnh `bint run resume` như các lần chặn
khác, kèm đường dẫn tới bằng chứng (hai chữ ký, hai dấu vân tay cây).

Để không tạo vòng chặn lặp: sau khi người `resume`, bộ phát hiện **tắt cho vòng kế của phiếu đó** (ghi vào `state.json`), nên một
lần người xác nhận luôn mua thêm đúng một vòng. Bộ phát hiện không bao giờ chạy với lượt dừng `external` (hết hạn mức, 5xx,
Ctrl-C) hay `awaiting_human`: các lớp đó đã có đường nối tiếp riêng và không phải "vòng sửa không tiến triển".

**Cổng bật mặc định (chốt trước khi đo, không đổi sau khi thấy kết quả).** `stop` không bao giờ là mặc định của bản phát hành
nếu chưa đạt cả hai điều kiện, tính trên GĐ2a và dữ liệu `shadow`:

- tối thiểu **20 sự kiện "lẽ ra dừng"** (ít hơn thì kết luận là "chưa đủ dữ liệu", giữ `shadow`);
- **tỷ lệ dừng nhầm ≤ 10 %**, trong đó dừng nhầm là phiếu mà sau điểm lẽ ra dừng vẫn tới `done` với review sạch ở một vòng sau;
- và `stop` không được làm tăng số phiếu `blocked` mà không giảm chi phí tương ứng (báo cả hai số).

Hai ngưỡng đầu là đề xuất, được chốt bằng một commit riêng trước khi chạy phân tích ở GĐ2a. Không đạt thì `stop` vẫn tồn tại như
tuỳ chọn người tự bật, và README ghi rõ chưa có bằng chứng.

### Kiểm thử

- Bảng kiểm thử cho chuẩn hoá chữ ký: mốc thời gian, thời lượng, đường dẫn tạm Unix và Windows (`C:\…\Temp\…`), hex, dòng trống.
  Hai đầu ra chỉ khác ở phần nhiễu → cùng chữ ký; khác một dòng lỗi → khác chữ ký.
- Bảng kiểm thử cho từng tín hiệu và cho các ca "nghi ngờ → có tiến triển" (log thiếu, cây không chụp được, vòng đầu).
- Kiểm thử Runner với agent giả: vòng sửa không đổi cây → `shadow` ghi nhận, luồng như cũ; `stop` → `blocked` với lý do đúng; sau
  `resume` không bị chặn lại ở vòng kế.
- Hồi quy: `mode: off` cho kết quả đúng như hiện nay (golden `internal/workflow/testdata/flow_golden.json` không đổi; nếu có
  trường mới làm golden đổi thì cập nhật có chủ đích).
- Cặp `*_unix.go`/`*_windows.go` chỉ khi cần dấu vân tay riêng cho nền tảng; ưu tiên dùng lại `WorktreeSnapshot`.

### Nghiệm thu

`NT-NOPROG-OFFLINE` (GĐ2a chạy trên toàn bộ phiếu hiện có, báo cáo số liệu), `NT-NOPROG-SHADOW` (chế độ bóng không đổi kết cục
của lượt chạy nào), `NT-NOPROG-STOP` (đạt cổng bật mặc định, hoặc ghi rõ không đạt).

---

## GĐ3 — Trần kích thước khối xuất bản

### Khái niệm

Khối tri thức xuất bản vào `AGENTS.md` có một ngân sách kích thước hiển thị được. Vượt thì **cảnh báo**, không chặn và không tự cắt.

### Nội dung

- Đo kích thước khối do `bint bootstrap publish` dựng (byte và số token ước tính cùng một công thức với nơi Bint đã ước token, nếu
  có; nếu không thì chỉ byte).
- `bint bootstrap publish --dry-run` và `bint doctor` (`knowledgeChecks`) in kích thước, trần, và danh sách mục lớn nhất để người
  chọn mục nào cần rút gọn hay hạ trạng thái.
- Khoá `knowledge.publish.max_bytes` (tên chốt khi lập phiếu). **Giá trị mặc định chọn sau khi đo** khối của chính repository
  này và một vài repository mẫu, để mặc định không cảnh báo nhầm; `null` = không kiểm.
- Không đổi thuật toán chọn mục (`knowledge.Select`), thứ tự hay `MaxItems`; chỉ thêm đo và cảnh báo.

### Nghiệm thu

`NT-PUBCAP-WARN`: đặt trần thấp → cả `publish --dry-run` và `doctor` cảnh báo; trần cao hoặc `null` → không cảnh báo; nội dung xuất
bản không đổi so với trước giai đoạn này.

---

## GĐ4 — Thử nghiệm effort bằng khung đánh giá (tuỳ chọn, có điều kiện)

### Câu hỏi

Mức nỗ lực suy luận cao hơn có nâng tỷ lệ đạt đủ để bù chi phí không? Đây là câu hỏi **đo**, không phải tính năng.

### Giới hạn của khung hiện tại (cần nói rõ)

`bint eval` chạy mỗi tác vụ như một lần giải độc lập và lặp lại `R` lần; **không có vòng sửa có phản hồi** trong khung đánh giá. Vì
vậy thí nghiệm khả thi ở đây là **"effort cao so với mặc định" như một biến của nhánh** (cạnh các biến như ngữ cảnh, MCP, harness
của cú pháp `bint@…`), đo tỷ lệ đạt và chi phí. Nó **không** kiểm trực tiếp ý "thử lại với effort cao hơn sau khi thất bại"; muốn
kiểm ý đó cần một chế độ hai lượt (lượt hai nhận phản hồi lượt một) là việc lớn hơn, chưa nằm trong kế hoạch này.

### Điều kiện bắt đầu (kiểm trước, không giả định)

1. Xác nhận từng harness có cờ hoặc khoá cấu hình effort dùng được ở chế độ không tương tác, và Bint truyền được mà không phá cô
   lập của lượt đánh giá. Chưa kiểm ở thời điểm viết kế hoạch; harness nào không có thì loại khỏi thí nghiệm.
2. Có ngân sách đo đã duyệt (`evolution.evaluation.budget_usd`) và chỗ trong vòng V3–V5 của acceptance.md để dùng chung nhánh
   `none`/`minimal`/`bint`.
3. Chốt ngưỡng trước khi chạy: dùng `evolution.evaluation.min_effect` hiện có làm mức cải thiện phải phát hiện được; không đạt
   thì kết luận `not_supported` và **không đưa gì vào cấu hình**.

### Nếu có kết quả dương

Chỉ khi đó mới xét một khoá cấu hình effort theo role và một đề xuất đi qua đường `evolve` thông thường (người duyệt). GĐ4 không
tự thêm khoá nào.

### Nghiệm thu

`NT-EFFORT-FEASIBLE` (xác nhận điều kiện 1), `NT-EFFORT-MEASURE` (báo cáo đo theo ngưỡng đã chốt, kể cả kết quả âm).

---

## Rủi ro và cách giảm

| Rủi ro | Giai đoạn | Cách giảm |
|---|---|---|
| Dừng nhầm vòng đang tiến triển, thêm `blocked` | GĐ2c | Mặc định `off`; offline và bóng trước; thiên về "có tiến triển" khi nghi ngờ; cổng ≤ 10 % dừng nhầm; mỗi lần `resume` mua thêm một vòng |
| Chữ ký phụ thuộc nhiễu của môi trường (đường dẫn, mốc thời gian) làm vòng giống bị coi khác | GĐ2 | Sai theo chiều an toàn (không dừng); bảng chuẩn hoá có ca Windows; đo tỷ lệ "không so được" ở GĐ2a |
| Ước lượng sai làm người tin con số | GĐ1 | Luôn in `n` và nhóm đã dùng; `unknown` khi thiếu; khoảng chứ không phải điểm; `NT-SIM-CALIB` đo độ khớp |
| `simulate` bị gắn vào `bint run` thành bước bắt buộc | GĐ1 | Ngoài phạm vi (xem dưới); lệnh riêng, chỉ đọc |
| Gợi ý `max_turns` đặt quá thấp làm cắt bước | GĐ1 | Chỉ in, không tự ghi; kèm cảnh báo và số mẫu |
| Cảnh báo kích thước khối thành chặn cứng | GĐ3 | Chỉ cảnh báo; `null` tắt hẳn |
| Thêm khoá cấu hình làm hỏng nạp cấu hình cũ | GĐ2b, GĐ3 | `KnownFields(true)` + `Default()` + `Validate()` + `Render` đủ bốn việc; `TestRenderMatchesDefaults` |
| Khuếch đại thời gian chạy test trên Windows | mọi GĐ | Bộ kiểm thử mới dùng thư mục tạm nhỏ, không sinh thêm tiến trình git ngoài cần thiết; đo trước khi tối ưu |

## Ngoài phạm vi

- **Tự chuyển sang harness khác khi hết hạn mức.** Đã cân nhắc và loại khỏi đợt này: nối tiếp cùng phiên (step-continuation) đã
  xử lý phần lớn trường hợp dừng ngoại cảnh, chưa có số liệu cho thấy đổi harness giữa chừng đáng cái giá của nó, và nó làm yếu
  trần chi phí cứng (Codex/Antigravity không báo USD) và có thể chạy mà thiếu hook cưỡng chế. Khoá
  `security.allow_external_agents` vẫn là khoá khai báo chưa gắn cơ chế; việc đó thuộc kế hoạch riêng.
- Dựng lại lưu trạng thái theo sự kiện thuần với phát lại; Bint dùng SQLite cộng tệp phiếu đã ổn định.
- Để mô hình tự soạn quy trình cho từng tác vụ: flow tuỳ biến do người viết, được `bint flow lint` và băm, giữ nguyên.
- Biến `simulate` thành bước bắt buộc hoặc cổng của `bint run`.
- Tự áp dụng gợi ý (`max_turns`, trần khối): chỉ người đặt.
- Hiển thị trong TUI: sau khi `--json` ổn định mới lập phiếu riêng.

## Ràng buộc chung

- Mã qua `go vet` và `staticcheck` với cả Linux và `GOOS=windows` (staticcheck cho Windows: cài binary rồi đặt `GOOS`).
- Trường mới trong `state.json`/sự kiện tương thích ngược (`omitempty`, không migration); chỉ thêm migration khi bắt buộc.
- Lệnh và cờ CLI mới: cập nhật `cliRefs` và `litsrUsage` (xem GĐ1).
- Thông điệp người dùng qua i18n (vi/en).
- Kiểm thử chạy trong phiên agent đặt lại biến môi trường agent (`asHuman(t)`) khi thử thao tác dành cho người.
- Nghiệm thu chỉ ghi ở [acceptance.md](acceptance.md) (các mã `NT-SIM-*`, `NT-NOPROG-*`, `NT-PUBCAP-*`, `NT-EFFORT-*` thêm vào khi
  lập phiếu), gom vào vòng chung để tiết kiệm token.
