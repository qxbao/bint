# Xuất bản skill gốc

## Sub-features

- Skill gốc (kể cả tệp phụ lồng thư mục: `tdd/tests.md`, `diagnosing-bugs/scripts/hitl-loop.template.sh`) xuất bản ra `.claude/skills` và
  `.agents/skills`; `.agents` bỏ `disable-model-invocation` khỏi frontmatter.
- Sửa tay một tệp đã xuất bản → `bint sync` báo xung đột, không ghi đè; `--force` ghi đè có sao lưu.
- `bint skill list` liệt kê phiên bản, `core.lock`, trạng thái từng nơi xuất bản.

## How to get to it (user POV)

`bint init` (lần đầu) hoặc `bint sync` (sau khi nâng cấp Bint); `bint skill list` để xem.

## Driving it with vb.sh

1. `vb.sh run <RUN> skills -- skill list` → mọi dòng `current` ở cả hai nơi.
2. `ls <RUN>/work/.claude/skills/tdd <RUN>/work/.agents/skills/diagnosing-bugs/scripts` → có `tests.md`, `mocking.md`, `hitl-loop.template.sh`.
3. `echo x >> <RUN>/work/.claude/skills/tdd/tests.md`, rồi `vb.sh run <RUN> aux-conflict -- sync` → dòng `✗ skill tdd: .claude/skills/tdd/tests.md —
   tệp phụ của skill gốc đã bị sửa ngoài Bint`, `[exit 1]`; tệp vẫn còn dòng `x`.
4. `vb.sh run <RUN> aux-force -- sync --force` → tệp về bản gốc, có đường dẫn sao lưu.

## Gotchas

- Bit thực thi của script chỉ quan sát được trên Unix; Windows không có.
