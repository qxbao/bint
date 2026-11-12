# Kế hoạch TUI cho Bint

Trạng thái: **giai đoạn 0, 1, 2, 3, 4, 5 và 6 đã triển khai** (giai đoạn 5 gồm hai phiếu: **5a** — tab Hệ thống — ngày 2026-11-12 và
**5b** — trình hướng dẫn `init`, Hệ thống › Cài đặt repo, nâng cấu hình, cập nhật Bint + khởi động lại — ngày 2026-11-12; giai đoạn 6 gồm hai phiếu: **6a** — chuột, chế độ hẹp, chân phím, đọc đủ thông báo bị cắt — và
**6b** — thông điệp theo phím TUI, `NO_COLOR`, chế độ chỉ đọc, trợ giúp theo ngữ cảnh, README, chốt giai đoạn — cùng ngày 2026-11-12); còn lại là thử với người dùng thật và ảnh VHS (mục 12.10). Sau đó thêm **màn MCP** (tab thứ tám, mục 5.8; giai đoạn M4 của `docs/mcp-gateway-plan.md`, 2026-11-13): thẻ MCP của Hệ thống chuyển sang màn này.
Kế hoạch đề xuất và rà soát lần 1 cùng ngày (phủ cấp cờ, vòng đời, tiêu chí thẩm mỹ). Chi tiết những gì đã làm
và những chỗ lệch so với kế hoạch ở mục 12 (12.1–12.2: giai đoạn 0; 12.3: giai đoạn 1; 12.4: giai đoạn 2; 12.5: giai đoạn 3; 12.6: giai đoạn 4; 12.7: giai đoạn 5a; 12.8: giai đoạn 5b; 12.9: giai đoạn 6a; 12.10: giai đoạn 6b; 12.11: bằng chứng tri thức lệch dạng có cấu trúc).

## 1. Mục tiêu

1. **Đẹp**: bố cục khung viền có tiêu đề nằm trên đường viền, thanh tab, màu nhấn tiết chế,
   phím tắt hiện ở viền dưới (tham khảo phong cách binsider). Có tiêu chí nghiệm thu bằng mắt
   (mục 4.4), không chỉ golden test.
2. **Thân thiện**: người không thuộc lệnh vẫn dùng được hết Bint. Mọi thao tác nhìn thấy
   được, không cần gõ cú pháp, không cần nhớ ID.
3. **Đủ tính năng**: mọi lệnh lá **và mọi cờ** của CLI đều có chỗ trong TUI, hoặc được loại
   trừ có lý do (bảng ở mục 6, quy tắc cờ ở mục 7.3). Có kiểm thử ép buộc điều này (mục 8).
4. **Không phá CLI**: CLI, `--json` và MCP giữ nguyên cho agent, CI và kịch bản.

Ngoài phạm vi: giao diện web. TUI vẫn chạy trong terminal; người dùng phổ thông hoàn toàn
không dùng terminal cần thêm kênh khác (duyệt qua MCP elicitation, `bint ui` web). Tầng
dịch vụ ở giai đoạn 0 được thiết kế để một giao diện web sau này dùng lại được.

## 2. Hiện trạng ảnh hưởng tới thiết kế

- Phần lớn nghiệp vụ nằm ở `internal/app`, `internal/evolve`, `internal/eval`,
  `internal/workflow`, nhưng **điều phối còn nằm trong tầng CLI**: `internal/cli/eval.go`
  (1375 dòng, ~98 lời gọi `eval.*`), `internal/cli/evolve.go` (1343 dòng),
  `internal/cli/evolve_cycle.go`, và vòng duyệt tương tác `bint knowledge review`
  (`internal/cli/knowledge.go`). TUI không thể gọi lại những đoạn này mà không tách ra.
- **Cờ**: ~240 định nghĩa cờ (~45 là `--json`; nhiều nhất ở `eval.go` 32, `task.go` 18,
  `evolve.go` 17). Ràng buộc giữa các cờ đang kiểm tra trong `RunE` của CLI, ví dụ
  `--hooks`/`--no-hooks`, `--mcp`/`--no-mcp` (`init`), `--template | --kind --file`
  (`harness component add`). Form TUI không dùng lại được nếu không đưa xuống tầng dịch vụ.
- **Hỏi xác nhận giữa chừng**: `init` và `run` nhận callback `opts.Confirm`; `eval run`,
  `eval bootstrap`, `evolve evaluate`, `bootstrap publish` gọi thẳng `confirm()` trong CLI.
  Trong TUI các lời hỏi này phát sinh ở goroutine nền.
- Thao tác chỉ dành cho người (`approve`, `reject`, `review`, `apply`, `rate`,
  `task approve`, `harness component approve/deprecate`) chặn bằng `app.InAgentSession`. Một
  số chỗ kiểm tra ở tầng CLI (`cli/knowledge.go`, `cli/runs.go`, `cli/task.go`); khi tách
  tầng dịch vụ phải đưa kiểm tra xuống dưới để TUI không vô tình bỏ qua.
- Hook không được mở SQLite và không được chặn agent. TUI là tiến trình riêng, đọc SQLite
  bình thường, nhưng phải chịu được việc hook và `bint run` cùng ghi.
- Cấu hình nằm ở worktree chính; `init --update` ở worktree phụ bị bỏ qua
  (`app/init.go`). `doctor` đã phát hiện cấu hình "theo mẫu cũ" và gợi ý `init --update`.
- `bint update` build lại chính binary đang chạy (`app.UpdateSelf`), trả về `OldBackup` và
  `Mismatch` khi bản trong PATH khác tệp vừa cài.
- Chưa có thư viện TUI nào trong `go.mod`.

## 3. Lựa chọn công nghệ

**Bubble Tea + Lip Gloss + Bubbles (Charm)**: mô hình Elm (Model/Update/View) dễ kiểm thử,
Lip Gloss lo viền, màu và bố cục, Bubbles có sẵn table, list, viewport, textinput,
textarea, spinner, progress, help. Chạy được trên Windows, phù hợp yêu cầu
`GOOS=windows` của CI. `teatest` cho kiểm thử ảnh chụp đầu ra.

Ghim phiên bản ổn định mới nhất tại thời điểm bắt đầu (cân nhắc v2 nếu đã ổn định) và
kiểm tra `go vet`/`staticcheck` với `GOOS=windows` ngay ở giai đoạn 0.

**Đã ghim (giai đoạn 0)**: dòng v2 — `charm.land/bubbletea/v2 v2.0.9`, `charm.land/lipgloss/v2 v2.0.6`,
`charm.land/bubbles/v2 v2.2.1` — cùng `golang.org/x/text v0.41.0` (chuẩn hoá NFC) và
`golang.org/x/term v0.45.0` (`term.IsTerminal`). Chọn bản v2 mới nhất còn khai `go 1.25.x`: bubbletea
v2.0.10+ và lipgloss v2.1.0 đòi Go 1.26, còn `x/text` ≥ v0.42 và `x/term` ≥ v0.46 cũng vậy; nâng Go
kéo theo staticcheck ghim trong `.github/workflows/ci.yml` (đường dẫn được bảo vệ). Chỉ thị `go`
trong `go.mod` giữ 1.25.1. Khi nâng Go, nâng cả các phụ thuộc này.

Những thứ phải tự làm hoặc thêm thư viện nhỏ (kiểm tra lại với phiên bản được ghim):

- **Tiêu đề trên viền và phím trên viền dưới**: Lip Gloss không vẽ sẵn; tự vẽ dòng viền
  trên/dưới trong `components/frame` (đo độ rộng bằng `lipgloss.Width`/`ansi`).
- **Chuột theo vùng** (bấm tab, bấm hàng): Bubble Tea chỉ trả toạ độ; dùng `bubblezone`
  hoặc tự giữ bảng vùng.
- **Logo chữ điểm ảnh**: tự vẽ bằng ký tự nửa khối (`▀ ▄ █`), không dùng figlet.
- **Ảnh PNG để duyệt bằng mắt**: VHS (Charm) chạy kịch bản `.tape` và xuất ảnh/GIF.

Không chọn tview: kiểu widget giữ trạng thái khó viết test ảnh chụp, khó tạo phong cách riêng.

## 4. Thiết kế giao diện

### 4.1 Bố cục chung

```text
┌───────────────────────────────│bint 0.1.0│────────────────────────────────┐
│ Tổng quan │ Tri thức (3) │ Tác vụ │ Lượt chạy │ Đánh giá │ Tiến hoá │ Hệ thống │
└────────────────────────────────────────────────────── ~/Project/Go/bint ───┘
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│                          (nội dung màn hình)                                │
│                                                                             │
└──[Enter→ Mở] [a→ Duyệt] [/→ Tìm] [:→ Lệnh] [?→ Trợ giúp] [q→ Thoát]─────────┘
```

- Tiêu đề khung nằm trên viền (`│Phụ thuộc│` như ảnh tham khảo); tab đang chọn in đậm,
  tab khác màu nhấn; số trong ngoặc trên tab là số việc đang chờ người, tô màu `key`
  (không dùng chữ số mũ: độ rộng không ổn định giữa các font).
- Viền dưới luôn hiện 4–6 phím quan trọng nhất **của ngữ cảnh hiện tại**; `?` mở bảng đầy đủ.
  Mỗi phím có độ ưu tiên trong sổ hành động; thiếu chỗ thì bỏ phím ưu tiên thấp trước, rồi
  rút nhãn, luôn giữ `?` và `q`.
- Màn hình hẹp (< 100 cột): danh sách và chi tiết chuyển từ hai cột sang xếp chồng; tab rút
  gọn thành biểu tượng/chữ đầu. Dưới 60×20: hiện thông báo yêu cầu phóng to.
- Thanh trên còn hiện: nhãn "CHỈ ĐỌC" (mục 7.4), số việc nền đang chạy, và cảnh báo khi
  đang ở worktree phụ.

### 4.2 Hệ thống thiết kế (`internal/tui/theme`)

- Bảng màu theo vai trò, không theo màu. Giá trị khởi điểm cho nền tối (chốt ở giai đoạn 0
  sau khi duyệt màn mẫu, mục 4.4):

  | Vai trò | Dùng cho | Tối (truecolor) | 16 màu |
  |---|---|---|---|
  | `fg` | nội dung | `#E6E6E6` | white |
  | `muted` | viền, chú thích, phân cách | `#7A7A7A` | bright black |
  | `accent` | nhãn, tab chưa chọn, liên kết | `#5FB3A1` (xanh ngọc) | cyan |
  | `key` | phím tắt, điểm nhấn hiếm | `#E5A55A` (cam) | yellow |
  | `ok` / `warn` / `err` | trạng thái | `#8FB573` / `#E5C07B` / `#E06C75` | green / yellow / red |

  Bộ sáng có giá trị riêng. Tự chọn theo nền terminal; tôn trọng `NO_COLOR` (chỉ còn đậm,
  gạch chân, đảo màu) và chế độ 16 màu. Nền dùng màu mặc định của terminal, không tô nền khung.
- **Tiết chế**: mỗi màn hình tối đa ba màu ngoài `fg` (thường `accent`, `key`, `muted`);
  màu trạng thái chỉ dùng cho ký hiệu trạng thái. Viền mảnh góc vuông.
- **Khoảng cách**: lề trong khung 1 cột/0 dòng; giữa các khung 1 dòng; hộp thông tin
  nhỏ ở Tổng quan đặt giữa, rộng theo nội dung chứ không kéo hết màn hình.
- Trạng thái tri thức có ký hiệu **và** màu (✓ verified, ◌ candidate, ⟳ stale,
  ✗ deprecated) để không phụ thuộc vào khả năng phân biệt màu.
- Chữ nghiêng chỉ để trang trí (khẩu hiệu, đường dẫn); không mang nghĩa, vì conhost cũ
  không hiện được.
- Chữ tiếng Việt: chuẩn hoá NFC trước khi đo độ rộng, kiểm thử căn cột với chuỗi có dấu.
- Logo chữ điểm ảnh "bint." ở màn hình Tổng quan khi đủ chỗ (≥ 100×30), giống tinh thần ảnh
  tham khảo; hẹp hơn thì chỉ hiện chữ "bint." đậm.

### 4.3 Nguyên tắc UX

1. **Hộp thư "Việc cần bạn"** là trang đầu: mục tri thức chờ duyệt, thành phần harness
   candidate, tác vụ `blocked`, đề xuất tiến hoá chờ áp dụng, ứng viên đánh giá, cảnh báo
   `doctor` (kể cả cấu hình theo mẫu cũ, skill lệch phiên bản). Enter nhảy thẳng tới chỗ xử lý.
2. **Bảng lệnh** (`:` hoặc Ctrl+K): tìm mờ trên mọi hành động của mọi màn hình, kể cả hành
   động hiếm dùng. Đây là bảo đảm "không tính năng nào bị giấu".
3. **Biểu mẫu thay cờ**: mỗi hành động có tham số mở form có nhãn, giá trị mặc định lấy từ
   cấu hình, kiểm tra ngay khi nhập, chọn ID/đường dẫn từ danh sách gợi ý. Form sinh từ
   struct `Options` của tầng dịch vụ (mục 7.3); trường ít dùng nằm trong nhóm "Nâng cao" thu
   gọn, nhưng vẫn có.
4. **Xem trước rồi mới ghi**: thao tác thay đổi tệp (publish AGENTS.md, apply, rollback,
   sync, init, nâng cấu hình) hiện diff trước, Enter để xác nhận. Thao tác không đảo ngược
   được cần gõ xác nhận. Tuỳ chọn `--force` hiện cảnh báo nói rõ kiểm tra nào bị bỏ qua.
5. **Hiện lệnh CLI tương đương** ở chân form (`bint knowledge approve 01M7…`), phím `y` để
   chép. Người dùng học dần CLI, và hành vi TUI luôn khớp CLI.
6. **Việc chạy lâu** (`run`, `eval run`, `evolve cycle`, `revalidate`, `update`) chạy nền:
   thanh tiến độ, ước tính chi phí trước khi chạy, xem log trực tiếp, Ctrl+C huỷ an toàn,
   thông báo khi xong dù đang ở màn hình khác. Câu hỏi giữa chừng hiện thành hộp thoại.
7. **Chuột** (cuộn, bấm tab, bấm hàng) bật mặc định; mọi thứ vẫn dùng được chỉ bằng bàn phím.
8. **Phím nhất quán** giữa các màn hình: `j/k`/mũi tên di chuyển, `Enter` mở, `Esc`/`Bksp`
   quay lại, `/` lọc, `Tab` đổi khung, `r` làm mới, `e` sửa trong `$EDITOR`, `x` chọn nhiều.
