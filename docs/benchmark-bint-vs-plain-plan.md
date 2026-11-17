# Kế hoạch: benchmark Bint so với không dùng Bint

Trạng thái: **đã chạy 2026-11-16.** Biên bản: [evals/bench-bint-vs-plain-20261116/README.md](evals/bench-bint-vs-plain-20261116/README.md).

## 1. Mục tiêu

Trả lời một câu: trên cùng một repository, cùng model, cùng lời yêu cầu đại khái, dùng Bint (`b-do`) có tốt hơn dùng
agent trực tiếp không, xét theo ba mặt:

- **Tốn kém**: token (input, cached, output).
- **Thời gian**: từ lúc bắt đầu tới khi xong, kể cả số lần cần người can thiệp.
- **Kết quả**: thay đổi có đúng yêu cầu không, chất lượng mã ra sao.

Đây là phép đo hiệu quả để quyết định lên production. Các mục "đo cho biết" trong `acceptance.md` không thuộc phạm vi này.

## 2. Thiết kế

| | Bên A (không Bint) | Bên B (Bint) |
|---|---|---|
| Repository | Clone repo bint ở commit gốc, gỡ hết dấu vết Bint (mục 3) | Clone repo bint ở cùng commit, Bint đã dùng sẵn (tri thức `.bint/knowledge`) |
| Cách giao việc | `codex exec` với prompt đại khái | `bint run b-do` với cùng prompt; planner, implementer, reviewer đều là Codex |
| Model | `gpt-6.1-sol` qua codex-lb (4 tài khoản) | Như bên A |
| Điều phối | Opus (phiên Claude Code) đóng vai người dùng | Opus, có quyền tự làm thao tác chỉ-người |
| Số lần | 1 lần/task | 1 lần/task |

Hai task × hai bên = 4 lượt, chạy song song (mỗi lượt một tài khoản Codex).

Vì mỗi bên chỉ chạy một lần, kết quả là **tín hiệu**, không phải kết luận thống kê. Chênh lệch nhỏ coi như hoà (mục 7).

## 3. Chuẩn bị repository

Cả hai clone từ cùng một commit, mỗi bên một nhánh `bench`.

**Bên A, gỡ khỏi repo và commit thành một commit "gỡ Bint":**

- `.bint/`, `.agents/`, `.claude/`, `.codex/`, `.mcp.json`
- `AGENTS.md`, `CLAUDE.md` (toàn bộ là khối do Bint sinh)
- khối `# bint:begin … # bint:end` trong `.gitignore`

Giữ nguyên README và `docs/`: một dự án không dùng Bint vẫn có các tài liệu này.

**Bên B:**

- `bint init --hooks`, sinh hook Codex.
- Đóng băng binary: build `bint` từ chính commit gốc vào `bin/` riêng rồi đặt lên đầu `PATH`. Không dùng binary đang cài, vì
  binary đó build từ repo chính đang có thay đổi dở của phiên khác.
- `bint mcp index codegraph`: một phần của bộ Bint mà người dùng thật sẽ có.
- Tin cậy project và hook Codex trong `~/.codex/config.toml`. Bên A cũng được đánh dấu tin cậy project để hai bên ngang nhau.

**Chung cho hai bên:**

- Cấu hình Codex toàn cục của máy (MCP, plugin, hook người dùng) giữ nguyên như người dùng bình thường. Ghi rõ trong biên bản.
- Trước khi chạy: `go test ./...` trên cả hai bản phải xanh. Nếu bản A đỏ vì gỡ Bint thì sửa ở bước chuẩn bị, không để agent
  gặp lỗi có sẵn.

## 4. Hai task

Lấy từ `docs/run-predictability-plan.md`: việc thật, chưa ai làm, có đặc tả và mục nghiệm thu. Tên khoá và tên lệnh chốt sẵn trong
prompt để test ẩn kiểm được kiểu hộp đen qua CLI.

