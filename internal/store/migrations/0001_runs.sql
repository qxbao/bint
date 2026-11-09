-- P0: ghi nhận lượt chạy và sự kiện.
-- Thời gian lưu dạng TEXT UTC cố định "YYYY-MM-DDTHH:MM:SS.mmmZ" để so sánh theo thứ tự chữ.
-- NULL nghĩa là không có hoặc chưa biết; không thay bằng 0.

CREATE TABLE runs (
    run_id              TEXT PRIMARY KEY,
    task_id             TEXT,
    parent_run_id       TEXT REFERENCES runs(run_id),
    workflow            TEXT,
    status              TEXT NOT NULL CHECK (status IN
                          ('created','running','completed','failed','cancelled','interrupted')),
    harness             TEXT,
    harness_version     TEXT,
    harness_session_id  TEXT,
    model               TEXT,
    model_version       TEXT,
    bint_version        TEXT,
    knowledge_revision  TEXT,
    git_base_commit     TEXT,
    git_branch          TEXT,
    worktree            TEXT,
    started_at          TEXT,
    finished_at         TEXT,
    last_event_at       TEXT,
    duration_ms         INTEGER,
    input_tokens        INTEGER,
    output_tokens       INTEGER,
    cost_usd            REAL,
    result_summary      TEXT,
    verification_status TEXT,
    end_reason          TEXT,
    UNIQUE (harness, harness_session_id)
);
CREATE INDEX runs_started_at ON runs(started_at);
CREATE INDEX runs_status ON runs(status);

CREATE TABLE run_steps (
    step_id     TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(run_id),
    name        TEXT NOT NULL,
    status      TEXT NOT NULL,
    started_at  TEXT,
    finished_at TEXT
);
CREATE INDEX run_steps_run ON run_steps(run_id);

CREATE TABLE events (
    event_id       TEXT PRIMARY KEY,
    run_id         TEXT REFERENCES runs(run_id),
    step_id        TEXT REFERENCES run_steps(step_id),
    event_type     TEXT NOT NULL,
    source         TEXT NOT NULL,
    timestamp      TEXT NOT NULL,
    payload        TEXT NOT NULL DEFAULT '{}',
    schema_version INTEGER NOT NULL,
    ingested_at    TEXT NOT NULL
);
CREATE INDEX events_run_time ON events(run_id, timestamp);
CREATE INDEX events_type ON events(event_type);

CREATE TABLE run_artifacts (
    artifact_id   TEXT PRIMARY KEY,
    run_id        TEXT NOT NULL REFERENCES runs(run_id),
    kind          TEXT NOT NULL,
    path          TEXT NOT NULL,
    first_seen_at TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    count         INTEGER NOT NULL DEFAULT 1,
    UNIQUE (run_id, kind, path)
);
