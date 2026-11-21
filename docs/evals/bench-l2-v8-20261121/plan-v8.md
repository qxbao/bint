# Đợt 8: A / B / B′ trên bài mới L2 "chuyển thư mục dự án không mất gì" (kế hoạch, 2026-11-21)

Bản này là đăng ký trước. Mọi thay đổi sau khi bắt đầu chạy ghi vào `prereg-and-incidents.md` kèm giờ và lý do, không sửa ngược bản này.
Tệp này và mọi tệp của test ẩn **không** được chép vào repo bài hay repo Bint.

## 1. Vì sao đề mới, và khác gì đợt 7

- Đề L1 (bí mật trong mọi nơi Bint lưu) đã bẩn: commit `39a9b70` thêm quy tắc vào b-req/b-goal sau khi thấy kết quả test ẩn đợt 6, test ẩn đợt 7 trùng
  từng byte đợt 6, và toàn bộ phần tăng 20 → 30 ô của B nằm đúng ở hai loại AWS vừa bị phạt (`reports/Đánh giá đợt 7 luồng Bint.md`).
- Đợt 8 giữ nguyên khung đợt 7 (cùng repo bài gốc, cùng model, cùng kiểu người dùng giả low-tech, cùng cách chấm bằng ma trận tất định + N1 + N2)
  nhưng đổi bài sang một chủ đề không liên quan tới bí mật hay che dữ liệu, có cỡ và mức rủi ro tương đương.
- Thay đổi sản phẩm cần đo: các cải tiến B′ sau đợt 7 (đề bài nối tiếp mang khối B′, kiểm hẹp tự chạy khi cây đứng yên và có đường chỉ đọc khi sandbox
  chặn `.git`, cổng theo pha, tầng hẹp chỉ gói đổi, thăm dò chỉ thấy tiêu chí người dùng, review soát độ phủ thước đo, lưu tệp thăm dò, lát mỏng chấp nhận
  test đỏ đã giữ, ngân sách tổng, phát hiện agent treo, phân loại lần đỏ, không đánh `failed` lượt Codex đã `turn.completed`).

## 2. Bài L2

### 2.1 Lời yêu cầu (nguyên văn, dùng cho mọi nhánh)

```text
Tôi hay đổi tên hoặc chuyển thư mục dự án sang chỗ khác, mà mỗi lần như vậy bint quên sạch: tác vụ, lịch sử, mọi thứ. Sửa giúp tôi để chuyển
thư mục thoải mái mà không mất gì. Việc này chắc lớn nên cứ làm từ từ qua nhiều lần cũng được.
```

### 2.2 Thẻ người dùng (agent không thấy; người dùng giả trả lời theo thẻ này)

- Mục tiêu: đổi tên hay chuyển thư mục dự án (kể cả sang chỗ có dấu cách, tiếng Việt, đổi hoa/thường) thì mọi thứ bint đã có vẫn còn nguyên.
- Điều không muốn: bint chạy hỏng, chậm hẳn đi, hoặc trộn dữ liệu của hai dự án khác nhau.
- "Có cần chạy lệnh tay không?": không, tự nhận ra là được.
- "Nếu có hai bản sao cùng lúc?": đó là hai dự án riêng, đừng trộn.
- Hỏi về chia việc/làm tiếp: cứ làm tiếp chỗ đang dở, tôi không nhớ chi tiết lần trước.
- Câu kỹ thuật khác: "tôi không rành, bạn tự quyết cho an toàn."
- Trần 6 tin nhắn cả lượt (như đợt 7).

Câu trả lời cố định (`ANSWER`, dùng cho mọi câu hỏi):

```text
Mục tiêu của tôi: đổi tên hay chuyển thư mục dự án đi đâu (kể cả chỗ có dấu cách, tiếng Việt, hay chỉ đổi chữ hoa/thường) thì mọi thứ bint đã có vẫn
còn nguyên. Không cần chạy lệnh tay, bint tự nhận ra là được. Nếu có hai bản sao dự án cùng lúc thì đó là hai dự án riêng, đừng trộn. Tôi không muốn
bint chạy hỏng hay chậm hẳn đi. Còn câu kỹ thuật khác thì tôi không rành, bạn tự quyết cho an toàn.
```

