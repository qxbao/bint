# Kế hoạch: tiến hoá quy trình và bài học từ thất bại

Ngày: 2026-11-11. Mở rộng P8 theo hai ý học từ các nghiên cứu tối ưu harness (hiện trạng tổng thể xem tại [docs/status.md](status.md)):

1. **Kế hoạch E — Bài học từ thất bại.** Mỗi lượt thất bại được tóm tắt, rồi rút thành bài học có phạm vi rõ ràng. Thư viện bài học tích luỹ dần, tương tự `EXPERIENCE.md`.
2. **Kế hoạch H — Sửa được quy trình.** Ngoài tri thức và skill, vòng tiến hoá được đề xuất thay đổi:
   - chỉ dẫn dùng công cụ;
   - guardrail;
   - script hỗ trợ;
   - tham số chọn ngữ cảnh.

   Mỗi thay đổi thuộc một trong hai loại: **chung** (mọi vùng mã) hoặc **riêng** (một vùng mã).

E tận dụng gần như toàn bộ hạ tầng đã có (mục tri thức loại `lesson`, đề xuất, đối chiếu, hook theo đề bài) và cũng là thành phần "bộ nhớ" của H. Hiện **E1–E5 và kiểm thử xác định của E6 đã triển khai xong** (chờ chạy thực nghiệm có ngân sách); **H1–H5 vẫn là thiết kế đề xuất**.

## Hiện trạng: Bint đang điều khiển được gì

| Thành phần quanh agent | Có trong Bint chưa | Vòng tiến hoá sửa được chưa |
|---|---|---|
| Tri thức (fact, rule, warning, pattern, decision, lesson) | Có | Có (L1) |
| Skill | Có (skill tiến hoá) | Có (L2), luôn chờ người duyệt |
| Bộ nhớ kinh nghiệm từ thất bại | Có (`experience digest`/`show`, reflector, ngữ cảnh bài học) | Có qua `failure_lesson` (L1); chưa đo trên reflector thật |
| Chỉ dẫn dùng công cụ | Văn bản cố định trong mã (ghi chú MCP) | Chưa |
| Guardrail | Chỉ có `protected_paths` (hỏi/chặn khi sửa) | Chưa |
| Script hỗ trợ | Chưa | Chưa |
| Chiến lược chọn ngữ cảnh | Có tham số và nhánh biến thể `bint@…` | Đo được, chưa đề xuất được |

---

## Kế hoạch E — Bài học từ thất bại (đã có mã, chưa đo)

### Luồng xử lý

```
lượt thất bại ──► bản tóm tắt thất bại ──► agent rút bài học ──► kiểm tra ──► đề xuất ──► đối chiếu ──► thư viện
   (eval validation,       (xác định,           (model mạnh)        (rò rỉ, trùng,     (L1, lesson)   (validation→test)  (chèn theo
    phiên thật bị gắn       không gọi model)                          phạm vi)                                            đề bài/đường dẫn)
    nhãn bad)
```

### E1. Nguồn thất bại và bản tóm tắt (không tốn token)

**Nguồn:**
- Lượt trượt trong đợt eval trên **tập validation**. Không bao giờ dùng tập test.
- Phiên làm việc thật đã gắn nhãn `bad` hoặc thuộc điểm nóng thất bại. Hai nguồn này đã có trong `evolve scan`.

**`bint experience digest <batch|run>`** dựng bản tóm tắt thất bại một cách xác định từ log của lượt chạy:
- Đề bài. Riêng với lượt eval chỉ lấy đề bài, không lấy diff lời giải.
- Chuỗi lệnh agent đã chạy và các tệp đã đọc/sửa.
- Test fail lúc chấm: tên test và vài dòng lỗi cuối.
- Dấu hiệu bất thường:
  - sửa tệp ngoài vùng;
  - không chạy test trước khi kết thúc;
  - lặp cùng một lệnh;
  - hết lượt hoặc hết thời gian;
  - bỏ qua tri thức đã được chèn.
- Bản tóm tắt giới hạn khoảng 1.500 token, để chi phí của bước rút bài học không phụ thuộc độ dài log.

