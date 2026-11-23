# Nguồn gốc

- pstack của Lauren Tan (https://github.com/cursor/plugins/tree/main/pstack) @ `7a58391` (2026-11-21), giấy phép MIT (`LICENSE`):
  `skills/maintain-verification-skill/`.

## Sửa đổi so với bản gốc

- Skill sinh ra là skill tuỳ biến của Bint: `.bint/skills/verify-<app>/` (thay cho `.cursor/skills/`), xuất bản bằng `bint sync`, kiểm bằng
  `bint skill lint`; frontmatter chỉ dùng khoá Bint cho phép.
- Lái CLI/TUI: thêm lựa chọn chạy được trên Windows (tmux không có native).
- Không mở PR: sửa đổi đã chứng minh đi thành một commit hoặc một phiếu `/b-do`; agent con chỉ dùng ngoài `bint run`.

Kế hoạch và lý do: `docs/mattpocock-skills-plan.md` (mục 4b, GĐ4b), ADR `docs/adr/0001-mattpocock-skills.md`.
