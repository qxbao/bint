# Kế hoạch TUI cho Bint

Trạng thái: đề xuất (2026-11-11), đã rà soát lần 1 cùng ngày (phủ cấp cờ, vòng đời, tiêu chí
thẩm mỹ). Chưa bắt đầu triển khai.

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
  **MCP** (`mcp tools`: danh sách công cụ đang mở cho agent), **Ngôn ngữ**.

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
| | `mcp tools` | Hệ thống › MCP |
| Workflow | `task new/list/show/set/status/write/criteria/error/review/lint/verify/approve/transitions` | Tác vụ |
| | `run`, `run resume/status` | Tác vụ › Chạy / Tiếp tục (việc nền) |
| | `skill *` | Hệ thống › Skill |
| Lượt chạy | `runs list/show/rate/scan`, `events`, `events ingest` | Lượt chạy |
| Đánh giá | `eval mine/list/show/spec/verify/approve/reject/estimate/run/report/bootstrap/clean` | Đánh giá |
| Tiến hoá | `evolve scan/propose/list/show/evaluate/apply/reject/rollback/history/queue/process/cycle` | Tiến hoá (cả đề xuất loại harness) |
| | `experience digest/show` | Lượt chạy › Bài học |
| Loại trừ | `mcp serve` (server stdio), `hook *` (do harness gọi), `help`, lệnh ẩn | Không có màn hình; hiện trong Hệ thống › Harness dưới dạng trạng thái |

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
  `bint --help` luôn in trợ giúp.
- **Tầng dịch vụ**: mọi hành động TUI gọi hàm trong `internal/app` trả về struct, không gọi
  `cobra`, không phân tích văn bản đầu ra, không chạy tiến trình `bint` con. CLI cũng chuyển
  sang gọi cùng hàm (CLI chỉ còn phân tích cờ và định dạng).
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
| 0 | Tách tầng dịch vụ cho `knowledge review`, `eval`, `evolve` (cycle, apply, report) khỏi `internal/cli`; đưa kiểm tra chỉ-dành-cho-người, ràng buộc cờ (`Options.Validate`), giá trị mặc định (`Defaults`) và lời hỏi `confirm()` xuống `app`. Khung TUI: theme, khung viền tự vẽ, tab, chân phím theo ưu tiên, bảng lệnh, sổ hành động, `internal/i18n` (vi, en) và khoá `ui.language`, test phủ lệnh + cờ với danh sách chưa làm. **Màn Tổng quan mẫu** + trang trưng bày + kịch bản VHS; màn tạm "Khởi tạo với mặc định" cho repo chưa `init`. | CLI chạy lại toàn bộ test không đổi hành vi; `bint` mở khung 7 tab, đổi `vi`/`en` được; chủ dự án duyệt ảnh màn mẫu và chốt bảng màu. |
| 1 | Tổng quan + hộp thư; Tri thức đầy đủ, gồm duyệt theo lô và lọc theo `nguon:*`. | Duyệt 30 mục từ một lần `/b-knowledge` trong < 1 phút, không gõ lệnh. |
| 2 | Tác vụ + chạy/tiếp tục workflow có log trực tiếp, câu hỏi giữa chừng thành hộp thoại, xác nhận khi thoát còn việc nền. | Từ TUI tạo tác vụ, chạy `b-do`, duyệt, xem review đến `done`. |
| 3 | Lượt chạy, Bài học, Tiến hoá (cả đề xuất loại harness). | Áp dụng và quay lui một đề xuất hoàn toàn trong TUI. |
| 4 | Đánh giá, có biểu đồ báo cáo. | Chạy một đợt nhỏ và đọc báo cáo trong TUI. |
| 5 | Hệ thống (cấu hình, cài đặt repo, harness + thành phần, skill, chỉ mục, MCP) + vòng đời: trình hướng dẫn `init`, nâng cấu hình, cập nhật Bint và khởi động lại. | Repo mới: `bint` → hướng dẫn → khởi tạo xong không cần đọc tài liệu; repo cấu hình cũ được nâng qua hộp thư; `update` rồi khởi động lại trong TUI. |
| 6 | Hoàn thiện: chuột, chế độ hẹp, `NO_COLOR`, chế độ chỉ đọc, trợ giúp theo ngữ cảnh, README. | Danh sách chưa làm rỗng, test phủ lệnh và cờ xanh; ảnh chụp ổn định; thử với 2–3 người không quen CLI. |

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
