-- P7: skill được kích hoạt trong lượt chạy (sự kiện skill.activated, mục 9.1 run_skill_usages).
-- Chỉ harness có công cụ gọi skill đi qua hook (Claude Code) mới ghi; không tổng hợp giả.
CREATE TABLE run_skill_usages (
    usage_id   TEXT PRIMARY KEY,
    run_id     TEXT REFERENCES runs(run_id),
    event_id   TEXT NOT NULL UNIQUE,
    skill      TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX run_skill_usages_skill ON run_skill_usages(skill);
CREATE INDEX run_skill_usages_run ON run_skill_usages(run_id);
