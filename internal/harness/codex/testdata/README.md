Fixture rollout của Codex (cấu trúc đối chiếu với rollout thật của codex-cli 0.167: `session_meta`, `event_msg`/`token_count` với
`info.total_token_usage` cộng dồn theo thread). Số liệu và nội dung do tay dựng (không phải bản chụp thật); hội thoại đã lược.

- `rollout-before-resume.jsonl`: thread sau lần chạy đầu (dòng `token_count` đầu có `info: null`).
- `rollout-resume.jsonl`: cùng tệp sau `codex exec resume` (Codex ghi tiếp vào rollout cũ, bộ đếm cộng dồn tiếp — giả định A2 của
  docs/step-continuation-plan.md; đo thật ở docs/acceptance.md mục nghiệm thu resume).
