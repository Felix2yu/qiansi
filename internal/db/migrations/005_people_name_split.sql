-- vCard 往返：拆分存储 Apple 姓/名两个字段（N:Family;Given;...），
-- 导入导出不再依赖显示顺序拼接，姓名显示顺序由 ComposeName 统一决定

ALTER TABLE people ADD COLUMN family_name TEXT NOT NULL DEFAULT '';
ALTER TABLE people ADD COLUMN given_name TEXT NOT NULL DEFAULT '';