Lời tự kiểm của A+ (`SELF_CHECK`, nếu chạy A+):

```text
Tự kiểm lại toàn bộ việc bạn đã làm so với yêu cầu ban đầu của tôi: «<PROMPT>» Rà mọi loại dữ liệu bint lưu cho một dự án xem loại nào còn mất khi
đổi tên hay chuyển thư mục, chạy test liên quan, và sửa mọi chỗ còn thiếu hoặc sai. Đừng làm lại phần đã đúng.
```

### 2.3 Vì sao bài này tương đương L1 về cỡ và rủi ro

| | L1 (đợt 6–7) | L2 (đợt 8) |
|---|---|---|
| Dạng | mối quan tâm cắt ngang mọi nơi Bint lưu | mối quan tâm cắt ngang mọi loại dữ liệu Bint lưu theo dự án |
| Lời người dùng | mơ hồ, low-tech, "mọi chỗ" | mơ hồ, low-tech, "mọi thứ" |
| Rủi ro | lộ bí mật (bảo mật) | mất dữ liệu hoặc trộn dữ liệu hai dự án (toàn vẹn dữ liệu) |
| Hai phía phải cân | che đủ / không che quá tay (N1) | giữ đủ / không gộp nhầm hai dự án (N1) |
| Hiệu năng | log lớn không chậm > 3× (N2) | lệnh thường không chậm > 3× khi có nhiều thư mục trạng thái (N2) |
| Ma trận | 7 loại × 6 nhóm nơi lưu (36 ô) | 9 loại dữ liệu × 4 kiểu chuyển (36 ô) |
| Cỡ diff đợt 7 | 32–87 tệp | ước lượng 20–80 tệp: định danh dự án ổn định, nhận ra repo đã chuyển, chuyển/ánh xạ thư mục trạng thái, đường dẫn tuyệt đối đã lưu (log, worktree, cwd của lượt chạy), xác nhận lệnh |

Không liên quan L1: không đụng tới bí mật, che, log nội dung; không có loại dữ liệu nào trùng tên loại bí mật; lời yêu cầu, thẻ người dùng và test ẩn viết mới.

### 2.4 Khoảng trống điểm đo bằng thăm dò trên binary gốc (`bin/bint-base.exe`, 2026-11-21)

- Sau khi đổi tên thư mục repo: `bint task list`, `bint status` trả "repository chưa được khởi tạo Bint trên máy này".
- Sau `bint init` lại ở chỗ mới: thư mục trạng thái mới (`moved-repo-<băm>`) tạo cạnh thư mục cũ; phiếu đã commit trong `.bint/tasks` được nhập lại,
  phiếu **chưa commit** không được nhập ("chỉ nhập bằng `bint task sync`"); `state.sqlite`, `runtime/` (log), `spool/`, `backups/`, `generated/`,
  `managed.json`, `writes.jsonl` nằm lại ở thư mục trạng thái cũ.
- Kết luận: phần lớn ô sẽ đỏ ở bản gốc, vài ô (phiếu đã commit) xanh sẵn, giống tỷ lệ ở L1. Con số chính xác lấy ở bước hiệu chuẩn (mục 4).

## 3. Test ẩn `hidden_l2.py`

Nguyên tắc chung (rút từ đợt 6–7):
- Chỉ đi qua lối vào công khai của `bint.exe` (CLI, `--json` khi có) và hệ tệp của repo; không đọc định dạng nội bộ của thư mục trạng thái, vì agent được
  phép đổi định dạng đó.
- So sánh bằng chuỗi đánh dấu duy nhất (canary) tìm trong đầu ra, không dựa vào tên trường JSON (agent có thể thêm trường); mỗi ô có canary riêng theo
  kiểu chuyển, để một kết quả quy được về đúng ô.
- Mỗi kiểu chuyển chạy trên một bản dựng fixture riêng, thư mục trạng thái riêng (`BINT_STATE_DIR`, `BINT_CONFIG_HOME`), TMP riêng, `GIT_CONFIG_NOSYSTEM=1`,
  xoá mọi biến môi trường agent (như `clean_env` của `hidden_l1.py`).
- Không in nội dung canary ra màn hình; kết quả ghi JSON.

### 3.1 Fixture (dựng ở vị trí gốc P0, bằng chính binary đang chấm)

