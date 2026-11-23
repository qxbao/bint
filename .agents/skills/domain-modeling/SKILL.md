---
name: domain-modeling
description: Build and sharpen a project's domain model. Use when discussing codebase terminology, writing or editing a GLOSSARY.md, or recording or editing an ADR.
---

# Domain Modeling

Actively build and sharpen the project's domain model as you design. This is the *active* discipline: challenging terms, inventing edge-case scenarios, and writing the glossary and decisions down the moment they crystallise. (Merely *reading* `GLOSSARY.md` for vocabulary is not this skill: that's a one-line habit any skill can do. This skill is for when you're changing the model, not just consuming it.)

## File structure

Most repos have a single context:

```
/
├── GLOSSARY.md
├── docs/
│   └── adr/
│       ├── 0001-event-sourced-orders.md
│       └── 0002-postgres-for-write-model.md
└── src/
```

If a `GLOSSARY-MAP.md` exists at the root, the repo has multiple contexts. The map points to where each one lives:

```
/
├── GLOSSARY-MAP.md
├── docs/
│   └── adr/                          ← system-wide decisions
├── src/
│   ├── ordering/
│   │   ├── GLOSSARY.md
│   │   └── docs/adr/                 ← context-specific decisions
│   └── billing/
│       ├── GLOSSARY.md
│       └── docs/adr/
```

Create files lazily: only when you have something to write. If no `GLOSSARY.md` exists, create one when the first term is resolved. If no `docs/adr/` exists, create it when the first ADR is needed.

## During the session

### Challenge against the glossary

When the user uses a term that conflicts with the existing language in `GLOSSARY.md`, call it out immediately. "Your glossary defines 'cancellation' as X, but you seem to mean Y. Which is it?"

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose a precise canonical term. "You're saying 'account': do you mean the Customer or the User? Those are different things."

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force the user to be precise about the boundaries between concepts.

### Cross-reference with code

When the user states how something works, check whether the code agrees. If you find a contradiction, surface it: "Your code cancels entire Orders, but you just said partial cancellation is possible. Which is right?"

### Update GLOSSARY.md inline

When a term is resolved, update `GLOSSARY.md` right there. Don't batch these up: capture them as they happen. Use the format in [GLOSSARY-FORMAT.md](./GLOSSARY-FORMAT.md).

`GLOSSARY.md` should be totally devoid of implementation details. Do not treat `GLOSSARY.md` as a spec, a scratch pad, or a repository for implementation decisions. It is a glossary and nothing else.

### Offer ADRs sparingly

Only offer to create an ADR when all three are true:

1. **Hard to reverse**: the cost of changing your mind later is meaningful
2. **Surprising without context**: a future reader will wonder "why did they do it this way?"
3. **The result of a real trade-off**: there were genuine alternatives and you picked one for specific reasons

If any of the three is missing, skip the ADR. Use the format in [ADR-FORMAT.md](./ADR-FORMAT.md).

## Khi chạy dưới Bint

- **Trong một bước `bint run`** (đề bài mở đầu bằng khối `<bint-task …>`): không sinh agent con; tự làm mọi việc ngay trong phiên (quy tắc 0 agent con ở bước implement, `docs/mattpocock-skills-plan.md` mục 3.2). Phạm vi sửa, lệnh kiểm chứng và cổng duyệt của Bint luôn thắng chỉ dẫn của skill này.
- Đọc `GLOSSARY.md` ở gốc repo (hoặc `GLOSSARY-MAP.md` khi có nhiều ngữ cảnh) nếu có, để dùng đúng từ vựng miền.
- Áp dụng một mục tri thức của dự án (`[k:<id>]`) thì báo bằng công cụ `cite_knowledge`.
- `GLOSSARY.md` sửa trực tiếp, không qua duyệt tri thức (cùng mô hình tin cậy "review qua Git" như skill tuỳ biến). Quyết định khó đảo ngược mà cần Bint truy xuất theo đường dẫn thì ghi thêm tri thức loại `decision` (`bint knowledge add --type decision …`, chờ người duyệt) trỏ vào tệp ADR.
- Trong `bint run`: chỉ sửa `GLOSSARY.md`/ADR khi chúng nằm trong phạm vi sửa (`scope.allow`); ngoài phạm vi thì nêu thuật ngữ đề xuất trong ghi chú bước (`requirement.md`/`plan.md` hoặc báo cáo).
