-- H2: số liệu guardrail của quy trình trong lượt đánh giá (nhánh bint có thành phần quy trình).
-- Mỗi lượt có một dòng cho MỌI guardrail có hiệu lực ở nhánh đó (kể cả guardrail không bao
-- giờ đủ điều kiện), để báo cáo có mẫu số. Điền từ nhật ký của lượt khi lượt kết thúc.
CREATE TABLE eval_guardrail_usage (
    attempt_id TEXT NOT NULL REFERENCES evaluation_runs(attempt_id),
    guardrail  TEXT NOT NULL,
    eligible   INTEGER NOT NULL DEFAULT 0,   -- điều kiện when đúng ít nhất một lần (0/1)
    fired      INTEGER NOT NULL DEFAULT 0,   -- số lần kích hoạt
    followed   INTEGER NOT NULL DEFAULT 0,   -- điều kiện unless được thoả sau khi kích hoạt (0/1)
    blocked    INTEGER NOT NULL DEFAULT 0,   -- số lần chặn: ask/deny hoặc chặn kết thúc ở stop
    bypass     INTEGER NOT NULL DEFAULT 0,   -- số lần deny ở stop vượt trần nên cho qua
    PRIMARY KEY (attempt_id, guardrail)
);
