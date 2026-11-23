# Bản đồ kiểm chứng bint (CLI)

Nguồn chuẩn để kiểm hành vi người dùng thấy của `bint`. Đọc mục này trước khi lái, rồi dùng tệp tính năng tương ứng làm công thức.

## Điều kiện nền

- `vb.sh launch` (repo nháp `<RUN>/work` đã `bint init --harness claude,codex --no-hooks`), rồi `vb.sh doctor <RUN>`.
- Mọi lệnh qua `vb.sh run <RUN> <tên> -- …`; sửa tệp/`git` trực tiếp trong `<RUN>/work`.
- Không lái lượt kiểm không do chính lượt này `launch`.

## Tính năng

- [skills-publish.md](skills-publish.md): `bint init`/`bint sync` xuất bản skill gốc (kèm tệp phụ) và bảo vệ bản sửa tay.
- [knowledge.md](knowledge.md): `bint knowledge add` (gồm `--enforced-by`) và `bint knowledge audit --all` (tín hiệu).
- [tasks.md](tasks.md): `bint task new` / `show` / `lint` trên phiếu mới.
