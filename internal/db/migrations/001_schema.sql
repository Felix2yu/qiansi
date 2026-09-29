-- 牵丝 v1 初始 schema

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    color TEXT DEFAULT '#6366f1',
    icon TEXT DEFAULT 'circle',
    sort_order INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    color TEXT DEFAULT '#6366f1'
);

CREATE TABLE IF NOT EXISTS event_types (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    color TEXT DEFAULT '#6366f1',
    icon TEXT DEFAULT 'calendar',
    is_default INTEGER DEFAULT 0,
    sort_order INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS people (
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
    FOREIGN KEY(category_id) REFERENCES categories(id)
);
CREATE INDEX IF NOT EXISTS idx_people_category ON people(category_id);
CREATE INDEX IF NOT EXISTS idx_people_grade ON people(grade);

CREATE TABLE IF NOT EXISTS person_fields (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL,
    label TEXT NOT NULL,
    value TEXT,
    sort_order INTEGER DEFAULT 0,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_pf_person ON person_fields(person_id);

CREATE TABLE IF NOT EXISTS relationships (
    id TEXT PRIMARY KEY,
    from_person_id TEXT NOT NULL,
    to_person_id TEXT NOT NULL,
    type TEXT NOT NULL,
    remark TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(from_person_id) REFERENCES people(id) ON DELETE CASCADE,
    FOREIGN KEY(to_person_id) REFERENCES people(id) ON DELETE CASCADE,
    UNIQUE(from_person_id, to_person_id)
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    type_id INTEGER,
    event_date TEXT NOT NULL,
    location TEXT,
    summary TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(type_id) REFERENCES event_types(id)
);
CREATE INDEX IF NOT EXISTS idx_events_date ON events(event_date);

CREATE TABLE IF NOT EXISTS event_participants (
    event_id TEXT NOT NULL,
    person_id TEXT NOT NULL,
    PRIMARY KEY(event_id, person_id),
    FOREIGN KEY(event_id) REFERENCES events(id) ON DELETE CASCADE,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memos (
    id TEXT PRIMARY KEY,
    person_id TEXT,
    speaker TEXT DEFAULT 'other',
    content TEXT NOT NULL,
    said_at TEXT NOT NULL,
    is_promise INTEGER DEFAULT 0,
    due_date TEXT,
    status TEXT DEFAULT 'open', -- open/fulfilled/broken
    created_at TEXT NOT NULL,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_memos_person ON memos(person_id);
CREATE INDEX IF NOT EXISTS idx_memos_status ON memos(status);

CREATE TABLE IF NOT EXISTS transactions (
    id TEXT PRIMARY KEY,
    person_id TEXT NOT NULL,
    kind TEXT NOT NULL,    -- loan/gift/expense/other
    direction TEXT NOT NULL, -- in/out
    amount_fen INTEGER NOT NULL,
    title TEXT,
    occurred_at TEXT NOT NULL,
    due_date TEXT,
    settled INTEGER DEFAULT 0,
    settled_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_trans_person ON transactions(person_id);

CREATE TABLE IF NOT EXISTS repayments (
    id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL,
    amount_fen INTEGER NOT NULL,
    occurred_at TEXT NOT NULL,
    note TEXT,
    FOREIGN KEY(transaction_id) REFERENCES transactions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS anniversaries (
    id TEXT PRIMARY KEY,
    person_id TEXT,
    title TEXT NOT NULL,
    date TEXT NOT NULL,
    is_lunar INTEGER DEFAULT 0,
    repeat_yearly INTEGER DEFAULT 1,
    remind_days TEXT DEFAULT '7,3,1,0',
    created_at TEXT NOT NULL,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS reminders (
    id TEXT PRIMARY KEY,
    person_id TEXT,
    ref_type TEXT NOT NULL,
    ref_id TEXT,
    title TEXT NOT NULL,
    due_at TEXT NOT NULL,
    status TEXT DEFAULT 'pending', -- pending/done
    created_at TEXT NOT NULL,
    completed_at TEXT,
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_reminders_status ON reminders(status, due_at);

CREATE TABLE IF NOT EXISTS attachments (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    file_name TEXT NOT NULL,
    stored_name TEXT NOT NULL,
    mime TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attach_entity ON attachments(entity_type, entity_id);

CREATE TABLE IF NOT EXISTS taggings (
    tag_id INTEGER NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    PRIMARY KEY(tag_id, target_type, target_id),
    FOREIGN KEY(tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS intimacy_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    person_id TEXT NOT NULL,
    day TEXT NOT NULL,
    score INTEGER NOT NULL,
    UNIQUE(person_id, day),
    FOREIGN KEY(person_id) REFERENCES people(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS notification_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ref_key TEXT NOT NULL,
    channel_url TEXT,
    title TEXT,
    body TEXT,
    success INTEGER NOT NULL,
    error TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notif_ref ON notification_logs(ref_key);