**Gom nhóm thất bại** theo hai trục:
- **Vùng mã:** thư mục bị sửa hoặc thư mục của test fail. Nhóm theo trục này sinh bài học *riêng*.
- **Dấu hiệu hành vi:** ví dụ "không chạy test liên quan". Nhóm theo trục này gom được qua nhiều vùng và sinh bài học *chung*.

**Cơ hội `failure_lesson`:** ngưỡng nhóm lấy từ `evolution.trigger.min_similar_runs` (mặc định 3), đếm tác vụ phân biệt để các lần lặp của cùng tác vụ không tự làm nhóm đủ điều kiện.

### E2. Rút bài học (tốn token, model cấu hình riêng)

- Vai trò `roles.reflector` lấy mô hình từ `harnesses.claude.models.reflector` (mặc định `opus`), tách khỏi mô hình thực thi (`bint evolve propose --reflector-model <model>` ghi đè cho một lần chạy; hiện chỉ hỗ trợ `claude` với `--tools ""` trong thư mục làm việc rỗng).
- Đầu vào: bản tóm tắt của một nhóm thất bại, kèm các bài học sẵn có cùng phạm vi để tránh trùng.
- Đầu ra theo khuôn cố định:

  ```yaml
  scope: global | domain:<glob>
  trigger: <khi nào áp dụng — tình huống, không phải tác vụ cụ thể>
  mistake: <agent đã làm sai gì>
  instead: <nên làm gì>
  evidence: [<attempt/run id>...]   # chính các lượt thất bại trong nhóm
  ```

- Agent viết tối đa 2 ứng viên theo hai phạm vi:
  - **bài học chung:** điểm yếu chung của agent, ví dụ "chạy test của module trước khi kết thúc";
  - **bài học riêng:** quy trình hay bẫy của vùng mã đó.

  Các ứng viên có mặt trong đầu ra đi tiếp sang bước kiểm tra.

### E3. Kiểm tra xác định (không tốn token)

1. **Rò rỉ.** Với lượt eval, `CheckLesson` so bài học với diff lời giải của các tác vụ nguồn: chặn dòng mã, định danh mới ở vị trí mã và chuỗi literal dài của phần thêm mới (trừ phần đã có trong kiểm thử).
2. **Tổng quát.** Bài học chung phải có bằng chứng từ ít nhất 2 vùng mã khác nhau. Bài học riêng phải có ít nhất 2 lượt thất bại trong vùng của nó.
3. **Trùng.** Dùng cơ chế phát hiện mục trùng đã có. Nếu trùng thì đề xuất gộp thay vì tạo mới.
4. **Độ dài và hình thức.** Trigger, mistake và instead đều phải có; tổng không quá khoảng 80 từ.
5. **Nguồn gốc.** Mục ghi `origin: agent`, `provenance: failure_lesson` và danh sách lượt chạy làm bằng chứng. Quy tắc đã có vẫn áp dụng: bài học rút từ tác vụ X không được dùng khi chấm lại X.

### E4. Đề xuất, đối chiếu, áp dụng

- Mỗi bài học qua kiểm tra trở thành đề xuất L1 `create lesson` (hoặc `update` gộp bài học `failure_lesson` trùng).
- Đề xuất đi đúng vòng `bint evolve cycle`: validation trước, test sau, có dừng sớm, và tự bác bỏ nếu không đạt.
- Một bài học **đạt trên tập test độc lập** có bằng chứng rõ là tổng quát hoá được, vì nó rút ra từ validation mà vẫn giúp được trên test.
- Áp dụng tự động vẫn theo quy tắc hiện có: chỉ khi rủi ro thấp, chỉ tạo mục mới, mục vào trạng thái candidate, và phải bật `auto_low_risk`.

### E5. Thư viện và cách đưa vào ngữ cảnh

- **`bint experience show`:** hiển thị thư viện như một `EXPERIENCE.md`. Bài học chung xếp trước, bài học riêng nhóm theo vùng. Mỗi bài học kèm số lần được chèn và số lần đạt/trượt khi có nó.
- **Không chèn cả tệp.** Hook theo đề bài:
  - luôn chèn top-k bài học *chung*, mặc định k=3 (`context.lessons.global_k`);
  - chèn bài học *riêng* khi đề bài hoặc đường dẫn khớp phạm vi.

  Phần bài học dùng ngân sách token riêng `context.lessons.max_tokens` (mặc định 600) để không chiếm chỗ của tri thức thường.
