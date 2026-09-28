-- 事件细节：多地点、礼物标记
-- 开销：Money 交易可挂到事件（transactions.event_id），实现「见面的花销」与账本打通

ALTER TABLE events ADD COLUMN locations TEXT NOT NULL DEFAULT '';   -- JSON 数组 ["茶馆","书店"]
ALTER TABLE events ADD COLUMN has_gift INTEGER NOT NULL DEFAULT 0;
ALTER TABLE events ADD COLUMN gift TEXT NOT NULL DEFAULT '';        -- 礼物说明，如「送了两盒茶」

ALTER TABLE transactions ADD COLUMN event_id TEXT;
CREATE INDEX IF NOT EXISTS idx_trans_event ON transactions(event_id);
