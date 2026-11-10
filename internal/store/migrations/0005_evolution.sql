-- P6: tiến hoá tri thức (L1, mục 10–11). Đề xuất tiến hoá chạy cục bộ trên máy từng
-- người (mục 8.7) nên nằm trong cơ sở dữ liệu trạng thái; thay đổi tri thức được áp
-- dụng vẫn đi vào tệp .bint/knowledge/items (nguồn chuẩn) và qua review mã như thường.

CREATE TABLE evolution_proposals (
    proposal_id     TEXT PRIMARY KEY,
    level           TEXT NOT NULL,                 -- L1
    target_type     TEXT NOT NULL,                 -- knowledge
    action          TEXT NOT NULL CHECK (action IN ('create','update','merge','deprecate','delete')),
    target_ids      TEXT NOT NULL DEFAULT '[]',    -- JSON
    opportunity_key TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('proposed','applied','rejected','rolled_back')),
    risk_level      TEXT NOT NULL CHECK (risk_level IN ('low','medium','high')),
    body            TEXT NOT NULL,                 -- JSON đầy đủ của đề xuất (schema có phiên bản)
    generator       TEXT NOT NULL,                 -- deterministic | agent:<harness>
    model           TEXT,
    cost_usd        REAL,                          -- chi phí tạo ứng viên (mục 12.8)
    status_reason   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX evolution_proposals_key ON evolution_proposals(opportunity_key);
CREATE INDEX evolution_proposals_status ON evolution_proposals(status);

-- Một lần đối chiếu base/candidate (Probe) của một đề xuất: một đợt eval.
CREATE TABLE evolution_evaluations (
    evaluation_id TEXT PRIMARY KEY,
    proposal_id   TEXT NOT NULL REFERENCES evolution_proposals(proposal_id),
    batch_id      TEXT NOT NULL REFERENCES eval_batches(batch_id),
    split         TEXT,                            -- validation | test | NULL (trộn)
    base_key      TEXT NOT NULL,                   -- phiên bản gốc lúc đánh giá
    delivery      TEXT NOT NULL,
    case_ids      TEXT NOT NULL,                   -- JSON
    verdict       TEXT,                            -- supported | not_supported | inconclusive
    independent   INTEGER,                         -- 1: tập test chưa dùng cho đề xuất khác cùng đối tượng
    detail        TEXT,
    created_at    TEXT NOT NULL,
    UNIQUE (batch_id)
);
CREATE INDEX evolution_evaluations_proposal ON evolution_evaluations(proposal_id);

-- Lịch sử áp dụng và quay lui (bản ghi kiểm toán, mục 15.4).
CREATE TABLE evolution_adoptions (
    adoption_id   TEXT PRIMARY KEY,
    proposal_id   TEXT NOT NULL REFERENCES evolution_proposals(proposal_id),
    action        TEXT NOT NULL CHECK (action IN ('apply','rollback')),
    actor         TEXT NOT NULL,
    verified      INTEGER NOT NULL,                -- 1: có kết quả supported, độc lập, trên tập test
    evaluation_id TEXT,
    harness       TEXT,                            -- harness/mô hình của lần đánh giá làm căn cứ (mục 12.6)
    model         TEXT,
    files         TEXT NOT NULL,                   -- JSON [{path, before, after}] để quay lui
    reason        TEXT,
    created_at    TEXT NOT NULL
);
CREATE INDEX evolution_adoptions_proposal ON evolution_adoptions(proposal_id, created_at);
