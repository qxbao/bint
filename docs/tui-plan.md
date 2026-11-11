# Kế hoạch TUI cho Bint

Trạng thái: đề xuất (2026-11-11). Chưa bắt đầu triển khai.

## 1. Mục tiêu

1. **Đẹp**: bố cục khung viền có tiêu đề nằm trên đường viền, thanh tab, màu nhấn tiết chế,
   phím tắt hiện ở viền dưới (tham khảo phong cách binsider).
2. **Thân thiện**: người không thuộc lệnh vẫn dùng được hết Bint. Mọi thao tác nhìn thấy
   được, không cần gõ cú pháp, không cần nhớ ID.
3. **Đủ tính năng**: mọi lệnh lá của CLI đều có chỗ trong TUI, hoặc được loại trừ có lý do
   (bảng ở mục 6). Có kiểm thử ép buộc điều này (mục 8).
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
- Thao tác chỉ dành cho người (`approve`, `reject`, `review`, `apply`, `rate`,
  `task approve`) chặn bằng `app.InAgentSession`. Một số chỗ kiểm tra ở tầng CLI
  (`cli/knowledge.go`, `cli/runs.go`, `cli/task.go`); khi tách tầng dịch vụ phải đưa kiểm
  tra xuống dưới để TUI không vô tình bỏ qua.
- Hook không được mở SQLite và không được chặn agent. TUI là tiến trình riêng, đọc SQLite
  bình thường, nhưng phải chịu được việc hook và `bint run` cùng ghi.
- Chưa có thư viện TUI nào trong `go.mod`.

## 3. Lựa chọn công nghệ

**Bubble Tea + Lip Gloss + Bubbles (Charm)**: mô hình Elm (Model/Update/View) dễ kiểm thử,
Lip Gloss lo viền, màu và bố cục, Bubbles có sẵn table, list, viewport, textinput,
textarea, spinner, progress, help. Chạy được trên Windows, phù hợp yêu cầu
`GOOS=windows` của CI. `teatest` cho kiểm thử ảnh chụp đầu ra.

Ghim phiên bản ổn định mới nhất tại thời điểm bắt đầu (cân nhắc v2 nếu đã ổn định) và
kiểm tra `go vet`/`staticcheck` với `GOOS=windows` ngay ở giai đoạn 0.

Không chọn tview: kiểu widget giữ trạng thái khó viết test ảnh chụp, khó tạo phong cách riêng.

## 4. Thiết kế giao diện

### 4.1 Bố cục chung

```text
┌───────────────────────────────│bint 0.1.0│────────────────────────────────┐
│ Tổng quan │ Tri thức ³ │ Tác vụ │ Lượt chạy │ Đánh giá │ Tiến hoá │ Hệ thống │
└────────────────────────────────────────────────────── ~/Project/Go/bint ───┘
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│                          (nội dung màn hình)                                │
│                                                                             │
└──[Enter→ Mở] [a→ Duyệt] [/→ Tìm] [:→ Lệnh] [?→ Trợ giúp] [q→ Thoát]─────────┘
```

- Tiêu đề khung nằm trên viền (`│Phụ thuộc│` như ảnh tham khảo); tab đang chọn in đậm,
  tab khác màu nhấn; số mũ trên tab là số việc đang chờ người.
- Viền dưới luôn hiện 4–6 phím quan trọng nhất **của ngữ cảnh hiện tại**; `?` mở bảng đầy đủ.
- Màn hình hẹp (< 100 cột): danh sách và chi tiết chuyển từ hai cột sang xếp chồng; tab rút
  gọn thành biểu tượng/chữ đầu. Dưới 60×20: hiện thông báo yêu cầu phóng to.

### 4.2 Hệ thống thiết kế (`internal/tui/theme`)

- Bảng màu theo vai trò, không theo màu: `fg`, `muted`, `accent` (xanh ngọc), `key`
  (vàng cam cho phím tắt), `ok`, `warn`, `err`, `border`. Có bộ sáng và tối, tự chọn theo
  nền terminal; tôn trọng `NO_COLOR` và chế độ 16 màu.
