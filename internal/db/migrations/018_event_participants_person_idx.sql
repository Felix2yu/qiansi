-- event_participants 的主键是 (event_id, person_id)，按人维度的过滤用不上它：
-- 「某人参与过的往来」（PersonTimeline）、统计 CTE 里与 people 的 JOIN、
-- 见面口径的参与人过滤都要全表扫。按 person_id 补一条索引。
CREATE INDEX IF NOT EXISTS idx_ep_person ON event_participants(person_id);
