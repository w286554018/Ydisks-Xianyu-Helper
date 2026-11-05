-- +goose Up
-- 市场情报：关注词、商品快照、AI 判定、每日聚合四张表。
-- 快照只存页面公开展示字段，不含凭证；原始快照保留 90 天，聚合永久保留（清理在应用层按日执行）。
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS market_intel_keywords (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS market_intel_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL,
    snapshot_date TEXT NOT NULL,
    item_id TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    price TEXT NOT NULL DEFAULT '',
    want_count INTEGER NOT NULL DEFAULT 0,
    want_count_text TEXT NOT NULL DEFAULT '',
    seller_nickname TEXT NOT NULL DEFAULT '',
    publish_time TEXT NOT NULL DEFAULT '',
    raw_json TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS market_intel_verdicts (
    snapshot_id INTEGER PRIMARY KEY,
    is_ai_related INTEGER NOT NULL DEFAULT 0,
    category TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (snapshot_id) REFERENCES market_intel_snapshots(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS market_intel_aggregates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    keyword TEXT NOT NULL,
    snapshot_date TEXT NOT NULL,
    total_count INTEGER NOT NULL DEFAULT 0,
    ai_count INTEGER NOT NULL DEFAULT 0,
    total_want INTEGER NOT NULL DEFAULT 0,
    category_json TEXT NOT NULL DEFAULT '',
    top_sellers_json TEXT NOT NULL DEFAULT '',
    verdict_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (keyword, snapshot_date)
);

CREATE INDEX IF NOT EXISTS idx_market_intel_snapshots_keyword_date ON market_intel_snapshots(keyword, snapshot_date);
CREATE INDEX IF NOT EXISTS idx_market_intel_snapshots_date ON market_intel_snapshots(snapshot_date);
CREATE INDEX IF NOT EXISTS idx_market_intel_aggregates_keyword_date ON market_intel_aggregates(keyword, snapshot_date);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS market_intel_aggregates;
DROP TABLE IF EXISTS market_intel_verdicts;
DROP TABLE IF EXISTS market_intel_snapshots;
DROP TABLE IF EXISTS market_intel_keywords;
-- +goose StatementEnd
