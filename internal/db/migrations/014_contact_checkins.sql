-- 014：联系打卡（N5 跟进节奏）
--
-- 「该联系了」是按节奏派生出来的待办，不在 reminders 表里，所以勾掉它不能靠改行状态——
-- 得给这个人接上一个新的「接触锚点」，下一次到期日从现在往后算。
--
-- 这张表记的就是锚点：用户点了「已联系过」，但没真去记一场往来、一句话。
-- 它是人对系统的陈述，不是账目，所以不进时间线、不进导出明细，
-- 只参与一件事——最近一次接触的日期取所有来源的最大值。
--
-- 主键用 (person_id, day)：同一天连点两次不该把节奏往后挪两轮，
-- 也让「完成」这个动作天然幂等（INSERT OR IGNORE）。
CREATE TABLE IF NOT EXISTS contact_checkins (
    person_id TEXT NOT NULL,
    day TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (person_id, day),
    FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_contact_checkins_person ON contact_checkins(person_id);
