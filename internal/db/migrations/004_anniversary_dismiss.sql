-- 纪念日提醒闭环：
-- 1) anniversary_dismiss 记录「某次纪念日提醒已完成」，避免用户勾掉后刷新又出现
-- 2) anniversaries.source 标记纪念日来源（birthday = 由人物生日自动生成）
-- 3) people.birthday_anniversary_id 记录生日对应的纪念日，实现双向同步

CREATE TABLE IF NOT EXISTS anniversary_dismiss (
    anniversary_id TEXT NOT NULL,
    occurrence TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(anniversary_id, occurrence)
);

ALTER TABLE anniversaries ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN birthday_anniversary_id TEXT;
CREATE INDEX IF NOT EXISTS idx_anniv_source ON anniversaries(source, person_id);