Repo Go nhỏ tạo mới (một gói, một test), `git init`, commit, `bint init`. Rồi tạo bằng CLI, mỗi mục mang canary của kiểu chuyển:

| Mã | Loại dữ liệu | Cách tạo (CLI) | Cách kiểm sau khi chuyển |
|---|---|---|---|
| D1 | phiếu đã commit vào `.bint/tasks` | `bint task new "<canary D1>"`, rồi `git add .bint && git commit` | `bint task list --all --json` chứa ID và canary |
| D2 | phiếu **chưa** commit | `bint task new "<canary D2>"` (không commit) | như D1, không chạy `bint task sync` hay lệnh tay nào |
| D3 | log kiểm chứng của tác vụ | trên phiếu D1: `task set --verify-command 'echo <canary D3>'`, `task approve` (vai người), `task verify` | `bint task show <id> --json`: mọi đường dẫn log trong đầu ra tồn tại và một tệp log chứa canary D3 |
| D4 | xác nhận lệnh kiểm chứng của người | (đã tạo ở D3) | `bint task verify <id>` chạy được, không báo "chưa được người xác nhận", đầu ra chứa canary D3 |
| D5 | lượt chạy đã ghi nhận | `bint hook …` với bản ghi sự kiện giả lập của một phiên có canary D5 trong prompt/cwd, rồi `bint events ingest` | `bint runs list --json` (hoặc `runs show`) chứa lượt có canary D5 |
| D6 | phát hiện review còn mở | `bint task review <id> -` (JSON một phát hiện `high` tiêu đề canary D6) | `bint findings list --json` chứa canary D6, trạng thái mở |
| D7 | câu hỏi/lỗi đã ghi của tác vụ | `bint task error <id> --step b-feat --question "<canary D7>"` | `bint task show <id> --json` chứa canary D7 |
| D8 | tri thức chờ duyệt | `bint knowledge add --type warning --paths <tệp có thật> --evidence <tệp>:1 "<canary D8>"` (vai agent) | `bint knowledge list --all --json` chứa canary D8 |
| D9 | điểm xanh của tác vụ | sau D3 (verify xanh), `task show --json` có last green | `bint task show <id> --json` vẫn có điểm xanh trỏ cây cũ và `bint task restore-green <id>` (xem trước) chạy được |

Ghi chú khi viết test (phải kiểm trên binary gốc trước khi khoá đề):
- Cú pháp chính xác của `bint hook` để ghi sự kiện (D5) lấy theo cách `hidden_l1.py` nhóm S5 đã làm.
- Thao tác chỉ dành cho người (`task approve`) chạy với môi trường đã xoá biến agent; `knowledge add` của D8 chạy với `BINT_AGENT=1` để thành mục chờ
  duyệt.
- Mỗi lệnh có trần thời gian 120 s; lệnh treo tính ô đỏ kèm lý do `timeout`.

### 3.2 Bốn kiểu chuyển (cột ma trận)

| Mã | Kiểu chuyển | Thao tác |
|---|---|---|
| M1 | đổi tên cùng thư mục cha | `P0 = …/w/proj-<id>` → `…/w/proj-<id>-renamed` |
| M2 | chuyển sang cha khác, tên có dấu cách và tiếng Việt | → `…/khác/Dự án của tôi (bản mới) <id>` |
| M3 | chỉ đổi hoa/thường | `…/w/Proj<id>` → `…/w/proj<id>` (Windows không phân biệt hoa thường; bắt lỗi băm theo chuỗi đường dẫn) |
| M4 | chuyển hai lần liên tiếp, có chạy lệnh ở giữa | P0 → P1, chạy `bint task list` ở P1, rồi P1 → P2; kiểm ở P2 |

Chuyển bằng đổi tên thư mục của hệ điều hành (`os.rename`; M2 dùng `shutil.move` nếu khác ổ), không gọi lệnh bint nào trước khi chuyển. Sau khi chuyển,
lệnh đầu tiên test chạy ở vị trí mới là `bint status` rồi tới các lệnh kiểm; **không** chạy `bint init` hay lệnh tay nào (người dùng nói "tự nhận ra").

Ô (Dk, Mj) xanh khi kiểm của Dk ở mục 3.1 đạt sau kiểu chuyển Mj. Điểm chính = ô xanh / 36.

