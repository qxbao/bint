# Thay đổi fixture so với lần 2 (mục 4.7 của plan)

Lý do: bản gốc `8757340` có cổng "bước implement không đổi tệp nào thì dừng `blocked`". `AGENT_SLOW` của D5 chỉ ghi `slow.txt`
(nằm trong `.gitignore` của fixture) nên bị chặn → D5 đỏ giả ở đối chứng âm (`noop`/`naive`: lượt chạy exit=1 status=blocked).

Sửa (áp cho mọi bản kiểm, vì cùng một `hidden2.py`): `AGENT_SLOW` ghi thêm `slow_changed.txt` (không bị ignore, để cổng thấy có đổi tệp);
`skip_files` của các snapshot thêm `slow_changed.txt` (như `slow.txt`). Bản gốc lưu ở `hidden/hidden2.py.v2`.
Không đổi gì khác. T2 (S1–S6, N1, N2) không cần đổi.