| Task | Nội dung | Công sức theo kế hoạch | Prompt (giống nhau cho hai bên) |
|---|---|---|---|
| Trung | GĐ3: đo kích thước khối xuất bản vào `AGENTS.md`, cảnh báo khi vượt trần ở `bootstrap publish --dry-run` và `doctor` | S | "Làm GĐ3 trong docs/run-predictability-plan.md, khoá đặt tên knowledge.publish.max_bytes." |
| Lớn | GĐ2a + GĐ2b: lệnh chỉ đọc `bint task progress [--json]` và chế độ bóng trong Runner (`workflow.no_progress.mode`) | S–M + S, đụng Runner, `state.json`, `task show`, `run status` | "Làm GĐ2a và GĐ2b trong docs/run-predictability-plan.md, lệnh tên bint task progress." |

Thiên lệch đã biết: cả hai task đụng chỗ mà tri thức của Bint có quy tắc (thêm khoá config phải sửa đủ bốn chỗ, thêm lệnh CLI phải
cập nhật `cliRefs`). Đó là lợi thế hợp lệ của Bint, vì tri thức học từ lịch sử chung chứ không chứa đáp án, nhưng cần ghi vào
biên bản.

## 5. Giao thức chạy

**Bên A:**

1. `codex exec` trong repo A với đúng prompt ở mục 4.
2. Khi agent báo xong, Opus làm như một người dùng bình thường: chạy `go build ./... && go test ./...`.
3. Nếu đỏ, gửi một câu chung chung (`codex exec resume`): "build/test đỏ, sửa đi". Tối đa 2 lần. Không gợi ý cách sửa.

**Bên B:**

1. `bint run b-do --driver process` với cùng prompt, `--autonomy auto`.
2. Câu hỏi mở của planner: Opus trả lời ở mức thông tin ngang bên A, tức là chỉ nhắc lại yêu cầu hoặc nói "tự quyết theo
   kế hoạch", không thêm chi tiết kỹ thuật.
3. Thao tác chỉ-người (`approve`, `land`…): Opus tự làm bằng `env -u CLAUDECODE -u AI_AGENT` (đã được chủ dự án cho phép trong
   benchmark). Mỗi lần tính là một lần can thiệp của người.

**Chung:**

- Trần thời gian mỗi lượt: 90 phút. Hết giờ thì dừng, chấm phần đã có.
- Không sửa tay mã của agent.
- Ghi lại mọi lần can thiệp: thời điểm, nội dung.

## 6. Cách đo

**Tốn kém.**

- Tổng token theo rollout Codex của mọi phiên thuộc lượt đó.
- Bên B: cộng mọi bước (planner, implementer, reviewer, các vòng sửa).
- Quy USD theo bảng giá công bố của model, nếu anh cần.

**Thời gian.** Thời gian tường từ đầu tới khi xong, cộng số lần và thời gian người can thiệp.

**Kết quả, phần chính: test ẩn.**

- Opus viết test ẩn **trước khi chạy**, từ mục "Nghiệm thu" và "Kiểm thử" của kế hoạch. Test để ngoài cả hai repo; anh duyệt
  trước khi chạy.
- Test chạy trên binary build từ từng bản, với fixture riêng và thư mục trạng thái tạm (`XDG_STATE_HOME`).

Chung cho cả hai task:

- `go build`, `go vet ./...`, `GOOS=windows go vet ./...`, `go test ./...` đều xanh. Đây là quy tắc bắt buộc của repo.

Task trung (GĐ3):

1. Trần thấp: `bootstrap publish --dry-run` thoát 0, in kích thước và trần, có cảnh báo.
2. Trần thấp: `doctor` có cảnh báo.
3. Trần cao, hoặc `null`: không cảnh báo.
4. Không đặt khoá: không cảnh báo trên chính repo này. Kế hoạch yêu cầu mặc định không được cảnh báo nhầm.
5. Nội dung `AGENTS.md` sau `publish --yes` giống hệt bản do binary gốc tạo, kể cả khi trần thấp: không tự cắt.
6. Giá trị sai (số âm) bị `Validate` từ chối.

Task lớn (GĐ2a + GĐ2b). Fixture dựng qua CLI: phiếu X có hai vòng review giống nhau, cây không đổi, kiểm chứng đỏ cùng đầu ra
(chỉ khác thời lượng); phiếu Y có hai vòng mà cây và đầu ra đều đổi; phiếu Z chưa có vòng nào.