### 3.3 N1: không trộn, không làm hỏng (một cổng, đạt/không đạt)

Chạy trên fixture riêng; mọi mục phải đạt:
1. **Bản sao song song**: `git clone P0 P3` (P0 vẫn còn), rồi ở P3 chạy `bint init` và `bint task new "<canary N1-clone>"`. Ở P3 **không** được thấy D2–D9
   của P0 (chỉ phiếu đã commit, tức D1, được phép thấy qua Git); ở P0 không thấy phiếu `N1-clone`.
2. **Hai dự án khác nhau, cùng tên thư mục**: repo không liên quan ở `…/a/app` và `…/b/app`; dữ liệu không lẫn.
3. **Chuyển rồi chuyển về**: P0 → P1 → P0, đủ D1–D9.
4. **Lệnh thường vẫn chạy ở repo không chuyển**: `task new`, `task verify`, `findings list`, `runs list` thoát mã 0 và cho kết quả đúng.
5. **Không để repo chuyển dùng chung trạng thái với repo mới tạo ở đúng đường dẫn cũ**: sau M1, tạo repo mới (`git init` khác lịch sử) ở đúng đường dẫn
   P0 rồi `bint init`; repo mới không thấy D1–D9, repo đã chuyển vẫn thấy.

### 3.4 N2: không chậm hẳn đi

- Dựng 200 thư mục trạng thái giả của các repo khác trong `BINT_STATE_DIR` (bằng cách `bint init` trên 200 repo rỗng nhỏ, làm một lần, dùng chung giữa
  binary gốc và binary chấm), rồi đo trung vị 5 lần `bint task list` ở repo đã chuyển (M1) và ở repo không chuyển.
- Đạt khi thời gian ở repo đã chuyển ≤ 3× thời gian của binary gốc ở repo không chuyển trên cùng máy, đo cùng lượt chấm, máy rảnh (khoá chấm toàn máy
  của `grade.py`). Lần chuyển đầu (có thể có bước di chuyển dữ liệu) đo riêng, chỉ báo, không chấm.

### 3.5 Chỉ số phụ (như đợt 7)

Build được, `go test ./...` của cây cuối tách đỏ mới khỏi đỏ nền (`controls/gotest-baseline-<commit>.json`), thời gian tường, token vào/ra gồm cache,
cách kết thúc (tự xong / trần / dừng chờ người), mốc thời gian (dòng mã đầu tiên, kết quả kiểm chứng đầu tiên, review), số lần kiểm hẹp chạy được.

## 4. Hiệu chuẩn trước khi khoá đề (bắt buộc, làm trước lượt chạy đầu)

1. Chạy `hidden_l2.py` trên binary gốc 3 lần. Cổng đi tiếp: điểm chính ổn định (cả 3 lần bằng nhau) và **≤ 30 %** ô xanh; N1 đạt (bản gốc không trộn);
   N2 đạt. Nếu > 30 % ô xanh: bỏ loại dữ liệu xanh sẵn ở mọi kiểu chuyển, thay bằng loại khác cùng cỡ, ghi lý do.
2. **Kiểm tính đạt được của từng ô** (tránh ô không ai xanh được vì test sai): trên binary gốc, sau mỗi kiểu chuyển, làm tay một "lời giải giả" (đổi tên
   thư mục trạng thái cũ sang khoá mới mà bản gốc tính ra, rồi sửa đường dẫn tuyệt đối trong tệp trạng thái bằng script), chạy lại phần kiểm. Ô nào vẫn
   đỏ dưới lời giải giả thì xem lại cách kiểm của ô đó. Ghi kết quả vào `controls/l2-oracle.json`.
3. Chấm 3 lần liên tiếp cùng một cây để đo độ chập chờn của test ẩn; ô nào đổi kết quả giữa các lần thì sửa test trước khi khoá.
4. Ghi băm SHA-256 của `hidden_l2.py` và của tệp kiểm soát vào `prereg-and-incidents.md`; từ lúc đó không sửa.

## 5. Chống nhiễm đề

