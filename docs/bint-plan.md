# BINT — KẾ HOẠCH THIẾT KẾ VÀ TRIỂN KHAI

**Tên:** Bao Intelligence (Bint)  
**Trạng thái:** Đề xuất kiến trúc — bản sửa đổi 2  
**Phạm vi:** CLI, repository intelligence, agent skills, orchestration, memory và self-evolution.

### Thay đổi chính so với bản trước

1. Đưa khung đánh giá lên giai đoạn đầu, thêm các cổng quyết định với tiêu chí dừng được ấn định trước (mục 1, 2, 12, 16).
2. Khai thác tác vụ đánh giá từ lịch sử Git của repository, có cơ chế chống rò rỉ đáp án (mục 12).
3. Thu hẹp bootstrap: ưu tiên quy tắc bất thường và điểm dễ sai, không sinh tổng quan kiến trúc mặc định (mục 4, 8).
4. Ghi nhận lượt chạy ngay từ P0; bổ sung nhãn kết quả, quy công và điều kiện tiên quyết về dữ liệu cho tiến hoá (mục 9, 10, 16).
5. Trạng thái cục bộ mặc định nằm ngoài repository, dùng chung giữa các worktree; hook ghi qua hàng đợi tệp (mục 3, 9, 13).
6. Tệp trong Git là nguồn chuẩn duy nhất của tri thức, SQLite chỉ là chỉ mục dẫn xuất; bổ sung quy tắc làm việc nhóm (mục 2, 8).
7. Thư mục gốc chỉ còn AGENTS.md và CLAUDE.md; tài liệu chi tiết là bản sinh từ tri thức (mục 3, 8).
8. Context Engine có giao diện MCP và cơ chế chèn ngữ cảnh theo đường dẫn tệp (mục 2, 7, 8, 13).
9. Fingerprint theo vùng ký hiệu thay vì cả tệp; xác thực lại theo mức rủi ro (mục 8).
10. Skill gốc bất biến, tuỳ biến theo dự án qua lớp phủ (mục 3, 6, 7).
11. Lưu nguồn gốc tri thức, chống đầu độc bộ nhớ; quy tắc bắt buộc chỉ do người duyệt (mục 8, 15).
12. Gắn mọi cải tiến với phiên bản mô hình; bổ sung phần khác biệt so với năng lực sẵn có của harness (mục 1, 12).

---

## 1. Tầm nhìn sản phẩm

Bint là một hệ thống hỗ trợ tác nhân lập trình có khả năng tìm hiểu, ghi nhớ, vận dụng và liên tục cải tiến phương pháp làm việc trên từng dự án phần mềm.

Thay vì mỗi phiên tác nhân phải khám phá lại cấu trúc dự án, Bint xây dựng một lớp tri thức có cấu trúc, được xác thực và cập nhật theo lịch sử phát triển của mã nguồn.

Bint hướng tới bốn năng lực:

1. **Understand:** Khám phá kiến trúc, nghiệp vụ, quy ước và các đặc thù của repository.
2. **Execute:** Hỗ trợ lập kế hoạch, thực thi, đánh giá thay đổi và phối hợp nhiều tác nhân.
3. **Remember:** Lưu lại kinh nghiệm có giá trị, truy xuất đúng ngữ cảnh khi gặp công việc tương ứng.
4. **Evolve:** Tự đề xuất, thử nghiệm, đánh giá và áp dụng các cải tiến đối với skill, tri thức và chiến lược điều phối.

### Giả thuyết cốt lõi và tiêu chí dừng

Toàn bộ Bint dựa trên một giả thuyết chưa được kiểm chứng:

> Một lớp tri thức có cấu trúc, được xác thực và truy xuất đúng lúc giúp agent hoàn thành đúng tác vụ với chi phí thấp hơn so với việc để agent tự khám phá repository.

