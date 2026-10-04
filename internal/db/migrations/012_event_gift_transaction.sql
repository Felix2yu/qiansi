-- 事件自带的那笔礼金（N2）
-- 婚礼/寿宴现场记完事就结账：事件表单上填的礼金金额直接生成一条 kind='gift' 的往来。
--
-- 这里只存「事件表单认领的那一条」的 id，不存金额：金额的真相在 transactions，
-- 事件上的数字是它的投影（与 expense_fen 同一套做法）。
-- 为什么不按 event_id 认领全部 gift：一场宴席的礼单可以有几十笔（每位来客一条），
-- 事件表单再保存一次就把手工登记的账全删了。
ALTER TABLE events ADD COLUMN gift_transaction_id TEXT REFERENCES transactions(id) ON DELETE SET NULL;
