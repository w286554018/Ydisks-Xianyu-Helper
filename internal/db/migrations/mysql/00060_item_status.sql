-- +goose Up
-- 商品平台生命周期状态：active=在售（默认），down=平台已下架。
-- status 独立于 deleted_at 软删除审计；平台同步（Upsert/SyncFromRemote）不重置本字段。
ALTER TABLE item_info ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active';

-- +goose Down
ALTER TABLE item_info DROP COLUMN status;
