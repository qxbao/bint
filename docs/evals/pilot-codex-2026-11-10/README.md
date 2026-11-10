# Pilot Codex — 2026-11-10

**Trạng thái:** pilot hạ tầng đã hoàn tất. Chưa đủ điều kiện nghiệm thu gate 1; gate 2 chưa mở chính thức.

## Thiết lập

| Mục | Giá trị |
|---|---|
| Repository thử nghiệm | Repository A: monorepo TypeScript (pnpm, Vitest), ~1.100 commit không phải merge, ~700 tệp test |
| Bint | `b5896a4` |
| Harness | Codex CLI 0.167.0, mô hình `gpt-6.1-sol`, effort medium, đăng nhập thuê bao (không báo USD) |
| Tập | 2 tác vụ validation × 3 lần lặp mỗi nhánh; chế độ chống rò rỉ `strict` |
| Chấm | build lại rồi chạy đúng tệp test của tác vụ (1–2 worker) |

`bint eval mine` tìm được 562 ứng viên. Mẫu nhỏ gồm thay đổi thư viện nhỏ, không đại diện cho độ khó chung của repository. Hai tác vụ được dùng đều fail ở commit cha và pass hai lần với lời giải thật. Hai ứng viên khác bị loại: một do lockfile lịch sử hỏng, một do test của lời giải lỗi quyền tệp. Không sửa test hay lockfile để ép tác vụ qua kiểm chứng.

Bootstrap `strict` tạo 2 ảnh chụp tri thức tại đúng commit cha, mỗi ảnh 4 mục ở trạng thái candidate, với 21 bằng chứng dùng fingerprint theo vùng dòng.

## Kết quả

Đợt nền P1 (`eb_01M725QA1H0CWBW1EBNWQWJK98`), chỉ nhánh `none`: đạt 3/6 lượt, 0 lỗi hạ tầng, 0 timeout. Xem [p1-baseline.json](p1-baseline.json).

Đợt so sánh (`eb_01M726DEZ2Q15V34AF085HMTWA`), 18/18 lượt:

| Nhánh | Đạt / lượt | Tỷ lệ đạt | CI95 theo tác vụ | Thời gian agent TB |
|---|---:|---:|---|---:|
| none | 3/6 | 50% | [0%, 100%] | 64,9 s |
| minimal | 3/6 | 50% | [0%, 100%] | 59,2 s |
| bint | 3/6 | 50% | [0%, 100%] | 69,1 s |

- `minimal − none`: Δ tỷ lệ đạt 0 điểm %, CI [0, 0]. Không có cost hoặc num_turns để kiểm điều kiện hiệu quả của gate 1.
- `bint − minimal`: Δ 0 điểm %, CI [0, 0]. Nhánh bint chậm hơn +9,9 s (CI +2,5…+17,4 s). Đây chỉ là số thăm dò, không thay cho tiêu chí USD của gate.
- CI [0, 0] chỉ phản ánh mẫu quá nhỏ, không chứng minh hai nhánh tương đương.
- Truy xuất qua MCP có ở 6/6 lượt bint (6 `context_for_task`, 8 `context_for_paths`). Trích dẫn ghi được 0/6, vì 4 lời gọi `cite_knowledge` bị chính sách duyệt của harness chặn ([mcp-findings.json](mcp-findings.json)). Nhánh Codex không có path hook.
- Một tác vụ trượt vì so khớp nguyên văn chuỗi chẩn đoán. Cặp đề bài–oracle này cần người duyệt; không đổi điểm và không làm yếu test.

## Quyết định

- **P1:** đã chạy được agent thật, chấm bằng test ẩn và tạo báo cáo lặp lại. Chưa đủ cỡ mẫu.
- **Gate 1:** chưa nghiệm thu. Điều kiện không kém về tỷ lệ đạt mới chỉ quan sát trong pilot; điều kiện giảm cost/num_turns chưa đo được.
- **Gate 2:** chưa mở. Lý do: gate 1 chưa qua, không thấy tăng tỷ lệ đạt, thiếu USD, và cấu hình Codex thiếu path hook cũng như quyền citation.
- Ngưỡng trong [gate-1.md](../../gate-1.md) và [gate-2.md](../../gate-2.md) giữ nguyên. Không đổi thước đo hồi tố.

## Artifact

[summary.json](summary.json), [attempts.json](attempts.json) (số liệu từng lượt), [gate1-pilot.json](gate1-pilot.json), [gate2-pilot.json](gate2-pilot.json), [gate-decisions.json](gate-decisions.json), [estimate.json](estimate.json), [eligibility.json](eligibility.json), [threshold-contracts.json](threshold-contracts.json), [readiness-tests.json](readiness-tests.json).

Không đăng đề bài, mã nguồn, đường dẫn máy hay transcript của repository thử nghiệm.