- Đề, thẻ người dùng, test ẩn và kế hoạch chỉ nằm trong `D:\bench\bench-v8-20261121\`, không nằm trong repo bài, repo Bint, skill, tri thức hay
  `docs/evals/` cho tới khi đợt 8 chấm xong.
- **Không** sửa skill/quy tắc của Bint theo bài L2 trước hay trong đợt này. Trước khi chạy, ghi commit của binary điều phối và chạy
  `git log <commit đợt 7>..<commit đợt 8> -- internal/skills internal/workflow/bprime*.go` để xác nhận không có thay đổi nhắc tới di chuyển repo, đường
  dẫn trạng thái hay định danh dự án.
- B′: **không** đặt `workflow.probe_catalog` (một danh mục viết cho bài này sẽ là rò đề). `workflow.probe_confirm: false` vì người dùng giả chỉ có một
  câu trả lời cố định.
- Sau đợt 8, khi sửa Bint theo kết quả, đợt 9 phải dùng bài khác hoặc test ẩn mới.

## 6. Giao thức chạy

### 6.1 Giữ như đợt 7
- Model Codex `gpt-6.2-sol`, effort high, `model_context_window` 872000, provider codex-lb. Mọi bước Bint dùng cùng model.
- Repo bài: bint ở 48dbfad. A: repo đã gỡ Bint (4db2657). B/B′: 48dbfad + skill/hook theo binary điều phối (`tmpl-B`).
- Môi trường sạch riêng mỗi lượt (bản sao repo, CODEX_HOME, thư mục trạng thái, TMP). Tác vụ B/B′ chạy `--shared`.
- Người dùng giả tự động theo bảng quyết định của `run.py` (cổng kế hoạch lớn → resume; câu hỏi → `ANSWER`; lát giữ → **`--slice more`** (đổi so với
  đợt 7, vì `more` giờ là mặc định và đứng đầu); hết vòng sửa / kiểm chứng cuối đỏ → dừng với cây hiện tại; tối đa 6 thao tác).

### 6.2 Sửa so với đợt 7 (từ `reports/Đánh giá đợt 7 luồng Bint.md`)
1. **Mẫu `tmpl-B` đỏ sẵn hai test đồng bộ skill.** Trước khi chạy: dựng lại `tmpl-B` bằng `bint sync` của chính binary điều phối, rồi chạy
   `grade.py <lượt> --baseline` trên `tmpl-A` và `tmpl-B` để có tập test đỏ nền; hai tập phải giống nhau ngoài test của chính Bint. Ghi vào
   `controls/`.
2. **Chấm**: dùng `grade.py` đã sửa (khoá chấm toàn máy, N2 không ghi đè, tách đỏ mới/đỏ nền). Không chấm song song với lượt đang chạy.
3. **Không gắn debugger hay công cụ nào vào tiến trình đang chạy.** Muốn xem tiến trình còn sống: đọc log, `tasklist`, không đụng tiến trình.
4. **Tải máy**: tối đa 2 lượt chạy song song; ghi số lượt đang chạy vào log điều phối mỗi phút.
5. **Chi phí**: ghi token vào gồm cache, token cache riêng, và chi phí quy đổi khi có bảng giá (`cost_usd` của đợt 7 rỗng).
6. **Chấm trung gian**: chụp và chấm cây ở các mốc 60/120/180 phút (không dừng lượt), để có đường điểm theo thời gian.

### 6.3 Nhánh và số lượt
- Nhánh: A, B (classic), B′. Khuyến nghị thêm A+ (prompt trực tiếp được tiêu tới trung vị token của B′) và best-of-3 prompt trực tiếp có chọn bằng test
  của repo, vì không có đối chứng ngân sách thì "B/B′ hơn A" không tách được khỏi "tiêu nhiều hơn".
- Số lượt: **ít nhất 3 lượt mỗi nhánh**. Một lượt mỗi nhánh (như đợt 7) chỉ cho tín hiệu định hướng.
- Trần 180 phút tường mỗi lượt. B′ đặt `workflow.budget_minutes: 170` (giữ mặc định 25 phút cho review và một vòng sửa) để đo đúng tính năng ngân
  sách tổng; B classic không có khoá này.

### 6.4 Cấu hình `.bint/local.yml` cho B′ (ghi lại nguyên văn khi chạy)

```yaml
workflow:
  variant: bprime
  budget_minutes: 170
  verify:
    scope: affected
    affected_command: "go test -count=1 {test_dirs}"
  # probe_catalog: không đặt (mục 5)
  # probe_confirm: false (mục 5)
