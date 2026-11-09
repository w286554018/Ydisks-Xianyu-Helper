-- +goose Up
-- 我的商品统计：每个自己商品的浏览量/想要量/收藏/已售，按账号与商品唯一。
-- 数据来自商品详情页公开信息，不含任何凭证；24 小时内复用不重抓（有效期在应用层判定）。
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS item_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cookie_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    browse_cnt INTEGER NOT NULL DEFAULT 0,
    want_cnt INTEGER NOT NULL DEFAULT 0,
    collect_cnt INTEGER NOT NULL DEFAULT 0,
    sold_cnt INTEGER NOT NULL DEFAULT 0,
    fetched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (cookie_id) REFERENCES cookies(id) ON DELETE CASCADE,
    UNIQUE(cookie_id, item_id)
);

CREATE INDEX IF NOT EXISTS idx_item_stats_cookie_fetched ON item_stats(cookie_id, fetched_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS item_stats;
-- +goose StatementEnd
