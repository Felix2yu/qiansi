-- 015：往来关联的货殖账单（1 : N）
--
-- 账单的权威数据在货殖，牵丝只持有「关联 + 快照」：
-- remote_id 指回货殖 transactions.id，其余几列是关联当时拷下来的展示字段。
--
-- 为什么存快照而不是每次实时查：
--   货殖没开、改域名、临时维护时，往来列表仍要能显示「这次见面花了多少、花在哪儿」，
--   而不是一片空白或一个转个不停的加载态。快照会过期，所以界面给「刷新」，
--   按 remote_id 批量重取——它是缓存，不是数据源。
--
-- 为什么不给货殖账单建外键：它是另一个库里的自增 id，跨系统外键无法维护，
-- 删账单后由同步失败体现为「已失效」，由用户决定解除关联。
--
-- 开销口径（store/events.go 的 eventColumns）：一个往来一旦有账单，
-- expense_fen = 各账单金额之和，手填的那笔不再计入，避免同一笔钱被算两次。
CREATE TABLE IF NOT EXISTS event_bills (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL,
    remote_id     TEXT NOT NULL,
    remote_base   TEXT NOT NULL DEFAULT '',
    amount_fen    INTEGER NOT NULL DEFAULT 0,
    type          TEXT NOT NULL DEFAULT 'expense',
    tx_date       TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    category_name TEXT NOT NULL DEFAULT '',
    account_name  TEXT NOT NULL DEFAULT '',
    book_name     TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    synced_at     TEXT NOT NULL,
    FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE,
    UNIQUE (event_id, remote_id)
);
CREATE INDEX IF NOT EXISTS idx_event_bills_event ON event_bills(event_id);
