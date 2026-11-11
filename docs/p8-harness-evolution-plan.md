# Kế hoạch: tiến hoá quy trình và bài học từ thất bại

Ngày: 2026-11-11. Mở rộng P8 theo hai ý học từ các nghiên cứu tối ưu harness.

1. **Kế hoạch E — Bài học từ thất bại.** Mỗi lượt thất bại được tóm tắt, rồi rút thành bài học có phạm vi rõ ràng. Thư viện bài học tích luỹ dần, tương tự `EXPERIENCE.md`.
2. **Kế hoạch H — Sửa được quy trình.** Ngoài tri thức và skill, vòng tiến hoá được đề xuất thay đổi:
   - chỉ dẫn dùng công cụ;
   - guardrail;
   - script hỗ trợ;
   - tham số chọn ngữ cảnh.

   Mỗi thay đổi thuộc một trong hai loại: **chung** (mọi vùng mã) hoặc **riêng** (một vùng mã).

Nên làm E trước. E tận dụng gần như toàn bộ hạ tầng đã có: mục tri thức loại `lesson`, đề xuất, đối chiếu, hook theo đề bài. E cũng là một thành phần ("bộ nhớ") của H. Bộ khung phân loại chung/riêng trong H dùng lại được cho bài học của E.

## Hiện trạng: Bint đang điều khiển được gì

| Thành phần quanh agent | Có trong Bint chưa | Vòng tiến hoá sửa được chưa |
|---|---|---|
| Tri thức (fact, rule, warning, pattern, decision, lesson) | Có | Có (L1) |
| Skill | Có (skill tiến hoá) | Có (L2), luôn chờ người duyệt |
| Bộ nhớ kinh nghiệm từ thất bại | Chỉ có loại mục `lesson`, chưa có nguồn sinh tự động | Chưa |
| Chỉ dẫn dùng công cụ | Văn bản cố định trong mã (ghi chú MCP) | Chưa |
| Guardrail | Chỉ có `protected_paths` (hỏi/chặn khi sửa) | Chưa |
| Script hỗ trợ | Chưa | Chưa |
| Chiến lược chọn ngữ cảnh | Có tham số và nhánh biến thể `bint@…` | Đo được, chưa đề xuất được |

---

## Kế hoạch E — Bài học từ thất bại

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

**Cơ hội mới `failure_lesson`:** sinh ra khi một nhóm có từ 3 lượt thất bại trở lên.

### E2. Rút bài học (tốn token, model cấu hình riêng)

- Thêm vai trò mới `roles.reflector`. Mặc định dùng model mạnh nhất có sẵn, ví dụ Opus, tách khỏi model thực thi.
- Đầu vào: bản tóm tắt của một nhóm thất bại, kèm các bài học sẵn có cùng phạm vi để tránh trùng.
- Đầu ra theo khuôn cố định:

  ```yaml
  scope: global | domain:<glob>
  trigger: <khi nào áp dụng — tình huống, không phải tác vụ cụ thể>
  mistake: <agent đã làm sai gì>
  instead: <nên làm gì>
  evidence: [<attempt/run id>...]   # chính các lượt thất bại trong nhóm
  ```

- Agent viết thành 2 ứng viên, tương tự cách tách "chung" và "riêng" của nghiên cứu harness:
  - **bài học chung:** điểm yếu chung của agent, ví dụ "chạy test của module trước khi kết thúc";
  - **bài học riêng:** quy trình hay bẫy của vùng mã đó.

  Cả hai đi tiếp sang bước kiểm tra.

### E3. Kiểm tra xác định (không tốn token)

