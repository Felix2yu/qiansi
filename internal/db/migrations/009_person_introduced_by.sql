-- 认识来源：每个人可记录「通过谁认识」。引荐人链由这一列自引用串成：
-- C 通过 B、B 通过 A、A 与我直达 → 我—…—C 的认识路径自动可得。
-- 可空自引用列可以直接 ADD（SQLite 仅禁止带非空默认值的加列），无需重建表。
ALTER TABLE people ADD COLUMN introduced_by_person_id TEXT REFERENCES people(id) ON DELETE SET NULL;
