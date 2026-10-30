-- +goose Up
-- 选品参考快照：按日保存平台热词与本店畅销榜，只存结构化结果与脱敏失败原因，不保存任何凭证。
CREATE TABLE IF NOT EXISTS sourcing_snapshots (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    kind VARCHAR(32) NOT NULL,
    snapshot_date VARCHAR(10) NOT NULL,
    payload_json LONGTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ok',
    error_message TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX idx_sourcing_snapshots_kind_date ON sourcing_snapshots(kind, snapshot_date);
CREATE INDEX idx_sourcing_snapshots_date ON sourcing_snapshots(snapshot_date);

-- +goose Down
DROP TABLE IF EXISTS sourcing_snapshots;
