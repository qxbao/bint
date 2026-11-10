-- Lượt đối chiếu: skill agent đã gọi qua công cụ Skill (mảng JSON; NULL = harness không đo được).
ALTER TABLE evaluation_runs ADD COLUMN skills_activated TEXT;
