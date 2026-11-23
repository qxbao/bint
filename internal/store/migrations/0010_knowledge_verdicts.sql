-- Nhận định của agent về mục tri thức cần rà soát (audit/verdict/revalidate dùng chung):
-- thêm verdict fix-evidence (kèm bằng chứng đề xuất, JSON) và nguồn tạo ra nhận định (agent/mô hình).
-- Chỉ là đầu vào cho người duyệt; không bao giờ tự đổi trạng thái tri thức.
CREATE TABLE knowledge_revalidations_new (
    revalidation_id   TEXT PRIMARY KEY,
    item_id           TEXT NOT NULL,
    evidence_key      TEXT NOT NULL,      -- fingerprint hiện tại của bằng chứng lúc nhận định
    verdict           TEXT NOT NULL CHECK (verdict IN ('valid','outdated','fix-evidence','unsure')),
    reason            TEXT,
    harness           TEXT,
    model             TEXT,
    cost_usd          REAL,
    created_at        TEXT NOT NULL,
    proposed_evidence TEXT                -- JSON {"path","lines","symbol"} khi verdict = fix-evidence
);
INSERT INTO knowledge_revalidations_new
    (revalidation_id, item_id, evidence_key, verdict, reason, harness, model, cost_usd, created_at)
    SELECT revalidation_id, item_id, evidence_key, verdict, reason, harness, model, cost_usd, created_at
    FROM knowledge_revalidations;
DROP TABLE knowledge_revalidations;
ALTER TABLE knowledge_revalidations_new RENAME TO knowledge_revalidations;
CREATE INDEX knowledge_revalidations_item ON knowledge_revalidations(item_id, created_at);
