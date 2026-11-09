-- P1: khung đánh giá (mục 12). Tác vụ đã duyệt nằm trong tệp .bint/evals/cases/*.yml
-- (nguồn chuẩn); cơ sở dữ liệu chỉ lưu các lần chạy và kết quả.
-- NULL nghĩa là không đo được (ví dụ harness không báo chi phí); không thay bằng 0.

CREATE TABLE eval_batches (
    batch_id           TEXT PRIMARY KEY,
    status             TEXT NOT NULL CHECK (status IN
                         ('running','completed','failed','cancelled','interrupted','budget_exhausted')),
    harness            TEXT NOT NULL,
    harness_version    TEXT,
    requested_model    TEXT,
    bint_version       TEXT,
    leakage_mode       TEXT NOT NULL,
    arms               TEXT NOT NULL,          -- JSON mảng tên nhánh
    repeats            INTEGER NOT NULL,
    case_ids           TEXT NOT NULL,          -- JSON mảng ID tác vụ
    prompt_version     INTEGER NOT NULL,
    seed               INTEGER NOT NULL,
    budget_usd         REAL,
    estimated_cost_usd REAL,
    git_head           TEXT,
    started_at         TEXT NOT NULL,
    finished_at        TEXT,
    notes              TEXT NOT NULL DEFAULT '[]' -- JSON mảng cảnh báo về độ tin cậy
);
CREATE INDEX eval_batches_started ON eval_batches(started_at);

CREATE TABLE evaluation_runs (
    attempt_id        TEXT PRIMARY KEY,
    batch_id          TEXT NOT NULL REFERENCES eval_batches(batch_id),
    case_id           TEXT NOT NULL,
    case_fingerprint  TEXT NOT NULL,
    case_split        TEXT,
    arm               TEXT NOT NULL,
    repeat_index      INTEGER NOT NULL,
    status            TEXT NOT NULL CHECK (status IN
                        ('running','passed','failed','error','timeout','skipped')),
    harness           TEXT NOT NULL,
    harness_version   TEXT,
    model             TEXT,                   -- mô hình harness báo đã dùng
    context_source    TEXT,                   -- nguồn ngữ cảnh của nhánh (ví dụ commit chứa tệp minimal)
    started_at        TEXT NOT NULL,
    finished_at       TEXT,
    duration_ms       INTEGER,
    agent_duration_ms INTEGER,
    agent_exit_code   INTEGER,
    num_turns         INTEGER,
    input_tokens      INTEGER,
    output_tokens     INTEGER,
    cost_usd          REAL,
    setup_ok          INTEGER,
    test_exit_code    INTEGER,
    test_passed       INTEGER,                -- 1/0; NULL khi không chấm được
    files_changed     INTEGER,
    lines_changed     INTEGER,
    touched_tests     INTEGER,
    error             TEXT,
    log_dir           TEXT,
    UNIQUE (batch_id, case_id, arm, repeat_index)
);
CREATE INDEX evaluation_runs_batch ON evaluation_runs(batch_id);
CREATE INDEX evaluation_runs_case ON evaluation_runs(case_id, arm);