- Trạng thái tri thức có ký hiệu **và** màu (✓ verified, ◌ candidate, ⟳ stale,
  ✗ deprecated) để không phụ thuộc vào khả năng phân biệt màu.
- Chữ tiếng Việt: chuẩn hoá NFC trước khi đo độ rộng, kiểm thử căn cột với chuỗi có dấu.
- Logo chữ khối "bint." ở màn hình Tổng quan khi đủ chỗ, giống tinh thần ảnh tham khảo.

### 4.3 Nguyên tắc UX

1. **Hộp thư "Việc cần bạn"** là trang đầu: mục tri thức chờ duyệt, tác vụ `blocked`, đề
   xuất tiến hoá chờ áp dụng, ứng viên đánh giá, cảnh báo `doctor`. Enter nhảy thẳng tới chỗ
   xử lý.
2. **Bảng lệnh** (`:` hoặc Ctrl+K): tìm mờ trên mọi hành động của mọi màn hình, kể cả hành
   động hiếm dùng. Đây là bảo đảm "không tính năng nào bị giấu".
3. **Biểu mẫu thay cờ**: mỗi hành động có tham số mở form có nhãn, giá trị mặc định lấy từ
   cấu hình, kiểm tra ngay khi nhập, chọn ID/đường dẫn từ danh sách gợi ý.
4. **Xem trước rồi mới ghi**: thao tác thay đổi tệp (publish AGENTS.md, apply, rollback,
   sync, init) hiện diff trước, Enter để xác nhận. Thao tác không đảo ngược được cần gõ xác
   nhận.
5. **Hiện lệnh CLI tương đương** ở chân form (`bint knowledge approve 01M7…`), phím `y` để
   chép. Người dùng học dần CLI, và hành vi TUI luôn khớp CLI.
6. **Việc chạy lâu** (`run`, `eval run`, `evolve cycle`, `revalidate`) chạy nền: thanh tiến
   độ, ước tính chi phí trước khi chạy, xem log trực tiếp, Ctrl+C huỷ an toàn, thông báo khi
   xong dù đang ở màn hình khác.
7. **Chuột** (cuộn, bấm tab, bấm hàng) bật mặc định; mọi thứ vẫn dùng được chỉ bằng bàn phím.
8. **Phím nhất quán** giữa các màn hình: `j/k`/mũi tên di chuyển, `Enter` mở, `Esc`/`Bksp`
   quay lại, `/` lọc, `Tab` đổi khung, `r` làm mới, `e` sửa trong `$EDITOR`, `x` chọn nhiều.
