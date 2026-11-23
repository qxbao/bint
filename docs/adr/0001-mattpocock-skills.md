---
status: accepted
---

# Nhập kỷ luật từ mattpocock/skills và pstack thẳng vào skill gốc

Bint nhập **phần kỷ luật** của bộ skill Matt Pocock ([mattpocock/skills](https://github.com/mattpocock/skills) @ `abeeaf9`) và pstack
của Lauren Tan ([cursor/plugins/pstack](https://github.com/cursor/plugins/tree/main/pstack) @ `7a58391`), cả hai giấy phép MIT, và **bỏ
phần điều phối** (spec → ticket → implement đã có b-req → `G-N` → b-feat cùng các cổng kiểm chứng, phạm vi, duyệt người). Kế hoạch đầy
đủ: [docs/mattpocock-skills-plan.md](../mattpocock-skills-plan.md).

Quyết định (khó đảo ngược vì mọi dự án chạy `bint sync` đều nhận skill gốc, và tên skill gốc thành tên cấm cho flow/skill tuỳ biến):

- **Đi thẳng vào skill gốc nhúng** (`internal/skills/core/`), không qua lớp phủ hay `.bint/skills/` của một repo: Bint chưa có người
  dùng ngoài. Số đo GĐ5 là căn cứ giữ/gỡ từng thành phần, không phải cổng vào. Skill gốc vì thế có **tệp phụ** (xuất bản cạnh
  `SKILL.md`, băm từng tệp trong `core.lock`, sửa tay → xung đột như `SKILL.md`).
- **Tên giữ như bản gốc** (`grilling`, `tdd`, …, không tiền tố `b-`) để câu "Call the Skill tool with …" trong các skill khác vẫn đúng.
  Ví dụ flow `tdd` trong tài liệu/test đổi thành `testfirst`.
- **Hỏi-đáp với người: A + B.** (A) grilling trước khi lập phiếu, trong phiên tương tác (`/grill-with-docs`, `/b-do`); (B) vòng `Q-N` có
  `Đề xuất:` trong b-req, `bint run resume` nối tiếp phiên planner. Không dùng terminal multiplexer (zellij/tmux).
- **0 agent con ở bước implement** (b-feat, b-fix, b-repro); bước chỉ đọc chưa bật agent con (căn cứ: bench v3, agent con ≈ 25 % token
  không đổi chất lượng). Ngoài `bint run` skill dùng agent con như bản gốc.
- **`GLOSSARY.md` ở gốc repo, sửa trực tiếp** (không qua duyệt tri thức): cùng mô hình tin cậy "review qua Git" như skill tuỳ biến và
  lớp phủ.

Quy ước kèm theo:

- **Giấy phép**: mỗi thư mục skill nhập có `LICENSE` (MIT, bản gốc) và `UPSTREAM.md` (repo, đường dẫn, commit, danh sách sửa đổi).
- **Ngôn ngữ**: thân skill nhập giữ tiếng Anh nguyên văn; mục "Khi chạy dưới Bint" và phần viết lại cho Bint viết tiếng Việt.
- Skill do người gọi giữ `disable-model-invocation: true`; vì `.agents/skills` bỏ khoá này, chúng có thêm `agents/openai.yaml`
  (`policy.allow_implicit_invocation: false`) cho Codex.
