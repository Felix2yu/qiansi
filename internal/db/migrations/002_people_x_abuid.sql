-- vCard 往返：保留 macOS 联系人.app 的 X-ABUID，导入回 Contacts 时可匹配更新而非新建

ALTER TABLE people ADD COLUMN x_abuid TEXT NOT NULL DEFAULT '';
