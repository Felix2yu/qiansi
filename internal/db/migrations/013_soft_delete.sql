-- 回收站：六类主记录改软删除
--
-- 联系人/往来/对话/账目/纪念日/待办这六类是用户手写的记录，删错了没法重建。
-- 以前 DELETE 直接落刀，外键级联还会连带吃掉参与人、还款、自定义字段，
-- 撤销窗口一过就只剩「从备份恢复」。这里给六张表加 deleted_at：
-- 非空即「暂时不存在」，删除只置标记，恢复只清标记，级联永远不会发生。
--
-- 牵连口径（回收站 = 这些人/记录当下不存在）：某人进了回收站，
-- 挂在他名下的往来、对话、账目、纪念日、待办也一起从列表/搜索/统计里消失；
-- 一场往来里只要有一位参与人被回收，这一场就整场藏起来（同桌别人的记录会随恢复回来）。
--
-- 读路径一律走下面的 live_* 视图，判定只在这一处写着；写路径与备份/搬运
-- （dump、导出 .db）仍然面对真表，回收站里的东西不该在备份里凭空消失。
-- 视图存的是语句文本，之后给这些表加的列会自动透过 live_* 可见。

ALTER TABLE people ADD COLUMN deleted_at TEXT;
ALTER TABLE events ADD COLUMN deleted_at TEXT;
ALTER TABLE memos ADD COLUMN deleted_at TEXT;
ALTER TABLE transactions ADD COLUMN deleted_at TEXT;
ALTER TABLE anniversaries ADD COLUMN deleted_at TEXT;
ALTER TABLE reminders ADD COLUMN deleted_at TEXT;

-- 回收站本身只有几百行，按「已回收」建偏索引，列表与清空都不必扫全表
CREATE INDEX IF NOT EXISTS idx_people_trash ON people(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_events_trash ON events(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_memos_trash ON memos(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_transactions_trash ON transactions(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_anniversaries_trash ON anniversaries(deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_reminders_trash ON reminders(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE VIEW live_people AS
SELECT * FROM people WHERE deleted_at IS NULL;

CREATE VIEW live_memos AS
SELECT m.* FROM memos m
WHERE m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = m.person_id AND p.deleted_at IS NOT NULL);

CREATE VIEW live_transactions AS
SELECT t.* FROM transactions t
WHERE t.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = t.person_id AND p.deleted_at IS NOT NULL);

CREATE VIEW live_anniversaries AS
SELECT a.* FROM anniversaries a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = a.person_id AND p.deleted_at IS NOT NULL);

CREATE VIEW live_reminders AS
SELECT r.* FROM reminders r
WHERE r.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM people p WHERE p.id = r.person_id AND p.deleted_at IS NOT NULL);

CREATE VIEW live_events AS
SELECT e.* FROM events e
WHERE e.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM event_participants ep JOIN people p ON p.id = ep.person_id
    WHERE ep.event_id = e.id AND p.deleted_at IS NOT NULL
  );
