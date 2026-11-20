# Lượt bị huỷ (nhiễm đáp án), 2026-11-20 03:03–04:10
Repo clone ở 17812a0 còn docs/evals/bench-bint-vs-plain-* và docs/benchmark-bint-vs-plain-*.md (đặc tả rủi ro, phát hiện review
đợt 5, mô tả fixture test ẩn). Rollout của A đọc chúng (v3 dir >2000 lần, T2-verified 77 lần); rollout b-req/b-goal của B cũng đọc.
Số đo trước khi huỷ (không dùng cho kết luận): A sau phiên 1 sạch 36/36 ô, 42 tệp; B sau phiên 1 (b-req 6m10s + b-goal 23m48s) 0/36.
Dừng giữa phiên 2 của cả hai bên. Chạy lại trên repo đã gỡ các tài liệu đó (commit bench 48dbfad trên 17812a0, cả hai bên).
