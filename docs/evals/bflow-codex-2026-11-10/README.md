# Pilot Codex thật trên Bflow — 2026-11-10

**Trạng thái: đã hoàn tất pilot Codex thật; chưa đủ điều kiện nghiệm thu gate 1, gate 2 chưa mở chính thức.**

## Phạm vi

- Bflow HEAD: `9d071abe858606b526cffcb087a233763df0a956`.
- Bint: `b5896a4c41737f5eea953f0018c400b432d30eaa`.
- Codex CLI 0.167.0, model yêu cầu `gpt-6.1-sol`, effort medium, đăng nhập ChatGPT. API-key env bị bỏ khỏi tiến trình đo; cấu hình Codex/HOME riêng, không đổi cấu hình người dùng.
- Ngân sách API trả thêm do chủ dự án chỉ định: 0 USD. Chi phí USD mà harness không báo vẫn là null.
- Đây là pilot tập validation: 2 tác vụ, 3 lần lặp mỗi nhánh. Không phải phép đo chính thức trên 20 tác vụ test/repository và 2 repository khác hệ sinh thái.

## Chuẩn bị và corpus

Repository có 701 tệp test, 1.403 commit tính cả merge. `bint eval mine --json` quét 1.104 commit không phải merge và tìm 562 ứng viên. Đợt pilot chọn các thay đổi thư viện nhỏ để kiểm tra quy trình trước khi mở rộng; không đại diện cho toàn bộ độ khó Bflow.

Hai tác vụ hợp lệ có kiểm thử thất bại ở commit cha và qua hai lần ở lời giải thật. Một ứng viên khác bị loại vì lockfile lịch sử thiếu importer; một ứng viên bị loại vì test của chính lời giải thật lỗi EACCES. Không sửa lockfile/test để buộc chúng qua. Xem [verification.json](verification.json).

Dựng dependencies bằng `pnpm install --frozen-lockfile --ignore-scripts`, build bằng `pnpm exec tsc -b --pretty false`. Lệnh chấm build lại và chạy Vitest đúng các tệp test của tác vụ, với 1–2 worker; không dùng toàn bộ suite 701 tệp làm thước đo cho từng tác vụ.

Bản sao HEAD hiện tại qua dependency check/build và 77/77 test ở hai tệp smoke. Xem [readiness.json](readiness.json) và [readiness-tests.json](readiness-tests.json). Không suy rộng kết quả này thành toàn bộ suite đã qua.

## Baseline P1

Đợt `eb_01M725QA1H0CWBW1EBNWQWJK98` hoàn tất 6/6 lượt bằng Codex thật, nhánh none, strict:

- Đạt 3/6 lượt, tỷ lệ trung bình theo tác vụ 50%, CI95 [0%, 100%].
- 0 lỗi hạ tầng, 0 timeout, 0 skipped.
- Độ lệch chuẩn trong từng tác vụ qua 3 lần lặp là 0 trong mẫu này; không chứng minh agent không có dao động nói chung.
- Chi phí USD và num_turns không được Codex cung cấp.

Báo cáo gốc: [p1-baseline.json](p1-baseline.json).

Một tác vụ không đạt ở phép so chuỗi diagnostic chính xác: đầu ra agent có expected/declaration/reasons nhưng khác câu chữ test yêu cầu. Đây là phát hiện cần chủ dự án duyệt ở cặp đề bài–oracle, không tự đổi điểm hoặc làm yếu test. Xem [oracle-review.json](oracle-review.json).

## Bootstrap strict

Codex thật tạo 2 snapshot tại đúng commit cha, mỗi snapshot 4 mục. Tám mục đều candidate; 21 bằng chứng đều dùng fingerprint lines. Xem [bootstrap-summary.json](bootstrap-summary.json) và [snapshot-fingerprints.json](snapshot-fingerprints.json).

CLI `eval bootstrap` hiện chưa có lựa chọn harness; đợt này dùng đường `--agent-command` với wrapper gọi Codex thật. Vì vậy metadata bootstrap gốc ghi harness `command`, còn baseline/đợt so sánh dùng adapter native và ghi `codex`. Wrapper giữ cost/num_turns thiếu, không tổng hợp số 0. Model, phiên bản Codex ngoài wrapper và băm launcher được ghi trong protocol/provenance.

## Điều kiện gate

Các ngưỡng trong [gate 1](../../gate-1.md) và [gate 2](../../gate-2.md) được giữ nguyên. Gate 1 chưa đủ điều kiện nghiệm thu chính thức; gate 2 chưa được mở chính thức. So sánh nhánh bint trong pilot là thăm dò kỹ thuật, không thay thế điều kiện mở cổng.

Các giới hạn hiện có: một repository, hai tác vụ validation, thiếu cost/num_turns của Codex; nhánh bint của Codex có MCP + AGENTS.md nhưng không có path hook trong đánh giá. Không đổi thước đo sang token/thời gian sau khi thấy kết quả.

Quyết định chi tiết: [fingerprint và tín hiệu kết quả](../../fingerprint-outcome-decisions.md).

## Artifact

[Protocol](protocol.json), [ước tính](estimate.json), [điều kiện nghiệm thu](eligibility.json), [nguồn/provenance](source.json), [băm launcher](runner-hashes.json), [ngưỡng đã chốt](threshold-contracts.json). Các case đã chuẩn bị nằm trong [cases/](cases/), gắn reviewer tự động của đợt chuẩn bị, chưa được coi là tập test chính thức do chủ dự án duyệt.

Thí nghiệm chạy tại `/home/DUCLH/.local/state/bint/experiments/bflow-20261110T071615Z/`; SQLite/log chi tiết tại `/home/DUCLH/.local/state/bint/repo-d99e6fd112ce/`. Không đăng các transcript đầy đủ của agent vào repository báo cáo. Tệp test đang sửa của người dùng trong Bflow gốc được giữ nguyên.

