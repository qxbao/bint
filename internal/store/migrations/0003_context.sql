-- P3: Context Engine và vòng đời tri thức.
-- knowledge_index / knowledge_fts là chỉ mục DẪN XUẤT từ .bint/knowledge/items
-- (nguồn chuẩn); xoá và dựng lại được bất cứ lúc nào (`bint index rebuild`).

CREATE TABLE knowledge_index (
    item_id     TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    status      TEXT NOT NULL,
    title       TEXT NOT NULL,
    scope_paths TEXT NOT NULL DEFAULT '[]',   -- JSON
    symbols     TEXT NOT NULL DEFAULT '[]',   -- JSON
    tags        TEXT NOT NULL DEFAULT '[]',   -- JSON
    domains     TEXT NOT NULL DEFAULT '[]',   -- JSON
    origin_kind TEXT NOT NULL,
    third_party INTEGER NOT NULL DEFAULT 0,
    eval_case   TEXT,
    author      TEXT,
    file_sha    TEXT NOT NULL,
    indexed_at  TEXT NOT NULL
);

CREATE VIRTUAL TABLE knowledge_fts USING fts5(
    item_id UNINDEXED, title, body, tags, paths, symbols,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE TABLE knowledge_index_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Mức "được truy xuất" (mục 9.7): Context Engine đã trả về mục.
CREATE TABLE context_usages (
    usage_id    TEXT PRIMARY KEY,
    run_id      TEXT REFERENCES runs(run_id),
    event_id    TEXT NOT NULL,
    channel     TEXT NOT NULL,          -- mcp:task | mcp:paths | hook:path | cli
    item_id     TEXT NOT NULL,
    rank        INTEGER,
    label       TEXT,                   -- rule | warning | reference | stale
    created_at  TEXT NOT NULL,
    UNIQUE (event_id, item_id)
);
CREATE INDEX context_usages_item ON context_usages(item_id);
CREATE INDEX context_usages_run ON context_usages(run_id);

-- Mức "được áp dụng" (mục 9.7): agent báo đã dùng mục qua cite_knowledge.
CREATE TABLE knowledge_citations (
    citation_id TEXT PRIMARY KEY,
    run_id      TEXT REFERENCES runs(run_id),
    event_id    TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    note        TEXT,
    created_at  TEXT NOT NULL,
    UNIQUE (event_id, item_id)
);
CREATE INDEX knowledge_citations_item ON knowledge_citations(item_id);

-- Nhận xét, lỗi, kinh nghiệm agent ghi qua report_observation.
CREATE TABLE observations (
    observation_id TEXT PRIMARY KEY,
    run_id         TEXT REFERENCES runs(run_id),
    event_id       TEXT NOT NULL UNIQUE,
    kind           TEXT NOT NULL,
    title          TEXT NOT NULL,
    body           TEXT,
    paths          TEXT NOT NULL DEFAULT '[]',
    item_id        TEXT,                -- mục candidate được tạo, nếu có
    created_at     TEXT NOT NULL
);

-- Tín hiệu kết quả (mục 9.6). Không gộp thành một điểm: mỗi tín hiệu một dòng.
CREATE TABLE outcome_signals (
    signal_id   TEXT PRIMARY KEY,
    run_id      TEXT NOT NULL REFERENCES runs(run_id),
    kind        TEXT NOT NULL,          -- tests | rating | committed | reverted
    polarity    TEXT NOT NULL CHECK (polarity IN ('positive','negative')),
    strength    TEXT NOT NULL CHECK (strength IN ('strong','medium','weak')),
    source      TEXT NOT NULL,          -- hook | cli | git-scan
    detail      TEXT,
    dedup_key   TEXT NOT NULL UNIQUE,
    observed_at TEXT NOT NULL
);
CREATE INDEX outcome_signals_run ON outcome_signals(run_id);

-- Nhận định của agent khi xác thực lại mục stale rủi ro cao (mục 8.4). Chỉ là đầu
-- vào cho người duyệt; không bao giờ tự đổi trạng thái tri thức.
CREATE TABLE knowledge_revalidations (
    revalidation_id TEXT PRIMARY KEY,
    item_id         TEXT NOT NULL,
    evidence_key    TEXT NOT NULL,      -- fingerprint hiện tại của bằng chứng lúc nhận định
    verdict         TEXT NOT NULL CHECK (verdict IN ('valid','outdated','unsure')),
    reason          TEXT,
    harness         TEXT,
    model           TEXT,
    cost_usd        REAL,
    created_at      TEXT NOT NULL
);
CREATE INDEX knowledge_revalidations_item ON knowledge_revalidations(item_id, created_at);

-- Truy xuất / trích dẫn trong lượt đánh giá (nhánh bint), phục vụ chẩn đoán 12.9.
CREATE TABLE eval_context_usage (
    attempt_id TEXT NOT NULL REFERENCES evaluation_runs(attempt_id),
    item_id    TEXT NOT NULL,
    retrieved  INTEGER NOT NULL DEFAULT 0,
    cited      INTEGER NOT NULL DEFAULT 0,
    ideal      INTEGER NOT NULL DEFAULT 0,   -- thuộc tập "đúng" của tác vụ (mục 12.4)
    channels   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (attempt_id, item_id)
);