- **Vòng đời:** bài học `failure_lesson` không sinh cơ hội `unused` theo trích dẫn `[k:id]` (vì là chỉ dẫn hành vi) mà được theo dõi qua số lần chèn và tỷ lệ đạt/trượt trong `experience show`; bài học trùng được gộp qua bước đề xuất.

### E6. Kiểm thử và đo

**Kiểm thử (không tốn token):**
- Tạo tóm tắt thất bại từ log mẫu.
- Gom nhóm.
- Kiểm tra rò rỉ có chặn được bài học chứa định danh lời giải.
- E2E với agent giả: chắc chắn log của tập test không bao giờ lọt vào đầu vào của reflector.

**Đo (tốn tiền, cần duyệt trước):**

| Bước | Việc | Ước tính |
|---|---|---|
| 1 | Một đợt `bint` trên validation để lấy lượt trượt (đã có kết quả cũ, dùng lại nếu log còn) | $0–6 |
| 2 | Reflector (Opus) rút bài học cho tối đa 4 nhóm | ~$2–4 |
| 3 | `bint evolve cycle` đối chiếu tối đa 3 bài học trên validation rồi test | ≤ $20 |

Trần: $30.

**Cổng quyết định:**
- Có bài học đạt trên test: đạt nghiệm thu P8 với nguồn thất bại.
- Không có bài học nào đạt sau 3 đề xuất: ghi kết luận âm tính và xem lại cách tóm tắt thất bại trước khi thử tiếp.

**Ước lượng công (đã hoàn thành phần mã):** khoảng 1,5–2 ngày viết mã.

---

### Trạng thái thực hiện (mã E1–E6, chưa đo)

Mã của E1–E5 và kiểm thử không tốn token của E6 đã xong; phần đo (đợt validation, reflector thật,
`evolve cycle` dùng nguồn bài học thật) **chưa chạy**.

- **E1:** `bint experience digest <đợt|lượt>` (`internal/evolve/failure*.go`). Lượt eval ghi vết công cụ
  (stream-json trong `agent.log`) và `files.txt` trước khi áp tệp kiểm thử ẩn (`internal/eval`). Log cũ
  không có vết được ghi là "không đo được". Cơ hội `failure_lesson` khi nhóm có ≥ `evolution.trigger.min_similar_runs`
  tác vụ phân biệt.
- **E2:** vai trò `roles.reflector` và `harnesses.<h>.models.reflector` (mặc định opus với Claude), không công cụ,
  chạy trong thư mục rỗng; với Claude ràng buộc được ép bằng cờ tường minh `--tools ""` (`ClaudeAgent.NoTools`),
  không dựa vào mặc định quyền; `roles.reflector` khác `claude` bị từ chối vì harness khác chưa có cơ chế tắt hết
  công cụ tương đương (sandbox chỉ-đọc vẫn đọc được toàn hệ tệp); khuôn `scope/trigger/mistake/instead/evidence`; tối đa hai ứng viên (chung, riêng).
- **E3:** `CheckLesson` (`internal/evolve/lesson_check.go`): rò rỉ, tổng quát, riêng, trùng, hình thức, nguồn gốc.
  Bài học bị chặn khi chứa dòng mã của lời giải (`CheckLeak`), định danh dạng mã (`eval.SolutionIdentifiers`), MỌI
  định danh mới của phần thêm mới kể cả tên một từ như `Reset` (`eval.SolutionNewIdentifiers`; chỉ định danh ở vị trí
  mã, bỏ chú thích và nội dung chuỗi để từ thường như `server`/`timeout` không chặn nhầm; trừ tên có trong kiểm
  thử), hoặc chuỗi literal đủ dài (≥ 12 ký tự, gồm `"..."`, `'...'` và raw backtick, bỏ literal có trong kiểm
  thử; `eval.SolutionLiterals`); không kiểm
  được rò rỉ thì loại. Scope riêng `domain:<glob>` phải nằm trong vùng của nhóm bằng chứng: glob khớp toàn repo
  (`**`, `*.go`) hoặc rộng hơn vùng bị từ chối.
