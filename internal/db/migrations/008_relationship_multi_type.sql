-- 关系边允许同一对人挂多种类型（既是同事又是球友）。
-- 原来的 UNIQUE(from_person_id,to_person_id) 配上 INSERT OR REPLACE，
-- 后建的那条会静默顶掉前一条；改成「两人+类型」唯一，只有真正的重复才拦。
-- 重建沿用 007 的顺序：先搬数据进新表，DROP 旧表后再改名。
PRAGMA foreign_keys=OFF;

CREATE TABLE relationships_new (
    id TEXT PRIMARY KEY,
    from_person_id TEXT NOT NULL,
    to_person_id TEXT NOT NULL,
    type TEXT NOT NULL,
    remark TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(from_person_id) REFERENCES people(id) ON DELETE CASCADE,
    FOREIGN KEY(to_person_id) REFERENCES people(id) ON DELETE CASCADE,
    UNIQUE(from_person_id, to_person_id, type)
);

INSERT INTO relationships_new (id,from_person_id,to_person_id,type,remark,created_at)
SELECT id,from_person_id,to_person_id,type,remark,created_at FROM relationships;

DROP TABLE relationships;

ALTER TABLE relationships_new RENAME TO relationships;

-- 原来的 UNIQUE(from,to) 自带一个前缀索引，换键之后得把它补回来，
-- 否则 RelationshipsOf 的 from/to 过滤就退化成全表扫。
CREATE INDEX IF NOT EXISTS idx_relationships_from ON relationships(from_person_id);
CREATE INDEX IF NOT EXISTS idx_relationships_to ON relationships(to_person_id);

PRAGMA foreign_keys=ON;
