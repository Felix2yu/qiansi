-- 016：事件认领自己的那笔开销（指针化，对齐礼金的做法）
--
-- 背景：applyEventExpense 原来按 event_id 批量删/改 kind='expense' 的交易，
-- 分不清「事件表单自动管理的那笔」和「用户从金钱页手工挂上来的那笔」——
-- 后者的 event_id 同样指向该事件。于是保存一次事件（哪怕没动开销）
-- 就可能把用户手工记的账删掉，属于不可恢复的数据丢失。
--
-- 礼金那边早就用指针解决了同一问题（events.gift_transaction_id，
-- 见 applyEventGift 的注释：绝不按 event_id 批量删）。开销沿用同一模式：
-- 事件只认领、只改动指针指向的那一条，其余挂账交易概不插手。
--
-- 回填口径：每场往来认领最老的一条 expense/out 交易（created_at, id 排序），
-- 与旧版「只保留最旧一条」的行为一致，保证升级后老数据的认领关系不变。
ALTER TABLE events ADD COLUMN expense_transaction_id TEXT REFERENCES transactions(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_events_expense_tx ON events(expense_transaction_id);
UPDATE events SET expense_transaction_id = (
    SELECT t.id FROM transactions t
    WHERE t.event_id = events.id AND t.kind = 'expense' AND t.direction = 'out'
    ORDER BY t.created_at, t.id LIMIT 1
) WHERE EXISTS (
    SELECT 1 FROM transactions t
    WHERE t.event_id = events.id AND t.kind = 'expense' AND t.direction = 'out'
);