1. **Rò rỉ.** Với lượt eval, so bài học với diff lời giải của các tác vụ làm bằng chứng. Bài học có chứa định danh (tên hàm, hằng, chuỗi) xuất hiện trong phần **thêm mới** của diff thì bị loại. Bài học là "cách làm", không được là "đáp án".
2. **Tổng quát.** Bài học chung phải có bằng chứng từ ít nhất 2 vùng mã khác nhau. Bài học riêng phải có ít nhất 2 lượt thất bại trong vùng của nó.
3. **Trùng.** Dùng cơ chế phát hiện mục trùng đã có. Nếu trùng thì đề xuất gộp thay vì tạo mới.
4. **Độ dài và hình thức.** Trigger, mistake và instead đều phải có; tổng không quá khoảng 80 từ.
5. **Nguồn gốc.** Mục ghi `origin: agent`, `provenance: failure_lesson` và danh sách lượt chạy làm bằng chứng. Quy tắc đã có vẫn áp dụng: bài học rút từ tác vụ X không được dùng khi chấm lại X.

### E4. Đề xuất, đối chiếu, áp dụng

- Mỗi bài học qua kiểm tra trở thành một đề xuất L1, loại `create lesson`.
- Đề xuất đi đúng vòng `bint evolve cycle`: validation trước, test sau, có dừng sớm, và tự bác bỏ nếu không đạt.
- Một bài học **đạt trên tập test độc lập** có bằng chứng rõ là tổng quát hoá được, vì nó rút ra từ validation mà vẫn giúp được trên test.
- Áp dụng tự động vẫn theo quy tắc hiện có: chỉ khi rủi ro thấp, chỉ tạo mục mới, mục vào trạng thái candidate, và phải bật `auto_low_risk`.

### E5. Thư viện và cách đưa vào ngữ cảnh

- **`bint experience show`:** hiển thị thư viện như một `EXPERIENCE.md`. Bài học chung xếp trước, bài học riêng nhóm theo vùng. Mỗi bài học kèm số lần được chèn và số lần đạt/trượt khi có nó.
- **Không chèn cả tệp.** Hook theo đề bài:
  - luôn chèn top-k bài học *chung*, mặc định k=3;
  - chèn bài học *riêng* khi đề bài hoặc đường dẫn khớp phạm vi.

  Hai phần có ngân sách token riêng để bài học không chiếm chỗ của tri thức.
- **Vòng đời:**
  - Bài học không được chèn sau N lượt, hoặc gắn với tỷ lệ đạt thấp hơn khi không có nó, trở thành cơ hội `unused`/`deprecate` (đã có sẵn).
  - Bài học trùng sinh cơ hội gộp (đã có sẵn).
  - Như vậy thư viện không phình mãi, khác với một điểm yếu đã được ghi nhận ở phương pháp gốc.

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

**Ước lượng công:** khoảng 1,5–2 ngày viết mã.

---

## Kế hoạch H — Sửa được quy trình, tách chung/riêng

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

1. E1–E3: mã và kiểm thử, không tốn token.
2. E: đo, trần $30. Cần duyệt.
3. H1–H2: mã, không tốn token. Có thể làm song song với bước 2.
4. H: đo bước 1, trần $13. Cần duyệt.
5. H3–H4: mã, rồi H đo bước 2, trần $25. Cần duyệt.

Tổng trần cả hai kế hoạch: khoảng $70. Mỗi bước tốn tiền đều cần người dùng duyệt trước.

## Ràng buộc chung

- Tập test không bao giờ là đầu vào cho bất kỳ agent nào sinh đề xuất hay bài học.
- Script và guardrail `deny` luôn chờ người duyệt. Agent không duyệt được bất kỳ thành phần quy trình nào (`requireHuman`).
- Không push lên remote. Báo cáo kết quả chỉ nêu số liệu benchmark, không nêu thông tin repository đo.
- **Kỳ vọng thực tế:** với agent mạnh trên tác vụ sửa lỗi nhỏ, hiệu ứng có thể chỉ vài điểm %, dưới mức 30–70 tác vụ phát hiện được. Nên đo E và H với model thực thi rẻ hơn (Haiku/Sonnet) và model đề xuất mạnh hơn (Opus), vì đó là cấu hình có nhiều chỗ để cải thiện nhất.
