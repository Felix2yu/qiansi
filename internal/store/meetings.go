package store

import (
	"context"
	"math"
	"sort"
	"time"
)

// 见面口径（N6）。
//
// 往来里除了见面（见面聊天、一起吃饭），还会记电话微信、纯送礼这类不见面
// 的活动。「上次见面」「见面间隔」「见面总览」只统计真正到场的往来：
// 事件类型字典上有「算见面」开关（event_types.is_meeting，见迁移 017），
// 没选类型的往来一律算见面——大多数往来确实是见面的，电话微信这类
// 少数类型由用户在设置里关掉。

// meetingEventCond 返回「一场往来算见面」的 SQL 条件，alias 是往来表的别名。
func meetingEventCond(alias string) string {
	return "(" + alias + ".type_id IS NULL OR " + alias +
		".type_id IN (SELECT id FROM event_types WHERE is_meeting=1))"
}

// meetingStatsCTE 按人聚合「算见面」的往来：次数、首见、末见。
// 日期取 substr 前 10 位（业务日期按本地 YYYY-MM-DD 存），空日期的往来
// 不属于任何一天，进不了间隔链，直接排除。
func meetingStatsCTE() string {
	return `meeting AS (
  SELECT ep.person_id, COUNT(*) AS cnt,
         MIN(substr(e.event_date,1,10)) AS first_d,
         MAX(substr(e.event_date,1,10)) AS last_d
  FROM event_participants ep JOIN live_events e ON e.id=ep.event_id
  WHERE substr(e.event_date,1,10)<>'' AND ` + meetingEventCond("e") + `
  GROUP BY ep.person_id)`
}

// avgMeetingGapDays 平均见面间隔：首见到末见的总天数 ÷（次数-1），四舍五入。
// 只见过一次（或日期读不懂）的人没有间隔可言，返回 0。
func avgMeetingGapDays(firstD, lastD string, count int) int {
	if count < 2 {
		return 0
	}
	first, ok1 := parseLocalDay(firstD)
	last, ok2 := parseLocalDay(lastD)
	if !ok1 || !ok2 {
		return 0
	}
	span := int(daytrunc(last).Sub(daytrunc(first)).Hours() / 24)
	if span <= 0 {
		return 0
	}
	return int(math.Round(float64(span) / float64(count-1)))
}

// MeetingOverviewRow 「见面总览」的一行：这个联系人上次见面是哪天、
// 见过几次、平均多久见一次。
type MeetingOverviewRow struct {
	PersonID     string `json:"person_id"`
	Name         string `json:"name"`
	Grade        int    `json:"grade"`
	MeetingCount int    `json:"meeting_count"`
	LastMeeting  string `json:"last_meeting"`
	DaysSince    int    `json:"days_since_last"`
	AvgGapDays   int    `json:"avg_gap_days,omitempty"`
}

// MeetingOverview 列出所有见过面的联系人，最久没见的排最前。
// 归档的人不出现（与渐远名单同口径）；本人由前端按 self_person_id 过滤，
// 后端不读设置，保持这个查询只碰往来数据。
func (s *Store) MeetingOverview(ctx context.Context) ([]*MeetingOverviewRow, error) {
	qry := `WITH ` + meetingStatsCTE() + `
SELECT p.id,p.name,p.grade,m.cnt,m.first_d,m.last_d
FROM live_people p JOIN meeting m ON m.person_id=p.id
WHERE p.archived=0`
	rows, err := s.DB.QueryContext(ctx, qry)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	today := daytrunc(time.Now())
	list := []*MeetingOverviewRow{}
	for rows.Next() {
		var r MeetingOverviewRow
		var firstD string
		if err := rows.Scan(&r.PersonID, &r.Name, &r.Grade, &r.MeetingCount, &firstD, &r.LastMeeting); err != nil {
			return nil, err
		}
		r.AvgGapDays = avgMeetingGapDays(firstD, r.LastMeeting, r.MeetingCount)
		if last, ok := parseLocalDay(r.LastMeeting); ok {
			r.DaysSince = int(today.Sub(daytrunc(last)).Hours() / 24)
		}
		list = append(list, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 最久没见的排最前，一眼看到「该约谁了」
	sort.SliceStable(list, func(i, j int) bool { return list[i].DaysSince > list[j].DaysSince })
	return list, nil
}
