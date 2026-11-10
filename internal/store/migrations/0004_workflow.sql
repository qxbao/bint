-- P4: workflow gốc và tác vụ. Trạng thái tác vụ là tệp trong <state>/runtime/tasks
-- (nguồn chuẩn); cơ sở dữ liệu chỉ liên kết lượt chạy với tác vụ (runs.task_id,
-- runs.parent_run_id, run_steps) và lưu tín hiệu kết quả.
CREATE INDEX runs_task ON runs(task_id);
CREATE INDEX runs_parent ON runs(parent_run_id);
