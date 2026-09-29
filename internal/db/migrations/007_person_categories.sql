-- 圈子改多对多：一个人可以同时属于多个圈子（既同学又同事）。
-- 关联建在 person_categories 上，people.category_id 搬过去后删列。
-- 重建 people 沿用 006 的顺序：先把数据搬进 people_new，关外键后 DROP 旧表，
-- 再把 people_new 改名——先 RENAME 会把其他表的 REFERENCES 一起改写掉。
PRAGMA foreign_keys=OFF;

CREATE TABLE person_categories (
    person_id TEXT NOT NULL,
    category_id INTEGER NOT NULL,
    PRIMARY KEY(person_id, category_id),
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE,
    FOREIGN KEY(category_id) REFERENCES categories(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_person_categories_category ON person_categories(category_id);

INSERT INTO person_categories(person_id, category_id)
SELECT id, category_id FROM people WHERE category_id IS NOT NULL;

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
    archived INTEGER DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    x_abuid TEXT NOT NULL DEFAULT '',
    birthday_anniversary_id TEXT,
    family_name TEXT NOT NULL DEFAULT '',
    given_name TEXT NOT NULL DEFAULT ''
);

INSERT INTO people_new (id,name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,
                        phone,wechat,location,notes,grade,archived,created_at,updated_at,
                        x_abuid,birthday_anniversary_id,family_name,given_name)
SELECT id,name,nickname,gender,birthday,birthday_is_lunar,avatar_attachment_id,
       phone,wechat,location,notes,grade,archived,created_at,updated_at,
       x_abuid,birthday_anniversary_id,family_name,given_name
FROM people;

DROP TABLE people;

ALTER TABLE people_new RENAME TO people;

CREATE INDEX IF NOT EXISTS idx_people_grade ON people(grade);

PRAGMA foreign_keys=ON;