Giả thuyết này đã có bằng chứng phản bác một phần. Nghiên cứu *Evaluating AGENTS.md: Are Repository-Level Context Files Helpful for Coding Agents?* (Gloaguen và cộng sự, ETH Zurich, 2026 — https://arxiv.org/abs/2602.11988) ghi nhận:

- Tệp ngữ cảnh do mô hình sinh ra làm giảm nhẹ tỷ lệ thành công; tệp do lập trình viên viết chỉ cải thiện ít.
- Tệp ngữ cảnh làm tăng chi phí suy luận trung bình hơn 20%.
- Phần tổng quan kiến trúc không giúp agent tìm tới tệp liên quan nhanh hơn.
- Agent tuân thủ tốt các chỉ dẫn về quy ước bất thường.

Hệ quả cho thiết kế:

- Khung đánh giá có/không có Bint phải tồn tại trước khi xây các lớp tri thức (xem mục 12 và 16).
- Tri thức được ưu tiên theo thứ tự: quy tắc bắt buộc bất thường → điểm dễ sai → mẫu triển khai đặc thù → tổng quan.
- Mỗi giai đoạn lớn kết thúc bằng một cổng quyết định với tiêu chí được ấn định **trước khi** chạy đo. Nếu không đạt, thu hẹp phạm vi hoặc đổi hướng thay vì tiếp tục xây chồng.

### Khác biệt so với năng lực sẵn có của harness

Các harness đã và sẽ tiếp tục bổ sung skill, hook, subagent, bộ nhớ, chế độ lập kế hoạch và lệnh khởi tạo tài liệu. Bint không cạnh tranh ở những năng lực này.

Phần Bint đóng góp, và khó bị harness thay thế:

- **Đo lường theo từng repository:** biết một thay đổi về tri thức, skill hay quy trình có thực sự giúp ích trên chính dự án đó hay không.
- **Cải tiến có kiểm chứng:** chỉ áp dụng thay đổi khi có bằng chứng từ tập đánh giá độc lập.
- **Độc lập harness:** cùng một nguồn tri thức và cùng một khung đánh giá cho nhiều harness.
- **Nguồn gốc và khả năng kiểm toán:** mỗi mục tri thức truy được tới bằng chứng và người duyệt.

Những phần trùng với năng lực sẵn có (workflow lập kế hoạch, review) chỉ được xây ở mức tối thiểu cần cho việc đo lường và điều phối.

### Nguyên tắc thiết kế

- Đo trước khi xây: mỗi lớp tính năng chỉ được mở rộng khi lớp trước đã có số liệu cho thấy nó có ích.
- Tri thức ưu tiên những điều bất thường của dự án, không tóm tắt những gì agent tự khám phá được.
- Độc lập với nhà cung cấp mô hình và harness.
- Ưu tiên xử lý cục bộ, không yêu cầu dịch vụ đám mây trung tâm.
- Dùng tri thức được xác thực làm nguồn chuẩn, không xem mọi kết luận của agent là sự thật.
- Cải tiến dựa trên bằng chứng, không dựa đơn thuần vào số lượt sử dụng.
- Tối ưu chi phí hoàn thành đúng tác vụ, không tối ưu số token bằng mọi giá.
- Bảo toàn khả năng kiểm tra, quay lui và tái lập.
- Hạn chế tối đa việc làm xáo trộn repository của người dùng.

### Ngoài phạm vi phiên bản đầu

- Huấn luyện hoặc cập nhật trọng số mô hình.
- Dịch vụ lưu trữ tập trung và đồng bộ đa thiết bị.
- Bảng điều khiển web.
- Tự động triển khai thay đổi lên môi trường sản xuất.
- Hỗ trợ toàn diện mọi harness ngay từ phiên bản đầu.
- Tự sửa chính sách bảo mật và tiêu chí đánh giá của hệ thống.

---

## 2. Kiến trúc tổng thể

Bint gồm bảy phân hệ chính.

### 2.1. Bint Core

Chương trình CLI viết bằng Go, chịu trách nhiệm quản lý dự án, cấu hình, cơ sở dữ liệu, các thao tác hệ thống và điều phối các phân hệ khác.

Core không phụ thuộc vào mô hình ngôn ngữ cụ thể. Những thao tác mang tính xác định như đọc cấu hình, phân tích Git, quản lý SQLite và kiểm tra trạng thái phải thực hiện được mà không cần gọi mô hình.

### 2.2. Repository Intelligence

Khám phá repository và xây dựng hệ thống tri thức:

- Nhận diện ngôn ngữ, framework và công cụ xây dựng.
- Phát hiện các module, thư mục chức năng và điểm vào chương trình.
- Phân tích quan hệ phụ thuộc ở mức phù hợp với từng ngôn ngữ.
- Trích xuất quy ước từ mã nguồn, tệp cấu hình và tài liệu hiện có.
- Ghi nhận khái niệm nghiệp vụ, quyết định kiến trúc và những trường hợp đặc biệt.
- Theo dõi thay đổi mã nguồn để xác định tri thức cần kiểm tra lại.

### 2.3. Context Engine

Nhận mô tả tác vụ, xác định các nhóm ngữ cảnh liên quan và tạo gói ngữ cảnh ngắn gọn.

Đầu ra bao gồm:

- Tóm tắt phần mã nguồn liên quan.
- Quy tắc bắt buộc.
- Các mẫu triển khai đã được xác thực.
- Skill có thể áp dụng.
- Cảnh báo về những kinh nghiệm lỗi thời hoặc chưa chắc chắn.
- Tham chiếu tới tệp và vị trí mã nguồn để agent tự kiểm tra khi cần.

Context Engine được cung cấp qua ba kênh:

- **MCP server** (`bint mcp serve`): kênh chính, trung lập với harness, cho phép agent gọi công cụ có kiểu dữ liệu rõ ràng.
- **Hook theo đường dẫn:** trước khi agent đọc hoặc sửa một tệp, hook tự chèn các quy tắc gắn với đường dẫn đó, không phụ thuộc vào việc agent nhớ gọi Context Engine.
- **CLI** (`bint context`): dự phòng cho harness không hỗ trợ MCP hoặc hook.

Đầu ra kèm ID của từng mục tri thức để agent trích dẫn khi áp dụng (phục vụ quy công, xem mục 9.7).

### 2.4. Workflow Orchestrator

Điều phối các quy trình b-req, b-feat, b-review và b-do.

Phân công theo role, chọn harness, quản lý trạng thái công việc, giới hạn quyền thực thi và truyền kết quả giữa các bước.

### 2.5. Experience Store

Quản lý lịch sử sử dụng, bằng chứng thực thi, kết quả kiểm chứng, tri thức và các phiên bản skill.

Phân chia trách nhiệm:

- **Tệp Markdown/YAML trong Git** là nguồn chuẩn duy nhất của tri thức, skill và bằng chứng tối thiểu đi kèm.
- **SQLite** lưu lịch sử lượt chạy, sự kiện, kết quả đánh giá và một chỉ mục dẫn xuất từ tệp tri thức. Chỉ mục có thể xoá và dựng lại bất cứ lúc nào (`bint index rebuild`) mà không mất thông tin.

SQLite nằm ngoài repository theo mặc định (xem mục 3.1).

### 2.6. Evolution Engine

Phát hiện cơ hội cải tiến, tạo phương án, chạy đánh giá đối chiếu và quản lý vòng đời các cải tiến.

Phân hệ này không được tự sửa đổi các chính sách bảo vệ cốt lõi mà không có sự chấp thuận phù hợp.

### 2.7. Evaluation Harness

Khung đánh giá độc lập với Evolution Engine, được xây từ giai đoạn đầu:

- Khai thác tác vụ đánh giá từ lịch sử Git của repository.
- Dựng trạng thái mã nguồn và tri thức tại thời điểm của từng tác vụ để tránh rò rỉ đáp án.
- Chạy cùng tác vụ trên nhiều nhánh so sánh (không ngữ cảnh, AGENTS.md tối giản, Bint đầy đủ).
- Lặp lại nhiều lần để ước lượng độ dao động, báo cáo kết quả kèm khoảng tin cậy.
- Ước tính chi phí trước khi chạy.

Evolution Engine dùng khung này nhưng không được sửa nó (xem mục 15.4).

---

## 3. Cấu trúc repository

Cấu trúc dự kiến sau khi chạy `bint init`:

```text
repository/
├── .bint/
│   ├── config.yml
│   ├── knowledge/
│   │   └── items/
│   │       └── <ulid>.md          # mỗi mục tri thức một tệp
│   ├── skills/
│   │   ├── core.lock              # phiên bản skill gốc đang dùng
│   │   ├── overlays/              # tuỳ biến theo dự án cho skill gốc
│   │   │   ├── b-req.md
│   │   │   ├── b-feat.md
│   │   │   ├── b-review.md
│   │   │   └── b-do.md
│   │   └── evolved/               # skill chuyên biệt do tiến hoá tạo ra
│   └── evals/
│       ├── cases/                 # tác vụ đánh giá đã duyệt
│       └── manifests/
│
├── .agents/skills/
├── .claude/skills/
├── AGENTS.md
└── CLAUDE.md
```

Các thư mục adapter chỉ được tạo khi harness tương ứng được kích hoạt.

Skill gốc (b-bootstrap, b-req, b-feat, b-review, b-do) đi kèm tệp thực thi Bint và không được sửa trực tiếp trong repository. `core.lock` ghi phiên bản đang dùng; phần tuỳ biến nằm trong `overlays/` (xem mục 6.6).

Không tạo mặc định CODEBASE.md, DOMAIN.md, PATTERNS.md, QUIRKS.md ở thư mục gốc. Khi cần tài liệu cho người đọc, `bint docs render` sinh chúng vào thư mục trạng thái hoặc một đường dẫn do người dùng chỉ định (xem mục 8.5).

### 3.1. Thư mục trạng thái cục bộ

Trạng thái không chia sẻ nằm **ngoài repository theo mặc định**:

```text
${XDG_STATE_HOME:-~/.local/state}/bint/<repo-id>/
├── state.sqlite
├── spool/          # hàng đợi sự kiện từ hook
├── runtime/        # tác vụ đang chạy, tệp tạm
├── eval-work/      # worktree và bản dựng phục vụ đánh giá
├── generated/
└── backups/
```

`<repo-id>` được tính từ đường dẫn tuyệt đối của `git rev-parse --git-common-dir`. Nhờ vậy mọi worktree của cùng một repository dùng chung một thư mục trạng thái; agent chạy song song trong worktree khác vẫn ghi về cùng một nơi.

Trên hệ điều hành không theo chuẩn XDG, dùng thư mục dữ liệu ứng dụng tương đương. Người dùng có thể ghi đè bằng biến môi trường `BINT_STATE_DIR` hoặc cấu hình cục bộ.

### Chính sách quản lý bằng Git

**Có thể commit:**

- `.bint/config.yml` với những giá trị không nhạy cảm.
- `.bint/knowledge/`.
- `.bint/skills/` (khoá phiên bản, lớp phủ, skill tiến hoá).
- `.bint/evals/cases/` khi tập đánh giá không chứa dữ liệu nhạy cảm.
- AGENTS.md, CLAUDE.md.
- Các skill được xuất bản cho harness.

**Không commit:**

- Mọi thứ trong thư mục trạng thái cục bộ (vốn đã nằm ngoài repository).
- Thông tin xác thực, token và bí mật.
- Bản sao dữ liệu nhạy cảm từ các lần chạy.

Chỉ mục tri thức không được commit vì sẽ gây xung đột khi nhiều người cùng sửa; nó được dựng lại từ các tệp trong `knowledge/items/`.

---

## 4. Khởi tạo dự án

### 4.1. Quy trình khởi tạo

Lệnh:

`bint init`

Các bước:

1. Xác định thư mục gốc Git.
2. Kiểm tra Bint đã được khởi tạo hay chưa.
3. Nhận diện những harness đang được cài đặt.
4. Khởi tạo `.bint`, cấu hình mặc định và thư mục trạng thái cục bộ (gồm SQLite) bên ngoài repository.
5. Cài đặt skill `b-bootstrap` cho các harness được chọn.
6. Cài hook ghi nhận lượt chạy nếu harness hỗ trợ và người dùng đồng ý, để bắt đầu tích luỹ dữ liệu ngay.
7. Cập nhật `.gitignore` theo nguyên tắc không xoá cấu hình hiện có.
8. Phát hiện lệnh kiểm thử và lệnh dựng môi trường, ghi vào cấu hình dưới dạng đề xuất chờ xác nhận (không chạy).
9. Thông báo các bước tiếp theo: khai thác tập đánh giá (`bint eval mine`) và bootstrap.

`bint init` không nên tự ý sử dụng mô hình, cài đặt phần mềm bổ sung hoặc chạy lệnh có tác dụng phụ trên dự án.

### 4.2. Bootstrap — khám phá repository bằng agent

Sau `init`, người dùng chạy `b-bootstrap` từ harness mong muốn, hoặc sử dụng cơ chế tương đương do Bint hỗ trợ.

Quy trình:

**Bước 1 — Static Discovery**

Bint Core thu thập dữ liệu xác định được bằng chương trình:

- Cây thư mục.
- Git metadata.
- Manifest và lockfile.
- Danh sách dependency.
- Cấu hình kiểm thử và xây dựng.
- Thống kê ngôn ngữ và tệp.
- Các tài liệu đang tồn tại.
- Lệnh kiểm thử và lệnh dựng môi trường.

**Bước 2 — Agent Investigation**

Agent phân tích cấu trúc, kiến trúc, luồng dữ liệu, phân chia miền nghiệp vụ, quy ước triển khai và các ràng buộc.

Agent phải ghi nguồn bằng chứng và đánh dấu thông tin chưa chắc chắn.

**Bước 3 — Knowledge Generation**

Sinh các mục tri thức ứng viên trong `.bint/knowledge/items/`, theo thứ tự ưu tiên:

1. **Quy tắc bất thường:** những quy ước khác với thông lệ của ngôn ngữ hoặc framework mà agent không thể tự đoán (lệnh build đặc biệt, thư mục không được sửa, cách đặt tên riêng).
2. **Điểm dễ sai:** lỗi đặc thù, điều kiện đặc biệt, những chỗ đã từng gây hồi quy — chỉ khi có bằng chứng.
3. **Mẫu triển khai đặc thù:** cách dự án làm một việc lặp lại, khác với cách phổ biến.
4. **Thuật ngữ nghiệp vụ:** khái niệm mà tên trong mã nguồn không tự giải thích.

Không sinh tổng quan kiến trúc hay mô tả cấu trúc thư mục như tri thức mặc định: agent tự khám phá được, và bằng chứng hiện có cho thấy nội dung này không giúp ích mà còn làm tăng chi phí.

Mọi mục do bootstrap sinh ra đều ở trạng thái `candidate`. Các mục loại `rule` cần người duyệt trước khi được chèn như quy tắc bắt buộc (xem mục 8.1).

Tài liệu cấp repository:

- `AGENTS.md`: ngắn, gồm chỉ dẫn dùng Bint và một số ít quy tắc bắt buộc quan trọng nhất. Bint chỉ quản lý phần nằm giữa hai dấu `<!-- bint:begin -->` và `<!-- bint:end -->`; nội dung người dùng bên ngoài không bị động tới.
- `CLAUDE.md`: chỉ nhập lại AGENTS.md (`@AGENTS.md`) cùng phần đặc thù Claude Code nếu thực sự cần, khi harness này được chọn.

**Bước 4 — Skill Adaptation**

Ghi phần điều chỉnh theo dự án vào lớp phủ (`.bint/skills/overlays/`), không sửa skill gốc.

Skill chỉ tham chiếu tới Context Engine và những quy tắc cần thiết, không sao chép tri thức dự án vào nội dung skill.

**Bước 5 — Validation**

Kiểm tra xác định được bằng chương trình: đường dẫn và ký hiệu được tham chiếu có tồn tại, ví dụ mã nguồn khớp với mã thật, không xung đột với tài liệu sẵn có, không có mục trùng lặp.

**Bước 6 — Publication**

Tạo bản thay đổi có thể xem xét trước khi ghi vào các tài liệu do người dùng quản lý.

**Bước 7 — Đo**

Nếu tập đánh giá đã sẵn sàng, chạy so sánh trước và sau bootstrap (xem mục 12). Kết quả quyết định giữ, thu hẹp hay loại bỏ các mục vừa sinh.

### 4.3. Khởi tạo lại

`bint init` phải có tính lặp an toàn.

Cần phân biệt:

- Khởi tạo mới.
- Đồng bộ adapter.
- Tái khảo sát repository.
- Cập nhật tài liệu.
- Khôi phục trạng thái.

Không được mặc định ghi đè những tài liệu người dùng đã chỉnh sửa.

---

## 5. Hệ thống cấu hình

Cấu hình trung tâm: `.bint/config.yml`.

Ví dụ đề xuất:

```yaml
version: 1

project:
  name: example
  root: auto

state:
  dir: auto                 # mặc định: ${XDG_STATE_HOME}/bint/<repo-id>

harnesses:
  enabled:
    - claude
    - codex
    - antigravity
  default: claude

roles:
  planner: claude
  implementer: codex
  reviewer: claude
  evolver: claude

context:
  max_tokens: 6000
  interfaces:
    mcp: true
    path_hooks: true
    cli: true
  retrieval:
    lexical: true
    structural: true
    semantic: false
  freshness:
    verify_changed_sources: true
    fingerprint: symbol     # symbol | hunk | file

knowledge:
  require_human_approval_for: [rule]

memory:
  enabled: true
  scope: project
  retention_days: 90

eval:
  test_command: null        # phát hiện khi init, người dùng xác nhận
  setup_command: null
  repeats: 3
  arms: [none, minimal, bint]
  leakage_mode: strict      # strict | economy
  budget_usd: null          # bắt buộc đặt trước khi chạy tự động

evolution:
  enabled: true
  mode: propose
  trigger:
    min_similar_runs: 3
    min_new_evidence: 2
  evaluation:
    enabled: true
    max_candidates: 2
  adoption:
    auto_low_risk: false
    bind_to_model_version: true

security:
  allow_external_agents: false
  allow_network_in_evals: false
  require_approval_for_commands: true
```

Đây là cấu hình mục tiêu, không phải đặc tả đã được triển khai.

### Thứ tự ưu tiên

1. Yêu cầu cụ thể của người dùng, trong phạm vi chính sách cho phép.
2. Chính sách bảo mật và giới hạn bắt buộc.
3. Cấu hình dự án.
4. Mặc định của Bint.
5. Cơ chế dự phòng nếu harness không khả dụng.

Yêu cầu của người dùng được quyền ghi đè lựa chọn role/harness, nhưng không được tự động vượt qua giới hạn bảo mật.

Cần hỗ trợ cấu hình toàn cục cho những giá trị dùng chung và cấu hình riêng ở máy cá nhân.

---

## 6. Hệ thống skill gốc

Các workflow dưới đây trùng một phần với năng lực sẵn có của harness (chế độ lập kế hoạch, lệnh review, subagent). Ở các giai đoạn đầu, chúng chỉ được xây ở mức tối thiểu cần cho việc ghi nhận kết quả và điều phối; phần đầu tư chính dành cho đo lường và tri thức.

### 6.1. b-req — Phân tích và lập kế hoạch

**Đầu vào:** Mô tả tính năng, yêu cầu thay đổi, tài liệu hoặc ticket.

**Quy trình:**

1. Phân tích mục tiêu và phạm vi.
2. Truy xuất kiến thức dự án.
3. Xác định các khu vực mã nguồn liên quan.
4. Phát hiện các yêu cầu còn thiếu hoặc mâu thuẫn.
5. Lập yêu cầu chức năng và tiêu chí chấp nhận.
6. Thiết kế hướng triển khai.
7. Phân rã đầu việc theo thứ tự phụ thuộc.
8. Xác định rủi ro và chiến lược kiểm thử.

**Đầu ra mặc định:**

```text
.bint/runtime/tasks/<task-id>/
├── requirement.md
├── plan.md
└── tasks.md
```

Có thể xuất các tài liệu sang `docs/spec/<feature>/` theo cấu hình dự án.

Yêu cầu phải gắn với tiêu chí chấp nhận; mỗi đầu việc phải có điều kiện hoàn thành rõ ràng.

### 6.2. b-feat — Thực thi kế hoạch

**Đầu vào:** Một kế hoạch được chấp nhận hoặc yêu cầu triển khai trực tiếp.

**Quy trình:**

1. Đọc đầu vào và trạng thái công việc.
2. Kiểm tra những tiêu chí chưa rõ.
3. Truy xuất quy ước và skill chuyên biệt.
4. Lập phạm vi chỉnh sửa dự kiến.
5. Thực hiện từng nhóm thay đổi.
6. Chạy kiểm thử phù hợp.
7. Ghi nhận lỗi và các lần thử sửa.
8. Đối chiếu với tiêu chí chấp nhận.
9. Cập nhật trạng thái công việc.

Không được tự động coi đầu việc hoàn thành nếu chưa có đủ bằng chứng.

Các thao tác có nguy cơ mất dữ liệu phải tuân thủ chính sách phê duyệt.

### 6.3. b-review — Đánh giá thay đổi

**Đầu vào:** Git diff, nhánh, commit range hoặc tác vụ đang thực hiện.

**Quy trình:**

1. Xác định chính xác phạm vi diff.
2. Đọc yêu cầu và tiêu chí chấp nhận nếu có.
3. Truy xuất các quy tắc liên quan.
4. Kiểm tra tính đúng đắn và nghiệp vụ.
5. Kiểm tra kiến trúc, bảo mật và hiệu năng.
6. Đánh giá kiểm thử, lỗi hồi quy và phạm vi thay đổi.
7. Đưa ra các phát hiện với đường dẫn, dòng mã, mức độ nghiêm trọng và bằng chứng.
8. Phân biệt lỗi thực tế, rủi ro và đề xuất cải thiện.

**Đầu ra:** Báo cáo có cấu trúc, có thể xuất Markdown hoặc JSON.

Bint có thể ghi nhận không phát hiện vấn đề nghiêm trọng, nhưng không được tự xem đó là bằng chứng tuyệt đối rằng thay đổi an toàn.

### 6.4. b-do — Điều phối toàn bộ công việc

**Đầu vào:** Một yêu cầu tự nhiên của người dùng.

**Quy trình mặc định:**

`Classify → b-req → b-feat → b-review → Finalize`

Tuỳ độ phức tạp, có thể bỏ qua những bước không cần thiết.

Ví dụ: sửa lỗi chính tả không nhất thiết phải tạo bộ yêu cầu hoàn chỉnh.

b-do chịu trách nhiệm:

- Chọn workflow.
- Xác định role.
- Lựa chọn harness.
- Gọi Context Engine.
- Điều phối các bước có quan hệ phụ thuộc.
- Giới hạn số vòng sửa đổi và đánh giá.
- Xử lý lỗi, gián đoạn và tiếp tục.
- Ghi nhận toàn bộ lần thực thi.

### 6.5. Chính sách đa agent

Mặc định, các role có thể sử dụng cùng một harness.

Chỉ thực thi đa harness khi:

- Adapter tương ứng đã được xác thực.
- Người dùng cho phép gọi chương trình ngoài.
- Có khả năng cô lập thay đổi.
- Chi phí nằm trong ngân sách.

Agent có quyền ghi phải sử dụng worktree hoặc môi trường cô lập khi chạy song song.

Không tự động trộn các bản thay đổi xung đột.

### 6.6. Skill gốc và lớp phủ

Skill gốc là bất biến trong repository và được nâng cấp cùng phiên bản Bint. Tuỳ biến theo dự án nằm trong lớp phủ:

- Skill gốc khai báo các điểm neo có tên (ví dụ: `project-rules`, `test-strategy`, `review-checklist`).
- Lớp phủ chỉ được thêm hoặc thay nội dung tại các điểm neo đó.
- `bint sync` ghép skill gốc với lớp phủ thành skill hoàn chỉnh cho từng harness.

Khi nâng cấp Bint:

1. Thay skill gốc bằng phiên bản mới, cập nhật `core.lock`.
2. Áp lại lớp phủ.
3. Nếu một điểm neo không còn tồn tại, báo xung đột và giữ bản cũ cho tới khi người dùng xử lý; không tự đoán vị trí mới.

---

## 7. Harness Adapter

Cần định nghĩa giao diện trừu tượng thay vì gắn logic Bint trực tiếp với Claude Code hoặc Codex.

Một adapter cung cấp các năng lực:

- Phát hiện chương trình và phiên bản.
- Cài đặt và đồng bộ skill.
- Ghép skill gốc với lớp phủ theo định dạng của harness.
- Sinh tài liệu hướng dẫn cho harness.
- Đăng ký MCP server của Bint.
- Xác định khả năng ẩn hoặc gọi ngầm skill.
- Đăng ký, xoá hoặc cập nhật hook.
- Khởi chạy tác vụ qua giao diện được hỗ trợ.
- Thu thập sự kiện và dữ liệu sử dụng nếu có.
- Báo cáo các khả năng không được hỗ trợ.

Ví dụ hợp đồng khái niệm:

```go
type HarnessAdapter interface {
    Name() string
    Detect(ctx context.Context) (Capabilities, error)
    SyncSkills(ctx context.Context, skills []Skill) error
    InstallHooks(ctx context.Context, hooks []Hook) error
    Execute(ctx context.Context, req ExecuteRequest) (RunResult, error)
}
```

Đây là hợp đồng mục tiêu; các phương thức có thể là tuỳ chọn qua những giao diện nhỏ hơn để tránh ép adapter phải hỗ trợ chức năng không tồn tại.

### Các cấp hỗ trợ

**Cấp 1 — Skill + MCP**

Bint sinh skill, đăng ký MCP server nếu harness hỗ trợ, và cung cấp CLI. Agent dùng Bint qua công cụ MCP; với harness không hỗ trợ MCP, qua các lệnh CLI được mô tả trong skill.

**Cấp 2 — Skill + Hook**

Bint thu thập sự kiện vòng đời và chèn ngữ cảnh theo đường dẫn trước khi agent đọc hoặc sửa tệp.

**Cấp 3 — Programmatic Execution**

Bint có thể khởi chạy agent từ CLI, truyền yêu cầu, thu kết quả và điều phối nhiều lượt.

**Cấp 4 — Full Orchestration**

Hỗ trợ phối hợp nhiều agent, cô lập thực thi, theo dõi chi phí và thử nghiệm cải tiến.

### Chính sách skill nội bộ

Skill phát triển được lưu trong `.bint/skills/evolved`.

Adapter chỉ xuất bản skill cho harness khi có chiến lược kích hoạt tương thích.

Với harness không hỗ trợ ẩn skill khỏi người dùng mà vẫn cho agent gọi tự động, Bint sẽ phân phối nội dung skill qua Context Engine hoặc skill gốc.

Không mô phỏng khả năng ẩn bằng một metadata không được harness hiểu.

---

## 8. Mô hình tri thức và Context Engine

### 8.1. Đơn vị tri thức

Bint không xem tệp Markdown là đơn vị tri thức cơ bản.

Mỗi đơn vị tri thức là một tệp `.bint/knowledge/items/<ulid>.md` gồm phần đầu YAML và nội dung. Phần đầu có:

- ID ổn định dạng ULID (tạo được trên nhiều máy mà không xung đột).
- Loại: `rule`, `fact`, `pattern`, `decision`, `lesson`, `warning`.
- Phạm vi áp dụng: đường dẫn (glob), ký hiệu, miền nghiệp vụ, thẻ.
- Nguồn bằng chứng: đường dẫn, vùng ký hiệu, commit.
- Fingerprint của từng nguồn bằng chứng.
- Nguồn gốc: `human`, `agent` hoặc `imported`; lượt chạy đã tạo ra mục này; các đầu vào agent đã đọc khi tạo.
- Người duyệt và thời điểm duyệt, nếu có.
- Mức tin cậy, phiên bản, thời điểm xác thực, trạng thái hiệu lực.

Các mức trạng thái:

`candidate → verified → stale → deprecated`

Quy tắc chuyển trạng thái:

| Chuyển | Điều kiện |
|---|---|
| `candidate → verified` (loại `rule`) | Người duyệt qua `bint knowledge approve` |
| `candidate → verified` (loại khác) | Qua kiểm tra xác định được bằng chương trình và không mâu thuẫn với mục `verified` khác; người dùng có thể yêu cầu duyệt tay cho mọi loại |
| `verified → stale` | Fingerprint của nguồn bằng chứng thay đổi |
| `stale → verified` | Xác thực lại theo mục 8.4 |
| `* → deprecated` | Người dùng, hoặc đề xuất tiến hoá đã được chấp nhận |

Agent không được tự nâng một mục do chính agent tạo lên thành quy tắc bắt buộc.

Thông tin được trích xuất trực tiếp từ mã nguồn vẫn có thể trở nên lỗi thời khi nguồn thay đổi.

### 8.2. Truy xuất ngữ cảnh

Đầu vào:

`bint context "<task-description>"`

Các bước:

1. Chuẩn hoá mô tả tác vụ.
2. Phân loại loại công việc và miền nghiệp vụ.
3. Xác định các khu vực mã nguồn có khả năng liên quan.
4. Truy xuất tri thức và skill theo tên, đường dẫn, thẻ và độ liên quan.
5. Kiểm tra trạng thái hiệu lực của bằng chứng.
6. Loại các mục trùng lặp hoặc mâu thuẫn chưa giải quyết.
7. Ưu tiên các quy tắc bắt buộc.
8. Tạo gói ngữ cảnh theo ngân sách.
9. Trả nội dung cùng các tham chiếu.

Bên cạnh truy xuất theo mô tả tác vụ, Context Engine hỗ trợ **truy xuất theo đường dẫn**:

`bint context --paths <path>...` (hoặc công cụ MCP tương ứng, hoặc hook trước khi đọc/sửa tệp)

Đường dẫn tệp agent sắp động tới là tín hiệu chính xác hơn mô tả tác vụ, vốn thường mơ hồ. Truy xuất theo đường dẫn chỉ trả về các mục có phạm vi khớp, ưu tiên `rule` và `warning`, và không lặp lại những mục đã chèn trong cùng phiên.

Công cụ MCP dự kiến:

| Công cụ | Chức năng |
|---|---|
| `context_for_task` | Gói ngữ cảnh theo mô tả tác vụ |
| `context_for_paths` | Quy tắc và cảnh báo theo đường dẫn |
| `knowledge_search` | Tìm kiếm tri thức |
| `knowledge_get` | Đọc một mục theo ID |
| `cite_knowledge` | Agent báo đã áp dụng mục nào |
| `report_observation` | Agent ghi nhận lỗi hoặc kinh nghiệm (chỉ tạo `candidate`) |

Các công cụ đều chỉ đọc, trừ hai công cụ ghi nhận; không công cụ nào thay đổi được trạng thái hiệu lực của tri thức.

### 8.3. Chiến lược tìm kiếm

**MVP:** Tìm kiếm từ khoá bằng SQLite FTS5 kết hợp thông tin cấu trúc repository.

**Giai đoạn sau:** Thêm truy xuất ngữ nghĩa khi FTS5 không đạt chất lượng mong muốn.

Không bắt buộc sử dụng cơ sở dữ liệu vector ngay từ đầu.

### 8.4. Chống ngữ cảnh lỗi thời

Fingerprint được gắn với **vùng ký hiệu** (hàm, kiểu, khối cấu hình) thay vì cả tệp, để tránh việc một thay đổi nhỏ làm hàng loạt mục bị đánh dấu lỗi thời.

Các mức fingerprint, theo thứ tự ưu tiên:

1. **Ký hiệu:** phân tích bằng tree-sitter với các ngôn ngữ được hỗ trợ.
2. **Vùng dòng:** khi không phân tích được ký hiệu, một mục chỉ bị đánh dấu lỗi thời nếu các khối thay đổi trong Git diff chồng lên vùng dòng được tham chiếu.
3. **Tệp:** phương án cuối cùng.

Khi một mục bị đánh dấu `stale`:

1. Chạy kiểm tra xác định được trước: đường dẫn, ký hiệu còn tồn tại không, ví dụ mã còn khớp không.
2. Chỉ dùng agent để xác thực lại các mục rủi ro cao (`rule`, `warning`), gom theo lô và trong giới hạn ngân sách.
3. Mục chưa được xác thực lại thì không được chèn như quy tắc chắc chắn; nếu được trả về thì phải kèm cảnh báo.
4. Mục `stale` quá thời hạn cấu hình mà không được xác thực sẽ được đề xuất chuyển sang `deprecated`.

### 8.5. Tài liệu cấp repository

Chỉ có hai tệp ở thư mục gốc: AGENTS.md và CLAUDE.md. Cả hai ngắn, ổn định, có chức năng dẫn đường, và chỉ chứa:

- Cách dùng Bint (MCP, CLI).
- Một số ít quy tắc bắt buộc quan trọng nhất, có hiệu lực ở mọi nơi trong repository.

Các tài liệu chi tiết hơn (bản đồ mã nguồn, thuật ngữ, mẫu triển khai) là **bản sinh từ tri thức**, không phải nguồn chuẩn. `bint docs render` tạo chúng, kèm dòng ghi chú "được tạo tự động, không sửa tay". Muốn sửa nội dung thì sửa mục tri thức tương ứng.

Nếu repository đã có tài liệu cũ mô tả cùng quy tắc, bootstrap đề xuất chọn một bên làm nguồn chuẩn và chuyển bên kia thành tham chiếu.

### 8.6. Nguồn chuẩn

- Tệp trong `.bint/knowledge/items/` là nguồn chuẩn duy nhất của tri thức và bằng chứng tối thiểu đi kèm.
- SQLite chứa chỉ mục FTS5, trạng thái tính toán và liên kết với lượt chạy. Mọi thông tin trong chỉ mục phải dựng lại được từ tệp.
- Khi phát hiện tệp thay đổi (sau `git pull`, khi chuyển nhánh), Bint cập nhật chỉ mục ở lần gọi tiếp theo.
- Bằng chứng chi tiết (nhật ký chạy, trích đoạn) có thể chỉ nằm ở máy cục bộ, nhưng mỗi mục phải mang đủ bằng chứng tối thiểu để người khác kiểm tra lại mà không cần dữ liệu cục bộ đó.

### 8.7. Làm việc nhóm

Tri thức và skill được commit nghĩa là nhiều người cùng dùng, trong khi tiến hoá chạy cục bộ trên máy từng người.

- Mỗi mục tri thức một tệp, ID dạng ULID, để giảm xung đột khi merge.
- Mỗi đề xuất tiến hoá ghi phiên bản gốc của đối tượng nó sửa. Nếu phiên bản gốc đã thay đổi (do người khác đã áp dụng thay đổi khác), đề xuất trở thành không hợp lệ và phải đánh giá lại trên phiên bản mới.
- Khi hai thay đổi cùng sửa một skill được merge qua Git, `bint doctor` phát hiện và yêu cầu xử lý thủ công; Bint không tự hợp nhất.
- Tri thức do người khác commit cũng là đầu vào cần kiểm soát nguồn gốc (xem mục 15.5).

---

## 9. Lưu lịch sử bằng SQLite

### 9.1. Các bảng chính

| Bảng | Vai trò |
|---|---|
| `runs` | Một lần thực thi workflow |
| `run_steps` | Các bước của workflow |
| `events` | Sự kiện được thu thập |
| `run_artifacts` | Tham chiếu tới đầu ra |
| `context_usages` | Ngữ cảnh đã được truy xuất |
| `knowledge_index` | Chỉ mục dẫn xuất từ tệp tri thức, dựng lại được |
| `evidence_details` | Bằng chứng chi tiết chỉ có ở máy cục bộ |
| `skills` | Danh mục skill |
| `skill_versions` | Phiên bản và fingerprint |
| `run_skill_usages` | Liên kết skill với lượt chạy |
| `knowledge_citations` | Mục tri thức agent báo đã áp dụng trong lượt chạy |
| `outcome_signals` | Tín hiệu kết quả của lượt chạy |
| `observations` | Nhận xét, lỗi và kinh nghiệm |
| `evolution_proposals` | Đề xuất cải tiến |
| `evaluation_cases` | Tập tác vụ đánh giá |
| `evaluation_runs` | Kết quả thực nghiệm |
| `evolution_adoptions` | Lịch sử áp dụng và quay lui |

### 9.2. Dữ liệu của một lần chạy

Tối thiểu:

```text
run_id
task_id
parent_run_id
workflow
status
harness
harness_version
model
model_version
bint_version
knowledge_revision
git_base_commit
git_branch
started_at
finished_at
duration_ms
input_tokens
output_tokens
cost_usd
result_summary
verification_status
```

`NULL` thể hiện dữ liệu không có hoặc chưa biết. Không chuyển các thông tin không đo được thành số 0.

### 9.3. Dữ liệu sự kiện

Một sự kiện gồm:

```text
event_id
run_id
step_id
event_type
source
timestamp
payload
schema_version
```

Các sự kiện phải có ID để chống ghi trùng, đặc biệt khi hook và CLI cùng gửi một thông tin.

### 9.4. Trạng thái lượt chạy

`created → running → completed | failed | cancelled | interrupted`

Một lượt chạy có thể ở trạng thái `interrupted` nếu phiên kết thúc bất thường hoặc không nhận được sự kiện kết thúc.

### 9.5. Chính sách lưu trữ

- SQLite nằm trong thư mục trạng thái cục bộ, dùng chung giữa các worktree.
- SQLite dùng WAL và migration có phiên bản.
- Hook không ghi trực tiếp vào SQLite: hook ghi sự kiện dạng JSON từng dòng vào `spool/`, CLI nạp vào cơ sở dữ liệu và loại trùng theo `event_id`. Cách này tránh tranh chấp ghi giữa nhiều phiên song song và giữ hook đủ nhanh.
- Ghi dữ liệu theo giao dịch.
- Sử dụng cơ chế khoá để tránh tiến hoá đồng thời trên cùng cấu hình.
- Không lưu toàn bộ lời nhắc, phản hồi và mã nguồn theo mặc định.
- Hỗ trợ xoá lịch sử và xuất dữ liệu.
- Dữ liệu nhạy cảm được lọc trước khi ghi.
- Các tệp dữ liệu được tạo phải có đường dẫn và quyền truy cập phù hợp.

### 9.6. Nhãn kết quả

Evolution Engine chỉ đáng tin khi kết quả của lượt chạy được gán nhãn đáng tin. Phần lớn lượt chạy thực tế không có tiêu chí chấp nhận rõ ràng, nên Bint thu thập nhiều tín hiệu gián tiếp và ghi rõ độ mạnh của từng loại:

| Tín hiệu | Nguồn | Độ mạnh |
|---|---|---|
| Kiểm thử liên quan qua/không qua | CLI, hook | Mạnh nếu kiểm thử bao phủ thay đổi |
| Đáp ứng tiêu chí chấp nhận | b-feat, b-review | Trung bình, do agent tự đánh giá |
| Người dùng sửa lại diff của agent trong phiên | Git, hook | Trung bình |
| Thay đổi bị hoàn tác trong N ngày | Quét lịch sử Git | Mạnh (tiêu cực) |
| Thay đổi được commit hoặc merge | Quét lịch sử Git | Yếu (tích cực) |
| Phát hiện nghiêm trọng từ b-review | b-review | Trung bình |
| Người dùng chấm trực tiếp (`bint runs rate`) | CLI | Mạnh nhưng thưa |

Quy tắc:

- Không gộp các tín hiệu thành một điểm duy nhất mà không ghi lại thành phần.
- Lượt chạy không có tín hiệu đủ mạnh được đánh dấu `unlabeled` và không được dùng làm bằng chứng cho đề xuất tiến hoá.
- Tín hiệu đến muộn (hoàn tác sau nhiều ngày) cập nhật lại nhãn; các đề xuất đã dùng nhãn cũ phải được xem xét lại.

### 9.7. Quy công

Một lượt chạy thành công không chứng minh mục tri thức hay skill nào đã giúp ích. Bint phân biệt ba mức:

- **Được truy xuất** (`context_usages`): Context Engine đã trả về mục đó.
- **Được áp dụng** (`knowledge_citations`): agent báo đã dùng mục đó qua công cụ `cite_knowledge`.
- **Có tác động:** chỉ xác định được qua đánh giá loại bỏ — chạy lại cùng tác vụ khi bỏ mục đó ra (xem mục 12.4).

Hai mức đầu chỉ là tín hiệu gợi ý cho đề xuất tiến hoá; kết luận về tác động phải dựa vào mức thứ ba.

---

## 10. Evolution Engine

### 10.1. Mục tiêu

Evolution Engine cải thiện chất lượng và hiệu quả thực thi bằng cách rút ra kinh nghiệm từ những lần chạy có bằng chứng.

Đối tượng cải tiến:

- Context routing.
- Skill chuyên biệt.
- Skill gốc.
- Tài liệu và tri thức dự án.
- Quy trình kiểm thử.
- Phân công role và lựa chọn harness.
- Chiến lược điều phối.

### 10.2. Phát hiện cơ hội tiến hoá

Không kích hoạt chỉ vì đạt một số lượt chạy cố định.

Xem xét các tín hiệu:

- Các tác vụ có cấu trúc tương tự xuất hiện nhiều lần.
- Agent lặp lại quá trình tìm kiếm cùng một thông tin.
- Cùng một lỗi xảy ra trong nhiều lượt.
- Một nhóm tác vụ có chi phí ngữ cảnh cao.
- Một quy trình được xác nhận là hiệu quả nhiều lần.
- Skill hiện tại thường bị bỏ qua hoặc dùng sai.
- Tri thức liên quan không còn phù hợp với repository.

### 10.3. Nhóm tác vụ

MVP sử dụng:

- Loại workflow.
- Các thư mục và module liên quan.
- Miền nghiệp vụ.
- Những quy tắc đã áp dụng.
- Từ khoá và đặc điểm cấu trúc.
- Kết quả đánh giá.

Có thể bổ sung biểu diễn ngữ nghĩa để cải thiện việc nhóm các tác vụ có mô tả khác nhau nhưng cùng bản chất.

### 10.4. Đề xuất cải tiến

Agent evolver nhận dữ liệu đã lọc và tạo đề xuất có cấu trúc:

```text
proposal_id
target_type
target_id
reason
supporting_run_ids
expected_benefit
candidate_changes
risk_level
evaluation_plan
```

Các hành động có thể đề xuất:

`create`, `update`, `merge`, `deprecate`, `delete`.

Mỗi đề xuất phải có bằng chứng và giả thuyết có thể kiểm tra.

### 10.5. Các cấp độ tiến hoá

**L1 — Knowledge Evolution**

Bổ sung, cập nhật hoặc vô hiệu hoá tri thức.

**L2 — Skill Evolution**

Tạo và sửa skill chuyên biệt, cải tiến mô tả kích hoạt, hợp nhất hoặc loại bỏ skill.

**L3 — Workflow Evolution**

Thay đổi cách lựa chọn context, phân chia công việc và vai trò.

**L4 — Harness Evolution**

Áp dụng quy trình đề xuất, thử nghiệm và kết hợp phương án theo tinh thần Harness².

Không triển khai toàn bộ bốn mức ngay ở MVP.

### 10.6. Điều kiện tiên quyết về dữ liệu

Trước khi bật bất kỳ cấp tiến hoá nào trên một repository, cần có:

- Đủ lượt chạy có nhãn (không phải `unlabeled`) trong nhóm tác vụ liên quan.
- Tập đánh giá từ lịch sử Git đủ lớn để phát hiện mức cải thiện mong muốn với độ dao động đã đo (xem mục 12.5).
- Ước tính chi phí của vòng Probe nằm trong ngân sách.

Một repository cá nhân có thể chỉ có vài chục tác vụ mỗi tháng. Nếu không đáp ứng các điều kiện trên, Bint chỉ được đề xuất ở cấp L1 (tri thức) với người duyệt, và phải nói rõ rằng đề xuất chưa được kiểm chứng định lượng.

L3 và L4 có thể không khả thi ở quy mô một repository; điều này phải được xác định bằng số liệu chi phí thực tế từ các giai đoạn trước, không giả định.

---

## 11. Vòng lặp Propose–Probe–Compose

### 11.1. Propose

Sinh một hoặc nhiều ứng viên thay đổi dựa trên lịch sử, bằng chứng và mục tiêu tối ưu.

Ứng viên phải giới hạn phạm vi sửa đổi và nêu rõ tác động dự kiến.

### 11.2. Probe

Chạy đối chiếu ứng viên với cấu hình gốc.

Yêu cầu:

- Cùng tác vụ.
- Cùng trạng thái mã nguồn đầu vào.
- Điều kiện thực thi tương đương.
- Dữ liệu đầu vào và điều kiện kiểm thử được ghi nhận.
- Môi trường tách biệt.
- Giới hạn tài nguyên.
- Lặp lại nhiều lần mỗi tác vụ và so sánh theo cặp trên cùng tác vụ.
- Tri thức được dựng theo trạng thái tại thời điểm của tác vụ (xem mục 12.3).
- Ước tính chi phí và được duyệt ngân sách trước khi chạy.

Cần phân biệt lỗi do cấu hình, lỗi do tác vụ, lỗi hạ tầng và việc thiếu kết quả đánh giá.

### 11.3. Compose

So sánh các ứng viên và kết hợp những thay đổi được hỗ trợ bởi bằng chứng.

Không tự động giả định rằng việc kết hợp hai ứng viên tốt sẽ cho kết quả tốt hơn từng ứng viên.

Phương án kết hợp cũng phải được kiểm chứng.

### 11.4. Reflect

Sau khi cải tiến được chấp nhận:

- Lưu lại kết quả.
- Ghi nhận điều kiện áp dụng.
- Cập nhật thư viện kinh nghiệm.
- Liên kết phiên bản mới với bằng chứng.
- Theo dõi kết quả trong các lần sử dụng tiếp theo.

### 11.5. Chính sách áp dụng

Ban đầu dùng chế độ `propose`: chỉ đề xuất và chờ phê duyệt.

Giai đoạn sau có thể cho phép tự áp dụng cải tiến rủi ro thấp khi vượt qua các tiêu chí đánh giá.

Các thay đổi đến chính sách bảo mật, giới hạn quyền, tiêu chí đánh giá và khả năng truy cập dữ liệu luôn phải chịu chính sách kiểm soát nghiêm ngặt hơn.

---

## 12. Hệ thống đánh giá và kiểm chứng

Bint không được đánh giá cải tiến chỉ bằng cảm nhận của agent.

### 12.1. Các thước đo

**Chất lượng:**

- Tỷ lệ đáp ứng tiêu chí chấp nhận.
- Kết quả kiểm thử.
- Lỗi nghiêm trọng được phát hiện.
- Số lần sửa lại.
- Mức độ tuân thủ ràng buộc.
- Khả năng hoàn thành tác vụ.

**Hiệu quả:**

- Thời gian thực thi.
- Số lượt gọi công cụ.
- Số token đầu vào và đầu ra khi có dữ liệu.
- Chi phí thực tế khi có dữ liệu.
- Lượng ngữ cảnh được tải.
- Số thao tác khám phá lặp lại.

**Chất lượng truy xuất:**

- Tỷ lệ tìm đúng quy tắc liên quan.
- Tỷ lệ bỏ sót quy tắc bắt buộc.
- Tỷ lệ chọn nhầm skill.
- Tỷ lệ truy xuất tri thức đã lỗi thời.

### 12.2. Tập đánh giá

**Nguồn tác vụ chính: lịch sử Git của repository.**

`bint eval mine` quét lịch sử và chọn các commit thoả:

- Có thay đổi mã nguồn kèm thêm hoặc sửa kiểm thử.
- Kiểm thử mới thất bại trên commit cha và qua trên commit đó.
- Có mô tả đủ để làm đề bài (thông điệp commit, mô tả PR hoặc issue liên kết).
- Phạm vi thay đổi trong giới hạn cấu hình (không quá lớn để chấm, không quá nhỏ để vô nghĩa).

Mỗi tác vụ gồm: commit gốc (commit cha), đề bài, kiểm thử dùng để chấm, lệnh dựng môi trường và lệnh kiểm thử. Đề bài được viết lại để không lộ lời giải; người dùng duyệt trước khi tác vụ vào tập chính thức.

Điều kiện cần là repository có kiểm thử chạy được và môi trường dựng lại được. Nếu không, tác vụ phải do người dùng viết tay, quy mô tập đánh giá sẽ nhỏ hơn nhiều, và điều này phải được báo rõ.

Tập đánh giá được chia thành:

- Tập xây dựng đề xuất.
- Tập xác thực ứng viên.
- Tập kiểm tra độc lập.

Không được dùng kết quả từ tập kiểm tra độc lập để điều chỉnh trực tiếp ứng viên rồi tiếp tục báo cáo điểm trên chính tập đó như một đánh giá khách quan.

Cần duy trì các trường hợp kiểm thử âm tính và những tình huống chưa từng xuất hiện trong lịch sử chạy.

### 12.3. Chống rò rỉ đáp án

Nếu tri thức được sinh từ trạng thái hiện tại của repository, nó đã chứa thông tin về lời giải của các commit cũ. Đánh giá tác vụ cũ bằng tri thức hiện tại sẽ cho kết quả đẹp giả tạo.

Hai chế độ:

- **Chặt (`strict`):** chạy lại bootstrap tại commit gốc của tác vụ để dựng tri thức đúng thời điểm. Tốn kém nhưng đáng tin; bắt buộc cho các kết luận dùng ở cổng quyết định.
- **Tiết kiệm (`economy`):** chỉ dùng các mục tri thức có bằng chứng tồn tại và khớp fingerprint tại commit gốc, đồng thời loại mọi mục được tạo từ lượt chạy hoặc commit sau thời điểm đó. Vẫn còn rủi ro rò rỉ gián tiếp, nên kết quả chỉ dùng để sàng lọc.

Bài học (`lesson`) rút ra từ chính một tác vụ đánh giá không bao giờ được dùng khi chấm lại tác vụ đó.

### 12.4. Nhánh so sánh

Mọi phép đo có/không có Bint chạy ít nhất ba nhánh trên cùng tập tác vụ:

1. **Không ngữ cảnh:** chỉ harness, không có AGENTS.md hay tri thức.
2. **Tối giản:** AGENTS.md ngắn chứa các quy tắc bất thường, không có Context Engine.
3. **Bint đầy đủ:** Context Engine, chèn theo đường dẫn, skill.

Nhánh 2 là mốc khó vượt nhất và quan trọng nhất: nếu Bint đầy đủ không tốt hơn nó, phần phức tạp còn lại không có lý do tồn tại.

Đánh giá loại bỏ (bỏ từng mục hoặc từng nhóm tri thức) dùng cùng cơ chế để xác định tác động thực (mục 9.7).

### 12.5. Độ dao động và cỡ mẫu

Kết quả của agent dao động đáng kể giữa các lần chạy cùng một tác vụ.

- Mỗi tác vụ được chạy lặp lại nhiều lần trên mỗi nhánh.
- So sánh theo cặp trên cùng tác vụ, báo cáo mức chênh lệch kèm khoảng tin cậy (ví dụ bằng phương pháp lấy mẫu lại), không chỉ báo trung bình.
- Trước khi chạy, ước tính cỡ mẫu cần thiết để phát hiện mức cải thiện mong muốn với độ dao động đã đo ở các lần trước.
- Trước khi chạy, ước tính chi phí: số tác vụ × số lần lặp × số nhánh × chi phí trung bình mỗi lượt. Không chạy nếu vượt ngân sách đã duyệt.

### 12.6. Phiên bản mô hình

Một skill hay chiến lược được tối ưu cho mô hình A có thể hồi quy trên mô hình B.

- Mọi lượt chạy, kết quả đánh giá và lần áp dụng cải tiến đều ghi phiên bản harness và mô hình.
- Khi phiên bản mô hình thay đổi, các cải tiến đã áp dụng được đánh dấu "chưa xác thực với mô hình hiện tại".
- Bint đề xuất chạy lại một tập nhỏ đại diện để kiểm tra hồi quy; mốc so sánh cũ không được dùng cho mô hình mới.

### 12.7. Điều kiện chấp nhận

Một cải tiến chỉ có thể được chấp nhận nếu:

- Không vi phạm chính sách bảo mật.
- Không làm mất các quy tắc bắt buộc.
- Không gây hồi quy nghiêm trọng ở các kiểm thử quan trọng.
- Thể hiện lợi ích trên thước đo được lựa chọn trước.
- Có bằng chứng đủ tin cậy.
- Có khả năng quay lui.

Các ngưỡng định lượng phải được hiệu chỉnh từ dữ liệu thực nghiệm, không gán tuỳ tiện như một bảo đảm chất lượng.

### 12.8. Chi phí tiến hoá

Theo dõi riêng:

- Chi phí thực thi tác vụ.
- Chi phí phân tích lịch sử.
- Chi phí tạo ứng viên.
- Chi phí chạy đối chiếu.
- Chi phí kiểm chứng sau triển khai.

Việc tiến hoá có thể bị từ chối khi lợi ích kỳ vọng không bù được chi phí, ngay cả khi ứng viên có kết quả tốt hơn.

---

## 13. Hook và cơ chế tự động kích hoạt

### 13.1. Nguyên tắc

CLI phải là nguồn ghi nhận chính cho các workflow do Bint điều phối.

Hook đóng vai trò bổ sung dữ liệu từ những hoạt động diễn ra trực tiếp trong harness.

Không nên chỉ dựa vào sự kiện kết thúc phiên để xác định tác vụ đã hoàn tất.

Hook có hai vai trò:

- **Ghi nhận:** ghi sự kiện vào hàng đợi `spool/`, không ghi trực tiếp vào SQLite, không gọi mô hình, hoàn tất trong thời gian rất ngắn.
- **Chèn ngữ cảnh theo đường dẫn:** trước khi agent đọc hoặc sửa tệp, hook gọi Context Engine ở chế độ chỉ đọc và trả về quy tắc khớp đường dẫn. Có giới hạn thời gian; quá hạn thì bỏ qua, không chặn agent.

### 13.2. Chuẩn hoá sự kiện

Adapter chuyển các sự kiện của harness sang định dạng Bint chung.

Ví dụ:

- `session.started`
- `task.started`
- `context.retrieved`
- `skill.activated`
- `tool.completed`
- `task.completed`
- `task.failed`
- `session.ended`

Các sự kiện không được hỗ trợ sẽ không được tự tổng hợp giả.

### 13.3. Bộ kích hoạt tiến hoá

Khi lượt chạy hoàn tất:

1. Lưu kết quả.
2. Cập nhật dữ liệu tổng hợp.
3. Đánh giá xem có đủ bằng chứng mới.
4. Nếu đủ điều kiện, tạo yêu cầu tiến hoá.
5. Kiểm tra ngân sách và trạng thái khoá.
6. Thực thi qua CLI, trình điều phối hoặc cơ chế công việc được hỗ trợ.

Hook đồng bộ không nên trực tiếp thực hiện một vòng gọi mô hình dài.

### 13.4. Chế độ thực thi

**Manual:** Người dùng gọi `bint evolve`.

**Deferred:** Bint đánh dấu yêu cầu tiến hoá và thực hiện khi có một lần gọi CLI phù hợp tiếp theo.

**Managed:** Một tiến trình Bint được cài đặt và quản lý riêng xử lý các yêu cầu tiến hoá.

Bản đầu ưu tiên Manual và Deferred. Không giả định mọi harness đều cho phép khởi chạy tác vụ nền bền vững.

---

## 14. CLI dự kiến

### Khởi tạo và quản lý dự án

| Lệnh | Chức năng |
|---|---|
| `bint init` | Khởi tạo Bint |
| `bint bootstrap` | Chuẩn bị hoặc chạy quy trình khảo sát |
| `bint inspect` | Khám phá repository |
| `bint doctor` | Kiểm tra môi trường |
| `bint sync` | Đồng bộ skill và adapter |
| `bint status` | Trạng thái Bint |

### Ngữ cảnh và tri thức

| Lệnh | Chức năng |
|---|---|
| `bint context "<task>"` | Truy xuất ngữ cảnh |
| `bint knowledge list` | Liệt kê tri thức |
| `bint knowledge verify` | Kiểm tra hiệu lực |
| `bint knowledge search "<query>"` | Tìm kiếm |
| `bint skill list` | Liệt kê skill |
| `bint skill inspect <id>` | Xem chi tiết |
| `bint context --paths <path>...` | Quy tắc theo đường dẫn |
| `bint knowledge approve <id>` | Duyệt mục tri thức |
| `bint knowledge diff` | Xem tri thức mới hoặc bị sửa từ người khác |
| `bint index rebuild` | Dựng lại chỉ mục từ tệp |
| `bint docs render` | Sinh tài liệu cho người đọc |
| `bint mcp serve` | Chạy MCP server |

### Workflow

| Lệnh | Chức năng |
|---|---|
| `bint run <workflow>` | Thực thi workflow |
| `bint run status <id>` | Trạng thái lượt chạy |
| `bint run resume <id>` | Tiếp tục khi được hỗ trợ |
| `bint runs list` | Lịch sử |
| `bint runs show <id>` | Chi tiết một lượt |
| `bint runs rate <id>` | Chấm kết quả lượt chạy |

### Đánh giá

| Lệnh | Chức năng |
|---|---|
| `bint eval mine` | Khai thác tác vụ từ lịch sử Git |
| `bint eval list` | Liệt kê tác vụ đánh giá |
| `bint eval estimate` | Ước tính cỡ mẫu và chi phí |
| `bint eval run` | Chạy so sánh các nhánh |
| `bint eval report` | Báo cáo kết quả kèm khoảng tin cậy |

### Tiến hoá

| Lệnh | Chức năng |
|---|---|
| `bint evolve scan` | Phát hiện cơ hội |
| `bint evolve propose` | Tạo đề xuất |
| `bint evolve evaluate <id>` | Kiểm chứng |
| `bint evolve apply <id>` | Áp dụng |
| `bint evolve rollback <id>` | Quay lui |
| `bint evolve history` | Lịch sử tiến hoá |

Các lệnh trên là giao diện mục tiêu; những lệnh chưa thuộc phiên bản hiện hành phải có thông báo rõ ràng thay vì hoạt động giả.

---

## 15. Bảo mật và ranh giới tin cậy

### 15.1. Dữ liệu repository là dữ liệu không đáng tin cậy

Mã nguồn, README, chú thích và tài liệu của bên thứ ba có thể chứa chỉ dẫn độc hại nhằm tác động lên agent.

Bint phải phân biệt nội dung được dùng làm bằng chứng với chỉ dẫn có quyền điều khiển hành vi.

Không được nâng một chỉ dẫn tìm thấy trong mã nguồn thành chính sách thực thi chỉ vì nó được đưa vào bộ nhớ.

### 15.2. Quyền thực thi

Các thao tác có nguy cơ cao:

- Chạy lệnh không được cho phép.
- Xoá hoặc ghi đè dữ liệu.
- Sửa chính sách bảo mật.
- Thực thi mã từ nguồn ngoài.
- Gửi dữ liệu repository ra bên ngoài.
- Thay đổi cấu hình CI hoặc triển khai.

Phải có cơ chế giới hạn quyền ở tầng chương trình và môi trường thực thi, không chỉ bằng lời nhắc.

### 15.3. Cô lập đánh giá

Các lần chạy thử nghiệm dùng worktree hoặc môi trường cô lập.

Không sử dụng cơ sở dữ liệu sản xuất, kho bí mật hay tài nguyên bên ngoài khi không có quyền cho phép rõ ràng.

### 15.4. Bảo vệ hệ thống tiến hoá

Không cho ứng viên tự sửa:

- Tiêu chí đánh giá dùng để chấm chính ứng viên.
- Chính sách giới hạn tài nguyên.
- Cấu hình quyền truy cập.
- Bằng chứng lịch sử được sử dụng trong đánh giá.
- Cơ chế chấp nhận hoặc quay lui.
- Khung đánh giá, tập đánh giá và cách chọn tác vụ.

Mọi thay đổi phải có bản ghi kiểm toán.

### 15.5. Chống đầu độc bộ nhớ

Tri thức do agent sinh ra từ nội dung không đáng tin, sau đó được commit và chèn vào mọi phiên của cả nhóm, là một con đường để chỉ dẫn độc hại tồn tại lâu dài.

Biện pháp:

- Mỗi mục tri thức lưu nguồn gốc: ai hoặc lượt chạy nào tạo ra, từ những đầu vào nào.
- Chỉ người mới được nâng mục loại `rule` lên `verified`; agent không tự nâng mục do agent tạo.
- Mục có đầu vào chứa nội dung từ bên thứ ba (dependency, tài liệu ngoài, issue) bị gắn cờ và không được chèn như quy tắc bắt buộc.
- Nội dung tri thức được chèn vào ngữ cảnh trong khung đánh dấu rõ là dữ liệu tham khảo, kèm ID và nguồn gốc.
- Thay đổi tri thức từ người khác (qua `git pull`) được đánh dấu để người dùng xem lại; `bint knowledge diff` liệt kê các mục mới hoặc bị sửa.

---

## 16. Kế hoạch phát triển theo giai đoạn

Nguyên tắc sắp xếp: phần ghi nhận dữ liệu và đo lường đi trước, vì mọi quyết định phía sau phụ thuộc vào chúng và dữ liệu cần thời gian để tích luỹ. Các phần trùng với năng lực sẵn có của harness chỉ được xây ở mức tối thiểu.

### P0 — Nền tảng CLI và ghi nhận lượt chạy

**Mục tiêu:** Có một công cụ độc lập, cấu hình ổn định và bắt đầu tích luỹ dữ liệu ngay.

Công việc:

- [x] Khởi tạo Go module và cấu trúc package.
- [x] Xây CLI và quản lý lỗi.
- [x] Định nghĩa config schema v1.
- [x] Thư mục trạng thái cục bộ ngoài repository, xác định theo `git-common-dir`.
- [x] Triển khai SQLite và migration (chọn driver hỗ trợ FTS5, ưu tiên không cần CGO để dễ biên dịch chéo; xác minh trước khi chốt).
- [x] Triển khai `bint init` và `bint doctor`.
- [x] Cơ chế đồng bộ tệp an toàn, có sao lưu; quản lý khối đánh dấu trong AGENTS.md.
- [x] Hàng đợi sự kiện `spool/` và lệnh nạp, loại trùng theo `event_id`.
- [x] Hook ghi nhận tối thiểu cho harness đầu tiên: bắt đầu và kết thúc phiên, tệp được sửa, lệnh kiểm thử.
- [x] Kiểm thử CLI trên các trạng thái repository khác nhau, kể cả nhiều worktree.

**Nghiệm thu:** Người dùng có thể khởi tạo Bint nhiều lần mà không mất dữ liệu hoặc ghi đè thay đổi chưa được chấp nhận; lượt chạy từ nhiều worktree song song được ghi về cùng một cơ sở dữ liệu mà không trùng hay mất sự kiện.

### P1 — Khung đánh giá

**Mục tiêu:** Đo được hiệu quả của bất kỳ thay đổi nào trên chính repository.

Công việc:

- [ ] `bint eval mine`: khai thác tác vụ từ lịch sử Git.
- [ ] Dựng môi trường tại commit gốc trong worktree cô lập.
- [ ] Viết lại đề bài để không lộ lời giải; quy trình duyệt tác vụ.
- [ ] Bộ chạy nhiều nhánh, nhiều lần lặp, cho một harness.
- [ ] Ước tính chi phí và cỡ mẫu trước khi chạy.
- [ ] Báo cáo so sánh theo cặp kèm khoảng tin cậy.
- [ ] Chế độ chống rò rỉ `strict` và `economy`.
- [ ] Đo mốc nhánh "không ngữ cảnh" trên các repository thử nghiệm.

**Nghiệm thu:** Trên ít nhất một repository thử nghiệm, khai thác được tập tác vụ đủ lớn, chạy được nhánh không ngữ cảnh và báo cáo được độ dao động giữa các lần lặp.

### P2 — Bootstrap tối giản

**Mục tiêu:** Sinh tri thức có căn cứ, tập trung vào những điều bất thường.

Công việc:

- [ ] Static discovery: ngôn ngữ, công cụ, manifest, lệnh kiểm thử.
- [ ] Định dạng tệp tri thức, ULID, nguồn gốc, bằng chứng.
- [ ] Quy trình `b-bootstrap` sinh quy tắc bất thường và điểm dễ sai.
- [ ] Kiểm tra xác định được: đường dẫn, ký hiệu, ví dụ mã.
- [ ] `bint knowledge approve` và bảng chuyển trạng thái.
- [ ] Sinh AGENTS.md (khối Bint) và CLAUDE.md (nhập lại).
- [ ] Đo nhánh "tối giản" so với "không ngữ cảnh".

**Cổng quyết định 1:** Với ngưỡng được ghi vào tài liệu trước khi chạy, nhánh tối giản không được kém nhánh không ngữ cảnh về tỷ lệ đạt (trong khoảng tin cậy) và phải giảm rõ rệt chi phí hoặc thao tác khám phá. Nếu không đạt: điều chỉnh loại tri thức được sinh và đo lại; sau hai vòng vẫn không đạt thì dừng hướng sinh tri thức tự động, chỉ giữ tri thức do người viết.

### P3 — Context Engine và vòng đời tri thức

**Mục tiêu:** Đưa đúng tri thức tới đúng chỗ và giữ nó không lỗi thời.

Công việc:

- [ ] Chỉ mục FTS5 dẫn xuất, `bint index rebuild`.
- [ ] Truy xuất theo mô tả tác vụ và theo đường dẫn.
- [ ] `bint mcp serve` với các công cụ ở mục 8.2.
- [ ] Hook chèn ngữ cảnh theo đường dẫn, có giới hạn thời gian.
- [ ] Fingerprint theo ký hiệu (tree-sitter) và theo vùng dòng.
- [ ] Xác thực lại theo mức rủi ro.
- [ ] Ghi nhận truy xuất và trích dẫn (`cite_knowledge`).
- [ ] Thu thập tín hiệu kết quả (mục 9.6), quét hoàn tác từ lịch sử Git.
- [ ] Đo nhánh "Bint đầy đủ" so với "tối giản".

**Cổng quyết định 2:** Bint đầy đủ phải tốt hơn nhánh tối giản trên thước đo đã chọn trước. Nếu không, Context Engine được thu hẹp về chế độ chèn theo đường dẫn (phần rẻ nhất) và các giai đoạn tiến hoá được xem xét lại.

### P4 — Workflow tối thiểu

**Mục tiêu:** Có chuỗi làm việc đầu tiên, đủ để ghi nhận và điều phối.

Công việc:

- [ ] Skill gốc bất biến, cơ chế điểm neo và lớp phủ.
- [ ] Xây b-req, b-feat, b-review, b-do ở mức tối thiểu.
- [ ] Định nghĩa cấu trúc task artifacts và state machine.
- [ ] Kiểm tra tiêu chí chấp nhận, gắn với tín hiệu kết quả.
- [ ] Ghi nhận lỗi và trạng thái gián đoạn.
- [ ] Nâng cấp skill gốc và áp lại lớp phủ.

**Nghiệm thu:** Một yêu cầu tính năng đi qua b-do với các đầu ra liên kết bằng task ID; nâng cấp phiên bản Bint không làm mất lớp phủ.

### P5 — Multi-Harness

**Mục tiêu:** Tách biệt workflow khỏi harness thực thi.

Công việc:

- [ ] Hoàn thiện HarnessAdapter và capability matrix (bao gồm hỗ trợ MCP và hook trước khi đọc/sửa tệp).
- [ ] Tích hợp Claude Code.
- [ ] Tích hợp Codex.
- [ ] Tích hợp Antigravity.
- [ ] Đồng bộ skill theo từng định dạng.
- [ ] Hỗ trợ role override.
- [ ] Chuẩn hoá sự kiện khi có hỗ trợ.
- [ ] Cô lập các tác vụ có quyền ghi.
- [ ] Mở rộng khung đánh giá cho nhiều harness.
- [ ] Kiểm thử hành vi khi adapter không khả dụng.

**Nghiệm thu:** Cùng một workflow và cùng một nguồn tri thức dùng được trên nhiều harness, với các giới hạn khả năng được báo cáo chính xác.

### P6 — Tiến hoá tri thức (L1)

**Mục tiêu:** Đề xuất thay đổi tri thức có bằng chứng.

Công việc:

- [ ] Kiểm tra điều kiện tiên quyết về dữ liệu (mục 10.6).
- [ ] Phát hiện nhóm tác vụ tương tự và chi phí khám phá lặp lại.
- [ ] Xây agent evolver; proposal schema có phiên bản gốc.
- [ ] Đánh giá loại bỏ để xác định tác động của từng mục.
- [ ] Đề xuất thêm, sửa, gộp, loại bỏ tri thức.
- [ ] Phê duyệt, quay lui, phát hiện đề xuất mất hiệu lực do phiên bản gốc thay đổi.

**Nghiệm thu:** Các đề xuất được chấp nhận cho thấy lợi ích trên tập kiểm tra độc lập, không chỉ trên các tác vụ đã dùng để tạo đề xuất.

### P7 — Tiến hoá skill (L2)

**Mục tiêu:** Tạo và cải tiến skill dựa trên kinh nghiệm.

Công việc:

- [ ] Tạo skill chuyên biệt.
- [ ] Hợp nhất và loại bỏ skill.
- [ ] Kiểm tra điều kiện kích hoạt.
- [ ] Quản lý phiên bản skill, gắn với phiên bản mô hình.
- [ ] Tạo và xem bản thay đổi.
- [ ] Phát hiện hồi quy khi đổi mô hình.

**Nghiệm thu:** Bint có thể đề xuất một skill mới từ lịch sử và chứng minh skill này hữu ích trên tập tác vụ đối chiếu trước khi người dùng áp dụng.

### P8 — Tiến hoá tự động có giới hạn (L3–L4)

**Điều kiện mở:** Số liệu chi phí từ P6–P7 cho thấy vòng Propose–Probe–Compose khả thi về chi phí ở quy mô repository mục tiêu.

Công việc:

- [ ] Triển khai Propose–Probe–Compose.
- [ ] Chạy ứng viên trong môi trường cô lập.
- [ ] Thử nghiệm chiến lược chọn ngữ cảnh.
- [ ] Thử nghiệm thay đổi workflow.
- [ ] Theo dõi chi phí tiến hoá.
- [ ] Tự áp dụng cải tiến rủi ro thấp.
- [ ] Phát hiện hồi quy và tự đề xuất quay lui.

**Nghiệm thu:** Hệ thống có thể cải thiện hiệu quả trên tập tác vụ mới, không chỉ những ví dụ đã dùng để đề xuất cải tiến, trong khi vẫn bảo đảm các điều kiện chất lượng và an toàn.

---

## 17. Chiến lược kiểm thử toàn hệ thống

### Unit Tests

Kiểm thử cấu hình, truy xuất tri thức, SQLite, fingerprint, trạng thái và logic phân loại.

### Integration Tests

Kiểm thử CLI với repository mẫu, Git worktree và adapter giả lập.

Bao gồm: nhiều worktree ghi đồng thời; nhiều phiên hook song song; xoá chỉ mục rồi dựng lại từ tệp và so sánh; `git pull` mang về tri thức xung đột; nâng cấp skill gốc khi có lớp phủ.

### Adapter Contract Tests

Mỗi adapter phải chứng minh:

- Không tạo skill sai định dạng.
- Không ghi đè dữ liệu ngoài phạm vi.
- Báo cáo chính xác khả năng hỗ trợ.
- Xử lý mất chương trình hoặc phiên lỗi.
- Không để lại trạng thái thực thi nguy hiểm.

### End-to-End Tests

Chọn các repository thử nghiệm đại diện, chẳng hạn:

- Go backend.
- TypeScript/Node.js backend.
- React hoặc Next.js frontend.
- Repository hỗn hợp nhiều package.

Thực hiện các tác vụ giống nhau trên ba nhánh so sánh (mục 12.4).

### Evaluation Integrity Tests

Kiểm chứng rằng:

- Chế độ `strict` không để lọt mục tri thức nào được tạo sau commit gốc.
- Bài học từ một tác vụ không xuất hiện khi chấm lại chính tác vụ đó.
- Đề bài được viết lại không chứa đoạn mã của lời giải.
- Chạy lại khung đánh giá với cùng tác vụ và cấu hình cho phân phối kết quả ổn định.

### Evolution Regression Tests

Kiểm chứng rằng:

- Skill mới không thay đổi các ràng buộc bắt buộc.
- Thao tác hợp nhất không gây mất kiến thức.
- Skill lỗi thời không được ưu tiên.
- Các đề xuất không tự sửa tiêu chí chấm điểm.
- Hệ thống quay lui đúng phiên bản.

---

## 18. Mốc phát hành

### v0.1 — Foundation

Hoàn thành P0 và P1.

Có CLI, ghi nhận lượt chạy, thư mục trạng thái cục bộ và khung đánh giá với mốc "không ngữ cảnh".

### v0.2 — Measured Bootstrap

Hoàn thành P2 và qua cổng quyết định 1.

Có bootstrap tối giản, AGENTS.md ngắn và số liệu so sánh với mốc.

### v0.3 — Context

Hoàn thành P3 và qua cổng quyết định 2.

Có Context Engine qua MCP, chèn ngữ cảnh theo đường dẫn, vòng đời tri thức và tín hiệu kết quả.

### v0.4 — Workflow & Interoperability

Hoàn thành P4 và P5.

Có chuỗi b-req, b-feat, b-review, b-do tối thiểu, lớp phủ skill và tích hợp ba harness mục tiêu theo những cấp khả năng đã được xác thực.

### v0.5 — Guided Evolution

Hoàn thành P6 và P7.

Có đề xuất tiến hoá tri thức và skill, kiểm chứng trên tập độc lập, phê duyệt và quay lui.

### v1.0 — Verified Autonomous Evolution

Hoàn thành P8 (nếu điều kiện mở được đáp ứng) và đáp ứng bộ tiêu chí chất lượng.

Có tiến hoá tự động có giới hạn, kiểm chứng đối chiếu và khả năng quay lui. Nếu P8 không khả thi về chi phí, v1.0 được định nghĩa lại từ v0.5 cộng với độ ổn định vận hành.

---

## 19. Các chỉ số quyết định thành công

Không nên định nghĩa thành công bằng số skill sinh ra hoặc số lần agent tự sửa prompt.

Các chỉ số chủ đạo:

**Chất lượng:** Tỷ lệ hoàn thành tác vụ đạt tiêu chí chấp nhận, tỷ lệ hồi quy và lỗi nghiêm trọng.

**Hiệu quả:** Chi phí và thời gian để hoàn thành đúng tác vụ, bao gồm chi phí sửa lỗi và chạy kiểm chứng.

**Ngữ cảnh:** Lượng thông tin phải đọc lại, tỷ lệ tìm đúng quy tắc và tỷ lệ dùng tri thức lỗi thời.

**Mốc so sánh:** Chênh lệch so với nhánh tối giản (AGENTS.md ngắn) là chỉ số chính; chỉ vượt nhánh không ngữ cảnh là chưa đủ.

**Tiến hoá:** Tỷ lệ đề xuất mang lại lợi ích trên tập đánh giá độc lập, tác dụng phụ, chi phí thử nghiệm và số lần phải quay lui.

**Khả năng vận hành:** Tính ổn định của CLI, khả năng nâng cấp phiên bản, tính nhất quán dữ liệu và tương thích với các harness.

Bint chỉ nên tuyên bố rằng hệ thống đang tiến hoá hiệu quả khi chất lượng và hiệu quả thực tế được chứng minh trên dữ liệu chưa dùng để tạo các cải tiến đó.

**Tiêu chí dừng:** Mỗi cổng quyết định ở mục 16 phải có ngưỡng cụ thể được ghi vào tài liệu trước khi chạy đo. Không điều chỉnh ngưỡng sau khi đã thấy kết quả.

---

## 20. Những quyết định kỹ thuật phải giữ nhất quán

1. **Bint Core là nguồn điều phối chuẩn**, không phải bất kỳ harness nào.
2. **Tệp trong Git là nguồn chuẩn của tri thức**; SQLite lưu lịch sử lượt chạy và chỉ mục dẫn xuất có thể dựng lại.
3. **Skill không phải đơn vị tri thức cơ bản**; skill được tổng hợp từ tri thức và kinh nghiệm.
4. **Hook không phải nguồn sự thật duy nhất** về một lượt thực thi.
5. **Không mặc định cần nhiều agent** cho một tác vụ.
6. **Không mặc định mọi harness có cùng khả năng**.
7. **Không tự động chấp nhận cải tiến chỉ vì agent đánh giá tích cực**.
8. **Không tự tăng số lượng skill vô hạn**; phải có hợp nhất và loại bỏ.
9. **Không sửa các quy tắc bảo vệ và tiêu chí đánh giá bằng chính ứng viên đang được chấm**.
10. **Không được làm suy giảm chất lượng chỉ để giảm số token**.
11. **Đo trước khi xây**: lớp tính năng sau chỉ được mở khi lớp trước qua cổng quyết định.
12. **Trạng thái cục bộ nằm ngoài repository** và dùng chung giữa các worktree.
13. **Skill gốc bất biến**; tuỳ biến theo dự án chỉ qua lớp phủ.
14. **Chỉ người mới nâng được quy tắc bắt buộc**; agent không tự xác thực tri thức do agent tạo.
15. **Đánh giá trên tác vụ lịch sử phải chống rò rỉ đáp án**.
16. **Mọi cải tiến gắn với phiên bản mô hình** đã dùng để kiểm chứng nó.

### Thứ tự triển khai ưu tiên

`CLI + Run Log → Eval Harness → Bootstrap tối giản → Context Engine (MCP, đường dẫn) → Core Skills tối thiểu → Harness Adapters → Knowledge Evolution → Skill Evolution → Harness Evolution`

Đây là chuỗi phụ thuộc kỹ thuật được đề xuất, không phải yêu cầu tất cả giai đoạn phải triển khai tuần tự tuyệt đối.

Mỗi giai đoạn phải có bộ kiểm thử và tiêu chí nghiệm thu riêng trước khi được xem là hoàn tất.