## Kết quả so sánh đã hoàn tất

Đợt `eb_01M726DEZ2Q15V34AF085HMTWA` hoàn tất 18/18 lượt, cùng model/harness/seed, strict, mỗi tác vụ × nhánh × 3 lần lặp. Đây là một đợt thăm dò chung; hai báo cáo so sánh dùng mốc none và minimal. Không coi nhánh bint trong đợt này là đã mở gate 2 sau khi gate 1 được nghiệm thu.

| Nhánh | Đạt / lượt | Tỷ lệ đạt | CI95 tỷ lệ đạt theo tác vụ | Thời gian agent trung bình |
|---|---:|---:|---|---:|
| none | 3/6 | 50% | [0%, 100%] | 64.9 giây |
| minimal | 3/6 | 50% | [0%, 100%] | 59.2 giây |
| bint | 3/6 | 50% | [0%, 100%] | 69.1 giây |

Không có lỗi hạ tầng theo trạng thái lượt chạy, timeout hoặc skipped; không có tiến trình Codex thoát lỗi. Các lượt không đạt là kết quả test chấm điểm, cần đọc cùng phát hiện về oracle ở trên. Agent có sửa/viết test trong 18 lượt; Bint áp lại đúng test của commit lời giải trước khi chấm, không lấy lời tự đánh giá của agent làm kết quả.

- `minimal − none`: chênh lệch tỷ lệ đạt 0 điểm %, CI bootstrap [0, 0]. Chưa có dữ liệu cost/num_turns để kiểm tra lợi ích hiệu quả theo gate 1.
- `bint − minimal`: chênh lệch tỷ lệ đạt 0 điểm %, CI bootstrap [0, 0]; không quan sát được lợi ích về tỷ lệ đạt trong pilot.
- Bint tốn thêm trung bình 9,9 giây thời gian agent so với minimal (CI theo hai tác vụ khoảng +2,5 đến +17,4 giây). Đây là chỉ số thăm dò, không được thay vào tiêu chí chi phí USD của gate.
- Khoảng CI chênh lệch [0,0] là hệ quả cả hai tác vụ có kết quả giống nhau ở các nhánh trong mẫu cực nhỏ; không chứng minh tương đương trên tác vụ mới/toàn repository.
- Báo cáo gốc không có cảnh báo chặn tự động, nhưng thiếu cỡ mẫu/repo/test split và chỉ số hiệu quả vẫn chặn nghiệm thu chính thức. Không dùng việc thiếu cảnh báo tự động như chứng nhận đã qua gate.

Báo cáo đầy đủ: [so minimal với none](gate1-pilot.json), [so bint với minimal](gate2-pilot.json), [tóm tắt](summary.json), [từng lượt](attempts.json), [quyết định gate](gate-decisions.json).

## Kiểm chứng MCP và giới hạn trích dẫn

Cả 6/6 lượt bint có bản ghi truy xuất; ghi nhận 6 lời gọi context_for_task và 8 context_for_paths thành công. Không có path hook trong nhánh Codex của đợt đánh giá. Số lượt có trích dẫn ghi nhận là 0/6; 4 lời gọi cite_knowledge bị chặn với lỗi `MCP tool call requires approval, but approval policy is never`. Hai lượt không có lời gọi cite thành công cũng không được suy ra là không áp dụng tri thức.

Xem [nhật ký truy xuất đã chuẩn hoá](context-usage.json) và [lỗi MCP đã trích](mcp-findings.json). Phần chẩn đoán tự động trả nguyên nhân undetermined do chưa có nhánh ideal. Không lấy số cited bằng 0 trong cấu hình bị chặn này để kết luận lỗi trình bày/agent không áp dụng tri thức. Đợt này không tự đổi quyền công cụ hoặc chạy thêm vòng chẩn đoán để hợp thức hoá gate.

## Quyết định và bước cần có trước phép đo chính thức

1. **P1:** đã chứng minh chạy agent thật, chấm test ẩn và tạo báo cáo lặp lại trên repository thật. Chưa đủ mẫu để ký nghiệm thu mục “tập tác vụ đủ lớn”.
2. **Gate 1:** chưa đủ điều kiện nghiệm thu; phần không kém về tỷ lệ đạt chỉ được quan sát trong pilot. Điều kiện giảm cost/num_turns chưa đo được.
3. **Gate 2:** chưa mở chính thức vì gate 1 chưa qua; pilot không cho thấy tăng tỷ lệ đạt, không kiểm chứng được giới hạn +25% USD, và cấu hình Codex còn thiếu path hook/quyền ghi citation.
4. **Fingerprint:** áp dụng quyết định D-FP-01 theo vùng dòng cho pilot TypeScript; giữ sai khác tree-sitter là deviation có giới hạn, không đánh dấu P3 hoàn thành nguyên văn.
5. **Tín hiệu:** D-SIG-01 giữ thiếu user diff-edit attribution là phần hoãn, không suy diễn từ commit/mtime; cited bị chặn không phải bằng chứng không áp dụng tri thức.

Trước phép đo chính thức: duyệt cặp đề bài–oracle; chuẩn bị tối thiểu 20 tác vụ test hợp lệ trên mỗi repository và thêm một repository khác hệ sinh thái; chốt cỡ mẫu từ dữ liệu phù hợp. Cần hoàn thiện/chốt riêng năng lực đo hiệu quả và quyền MCP của harness. Nếu muốn dùng token/thời gian thay USD/num_turns, phải chốt giao thức mới trước đợt đo mới trên dữ liệu chưa dùng để chẩn đoán; không sửa ngưỡng hồi tố từ pilot này.
