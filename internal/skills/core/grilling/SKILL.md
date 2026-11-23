---
name: grilling
description: Grill the user relentlessly about a plan, decision, or idea. Use when the user wants to stress-test their thinking, or uses any 'grill' trigger phrases.
---

Interview the user relentlessly until you reach a shared understanding. Map this as a **design tree**: every decision branches into the decisions that hang off it.

Work the tree in **rounds**. The **frontier** is every decision whose prerequisites are already settled: the questions you can ask _now_ without guessing at answers you haven't heard yet. Ask the whole frontier in one round: number each question and give your recommended answer. Then wait for the user's answers before the next round.

Format a round like so:

```
❓ **Q1** - **<question title>**: <question body, might be multiple paragraphs, including multiple choices>

➡️ <your recommended answer>

---

❓ **Q2** - **<question title>**: <question body, might be multiple paragraphs, including multiple choices>

➡️ <your recommended answer>
```

Word each question so "yes" accepts your recommended answer.

Each round the user answers reshapes the tree: settled decisions push the frontier outward and unblock questions that depended on them. Recompute the frontier and ask the next round. A question whose answer depends on another question still open in this round belongs to a _later_ round, not this one.

Finding _facts_ is your job, never the user's. When a frontier question needs a fact from the environment (filesystem, tools, etc.), dispatch a sub-agent with a fresh context window to find it; don't ask the user for anything you could look up yourself. Don't block on it: a running exploration is an unsettled prerequisite, so only the questions downstream of it wait for the sub-agent to report; ask the rest of the frontier now. The _decisions_ are the user's: put each to them and wait.

The session is done when the frontier is empty: every branch of the design tree visited, nothing left silently assumed. Do not act on it until the user confirms you have reached a shared understanding.

## Khi chạy dưới Bint

- **Trong một bước `bint run`** (đề bài mở đầu bằng khối `<bint-task …>`): không sinh agent con; tự làm mọi việc ngay trong phiên (quy tắc 0 agent con ở bước implement, `docs/mattpocock-skills-plan.md` mục 3.2). Phạm vi sửa, lệnh kiểm chứng và cổng duyệt của Bint luôn thắng chỉ dẫn của skill này.
- Đọc `GLOSSARY.md` ở gốc repo (hoặc `GLOSSARY-MAP.md` khi có nhiều ngữ cảnh) nếu có, để dùng đúng từ vựng miền.
- Áp dụng một mục tri thức của dự án (`[k:<id>]`) thì báo bằng công cụ `cite_knowledge`.
- Trong `bint run` (bước không tương tác): tự tra sự thật ngay trong phiên thay vì gửi agent con. Câu hỏi cho người ghi thành mục `Q-N` trong `requirement.md`, mỗi câu kèm dòng `Đề xuất: …` (đáp án bạn khuyên, để "có" nghĩa là chấp nhận đề xuất); chỉ hỏi về hướng sản phẩm, không hỏi điều tự kiểm được. Câu hỏi phụ thuộc câu khác chưa trả lời để sang vòng sau (Bint nối tiếp phiên của bạn sau khi người trả lời).
- Phiên tương tác (`/grill-with-docs`, phiên điều phối `/b-do`): làm đúng như trên, theo vòng; được dùng agent con để tra sự thật.
