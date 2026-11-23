---
name: grill-with-docs
description: A relentless interview to sharpen a plan or design before it becomes a Bint ticket, writing the glossary and ADRs as we go and ending with a ready-to-run `bint task new` draft.
argument-hint: "Plan, idea or request to grill"
disable-model-invocation: true
---

# Grill with docs

Yêu cầu: $ARGUMENTS

Phiên **tương tác** để chốt một yêu cầu trước khi lập phiếu (cách A của `docs/adr/0001-mattpocock-skills.md`). Bạn phỏng vấn người dùng,
ghi thuật ngữ và quyết định ngay khi chúng thành hình, rồi dừng ở một **bản nháp phiếu**: người chạy nó (hoặc chuyển sang `/b-do`).

## Bước

1. Call the Skill tool twice, for "grilling" and "domain-modeling". Hai skill đó là luật của phiên: hỏi theo vòng, mỗi vòng trọn
   *frontier*, mỗi câu kèm đáp án đề xuất; tra sự thật là việc của bạn (đọc mã, `bint context`, MCP `cg_*`, agent con được phép ở đây);
   quyết định là của người.
2. Đọc `GLOSSARY.md` (hoặc `GLOSSARY-MAP.md`), ADR trong `docs/adr/`, và tri thức liên quan (`context_for_task`). Thuật ngữ được chốt →
   sửa `GLOSSARY.md` ngay; quyết định khó đảo ngược + gây bất ngờ + có đánh đổi thật → đề nghị ADR.
3. Trong vòng hỏi, đảm bảo frontier có đủ các nhánh mà phiếu Bint cần:
   - **kết quả mong muốn** từ góc người dùng và điều **ngoài phạm vi**;
   - **tiêu chí chấp nhận** kiểm được (mỗi tiêu chí một câu, có cách quan sát);
   - **seam kiểm thử**: seam cao nhất chạm được hành vi, càng ít càng tốt (lý tưởng là một); ưu tiên seam có sẵn;
   - **phạm vi sửa** (glob) và **lệnh kiểm chứng** nếu đã biết;
   - kế hoạch nhiều giai đoạn giao riêng → đề nghị phiếu cha + phiếu con (`--parent`, `--depends-on`).
4. **Hoàn thành khi** frontier rỗng (mọi nhánh của cây quyết định đã đi qua, không còn giả định ngầm) **và** người dùng xác nhận đã hiểu
   chung. Chưa đạt thì chưa viết bản nháp.
5. In bản nháp phiếu, chưa chạy:

   ```sh
   bint task new "<yêu cầu đã chốt, một đoạn>" --workflow b-do --plan [--depth deep] \
     --goal "<mục tiêu một dòng>" \
     --criteria "<tiêu chí 1>" --criteria "<tiêu chí 2>" \
     --clear-scope --allow '<glob>' [--allow '<glob>'] \
     [--constraint "Seam kiểm thử: <seam> — <bắt được gì / bỏ sót gì>"] [--verify-command '<lệnh>'] --json
   ```

   Kèm một đoạn ngắn: thuật ngữ/ADR đã ghi, câu hỏi nào đã chốt theo đề xuất, điều gì còn để b-req quyết.
6. Hỏi người dùng muốn (a) tự chạy lệnh trên, hay (b) chuyển sang `/b-do` với yêu cầu đã chốt. Không tự `bint task approve`, không tự xác
   nhận lệnh kiểm chứng: đó là việc của người.

## Ghi chú

- Không đăng issue lên tracker ngoài: Bint dùng kho phiếu riêng (`bint task`).
- Lệnh kiểm chứng chỉ chạy sau khi người xác nhận (`bint task approve`); bản nháp chỉ đề xuất nó.
- Seam kiểm thử đi tiếp vào `plan.md` (mục `## Seam kiểm thử`) khi b-req lập kế hoạch; ghi nó vào ràng buộc để b-req thấy.