```

Các khoá còn lại như đợt 7. Ghi `bint --version` và commit vào `log/bint-commit.txt`.

## 7. Tiêu chí dừng (đăng ký trước, cho tầng L2)

Với n ≥ 3 mỗi nhánh, hiệu trung bình điểm chính, khoảng tin cậy 95 % theo t (Welch):
1. B không hơn A (hoặc A+ nếu có) ít nhất 10 điểm % → bằng chứng bỏ luồng classic làm mặc định ở tầng này.
2. B′ hơn A (hoặc A+) ≥ 10 điểm % với token vào ≤ 2,5× A → giữ B′ cho việc cỡ L2.
3. B′ không hơn A (hoặc A+) → ở tầng L2 không có lợi thế cấu trúc.
4. Riêng cải tiến sau đợt 7: B′ phải có (a) kết quả kiểm chứng đầu tiên trước 25 % ngân sách ở ≥ 2/3 lượt, (b) ≥ 1 lần kiểm hẹp chạy được mỗi lượt,
   (c) tới được review ở ≥ 2/3 lượt. Không đạt thì cải tiến chưa có tác dụng như thiết kế, bất kể điểm chính.

Với n = 3, chỉ chênh lệch rất lớn mới có CI không chứa 0; kết quả không đạt mức này ghi là "chưa kết luận", không ghi là "không khác".

## 8. Việc chuẩn bị (theo thứ tự)

1. Tạo `D:\bench\bench-v8-20261121\` bằng cách chép `tools/`, `bin/`, `codex-home-tmpl/`, `tmpl-A/`, `tmpl-B/` từ đợt 7 (không chép `hidden/`, `controls/`,
   `runs/`, `log/`).
2. Build binary điều phối từ `main` hiện tại vào `bin/bint.exe`; giữ `bin/bint-base.exe` (bản gốc 48dbfad) cho hiệu chuẩn và N2.
3. Dựng lại `tmpl-B` (mục 6.2.1), chạy `--baseline` cho hai mẫu.
4. Viết `hidden/hidden_l2.py` theo mục 3; viết `log/persona-card.md` theo mục 2.2; sửa `tools/run.py`: `PROMPT`, `ANSWER`, `SELF_CHECK`, `--slice more`,
   chấm trung gian, đếm lượt song song; sửa `tools/grade.py`: gọi `hidden_l2.py`, bỏ N2 kiểu log của L1.
5. Hiệu chuẩn (mục 4); ghi băm và khoá đề.
6. Lượt khói: một lượt B′ ngắn (trần 20 phút) để xác nhận cấu hình, kiểm hẹp tự chạy, và người dùng giả chạy đúng; không tính vào kết quả.
7. Chạy chính: A, B, B′ × 3 (thêm A+ nếu chọn), tối đa 2 lượt song song.

## 9. Đề đã cân nhắc và loại

| Đề | Lý do loại |
|---|---|
| Đường dẫn có dấu cách, tiếng Việt, ký tự đặc biệt chạy được ở mọi lệnh | Thăm dò trên binary gốc: `task new`, `approve`, `verify`, `context` đã chạy đúng; chỉ `knowledge add` hỏng (khớp phạm vi). Khoảng trống điểm quá nhỏ. Một phần ý được giữ trong kiểu chuyển M2. |
| Không xoá/ghi đè mất việc của người dùng | Bản gốc đã có nhiều lớp bảo vệ (`bint sync` báo xung đột khi skill bị sửa, `restore-green` mặc định xem trước và bỏ qua tệp không thuộc tác vụ, `worktree_guard`, `repowrite`); khoảng trống điểm không chắc. |
| Tự dọn dữ liệu cũ cho đỡ tốn ổ đĩa | Test ẩn phải giả "cũ" qua mtime, thời điểm trong `state.json` và bản ghi SQLite; lời giải hợp lệ có thể dựa vào nguồn thời gian khác, nên chấm không công bằng. |
| Giao diện tiếng Anh | Rủi ro thấp hơn hẳn L1 và cỡ lớn hơn nhiều (hàng nghìn chuỗi). |
| Che dữ liệu cá nhân (email, số điện thoại) | Quá gần L1, thừa hưởng quy tắc đã chỉnh theo L1. |