- **E4:** mỗi ứng viên qua kiểm tra thành đề xuất L1 `create lesson` (hoặc `update` gộp khi trùng, không định lượng),
  đi qua `bint evolve propose` và `bint evolve cycle`. `SelectCases` không chấm lại trên tác vụ nguồn
  (`ProposeInput.EvalCases`) và ghi lý do khi delivery là `minimal`.
- **E5:** `bint experience show` (chung trước, riêng theo vùng, số lần chèn, đạt/trượt của lượt eval và đạt/trượt
  theo nhãn lượt thật `real_success`/`real_failure`). Sự kiện `context.retrieved` ghi cả ID bài học (nhãn `lesson`) nên
  số lần chèn ở lượt thật đếm được. Chỉ mục `failure_lesson` (`Item.LessonFromRuns`) đi qua phần bài học; mục
  `lesson` do người viết hoặc `--from-review` đi luồng tri thức thường. Cùng định nghĩa này dùng cho thư viện
  `experience show`, danh sách bài học sẵn có của reflector và kiểm trùng (lesson của người không bị gộp/ghi đè). Nhánh ideal của eval chở cả bài học
  (ngân sách bài học lớn). Gói ngữ cảnh có `Pack.Lessons` (`internal/ctxengine/lessons.go`): top-k chung (`context.lessons.global_k`, mặc định 3),
  bài học riêng khi khớp, ngân sách riêng `context.lessons.max_tokens` (mặc định 600). Hook không gate theo tri thức
  rỗng nữa: chèn bài học kể cả khi không có mục tri thức nào.
- **E6 (không tốn token):** digest từ log mẫu (`internal/evolve/testdata/failure`), gom nhóm, chặn rò rỉ,
  và E2E với agent giả (`lesson_e2e_test.go`): đầu vào reflector không chứa chuỗi đánh dấu của tập test,
  không có lượt tập test, làm việc trong thư mục rỗng.

Giới hạn đã biết:
- Phiên thật chỉ ghi lệnh kiểm thử (`test.completed`), nên dấu hiệu "không chạy test" và "lặp lệnh" không đo được
  với lượt thật; chỉ lượt eval có vết đầy đủ.
- Reflector nhận tối đa 8 bản tóm tắt đầu của nhóm (theo ID).
- `LoadFailures` chỉ nạp lượt eval trong cửa sổ lịch sử của `Scan` và đọc `agent.log` theo luồng từng dòng
  (bộ nhớ bị chặn; dòng quá 4 MiB bị bỏ, log quá 256 MiB bị cắt đuôi). Vết thiếu được đánh dấu `Trace.Truncated`
  và không dùng để suy ra `no_test`. Log vẫn được đọc lúc quét (không hoãn tới lúc dựng digest) vì dấu hiệu hành vi
  cần vết để nhóm thất bại.
- Bài học `failure_lesson` không sinh cơ hội `unused` (đề xuất loại bỏ vì không được trích dẫn): chúng là hướng dẫn
  hành vi, ít khi được trích bằng `[k:id]`, và được đo bằng nhãn lượt thật và kết quả đánh giá (`experience show`).
- `bint evolve propose` với `roles.reflector` khác `claude` chỉ cảnh báo và bỏ cơ hội `failure_lesson`; các đề xuất
  xác định và của evolver vẫn chạy.
  Lượt `timeout` nhưng test đạt không phải thất bại.
- Rò rỉ bài học là kiểm tra chuỗi/định danh xác định: nó chặn nội dung nhắc lại đáp án nguyên văn, không chặn
  diễn đạt lại ý của lời giải.
- Chất lượng bài học do reflector thật viết và hiệu ứng lên tỷ lệ đạt **chưa được kiểm chứng**.

## Kế hoạch H — Sửa được quy trình, tách chung/riêng (đề xuất, chưa triển khai)

> **Lưu ý:** Cấu trúc thư mục `.bint/harness/`, lệnh `bint harness add/approve/render` và loại đề xuất `TargetHarness` ở H1–H5 dưới đây là thiết kế dự kiến, chưa có trong mã nguồn hiện tại.

