-- +goose Up
-- 竞品商品详情缓存：按需抓取的商品完整信息，同商品 7 天内复用不重抓。
-- 描述与图片来自详情页公开数据，不含任何凭证。
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS market_intel_item_details (
    item_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    price TEXT NOT NULL DEFAULT '',
    desc_text TEXT NOT NULL DEFAULT '',
    images_json TEXT NOT NULL DEFAULT '',
    seller_nickname TEXT NOT NULL DEFAULT '',
    seller_id TEXT NOT NULL DEFAULT '',
    want_count_text TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS market_intel_item_details;
-- +goose StatementEnd
