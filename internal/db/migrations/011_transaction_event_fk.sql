-- transactions.event_id 补上外键。
-- 003 只是 ALTER ADD COLUMN，没带 FK，于是「事件已删除、开销还挂在它上面」的孤儿行
-- 一路无人拦截：事件详情页按 event_id 汇总开销，孤儿行既统计不进去，也永远清不掉。
-- ADD COLUMN 不能直接加表级约束，沿用 007/008 的重建顺序：搬数据进新表 → DROP 旧表 → 改名。
PRAGMA foreign_keys=OFF;

CREATE TABLE transactions_new (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL,
    kind TEXT NOT NULL,    -- loan/gift/expense/other
    direction TEXT NOT NULL, -- in/out
    amount_fen INTEGER NOT NULL,
    title TEXT,
    occurred_at TEXT NOT NULL,
    due_date TEXT,
    settled INTEGER DEFAULT 0,
    settled_at TEXT,
    created_at TEXT NOT NULL,
    event_id TEXT,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE,
    FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE SET NULL
);

-- 搬的同时把存量孤儿置空，否则外键一开，之后任何一次删除事件都会被 CASCADE/RESTRICT 逻辑卡住。
INSERT INTO transactions_new (id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at,event_id)
SELECT id,person_id,kind,direction,amount_fen,title,occurred_at,due_date,settled,settled_at,created_at,
       CASE WHEN EXISTS(SELECT 1 FROM events e WHERE e.id = transactions.event_id) THEN event_id END
FROM transactions;

DROP TABLE transactions;

ALTER TABLE transactions_new RENAME TO transactions;

CREATE INDEX IF NOT EXISTS idx_trans_person ON transactions(person_id);
CREATE INDEX IF NOT EXISTS idx_trans_event ON transactions(event_id);

PRAGMA foreign_keys=ON;