### H1. Đưa thành phần quy trình thành tệp, có phiên bản (không tốn token)

Thư mục mới `.bint/harness/`, quản lý bằng Git như tri thức:

```
.bint/harness/
  guidance/<id>.md      # chỉ dẫn dùng công cụ / quy trình làm việc
  guardrails/<id>.yml   # luật kiểm tra hành vi trong vòng gọi công cụ
  scripts/<id>.sh       # script hỗ trợ agent gọi được (kèm <id>.yml mô tả)
  strategy.yml          # tham số chọn ngữ cảnh (tokens, items, bật/tắt kênh)
```

Mỗi thành phần có các trường sau:

```yaml
id: run-module-tests
scope: global | domain:<glob>       # chung / riêng
status: candidate | verified | deprecated
origin: human | agent
```

**Chỉ dẫn dùng công cụ (`guidance`):**
- Thay đoạn ghi chú MCP đang viết cứng trong mã bằng một mục `guidance` mặc định.
- Chỉ dẫn chung được render vào AGENTS.md hoặc hook theo đề bài. Chỉ dẫn riêng được chèn khi đường dẫn khớp phạm vi.

**Guardrail** được khai báo, Bint thực thi qua hook:

```yaml
id: test-before-stop
scope: global
on: stop                      # pre_edit | pre_bash | stop
when: { edited: "src/**" }    # điều kiện
unless: { ran: "test" }       # đã chạy lệnh khớp mẫu
action: remind                # remind | ask | deny
message: "Chưa chạy test cho phần đã sửa; chạy `make test-<module>` trước khi kết thúc."
```

- `remind`: chèn ngữ cảnh, không chặn.
- `ask`/`deny`: dùng quyết định quyền của harness. Claude Code hỗ trợ đủ cả ba. Với harness khác, Bint hạ xuống mức mà harness hỗ trợ, theo bảng năng lực đã có.
- Cần thêm sự kiện `Stop` vào hook của Claude Code, và theo dõi lệnh đã chạy trong phiên. Bint đã ghi lệnh vào spool.

**Script hỗ trợ:**
- Script nằm trong repository và được liệt kê trong chỉ dẫn, để agent biết mà gọi, ví dụ "chạy đúng test cho tệp X".
- Không tự chạy.
- Luôn rủi ro cao, luôn chờ người duyệt. Script là mã thực thi nên không bao giờ được tự áp dụng.

**Chiến lược ngữ cảnh:** `strategy.yml` ghi đè `context.*` trong cấu hình. Đây chính là các tham số mà nhánh biến thể `bint@…` đang đo được.

**Lệnh:**
- `bint harness list|show|add|approve|deprecate`, cùng mô hình duyệt như `bint knowledge`. Agent không duyệt được.
- `bint harness render` để xem agent sẽ nhận được gì.

### H2. Đo được thay đổi quy trình (không tốn token)

- Nhánh eval `base`/`candidate` dựng thư mục `.bint/harness/` khác nhau, giống cách skill đang dùng `SkillVariant`.
- Hook, AGENTS.md và guardrail trong lượt eval đọc từ bản dựng đó.
- Báo cáo eval thêm số liệu cho từng guardrail: số lần kích hoạt, số lần agent làm theo, và số lần bị chặn.

### H3. Đề xuất thay đổi quy trình (tốn token khi chạy)

**Loại đề xuất mới `TargetHarness`**, gồm các hành động create/update/deprecate trên một thành phần.

**Rủi ro do Bint tính, không do agent tự khai:**

| Thay đổi | Rủi ro | Tự áp dụng được? |
|---|---|---|
| guidance chung/riêng, guardrail `remind` | thấp | Có (nếu `auto_low_risk`) |
| guardrail `ask`, strategy | trung bình | Không |
| guardrail `deny`, script | cao | Không |

**Nguồn cơ hội:**
- Nhóm thất bại theo dấu hiệu hành vi của E1. Ví dụ "không chạy test" dẫn tới guardrail `test-before-stop`, "lặp lệnh" dẫn tới guidance.
- Tín hiệu đo: kênh MCP không được gọi dẫn tới đề xuất sửa guidance; tri thức được chèn nhưng không dùng dẫn tới đề xuất sửa strategy.

