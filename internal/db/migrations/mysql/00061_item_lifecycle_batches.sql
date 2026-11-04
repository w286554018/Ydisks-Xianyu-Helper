-- +goose Up
-- 商品生命周期批次：跨账号批量下架/删除的任务表；动作与行级状态独立于发布批次。
-- 行间执行带抖动与风控即停约束（见任务 11-01 design.md），表结构只存事实不存平台凭证。
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS item_lifecycle_batches (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    total_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    worker_token TEXT NOT NULL DEFAULT '',
    lease_expires_at INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS item_lifecycle_batch_rows (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    batch_id TEXT NOT NULL,
    row_no INTEGER NOT NULL,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    failure_kind TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (batch_id) REFERENCES item_lifecycle_batches(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_item_lifecycle_batches_user ON item_lifecycle_batches(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_item_lifecycle_batch_rows_batch ON item_lifecycle_batch_rows(batch_id, row_no);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_lifecycle_batch_rows;
DROP TABLE IF EXISTS item_lifecycle_batches;
-- +goose StatementEnd