9. **Lỗi có hướng xử lý**: lỗi hiện trong khung, kèm gợi ý (ví dụ "chưa `init` → nhấn i để
   khởi tạo"), không bao giờ thoát TUI vì một lỗi nghiệp vụ.

### 4.4 Tiêu chí thẩm mỹ

"Đẹp" không kiểm được bằng golden test (golden chỉ chống thụt lùi), nên có ba chốt chặn:

1. **Màn mẫu trước, màn thật sau**: giai đoạn 0 dựng màn Tổng quan bằng component thật theo
   phong cách ảnh tham khảo (logo, hộp thông tin đặt giữa, khung có tiêu đề trên viền, chân
   phím trên viền dưới). Chủ dự án duyệt ảnh chụp, chốt bảng màu và khoảng cách ở mục 4.2,
   rồi mới làm các màn khác.
2. **Trang trưng bày** `bint tui --gallery` (ẩn): mọi component (khung, tab, bảng, form, diff,
   Markdown, hộp xác nhận, thông báo, biểu đồ cột) ở nền sáng, tối và `NO_COLOR`, chuỗi `vi`,
   `en` và locale kéo dài. Dùng để duyệt component mới mà không cần dựng dữ liệu.
3. **Ảnh PNG bằng VHS**: mỗi màn hình có một kịch bản `.tape` trong `internal/tui/testdata/vhs`;
   CI xuất ảnh làm artifact để duyệt trong PR thay đổi giao diện. Không so pixel tự động.

Danh sách tự kiểm khi duyệt: không quá ba màu nhấn trên một màn; không chữ bị cắt ở 80×24;
cột thẳng với chuỗi có dấu; khoảng trống chủ đích (không nhồi bảng kín khung khi ít dữ liệu);
chân phím không tràn.

## 5. Các màn hình

### 5.1 Tổng quan
Trạng thái repository (`status`), hộp thư "Việc cần bạn", kết quả `doctor` rút gọn, lượt
chạy gần đây, chi phí tiến hoá/đánh giá trong kỳ. Lần đầu (chưa `init`): hiện **trình
hướng dẫn khởi tạo** thay cho bảng điều khiển (mục 7.6).

### 5.2 Tri thức
```text
┌│Tri thức│ lọc: candidate · nguon:api-guidelines ───────┐┌│Chi tiết│─────────────────────┐
│ ◌ rule     Endpoint phải có /v1          api/**       ││ rule · candidate · agent ⚑    │
│ ◌ warning  Không gọi sql.Open trực tiếp  internal/db  ││ Endpoint phải có phiên bản... │
│ ✓ pattern  Handler đi qua wrap()         *            ││─│Bằng chứng│──────────────────│
│                                                       ││ docs/api.md:42-48             │
│                                                       ││   42  ## Versioning           │
│                                                       ││─│Kiểm tra│ ✓ 0 lỗi ───────────│
└───────────────────────────────────────────────────────┘└───────────────────────────────┘
──[a→ Duyệt] [r→ Từ chối] [e→ Sửa] [x→ Chọn] [A→ Duyệt cả lô] [n→ Thêm] [/→ Tìm]──────────
```
- Lọc theo trạng thái, loại, nguồn, nhãn lô `nguon:*` (do `/b-knowledge` gắn), đường dẫn.
- **Duyệt theo lô** thay cho vòng hỏi đáp của `knowledge review`: chọn nhiều hoặc cả lô, xem
  từng mục ở khung phải, duyệt một lần. Mục có lỗi kiểm tra không cho duyệt hàng loạt.
- Thêm mục bằng form (loại, tiêu đề, phạm vi, bằng chứng chọn từ trình duyệt tệp + vùng dòng).
- Danh sách chỉ vẽ các hàng đang hiện (chịu được hàng nghìn mục).
- Thẻ phụ: Tìm kiếm (FTS), Thống kê, Thay đổi (`diff`, kể cả khoảng commit và `--all`),
  Thẩm định lại (`revalidate`), Xuất bản AGENTS.md (`bootstrap publish` với diff xem trước),
  Khảo sát (`inspect`, `bootstrap`), Ngữ cảnh (`context` theo mô tả hoặc đường dẫn: xem đúng
  gói agent sẽ nhận).

### 5.3 Tác vụ
Danh sách theo trạng thái; chi tiết theo thẻ con: Phiếu, Tài liệu (requirement/plan/tasks,
xem dạng Markdown), Tiêu chí, Kiểm chứng, Review (phát hiện theo mức độ, Enter tới tệp:dòng),
Lỗi, Lượt chạy. Hành động: tạo tác vụ, chạy workflow (chọn `b-do`/`b-req`/`b-feat`/`b-review`,
harness theo vai trò, `--no-review`), tiếp tục, duyệt (`task approve`, hiện lệnh kiểm chứng
và đường dẫn bảo vệ cần xác nhận), đổi trạng thái theo bảng chuyển, chuyển phát hiện review
thành tri thức.

### 5.4 Lượt chạy
Danh sách + chi tiết lượt chạy, chấm điểm (`rate`), quét Git (`scan`), hàng đợi sự kiện
(`events`, nạp spool bằng `events ingest`), bài học từ thất bại (`experience show/digest`).

### 5.5 Đánh giá
Ứng viên (duyệt/từ chối/kiểm chứng fail→pass), tác vụ đã duyệt, đặc tả (`spec`), ước tính
chi phí, chạy so sánh có tiến độ theo ô (nhánh × lần lặp), báo cáo với biểu đồ cột tỷ lệ đạt
và khoảng tin cậy, dọn kho đệm.

### 5.6 Tiến hoá
Cơ hội (`scan`), đề xuất (danh sách, chi tiết với diff, kết quả Probe), đối chiếu, áp dụng,
bác bỏ, quay lui, lịch sử, hàng đợi deferred (`queue/process`), chạy `cycle` trong ngân sách.

Đề xuất loại harness (`HarnessChanges`) có khung chi tiết riêng theo loại thành phần:
guidance (tiêu đề, nội dung Markdown), guardrail (`on/when/unless/action`, lời nhắc, mức rủi
ro), script (mô tả, cách dùng, nội dung script kèm cảnh báo "Bint không bao giờ tự chạy;
chờ người duyệt"), strategy (`strategy.yml`); có diff so với thành phần hiện có.

### 5.7 Hệ thống
- **Cấu hình**: xem theo nhóm, sửa trong `$EDITOR`, kiểm tra `Validate()` khi lưu; nâng lên
  mẫu mới (`init --update`) với diff xem trước.
- **Cài đặt repo** (chạy lại `init` với form): harness, hook, phạm vi hook, MCP, AGENTS.md,
  skill, chế độ Git `track|local` (có giải thích hệ quả với `.gitignore`/`info/exclude`/
  skip-worktree).
- **Harness**: danh sách, bảng khả năng, trạng thái hook/MCP (`hook`, `mcp serve` hiện ở đây
  dưới dạng trạng thái); **thành phần quy trình** (`harness component list/show/add/approve/
  deprecate/render`, thêm từ mẫu dựng sẵn hoặc tệp).
- **Skill**: gốc/lớp phủ/skill tiến hoá (`list/show/overlay/diff/versions/check/regress`),
  `sync`.
- **Cập nhật Bint** (`update`, mục 7.6), **Doctor** đầy đủ, **Chỉ mục** (`index rebuild`),
  **Ngôn ngữ**. (Thẻ MCP — `mcp tools` — đã chuyển sang màn MCP, mục 5.8.)

### 5.8 MCP
Màn thứ tám (phím `8`, ngay sau Hệ thống), giai đoạn M4 của `docs/mcp-gateway-plan.md` (mục 8). Dùng tầng dịch vụ
`internal/app/mcp_svc.go` và `mcp_gateway_svc.go`; mọi hành động gọi dịch vụ có `CLIPath` (hành động giao diện thuần — lọc, đổi thẻ, làm mới,
mở `$EDITOR` — không có). Ba thẻ phụ (`.`/`,`):
- **Dịch vụ**: danh sách mọi mục danh mục, mỗi mục đúng một trạng thái — chưa đăng ký, đã đăng ký, **đang chạy** (PID, RSS, số phiên;
  `+N` khi còn phân vùng khác), **degraded**, **lỗi** — suy bằng hàm thuần `screens/mcp.State` theo thứ tự ưu tiên của kế hoạch MCP: check
  fail, phân vùng bị giới hạn khởi động lại hoặc chết còn `LastError` → lỗi; check degraded → degraded; có phân vùng chạy (ưu tiên phân vùng của
  worktree hiện tại) → đang chạy. Danh sách không tự check (check sinh tiến trình, tới 30 giây mỗi service) nên degraded/lỗi do check chỉ có sau khi
  người dùng chạy check hoặc thêm service; kết quả sống trong phiên, không lưu xuống đĩa. `p` chỉ giữ service mà dự án khai báo (`mcp.services`).
  Phím: `a` thêm (form sinh từ `MCPAddOptions`: service, lệnh, đối số, tên biến môi trường, URL — **không có ô giá trị bí mật**; màn hiện
  đường dẫn `secrets.env` và các biến còn thiếu), `x` gỡ (hộp xác nhận), `c` kiểm tra (việc nền, log từng service), `i` lập chỉ mục codegraph
  (việc nền, đầu ra codegraph vào khung log), `R` khởi động lại upstream (hộp xác nhận, việc nền), `l` xem log (stderr đã che credential của
  upstream đang chạy và các dòng `gateway.log` của service: `mcp gateway log`), `G`/`S` bật/tắt daemon (tắt có hộp xác nhận).
- **Quyền**: bảng role × service (ô = các nhóm thao tác) giải từ `app.MCPGrants`, gồm role `interactive` ngầm (đánh dấu `~`) và cảnh báo; chỉ
  để xem. `e` mở `.bint/config.yml` trong `$EDITOR` bằng cách chuyển sang Hệ thống và chạy `system.config_edit` (chọn lớp, `Validate()` khi
  lưu, gộp ba chiều): màn MCP không chép luồng đó.
- **Công cụ Bint**: công cụ của `bint mcp serve` (`mcp tools`, trước đây ở Hệ thống).

Chế độ chỉ đọc (mục 7.4) ẩn `add`, `remove`, `index`, `restart`, `gateway start/stop`, `config_edit` (Writes); phiên agent khoá `add`, `remove`,
`index`, `restart`, `gateway stop` (HumanOnly: gỡ/khởi động lại cắt lời gọi của phiên khác). `check` không ghi dữ liệu của Bint và agent được chạy
(quyết định D5) nên không là Writes. Màn dùng `frame.Box.BottomHints` cho chân phím trong khung (hẹp: tách nửa phím sang khung chi tiết),
`list.Render` cho danh sách và `zone.Mark` cho hàng, thẻ phụ và phím.

## 6. Bảng phủ tính năng

| Nhóm CLI | Lệnh | Vị trí trong TUI |
|---|---|---|
| Dự án | `status`, `doctor` | Tổng quan; Hệ thống › Doctor |
| | `init` | Trình hướng dẫn lần đầu; Hệ thống › Cài đặt repo |
| | `init --update` | Hộp thư (khi `doctor` báo mẫu cũ); Hệ thống › Cấu hình › Nâng mẫu |
| | `sync` | Hệ thống › Skill |
| | `update` | Hệ thống › Cập nhật Bint (việc nền) |
| | `harness list/show` | Hệ thống › Harness |
| | `harness component list/show/add/render` | Hệ thống › Harness › Thành phần |
| | `harness component approve/deprecate` | Hộp thư; Hệ thống › Harness › Thành phần (chỉ người) |
| Tri thức | `knowledge list/show/search/stats/diff/transitions` | Tri thức (danh sách, chi tiết, thẻ phụ; bảng chuyển trong `?`) |
| | `knowledge add/rm/verify` | Tri thức › Thêm / Xoá / Kiểm tra |
| | `knowledge approve/reject/deprecate/review` | Tri thức › Duyệt (từng mục, theo lô) |
| | `knowledge revalidate` | Tri thức › Thẩm định lại (việc nền) |
| | `bootstrap`, `bootstrap publish/skill`, `inspect` | Tri thức › Khảo sát / Xuất bản |
| | `context` | Tri thức › Ngữ cảnh |
| | `index rebuild` | Hệ thống › Chỉ mục |
| MCP | `mcp list`, `mcp gateway status` | MCP › Dịch vụ (danh sách, chi tiết, lọc dự án) |
| | `mcp add/remove` | MCP › Dịch vụ (form thêm; gỡ có xác nhận; chỉ người) |
| | `mcp check` | MCP › Dịch vụ (việc nền) |
| | `mcp index` | MCP › Dịch vụ (việc nền; chỉ người) |
| | `mcp gateway start/stop/restart` | MCP › Dịch vụ (stop/restart chỉ người, có xác nhận) |
| | `mcp gateway log` | MCP › Dịch vụ (khung log) |
| | `mcp grants` | MCP › Quyền (chỉ xem; sửa qua Hệ thống › Cấu hình) |
| | `mcp tools` | MCP › Công cụ Bint |
| Workflow | `task new/list/show/set/status/write/criteria/error/review/lint/verify/approve/transitions` | Tác vụ |
| | `run`, `run resume/status` | Tác vụ › Chạy / Tiếp tục (việc nền) |
| | `skill *` | Hệ thống › Skill |
| Lượt chạy | `runs list/show/rate/scan`, `events`, `events ingest` | Lượt chạy |
| Đánh giá | `eval mine/list/show/spec/verify/approve/reject/estimate/run/report/bootstrap/clean` | Đánh giá |
| Tiến hoá | `evolve scan/propose/list/show/evaluate/apply/reject/rollback/history/queue/process/cycle` | Tiến hoá (cả đề xuất loại harness) |
| | `experience digest/show` | Lượt chạy › Bài học |
| Loại trừ | `mcp serve` (server stdio), `hook *` (do harness gọi), `help`, lệnh ẩn | Không có màn hình; hiện trong Hệ thống › Harness dưới dạng trạng thái (`mcp serve`: công cụ của nó ở MCP › Công cụ Bint) |

`task write/criteria/error/review` chủ yếu do agent gọi; TUI hiện dữ liệu và cho người sửa
khi cần, không ép người nhập JSON.

Bảng này viết tay để đọc; nguồn sự thật là sổ hành động và test phủ (mục 8).

## 7. Kiến trúc

```text
cmd/bint ──► internal/cli  (cobra, in văn bản / JSON)      ┐
         └─► internal/tui  (Bubble Tea)                     ├─► internal/app (dịch vụ) ─► knowledge, task,
             internal/mcpserver                             ┘                              workflow, eval, evolve, store
```

- **Điểm vào** (đã chốt): `bint` không đối số mở TUI. Chưa `init` thì vào thẳng trình hướng
  dẫn khởi tạo. Hai trường hợp vẫn in trợ giúp như hiện nay: không có TTY (đường ống, CI) và
  đang trong phiên agent (agent không điều khiển được TUI). `bint tui` mở TUI tường minh;
  `bint --help` luôn in trợ giúp. **Cách làm**: chặn ở `cli.Execute` (không thêm `RunE` cho lệnh gốc,
  vì `RunE` làm gốc thành runnable và cobra đổi phần `Usage:` của `bint --help`); kiểm TTY bằng
  `term.IsTerminal` chứ không dùng `ModeCharDevice` (`/dev/null` và `NUL` cũng là thiết bị ký tự).
- **Tầng dịch vụ**: mọi hành động TUI gọi hàm trong `internal/app` trả về struct, không gọi
  `cobra`, không phân tích văn bản đầu ra, không chạy tiến trình `bint` con. CLI cũng chuyển
  sang gọi cùng hàm (CLI chỉ còn phân tích cờ và định dạng). Dịch vụ của nhóm `evolve` nằm ở gói con
  `internal/app/evolvesvc` (gói `evolve` import `app` nên không thể đặt dịch vụ ngay trong `app`).
  Dịch vụ dùng chung: `app.RequireHuman`, `app.UsageError`/`app.IsUsage` (CLI ánh xạ sang mã thoát 2),
  `app.ConfirmRequest`/`ConfirmFunc` (lời hỏi giữa chừng: CLI in `Text`, TUI dịch theo `Key`).
- **Sổ hành động** (`internal/tui/actions`): mỗi hành động khai báo tên, màn hình, phím, độ
  ưu tiên ở chân phím, kiểu `Options` của tham số, hàm dịch vụ, đường dẫn lệnh CLI tương
  đương, cờ `Writes` và có phải thao tác chỉ dành cho người không. Bảng lệnh, viền dưới và
  trợ giúp đều sinh từ sổ này.
- **Làm mới dữ liệu**: đọc lại khi chuyển màn hình, khi nhấn `r`, và theo chu kỳ ngắn cho
  màn hình đang có việc nền. Không giữ khoá SQLite lâu; thử lại khi `SQLITE_BUSY`.
- **Thao tác chỉ dành cho người**: kiểm tra `InAgentSession` nằm ở tầng dịch vụ. Nếu TUI bị
  mở trong phiên agent, các hành động này hiện khoá kèm lý do.
- **Gói**: `internal/tui/{app,theme,components,screens/<tên>,actions,gallery}`. Thành phần
  dùng chung: khung có tiêu đề trên viền, thanh tab, chân phím, bảng (vẽ theo vùng nhìn),
  khung chi tiết cuộn được, form, hộp xác nhận, trình xem diff, trình xem Markdown, bảng
  lệnh, thông báo nổi, biểu đồ cột, logo.

### 7.1 Đa ngôn ngữ

- **Phạm vi**: mọi chuỗi giao diện của TUI đi qua danh mục dịch ngay từ giai đoạn 0. Ngôn ngữ
  đầu tiên: `vi` (mặc định) và `en`. Chuỗi của CLI (`--help`, thông báo, lỗi) chuyển dần
  sau, không chặn TUI. Nội dung người dùng (mục tri thức, phiếu tác vụ, log) không dịch.
- **Danh mục**: gói `internal/i18n`, tệp nhúng `locales/vi.yaml`, `locales/en.yaml`, tra
  theo khoá (`knowledge.approve.confirm`), tham số có tên (`{count}`), số nhiều dạng
  `one/other`. Tự viết gọn thay vì kéo thư viện lớn; đổi sang `golang.org/x/text` nếu sau
  này cần quy tắc số nhiều phức tạp.
- **Chọn ngôn ngữ**: cờ `--lang` > biến `BINT_LANG` > khoá cấu hình `ui.language` > `LANG`/
  `LC_ALL` > `vi`. Đổi được ngay trong TUI (Hệ thống › Ngôn ngữ), không cần khởi động lại.
  Khoá cấu hình mới làm theo quy ước của dự án: trường trong struct, `Default()`,
  `Validate()` và dòng có chú thích trong mẫu `Render`; `config.Upgrade` chèn được khoá này
  vào cấu hình cũ (repo cũ thấy gợi ý `init --update` trong hộp thư).
- **Lỗi từ tầng dịch vụ**: lỗi đã biết (`ErrAgentForbidden`, lỗi kiểm tra tri thức, lỗi
  chuyển trạng thái…) mang mã ổn định để TUI dịch; lỗi chưa có mã hiện nguyên văn kèm nhãn
  "chi tiết kỹ thuật".
- **Bố cục chịu được độ dài khác nhau**: không đặt chiều rộng cố định theo chuỗi tiếng Việt;
  ảnh chụp kiểm thử chạy cho cả `vi` và `en`, và một locale giả kéo dài chuỗi ~40% để bắt lỗi
  tràn khung.
- **Kiểm thử danh mục**: mọi khoá có ở mọi locale, tham số khớp nhau; một test quét
  `internal/tui/**` báo chuỗi có dấu tiếng Việt viết thẳng trong mã (phải qua danh mục).

### 7.2 Việc nền

- Goroutine với `context` huỷ được, đẩy sự kiện tiến độ qua kênh thành `tea.Msg`. Workflow
  và eval đã có ghi log/lượt chạy; TUI đọc lại thay vì tự giữ trạng thái.
- **Câu hỏi giữa chừng**: callback `Confirm` (và các lời hỏi khác) của tầng dịch vụ được TUI
  cài bằng một hàm gửi yêu cầu qua kênh rồi chờ trả lời; vòng Update hiện hộp thoại và gửi
  câu trả lời về. Huỷ việc nền thì trả "không" cho câu hỏi đang chờ. Các lời gọi `confirm()`
  đang nằm trong CLI (`eval run`, `eval bootstrap`, `evolve evaluate`, `bootstrap publish`)
  chuyển thành callback trong `Options` ở giai đoạn 0.
- **Thoát khi còn việc nền**: việc nền sống trong tiến trình TUI. `q` khi còn việc đang chạy
  hỏi lại: huỷ an toàn rồi thoát, hoặc ở lại. Việc nào có trạng thái lưu được (`run` có
  `run resume`) thì hộp thoại nói rõ cách tiếp tục sau.
- **Việc của phiên khác** (CLI hoặc TUI khác): chỉ hiện được những gì đã ghi ra đĩa (lượt
  chạy, log workflow/eval, khoá ghi). TUI không huỷ được việc của tiến trình khác.

### 7.3 Tham số và cờ

- **Nguồn duy nhất là struct `Options` của tầng dịch vụ**. Mỗi trường mang thẻ mô tả cho form
  (nhãn qua khoá i18n, kiểu, nhóm "Nâng cao", nguồn gợi ý: ID tri thức, tác vụ, harness,
  model…) và tên cờ CLI tương ứng. Form TUI và lệnh CLI tương đương sinh từ đó.
- **`Options.Validate()`** chứa mọi ràng buộc đang nằm trong `RunE` (loại trừ nhau, bắt buộc
  cùng nhau, khoảng giá trị). CLI gọi nó thay cho kiểm tra riêng; form gọi nó khi nhập.
- **Giá trị mặc định** lấy qua một hàm `Defaults(cfg)` cho từng `Options`, dùng chung cho CLI
  (giá trị mặc định của cờ) và form. Không chép hằng số sang TUI.
- **Cờ không thành trường form** (phải có lý do trong danh sách loại trừ của test):
  `--json` (TUI luôn có dữ liệu cấu trúc), `--yes` (thay bằng hộp xác nhận), cờ chỉ để định
  dạng đầu ra văn bản. `--force` vẫn là trường, kèm cảnh báo (mục 4.3.4).
- Cờ thuộc nhóm "Nâng cao" vẫn tìm được trong bảng lệnh (tìm theo tên cờ cũng ra hành động).

### 7.4 Chế độ chỉ đọc

- `bint tui --read-only` (hoặc `bint --read-only`): xem được mọi màn hình, mọi hành động ghi
  bị ẩn khỏi viền dưới, bảng lệnh và trợ giúp. Thanh trên hiện nhãn "CHỈ ĐỌC".
- Dựa trên cờ `Writes` của từng hành động trong sổ hành động; sổ là nơi duy nhất quyết định,
  màn hình không tự kiểm tra.
- Việc nền đang chạy (do phiên khác khởi động) vẫn xem được tiến độ và log (mục 7.2), nhưng
  không huỷ được.
- Trình hướng dẫn `init` không mở ở chế độ này; repo chưa `init` thì hiện thông báo.
- Đây là chế độ tiện dụng (trình chiếu, máy dùng chung, người chỉ theo dõi), **không** phải
  ranh giới bảo mật: người có quyền ghi repo vẫn chạy CLI được.
- Kiểm thử: với mọi hành động có `Writes = true`, khẳng định không gọi được ở chế độ chỉ đọc,
  kể cả qua phím tắt và bảng lệnh.
- Chân phím trong khung của các màn tra sổ qua `ui.Env.Allowed` (`footer.Hint.ActionID`, `Env.AllowedHints`); màn hình không tự kiểm `ReadOnly`. Bảng kiểm `TestWritesAudit`
  buộc mọi hành động ghi tệp, ghi CSDL hoặc mở trình soạn thảo có `Writes = true` (mục 12.10).

### 7.5 Worktree

TUI mở ở worktree phụ: thanh trên ghi "worktree phụ · cấu hình ở <đường dẫn chính>"; hành
động sửa cấu hình (`init --update`, sửa config) báo phải làm ở worktree chính, kèm phím mở
TUI ở đó nếu được (hoặc chép lệnh `cd … && bint`).

### 7.6 Vòng đời: khởi tạo, nâng cấu hình, cập nhật Bint

- **Lần đầu** (chưa `init`): trình hướng dẫn nhiều bước: phát hiện harness → chọn hook
  (phạm vi), MCP, AGENTS.md, skill → chế độ Git `track|local` (giải thích hệ quả) → xem trước
  danh sách tệp sẽ tạo/sửa → chạy → hiện báo cáo bước (`StepCreated/Updated/Skipped/Warn/
  Conflict`) và `NextSteps` thành nút bấm. Câu hỏi `opts.Confirm` của `init` thành hộp thoại.
  Trước giai đoạn 5 (chưa có wizard): màn hình tạm hiện giải thích và nút "Khởi tạo với mặc
  định" (gọi `app.Init` với `Defaults`) cùng lệnh CLI tương đương.
- **Chạy lại `init`**: Hệ thống › Cài đặt repo, form điền sẵn trạng thái hiện tại (hook đã
  cài, MCP, chế độ Git đã nhớ); chỉ gửi phần thay đổi, có xem trước.
- **Nâng cấu hình** (`init --update`): `doctor` báo mẫu cũ → mục trong hộp thư → diff giữa
  cấu hình hiện tại và kết quả `config.Upgrade` (chỉ chèn dòng, giữ giá trị) → xác nhận.
  Nhắc rằng bản cũ được sao lưu vào thư mục trạng thái.
- **Cập nhật Bint** (`update`): form `--source` (gợi ý từ `BINT_SOURCE_DIR`/thư mục build) và
  `--pull`; chạy nền với log `go install`; xong thì hiện bản cũ → bản mới, `OldBackup`, cảnh
  báo `Mismatch` (PATH trỏ bản khác). TUI đang chạy là bản cũ nên hiện hộp "Khởi động lại":
  Unix tự `exec` lại binary mới với cùng đối số; Windows yêu cầu người dùng mở lại. Sau khi
  khởi động lại, nếu skill trong repo cũ hơn binary thì hộp thư gợi ý `sync`/`init --update`.
- **Lệch phiên bản**: Tổng quan hiện phiên bản binary, phiên bản mẫu cấu hình và `core.lock`
  của skill; lệch thì đưa vào hộp thư.

## 8. Kiểm thử

- **Phủ lệnh**: một test duyệt cây lệnh cobra, lấy mọi lệnh lá và đòi mỗi lệnh có
  hành động trong sổ, hoặc nằm trong danh sách loại trừ có lý do, hoặc nằm trong danh sách
  **chưa làm** gắn giai đoạn. Danh sách chưa làm chỉ được thu nhỏ; giai đoạn 6 đòi nó rỗng.
  Test luôn xanh nên không chặn CI giữa chừng, nhưng thêm lệnh CLI mới mà không khai báo thì
  hỏng ngay.
- **Phủ cờ**: với mỗi lệnh lá đã có hành động, mọi cờ cobra phải ánh xạ tới một trường của
  `Options` (qua thẻ tên cờ) hoặc nằm trong danh sách loại trừ cờ có lý do (mục 7.3). Thêm
  cờ CLI mà quên form thì test hỏng.
- **Ràng buộc**: bảng ca kiểm cho `Options.Validate()` (loại trừ nhau, bắt buộc cùng nhau),
  chạy chung cho CLI và form.
- **Ảnh chụp** (`teatest`, golden file) cho từng màn hình ở 80×24 và 140×40, giao diện sáng
  và tối, `NO_COLOR`, có chuỗi tiếng Việt dài.
- **Duyệt bằng mắt**: kịch bản VHS xuất PNG làm artifact CI (mục 4.4); không so pixel.
- **Tương đương CLI**: với mỗi hành động ghi dữ liệu, chạy qua TUI và qua CLI trên hai repo
  tạm giống nhau rồi so trạng thái `.bint/` và SQLite.
- **Câu hỏi giữa chừng**: việc nền có `Confirm` trả "không" thì không ghi gì; huỷ khi đang chờ
  câu trả lời không treo goroutine.
- **Chỉ dành cho người**: đặt `CLAUDECODE=1`, khẳng định hành động bị khoá (dùng `asHuman(t)`
  cho chiều ngược lại, theo quy ước trong AGENTS.md).
- **Vòng đời**: repo tạm chưa `init` → wizard; cấu hình theo mẫu cũ → hộp thư có mục nâng
  cấu hình và diff đúng với `config.Upgrade`.
- **Windows**: `GOOS=windows go vet ./...` và staticcheck cho `internal/tui/...`; phần tự
  `exec` sau `update` tách `*_unix.go`/`*_windows.go`.

## 9. Giai đoạn

| GĐ | Nội dung | Xong khi |
|---|---|---|
| 0 ✅ | Tách tầng dịch vụ cho `knowledge review`, `eval`, `evolve` (cycle, apply, report) khỏi `internal/cli`; đưa kiểm tra chỉ-dành-cho-người, ràng buộc cờ (`Options.Validate`), giá trị mặc định (`Defaults`) và lời hỏi `confirm()` xuống `app`. Khung TUI: theme, khung viền tự vẽ, tab, chân phím theo ưu tiên, bảng lệnh, sổ hành động, `internal/i18n` (vi, en) và khoá `ui.language`, test phủ lệnh + cờ với danh sách chưa làm. **Màn Tổng quan mẫu** + trang trưng bày + kịch bản VHS; màn tạm "Khởi tạo với mặc định" cho repo chưa `init`. | CLI chạy lại toàn bộ test không đổi hành vi; `bint` mở khung 7 tab, đổi `vi`/`en` được; chủ dự án duyệt ảnh màn mẫu và chốt bảng màu. |
| 1 ✅ | Tổng quan + hộp thư; Tri thức đầy đủ, gồm duyệt theo lô và lọc theo `nguon:*`. | Duyệt 30 mục từ một lần `/b-knowledge` trong < 1 phút, không gõ lệnh. |
| 2 ✅ | Tác vụ + chạy/tiếp tục workflow có log trực tiếp, câu hỏi giữa chừng thành hộp thoại, xác nhận khi thoát còn việc nền. | Từ TUI tạo tác vụ, chạy `b-do`, duyệt, xem review đến `done`. |
| 3 ✅ | Lượt chạy, Bài học, Tiến hoá (cả đề xuất loại harness). | Áp dụng và quay lui một đề xuất hoàn toàn trong TUI. |
| 4 ✅ | Đánh giá, có biểu đồ báo cáo. | Chạy một đợt nhỏ và đọc báo cáo trong TUI. |
| 5 ✅ | Hệ thống (cấu hình, cài đặt repo, harness + thành phần, skill, chỉ mục, MCP) + vòng đời: trình hướng dẫn `init`, nâng cấu hình, cập nhật Bint và khởi động lại. Tách làm hai phiếu: **5a ✅** (tab Hệ thống: Cấu hình, Harness, Thành phần, Skill + sync, Chỉ mục, MCP, Ngôn ngữ, Doctor; mục 12.7) và **5b ✅** (trình hướng dẫn `init`, Cài đặt repo với 11 cờ `init`, nâng cấu hình `init --update` qua hộp thư, `update` + khởi động lại; mục 12.8). | Repo mới: `bint` → hướng dẫn → khởi tạo xong không cần đọc tài liệu; repo cấu hình cũ được nâng qua hộp thư; `update` rồi khởi động lại trong TUI. |
| 6 ✅ | Hoàn thiện: chuột, chế độ hẹp, `NO_COLOR`, chế độ chỉ đọc, trợ giúp theo ngữ cảnh, README; thông báo theo phím TUI, đọc đủ thông báo bị cắt, phím thường dùng luôn thấy (xem dưới). Tách làm hai phiếu: **6a ✅** (tương tác: sửa 5 phát hiện review của 5a/5b, chuột, chế độ hẹp đầy đủ, chân phím không đẩy mất phím, đọc đủ thông báo/ô chi tiết; mục 12.9) và **6b ✅** (viết lại thông báo nhắc lệnh CLI thành phím TUI, `NO_COLOR` đầy đủ, chế độ chỉ đọc theo mục 7.4, trợ giúp theo ngữ cảnh, README, `CurrentPhase = 6`; mục 12.10). | Danh sách chưa làm rỗng, test phủ lệnh và cờ xanh; ảnh chụp ổn định; thử với 2–3 người không quen CLI. |

Yêu cầu bổ sung cho giai đoạn 6 (chủ dự án, 2026-11-12, khi xem màn Tri thức có mục lỗi bằng chứng):

- **Thông báo nói theo TUI, không theo CLI**: lỗi và gợi ý của tầng dịch vụ hiện nhắc lệnh CLI (ví dụ
  lỗi duyệt "xem lại (`bint knowledge show …`) rồi…", kiểm tra bằng chứng "`bint knowledge verify --apply`
  cập nhật vùng"). Trong TUI phải chỉ đúng phím/hành động tương ứng (`v` + ô apply, `e`, `f`…). Có khoảng
  145 chuỗi chứa `bint <lệnh>` trong `internal/app`, `knowledge`, `task`, `workflow`, `eval`, `evolve` (không
  phải chuỗi nào cũng hiện lên TUI): rà hết ở mọi màn, không chỉ Tri thức. CLI giữ nguyên chữ hiện tại.
- **Thông báo bị cắt vẫn đọc được đủ**: thông báo trên thanh trạng thái, ô chi tiết hay hàng danh sách bị
  rút gọn bằng `…` phải có cách xem bản đầy đủ (mở hộp/khung chi tiết, cuộn, hoặc xuống dòng); không được
  mất nội dung lỗi.
- **Phím thường dùng luôn thấy, hoặc dùng được bằng chuột**: chân phím không được đẩy mất phím hay dùng khi
  cửa sổ hẹp; bấm chuột vào phím ở chân, thẻ và hàng thực hiện đúng hành động.

Giai đoạn 1 đem lại giá trị lớn nhất (thay vòng `knowledge review` gõ phím từng mục) nên có
thể phát hành riêng trước khi xong các giai đoạn sau.

## 10. Rủi ro

- **Khối lượng**: ~80 lệnh lá, ~240 cờ. Sổ hành động + form sinh từ `Options` giúp tránh viết
  tay từng màn hình; giữ màn hình chuyên biệt cho những chỗ dùng nhiều (Tri thức, Tác vụ).
- **Tách tầng dịch vụ làm đổi hành vi CLI**: làm trước khi viết TUI, mỗi bước chạy toàn bộ
  test e2e hiện có. Chuyển ràng buộc cờ xuống `Validate()` có thể đổi thông báo lỗi; giữ
  `usageError` ở CLI để mã thoát không đổi.
- **Đồng thời**: hook, `bint run` và TUI cùng chạm dữ liệu. Không thêm khoá mới kiểu
  `evolve.AcquireLock`; nếu cần khoá liên tiến trình, theo `workflow.AcquireWriteLock`.
- **Thay đổi API thư viện TUI**: ghim phiên bản, bọc trong `internal/tui/components`.
- **Thẩm mỹ không đạt kỳ vọng**: chặn sớm bằng màn mẫu ở giai đoạn 0 (mục 4.4); phần vẽ viền
  tự làm nằm gọn trong `components/frame` để chỉnh một chỗ.
- **Độ rộng ký tự** tiếng Việt và emoji khác nhau giữa các terminal: chuẩn hoá NFC, tránh
  emoji và chữ số mũ trong bố cục cột, kiểm thử trên Windows Terminal và một terminal Linux
  phổ biến.
- **Tự cập nhật trong lúc chạy**: binary bị thay khi TUI đang mở; Windows không ghi đè được
  tệp đang chạy (đã xử lý bằng `OldBackup`). TUI không tự `exec` trên Windows.

## 11. Câu hỏi mở

Đã chốt (2026-11-11): `bint` không đối số mở TUI (mục 7); chuẩn bị đa ngôn ngữ từ giai
đoạn 0 (mục 7.1); hỗ trợ chế độ chỉ đọc (mục 7.4); phủ đến cấp cờ (mục 7.3); vòng đời
`init`/`init --update`/`update` trong TUI (mục 7.6); duyệt màn mẫu trước khi làm màn thật
(mục 4.4).

Không còn câu hỏi mở.

## 12. Tiến độ triển khai

### 12.1 Giai đoạn 0 (đã xong, 2026-11-11)

**Tầng dịch vụ** (`internal/app`, `internal/app/evolvesvc`): `knowledge review` (`app.ReviewQueue`),
`bootstrap publish` (`PublishOptions.Confirm`), mọi lệnh lá `eval` (`Eval<X>Options`, `OpenEval`) và
`evolve` (`evolvesvc.<X>Options`, `evolvesvc.Open`) đã tách khỏi `internal/cli`; `app.Status` tách từ
`bint status`. Mỗi lệnh có kiểu `Options` (thẻ `flag:"tên-cờ"`), `Validate()` (ràng buộc mức giá trị,
trả `app.UsageError` → mã thoát 2) và `Defaults(cfg)` (giá trị theo cấu hình; CLI dựng `Defaults` rồi
chỉ ghi đè cờ có `Changed`, nên giá trị mặc định của cờ cobra và `--help` không đổi). Lời hỏi xác nhận
của `eval run`, `eval bootstrap`, `evolve evaluate`, `bootstrap publish` là callback `Confirm` trong
`Options` (không `Confirm` và không `Yes` → `UsageError` "cần --yes khi chạy không tương tác"); tiến độ
đi qua callback có kiểu (`OnEstimate`, `OnNotes`, `Progress`, `OnSelect`, `OnStep`...) để CLI in đúng
thứ tự cũ. Kiểm tra chỉ-dành-cho-người nằm ở dịch vụ (`app.RequireHuman` cho review/propose/process;
`evolve.Apply/Reject/Rollback` tự kiểm). `evolve cycle` chạy **trong tiến trình** qua giao diện
`evolvesvc.CycleRunner` (`Evaluate`, `Propose`); `internal/cli` không còn `os.Executable`/`exec` trong
mã evolve; lệnh con `bint ... --json` cũ và `lastJSON` đã bỏ. Test hộp trắng của cycle, của agent
reflector và của kiểm chứng đã chuyển theo mã (giữ khẳng định).

**Cấu hình và i18n**: khoá `ui.language` (`auto|vi|en`, mặc định `auto`) đủ struct, `Default()`,
`Validate()`, mẫu `Render` (`config.Upgrade` chèn được vào cấu hình cũ). `internal/i18n`: danh mục YAML
nhúng (`locales/vi.yaml`, `locales/en.yaml`), khoá có chấm, tham số `{x}`, số nhiều `one/other`, NFC,
locale giả `xx` (kéo dài ~40%) sinh bằng mã, `Resolve` theo `--lang` > `BINT_LANG` > `ui.language` >
`LC_ALL`/`LANG` > `vi`.

**Khung TUI** (`internal/tui`): `theme` (vai trò màu; tối/sáng/16 màu/đơn sắc), `components/{frame,tabs,
footer,palette,help,logo,text}`, `actions` (sổ hành động + dữ liệu phủ), `screens/{overview,
initplaceholder,placeholder}`, `app` (mô hình gốc, vòng quay của Bubbles `spinner`), `gallery`, `tui.Run`.
Phím: `1-8`/`[`/`]`/`Tab` đổi tab, `:` hoặc `Ctrl+K` bảng lệnh, `?` trợ giúp, `L` đổi ngôn ngữ (vi ⇄ en,
ngay trong phiên), `r` làm mới, `q`/`Ctrl+C` thoát. Điểm vào: `bint` không đối số (TTY, ngoài phiên agent),
`bint tui [--lang] [--read-only]` và `bint tui --gallery` (cờ ẩn).

**Kiểm thử mới**: `Validate`/`Defaults`/`Confirm`/chỉ-dành-cho-người của dịch vụ (cả khi `go test` chạy trong
phiên agent), `cycle` với agent giả chạy `evolve.Propose` và `Evaluate` thật, phủ lệnh + phủ cờ
(`internal/cli/coverage_test.go`, có ca ngược: thêm lệnh/cờ không khai báo làm test đỏ), danh mục i18n
(khoá/tham số khớp giữa locale, quét literal tiếng Việt trong `internal/tui`, mọi khoá dùng trong mã có trong
danh mục), golden `View()` (80×24 và 140×40; vi/en/xx; đơn sắc, và từng chế độ màu ở dạng có mã màu),
trang trưng bày ở locale giả không có dòng vượt chiều rộng, điểm vào CLI (`bint` không TTY / trong phiên agent
in đúng đầu ra của `bint --help`, mã 0; TTY giả gọi TUI).

**Sửa trình bày sau nghiệm thu ảnh (2026-11-11)**: màn Khởi tạo đổi tiêu đề/đoạn mở đầu theo trạng thái
(chưa init / đã xong / lỗi) và không còn nói "chưa khởi tạo" sau khi init xong; hộp phủ (`?`, `:`) che hẳn nền
trong khung nội dung; danh sách gạch đầu dòng thụt treo (`text.WrapHang`, đoạn mã trong dấu huyền không bị
ngắt); Tổng quan căn giữa dọc khi thừa chỗ và mục Doctor chỉ hiện số đếm (chi tiết ở hộp thư); mọi đường dẫn
hiển thị đi qua `text.Path` (`~` + bỏ phần đầu bằng `…`); trạng thái lượt chạy dịch qua i18n
(`overview.runstatus.*`). Sau khởi tạo thành công, `app.initDone` giữ repository là đã init kể cả khi lần nạp
lại lỗi (lỗi hiện ở màn báo cáo và ở Tổng quan); `Ctrl+C` đóng bảng trợ giúp như bảng lệnh.

**Chạy tay**

```sh
go test ./internal/tui/app ./internal/tui/gallery -update     # ghi lại golden sau thay đổi giao diện chủ ý
bint tui --gallery                                            # duyệt component (m: màu, l: ngôn ngữ, j/k: cuộn)
vhs internal/tui/testdata/vhs/overview.tape                   # ảnh PNG/GIF vào internal/tui/testdata/vhs/out/
```

Xem `internal/tui/testdata/vhs/README.md` cho `overview`, `overview-wide`, `init`, `gallery`.

### 12.2 Chỗ lệch so với kế hoạch và việc còn lại

- **VHS chưa chạy thử**: môi trường làm việc không có `vhs`/`ttyd`. Các `.tape` được kiểm cú pháp sơ bộ bằng
  test (`internal/tui/vhs_test.go`) nhưng chưa xuất ảnh thật; chủ dự án cần chạy và chỉnh kích thước cửa sổ nếu
  số cột/dòng lệch 80×24 / 140×40. CI **chưa** xuất ảnh làm artifact vì `.github/workflows/**` là đường dẫn
  được bảo vệ (thêm bước VHS cần người duyệt riêng).
- **Logo**: bản điểm ảnh dùng ký tự khối đầy `██` (mỗi điểm 2 cột × 1 dòng, 5 dòng) ở vùng ≥ 100×30; bản nửa
  khối 3 dòng thử trước đó quá khó đọc nên bỏ. Dưới ngưỡng chỉ hiện chữ "bint." đậm, như kế hoạch.
- **Bảng màu**: giá trị khởi điểm mục 4.2 giữ nguyên, chờ chủ dự án duyệt ảnh. Ở chế độ 16 màu vai trò `fg`
  dùng màu mặc định của terminal thay vì `white` (để đọc được trên cả nền sáng).
- **Chân phím** xếp phím riêng của màn hình trước, rồi phím toàn cục, cuối cùng các phím ghim (`?`, `q`). Từ 6a: thiếu chỗ thì rút nhãn trước khi bỏ phím (mục 12.9).
- **Đổi ngôn ngữ chỉ có hiệu lực trong phiên** (phím `L`): từ giai đoạn 5a, Hệ thống › Ngôn ngữ ghi `ui.language` vào cấu hình (mục 12.7).
  `.bint/config.yml` của chính repository này chưa có khối `ui`: chạy `bint init --update` hoặc chọn ngôn ngữ ở Hệ thống › Ngôn ngữ (chèn khối `ui`).
- **Đổi nhỏ về hành vi CLI** (ngoài phạm vi "không đổi" nên ghi rõ): `knowledge review --status <giá trị lạ>`
  nay là lỗi dùng sai (mã 2) thay vì lặng lẽ không khớp mục nào; với `evolve propose/process`, kiểm tra
  chỉ-dành-cho-người chạy sau khi mở workspace (lỗi "chưa init" thắng lỗi "chỉ dành cho người" nếu cả hai đúng).
- **`evolve cycle` trong tiến trình giữ hành vi cũ khi đợt đối chiếu chạy dở**: bản cũ chạy `evolve evaluate
  --json` nên có đợt (kể cả dừng vì `ErrBudget`/`ErrQuota`) là thoát 0 kèm verdict. `probe` nay dùng verdict
  của phần đã chạy và bỏ lỗi của đợt (chỉ context bị huỷ mới dừng vòng); hết ngân sách vòng thì lần đối chiếu
  kế trả `errBudgetLeft` và vòng dừng êm, mã 0. Có test vòng với runner giả (bước Probe, bước hồi quy).
- **Màn tạm Khởi tạo** (`internal/tui/app`; **đã thay bằng trình hướng dẫn ở giai đoạn 5b**, mục 12.8 — `init.defaults` và `screens/initplaceholder` không còn): hành động `init.*` chỉ có ở màn Khởi tạo (bảng lệnh, chân phím,
  trợ giúp và `run()` cùng lọc qua `applicable`), `init.run` chỉ chạy khi repo còn chưa init. Khởi tạo xong
  thì **giữ** báo cáo (bước, cảnh báo, bước tiếp theo; bước cảnh báo/xung đột lên trước, danh sách dài được
  rút gọn để bước tiếp theo luôn hiện) đến khi bấm Enter "Tiếp tục". `q`/`Ctrl+C` khi đang khởi tạo chỉ nhắc;
  nhấn lần nữa mới thoát (hộp thoại đầy đủ để giai đoạn 2).
- **Nạp Tổng quan**: không chạy song song (yêu cầu thêm trong lúc đang nạp gộp thành đúng một lần nạp lại).
  Ở `--read-only` dùng `app.StatusNoIngest` nên không nạp spool vào SQLite (Tổng quan ở chế độ chỉ đọc có thể
  chậm cập nhật lượt chạy mới); mở cơ sở dữ liệu vẫn có thể áp migration nếu schema cũ.
- **`bootstrap publish`**: `PublishOptions.Yes` + `Confirm`; ghi thay đổi mà không Yes và không Confirm là
  `UsageError` "cần --yes" (như eval/evolve evaluate); `DryRun` không cần xác nhận.
- **Đã tách ở giai đoạn 3**: kiểm `InAgentSession` của `runs rate` nay nằm ở dịch vụ (`app.RateRun`, mục 12.5); của `task approve` đã tách ở giai đoạn 2 (mục 12.4).
- **Chưa có**: `teatest` (golden dùng `View()` của mô hình); thử với 2–3 người không quen CLI và xuất ảnh VHS thật (mục 12.10). **Chuột và chế độ hẹp có từ 6a** (mục 12.9) và **thông báo theo phím TUI, `NO_COLOR`, chế độ chỉ đọc đầy đủ, trợ giúp theo ngữ cảnh, README có từ 6b** (mục 12.10); không dùng `bubblezone` mà tự đánh dấu vùng bấm bằng gói `components/zone` (lý do ở 12.9). Danh sách chưa làm ở
  `internal/tui/actions/coverage.go` (`PendingCeiling` giảm mỗi khi xoá mục: 96 sau giai đoạn 0, 78 sau giai đoạn 1, 61 sau giai đoạn 2, 42 sau giai đoạn 3, 30 sau giai đoạn 4, 12 sau giai đoạn 5a, **0 sau giai đoạn 5b**:
  `Pending` và `PendingFlags` **rỗng**, **`CurrentPhase = 6` từ 6b** (test đòi danh sách rỗng ở giai đoạn 6 nay chạy thật).

### 12.3 Giai đoạn 1 (đã xong, 2026-11-11)

Mốc "xong": duyệt 30 mục của một lần `/b-knowledge` (cùng nhãn `nguon:<lô>`) không gõ chữ. Kiểm bằng
`TestMilestoneApproveThirtyItemsFromInbox` (`internal/tui/app`): repo tạm `app.Init` + 30 mục `origin: agent` bên thứ
ba gắn `nguon:lo-thu` + 1 mục lỗi bằng chứng cùng lô; **3 phím** (Enter trên dòng lô ở hộp thư → `A` → Enter) đưa 30 mục
thành verified, mục lỗi vẫn candidate. Chạy thật binary dưới pty cũng cho kết quả đó (6 mục). Thời gian "< 1 phút" với
người thật chưa đo được tự động (test chỉ đo số phím).

**Tầng dịch vụ** (`internal/app`): `knowledge list/show/add/rm/verify/approve/reject/deprecate/search/stats/diff/
revalidate`, `inspect`, `bootstrap`, `bootstrap skill`, `context` đều có kiểu `Options` (thẻ `flag`, `arg`, `form`),
`Validate()` và `Defaults(cfg)`; CLI chỉ còn phân tích cờ và in văn bản. Mới: `knowledge.Filter` (bộ lọc thuần dùng chung
CLI/TUI, gồm nhãn lô `nguon:*` và đường dẫn theo `Scope.MatchScope`), `PlanBatch`/`ReviewBatch` (lô: `RequireHuman` →
loại mục có finding mức error → `ReviewKnowledge`), `PendingWork` (hộp thư, sáu nguồn, lỗi từng nguồn tách riêng và trả
luôn báo cáo doctor đầy đủ để Tổng quan không chạy doctor hai lần), `EditorArgv`, `TrackedFiles`. Mọi thao tác chỉ dành
cho người kiểm `InAgentSession` ở dịch vụ (test gọi thẳng `Action.Do` với `CLAUDECODE=1` nhận `ErrAgentForbidden`).

**Component** (`internal/tui/components`): `input` (ô nhập một/nhiều dòng, gợi ý mờ), `list` (danh sách ảo + tập chọn),
`viewer` (cuộn, tô diff), `confirm` (xác nhận/nhập lý do), `form` (sinh bằng phản xạ từ `Options`: kiểu, nhóm Nâng cao,
ẩn, bắt buộc, lựa chọn, gợi ý, `Validate` khi gửi tại đúng ô, dòng lệnh CLI tương đương, `Ctrl+Y` chép). Có trong gallery.

**Sổ hành động**: `Action.Do`, `RequestMsg`/`ResultMsg`/`DeniedMsg`/`ExecMsg`/`CancelMsg`; mô hình gốc tra sổ (`Check`) rồi mới
gọi `Do` với context huỷ được (`jobs`). `Action.FormID` cho các hành động dùng chung `Options` dùng chung nhãn. `CurrentPhase = 1`,
`PendingCeiling = 78` (gỡ 19 mục lệnh, thêm `PendingFlags["knowledge add --from-review"] = 2`; `knowledge add --body-file`
vào `ExcludedCmdFlags`). Test phủ có thêm ca ngược: Pending còn mục của giai đoạn đã xong → đỏ; bỏ thẻ `flag` của
`AddOptions` → đỏ. Test i18n buộc mọi trường của mọi `Options` có nhãn `form.<ID>.<cờ|đối số>` ở vi và en.

**Màn Tri thức** (`internal/tui/screens/knowledge`): danh sách + chi tiết (≥ 100 cột hai khung; hẹp một khung, `Tab`/`Enter`
đổi), lọc (`/` chữ, `F` form: trạng thái, loại, nguồn, ⚑, lô `nguon:*`, đường dẫn), duyệt `a`/`f`/`r`/`d`, chọn `x`/`X`, lô
`A`/`R` (hộp xác nhận nêu số mục và liệt kê mục bị loại), sửa `e` (`tea.ExecProcess`), thêm `n`, xoá `Del`, kiểm tra `v`
(`--apply` chỉ người), bảng chuyển `T`, làm mới `Ctrl+R`; tám thẻ phụ (`,` `.`): Danh sách, Tìm kiếm, Thống kê (không
nạp spool ở `--read-only`), Thay đổi, Thẩm định lại (việc nền, xác nhận ngân sách, `Esc` huỷ context), Xuất bản AGENTS.md,
Khảo sát (`inspect`/`bootstrap`/`bootstrap skill`, `m` đổi), Ngữ cảnh. Chi tiết/bằng chứng nạp lười (kiểm tra chỉ mục đang
xem); danh sách chỉ vẽ hàng trong vùng nhìn (test 5 000 mục). Lỗi dịch vụ đã biết dịch qua khoá (`err.*`), mã finding
(`finding.*`) và điều kiện chuyển trạng thái (`transition.<chỉ số>`, test khớp số dòng bảng) dịch; chi tiết khác hiện nguyên
văn kèm nhãn "chi tiết kỹ thuật".

**Hộp thư** (Tổng quan): một dòng mỗi lô `nguon:*`, một dòng tri thức còn lại, thành phần harness, mỗi tác vụ blocked một
dòng (tối đa 5), đề xuất, ứng viên đánh giá, từng cảnh báo/lỗi doctor, và một dòng lỗi cho mỗi nguồn đọc hỏng; cuộn quanh dòng
đang chọn khi nhiều. Enter trên dòng tri thức mở màn Tri thức với bộ lọc chờ duyệt (+ nhãn lô nếu là dòng lô); dòng thuộc tab
chưa làm mở trang giữ chỗ liệt kê việc đang chờ kèm lệnh CLI (`bint task show <id>`, `bint harness component approve <id>`...).

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**

- **Phím**: ở màn Tri thức `Tab` đổi khung (mục 4.3.8), ở Tổng quan vẫn đổi tab; `[` `]` `1-8` luôn đổi tab; `q` `?` `:` `L`
  `Ctrl+C` không bị màn hình chiếm, trừ khi màn đang nhận chữ (ô lọc, form, hộp thoại, form thẻ phụ): khi đó mọi phím vào ô nhập,
  `Esc` thoát (đang chạy việc nền của thẻ phụ thì `Esc`/`Ctrl+C` huỷ việc). Làm mới là `Ctrl+R` (`r` là từ chối, như mục 5.2).
- **Giao diện `Screen`**: lúc đó chưa tách; đã tách ở giai đoạn 2 khi có màn thứ hai (`ui.Screen`, mục 12.4).
- **Xuất bản AGENTS.md hai bước** (xem trước → `w` → xác nhận). Không có kênh/hộp thoại do dịch vụ gọi ngược (hạ tầng đó thuộc
  giai đoạn 2); thay vào đó bước ghi truyền `PublishOptions.Confirm` đồng bộ so `req.Detail` với diff đã xem và **không đặt `Yes`**:
  khác thì dịch vụ không ghi, thẻ hiện cảnh báo kèm diff mới để xem lại rồi `w` lần nữa. `w` bị chặn khi bản xem trước báo không
  có thay đổi (và sau khi đã ghi). `--force` mở hộp màu đỏ nêu rõ kiểm tra bị bỏ qua.
- **Ngữ cảnh không ghi nhận lượt truy xuất**: hành động `context` là chỉ-đọc (`Writes=false`) nên đặt `ContextOptions.NoTrack`
  (bỏ `BINT_SESSION_ID`, không ghi spool/bộ nhớ phiên); ô Session không có trong form TUI.
- **Lý do từ chối mặc định** là hằng `app.DefaultRejectReason` dùng chung CLI và TUI (ghi vào tệp mục nên không theo ngôn ngữ giao
  diện); i18n chỉ dùng cho gợi ý mờ của ô nhập.
- **Chi tiết mục** dựng từ danh sách đã nạp và không gian tri thức mở lúc nạp (`Data.WS` + `DetailOf`), không đọc lại cả kho mỗi lần
  con trỏ dời. Dòng "… còn N dòng" của trích đoạn bằng chứng là `EvidenceView.More`, mỗi bên tự định dạng theo ngôn ngữ.
- **Dòng hộp thư "không thuộc lô nào"** mở bộ lọc có `Filter.NoBatch`, nên danh sách và lô lập bằng `A` khớp đúng số ở dòng đó.
  `PendingWork` báo lỗi nguồn tác vụ cả khi còn tác vụ đọc được (kèm số tệp hỏng).
- **Bằng chứng/phạm vi**: ô nhập có gợi ý mờ từ tệp được Git theo dõi (`→` hoàn tất), không có trình duyệt cây tệp (giai đoạn 6/sau).
- **`knowledge add --from-review`**: lúc đó chờ danh sách phát hiện review của màn Tác vụ; đã làm ở giai đoạn 2 (mục 12.4).
- **Duyệt hàng loạt chặn cả mục stale chỉ lỗi fingerprint** (như CLI bắt `approve --refresh` sau khi xem): duyệt từng mục bằng `f`.
- **Mục có finding mức cảnh báo** (không phải error) vẫn vào lô duyệt; chỉ mức error bị loại.
- **Thẩm định lại/Khảo sát/Xuất bản/Ngữ cảnh** chạy qua form của thẻ phụ, kết quả cuộn trong khung dưới; Thống kê và Thay đổi tự chạy
  khi mở thẻ lần đầu.
- **Đổi nhỏ về hành vi CLI**: không có (các ca mã thoát 2 của `add`/`reject`/`context` có test mới; `bootstrap` vẫn in phần đã làm
  trước khi báo lỗi cài skill, kể cả khi chưa có mục tri thức nào: `BootstrapReport.ItemsRead`). Hai ca CLI **cố ý không** gọi
  `Validate()` để giữ mã thoát cũ (có test `TestKnowledgeLegacyExitCodesKept`): `knowledge revalidate --budget < 0` (agent lệnh
  vẫn chạy; Claude Code trả `ErrNoBudget`, mã 1) và `knowledge search` chuỗi trắng (in "Không tìm thấy.", mã 0). Form TUI vẫn
  kiểm tra hai ca này khi nhập.

**Chạy tay**

```sh
go test ./internal/tui/app ./internal/tui/gallery -update    # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
vhs internal/tui/testdata/vhs/knowledge.tape                 # 80×24; knowledge-wide.tape cho 140×40
```

**Chưa kiểm được tự động**: ảnh PNG VHS (môi trường không có `vhs`; `.tape` mới chỉ qua kiểm cú pháp sơ bộ và phần dựng repo ẩn đã
chạy tay), cảm nhận thẩm mỹ, thời gian thật "< 1 phút" với người, `$EDITOR` thật (test thay bằng hàm giả; `tea.ExecProcess` chưa
chạy trong test).

### 12.4 Giai đoạn 2 (đã xong, 2026-11-11)

Mốc "xong": từ TUI tạo tác vụ, chạy `b-do`, duyệt, xem review đến `done`. Kiểm bằng `TestMilestoneTaskToDone`
(`internal/tui/app`): repo tạm `app.Init` + agent giả tiêm qua `Options.AgentFactory` (b-req ghi ba tài liệu hợp lệ, b-feat
sửa `app.txt`, b-review ghi một vòng không có phát hiện chặn). Kịch bản: form `n` (mô tả, lệnh kiểm chứng `git --version`,
bật "Luôn qua b-req") → `r` (chạy `b-do`, việc nền) → tác vụ **blocked** vì lệnh kiểm chứng chưa được người xác nhận →
`a` (hộp duyệt nêu đúng lệnh; chưa ghi gì tới khi đồng ý) → `v` (kiểm chứng, việc nền) → `c` (tiếp tục) → `done`; thẻ Review
hiện vòng 2/2 của b-review. Chưa đo được tự động: ảnh VHS, `$EDITOR` thật, log của agent thật (claude/codex).

**Tầng dịch vụ** (`internal/app`): `task_svc.go` và `run_svc.go`. Mỗi lệnh `task new/list/show/set/write/status/verify/criteria/
error/review/lint/approve/transitions` và `run`, `run resume`, `run status` có kiểu `Options` (thẻ `flag`/`arg`/`form`),
`Validate()` (trả `UsageError` → mã thoát 2) và `Defaults(cfg)`; `OpenTasks` thay `openWorkflow` của CLI. Hàm mới:
`NewTask`, `ListTasks`, `ShowTask` (phiếu + đánh giá + review + điều chặn done + tài liệu + kết quả lint), `SetTask`,
`WriteTaskArtifact`, `SetTaskStatus`, `VerifyTask`, `CheckTaskCriterion`, `RecordTaskError`, `SubmitTaskReview`, `LintTask`,
`PlanTaskApprove` (**chỉ đọc**: lệnh kiểm chứng sẽ được xác nhận + đường dẫn chờ duyệt/chưa duyệt/`--path`, không ghi
`state.json`), `ApproveTask`, `TaskRuns` (`NoIngest` cho `--read-only`), `RunWorkflow`, `ResumeWorkflow`. `RunCallbacks`
(`Confirm`, `Ask`, `Log`, `OnStep`, `Agent` để tiêm agent giả) nhúng vào Options của `run`/`run resume`; trong phiên agent
`Confirm`/`Ask` không được cài nên Runner không tương tác như trước. Chỉ-dành-cho-người nằm ở dịch vụ: `ApproveTask`,
`SetTaskStatus{Force}` và đặt `autonomy: auto` (ở `NewTask`, `SetTask`, `RunWorkflow`) gọi `RequireHuman` (lỗi bọc
`ErrAgentForbidden`). `knowledge add --from-review` chuyển vào `AddOptions.FromReview` + `FillFromReview` (giữ giá trị đã
đặt, gọi lại không nhân đôi nhãn/đầu vào; `AddKnowledge` tự áp). CLI chỉ còn phân tích cờ, gọi dịch vụ và in như cũ.
`workflow.Runner.Confirm/Ask` nhận `workflow.Question{Key, Text, Params, Paths}` (khoá `QAskVerifyCommand`, `QConfirmFeat`,
`QConfirmScope`): CLI in `Text` nên đầu ra không đổi, TUI dịch theo khoá. Thêm `Runner.OnStep` (`StepEvent`: tác vụ, bước,
lượt con, đường dẫn log, bắt đầu/kết thúc).

**Việc nền** (`internal/tui/jobs`, `internal/tui/app/jobs.go`): bảng việc (`jobs.Table`: hành động, loại, nhãn, tác vụ, lệnh
tiếp tục, log) và cầu `Bridge` dựng `Confirm`/`Ask`/`Log`/`OnStep` gửi `PromptMsg`/`LogMsg`/`StepMsg`/`DoneMsg` qua một
kênh có đệm; một `jobs.Wait` đọc từng thông điệp và đăng ký lại (không đăng ký khi đang hiện câu hỏi, nên thông điệp sau chờ
trong kênh). `Reply` có đệm 1 và hàm hỏi `select` với `ctx.Done()`: huỷ khi câu hỏi còn chờ thì trả "không"/rỗng ngay.
Sổ hành động có `Action.Job` (chạy nền, có cầu) cạnh `Action.Do` (đồng bộ); mỗi lúc **một** việc workflow (`ErrWorkflowBusy`),
việc kiểm chứng và việc thường chạy song song. Log trực tiếp: `eval.runProcess` ghi `LogPath` dần (đã kiểm: `MultiWriter`
vào tệp mở `O_APPEND`, không đệm tới cuối), `Job.ReadTail` đọc phần mới mỗi `TailEvery` (400 ms) và chỉ đọc thêm các dòng
đã hoàn chỉnh; `components/logview` hiện dòng `Runner.Logf` ở trên và đuôi log bước đang chạy ở dưới, tự cuộn tới dòng mới
trừ khi người dùng cuộn lên. Xong thì toast ở mọi màn hình (kết quả `blocked/failed/interrupted` ghi rõ "cần bạn"), danh
sách tác vụ và chi tiết nạp lại, thanh trên hiện số việc nền. **Thoát** khi còn việc (kể cả khởi tạo): hộp thoại liệt kê
việc kèm `bint run resume <id>`; "Ở lại" không làm gì, "Huỷ việc rồi thoát" huỷ mọi context rồi chỉ `tea.Quit` sau khi việc
báo xong hoặc hết `ExitGrace` (15 giây); `Ctrl+C` lần nữa thoát ngay. Thay cơ chế "nhấn lần nữa" của màn Khởi tạo.

**Giao diện `Screen`** (`ui.Screen`: `SetEnv/Ensure/Update/Key/Capturing/WantsInterrupt/Busy/NotList/Command/View`): mô hình
gốc giữ bảng `screens` theo tab (Tri thức, Tác vụ), thông điệp nội bộ của màn hình đi theo `actions.Targeted`, kết quả và từ
chối theo màn hình khai trong sổ; mô hình gốc không còn rẽ nhánh theo từng màn.

**Màn Tác vụ** (`internal/tui/screens/tasks`): danh sách xếp theo nhóm trạng thái (Chờ bạn = blocked/interrupted/failed
lên đầu, Đang làm, Sắp làm, Đã xong; trong nhóm mới cập nhật trước), `A` bật/tắt hiện done+cancelled (như `task list --all`),
`/` lọc chữ, `Ctrl+R` làm mới; ≥ 100 cột hai khung, hẹp một khung (`Tab`/`Enter` đổi). Thẻ con `,` `.`: **Phiếu** (mục tiêu,
phạm vi, tham chiếu, autonomy, phân loại/độ sâu, rủi ro, câu hỏi/ghi chú của đánh giá, chờ duyệt, điều chặn done, yêu cầu
gốc, lịch sử), **Tài liệu** (requirement/plan/tasks dạng Markdown, `d` đổi tài liệu, kèm kết quả lint), **Tiêu chí**,
**Kiểm chứng** (lệnh, đã xác nhận chưa, các lần chạy, đuôi log lần cuối), **Review** (xếp critical → low; `j/k` chọn phát
hiện, `Enter` mở trích đoạn `path:lines` kèm 3 dòng ngữ cảnh — chỉ đọc nên dùng được ở `--read-only`, `e` mở `$EDITOR`
tại dòng đầu của `lines` với `+N`, `K` chuyển thành tri thức), **Lỗi**, **Lượt chạy** (`app.TaskRuns`), và **Log** khi tác
vụ có việc nền (đang chạy hoặc vừa xong; `Esc`/`Ctrl+C` huỷ việc). Hành động: `n` tạo tác vụ, `r` chạy workflow
(`b-do/b-req/b-feat/b-review`, `--harness`, `--role`, `--model`, `--no-review`, độ sâu, `--plan/--no-plan`; trên tác vụ đang
chọn hoặc từ mô tả mới), `c` tiếp tục, `a` duyệt (hộp xác nhận dựng từ `PlanTaskApprove`; hộp đỏ khi có đường dẫn bảo vệ), `s`
đổi trạng thái (form chỉ đưa trạng thái đích hợp lệ theo `task.TransitionsFrom`; `--force` mở hộp đỏ cảnh báo kèm điều còn
thiếu), `T` bảng chuyển, `v` kiểm chứng (việc nền), `l` kiểm tài liệu, `e` sửa phiếu / sửa tài liệu / mở tệp tuỳ thẻ, `i`
ghi lỗi, `m` đối chiếu tiêu chí, `w` ghi vòng review (mẫu JSON trong `$EDITOR`), `K` chuyển phát hiện thành tri thức. Mọi hành
động có trong bảng lệnh; form chân có dòng CLI tương đương. Hộp thư (Tổng quan): Enter trên dòng tác vụ blocked mở thẳng
màn Tác vụ tại tác vụ đó.

**Phủ và kiểm thử**: `CurrentPhase = 2`; gỡ 16 lệnh `task *`/`run *` và `PendingFlags["knowledge add --from-review"]` khỏi
danh sách chưa làm (`PendingCeiling` 78 → 61); `ExcludedCmdFlags["task new --file"]` (TUI soạn mô tả trong ô nhiều dòng); thêm
`serviceBindings` cho task/run và ca ngược "bỏ thẻ `flag` một trường Options của run" làm test đỏ. Test mới: bảng ca `Validate`,
chỉ-dành-cho-người (`CLAUDECODE=1` và `asHuman`), `PlanTaskApprove` không đổi `state.json`, `FromReview` (người, agent, ID sai,
gọi lại), mã thoát 2 của CLI (kể cả khi repo chưa init), khoá câu hỏi của workflow, `OnStep`, markdown/logview (độ rộng chữ có dấu),
màn Tác vụ (nhóm, lọc, thẻ con, trích đoạn đúng dòng, `K`, từng form gửi đúng Options + dòng CLI), cầu hỏi với dịch vụ thật
("không" thì không có `ProtectedApproval`/`VerifyApproval` mới; huỷ khi đang chờ kết thúc trong hạn giờ; chạy `go test -race`),
log trực tiếp thấy dòng mới trước khi bước kết thúc, `Esc` huỷ → `interrupted`, hộp thoại thoát, mọi hành động `Writes` bị từ
chối ở `--read-only` qua phím/bảng lệnh/yêu cầu trực tiếp, `HumanOnly` bị khoá khi `CLAUDECODE=1`, golden màn Tác vụ 80×24
và 140×40 (vi/en/xx, đơn sắc; từng chế độ màu ở dạng có mã màu), trang trưng bày có markdown/logview.

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**

- **`K` tạo mục `candidate`, origin `agent`**: form Thêm tri thức điền sẵn `Origin: agent` (trường nâng cao, đổi được) vì nội dung
  do b-review viết chứ không phải người đã kiểm; `bint knowledge add --from-review` ở CLI vẫn theo người dùng (người → `verified`,
  agent → `candidate`). Người vào màn Tri thức duyệt như mục khác.
- **"Enter tới tệp:dòng" là mở trích đoạn trong TUI** (chỉ đọc); `e` mới mở `$EDITOR` (`+N` cho vi/vim/nvim/nano/emacs/micro,
  `tệp:N` cho hx/helix, trình khác chỉ mở tệp). Ghi vòng review bằng mẫu JSON trong `$EDITOR`; sửa tài liệu qua tệp tạm rồi
  `task write` (không ghi thẳng vào tệp tác vụ) và bỏ qua nếu nội dung không đổi.
- **Markdown tự viết** (tiêu đề, danh sách, ô kiểm, khối mã, trích dẫn, đường kẻ, chữ đậm và mã nội dòng); không bảng/ảnh/liên kết
  và không thêm phụ thuộc vào `go.mod`.
- **Kiểm chứng không tự chạy khi duyệt**: `a` chỉ xác nhận lệnh; sau đó phải `v` (hoặc để Runner chạy khi lệnh đã xác nhận ngay
  sau b-feat) trước `c`, vì tiếp tục từ `blocked` chạy lại b-review chứ không chạy lại kiểm chứng. Mốc `TestMilestoneTaskToDone`
  đi đúng chuỗi này.
- **`Confirm` trả không với `checkScope`** vẫn ghi `PendingPaths` và chuyển blocked (hành vi có sẵn của CLI); điều "không ghi" là
  không có `ProtectedApproval`/`VerifyApproval` mới (có test). Dịch vụ dùng `ConfirmOrFail` thì không ghi gì.
- **`--read-only`**: danh sách/chi tiết không đối chiếu lại tiến trình điều phối đã chết (`NoReconcile`) và thẻ Lượt chạy không nạp
  spool (`NoIngest`); mở cơ sở dữ liệu vẫn có thể áp migration nếu schema cũ (như Tổng quan).
- **Việc chạy từ mô tả mới** chưa biết tác vụ cho tới bước đầu tiên: log của nó hiện ở thẻ Log của tác vụ đang chọn, `Job.TaskID`
  được điền khi nhận `StepMsg` đầu, và con trỏ nhảy tới tác vụ vừa lập khi việc xong.
- **Phím**: `Ctrl+R` làm mới (như Tri thức; mục 4.3.8 dùng `r`), `A` hiện cả xong/huỷ, `.`/`,` đổi thẻ con; `q ? : L [ ] 1-8` vẫn là
  phím toàn cục trừ khi màn đang nhận chữ (ô lọc, form, hộp thoại).
- **Đổi nhỏ về hành vi CLI**: thông báo lỗi của "đặt `autonomy: auto` trong phiên agent" nay bọc `app.ErrAgentForbidden` (kèm tên biến
  agent) thay vì `workflow.ErrAgent`; mã thoát vẫn 1. Lỗi dùng sai vẫn được kiểm trước khi mở workspace nên mã 2 thắng mã 4. Đầu ra
  và mã thoát của mọi e2e cũ giữ nguyên (không sửa khẳng định nào).
- **Chưa làm** (đúng phạm vi): chạy nhiều workflow song song trong một phiên TUI, huỷ việc của tiến trình khác, trình duyệt cây tệp,
  tô cú pháp trích đoạn, ảnh VHS và bước CI xuất ảnh (`.github/workflows/**` được bảo vệ), chuột và chế độ hẹp đầy đủ (giai đoạn 6).
  Mục tri thức k:01M746PHJXDZN6QMA3BEVEDNPP và các phát hiện mức thấp của giai đoạn 1 (12.3) vẫn còn mở.

**Chạy tay**

```sh
go test ./internal/tui/app ./internal/tui/gallery -update    # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
go test -race ./internal/tui/app -run 'Confirm|Cancel|LiveLog|Exit|Milestone'   # việc nền, câu hỏi, huỷ, thoát
bint tui                                                      # 3 → n (tạo) → r (chạy b-do); q khi còn việc → hộp thoại thoát
```

### 12.5 Giai đoạn 3 (đã xong, 2026-11-11)

Mốc "xong": áp dụng và quay lui một đề xuất hoàn toàn trong TUI. Kiểm bằng `TestMilestoneApplyRollbackProposal` (`internal/tui/app`,
dịch vụ thật, không gọi mô hình): repo tạm có mục tri thức được truy xuất nhưng chưa trích dẫn và spool 3 phiên có nhãn → `4` rồi `I`
(nạp spool) và `S` (quét Git) → `6` rồi `s` (quét cơ hội) → chọn cơ hội `unused`, `p` (bật "Không dùng agent", việc nền có log) → thẻ Đề xuất
tự mở tại đề xuất loại bỏ (deprecate) → `a` (hộp đỏ vì chưa kiểm chứng; chọn các ô rồi Enter) → mục thành `deprecated` → `u` (hộp quay lui,
không xung đột) → mục hết `deprecated`; thẻ Lịch sử ghi cả `apply` và `rollback`. Chưa đo được tự động: ảnh VHS, cảm nhận thẩm mỹ, `propose`/
`evaluate`/`cycle` với mô hình thật.

**Tầng dịch vụ**: `internal/app/runs_svc.go` — `runs list/show/rate/scan` và `events ingest` có `Options` (thẻ `flag`/`arg`/`form`),
`Validate()` (trả `UsageError`) và `Defaults(cfg)`; hàm `ListRuns` (kèm nhãn + lý do mỗi lượt), `ShowRun` (lượt, nhãn, tín hiệu, đầu ra, sự kiện),
`RateRun`, `ScanRuns`, `IngestEvents`; `OpenRuns(ctx, dir, ingest)` thay `openAndIngest` của CLI (mở không nạp spool cho `--read-only`). **`RateRun`
giữ thứ tự lỗi của CLI cũ**: tham số sai (mã 2) → `RequireHuman` → mở workspace. `internal/app/evolvesvc`: `experience digest/show`
(`Digest`, `Experience`), `OpenNoIngest`/`OpenIngest` (biến thể mở không nạp spool), thẻ `arg`/`form` cho `Options` đã có (trường `--unverified`,
`--confirm-risk`, `--force`, `--yes` là `form:"hidden"`: chỉ đặt qua hộp cảnh báo/hộp xác nhận), `PlanApply` (chỉ đọc: trạng thái, phiên bản gốc còn khớp,
đã kiểm chứng độc lập hay chưa, rủi ro cao + lý do, đề xuất skill/harness, danh sách thay đổi), `PlanRollback` (chỉ đọc: tệp sẽ khôi phục kèm nội dung
trước/sau/hiện tại và tập xung đột), `RollbackOptions.ExpectConflicts` (tệp xung đột → dấu vân tay sha256 nội dung lúc xem, dựng bằng
`RollbackPlan.SeenConflicts`; khác `nil` + `Force`: nếu tập xung đột hoặc nội dung một tệp xung đột đã đổi so với lúc xem thì từ chối, không ghi — trường
không có thẻ `flag` nên CLI không đặt) và `ShowResult.Harness` (thành phần quy trình hiện có để so với đề xuất). Form
(`components/form`) hỗ trợ thêm `*float64` (ô trống = `nil`, cho `--budget` của `evolve evaluate`) và `uint64` (`--seed`). CLI chỉ còn phân tích cờ,
gọi dịch vụ và in; cờ lấy giá trị mặc định từ `Defaults(config.Default())`. Đầu ra và mã thoát của mọi e2e cũ giữ nguyên (không sửa khẳng định nào).

**Sổ hành động và phủ**: `actions/runs.go` (`runs.rate/scan/ingest/list/show/lesson_show/lesson_digest` + hành động giao diện) và
`actions/evolve.go` (`evolve.scan/propose/list/show/evaluate/apply/plan_apply/reject/rollback/plan_rollback/history/queue/process/cycle`).
`Writes`: rate, runs scan, ingest, propose, evaluate, apply, reject, rollback, process, cycle. `HumanOnly`: rate, propose, apply, reject, rollback, process
(đúng nơi dịch vụ gọi `RequireHuman`). `propose/evaluate/process/cycle` là `Action.Job` (`jobs.KindEvolve`; mỗi lúc **một** việc loại này trong tiến
trình TUI, việc thứ hai nhận `ErrEvolveBusy` và chỉ hiện thông báo); cầu `Bridge`: dòng tiến độ, kế hoạch Probe, ước tính chi phí (dịch qua i18n, khoá
`evolve.log.*`), `Warn`/`Log` (io.Writer) tách dòng gửi `br.Log`, `Confirm` thành hộp thoại (khoá `prompt.evolve.evaluate.confirm.*`) kèm chính các dòng
chọn tác vụ/kế hoạch/ước tính làm phần chi tiết để người quyết định theo ước tính. `Env` thêm `Cat` (danh mục chuỗi cho log) và `CycleRunner` (tiêm
runner giả trong kiểm thử). `CurrentPhase = 3`; gỡ 19 lệnh (runs ×4, events ingest, experience ×2, evolve ×12) khỏi `Pending` (`PendingCeiling` 61 → 42);
`serviceBindings` thêm runs/events/experience; các ca ngược của `coverage_test.go` đổi mẫu từ `runs list` sang `eval list`/`sync` (giai đoạn 4–5) và vẫn đỏ
đúng chỗ.

**Màn Lượt chạy** (`internal/tui/screens/runs`): danh sách mới trước (`l` đổi `--limit`, `/` lọc, `Ctrl+R` làm mới), chi tiết (trạng thái + lý do kết
thúc, harness, mô hình, Git, thời gian, nhãn + lý do, tín hiệu, đầu ra, sự kiện cuộn được); ≥ 100 cột hai khung, hẹp một khung (`Tab`/`Enter` đổi). `+`/`-` mở
hộp nhập ghi chú rồi chấm good/bad (chỉ người); `S` form quét Git (days, revert-days mặc định 30/14 từ `Defaults`); `I` nạp spool. Thẻ **Bài học** (`,`/`.`):
thư viện bài học (chung trước, riêng nhóm theo vùng; số lần chèn, đạt/trượt) và `d` bản tóm tắt thất bại theo đợt/lượt/`latest` (ở thẻ Danh sách điền sẵn lượt
đang chọn) kèm nhóm và trạng thái sẵn sàng.

**Màn Tiến hoá** (`internal/tui/screens/evolve`): thẻ **Cơ hội** (kết quả quét không quét Git khi nạp; `s` quét đầy đủ; điều kiện tiên quyết, nhóm tác vụ,
chi tiết cơ hội; `p` đề xuất cho cơ hội đang chọn), **Đề xuất** (đang chờ; `A` mọi trạng thái; chi tiết có thẻ con `v`: Thông tin / Thay đổi / Đối chiếu), **Lịch sử**
(`history` + tổng vòng tự động), **Hàng đợi** (`queue`, `A` = `--all`), và **Log** khi có việc nền. Thẻ con Thay đổi: tri thức = diff tiêu đề/phạm vi/bằng chứng/nội
dung với hiện trạng (`skills.Diff`); skill = diff `SKILL.md`; harness = khung riêng theo loại (guidance: tiêu đề + Markdown; guardrail: on/when/unless/action, lời
nhắc, rủi ro; script: mô tả, cách dùng, nội dung kèm cảnh báo "Bint không bao giờ tự chạy; chờ người duyệt"; strategy: `strategy.yml`) kèm diff với thành phần hiện
có cùng loại và ID. Phím: `s p E a x u P C`, `A`, `v`, `Ctrl+R`; Esc/Ctrl+C ở thẻ Log huỷ việc đang chạy.

**Hộp xác nhận**: `a` gọi `evolve.plan_apply` rồi dựng hộp từ `ApplyPlan`: đề xuất không áp dụng được (không ở `proposed` hoặc gốc đã đổi) chỉ có hộp đọc; cần
`--unverified`/`--confirm-risk` thì hộp **đỏ** có một ô chọn riêng cho từng cờ (mặc định không chọn; `components/confirm` thêm `Checks`: Tab/↑↓ dời, Space/`x` đảo, y/n
không xác nhận khi có ô chọn) và ô ghi chú; không chọn thì yêu cầu gửi không có cờ và dịch vụ trả lỗi nêu rõ cờ. `u` gọi `evolve.plan_rollback`: có tệp xung đột thì hộp đỏ
với ô `--force` và diff "hiện tại (sẽ mất) → sau quay lui" của từng tệp xung đột (tối đa 20 dòng mỗi tệp) — chọn thì gửi `Force` kèm `ExpectConflicts`
(tệp + nội dung đã xem), không chọn thì không `Force`. `a/x/u/E` chỉ thao tác ở thẻ Đề xuất; ở thẻ khác chúng chuyển sang thẻ Đề xuất và nhắc chọn đề xuất. `x` mở hộp bác bỏ có ô lý do. Sau apply/rollback đề xuất skill, hành
động gọi `app.InstallSkills` và trả kết quả đồng bộ trong `ApplyOutcome`/`RollbackOutcome` (hộp kết quả hiện "đã đồng bộ"/xung đột/lỗi); CLI giữ `syncAfterSkillChange`.

**Tích hợp**: hai màn vào bảng `screens`; `overview.NavMsg.ProposalID` + `evolve.Focus(id)` (nạp lại với mọi trạng thái nếu đề xuất không còn chờ); hộp thư
nay có **một dòng mỗi đề xuất** (khoá `overview.inbox.proposal`, tối đa 5 rồi "và N đề xuất khác"), Enter mở màn Tiến hoá tại đề xuất đó; `screens/placeholder` bỏ Lượt chạy và
Tiến hoá. Sau khi `propose` tạo được đề xuất, thẻ Đề xuất tự mở tại đề xuất đầu tiên.

**Phủ và kiểm thử**: test dịch vụ (`Validate` + `Defaults` + `CLAUDECODE=1`/`asHuman` của `RateRun`, `OpenRuns`/`OpenNoIngest` không nạp spool, `PlanApply`: chưa kiểm
chứng/rủi ro/gốc đã đổi/đã áp dụng, `PlanRollback`: xung đột, `Rollback` với `Force` + `ExpectConflicts` khác tập hoặc khác nội dung → lỗi và tệp không đổi), test CLI (thứ tự lỗi mã 2 trước "chỉ người",
mặc định cờ lấy từ `Defaults`), test form (`*float64`, `uint64`), test `confirm` (ô chọn), test hành động với dịch vụ thật (đồng bộ skill sau apply/rollback, quét chỉ-đọc ép `NoGit` và
không nạp spool, các hành động đọc), test màn hình (lọc, thẻ, khung hẹp, form quét/digest gửi đúng `Options` + dòng CLI, bài học nhóm theo vùng, bốn loại harness + tri thức + skill, diff với
thành phần hiện có, hộp áp dụng/bác bỏ/quay lui, log trực tiếp, một việc nặng mỗi lúc, Esc huỷ, `Confirm` của `evaluate` trả "không"/huỷ khi đang hỏi không treo, `--read-only` từ chối mọi
`Writes`, `CLAUDECODE=1` khoá `HumanOnly`, Enter ở hộp thư), golden 80×24 và 140×40 (vi/en/xx, đơn sắc; thêm mã màu ở bốn chế độ cho một khung mỗi màn), `i18n_scan_test` (tiền tố động mới
`action.runs.`, `action.evolve.`, `prompt.evolve.`, `rn.*`, `ev.*`), `.tape` cho hai màn (`runs-wide.tape`, `evolve-wide.tape`).

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**

- **Xem trước apply không là tệp đích**: `evolve.Apply` không có dry-run và `internal/evolve/**` bị chặn ghi nên "xem trước" = nội dung đề xuất so với hiện trạng (thẻ Thay đổi) cộng điều kiện từ `PlanApply`;
  tệp thực sự đã ghi hiện sau khi áp dụng (`ApplyResult.Files`). `Apply` tự kiểm lại mọi điều kiện lúc ghi nên sai lệch giữa lúc xem và lúc ghi chỉ làm thất bại an toàn. Quay lui xem trước được chính xác
  (bản kiểm toán có Before/After).
- **`evolve scan` ở `--read-only`** ép bỏ quét Git và không nạp spool (mục 7.4); ở chế độ thường như CLI (nạp spool + quét Git). Khi nạp màn hình luôn quét **không** quét Git (tín hiệu Git đã ghi vẫn dùng);
  mở cơ sở dữ liệu vẫn có thể áp migration nếu schema cũ (như các màn trước).
- **Khoá "một việc Tiến hoá" chỉ trong tiến trình TUI** (bảng việc). Không thêm khoá liên tiến trình: apply/rollback dùng khoá có sẵn của `evolve` (`AcquireLock`, mẫu O_EXCL — xem tri thức
  k:01M746PTBYPSXDT0WJZS38KPFD: không chép mẫu này cho khoá mới); `Rollback` với `ExpectConflicts` vẫn còn một cửa sổ rất nhỏ giữa lúc kiểm tập xung đột và lúc `evolve.Rollback` ghi (kiểm lại nằm ngoài khoá).
- **Phím**: `+`/`-` chấm good/bad (`g`/`Home` về đầu, `G`/`End` về cuối, như màn Tiến hoá); `Ctrl+R` làm mới, `A` mọi trạng thái, `.`/`,` đổi thẻ (như màn Tác vụ).
- **Hộp thư đổi hình dạng**: dòng "N đề xuất tiến hoá chờ duyệt" (khoá `overview.inbox.evolve`) thay bằng một dòng mỗi đề xuất (golden Tổng quan đổi đúng một dòng).
- **Form `evolve evaluate`**: cờ "tường minh" (`RepeatsExplicit`, `DeliveryExplicit`) suy ra khi giá trị khác mặc định theo cấu hình (tương đương "cờ có mặt" của CLI về hiệu lực).
- **`evolve propose` không bắt buộc ngân sách ở form** (như CLI: chỉ cơ hội cần agent mới cần `--budget`, `evolve.Propose` kiểm theo từng cơ hội); `evolve cycle` bắt buộc (`required`, `Validate`).
- **Chưa làm** (đúng phạm vi): chạy nhiều việc nặng của Tiến hoá song song, huỷ việc của tiến trình khác, ảnh VHS thật và bước CI xuất ảnh (`.github/workflows/**` được bảo vệ), màn Đánh giá (giai đoạn 4), chuột và chế độ hẹp đầy đủ (giai đoạn 6),
  6 phát hiện review còn mở của giai đoạn 2 và phát hiện mức thấp của giai đoạn 1 (phiếu riêng; riêng phát hiện TOCTOU của hộp duyệt đã được xử lý cho `rollback --force` bằng `ExpectConflicts`).

**Sửa theo review (vòng 1, sửa trực tiếp)**: phím `a/x/u/E` ở thẻ không phải Đề xuất từng thao tác lên đề xuất ẩn (medium) — nay chuyển thẻ và nhắc
(`TestEvolveProposalKeysNeedProposalsTab`); `Focus` khi đang nạp không còn chạy hai lần nạp song song (`TestFocusWhileLoadingDoesNotRaceStaleLoad`); `ExpectConflicts`
so cả nội dung và hộp quay lui hiện phần sẽ mất; bỏ trường `NoIngest` không dùng của `RunsListOptions`/`RunsShowOptions` (nạp spool do `OpenRuns` quyết định); phím chấm đổi
`g`/`b` → `+`/`-`.

**Chạy tay**

```sh
go test ./internal/tui/app ./internal/tui/gallery -update    # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
go test -race ./internal/tui/app -run 'Milestone|Confirm|Cancel|LiveLog|Exit|OneHeavy|Evolve'   # việc nền, câu hỏi, huỷ
bint tui                                                      # 4 (Lượt chạy) → I (nạp spool) → 6 (Tiến hoá) → s → p → a/u
```

### 12.6 Giai đoạn 4 (đã xong, 2026-11-11)

Mốc "xong": chạy một đợt nhỏ và đọc báo cáo trong TUI. Kiểm bằng `TestMilestoneEvalRunReport` (`internal/tui/app`, dịch vụ thật, `sh`): repo tạm có `eval.test_command` và một commit lời
giải kèm kiểm thử mới → `5` rồi `m` (bật "Kiểm chứng fail→pass ngay", việc nền có log) → thẻ Ứng viên hiện ứng viên đã kiểm chứng → `a` (hộp thường, Enter) → thẻ Tác vụ có tác vụ → `r` (form: một nhánh
`none`, 2 lần lặp, agent lệnh tuỳ chỉnh ở nhóm Nâng cao; dòng CLI tương đương khớp) → hộp xác nhận kèm ước tính và lưu ý → Enter → đợt `completed` (2/2 lượt) → thẻ Đợt tự mở báo cáo với dòng biểu đồ cột
`none █████… 100%`. Cùng nhóm kiểm thử: trả "không" ở hộp xác nhận không tạo đợt nào trong SQLite; huỷ khi đang hỏi kết thúc trong hạn giờ; Esc khi đang chạy cho đợt `cancelled` và màn hiện
`bint eval run --resume <đợt>`. Chưa đo được tự động: ảnh VHS, cảm nhận thẩm mỹ của biểu đồ, đợt với harness thật (claude/codex, có chi phí).

**Tầng dịch vụ** (`internal/app/eval_cases.go`, `eval_run.go`): thẻ `arg`/`form` cho mọi trường `Options` dùng trong form — `IDs` là `arg:"id"` (gợi ý `candidates`), `EvalShowOptions.ID` bắt buộc,
`EvalReportOptions.BatchID` là `arg:"batch-id"`; `--split` có `choices=build|validation|test`, `--leakage-mode` có `choices=strict|economy`; `--arms`, `--harness`, `--baseline` có gợi ý mờ
(`arms`, `harnesses`) thay vì `choices` (`choices` chỉ áp cho ô một giá trị, còn `--arms` là danh sách); nhóm Nâng cao: `--limit`, `--seed`, `--parallel`, `--keep-workdirs`, `--bare`,
`--cost-per-run`, `--agent-command`, `--model`, `--effect`, `--verify-runs`, `--runs`; `form:"hidden"`: `--yes`, `--resume` (đặt bởi hành động "Chạy tiếp"), `--list`, và `--force`/`--unverified` của approve
(chỉ đặt qua ô chọn của hộp đỏ). Thêm `EvalRunOptions.OnPlan func(EvalRunPlan)` với `EvalRunPlan{BatchID, CaseIDs, Arms, Repeats, Done}`: gọi **trước** lời hỏi xác nhận, ở đợt mới (sau `Pick` và
`resolveArms`) và ở `--resume` (`Done` đọc bằng `eval.Attempts`, bỏ lượt đang chạy dở và lỗi hạ tầng như `eval.Run`, vì các lượt đó chạy lại). CLI không đặt `OnPlan` nên đầu ra không đổi; mọi e2e eval cũ
chạy nguyên, không sửa khẳng định nào. **`components/form`** nay duyệt trường nhúng (`Field.index` thành đường chỉ số, `FieldByIndex`, đệ quy vào trường `Anonymous` như `actions.flagNames`): trước đó
`EvalSelection` (`--split/--case/--limit`) nhúng trong `EvalRunOptions`/`EvalEstimateOptions`/`EvalBootstrapOptions` bị bỏ qua; ô của struct nhúng đứng đúng vị trí khai báo, thứ tự ô của struct không nhúng giữ nguyên.

**Sổ hành động** (`actions/eval.go`): `eval.mine/verify/list/show/spec/approve/reject/estimate/run/report/bootstrap/clean` cùng hành động giao diện (`eval.resume`, `eval.open_file`, `eval.baseline`,
`eval.split`, `eval.filter`, `eval.refresh`, `eval.next_view`, `eval.prev_view`). `Writes`: mine, verify, spec, approve, reject, run, bootstrap, clean, và hai hành động giao diện ghi (`resume`, `open_file`). Việc
nặng chạy nền (`Action.Job`, loại mới `jobs.KindEval`, **mỗi lúc một việc**, việc thứ hai nhận `actions.ErrEvalBusy` và chỉ hiện thông báo): run, resume, bootstrap, verify, mine khi bật kiểm chứng (mine không
kiểm chứng là việc thường). `runEvalRun`: `OnResume/OnEstimate/OnNotes/Progress` thành dòng log dịch qua khoá `eval.log.*`; `Confirm` thành hộp thoại (khoá `prompt.eval.run.confirm.*`,
`prompt.eval.bootstrap.confirm.*`) kèm chính các dòng ước tính + lưu ý làm phần chi tiết; **không bao giờ đặt `Yes`**. `ArmsExplicit`/`BudgetExplicit` suy ra khi giá trị khác mặc định theo cấu hình. Lưới ô cần
thông điệp có kiểu từ việc nền: thêm `jobs.Bridge.Send(v any)` sinh `jobs.DataMsg{Job, Value}` (mô hình gốc chuyển cho màn sở hữu việc); màn nhận `app.EvalRunPlan`, `actions.EvalAttempt` (một lượt kèm
`N`/`Total`), `actions.EvalVerifyStep` và `actions.EvalBootstrapProgress`, không phân tích chữ log. `coverage.go`: gỡ 12 lệnh `eval`, `CurrentPhase = 4`, `PendingCeiling` 42 → 30; các ca ngược của
`coverage_test.go` đổi mẫu từ `eval list` sang `sync` (giai đoạn 5) và vẫn đỏ đúng chỗ.

**Màn Đánh giá** (`internal/tui/screens/eval`): thẻ **Ứng viên** (chờ duyệt; `A` gồm đã loại; chi tiết: đề bài dựng bằng trình xem Markdown, nguồn commit + gốc, kiểm chứng, rò rỉ (tính lại khi xem),
đặc tả + chữ ký giao diện, lý do loại, lệnh chấm), **Tác vụ** (đã duyệt, `t` lọc theo tập build/validation/test), **Đợt** (danh sách; chi tiết là báo cáo: thông tin đợt, **biểu đồ cột tỷ lệ đạt + CI95**
theo nhánh, từng nhánh (đạt kèm CI, chấm/lượt, lỗi/bỏ/quá giờ, chi phí, thời gian, lượt agent, dao động, mô hình), so sánh theo cặp, bảng theo tác vụ, guardrail, chẩn đoán độ tin cậy, chẩn đoán cổng 2; `b` đổi
mốc `--baseline`), **Log** khi có việc nền (lưới ô + log trực tiếp; Esc huỷ an toàn). ≥ 100 cột (vùng nội dung, như các màn trước) hai khung, hẹp một khung (`Tab`/`Enter` đổi); `/` lọc, `Ctrl+R` làm mới, `,`/`.` đổi thẻ. Phím:
`m v V p a x e A t r R E b B C`. Hàm định dạng số và CI viết trong màn (không import `internal/cli`).

**Hộp duyệt/loại** dựng từ dữ liệu đã nạp: ứng viên hợp lệ → hộp thường có ô nhập tập (trống = tự chia theo ID); bị chặn (đã loại, rò rỉ `fail`, chưa kiểm chứng) → hộp **đỏ** có ô chọn riêng cho `--force` (nêu lý do:
đã loại / lộ lời giải) và `--unverified`, mặc định không chọn — không chọn thì yêu cầu gửi không có cờ và dịch vụ trả `Problem` nguyên văn. `EvalApprove` tự kiểm lại lúc ghi nên lệch giữa lúc xem và lúc ghi
chỉ thất bại an toàn. Hộp kết quả liệt kê từng ID (`EvalApproveOutcome`: đã duyệt kèm tập và ghi chú, đã duyệt từ trước, không duyệt được + lý do). `x` loại kèm ô lý do (trống = lý do mặc định của CLI). `C` hỏi trước khi
xoá kho đệm. **Chạy tiếp**: `R` ở thẻ Đợt trên đợt chưa `completed` mở form dùng chung ô với `eval run` (đặt lại ngân sách hoặc lệnh agent: đợt chạy bằng agent lệnh phải tiếp tục bằng chính harness đó; các ô tác vụ/nhánh/lặp bị
dịch vụ bỏ qua) rồi hộp xác nhận ở việc nền.

**Component `components/barchart`**: `Render(th, rows, width)`; `Row{Label, Value, Lo, Hi, Valid, Note}`; cột ngang, nhãn căn theo `text.Width` sau NFC, khối đặc (accent) cho phần đạt, khối rỗng (muted), hai đầu khoảng tin cậy bằng
`[` `]` (hoặc `┃` khi một ô) màu key, phần trăm + `[lo–hi]` bên phải khi đủ rộng (≥ 44 cột), chú thích khi ≥ 64 cột; CI không hợp lệ thì không vẽ mốc và không in khoảng. Có trong gallery (`gallery.section.barchart`).

**Tích hợp**: màn vào bảng `screens`; `overview.NavMsg.EvalCaseID` + `Focus(id)`; hộp thư nay có **một dòng mỗi ứng viên đánh giá** (khoá `overview.inbox.eval`, tối đa 5 rồi "và N ứng viên khác"), Enter mở màn Đánh giá, thẻ
Ứng viên, chọn đúng ứng viên; `screens/placeholder` chỉ còn tab Hệ thống. `eval run` xong → toast và thẻ Đợt mở báo cáo của đợt vừa chạy; đợt dừng (huỷ, ngân sách, hạn mức) → hộp thoại kèm lệnh `--resume`.

**Phủ và kiểm thử**: test dịch vụ (`OnPlan` trước `Confirm` ở đợt mới và `--resume`, trả không không tạo đợt), test form (struct nhúng; mỗi form eval gửi đúng `Options` + dòng CLI; trường ẩn không có ô), test hành động với dịch vụ
thật (mine, spec, verify nền, list, show, approve, reject, estimate, report, clean; `run`/`bootstrap` qua cầu kênh), test `Writes`/`HumanOnly`, test màn hình (thẻ, lọc, tập con, khung hẹp, báo cáo từ `eval.Report` dựng tay, đổi mốc,
hộp duyệt thường/đỏ + ô chọn, hộp loại, lưới ô nhận `DataMsg`, Esc huỷ, `ErrEvalBusy`, chạy tiếp, `--read-only` từ chối mọi `Writes` qua phím, bảng lệnh và yêu cầu trực tiếp cho cả Do và Job, Enter ở hộp thư, không dòng nào vượt
chiều rộng), mốc `TestMilestoneEvalRunReport` và hai kiểm thử dịch vụ thật (trả không/huỷ khi đang hỏi, Esc → `cancelled`), golden 80×24 và 140×40 (vi/en/xx, đơn sắc; thêm mã màu ở bốn chế độ cho báo cáo), `i18n_scan_test` (tiền
tố động mới `action.eval.`, `prompt.eval.`, `eval.tab.`, `eval.st.`, `eval.vs.`, `eval.ls.`, `eval.ss.`, `eval.bst.`, `eval.job.title.`, `eval.approve.verify_`), `eval-wide.tape`.

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**

- **`eval approve/reject` không là `HumanOnly`**: CLI hiện không chặn `InAgentSession` ở hai lệnh này (mục 2 của kế hoạch không liệt kê eval trong thao tác chỉ dành cho người) và giai đoạn này giữ nguyên hành vi CLI. Muốn khoá
  thì cần phiếu riêng (đổi CLI + đặt `HumanOnly`).
- **Loại việc `KindEval` riêng**, tách khỏi `KindEvolve` (`evolve evaluate` cũng chạy eval): hai bên ghi đợt riêng và CLI cho chạy song song; mỗi loại một việc đủ chống bấm nhầm hai lần.
- **Ô không có trạng thái "đang chạy"**: dịch vụ chỉ báo lượt đã xong (`Progress` sau `runAttempt`), không có sự kiện bắt đầu lượt, nên ô chỉ có chờ hoặc kết quả (đạt, trượt, quá giờ, lỗi, bỏ). Ô "đang chạy" cần sửa
  `internal/eval/runner.go` (ngoài phạm vi).
- **`EvalEstimateDefaults.Harness` là "claude"** còn `EvalRunDefaults.Harness` là `eval.harness`: giữ đúng CLI.
- **`--arms`/`--harness` là ô gợi ý mờ**, không phải ô chọn (`choices` chỉ áp cho giá trị đơn).
- **Lưới ô và `DataMsg`** thay vì phân tích chữ log (mục 7); `actions.EvalAttempt` bọc `eval.AttemptRow` kèm `N`/`Total`.
- **Hộp thư đổi hình dạng**: dòng "N ứng viên đánh giá chờ duyệt" thay bằng một dòng mỗi ứng viên (golden Tổng quan không đổi vì mẫu không có ứng viên; test hộp thư đổi).
- **`eval estimate` ở `--read-only`** vẫn dùng được (đọc, mở cơ sở dữ liệu có thể áp migration nếu schema cũ như các màn trước); mọi hành động `Writes` bị ẩn/từ chối.
- **Chưa làm** (đúng phạm vi): duyệt/loại theo lô (chọn nhiều; dịch vụ đã nhận nhiều ID nhưng form/hộp một ID), sửa đề bài ngoài việc mở tệp ứng viên bằng `e`, chạy nhiều đợt song song, huỷ đợt của tiến trình khác,
  `bootstrap --list` (ảnh chụp tri thức), `eval report --cases` (đã loại trừ: bảng theo tác vụ luôn hiện), ảnh VHS thật và bước CI xuất ảnh (`.github/workflows/**` được bảo vệ), chuột và chế độ hẹp đầy đủ (giai đoạn 6).

**Lỗi tìm thấy khi viết kiểm thử (đã sửa)**: `rebuild()` của màn dùng chỉ số danh sách cũ khi `Data` vừa bị thay bằng danh sách ngắn hơn (sau duyệt/loại) gây panic — nay chụp con trỏ theo ID trước khi thay dữ liệu và các hàm
`Cand/CursorCase/CursorBatch` kiểm biên; `form.fields()` bỏ qua trường nhúng (xem trên).

**Chạy tay**

```sh
go test ./internal/tui/app ./internal/tui/gallery -update    # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
go test -race ./internal/tui/app -run 'Milestone|Eval'        # việc nền, câu hỏi, huỷ, một việc nặng mỗi lúc
vhs internal/tui/testdata/vhs/eval-wide.tape                  # ảnh màn Đánh giá (cần vhs, ttyd, ffmpeg; xem README cùng thư mục)
bint tui                                                      # 5 (Đánh giá) → m (khai thác + kiểm chứng) → a (duyệt) → E (ước tính) → r (chạy) → xem thẻ Đợt
```

### 12.7 Giai đoạn 5a (đã xong, 2026-11-12)

Giai đoạn 5 được tách làm hai phiếu. **5a** (phiếu này) chỉ làm tab Hệ thống (mục 5.7); **5b** giữ trình hướng dẫn `init` / Hệ thống › Cài đặt repo cùng 11 cờ `init`,
nâng cấu hình (`init --update`) qua hộp thư và Cấu hình › Nâng mẫu, `update` + khởi động lại — các mục đó **vẫn nằm trong danh sách chưa làm gắn giai đoạn 5**
(`Pending["update"]` và 11 mục `PendingFlags["init …"]`; đã làm ở mục 12.8). Mốc "xong" của 5a: dùng được mọi thẻ của tab Hệ thống mà không gõ lệnh; kiểm bằng `TestSystem*` (`internal/tui/app`,
dịch vụ thật cho phần sửa cấu hình/ngôn ngữ và trình soạn thảo giả) và các bảng ca của dịch vụ.

**Tầng dịch vụ** (`internal/app`, `internal/app/evolvesvc`): 18 lệnh lá có kiểu `Options` (thẻ `flag`/`arg`/`form`), `Validate()` (trả `UsageError` → mã thoát 2) và `Defaults(cfg)`;
CLI chỉ còn phân tích cờ, gọi dịch vụ và in như cũ (đầu ra, mã thoát và `--help` không đổi; mọi e2e CLI cũ chạy nguyên, không sửa khẳng định nào).
- `harness_svc.go`: `ListHarnesses`/`ShowHarness`/`HarnessVersion`, và `HarnessRuntime` (trạng thái hook theo phạm vi đã cài + MCP đã đăng ký + Codex chưa tin cậy của từng harness
  **đang bật**) tách từ `app.Doctor`: doctor dựng lại các dòng `hook <tên>`, `MCP <tên>`, `MCP codex citation`, `codex trust` từ chính `runtimeOf`, nên màn Harness và doctor không lệch
  (`Check.Detail` giữ nguyên từng chữ; test so khớp `Doctor` trên repo tạm có/không hook, có/không MCP).
- `harness_components.go`: `ComponentListOptions/ShowOptions/ReviewOptions`, thẻ `flag` cho `AddComponentOptions` và `RenderOptions`; `Validate()` nhận ràng buộc từ `RunE`;
  `ListComponents`/`ShowComponent` trả dòng + nội dung (`ComponentRow` chuyển từ CLI); `ReviewComponent(ctx, w, o, to)` vẫn `RequireHuman`.
- `skills_svc.go`: `ListSkills`, `ShowSkill`, `CreateOverlay` (không bao giờ ghi đè), `DiffSkill`, `SkillVersions`, `Sync`, `PlanSync`; `evolvesvc/skills.go`: `SkillCheck`, `SkillRegress`
  (CLI giữ mã thoát 1 khi `skill check` không đạt). `index_svc.go`: `RebuildIndex`; `mcp_svc.go`: `MCPTools`.
- `config_svc.go` + `internal/config/edit.go` (không thêm khoá cấu hình nên `Default()`/`Render` không đổi): `config.ValidateWith(root, layer, data)` chạy đúng `load` của `Load` (toàn cục →
  dự án → strategy → cục bộ, `KnownFields(true)`, `Validate()`) với nội dung thay cho một lớp (`mergeFile` nhận bảng ghi đè); `config.SetUILanguage` sửa **đúng dòng** `ui.language`
  (thay token giá trị theo vị trí `yaml.Node`; chèn dòng khi thiếu khoá; chèn khối `ui:` có chú thích khi thiếu khối; tạo mới khi chưa có tệp; giữ CRLF, chú thích, dạng flow `ui: {}`).
  Dịch vụ: `ConfigView` (cấu hình hiệu lực tách theo khoá cấp một bằng `yaml.Marshal` từng trường + bốn lớp nguồn + cờ worktree phụ), `PrepareConfigEdit` → `CheckConfigEdit` → `SaveConfigEdit`
  (kiểm lại bằng `ValidateWith`, so băm tệp gốc → `ErrConfigChanged`, sao lưu tệp cũ vào thư mục trạng thái bằng `fsafe.Backup`, ghi nguyên tử), `SetUILanguage`
  (`UILanguageOptions{Language, Layer}`, mặc định `local`). Ở worktree phụ cả hai đều trả `LinkedWorktreeError{Main}` (mục 7.5).

**Sổ hành động và phủ** (`actions/system.go`, `coverage.go`): 33 hành động `system.*` (18 lệnh + hành động giao diện/phụ: `config_edit/check/save/view`, `language_save`, `harness_runtime/version`, `open_file`,
`sync_plan`, `doctor`...). `Writes`: `config_edit`, `config_save`, `language_save`, `component_add/approve/deprecate`, `skill_overlay`, `open_file`, `sync`, `index_rebuild`; `HumanOnly`: `component_approve/deprecate`.
`coverage.go` gỡ 18 lệnh khỏi `Pending` (`PendingCeiling` 30 → **12**, `CurrentPhase` giữ **4**), sửa lý do `Excluded` của `hook *`/`mcp serve`; `serviceBindings` thêm 18 lệnh; các ca ngược của
`coverage_test.go` đổi mẫu từ `sync` sang `update` và vẫn đỏ đúng chỗ, thêm ca "bỏ thẻ `flag` của `AddComponentOptions`" và "cờ mới của `skill diff` mà quên trường Options".

**Màn Hệ thống** (`internal/tui/screens/system`, `ui.Screen`; thay `screens/placeholder`, đã xoá cùng khoá `placeholder.*`): thẻ con đổi bằng `,`/`.`; ≥ 100 cột (vùng nội dung) hai khung, hẹp một
khung (`Tab`/`Enter` đổi); nạp lười theo thẻ, dữ liệu cũ vẫn hiện trong lúc nạp lại; Doctor, bảng harness và trạng thái hook/MCP nạp trong `tea.Cmd`; phím riêng của thẻ nằm ở viền dưới khung.
- **Cấu hình**: hàng đầu là các lớp nguồn (toàn cục/dự án/strategy/cục bộ, đường dẫn, "sửa được"), các hàng sau là 13 nhóm khoá cấp một kèm YAML của nhóm. `e` mở form chọn lớp (`project` = `config.yml`,
  `local` = `local.yml`) → bản sao tạm → `$EDITOR` (`tea.ExecProcess`) → `CheckConfigEdit`: không đổi → không ghi; không hợp lệ → hộp đỏ nêu lỗi của `Validate()`/giải mã, tệp gốc giữ nguyên, chọn "Sửa lại"
  (mở lại cùng bản tạm) hoặc "Bỏ" (xoá bản tạm); hợp lệ → hộp diff → Enter ghi (tệp gốc đã đổi trong lúc sửa → từ chối, không ghi đè).
- **Harness**: bảng (bật, trên máy, phiên bản, đối chiếu, cấp), bảng khả năng ✓ ~ ✗ (tiêu đề cột là 4 chữ đầu tên harness), chi tiết như `harness show` kèm hook (phạm vi đã cài) và MCP (đã đăng ký hay chưa) từ
  `HarnessRuntime`; `V` lấy phiên bản bằng cách gọi chương trình harness (nền, riêng từng harness).
- **Thành phần**: danh sách (`f` form lọc loại/trạng thái, `/` lọc chữ, vấn đề tải), chi tiết + nội dung (+ `.sh` kèm cảnh báo "Bint không bao giờ tự chạy"), `n` thêm (mẫu hoặc `--kind` + `--file`; `Validate()` chặn ngay trong
  form), `a` duyệt / `d` loại bỏ (hộp xác nhận; **duyệt script là hộp đỏ**; `HumanOnly`), `R` render theo harness/chế độ/đường dẫn.
- **Skill**: danh sách gốc/tiến hoá (loại, phiên bản, trạng thái xuất bản theo thư mục, `core.lock`, lớp phủ, lớp phủ mồ côi, skill tiến hoá hỏng/lỗi thời), `Enter` nội dung đã ghép / `C` bản gốc (`--core`), `o` tạo lớp phủ rồi mở
  `$EDITOR`, `d` diff, `v` lịch sử phiên bản, `c` check, `g` regress (form, kết quả ở khung chi tiết; `Esc` đóng), `s` **sync**: `PlanSync` → hộp xem trước (trạng thái + diff từng tệp, xung đột) → Enter ghi bằng `Sync`;
  `F` = `--force` → hộp đỏ nêu kiểm tra bị bỏ qua.
- **Chỉ mục** (`b` → xác nhận → `RebuildIndex`, báo số mục và tệp hỏng), **MCP** (danh sách công cụ + mô tả), **Ngôn ngữ** (`auto|vi|en` + nơi ghi `local` mặc định hoặc `project`; giao diện đổi ngay sau khi ghi bằng
  `actions.LanguageMsg`; `auto` chọn theo `BINT_LANG`, `LC_ALL`/`LANG`; phím `L` vẫn chỉ đổi trong phiên), **Doctor** (mọi kiểm tra với mức và chi tiết, `w` chỉ cảnh báo/lỗi, `Ctrl+R` chạy lại).
- **Hộp thư** (Tổng quan): mỗi thành phần candidate một dòng (`overview.inbox.component`, tối đa 5 rồi "và N thành phần…"), Enter mở Hệ thống › Thành phần tại đúng thành phần (`NavMsg.ComponentID/ComponentKind` → `Focus`);
  dòng doctor mở thẻ Doctor (`NavMsg.SystemTab`).

**Phủ và kiểm thử**: test dịch vụ (`system_svc_test.go`: `Validate`/`Defaults`, `HarnessRuntime` khớp `Doctor`, `ReviewComponent` với `CLAUDECODE=1` → `ErrAgentForbidden` và `asHuman` → đạt, `PlanSync` không đổi tệp nào và khớp kết quả
`Sync` (kể cả xung đột và `--force`), `CreateOverlay` không ghi đè, `RebuildIndex` đếm đúng/báo tệp hỏng, `MCPTools`, `ConfigView`, luồng sửa cấu hình: nội dung sai → tệp gốc không đổi, tệp gốc đổi giữa chừng → từ chối, `SetUILanguage` trên tệp có chú thích
và tệp thiếu khối `ui`, từ chối ở worktree phụ không đổi tệp nào), test `internal/config` (bảng ca `SetUILanguage` gồm chú thích, thiếu khối, flow, CRLF, tệp chưa có; `ValidateWith`: giá trị sai ở lớp dự án, lớp cục bộ đè giá trị sai, khoá lạ),
test hành động với dịch vụ thật (`actions/system_test.go`: cờ `Writes`/`HumanOnly`, đọc/ghi trên repo tạm, sửa cấu hình với trình soạn thảo giả), e2e CLI mới (`system_e2e_test.go`: `mcp tools` giống từng byte, mã thoát 2 của `harness component add/list/show`,
`harness render`, `harness show`; `skill check` không đạt → mã 1), test màn hình (`system_screen_test.go`, `system_flows_test.go`: thẻ, lọc, form gửi đúng `Options` + dòng CLI, hộp duyệt thường/đỏ, `HumanOnly` khoá khi phiên agent,
sync xác nhận "không" thì không ghi, `--force` hộp đỏ, ngôn ngữ đổi ngay, `--read-only` từ chối mọi `Writes` qua phím/bảng lệnh/yêu cầu trực tiếp, worktree phụ, Enter ở hộp thư), golden 80×24 và 140×40 (vi/en/xx, đơn sắc, 21 trạng thái mỗi cỡ;
thêm mã màu ở bốn chế độ cho Thành phần và Harness) kèm khẳng định không dòng nào vượt chiều rộng và không còn khoá chưa dịch, `i18n_scan_test` (xem dưới), `system-wide.tape`.

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**

- **Xem trước sync chạy chính `skills.Install` trên bản sao tạm** (`.bint/skills`, các thư mục skill của harness, manifest) rồi so từng tệp với bản thật, thay vì tự dựng từ `skills.Composed` + `skills.Status` như kế hoạch: kết quả (kể cả xung đột
  do sửa tay hay lớp phủ) đúng như `Sync` sẽ cho mà không chép lại logic; đổi lại tốn một lần sao chép thư mục nhỏ. `Sync` vẫn tự kiểm lại lúc ghi nên lệch giữa lúc xem và lúc ghi chỉ cho kết quả khác. Ở `--read-only` phím `s`/`F` không mở bản xem trước
  (sync ghi tệp nên không có gì để xác nhận).
- **Đổi nhỏ về hành vi CLI**: `harness component add --template … --kind …/--file …` nay là lỗi dùng sai (mã 2); trước đó `--template` lặng lẽ thắng. `--kind`/`--status`/`--mode`/`--harness` sai vẫn mã 2 (nay qua `Validate()`).
- **Hộp thư đổi hình dạng**: dòng "N thành phần harness chờ duyệt" (`overview.inbox.components`) thay bằng một dòng mỗi thành phần; chỉ số dòng trong ba test hộp thư (`TestInboxLinesAndTargets` và hai test Enter) dịch theo. Golden Tổng quan không đổi (mẫu không có thành phần).
- **Thẻ Ngôn ngữ và Cấu hình dùng chung một lần nạp** (`ConfigView`); lưu ngôn ngữ từ chối nếu nội dung hiện có của tệp đích đã không hợp lệ ở chỗ khác (`ValidateWith` chạy trên toàn bộ cấu hình ghép lớp).
- **`HarnessRuntime` chỉ gồm harness đang bật** (như doctor); harness chưa bật hiện "chưa bật trong cấu hình" thay vì trạng thái hook/MCP. Nhãn khả năng và cấp (`sy.cap.*`, `sy.level.*`) dịch qua i18n thay vì dùng chuỗi tiếng Việt của `internal/harness`.
- **Thanh thẻ con rút gọn ở 80 cột** (bỏ đệm và dấu `·` của thẻ không chọn) để không cắt tên thẻ (từ 5b có mười thẻ nên thêm dạng cửa sổ, xem 12.8); bảng khả năng dùng cột 4 ký tự cho mỗi harness.
- **Mở `$EDITOR`**: `config_edit` và `open_file` khai `Writes = true` (người sửa được tệp); việc ghi cấu hình thật là `config_save`. Sửa cấu hình dùng bản sao tạm nên thoát editor giữa chừng không để lại cấu hình hỏng; không dùng khoá liên tiến trình nên
  còn một cửa sổ rất nhỏ giữa lúc so băm tệp gốc và lúc ghi nguyên tử (không chép mẫu `evolve.AcquireLock`, xem tri thức k:01M746PTBYPSXDT0WJZS38KPFD).
- **Ô gợi ý tệp (`suggest=files`) của form thêm thành phần chưa nối** (cần nhập đường dẫn tay); form có gợi ý mờ cho tên mẫu, harness, thành phần và skill. `ErrConfigChanged` hiện với nhãn "chi tiết kỹ thuật" (chưa có khoá dịch riêng).
- **Doctor chạy hai lần** khi mở thẻ Doctor sau Tổng quan (mỗi nơi gọi `app.Doctor` riêng).
- **Test khoá i18n**: `keyCallRE` nay bắt cả `m.t("…")`/`m.n("…")` viết thường (k:01M75NXM4Z6S1CS3X7B2M63EBC) và bỏ qua tiền tố ghép động kết thúc bằng `_`; thêm tiền tố động `action.system.`, `sy.tab.`, `sy.cap.`, `sy.level.`, `sy.har.f.`,
  `sy.comp.f.`, `sy.skill.f.|kind.|pub.|tgt.`, `sy.config.layer.`, `sy.lang.f.|opt.`, `sy.doc.level.`.
- **Chưa làm** (đúng phạm vi): trình hướng dẫn `init`, Hệ thống › Cài đặt repo và 11 cờ `init`, nâng cấu hình `init --update` qua hộp thư/Cấu hình › Nâng mẫu, `update` + khởi động lại, cài/gỡ hook và MCP từ TUI (5b); sửa cấu hình toàn cục
  (chỉ là một lớp nguồn); soạn thành phần từ khung trống bằng `$EDITOR`, trình duyệt cây tệp, chuột và chế độ hẹp đầy đủ (giai đoạn 6); ảnh VHS thật và bước CI xuất ảnh (`.github/workflows/**` được bảo vệ).

**Sửa sau review** (phiếu 01M75YH2JGR9AD685VH886WE33; mỗi lỗi có test tái hiện chạy đỏ trước khi sửa)

- **01M75Q4YV52TJXPX4M6DQ8MRA0.1** (ghi cấu hình bị từ chối làm mất bản sửa): `system.config_save` chỉ xoá bản tạm khi ghi thành công; bị từ chối (`ErrConfigChanged`, `Validate` lỗi lúc ghi) thì màn mở hộp đỏ "Sửa lại / Bỏ" kèm đường dẫn bản tạm. Sửa lại sau `ErrConfigChanged` đặt cờ `Rebase` cho lần kiểm kế tiếp: `app.RebaseConfigEdit` đặt lại mốc (`Original`, `BaseHash`) theo tệp gốc hiện tại, nên diff tính theo nội dung mới và lần ghi lại thành công (đổi tiếp sau mốc mới vẫn bị từ chối). Test: `TestSystemConfigEditRefusedWhenOriginalChangedMeanwhile` (mở rộng), `TestSystemConfigEditWriteRejectedKeepsTempUntilDiscard`, `TestRebaseConfigEdit`.
- **.2** (`SetUILanguage` sinh `language:vi`): `replaceScalar` chèn `" "+giá trị` ngay sau dấu `:` khi nút là null rỗng (giữ chú thích sau đó). Test: hai ca mới trong `TestSetUILanguageTable`, `TestSetUILanguageEmptyValue` (qua `ValidateWith`).
- **.3** (Enter hộp thư dùng danh sách thành phần cũ): `Focus` luôn nạp lại thẻ (cả khi đã nạp hoặc đang nạp), giữ yêu cầu focus tới khi dữ liệu mới về, `CursorComponent` trả nil trong lúc chờ (duyệt/loại bỏ không nhắm thành phần cũ); không thấy thì toast `sy.comp.notfound`; còn một lần nạp xếp hàng thì chưa kết luận "không thấy". Test: `TestInboxEnterOnComponentReloadsWhenLoaded`.
- **.4** (cùng gốc với 5b.3: `PlanSync` bỏ sót `core.lock`): `SyncPlan` thêm `LockChanged`/`LockDiff`; `PlanSync` so `core.lock` sau `skills.Install` trên bản tạm với bản thật (khác phiên bản hoặc thiếu tệp) và tính vào `Changes`, nên `onSyncPlan` mở hộp xác nhận thay vì "đã cập nhật"; hộp liệt kê dòng `core.lock` kèm diff. Test: `TestPlanSyncCountsCoreLock`, `TestInboxVersionSkewSyncClearsItem` (lock phiên bản cũ, skill khớp → Enter hộp thư → `s` → xác nhận → lock mang phiên bản binary, mục hộp thư biến mất).
- **.5** (đổi ngôn ngữ trong phiên bỏ qua lớp cấu hình/`BINT_LANG`): `UILanguageResult` thêm `Effective` (ui.language sau ghép lớp) và `ShadowedBy`; `LanguageMsg` mang `Lang` (hiệu lực), `Chosen`, `Layer`; `tuiapp.Options.LangFlag` nhận `--lang`; `setLanguage` chọn bằng `i18n.ResolveFrom` (thứ tự `--lang` > `BINT_LANG` > hiệu lực > `LC_ALL`/`LANG`) và toast `sy.lang.shadowed` nêu nguồn che (`--lang`, `BINT_LANG`, `.bint/local.yml`). Test: `TestSystemLanguageShadowed` (ghi project khi local đặt khác; `BINT_LANG`; `--lang`; không bị che), `TestSetUILanguageReportsEffective`, `TestResolveFromReportsSource`.

**Chạy tay**

```sh
go test ./internal/tui/app -run 'TestGoldenSystem' -update    # ghi lại golden của màn Hệ thống sau thay đổi giao diện chủ ý (đọc diff trước)
go test ./internal/tui/... ./internal/app/... ./internal/config/... ./internal/cli/...
GOOS=windows go vet ./...                                      # cùng go vet ./...; staticcheck cho linux và windows
vhs internal/tui/testdata/vhs/system-wide.tape                # ảnh màn Hệ thống (cần vhs, ttyd, ffmpeg; xem README cùng thư mục)
bint tui                                                       # 7 (Hệ thống) → e (sửa cấu hình, $EDITOR thật) → . (thẻ kế) … s (xem trước sync) → ., ., ., Enter (chọn ngôn ngữ)
```

**Chưa kiểm được tự động**: `$EDITOR` thật (test dùng hàm giả và gửi `ResultMsg` thay cho `tea.ExecProcess`), binary harness thật (phiên bản, trust của Codex), ảnh VHS, cảm nhận thẩm mỹ (thanh thẻ rút gọn, bảng khả năng cột hẹp),
thời gian của `PlanSync` trên thư mục skill lớn.

### 12.8 Giai đoạn 5b (đã xong, 2026-11-12)

Phiếu thứ hai của giai đoạn 5: phần **vòng đời** (mục 7.6, 5.7 Cài đặt repo, 7.5 worktree). Mốc "xong": repo mới `bint` → trình hướng dẫn → khởi tạo xong không cần đọc tài liệu; repo cấu hình cũ được nâng qua hộp thư;
`update` rồi khởi động lại trong TUI; danh sách chưa làm rỗng. Kiểm bằng `TestInitWizard*`, `TestSetupTab*`, `TestConfigUpgrade*`, `TestUpdate*`, `TestRestart*` (`internal/tui/app`), test dịch vụ (`internal/app/init_svc_test.go`), e2e CLI mới và test phủ.

**Tầng dịch vụ** (`internal/app`)
- `init_svc.go`: `InitFlagOptions` — một cờ một trường (thẻ `flag`) cho **đủ 11 cờ** `init` (`harness`, `hooks`, `no-hooks`, `hooks-scope`, `agents-md`, `mcp`, `no-mcp`, `force`, `no-skills`, `git`, `update`); `Validate()` trả `UsageError`
  cho `--hooks`+`--no-hooks`, `--mcp`+`--no-mcp`, `--hooks-scope` sai, `--git` sai, harness lạ; `ToInit` (gọi `Validate`) đổi cặp bool thành `*bool` của `InitOptions`; `InitDefaults`. `bint init` dựng struct này từ cờ rồi gọi `Validate()`/`ToInit`
  (`Confirm` giữ ở CLI); `app.Init` vẫn tự kiểm `GitMode`/harness cho người gọi khác. Mọi e2e init cũ chạy nguyên, không sửa khẳng định.
- `ReadInitState` (chỉ đọc): chưa init, harness bật + `detect`, hook theo phạm vi đã cài, MCP, chế độ Git đã lưu, khối `AGENTS.md`/nhập `CLAUDE.md`, cấu hình theo mẫu cũ (`config.Upgrade`), worktree phụ. `InitState.FormDefaults()` điền sẵn form.
  `PlanInit` (chỉ đọc): danh sách (đường dẫn, tạo/sửa/gỡ/giữ, chi tiết) dựng từ Options + thăm dò rẻ, kèm diff cấu hình khi `--update`. Test chụp băm cả cây repo và thư mục trạng thái trước/sau, bằng nhau (không `store.Open`, không `Ensure`).
- `PlanConfigUpgrade` (diff khớp `config.Upgrade`, `LinkedWorktreeError` ở worktree phụ), `UpgradeConfig` (`app.Init` với `Update`, nên giống `bint init --update`), `ConfigBackupPath`.
- `doctor.go`: kiểm tra mức **warn** `CheckConfigTemplate` ("mẫu cấu hình") khi `config.Upgrade(...).Changed()`; hộp thư lấy sẵn nên tự có mục. `version_svc.go`: `VersionInfo` (binary, revision, `bint_version` của `core.lock`, mẫu cũ, `LockMismatch`).
- `update.go`: `UpdateOptions` có thẻ `source` và `pull` (`Out` không thẻ), `Validate()` (nguồn chỉ định phải là mã nguồn Bint → `UsageError`), `UpdateDefaults()`; `SelfRevision` xuất ra.

**Sổ hành động và phủ**: `init.run`/`init.plan` thay `init.defaults`; `system.repo_state/repo_plan/repo_apply`, `system.config_upgrade_plan/config_upgrade`, `system.update` (việc nền, `Job` + `JobInfo`, log go install tách dòng về khung log, kết quả `UpdateOutcome` kèm cả log), `system.restart`
(giao diện thuần). `Env.Update` thay hàm cập nhật (kiểm thử không chạy `go install`). `coverage.go`: **`Pending` và `PendingFlags` rỗng, `PendingCeiling = 0`, `CurrentPhase = 5`**; `coverage_test.go` thêm `serviceBindings` cho `init`/`update` và viết lại các ca ngược
bằng mục giả (lệnh `zebra` trong `Pending`, `tui --lang` cho `PendingFlags` sai, bỏ `system.update` khỏi sổ + binding, bỏ thẻ `flag` của `InitFlagOptions`) — vẫn đỏ đúng chỗ.

**Trình hướng dẫn** (`screens/initwizard`, `ui.Screen`; thay `screens/initplaceholder`): harness (chọn nhiều, đánh dấu có trên máy) → hook + phạm vi → MCP → AGENTS.md → skill → Git `track|local` kèm đoạn giải thích hệ quả → **xem trước** (`init.plan`, dòng CLI tương đương)
→ chạy (`init.run`) → báo cáo; "bước tiếp theo" chọn được (`j`/`k`), Enter dẫn tới tab tương ứng khi có ánh xạ (`bint doctor` → Hệ thống › Doctor, `bint runs list` → Lượt chạy, `bint knowledge review` → Tri thức), còn lại chỉ rời trình hướng dẫn.
Phím: Enter tiếp, Esc/`b` lùi, `j`/`k` di chuyển, **Space chọn** (di chuyển không đổi lựa chọn), `d` dùng mặc định và nhảy tới xem trước, `r` nạp lại trạng thái. Luôn đặt `Hooks` và `GitMode` tường minh nên `Confirm` của `app.Init` không được gọi.
`--read-only`: không mở trình hướng dẫn (hiện lý do và lệnh CLI); `init.run` bị ẩn theo sổ.

**Hệ thống** (từ 8 thành 10 thẻ: thêm **Cài đặt** và **Cập nhật**; tên đầy đủ "Cài đặt repo"/"Cập nhật Bint" là tiêu đề khung)
- **Cài đặt repo**: trạng thái `InitState` (từng harness: bật, hook + phạm vi, MCP; Git; AGENTS.md; mẫu cấu hình). `e` mở form `InitFlagOptions` điền sẵn; gửi form so với giá trị ban đầu — **không đổi gì thì không chạy init** ("Không có gì thay đổi");
  có đổi thì `PlanInit` → hộp xem trước (tệp tạo/sửa/gỡ, dòng CLI chỉ chứa cờ đã đổi, ví dụ `bint init --mcp`) → Enter → `app.Init` chỉ nhận phần đã đổi → báo cáo các bước. `--force` có cảnh báo và hộp đỏ; ô `--harness` chỉ để xem (đổi ở thẻ Cấu hình).
- **Nâng mẫu cấu hình**: phím `U` ở thẻ Cấu hình (và Cài đặt repo) hoặc Enter trên dòng doctor `CheckConfigTemplate` ở hộp thư (`NavMsg.ConfigUpgrade`) → hộp diff khớp `config.Upgrade` (+ ghi chú, nhắc bản sao lưu) → xác nhận ghi, hộp kết quả nêu **đường dẫn bản sao lưu**;
  từ chối thì tệp giữ nguyên; ở worktree phụ: từ chối kèm `cd <worktree chính> && bint`, không đổi tệp nào.
- **Cập nhật Bint**: bản đang chạy (version + revision, đường dẫn tệp) và mã nguồn tìm được; `u` mở form `source`/`pull` → việc nền (`Esc` huỷ) → khung log go install theo từng dòng → kết quả `cũ → mới`, tệp vừa cài, `OldBackup`, cảnh báo `Mismatch`.
  Thành công: Unix hỏi **"Khởi động lại ngay?"** (Enter → thoát Bubble Tea, rồi `tui.Run` exec tệp vừa cài với cùng đối số và môi trường; "Để sau" thì nhắc vẫn ở thẻ, `R` mở lại); Windows chỉ hiện hộp "đóng và mở lại bint" kèm `OldBackup`. Còn việc nền khi khởi động lại → hộp "Huỷ việc rồi khởi động lại / Ở lại".
  `restart_unix.go` (`//go:build !windows`: `execFn = syscall.Exec` thay được, `RestartSupported = true`) và `restart_windows.go` (`//go:build windows`: `RestartSupported = false`) cùng chữ ký `restartSelf`; `GOOS=windows go vet ./...` và staticcheck cho cả hai nền tảng sạch.
- **Tổng quan**: phiên bản binary (+ revision), trạng thái mẫu cấu hình và `bint_version` của `core.lock` nằm trên viền dưới hộp thông tin (không thêm dòng); `core.lock` khác phiên bản binary → mục hộp thư `overview.inbox.version_skew`, Enter mở Hệ thống › Skill (`s` sync).

**Chỗ lệch so với kế hoạch (người duyệt có thể đổi)**
- **Mã thoát 1 → 2** cho lỗi dùng sai của `init` (`--git` sai, `--harness` lạ) và `update --source` không phải mã nguồn Bint: chuyển xuống `Validate()` nên là `UsageError`. Không e2e cũ khẳng định mã 1; e2e mới (`init_update_usage_e2e_test.go`) khẳng định mã 2. Thông báo `--harness` có thêm tiền tố `--harness:`.
- **Doctor thêm một dòng cảnh báo** cho repo theo mẫu cũ (trước đây chỉ `init` báo); repo do `app.Init` tạo theo mẫu hiện tại không có dòng đó. Repo của chính dự án (chưa có khối `ui`) sẽ thấy dòng này tới khi chạy `bint init --update`.
- **Xem trước init là kế hoạch tĩnh** từ Options + thăm dò chỉ đọc (không chạy trên bản sao tạm như `PlanSync`): không mô phỏng được mọi hệ quả (skip-worktree, hook phạm vi user, bản sao lưu); hộp ghi rõ "dự kiến", báo cáo sau khi chạy mới chính xác.
- **Hooks là một cờ chung cho mọi harness** (giới hạn của `InitOptions`): form một ô; điền sẵn "bật" khi mọi harness có hook đều đã cài. Codex dùng chung `.codex/hooks.json` cho phạm vi `local` và `project` nên `InitState` tính hook ở tệp đó cho cả hai phạm vi.
  Đổi phạm vi mà hook đang bật thì phần gửi đi gồm cả `--hooks` (nếu không, init chỉ báo "chưa có sự đồng ý").
- **Bước tiếp theo → màn hình** khớp theo chuỗi ("bint doctor", "bint runs list", "bint knowledge review") trong văn bản `NextSteps` của `app.Init` (không có mã/ánh xạ trong dịch vụ).
- **Phím của trình hướng dẫn**: kế hoạch ghi Enter/→ tiếp và Esc/← lùi; mũi tên trái/phải là phím toàn cục (đổi tab) nên chỉ dùng Enter/Esc/`b`. Di chuyển không tự chọn (Space mới chọn) vì hàng phạm vi hook nằm sau hàng Có/Không.
- **Thanh thẻ Hệ thống**: mười thẻ không vừa 80 cột nên thêm dạng cửa sổ quanh thẻ đang chọn (`‹ … ›`); khi có thông báo thì dành chỗ cho cả thông báo (tối đa nửa chiều rộng) trước khi rút gọn. Tên thẻ ngắn ("Cài đặt", "Cập nhật") để vừa 100–140 cột.
- **Exec tệp `Target`** (tệp vừa cài) chứ không phải `os.Executable`: khi `Mismatch` tệp đang chạy không phải bản mới; hộp vẫn cảnh báo PATH.
- **Phiên bản binary gần như hằng** (`cli.Version = "0.1.0-dev"`): `LockMismatch` chỉ báo khi `bint_version` khác `Version`; lệch nội dung skill sau `update` vẫn do doctor báo theo băm. Revision chỉ để hiện, không ghi vào `core.lock` (`internal/skills/**` ngoài phạm vi).
- **`q`/thoát khi đang khởi tạo** dùng chung hộp thoát việc nền (nhãn `bint init`); khi hết việc lúc đang thoát, mọi `ResultMsg` (không chỉ khởi tạo) đều kiểm "đã hết việc thì thoát".
- **`bint task show` báo `docs/tui-plan.md` ngoài phạm vi** dù có trong `scope.allow`: coi là cảnh báo sai (bộ so khớp không nhận đường dẫn literal).

**Chạy tay**

```sh
go test ./internal/tui/app -run 'Golden|Wizard' -update      # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
go test ./...
GOOS=windows go vet ./...                                      # cùng go vet ./...; staticcheck cho linux và windows (cài binary rồi đặt GOOS)
bint tui                                                       # repo chưa init: trình hướng dẫn; 7 (Hệ thống) → . ×8 (Cài đặt) → e; . (Cập nhật) → u → khởi động lại
```

**Chưa kiểm được tự động**: `go install` thật (test dùng hàm cập nhật giả), `syscall.Exec` thật (test thay `execFn`; chạy tay: `bint` → 7 → Cập nhật → khởi động lại), hành vi Windows thật (chỉ `go vet`/staticcheck với `GOOS=windows`), ảnh VHS, chạy `init` của Codex với `codex` thật (kiểm trust hook).

**Sửa sau review** (phiếu 01M75YH2JGR9AD685VH886WE33; mỗi lỗi có test tái hiện chạy đỏ trước khi sửa)

- **01M75TK7KZVPDQP41N0F01PGNM.1** (tắt/đổi phạm vi hook không tác động phạm vi khác local): `app.SetupSteps` tách thay đổi của form thành các lần chạy `init` tuần tự — tắt hook: một bước `--no-hooks --hooks-scope <p>` cho mỗi phạm vi đang có hook; đổi phạm vi: bước gỡ ở phạm vi cũ TRƯỚC, bước `--hooks --hooks-scope <mới>` SAU (phạm vi cũ mà hook nằm đúng tệp sắp cài, như Codex dùng chung `.codex/hooks.json` cho local và project, thì không gỡ). `submitSetup` xin kế hoạch từng bước, hộp xem trước liệt kê "Bước i/n" kèm dòng CLI tương đương, xác nhận thì chạy `system.repo_apply` tuần tự và dừng ở lỗi đầu tiên. Kế hoạch của các bước sau tính trên trạng thái trước khi bước đầu chạy (chỉ là dự kiến). Phạm vi `user` chạm ngoài repo (`~/.claude`): hộp xem trước nêu tệp. Test: `TestSetupSteps`, `TestSetupStepsRunMovesHooksWithSharedCodexFile`, `TestSetupTabHookScopeOff`, `TestSetupTabHookScopeMove` (sau khi chạy `InitState.HookScopes` chỉ còn phạm vi đã chọn, form mở lại điền sẵn đúng).
- **.2** (form Cài đặt repo mở khi chưa nạp xong/nạp lỗi): `openSetup` chỉ mở khi thẻ đã nạp, không lỗi và không đang nạp; từ bảng lệnh khi chưa nạp thì nhớ (`setupPending`) và mở trong `onLoaded` với `InitState` thật; phím `e` lúc đang nạp hoặc sau nạp lỗi chỉ báo (`sy.setup.loading`, `sy.setup.loaderr`). Test: `TestSetupFormWaitsForLoad` (ba ca).
- **.3** (`PlanSync` không tính `core.lock`): cùng sửa với 5a.4 ở trên (`TestPlanSyncCountsCoreLock`, `TestInboxVersionSkewSyncClearsItem`).
- **.4** (`ConfigBackupPath` cắt chuỗi tiếng Việt): `InitReport.ConfigBackup` do `upgradeConfig` điền; `ConfigBackupPath` và hộp nâng cấu hình dùng trường đó (Detail của bước giữ nguyên chữ cho CLI). Test: `TestConfigBackupPathUsesStructuredField`, `TestDoctorAndStateReportOldConfigTemplate` (có kiểm `rep.ConfigBackup`), `TestConfigUpgradeDialog` (hộp hiện đường dẫn thật), golden `system_upgrade_done` (dựng báo cáo bằng trường mới).
- **.5** (diff có sửa `.bint/knowledge/items/*` ngoài phạm vi): không phải thay đổi của phiếu này; tệp tri thức do Bint tự xác thực lại (`knowledge verify --apply`, commit a719979). Phiếu sửa này không đụng `.bint/knowledge/**`; mục mới chưa theo dõi `01M75SJEVVEJHAYFKFJ8G1T1E1.md` để người duyệt quyết định riêng.

### 12.9 Giai đoạn 6a (đã xong, 2026-11-12)

Phiếu thứ nhất của giai đoạn 6 (6a = tương tác; 6b = thông điệp và hoàn tất): sửa các phát hiện review còn mở của 5a/5b, **chuột** (mục 4.3.7), **chế độ hẹp** đầy đủ (mục 4.1), chân phím không đẩy mất phím thường dùng và đọc đủ mọi thông báo/ô chi tiết bị cắt
(yêu cầu bổ sung ở mục 9). Kiểm bằng `TestMouse*`, `TestNotices*`, `TestNarrowScan`, `TestGoldenNarrow`, `TestDetailShowsFull*`, `TestTruncatedListColumnsHaveFullView` (`internal/tui/app`), test `footer`, `zone`, `TestRebaseConfigEdit*`/`TestMerge3` (`internal/app`).

**Sửa sau review** (phiếu 01M75YH2JGR9AD685VH886WE33; mã `.n` là phát hiện thứ n của phiếu đó)
- **.5 (sửa lại sau `ErrConfigChanged` hoàn tác thay đổi của người khác)**: `app.RebaseConfigEdit` nay gộp **ba chiều theo dòng** (`app.Merge3`: mốc = `Seed`, bản của bạn = bản tạm, bản mới = tệp gốc hiện tại) rồi ghi lại bản tạm; `Seed/Original/BaseHash` theo tệp mới nên diff xác nhận chỉ còn thay đổi của người dùng.
  Hai bên sửa đoạn không chồng nhau (kể cả liền kề: một bên sửa dòng cuối khối, bên kia thêm dòng ngay sau) thì tự gộp; chồng nhau thì bản tạm mang khối `<<<<<<< bint: bản sửa của bạn … ======= … >>>>>>> bint: tệp gốc mới`, trình soạn thảo mở lại và `CheckConfigEdit` từ chối nội dung còn dấu (`HasConflictMarkers`).
  Tệp gốc bị xoá thì giữ bản của người dùng làm tệp mới (lớp `local`; xoá `config.yml` thì repo coi như chưa init nên gộp báo lỗi và bản tạm còn nguyên). Hành động `system.config_rebase` (chỉ ghi bản tạm) chạy **trước** khi mở lại trình soạn thảo; `config_check` không còn cờ `Rebase`.
  Test: `TestRebaseConfigEditMergesThreeWay` (không xung đột, xung đột), `TestRebaseConfigEditOriginalDeleted`, `TestRebaseConfigEditErrorKeepsTemp`, `TestMerge3` (`internal/app`); `TestSystemConfigEditRebaseKeepsOthersChange` (người khác thêm dòng sau khi mở, người dùng sửa khoá khác, Sửa lại rồi xác nhận → tệp có cả hai), `TestSystemConfigEditRebaseConflictMarkers` (`internal/tui/app`).
- **.3 (đặt lại mốc lỗi vẫn xoá bản tạm qua `drop()`)**: gộp lỗi (không đọc được bản tạm/tệp gốc, `configCtx` lỗi) và kiểm lỗi mở lại hộp **Sửa lại / Bỏ** kèm lỗi (`showConfigRejected`); bản tạm chỉ bị xoá khi ghi thành công hoặc Bỏ. Sửa lại sau lỗi gộp thử gộp lần nữa. Test: `TestSystemConfigEditRebaseErrorKeepsTempUntilDiscard`.
- **.1 + .2 (Cài đặt repo nhiều bước)**: `resetSetupRun` xoá luôn `setupProblem` và được gọi ở mọi đường kết thúc (xong, lỗi, yêu cầu trực tiếp); lỗi ở bước sau thì khung kết quả (`sy.setup.failed.title`) hiện báo cáo các bước đã xong kèm dòng `✗ Bước i/n lỗi: …` và toast; bước đã chạy không bị mất. Lần chạy có cảnh báo dùng `sy.setup.applied.problem` (chữ khác hẳn lần sạch).
  Test: `TestSetupMultiStepFailureKeepsReportAndClearsProblem`.
- **.4 (`setupPending` treo)**: thẻ Cài đặt repo nạp xong khi đang có hộp/form thì bỏ yêu cầu mở form đang chờ kèm toast `sy.setup.dropped`; Ctrl+R sau đó không tự mở form. Test: `TestSetupPendingDroppedWhenFormOpen`.

**Chuột** (bật mặc định: `View.MouseMode = MouseModeCellMotion`; hành động `app.mouse` trong bảng lệnh tắt/bật trong phiên, toast báo; mọi thứ vẫn dùng được chỉ bằng bàn phím)
- **Cách làm**: không dùng `bubblezone` (thêm phụ thuộc, và dấu của nó chèn ký tự vào chuỗi) mà gói `internal/tui/components/zone`: component/màn hình bọc phần tử bấm được bằng `zone.Mark(id, s)` — chuỗi APC `ESC _ Zs;<id> ESC \ … ESC _ Ze ESC \` rộng 0 ô nên mọi phép đo/cắt/đệm/`Overlay` giữ nguyên.
  `Model.Render()` bật `zone.Begin()`, vẽ, `zone.Scan` **gỡ mọi dấu** (đầu ra không còn dấu; golden không đổi vì chuột) và giữ danh sách vùng `[]zone.Zone{ID, X0, X1, Y}`. Sự kiện chuột vẽ lại một lần để vùng khớp trạng thái rồi tra vùng hẹp nhất dưới con trỏ. `zone.Mark` chỉ có tác dụng khi mô hình gốc đang vẽ nên gallery và test component không thấy dấu.
- **Vùng**: `tab:<i>` (thanh tab; cả tab rút gọn), `key:<phím>` (mọi phím ở chân phím khung ngoài và khung trong, nút `[Enter]`/`[Esc]` của hộp thoại, dấu `[!]`: như nhấn phím đó, qua `footer.KeyMsg`), `r:<i>` (hàng của mọi `list.Model` và hộp thư Tổng quan), `t:<i>` (thẻ phụ của Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá, Hệ thống; cả `‹ ›`),
  `p:<khung>` (khung danh sách/chi tiết/kết quả: bấm = focus, lăn bánh xe = cuộn), `pal:<i>` (dòng bảng lệnh), `dlg` (hộp thoại). Màn hình nhận chuột qua giao diện tuỳ chọn `ui.Pointer` (`Click(id)`, `Wheel(pane, dy)`; `pointer.go` của từng màn).
- **Hành vi**: bấm hàng dời con trỏ (kèm nạp chi tiết như phím `j/k`); bấm lại hàng đang chọn = Enter; bánh xe 3 dòng/nấc: trên khung danh sách dời con trỏ, trên khung chi tiết/kết quả cuộn nội dung, trên hộp thư Tổng quan dời con trỏ, trên hộp thoại/bảng lệnh cuộn nội dung của hộp.
  Khi có hộp thoại hoặc bảng phủ (vùng `dlg`) chỉ giữ vùng nằm trong khung chữ nhật của hộp (`filterModal`): bấm ngoài hộp không tác động màn bên dưới. Hộp thoại, form và ô lọc của màn hình chặn bấm hàng/thẻ; ô nhập của thẻ phụ Tri thức không chặn (bấm thẻ phụ khác là rời nó). Bấm `[!]` mở hộp Thông báo kể cả khi một ô nhập đang nhận phím.
- **Test**: `TestMouseOnByDefaultNoMarkersAndToggle` (MouseMode, Render/View không còn dấu, `app.mouse` bật/tắt, chuột tắt thì bấm không làm gì), `TestMouseClickTabs`, `TestMouseOverviewInbox`, `TestMouseKnowledge`, `TestMouseTasks`, `TestMouseSystem` (toạ độ lấy từ chính bản vẽ), `TestMouseFooterHintsEqualKeys` (bấm = nhấn: so bản vẽ hai mô hình),
  `TestMouseDialogButtonsAndOutsideClicks` (hộp thoát, hộp của màn hình, bảng lệnh, trợ giúp), `TestMouseNoticeMarkWhileFormCaptures`; `zone_test.go`, `footer_test.go`.

**Chân phím** (`components/footer`)
- `footer.Fit` rút nhãn **trước** khi bỏ phím: nhãn đầy đủ → cắt theo từ còn 10/7/5/3 ô → chỉ còn phím; chỉ khi chỉ-còn-phím vẫn tràn mới bỏ phím ưu tiên thấp (thứ tự bỏ: phím thường theo `Priority` giảm dần, rồi phím bắt buộc, ghim cuối). Phím ghim và `Priority ≤ footer.MustPriority (2)` không bị giới hạn `MaxHints`; `:` đổi từ ưu tiên 3 sang 2 nên luôn hiện cùng `?` và `q`.
- Chân phím trong khung (các màn từng tự ghép chuỗi rồi bị `frame` cắt giữa phím) nay là `frame.Box.BottomHints []footer.Hint` (kiểu Plain `phím nhãn  phím nhãn`): tự chọn/rút theo độ rộng khung và bấm được. Trình hướng dẫn init dùng cùng `footer.Inline`.
- Test: `TestFitShortensLabelsBeforeDroppingKeys`, `TestFitDropsLowestPriorityOnlyWhenKeysOnlyOverflow`, `TestFitKeepsMustHintsBeyondMax`, `TestInlineNeverOverflowsOrCutsKey`; `TestNarrowScan` kiểm chân phím ngoài của **mọi** kịch bản ảnh chụp ở 60×20 và 80×24 (vi, en, xx): có `?`, `q`, `:` và mọi phím ưu tiên ≤ 2 của ngữ cảnh, không dòng viền nào bị cắt (`…─`, `…┘`).

**Đọc đủ thông báo và nội dung bị cắt**
- Toast (khung chính và thanh thẻ phụ của mọi màn) dài quá chỗ thì rút gọn bằng `…` kèm dấu `[!]` (`ui.ToastLabel`); toast của khung chính không còn bị bỏ khỏi viền trên (trước đây `frame` bỏ cả nhãn phụ khi không vừa; nay rút gọn). Phím **`!`** (toàn cục như `?`) hoặc bấm vào `[!]` mở hộp **Thông báo** (`app.notifications`): nguyên văn thông báo hiện tại (ngắt dòng, cuộn được) và tối đa **50** thông báo gần nhất của mọi màn trong phiên (`harvestNotices` thu toast của mô hình gốc và của màn hình qua `ui.Toaster` sau mỗi `Update`).
- Chi tiết: lỗi nạp (danh sách và chi tiết, mọi màn) dùng `ui.ErrBlock` (ngắt dòng, thụt treo) thay vì `Truncate`; mọi viewer chi tiết/kết quả dùng `viewer.SetWrapped` (dòng dài ngắt theo từ, giữ mã màu) và các hàm dựng chi tiết (Tác vụ, Lượt chạy, Tri thức, Tiến hoá) bỏ `text.Truncate` ở giá trị; `system.fitLines` ngắt dòng; Tổng quan: giá trị dài của hộp thông tin ngắt dòng, hàng hộp thư đang chọn bị rút gọn thì hiện nguyên văn (tối đa 3 dòng) ngay dưới danh sách.
- **Bảng kiểm** các danh sách có cột bị cắt và nơi đọc đủ (`TestTruncatedListColumnsHaveFullView`, 80×24): Tổng quan hộp thư → dòng nguyên văn dưới danh sách; Tri thức (tiêu đề), Tác vụ (mục tiêu), Lượt chạy (nhãn/lý do), Đánh giá (tiêu đề ứng viên), Tiến hoá (tóm tắt cơ hội, lý do đề xuất), Hệ thống › Harness (mô tả) → khung chi tiết (Enter, cuộn được). Thêm danh sách mới có cột bị cắt mà không có chỗ đọc đủ thì thêm một dòng vào bảng này.
- Test: `TestNoticesFullTextOfRootAndScreenToasts` (toast 300 ký tự ở 80×24 của mô hình gốc, của màn Hệ thống, bấm `[!]`, giới hạn 50), `TestDetailShowsFullLongTitlesAndErrors` (tiêu đề và lỗi 300 ký tự, Tri thức/Tác vụ/Lượt chạy/Hệ thống, cuộn bằng `pgdown`).

**Chế độ hẹp** (60×20 và 80×24)
- `ui.SubBar` dùng chung cho thanh thẻ phụ của Lượt chạy, Đánh giá, Tiến hoá: đầy đủ → chỉ thẻ chọn có đệm → cửa sổ quanh thẻ chọn kèm `‹ ›`; **tên thẻ đang chọn luôn hiện đủ** (`ui.SubTabber` cho test). `frame` đẩy tiêu đề căn giữa sang trái khi nhãn phụ không vừa (trước đây viền trên của khung ngoài tràn 4 ô ở 60×20 khi có việc nền).
- Tổng quan: bỏ chữ `bint.` và khẩu hiệu khi thấp, phiên bản/mẫu cấu hình/`core.lock` chuyển thành dòng trong hộp khi viền dưới không đủ chỗ (không còn bị cắt).
- Golden mới `*_60x20_vi.txt` cho Tổng quan, Tri thức, Tác vụ, Lượt chạy, Đánh giá, Tiến hoá, Hệ thống (`TestGoldenNarrow`). `TestNarrowScan` quét **mọi** kịch bản golden (màn hình chính và Hệ thống) ở 60×20 và 80×24, vi/en/xx: mọi dòng rộng đúng W và đủ H dòng, tên thẻ phụ đang chọn hiện đủ, không còn khoá i18n thô (chuỗi trùng một khoá của danh mục), viền không bị cắt.
  Golden 80×24 đổi theo (chân phím rút nhãn thay vì bỏ phím; `:` luôn hiện).

**Chạy đỏ trước khi sửa** (kiểm trên cây `HEAD` bằng worktree tạm, chỉ chép tệp test): `TestSystemConfigEditRebaseKeepsOthersChange` ("bản tạm phải mang cả hai thay đổi"), `TestSystemConfigEditRebaseConflictMarkers` ("bản tạm phải mang dấu xung đột"), `TestSystemConfigEditRebaseErrorKeepsTempUntilDiscard` ("gộp lỗi thì không mở trình soạn thảo: 2"), `TestSetupMultiStepFailureKeepsReportAndClearsProblem` (thiếu "Cài đặt repo: dừng ở bước lỗi", "Bước 1/2"),
`TestSetupPendingDroppedWhenFormOpen` (hết hạn chờ: không có toast bỏ yêu cầu), `TestDetailShowsFullLongTitlesAndErrors` (lỗi nạp danh sách/chi tiết của Tri thức, Tác vụ, Lượt chạy và lỗi thẻ Hệ thống: thiếu 34–35/43 từ). Test của dịch vụ (`TestRebaseConfigEdit*`) đỏ ở bước biên dịch (hàm mới).

**Chỗ lệch so với kế hoạch / giới hạn (người duyệt có thể đổi)**
- **Không dùng `bubblezone`** (mục 3 nêu làm ứng viên): tự viết gói `zone` nhỏ (APC, không phụ thuộc mới; `go.mod` không đổi). Cờ bật `zone.Begin/End` là biến toàn cục, an toàn vì Bubble Tea vẽ trong một goroutine; đổi sang `ui.Env` nếu sau này vẽ song song.
- **Bánh xe trên danh sách dời con trỏ** (3 hàng/nấc) thay vì cuộn vùng nhìn giữ nguyên con trỏ: đơn giản và nạp chi tiết theo con trỏ như phím `j/k`; trợ giúp (`?`) vẫn không cuộn được (chỉ cắt cuối kèm "còn nữa"; thuộc "trợ giúp theo ngữ cảnh" của 6b — **đã làm ở 6b**, mục 12.10: cuộn được kèm vị trí cuộn).
- **Chân phím rất hẹp chỉ còn phím**: ở 60 cột chân phím khung ngoài thường là `[a] [r] [Enter] [:] [q] [?]` (không nhãn) vì phải giữ mọi phím ưu tiên ≤ 2; nhãn nằm ở chân phím trong khung. Locale giả `xx` cho thấy nhãn rút còn 3–5 ô ở khung trong hẹp.
- **60×20 quá thấp cho Tổng quan** khi hộp thông tin dài thêm dòng: dòng đếm Doctor có thể bị cắt ở cuối (các cảnh báo/lỗi vẫn nằm trong hộp thư).
- **Giá trị cột danh sách vẫn rút gọn bằng `…`** (kế hoạch hẹn "có cách xem bản đầy đủ", không hẹn bỏ `…`): bản đầy đủ ở khung chi tiết (Enter), bảng kiểm ở trên.
- **Chưa kiểm được tự động**: sự kiện chuột thật từ terminal (test gửi `tea.MouseClickMsg`/`MouseWheelMsg` dựng sẵn; chế độ `CellMotion` do Bubble Tea bật), hành vi Windows thật (chỉ `go vet`/staticcheck với `GOOS=windows`), ảnh VHS, thử với người dùng không quen CLI.

**Chạy tay**

```sh
go test ./internal/tui/app -run 'Golden|Narrow' -update      # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước)
go test ./...                                                 # cần ≥ ~1 GB trống ở /tmp (test eval kiểm dung lượng đĩa)
GOOS=windows go vet ./...                                     # cùng go vet ./...; staticcheck cho linux và windows
bint tui                                                      # chuột: bấm tab/hàng/phím ở chân trang, lăn bánh xe; `!` đọc đủ thông báo; bảng lệnh › Chuột bật/tắt
```

### 12.10 Giai đoạn 6b (đã xong, 2026-11-12)

Phiếu cuối của giai đoạn 6 (6a = tương tác; 6b = thông điệp và hoàn tất): sửa 4 phát hiện review của 6a, **thông điệp của tầng dịch vụ nhắc phím TUI thay vì lệnh CLI** (yêu cầu bổ sung ở mục 9),
`NO_COLOR` đầy đủ, chế độ chỉ đọc theo mục 7.4, trợ giúp theo ngữ cảnh, README, và chốt giai đoạn (`actions.CurrentPhase = 6`). Làm tiếp trên cây làm việc còn chưa commit của 6a.

**Sửa sau review của 6a** (phiếu 01M764SBMNG91M0998BX0AWKN0; mã `.n` là phát hiện thứ n)
- **.1 (medium, chuột ở Lượt chạy › Bài học đặt con trỏ lên tiêu đề nhóm)**: `runs.Click` bỏ qua hàng tiêu đề nhóm (`rows[i].row == nil`), chọn bài học khác thì đặt lại khung chi tiết như `moveLesson`; `runs.Wheel("list")` ở thẻ Bài học gọi `moveLesson` (bỏ qua tiêu đề, đặt lại chi tiết)
  thay vì `cur().Move`. Thêm `Model.LessonCursor()` cho test. Test: `TestMouseRunsLessonsSkipGroupHeaders`.
- **.2 (test nút Bỏ xoá bản tạm)**: tách `rebaseErrorSetup`; `TestSystemConfigEditRebaseErrorKeepsTempAndRetries` (bản tạm còn sau lỗi gộp, thử lại được) và `TestSystemConfigEditRebaseErrorDiscardDeletesTemp` (nhấn **Bỏ** ngay ở hộp Sửa lại/Bỏ
  sau lỗi → bản tạm bị xoá, không mở lại trình soạn thảo); bỏ import `tea` thừa và `var _ = tea.Quit`.
- **.3 (toast của mô hình gốc luôn là lỗi trong hộp Thông báo)**: `Model.toastErr` đặt cùng `m.toast` (lỗi/khoá/ngôn ngữ bị che = true; `app.mouse` bật/tắt = false), dùng ở `toastLabel`, `harvestNotices`, `currentNotice`. Test: `TestRootToastErrFlag`.
- **.4 (`windowedBar` trùng `ui.SubBar`)**: thanh thẻ phụ của Hệ thống dùng `ui.SubBar` (mười thẻ, id = chỉ số), `windowedBar` và `build` riêng bị xoá. **Golden Hệ thống đổi có giải thích**: `SubBar` đo vừa khít hơn nên ở 80×24 hiện được nhiều thẻ hơn trước khi rút
  về cửa sổ (ví dụ `… Ngôn ngữ Doctor ›` thay vì `… Ngôn ngữ ›`), và `‹` ở bản `xx` biến mất vì thẻ đầu đã nằm trong cửa sổ. Tên thẻ đang chọn vẫn luôn hiện đủ (`TestNarrowScan`).
- **.5** (tệp tri thức chưa theo dõi nằm ngoài `scope.allow`): không làm gì trong mã; các tệp `.bint/knowledge/items/*.md` vẫn là tri thức chờ người duyệt/commit.

**Thông điệp theo phím TUI** (yêu cầu bổ sung ở mục 9)
- **Cách làm (hướng 2: viết lại ở tầng hiển thị)**: một phương thức duy nhất `Rewrite(s)` của `ui.Rewriter{Cat, Reg, ReadOnly}` (`internal/tui/ui/cliref.go`; mỗi mô hình gốc giữ một bản của riêng nó và truyền qua `ui.Env.RW` cho mọi màn hình, không có trạng thái toàn cục của gói) tìm đoạn "`bint` + đường lệnh + đối số/cờ" (có hoặc không dấu huyền, nằm trong ngoặc, `reject/deprecate`, `` `bint knowledge reject` / `deprecate` ``), tra
  bảng ánh xạ `cliRefs` (`internal/tui/actions/cliref.go`: đường lệnh + cờ bắt buộc → hành động trong sổ + thẻ phụ nơi phím dùng được, vì phím trong sổ chỉ có nghĩa ở một thẻ phụ của màn) rồi thay bằng tham chiếu đã dịch theo vi/en:
  `nhấn U ở Hệ thống › Cấu hình` / `press U in System › Config` (động từ "chạy/dùng/chạy lại `bint …`" được gộp vào tham chiếu), dạng danh từ `phím a ở Tác vụ`, mục bảng lệnh `bảng lệnh (:) › …` cho hành động không có phím, `thẻ Hệ thống › Doctor` cho lệnh
  chỉ là một thẻ, và `[ô Áp dụng]` / `[ô Làm mới]` / `[ô Độ sâu b-req]` cho cờ có ô form (`verify --apply`, `init --mcp`, `run b-req --task --depth`…; nhãn lấy từ `form.<ID>.<cờ>`, cờ nhận giá trị suy từ kiểu `Options`). Cụm dùng "bint run" như danh từ
  ("`bint run` điều phối", "tiến trình bint run", "chạy `bint run` lồng", "bint run bị huỷ", "chưa `bint init` trên máy") có bảng cụm riêng (`ui/cliref_vi.go`, dữ liệu nhận dạng chữ của tầng dịch vụ nên được test quét chuỗi tiếng Việt bỏ qua).
  Đường lệnh không có hành động trong sổ (`bint hook`, `bint mcp serve`, `bint knowledge import`) và chữ "bint" không kèm lệnh được giữ nguyên. Ở chỉ đọc, tham chiếu tới hành động ghi thêm "(không dùng được ở chế độ chỉ đọc)" (`Rewriter.ReadOnly` của mô hình đó; lệnh mang cờ ghi như `knowledge verify --apply` cũng có ghi chú vì phần chạy theo cờ là hành động ghi, qua `CLITarget.WriteID`).
  **Không đổi mã dịch vụ**: CLI chắc chắn giữ chữ; hàm viết lại cũng phủ chuỗi đã lưu (lý do `blocked` trong `task.yml`).
- **Điểm hiển thị đã nối**: `ui.ErrText` (cả `err.transition` và `err.technical`), `ui.FindingText`; Tri thức (finding, kết quả xác minh, lý do bị loại khi xuất bản, bước và ghi chú của xuất bản, cảnh báo ngữ cảnh); Tác vụ (lý do hiện tại và lịch sử, điều kiện chưa thể done ở thẻ Phiếu và hộp ép trạng thái, lỗi lint, lý do của lượt chạy, dòng "Tiếp tục từ", gợi ý tiếp tục); Lượt chạy; Đánh giá (kiểm chứng, rò rỉ, đặc tả,
  chẩn đoán báo cáo, cổng 2, vấn đề khi duyệt, lệnh chạy tiếp ở thẻ Log); Tiến hoá (điều kiện tiên quyết, lợi ích, ghi chú, kết quả đối chiếu, đề xuất bị bỏ qua, dừng); Hệ thống (doctor, khả năng/hook của harness, vấn đề thành phần/skill, kiểm skill, xuất bản, bước và ghi chú của Cài đặt repo và nâng cấu hình);
  trình hướng dẫn `init` và báo cáo (bước, bước tiếp theo — `NavFor` vẫn nhận dạng theo chữ gốc để Enter mở đúng màn); hộp thư Tổng quan (doctor); hộp Thông báo, toast ở viền trên và câu hỏi giữa chừng của việc nền (`ui.Rewrite` idempotent nên viết lại lần hai vô hại); hộp thoát khi còn việc nền thì in **nguyên** lệnh CLI tiếp tục (`j.Resume`, kèm mã tác vụ hoặc mã đợt đầy đủ, không qua hàm viết lại vì người dùng đang rời TUI để dán lệnh vào terminal).
  **Không viết lại**: diff, Markdown, log của agent, nội dung tệp (test `TestCLIRefLeavesFileContentAlone`: diff xuất bản AGENTS.md vẫn còn `` `bint knowledge add …` ``).
- **Chuỗi của chính danh mục TUI** cũng nhắc lệnh (gợi ý lỗi "chạy `bint doctor`", `err.not_initialized`, `kn.boot.not_init`, `rn.lesson.empty`, `ev.sync.*`, `overview.inbox.version_skew`, `sy.update.skills_hint`, `sy.har.cli` …): sửa trực tiếp ở `vi.yaml`/`en.yaml`
  thành phím/màn TUI; chỉ giữ dòng "Lệnh CLI tương đương" của form, cột CLI của trợ giúp, `sy.index.cli`, `sy.mcp.note` (`bint mcp serve` không có hành động) và hai thông báo hướng dẫn mở terminal ngoài (`bint init` khi chỉ đọc hoặc khi trình hướng dẫn lỗi). Nhãn việc nền `bint init`/`bint update` đổi thành `init`/`update`;
  lệnh tiếp tục của việc nền (`bint run resume <id>`, `bint eval run --resume <id>`) là chuỗi CLI nguyên văn ở hộp thoát, còn ở các chỗ khác trong TUI đi qua `Rewriter.Rewrite`.
- **Con số thật**: kiểm kê bằng `go/ast` thấy **245 literal chuỗi** nhắc `bint <chữ thường>` hoặc mang token `--cờ` đứng rời (kể cả khi không có chữ "bint"; literal chỉ là một token argv của lệnh ngoài như `--name-only` hoặc `--format=%H` bị bỏ qua theo cấu trúc) trong `internal/app` (gồm `evolvesvc`), `knowledge`, `task`, `workflow`, `eval`, `evolve` (không tính chú thích và tệp `_test`); **175** bị hàm viết lại đổi và **69 mục loại trừ có lý do**: 14 mục riêng (lỗi `bint hook`, `bint mcp serve`, `knowledge import`, hướng dẫn sửa cấu hình Codex bằng tay, 2 prompt gửi agent, khối AGENTS.md, chú thích tệp sinh ra; hai nhóm sau hàm viết lại đổi được nhưng TUI không bao giờ đưa nội dung tệp/prompt qua nó) và 55 mục theo nhóm lý do trong `cliref_inventory_test.go`: 44 lỗi kiểm tra đối số (`rUsage`, xem chỗ lệch), 6 cờ của công cụ ngoài (git, claude, codex), 1 mẫu biểu thức chính quy, 1 mảnh chuỗi nối, 2 lỗi chỉ trả cho phiên agent, 1 lỗi chỉ có ở chế độ không tương tác. `go test ./internal/cli -run TestServiceMessageCLIInventory -v` in bản trước/sau của từng literal bị đổi để người duyệt đọc.
- **Test**: `TestRewriteTable` (`internal/tui/ui`: dấu huyền, ngoặc, ID, cờ đặc thù, nhiều lệnh một câu, lệnh anh em, lệnh ngoài sổ, "bint" không kèm lệnh, đường dẫn chứa `bint`, en); `TestServiceMessageCLIInventory` và `TestServiceMessageCLIInventoryCatchesGaps` (`internal/cli`: ca ngược —
  literal nhắc lệnh mà bảng không phủ thì đỏ, loại trừ thiếu lý do hoặc thừa thì đỏ); `TestApproveChangedEvidenceKeepsCLIWording` (CLI: lỗi duyệt mục có bằng chứng đã đổi vẫn in `` `bint knowledge show …` `` và `` `bint knowledge approve --refresh …` ``; toàn bộ test CLI cũ xanh không sửa khẳng định);
  đầu-cuối theo màn trong `internal/tui/app/cliref_screens_test.go`: `TestCLIRefKnowledgeScreen` (lỗi duyệt thật của `ReviewKnowledge` — từ mục 12.11 là `*app.EvidenceError`, TUI dựng phím từ cấu trúc chứ không qua `Rewrite` — và finding `relocated` thật từ `Verify` trên repo tạm), `TestCLIRefTasksScreen` (điều kiện chưa thể done thật, lỗi `SetTaskStatus` thật, lý do blocked đúng mẫu `askVerify`),
  `TestCLIRefOverviewAndSystemDoctor` (doctor thật: cấu hình mẫu cũ và skill bị sửa tay → `init --update` và `sync` ở hộp thư và Hệ thống › Doctor), `TestCLIRefRunsEvalEvolveScreens` (chuỗi lấy đúng từ nguồn dịch vụ, có kiểm còn trong nguồn), `TestCLIRefLeavesFileContentAlone`.
  Mỗi test khẳng định bản vẽ và hộp Thông báo không còn "bint `<lệnh>`" ngoài dòng "Lệnh CLI tương đương" của form và cột CLI của trợ giúp, và có phím/hành động TUI tương ứng.

**NO_COLOR đầy đủ**
- `tui.DetectMode`: `NO_COLOR` khác rỗng → Mono ở **mọi** color profile (trước đây `TrimSpace` coi khoảng trắng là chưa đặt; nay đúng no-color.org). `TestDetectModeNoColor`.
- Ở Mono không thông tin nào chỉ dựa vào màu: khung đang focus đảo màu tiêu đề (`theme.Selected` = đậm + đảo, kiểu chữ chứ không phải màu; bản vẽ thuần chữ không đổi nên golden `.txt` không đổi vì việc này); toast lỗi có dấu `✗ ` khác toast thường; hành động bị khoá có lý do bằng chữ ở bảng lệnh và trợ giúp.
- `theme.Theme.NoColor` bỏ mã màu SGR (30–38, 40–48, 90–97, 100–107, dạng `38;5;n`/`38;2;r;g;b`) nhưng giữ đậm/mờ/gạch chân/đảo màu và mã đặt lại; `Model.Render()` gọi nó ở Mono nên nội dung ngoài (log của agent, trích đoạn tệp, diff) mang màu riêng cũng bị bỏ màu.
- Test: `TestNoColorStripsOnlyColors` (theme), `TestMonoRenderHasNoColorCodes` (`Render()` của **mọi** kịch bản golden màn chính và Hệ thống ở 60×20, 80×24, 140×40, vi/en/xx không có mã màu), `TestMonoStripsExternalContentColor` (có đối chứng ở Dark), `TestMonoNothingReliesOnColorAlone` (focus, toast, khoá).
- Sửa phát hiện review vòng 1 (01M769D0Y45T2Y178ZC5GHD5QW.1): `NoColor` từng xoá cả `ESC[m` (đặt lại không tham số do lipgloss/git phát) nên đậm/đảo màu tràn sang phần sau màn hình ở Mono; nay giữ nguyên chuỗi không tham số. Test: ca `ESC[m` trong `TestNoColorStripsOnlyColors` và `TestMonoRenderDoesNotLeakAttributes` (mô phỏng trạng thái SGR, không kiểu nào còn bật ở cuối `Render()` của mọi kịch bản; đỏ trên mã cũ). Golden `*_mono.ansi.txt` ghi lại, nay có lại các `␛[m`.

**Chế độ chỉ đọc theo mục 7.4**
- `ui.Env.Allowed(id)` (mô hình gốc dựng từ sổ: `Check(mode) == Available`), `footer.Hint.ActionID` (rỗng = phím giao diện thuần) và `Env.AllowedHints` lọc chân phím trong khung; **màn hình không tự kiểm ReadOnly**. Đã gắn `ActionID` cho Tri thức (`panelFooter`), Tác vụ (`detailFooter`), Lượt chạy, Đánh giá, Tiến hoá (bộ ba
  `{phím, khoá nhãn, ID}`) và Hệ thống (`hintAction[thẻ][phím]`, `Model.hintsLine`).
- **Phát hiện khi đọc mã**: `tasks.open_file` mở trình soạn thảo nhưng `Writes = false` (khác `eval.open_file`, `knowledge.edit`, `system.open_file`) → sửa thành `true`. Chân phím trong khung của 6 màn dựng cứng phím ghi (không lọc theo chỉ đọc) → nay lọc.
- Test: `TestWritesAudit` (`internal/tui/actions`: bảng kiểm **mọi** hành động ghi tệp/CSDL/mở trình soạn thảo có `Writes = true`, mọi hành động còn lại `false`, hành động mới chưa phân loại thì đỏ); `TestReadOnlyNeverRunsWritesAnyPath` (hành động Writes khai báo bằng `Do`, `Run` hay `Job` — hàm giả đếm — không chạy được qua `RequestMsg`,
  bảng lệnh, phím tắt ở mọi (màn, thẻ phụ) và bấm chuột vào mọi phím hiện ra ở chân phím; bảng lệnh không liệt kê chúng); `TestInnerFooterHidesWritesInReadOnly` (Đánh giá, Tiến hoá, Lượt chạy, Tác vụ, Hệ thống: phím ghi có ở chế độ thường và mất ở chỉ đọc, phím đọc còn).
  Test cũ `TestReadOnlyAndAgentNeverReachDo` giữ nguyên (chỉ duyệt `Do`); việc nền (`Job`) có test chỉ đọc/`HumanOnly` riêng ở trên.

**Trợ giúp theo ngữ cảnh** (`?`)
- Hộp mở có tiêu đề **màn › thẻ phụ**, đoạn giới thiệu ngắn của thẻ (khoá `help.ctx.<màn>[.<thẻ>]`, 39 khoá ở vi và en), **nhóm phím của ngữ cảnh** (chân phím trong khung của thẻ phụ), rồi nhóm hành động của màn, phím toàn cục, các màn khác. Phím của ngữ cảnh **không khai báo lần hai**: `ui.Env.Collect` thu mọi chân phím trong khung (đã lọc theo chỉ đọc)
  mà màn hình dựng khi vẽ; màn hình chỉ thêm `ui.Helper.HelpTab()` (mã thẻ phụ). Màn không có thẻ phụ (Tổng quan, Khởi tạo) gộp ngữ cảnh và hành động thành một nhóm.
- `components/help` viết lại: `Layout`, `Window`, `Render(…, top, pos)`; cuộn bằng `j`/`k`/mũi tên, `PgUp`/`PgDn`/`Space`, `Home`/`End`, **bánh xe chuột** (qua khung `dlg`), và viền dưới hiện **vị trí cuộn** `từ–đến/tổng ↕` thay vì cắt im lặng; khoá `help.more` bỏ.
- Test: `TestHelpContextEveryScreenAndSubTab` (đi qua từng thẻ phụ của sáu màn bằng chuột ở chế độ thường và chỉ đọc: tiêu đề màn › thẻ, giới thiệu, đủ phím của chân phím trong khung, thứ tự nhóm, không có tiêu đề hành động Writes ở chỉ đọc; thẻ Log chỉ hiện khi có việc nền nên được bỏ qua), `TestHelpContextReadOnlyDropsWriteKeys`,
  `TestHelpScrollKeysAndWheel` (j/k, PgUp/PgDn, bánh xe, End → thấy dòng cuối, vị trí cuộn); golden `help_80x24_vi` và 140×40 ghi lại.

**README**: mục "Giao diện terminal (TUI)" (cách mở, khi nào `bint` in trợ giúp thay vì mở TUI, tám tab, phím toàn cục, chuột và cách bật tắt, chế độ chỉ đọc, `NO_COLOR`, ngôn ngữ, liên kết tài liệu này). `TestREADMEDocumentsTUIGlobalKeys` đối chiếu mọi phím của hành động toàn cục có `Key` trong sổ với mục đó.

**Chốt giai đoạn 6**: `actions.CurrentPhase = 6`; `Pending`/`PendingFlags` rỗng, `PendingCeiling = 0`; subtest của `internal/cli/coverage_test.go` đổi sang giai đoạn 6; `TestCommandAndFlagCoverage` xanh. Golden: ghi lại bằng `-update` hai lần liền không đổi tệp nào; `go test ./internal/tui/... -count=2` xanh.

**Sửa sau review của 6b** (phiếu 01M76KN3M9BZCNMHKN4KNP1639; mã `.n` là phát hiện 01M769D0Y45T2Y178ZC5GHD5QW.n)
- **.2/.8 (toast kết thúc việc nền)**: mọi chỗ gán toast của mô hình gốc đi qua `Model.setToast(text, isErr)` (đặt chữ và cờ lỗi cùng lúc, xoá toast cũng xoá cờ); `doneToast` trả (chữ, lỗi): việc nền lỗi hoặc dừng ở blocked/failed/interrupted là toast lỗi, việc thành công là toast thường kể cả ngay sau toast lỗi, `exit.stopping` là toast thường.
  Test: `TestBackgroundJobDoneToastErrFlag`, `TestBackgroundJobDoneSuccessAfterErrorToast`, `TestExitStoppingToastIsPlainAndClearResetsFlag`, `TestToastOnlyAssignedInSetToast` (quét `go/ast`, có ca ngược).
- **.3 (cờ ghi ở chỉ đọc)**: `knowledge verify --apply` vẫn chỉ phím v ở Tri thức kèm ô Áp dụng; ở chỉ đọc thêm ghi chú vì phần áp dụng chạy `knowledge.verify_apply` (`Writes`), ở chế độ thường không có. Test: `TestRewriteWriteFlagReadOnlyNote`.
- **.4/.10 (không còn trạng thái toàn cục)**: hàm đặt chế độ chỉ đọc toàn cục của gói `ui`, biến `readOnly` và `cliReg` bị xoá; `ui.Rewriter{Cat, Reg, ReadOnly}` là của từng mô hình gốc (`Options.Registry`, `Options.ReadOnly`), truyền qua `ui.Env.RW` tới mọi màn hình, Tổng quan, `ErrText`, `FindingText`.
  Test: `TestTwoModelsKeepTheirOwnReadOnlyNote` (hai mô hình khác `ReadOnly` trong cùng tiến trình, cả hai thứ tự dựng), `TestModelWithCustomRegistryRewritesByItsKeys`, `TestRewriterSurvivesLanguageChange`.
- **.5/.9 (test trợ giúp và chân phím chặt hơn)**: `TestHelpContextEveryScreenAndSubTab` lấy phím và nhãn từ chân phím trong khung đã vẽ trước khi nhấn `?` (không lấy từ `helpContext`) và khẳng định từng cặp có trong nhóm ngữ cảnh; ở chỉ đọc khẳng định theo `ActionID` của hint rằng không phím nào của hành động `Writes` nằm trong nhóm.
  `TestInnerFooterHidesWritesInReadOnly` sinh ca từ `subStates()` cho mọi màn và thẻ phụ thay vì bảng tay, có ngưỡng tối thiểu số phím ghi được kiểm. Ca ngược: `TestFooterAssertHelpersCatchGaps`, `TestInnerFooterAssertionCatchesLeakAndLoss` (đỏ khi thiếu phím hoặc lọt phím ghi).
- **.6/.11 (hộp thoát)**: lệnh tiếp tục (`j.Resume`, kèm mã tác vụ hoặc mã đợt đầy đủ) sau "Tiếp tục sau bằng" in nguyên văn, không qua hàm viết lại. Test: `TestExitDialogWithRunningWorkflow` (`bint run resume <ULID>`), `TestExitDialogKeepsFullEvalResumeCommand` (`bint eval run --resume eb_…`).
- **.7 (kết quả vòng tiến hoá và lý do trong Lịch sử)**: `Outcome`/`StopReason` của `evolve.cycle`, `Locked` của `evolve.process`, lý do bỏ qua và loại của `evolve.propose` và `Reason` ở thẻ Lịch sử đi qua hàm viết lại của mô hình. Test: `TestEvolveServiceMessagesGoThroughRewriter`, `TestEvolveProcessLockedAndProposeSkippedRewritten`.
- **.12 (mã có tiền tố và cờ rời)**: mã tiền tố chữ thường + gạch dưới (`ep_`, `eb_`, `ea_`, `ec_`, `er_`, `ee_`, `ead_`) là đối số của lệnh nên không sót sau tham chiếu; cờ rời `--unverified`, `--confirm-risk`, `--force`, `(--git local)`, `--update:` được thay bằng cụm TUI (khoá `cliref.flag.*` ở vi và en) kể cả khi chuỗi không có chữ "bint";
  cờ trong lỗi ngoài (`git: use --force …`) không đổi. Test: `TestRewritePrefixedIDsAndLooseFlags`, `TestPhraseKeysExistInBothLanguages` (`internal/tui/ui`), `TestServiceMessageCLIInventory` và `TestServiceMessageCLIInventoryCatchesGaps` (`internal/cli`).

**Sửa sau review vòng 1 của phiếu 01M76KN3M9BZCNMHKN4KNP1639** (mã `.n` là phát hiện 01M76KN3M9BZCNMHKN4KNP1639.n; sửa ở phiếu 01M76NWG57TVW63NE4J1MMHQFQ)
- **.1 (mục này)**: ghi phần sửa `.2`–`.12` của phiếu 01M769D0… ở đoạn trên; sửa các câu lỗi thời (`ui.Rewrite(cat, s)` → `Rewriter.Rewrite`, bỏ hàm đặt chỉ đọc toàn cục ở mô tả và ở mục lệch, lệnh tiếp tục ở hộp thoát in nguyên văn thay vì đi qua hàm viết lại); không còn chỗ nào nhắc trạng thái chỉ đọc toàn cục của gói `ui`.
- **.2 (cờ rời trong luồng TUI)**: thêm cụm viết lại `cliref.flag.*` (vi và en, không còn token `--`) cho: `dùng --force nếu vẫn muốn duyệt` và `hoặc dùng --force` (duyệt ứng viên, chỉ tới ô "Vẫn duyệt dù …" của hộp duyệt), `hoặc dùng --force để chạy vẫn` (đánh giá đề xuất, ô "Chạy dù không khả thi"; đứng trước cụm ngắn hơn),
  `dùng --force để ghi đè` (quay lui, ô ghi đè của hộp quay lui), `bỏ --no-agent` (ba nơi: `generate.go`, `harness_propose.go`, `reflector.go`; ô "Không dùng agent") và `tăng --budget` (ô "Ngân sách ($)" của form chạy). Test bảng ở `internal/tui/ui` (`TestRewriteLooseFlagsOfServiceFlows`) dùng chuỗi đọc nguyên văn từ nguồn dịch vụ (`srcFrag` báo khi chuỗi đổi);
  đầu-cuối ở `internal/tui/app` (`TestCLIRefLooseFlagsInServiceFlows`): hộp kết quả duyệt ứng viên, hộp kết quả đề xuất và lỗi quay lui (hộp Thông báo và màn hình) vẽ ra không còn `--force`/`--no-agent`.
- **.3 (kiểm kê bắt được cờ rời mới)**: bỏ `knownLooseFlagRE`; `inventoried` nhận mọi literal nhắc `bint <lệnh>` hoặc có token `--cờ` đứng rời, trừ literal chỉ là một token argv của lệnh ngoài (`^--?x(=…)?$`). Quét thật ra 62 literal còn cờ rời: 20 hướng dẫn hành động được viết lại thêm
  (ngân sách `--budget <USD>`/`(USD)`/`> 0`, `giảm --limit/--repeats/--arms`, `dùng --bare`, `thêm --approved`, `--source`, `--hooks`, `--allow`, `--no-plan`, `--arms`, `--task <id>`, `--delivery bint`, `--prospective`, `cần --reason`/`--evidence`, `thiếu --type`, `chỉ định --kind`, `--paths`) và 55 loại trừ theo nhóm lý do.
  Ca ngược: `dùng --frobnicate nếu muốn` (không có "bint", không thuộc cụm nào) làm `checkInventory` báo "cờ CLI rời"; `inventoried` loại `--name-only`, `--format=%H`.
- **.4 (`posRE`)**: đối số mã dạng tiền tố + gạch dưới cần ít nhất một chữ số ở phần sau (`[a-z]{1,4}_[0-9A-Za-z]*[0-9][0-9A-Za-z]*`), nên `bint task show task_id`, `key_name`, `item_x` giữ nguyên chữ sau lệnh, còn `ep_<ULID>`, `eb_123`, `ea_1`, `ead_<ULID>`, `ee_7` vẫn được nuốt. Test: `TestRewritePositionalIDNeedsDigit`.
- **Chỗ lệch còn lại**: 44 literal lỗi kiểm tra đối số (`--repeats phải >= 1`, `--plan và --no-plan loại trừ nhau`, `--git: phải là …`, …) chưa có cụm TUI riêng: form TUI dựng từ cờ của lệnh nên tên cờ trùng tên ô và ô chọn/số/công tắc đã ngăn phần lớn giá trị sai, nhưng nếu vẫn lọt thì người dùng thấy tên cờ nguyên văn.
  Nhóm này nằm trong `rUsage` của `internal/cli/cliref_inventory_test.go`; thêm literal mới cùng loại phải thêm vào đó kèm lý do hoặc viết lại bằng cụm `cliref.flag.*`.
- Golden không đổi (không chạy `-update`).

**Chạy đỏ trước khi sửa**
(kiểm bằng cách vô hiệu hoá chính thay đổi trong cây làm việc rồi chạy test, vì cây `HEAD` không có thay đổi của 6a; sau khi ghi kết quả đã hoàn nguyên)
- Sửa .1: `TestMouseRunsLessonsSkipGroupHeaders` đỏ trên mã trước khi sửa: "bấm tiêu đề nhóm đặt con trỏ lên tiêu đề".
- Sửa .3: `TestRootToastErrFlag` không biên dịch được trên mã cũ (chưa có `toastErr`); logic cũ luôn trả "lỗi" cho toast của mô hình gốc.
- Chỉ đọc: gỡ `Env.Allowed` thì `TestInnerFooterHidesWritesInReadOnly` đỏ ("Đánh giá › Ứng viên: chế độ chỉ đọc không được còn phím ghi "a" ở chân phím trong khung") và `TestHelpContextReadOnlyDropsWriteKeys` đỏ ("phím ghi "a": thường=true chỉ đọc=true"); `tasks.open_file` có `Writes = false` thì `TestWritesAudit` đỏ ("tasks.open_file: Writes = false, bảng kiểm muốn true").
- NO_COLOR: bỏ đảo màu tiêu đề khung focus thì `TestMonoNothingReliesOnColorAlone` đỏ ("khung đang focus phải khác khung thường ở Mono").
- Review vòng 1 của phiếu 01M76KN3… (kiểm bằng cách gỡ tạm chính thay đổi rồi hoàn nguyên): `.4` — `TestRewritePositionalIDNeedsDigit` đỏ ("\"task_id\" bị nuốt vào tham chiếu: phím Enter ở Tác vụ", cả vi và en, cả `key_name` và `item_x`); `.2` — gỡ cụm `cliref.flag.force_*`/`no_agent`/`budget` thì `TestRewriteLooseFlagsOfServiceFlows` đỏ (22 dòng lỗi) và `TestCLIRefLooseFlagsInServiceFlows` đỏ ("duyệt ứng viên: còn token cờ --force", "kết quả đề xuất: còn token cờ --no-agent", "lỗi quay lui (hộp Thông báo): còn token cờ --force");
  `.3` — thay `inventoried` bằng bộ lọc cũ (chỉ cụm đã biết) thì `TestServiceMessageCLIInventoryCatchesGaps` đỏ ("literal chỉ có cờ rời mới phải vào phạm vi kiểm kê").
- Thông điệp theo phím TUI: trước khi nối `Rewriter.Rewrite` vào dòng "Tiếp tục từ … (bint run resume …)" của Tác vụ, `TestCLIRefTasksScreen` đỏ ("lý do blocked: còn lệnh CLI trên màn hình/hộp Thông báo").

**Chỗ lệch so với kế hoạch / giới hạn (người duyệt có thể đổi)**
- **Viết lại ở tầng hiển thị thay vì mã lỗi có cấu trúc** (mục 7.1 hẹn mã lỗi cho từng chuỗi; **riêng thông điệp bằng chứng tri thức lệch đã chuyển sang lỗi có cấu trúc ở mục 12.11**, phần còn lại vẫn viết lại): đổi hàng trăm điểm sinh lỗi ở sáu gói là rủi ro làm lệch chữ CLI và không phủ chuỗi đã lưu; bù lại ngữ pháp đôi chỗ gượng ("nhấn e ở Hệ thống › Cài đặt [ô Đăng ký MCP]" sau "chưa đăng ký MCP server của Bint (…)"). Chuỗi dịch vụ vẫn là
  tiếng Việt kể cả khi giao diện `en` (chưa dịch ở tầng dịch vụ): chỉ phần tham chiếu TUI được dịch.
- **Tham chiếu theo thẻ phụ do bảng `cliRefs` ghi tay** (phím trong sổ chỉ có nghĩa ở một thẻ phụ): thêm lệnh CLI mới có hành động mà quên bảng thì `TestServiceMessageCLIInventory` đỏ nếu có literal nhắc lệnh đó; nếu chỉ thêm hành động mà không có thông điệp nào nhắc thì không có gì đỏ (và không cần).
- **Khung focus ở Mono khác nhau bằng kiểu chữ (đảo màu tiêu đề)**, không bằng ký tự: bản vẽ thuần chữ giống nhau nên golden `.txt` không đổi; nếu terminal không hiển thị đảo màu thì không thấy khác (chọn thế để không đổi hàng trăm golden và viền).
- **Test trợ giúp bỏ qua thẻ Log** (Tác vụ, Đánh giá, Tiến hoá: thẻ chỉ hiện khi có việc nền); trợ giúp của thẻ Log dùng chung đường mã với các thẻ khác.
- **`!` và hộp Thông báo** nhận văn bản đã viết lại (một lần ở `harvestNotices`/`currentNotice`); toast của màn hình không có cờ riêng cho hộp ngoài `isErr` của `ui.Toaster`.
- **Chưa kiểm được tự động**: terminal thật với `NO_COLOR`, Windows thật (chỉ `go vet`/staticcheck `GOOS=windows`), thử với 2–3 người không quen CLI (tiêu chí nghiệm thu mục 9), ảnh VHS (`vhs`/`ttyd` không có ở môi trường làm việc; `.github/workflows/**` được bảo vệ).

**Chạy tay**

```sh
go test ./internal/tui/app -run 'Golden|Narrow|Help' -update   # ghi lại golden sau thay đổi giao diện chủ ý (đọc diff trước); chạy hai lần không đổi tệp nào
go test ./internal/cli -run 'Inventory' -v                      # in bản trước/sau của từng literal nhắc lệnh bint bị viết lại
NO_COLOR=1 bint tui                                             # chế độ đơn sắc; thử `bint tui --read-only` và phím ? ở từng thẻ phụ
```

### 12.11 Bằng chứng tri thức lệch: lỗi có cấu trúc (đã xong, 2026-11-12)

Phiếu 01M76RV9STQEA4A47AY1QC4V8T. **Ca thật**: mục 01M74PDT2ETJAYRWFV9ZPWVHMR có bằng chứng `internal/cli/eval.go:1009`, nhưng dòng đó đã chuyển sang `internal/app/eval_run.go:220`.
`bint knowledge approve` báo "bằng chứng đã đổi, xem lại rồi `--refresh`", TUI viết lại thành "xem lại (Enter) rồi bấm f", và `f` hỏng y hệt (vùng 1009 vượt quá tệp mới ngắn đi) — vòng lặp không lối ra; ở giao diện en
còn lẫn tiếng Việt ("key Enter in Knowledge" trong câu tiếng Việt cứng). Gốc rễ: tầng dịch vụ trả **chuỗi tiếng Việt có lệnh CLI**, người dựng câu phải đoán lại thông tin (chuyển tệp hay đổi nội dung? `--refresh` có chạy được không?) bằng mẫu chuỗi.
Hướng sửa: tầng dịch vụ trả **dữ liệu có cấu trúc**, CLI và TUI tự dựng câu.

**Kiểu dữ liệu** (`internal/knowledge/issue.go`)
- `knowledge.EvidenceIssue{Index, Kind, Path, Lines, Symbol, FileLines, NewPath, NewLines, Candidates, Truncated}` và `Fix()` ∈ `apply | refresh | edit | none`.
  Loại (`Kind`) và cách sửa: `relocated`, `moved` → `apply` (cập nhật vị trí bằng `verify --apply`); `changed` (vùng còn trong tệp, nội dung đã đổi) → `refresh` (xem lại rồi `approve --refresh`);
  `missing_file`, `out_of_range` (kèm `FileLines`), `symbol_gone`, `bad_range`, `ambiguous` (kèm tối đa 5 ứng viên `path:lines`) → `edit` (sửa bằng chứng trong tệp mục hoặc từ chối); `no_fingerprint` → `none`.
- `Finding.Evidence *EvidenceIssue` (JSON `evidence`, `omitempty`) gắn vào mọi finding về bằng chứng; mã mới `CodeMoved = "moved"` (mức `error`: mục trỏ tệp sai, chặn duyệt). Các mã cũ giữ nguyên cho từng ca (`out_of_range`/`changed` vẫn `fingerprint`, `missing_file` vẫn `evidence`,
  ký hiệu mất vẫn `symbol`) nên `VerifyKnowledge`, `revalidate`, `PlanBatch` và test cũ không đổi nghĩa. `Finding.Detail` vẫn là câu tiếng Việt trung tính cho JSON/MCP/lý do trạng thái nhưng **không còn lệnh bint**.
- `EvidenceCheck` thêm `Kind` và `FileLines` (`CheckEvidence` giữ chữ ký, `eval` và `ctxengine` vẫn dùng như cũ). `FillFingerprints`/`FingerprintWith` trả `*knowledge.EvidenceIssueError` (`Error()` giữ chữ cũ, lấy `Issue` bằng `errors.As`).
- `app.EvidenceError{ID, Status, File, Issues}` (`internal/app/evidence_err.go`): lỗi duyệt mục có bằng chứng lệch (`approve` và `approve --refresh`), lấy bằng `errors.As`. `Fix()` là cách sửa nặng nhất (edit hơn apply hơn refresh);
  `Error()` là câu tiếng Việt trung tính không có lệnh. `approve --refresh` **kiểm trước**: có vấn đề `edit` thì trả `*EvidenceError` mà không băm gì (trước đây trả lỗi thô "có N dòng, bằng chứng tham chiếu dòng …");
  vùng `relocated`/`moved` thì áp vị trí mới rồi mới làm mới fingerprint (sửa luôn lỗi tiềm ẩn: trước đây `--refresh` trên vùng đã dời băm nhầm nội dung ở số dòng cũ).
  `VerifyReport.Moved` liệt kê mục bị đổi đường dẫn.

**Tìm chuyển tệp** (`internal/knowledge/moved.go`, bật bằng `Verifier.FindMoved`, mặc định tắt; `app` bật ở `VerifyKnowledge`, `ReviewKnowledge`, `DetailOf`, `PlanBatch`; `AddKnowledge`, `revalidate`, `evolve`, `eval`, `ctxengine` không bật nên không đổi hành vi hay chi phí)
- Chỉ chạy khi **khối không còn ở tệp cũ** và bằng chứng có fingerprint: tệp mất (`missing_file`), vùng dòng vượt quá tệp (`out_of_range`) hoặc ký hiệu mất (`symbol_gone`). **Nội dung đổi tại chỗ** (`changed`: vùng còn trong tệp nhưng nội dung khác; mức `file`/`symbol` mà tệp/ký hiệu còn) **không** tìm chuyển tệp: vẫn là `changed` (cách sửa `refresh`, mục `verified` thành `stale` khi `--apply`), dù khối cũ còn ở một, nhiều tệp khác hoặc là khối yếu — bản sao ở nơi khác không thay được khối đã sửa và không được chặn `approve --refresh`. `bad_range` cũng không tìm. Tìm khối **cùng fingerprint** ở tệp khác theo mức đã lưu: `lines` (cửa sổ trượt cùng độ dài), `symbol` (`symbols.Locate` rồi so băm), `file` (băm cả tệp).
- Thứ tự ứng viên: (a) đích đổi tên của tệp cũ theo `git diff -M --name-status <commit>` (`commit` = `evidence.commit`, rồi `origin.commit`); (b) tệp thêm/sửa kể từ commit đó và tệp chưa theo dõi, **cùng đuôi** với tệp cũ (tài liệu `.md` trích mã không tính);
  (c) không có commit dùng được (không ghi, hoặc git lỗi): mọi tệp cùng đuôi, gần thư mục cũ trước. `ChangedSince` (`ChangeTracker`, cài cho `WorktreeSource` và `CommitSource`) nhớ theo commit để verify cả kho không gọi git lặp lại.
- **Giới hạn chi phí cố định cho mỗi bằng chứng**: `maxMoveFiles = 400` tệp ứng viên, `maxMoveBytes = 32 MiB` đã đọc, bỏ tệp lớn hơn 1 MiB, tổng công việc băm dùng chung `maxRelocateWork` (4 triệu). Vượt giới hạn (số tệp, byte hoặc công việc băm): dừng và đặt `Truncated` (hiện "tìm chuyển tệp dừng ở giới hạn chi phí"), **không bao giờ báo "đã chuyển"**: một nơi khớp trong phần đã quét chưa chắc là nơi duy nhất, nên kết quả là `ambiguous` kèm ứng viên đó và `Truncated`.
- Đúng **một** vị trí khớp và khối đủ đặc trưng (từ 20 ký tự không trắng) → `moved` (kèm `Relocation{FromPath, ToPath, From, To}`). Từ hai vị trí trở lên, khối quá ngắn như `}`, hoặc tìm bị cắt (`Truncated`) dù chỉ một nơi khớp → `ambiguous` kèm ứng viên, **không** chuyển. `ambiguous` phát sinh từ vùng vượt quá tệp giữ `FileLines` (số dòng hiện có của tệp); `moved` đặt `FileLines = 0`.
- `ApplyRelocations` (`verify --apply`) áp cả đổi đường dẫn lẫn vùng dòng, **chỉ khi path và lines hiện tại còn đúng như lúc kiểm**; không đụng fingerprint, ký hiệu, commit; `ambiguous` không bao giờ áp. Giữ mọi bất biến cũ: trạng thái không đổi chỉ vì dời; loại cần người duyệt (cấu hình `knowledge.require_human_approval_for`,
  mục bên thứ ba) không tự thành verified; `RequireHuman` vẫn chặn từ phiên agent. Sau khi áp, `VerifyReport.Results` của mục là **kết quả kiểm lại sau khi áp** (không còn finding `moved`/`✗`), `Relocated`/`Moved` ghi vị trí nào đã đổi; nên `verify --apply` thoát 0 khi chỉ còn việc đã áp, và hộp kết quả của TUI không gợi ý phím cập nhật vị trí. **Đổi nghĩa có chủ ý**: mục `verified` có bằng chứng chuyển tệp trước đây thành `stale` khi `--apply`, nay được dời và giữ `verified` (giống `relocated`).

**CLI dựng câu** (`internal/cli/knowledge_evidence.go`, chỉ tiếng Việt)
- `evidenceText(issue)` dựng câu từ tham số (vị trí cũ, vị trí mới hoặc ứng viên, số dòng tệp); `evidenceHint(fix, id, file)` chọn gợi ý lệnh đúng ca: `apply` → `` `bint knowledge verify --apply <ID>` ``; `refresh` → "xem lại (`bint knowledge show <ID>`) rồi `bint knowledge approve --refresh <ID>`" (chữ cũ,
  `TestApproveChangedEvidenceKeepsCLIWording` không đổi); `edit` → sửa bằng chứng trong tệp mục (`.bint/knowledge/items/<ID>.md`) hoặc `` `bint knowledge reject <ID> --reason "..."` `` và **không** gợi ý `--refresh`. Dùng ở `show`, `verify`, `review`, `approve`/`reject` (`errors.As` ra `*app.EvidenceError`).
  `verify` thêm dòng "đổi đường dẫn vì khối chuyển tệp"; câu `ambiguous` thêm "tệp <path> chỉ còn N dòng" khi biết số dòng; trợ giúp Long của `knowledge verify` và chữ cờ `--apply` nói `--apply` cũng đổi `path` khi khối chuyển tệp (trạng thái giữ nguyên); JSON giữ khoá cũ, thêm `evidence` ở finding, `from_path`/`to_path` ở `relocated`, `moved` ở báo cáo.

**TUI dựng câu qua i18n** (`internal/tui/ui/evidence.go`; không qua `Rewriter.Rewrite`)
- `EvidenceCause(rw, issue)` (vị trí cũ, chi tiết theo loại, vị trí mới/ứng viên, số dòng tệp, ghi chú cắt giới hạn), `EvidenceHint(rw, fix)` và `EvidenceLines/EvidenceText`; `ErrText` có nhánh `errors.As(*app.EvidenceError)` (`err.evidence`); `FindingText` dùng cấu trúc khi `Finding.Evidence != nil`,
  `FindingCause` (không có cách xử lý) khi mục chưa được ghi (thêm mục). Hộp kết quả của `verify --apply` thêm dòng báo đã cập nhật vị trí/đổi đường dẫn từ `VerifyReport.Relocated`/`Moved` (`kn.verify.relocated`, `kn.verify.moved`). Số dòng tệp (`kn.ev.file_lines`) hiện cho mọi ca có `FileLines > 0`, kể cả `ambiguous`. Khoá: `kn.ev.at|new|candidates|file_lines|truncated`, `kn.ev.<loại>`, `kn.ev.fix.<cách sửa>`, `err.evidence`, `finding.moved` ở `vi.yaml` và `en.yaml`.
- Phím dựng từ sổ hành động bằng `Rewriter.ActionRef(id, cờ…)` (`actions.TargetFor`; cùng định dạng và ghi chú chỉ đọc với tham chiếu viết lại, **không phân tích chuỗi**). Gợi ý theo ca: `apply` → phím `v` ở Tri thức `[ô Áp dụng]`; `refresh` → `Enter` rồi `f`;
  `edit` → `e` (sửa bằng chứng trong tệp mục) hoặc `r` (từ chối). **Không bao giờ gợi ý `f` khi cách sửa không phải `refresh`.**
- Nối vào: khung Detail của mục (dòng riêng cho vị trí cũ, vị trí mới/ứng viên/số dòng, cách xử lý), hộp kết quả xác minh, toast và hộp Thông báo của lỗi duyệt (`afterOutcomes` không lặp lại finding bằng chứng đã có trong lỗi), form Thêm mục (chỉ nguyên nhân).

**Phần còn đi qua `Rewriter`**: mọi thông điệp dịch vụ khác vẫn là chuỗi có lệnh CLI và đi qua `Rewriter.Rewrite` (bảng `cliRefs`, `phraseRE`); bảng và luật không bị gỡ vì `transition.go` còn nhắc `verify --apply` và `approve --refresh`, và `TestServiceMessageCLIInventory`
đòi mọi literal nhắc lệnh phải được viết lại. Thông điệp bằng chứng không còn đi qua đó, nên ca test chỉ dựa vào chuỗi dịch vụ đã bỏ ("cập nhật vùng dòng", `approve --refresh` trong lỗi duyệt) được chuyển sang literal còn tồn tại (`transition.go`).

**Test**
- `internal/knowledge/moved_test.go`: `TestMovedOneLineWithCommit` (ca thật: khối 1 dòng cùng thụt lề từ `internal/cli/eval.go` sang `internal/app/eval_run.go`, `origin.commit` và `evidence.commit`), `TestMovedOneLineWithoutCommit` (kể cả commit không tồn tại), `TestMovedFileDeleted`,
  `TestMovedRename` (mức `lines`, `symbol`, `file`), `TestMovedSymbol`, `TestMovedAmbiguous`, `TestMovedWeakBlockNotSearched`, `TestMovedBudgetTruncated`, `TestMovedOffByDefault`, `TestIssueKinds`, `TestFillFingerprintsStructuredError`, `TestApplyRelocationsPathGuard`, `TestMovedNotForInPlaceChange` (đổi tại chỗ, khối cũ ở một/hai tệp khác hoặc khối yếu → `changed`/`refresh`, không `moved`), `TestMovedTruncatedSingleMatchAmbiguous` (một bản trong giới hạn, một bản ngoài → `ambiguous` + `Truncated`).
- `internal/app/evidence_err_test.go`: `TestVerifyApplyMovedRealCase` (mục warning candidate: sau `verify --apply` trỏ path/lines mới, còn candidate, `approve` không `--refresh` thành công), `TestVerifyApplyMovedKeepsVerified`, `TestVerifyApplyAmbiguousNotApplied`,
  `TestApproveEvidenceErrorFixes` (chuyển tệp, tệp ngắn đi, tệp xoá, nội dung đổi; `approve` và `--refresh`), `TestRefreshHashesRelocatedRegion`, `TestVerifyApplyMovedResultsAfterApply` (`Results` sau khi áp), `TestRefreshInPlaceChangeWithCopies` (đổi tại chỗ, khối cũ ở hai tệp khác: `--refresh` thành công), `TestVerifyApplyInPlaceChangeGoesStale` (mục `verified`, bản sao ở một tệp khác: `stale`, path giữ).
- `internal/cli/knowledge_evidence_e2e_test.go`: `TestApproveEvidenceCasesCLIWording` (năm ca; `approve`, `show`, `verify`), `TestVerifyApplyMovedThenApproveCLI` (thoát 0, có dòng đổi đường dẫn, không `✗`, không gợi ý `verify --apply`), `TestVerifyHelpMentionsMovedApply`; ca nhiều nơi khớp từ tệp ngắn đi có "chỉ còn N dòng".
- `internal/tui/ui/evidence_test.go` (mọi loại và cách sửa ở vi và en), `TestEvidenceCauseAmbiguousKeepsFileLines`, `TestEvidenceKeysBothLanguages` (`internal/tui`), `TestVerifyApplyMovedBoxAfterApply` (hộp `verify --apply` đã áp: không `✗`, không gợi ý phím, có dòng báo cập nhật vị trí, vi và en), `TestEvidenceFlowAllSurfaces` (`internal/tui/app/evidence_flow_test.go`: dịch vụ thật trên repo tạm, năm ca ở vi và en, khung Detail, hộp xác minh, toast, hộp Thông báo;
  đỏ khi gỡ tạm nhánh `*EvidenceError` của `ErrText`).

**Chỗ lệch / giới hạn**
- CLI chỉ có tiếng Việt nên test CLI chỉ ở vi; vi và en cùng được kiểm ở TUI.
- Khớp bằng băm 16 hex của fingerprint; nội dung gốc không có sẵn nên không so được từng dòng. Khối ngắn dùng ngưỡng 20 ký tự không trắng; khối lặp lại ở nhiều tệp là `ambiguous`.
- Sau khi dời, `scope.paths` của mục có thể không phủ path mới (ca thật): không tự sửa; finding `scope` vẫn báo.
- Không chạy `--apply` trên mục thật 01M74PDT… (tác vụ cấm sửa `.bint/knowledge/items`); xem kết quả bằng `bint knowledge verify 01M74PDT2ETJAYRWFV9ZPWVHMR` (không `--apply`).
- Chưa đo hiệu năng trên kho rất lớn (chỉ có test giới hạn).
