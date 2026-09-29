-- 亲密度不再默认 3 星：新建/导入未指定等级时记为 0（未设置）。
-- SQLite 不支持 ALTER COLUMN，这里重建 people 表只改列默认值，数据原样搬移。
-- 注意顺序：不能先 RENAME people（会把其他表的 REFERENCES 改写成新表名），
-- 而是先把数据搬进 people_new，关外键后 DROP 旧表，再把 people_new 改名为 people——
-- 这样其他表的 REFERENCES 始终写着 people，改名时不会被改写，重建后引用立即有效。
PRAGMA foreign_keys=OFF;

CREATE TABLE people_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    nickname TEXT,
    gender TEXT,
    birthday TEXT,
    birthday_is_lunar INTEGER DEFAULT 0,
    avatar_attachment_id TEXT,
    phone TEXT,
    wechat TEXT,
    location TEXT,
    notes TEXT,
    grade INTEGER DEFAULT 0,
    category_id INTEGER,
    archived INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    x_abuid TEXT NOT NULL DEFAULT '',
    birthday_anniversary_id TEXT,
    family_name TEXT NOT NULL DEFAULT '',
    given_name TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(category_id) REFERENCES categories(id)
);

INSERT INTO people_new (id,name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,
                        phone,wechat,location,notes,grade,category_id,archived,created_at,updated_at,
                        x_abuid,birthday_anniversary_id,family_name,given_name)
SELECT id,name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,
       phone,wechat,location,notes,grade,category_id,archived,created_at,updated_at,
       x_abuid,birthday_anniversary_id,family_name,given_name
FROM people;

DROP TABLE people;

ALTER TABLE people_new RENAME TO people;

CREATE INDEX IF NOT EXISTS idx_people_category ON people(category_id);
CREATE INDEX IF NOT EXISTS idx_people_grade ON people(grade);

PRAGMA foreign_keys=ON;