**Người đề xuất** dùng model mạnh (`roles.evolver` có thể đặt model riêng), chỉ có quyền Read/Grep/Glob.

**Kiểm tra xác định:**
- Guardrail phải parse được và có mẫu lệnh hợp lệ.
- Phạm vi phải khớp tệp có thật (dùng lại `pruneScopePaths`).
- Không được trùng hoặc mâu thuẫn guardrail sẵn có, ví dụ cùng điều kiện nhưng hành động khác.

### H4. Tách chung/riêng trong vòng tiến hoá

**Propose:** mỗi cơ hội sinh **hai** ứng viên, một bản *chung* và một bản *riêng*, tương tự thiết kế hai ứng viên của nghiên cứu gốc.

**Probe** đo hai bản trên các tập tác vụ khác nhau:
- Bản chung đo trên mẫu đại diện của *toàn bộ* validation.
- Bản riêng chỉ đo trên tác vụ của vùng đó, dùng `SelectCases` theo phạm vi như đang làm với tri thức.

**Compose** chọn theo kết quả:
- Chỉ một bản đạt: giữ bản đó.
- Cả hai đạt: giữ bản chung nếu hiệu ứng của nó không kém bản riêng trên vùng; ngược lại giữ bản riêng.
- **Không gộp** hai bản rồi áp dụng luôn khi chưa đo bản gộp. Bản gộp phải là một đề xuất mới và đối chiếu lại.

**Chi phí:** tăng tối đa gấp đôi mỗi cơ hội. Ngân sách vòng (`--budget`) giữ nguyên trần; ứng viên đầu tiên bị loại sớm trên validation sẽ giải phóng ngân sách cho ứng viên kia.

### H5. Đo

| Bước | Việc | Ước tính |
|---|---|---|
| 1 | Guardrail mặc định do người viết (`test-before-stop`, dạng remind): `bint` so với `bint` + guardrail trên 30 tác vụ | ~$13 |
| 2 | Một vòng cycle với cơ hội quy trình từ E1, tối đa 2 cơ hội × 2 ứng viên | ≤ $25 |

Trần: $40.

**Cổng quyết định:** bước 1 là phép thử rẻ xem guardrail có tác động không. Nếu agent đã tự chạy test ở hầu hết lượt (đọc từ log) thì guardrail này không còn tác dụng; khi đó chuyển sang dấu hiệu khác trước khi chạy bước 2.

**Ước lượng công:** H1–H2 khoảng 2–3 ngày, H3–H4 khoảng 2 ngày.

---

## Thứ tự và tổng chi phí

1. E1–E5 và kiểm thử xác định E6 (không tốn token): **đã hoàn thành**.
2. E: chạy thực nghiệm đo hiệu quả, trần $30 (cần duyệt trước khi chạy).
3. H1–H2: viết mã và kiểm thử (không tốn token; có thể làm độc lập với bước 2).
4. H: đo bước 1, trần $13 (cần duyệt).
5. H3–H4: viết mã, rồi đo bước 2 của H, trần $25 (cần duyệt).

Tổng trần cả hai kế hoạch: khoảng $70. Mỗi bước tốn tiền đều cần người dùng duyệt trước.

## Ràng buộc chung

- Tập test không bao giờ là đầu vào cho bất kỳ agent nào sinh đề xuất hay bài học.
- Script và guardrail `deny` luôn chờ người duyệt. Agent không duyệt được bất kỳ thành phần quy trình nào (`requireHuman`).
- Không push lên remote. Báo cáo kết quả chỉ nêu số liệu benchmark, không nêu thông tin repository đo.
- **Kỳ vọng thực tế:** với agent mạnh trên tác vụ sửa lỗi nhỏ, hiệu ứng có thể chỉ vài điểm %, dưới mức 30–70 tác vụ phát hiện được. Nên đo E và H với model thực thi rẻ hơn (Haiku/Sonnet) và model đề xuất mạnh hơn (Opus), vì đó là cấu hình có nhiều chỗ để cải thiện nhất.