9. **Lỗi có hướng xử lý**: lỗi hiện trong khung, kèm gợi ý (ví dụ "chưa `init` → nhấn i để
   khởi tạo"), không bao giờ thoát TUI vì một lỗi nghiệp vụ.

## 5. Các màn hình

### 5.1 Tổng quan
Trạng thái repository (`status`), hộp thư "Việc cần bạn", kết quả `doctor` rút gọn, lượt
chạy gần đây, chi phí tiến hoá/đánh giá trong kỳ. Lần đầu (chưa `init`): hiện **trình
hướng dẫn khởi tạo** thay cho bảng điều khiển.

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
- Thẻ phụ: Tìm kiếm (FTS), Thống kê, Thay đổi (`diff`), Thẩm định lại (`revalidate`),
  Xuất bản AGENTS.md (`bootstrap publish` với diff xem trước), Khảo sát (`inspect`,
  `bootstrap`), Ngữ cảnh (`context` theo mô tả hoặc đường dẫn: xem đúng gói agent sẽ nhận).

### 5.3 Tác vụ
Danh sách theo trạng thái; chi tiết theo thẻ con: Phiếu, Tài liệu (requirement/plan/tasks,
xem dạng Markdown), Tiêu chí, Kiểm chứng, Review (phát hiện theo mức độ, Enter tới tệp:dòng),
Lỗi, Lượt chạy. Hành động: tạo tác vụ, chạy workflow (chọn `b-do`/`b-req`/`b-feat`/`b-review`,
harness theo vai trò, `--no-review`), tiếp tục, duyệt (`task approve`, hiện lệnh kiểm chứng
và đường dẫn bảo vệ cần xác nhận), đổi trạng thái theo bảng chuyển, chuyển phát hiện review
thành tri thức.

### 5.4 Lượt chạy
Danh sách + chi tiết lượt chạy, chấm điểm (`rate`), quét Git (`scan`), hàng đợi sự kiện
(`events`), bài học từ thất bại (`experience show/digest`).

### 5.5 Đánh giá
Ứng viên (duyệt/từ chối/kiểm chứng fail→pass), tác vụ đã duyệt, đặc tả (`spec`), ước tính
chi phí, chạy so sánh có tiến độ theo ô (nhánh × lần lặp), báo cáo với biểu đồ cột tỷ lệ đạt
và khoảng tin cậy, dọn kho đệm.

### 5.6 Tiến hoá
Cơ hội (`scan`), đề xuất (danh sách, chi tiết với diff, kết quả Probe), đối chiếu, áp dụng,
bác bỏ, quay lui, lịch sử, hàng đợi deferred (`queue/process`), chạy `cycle` trong ngân sách.

### 5.7 Hệ thống
Cấu hình (xem theo nhóm, sửa trong `$EDITOR`, kiểm tra `Validate()` khi lưu), harness và
bảng khả năng, skill gốc/lớp phủ/skill tiến hoá (`list/show/overlay/diff/versions/check/regress`),
`sync`, `init` (form các tuỳ chọn hook, MCP, `--git track|local`), `doctor` đầy đủ,
chỉ mục (`index`).

## 6. Bảng phủ tính năng

| Nhóm CLI | Lệnh | Vị trí trong TUI |
|---|---|---|
| Dự án | `status`, `doctor` | Tổng quan; Hệ thống › Doctor |
| | `init`, `sync` | Trình hướng dẫn lần đầu; Hệ thống |
| | `harness list/show` | Hệ thống › Harness |
| Tri thức | `knowledge list/show/search/stats/diff/transitions` | Tri thức (danh sách, chi tiết, thẻ phụ; bảng chuyển trong `?`) |
| | `knowledge add/rm/verify` | Tri thức › Thêm / Xoá / Kiểm tra |
| | `knowledge approve/reject/deprecate/review` | Tri thức › Duyệt (từng mục, theo lô) |
| | `knowledge revalidate` | Tri thức › Thẩm định lại (việc nền) |
| | `bootstrap`, `bootstrap publish/skill`, `inspect` | Tri thức › Khảo sát / Xuất bản |
| | `context` | Tri thức › Ngữ cảnh |
| | `index` | Hệ thống › Chỉ mục |
| Workflow | `task new/list/show/set/status/write/criteria/error/review/lint/verify/approve/transitions` | Tác vụ |
| | `run`, `run resume/status` | Tác vụ › Chạy / Tiếp tục (việc nền) |
| | `skill *` | Hệ thống › Skill |
| Lượt chạy | `runs list/show/rate/scan`, `events` | Lượt chạy |
| Đánh giá | `eval mine/list/show/spec/verify/approve/reject/estimate/run/report/bootstrap/clean` | Đánh giá |
| Tiến hoá | `evolve scan/propose/list/show/evaluate/apply/reject/rollback/history/queue/process/cycle` | Tiến hoá |
| | `experience digest/show` | Lượt chạy › Bài học |
| Loại trừ | `mcp` (server stdio), `hook` (do harness gọi), `help`, lệnh ẩn | Không có màn hình; hiện trong Hệ thống › Harness dưới dạng trạng thái |

`task write/criteria/error/review` chủ yếu do agent gọi; TUI hiện dữ liệu và cho người sửa
khi cần, không ép người nhập JSON.

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
- **Sổ hành động** (`internal/tui/actions`): mỗi hành động khai báo tên, màn hình, phím,
  form tham số, hàm dịch vụ, lệnh CLI tương đương, và có phải thao tác chỉ dành cho người
  không. Bảng lệnh, viền dưới và trợ giúp đều sinh từ sổ này.
- **Việc nền**: goroutine với `context` huỷ được, đẩy sự kiện tiến độ qua kênh thành
  `tea.Msg`. Workflow và eval đã có ghi log/lượt chạy; TUI đọc lại thay vì tự giữ trạng thái.
- **Làm mới dữ liệu**: đọc lại khi chuyển màn hình, khi nhấn `r`, và theo chu kỳ ngắn cho
  màn hình đang có việc nền. Không giữ khoá SQLite lâu; thử lại khi `SQLITE_BUSY`.
- **Thao tác chỉ dành cho người**: kiểm tra `InAgentSession` nằm ở tầng dịch vụ. Nếu TUI bị
  mở trong phiên agent, các hành động này hiện khoá kèm lý do.
- **Đa ngôn ngữ** (đã chốt): xem mục 7.1.
- **Gói**: `internal/tui/{app,theme,components,screens/<tên>,actions}`. Thành phần dùng
  chung: khung có tiêu đề trên viền, thanh tab, chân phím, bảng, khung chi tiết cuộn được,
  form, hộp xác nhận, trình xem diff, trình xem Markdown, bảng lệnh, thông báo nổi.

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
  `Validate()` và dòng có chú thích trong mẫu `Render`.
- **Lỗi từ tầng dịch vụ**: lỗi đã biết (`ErrAgentForbidden`, lỗi kiểm tra tri thức, lỗi
  chuyển trạng thái…) mang mã ổn định để TUI dịch; lỗi chưa có mã hiện nguyên văn kèm nhãn
  "chi tiết kỹ thuật".
- **Bố cục chịu được độ dài khác nhau**: không đặt chiều rộng cố định theo chuỗi tiếng Việt;
  ảnh chụp kiểm thử chạy cho cả `vi` và `en`, và một locale giả kéo dài chuỗi ~40% để bắt lỗi
  tràn khung.
- **Kiểm thử danh mục**: mọi khoá có ở mọi locale, tham số khớp nhau; một test quét
  `internal/tui/**` báo chuỗi có dấu tiếng Việt viết thẳng trong mã (phải qua danh mục).

### 7.2 Chế độ chỉ đọc

- `bint tui --read-only` (hoặc `bint --read-only`): xem được mọi màn hình, mọi hành động ghi
  bị ẩn khỏi viền dưới, bảng lệnh và trợ giúp. Thanh trên hiện nhãn "CHỈ ĐỌC".
- Dựa trên cờ `Writes` của từng hành động trong sổ hành động; sổ là nơi duy nhất quyết định,
  màn hình không tự kiểm tra.
- Việc nền đang chạy (do phiên khác khởi động) vẫn xem được tiến độ và log, nhưng không huỷ được.
- Trình hướng dẫn `init` không mở ở chế độ này; repo chưa `init` thì hiện thông báo.
- Đây là chế độ tiện dụng (trình chiếu, máy dùng chung, người chỉ theo dõi), **không** phải
  ranh giới bảo mật: người có quyền ghi repo vẫn chạy CLI được.
- Kiểm thử: với mọi hành động có `Writes = true`, khẳng định không gọi được ở chế độ chỉ đọc,
  kể cả qua phím tắt và bảng lệnh.

## 8. Kiểm thử

- **Phủ tính năng**: một test duyệt cây lệnh cobra, lấy mọi lệnh lá và đòi mỗi lệnh có
  hành động trong sổ hoặc nằm trong danh sách loại trừ có lý do. Thêm lệnh CLI mà quên TUI
  thì test hỏng.
- **Ảnh chụp** (`teatest`, golden file) cho từng màn hình ở 80×24 và 140×40, giao diện sáng
  và tối, có chuỗi tiếng Việt dài.
- **Tương đương CLI**: với mỗi hành động ghi dữ liệu, chạy qua TUI và qua CLI trên hai repo
  tạm giống nhau rồi so trạng thái `.bint/` và SQLite.
- **Chỉ dành cho người**: đặt `CLAUDECODE=1`, khẳng định hành động bị khoá (dùng `asHuman(t)`
  cho chiều ngược lại, theo quy ước trong AGENTS.md).
- **Windows**: `GOOS=windows go vet ./...` và staticcheck cho `internal/tui/...`.

## 9. Giai đoạn

| GĐ | Nội dung | Xong khi |
|---|---|---|
| 0 | Tách tầng dịch vụ cho `knowledge review`, `eval`, `evolve` (cycle, apply, report) khỏi `internal/cli`; đưa kiểm tra chỉ-dành-cho-người xuống `app`. Khung TUI rỗng: theme, khung viền, tab, chân phím, bảng lệnh, sổ hành động, `internal/i18n` (vi, en) và khoá `ui.language`, test phủ (đang đỏ là đúng). | CLI chạy lại toàn bộ test không đổi hành vi; `bint` mở khung 7 tab, đổi `vi`/`en` được. |
| 1 | Tổng quan + hộp thư; Tri thức đầy đủ, gồm duyệt theo lô và lọc theo `nguon:*`. | Duyệt 30 mục từ một lần `/b-knowledge` trong < 1 phút, không gõ lệnh. |
| 2 | Tác vụ + chạy/tiếp tục workflow có log trực tiếp. | Từ TUI tạo tác vụ, chạy `b-do`, duyệt, xem review đến `done`. |
| 3 | Lượt chạy, Bài học, Tiến hoá. | Áp dụng và quay lui một đề xuất hoàn toàn trong TUI. |
| 4 | Đánh giá, có biểu đồ báo cáo. | Chạy một đợt nhỏ và đọc báo cáo trong TUI. |
| 5 | Hệ thống + trình hướng dẫn `init` lần đầu. | Repo mới: `bint` → hướng dẫn → khởi tạo xong không cần đọc tài liệu. |
| 6 | Hoàn thiện: chuột, chế độ hẹp, `NO_COLOR`, chế độ chỉ đọc, trợ giúp theo ngữ cảnh, README. | Test phủ xanh; ảnh chụp ổn định; thử với 2–3 người không quen CLI. |

Giai đoạn 1 đem lại giá trị lớn nhất (thay vòng `knowledge review` gõ phím từng mục) nên có
thể phát hành riêng trước khi xong các giai đoạn sau.

## 10. Rủi ro

- **Khối lượng**: ~80 lệnh lá. Sổ hành động + form sinh từ khai báo giúp tránh viết tay từng
  màn hình; giữ màn hình chuyên biệt cho những chỗ dùng nhiều (Tri thức, Tác vụ).
- **Tách tầng dịch vụ làm đổi hành vi CLI**: làm trước khi viết TUI, mỗi bước chạy toàn bộ
  test e2e hiện có.
- **Đồng thời**: hook, `bint run` và TUI cùng chạm dữ liệu. Không thêm khoá mới kiểu
  `evolve.AcquireLock`; nếu cần khoá liên tiến trình, theo `workflow.AcquireWriteLock`.
- **Thay đổi API thư viện TUI**: ghim phiên bản, bọc trong `internal/tui/components`.
- **Độ rộng ký tự** tiếng Việt và emoji khác nhau giữa các terminal: chuẩn hoá NFC, tránh
  emoji trong bố cục cột, kiểm thử trên Windows Terminal và một terminal Linux phổ biến.

## 11. Câu hỏi mở

Đã chốt (2026-11-11): `bint` không đối số mở TUI (mục 7); chuẩn bị đa ngôn ngữ từ giai
đoạn 0 (mục 7.1); hỗ trợ chế độ chỉ đọc (mục 7.2).

Không còn câu hỏi mở.