1. `bint task progress` và `--json` thoát 0. JSON hợp lệ, có đủ ID của ba phiếu.
2. Phiếu X có tín hiệu `no_change` hoặc `same_failure`. Phiếu Y không có.
3. Phiếu Z không làm lệnh lỗi.
4. Chỉ đọc: tệp của phiếu và CSDL không đổi sau khi chạy lệnh.
5. `workflow.no_progress.mode` nhận `off`, `shadow`, `stop`; giá trị lạ bị từ chối; mặc định `off`.
6. `TestRenderMatchesDefaults` xanh: đã nằm trong `go test`.

Phần runtime của chế độ `shadow` khó kiểm hộp đen, nên giao cho người chấm (dưới đây) đối chiếu với đặc tả.

**Kết quả, phần phụ: chấm mù.**

- Một agent Opus mới, không phải phiên điều phối, nhận cho mỗi task:
  - đặc tả,
  - hai diff đã xoá dấu vết (không có tệp `.bint/`, tên phiếu, tên bên; gán nhãn ngẫu nhiên X/Y),
  - kết quả test ẩn.
- Chấm theo thang 1–5 cho: đúng đặc tả, chất lượng mã, test kèm theo, phạm vi thay đổi (có sửa thừa không).
- Phiên điều phối không tự chấm, vì biết bên nào là bên nào.

## 7. Đọc kết quả

Mỗi task, so ba mặt:

- **Kết quả**: số test ẩn qua là chính, điểm chấm mù là phụ.
- **Tốn kém và thời gian**: chênh dưới khoảng 25% coi như hoà, vì một lần chạy dao động nhiều.

Kết luận chung có ba khả năng:

| Kết luận | Điều kiện |
|---|---|
| Bint có lợi rõ | Kết quả tốt hơn ở cả hai task, chi phí không tăng quá rõ |
| Đánh đổi | Kết quả tốt hơn nhưng tốn hơn rõ, hoặc ngược lại. Ghi tỷ lệ |
| Không có tín hiệu | Kết quả hoà |

Nếu kết luận là "Đánh đổi" hoặc "Không có tín hiệu", có thể chạy thêm lần lặp (mỗi lần ước $40–50 token) trước khi quyết.

## 8. Rủi ro và thiên lệch

- Một lần chạy: dao động của model có thể lớn hơn hiệu ứng của Bint.
- Lợi thế tri thức: xem mục 4.
- Bên B tự duyệt hoàn toàn: không đo được chi phí duyệt thật của người.
- Hook Codex của Bint có thể không chạy (`NT-CODEX-HOOK`): khi đó bên B mất phần kiểm phạm vi khi sửa tệp. Kiểm ngay trong
  lượt và ghi lại.
- Codex qua codex-lb chuyển giữa các tài khoản: cached tokens có thể khác nhau giữa các lượt.
- Repo đích chính là mã nguồn Bint: thay đổi của bên B đụng tới mã mà chính binary đang chạy dựa vào. Binary đã đóng băng nên
  không ảnh hưởng lượt chạy.

## 9. Thời gian và chi phí ước tính

| Việc | Thời gian | Ai |
|---|---|---|
| Chuẩn bị repo, viết test ẩn | ~40 phút | Opus |
| Anh duyệt plan và test ẩn | ~10 phút | Người |
| Chạy 4 lượt song song | ~60–90 phút | Codex, Opus giám sát |
| Chạy test ẩn, chấm mù, viết biên bản | ~30 phút | Opus + agent chấm |

Token Codex: 4 lượt, bên B tốn nhiều hơn vì có planner và reviewer. Đi qua codex-lb, không trả USD trực tiếp. Phiên điều phối
và agent chấm (Claude Opus): ước $5–10.

## 10. Chờ chủ dự án quyết

1. Duyệt cặp task và đúng hai câu prompt ở mục 4.
2. Duyệt giao thức nhắc lại của bên A: tối đa 2 câu chung chung.
3. Trần 90 phút mỗi lượt.
4. Nơi lưu biên bản: `docs/evals/bench-bint-vs-plain-<ngày>/`.
