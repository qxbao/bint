planning-write-command.txt: lệnh Bash của bước b-req (chỉ đọc) trong phiên Claude Code 2.1.322 thật. Dòng đầu là tiền tố lệnh thật lấy từ sự kiện hook đã ghi (đã che đường dẫn binary); phần thân heredoc là requirement.md thật do lệnh đó ghi (toàn văn lệnh gốc không được lưu, hook chỉ giữ 300 ký tự đầu).

transcript-before-resume.jsonl / transcript-after-resume.jsonl: transcript chính của một phiên `claude -p` trước và sau `claude -p --resume <id>`
(chỉ giữ dòng mode/user/assistant.usage/cost-state; hội thoại đã lược, số liệu do tay dựng theo cấu trúc của transcript thật 2.1.322).
Hai điểm quan trọng cho chi phí của bước nối tiếp (docs/step-continuation-plan.md giai đoạn 3):
  - message.usage trong transcript CỘNG DỒN theo cả đời phiên (tệp sau resume = tệp trước + phần mới, cùng tên tệp): phần của lần nối tiếp
    = usage(sau) − usage(trước, mốc lưu lúc dừng).
  - transcript thật có dòng `cost-state` (totalCostUSD) để `--resume` khôi phục chi phí phiên, nên `total_cost_usd` báo sau resume có thể là
    chi phí cả phiên: Bint không dùng nó cho bước nối tiếp. Đo thật ở docs/acceptance.md (mục nghiệm thu resume Claude/Codex).
